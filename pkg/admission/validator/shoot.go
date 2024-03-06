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

	"github.com/stackitcloud/gardener-extension-acl/pkg/webhook"
)

// NewShootValidator returns a new instance of a shoot validator.
func NewShootValidator(mgr manager.Manager) extensionswebhook.Validator {
	return &shoot{
		client: mgr.GetClient(),
	}
}

const (
	ruleKey  = "rule"
	cidrsKey = "cidrs"
)

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
func (s *shoot) Validate(ctx context.Context, new, old client.Object) error {
	shoot, ok := new.(*core.Shoot)
	if !ok {
		return fmt.Errorf("wrong object type %T", new)
	}
	if old != nil {
		return s.validateShootUpdate(ctx, shoot)
	}
	return s.validateShootCreation(ctx, shoot)
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
		if rule, ok := aclRules[ruleKey].(map[string]interface{}); ok {
			if cidrs, ok := rule[cidrsKey].([]interface{}); ok {
				if len(cidrs) > DefaultAddOptions.MaxAllowedCIDRs {
					fldPath := field.NewPath("spec", "extensions", "providerConfig", "rule", "cidrs")
					return field.TooMany(fldPath, len(cidrs), DefaultAddOptions.MaxAllowedCIDRs)
				}
			}
		}
	}
	return nil
}
func (s *shoot) validateShootUpdate(ctx context.Context, shoot *core.Shoot) error {
	return s.validateShoot(ctx, shoot)
}
func (s *shoot) validateShootCreation(ctx context.Context, shoot *core.Shoot) error {
	return s.validateShoot(ctx, shoot)
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

func (s *shoot) decodeAclExtension(aclExt *runtime.RawExtension) (map[string]interface{}, error) {
	var aclRules map[string]interface{}
	if aclExt == nil || aclExt.Raw == nil {
		return map[string]interface{}{}, nil
	}
	if err := json.Unmarshal(aclExt.Raw, &aclRules); err != nil {
		return nil, err
	}
	return aclRules, nil
}
