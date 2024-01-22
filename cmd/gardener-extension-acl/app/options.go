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
	"os"

	extensionscmdcontroller "github.com/gardener/gardener/extensions/pkg/controller/cmd"
	extensionscmdwebhook "github.com/gardener/gardener/extensions/pkg/webhook/cmd"

	extensioncmd "github.com/stackitcloud/gardener-extension-acl/pkg/cmd"
)

// ExtensionName is the name of the extension.
const ExtensionName = "acl"

// Options holds configuration passed to the service controller.
type Options struct {
	generalOptions     *extensionscmdcontroller.GeneralOptions
	extensionOptions   *extensioncmd.ExtensionOptions
	restOptions        *extensionscmdcontroller.RESTOptions
	managerOptions     *extensionscmdcontroller.ManagerOptions
	controllerOptions  *extensionscmdcontroller.ControllerOptions
	healthOptions      *extensionscmdcontroller.ControllerOptions
	controllerSwitches *extensionscmdcontroller.SwitchOptions
	webhookOptions     *extensioncmd.AddToManagerOptions
	reconcileOptions   *extensionscmdcontroller.ReconcilerOptions
	optionAggregator   extensionscmdcontroller.OptionAggregator
}

// NewOptions creates a new Options instance.
func NewOptions() *Options {
	options := &Options{
		generalOptions: &extensionscmdcontroller.GeneralOptions{},
		extensionOptions: &extensioncmd.ExtensionOptions{
			AdditionalAllowedCIDRs: nil,
			ChartPath:              "charts",
		},
		restOptions: &extensionscmdcontroller.RESTOptions{},
		managerOptions: &extensionscmdcontroller.ManagerOptions{
			// These are default values.
			LeaderElection:          true,
			LeaderElectionID:        extensionscmdcontroller.LeaderElectionNameID(ExtensionName),
			LeaderElectionNamespace: os.Getenv("LEADER_ELECTION_NAMESPACE"),
		},
		controllerOptions: &extensionscmdcontroller.ControllerOptions{
			// This is a default value.
			MaxConcurrentReconciles: 5,
		},
		healthOptions: &extensionscmdcontroller.ControllerOptions{
			// This is a default value.
			MaxConcurrentReconciles: 5,
		},
		controllerSwitches: extensioncmd.ControllerSwitches(),
		webhookOptions: extensioncmd.NewAddToManagerOptions(
			ExtensionName,
			&extensionscmdwebhook.ServerOptions{
				Namespace: os.Getenv("WEBHOOK_CONFIG_NAMESPACE"),
			},
			extensioncmd.WebhookSwitchOptions(),
		),
		reconcileOptions: &extensionscmdcontroller.ReconcilerOptions{},
	}

	options.optionAggregator = extensionscmdcontroller.NewOptionAggregator(
		options.generalOptions,
		options.restOptions,
		options.managerOptions,
		options.controllerOptions,
		options.extensionOptions,
		extensionscmdcontroller.PrefixOption("healthcheck-", options.healthOptions),
		options.controllerSwitches,
		options.webhookOptions,
		options.reconcileOptions,
	)

	return options
}
