package envoyfilters

import (
	"net"
	"strings"
)

type EnvoyFilterService struct{}

type ACLRule struct {
	// Cidrs contains a list of CIDR blocks to which the ACL rule applies
	Cidrs []string `json:"cidrs"`
	// Action defines if the rule is a DENY or an ALLOW rule
	Action string `json:"action"`
	// Type can either be "source_ip", "direct_remote_ip" or "remote_ip"
	Type string `json:"type"`
}

// TODO maybe use cidrs in format or ip/length ? for easier typing?
type Cidr struct {
	// AddressPrefix contains an IP subnet address prefix
	AddressPrefix string `json:"addressPrefix"`
	// PrefixLength determines the length of the address prefix to consider
	PrefixLength int `json:"prefixLength"`
}

// buildEnvoyFilterSpecForHelmChart assembles EnvoyFilter patches for API server
// and VPN networking for every rule in the extension spec.
//
// We use the technical ID of the shoot for the VPN rule, which is de facto the
// same as the seed namespace of the shoot. (Gardener uses the seedNamespace
// value in the botanist vpnshoot task.)
func (e *EnvoyFilterService) BuildEnvoyFilterSpecForHelmChart(
	rules []ACLRule, hosts []string, technicalShootID string, alwaysAllowedCIDRs []string,
) (map[string]interface{}, error) {
	configPatches := []map[string]interface{}{}

	for i := range rules {
		rule := &rules[i]
		// TODO check if rule is well defined

		apiConfigPatch, err := e.CreateAPIConfigPatchFromRule(rule, hosts, alwaysAllowedCIDRs)
		if err != nil {
			return nil, err
		}

		vpnConfigPatch, err := e.CreateVPNConfigPatchFromRule(rule, technicalShootID, alwaysAllowedCIDRs)
		if err != nil {
			return nil, err
		}

		configPatches = append(configPatches, apiConfigPatch, vpnConfigPatch)
	}

	return map[string]interface{}{
		"workloadSelector": map[string]interface{}{
			"labels": map[string]interface{}{
				"app":   "istio-ingressgateway",
				"istio": "ingressgateway",
			},
		},
		"configPatches": configPatches,
	}, nil
}

func (e *EnvoyFilterService) CreateAPIConfigPatchFromRule(
	rule *ACLRule, hosts, alwaysAllowedCIDRs []string,
) (map[string]interface{}, error) {
	// TODO use all hosts?
	host := hosts[0]
	rbacName := "acl-api"
	principals := ruleCIDRsToPrincipal(rule, alwaysAllowedCIDRs)

	return map[string]interface{}{
		"applyTo": "NETWORK_FILTER",
		"match": map[string]interface{}{
			"context": "GATEWAY",
			"listener": map[string]interface{}{
				"filterChain": map[string]interface{}{
					"sni": host,
				},
			},
		},
		"patch": principalsToPatch(rbacName, rule.Action, "network", principals, alwaysAllowedCIDRs),
	}, nil
}

func (e *EnvoyFilterService) CreateVPNConfigPatchFromRule(
	rule *ACLRule, technicalShootID string, alwaysAllowedCIDRs []string,
) (map[string]interface{}, error) {
	rbacName := "acl-vpn"

	// In the case of VPN, we need to nest the principal rules in a EnvoyFilter
	// "and_ids" structure, because we add an additional principal matching on
	// the "reversed-vpn" header, which needs to be ANDed with the other rules.
	// Principals are concatenated using an OR rule, so matching one of them sufficies...
	oredPrincipals := ruleCIDRsToPrincipal(rule, alwaysAllowedCIDRs)

	// ...but only if the VPN header is also set, therefore combine via AND rule
	principals := []map[string]interface{}{
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
								"contains": technicalShootID,
							},
						},
					},
				},
			},
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
		"patch": principalsToPatch(rbacName, rule.Action, "http", principals, alwaysAllowedCIDRs),
	}, nil
}

func (e *EnvoyFilterService) CreateInternalFilterPatchFromRule(rule *ACLRule, alwaysAllowedCIDRs []string) (map[string]interface{}, error) {
	rbacName := "acl-internal"
	principals := ruleCIDRsToPrincipal(rule, alwaysAllowedCIDRs)

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
		prefix, length := getPrefixAndPrefixLength(cidr)
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
			prefix, length := getPrefixAndPrefixLength(cidr)
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

func getPrefixAndPrefixLength(cidr string) (prefix string, prefixLen int) {
	// rule gets validated early in the code
	ip, mask, _ := net.ParseCIDR(cidr)
	prefixLen, _ = mask.Mask.Size()

	// TODO use ip here or the one from the mask?
	return ip.String(), prefixLen
}

func principalsToPatch(
	rbacName, ruleAction, filterType string, principals []map[string]interface{}, alwaysAllowedCIDRs []string,
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
