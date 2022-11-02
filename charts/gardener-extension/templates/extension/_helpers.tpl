{{- define "gardener-extension-acl.extensionName" -}}
{{ include "gardener-extension-acl.name" . }}-extension
{{- end -}}

{{/*
extension common labels
*/}}
{{- define "gardener-extension-acl.extensionLabels" -}}
{{ include "gardener-extension-acl.labels" . }}
app.kubernetes.io/component: extension
{{- end }}

{{/*
extension selector labels
*/}}
{{- define "gardener-extension-acl.extensionSelectorLabels" -}}
{{ include "gardener-extension-acl.selectorLabels" . }}
app.kubernetes.io/component: extension
{{- end }}