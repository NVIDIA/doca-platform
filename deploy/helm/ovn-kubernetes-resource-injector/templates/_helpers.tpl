{{/*
Expand the name of the chart.
*/}}
{{- define "ovn-kubernetes-resource-injector.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 50 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 50 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "ovn-kubernetes-resource-injector.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 50 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 50 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 50 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "ovn-kubernetes-resource-injector.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 50 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "ovn-kubernetes-resource-injector.labels" -}}
helm.sh/chart: {{ include "ovn-kubernetes-resource-injector.chart" . }}
{{ include "ovn-kubernetes-resource-injector.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "ovn-kubernetes-resource-injector.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ovn-kubernetes-resource-injector.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
The name of the webhook certificate
*/}}
{{- define "ovn-kubernetes-resource-injector.webhook.certificateName" -}}
{{ include "ovn-kubernetes-resource-injector.fullname" . }}-webhook
{{- end }}

{{/*
The name of the webhook secret that contains the certificate
*/}}
{{- define "ovn-kubernetes-resource-injector.webhook.secretName" -}}
{{ include "ovn-kubernetes-resource-injector.fullname" . }}-webhook-cert
{{- end }}

{{/*
The name of the webhook service
*/}}
{{- define "ovn-kubernetes-resource-injector.webhook.serviceName" -}}
{{ include "ovn-kubernetes-resource-injector.fullname" . }}-webhook
{{- end }}

{{/*
The args to be passed to the webhook
*/}}
{{- define "ovn-kubernetes-resource-injector.webhook.args" -}}
{{- $args := .Values.controllerManager.webhook.args }}
{{- $args = append $args (printf "--nad-namespace=%s" .Release.Namespace) }}
{{- $args = append $args (printf "--nad-name=%s" .Values.nadName) }}
{{- toYaml $args }}
{{- end }}
