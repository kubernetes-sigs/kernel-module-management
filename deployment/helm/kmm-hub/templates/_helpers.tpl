{{/*
Name prefix for all resources, mirrors kustomize namePrefix.
*/}}
{{- define "kmm-hub.namePrefix" -}}
{{- .Values.namePrefix -}}
{{- end }}

{{/*
Common labels applied to all resources.
*/}}
{{- define "kmm-hub.labels" -}}
{{ toYaml .Values.labels }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels (subset of common labels used in matchLabels).
*/}}
{{- define "kmm-hub.selectorLabels" -}}
{{ toYaml .Values.labels }}
{{- end }}
