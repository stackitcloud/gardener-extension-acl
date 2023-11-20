package helper

import (
	"encoding/json"
	"errors"

	openstackv1alpha1 "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/v1alpha1"
	"github.com/gardener/gardener-extension-provider-openstack/pkg/openstack"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
)

// Error variables for helper pkg
var (
	ErrProviderStatusRawIsNil  = errors.New("providerStatus.Raw is nil, and can't be unmarshalled")
	ErrWrongInfrastructureType = errors.New("infrastructure type is not correct")
)

// GetProviderSpecificAllowedCIDRs returns the allowed CIDRs for the Infrastructure object.
func GetProviderSpecificAllowedCIDRs(
	infra *extensionsv1alpha1.Infrastructure,
) ([]string, error) {
	cidrs := make([]string, 0)

	//nolint:gocritic // Will likely be extended with other infra types in the future
	switch infra.Spec.Type {
	case openstack.Type:
		openstackCIDRs, err := getOpenstackProviderSpecificAllowedCIDRs(infra)
		if err != nil {
			return nil, err
		}
		cidrs = append(cidrs, openstackCIDRs...)
	}
	return cidrs, nil
}

func getOpenstackProviderSpecificAllowedCIDRs(
	infra *extensionsv1alpha1.Infrastructure,
) ([]string, error) {
	if infra.Spec.Type != openstack.Type {
		return nil, ErrWrongInfrastructureType
	}
	cidrs := make([]string, 0)
	// This would be our preferred solution:
	//
	// infraStatus, err := openstackhelper.InfrastructureStatusFromRaw(infra.Status.ProviderStatus)
	//
	// but decoding isn't possible. We suspect it's because in the function
	// infraStatus is assigned like this:
	//
	// infraStatus := &InfrasttuctureStatus{}
	//
	// instead of doing it without a pointer and then referencing it in the
	// unmarshal step, like we now have to do manually here:
	if infra.Status.ProviderStatus == nil || infra.Status.ProviderStatus.Raw == nil {
		return nil, ErrProviderStatusRawIsNil
	}

	infraStatus := openstackv1alpha1.InfrastructureStatus{}
	err := json.Unmarshal(infra.Status.ProviderStatus.Raw, &infraStatus)
	if err != nil {
		return nil, err
	}

	router32CIDR := infraStatus.Networks.Router.IP + "/32"

	cidrs = append(cidrs, router32CIDR)

	return cidrs, nil
}
