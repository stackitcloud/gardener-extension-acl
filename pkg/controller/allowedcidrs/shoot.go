package allowedcidrs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/gardener/gardener/extensions/pkg/controller"
	v1beta1helper "github.com/gardener/gardener/pkg/api/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stackitcloud/gardener-extension-acl/pkg/helper"
)

// Shoot implements [AllowedCIDRer] and retrieves CIDRs and Hosts relevant for Shoot extension
type Shoot struct {
	Cluster        *controller.Cluster
	Client         client.Client
	IstioNamespace string
}

// AllowedCIDRs returns always allowed cidrs for shoot ACL.
// It contains:
// - SeedSpecific CIDRs
// - Pod and Node CIDR if shoot is not workerless
func (s *Shoot) AllowedCIDRs(ctx context.Context, ex *extensionsv1alpha1.Extension) ([]string, error) {
	log := logf.FromContext(ctx)
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

	// This relies on the LB hairpinning in-cluster traffic out and back in
	// through the Seed's egress IP, which is the common case when the LB
	// exposes ipMode: Proxy and the CNI does not short-circuit clusterIP
	// traffic (e.g., Cilium with bpfSocketLBHostnsOnly: true).
	if ok, err := s.usesProxyTypeLBService(ctx); err != nil {
		log.Error(err, "unable to get Istio Ingressgateway service", "namespace", s.IstioNamespace)
		return nil, fmt.Errorf("unable to get istio service: %w", err)
	} else if ok {
		egressCIDRs, err := s.getSeedEgressIPOnManagedSeeds(ctx)
		if err != nil {
			return nil, err
		}
		alwaysAllowedCIDRs = append(alwaysAllowedCIDRs, egressCIDRs...)
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

// getSeedEgressIPOnManagedSeeds returns the egressIP CIDRs of the ManagedSeed, if the
// Seed is not a shoot, it will return an empty list
func (s *Shoot) getSeedEgressIPOnManagedSeeds(ctx context.Context) ([]string, error) {
	cm := corev1.ConfigMap{}
	if err := s.Client.Get(ctx,
		client.ObjectKey{
			Name:      v1beta1constants.ConfigMapNameShootInfo,
			Namespace: metav1.NamespaceSystem,
		},
		&cm); err != nil {
		if apierrors.IsNotFound(err) {
			return []string{}, nil
		}
		return nil, err
	}

	cidrsStr, ok := cm.Data["egressCIDRs"]
	if !ok {
		return nil, errors.New("unable to get egress CIDRs from shoot-info ConfigMap")
	}

	var cidrs []string
	for i := range strings.SplitSeq(cidrsStr, ",") {
		_, _, err := net.ParseCIDR(i)
		if err != nil {
			return nil, err
		}
		cidrs = append(cidrs, i)
	}

	return cidrs, nil
}

// usesProxyTypeLBService checks the `istio-ingressgateway` LoadBalancer Service
// selected by its labels whether it is exposing the service with the Proxy IPMode
func (s *Shoot) usesProxyTypeLBService(
	ctx context.Context,
) (bool, error) {
	svc := corev1.Service{}
	err := s.Client.Get(
		ctx,
		client.ObjectKey{
			Name:      v1beta1constants.DefaultSNIIngressServiceName,
			Namespace: s.IstioNamespace,
		},
		&svc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, ing := range svc.Status.LoadBalancer.Ingress {
		if m := ing.IPMode; m != nil && *m == corev1.LoadBalancerIPModeProxy {
			return true, nil
		}
	}

	return false, nil
}
