package helper

import (
	"encoding/json"

	openstackv1alpha1 "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/v1alpha1"
	"github.com/gardener/gardener-extension-provider-openstack/pkg/openstack"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// TODO Add test for the acl-api-shoot and acl-vpn envoy filter

var _ = Describe("provider Unit Tests", func() {
	Describe("GetProviderSpecificAllowedCIDRs", func() {
		When("there is an infrastructure object for which no custom behavior is required", func() {
			It("Should leave the alwaysAllowedCIDRs slice unaltered and should not error", func() {
				infra := &extensionsv1alpha1.Infrastructure{
					Spec: extensionsv1alpha1.InfrastructureSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							Type: "non-existent",
						},
					},
				}

				allowedCIDRs := []string{"a", "b"}
				providerIPs, err := GetProviderSpecificAllowedCIDRs(infra)
				Expect(err).To(Succeed())

				allowedCIDRs = append(allowedCIDRs, providerIPs...)
				Expect(allowedCIDRs).To(Equal([]string{"a", "b"}))
			})
		})
		When("there is an infrastructure object of type 'openstack'", func() {
			It("Should add the router IP to the alwaysAllowedCIDRs slice and should not error", func() {
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

				infra := &extensionsv1alpha1.Infrastructure{
					Spec: extensionsv1alpha1.InfrastructureSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							Type: openstack.Type,
						},
					},
					Status: extensionsv1alpha1.InfrastructureStatus{
						DefaultStatus: extensionsv1alpha1.DefaultStatus{
							ProviderStatus: &runtime.RawExtension{
								Raw: infraStatusJSON,
							},
						},
					},
				}

				allowedCIDRs := []string{"a", "b"}
				providerIPs, err := GetProviderSpecificAllowedCIDRs(infra)
				Expect(err).To(Succeed())

				allowedCIDRs = append(allowedCIDRs, providerIPs...)
				Expect(allowedCIDRs).To(Equal([]string{"a", "b", "10.9.8.7/32"}))
			})
		})
	})
})
