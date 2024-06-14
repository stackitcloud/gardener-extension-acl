package envoyfilters

import (
	"errors"
	"net"
	"strings"

	"github.com/gardener/gardener/extensions/pkg/controller"

	"github.com/stackitcloud/gardener-extension-acl/pkg/helper"
)

// Error variables for envoyfilters pkg
var (
	ErrNoHostsGiven = errors.New("no hosts were given, at least one host is needed")
)

// ACLMapping maps a an ACL rule to a specific shoot and also contains the
// shoot-specific CIDRs.
type ACLMapping struct {
	ShootName          string   `json:"shootName"`
	Rule               ACLRule  `json:"rule"`
	ShootSpecificCIDRs []string `json:"ShootSpecificCIDRs"`
}

// ACLRule contains a single ACL rule, consisting of a list of CIDRs, an action
// and a rule type.
type ACLRule struct {
	// Cidrs contains a list of CIDR blocks to which the ACL rule applies
	Cidrs []string `json:"cidrs"`
	// Action defines if the rule is a DENY or an ALLOW rule
	Action string `json:"action"`
	// Type can either be "source_ip", "direct_remote_ip" or "remote_ip"
	Type string `json:"type"`
}

// BuildAPIEnvoyFilterSpecForHelmChart assembles EnvoyFilter patches for API server
// networking for every rule in the extension spec.
func BuildAPIEnvoyFilterSpecForHelmChart(
	rule *ACLRule, hosts, alwaysAllowedCIDRs []string, istioLabels map[string]string,
) (map[string]interface{}, error) {
	apiConfigPatch, err := CreateAPIConfigPatchFromRule(rule, hosts, alwaysAllowedCIDRs)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"workloadSelector": map[string]interface{}{
			"labels": istioLabels,
		},
		"configPatches": []map[string]interface{}{
			apiConfigPatch,
		},
	}, nil
}

// BuildIngressEnvoyFilterSpecForHelmChart assembles EnvoyFilter patches for
// endpoints using the seed ingress domain.
func BuildIngressEnvoyFilterSpecForHelmChart(
	cluster *controller.Cluster, rule *ACLRule, alwaysAllowedCIDRs []string, istioLabels map[string]string,
) map[string]interface{} {
	seedIngressDomain := helper.GetSeedIngressDomain(cluster.Seed)
	if seedIngressDomain != "" {
		shootID := helper.ComputeShortShootID(cluster.Shoot)

		return map[string]interface{}{
			"workloadSelector": map[string]interface{}{
				"labels": istioLabels,
			},
			"configPatches": []map[string]interface{}{
				CreateIngressConfigPatchFromRule(rule, seedIngressDomain, shootID, alwaysAllowedCIDRs),
			},
		}
	}
	return nil
}

// BuildVPNEnvoyFilterSpecForHelmChart assembles EnvoyFilter patches for VPN.
func BuildVPNEnvoyFilterSpecForHelmChart(
	cluster *controller.Cluster, rule *ACLRule, alwaysAllowedCIDRs []string, istioLabels map[string]string,
) (map[string]interface{}, error) {
	vpnConfigPatch, err := CreateVPNConfigPatchFromRule(rule, helper.ComputeShortShootID(cluster.Shoot), cluster.Shoot.Status.TechnicalID, alwaysAllowedCIDRs)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"workloadSelector": map[string]interface{}{
			"labels": istioLabels,
		},
		"configPatches": []map[string]interface{}{
			vpnConfigPatch,
		},
	}, nil
}

// BuildLegacyVPNEnvoyFilterSpecForHelmChart assembles a single EnvoyFilter for all
// shoots on the seed, due to the fact that we can't create one EnvoyFilter per
// shoot - this doesn't work because all the VPN traffic flows through the same
// filter.
//
// We use the technical ID of the shoot for the VPN rule, which is de facto the
// same as the seed namespace of the shoot. (Gardener uses the seedNamespace
// value in the botanist vpnshoot task.)
func BuildLegacyVPNEnvoyFilterSpecForHelmChart(
	mappings []ACLMapping, alwaysAllowedCIDRs []string, istioLabels map[string]string,
) (map[string]interface{}, error) {
	vpnConfigPatch, err := CreateLegacyVPNConfigPatchFromRule(mappings, alwaysAllowedCIDRs)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"workloadSelector": map[string]interface{}{
			"labels": istioLabels,
		},
		"configPatches": []map[string]interface{}{
			vpnConfigPatch,
		},
	}, nil
}

