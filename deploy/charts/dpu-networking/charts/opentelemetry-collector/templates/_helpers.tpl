{{/*
Expand the name of the chart.
*/}}
{{- define "opentelemetry-collector.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "opentelemetry-collector.fullname" -}}
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
{{- define "opentelemetry-collector.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "opentelemetry-collector.labels" -}}
helm.sh/chart: {{ include "opentelemetry-collector.chart" . }}
{{ include "opentelemetry-collector.selectorLabels" . }}
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
{{- define "opentelemetry-collector.selectorLabels" -}}
app.kubernetes.io/name: {{ include "opentelemetry-collector.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
annotations
*/}}
{{- define "opentelemetry-collector.annotations" -}}
{{- with (.Values.global.serviceDaemonSet).annotations }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "opentelemetry-collector.serviceAccountName" -}}
{{- include "opentelemetry-collector.fullname" . }}
{{- end }}

{{/*
Name of the DPUService. It can be used for naming resources like a Service, so clients inside a DPUCluster can reach
it by name without knowing the DPUCluster name, which is part of the release name. Only use it in charts of DPUServices
deployed to DPUClusters (spec.deployInCluster: false). On the host cluster it would cause name collisions.
*/}}
{{- define "opentelemetry-collector.serviceName" -}}
{{- required "global.dpf.dpuService.name is set by the DPUService controller" ((.Values.global.dpf).dpuService).name | trunc 63 | trimSuffix "-" }}
{{- end }}
