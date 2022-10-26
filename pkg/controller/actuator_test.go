package controller

import (
	"os"
	"path"

	"github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"gopkg.in/yaml.v2"

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

	Describe("buildEnvoyFilterSpecForHelmChart", func() {
		When("there is an extension resource with one rule", func() {
			It("Should create a envoyFilter spec matching the expected one", func() {
				extSpec := getExtensionSpec()
				addRuleToSpec(extSpec, "DENY", "source_ip", "0.0.0.0", 0)
				hosts := []string{
					"api.test.garden.s.testseed.dev.ske.eu01.stackit.cloud",
					"api.test.garden.internal.testseed.dev.ske.eu01.stackit.cloud",
				}
				shootName := "test"

				result, err := a.buildEnvoyFilterSpecForHelmChart(extSpec, hosts, shootName)

				Expect(err).ToNot(HaveOccurred())
				checkIfMapEqualsYAML(result, "envoyFilterSpecWithOneDenyRule.yaml")
			})
		})
	})

	Describe("updateEnvoyFilterHash", func() {
		When("there is an extension resource with one rule", func() {
			shootName := "test-123"
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
				addRuleToSpec(extSpec, "DENY", "source_ip", "0.0.0.0", 0)

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
			shootName := "test-abc"
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
})

func getNewActuator() *actuator {
	return &actuator{
		client: k8sClient,
		config: cfg,
		logger: logger,
		extensionConfig: config.Config{
			ChartPath: "../../charts",
		},
	}
}

func getClusterAsCtrlCluster(namespace string) *controller.Cluster {
	// of course there are two different cluster types :)
	ctrlCluster, err := controller.GetCluster(ctx, k8sClient, namespace)
	Expect(err).ToNot(HaveOccurred())
	return ctrlCluster
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
	return &ExtensionSpec{
		Rules: []envoyfilters.ACLRule{},
	}
}

func addRuleToSpec(extSpec *ExtensionSpec, action, ruleType, addPref string, prefLen int) {
	extSpec.Rules = append(extSpec.Rules, envoyfilters.ACLRule{
		Cidrs: []envoyfilters.Cidr{
			{
				AddressPrefix: addPref,
				PrefixLength:  prefLen,
			},
		},
		Action: action,
		Type:   ruleType,
	})
}

// checkIfMapEqualsYAML takes a map as input, and tries to compare its
// marshalled contents to the string coming from the specified testdata file.
// Fails the test if strings differ. The file contents are unmarshalled and
// marshalled again to guarantee the strings are comparable at all.
func checkIfMapEqualsYAML(input map[string]interface{}, relTestingFilePath string) {
	goldenYAMLByteArray, err := os.ReadFile(path.Join("./testdata", relTestingFilePath))
	Expect(err).ToNot(HaveOccurred())
	goldenMap := map[string]interface{}{}
	Expect(yaml.Unmarshal(goldenYAMLByteArray, goldenMap)).To(Succeed())
	goldenYAMLProcessedByteArray, err := yaml.Marshal(goldenMap)

	// we also have to mangle the input through a marshal+unmarshal step,
	// because otherwise Go is too smart about the nested types of the map contents
	Expect(err).ToNot(HaveOccurred())

	inputByteArray, err := yaml.Marshal(input)

	Expect(string(inputByteArray)).To(Equal(string(goldenYAMLProcessedByteArray)))
}
