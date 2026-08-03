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

package config

import "github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"

// Config contains configuration for the extension service.
type Config struct {
	// TODO define options
	ChartPath string
	// AdditionalAllowedCIDRs additional allowed cidrs that will be added to the list of allowed cidrs.
	AdditionalAllowedCIDRs []string
	// MaxAllowedCIDRs is the maximum number of allowed CIDRs per cluster
	MaxAllowedCIDRs int
	// DefaultRule is applied to Extension resources that do not define a rule of
	// their own (i.e. have no providerConfig). This allows operators to enable
	// the extension for all shoots (e.g. via autoEnable in the
	// ControllerRegistration) with a sensible default, while individual shoots
	// can still provide their own rule to override it. When nil, Extension
	// resources without a rule are rejected (previous behavior).
	DefaultRule *envoyfilters.ACLRule
}
