package helper

import (
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("helper", func() {
	//nolint: revive // the projectName is part of the technical ID
	DescribeTable("#ComputeShortShootID", func(shootName, projectName, technicalID string, matcher gomegatypes.GomegaMatcher) {
		var (
			shoot = &gardencorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name: shootName,
				},
			}
		)
		shoot.Status = gardencorev1beta1.ShootStatus{
			TechnicalID: technicalID,
		}
		Expect(ComputeShortShootID(shoot)).To(matcher)
	},
		Entry("short shoot ID calculation (historic stored technical ID with a single dash)",
			"fooShoot",
			"barProject",
			"shoot-barProject--fooShoot",
			Equal("barProject--fooShoot")),
		Entry("short shoot ID (current stored technical ID with two dashes)",
			"fooShoot",
			"barProject",
			"shoot--barProject--fooShoot",
			Equal("barProject--fooShoot")),
	)
})
