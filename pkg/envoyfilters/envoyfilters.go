package envoyfilters

import (
	"errors"
	"fmt"
	"net"
	"strings"

	envoy_corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_rbacv3 "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	envoy_routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_httprbacv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	envoy_networkrbacv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	envoy_matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/gardener/gardener/extensions/pkg/controller"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	istio_networkingv1alpha3 "istio.io/api/networking/v1alpha3"

	"github.com/stackitcloud/gardener-extension-acl/pkg/helper"
)

// Error variables for envoyfilters pkg
var (
	ErrNoHostsGiven = errors.New("no hosts were given, at least one host is needed")
)

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

func (r *ACLRule) actionProto() envoy_rbacv3.RBAC_Action {
	switch r.Action {
	case "DENY":
		return envoy_rbacv3.RBAC_DENY
	case "ALLOW":
		return envoy_rbacv3.RBAC_ALLOW
	default:
		panic("unknown action")
	}
}

// FilterPatch represents the object beneath EnvoyFilter.spec.configPatches.patch.value
// It holds the name of the filter and it's typed config to inject into the envoy config
type FilterPatch struct {
	Name        string           `json:"name"`
	TypedConfig *structpb.Struct `json:"typed_config"`
}

// asStructPB returns FilterPatch represented as a structpb.Struct
func (f *FilterPatch) asStructPB() *structpb.Struct {
	pb, err := structpb.NewStruct(map[string]any{
		"name":         f.Name,
		"typed_config": f.TypedConfig.AsMap(),
	})
	if err != nil {
		// This state is not valid and should not be propergated
		panic(err)
	}
	return pb
}

// AsMap returns FilterPatch represented as a map[string]interface{}
func (f *FilterPatch) AsMap() map[string]interface{} {
	return f.asStructPB().AsMap()
}

// BuildAPIEnvoyFilterSpecForHelmChart assembles EnvoyFilter patches for API server
// networking for every rule in the extension spec.
func BuildAPIEnvoyFilterSpecForHelmChart(
	rule *ACLRule, hosts, alwaysAllowedCIDRs []string, istioLabels map[string]string,
) (*istio_networkingv1alpha3.EnvoyFilter, error) {
	apiConfigPatch, err := CreateAPIConfigPatchFromRule(rule, hosts, alwaysAllowedCIDRs)
	if err != nil {
		return nil, err
	}
	return &istio_networkingv1alpha3.EnvoyFilter{
		WorkloadSelector: &istio_networkingv1alpha3.WorkloadSelector{
			Labels: istioLabels,
		},
		ConfigPatches: []*istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
			apiConfigPatch,
		},
	}, nil
}

// BuildIngressEnvoyFilterSpecForHelmChart assembles EnvoyFilter patches for
// endpoints using the seed ingress domain.
func BuildIngressEnvoyFilterSpecForHelmChart(
	cluster *controller.Cluster, rule *ACLRule, alwaysAllowedCIDRs []string, istioLabels map[string]string,
) *istio_networkingv1alpha3.EnvoyFilter {
	seedIngressDomain := helper.GetSeedIngressDomain(cluster.Seed)
	if seedIngressDomain != "" {
		shootID := helper.ComputeShortShootID(cluster.Shoot)

		return &istio_networkingv1alpha3.EnvoyFilter{
			WorkloadSelector: &istio_networkingv1alpha3.WorkloadSelector{
				Labels: istioLabels,
			},
			ConfigPatches: []*istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
				ingressConfigPatchFromRule(rule, seedIngressDomain, shootID, alwaysAllowedCIDRs),
			},
		}
	}
	return nil
}

