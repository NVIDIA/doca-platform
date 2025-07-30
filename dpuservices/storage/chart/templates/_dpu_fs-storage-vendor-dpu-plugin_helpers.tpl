{{/*
Expand the name of the chart.
*/}}
{{- define "fs-storage-vendor-dpu-plugin.name" -}}
{{- default "fs-storage-vendor-dpu-plugin" .Values.dpu.fsStorageVendorDpuPlugin.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "fs-storage-vendor-dpu-plugin.fullname" -}}
{{- if .Values.dpu.fsStorageVendorDpuPlugin.fullnameOverride }}
{{- .Values.dpu.fsStorageVendorDpuPlugin.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "fs-storage-vendor-dpu-plugin" .Values.dpu.fsStorageVendorDpuPlugin.nameOverride }}
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
{{- define "fs-storage-vendor-dpu-plugin.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "fs-storage-vendor-dpu-plugin.labels" -}}
helm.sh/chart: {{ include "fs-storage-vendor-dpu-plugin.chart" . }}
{{ include "fs-storage-vendor-dpu-plugin.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "fs-storage-vendor-dpu-plugin.selectorLabels" -}}
app.kubernetes.io/name: {{ include "fs-storage-vendor-dpu-plugin.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "fs-storage-vendor-dpu-plugin.serviceAccountName" -}}
{{- printf "%s-sa" (include "fs-storage-vendor-dpu-plugin.fullname" .) }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "fs-storage-vendor-dpu-plugin.roleName" -}}
{{- printf "%s-role" (include "fs-storage-vendor-dpu-plugin.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "fs-storage-vendor-dpu-plugin.roleBindingName" -}}
{{- printf "%s-role-binding" (include "fs-storage-vendor-dpu-plugin.fullname" .) }}
{{- end }} 