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

package cmd

import (
	"time"

	"github.com/stackitcloud/gardener-extension-acl/pkg/controller"
	controllerconfig "github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
	healthcheckcontroller "github.com/stackitcloud/gardener-extension-acl/pkg/controller/healthcheck"

	"github.com/gardener/gardener/extensions/pkg/controller/cmd"
	extensionshealthcheckcontroller "github.com/gardener/gardener/extensions/pkg/controller/healthcheck"
	healthcheckconfig "github.com/gardener/gardener/extensions/pkg/controller/healthcheck/config"
	"github.com/spf13/pflag"
)

const (
	SyncPeriod = 30 * time.Second
	ChartPath  = "charts"
)

// ExtensionOptions holds options related to the extension (not the extension controller)
type ExtensionOptions struct {
	HealthCheckSyncPeriod time.Duration
	ChartPath             string
}

// AddFlags implements Flagger.AddFlags.
func (o *ExtensionOptions) AddFlags(fs *pflag.FlagSet) {
	fs.DurationVar(&o.HealthCheckSyncPeriod, "healthcheck-sync-period", SyncPeriod, "Default healthcheck sync period.")
	fs.StringVar(&o.ChartPath, "chart-path", ChartPath, "Location of the chart directories to deploy")
}

// Complete implements Completer.Complete.
func (o *ExtensionOptions) Complete() error {
	// TODO validate mandatory input options
	return nil
}

func (o *ExtensionOptions) Completed() *ExtensionOptions {
	return o
}

// Apply applies the ExtensionOptions to the passed ControllerConfig instance.
func (o *ExtensionOptions) Apply(config *controllerconfig.Config) {
	// TODO pass controller options from extensionoptions to config param
	config.ChartPath = o.ChartPath
}

func (o *ExtensionOptions) ApplyHealthCheckConfig(config *healthcheckconfig.HealthCheckConfig) {
	config.SyncPeriod.Duration = o.HealthCheckSyncPeriod
}

// ControllerSwitches are the cmd.SwitchOptions for the provider controllers.
func ControllerSwitches() *cmd.SwitchOptions {
	return cmd.NewSwitchOptions(
		cmd.Switch(controller.Type, controller.AddToManager),
		cmd.Switch(extensionshealthcheckcontroller.ControllerName, healthcheckcontroller.AddToManager),
	)
}
