package allowedcidrs

import (
	"context"
	"strings"

	gardenercorev1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Shoot", func() {
	var shoot *Shoot
	istioNamespace := "istio-ingress"
	istioIngressGatewayServiceName := "istio-ingressgateway"
	var k8sClient client.Client

	BeforeEach(func() {
		k8sClient = fake.NewFakeClient()
		shoot = &Shoot{
			Client:         k8sClient,
			IstioNamespace: istioNamespace,
		}
	})

	Describe("#usesProxyTypeLBService", func() {
		BeforeEach(func(ctx context.Context) {
			createNewService(
				ctx,
				k8sClient,
				istioIngressGatewayServiceName,
				istioNamespace,
				corev1.ServiceTypeLoadBalancer,
			)
			updateServiceStatus(
				ctx,
				k8sClient,
				istioIngressGatewayServiceName,
				istioNamespace,
				corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{
							IP:     "1.1.1.1",
							IPMode: ptr.To(corev1.LoadBalancerIPModeProxy),
						}},
					},
				},
			)
		})

		It("should not get the egressIPs if the LoadBalancer IPMode is not set to Proxy", func(ctx context.Context) {
			updateServiceStatus(
				ctx,
				k8sClient,
				istioIngressGatewayServiceName,
				istioNamespace,
				corev1.ServiceStatus{},
			)
			Expect(shoot.usesProxyTypeLBService(ctx)).To(BeFalse())

			updateServiceStatus(
				ctx,
				k8sClient,
				istioIngressGatewayServiceName,
				istioNamespace,
				corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{
							IP:     "1.1.1.1",
							IPMode: ptr.To(corev1.LoadBalancerIPModeVIP),
						}},
					},
				},
			)
			Expect(shoot.usesProxyTypeLBService(ctx)).To(BeFalse())
		})

		It("should get the egressIPs if the LoadBalancer IPMode is set to Proxy", func(ctx context.Context) {
			Expect(shoot.usesProxyTypeLBService(ctx)).To(BeTrue())
		})
	})
	Describe("#getSeedEgressIPOnManagedSeeds", func() {
		It("should return an empty slice of egressIPs if no shoot-info ConfigMap exists", func(ctx context.Context) {
			cidrs, err := shoot.getSeedEgressIPOnManagedSeeds(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(cidrs).To(BeEmpty())
		})

		It("should fail to return egressIPs if the shoot-info ConfigMap contains invalid CIDRs", func(ctx context.Context) {
			createShootInfo(ctx, k8sClient, []string{"1.1.1.1", "1.1.1.2/32"})

			_, err := shoot.getSeedEgressIPOnManagedSeeds(ctx)
			Expect(err).To(HaveOccurred())
		})

		It("should return the egressIP CIDRs of the shoot-info ConfigMap", func(ctx context.Context) {
			c := []string{"1.1.1.1/32", "1.1.1.2/32"}
			createShootInfo(ctx, k8sClient, c)

			cidrs, err := shoot.getSeedEgressIPOnManagedSeeds(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(cidrs).To(BeEquivalentTo(c))
		})
	})
})

func createNewService(ctx context.Context, k8sClient client.Client, name, namespace string, serviceType corev1.ServiceType) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Ports: []corev1.ServicePort{{
				Port: 80,
			}},
		},
	}
	GinkgoLogr.Info("creating service", "name", svc.Name, "namespace", svc.Namespace, "labels", svc.Labels)
	Expect(k8sClient.Create(ctx, svc)).ShouldNot(HaveOccurred())
}

func updateServiceStatus(ctx context.Context, k8sClient client.Client, name, namespace string, status corev1.ServiceStatus) {
	svc := &corev1.Service{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, svc)).ShouldNot(HaveOccurred())
	svc.Status = status
	Expect(k8sClient.Status().Update(ctx, svc)).ShouldNot(HaveOccurred())
}

func createShootInfo(ctx context.Context, k8sClient client.Client, cidrs []string) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gardenercorev1beta1constants.ConfigMapNameShootInfo,
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"egressCIDRs": strings.Join(cidrs, ","),
		},
	}
	Expect(k8sClient.Create(ctx, cm)).ShouldNot(HaveOccurred())
}
