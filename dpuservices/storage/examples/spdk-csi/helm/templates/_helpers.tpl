{{/*
Expand the name of the chart.
*/}}
{{- define "spdk-csi-controller.name" -}}
{{- default "spdk-csi-controller" .Values.dpu.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "spdk-csi-controller.fullname" -}}
{{- if .Values.dpu.fullnameOverride }}
{{- .Values.dpu.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "spdk-csi-controller" .Values.dpu.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "spdk-csi-controller.chart" -}}
{{- printf "%s-%s" "spdk-csi-controller" .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "spdk-csi-controller.labels" -}}
helm.sh/chart: {{ include "spdk-csi-controller.chart" . }}
{{ include "spdk-csi-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "spdk-csi-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "spdk-csi-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
