# node-problem-detector

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v1.36.0](https://img.shields.io/badge/AppVersion-v1.36.0-informational?style=flat-square)

Node-problem-detector for DPU nodes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"registry.k8s.io/node-problem-detector/node-problem-detector"` |  |
| image.tag | string | `"v1.36.0@sha256:b4db5a168c5f6192c3fb3d650920d192d83776598fcdfcc092d16ca8dc9213ff"` |  |
| imagePullSecrets | list | `[]` |  |
| resources.limits.cpu | string | `"200m"` |  |
| resources.limits.memory | string | `"256Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.memory | string | `"128Mi"` |  |
| serviceDaemonSet.labels | object | `{}` |  |

