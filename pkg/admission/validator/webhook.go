package validator

import (
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/pkg/apis/core"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	// Name is a name for a validation webhook.
	Name = "validator"
)

var logger = log.Log.WithName("acl-validator-webhook")

// New creates a new webhook that validates Shoot resources.
func New(mgr manager.Manager) (*extensionswebhook.Webhook, error) {
	logger.Info("Setting up webhook", "name", Name)
	return extensionswebhook.New(mgr, extensionswebhook.Args{
		Provider: "acl",
		Name:     Name,
		Path:     "/webhooks/validate",
		Validators: map[extensionswebhook.Validator][]extensionswebhook.Type{
			NewShootValidator(): {{Obj: &core.Shoot{}}},
		},
		Target: extensionswebhook.TargetSeed,
		ObjectSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"extensions.extensions.gardener.cloud/acl": "true"},
		},
	})
}
