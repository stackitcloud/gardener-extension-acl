// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved.
// This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
	"github.com/stackitcloud/gardener-extension-acl/pkg/imagevector"

	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/chartrenderer"
	"github.com/gardener/gardener/pkg/utils/chart"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	managedresources "github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	istioapisecurityv1beta1 "istio.io/api/security/v1beta1"
	istiov1beta1 "istio.io/api/type/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// ActuatorName is only used for the logger instance
	ActuatorName     = "acl-actuator"
	ResourceNameSeed = "acl-seed"
	ChartNameSeed    = "seed"
	// ImageName is used for the image vector override.
	// This is currently not implemented correctly.
	// TODO implement
	ImageName       = "image-name"
	deletionTimeout = 2 * time.Minute
)

// NewActuator returns an actuator responsible for Extension resources.
func NewActuator(cfg config.Config) extension.Actuator {
	return &actuator{
		logger:          log.Log.WithName(ActuatorName),
		extensionConfig: cfg,
	}
}

type actuator struct {
	client          client.Client
	config          *rest.Config
	decoder         runtime.Decoder
	extensionConfig config.Config

	logger logr.Logger
}

type ExtensionSpec struct {
	// Action is the action to take on the source of request.
	Action *string
	// IPBlocks is list of IP blocks (Ipv4 & Ipv6), populated from the source address of the IP packet.
	// Single IP (e.g. "1.2.3.4") and CIDR (e.g. "1.2.3.0/24") are supported.
	IPBlocks []string
	// NotIPBlocks is a list of negative match of IP blocks.
	NotIPBlocks []string
	// RemoteIPBlocks is a list of IP blocks, populated from X-Forwarded-For header or proxy protocol.
	// Single IP (e.g. “1.2.3.4”) and CIDR (e.g. “1.2.3.0/24”) are supported.
	RemoteIPBlocks []string
	// NotRemoteIPBlocks is a list of negative match of remote IP blocks.
	NotRemoteIPBlocks []string
}

// Reconcile the Extension resource.
func (a *actuator) Reconcile(ctx context.Context, ex *extensionsv1alpha1.Extension) error {
	namespace := ex.GetNamespace()

	cluster, err := controller.GetCluster(ctx, a.client, namespace)
	if err != nil {
		return err
	}

	// TODO unless you put anything in the ProviderConfig field of the extension
	// object, this Unmarshal will fail with an invalid nil pointer dereference
	extSpec := &ExtensionSpec{}
	if err := json.Unmarshal(ex.Spec.ProviderConfig.Raw, &extSpec); err != nil {
		return err
	}

	if err := a.createSeedResources(ctx, extSpec, cluster, namespace); err != nil {
		return err
	}

	return a.updateStatus(ctx, ex, extSpec)
}

// Delete the Extension resource.
func (a *actuator) Delete(ctx context.Context, ex *extensionsv1alpha1.Extension) error {
	namespace := ex.GetNamespace()
	a.logger.Info("Component is being deleted", "component", "", "namespace", namespace)

	return a.deleteSeedResources(ctx, namespace)
}

// Restore the Extension resource.
func (a *actuator) Restore(ctx context.Context, ex *extensionsv1alpha1.Extension) error {
	// TODO If your extension needs to restore something from its state
	//
	// If everything concerning your extension can be restored by a simple
	// reconciliation (i.e. if it's stateless), you do not need to do anything
	// here. Otherwise, you have to recreate resources from the extension's
	// Status.Resources field before you can trigger the reconcile.
	return a.Reconcile(ctx, ex)
}

// Migrate the Extension resource.
func (a *actuator) Migrate(ctx context.Context, ex *extensionsv1alpha1.Extension) error {
	return a.Delete(ctx, ex)
}

// InjectConfig injects the rest config to this actuator.
func (a *actuator) InjectConfig(cfg *rest.Config) error {
	a.config = cfg
	return nil
}

// InjectClient injects the controller runtime client into the reconciler.
func (a *actuator) InjectClient(c client.Client) error {
	a.client = c
	return nil
}

// InjectScheme injects the given scheme into the reconciler.
func (a *actuator) InjectScheme(scheme *runtime.Scheme) error {
	a.decoder = serializer.NewCodecFactory(scheme, serializer.EnableStrict).UniversalDecoder()
	return nil
}

// TODO use the extension spec and cluster resources, or remove them from
// the function
func (a *actuator) createSeedResources(ctx context.Context, spec *ExtensionSpec, cluster *controller.Cluster, namespace string) error {
	var err error

	hosts := []string{
		"localhost",
		"test.me.svc",
	}

	apiServerRule, err := a.getAccessControlAPIServerSpec(spec, hosts)
	vpnServerRule, err := a.getAccessControlVPNServerSpec(spec, cluster.ObjectMeta.Name)

	cfg := map[string]interface{}{
		"shootName":       cluster.Shoot.Name,
		"targetNamespace": "istio-ingress",
		"apiServerRule":   apiServerRule,
		"vpnRule":         vpnServerRule,
	}

	cfg, err = chart.InjectImages(cfg, imagevector.ImageVector(), []string{ImageName})
	if err != nil {
		return fmt.Errorf("failed to find image version for %s: %v", ImageName, err)
	}

	renderer, err := chartrenderer.NewForConfig(a.config)
	if err != nil {
		return errors.Wrap(err, "could not create chart renderer")
	}

	a.logger.Info("Component is being applied", "component", "component-name", "namespace", namespace)

	return a.createManagedResource(ctx, namespace, ResourceNameSeed, "seed", renderer, ChartNameSeed, namespace, cfg, nil)
}