// ingressConfigPatchFromRule creates a network filter patch that can be
// applied to the `GATEWAY` network filter chain matching the wildcard ingress domain.
func ingressConfigPatchFromRule(
	rule *ACLRule, seedIngressDomain, shootID string, alwaysAllowedCIDRs []string,
) *istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	rbacName := "acl-ingress"
	ingressSuffix := "-" + shootID + "." + seedIngressDomain

	rbacFilter := &envoy_networkrbacv3.RBAC{
		StatPrefix: "envoyrbac",
		Rules: &envoy_rbacv3.RBAC{
			Action: envoy_rbacv3.RBAC_ALLOW,
			Policies: map[string]*envoy_rbacv3.Policy{
				shootID + "-inverse": {
					Permissions: []*envoy_rbacv3.Permission{
						{
							Rule: &envoy_rbacv3.Permission_NotRule{
								NotRule: &envoy_rbacv3.Permission{
									Rule: &envoy_rbacv3.Permission_RequestedServerName{
										RequestedServerName: &envoy_matcherv3.StringMatcher{
											MatchPattern: &envoy_matcherv3.StringMatcher_Suffix{
												Suffix: ingressSuffix,
											},
										},
									},
								},
							},
						},
					},
					Principals: []*envoy_rbacv3.Principal{
						{
							Identifier: &envoy_rbacv3.Principal_RemoteIp{
								RemoteIp: &envoy_corev3.CidrRange{
									AddressPrefix: "0.0.0.0",
									PrefixLen:     wrapperspb.UInt32(0),
								},
							},
						},
					},
				},
				shootID: {
					Permissions: []*envoy_rbacv3.Permission{
						{
							Rule: &envoy_rbacv3.Permission_RequestedServerName{
								RequestedServerName: &envoy_matcherv3.StringMatcher{
									MatchPattern: &envoy_matcherv3.StringMatcher_Suffix{
										Suffix: ingressSuffix,
									},
								},
							},
						},
					},
					Principals: ruleCIDRsToPrincipal(rule, alwaysAllowedCIDRs),
				},
			},
		},
	}
	typedConfig, err := protoMessageToTypedConfig(rbacFilter)
	if err != nil {
		// this is a error in the code itself, don't return to caller
		panic(err)
	}
	filter := &FilterPatch{
		Name:        rbacName,
		TypedConfig: typedConfig,
	}
	return &istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istio_networkingv1alpha3.EnvoyFilter_NETWORK_FILTER,
		Match: &istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istio_networkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &istio_networkingv1alpha3.EnvoyFilter_ListenerMatch{
					FilterChain: &istio_networkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Sni: fmt.Sprintf("*.%s", seedIngressDomain),
					},
				},
			},
		},
		Patch: &istio_networkingv1alpha3.EnvoyFilter_Patch{
			Operation: istio_networkingv1alpha3.EnvoyFilter_Patch_INSERT_FIRST,
			Value:     filter.asStructPB(),
		},
	}
}

// BuildVPNEnvoyFilterSpecForHelmChart assembles EnvoyFilter patches for VPN.
func BuildVPNEnvoyFilterSpecForHelmChart(
	cluster *controller.Cluster, rule *ACLRule, alwaysAllowedCIDRs []string, istioLabels map[string]string,
) *istio_networkingv1alpha3.EnvoyFilter {
	return &istio_networkingv1alpha3.EnvoyFilter{
		WorkloadSelector: &istio_networkingv1alpha3.WorkloadSelector{
			Labels: istioLabels,
		},
		ConfigPatches: []*istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
			vpnConfigPatchFromRule(rule, helper.ComputeShortShootID(cluster.Shoot), cluster.Shoot.Status.TechnicalID, alwaysAllowedCIDRs),
		},
	}
}

