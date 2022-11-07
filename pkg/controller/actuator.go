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
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"github.com/stackitcloud/gardener-extension-acl/pkg/imagevector"

	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/chartrenderer"
	"github.com/gardener/gardener/pkg/utils/chart"
	managedresources "github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
	ImageName       = "image-name"
	deletionTimeout = 2 * time.Minute
)

var (
	ErrSpecAction = errors.New("action must either be 'ALLOW' or 'DENY'")
	ErrSpecRules  = errors.New("rules must not be an empty list")
	ErrSpecType   = errors.New("type must either be 'direct_remote_ip', 'remote_ip' or 'source_ip'")
	ErrSpecCIDR   = errors.New("CIDRs must not be empty")
)

// NewActuator returns an actuator responsible for Extension resources.
func NewActuator(cfg config.Config) extension.Actuator {
	logger := log.Log.WithName(ActuatorName)
	return &actuator{
		logger:             logger,
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

	logger logr.Logger
}

type ExtensionSpec struct {
	// Rules contains a list of user-defined Access Control Rules
	Rules []envoyfilters.ACLRule `json:"rules"`
}

// Reconcile the Extension resource.
func (a *actuator) Reconcile(ctx context.Context, ex *extensionsv1alpha1.Extension) error {
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

	if err := ValidateExtensionSpec(extSpec); err != nil {
		return err
	}

	if err := a.updateEnvoyFilterHash(ctx, namespace, extSpec, false); err != nil {
		return err
	}

	if err := a.createSeedResources(ctx, extSpec, cluster, namespace); err != nil {
		return err
	}

	return a.updateStatus(ctx, ex, extSpec)
}

func ValidateExtensionSpec(spec *ExtensionSpec) error {
	if len(spec.Rules) < 1 {
		return ErrSpecRules
	}

	for i := range spec.Rules {
		rule := &spec.Rules[i]

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
	}

	return nil
}

// Delete the Extension resource.
func (a *actuator) Delete(ctx context.Context, ex *extensionsv1alpha1.Extension) error {
	namespace := ex.GetNamespace()
	a.logger.Info("Component is being deleted", "component", "", "namespace", namespace)

	if err := a.updateEnvoyFilterHash(ctx, namespace, nil, true); err != nil {
		return err
	}

	return a.deleteSeedResources(ctx, namespace)
}

// Restore the Extension resource.
func (a *actuator) Restore(ctx context.Context, ex *extensionsv1alpha1.Extension) error {
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

func (a *actuator) createSeedResources(ctx context.Context, spec *ExtensionSpec, cluster *controller.Cluster, namespace string) error {
	var err error

	hosts := make([]string, 0)
	for _, address := range cluster.Shoot.Status.AdvertisedAddresses {
		hosts = append(hosts, strings.Split(address.URL, "//")[1])
	}

	alwaysAllowedCIDRs := []string{
		*cluster.Shoot.Spec.Networking.Nodes,
	}

	apiEnvoyFilterSpec, err := a.envoyfilterService.BuildEnvoyFilterSpecForHelmChart(
		spec.Rules, hosts, cluster.Shoot.Status.TechnicalID, alwaysAllowedCIDRs,
	)
	if err != nil {
		return err
	}

	cfg := map[string]interface{}{
		"shootName":       cluster.Shoot.Name,
		"targetNamespace": IngressNamespace,
		"envoyFilterSpec": apiEnvoyFilterSpec,
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
	newHashString, err := HashData(extSpec.Rules)
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
