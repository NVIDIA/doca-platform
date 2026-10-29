# dummydpuservice-chart

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

Dummydpuservice chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| exposedPorts.labels | object | `{}` |  |
| exposedPorts.ports | object | `{}` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `""` |  |
| image.tag | string | `""` |  |
| imagePullSecrets | list | `[]` |  |
| podSecurityContext | object | `{}` |  |
| securityContext | object | `{}` |  |
| serviceDaemonSet.annotations | object | `{}` |  |
| serviceDaemonSet.labels | object | `{}` |  |
| serviceDaemonSet.nodeSelector | string | `nil` |  |
| serviceDaemonSet.resources | object | `{}` |  |
| serviceDaemonSet.updateStrategy | object | `{}` |  |
| tolerations | list | `[]` |  |

