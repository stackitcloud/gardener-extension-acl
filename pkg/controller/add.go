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

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
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

var (
	// DefaultAddOptions are the default AddOptions for AddToManager.
	DefaultAddOptions = AddOptions{}
)

// AddOptions are options to apply when adding the shoot service controller to the manager.
type AddOptions struct {
	// ControllerOptions contains options for the controller.
	ControllerOptions controller.Options
	// ExtensionConfig contains configuration for the extension service
	ExtensionConfig controllerconfig.Config
	// IgnoreOperationAnnotation specifies whether to ignore the operation annotation or not.
	IgnoreOperationAnnotation bool
	// ExtensionClass defines the extension class this extension is responsible for.
	ExtensionClass extensionsv1alpha1.ExtensionClass
}

// AddToManager adds a controller with the default Options to the given Controller Manager.
func AddToManager(ctx context.Context, mgr manager.Manager) error {
	return AddToManagerWithOptions(ctx, mgr, &DefaultAddOptions)
}

// AddToManagerWithOptions adds a controller with the given Options to the given manager.
// The opts.Reconciler is being set with a newly instantiated actuator.
func AddToManagerWithOptions(ctx context.Context, mgr manager.Manager, opts *AddOptions) error {
	return extension.Add(mgr, extension.AddArgs{
		Actuator:          NewActuator(mgr, opts.ExtensionConfig),
		ControllerOptions: opts.ControllerOptions,
		Name:              Type + suffix,
		FinalizerSuffix:   Type + suffix,
		Resync:            0,
		Predicates:        extension.DefaultPredicates(ctx, mgr, DefaultAddOptions.IgnoreOperationAnnotation),
		Type:              Type,
		ExtensionClass:    opts.ExtensionClass,
		WatchBuilder:      watchInfrastructure(mgr),
	})
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