func (a *actuator) deleteSeedResources(ctx context.Context, namespace string) error {
	a.logger.Info("Deleting managed resource for seed", "namespace", namespace)

	// TODO this code block is only needed if you have unmanaged resources to delete
	if err := kutil.DeleteObjects(ctx, a.client,
		// TODO specify resources to be deleted that are not part of a ManagedResource
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-name", Namespace: namespace}},
	); err != nil {
		return err
	}

	if err := managedresources.Delete(ctx, a.client, namespace, ResourceNameSeed, false); err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, deletionTimeout)
	defer cancel()
	return managedresources.WaitUntilDeleted(timeoutCtx, a.client, namespace, ResourceNameSeed)
}

func (a *actuator) createManagedResource(
	ctx context.Context,
	namespace, name, class string,
	renderer chartrenderer.Interface,
	chartName, chartNamespace string,
	chartValues map[string]interface{},
	injectedLabels map[string]string,
) error {
	chartPath := filepath.Join(a.extensionConfig.ChartPath, chartName)
	renderedChart, err := renderer.Render(chartPath, chartName, chartNamespace, chartValues)
	if err != nil {
		return err
	}

	data := map[string][]byte{chartName: renderedChart.Manifest()}
	keepObjects := false
	forceOverwriteAnnotations := false
	return managedresources.Create(
		ctx, a.client, namespace, name, false, class, data, &keepObjects, injectedLabels, &forceOverwriteAnnotations,
	)
}

// TODO collect resources that you want to put in the status
//
// shoot-cert-service uses ResourceReferences to do this, but this doesn't
// convey any information about the resource other than its name. So, if you
// want to restore any information after a migration (of resources that are
// not part of a ManagedResource), you need to save it here.
func (a *actuator) updateStatus(ctx context.Context, ex *extensionsv1alpha1.Extension, _ *ExtensionSpec) error {
	var resources []gardencorev1beta1.NamedResourceReference

	patch := client.MergeFrom(ex.DeepCopy())
	ex.Status.Resources = resources
	return a.client.Status().Patch(ctx, ex, patch)
}

func (a *actuator) getAccessControlSpec(spec *ExtensionSpec) (*istioapisecurityv1beta1.AuthorizationPolicy, error) {
	if spec == nil {
		return nil, errors.New("spec is nil")
	}

	ac := *spec
	action := istioapisecurityv1beta1.AuthorizationPolicy_ALLOW

	var err error
	if ac.Action != nil {
		action, err = toIstioAuthPolicyAction(*ac.Action)
	}
	if err != nil {
		return nil, err
	}

	rules := []*istioapisecurityv1beta1.Rule{{
		From: []*istioapisecurityv1beta1.Rule_From{{
			Source: &istioapisecurityv1beta1.Source{
				IpBlocks:          notNilSlice(ac.IPBlocks),
				NotIpBlocks:       notNilSlice(ac.NotIPBlocks),
				RemoteIpBlocks:    notNilSlice(ac.RemoteIPBlocks),
				NotRemoteIpBlocks: notNilSlice(ac.NotRemoteIPBlocks),
			},
		}},
	}}

	accessControlSpec := istioapisecurityv1beta1.AuthorizationPolicy{
		Selector: &istiov1beta1.WorkloadSelector{
			// TODO get these dynamically
			MatchLabels: map[string]string{
				"app":   "istio-ingressgateway",
				"istio": "ingressgateway",
			},
		},
		Action: action,
		Rules:  rules,
	}
	return &accessControlSpec, nil
}

func (a *actuator) getAccessControlAPIServerSpec(spec *ExtensionSpec, hosts []string) (*istioapisecurityv1beta1.AuthorizationPolicy, error) {
	control, err := a.getAccessControlSpec(spec)
	if err != nil {
		return nil, err
	}

	for i := range control.Rules {
		control.Rules[i].When = []*istioapisecurityv1beta1.Condition{{
			Key:    "connection.sni",
			Values: hosts,
		}}
	}

	return control, nil
}

func (a *actuator) getAccessControlVPNServerSpec(spec *ExtensionSpec, namespace string) (*istioapisecurityv1beta1.AuthorizationPolicy, error) {
	control, err := a.getAccessControlSpec(spec)
	if err != nil {
		return nil, err
	}

	for i := range control.Rules {
		control.Rules[i].When = []*istioapisecurityv1beta1.Condition{{
			Key:    "request.headers[reversed-vpn]",
			Values: []string{fmt.Sprintf("outbound|1194||vpn-seed-server.%s.svc.cluster.local", namespace)},
		}}
	}

	return control, nil
}

// notNilSlice returns either the passed slice or an empty slice (not nil) if the length is zero.
func notNilSlice[T any](t []T) []T {
	if len(t) > 0 {
		return t
	}
	return []T{}
}

func toIstioAuthPolicyAction(action string) (istioapisecurityv1beta1.AuthorizationPolicy_Action, error) {
	switch action {
	case "ALLOW":
		return istioapisecurityv1beta1.AuthorizationPolicy_ALLOW, nil
	case "DENY":
		return istioapisecurityv1beta1.AuthorizationPolicy_DENY, nil
	default:
		return istioapisecurityv1beta1.AuthorizationPolicy_Action(0), fmt.Errorf("unsupported authorization policy action: %s", action)
	}
}
