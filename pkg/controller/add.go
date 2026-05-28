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
	"slices"
	"time"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	kubernetesutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	controllerconfig "github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
)

const (
	// Type is the type of Extension resource.
	Type   = "acl"
	suffix = "-extension-service"
)

// DefaultAddOptions are the default AddOptions for AddToManager.
var DefaultAddOptions = AddOptions{}

// AddOptions are options to apply when adding the shoot service controller to the manager.
type AddOptions struct {
	// ControllerOptions contains options for the controller.
	ControllerOptions controller.Options
	// ExtensionConfig contains configuration for the extension service
	ExtensionConfig controllerconfig.Config
	// IgnoreOperationAnnotation specifies whether to ignore the operation annotation or not.
	IgnoreOperationAnnotation bool
	// ExtensionClasses defines the extension classes this extension is responsible for.
	ExtensionClasses []extensionsv1alpha1.ExtensionClass
}

// AddToManager adds a controller with the default Options to the given Controller Manager.
func AddToManager(ctx context.Context, mgr manager.Manager, gardenCluster cluster.Cluster) error {
	return AddToManagerWithOptions(ctx, mgr, gardenCluster, &DefaultAddOptions)
}

// AddToManagerWithOptions adds a controller with the given Options to the given manager.
// The opts.Reconciler is being set with a newly instantiated actuator.
func AddToManagerWithOptions(ctx context.Context, mgr manager.Manager, gardenCluster cluster.Cluster, opts *AddOptions) error {
	args := extension.AddArgs{
		Actuator:          NewActuator(mgr, gardenCluster, opts.ExtensionConfig),
		ControllerOptions: opts.ControllerOptions,
		Name:              Type + suffix,
		FinalizerSuffix:   Type + suffix,
		Resync:            0,
		Predicates:        extension.DefaultPredicates(ctx, mgr, DefaultAddOptions.IgnoreOperationAnnotation),
		Type:              Type,
		ExtensionClasses:  opts.ExtensionClasses,
	}
	if !slices.Contains(opts.ExtensionClasses, extensionsv1alpha1.ExtensionClassGarden) {
		args.WatchBuilder = watchInfrastructure(mgr)
	} else {
		args.WatchBuilder = watchShootsOfManagedSeeds(mgr, gardenCluster)
	}
	return extension.Add(mgr, args)
}

func infrastructurePredicate() predicate.TypedFuncs[*extensionsv1alpha1.Infrastructure] {
	return predicate.TypedFuncs[*extensionsv1alpha1.Infrastructure]{
		UpdateFunc: func(e event.TypedUpdateEvent[*extensionsv1alpha1.Infrastructure]) bool {
			// We want to reconcile if the EgressCIDRs of the Infrastructure changed
			oldEgressCIDRs := slices.Clone(e.ObjectOld.Status.EgressCIDRs)
			newEgressCIDRs := slices.Clone(e.ObjectNew.Status.EgressCIDRs)
			slices.Sort(oldEgressCIDRs)
			slices.Sort(newEgressCIDRs)

			return !slices.Equal(oldEgressCIDRs, newEgressCIDRs)
		},
		CreateFunc: func(_ event.TypedCreateEvent[*extensionsv1alpha1.Infrastructure]) bool {
			return false
		},
		DeleteFunc: func(_ event.TypedDeleteEvent[*extensionsv1alpha1.Infrastructure]) bool {
			return false
		},
		GenericFunc: func(_ event.TypedGenericEvent[*extensionsv1alpha1.Infrastructure]) bool {
			return false
		},
	}
}

// watchInfrastructure watches for Infrastructure changes and triggers the Extension reconciliation.
func watchInfrastructure(mgr manager.Manager) extensionscontroller.WatchBuilder {
	// Map Infrastructure changes to the Extension
	mapFunc := func(_ context.Context, infrastructure *extensionsv1alpha1.Infrastructure) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{
			Name:      Type,
			Namespace: infrastructure.Namespace,
		}}}
	}

	// Watch for Infrastructure changes outside shoot reconciliation
	return extensionscontroller.NewWatchBuilder(func(ctrl controller.Controller) error {
		return ctrl.Watch(source.Kind(mgr.GetCache(), &extensionsv1alpha1.Infrastructure{},
			handler.TypedEnqueueRequestsFromMapFunc(mapFunc),
			infrastructurePredicate(),
		))
	})
}

