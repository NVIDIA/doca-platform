# mock-dms

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for mock-dms

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| certIPAddresses | list | `[]` |  |
| controllerManager.manager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| controllerManager.manager.containerSecurityContext.capabilities.drop[0] | string | `"ALL"` |  |
| controllerManager.manager.image.repository | string | `"example.com/dpf-system"` |  |
| controllerManager.manager.image.tag | string | `"v0.1.0"` |  |
| controllerManager.replicas | int | `1` |  |
| controllerManager.serviceAccount.annotations | object | `{}` |  |
| imagePullSecrets | list | `[]` |  |
| kubernetesClusterDomain | string | `"cluster.local"` |  |
| tolerations[0].effect | string | `"NoSchedule"` |  |
| tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| tolerations[0].operator | string | `"Exists"` |  |
| tolerations[1].effect | string | `"NoSchedule"` |  |
| tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| tolerations[1].operator | string | `"Exists"` |  |

