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
	"crypto/sha256"
	"embed"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	v1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/chartrenderer"
	"github.com/gardener/gardener/pkg/utils/chart"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	istionetworkv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istionetworkv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-acl/charts"
	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"github.com/stackitcloud/gardener-extension-acl/pkg/helper"
	"github.com/stackitcloud/gardener-extension-acl/pkg/imagevector"
)

const (
	// ActuatorName is only used for the logger instance
	ActuatorName = "acl-actuator"
	// ResourceNameSeed is name of the managedResource object
	ResourceNameSeed = "acl-seed"
	// ChartNameSeed name of the helm chart
	ChartNameSeed = "seed"
	// IngressNamespace is the namespace of the istio
	IngressNamespace = "istio-ingress"
	// HashAnnotationName name of annotation for triggering the envoyfilter webhook
	// DEPRECATED: Remove after annotation has been removed from all EnvoyFilters
	HashAnnotationName = "acl-ext-rule-hash"
	// ImageName is used for the image vector override.
	// This is currently not implemented correctly.
	// TODO implement
	ImageName        = "image-name"
	deletionTimeout  = 2 * time.Minute
	istioGatewayName = "kube-apiserver"
)

// Error variables for controller pkg
var (
	ErrSpecAction            = errors.New("action must either be 'ALLOW' or 'DENY'")
	ErrSpecRule              = errors.New("rule must be present")
	ErrSpecType              = errors.New("type must either be 'direct_remote_ip', 'remote_ip' or 'source_ip'")
	ErrSpecCIDR              = errors.New("CIDRs must not be empty")
	ErrNoExtensionsFound     = errors.New("could not list any extensions")
	ErrNoAdvertisedAddresses = errors.New("advertised addresses are not available, likely because cluster creation has not yet completed")
)

// ExtensionState contains the State of the Extension
type ExtensionState struct {
	IstioNamespace *string `json:"istioNamespace"`
}

// NewActuator returns an actuator responsible for Extension resources.
func NewActuator(mgr manager.Manager, cfg config.Config) extension.Actuator {
	return &actuator{
		extensionConfig: cfg,
		client:          mgr.GetClient(),
		config:          mgr.GetConfig(),
		decoder:         serializer.NewCodecFactory(mgr.GetScheme(), serializer.EnableStrict).UniversalDecoder(),
	}
}

type actuator struct {
	client          client.Client
	config          *rest.Config
	decoder         runtime.Decoder
	extensionConfig config.Config
}

// ExtensionSpec is the content of the ProviderConfig of the acl extension object
type ExtensionSpec struct {
	// Rule contain the user-defined Access Control Rule
	Rule *envoyfilters.ACLRule `json:"rule"`
}

