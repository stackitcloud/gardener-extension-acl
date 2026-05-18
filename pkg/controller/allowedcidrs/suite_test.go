package allowedcidrs

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAllowedCIDRs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "allowedcidrs Test Suite")
}
