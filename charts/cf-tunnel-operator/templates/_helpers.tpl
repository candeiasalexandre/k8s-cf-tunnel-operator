{{- define "cf-tunnel-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "cf-tunnel-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "cf-tunnel-operator.namespace" -}}
{{- default .Release.Namespace .Values.namespace }}
{{- end }}

{{- define "cf-tunnel-operator.labels" -}}
helm.sh/chart: {{ include "cf-tunnel-operator.name" . }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "cf-tunnel-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "cf-tunnel-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cf-tunnel-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "cf-tunnel-operator.image" -}}
{{- if .Values.image.digest }}
{{- .Values.image.repository }}@{{ .Values.image.digest }}
{{- else }}
{{- .Values.image.repository }}:{{ .Values.image.tag }}
{{- end }}
{{- end }}
