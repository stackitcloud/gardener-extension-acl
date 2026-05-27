package helper

import operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"

// GetGardenSpecificAllowedCIDRs returns the node and pod CIDRs from the garden runtime cluster
func GetGardenSpecificAllowedCIDRs(garden *operatorv1alpha1.Garden) []string {
	cidrs := make([]string, 0)
	cidrs = append(cidrs, garden.Spec.RuntimeCluster.Networking.Nodes...)
	cidrs = append(cidrs, garden.Spec.RuntimeCluster.Networking.Pods...)
	return cidrs
}
