package controller

import (
	"encoding/json"
	"slices"

	"github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"github.com/stackitcloud/gardener-extension-acl/pkg/extensionspec"
)

var _ = Describe("actuator garden test", func() {
	var (
		a                      *actuator
		istioNamespace         string
		istioNamespaceSelector map[string]string
	)

	BeforeEach(func() {
		istioNamespace = createNewIstioNamespace()
		istioNamespaceSelector = map[string]string{
			"app":   "istio-ingressgateway",
			"istio": istioNamespace,
		}

		createNewGateway("virtual-garden-kube-apiserver", constants.GardenNamespace, istioNamespaceSelector)
		createNewIstioDeployment(istioNamespace, istioNamespaceSelector)
		createNewGarden()

		a = getNewActuator()
		a.gardenClient = k8sClient
	})

	AfterEach(func() {
		deleteNamespace(istioNamespace)
	})

	Describe("reconciliation of an ACL extension object", func() {
		It("should create managed resource containing acl-api-garden EnvoyFilter object", func() {
			extSpec := extensionspec.ExtensionSpec{
				Rule: &envoyfilters.ACLRule{
					Cidrs:  []string{"1.2.3.4/24"},
					Action: "ALLOW",
					Type:   "remote_ip",
				},
			}
			extSpecJSON, err := json.Marshal(extSpec)
			Expect(err).NotTo(HaveOccurred())
			ext := createNewExtension(constants.GardenNamespace, extSpecJSON, extensionsv1alpha1.ExtensionClassGarden)
			Expect(ext).To(Not(BeNil()))

			Expect(a.Reconcile(ctx, logger, ext)).To(Succeed(), "actuator reconcile")

			objs, err := managedresources.GetObjects(ctx, a.client, constants.GardenNamespace, ResourceNameGarden)
			Expect(err).NotTo(HaveOccurred())
			idx := slices.IndexFunc(objs, func(obj client.Object) bool {
				filter, ok := obj.(*istionetworkingv1alpha3.EnvoyFilter)
				if !ok {
					return false
				}
				return filter.Name == "acl-api-garden"
			})
			Expect(idx).NotTo(Equal(-1), "envoy filter acl-api-garden not found")
			filter := objs[idx].(*istionetworkingv1alpha3.EnvoyFilter)
			configPatch := filter.Spec.ConfigPatches[0]
			patch, err := configPatch.Patch.Value.MarshalJSON()
			Expect(err).NotTo(HaveOccurred())
			Expect(patch).To(ContainSubstring("1.2.3.4"))
			Expect(patch).To(ContainSubstring(virtualGardenPodCIDR.Addr().String()))
			Expect(patch).To(ContainSubstring(virtualGardenNodeCIDR.Addr().String()))

			Expect(configPatch.Match.GetListener().FilterChain.Sni).To(Equal(virtualGardenURL))
		})
	})
})
