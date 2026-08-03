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
	"fmt"
	"time"

	extensionsconfigv1alpha1 "github.com/gardener/gardener/extensions/pkg/apis/config/v1alpha1"
	extensionscmdcontroller "github.com/gardener/gardener/extensions/pkg/controller/cmd"
	extensionshealthcheckcontroller "github.com/gardener/gardener/extensions/pkg/controller/healthcheck"
	"github.com/spf13/pflag"

	"github.com/stackitcloud/gardener-extension-acl/pkg/controller"
	controllerconfig "github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
	healthcheckcontroller "github.com/stackitcloud/gardener-extension-acl/pkg/controller/healthcheck"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"github.com/stackitcloud/gardener-extension-acl/pkg/extensionspec"
)

const (
	// DefaultSyncPeriod is the default healthcheck-sync-period
	DefaultSyncPeriod = 30 * time.Second
	// ChartPath is the path to the chart folder
	ChartPath = "charts"
)

// ExtensionOptions holds options related to the extension (not the extension controller)
type ExtensionOptions struct {
	HealthCheckSyncPeriod  time.Duration
	ChartPath              string
	AdditionalAllowedCIDRs []string
	DefaultRuleAction      string
	DefaultRuleType        string
	DefaultRuleCIDRs       []string

	defaultRule *envoyfilters.ACLRule
}

// AddFlags implements Flagger.AddFlags.
func (o *ExtensionOptions) AddFlags(fs *pflag.FlagSet) {
	fs.DurationVar(&o.HealthCheckSyncPeriod, "healthcheck-sync-period", DefaultSyncPeriod, "Default healthcheck sync period.")
	fs.StringVar(&o.ChartPath, "chart-path", ChartPath, "Location of the chart directories to deploy")
	fs.StringSliceVar(
		&o.AdditionalAllowedCIDRs,
		"additional-allowed-cidrs",
		nil,
		"List of IPs that will be added to the list of allowed CIDRs, e.g. '192.168.1.40/32,10.250.0.0/16'",
	)
	fs.StringVar(
		&o.DefaultRuleAction,
		"default-rule-action",
		"ALLOW",
		"Action of the default rule applied to Extension resources without a rule of their own. Only used when --default-rule-cidrs is set.",
	)
	fs.StringVar(
		&o.DefaultRuleType,
		"default-rule-type",
		"remote_ip",
		"Type of the default rule applied to Extension resources without a rule of their own ('source_ip', 'direct_remote_ip' or 'remote_ip'). Only used when --default-rule-cidrs is set.",
	)
	fs.StringSliceVar(
		&o.DefaultRuleCIDRs,
		"default-rule-cidrs",
		nil,
		"List of CIDRs for a default rule that is applied to Extension resources that do not define a rule of their own (e.g. extensions enabled via autoEnable without a providerConfig). "+
			"When unset, Extension resources without a rule are rejected (previous behavior). A rule from the Shoot's providerConfig always takes precedence.",
	)
}

// Complete implements Completer.Complete.
func (o *ExtensionOptions) Complete() error {
	if len(o.DefaultRuleCIDRs) == 0 {
		return nil
	}

	o.defaultRule = &envoyfilters.ACLRule{
		Action: o.DefaultRuleAction,
		Type:   o.DefaultRuleType,
		Cidrs:  o.DefaultRuleCIDRs,
	}

	// Fail fast on an invalid operator-provided default rule (the per-shoot
	// maximum is not applied to the operator-configured default).
	if err := controller.ValidateExtensionSpec(&extensionspec.ExtensionSpec{Rule: o.defaultRule}, 0); err != nil {
		return fmt.Errorf("invalid default rule: %w", err)
	}

	return nil
}

// Completed returns ExtensionOptions.
func (o *ExtensionOptions) Completed() *ExtensionOptions {
	return o
}

// Apply applies the ExtensionOptions to the passed ControllerConfig instance.
func (o *ExtensionOptions) Apply(config *controllerconfig.Config) {
	// TODO pass controller options from extensionoptions to config param
	config.ChartPath = o.ChartPath
	config.AdditionalAllowedCIDRs = o.AdditionalAllowedCIDRs
	config.DefaultRule = o.defaultRule
}

// ApplyHealthCheckConfig applies the ExtensionOptions to the passed HealthCheckConfig.
func (o *ExtensionOptions) ApplyHealthCheckConfig(config *extensionsconfigv1alpha1.HealthCheckConfig) {
	config.SyncPeriod.Duration = o.HealthCheckSyncPeriod
}

// ControllerSwitches are the cmd.SwitchOptions for the provider controllers.
func ControllerSwitches() *extensionscmdcontroller.SwitchOptions {
	return extensionscmdcontroller.NewSwitchOptions(
		extensionscmdcontroller.Switch(controller.Type, controller.AddToManager),
		extensionscmdcontroller.Switch(extensionshealthcheckcontroller.ControllerName, healthcheckcontroller.AddToManager),
	)
}
