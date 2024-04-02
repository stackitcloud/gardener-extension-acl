package validator

import (
	"context"
	"encoding/json"
	"fmt"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/pkg/apis/core"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-acl/pkg/extensionspec"
	"github.com/stackitcloud/gardener-extension-acl/pkg/webhook"
)

// NewShootValidator returns a new instance of a shoot validator.
func NewShootValidator(mgr manager.Manager) extensionswebhook.Validator {
	return &shoot{
		client: mgr.GetClient(),
	}
}

// DefaultAddOptions are the default options to apply when adding the webhook to the manager.
var DefaultAddOptions = AddOptions{}

// AddOptions are options to apply when adding the webhook to the manager.
type AddOptions struct {
	MaxAllowedCIDRs int
}

type shoot struct {
	client client.Client
}

// Validate validates the given shoot object.
func (s *shoot) Validate(ctx context.Context, new, _ client.Object) error {
	shoot, ok := new.(*core.Shoot)
	if !ok {
		return fmt.Errorf("wrong object type %T", new)
	}
	return s.validateShoot(ctx, shoot)
}

func (s *shoot) validateShoot(_ context.Context, shoot *core.Shoot) error {
	aclExtension := s.findExtension(shoot)
	if aclExtension == nil {
		return nil
	}
	aclRules, err := s.decodeAclExtension(aclExtension.ProviderConfig)
	if err != nil {
		return err
	}

	if aclRules != nil {
		if len(aclRules.Rule.Cidrs) > DefaultAddOptions.MaxAllowedCIDRs {
			fldPath := field.NewPath("spec", "extensions", "providerConfig", "rule", "cidrs")
			return field.TooMany(fldPath, len(aclRules.Rule.Cidrs), DefaultAddOptions.MaxAllowedCIDRs)
		}
	}

	return nil
}

// findExtension returns acl extension.
func (s *shoot) findExtension(shoot *core.Shoot) *core.Extension {
	for i, ext := range shoot.Spec.Extensions {
		if ext.Type == webhook.ExtensionName {
			return &shoot.Spec.Extensions[i]
		}
	}
	return nil
}

func (s *shoot) decodeAclExtension(aclExt *runtime.RawExtension) (*extensionspec.ExtensionSpec, error) {
	extSpec := &extensionspec.ExtensionSpec{}

	if aclExt != nil && aclExt.Raw != nil {
		if err := json.Unmarshal(aclExt.Raw, &extSpec); err != nil {
			return nil, err
		}
	}
	return extSpec, nil
}