// CreateAPIConfigPatchFromRule combines an ACLRule, the first entry  of the
// hosts list and the alwaysAllowedCIDRs into a network filter patch that can be
// applied to the `GATEWAY` network filter chain matching the host.
func CreateAPIConfigPatchFromRule(
	rule *ACLRule, hosts, alwaysAllowedCIDRs []string,
) (map[string]interface{}, error) {
	if len(hosts) == 0 {
		return nil, ErrNoHostsGiven
	}
	rbacName := "acl-api"
	principals := ruleCIDRsToPrincipal(rule, alwaysAllowedCIDRs)

	return map[string]interface{}{
		"applyTo": "NETWORK_FILTER",
		"match": map[string]interface{}{
			"context": "GATEWAY",
			"listener": map[string]interface{}{
				"filterChain": map[string]interface{}{
					// There is one filter chain per shoot in the SNI listener that has two SNI matches: one for the internal and
					// one for the external shoot domain.
					// We can use either shoot domain to match the filter chain that we want to patch with this EnvoyFilter.
					// The ACL config will apply to traffic going via both the internal and the external API server address.
					// See: https://istio.io/latest/docs/reference/config/networking/envoy-filter/#EnvoyFilter-ListenerMatch-FilterChainMatch
					"sni": hosts[0],
				},
			},
		},
		"patch": principalsToPatch(rbacName, rule.Action, "network", principals),
	}, nil
}

// CreateIngressConfigPatchFromRule creates a network filter patch that can be
// applied to the `GATEWAY` network filter chain matching the wildcard ingress domain.
func CreateIngressConfigPatchFromRule(
	rule *ACLRule, seedIngressDomain, shootID string, alwaysAllowedCIDRs []string,
) map[string]interface{} {
	rbacName := "acl-ingress"
	ingressSuffix := "-" + shootID + "." + seedIngressDomain
	return map[string]interface{}{
		"applyTo": "NETWORK_FILTER",
		"match": map[string]interface{}{
			"context": "GATEWAY",
			"listener": map[string]interface{}{
				"filterChain": map[string]interface{}{
					"sni": "*." + seedIngressDomain,
				},
			},
		},

		"patch": map[string]interface{}{
			"operation": "INSERT_FIRST",
			"value": map[string]interface{}{
				"name": rbacName,
				"typed_config": map[string]interface{}{
					"@type": "type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC",
					"rules": map[string]interface{}{
						"action": "ALLOW",
						"policies": map[string]interface{}{
							shootID + "-inverse": map[string]interface{}{
								"permissions": []map[string]interface{}{{
									"not_rule": map[string]interface{}{
										"requested_server_name": map[string]interface{}{
											"suffix": ingressSuffix,
										},
									},
								}},
								"principals": []map[string]interface{}{{
									"remote_ip": map[string]interface{}{
										"address_prefix": "0.0.0.0",
										"prefix_len":     0,
									},
								}},
							},
							shootID: map[string]interface{}{
								"permissions": []map[string]interface{}{{
									"requested_server_name": map[string]interface{}{
										"suffix": ingressSuffix,
									},
								}},
								"principals": ruleCIDRsToPrincipal(rule, alwaysAllowedCIDRs),
							},
						},
					},
					"stat_prefix": "envoyrbac",
				},
			},
		},
	}
}

