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

	"github.com/go-logr/logr"
	"github.com/stackitcloud/gardener-extension-acl/charts"
	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"github.com/stackitcloud/gardener-extension-acl/pkg/imagevector"

	openstackv1alpha1 "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/v1alpha1"
	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/chartrenderer"
	"github.com/gardener/gardener/pkg/utils/chart"
	managedresources "github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/pkg/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	istionetworkingClientGo "istio.io/client-go/pkg/apis/networking/v1alpha3"
)

const (
	// ActuatorName is only used for the logger instance
	ActuatorName       = "acl-actuator"
	ResourceNameSeed   = "acl-seed"
	ChartNameSeed      = "seed"
	IngressNamespace   = "istio-ingress"
	HashAnnotationName = "acl-ext-rule-hash"
	// ImageName is used for the image vector override.
	// This is currently not implemented correctly.
	// TODO implement
	ImageName         = "image-name"
	deletionTimeout   = 2 * time.Minute
	OpenstackTypeName = "openstack"
)

var (
	ErrSpecAction             = errors.New("action must either be 'ALLOW' or 'DENY'")
	ErrSpecRule               = errors.New("rule must be present")
	ErrSpecType               = errors.New("type must either be 'direct_remote_ip', 'remote_ip' or 'source_ip'")
	ErrSpecCIDR               = errors.New("CIDRs must not be empty")
	ErrNoExtensionsFound      = errors.New("could not list any extensions")
	ErrNoAdvertisedAddresses  = errors.New("advertised addresses are not available, likely because cluster creation has not yet completed")
	ErrProviderStatusRawIsNil = errors.New("providerStatus.Raw is nil, and can't be unmarshalled")
)

// NewActuator returns an actuator responsible for Extension resources.
func NewActuator(cfg config.Config) extension.Actuator {
	return &actuator{
		extensionConfig:    cfg,
		envoyfilterService: envoyfilters.EnvoyFilterService{},
	}
}

type actuator struct {
	client             client.Client
	config             *rest.Config
	decoder            runtime.Decoder
	extensionConfig    config.Config
	envoyfilterService envoyfilters.EnvoyFilterService
}

type ExtensionSpec struct {
	// Rule contain the user-defined Access Control Rule
	Rule *envoyfilters.ACLRule `json:"rule"`
}

// Reconcile the Extension resource.
func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	namespace := ex.GetNamespace()

	cluster, err := controller.GetCluster(ctx, a.client, namespace)
	if err != nil {
		return err
	}

	extSpec := &ExtensionSpec{}
	if ex.Spec.ProviderConfig != nil && ex.Spec.ProviderConfig.Raw != nil {
		if err := json.Unmarshal(ex.Spec.ProviderConfig.Raw, &extSpec); err != nil {
			return err
		}
	}

	aclMappings, err := a.getAllShootsWithACLExtension(ctx)
	if err != nil {
		return err
	}

	if err := ValidateExtensionSpec(extSpec); err != nil {
		return err
	}

	if err := a.updateEnvoyFilterHash(ctx, namespace, extSpec, false); err != nil {
		return err
	}

	hosts := make([]string, 0)

	if cluster.Shoot.Status.AdvertisedAddresses == nil || len(cluster.Shoot.Status.AdvertisedAddresses) < 1 {
		return ErrNoAdvertisedAddresses
	}

	for _, address := range cluster.Shoot.Status.AdvertisedAddresses {
		hosts = append(hosts, strings.Split(address.URL, "//")[1])
	}

	alwaysAllowedCIDRs := []string{
		*cluster.Shoot.Spec.Networking.Nodes,
		cluster.Seed.Spec.Networks.Pods,
	}

	infra := &extensionsv1alpha1.Infrastructure{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      cluster.Shoot.Name,
	}

	if err := a.client.Get(ctx, namespacedName, infra); err != nil {
		return err
	}

	if err := ConfigureProviderSpecificAllowedCIDRs(ctx, infra, &alwaysAllowedCIDRs); err != nil {
		return err
	}

	if err := a.createSeedResources(ctx, log, namespace, extSpec, cluster, hosts, alwaysAllowedCIDRs); err != nil {
		return err
	}

	if err := a.reconcileVPNEnvoyFilter(ctx, namespace, aclMappings, hosts, alwaysAllowedCIDRs); err != nil {
		return err
	}

	return a.updateStatus(ctx, ex, extSpec)
}

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

	if err := a.updateEnvoyFilterHash(ctx, namespace, nil, true); err != nil {
		return err
	}

	return a.deleteSeedResources(ctx, log, namespace)
}

// Restore the Extension resource.
func (a *actuator) Restore(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Reconcile(ctx, log, ex)
}

// Migrate the Extension resource.
func (a *actuator) Migrate(ctx context.Context, log logr.Logger, ex *extensionsv1alpha1.Extension) error {
	return a.Delete(ctx, log, ex)
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

func (a *actuator) reconcileVPNEnvoyFilter(
	ctx context.Context,
	namespace string,
	aclMappings []envoyfilters.ACLMapping,
	hosts []string,
	alwaysAllowedCIDRs []string,
) error {
	vpnEnvoyFilterSpec, err := a.envoyfilterService.BuildVPNEnvoyFilterSpecForHelmChart(
		aclMappings, hosts, alwaysAllowedCIDRs,
	)
	if err != nil {
		return err
	}

	// build EnvoyFilter object as map[string]interface{}
	// because the actual EnvoyFilter struct is a pain to type
	name := "acl-vpn"
	filterMap := map[string]interface{}{
		"apiVersion": "networking.istio.io/v1alpha3",
		"kind":       "EnvoyFilter",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": IngressNamespace,
		},
		"spec": vpnEnvoyFilterSpec,
	}

	filterJSON, err := json.Marshal(filterMap)
	if err != nil {
		return err
	}

	filter := &istionetworkingClientGo.EnvoyFilter{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: IngressNamespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, a.client, filter, func() error {
		return json.Unmarshal(filterJSON, filter)
	})

	return err
}

