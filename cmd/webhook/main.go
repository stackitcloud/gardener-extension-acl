package main

import (
	"flag"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"

	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	aclwebhook "github.com/stackitcloud/gardener-extension-acl/pkg/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(extensionsv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var certDir, keyName, certName string
	var additionalAllowedCidrs string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")

	flag.StringVar(&certDir, "cert-dir", "", "Folder where key-name and cert-name are located.")
	flag.StringVar(&keyName, "key-name", "", "Filename for .key file.")
	flag.StringVar(&certName, "cert-name", "", "Filename for .cert file.")
	flag.StringVar(
		&additionalAllowedCidrs,
		"additional-allowed-cidrs",
		"",
		"Comma separated list of ips that will be added to the allowed cidr list i.e. (192.168.1.40/32,...)",
	)

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{

		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
		Metrics: metricsserver.Options{
			SecureServing: false,
			BindAddress:   metricsAddr,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Server uses default values if provided paths are empty
	server := webhook.NewServer(webhook.Options{
		Port:     9443,
		CertDir:  certDir,
		CertName: certName,
		KeyName:  keyName,
	})

	allowedCidrs := strings.Split(additionalAllowedCidrs, ",")

	server.Register("/mutate", &webhook.Admission{Handler: &aclwebhook.EnvoyFilterWebhook{
		Client:             mgr.GetClient(),
		EnvoyFilterService: envoyfilters.EnvoyFilterService{},
		WebhookConfig:      aclwebhook.Config{AdditionalAllowedCidrs: allowedCidrs},
	}})

	mgr.Add(server)

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
