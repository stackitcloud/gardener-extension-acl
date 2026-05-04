package allowedcidrs

import (
	"context"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	seedmanagementv1alpha1 "github.com/gardener/gardener/pkg/apis/seedmanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/types"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	"github.com/stackitcloud/gardener-extension-acl/pkg/helper"
)

type Garden struct {
	Garden *operatorv1alpha1.Garden
	Client client.Client
}

func (g *Garden) AllowedCIDRs(ctx context.Context, ex *extensionsv1alpha1.Extension) ([]string, error) {
	var cidrs []string
	cidrs = append(cidrs, helper.GetGardenSpecificAllowedCIDRs(g.Garden)...)

	seedCidrs, err := g.managedSeedsEgressCIDRs(ctx)
	if err != nil {
		return nil, err
	}
	cidrs = append(cidrs, seedCidrs...)
	return cidrs, nil
}

func (g *Garden) Hosts() ([]string, error) {
	hosts := make([]string, 0)
	if g.Garden.Status.VirtualClusterStatus == nil || len(g.Garden.Status.VirtualClusterStatus.AdvertisedAddresses) == 0 {
		return nil, ErrNoAdvertisedAddresses
	}
	for _, address := range g.Garden.Status.VirtualClusterStatus.AdvertisedAddresses {
		if address.Name != operatorv1alpha1.AdvertisedAddressVirtual {
			continue
		}
		hosts = append(hosts, strings.Split(address.URL, "//")[1])
	}
	return hosts, nil
}

func (g *Garden) managedSeedsEgressCIDRs(ctx context.Context) ([]string, error) {
	managedSeeds := &seedmanagementv1alpha1.ManagedSeedList{}
	if err := g.Client.List(ctx, managedSeeds, client.InNamespace(v1beta1constants.GardenNamespace)); err != nil {
		return nil, err
	}

	seedCIDRs := make([]string, 0, len(managedSeeds.Items))
	for _, managedSeed := range managedSeeds.Items {
		shoot := &gardencorev1beta1.Shoot{}
		if err := g.Client.Get(ctx, types.NamespacedName{Namespace: managedSeed.Namespace, Name: managedSeed.Spec.Shoot.Name}, shoot); err != nil {
			return nil, err
		}
		if shoot.Status.Networking == nil {
			continue
		}
		seedCIDRs = append(seedCIDRs, shoot.Status.Networking.EgressCIDRs...)
	}

	return seedCIDRs, nil
}
