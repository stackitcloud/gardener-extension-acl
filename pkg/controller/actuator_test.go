package controller

import (
	"encoding/json"

	"github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	istionetworkingClientGo "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
)

var _ = Describe("actuator test", func() {
	var (
		a                                *actuator
		shootNamespace1, shootNamespace2 string
		istioNamespace1                  string
		istioNamespace1Selector          map[string]string
	)

	a = getNewActuator()

	BeforeEach(func() {
		shootNamespace1 = createNewShootNamespace()
		istioNamespace1, istioNamespace1Selector = createNewIstioNamespace()

		createNewEnvoyFilter(shootNamespace1, istioNamespace1)
		createNewGateway(shootNamespace1, istioNamespace1Selector)
		createNewCluster(shootNamespace1)
		createNewInfrastructure(shootNamespace1)

		a = getNewActuator()
	})

	AfterEach(func() {
		deleteNamespace(shootNamespace1)
		deleteNamespace(istioNamespace1)
	})

	Describe("reconciliation of an ACL extension object", func() {
		It("should create an acl-vpn EnvoyFilter object with the correct contents", func() {
			extSpec := ExtensionSpec{
				Rule: &envoyfilters.ACLRule{
					Cidrs:  []string{"1.2.3.4/24"},
					Action: "ALLOW",
					Type:   "remote_ip",
				},
			}
			extSpecJSON, err := json.Marshal(extSpec)
			Expect(err).To(BeNil())
			ext := createNewExtension(shootNamespace1, extSpecJSON)
			Expect(ext).To(Not(BeNil()))

			Expect(a.Reconcile(ctx, logger, ext)).To(Succeed())

			envoyFilter := &istionetworkingClientGo.EnvoyFilter{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "acl-vpn", Namespace: istioNamespace1}, envoyFilter)).To(Succeed())
			Expect(envoyFilter.Spec.MarshalJSON()).To(ContainSubstring("1.2.3.4"))
		})

		It("should create managed resource for acl-api-shoot EnvoyFilter object", func() {
			extSpec := ExtensionSpec{
				Rule: &envoyfilters.ACLRule{
					Cidrs:  []string{"1.2.3.4/24"},
					Action: "ALLOW",
					Type:   "remote_ip",
				},
			}
			extSpecJSON, err := json.Marshal(extSpec)
			Expect(err).To(BeNil())
			ext := createNewExtension(shootNamespace1, extSpecJSON)
			Expect(ext).To(Not(BeNil()))

			Expect(a.Reconcile(ctx, logger, ext)).To(Succeed())

			mr := &v1alpha1.ManagedResource{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ResourceNameSeed, Namespace: shootNamespace1}, mr)).To(Succeed())
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mr.Spec.SecretRefs[0].Name, Namespace: shootNamespace1}, secret)).To(Succeed())
			Expect(secret.Data["seed"]).To(ContainSubstring("1.2.3.4"))
		})
	})

	Describe("reconciliation of an extension object with other ACL extensions being present", func() {
		BeforeEach(func() {
			shootNamespace2 = createNewShootNamespace()
			createNewEnvoyFilter(shootNamespace2, istioNamespace1)
			createNewGateway(shootNamespace2, istioNamespace1Selector)
			createNewCluster(shootNamespace2)
			createNewInfrastructure(shootNamespace2)
		})

		AfterEach(func() {
			deleteNamespace(shootNamespace2)
		})

		It("should create an acl-vpn EnvoyFilter object with the correct contents", func() {
			extSpec1 := ExtensionSpec{
				Rule: &envoyfilters.ACLRule{
					Cidrs:  []string{"1.2.3.4/24"},
					Action: "ALLOW",
					Type:   "remote_ip",
				},
			}
			extSpecJSON1, err := json.Marshal(extSpec1)
			Expect(err).To(BeNil())
			ext1 := createNewExtension(shootNamespace1, extSpecJSON1)
			Expect(ext1).To(Not(BeNil()))

			extSpec2 := ExtensionSpec{
				Rule: &envoyfilters.ACLRule{
					Cidrs:  []string{"5.6.7.8/24"},
					Action: "ALLOW",
					Type:   "remote_ip",
				},
			}
			extSpecJSON2, err := json.Marshal(extSpec2)
			Expect(err).To(BeNil())
			ext2 := createNewExtension(shootNamespace2, extSpecJSON2)
			Expect(ext2).To(Not(BeNil()))

			// reconcile either of the two extensions
			Expect(a.Reconcile(ctx, logger, ext1)).To(Succeed())

			envoyFilter := &istionetworkingClientGo.EnvoyFilter{}
			Expect(k8sClient.Get(
				ctx, types.NamespacedName{
					Name:      "acl-vpn",
					Namespace: istioNamespace1,
				},
				envoyFilter,
			)).To(Succeed())

			// as there is only one acl-vpn EnvoyFilter, this EnvoyFilter must
			// contain the allowed CIDRs from all ACL extensions
			Expect(envoyFilter.Spec.MarshalJSON()).To(And(
				ContainSubstring("1.2.3.4"),
				ContainSubstring("5.6.7.8"),
			))
		})
	})
})

