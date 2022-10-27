package envoyfilters

import (
	"net"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type EnvoyFilterService struct {
	Client client.Client
}

type ACLRule struct {
	// Cidrs contains a list of CIDR blocks to which the ACL rule applies
	Cidrs []string `json:"cidrs"`
	// Action defines if the rule is a DENY or an ALLOW rule
	Action string `json:"action"`
	// Type can either be "source_ip", "direct_remote_ip" or "remote_ip"
	Type string `json:"type"`
}

func (e *EnvoyFilterService) CreateAPIConfigPatchFromRule(rule *ACLRule, hosts []string) (map[string]interface{}, error) {
	// TODO use all hosts?
	host := hosts[0]
	rbacName := "acl-api"
	principals := ruleCidrsToPrincipal(rule.Cidrs, rule.Type)

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
		"patch": principalsToPatch(rbacName, rule.Action, "network", principals),
	}, nil
}

func (e *EnvoyFilterService) CreateVPNConfigPatchFromRule(rule *ACLRule, technicalShootID string) (map[string]interface{}, error) {
	rbacName := "acl-vpn"

	// In the case of VPN, we need to nest the principal rules in a EnvoyFilter
	// "and_ids" structure, because we add an additional principal matching on
	// the "reversed-vpn" header, which needs to be ANDed with the other rules.

	andedPrincipals := ruleCidrsToPrincipal(rule.Cidrs, rule.Type)

	andedPrincipals = append(andedPrincipals, map[string]interface{}{
		"header": map[string]interface{}{
			"name": "reversed-vpn",
			"string_match": map[string]interface{}{
				"contains": technicalShootID,
			},
		},
	})

	principals := []map[string]interface{}{
		{
			"and_ids": map[string]interface{}{
				"ids": andedPrincipals,
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
		"patch": principalsToPatch(rbacName, rule.Action, "http", principals),
	}, nil
}

func (e *EnvoyFilterService) CreateInternalFilterPatchFromRule(rule *ACLRule) (map[string]interface{}, error) {
	rbacName := "acl-internal"
	principals := ruleCidrsToPrincipal(rule.Cidrs, rule.Type)

	return map[string]interface{}{
		"name":         rbacName + "-" + strings.ToLower(rule.Type),
		"typed_config": typedConfigToPatch(rbacName, rule.Action, "network", principals),
	}, nil
}

func ruleCidrsToPrincipal(cidrs []string, ruleType string) []map[string]interface{} {
	principals := []map[string]interface{}{}

	for _, cidr := range cidrs {
		// rule gets validated early in the code
		ip, mask, _ := net.ParseCIDR(cidr)
		prefix, _ := mask.Mask.Size()

		principals = append(principals, map[string]interface{}{
			strings.ToLower(ruleType): map[string]interface{}{
				// TODO use ip here or the one from the mask?
				"address_prefix": ip.String(),
				"prefix_len":     prefix,
			},
		})
	}

	return principals
}

func principalsToPatch(rbacName, ruleAction, filterType string, principals []map[string]interface{}) map[string]interface{} {
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
