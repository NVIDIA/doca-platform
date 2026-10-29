{{/*
Expand the name of the chart.
*/}}
{{- define "snap-configuration.name" -}}
{{- default "snap-configuration" .Values.dpu.configuration.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "snap-configuration.fullname" -}}
{{- if .Values.dpu.configuration.fullnameOverride }}
{{- .Values.dpu.configuration.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "snap-configuration" .Values.dpu.configuration.nameOverride }}
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
{{- define "snap-configuration.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "snap-configuration.labels" -}}
helm.sh/chart: {{ include "snap-configuration.chart" . }}
{{ include "snap-configuration.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "snap-configuration.selectorLabels" -}}
app.kubernetes.io/name: {{ include "snap-configuration.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "snap-configuration.serviceAccountName" -}}
{{- printf "%s-sa" (include "snap-configuration.fullname" .) }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "snap-configuration.roleName" -}}
{{- printf "%s-role" (include "snap-configuration.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "snap-configuration.roleBindingName" -}}
{{- printf "%s-role-binding" (include "snap-configuration.fullname" .) }}
{{- end }}
