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

	"github.com/stackitcloud/gardener-extension-example/pkg/controller/config"
	"github.com/stackitcloud/gardener-extension-example/pkg/imagevector"

	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	"github.com/gardener/gardener/extensions/pkg/util"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/chartrenderer"
	"github.com/gardener/gardener/pkg/utils/chart"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	managedresources "github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
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
	ActuatorName      = "some-actuator"
	ResourceNameShoot = "resource-name-shoot"
	ChartNameShoot    = "example-shoot"
	ResourceNameSeed  = "resource-name-seed"
	ChartNameSeed     = "example-seed"
	// ImageName is used for the image vector override.
	// This is currently not implemented correctly.
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
	SampleString string
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

	if !controller.IsHibernated(cluster) {
		if err := a.createShootResources(ctx, extSpec, cluster, namespace); err != nil {
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
	if err := a.deleteShootResources(ctx, namespace); err != nil {
		return err
	}

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
	// TODO if your extension manages resources in shoot clusters
	//
	// Keep objects for shoot managed resources so that they are not deleted
	// from the shoot during the migration
	if err := managedresources.SetKeepObjects(ctx, a.client, ex.GetNamespace(), ResourceNameShoot, true); err != nil {
		return err
	}

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
func (a *actuator) createSeedResources(ctx context.Context, _ *ExtensionSpec, _ *controller.Cluster, namespace string) error {
	// TODO construct a map[string]interface{} that is passed to InjectImages
	cfg := map[string]interface{}{}

	cfg, err := chart.InjectImages(cfg, imagevector.ImageVector(), []string{ImageName})
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

// TODO use the extension spec
func (a *actuator) createShootResources(ctx context.Context, _ *ExtensionSpec, cluster *controller.Cluster, namespace string) error {

	values := map[string]interface{}{}

	renderer, err := util.NewChartRendererForShoot(cluster.Shoot.Spec.Kubernetes.Version)
	if err != nil {
		return errors.Wrap(err, "could not create chart renderer")
	}

	// TODO get these values from constants
	return a.createManagedResource(ctx, namespace, ResourceNameShoot, "", renderer, ChartNameShoot, metav1.NamespaceSystem, values, nil)
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

func (a *actuator) deleteShootResources(ctx context.Context, namespace string) error {
	a.logger.Info("Deleting managed resource for shoot", "namespace", namespace)
	if err := managedresources.Delete(ctx, a.client, namespace, ResourceNameShoot, false); err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, deletionTimeout)
	defer cancel()
	return managedresources.WaitUntilDeleted(timeoutCtx, a.client, namespace, ResourceNameShoot)
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
