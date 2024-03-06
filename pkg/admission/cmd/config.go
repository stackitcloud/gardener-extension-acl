package cmd

import (
	"github.com/spf13/pflag"

	controllerconfig "github.com/stackitcloud/gardener-extension-acl/pkg/controller/config"
)

// AdmissionOptions are command line options that can be set for admission controller.
type AdmissionOptions struct {
	// MaxAllowedCIDRs is the maximum number of allowed CIDRs per cluster
	MaxAllowedCIDRs int
}

// AddFlags implements Flagger.AddFlags.
func (a *AdmissionOptions) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&a.MaxAllowedCIDRs, "maxAllowedCIDRs", 50, "maximum number of allowed CIDRs per cluster")
}

// Complete implements Completer.Complete.
func (a *AdmissionOptions) Complete() error {
	return nil
}

// Completed returns the completed Config. Only call this if `Complete` was successful.
func (a *AdmissionOptions) Completed() *AdmissionOptions {
	return a
}

// Apply sets the values of this Config in the given config.ControllerConfiguration.
func (a *AdmissionOptions) Apply(config *controllerconfig.Config) {
	config.MaxAllowedCIDRs = a.MaxAllowedCIDRs
}
