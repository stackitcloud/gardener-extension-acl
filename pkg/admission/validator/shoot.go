package validator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/pkg/apis/core"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-acl/pkg/controller"
	"github.com/stackitcloud/gardener-extension-acl/pkg/extensionspec"
)

// NewShootValidator returns a new instance of a shootValidator.
func NewShootValidator() extensionswebhook.Validator {
	return &shootValidator{}
}

// DefaultAddOptions are the default options to apply when adding the webhook to the manager.
var DefaultAddOptions = AddOptions{}

// AddOptions are options to apply when adding the webhook to the manager.
type AddOptions struct {
	MaxAllowedCIDRs int
}

type shootValidator struct{}

// Validate validates the given shoot object.
func (s *shootValidator) Validate(ctx context.Context, new, _ client.Object) error {
	shoot, ok := new.(*core.Shoot)
	if !ok {
		return fmt.Errorf("wrong object type %T", new)
	}
	return s.validateShoot(ctx, shoot)
}

func (s *shootValidator) validateShoot(_ context.Context, shoot *core.Shoot) error {
	aclExtension, extensionIndex := s.findExtension(shoot)
	if aclExtension == nil {
		return nil
	}
	fldPath := field.NewPath("spec", "extensions").Index(extensionIndex).Child("providerConfig")

	if aclExtension.Disabled != nil && *aclExtension.Disabled {
		return nil
	}

	extensionSpec, err := s.decodeExtensionSpec(aclExtension.ProviderConfig)
	if err != nil {
		return fmt.Errorf("error decoding ACL extension spec: %w", err)
	}

	if extensionSpec == nil || extensionSpec.Rule == nil {
		return nil
	}

	if err := controller.ValidateExtensionSpec(extensionSpec, DefaultAddOptions.MaxAllowedCIDRs); err != nil {
		// field error for too many CIDRs
		if errors.Is(err, controller.ErrSpecTooManyCIDRs) {
			return field.TooMany(fldPath.Child("rule", "cidrs"), len(extensionSpec.Rule.Cidrs), DefaultAddOptions.MaxAllowedCIDRs)
		}
		return err
	}

	return nil
}

func (s *shootValidator) findExtension(shoot *core.Shoot) (*core.Extension, int) {
	for i, ext := range shoot.Spec.Extensions {
		if ext.Type == controller.Type {
			return &shoot.Spec.Extensions[i], i
		}
	}
	return nil, 0
}

func (s *shootValidator) decodeExtensionSpec(aclExt *runtime.RawExtension) (*extensionspec.ExtensionSpec, error) {
	extSpec := &extensionspec.ExtensionSpec{}

	if aclExt != nil && aclExt.Raw != nil {
		if err := json.Unmarshal(aclExt.Raw, &extSpec); err != nil {
			return nil, err
		}
	}
	return extSpec, nil
}