// vpnConfigPatchFromRule creates an HTTP filter patch that can be applied to the
// `GATEWAY` HTTP filter chain for the VPN.
func vpnConfigPatchFromRule(rule *ACLRule,
	shortShootID, technicalShootID string, alwaysAllowedCIDRs []string,
) *istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	rbacName := "acl-vpn"
	headerMatcher := envoy_routev3.HeaderMatcher{
		Name: "reversed-vpn",
		HeaderMatchSpecifier: &envoy_routev3.HeaderMatcher_StringMatch{
			StringMatch: &envoy_matcherv3.StringMatcher{
				MatchPattern: &envoy_matcherv3.StringMatcher_Contains{
					// The actual header value will look something like
					// `outbound|1194||vpn-seed-server.<technical-ID>.svc.cluster.local`.
					// Include dots in the contains matcher as anchors, to always match the entire technical shoot ID.
					// Otherwise, if there was one cluster named `foo` and one named `foo-bar` (in the same project),
					// `foo` would effectively inherit the ACL of `foo-bar`.
					// We don't match with the full header value to allow service names and ports to change while still making sure
					// we catch all traffic targeting this shoot.
					Contains: "." + technicalShootID + ".",
				},
			},
		},
	}

	rbacFilter := &envoy_httprbacv3.RBAC{
		RulesStatPrefix: "envoyrbac",
		Rules: &envoy_rbacv3.RBAC{
			Action: envoy_rbacv3.RBAC_ALLOW,
			Policies: map[string]*envoy_rbacv3.Policy{
				shortShootID + "-inverse": {
					Permissions: []*envoy_rbacv3.Permission{
						{
							Rule: &envoy_rbacv3.Permission_NotRule{
								NotRule: &envoy_rbacv3.Permission{
									Rule: &envoy_rbacv3.Permission_Header{
										Header: &headerMatcher,
									},
								},
							},
						},
					},
					Principals: []*envoy_rbacv3.Principal{
						{
							Identifier: &envoy_rbacv3.Principal_RemoteIp{
								RemoteIp: &envoy_corev3.CidrRange{
									AddressPrefix: "0.0.0.0",
									PrefixLen:     wrapperspb.UInt32(0),
								},
							},
						},
					},
				},
				shortShootID: {
					Permissions: []*envoy_rbacv3.Permission{
						{
							Rule: &envoy_rbacv3.Permission_Header{
								Header: &headerMatcher,
							},
						},
					},
					Principals: ruleCIDRsToPrincipal(rule, alwaysAllowedCIDRs),
				},
			},
		},
	}
	typedConfig, err := protoMessageToTypedConfig(rbacFilter)
	if err != nil {
		// this is a error in the code itself, don't return to caller
		panic(err)
	}
	filterPatch := &FilterPatch{
		Name:        rbacName,
		TypedConfig: typedConfig,
	}
	return &istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istio_networkingv1alpha3.EnvoyFilter_HTTP_FILTER,
		Match: &istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istio_networkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &istio_networkingv1alpha3.EnvoyFilter_ListenerMatch{
					Name: "0.0.0.0_8132",
				},
			},
		},
		Patch: &istio_networkingv1alpha3.EnvoyFilter_Patch{
			Operation: istio_networkingv1alpha3.EnvoyFilter_Patch_INSERT_FIRST,
			Value:     filterPatch.asStructPB(),
		},
	}
}

// CreateAPIConfigPatchFromRule combines an ACLRule, the first entry  of the
// hosts list and the alwaysAllowedCIDRs into a network filter patch that can be
// applied to the `GATEWAY` network filter chain matching the host.
func CreateAPIConfigPatchFromRule(
	rule *ACLRule, hosts, alwaysAllowedCIDRs []string,
) (*istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, error) {
	if len(hosts) == 0 {
		return nil, ErrNoHostsGiven
	}
	rbacName := "acl-api"
	principals := ruleCIDRsToPrincipal(rule, alwaysAllowedCIDRs)

	return &istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istio_networkingv1alpha3.EnvoyFilter_NETWORK_FILTER,
		Match: &istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istio_networkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istio_networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &istio_networkingv1alpha3.EnvoyFilter_ListenerMatch{
					FilterChain: &istio_networkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Sni: hosts[0],
					},
				},
			},
		},
		Patch: principalsToPatch(rbacName, rule.actionProto(), principals),
	}, nil
}

func principalsToPatch(
	rbacName string, ruleAction envoy_rbacv3.RBAC_Action, principals []*envoy_rbacv3.Principal,
) *istio_networkingv1alpha3.EnvoyFilter_Patch {
	rbacFilter := newRBACFilter(rbacName, ruleAction, principals)
	typedConfig, err := protoMessageToTypedConfig(rbacFilter)
	if err != nil {
		// this is a error in the code itself, don't return to caller
		panic(err)
	}
	filter := &FilterPatch{
		Name:        rbacName,
		TypedConfig: typedConfig,
	}
	return &istio_networkingv1alpha3.EnvoyFilter_Patch{
		Operation: istio_networkingv1alpha3.EnvoyFilter_Patch_INSERT_FIRST,
		Value:     filter.asStructPB(),
	}
}

