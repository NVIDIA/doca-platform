{{/*
Expand the name of the chart.
*/}}
{{- define "vpc-ovn-controller.name" -}}
{{- default "vpc-ovn-controller" .Values.host.vpcOVNController.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "vpc-ovn-controller.fullname" -}}
{{- if .Values.host.vpcOVNController.fullnameOverride }}
{{- .Values.host.vpcOVNController.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "vpc-ovn-controller" .Values.host.vpcOVNController.nameOverride }}
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
{{- define "vpc-ovn-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "vpc-ovn-controller.labels" -}}
helm.sh/chart: {{ include "vpc-ovn-controller.chart" . }}
{{ include "vpc-ovn-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "vpc-ovn-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "vpc-ovn-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "vpc-ovn-controller.serviceAccountName" -}}
{{- printf "%s-sa" (include "vpc-ovn-controller.fullname" .) }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "vpc-ovn-controller.roleName" -}}
{{- printf "%s-role" (include "vpc-ovn-controller.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "vpc-ovn-controller.roleBindingName" -}}
{{- printf "%s-role-binding" (include "vpc-ovn-controller.fullname" .) }}
{{- end }}
