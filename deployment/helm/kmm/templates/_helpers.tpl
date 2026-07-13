{{/*
Name prefix for all resources, mirrors kustomize namePrefix.
*/}}
{{- define "kmm.namePrefix" -}}
{{- .Values.namePrefix -}}
{{- end }}

{{/*
Common labels applied to all resources.
*/}}
{{- define "kmm.labels" -}}
{{ toYaml .Values.labels }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels (subset of common labels used in matchLabels).
*/}}
{{- define "kmm.selectorLabels" -}}
{{ toYaml .Values.labels }}
{{- end }}
