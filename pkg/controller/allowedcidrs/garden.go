package allowedcidrs

import (
	"context"
	"strings"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	seedmanagementv1alpha1 "github.com/gardener/gardener/pkg/apis/seedmanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-acl/pkg/helper"
)

// Garden implements [AllowedCIDRer] and retrieves CIDRs and Hosts relevant for Garden extension
type Garden struct {
	Garden *operatorv1alpha1.Garden
	Client client.Client
}

// AllowedCIDRs returns always allowed cidrs for garden ACL
func (g *Garden) AllowedCIDRs(ctx context.Context, _ *extensionsv1alpha1.Extension) ([]string, error) {
	var cidrs []string
	// add node and pod CIDR from runtime cluster to ensure garden components (e.g. scheduler, controller-manager etc.) are able to access the virtual garden
	cidrs = append(cidrs, helper.GetGardenSpecificAllowedCIDRs(g.Garden)...)

	seedCidrs, err := g.managedSeedsEgressCIDRs(ctx)
	if err != nil {
		return nil, err
	}
	cidrs = append(cidrs, seedCidrs...)
	return cidrs, nil
}

// Hosts returns SNI names of virtual garden kube-apiserver
func (g *Garden) Hosts() ([]string, error) {
	hosts := make([]string, 0)
	for _, address := range g.Garden.Status.AdvertisedAddresses {
		if address.Name != operatorv1alpha1.AdvertisedAddressVirtualGarden {
			continue
		}
		hosts = append(hosts, strings.Split(address.URL, "//")[1])
	}
	if len(hosts) == 0 {
		return nil, ErrNoAdvertisedAddresses
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
