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

package healthcheck

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/healthcheck"
	healthcheckconfig "github.com/gardener/gardener/extensions/pkg/controller/healthcheck/config"
	"github.com/gardener/gardener/extensions/pkg/controller/healthcheck/general"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"

	controller "github.com/stackitcloud/gardener-extension-acl/pkg/controller"
)

var (
	DefaultAddOptions = healthcheck.DefaultAddArgs{
		HealthCheckConfig: healthcheckconfig.HealthCheckConfig{SyncPeriod: metav1.Duration{}},
	}
)

// RegisterHealthChecks registers health checks for each extension resource
// HealthChecks are grouped by extension (e.g worker), extension.type (e.g aws) and  Health Check Type (e.g SystemComponentsHealthy)
func RegisterHealthChecks(mgr manager.Manager, opts *healthcheck.DefaultAddArgs) error {
	preCheckFunc := func(_ client.Object, cluster *extensionscontroller.Cluster) bool {
		// TODO custom precheck logic
		// example: return cluster.Shoot.Spec.DNS != nil && cluster.Shoot.Spec.DNS.Domain != nil
		return true
	}

	return healthcheck.DefaultRegistration(
		controller.Type,
		extensionsv1alpha1.SchemeGroupVersion.WithKind(extensionsv1alpha1.ExtensionResource),
		func() client.ObjectList { return &extensionsv1alpha1.ExtensionList{} },
		func() extensionsv1alpha1.Object { return &extensionsv1alpha1.Extension{} },
		mgr,
		*opts,
		nil,
		[]healthcheck.ConditionTypeToHealthCheck{
			{
				ConditionType: string(gardencorev1beta1.SeedExtensionsReady),
				HealthCheck:   general.CheckManagedResource(controller.ResourceNameSeed),
				PreCheckFunc:  preCheckFunc,
			},
		},
	)
}

// AddToManager adds a controller with the default Options.
func AddToManager(mgr manager.Manager) error {
	return RegisterHealthChecks(mgr, &DefaultAddOptions)
}
