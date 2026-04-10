{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "nvidia-k8s-ipam.name" -}}
{{- default .Chart.Name .Values.nvIpam.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "nvidia-k8s-ipam.fullname" -}}
{{- if .Values.nvIpam.fullnameOverride }}
{{- .Values.nvIpam.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nvIpam.nameOverride }}
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
{{- define "nvidia-k8s-ipam.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nvidia-k8s-ipam.labels" -}}
helm.sh/chart: {{ include "nvidia-k8s-ipam.chart" . }}
{{ include "nvidia-k8s-ipam.selectorLabels" . }}
{{- with (.Values.global.serviceDaemonSet).labels }}
{{ toYaml . }}
{{- end }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nvidia-k8s-ipam.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nvidia-k8s-ipam.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
annotations
*/}}
{{- define "nvidia-k8s-ipam.annotations" -}}
{{- with (.Values.global.serviceDaemonSet).annotations }}
{{- toYaml . }}
{{- end }}
{{- end }}
