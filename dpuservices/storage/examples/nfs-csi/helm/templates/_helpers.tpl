{{/*
Expand the name of the chart.
*/}}
{{- define "nfs-csi-controller.name" -}}
{{- default "nfs-csi-controller" .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "nfs-csi-controller.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "nfs-csi-controller" .Values.nameOverride }}
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
{{- define "nfs-csi-controller.chart" -}}
{{- printf "%s-%s" "nfs-csi-controller" .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nfs-csi-controller.labels" -}}
helm.sh/chart: {{ include "nfs-csi-controller.chart" . }}
{{ include "nfs-csi-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nfs-csi-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nfs-csi-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
