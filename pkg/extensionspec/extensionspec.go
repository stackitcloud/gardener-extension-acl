package extensionspec

import "github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"

// ExtensionSpec is the content of the ProviderConfig of the acl extension object
type ExtensionSpec struct {
	// Rule contain the user-defined Access Control Rule
	Rule *envoyfilters.ACLRule `json:"rule"`
}
