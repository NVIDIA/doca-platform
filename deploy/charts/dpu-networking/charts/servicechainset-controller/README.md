# servicechainset-controller

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| controllerManager.manager.args[0] | string | `"--leader-elect"` |  |
| controllerManager.manager.args[1] | string | `"--leader-election-namespace=$(POD_NAMESPACE)"` |  |
| controllerManager.manager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| controllerManager.manager.containerSecurityContext.capabilities.drop[0] | string | `"ALL"` |  |
| controllerManager.manager.containerSecurityContext.privileged | bool | `false` |  |
| controllerManager.manager.containerSecurityContext.runAsUser | int | `65532` |  |
| controllerManager.manager.image.repository | string | `"example.com/dpf-system"` |  |
| controllerManager.manager.image.tag | string | `"v0.1.0"` |  |
| controllerManager.manager.resources.limits.cpu | string | `"1500m"` |  |
| controllerManager.manager.resources.limits.memory | string | `"2560Mi"` |  |
| controllerManager.manager.resources.requests.cpu | string | `"250m"` |  |
| controllerManager.manager.resources.requests.memory | string | `"512Mi"` |  |
| controllerManager.replicas | int | `2` |  |
| deployDPUManifests | bool | `false` | deployDPUManifests controls whether the manifests that are required in the DPUCluster should be deployed. |
| deployHostManifests | bool | `false` | deployHostManifests controls whether the manifests that are required in the host cluster should be deployed. |
| env | list | `[]` |  |
| imagePullSecrets | list | `[]` |  |
| rbac.serviceAccounts | list | `[]` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.labels | object | `{}` |  |
| serviceAccount.name | string | `nil` |  |
| tracing | object | `{"caPEM":"","endpoint":"","recordIdleReconciles":false,"recordUnchangedReconciles":false,"rolloutTimeout":"","transport":""}` | tracing configures the servicechainset-controller's own OTLP trace export. The operator sets it from DPFOperatorConfig spec.monitoring.openTelemetryCollector.tracing. |
| tracing.caPEM | string | `""` | caPEM is a PEM-encoded CA certificate bundle used to verify the endpoint's TLS certificate. If empty, TLS endpoints are verified against the system CA pool. |
| tracing.endpoint | string | `""` | endpoint is the OTLP endpoint to export trace spans to. If empty, tracing is disabled. |
| tracing.recordIdleReconciles | bool | `false` | recordIdleReconciles exports the spans of idle reconciles. By default they are skipped. |
| tracing.recordUnchangedReconciles | bool | `false` | recordUnchangedReconciles exports the spans of reconciles that changed nothing on their object, did not fail and have no child spans. By default they are skipped. |
| tracing.rolloutTimeout | string | `""` | rolloutTimeout is how long a change may take to become ready before its rollout span is exported as timed out, for example "30m". "0s" turns the timeout off. If empty, the controller's default of 1h is used. |
| tracing.transport | string | `""` | transport is the OTLP transport used to export trace spans: "http" (default) or "grpc". |
| volumeMounts | list | `[]` |  |
| volumes | list | `[]` |  |