// CreateVPNConfigPatchFromRule creates an HTTP filter patch that can be applied to the
// `GATEWAY` HTTP filter chain for the VPN.
func CreateVPNConfigPatchFromRule(rule *ACLRule,
	shortShootID, technicalShootID string, alwaysAllowedCIDRs []string,
) (map[string]interface{}, error) {
	rbacName := "acl-vpn"
	headerMatcher := map[string]interface{}{
		"name": "reversed-vpn",
		"string_match": map[string]interface{}{
			// The actual header value will look something like
			// `outbound|1194||vpn-seed-server.<technical-ID>.svc.cluster.local`.
			// Include dots in the contains matcher as anchors, to always match the entire technical shoot ID.
			// Otherwise, if there was one cluster named `foo` and one named `foo-bar` (in the same project),
			// `foo` would effectively inherit the ACL of `foo-bar`.
			// We don't match with the full header value to allow service names and ports to change while still making sure
			// we catch all traffic targeting this shoot.
			"contains": "." + technicalShootID + ".",
		},
	}
	return map[string]interface{}{
		"applyTo": "HTTP_FILTER",
		"match": map[string]interface{}{
			"context": "GATEWAY",
			"listener": map[string]interface{}{
				"name": "0.0.0.0_8132",
			},
		},
		"patch": map[string]interface{}{
			"operation": "INSERT_FIRST",
			"value": map[string]interface{}{
				"name": rbacName,
				"typed_config": map[string]interface{}{
					"@type": "type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC",
					"rules": map[string]interface{}{
						"action": "ALLOW",
						"policies": map[string]interface{}{
							shortShootID + "-inverse": map[string]interface{}{
								"permissions": []map[string]interface{}{{
									"not_rule": map[string]interface{}{
										"header": headerMatcher,
									},
								}},
								"principals": []map[string]interface{}{{
									"remote_ip": map[string]interface{}{
										"address_prefix": "0.0.0.0",
										"prefix_len":     0,
									},
								}},
							},
							shortShootID: map[string]interface{}{
								"permissions": []map[string]interface{}{{
									"header": headerMatcher,
								}},
								"principals": ruleCIDRsToPrincipal(rule, alwaysAllowedCIDRs),
							},
						},
					},
					"stat_prefix": "envoyrbac",
				},
			},
		},
	}, nil
}

// CreateLegacyVPNConfigPatchFromRule combines a list of ACLMappings and the
// alwaysAllowedCIDRs into a HTTP filter patch that can be applied to the
// `GATEWAY` HTTP filter chain for the VPN.
func CreateLegacyVPNConfigPatchFromRule(
	mappings []ACLMapping, alwaysAllowedCIDRs []string,
) (map[string]interface{}, error) {
	rbacName := "acl-vpn"

	policies := map[string]interface{}{}

	policies[rbacName+"-inverse"] = createInverseVPNPolicy(mappings)

	for i := range mappings {
		mapping := &mappings[i]
		policies[mapping.ShootName] = createVPNPolicyForShoot(
			&mapping.Rule,
			append(alwaysAllowedCIDRs, mapping.ShootSpecificCIDRs...),
			mapping.ShootName,
		)
	}

	return map[string]interface{}{
		"applyTo": "HTTP_FILTER",
		"match": map[string]interface{}{
			"context": "GATEWAY",
			"listener": map[string]interface{}{
				"name": "0.0.0.0_8132",
			},
		},
		"patch": map[string]interface{}{
			"operation": "INSERT_FIRST",
			"value": map[string]interface{}{
				"name": rbacName,
				"typed_config": map[string]interface{}{
					"@type":       "type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC",
					"stat_prefix": "envoyrbac",
					"rules": map[string]interface{}{
						"action":   "ALLOW",
						"policies": policies,
					},
				},
			},
		},
	}, nil
}

// CreateInternalFilterPatchFromRule combines an ACLRule, the
// alwaysAllowedCIDRs, and the shootSpecificCIDRs into a filter patch.
func CreateInternalFilterPatchFromRule(
	rule *ACLRule,
	alwaysAllowedCIDRs []string,
	shootSpecificCIDRs []string,
) (map[string]interface{}, error) {
	rbacName := "acl-internal"
	principals := ruleCIDRsToPrincipal(rule, append(alwaysAllowedCIDRs, shootSpecificCIDRs...))

	return map[string]interface{}{
		"name":         rbacName + "-" + strings.ToLower(rule.Type),
		"typed_config": typedConfigToPatch(rbacName, rule.Action, "network", principals),
	}, nil
}

// ruleCIDRsToPrincipal translates a list of strings in the form "0.0.0.0/0"
// into a list of envoy principals. The function checks for the rule action: If
// the action is "ALLOW", the alwaysAllowedCIDRs are appended to the principals
// to guarantee the downstream flow for these CIDRs is not blocked.
func ruleCIDRsToPrincipal(rule *ACLRule, alwaysAllowedCIDRs []string) []map[string]interface{} {
	principals := []map[string]interface{}{}

	for _, cidr := range rule.Cidrs {
		prefix, length, err := getPrefixAndPrefixLength(cidr)
		if err != nil {
			continue
		}
		principals = append(principals, map[string]interface{}{
			strings.ToLower(rule.Type): map[string]interface{}{
				"address_prefix": prefix,
				"prefix_len":     length,
			},
		})
	}

	// if the rule has action "ALLOW" (which means "limit the access to only the
	// specified IPs", we need to insert the node CIDR range to not block
	// cluster-internal communication)
	if rule.Action == "ALLOW" {
		for _, cidr := range alwaysAllowedCIDRs {
			prefix, length, err := getPrefixAndPrefixLength(cidr)
			if err != nil {
				continue
			}
			principals = append(principals, map[string]interface{}{
				"remote_ip": map[string]interface{}{
					"address_prefix": prefix,
					"prefix_len":     length,
				},
			})
		}
	}

	return principals
}

