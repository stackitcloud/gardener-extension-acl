package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	istionetworking "istio.io/client-go/pkg/apis/networking/v1alpha3"
)

type EnvoyFilterWebhook struct {
	Client  client.Client
	decoder *admission.Decoder
}

func (e *EnvoyFilterWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	// decode envoyfilter
	filter := &istionetworking.EnvoyFilter{}
	if err := e.decoder.Decode(req, filter); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	fmt.Printf("Called")

	filter.Annotations["test-me-hard"] = "tonight"

	// check if extension object of type acl exists for this shoots namespace

	// if no: goodbye

	// if yes: check for extension provider config and extract rules

	// mutate filter with rules

	// marshal filter
	marshaled, err := json.Marshal(filter)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}

func (e *EnvoyFilterWebhook) InjectDecoder(d *admission.Decoder) error {
	e.decoder = d
	return nil
}