// CreateInternalFilterPatchFromRule combines an ACLRule, the
// alwaysAllowedCIDRs, and the shootSpecificCIDRs into a filter patch.
func CreateInternalFilterPatchFromRule(
	rule *ACLRule,
	alwaysAllowedCIDRs []string,
	shootSpecificCIDRs []string,
) *FilterPatch {
	rbacName := "acl-internal"
	principals := ruleCIDRsToPrincipal(rule, append(alwaysAllowedCIDRs, shootSpecificCIDRs...))
	rbacFilter := newRBACFilter(rbacName, rule.actionProto(), principals)
	typedConfig, err := protoMessageToTypedConfig(rbacFilter)
	if err != nil {
		// this is a error in the code itself, don't return to caller
		panic(err)
	}

	return &FilterPatch{
		Name:        rbacName + "-" + strings.ToLower(rule.Type),
		TypedConfig: typedConfig,
	}
}

// ruleCIDRsToPrincipal translates a list of strings in the form "0.0.0.0/0"
// into a list of envoy principals. The function checks for the rule action: If
// the action is "ALLOW", the alwaysAllowedCIDRs are appended to the principals
// to guarantee the downstream flow for these CIDRs is not blocked.
func ruleCIDRsToPrincipal(rule *ACLRule, alwaysAllowedCIDRs []string) []*envoy_rbacv3.Principal {
	principals := []*envoy_rbacv3.Principal{}

	for _, cidr := range rule.Cidrs {
		prefix, length, err := getPrefixAndPrefixLength(cidr)
		if err != nil {
			continue
		}
		cidrRange := envoy_corev3.CidrRange{
			AddressPrefix: prefix,
			PrefixLen:     wrapperspb.UInt32(uint32(length)),
		}
		p := new(envoy_rbacv3.Principal)
		switch strings.ToLower(rule.Type) {
		case "source_ip":
			p.Identifier = &envoy_rbacv3.Principal_SourceIp{SourceIp: &cidrRange}
		case "remote_ip":
			p.Identifier = &envoy_rbacv3.Principal_RemoteIp{RemoteIp: &cidrRange}
		case "direct_remote_ip":
			p.Identifier = &envoy_rbacv3.Principal_DirectRemoteIp{DirectRemoteIp: &cidrRange}
		default:
			continue
		}
		principals = append(principals, p)
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
			principals = append(principals, &envoy_rbacv3.Principal{
				Identifier: &envoy_rbacv3.Principal_RemoteIp{
					RemoteIp: &envoy_corev3.CidrRange{
						AddressPrefix: prefix,
						PrefixLen:     wrapperspb.UInt32(uint32(length)),
					},
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

func newRBACFilter(rbacName string, ruleAction envoy_rbacv3.RBAC_Action, principals []*envoy_rbacv3.Principal) *envoy_networkrbacv3.RBAC {
	return &envoy_networkrbacv3.RBAC{
		StatPrefix: "envoyrbac",
		Rules: &envoy_rbacv3.RBAC{
			Action: ruleAction,
			Policies: map[string]*envoy_rbacv3.Policy{
				rbacName: {
					Permissions: []*envoy_rbacv3.Permission{
						{
							Rule: &envoy_rbacv3.Permission_Any{
								Any: true,
							},
						},
					},
					Principals: principals,
				},
			},
		},
	}
}

func protoMessageToTypedConfig(m proto.Message) (*structpb.Struct, error) {
	raw, err := protojson.MarshalOptions{
		UseProtoNames: true,
	}.Marshal(m)
	if err != nil {
		return nil, err
	}
	s := new(structpb.Struct)
	if err := protojson.Unmarshal(raw, s); err != nil {
		return nil, err
	}
	typeName := fmt.Sprintf("type.googleapis.com/%s", proto.MessageName(m))
	s.Fields["@type"] = structpb.NewStringValue(typeName)
	return s, nil
}