// Reconcile the Extension resource.
//
//nolint:gocyclo // this is the main reconcile loop
func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	cluster, err := helper.GetClusterForExtension(ctx, a.client, ex)
	if err != nil {
		return err
	}

	extSpec := &ExtensionSpec{}
	if ex.Spec.ProviderConfig != nil && ex.Spec.ProviderConfig.Raw != nil {
		if err := json.Unmarshal(ex.Spec.ProviderConfig.Raw, &extSpec); err != nil {
			return err
		}
	}
	// validate the ExtensionSpec
	if err := ValidateExtensionSpec(extSpec); err != nil {
		return err
	}

	istioNamespace, istioLabels, err := a.findIstioNamespaceForExtension(ctx, ex)
	if err != nil {
		// we ignore errors for hibernated clusters if they don't have a Gateway
		// resource for the extension to get the istio namespace from
		if controller.IsHibernated(cluster) {
			return client.IgnoreNotFound(err)
		}
		return err
	}

	extState, err := getExtensionState(ex)
	if err != nil {
		return err
	}

	if err := a.triggerWebhook(ctx, ex.GetNamespace(), istioNamespace); err != nil {
		return err
	}

	hosts := make([]string, 0)
	if cluster.Shoot.Status.AdvertisedAddresses == nil || len(cluster.Shoot.Status.AdvertisedAddresses) < 1 {
		return ErrNoAdvertisedAddresses
	}

	for _, address := range cluster.Shoot.Status.AdvertisedAddresses {
		hosts = append(hosts, strings.Split(address.URL, "//")[1])
	}
	var shootSpecificCIDRs []string
	var alwaysAllowedCIDRs []string

	alwaysAllowedCIDRs = append(alwaysAllowedCIDRs, helper.GetSeedSpecificAllowedCIDRs(cluster.Seed)...)

	if len(a.extensionConfig.AdditionalAllowedCIDRs) >= 1 {
		alwaysAllowedCIDRs = append(alwaysAllowedCIDRs, a.extensionConfig.AdditionalAllowedCIDRs...)
	}

	// Gardener supports workerless Shoots. These don't have an associated
	// Infrastructure object and don't need Node- or Pod-specific CIDRs to be
	// allowed. Therefore, skip these steps for workerless Shoots.
	if !v1beta1helper.IsWorkerless(cluster.Shoot) {
		shootSpecificCIDRs = append(shootSpecificCIDRs, helper.GetShootNodeSpecificAllowedCIDRs(cluster.Shoot)...)

		infra, err := helper.GetInfrastructureForExtension(ctx, a.client, ex, cluster.Shoot.Name)
		if err != nil {
			return err
		}

		providerSpecificCIRDs, err := helper.GetProviderSpecificAllowedCIDRs(infra)
		if err != nil {
			return err
		}

		shootSpecificCIDRs = append(shootSpecificCIDRs, providerSpecificCIRDs...)
	}

	if err := a.createSeedResources(
		ctx,
		log,
		ex.GetNamespace(),
		extSpec,
		cluster,
		hosts,
		shootSpecificCIDRs,
		alwaysAllowedCIDRs,
		istioNamespace,
		istioLabels,
	); err != nil {
		return err
	}

	if err := a.reconcileVPNEnvoyFilter(ctx, alwaysAllowedCIDRs, istioNamespace, istioLabels); err != nil {
		return err
	}

	if extState.IstioNamespace != nil && *extState.IstioNamespace != istioNamespace {
		// we need to cleanup the old vpn object if the istioNamespace changed
		if err := a.reconcileVPNEnvoyFilter(ctx, alwaysAllowedCIDRs, *extState.IstioNamespace, nil); err != nil {
			return err
		}
	}

	extState.IstioNamespace = &istioNamespace

	return a.updateStatus(ctx, ex, extState)
}

// ValidateExtensionSpec checks if the ExtensionSpec exists, and if its action,
// type and CIDRs are valid.
func ValidateExtensionSpec(spec *ExtensionSpec) error {
	rule := spec.Rule

	if rule == nil {
		return ErrSpecRule
	}

	// action
	a := strings.ToLower(rule.Action)
	if a != "allow" && a != "deny" {
		return ErrSpecAction
	}

	// type
	t := strings.ToLower(rule.Type)
	if t != "direct_remote_ip" &&
		t != "remote_ip" &&
		t != "source_ip" {
		return ErrSpecType
	}

	// cidrs
	if len(rule.Cidrs) < 1 {
		return ErrSpecCIDR
	}

	for ii := range rule.Cidrs {
		_, mask, err := net.ParseCIDR(rule.Cidrs[ii])
		if err != nil {
			return err
		}
		if mask == nil {
			return ErrSpecCIDR
		}
	}

	return nil
}

// Delete the Extension resource.
func (a *actuator) Delete(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	namespace := ex.GetNamespace()
	log.Info("Component is being deleted", "component", "", "namespace", namespace)

	if err := a.deleteSeedResources(ctx, log, namespace); err != nil {
		return err
	}

	var istioNamespace string

	istioNamespace, _, err := a.findIstioNamespaceForExtension(ctx, ex)
	if client.IgnoreNotFound(err) != nil {
		return err
	}
	if apierrors.IsNotFound(err) {
		exState, err := getExtensionState(ex)
		if err != nil {
			return err
		}
		if exState.IstioNamespace == nil {
			// we have never reconciled this cluster completely, therefore no
			// cleanup needs to be performed
			return nil
		}

		// the cluster has no Gateway object, but we can get the information
		// from the extension state
		istioNamespace = *exState.IstioNamespace
	}

	return a.triggerWebhook(ctx, namespace, istioNamespace)
}

// ForceDelete implements Network.Actuator.
func (a *actuator) ForceDelete(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Delete(ctx, log, ex)
}

// Restore the Extension resource.
func (a *actuator) Restore(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Reconcile(ctx, log, ex)
}

