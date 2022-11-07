{{- define "gardener-extension.webhookName" -}}
{{ include "gardener-extension.name" . }}-mutating-webhook
{{- end -}}

{{/*
webhook common labels
*/}}
{{- define "gardener-extension.webhookLabels" -}}
{{ include "gardener-extension.labels" . }}
app.kubernetes.io/component: webhook
{{- end }}

{{/*
webhook selector labels
*/}}
{{- define "gardener-extension.webhookSelectorLabels" -}}
{{ include "gardener-extension.selectorLabels" . }}
app.kubernetes.io/component: webhook
{{- end }}