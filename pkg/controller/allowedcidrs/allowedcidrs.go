package allowedcidrs

import (
	"context"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
)

type AllowedCIDRer interface {
	Hosts() ([]string, error)
	AllowedCIDRs(ctx context.Context, ex *extensionsv1alpha1.Extension) ([]string, error)
}
