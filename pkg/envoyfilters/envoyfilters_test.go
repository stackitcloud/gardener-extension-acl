package envoyfilters

import (
	"encoding/json"
	"os"
	"path"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/extensions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var _ = Describe("EnvoyFilter Unit Tests", func() {
	var (
		alwaysAllowedCIDRs = []string{
			"10.250.0.0/16",
			"10.96.0.0/11",
		}
		cluster = &extensions.Cluster{
			Shoot: &gardencorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Status: gardencorev1beta1.ShootStatus{
					TechnicalID: "shoot--bar--foo",
				},
			},
			Seed: &gardencorev1beta1.Seed{
				Spec: gardencorev1beta1.SeedSpec{
					Ingress: &gardencorev1beta1.Ingress{
						Domain: "ingress.testseed.dev.ske.eu01.stackit.cloud",
					},
				},
			},
		}
	)

	Describe("BuildAPIEnvoyFilterSpecForHelmChart", func() {
		When("there is an extension resource with one rule", func() {
			It("Should create a envoyFilter spec matching the expected one", func() {
				rule := createRule("ALLOW", "source_ip", "0.0.0.0/0")
				hosts := []string{
					"api.test.garden.s.testseed.dev.ske.eu01.stackit.cloud",
					"api.test.garden.internal.testseed.dev.ske.eu01.stackit.cloud",
				}
				labels := map[string]string{
					"app":   "istio-ingressgateway",
					"istio": "ingressgateway",
				}
				result, err := BuildAPIEnvoyFilterSpecForHelmChart(rule, hosts, alwaysAllowedCIDRs, labels)

				Expect(err).ToNot(HaveOccurred())
				checkIfFilterEquals(result, "apiEnvoyFilterSpecWithOneAllowRule.yaml")
			})
		})
	})

	Describe("BuildIngressEnvoyFilterSpecForHelmChart", func() {
		When("there is an extension resource with one rule", func() {
			It("Should create an envoyFilter spec matching the expected one", func() {
				rule := createRule("ALLOW", "remote_ip", "10.180.0.0/16")
				labels := map[string]string{
					"app":   "istio-ingressgateway",
					"istio": "ingressgateway",
				}
				ingressEnvoyFilterSpec := BuildIngressEnvoyFilterSpecForHelmChart(cluster, rule, alwaysAllowedCIDRs, labels)

				checkIfFilterEquals(ingressEnvoyFilterSpec, "ingressEnvoyFilterSpecWithOneAllowRule.yaml")
			})
			It("Should not create an envoyFilter spec when seed has no ingress", func() {
				rule := createRule("ALLOW", "remote_ip", "10.180.0.0/16")
				cluster.Seed.Spec.Ingress = nil
				labels := map[string]string{
					"app":   "istio-ingressgateway",
					"istio": "ingressgateway",
				}
				ingressEnvoyFilterSpec := BuildIngressEnvoyFilterSpecForHelmChart(cluster, rule, alwaysAllowedCIDRs, labels)
				Expect(ingressEnvoyFilterSpec).To(BeNil())
			})
		})
	})

	Describe("BuildVPNEnvoyFilterSpecForHelmChart", func() {
		When("there is one shoot with a rule", func() {
			It("Should create a envoyFilter spec matching the expected one", func() {
				rule := createRule("ALLOW", "remote_ip", "10.180.0.0/16")
				labels := map[string]string{
					"app":   "istio-ingressgateway",
					"istio": "ingressgateway",
				}
				result := BuildVPNEnvoyFilterSpecForHelmChart(cluster, rule, alwaysAllowedCIDRs, labels)
				checkIfFilterEquals(result, "vpnEnvoyFilterSpecWithOneAllowRule.yaml")
			})
		})
	})

	Describe("CreateInternalFilterPatchFromRule", func() {
		When("there is an allow rule", func() {
			It("Should create a filter spec matching the expected one, including the always allowed CIDRs", func() {
				rule := createRule("ALLOW", "remote_ip", "0.0.0.0/0")

				result := CreateInternalFilterPatchFromRule(rule, alwaysAllowedCIDRs, []string{})
				checkIfFilterEquals(result, "singleFiltersAllowEntry.yaml")
			})
		})
	})

	Describe("CreateAPIConfigPatchFromRule", func() {
		When("there are no hosts", func() {
			It("should return the appropriate error", func() {
				rule := createRule("ALLOW", "remote_ip", "0.0.0.0/0")

				result, err := CreateAPIConfigPatchFromRule(rule, nil, alwaysAllowedCIDRs)

				Expect(err).To(Equal(ErrNoHostsGiven))
				Expect(result).To(BeNil())
			})
		})
	})
})

//nolint:unparam // action currently only accepts ALLOW but that might change, so we leave the parameterization
func createRule(action, ruleType, cidr string) *ACLRule {
	return &ACLRule{
		Cidrs: []string{
			cidr,
		},
		Action: action,
		Type:   ruleType,
	}
}

// checkIfFilterEqualsYAML takes a map as input, and tries to compare its
// marshaled contents to the string coming from the specified testdata file.
// Fails the test if strings differ. The file contents are unmarshaled and
// marshaled again to guarantee the strings are comparable.
func checkIfFilterEquals(input any, relTestingFilePath string) {
	goldenYAMLBytes, err := os.ReadFile(path.Join("./testdata", relTestingFilePath))
	Expect(err).ToNot(HaveOccurred())

	goldenJSON, err := yaml.YAMLToJSON(goldenYAMLBytes)
	Expect(err).ToNot(HaveOccurred())

	var actual []byte
	if m, ok := input.(proto.Message); ok {
		actual, err = protojson.Marshal(m)
	} else {
		actual, err = json.Marshal(input)
	}
	Expect(err).ToNot(HaveOccurred())

	Expect(actual).To(MatchJSON(goldenJSON))
}
