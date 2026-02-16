# kube-state-metrics

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

Kube-state-metrics for DPU custom resources

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| deployDPUManifests | bool | `false` |  |
| deployHostManifests | bool | `false` |  |
| env | list | `[]` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"registry.k8s.io/kube-state-metrics/kube-state-metrics"` |  |
| image.tag | string | `"v2.18.0"` |  |
| imagePullSecrets | list | `[]` |  |
| rbac.serviceAccounts | list | `[]` |  |
| resources.limits.cpu | string | `"200m"` |  |
| resources.limits.memory | string | `"512Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.memory | string | `"256Mi"` |  |
| serviceAccount.name | string | `nil` |  |
| volumeMounts | list | `[]` |  |
| volumes | list | `[]` |  |

