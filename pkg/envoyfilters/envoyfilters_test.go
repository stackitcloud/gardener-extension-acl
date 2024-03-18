package envoyfilters

import (
	"os"
	"path"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("EnvoyFilter Unit Tests", func() {
	var (
		alwaysAllowedCIDRs = []string{
			"10.250.0.0/16",
			"10.96.0.0/11",
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
				checkIfMapEqualsYAML(result, "apiEnvoyFilterSpecWithOneAllowRule.yaml")
			})
		})
	})

	Describe("BuildIngressEnvoyFilterSpecForHelmChart", func() {
		When("there is an extension resource with one rule", func() {
			It("Should create an envoyFilter spec matching the expected one", func() {
				rule := createRule("ALLOW", "remote_ip", "10.180.0.0/16")
				ingressSNI := "ingress.testseed.dev.ske.eu01.stackit.cloud"
				shootID := "project-shoot"

				labels := map[string]string{
					"app":   "istio-ingressgateway",
					"istio": "ingressgateway",
				}
				result, err := BuildIngressEnvoyFilterSpecForHelmChart(rule, ingressSNI, shootID, alwaysAllowedCIDRs, labels)

				Expect(err).ToNot(HaveOccurred())
				checkIfMapEqualsYAML(result, "ingressEnvoyFilterSpecWithOneAllowRule.yaml")
			})
		})
	})

	Describe("BuildVPNEnvoyFilterSpecForHelmChart", func() {
		When("there is one shoot with a rule", func() {
			It("Should create a envoyFilter spec matching the expected one", func() {
				mappings := []ACLMapping{
					{
						ShootName: "shoot--projectname--shootname",
						Rule:      *createRule("ALLOW", "remote_ip", "0.0.0.0/0"),
					},
				}
				labels := map[string]string{
					"app":   "istio-ingressgateway",
					"istio": "ingressgateway",
				}
				result, err := BuildVPNEnvoyFilterSpecForHelmChart(mappings, alwaysAllowedCIDRs, labels)

				Expect(err).ToNot(HaveOccurred())
				checkIfMapEqualsYAML(result, "vpnEnvoyFilterSpecWithOneAllowRule.yaml")
			})
		})
	})

	Describe("CreateInternalFilterPatchFromRule", func() {
		When("there is an allow rule", func() {
			It("Should create a filter spec matching the expected one, including the always allowed CIDRs", func() {
				rule := createRule("ALLOW", "remote_ip", "0.0.0.0/0")

				result, err := CreateInternalFilterPatchFromRule(rule, alwaysAllowedCIDRs, []string{})

				Expect(err).ToNot(HaveOccurred())
				checkIfMapEqualsYAML(result, "singleFiltersAllowEntry.yaml")
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

// checkIfMapEqualsYAML takes a map as input, and tries to compare its
// marshaled contents to the string coming from the specified testdata file.
// Fails the test if strings differ. The file contents are unmarshaled and
// marshaled again to guarantee the strings are comparable.
func checkIfMapEqualsYAML(input map[string]interface{}, relTestingFilePath string) {
	goldenYAMLByteArray, err := os.ReadFile(path.Join("./testdata", relTestingFilePath))
	Expect(err).ToNot(HaveOccurred())
	goldenMap := map[string]interface{}{}
	Expect(yaml.Unmarshal(goldenYAMLByteArray, goldenMap)).To(Succeed())
	goldenYAMLProcessedByteArray, err := yaml.Marshal(goldenMap)
	Expect(err).ToNot(HaveOccurred())

	inputByteArray, err := yaml.Marshal(input)
	Expect(err).ToNot(HaveOccurred())

	Expect(string(inputByteArray)).To(Equal(string(goldenYAMLProcessedByteArray)))
}
