{{/*
Expand the name of the chart.
*/}}
{{- define "vpc-ovn-node.name" -}}
{{- default "vpc-ovn-node" .Values.dpu.vpcOVNNode.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "vpc-ovn-node.fullname" -}}
{{- if .Values.dpu.vpcOVNNode.fullnameOverride }}
{{- .Values.dpu.vpcOVNNode.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "vpc-ovn-node" .Values.dpu.vpcOVNNode.nameOverride }}
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
{{- define "vpc-ovn-node.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "vpc-ovn-node.labels" -}}
helm.sh/chart: {{ include "vpc-ovn-node.chart" . }}
{{ include "vpc-ovn-node.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "vpc-ovn-node.selectorLabels" -}}
app.kubernetes.io/name: {{ include "vpc-ovn-node.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "vpc-ovn-node.serviceAccountName" -}}
{{- printf "%s-sa" (include "vpc-ovn-node.fullname" .) }}
{{- end }}

{{/*
Create the name of the role to use
*/}}
{{- define "vpc-ovn-node.roleName" -}}
{{- printf "%s-role" (include "vpc-ovn-node.fullname" .) }}
{{- end }}

{{/*
Create the name of the role binding to use
*/}}
{{- define "vpc-ovn-node.roleBindingName" -}}
{{- printf "%s-role-binding" (include "vpc-ovn-node.fullname" .) }}
{{- end }}
