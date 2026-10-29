{{/*
Expand the name of the chart.
*/}}
{{- define "snap-host-controller.name" -}}
{{- default "snap-host-controller" .Values.host.snapHostController.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "snap-host-controller.fullname" -}}
{{- if .Values.host.snapHostController.fullnameOverride }}
{{- .Values.host.snapHostController.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "snap-host-controller" .Values.host.snapHostController.nameOverride }}
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
{{- define "snap-host-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "snap-host-controller.labels" -}}
helm.sh/chart: {{ include "snap-host-controller.chart" . }}
{{ include "snap-host-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "snap-host-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "snap-host-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "snap-host-controller.roleName" -}}
{{- printf "%s-role" (include "snap-host-controller.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "snap-host-controller.roleBindingName" -}}
{{- printf "%s-role-binding" (include "snap-host-controller.fullname" .) }}
{{- end }}
