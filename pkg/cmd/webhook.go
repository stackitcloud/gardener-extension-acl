package cmd

import (
	"context"
	"fmt"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/extensions/pkg/webhook/certificates"
	extensionscmdwebhook "github.com/gardener/gardener/extensions/pkg/webhook/cmd"
	"github.com/gardener/gardener/pkg/utils/flow"
	"github.com/spf13/pflag"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-acl/pkg/webhook"
)

// AddToManagerOptions are options to create an `AddToManager` function from ServerOptions and SwitchOptions.
type AddToManagerOptions struct {
	extensionName string

	Server extensionscmdwebhook.ServerOptions
	Switch extensionscmdwebhook.SwitchOptions
}

// NewAddToManagerOptions creates new AddToManagerOptions with the given server name, server, and switch options.
// It is supposed to be used for webhooks which should be automatically registered in the cluster via a MutatingWebhookConfiguration.
func NewAddToManagerOptions(extensionName string, serverOpts *extensionscmdwebhook.ServerOptions, switchOpts *extensionscmdwebhook.SwitchOptions) *AddToManagerOptions {
	return &AddToManagerOptions{
		extensionName: extensionName,
		Server:        *serverOpts,
		Switch:        *switchOpts,
	}
}

// AddFlags implements Option.
func (c *AddToManagerOptions) AddFlags(fs *pflag.FlagSet) {
	c.Switch.AddFlags(fs)
	c.Server.AddFlags(fs)
}

// Complete implements Option.
func (c *AddToManagerOptions) Complete() error {
	if err := c.Switch.Complete(); err != nil {
		return err
	}

	return c.Server.Complete()
}

// Completed returns the completed AddToManagerConfig. Only call this if a previous call to `Complete` succeeded.
func (c *AddToManagerOptions) Completed() *AddToManagerConfig {
	return &AddToManagerConfig{
		extensionName: c.extensionName,

		Server: *c.Server.Completed(),
		Switch: *c.Switch.Completed(),
	}
}

// AddToManagerConfig is a completed AddToManager configuration.
type AddToManagerConfig struct {
	extensionName string

	Server extensionscmdwebhook.ServerConfig
	Switch extensionscmdwebhook.SwitchConfig
	Clock  clock.Clock
}

// AddToManager instantiates all webhooks of this configuration. If there are any webhooks, it creates a
// webhook server, registers the webhooks and adds the server to the manager. Otherwise, it is a no-op.
// It generates and registers the seed targeted webhooks via a MutatingWebhookConfiguration.
func (c *AddToManagerConfig) AddToManager(ctx context.Context, mgr manager.Manager) error {
	if c.Clock == nil {
		c.Clock = &clock.RealClock{}
	}

	_, err := c.Switch.WebhooksFactory(mgr)
	if err != nil {
		return fmt.Errorf("could not create webhooks: %w", err)
	}
	webhookServer := mgr.GetWebhookServer()

	servicePort := webhookServer.Port
	if (c.Server.Mode == extensionswebhook.ModeService || c.Server.Mode == extensionswebhook.ModeURLWithServiceName) && c.Server.ServicePort > 0 {
		servicePort = c.Server.ServicePort
	}

	webhookConfig := BuildWebhookConfig(
		extensionswebhook.BuildClientConfigFor(
			webhook.WebhookPath,
			c.Server.Namespace,
			webhook.ExtensionName,
			servicePort,
			c.Server.Mode,
			c.Server.URL,
			nil,
		),
	)

	if c.Server.Namespace == "" {
		// If the namespace is not set (e.g. when running locally), then we can't use the secrets manager for managing
		// the webhook certificates. We simply generate a new certificate and write it to CertDir in this case.
		mgr.GetLogger().Info("Running webhooks with unmanaged certificates (i.e., the webhook CA will not be rotated automatically). " +
			"This mode is supposed to be used for development purposes only. Make sure to configure --webhook-config-namespace in production.")

		caBundle, err := certificates.GenerateUnmanagedCertificates(c.extensionName, webhookServer.CertDir, c.Server.Mode, c.Server.URL)
		if err != nil {
			return fmt.Errorf("error generating new certificates for webhook server: %w", err)
		}

		// register seed webhook config once we become leader – with the CA bundle we just generated
		// also reconcile all shoot webhook configs to update the CA bundle
		if err := mgr.Add(runOnceWithLeaderElection(flow.Sequential(
			c.reconcileSeedWebhookConfig(mgr, webhookConfig, caBundle),
		))); err != nil {
			return err
		}

		return nil
	}

	// register seed webhook config once we become leader – without CA bundle
	// We only care about registering the desired webhooks here, but not the CA bundle, it will be managed by the
	// reconciler. That's why we also don't reconcile the shoot webhook configs here. They are registered in the
	// ControlPlane actuator and our reconciler will update the included CA bundles if necessary.
	if err := mgr.Add(runOnceWithLeaderElection(
		c.reconcileSeedWebhookConfig(mgr, webhookConfig, nil),
	)); err != nil {
		return err
	}

	if err := certificates.AddCertificateManagementToManager(
		ctx,
		mgr,
		c.Clock,
		[]client.Object{
			webhookConfig,
		},
		nil,
		nil,
		nil,
		"",
		c.extensionName,
		c.Server.Namespace,
		c.Server.Mode,
		c.Server.URL,
	); err != nil {
		return err
	}

	return nil
}

func (c *AddToManagerConfig) reconcileSeedWebhookConfig(mgr manager.Manager, seedWebhookConfig client.Object, caBundle []byte) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if seedWebhookConfig != nil {
			if err := extensionswebhook.ReconcileSeedWebhookConfig(ctx, mgr.GetClient(), seedWebhookConfig, c.Server.Namespace, caBundle); err != nil {
				return fmt.Errorf("error reconciling seed webhook config: %w", err)
			}
		}
		return nil
	}
}

// runOnceWithLeaderElection is a function that is run exactly once when the manager, it is added to, becomes leader.
type runOnceWithLeaderElection func(ctx context.Context) error

func (r runOnceWithLeaderElection) NeedLeaderElection() bool {
	return true
}

func (r runOnceWithLeaderElection) Start(ctx context.Context) error {
	return r(ctx)
}

// BuildWebhookConfig returns MutatingWebhookConfiguration for WebhookClientConfig
func BuildWebhookConfig(clientConfig admissionregistrationv1.WebhookClientConfig) *admissionregistrationv1.MutatingWebhookConfiguration {
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhook.ExtensionName,
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name:         "acl.stackit.cloud",
				ClientConfig: clientConfig,
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"networking.istio.io"},
							APIVersions: []string{"v1alpha3"},
							Resources:   []string{"envoyfilters"},
						},
					},
				},
				FailurePolicy:           ptr.To(admissionregistrationv1.Fail),
				SideEffects:             ptr.To(admissionregistrationv1.SideEffectClassNone),
				TimeoutSeconds:          ptr.To(int32(5)),
				AdmissionReviewVersions: []string{"v1"},
			},
		},
	}
}
