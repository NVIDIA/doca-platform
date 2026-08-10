# opentelemetry-collector

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.157.0](https://img.shields.io/badge/AppVersion-0.157.0-informational?style=flat-square)

OpenTelemetry Collector for DPU nodes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"otel/opentelemetry-collector-contrib"` |  |
| image.tag | string | `"0.157.0@sha256:f2f01157055a9b2aab9df7118e1f1c9abf345e99b23bc7a2bc791db374a7d0f6"` |  |
| imagePullSecrets | list | `[]` |  |
| logging.endpoint | string | `""` | endpoint where the DPU cluster collector sends logs to the management cluster. This MUST be set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.logging.endpoint. Example: "http://10.0.110.1:30050" (management cluster node IP with NodePort). |
| metrics.endpoint | string | `""` | endpoint where the DPU cluster collector sends metrics to the management cluster. This MUST be set via DPFOperatorConfig.spec.monitoring.openTelemetryCollector.metrics.endpoint. Example: "http://10.0.110.1:30050" (management cluster node IP with NodePort). |
| resources.limits.cpu | string | `"200m"` |  |
| resources.limits.memory | string | `"512Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.memory | string | `"256Mi"` |  |
| serviceDaemonSet.labels | object | `{}` |  |