// Migrate the Extension resource.
func (a *actuator) Migrate(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Delete(ctx, log, ex)
}

func (a *actuator) reconcileVPNEnvoyFilter(
	ctx context.Context,
	alwaysAllowedCIDRs []string,
	istioNamespace string,
	istioLabels map[string]string,
) error {
	aclMappings, istioLabelsFromExt, err := a.getAllShootsWithACLExtension(ctx, istioNamespace)
	if err != nil {
		return err
	}

	// build EnvoyFilter object as map[string]interface{}
	// because the actual EnvoyFilter struct is a pain to type
	name := "acl-vpn"

	// build EnvoyFilter object as map[string]interface{}
	// because the actual EnvoyFilter struct is a pain to type
	envoyFilter := &unstructured.Unstructured{}
	envoyFilter.SetGroupVersionKind(istionetworkv1alpha3.SchemeGroupVersion.WithKind("EnvoyFilter"))
	envoyFilter.SetNamespace(istioNamespace)
	envoyFilter.SetName(name)

	if len(aclMappings) == 0 {
		// no shoot in this namespace with the ACL extension, so we can delete the config
		err = a.client.Delete(ctx, envoyFilter)
		return client.IgnoreNotFound(err)
	}
	if istioLabels == nil {
		istioLabels = istioLabelsFromExt
	}

	vpnEnvoyFilterSpec, err := envoyfilters.BuildVPNEnvoyFilterSpecForHelmChart(
		aclMappings, alwaysAllowedCIDRs, istioLabels,
	)
	if err != nil {
		return err
	}

	err = a.client.Get(ctx, client.ObjectKeyFromObject(envoyFilter), envoyFilter)
	if client.IgnoreNotFound(err) != nil {
		return err
	}

	envoyFilter.Object["spec"] = vpnEnvoyFilterSpec

	if apierrors.IsNotFound(err) {
		return a.client.Create(ctx, envoyFilter)
	}

	return a.client.Update(ctx, envoyFilter)
}

func (a *actuator) createSeedResources(
	ctx context.Context,
	log logr.Logger,
	namespace string,
	spec *ExtensionSpec,
	cluster *controller.Cluster,
	hosts []string,
	shootSpecificCIRDs []string,
	alwaysAllowedCIDRs []string,
	istioNamespace string,
	istioLabels map[string]string,
) error {
	var err error

	alwaysAllowedCIDRs = append(alwaysAllowedCIDRs, shootSpecificCIRDs...)

	apiEnvoyFilterSpec, err := envoyfilters.BuildAPIEnvoyFilterSpecForHelmChart(
		spec.Rule, hosts, alwaysAllowedCIDRs, istioLabels,
	)
	if err != nil {
		return err
	}

	defaultLabels, err := a.findDefaultIstioLabels(ctx)
	if err != nil {
		return err
	}

	ingressEnvoyFilterSpec := envoyfilters.BuildIngressEnvoyFilterSpecForHelmChart(
		cluster, spec.Rule, alwaysAllowedCIDRs, defaultLabels)

	cfg := map[string]interface{}{
		"shootName":              cluster.Shoot.Status.TechnicalID,
		"targetNamespace":        istioNamespace,
		"apiEnvoyFilterSpec":     apiEnvoyFilterSpec,
		"ingressEnvoyFilterSpec": ingressEnvoyFilterSpec,
	}

	cfg, err = chart.InjectImages(cfg, imagevector.ImageVector(), []string{ImageName})
	if err != nil {
		return fmt.Errorf("failed to find image version for %s: %v", ImageName, err)
	}

	renderer, err := chartrenderer.NewForConfig(a.config)
	if err != nil {
		return errors.Wrap(err, "could not create chart renderer")
	}

	log.Info("Component is being applied", "component", "component-name", "namespace", namespace)

	return a.createManagedResource(ctx, namespace, ResourceNameSeed, "seed", renderer, ChartNameSeed, namespace, cfg, nil, charts.Seed)
}

