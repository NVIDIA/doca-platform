{{/*
Expand the name of the chart.
*/}}
{{- define "nfs-storage-vendor-dpu-plugin.name" -}}
{{- default "nfs-storage-vendor-dpu-plugin" .Values.dpu.nfsStorageVendorDpuPlugin.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "nfs-storage-vendor-dpu-plugin.fullname" -}}
{{- if .Values.dpu.nfsStorageVendorDpuPlugin.fullnameOverride }}
{{- .Values.dpu.nfsStorageVendorDpuPlugin.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "nfs-storage-vendor-dpu-plugin" .Values.dpu.nfsStorageVendorDpuPlugin.nameOverride }}
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
{{- define "nfs-storage-vendor-dpu-plugin.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nfs-storage-vendor-dpu-plugin.labels" -}}
helm.sh/chart: {{ include "nfs-storage-vendor-dpu-plugin.chart" . }}
{{ include "nfs-storage-vendor-dpu-plugin.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nfs-storage-vendor-dpu-plugin.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nfs-storage-vendor-dpu-plugin.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "nfs-storage-vendor-dpu-plugin.serviceAccountName" -}}
{{- printf "%s-sa" (include "nfs-storage-vendor-dpu-plugin.fullname" .) }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "nfs-storage-vendor-dpu-plugin.roleName" -}}
{{- printf "%s-role" (include "nfs-storage-vendor-dpu-plugin.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "nfs-storage-vendor-dpu-plugin.roleBindingName" -}}
{{- printf "%s-role-binding" (include "nfs-storage-vendor-dpu-plugin.fullname" .) }}
{{- end }}