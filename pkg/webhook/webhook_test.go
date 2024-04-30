package webhook

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"strings"

	openstackv1alpha1 "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/v1alpha1"
	"github.com/gardener/gardener-extension-provider-openstack/pkg/openstack"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	istionetworkingClientGo "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"github.com/stackitcloud/gardener-extension-acl/pkg/extensionspec"
)

var _ = Describe("webhook unit test", func() {
	var (
		e         *EnvoyFilterWebhook
		ext       *extensionsv1alpha1.Extension
		cluster   *extensionsv1alpha1.Cluster
		infra     *extensionsv1alpha1.Infrastructure
		namespace string
		name      string
	)

	BeforeEach(func() {
		name = "some-shoot"
		namespace = createNewNamespace()
		infra = getNewInfrastructure(namespace, name, "non-existent", []byte("{}"), []byte("{}"))
		Expect(k8sClient.Create(ctx, infra)).To(Succeed())

		// set up default shoot part of cluster resource
		shoot := &gardencorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: gardencorev1beta1.ShootSpec{
				Networking: &gardencorev1beta1.Networking{
					Nodes:    ptr.To("10.250.0.0/16"),
					Pods:     ptr.To("100.96.0.0/11"),
					Services: ptr.To("100.64.0.0/13"),
					Type:     ptr.To("calico"),
				},
				// we need a provider section with at least one worker pool,
				// otherwise the Shoot will be considered workerless
				Provider: gardencorev1beta1.Provider{
					Workers: []gardencorev1beta1.Worker{
						{
							Name: "workerpool",
						},
					},
				},
			},
		}

		// set up default seed part of cluster resource
		seed := &gardencorev1beta1.Seed{
			Spec: gardencorev1beta1.SeedSpec{
				Networks: gardencorev1beta1.SeedNetworks{
					Nodes:    ptr.To("100.250.0.0/16"),
					Pods:     "10.96.0.0/11",
					Services: "10.64.0.0/13",
				},
			},
		}

		cluster = getNewCluster(namespace, shoot, seed)
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		e = getNewWebhook()
	})

	AfterEach(func() {
		deleteNamespace(namespace)
	})

	Describe("createAdmissionResponse", func() {
		When("the name of the EnvoyFilter doesn't start with 'shoot-'", func() {
			It("issues no patch for the EnvoyFilter", func() {
				df, dfJSON := getEnvoyFilterFromFile("non-shoot-envoyfilter")

				ar := e.createAdmissionResponse(context.Background(), df, dfJSON)

				Expect(ar.Allowed).To(BeTrue())
				Expect(ar.Result.Message).To(ContainSubstring("not an EnvoyFilter managed by this webhook"))
				Expect(ar.Patch).To(BeEmpty())
			})
		})

		When("there is no extension", func() {
			When("the shoot uses the legacy technical ID format 'shoot-'", func() {
				It("issues no patch for the EnvoyFilter", func() {
					df, dfJSON := getEnvoyFilterFromFile("shoot-foo-bar")

					ar := e.createAdmissionResponse(context.Background(), df, dfJSON)

					Expect(ar.Allowed).To(BeTrue())
					Expect(ar.Result.Message).To(ContainSubstring("not enabled for shoot"))
					Expect(ar.Patch).To(BeEmpty())
				})
			})

			When("the shoot uses the current technical ID format 'shoot--'", func() {
				It("issues no patch for the EnvoyFilter", func() {
					df, dfJSON := getEnvoyFilterFromFile(namespace)

					ar := e.createAdmissionResponse(context.Background(), df, dfJSON)

					Expect(ar.Allowed).To(BeTrue())
					Expect(ar.Result.Message).To(ContainSubstring("not enabled for shoot"))
					Expect(ar.Patch).To(BeEmpty())
				})
			})
		})

		When("there is an extension resource with one DENY rule", func() {
			extSpec := getExtensionSpec()

			BeforeEach(func() {
				addRuleToSpec(extSpec, "DENY", "source_ip", "0.0.0.0/0")
				ext = getNewExtension(namespace, *extSpec)

				Expect(k8sClient.Create(ctx, ext)).To(Succeed())
			})

			AfterEach(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ext))).To(Succeed())
			})

			It("patches this rule into the filters object", func() {
				df, dfJSON := getEnvoyFilterFromFile(namespace)

				ar := e.createAdmissionResponse(context.Background(), df, dfJSON)

				Expect(ar.Allowed).To(BeTrue())

				expectedFilters := []map[string]interface{}{
					{
						"name": "acl-internal-source_ip",
						"typed_config": map[string]interface{}{
							"stat_prefix": "envoyrbac",
							"rules": map[string]interface{}{
								"policies": map[string]interface{}{
									"acl-internal": map[string]interface{}{
										"permissions": []map[string]interface{}{
											{
												"any": true,
											},
										},
										"principals": []map[string]interface{}{
											{
												"source_ip": map[string]interface{}{
													"address_prefix": "0.0.0.0",
													"prefix_len":     0,
												},
											},
										},
									},
								},
								"action": "DENY",
							},
							"@type": "type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC",
						},
					},

					{
						"name": "envoy.filters.network.tcp_proxy",
						"typed_config": map[string]interface{}{
							"@type":       "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy",
							"cluster":     "outbound|443||kube-apiserver." + namespace + ".svc.cluster.local",
							"stat_prefix": "outbound|443||kube-apiserver." + namespace + ".svc.cluster.local",
						},
					},
				}

				Expect(ar.Patches[0].Value).To(Equal(expectedFilters))
			})
		})

		When("there is an extension resource with one ALLOW rule", func() {
			extSpec := getExtensionSpec()

			BeforeEach(func() {
				addRuleToSpec(extSpec, "ALLOW", "source_ip", "0.0.0.0/0")
				ext = getNewExtension(namespace, *extSpec)

				Expect(k8sClient.Create(ctx, ext)).To(Succeed())
			})

			AfterEach(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ext))).To(Succeed())
			})

			It("patches this rule into the filters object, including CIDRs for Seed|Shoot nodes and pods", func() {
				df, dfJSON := getEnvoyFilterFromFile(namespace)

				ar := e.createAdmissionResponse(context.Background(), df, dfJSON)

				Expect(ar.Allowed).To(BeTrue())

				expectedFilters := []map[string]interface{}{
					{
						"name": "acl-internal-source_ip",
						"typed_config": map[string]interface{}{
							"stat_prefix": "envoyrbac",
							"rules": map[string]interface{}{
								"policies": map[string]interface{}{
									"acl-internal": map[string]interface{}{
										"permissions": []map[string]interface{}{
											{
												"any": true,
											},
										},
										"principals": []map[string]interface{}{
											{
												"source_ip": map[string]interface{}{
													"address_prefix": "0.0.0.0",
													"prefix_len":     0,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "100.250.0.0",
													"prefix_len":     16,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "10.96.0.0",
													"prefix_len":     11,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "10.250.0.0",
													"prefix_len":     16,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "100.96.0.0",
													"prefix_len":     11,
												},
											},
										},
									},
								},
								"action": "ALLOW",
							},
							"@type": "type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC",
						},
					},

					{
						"name": "envoy.filters.network.tcp_proxy",
						"typed_config": map[string]interface{}{
							"@type":       "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy",
							"cluster":     "outbound|443||kube-apiserver." + namespace + ".svc.cluster.local",
							"stat_prefix": "outbound|443||kube-apiserver." + namespace + ".svc.cluster.local",
						},
					},
				}

				Expect(ar.Patches[0].Value).To(Equal(expectedFilters))
			})
		})

		When("there is an extension resource with one ALLOW rule and infra is of type OpenStack", func() {
			extSpec := getExtensionSpec()

			BeforeEach(func() {
				addRuleToSpec(extSpec, "ALLOW", "source_ip", "0.0.0.0/0")
				ext = getNewExtension(namespace, *extSpec)

				Expect(k8sClient.Create(ctx, ext)).To(Succeed())

				infra.Spec.Type = openstack.Type
				Expect(k8sClient.Update(ctx, infra)).To(Succeed())

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

				infra.Status = extensionsv1alpha1.InfrastructureStatus{
					DefaultStatus: extensionsv1alpha1.DefaultStatus{
						ProviderStatus: &runtime.RawExtension{
							Raw: infraStatusJSON,
						},
					},
				}
				Expect(k8sClient.Status().Update(ctx, infra)).To(Succeed())
			})

			AfterEach(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ext))).To(Succeed())
			})

			It("patches this rule into the filters object, including CIDRs for Seed|Shoot nodes and pods and also the OpenStack router IP", func() {
				df, dfJSON := getEnvoyFilterFromFile(namespace)

				ar := e.createAdmissionResponse(context.Background(), df, dfJSON)

				Expect(ar.Allowed).To(BeTrue())

				expectedFilters := []map[string]interface{}{
					{
						"name": "acl-internal-source_ip",
						"typed_config": map[string]interface{}{
							"stat_prefix": "envoyrbac",
							"rules": map[string]interface{}{
								"policies": map[string]interface{}{
									"acl-internal": map[string]interface{}{
										"permissions": []map[string]interface{}{
											{
												"any": true,
											},
										},
										"principals": []map[string]interface{}{
											{
												"source_ip": map[string]interface{}{
													"address_prefix": "0.0.0.0",
													"prefix_len":     0,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "100.250.0.0",
													"prefix_len":     16,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "10.96.0.0",
													"prefix_len":     11,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "10.250.0.0",
													"prefix_len":     16,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "100.96.0.0",
													"prefix_len":     11,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "10.9.8.7",
													"prefix_len":     32,
												},
											},
										},
									},
								},
								"action": "ALLOW",
							},
							"@type": "type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC",
						},
					},

					{
						"name": "envoy.filters.network.tcp_proxy",
						"typed_config": map[string]interface{}{
							"@type":       "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy",
							"cluster":     "outbound|443||kube-apiserver." + namespace + ".svc.cluster.local",
							"stat_prefix": "outbound|443||kube-apiserver." + namespace + ".svc.cluster.local",
						},
					},
				}

				Expect(ar.Patches[0].Value).To(Equal(expectedFilters))
			})
		})

		When("the Shoot is workerless, and there is one allow rule", func() {
			extSpec := getExtensionSpec()

			BeforeEach(func() {
				addRuleToSpec(extSpec, "ALLOW", "source_ip", "0.0.0.0/0")
				ext = getNewExtension(namespace, *extSpec)
				Expect(k8sClient.Create(ctx, ext)).To(Succeed())

				// define a workerless shoot
				shoot := &gardencorev1beta1.Shoot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: namespace,
					},
					Spec: gardencorev1beta1.ShootSpec{},
				}
				shootJSON, err := json.Marshal(shoot)
				Expect(err).To(BeNil())

				cluster.Spec.Shoot = runtime.RawExtension{Raw: shootJSON}
				Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
			})

			AfterEach(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ext))).To(Succeed())
			})

			It("patches only the rule into the filters object, and no CIDRs for Seed|Shoot nodes and pods", func() {
				df, dfJSON := getEnvoyFilterFromFile(namespace)

				ar := e.createAdmissionResponse(context.Background(), df, dfJSON)

				Expect(ar.Allowed).To(BeTrue())

				expectedFilters := []map[string]interface{}{
					{
						"name": "acl-internal-source_ip",
						"typed_config": map[string]interface{}{
							"stat_prefix": "envoyrbac",
							"rules": map[string]interface{}{
								"policies": map[string]interface{}{
									"acl-internal": map[string]interface{}{
										"permissions": []map[string]interface{}{
											{
												"any": true,
											},
										},
										"principals": []map[string]interface{}{
											{
												"source_ip": map[string]interface{}{
													"address_prefix": "0.0.0.0",
													"prefix_len":     0,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "100.250.0.0",
													"prefix_len":     16,
												},
											},
											{
												"remote_ip": map[string]interface{}{
													"address_prefix": "10.96.0.0",
													"prefix_len":     11,
												},
											},
										},
									},
								},
								"action": "ALLOW",
							},
							"@type": "type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC",
						},
					},

					{
						"name": "envoy.filters.network.tcp_proxy",
						"typed_config": map[string]interface{}{
							"@type":       "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy",
							"cluster":     "outbound|443||kube-apiserver." + namespace + ".svc.cluster.local",
							"stat_prefix": "outbound|443||kube-apiserver." + namespace + ".svc.cluster.local",
						},
					},
				}

				Expect(ar.Patches[0].Value).To(Equal(expectedFilters))
			})
		})
	})
})

