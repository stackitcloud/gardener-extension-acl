package cmd

import (
	extensionscmdwebhook "github.com/gardener/gardener/extensions/pkg/webhook/cmd"

	"github.com/stackitcloud/gardener-extension-acl/pkg/admission/validator"
)

// GardenWebhookSwitchOptions are the extensionscmdwebhook.SwitchOptions for the admission webhooks.
func GardenWebhookSwitchOptions() *extensionscmdwebhook.SwitchOptions {
	return extensionscmdwebhook.NewSwitchOptions(
		extensionscmdwebhook.Switch(validator.Name, validator.New),
	)
}
