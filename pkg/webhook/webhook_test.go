package webhook

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"strings"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/stackitcloud/gardener-extension-acl/pkg/controller"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	istionetworkingClientGo "istio.io/client-go/pkg/apis/networking/v1alpha3"
)

var _ = Describe("webhook unit test", func() {
	var (
		e         *EnvoyFilterWebhook
		ext       *extensionsv1alpha1.Extension
		cluster   *extensionsv1alpha1.Cluster
		namespace string
	)

	BeforeEach(func() {
		namespace = createNewNamespace()
		cluster = getNewCluster(namespace)
		e = getNewWebhook()

		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
	})

	AfterEach(func() {
		deleteNamespace(namespace)
	})

	Describe("createAdmissionResponse", func() {
		When("the name of the EnvoyFilter doesn't start with 'shoot--'", func() {
			It("issues no patch for the EnvoyyFilter", func() {
				df, dfJSON := getEnvoyFilterFromFile("defaultEnvoyFilter.json", "non-shoot-envoyfilter")

				ar := e.createAdmissionResponse(context.Background(), df, dfJSON)

				Expect(ar.Allowed).To(BeTrue())
				Expect(string(ar.Patch)).To(Equal(""))
			})
		})

		When("there is no extension", func() {
			It("issues no patch for the EnvoyyFilter", func() {
				df, dfJSON := getEnvoyFilterFromFile("defaultEnvoyFilter.json", namespace)

				ar := e.createAdmissionResponse(context.Background(), df, dfJSON)

				Expect(ar.Allowed).To(BeTrue())
				Expect(string(ar.Patch)).To(Equal(""))
			})
		})

		When("there is an extension resource with one DENY rule", func() {
			extSpec := getExtensionSpec()

			BeforeEach(func() {
				addRuleToSpec(extSpec, "DENY", "source_ip", "0.0.0.0/0")
				ext = getNewExtension(namespace, *extSpec)

				Expect(k8sClient.Create(ctx, ext)).To(Succeed())

				cluster.Spec.Shoot.Raw = []byte(`{
					"spec": {
						"networking": {
							"nodes": "10.250.0.0/16",
							"pods": "100.96.0.0/11",
							"services": "100.64.0.0/13",
							"type": "calico"
						}
					}
				}`)

				Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
			})

			AfterEach(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ext))).To(Succeed())
			})

			It("patches this rule into the filters object", func() {
				df, dfJSON := getEnvoyFilterFromFile("defaultEnvoyFilter.json", namespace)

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

				cluster.Spec.Shoot.Raw = []byte(`{
					"spec": {
						"networking": {
							"nodes": "10.250.0.0/16",
							"pods": "100.96.0.0/11",
							"services": "100.64.0.0/13",
							"type": "calico"
						}
					}
				}`)

				cluster.Spec.Seed.Raw = []byte(`{
					"spec": {
						"networks": {
							"nodes": "100.250.0.0/16",
							"pods": "10.96.0.0/11",
							"services": "10.64.0.0/13"
						}
					}
				}`)

				Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
			})

			AfterEach(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ext))).To(Succeed())
			})

			It("patches this rule into the filters object, including the Node CIDRs", func() {
				df, dfJSON := getEnvoyFilterFromFile("defaultEnvoyFilter.json", namespace)

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
													"address_prefix": "10.250.0.0",
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
	return &EnvoyFilterWebhook{
		Client:             k8sClient,
		EnvoyFilterService: envoyfilters.EnvoyFilterService{},
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

func getExtensionSpec() *controller.ExtensionSpec {
	return &controller.ExtensionSpec{
		Rules: []envoyfilters.ACLRule{},
	}
}

func addRuleToSpec(extSpec *controller.ExtensionSpec, action, ruleType, cidr string) {
	extSpec.Rules = append(extSpec.Rules, envoyfilters.ACLRule{
		Cidrs: []string{
			cidr,
		},
		Action: action,
		Type:   ruleType,
	})
}

// getEnvoyFilterFromFile takes the technical shoot ID as a parameter to render
// into the JSON tempate file. Returns both the JSON representation as string
// and the struct type.
func getEnvoyFilterFromFile(fileName, technicalID string) (filter *istionetworkingClientGo.EnvoyFilter, filterAsString string) {
	filterSpecJSON, err := os.ReadFile(path.Join("./testdata", "defaultEnvoyFilter.json"))
	Expect(err).ShouldNot(HaveOccurred())

	templatedFilterSpec := strings.ReplaceAll(string(filterSpecJSON), "{{TECHNICAL-SHOOT-ID}}", technicalID)

	filter = &istionetworkingClientGo.EnvoyFilter{}

	Expect(json.Unmarshal([]byte(templatedFilterSpec), filter)).To(Succeed())

	return filter, templatedFilterSpec
}
