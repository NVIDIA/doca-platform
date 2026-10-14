# opentelemetry-collector

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.157.0](https://img.shields.io/badge/AppVersion-0.157.0-informational?style=flat-square)

OpenTelemetry Collector for DPU nodes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| config | object | `{}` | Collector configuration for this chart release. Set it on the DPUService that installs the dpu-networking chart, at spec.helmChart.values.opentelemetry-collector.config. Accepts a map, rendered as YAML, or a string of raw collector YAML. An empty config keeps the pipelines built from logging, metrics, and tracing. The DaemonSet probes call the health_check extension on port 13133, so a custom config must keep that extension or the collector will not become Ready. The Service exposes 4317 (otlp-grpc), 4318 (otlp-http), and 8888 (metrics). Use extraPorts for any other port the custom config listens on. |
| extraPorts | list | `[]` | Additional ports exposed on the collector container and Service when config listens somewhere other than 4317, 4318, or 8888. Each entry requires name and containerPort. protocol defaults to TCP. servicePort defaults to containerPort. Example: - name: jaeger-grpc   containerPort: 14250   protocol: TCP   servicePort: 14250 |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"otel/opentelemetry-collector-contrib"` |  |
| image.tag | string | `"0.157.0@sha256:f2f01157055a9b2aab9df7118e1f1c9abf345e99b23bc7a2bc791db374a7d0f6"` |  |
| imagePullSecrets | list | `[]` |  |
| logging.caCert | string | `""` | PEM-encoded CA certificate bundle used to verify the endpoint's TLS certificate. Set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.logging.caSecretRef. If empty, TLS endpoints are verified against the system CA pool. |
| logging.endpoint | string | `""` | endpoint where the DPU cluster collector sends logs to the management cluster. This MUST be set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.logging.endpoint. Used when config is empty. Example: "http://10.0.110.1:30050" (management cluster node IP with NodePort). |
| logging.transport | string | `"http"` | OTLP transport used to export to the endpoint: "http" (OTLP/HTTP) or "grpc" (OTLP/gRPC). Set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.logging.transport. |
| metrics.caCert | string | `""` | PEM-encoded CA certificate bundle used to verify the endpoint's TLS certificate. Set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.metrics.caSecretRef. If empty, TLS endpoints are verified against the system CA pool. |
| metrics.endpoint | string | `""` | endpoint where the DPU cluster collector sends metrics to the management cluster. This MUST be set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.metrics.endpoint. Used when config is empty. Example: "http://10.0.110.1:30050" (management cluster node IP with NodePort). |
| metrics.transport | string | `"http"` | OTLP transport used to export to the endpoint: "http" (OTLP/HTTP) or "grpc" (OTLP/gRPC). Set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.metrics.transport. |
| resources.limits.cpu | string | `"200m"` |  |
| resources.limits.memory | string | `"512Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.memory | string | `"256Mi"` |  |
| serviceDaemonSet.labels | object | `{}` |  |
| tracing.caCert | string | `""` | PEM-encoded CA certificate bundle used to verify the endpoint's TLS certificate. Set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.tracing.caSecretRef. If empty, TLS endpoints are verified against the system CA pool. |
| tracing.endpoint | string | `""` | endpoint where the DPU cluster collector forwards traces received from local OTLP producers (e.g. DOCA services on the DPU) to the management cluster. This MUST be set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.tracing.endpoint. Used when config is empty. Example: "http://10.0.110.1:30050" (management cluster node IP with NodePort). |
| tracing.transport | string | `"http"` | OTLP transport used to export to the endpoint: "http" (OTLP/HTTP) or "grpc" (OTLP/gRPC). Set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.tracing.transport. |

