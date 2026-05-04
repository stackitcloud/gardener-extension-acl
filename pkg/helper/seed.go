package helper

import (
	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
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

// GetGardenSpecificAllowedCIDRs returns the node and pod CIDRs from the garden runtime cluster
func GetGardenSpecificAllowedCIDRs(garden *operatorv1alpha1.Garden) []string {
	cidrs := make([]string, 0)
	cidrs = append(cidrs, garden.Spec.RuntimeCluster.Networking.Nodes...)
	cidrs = append(cidrs, garden.Spec.RuntimeCluster.Networking.Pods...)
	return cidrs
}
