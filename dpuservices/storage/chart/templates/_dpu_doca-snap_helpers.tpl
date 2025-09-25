{{/*
Expand the name of the chart.
*/}}
{{- define "doca-snap.name" -}}
{{- default "doca-snap" .Values.dpu.docaSnap.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "doca-snap.fullname" -}}
{{- if .Values.dpu.docaSnap.fullnameOverride }}
{{- .Values.dpu.docaSnap.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "doca-snap" .Values.dpu.docaSnap.nameOverride }}
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
{{- define "doca-snap.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "doca-snap.labels" -}}
helm.sh/chart: {{ include "doca-snap.chart" . }}
{{ include "doca-snap.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "doca-snap.selectorLabels" -}}
app.kubernetes.io/name: {{ include "doca-snap.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "doca-snap.serviceAccountName" -}}
{{- printf "%s-sa" (include "doca-snap.fullname" .) }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "doca-snap.roleName" -}}
{{- printf "%s-role" (include "doca-snap.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "doca-snap.roleBindingName" -}}
{{- printf "%s-role-binding" (include "doca-snap.fullname" .) }}
{{- end }}
