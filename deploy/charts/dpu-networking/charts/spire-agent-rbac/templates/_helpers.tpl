{{/*
Expand the name of the chart.
*/}}
{{- define "spire-agent-rbac.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "spire-agent-rbac.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "spire-agent-rbac.labels" -}}
helm.sh/chart: {{ include "spire-agent-rbac.chart" . }}
{{ include "spire-agent-rbac.selectorLabels" . }}
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
{{- define "spire-agent-rbac.selectorLabels" -}}
app.kubernetes.io/name: {{ include "spire-agent-rbac.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
