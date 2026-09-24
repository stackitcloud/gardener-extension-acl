package controller

import (
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/component/networking/istio"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("findIstioNamespaceForExtension", func() {
	var (
		a              *actuator
		shootNamespace string
		selector       map[string]string
		ex             *extensionsv1alpha1.Extension
	)

	BeforeEach(func() {
		a = getNewActuator()
		shootNamespace = createNewShootNamespace()
		selector = map[string]string{"app": "istio-ingressgateway", "istio": shootNamespace}
		ex = &extensionsv1alpha1.Extension{ObjectMeta: metav1.ObjectMeta{Namespace: shootNamespace}}
	})

	It("selects the single gateway matching the plain selector", func() {
		createNewGateway(istioGatewayName, shootNamespace, selector)
		seedNamespace := createNewIstioNamespace()
		createNewIstioDeployment(seedNamespace, selector)

		namespace, labels, err := a.findIstioNamespaceForExtension(ctx, ex)

		Expect(err).NotTo(HaveOccurred())
		Expect(namespace).To(Equal(seedNamespace))
		Expect(labels).To(Equal(selector))
	})

	It("selects the seed gateway directly when the Gateway selector already carries istio-role=seed", func() {
		// Recent gardener: the Gateway selector itself carries the istio-role label,
		// so a single list resolves the seed gateway even though the virtual-garden
		// gateway shares the app/istio labels.
		seedSelector := withIstioRole(selector, istio.RoleSeed)
		createNewGateway(istioGatewayName, shootNamespace, seedSelector)
		seedNamespace := createNewIstioNamespace()
		gardenNamespace := createNewIstioNamespace()
		createNewIstioDeployment(seedNamespace, seedSelector)
		createNewIstioDeployment(gardenNamespace, withIstioRole(selector, istio.RoleGarden))

		namespace, labels, err := a.findIstioNamespaceForExtension(ctx, ex)

		Expect(err).NotTo(HaveOccurred())
		Expect(namespace).To(Equal(seedNamespace))
		Expect(labels).To(Equal(seedSelector))
	})

	It("narrows to the seed gateway when the plain selector matches both seed and virtual-garden gateways", func() {
		// Older gardener: the Gateway selector does not carry istio-role, so the plain
		// selector matches both the seed and the virtual-garden gateway Deployments.
		// The function must then narrow by istio-role=seed rather than error.
		createNewGateway(istioGatewayName, shootNamespace, selector)
		seedNamespace := createNewIstioNamespace()
		gardenNamespace := createNewIstioNamespace()
		createNewIstioDeployment(seedNamespace, withIstioRole(selector, istio.RoleSeed))
		createNewIstioDeployment(gardenNamespace, withIstioRole(selector, istio.RoleGarden))

		namespace, labels, err := a.findIstioNamespaceForExtension(ctx, ex)

		Expect(err).NotTo(HaveOccurred())
		Expect(namespace).To(Equal(seedNamespace))
		Expect(labels).To(Equal(selector))
	})

	It("fails when several matching gateways cannot be disambiguated by istio-role", func() {
		createNewGateway(istioGatewayName, shootNamespace, selector)
		createNewIstioDeployment(createNewIstioNamespace(), selector)
		createNewIstioDeployment(createNewIstioNamespace(), selector)

		_, _, err := a.findIstioNamespaceForExtension(ctx, ex)

		Expect(err).To(MatchError(ContainSubstring("number of deployments found is 2")))
	})
})

func withIstioRole(labels map[string]string, role string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		out[k] = v
	}
	out[istio.RoleKey] = role
	return out
}
