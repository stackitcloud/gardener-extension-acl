package helper

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "EnvoyFilters Test Suite")
}

var _ = BeforeSuite(func() {

})

var _ = AfterSuite(func() {

})
