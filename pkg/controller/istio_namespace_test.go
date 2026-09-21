package controller

import (
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("seedIngressGatewaySelector", func() {
	It("adds the seed istio-role label to the gateway selector", func() {
		selector := map[string]string{"app": "istio-ingressgateway", "istio": "ingressgateway"}

		Expect(seedIngressGatewaySelector(selector)).To(Equal(map[string]string{
			"app":        "istio-ingressgateway",
			"istio":      "ingressgateway",
			"istio-role": "seed",
		}))
	})

	It("does not modify the original selector", func() {
		selector := map[string]string{"istio": "ingressgateway"}

		_ = seedIngressGatewaySelector(selector)

		Expect(selector).To(Equal(map[string]string{"istio": "ingressgateway"}))
	})

	It("overrides a foreign istio-role value", func() {
		Expect(seedIngressGatewaySelector(map[string]string{"istio-role": "garden"})).To(HaveKeyWithValue("istio-role", "seed"))
	})
})

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
		createNewGateway(istioGatewayName, shootNamespace, selector)
		ex = &extensionsv1alpha1.Extension{ObjectMeta: metav1.ObjectMeta{Namespace: shootNamespace}}
	})

	It("selects the istio-role=seed gateway when the virtual garden gateway matches the same selector", func() {
		seedNamespace := createNewIstioNamespace()
		gardenNamespace := createNewIstioNamespace()
		createNewIstioDeployment(seedNamespace, withLabel(selector, istioRoleLabelKey, istioRoleSeed))
		createNewIstioDeployment(gardenNamespace, withLabel(selector, istioRoleLabelKey, "garden"))

		namespace, labels, err := a.findIstioNamespaceForExtension(ctx, ex)

		Expect(err).NotTo(HaveOccurred())
		Expect(namespace).To(Equal(seedNamespace))
		Expect(labels).To(Equal(selector))
	})

	It("falls back to the plain selector when no gateway carries the istio-role label", func() {
		seedNamespace := createNewIstioNamespace()
		createNewIstioDeployment(seedNamespace, selector)

		namespace, _, err := a.findIstioNamespaceForExtension(ctx, ex)

		Expect(err).NotTo(HaveOccurred())
		Expect(namespace).To(Equal(seedNamespace))
	})

	It("still fails when several unlabelled gateways match", func() {
		createNewIstioDeployment(createNewIstioNamespace(), selector)
		createNewIstioDeployment(createNewIstioNamespace(), selector)

		_, _, err := a.findIstioNamespaceForExtension(ctx, ex)

		Expect(err).To(MatchError(ContainSubstring("number of deployments found is 2")))
	})
})

func withLabel(labels map[string]string, key, value string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		out[k] = v
	}
	out[key] = value
	return out
}
