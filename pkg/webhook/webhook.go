package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	extController "github.com/gardener/gardener/extensions/pkg/controller"
	extensions "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/stackitcloud/gardener-extension-acl/pkg/controller"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"github.com/tidwall/gjson"
	"gomodules.xyz/jsonpatch/v2"
	v1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	istionetworkingClientGo "istio.io/client-go/pkg/apis/networking/v1alpha3"
)

const (
	ShootFilterPrefix             = "shoot--"
	ExtensionName                 = "acl"
	AllowedReasonNoPatchNecessary = "No patch necessary"
)

type Config struct {
	// AdditionalAllowedCidrs additional allowed cidrs that will be added to the list of allowed cidrs.
	AdditionalAllowedCidrs []string
}

type EnvoyFilterWebhook struct {
	Client             client.Client
	EnvoyFilterService envoyfilters.EnvoyFilterService
	decoder            *admission.Decoder
	WebhookConfig      Config
}

//nolint:gocritic // the signature is forced by kubebuilder
func (e *EnvoyFilterWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	filter := &istionetworkingClientGo.EnvoyFilter{}
	if err := e.decoder.Decode(req, filter); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return e.createAdmissionResponse(ctx, filter, string(req.Object.Raw))
}

func (e *EnvoyFilterWebhook) createAdmissionResponse(
	ctx context.Context,
	filter *istionetworkingClientGo.EnvoyFilter,
	originalObjectJSON string,
) admission.Response {
	// filter out envoyfilters that are not managed by this webhook
	if !strings.HasPrefix(filter.Name, ShootFilterPrefix) {
		return admission.Allowed(AllowedReasonNoPatchNecessary)
	}

	aclExtension := &extensions.Extension{}
	err := e.Client.Get(ctx, types.NamespacedName{Name: ExtensionName, Namespace: filter.Name}, aclExtension)

	if client.IgnoreNotFound(err) != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	filters := []map[string]interface{}{}

	// patch envoyfilter according to rules
	// otherwhise just the original filter will be applied
	if err == nil && aclExtension.DeletionTimestamp.IsZero() {
		extSpec := &controller.ExtensionSpec{}
		if err := json.Unmarshal(aclExtension.Spec.ProviderConfig.Raw, extSpec); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		if err := controller.ValidateExtensionSpec(extSpec); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		cluster, err := extController.GetCluster(ctx, e.Client, filter.Name)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		alwaysAllowedCIDRs := []string{
			*cluster.Shoot.Spec.Networking.Nodes,
			*cluster.Shoot.Spec.Networking.Pods,
			cluster.Seed.Spec.Networks.Pods,
			*cluster.Seed.Spec.Networks.Nodes,
		}

		if len(e.WebhookConfig.AdditionalAllowedCidrs) >= 1 {
			alwaysAllowedCIDRs = append(alwaysAllowedCIDRs, e.WebhookConfig.AdditionalAllowedCidrs...)
		}

		infra := &extensions.Infrastructure{}
		namespacedName := types.NamespacedName{
			Namespace: filter.Name,
			Name:      cluster.Shoot.Name,
		}

		if err := e.Client.Get(ctx, namespacedName, infra); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		if err := controller.ConfigureProviderSpecificAllowedCIDRs(infra, &alwaysAllowedCIDRs); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		filter, err := e.EnvoyFilterService.CreateInternalFilterPatchFromRule(extSpec.Rule, alwaysAllowedCIDRs)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		filters = append(filters, filter)
	}

	originalFilter := gjson.Get(originalObjectJSON, `spec.configPatches.0.patch.value.filters.#(name="envoy.filters.network.tcp_proxy")`)
	originalFilterMap := map[string]interface{}{}
	if err := json.Unmarshal([]byte(originalFilter.Raw), &originalFilterMap); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// make sure the original filter is the last
	filters = append(filters, originalFilterMap)

	pt := v1.PatchTypeJSONPatch
	return admission.Response{
		Patches: []jsonpatch.Operation{
			{
				Operation: "replace",
				Path:      "/spec/configPatches/0/patch/value/filters",
				Value:     filters,
			},
		},
		AdmissionResponse: v1.AdmissionResponse{
			Allowed:   true,
			PatchType: &pt,
		},
	}
}

func (e *EnvoyFilterWebhook) InjectDecoder(d *admission.Decoder) error {
	e.decoder = d
	return nil
}
