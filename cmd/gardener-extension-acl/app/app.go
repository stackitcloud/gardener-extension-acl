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

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/util"
	"github.com/gardener/gardener/pkg/api/indexer"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/spf13/cobra"
	istionetworkv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istionetworkv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	componentbaseconfigv1alpha1 "k8s.io/component-base/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-acl/pkg/controller"
	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/healthcheck"
)

// NewControllerManagerCommand creates a new command that is used to start the service controller.
func NewControllerManagerCommand(ctx context.Context) *cobra.Command {
	options := NewOptions()

	cmd := &cobra.Command{
		Use:           "acl-controller",
		Short:         "ACL Extension Controller",
		SilenceErrors: true,

		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := options.optionAggregator.Complete(); err != nil {
				return fmt.Errorf("error completing options: %s", err)
			}
			cmd.SilenceUsage = true
			return options.run(ctx)
		},
	}

	options.optionAggregator.AddFlags(cmd.Flags())

	return cmd
}

func (o *Options) run(ctx context.Context) error {
	// TODO: Make these flags configurable via command line parameters or component config file.
	util.ApplyClientConnectionConfigurationToRESTConfig(&componentbaseconfigv1alpha1.ClientConnectionConfiguration{
		QPS:   100.0,
		Burst: 130,
	}, o.restOptions.Completed().Config)

	mgrOpts := o.managerOptions.Completed().Options()

	// TODO why??
	mgrOpts.Client = client.Options{
		Cache: &client.CacheOptions{
			DisableFor: []client.Object{
				&corev1.Secret{},    // applied for ManagedResources
				&corev1.ConfigMap{}, // applied for monitoring config and shoot-info
			},
		},
	}

	// Only cache services that are needed to check for ProxyProto usage
	mgrOpts.Cache.ByObject = map[client.Object]cache.ByObject{
		&corev1.Service{}: {
			Label: labels.Set{
				v1beta1constants.LabelApp: v1beta1constants.DefaultIngressGatewayAppLabelValue,
			}.AsSelector(),
		},
	}

	mgr, err := manager.New(o.restOptions.Completed().Config, mgrOpts)
	if err != nil {
		return fmt.Errorf("could not instantiate controller-manager: %s", err)
	}

	if err := extensionscontroller.AddToScheme(mgr.GetScheme()); err != nil {
		return fmt.Errorf("could not update manager scheme: %s", err)
	}

	if err := operatorv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return fmt.Errorf("could not update manager scheme: %s", err)
	}

	if err := istionetworkv1alpha3.AddToScheme(mgr.GetScheme()); err != nil {
		return fmt.Errorf("could not update manager scheme: %s", err)
	}
	if err := istionetworkv1beta1.AddToScheme(mgr.GetScheme()); err != nil {
		return fmt.Errorf("could not update manager scheme: %s", err)
	}

	ctrlConfig := o.extensionOptions.Completed()
	ctrlConfig.ApplyHealthCheckConfig(&healthcheck.DefaultAddOptions.HealthCheckConfig)
	ctrlConfig.Apply(&controller.DefaultAddOptions.ExtensionConfig)

	o.controllerOptions.Completed().Apply(&controller.DefaultAddOptions.ControllerOptions)
	o.healthOptions.Completed().Apply(&healthcheck.DefaultAddOptions.Controller)
	o.reconcileOptions.Completed().Apply(&controller.DefaultAddOptions.IgnoreOperationAnnotation)
	healthcheck.DefaultAddOptions.ExtensionClasses = o.generalOptions.Completed().ExtensionClasses

	o.reconcileOptions.Completed().Apply(&controller.DefaultAddOptions.IgnoreOperationAnnotation)
	controller.DefaultAddOptions.ExtensionClasses = o.generalOptions.Completed().ExtensionClasses

	if err := o.controllerSwitches.Completed().AddToManager(ctx, mgr); err != nil {
		return fmt.Errorf("could not add controllers to manager: %s", err)
	}

	var gardenCluster cluster.Cluster
	if slices.Contains(controller.DefaultAddOptions.ExtensionClasses, extensionsv1alpha1.ExtensionClassGarden) {
		gardenCluster, err = setupGardenCluster(mgr)
		if err != nil {
			return fmt.Errorf("unable to set up garden cluster: %w", err)
		}
		if err := indexer.AddManagedSeedShootName(ctx, gardenCluster.GetFieldIndexer()); err != nil {
			return fmt.Errorf("adding index for managedSeed to garden cluster: %w", err)
		}
	}

	if err := controller.AddToManager(ctx, mgr, gardenCluster); err != nil {
		return fmt.Errorf("could not add controller to manager: %w", err)
	}

	// TODO(Wieneo): Remove this once a couple extension versions included the migration code
	// migration code: remove mutating webhook from cluster as it is not served by this controller anymore
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		if err := client.IgnoreNotFound(mgr.GetClient().Delete(ctx, &admissionregistrationv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: ExtensionName}})); err != nil {
			return fmt.Errorf("could not delete mutatingwebhook %s: %w", ExtensionName, err)
		}
		return nil
	})); err != nil {
		return fmt.Errorf("could not add runnable to manager: %w", err)
	}

	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("error running manager: %w", err)
	}

	return nil
}

func setupGardenCluster(mgr manager.Manager) (cluster.Cluster, error) {
	// configure garden
	kubeconfigFile := os.Getenv("GARDEN_KUBECONFIG")
	if kubeconfigFile == "" {
		return nil, errors.New("GARDEN_KUBECONFIG environment variable is empty, cannot setup garden cluster")
	}
	gardenRESTConfig, err := kubernetes.RESTConfigFromKubeconfigFile(kubeconfigFile, kubernetes.AuthTokenFile, kubernetes.AuthExec)
	if err != nil {
		return nil, err
	}

	gardenCluster, err := cluster.New(gardenRESTConfig, func(opts *cluster.Options) {
		opts.Scheme = kubernetes.GardenScheme
		opts.Cache = cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				// ManagedSeeds (and their underlying Shoots) must always be in the "garden" namespace, so that is the
				// only one we need to watch.
				v1beta1constants.GardenNamespace: {},
			},
		}
	})
	if err != nil {
		return nil, err
	}

	return gardenCluster, mgr.Add(gardenCluster)
}
