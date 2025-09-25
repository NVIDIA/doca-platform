{{/*
Expand the name of the chart.
*/}}
{{- define "snap-node-driver.name" -}}
{{- default "snap-node-driver" .Values.dpu.snapNodeDriver.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "snap-node-driver.fullname" -}}
{{- if .Values.dpu.snapNodeDriver.fullnameOverride }}
{{- .Values.dpu.snapNodeDriver.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "snap-node-driver" .Values.dpu.snapNodeDriver.nameOverride }}
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
{{- define "snap-node-driver.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "snap-node-driver.labels" -}}
helm.sh/chart: {{ include "snap-node-driver.chart" . }}
{{ include "snap-node-driver.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "snap-node-driver.selectorLabels" -}}
app.kubernetes.io/name: {{ include "snap-node-driver.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "snap-node-driver.serviceAccountName" -}}
{{- printf "%s-sa" (include "snap-node-driver.fullname" .) }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "snap-node-driver.roleName" -}}
{{- printf "%s-role" (include "snap-node-driver.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "snap-node-driver.roleBindingName" -}}
{{- printf "%s-role-binding" (include "snap-node-driver.fullname" .) }}
{{- end }}
