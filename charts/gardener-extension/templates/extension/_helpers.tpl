{{- define "gardener-extension.extensionName" -}}
{{ include "gardener-extension.name" . }}-extension
{{- end -}}

{{/*
extension common labels
*/}}
{{- define "gardener-extension.extensionLabels" -}}
{{ include "gardener-extension.labels" . }}
app.kubernetes.io/component: extension
{{- end }}

{{/*
extension selector labels
*/}}
{{- define "gardener-extension.extensionSelectorLabels" -}}
{{ include "gardener-extension.selectorLabels" . }}
app.kubernetes.io/component: extension
{{- end }}