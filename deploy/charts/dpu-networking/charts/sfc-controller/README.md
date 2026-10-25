# sfc-controller

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| controllerManager.manager.args | list | `[]` |  |
| controllerManager.manager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| controllerManager.manager.containerSecurityContext.capabilities.drop[0] | string | `"ALL"` |  |
| controllerManager.manager.image.repository | string | `"example.com/dpf-system"` |  |
| controllerManager.manager.image.tag | string | `"v0.1.0"` |  |
| controllerManager.manager.ports.health | int | `61181` |  |
| controllerManager.manager.ports.metrics | int | `61180` |  |
| controllerManager.manager.resources.limits.memory | string | `"512Mi"` |  |
| controllerManager.manager.resources.requests.cpu | string | `"100m"` |  |
| controllerManager.manager.resources.requests.memory | string | `"512Mi"` |  |
| controllerManager.manager.secureFlowDeletionTimeout | string | `"0s"` | Zero indicates feature is disabled (default). Set a non zero value to timeout duration and enable this feature. Kubernetes can cause the pod to restart earlier under connectivity constraints in which case flows are always cleaned up under this mode. |
| controllerManager.serviceAccount.annotations | object | `{}` |  |
| dpuLinkerCachePath | string | `""` |  |
| dpuOptLibraryPath | string | `""` |  |
| global.serviceDaemonSet.updateStrategy.type | string | `"RollingUpdate"` | DaemonSet update strategy. The DPUService controller supplies rollout settings when managed by DPF. |
| imagePullSecrets | list | `[]` |  |
| openvSwitchBinDir | string | `"/usr/bin"` |  |
| openvSwitchRunDir | string | `"/var/run/openvswitch"` |  |
| openvSwitchSharedLibrary64Dir | string | `""` |  |
| openvSwitchSharedLibraryDir | string | `"/lib"` |  |
| tracing | object | `{"caPEM":"","dpuCollector":false,"endpoint":"","recordIdleReconciles":false,"recordUnchangedReconciles":false,"rolloutTimeout":"","transport":""}` | tracing configures the sfc-controller's own OTLP trace export. When DPFOperatorConfig spec.monitoring.openTelemetryCollector.tracing is set, the operator points it at the DPU cluster opentelemetry-collector Service, which forwards the spans to the management cluster. |
| tracing.caPEM | string | `""` | caPEM is a PEM-encoded CA certificate bundle used to verify the endpoint's TLS certificate. If empty, TLS endpoints are verified against the system CA pool. |
| tracing.dpuCollector | bool | `false` | dpuCollector sends the spans to the DPU cluster opentelemetry-collector Service of the same cluster. It is ignored when endpoint is set. |
| tracing.endpoint | string | `""` | endpoint is the OTLP endpoint to export trace spans to. If empty and dpuCollector is false, tracing is disabled. |
| tracing.recordIdleReconciles | bool | `false` | recordIdleReconciles exports the spans of idle reconciles. By default they are skipped. |
| tracing.recordUnchangedReconciles | bool | `false` | recordUnchangedReconciles exports the spans of reconciles that changed nothing on their object, did not fail and have no child spans. By default they are skipped. |
| tracing.rolloutTimeout | string | `""` | rolloutTimeout is how long a change may take to become ready before its rollout span is exported as timed out, for example "30m". "0s" turns the timeout off. If empty, the controller's default of 1h is used. |
| tracing.transport | string | `""` | transport is the OTLP transport used to export trace spans: "http" (default) or "grpc". |

