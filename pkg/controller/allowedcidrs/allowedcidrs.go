package allowedcidrs

import (
	"context"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
)

// AllowedCIDRer defines an interface on how to get AllowedCIDRs and SNI Hosts
type AllowedCIDRer interface {
	Hosts() ([]string, error)
	AllowedCIDRs(context.Context, *extensionsv1alpha1.Extension) ([]string, error)
}
