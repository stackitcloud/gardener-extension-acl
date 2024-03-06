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

// Config contains configuration for the extension service.
type Config struct {
	// TODO define options
	ChartPath string
	// AdditionalAllowedCIDRs additional allowed cidrs that will be added to the list of allowed cidrs.
	AdditionalAllowedCIDRs []string
	// MaxAllowedCIDRs is the maximum number of allowed CIDRs per cluster
	MaxAllowedCIDRs int
}
