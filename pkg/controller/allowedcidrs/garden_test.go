package allowedcidrs

import (
	"context"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	operatorv1alpha1 "github.com/gardener/gardener/pkg/apis/operator/v1alpha1"
	seedmanagementv1alpha1 "github.com/gardener/gardener/pkg/apis/seedmanagement/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("#Garden", func() {
	var garden *Garden
	nodeCIDR := "192.168.0.0/16"
	podCIDR := "10.0.0.0/16"

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		Expect(seedmanagementv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(gardencorev1beta1.AddToScheme(scheme)).To(Succeed())
		garden = &Garden{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			Garden: &operatorv1alpha1.Garden{
				Spec: operatorv1alpha1.GardenSpec{
					RuntimeCluster: operatorv1alpha1.RuntimeCluster{
						Networking: operatorv1alpha1.RuntimeNetworking{
							Pods:  []string{podCIDR},
							Nodes: []string{nodeCIDR},
						},
					},
				},
			},
		}
	})

	Describe("#AllowedCIDRs", func() {
		Context("without managed seeds", func() {
			It("should return pod and node cidr from garden runtime cluster", func(ctx context.Context) {
				cidrs, err := garden.AllowedCIDRs(ctx, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(cidrs).To(ConsistOf(podCIDR, nodeCIDR))
			})
		})
		Context("with managed seeds", func() {
			BeforeEach(func(ctx context.Context) {
				managedSeed := &seedmanagementv1alpha1.ManagedSeed{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "seed",
						Namespace: constants.GardenNamespace,
					},
					Spec: seedmanagementv1alpha1.ManagedSeedSpec{
						Shoot: &seedmanagementv1alpha1.Shoot{
							Name: "shoot",
						},
					},
				}
				shoot := &gardencorev1beta1.Shoot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "shoot",
						Namespace: constants.GardenNamespace,
					},
					Status: gardencorev1beta1.ShootStatus{
						Networking: &gardencorev1beta1.NetworkingStatus{
							EgressCIDRs: []string{"192.168.198.20/32"},
						},
					},
				}
				Expect(garden.Client.Create(ctx, managedSeed)).To(Succeed(), "managed seed create")
				Expect(garden.Client.Create(ctx, shoot)).To(Succeed(), "shoot create")
			})
			It("should add egress cidrs of managed seed shoots", func(ctx context.Context) {
				cidrs, err := garden.AllowedCIDRs(ctx, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(cidrs).To(ContainElements("192.168.198.20/32"))
			})
		})
	})

	Describe("#Hosts", func() {
		It("should ErrNoAdvertisedAddresses if no AdvertisedAddresses is present in garden", func() {
			_, err := garden.Hosts()
			Expect(err).To(MatchError(ErrNoAdvertisedAddresses))
		})

		It("should return only addresses of virtual garden", func() {
			garden.Garden.Status.AdvertisedAddresses = []operatorv1alpha1.AdvertisedAddress{
				{
					Name: "foo",
					URL:  "https://foo",
				},
				{
					Name: operatorv1alpha1.AdvertisedAddressVirtualGarden,
					URL:  "https://bar",
				},
			}
			hosts, err := garden.Hosts()
			Expect(err).NotTo(HaveOccurred())
			Expect(hosts).To(ConsistOf("bar"))
		})
	})
})
