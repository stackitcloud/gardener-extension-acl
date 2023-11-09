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
	webhookcmd "github.com/gardener/gardener/extensions/pkg/webhook/cmd"
	"os"

	extensioncmd "github.com/stackitcloud/gardener-extension-acl/pkg/cmd"

	controllercmd "github.com/gardener/gardener/extensions/pkg/controller/cmd"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// ExtensionName is the name of the extension.
const ExtensionName = "acl-extension"

// Options holds configuration passed to the service controller.
type Options struct {
	generalOptions     *controllercmd.GeneralOptions
	extensionOptions   *extensioncmd.ExtensionOptions
	restOptions        *controllercmd.RESTOptions
	managerOptions     *controllercmd.ManagerOptions
	controllerOptions  *controllercmd.ControllerOptions
	healthOptions      *controllercmd.ControllerOptions
	controllerSwitches *controllercmd.SwitchOptions
	webhookOptions     *extensioncmd.AddToManagerOptions
	reconcileOptions   *controllercmd.ReconcilerOptions
	optionAggregator   controllercmd.OptionAggregator
}

// NewOptions creates a new Options instance.
func NewOptions() *Options {
	options := &Options{
		generalOptions: &controllercmd.GeneralOptions{},
		extensionOptions: &extensioncmd.ExtensionOptions{
			AdditionalAllowedCIDRs: nil,
			ChartPath:              "charts",
		},
		restOptions: &controllercmd.RESTOptions{},
		managerOptions: &controllercmd.ManagerOptions{
			// These are default values.
			LeaderElection:             true,
			LeaderElectionID:           controllercmd.LeaderElectionNameID(ExtensionName),
			LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
			LeaderElectionNamespace:    os.Getenv("LEADER_ELECTION_NAMESPACE"),
		},
		controllerOptions: &controllercmd.ControllerOptions{
			// This is a default value.
			MaxConcurrentReconciles: 5,
		},
		healthOptions: &controllercmd.ControllerOptions{
			// This is a default value.
			MaxConcurrentReconciles: 5,
		},
		controllerSwitches: extensioncmd.ControllerSwitches(),
		webhookOptions: extensioncmd.NewAddToManagerOptions(
			"acl", //TODO
			&webhookcmd.ServerOptions{
				Namespace: os.Getenv("WEBHOOK_CONFIG_NAMESPACE"),
			},
			extensioncmd.WebhookSwitchOptions(),
		),
		reconcileOptions: &controllercmd.ReconcilerOptions{},
	}

	options.optionAggregator = controllercmd.NewOptionAggregator(
		options.generalOptions,
		options.restOptions,
		options.managerOptions,
		options.controllerOptions,
		options.extensionOptions,
		controllercmd.PrefixOption("healthcheck-", options.healthOptions),
		options.controllerSwitches,
		options.webhookOptions,
		options.reconcileOptions,
	)

	return options
}
