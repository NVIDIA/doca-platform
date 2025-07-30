{{/*
Expand the name of the chart.
*/}}
{{- define "block-storage-vendor-dpu-plugin.name" -}}
{{- default "block-storage-vendor-dpu-plugin" .Values.dpu.blockStorageVendorDpuPlugin.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "block-storage-vendor-dpu-plugin.fullname" -}}
{{- if .Values.dpu.blockStorageVendorDpuPlugin.fullnameOverride }}
{{- .Values.dpu.blockStorageVendorDpuPlugin.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "block-storage-vendor-dpu-plugin" .Values.dpu.blockStorageVendorDpuPlugin.nameOverride }}
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
{{- define "block-storage-vendor-dpu-plugin.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "block-storage-vendor-dpu-plugin.labels" -}}
helm.sh/chart: {{ include "block-storage-vendor-dpu-plugin.chart" . }}
{{ include "block-storage-vendor-dpu-plugin.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "block-storage-vendor-dpu-plugin.selectorLabels" -}}
app.kubernetes.io/name: {{ include "block-storage-vendor-dpu-plugin.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "block-storage-vendor-dpu-plugin.serviceAccountName" -}}
{{- printf "%s-sa" (include "block-storage-vendor-dpu-plugin.fullname" .) }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "block-storage-vendor-dpu-plugin.roleName" -}}
{{- printf "%s-role" (include "block-storage-vendor-dpu-plugin.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "block-storage-vendor-dpu-plugin.roleBindingName" -}}
{{- printf "%s-role-binding" (include "block-storage-vendor-dpu-plugin.fullname" .) }}
{{- end }} 