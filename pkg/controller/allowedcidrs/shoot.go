package allowedcidrs

import (
	"context"
	"strings"

	"github.com/gardener/gardener/extensions/pkg/controller"
	v1beta1helper "github.com/gardener/gardener/pkg/api/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-acl/pkg/helper"
)

// Shoot implements [AllowedCIDRer] and retrieves CIDRs and Hosts relevant for Shoot extension
type Shoot struct {
	Cluster *controller.Cluster
	Client  client.Client
}

// AllowedCIDRs returns always allowed cidrs for shoot ACL.
// It contains:
// - SeedSpecific CIDRs
// - Pod and Node CIDR if shoot is not workerless
func (s *Shoot) AllowedCIDRs(ctx context.Context, ex *extensionsv1alpha1.Extension) ([]string, error) {
	var shootSpecificCIDRs []string
	var alwaysAllowedCIDRs []string

	alwaysAllowedCIDRs = append(alwaysAllowedCIDRs, helper.GetSeedSpecificAllowedCIDRs(s.Cluster.Seed)...)

	// Gardener supports workerless Shoots. These don't have an associated
	// Infrastructure object and don't need Node- or Pod-specific CIDRs to be
	// allowed. Therefore, skip these steps for workerless Shoots.
	if !v1beta1helper.IsWorkerless(s.Cluster.Shoot) {
		shootSpecificCIDRs = append(shootSpecificCIDRs, helper.GetShootNodeSpecificAllowedCIDRs(s.Cluster.Shoot)...)

		infra, err := helper.GetInfrastructureForExtension(ctx, s.Client, ex, s.Cluster.Shoot.Name)
		if err != nil {
			return nil, err
		}

		shootSpecificCIDRs = append(shootSpecificCIDRs, infra.Status.EgressCIDRs...)
	}

	alwaysAllowedCIDRs = append(alwaysAllowedCIDRs, shootSpecificCIDRs...)
	return alwaysAllowedCIDRs, nil
}

// Hosts returns SNI names of shoot's kube-apiserver
func (s *Shoot) Hosts() ([]string, error) {
	hosts := make([]string, 0)
	if len(s.Cluster.Shoot.Status.AdvertisedAddresses) < 1 {
		return nil, ErrNoAdvertisedAddresses
	}

	for _, address := range s.Cluster.Shoot.Status.AdvertisedAddresses {
		hosts = append(hosts, strings.Split(address.URL, "//")[1])
	}
	return hosts, nil
}
