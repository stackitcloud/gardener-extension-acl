package validator_test

import (
	"context"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/pkg/apis/core"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/stackitcloud/gardener-extension-acl/pkg/admission/validator"
)

var _ = Describe("Shoot validator", func() {
	Describe("#Validate", func() {
		const namespace = "garden-dev"

		var (
			shootValidator extensionswebhook.Validator

			shoot *core.Shoot

			ctx = context.Background()
		)

		BeforeEach(func() {
			shootValidator = validator.NewShootValidator()
			validator.DefaultAddOptions.MaxAllowedCIDRs = 5

			shoot = &core.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: namespace,
				},
				Spec: core.ShootSpec{
					Extensions: []core.Extension{
						{
							Type:           "acl",
							ProviderConfig: &runtime.RawExtension{Raw: []byte(`{"rule":{"action":"ALLOW","cidrs":["1.2.3.4/24","10.250.0.0/16"],"type":"remote_ip"}}`)},
						},
					},
				},
			}
		})

		Context("Shoot creation (old is nil)", func() {
			It("should succeed if number of specified cidrs in acl extension is below maximum", func() {
				Expect(shootValidator.Validate(ctx, shoot, nil)).To(Succeed())
			})

			It("should return err if to many cidrs are specified in acl extension", func() {
				shoot.Spec.Extensions[0].ProviderConfig = &runtime.RawExtension{Raw: []byte(`{"rule":{"action":"ALLOW","cidrs":["1.2.3.4/24","10.250.0.0/16","208.127.57.6/32","165.1.187.201/32","165.1.187.202/32","165.1.187.203/32","165.1.187.207/32","165.1.187.208/32"],"type":"remote_ip"}}`)}
				err := shootValidator.Validate(ctx, shoot, nil)
				Expect(err).To(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeTooMany),
					"Field": Equal("spec.extensions[0].providerConfig.rule.cidrs"),
				})))
			})
		})

		Context("Shoot update", func() {
			It("should return err if to many cidrs are specified in acl extension", func() {
				newShoot := shoot
				newShoot.Spec.Extensions[0].ProviderConfig = &runtime.RawExtension{Raw: []byte(`{"rule":{"action":"ALLOW","cidrs":["1.2.3.4/24","10.250.0.0/16","208.127.57.6/32","165.1.187.201/32","165.1.187.202/32","165.1.187.203/32","165.1.187.207/32","165.1.187.208/32"],"type":"remote_ip"}}`)}
				err := shootValidator.Validate(ctx, newShoot, shoot)
				Expect(err).To(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeTooMany),
					"Field": Equal("spec.extensions[0].providerConfig.rule.cidrs"),
				})))
			})

			It("should succeed if number of specified cidrs in acl extension is below maximum", func() {
				newShoot := shoot
				newShoot.Spec.Extensions[0].ProviderConfig = &runtime.RawExtension{Raw: []byte(`{"rule":{"action":"ALLOW","cidrs":["1.2.3.4/24","10.250.0.0/16","208.127.57.6/32","165.1.187.201/32","165.1.187.202/32"],"type":"remote_ip"}}`)}
				Expect(shootValidator.Validate(ctx, newShoot, shoot)).To(Succeed())
			})
		})
	})
})