func getNewWebhook() *EnvoyFilterWebhook {
	decoder := admission.NewDecoder(clientScheme)
	return &EnvoyFilterWebhook{
		Client:  k8sClient,
		Decoder: decoder,
	}
}

func getNewCluster(namespace string, shoot *gardencorev1beta1.Shoot, seed *gardencorev1beta1.Seed) *extensionsv1alpha1.Cluster {
	shootJSON, err := json.Marshal(shoot)
	Expect(err).To(BeNil())

	seedJSON, err := json.Marshal(seed)
	Expect(err).To(BeNil())

	return &extensionsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
		Spec: extensionsv1alpha1.ClusterSpec{
			CloudProfile: runtime.RawExtension{Raw: []byte("{}")},
			Seed:         runtime.RawExtension{Raw: seedJSON},
			Shoot:        runtime.RawExtension{Raw: shootJSON},
		},
	}
}

func getNewInfrastructure(
	namespace, name, typeName string,
	providerConfigJSON, providerStatusJSON []byte,
) *extensionsv1alpha1.Infrastructure {
	return &extensionsv1alpha1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: extensionsv1alpha1.InfrastructureSpec{
			DefaultSpec: extensionsv1alpha1.DefaultSpec{
				Type: typeName,
				ProviderConfig: &runtime.RawExtension{
					Raw: providerConfigJSON,
				},
			},
		},
		Status: extensionsv1alpha1.InfrastructureStatus{
			DefaultStatus: extensionsv1alpha1.DefaultStatus{
				ProviderStatus: &runtime.RawExtension{
					Raw: providerStatusJSON,
				},
			},
		},
	}
}

func getExtensionSpec() *extensionspec.ExtensionSpec {
	return &extensionspec.ExtensionSpec{
		Rule: &envoyfilters.ACLRule{},
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

// getEnvoyFilterFromFile takes the technical shoot ID as a parameter to render
// into the JSON tempate file. Returns both the JSON representation as string
// and the struct type.
func getEnvoyFilterFromFile(technicalID string) (filter *istionetworkingClientGo.EnvoyFilter, filterAsString string) {
	filterSpecJSON, err := os.ReadFile(path.Join("./testdata", "defaultEnvoyFilter.json"))
	Expect(err).ShouldNot(HaveOccurred())

	templatedFilterSpec := strings.ReplaceAll(string(filterSpecJSON), "{{TECHNICAL-SHOOT-ID}}", technicalID)

	filter = &istionetworkingClientGo.EnvoyFilter{}

	Expect(json.Unmarshal([]byte(templatedFilterSpec), filter)).To(Succeed())

	return filter, templatedFilterSpec
}
