# coredns

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

CoreDNS serving a DPU cluster, running on the host cluster

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| deployDPUManifests | bool | `false` | deployDPUManifests controls whether the manifests that are required in the DPUCluster should be deployed. |
| deployHostManifests | bool | `false` | deployHostManifests controls whether the manifests that are required in the host cluster should be deployed. |
| env | list | `[]` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"registry.k8s.io/coredns/coredns"` |  |
| image.tag | string | `"v1.12.1"` |  |
| imagePullSecrets | list | `[]` |  |
| nodeSelector | object | `{}` | nodeSelector narrows the control plane nodes CoreDNS runs on. The operator sets it to the keepalived node selector of the DPUCluster, so CoreDNS follows the VIP it is reached through. |
| port | int | `5353` | port CoreDNS listens on. It is deliberately above 1024 so the container can run unprivileged. The Service maps 53 onto it. |
| rbac.serviceAccounts | list | `[]` |  |
| replicas | int | `2` |  |
| resources.limits.cpu | string | `"500m"` |  |
| resources.limits.memory | string | `"256Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.memory | string | `"128Mi"` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.labels | object | `{}` |  |
| serviceAccount.name | string | `nil` |  |
| upstreamNameservers | string | `"/etc/resolv.conf"` | upstream nameservers used for names outside clusterDomain. Defaults to the host cluster resolver configuration of the node CoreDNS runs on. |
| volumeMounts | list | `[]` |  |
| volumes | list | `[]` |  |

