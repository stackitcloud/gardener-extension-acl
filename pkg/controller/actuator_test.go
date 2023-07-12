package controller

import (
	"encoding/json"

	openstackv1alpha1 "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/v1alpha1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	istionetworkingClientGo "istio.io/client-go/pkg/apis/networking/v1alpha3"
)

var _ = Describe("actuator unit test", func() {
	var (
		a           *actuator
		ext         *extensionsv1alpha1.Extension
		cluster     *extensionsv1alpha1.Cluster
		namespace   string
		envoyFilter *istionetworkingClientGo.EnvoyFilter
	)

	BeforeEach(func() {
		namespace = createNewNamespace()
		ext = getNewExtension(namespace)
		cluster = getNewCluster(namespace)
		a = getNewActuator()

		Expect(k8sClient.Create(ctx, ext)).To(Succeed())
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
	})

	AfterEach(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ext))).To(Succeed())
		deleteNamespace(namespace)
	})

	Describe("updateEnvoyFilterHash", func() {
		When("there is an extension resource with one rule", func() {
			shootName := GetUniqueShootName()
			BeforeEach(func() {
				envoyFilter = &istionetworkingClientGo.EnvoyFilter{
					ObjectMeta: metav1.ObjectMeta{
						Name:      shootName,
						Namespace: IngressNamespace,
					},
				}

				Expect(k8sClient.Create(ctx, envoyFilter)).To(Succeed())
			})
			It("Should add an anotation with a hash", func() {
				extSpec := getExtensionSpec()
				addRuleToSpec(extSpec, "DENY", "source_ip", "0.0.0.0/0")

				Expect(a.updateEnvoyFilterHash(ctx, shootName, extSpec, false)).To(Succeed())
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
			shootName := GetUniqueShootName()
			BeforeEach(func() {
				envoyFilter = &istionetworkingClientGo.EnvoyFilter{
					ObjectMeta: metav1.ObjectMeta{
						Name:      shootName,
						Namespace: IngressNamespace,
						Annotations: map[string]string{
							HashAnnotationName: "this-should-be-removed",
						},
					},
				}

				Expect(k8sClient.Create(ctx, envoyFilter)).To(Succeed())
			})
			It("Should remove the hash annotation", func() {
				Expect(a.updateEnvoyFilterHash(ctx, shootName, nil, true)).To(Succeed())
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
				extSpec := getExtensionSpec()
				addRuleToSpec(extSpec, "DENY", "source_ip", "0.0.0.0/0")

				Expect(ValidateExtensionSpec(extSpec)).To(Succeed())
			})
		})

		When("there is an extension resource without rules", func() {
			It("Should return an error", func() {
				extSpec := getExtensionSpec()
				Expect(ValidateExtensionSpec(extSpec)).To(Equal(ErrSpecRule))
			})
		})

		When("there is an extension resource with a rule with invalid rule type", func() {
			It("Should return the correct error", func() {
				extSpec := getExtensionSpec()
				addRuleToSpec(extSpec, "DENY", "nonexistent", "0.0.0.0/0")

				Expect(ValidateExtensionSpec(extSpec)).To(Equal(ErrSpecType))
			})
		})

		When("there is an extension resource with a rule with invalid rule action", func() {
			It("Should return the correct error", func() {
				extSpec := getExtensionSpec()
				addRuleToSpec(extSpec, "NONEXISTENT", "remote_ip", "0.0.0.0/0")

				Expect(ValidateExtensionSpec(extSpec)).To(Equal(ErrSpecAction))
			})
		})

		When("there is an extension resource with CIDR", func() {
			It("Should return the correct error", func() {
				extSpec := getExtensionSpec()
				addRuleToSpec(extSpec, "DENY", "remote_ip", "n0n3x1st3/nt")

				// we're not testing for a specific error, as they come from the
				// net package here - no need for us to test these
				Expect(ValidateExtensionSpec(extSpec)).ToNot(Succeed())
			})
		})

		When("there is an extension resource with a rule without CIDR", func() {
			It("Should return the correct error", func() {
				extSpec := getExtensionSpec()

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
				extSpec := getExtensionSpec()
				addRuleToSpec(extSpec, "DENY", "remote_ip", "n0n3x1st3/nt")

				// we're not testing for a specific error, as they come from the
				// net package here - no need for us to test these
				Expect(ValidateExtensionSpec(extSpec)).ToNot(Succeed())
			})
		})
	})

	Describe("ConfigureProviderSpecificAllowedCIDRs", func() {
		When("there is an infrastructure object for which no custom behavior is required", func() {
			It("Should leave the alwaysAllowedCIDRs slice unaltered and should not error", func() {
				infra := &extensionsv1alpha1.Infrastructure{
					Spec: extensionsv1alpha1.InfrastructureSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							Type: "non-existent",
						},
					},
				}

				alwaysAllowedCIDRs := []string{"a", "b"}

				Expect(ConfigureProviderSpecificAllowedCIDRs(infra, &alwaysAllowedCIDRs)).To(Succeed())
				Expect(alwaysAllowedCIDRs).To(Equal([]string{"a", "b"}))
			})
		})
		When("there is an infrastructure object of type 'openstack'", func() {
			It("Should add the router IP to the alwaysAllowedCIDRs slice and should not error", func() {
				infraStatusJSON, err := json.Marshal(&openstackv1alpha1.InfrastructureStatus{
					TypeMeta: metav1.TypeMeta{
						Kind:       "InfrastructureStatus",
						APIVersion: "openstack.provider.extensions.gardener.cloud/v1alpha1",
					},
					Networks: openstackv1alpha1.NetworkStatus{
						Router: openstackv1alpha1.RouterStatus{
							ID: "router-id",
							IP: "10.9.8.7",
						},
					},
				})
				Expect(err).To(BeNil())

				infra := &extensionsv1alpha1.Infrastructure{
					Spec: extensionsv1alpha1.InfrastructureSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							Type: OpenstackTypeName,
						},
					},
					Status: extensionsv1alpha1.InfrastructureStatus{
						DefaultStatus: extensionsv1alpha1.DefaultStatus{
							ProviderStatus: &runtime.RawExtension{
								Raw: infraStatusJSON,
							},
						},
					},
				}

				alwaysAllowedCIDRs := []string{"a", "b"}

				Expect(ConfigureProviderSpecificAllowedCIDRs(infra, &alwaysAllowedCIDRs)).To(Succeed())
				Expect(alwaysAllowedCIDRs).To(Equal([]string{"a", "b", "10.9.8.7/32"}))
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

func getNewCluster(namespace string) *extensionsv1alpha1.Cluster {
	return &extensionsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
		Spec: extensionsv1alpha1.ClusterSpec{
			CloudProfile: runtime.RawExtension{Raw: []byte("{}")},
			Seed:         runtime.RawExtension{Raw: []byte("{}")},
			Shoot:        runtime.RawExtension{Raw: []byte("{}")},
		},
	}
}

func getExtensionSpec() *ExtensionSpec {
	return &ExtensionSpec{}
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
