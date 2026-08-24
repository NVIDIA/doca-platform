{{/* Use the release name in namespaced resource names as required by DPUService charts. */}}
{{- define "spiffe-csi-driver.fullname" -}}
{{- if contains .Chart.Name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/* Common labels. */}}
{{- define "spiffe-csi-driver.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "spiffe-csi-driver.selectorLabels" . }}
{{- with (dig "serviceDaemonSet" "labels" (dict) (.Values.global | default dict)) }}
{{ toYaml . }}
{{- end }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/* Labels used by the DaemonSet selector. */}}
{{- define "spiffe-csi-driver.selectorLabels" -}}
app.kubernetes.io/name: spiffe-csi-driver
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Annotations passed by the DPUService controller. */}}
{{- define "spiffe-csi-driver.annotations" -}}
{{- with (dig "serviceDaemonSet" "annotations" (dict) (.Values.global | default dict)) }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{/* Use the DPUService resource contract when set, otherwise keep container defaults. */}}
{{- define "spiffe-csi-driver.resources" -}}
{{- if .serviceDaemonSet }}
limits:
  {{- toYaml .serviceDaemonSet | nindent 2 }}
requests:
  {{- toYaml .serviceDaemonSet | nindent 2 }}
{{- else }}
{{- toYaml .defaults }}
{{- end }}
{{- end }}
