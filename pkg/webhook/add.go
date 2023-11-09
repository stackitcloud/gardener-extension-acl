package webhook

import (
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	// WebhookName is the name of the webhook acl ACL extension.
	WebhookName = "acl-webhook"
)

var (
	logger = log.Log.WithName(WebhookName)

	// DefaultAddOptions are the default AddOptions for AddToManager.
	DefaultAddOptions = AddOptions{}
)

// AddOptions are options to apply when adding the webhook to the manager.
type AddOptions struct {
	AllowedCIDRs []string
}

// AddToManagerWithOptions creates a webhook with the given options and adds it to the manager.
func AddToManagerWithOptions(
	mgr manager.Manager,
	options AddOptions,
) error {
	logger.Info("Adding webhook to manager")

	mgr.GetWebhookServer().Register("/mutate", &webhook.Admission{Handler: &EnvoyFilterWebhook{
		Client:                 mgr.GetClient(),
		EnvoyFilterService:     envoyfilters.EnvoyFilterService{},
		AdditionalAllowedCIDRs: options.AllowedCIDRs,
	}})

	return nil
}

// AddToManager creates a webhook with the default options and adds it to the manager.
func AddToManager(mgr manager.Manager) (*extensionswebhook.Webhook, error) {
	return nil, AddToManagerWithOptions(
		mgr,
		DefaultAddOptions,
	)
}
