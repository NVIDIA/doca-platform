{{/*
The chart contains multiple components but it is designed to deploy single component at once.
We check top-level "enabled" flags for all components to make sure that only one component is enabled.
This is required to avoid misusage of the chart.
*/}}

{{- define "validate" -}}
{{- $enabledCount := 0 }}
{{- if .Values.dpu.docaSnap.enabled }}{{ $enabledCount = add1 $enabledCount }}{{- end }}
{{- if .Values.dpu.snapNodeDriver.enabled }}{{ $enabledCount = add1 $enabledCount }}{{- end }}
{{- if .Values.dpu.blockStorageVendorDpuPlugin.enabled }}{{ $enabledCount = add1 $enabledCount }}{{- end }}
{{- if .Values.dpu.fsStorageVendorDpuPlugin.enabled }}{{ $enabledCount = add1 $enabledCount }}{{- end }}
{{- if .Values.dpu.nfsStorageVendorDpuPlugin.enabled }}{{ $enabledCount = add1 $enabledCount }}{{- end }}
{{- if .Values.host.snapHostController.enabled }}{{ $enabledCount = add1 $enabledCount }}{{- end }}
{{- if .Values.host.snapCsiPlugin.enabled }}{{ $enabledCount = add1 $enabledCount }}{{- end }}
{{- if eq $enabledCount 0 }}
{{- fail "The chart has no enabled components. Ensure that exactly one component is enabled." }}
{{- end }}
{{- if ne $enabledCount 1 }}
{{- fail "The chart has multiple enabled components, which is not supported. Please enable only one component at a time." }}
{{- end }}
{{- end -}}
