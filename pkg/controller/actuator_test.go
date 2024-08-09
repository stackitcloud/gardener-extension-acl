package controller

import (
	"encoding/json"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istionetworkingClientGo "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istionetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"github.com/stackitcloud/gardener-extension-acl/pkg/extensionspec"
)

var _ = Describe("actuator test", func() {
	var (
		a                                                *actuator
		shootNamespace1, shootNamespace2                 string
		istioNamespace1, istioNamespace2                 string
		istioNamespace1Selector, istioNamespace2Selector map[string]string
	)

	BeforeEach(func() {
		shootNamespace1 = createNewShootNamespace()
		istioNamespace1 = createNewIstioNamespace()
		istioNamespace1Selector = map[string]string{
			"app":   "istio-ingressgateway",
			"istio": istioNamespace1,
		}

		createNewEnvoyFilter(shootNamespace1, istioNamespace1)
		createNewGateway("kube-apiserver", shootNamespace1, istioNamespace1Selector)
		createNewIstioDeployment(istioNamespace1, istioNamespace1Selector)
		createNewCluster(shootNamespace1)
		createNewInfrastructure(shootNamespace1)

		a = getNewActuator()
	})

	AfterEach(func() {
		deleteNamespace(shootNamespace1)
		deleteNamespace(istioNamespace1)
	})

	Describe("reconciliation of an ACL extension object", func() {
		It("should create a legacy acl-vpn EnvoyFilter object with the correct contents", func() {
			extSpec := extensionspec.ExtensionSpec{
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
		It("should create managed resource containing acl-api-shoot and acl-vpn-shoot EnvoyFilter object", func() {
			extSpec := extensionspec.ExtensionSpec{
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
			Expect(secret.Data["seed"]).To(ContainSubstring("acl-api-" + shootNamespace1))
			Expect(secret.Data["seed"]).To(ContainSubstring("acl-vpn-" + shootNamespace1))
		})

		It("should record the last seen istio namespace in the status of the extension object", func() {
			// arrange
			extSpec := extensionspec.ExtensionSpec{
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

			// act
			Expect(a.Reconcile(ctx, logger, ext)).To(Succeed())

			// assert
			ext = &extensionsv1alpha1.Extension{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: shootNamespace1, Name: "acl"}, ext)).To(Succeed())

			extState := &ExtensionState{}
			Expect(ext.Status.State).ToNot(BeNil())
			Expect(ext.Status.State.Raw).ToNot(BeNil())
			Expect(json.Unmarshal(ext.Status.State.Raw, &extState)).To(Succeed())

			Expect(extState.IstioNamespace).ToNot(BeNil())
			Expect(*extState.IstioNamespace).To(Equal(istioNamespace1))
		})

		// gardener >= v1.89, including https://github.com/gardener/gardener/pull/9038
		Context("ingress-nginx is exposed via istio", func() {
			BeforeEach(func() {
				gateway := createNewGateway("nginx-ingress-controller", "garden", map[string]string{
					"app":   "istio-ingressgateway",
					"istio": "ingressgateway",
				})

				DeferCleanup(func() {
					Expect(k8sClient.Delete(ctx, gateway)).To(Or(Succeed(), BeNotFoundError()))
				})
			})

			It("should create managed resource including acl-ingress-shoot EnvoyFilter object", func() {
				extSpec := extensionspec.ExtensionSpec{
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
				Expect(secret.Data["seed"]).To(ContainSubstring("acl-ingress-" + shootNamespace1))
			})
		})

		// gardener < v1.89
		Context("ingress-nginx is not exposed via istio", func() {
			It("should create managed resource not including acl-ingress-shoot EnvoyFilter object", func() {
				extSpec := extensionspec.ExtensionSpec{
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
				Expect(secret.Data["seed"]).NotTo(ContainSubstring("acl-ingress-" + shootNamespace1))
			})
		})
	})

	Describe("reconciliation of an extension object with other ACL extensions being present", func() {
		BeforeEach(func() {
			shootNamespace2 = createNewShootNamespace()
			createNewEnvoyFilter(shootNamespace2, istioNamespace1)
			createNewGateway("kube-apiserver", shootNamespace2, istioNamespace1Selector)
			createNewCluster(shootNamespace2)
			createNewInfrastructure(shootNamespace2)
		})

		AfterEach(func() {
			deleteNamespace(shootNamespace2)
		})

		It("should create a legacy acl-vpn EnvoyFilter object with the correct contents (from both extensions)", func() {
			// arrange
			extSpec1 := extensionspec.ExtensionSpec{
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

			extSpec2 := extensionspec.ExtensionSpec{
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

			// act (shouldn't matter which extension is reconciled)
			Expect(a.Reconcile(ctx, logger, ext1)).To(Succeed())

			// assert
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

		It("should migrate from a legacy acl-vpn EnvoyFilter to shoot specific EnvoyFilters", func() {
			By("should create a legacy acl-vpn EnvoyFilter and a ManagedResource containing shoot specific EnvoyFilters")

			extSpec1 := extensionspec.ExtensionSpec{
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

			extSpec2 := extensionspec.ExtensionSpec{
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

			Expect(a.Reconcile(ctx, logger, ext1)).To(Succeed())
			Expect(a.Reconcile(ctx, logger, ext2)).To(Succeed())

			envoyFilter := &istionetworkingClientGo.EnvoyFilter{}
			Expect(k8sClient.Get(
				ctx, types.NamespacedName{
					Name:      "acl-vpn",
					Namespace: istioNamespace1,
				},
				envoyFilter,
			)).To(Succeed())

			Expect(envoyFilter.Spec.MarshalJSON()).To(And(
				ContainSubstring("1.2.3.4"),
				ContainSubstring("5.6.7.8"),
			))

			mr := &v1alpha1.ManagedResource{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ResourceNameSeed, Namespace: shootNamespace1}, mr)).To(Succeed())
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mr.Spec.SecretRefs[0].Name, Namespace: shootNamespace1}, secret)).To(Succeed())
			Expect(secret.Data["seed"]).To(ContainSubstring("acl-vpn-" + shootNamespace1))

			mr = &v1alpha1.ManagedResource{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ResourceNameSeed, Namespace: shootNamespace2}, mr)).To(Succeed())
			secret = &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mr.Spec.SecretRefs[0].Name, Namespace: shootNamespace2}, secret)).To(Succeed())
			Expect(secret.Data["seed"]).To(ContainSubstring("acl-vpn-" + shootNamespace2))

			By("should delete shoot specific rules after reconcile")

			shootEnvoyFilter1 := &istionetworkingClientGo.EnvoyFilter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "acl-vpn-" + shootNamespace1,
					Namespace: istioNamespace1,
				},
			}
			Expect(k8sClient.Create(ctx, shootEnvoyFilter1)).To(Succeed())

			Expect(a.Reconcile(ctx, logger, ext1)).To(Succeed())
			envoyFilter = &istionetworkingClientGo.EnvoyFilter{}
			Expect(k8sClient.Get(
				ctx, types.NamespacedName{
					Name:      "acl-vpn",
					Namespace: istioNamespace1,
				},
				envoyFilter,
			)).To(Succeed())

			Expect(envoyFilter.Spec.MarshalJSON()).To(And(
				Not(ContainSubstring("1.2.3.4")),
				ContainSubstring("5.6.7.8"),
			))

			By("should delete legacy envoy filter after all shoot specific filters are present")

			shootEnvoyFilter2 := &istionetworkingClientGo.EnvoyFilter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "acl-vpn-" + shootNamespace2,
					Namespace: istioNamespace1,
				},
			}
			Expect(k8sClient.Create(ctx, shootEnvoyFilter2)).To(Succeed())

			Expect(a.Reconcile(ctx, logger, ext1)).To(Succeed())

			envoyFilter = &istionetworkingClientGo.EnvoyFilter{}
			err = k8sClient.Get(
				ctx, types.NamespacedName{
					Name:      "acl-vpn",
					Namespace: istioNamespace1,
				},
				envoyFilter,
			)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("should not fail when the Gateway resource can't be found for an extension other than the one being reconciled (e.g. for hibernated clusters)", func() {
			// arrange
			extSpec1 := extensionspec.ExtensionSpec{
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

			// contents of the seconds extension don't matter, it just needs to exist
			ext2 := createNewExtension(shootNamespace2, []byte("{}"))
			Expect(ext2).To(Not(BeNil()))

			// simulate a hibernated cluster by deleting the Gateway object
			gw2 := &istionetworkingv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kube-apiserver",
					Namespace: shootNamespace2,
				},
			}
			Expect(k8sClient.Delete(ctx, gw2)).To(Succeed())

			// act (reconcile the existing extension)
			Expect(a.Reconcile(ctx, logger, ext1)).To(Succeed())

			// assert
			envoyFilter := &istionetworkingClientGo.EnvoyFilter{}
			Expect(k8sClient.Get(
				ctx, types.NamespacedName{
					Name:      "acl-vpn",
					Namespace: istioNamespace1,
				},
				envoyFilter,
			)).To(Succeed())

			// we expect the filter to contain the settings from the first extension
			Expect(envoyFilter.Spec.MarshalJSON()).To(And(
				ContainSubstring("1.2.3.4"),
			))

		})
	})

	Describe("a shoot switching the istio namespace (e.g. when being migrated to HA)", func() {
		It("should modify the EnvoyFilter objects accordingly", func() {
			By("1) creating the EnvoyFilter object correctly in the ORIGINAL namespace")
			// arrange
			extSpec := extensionspec.ExtensionSpec{
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

			// act
			Expect(a.Reconcile(ctx, logger, ext)).To(Succeed())

			// assert
			mr := &v1alpha1.ManagedResource{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ResourceNameSeed, Namespace: shootNamespace1}, mr)).To(Succeed())
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mr.Spec.SecretRefs[0].Name, Namespace: shootNamespace1}, secret)).To(Succeed())
			Expect(secret.Data["seed"]).To(ContainSubstring("1.2.3.4"))
			Expect(secret.Data["seed"]).To(ContainSubstring(istioNamespace1))
			Expect(secret.Data["seed"]).To(ContainSubstring("acl-vpn-" + shootNamespace1))

			// check legacy envoyfilter
			envoyFilter := &istionetworkingClientGo.EnvoyFilter{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "acl-vpn", Namespace: istioNamespace1}, envoyFilter)).To(Succeed())
			Expect(envoyFilter.Spec.MarshalJSON()).To(ContainSubstring("1.2.3.4"))

			By("2) allowing for the shoot to switch to a different Istio namespace")
			istioNamespace2 = createNewIstioNamespace()
			istioNamespace2Selector = map[string]string{
				"app":   "istio-ingressgateway",
				"istio": istioNamespace2,
			}

			// switching the istio namespace by recreating the gateway in the
			// shoot namespace with a new selector and creating a new
			// EnvoyFilter for the shoot and Istio Deployment in the second
			// istio namespace
			gw := &istionetworkingv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kube-apiserver",
					Namespace: shootNamespace1,
				},
			}
			Expect(k8sClient.Delete(ctx, gw)).To(Succeed())
			createNewGateway("kube-apiserver", shootNamespace1, istioNamespace2Selector)
			createNewIstioDeployment(istioNamespace2, istioNamespace2Selector)
			createNewEnvoyFilter(shootNamespace1, istioNamespace2)

			By("3) creating the EnvoyFilter object correctly in the NEW namespace")
			// arrange
			ext = &extensionsv1alpha1.Extension{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: shootNamespace1, Name: "acl"}, ext)).To(Succeed())

			// act
			Expect(a.Reconcile(ctx, logger, ext)).To(Succeed())

			// assert
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ResourceNameSeed, Namespace: shootNamespace1}, mr)).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mr.Spec.SecretRefs[0].Name, Namespace: shootNamespace1}, secret)).To(Succeed())
			Expect(secret.Data["seed"]).To(ContainSubstring(istioNamespace2))
			Expect(secret.Data["seed"]).To(ContainSubstring("acl-vpn-" + shootNamespace1))

			// check legacy envoyfilter
			envoyFilter = &istionetworkingClientGo.EnvoyFilter{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "acl-vpn", Namespace: istioNamespace2}, envoyFilter)).To(Succeed())
			Expect(envoyFilter.Spec.MarshalJSON()).To(ContainSubstring("1.2.3.4"))

			By("4) should have removed the EnvoyFilter object in the ORIGINAL namespace")
			Expect(secret.Data["seed"]).NotTo(ContainSubstring(istioNamespace1))
			// check legacy envoyfilter
			envoyFilter = &istionetworkingClientGo.EnvoyFilter{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "acl-vpn", Namespace: istioNamespace1}, envoyFilter)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("deletion of a hibernated cluster (no Gateway resource exists)", func() {
		It("should properly clean up according ManagedResource", func() {
			// arrange
			extSpec := extensionspec.ExtensionSpec{
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

			// simulate a hibernated cluster by deleting the Gateway object
			gw := &istionetworkingv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kube-apiserver",
					Namespace: shootNamespace1,
				},
			}

			Expect(k8sClient.Delete(ctx, gw)).To(Succeed())

			// act
			Expect(a.Delete(ctx, logger, ext)).To(Succeed())

			// assert
			mr = &v1alpha1.ManagedResource{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ResourceNameSeed, Namespace: shootNamespace1}, mr)
			Expect(err).ToNot(BeNil())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

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
		istioNamespace = createNewIstioNamespace()
		istioNamespaceSelector = map[string]string{
			"app":   "istio-ingressgateway",
			"istio": istioNamespace,
		}

		createNewGateway("kube-apiserver", namespace, istioNamespaceSelector)
		createNewIstioDeployment(istioNamespace, istioNamespaceSelector)
		createNewCluster(namespace)
		createNewInfrastructure(namespace)

		a = getNewActuator()
	})

	AfterEach(func() {
		deleteNamespace(namespace)
	})

	// TODO: test case can be removed together with the migration code in the
	// triggerWebhook() function, the test only checks that the deprecated hash
	// annotation is properly removed
	Describe("triggerWebhook", func() {
		When("the envoyfilter has a hash annotation", func() {
			It("Should remove the hash annotation", func() {
				envoyFilter := &istionetworkingClientGo.EnvoyFilter{
					ObjectMeta: metav1.ObjectMeta{
						Name:      namespace,
						Namespace: istioNamespace,
						Annotations: map[string]string{
							"acl-ext-rule-hash": "should-be-removed",
						},
					},
				}
				Expect(k8sClient.Create(ctx, envoyFilter)).To(Succeed())

				Expect(a.triggerWebhook(ctx, namespace, istioNamespace)).To(Succeed())
				Expect(k8sClient.Get(
					ctx, types.NamespacedName{
						Name:      envoyFilter.Name,
						Namespace: envoyFilter.Namespace,
					},
					envoyFilter,
				)).To(Succeed())

				Expect(envoyFilter.Annotations).ToNot(HaveKey("acl-ext-rule-hash"))
			})
		})
	})

	Describe("ValidateExtensionSpec", func() {
		When("there is an extension resource with one valid rule", func() {
			It("Should not return an error", func() {
				extSpec := &extensionspec.ExtensionSpec{}
				addRuleToSpec(extSpec, "DENY", "source_ip", "0.0.0.0/0")

				Expect(ValidateExtensionSpec(extSpec)).To(Succeed())
			})
		})

		When("there is an extension resource without rules", func() {
			It("Should return an error", func() {
				extSpec := &extensionspec.ExtensionSpec{}
				Expect(ValidateExtensionSpec(extSpec)).To(Equal(ErrSpecRule))
			})
		})

		When("there is an extension resource with a rule with invalid rule type", func() {
			It("Should return the correct error", func() {
				extSpec := &extensionspec.ExtensionSpec{}
				addRuleToSpec(extSpec, "DENY", "nonexistent", "0.0.0.0/0")

				Expect(ValidateExtensionSpec(extSpec)).To(Equal(ErrSpecType))
			})
		})

		When("there is an extension resource with a rule with invalid rule action", func() {
			It("Should return the correct error", func() {
				extSpec := &extensionspec.ExtensionSpec{}
				addRuleToSpec(extSpec, "NONEXISTENT", "remote_ip", "0.0.0.0/0")

				Expect(ValidateExtensionSpec(extSpec)).To(Equal(ErrSpecAction))
			})
		})

		When("there is an extension resource with CIDR", func() {
			It("Should return the correct error", func() {
				extSpec := &extensionspec.ExtensionSpec{}
				addRuleToSpec(extSpec, "DENY", "remote_ip", "n0n3x1st3/nt")

				// we're not testing for a specific error, as they come from the
				// net package here - no need for us to test these
				Expect(ValidateExtensionSpec(extSpec)).ToNot(Succeed())
			})
		})

		When("there is an extension resource with a rule without CIDR", func() {
			It("Should return the correct error", func() {
				extSpec := &extensionspec.ExtensionSpec{}

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
				extSpec := &extensionspec.ExtensionSpec{}
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
			ChartPath:              "../../charts",
			MigrateLegacyVPNFilter: true,
		},
	}
}

func addRuleToSpec(extSpec *extensionspec.ExtensionSpec, action, ruleType, cidr string) {
	extSpec.Rule = &envoyfilters.ACLRule{
		Cidrs: []string{
			cidr,
		},
		Action: action,
		Type:   ruleType,
	}
}
