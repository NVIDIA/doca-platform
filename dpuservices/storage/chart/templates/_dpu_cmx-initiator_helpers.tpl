{{/*
Expand the name of the chart.
*/}}
{{- define "cmx-initiator.name" -}}
{{- default "cmx-initiator" .Values.dpu.cmxInitiator.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "cmx-initiator.fullname" -}}
{{- if .Values.dpu.cmxInitiator.fullnameOverride }}
{{- .Values.dpu.cmxInitiator.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "cmx-initiator" .Values.dpu.cmxInitiator.nameOverride }}
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
{{- define "cmx-initiator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "cmx-initiator.labels" -}}
helm.sh/chart: {{ include "cmx-initiator.chart" . }}
{{ include "cmx-initiator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "cmx-initiator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cmx-initiator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Directory of the SNAP provider served by the component. The snap-node-driver and the storage vendor DPU plugins
reach the service through the RPC socket in this directory, and the plugins mount volumes for it under it.
*/}}
{{- define "cmx-initiator.providerDir" -}}
{{- $provider := toString (required "dpu.cmxInitiator.providerName is required" .Values.dpu.cmxInitiator.providerName) }}
{{- if not (regexMatch "^[a-z0-9-]+$" $provider) }}
{{- fail (printf "dpu.cmxInitiator.providerName %q must contain only lower case letters, digits and '-'" $provider) }}
{{- end }}
{{- printf "/var/lib/nvidia/storage/snap/providers/%s" $provider }}
{{- end }}

{{/*
Path of the SNAP RPC socket
*/}}
{{- define "cmx-initiator.socketPath" -}}
{{- printf "%s/snap.sock" (include "cmx-initiator.providerDir" .) }}
{{- end }}

{{/*
Arguments of the service. The chart passes the RPC socket of the provider with -s, and appArgs must not set -s, otherwise
the service listens elsewhere and the snap-node-driver and the storage vendor DPU plugins cannot reach it.
*/}}
{{- define "cmx-initiator.appArgs" -}}
{{- $appArgs := toString (.Values.dpu.cmxInitiator.appArgs | default "") }}
{{- if regexMatch "(^|\\s)-s" $appArgs }}
{{- fail "dpu.cmxInitiator.appArgs must not set -s, the chart passes the RPC socket of providerName" }}
{{- end }}
{{- printf "%s -s %s" $appArgs (include "cmx-initiator.socketPath" .) | trim }}
{{- end }}
