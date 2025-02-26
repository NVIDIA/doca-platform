# dummydpuservice-chart

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

Dummydpuservice chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"example.com/dummydpuservice"` |  |
| image.tag | string | `"v0.1.0"` |  |
| imagePullSecrets | list | `[]` |  |
| podSecurityContext | object | `{}` |  |
| resources | object | `{}` |  |
| securityContext | object | `{}` |  |
| serviceDaemonSet.annotations | object | `{}` |  |
| serviceDaemonSet.labels | object | `{}` |  |
| serviceDaemonSet.updateStrategy | object | `{}` |  |
| serviceID | string | `""` |  |
| tolerations | list | `[]` |  |

