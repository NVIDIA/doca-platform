{{/*
Expand the name of the chart.
*/}}
{{- define "servicechain.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "servicechain.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
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
{{- define "servicechain.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "servicechain.labels" -}}
helm.sh/chart: {{ include "servicechain.chart" . }}
{{ include "servicechain.selectorLabels" . }}
{{- with (.Values.global.serviceDaemonSet).labels }}
{{ toYaml . }}
{{- end }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
dpu.nvidia.com/component: servicechainset-controller-manager
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "servicechain.selectorLabels" -}}
app.kubernetes.io/name: {{ include "servicechain.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
annotations
*/}}
{{- define "servicechain.annotations" -}}
{{- with (.Values.global.serviceDaemonSet).annotations }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{/*
The args to be passed to the manager
*/}}
{{- define "servicechain.manager.args" -}}
{{- $args := .Values.controllerManager.manager.args }}
{{- if .Values.tracing.endpoint }}
{{- $args = append $args (printf "--otel-tracing-endpoint=%s" .Values.tracing.endpoint) }}
{{- with .Values.tracing.transport }}
{{- $args = append $args (printf "--otel-tracing-transport=%s" .) }}
{{- end }}
{{- with .Values.tracing.caPEM }}
{{- $args = append $args (printf "--otel-tracing-ca-pem=%s" .) }}
{{- end }}
{{- if .Values.tracing.recordIdleReconciles }}
{{- $args = append $args "--otel-tracing-record-idle-reconciles=true" }}
{{- end }}
{{- if .Values.tracing.recordUnchangedReconciles }}
{{- $args = append $args "--otel-tracing-record-unchanged-reconciles=true" }}
{{- end }}
{{- if hasKey .Values.tracing "samplingPercent" }}
{{- $args = append $args (printf "--otel-tracing-sampling-percent=%d" (int .Values.tracing.samplingPercent)) }}
{{- end }}
{{- with .Values.tracing.rolloutTimeout }}
{{- $args = append $args (printf "--otel-tracing-rollout-timeout=%s" .) }}
{{- end }}
{{- end }}
{{- toYaml $args }}
{{- end }}