// watchShootsOfManagedSeeds watches for Shoot changes that are a managed seed and triggers the Extension reconciliation.
func watchShootsOfManagedSeeds(mgr manager.Manager, gardenCluster cluster.Cluster) extensionscontroller.WatchBuilder {
	// Watch for Infrastructure changes outside shoot reconciliation
	return extensionscontroller.NewWatchBuilder(func(ctrl controller.Controller) error {
		return ctrl.Watch(source.Kind(gardenCluster.GetCache(), &gardencorev1beta1.Shoot{},
			handler.TypedEnqueueRequestsFromMapFunc(mapShootsOfManagedSeedsToExtensions(mgr.GetClient())),
			shootsOfManagedSeedsPredicate(gardenCluster.GetClient()),
		))
	})
}

func mapShootsOfManagedSeedsToExtensions(r client.Reader) handler.TypedMapFunc[*gardencorev1beta1.Shoot, reconcile.Request] {
	return func(ctx context.Context, shoot *gardencorev1beta1.Shoot) []reconcile.Request {
		log := logf.FromContext(ctx).WithValues("shoot", shoot)

		exts := &extensionsv1alpha1.ExtensionList{}
		if err := r.List(ctx, exts, client.InNamespace(v1beta1constants.GardenNamespace)); err != nil {
			log.Error(err, "listing extensions for managedseed enqueue requests")
			return nil
		}
		gardenExtensions := slices.DeleteFunc(exts.Items, func(ext extensionsv1alpha1.Extension) bool {
			if ext.Spec.Type != "acl" {
				return true
			}
			if ext.Spec.Class == nil {
				return true
			}
			return *ext.Spec.Class != extensionsv1alpha1.ExtensionClassGarden
		})

		reqs := make([]reconcile.Request, 0, len(gardenExtensions))
		for _, ext := range gardenExtensions {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&ext)})
		}
		return reqs
	}
}

// shootsOfManagedSeedsPredicate filters change events to only enqueue if the shoot has a managed seed associated with it and the EgressCIDRs changes
func shootsOfManagedSeedsPredicate(c client.Reader) predicate.TypedFuncs[*gardencorev1beta1.Shoot] {
	log := logf.Log.WithName("shootsOfManagedSeedsPredicate")
	return predicate.TypedFuncs[*gardencorev1beta1.Shoot]{
		UpdateFunc: func(e event.TypedUpdateEvent[*gardencorev1beta1.Shoot]) bool {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			log.WithValues("shoot", e.ObjectNew)
			ms, err := kubernetesutil.GetManagedSeedWithReader(ctx, c, e.ObjectNew.Namespace, e.ObjectNew.Name)
			if err != nil {
				log.Error(err, "getting managedseed from shoot")
				return false
			}
			if ms == nil {
				log.V(1).Info("no ManagedSeed available for shoot")
				return false
			}
			newEgress := ptr.Deref(e.ObjectNew.Status.Networking, gardencorev1beta1.NetworkingStatus{}).EgressCIDRs
			oldEgress := ptr.Deref(e.ObjectOld.Status.Networking, gardencorev1beta1.NetworkingStatus{}).EgressCIDRs
			return !slices.Equal(slices.Sorted(slices.Values(newEgress)), slices.Sorted(slices.Values(oldEgress)))
		},
		DeleteFunc: func(event.TypedDeleteEvent[*gardencorev1beta1.Shoot]) bool {
			// in case of shoot deletion we need to remove the CIDR of the seed
			return true
		},
		GenericFunc: func(event.TypedGenericEvent[*gardencorev1beta1.Shoot]) bool {
			return false
		},
		CreateFunc: func(event.TypedCreateEvent[*gardencorev1beta1.Shoot]) bool {
			return false
		},
	}
}
