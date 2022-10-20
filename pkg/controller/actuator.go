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
	"strings"
	"time"

	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
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

// kind: Shoot
// spec:
//   extensions:
//   - type: acl
//     providerConfig:
//     - cidrs:
//       - x.x.x.x/24
//       - y.y.y.y/24
//       action: ALLOW # default
//       type: ip|remote # use IPBlocks or RemoteIPBlocks
//     - cidrs:
//       - ......

type ExtensionSpec struct {
	// Rules contains a list of user-defined Access Control Rules
	Rules []AclRule `json:"rules"`
}

type AclRule struct {
	// Cidrs contains a list of CIDR blocks to which the ACL rule applies
	Cidrs []Cidr `json:"cidrs"`
	// Action defines if the rule is a DENY or an ALLOW rule
	Action string `json:"action"`
	// Type can either be ip, remote, or source
	Type string `json:"type"`
}

// TODO maybe use cidrs in format or ip/length ? for easier typing?
type Cidr struct {
	// AddressPrefix contains an IP subnet address prefix
	AddressPrefix string `json:"addressPrefix"`
	// PrefixLength determines the length of the address prefix to consider
	PrefixLength int `json:"prefixLength"`
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

	apiEnvoyFilterSpec, err := a.buildEnvoyFilterSpec(spec, hosts)
	if err != nil {
		return err
	}

	cfg := map[string]interface{}{
		"shootName":       cluster.Shoot.Name,
		"targetNamespace": "istio-ingress",
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

func (a *actuator) buildEnvoyFilterSpec(
	spec *ExtensionSpec, hosts []string,
) (map[string]interface{}, error) {
	configPatches := []map[string]interface{}{}

	for i := range spec.Rules {
		rule := &spec.Rules[i]
		// TODO check if rule is well defined

		apiConfigPatch, err := a.createAPIConfigPatchFromRule(rule, hosts)
		if err != nil {
			return nil, err
		}

		vpnConfigPatch, err := a.createVPNConfigPatchFromRule(rule, "TODO-shoot-name")
		if err != nil {
			return nil, err
		}

		configPatches = append(configPatches, apiConfigPatch, vpnConfigPatch)
	}

	return map[string]interface{}{
		"workloadSelector": map[string]interface{}{
			"labels": map[string]interface{}{
				"app":   "istio-ingressgateway",
				"istio": "ingressgateway",
			},
		},
		"configPatches": configPatches,
	}, nil
}

func (a *actuator) createAPIConfigPatchFromRule(rule *AclRule, hosts []string) (map[string]interface{}, error) {
	// TODO use all hosts?
	host := hosts[0]
	rbacName := "acl-api"
	principals := []map[string]interface{}{}

	for i := range rule.Cidrs {
		cidr := &rule.Cidrs[i]
		principals = append(principals, ruleCidrToPrincipal(cidr, rule.Type))
	}

	return map[string]interface{}{
		"applyTo": "NETWORK_FILTER",
		"match": map[string]interface{}{
			"context": "GATEWAY",
			"listener": map[string]interface{}{
				"filterChain": map[string]interface{}{
					"sni": host,
				},
			},
		},
		"patch": principalsToPatch(rbacName, rule.Action, "network", principals),
	}, nil
}

func (a *actuator) createVPNConfigPatchFromRule(rule *AclRule, shootName string) (map[string]interface{}, error) {
	rbacName := "acl-vpn"
	andedPrincipals := []map[string]interface{}{}

	for i := range rule.Cidrs {
		cidr := &rule.Cidrs[i]
		andedPrincipals = append(andedPrincipals, ruleCidrToPrincipal(cidr, rule.Type))
	}

	andedPrincipals = append(andedPrincipals, map[string]interface{}{
		"header": map[string]interface{}{
			"name": "reversed-vpn",
			"string_match": map[string]interface{}{
				"contains": shootName,
			},
		},
	})

	principals := []map[string]interface{}{
		{
			"and_ids": map[string]interface{}{
				"ids": andedPrincipals,
			},
		},
	}

	return map[string]interface{}{
		"applyTo": "HTTP_FILTER",
		"match": map[string]interface{}{
			"context": "GATEWAY",
			"listener": map[string]interface{}{
				"name": "0.0.0.0_8132",
			},
		},
		"patch": principalsToPatch(rbacName, rule.Action, "http", principals),
	}, nil
}

func ruleCidrToPrincipal(cidr *Cidr, ruleType string) map[string]interface{} {
	return map[string]interface{}{
		ruleType: map[string]interface{}{
			"address_prefix": cidr.AddressPrefix,
			"prefix_len":     cidr.PrefixLength,
		},
	}
}

func principalsToPatch(rbacName, ruleAction, filterType string, principals []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"operation": "INSERT_FIRST",
		"value": map[string]interface{}{
			"name": rbacName,
			"typed_config": map[string]interface{}{
				"@type":       "type.googleapis.com/envoy.extensions.filters." + filterType + ".rbac.v3.RBAC",
				"stat_prefix": "envoyrbac",
				"rules": map[string]interface{}{
					"action": ruleAction,
					"policies": map[string]interface{}{
						rbacName: map[string]interface{}{
							"permissions": []map[string]interface{}{
								{"any": true},
							},
							"principals": principals,
						},
					},
				},
			},
		},
	}
}