func getPrefixAndPrefixLength(cidr string) (prefix string, prefixLen int, err error) {
	// rule gets validated early in the code
	ip, mask, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", 0, err
	}
	prefixLen, _ = mask.Mask.Size()

	// TODO use ip here or the one from the mask?
	return ip.String(), prefixLen, nil
}

func principalsToPatch(
	rbacName, ruleAction, filterType string, principals []map[string]interface{},
) map[string]interface{} {
	return map[string]interface{}{
		"operation": "INSERT_FIRST",
		"value": map[string]interface{}{
			"name":         rbacName,
			"typed_config": typedConfigToPatch(rbacName, ruleAction, filterType, principals),
		},
	}
}

func typedConfigToPatch(rbacName, ruleAction, filterType string, principals []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"@type":       "type.googleapis.com/envoy.extensions.filters." + filterType + ".rbac.v3.RBAC",
		"stat_prefix": "envoyrbac",
		"rules": map[string]interface{}{
			"action": strings.ToUpper(ruleAction),
			"policies": map[string]interface{}{
				rbacName: map[string]interface{}{
					"permissions": []map[string]interface{}{
						{"any": true},
					},
					"principals": principals,
				},
			},
		},
	}
}

func createVPNPolicyForShoot(rule *ACLRule, alwaysAllowedCIDRs []string, technicalShootID string) map[string]interface{} {
	// In the case of VPN, we need to nest the principal rules in a EnvoyFilter
	// "and_ids" structure, because we add an additional principal matching on
	// the "reversed-vpn" header, which needs to be ANDed with the other rules.
	// Principals are concatenated using an OR rule, so matching one of them sufficies...
	oredPrincipals := ruleCIDRsToPrincipal(rule, alwaysAllowedCIDRs)

	// ...but only if the VPN header is also set, therefore combine via AND rule
	return map[string]interface{}{
		"permissions": []map[string]interface{}{
			{"any": true},
		},
		"principals": []map[string]interface{}{
			{
				"and_ids": map[string]interface{}{
					"ids": []map[string]interface{}{
						{
							"or_ids": map[string]interface{}{
								"ids": oredPrincipals,
							},
						},
						{
							"header": map[string]interface{}{
								"name": "reversed-vpn",
								"string_match": map[string]interface{}{
									// The actual header value will look something like
									// `outbound|1194||vpn-seed-server.<technical-ID>.svc.cluster.local`.
									// Include dots in the contains matcher as anchors, to always match the entire technical shoot ID.
									// Otherwise, if there was one cluster named `foo` and one named `foo-bar` (in the same project),
									// `foo` would effectively inherit the ACL of `foo-bar`.
									"contains": "." + technicalShootID + ".",
								},
							},
						},
					},
				},
			},
		},
	}
}

func createInverseVPNPolicy(mappings []ACLMapping) map[string]interface{} {
	notHeaderPrincipals := []map[string]interface{}{}

	for i := range mappings {
		notHeaderPrincipals = append(notHeaderPrincipals, map[string]interface{}{
			"not_id": map[string]interface{}{
				"header": map[string]interface{}{
					"name": "reversed-vpn",
					"string_match": map[string]interface{}{
						// The actual header value will look something like
						// `outbound|1194||vpn-seed-server.<technical-ID>.svc.cluster.local`.
						// Include dots in the contains matcher as anchors, to always match the entire technical shoot ID.
						// Otherwise, if there was one cluster named `foo` and one named `foo-bar` (in the same project),
						// `foo` would effectively inherit the ACL of `foo-bar`.
						"contains": "." + mappings[i].ShootName + ".",
					},
				},
			},
		})
	}

	return map[string]interface{}{
		"permissions": []map[string]interface{}{
			{"any": true},
		},
		"principals": []map[string]interface{}{
			{
				"and_ids": map[string]interface{}{
					"ids": notHeaderPrincipals,
				},
			},
		},
	}
}
