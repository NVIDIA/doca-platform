{{/*
Expand the name of the chart.
*/}}
{{- define "snap-controller.name" -}}
{{- default "snap-controller" .Values.host.snapController.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "snap-controller.fullname" -}}
{{- if .Values.host.snapController.fullnameOverride }}
{{- .Values.host.snapController.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "snap-controller" .Values.host.snapController.nameOverride }}
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
{{- define "snap-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "snap-controller.labels" -}}
helm.sh/chart: {{ include "snap-controller.chart" . }}
{{ include "snap-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "snap-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "snap-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "snap-controller.roleName" -}}
{{- printf "%s-role" (include "snap-controller.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "snap-controller.roleBindingName" -}}
{{- printf "%s-role-binding" (include "snap-controller.fullname" .) }}
{{- end }}
