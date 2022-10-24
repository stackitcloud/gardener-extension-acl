package envoyfilters

import (
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type EnvoyFilterService struct {
	Client client.Client
}

type AclRule struct {
	// Cidrs contains a list of CIDR blocks to which the ACL rule applies
	Cidrs []Cidr `json:"cidrs"`
	// Action defines if the rule is a DENY or an ALLOW rule
	Action string `json:"action"`
	// Type can either be ip, remote, or source
	Type string `json:"type"`
}

// TODO maybe use cidrs in format or ip/length ? for easier typing?
type Cidr struct {
	// AddressPrefix contains an IP subnet address prefix
	AddressPrefix string `json:"addressPrefix"`
	// PrefixLength determines the length of the address prefix to consider
	PrefixLength int `json:"prefixLength"`
}

func (e *EnvoyFilterService) CreateAPIConfigPatchFromRule(rule *AclRule, hosts []string) (map[string]interface{}, error) {
	// TODO use all hosts?
	host := hosts[0]
	rbacName := "acl-api"
	principals := []map[string]interface{}{}

	for i := range rule.Cidrs {
		cidr := &rule.Cidrs[i]
		principals = append(principals, ruleCidrToPrincipal(cidr, rule.Type))
	}

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

func (e *EnvoyFilterService) CreateVPNConfigPatchFromRule(rule *AclRule, shootName string) (map[string]interface{}, error) {
	rbacName := "acl-vpn"

	// In the case of VPN, we need to nest the principal rules in a EnvoyFilter
	// "and_ids" structure, because we add an additional principal matching on
	// the "reversed-vpn" header, which needs to be ANDed with the other rules.
	andedPrincipals := []map[string]interface{}{}

	for i := range rule.Cidrs {
		cidr := &rule.Cidrs[i]
		andedPrincipals = append(andedPrincipals, ruleCidrToPrincipal(cidr, rule.Type))
	}

	andedPrincipals = append(andedPrincipals, map[string]interface{}{
		"header": map[string]interface{}{
			"name": "reversed-vpn",
			"string_match": map[string]interface{}{
				"contains": shootName,
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

func (e *EnvoyFilterService) CreateInternalFilterPatchFromRule(rule *AclRule) (map[string]interface{}, error) {
	rbacName := "acl-internal"
	principals := []map[string]interface{}{}

	for i := range rule.Cidrs {
		cidr := &rule.Cidrs[i]
		principals = append(principals, ruleCidrToPrincipal(cidr, rule.Type))
	}

	return map[string]interface{}{
		"name":         rbacName + "-" + strings.ToLower(rule.Type),
		"typed_config": typedConfigToPatch(rbacName, rule.Action, "network", principals),
	}, nil
}

func ruleCidrToPrincipal(cidr *Cidr, ruleType string) map[string]interface{} {
	return map[string]interface{}{
		ruleType: map[string]interface{}{
			"address_prefix": cidr.AddressPrefix,
			"prefix_len":     cidr.PrefixLength,
		},
	}
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
			"action": ruleAction,
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
