package helper

import (
	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
)

// GetSeedSpecificAllowedCIDRs returns the node and pod CIDRs from the seed
func GetSeedSpecificAllowedCIDRs(seed *v1beta1.Seed) []string {
	cidrs := make([]string, 0)
	if seed.Spec.Networks.Nodes != nil {
		cidrs = append(cidrs, *seed.Spec.Networks.Nodes)
	}
	if seed.Spec.Networks.Pods != "" {
		cidrs = append(cidrs, seed.Spec.Networks.Pods)
	}
	return cidrs
}

// GetSeedIngressDomain returns the ingress domain of the seed
func GetSeedIngressDomain(seed *v1beta1.Seed) string {
	domain := ""
	if seed.Spec.Ingress != nil {
		domain = seed.Spec.Ingress.Domain
	}
	return domain
}