func (a *actuator) createSeedResources(
	ctx context.Context,
	log logr.Logger,
	namespace string,
	spec *ExtensionSpec,
	cluster *controller.Cluster,
	hosts []string,
	alwaysAllowedCIDRs []string,
) error {
	var err error

	apiEnvoyFilterSpec, err := a.envoyfilterService.BuildAPIEnvoyFilterSpecForHelmChart(
		spec.Rule, hosts, alwaysAllowedCIDRs,
	)
	if err != nil {
		return err
	}

	cfg := map[string]interface{}{
		"shootName":          cluster.Shoot.Name,
		"targetNamespace":    IngressNamespace,
		"apiEnvoyFilterSpec": apiEnvoyFilterSpec,
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
		false, // secretNameWithPrefix
		class,
		data,
		&keepObjects,
		injectedLabels,
		&forceOverwriteAnnotations,
	)
}

func (a *actuator) updateStatus(ctx context.Context, ex *extensionsv1alpha1.Extension, _ *ExtensionSpec) error {
	var resources []gardencorev1beta1.NamedResourceReference

	patch := client.MergeFrom(ex.DeepCopy())
	ex.Status.Resources = resources
	return a.client.Status().Patch(ctx, ex, patch)
}

func (a *actuator) updateEnvoyFilterHash(ctx context.Context, shootName string, extSpec *ExtensionSpec, inDeletion bool) error {
	// get envoyfilter with the shoot's name
	envoyFilter := &istionetworkingClientGo.EnvoyFilter{}
	namespacedName := types.NamespacedName{
		Namespace: IngressNamespace,
		Name:      shootName,
	}

	if err := a.client.Get(ctx, namespacedName, envoyFilter); err != nil {
		return client.IgnoreNotFound(err)
	}

	if inDeletion {
		delete(envoyFilter.Annotations, HashAnnotationName)
		return a.client.Update(ctx, envoyFilter)
	}

	// calculate the new hash
	newHashString, err := HashData(extSpec.Rule)
	if err != nil {
		return err
	}

	// get the annotation hash
	if envoyFilter.Annotations == nil {
		envoyFilter.Annotations = map[string]string{}
	}

	oldHashString, ok := envoyFilter.Annotations[HashAnnotationName]

	// set hash if not present or different
	if !ok || oldHashString != newHashString {
		envoyFilter.Annotations[HashAnnotationName] = newHashString
		return a.client.Update(ctx, envoyFilter)
	}

	// hash unchanged, do nothing
	return nil
}

// getAllShootsWithACLExtension returns a list of all shoots that have the ACL
// extension enabled, together with their rule.
func (a *actuator) getAllShootsWithACLExtension(ctx context.Context) ([]envoyfilters.ACLMapping, error) {
	extensions := &extensionsv1alpha1.ExtensionList{}
	err := a.client.List(ctx, extensions)
	if err != client.IgnoreNotFound(err) {
		return nil, err
	}

	if len(extensions.Items) < 1 {
		return nil, ErrNoExtensionsFound
	}

	mappings := []envoyfilters.ACLMapping{}

	for i := range extensions.Items {
		ex := &extensions.Items[i]
		if ex.Spec.Type != Type {
			continue
		}
		extSpec := &ExtensionSpec{}
		if ex.Spec.ProviderConfig != nil && ex.Spec.ProviderConfig.Raw != nil {
			if err := json.Unmarshal(ex.Spec.ProviderConfig.Raw, &extSpec); err != nil {
				return nil, err
			}
		}

		mappings = append(mappings, envoyfilters.ACLMapping{
			ShootName: ex.Namespace,
			Rule:      *extSpec.Rule,
		})
	}
	return mappings, nil
}

func ConfigureProviderSpecificAllowedCIDRs(
	ctx context.Context,
	infra *extensionsv1alpha1.Infrastructure,
	alwaysAllowedCIDRs *[]string,
) error {
	//nolint:gocritic // Will likely be extended with other infra types in the future
	switch infra.Spec.Type {
	case OpenstackTypeName:
		// This would be our preferred solution:
		//
		// infraStatus, err := openstackhelper.InfrastructureStatusFromRaw(infra.Status.ProviderStatus)
		//
		// but decoding isn't possible. We suspect it's because in the function
		// infraStatus is assigned like this:
		//
		// infraStatus := &InfrasttuctureStatus{}
		//
		// instead of doing it without a pointer and then referencing it in the
		// unmarshal step, like we now have to do manually here:
		if infra.Status.ProviderStatus == nil || infra.Status.ProviderStatus.Raw == nil {
			return ErrProviderStatusRawIsNil
		}

		infraStatus := openstackv1alpha1.InfrastructureStatus{}
		err := json.Unmarshal(infra.Status.ProviderStatus.Raw, &infraStatus)
		if err != nil {
			return err
		}

		router32CIDR := infraStatus.Networks.Router.IP + "/32"

		*alwaysAllowedCIDRs = append(*alwaysAllowedCIDRs, router32CIDR)
	}
	return nil
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
