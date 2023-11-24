package webhook

import (
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// WebhookName is the name of the webhook acl ACL extension.
	WebhookName = "acl-webhook"
	// WebhookPath is the path where the webhook listen to.
	WebhookPath = "/mutate"
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

	decoder, err := admission.NewDecoder(mgr.GetScheme())
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register(WebhookPath, &webhook.Admission{Handler: &EnvoyFilterWebhook{
		Client:                 mgr.GetClient(),
		AdditionalAllowedCIDRs: options.AllowedCIDRs,
		Decoder:                decoder,
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