func (a *actuator) deleteSeedResources(ctx context.Context, log logr.Logger, namespace string) error {
	log.Info("Deleting managed resource for seed", "namespace", namespace)

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
	embeddedChart embed.FS,
) error {
	renderedChart, err := renderer.RenderEmbeddedFS(embeddedChart, chartName, chartName, chartNamespace, chartValues)
	if err != nil {
		return err
	}

	data := map[string][]byte{chartName: renderedChart.Manifest()}
	keepObjects := false
	forceOverwriteAnnotations := false
	return managedresources.Create(
		ctx,
		a.client,
		namespace,
		name,
		map[string]string{}, // labels
		false,               // secretNameWithPrefix
		class,
		data,
		&keepObjects,
		injectedLabels,
		&forceOverwriteAnnotations,
	)
}

func (a *actuator) updateStatus(
	ctx context.Context,
	ex *extensionsv1alpha1.Extension,
	state *ExtensionState,
) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}

	patch := client.MergeFrom(ex.DeepCopy())

	ex.Status.State = &runtime.RawExtension{Raw: stateJSON}
	return a.client.Status().Patch(ctx, ex, patch)
}

func getExtensionState(ex *extensionsv1alpha1.Extension) (*ExtensionState, error) {
	extState := &ExtensionState{}
	if ex.Status.State != nil && ex.Status.State.Raw != nil {
		if err := json.Unmarshal(ex.Status.State.Raw, &extState); err != nil {
			return nil, err
		}
	}

	return extState, nil
}

// triggerWebhook allows us to "reconcile" the existing EnvoyFilter which we
// need to modify using a mutating webhook. This is achieved by sending an empty
// patch for this EnvoyFilter, which invokes the ACL webhook component. This
// makes sure this EnvoyFilter is kept in sync with the ACL extension config
// (e.g. when the allowed CIDRS from the extension spec or the
// alwaysAllowedCIDRs change).
func (a *actuator) triggerWebhook(ctx context.Context, shootName, istioNamespace string) error {
	// get envoyfilter with the shoot's name
	envoyFilter := &istionetworkv1alpha3.EnvoyFilter{}
	namespacedName := types.NamespacedName{
		Namespace: istioNamespace,
		Name:      shootName,
	}

	if err := a.client.Get(ctx, namespacedName, envoyFilter); err != nil {
		return client.IgnoreNotFound(err)
	}

	// TODO remove migration code: previously, hash annotations have been used
	// to check if the rule set of an ACL extension object had changed. If that
	// was the case, the changed hash annotation would trigger the webhook.
	// Sending an empty patch is better, as it's 1) easier and 2) also updates
	// the EnvoyFilter when the alwaysAllowedCIDRs changed, which wasn't
	// correctly handled before
	// --> migration code start
	if _, ok := envoyFilter.Annotations[HashAnnotationName]; ok {
		delete(envoyFilter.Annotations, HashAnnotationName)
		return a.client.Update(ctx, envoyFilter)
	}
	// --> migration code end

	return a.client.Patch(ctx, envoyFilter, client.RawPatch(types.MergePatchType, []byte("{}")))
}

