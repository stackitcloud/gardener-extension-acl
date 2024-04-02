package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	v1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/tidwall/gjson"
	"gomodules.xyz/jsonpatch/v2"
	istionetworkingClientGo "istio.io/client-go/pkg/apis/networking/v1alpha3"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/stackitcloud/gardener-extension-acl/pkg/controller"
	"github.com/stackitcloud/gardener-extension-acl/pkg/envoyfilters"
	"github.com/stackitcloud/gardener-extension-acl/pkg/extensionspec"
	"github.com/stackitcloud/gardener-extension-acl/pkg/helper"
)

const (
	// ShootFilterPrefix is prefix from shoot technicalID
	ShootFilterPrefix = "shoot--"
	// ExtensionName contains the ACl extension name.
	ExtensionName = "acl"
)

const allowedReasonNoPatchNecessary = "No patch necessary"

// EnvoyFilterWebhook is a service struct that defines functions to handle
// admission requests for EnvoyFilters.
type EnvoyFilterWebhook struct {
	Client                 client.Client
	Decoder                *admission.Decoder
	AdditionalAllowedCIDRs []string
}

// Handle receives incoming admission requests for EnvoyFilters and returns a
// response. The response contains patches for specific EnvoyFilters, which need
// to be patched for the ACL extension to be able to control all filter chains.
//
//nolint:gocritic // the signature is forced by kubebuilder
func (e *EnvoyFilterWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	filter := &istionetworkingClientGo.EnvoyFilter{}
	if err := e.Decoder.Decode(req, filter); err != nil {
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
		return admission.Allowed(allowedReasonNoPatchNecessary)
	}

	aclExtension := &extensionsv1alpha1.Extension{}
	err := e.Client.Get(ctx, types.NamespacedName{Name: ExtensionName, Namespace: filter.Name}, aclExtension)

	if client.IgnoreNotFound(err) != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// if an error occured or the extension is in deletion, just allow without
	// introducing any patches
	if err != nil || !aclExtension.DeletionTimestamp.IsZero() {
		return admission.Response{
			AdmissionResponse: admissionv1.AdmissionResponse{
				Allowed: true,
			},
		}
	}

	extSpec := &extensionspec.ExtensionSpec{}
	if err := json.Unmarshal(aclExtension.Spec.ProviderConfig.Raw, extSpec); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if err := controller.ValidateExtensionSpec(extSpec); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	cluster, err := helper.GetClusterForExtension(ctx, e.Client, aclExtension)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	var alwaysAllowedCIDRs []string
	var shootSpecificCIRDs []string

	alwaysAllowedCIDRs = append(alwaysAllowedCIDRs, helper.GetSeedSpecificAllowedCIDRs(cluster.Seed)...)

	if len(e.AdditionalAllowedCIDRs) >= 1 {
		alwaysAllowedCIDRs = append(alwaysAllowedCIDRs, e.AdditionalAllowedCIDRs...)
	}

	// Gardener supports workerless Shoots. These don't have an associated
	// Infrastructure object and don't need Node- or Pod-specific CIDRs to be
	// allowed. Therefore, skip these steps for workerless Shoots.
	if !v1beta1helper.IsWorkerless(cluster.Shoot) {
		shootSpecificCIRDs = append(shootSpecificCIRDs, helper.GetShootNodeSpecificAllowedCIDRs(cluster.Shoot)...)
		shootSpecificCIRDs = append(shootSpecificCIRDs, helper.GetShootPodSpecificAllowedCIDRs(cluster.Shoot)...)

		infra, err := helper.GetInfrastructureForExtension(ctx, e.Client, aclExtension, cluster.Shoot.Name)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		providerSpecificCIRDs, err := helper.GetProviderSpecificAllowedCIDRs(infra)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		shootSpecificCIRDs = append(shootSpecificCIRDs, providerSpecificCIRDs...)
	}

	originalFilter := gjson.Get(originalObjectJSON, `spec.configPatches.0.patch.value.filters.#(name="envoy.filters.network.tcp_proxy")`)
	originalFilterMap := map[string]interface{}{}
	if err := json.Unmarshal([]byte(originalFilter.Raw), &originalFilterMap); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	filterPatch, err := envoyfilters.CreateInternalFilterPatchFromRule(extSpec.Rule, alwaysAllowedCIDRs, shootSpecificCIRDs)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// make sure the original filter is the last
	filterPatches := []map[string]interface{}{filterPatch, originalFilterMap}

	return buildAdmissionResponseWithFilterPatches(filterPatches)
}

func buildAdmissionResponseWithFilterPatches(filters []map[string]interface{}) admission.Response {
	return admission.Response{
		Patches: []jsonpatch.Operation{
			{
				Operation: "replace",
				Path:      "/spec/configPatches/0/patch/value/filters",
				Value:     filters,
			},
		},
		AdmissionResponse: admissionv1.AdmissionResponse{
			Allowed:   true,
			PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
		},
	}
}
