package envoyfilters

import (
	"os"
	"path"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("EnoyFilter Unit Tests", func() {
	var (
		e *EnvoyFilterService
	)

	BeforeEach(func() {
		e = &EnvoyFilterService{}
	})

	AfterEach(func() {

	})

	Describe("BuildEnvoyFilterSpecForHelmChart", func() {
		When("there is an extension resource with one rule", func() {
			It("Should create a envoyFilter spec matching the expected one", func() {
				rules := []ACLRule{}
				rules = append(rules, *createRule("DENY", "source_ip", "0.0.0.0", 0))
				hosts := []string{
					"api.test.garden.s.testseed.dev.ske.eu01.stackit.cloud",
					"api.test.garden.internal.testseed.dev.ske.eu01.stackit.cloud",
				}
				technicalShootID := "shoot--projectname--shootname"

				result, err := e.BuildEnvoyFilterSpecForHelmChart(rules, hosts, technicalShootID)

				Expect(err).ToNot(HaveOccurred())
				checkIfMapEqualsYAML(result, "envoyFilterSpecWithOneDenyRule.yaml")
			})
		})
	})

	Describe("CreateInternalFilterPatchFromRule", func() {
		When("there is a deny rule", func() {
			It("Should create a filter spec matching the expected one", func() {
				rule := createRule("DENY", "remote_ip", "0.0.0.0", 0)

				result, err := e.CreateInternalFilterPatchFromRule(rule)

				Expect(err).ToNot(HaveOccurred())
				checkIfMapEqualsYAML(result, "singleFiltersEntry.yaml")
			})
		})
	})
})

func createRule(action, ruleType, addPref string, prefLen int) *ACLRule {
	return &ACLRule{
		Cidrs: []Cidr{
			{
				AddressPrefix: addPref,
				PrefixLength:  prefLen,
			},
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