// getAllShootsWithACLExtension returns a list of all shoots that have the ACL
// extension enabled, together with their rule.
func (a *actuator) getAllShootsWithACLExtension(
	ctx context.Context, istioNamespace string,
) ([]envoyfilters.ACLMapping, map[string]string, error) {
	extensions := &extensionsv1alpha1.ExtensionList{}
	err := a.client.List(ctx, extensions)
	if err != client.IgnoreNotFound(err) {
		return nil, nil, err
	}

	if len(extensions.Items) < 1 {
		return nil, nil, ErrNoExtensionsFound
	}

	mappings := []envoyfilters.ACLMapping{}

	var istioLabels map[string]string

	for i := range extensions.Items {
		ex := &extensions.Items[i]
		if ex.Spec.Type != Type {
			continue
		}

		var shootIstioNamespace string
		var shootIstioLabels map[string]string

		shootIstioNamespace, shootIstioLabels, err = a.findIstioNamespaceForExtension(ctx, &extensions.Items[i])
		if client.IgnoreNotFound(err) != nil {
			return nil, nil, err
		}
		// If we don't find a Gateway object to get the Istio Namespace from, we
		// try the extension status as a fallback. If both aren't available, we
		// ignore the Shoot's ACL rules entirely in this pass. This can only
		// occur when the ACL extension for the Shoot in question itself has
		// never been reconciled before.
		if apierrors.IsNotFound(err) {
			extState, err := getExtensionState(ex)
			if err != nil {
				return nil, nil, err
			}
			if extState.IstioNamespace == nil {
				continue
			}

			shootIstioNamespace = *extState.IstioNamespace
		}

		if istioNamespace != shootIstioNamespace {
			continue
		}

		if istioLabels == nil {
			istioLabels = shootIstioLabels
		}

		extSpec := &ExtensionSpec{}
		if ex.Spec.ProviderConfig != nil && ex.Spec.ProviderConfig.Raw != nil {
			if err := json.Unmarshal(ex.Spec.ProviderConfig.Raw, &extSpec); err != nil {
				return nil, nil, err
			}
		}

		cluster, err := controller.GetCluster(ctx, a.client, ex.GetNamespace())
		if err != nil {
			return nil, nil, err
		}

		var shootSpecificCIDRs []string

		// Gardener supports workerless Shoots. These don't have an associated
		// Infrastructure object and don't need Node- or Pod-specific CIDRs to be
		// allowed. Therefore, skip these steps for workerless Shoots.
		if !v1beta1helper.IsWorkerless(cluster.Shoot) {
			infra := &extensionsv1alpha1.Infrastructure{}
			namespacedName := types.NamespacedName{
				Namespace: ex.GetNamespace(),
				Name:      cluster.Shoot.Name,
			}

			if err := a.client.Get(ctx, namespacedName, infra); err != nil {
				return nil, nil, err
			}

			shootSpecificCIDRs = append(shootSpecificCIDRs, helper.GetShootNodeSpecificAllowedCIDRs(cluster.Shoot)...)
			providerSpecificCIRDs, err := helper.GetProviderSpecificAllowedCIDRs(infra)
			if err != nil {
				return nil, nil, err
			}
			shootSpecificCIDRs = append(shootSpecificCIDRs, providerSpecificCIRDs...)
		}

		mappings = append(mappings, envoyfilters.ACLMapping{
			ShootName:          ex.Namespace,
			Rule:               *extSpec.Rule,
			ShootSpecificCIDRs: shootSpecificCIDRs,
		})
	}
	return mappings, istioLabels, nil
}

// HashData returns a 16 char hash for the given object.
func HashData(data interface{}) (string, error) {
	var jsonSpec []byte
	var err error
	if jsonSpec, err = json.Marshal(data); err != nil {
		return "", err
	}

	bytes := sha256.Sum256(jsonSpec)
	return strings.ToLower(base32.StdEncoding.EncodeToString(bytes[:]))[:16], nil
}

// findIstioNamespaceForExtension finds the Istio namespace by the Istio Gateway
// object named "kube-apiserver", which is expected to be present in every
// Shoot namespace (except when the Shoot is hibernated - in this case, the
// function returns a NotFoundError which the caller should handle).
//
// The Gateway object has a Selector field that selects a Deployment in the
// namespace we need. We list Deployments filtered by the labelSelector and
// return the namespace of the returned Deployment.
func (a *actuator) findIstioNamespaceForExtension(
	ctx context.Context, ex *extensionsv1alpha1.Extension,
) (
	istioNamespace string,
	istioLabels map[string]string,
	err error,
) {
	gw := istionetworkv1beta1.Gateway{}

	err = a.client.Get(ctx, client.ObjectKey{
		Namespace: ex.Namespace,
		Name:      istioGatewayName,
	}, &gw)
	if err != nil {
		return "", nil, err
	}

	labelsSelector := client.MatchingLabels(gw.Spec.Selector)

	deployments := appsv1.DeploymentList{}
	err = a.client.List(ctx, &deployments, labelsSelector)
	if err != nil {
		return "", nil, err
	}
	if len(deployments.Items) != 1 {
		return "", nil, fmt.Errorf("no istio namespace could be selected, because the number of deployments found is %d", len(deployments.Items))
	}

	return deployments.Items[0].Namespace, gw.Spec.Selector, nil
}

func (a *actuator) findDefaultIstioLabels(
	ctx context.Context,
) (
	istioLabels map[string]string,
	err error,
) {
	gwl := istionetworkv1beta1.GatewayList{}

	err = a.client.List(ctx, &gwl, &client.ListOptions{Namespace: v1beta1constants.GardenNamespace})
	if err != nil {
		return nil, err
	}
	if len(gwl.Items) == 0 {
		return nil, errors.New("no ingress gateway found in namespace")
	}
	return gwl.Items[0].Spec.Selector, nil
}
