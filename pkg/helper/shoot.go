package helper

import (
	"fmt"
	"regexp"

	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
)

// GetShootNodeSpecificAllowedCIDRs returns the node CIDRs of the shoot
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

// GetShootPodSpecificAllowedCIDRs returns the pod CIDRs of the shoot
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

// Copied calculation of shortID from here https://github.com/gardener/gardener/blob/41a694b024144465bd4f02f39d6d1c3493549c2f/pkg/gardenlet/operation/operation.go#L435-L442
// TODO: Move to a shared helper function.

// technicalIDPattern addresses the ambiguity that one or two dashes could follow the prefix "shoot" in the technical ID of the shoot.
var technicalIDPattern = regexp.MustCompile(fmt.Sprintf("^%s-?", v1beta1constants.TechnicalIDPrefix))

// ComputeShortShootID computes the host for a given prefix.
func ComputeShortShootID(shoot *v1beta1.Shoot) string {
	shortID := technicalIDPattern.ReplaceAllString(shoot.Status.TechnicalID, "")
	return shortID
}
