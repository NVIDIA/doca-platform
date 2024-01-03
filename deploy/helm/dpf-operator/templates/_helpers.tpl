{{/*
Expand the name of the chart.
*/}}
{{- define "dpf-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "dpf-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "dpf-operator.labels" -}}
helm.sh/chart: {{ include "dpf-operator.chart" . }}
{{ include "dpf-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "dpf-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dpf-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

