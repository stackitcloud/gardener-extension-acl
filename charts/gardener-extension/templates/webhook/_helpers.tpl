{{- define "gardener-extension-acl.webhookName" -}}
{{ include "gardener-extension-acl.name" . }}-mutating-webhook
{{- end -}}

{{/*
webhook common labels
*/}}
{{- define "gardener-extension-acl.webhookLabels" -}}
{{ include "gardener-extension-acl.labels" . }}
app.kubernetes.io/component: webhook
{{- end }}

{{/*
webhook selector labels
*/}}
{{- define "gardener-extension-acl.webhookSelectorLabels" -}}
{{ include "gardener-extension-acl.selectorLabels" . }}
app.kubernetes.io/component: webhook
{{- end }}