var _ = Describe("actuator unit test", func() {
	var (
		a              *actuator
		namespace      string
		istioNamespace string
	)

	BeforeEach(func() {
		var istioNamespaceSelector map[string]string

		namespace = createNewShootNamespace()
		istioNamespace, istioNamespaceSelector = createNewIstioNamespace()

		createNewGateway(namespace, istioNamespaceSelector)
		createNewCluster(namespace)
		createNewInfrastructure(namespace)
		namespace = createNewShootNamespace()

		a = getNewActuator()
	})

	AfterEach(func() {
		deleteNamespace(namespace)
	})

	Describe("updateEnvoyFilterHash", func() {
		When("there is an extension resource with one rule", func() {
			It("Should add an anotation with a hash", func() {
				createNewEnvoyFilter(namespace, istioNamespace)
				envoyFilter := &istionetworkingClientGo.EnvoyFilter{
					ObjectMeta: metav1.ObjectMeta{Name: namespace, Namespace: istioNamespace},
				}
				extSpec := &ExtensionSpec{}
				addRuleToSpec(extSpec, "DENY", "source_ip", "0.0.0.0/0")

				Expect(a.updateEnvoyFilterHash(ctx, namespace, extSpec, istioNamespace, false)).To(Succeed())
				Expect(k8sClient.Get(
					ctx, types.NamespacedName{
						Name:      envoyFilter.Name,
						Namespace: envoyFilter.Namespace,
					},
					envoyFilter,
				)).To(Succeed())

				Expect(envoyFilter.Annotations).ToNot(BeNil())
			})
		})

		When("the extension resource is being deleted, and the envoyfilter has an annotation", func() {
			It("Should remove the hash annotation", func() {
				createNewEnvoyFilter(namespace, istioNamespace)
				envoyFilter := &istionetworkingClientGo.EnvoyFilter{
					ObjectMeta: metav1.ObjectMeta{Name: namespace, Namespace: istioNamespace},
				}
				Expect(k8sClient.Get(
					ctx, types.NamespacedName{
						Name:      envoyFilter.Name,
						Namespace: envoyFilter.Namespace,
					},
					envoyFilter,
				)).To(Succeed())

				envoyFilter.Annotations = map[string]string{
					HashAnnotationName: "should-be-removed",
				}

				Expect(a.updateEnvoyFilterHash(ctx, namespace, nil, istioNamespace, true)).To(Succeed())
				Expect(k8sClient.Get(
					ctx, types.NamespacedName{
						Name:      envoyFilter.Name,
						Namespace: envoyFilter.Namespace,
					},
					envoyFilter,
				)).To(Succeed())

				_, ok := envoyFilter.Annotations[HashAnnotationName]
				Expect(ok).To(BeFalse())
			})
		})
	})

	Describe("ValidateExtensionSpec", func() {
		When("there is an extension resource with one valid rule", func() {
			It("Should not return an error", func() {
				extSpec := &ExtensionSpec{}
				addRuleToSpec(extSpec, "DENY", "source_ip", "0.0.0.0/0")

				Expect(ValidateExtensionSpec(extSpec)).To(Succeed())
			})
		})

		When("there is an extension resource without rules", func() {
			It("Should return an error", func() {
				extSpec := &ExtensionSpec{}
				Expect(ValidateExtensionSpec(extSpec)).To(Equal(ErrSpecRule))
			})
		})

		When("there is an extension resource with a rule with invalid rule type", func() {
			It("Should return the correct error", func() {
				extSpec := &ExtensionSpec{}
				addRuleToSpec(extSpec, "DENY", "nonexistent", "0.0.0.0/0")

				Expect(ValidateExtensionSpec(extSpec)).To(Equal(ErrSpecType))
			})
		})

		When("there is an extension resource with a rule with invalid rule action", func() {
			It("Should return the correct error", func() {
				extSpec := &ExtensionSpec{}
				addRuleToSpec(extSpec, "NONEXISTENT", "remote_ip", "0.0.0.0/0")

				Expect(ValidateExtensionSpec(extSpec)).To(Equal(ErrSpecAction))
			})
		})

		When("there is an extension resource with CIDR", func() {
			It("Should return the correct error", func() {
				extSpec := &ExtensionSpec{}
				addRuleToSpec(extSpec, "DENY", "remote_ip", "n0n3x1st3/nt")

				// we're not testing for a specific error, as they come from the
				// net package here - no need for us to test these
				Expect(ValidateExtensionSpec(extSpec)).ToNot(Succeed())
			})
		})

		When("there is an extension resource with a rule without CIDR", func() {
			It("Should return the correct error", func() {
				extSpec := &ExtensionSpec{}

				extSpec.Rule = &envoyfilters.ACLRule{
					Action: "DENY",
					Type:   "remote_ip",
				}

				// we're not testing for a specific error, as they come from the
				// net package here - no need for us to test these
				Expect(ValidateExtensionSpec(extSpec)).To(Equal(ErrSpecCIDR))
			})
		})

		When("there is an extension resource with a rule with invalid CIDR", func() {
			It("Should return the correct error", func() {
				extSpec := &ExtensionSpec{}
				addRuleToSpec(extSpec, "DENY", "remote_ip", "n0n3x1st3/nt")

				// we're not testing for a specific error, as they come from the
				// net package here - no need for us to test these
				Expect(ValidateExtensionSpec(extSpec)).ToNot(Succeed())
			})
		})
	})
})

func getNewActuator() *actuator {
	return &actuator{
		client: k8sClient,
		config: cfg,
		extensionConfig: config.Config{
			ChartPath: "../../charts",
		},
	}
}

func addRuleToSpec(extSpec *ExtensionSpec, action, ruleType, cidr string) {
	extSpec.Rule = &envoyfilters.ACLRule{
		Cidrs: []string{
			cidr,
		},
		Action: action,
		Type:   ruleType,
	}
}
