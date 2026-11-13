{{/*
Expand the name of the chart.
*/}}
{{- define "node-problem-detector.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "node-problem-detector.fullname" -}}
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
{{- define "node-problem-detector.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "node-problem-detector.labels" -}}
helm.sh/chart: {{ include "node-problem-detector.chart" . }}
{{ include "node-problem-detector.selectorLabels" . }}
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
{{- define "node-problem-detector.selectorLabels" -}}
app.kubernetes.io/name: {{ include "node-problem-detector.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
annotations
*/}}
{{- define "node-problem-detector.annotations" -}}
{{- with (.Values.global.serviceDaemonSet).annotations }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "node-problem-detector.serviceAccountName" -}}
{{- include "node-problem-detector.fullname" . }}
{{- end }}

{{/*
Name of the DPUService. It can be used for naming resources like a Service, so clients inside a DPUCluster can reach
it by name without knowing the DPUCluster name, which is part of the release name. Only use it in charts of DPUServices
deployed to DPUClusters (spec.deployInCluster: false). On the host cluster it would cause name collisions.
*/}}
{{- define "node-problem-detector.serviceName" -}}
{{- required "global.dpf.dpuService.name is set by the DPUService controller" ((.Values.global.dpf).dpuService).name | trunc 63 | trimSuffix "-" }}
{{- end }}
