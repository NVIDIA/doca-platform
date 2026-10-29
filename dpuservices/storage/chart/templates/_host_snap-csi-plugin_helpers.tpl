{{/*
Expand the name of the chart.
*/}}
{{- define "snap-csi-plugin.name" -}}
{{- default "snap-csi-plugin" .Values.host.snapCsiPlugin.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "snap-csi-plugin.fullname" -}}
{{- if .Values.host.snapCsiPlugin.fullnameOverride }}
{{- .Values.host.snapCsiPlugin.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "snap-csi-plugin" .Values.host.snapCsiPlugin.nameOverride }}
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
{{- define "snap-csi-plugin.chart" -}}
{{- printf "%s-%s" "snap-csi-plugin" .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "snap-csi-plugin.labels" -}}
helm.sh/chart: {{ include "snap-csi-plugin.chart" . }}
{{ include "snap-csi-plugin.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "snap-csi-plugin.selectorLabels" -}}
app.kubernetes.io/name: {{ include "snap-csi-plugin.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "snap-csi-plugin.serviceAccountName" -}}
{{- printf "%s-sa" (include "snap-csi-plugin.fullname" .) }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "snap-csi-plugin.roleName" -}}
{{- printf "%s-role" (include "snap-csi-plugin.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "snap-csi-plugin.roleBindingName" -}}
{{- printf "%s-role-binding" (include "snap-csi-plugin.fullname" .) }}
{{- end }}
