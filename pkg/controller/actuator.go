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
	"maps"
	"net"
	"strings"
	"time"

	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/chartrenderer"
	"github.com/gardener/gardener/pkg/utils/chart"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	istionetworkv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istionetworkv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	v1 "k8s.io/api/core/v1"
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
	HashAnnotationName = "acl-ext-rule-hash"
	// ImageName is used for the image vector override.
	// This is currently not implemented correctly.
	// TODO implement
	ImageName       = "image-name"
	deletionTimeout = 2 * time.Minute
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
		return err
	}

	extState := &ExtensionState{}
	if ex.Status.State != nil && ex.Status.State.Raw != nil {
		if err := json.Unmarshal(ex.Status.State.Raw, &extState); err != nil {
			return err
		}
	}

	if err := a.updateEnvoyFilterHash(ctx, ex.GetNamespace(), extSpec, istioNamespace, false); err != nil {
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
	return a.updateStatus(ctx, ex, extSpec, extState)
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

	istioNamespace, _, err := a.findIstioNamespaceForExtension(ctx, ex)
	if err != nil {
		return err
	}

	if err := a.updateEnvoyFilterHash(ctx, namespace, nil, istioNamespace, true); err != nil {
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
		// no shoot in this namespace with the acl extension so we can delete the config
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

	apiEnvoyFilterSpec, err := envoyfilters.BuildAPIEnvoyFilterSpecForHelmChart(
		spec.Rule, hosts, append(alwaysAllowedCIDRs, shootSpecificCIRDs...), istioLabels,
	)
	if err != nil {
		return err
	}

	cfg := map[string]interface{}{
		"shootName":          cluster.Shoot.Status.TechnicalID,
		"targetNamespace":    istioNamespace,
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
	_ *ExtensionSpec,
	state *ExtensionState,
) error {
	var resources []gardencorev1beta1.NamedResourceReference

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}

	patch := client.MergeFrom(ex.DeepCopy())
	ex.Status.Resources = resources

	ex.Status.State = &runtime.RawExtension{Raw: stateJSON}
	return a.client.Status().Patch(ctx, ex, patch)
}

func (a *actuator) updateEnvoyFilterHash(
	ctx context.Context,
	shootName string,
	extSpec *ExtensionSpec,
	istioNamespace string,
	inDeletion bool,
) error {
	// get envoyfilter with the shoot's name
	envoyFilter := &istionetworkv1alpha3.EnvoyFilter{}
	namespacedName := types.NamespacedName{
		Namespace: istioNamespace,
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

	var istioLables map[string]string

	for i := range extensions.Items {
		ex := &extensions.Items[i]
		if ex.Spec.Type != Type {
			continue
		}

		var shootIstioNamespace string
		var shootIstioLables map[string]string

		shootIstioNamespace, shootIstioLables, err = a.findIstioNamespaceForExtension(ctx, &extensions.Items[i])
		if err != nil {
			return nil, nil, err
		}
		if istioNamespace != shootIstioNamespace {
			continue
		}

		if istioLables == nil {
			istioLables = shootIstioLables
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

		infra := &extensionsv1alpha1.Infrastructure{}
		namespacedName := types.NamespacedName{
			Namespace: ex.GetNamespace(),
			Name:      cluster.Shoot.Name,
		}

		if err := a.client.Get(ctx, namespacedName, infra); err != nil {
			return nil, nil, err
		}

		var shootSpecificCIDRs []string

		shootSpecificCIDRs = append(shootSpecificCIDRs, helper.GetShootNodeSpecificAllowedCIDRs(cluster.Shoot)...)
		providerSpecificCIRDs, err := helper.GetProviderSpecificAllowedCIDRs(infra)
		if err != nil {
			return nil, nil, err
		}
		shootSpecificCIDRs = append(shootSpecificCIDRs, providerSpecificCIRDs...)

		mappings = append(mappings, envoyfilters.ACLMapping{
			ShootName:          ex.Namespace,
			Rule:               *extSpec.Rule,
			ShootSpecificCIDRs: shootSpecificCIDRs,
		})
	}
	return mappings, istioLables, nil
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
		Name:      "kube-apiserver",
	}, &gw)
	if err != nil {
		return "", nil, err
	}

	namespaceSelector := maps.Clone(gw.Spec.Selector)

	delete(namespaceSelector, "app")

	labelsSelector := client.MatchingLabels(namespaceSelector)

	nsList := v1.NamespaceList{}
	err = a.client.List(ctx, &nsList, labelsSelector)
	if err != nil {
		return "", nil, err
	}

	if len(nsList.Items) != 1 {
		return "", nil, errors.New("no istio namespace could be selected")
	}

	return nsList.Items[0].Name, gw.Spec.Selector, nil
}
