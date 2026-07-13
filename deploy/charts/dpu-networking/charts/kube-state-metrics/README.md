# kube-state-metrics

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

Kube-state-metrics for DPU custom resources

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| collectors | list | `["cronjobs","daemonsets","deployments","endpoints","jobs","namespaces","nodes","persistentvolumeclaims","persistentvolumes","pods","replicasets","resourcequotas","services","statefulsets"]` | collectors is the list of resources kube-state-metrics collects state metrics for. The container --resources argument, the ClusterRole rules and the ServiceMonitor metric allowlist are all derived from this list. Only resources from the core, apps and batch API groups are supported; pod metrics are additionally filtered to a low-cardinality allowlist in the ServiceMonitor. |
| deployDPUManifests | bool | `false` | deployDPUManifests controls whether the manifests that are required in the DPUCluster should be deployed. |
| deployHostManifests | bool | `false` | deployHostManifests controls whether the manifests that are required in the host cluster should be deployed. |
| env | list | `[]` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"registry.k8s.io/kube-state-metrics/kube-state-metrics"` |  |
| image.tag | string | `"v2.18.0@sha256:1545919b72e3ae035454fc054131e8d0f14b42ef6fc5b2ad5c751cafa6b2130e"` |  |
| imagePullSecrets | list | `[]` |  |
| rbac.serviceAccounts | list | `[]` |  |
| resources.limits.cpu | string | `"200m"` |  |
| resources.limits.memory | string | `"512Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.memory | string | `"256Mi"` |  |
| serviceAccount.name | string | `nil` |  |
| volumeMounts | list | `[]` |  |
| volumes | list | `[]` |  |

