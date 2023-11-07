package helper

import (
	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
)

func GetShootNodeSpecificAllowedCIDRs(shoot *v1beta1.Shoot) []string {
	cidrs := make([]string, 0)

	if shoot.Spec.Networking == nil {
		return cidrs
	}
	if shoot.Spec.Networking.Nodes != nil {
		cidrs = append(cidrs, *shoot.Spec.Networking.Nodes)
	}
	return cidrs
}

func GetShootPodSpecificAllowedCIDRs(shoot *v1beta1.Shoot) []string {
	cidrs := make([]string, 0)

	if shoot.Spec.Networking == nil {
		return cidrs
	}
	if shoot.Spec.Networking.Pods != nil {
		cidrs = append(cidrs, *shoot.Spec.Networking.Pods)
	}
	return cidrs
}
