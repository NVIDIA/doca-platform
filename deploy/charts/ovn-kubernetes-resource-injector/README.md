# ovn-kubernetes-resource-injector-chart

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| controllerManager.nodeSelector."node-role.kubernetes.io/control-plane" | string | `""` |  |
| controllerManager.replicas | int | `1` |  |
| controllerManager.serviceAccount.annotations | object | `{}` |  |
| controllerManager.tolerations[0].effect | string | `"NoSchedule"` |  |
| controllerManager.tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| controllerManager.tolerations[0].operator | string | `"Exists"` |  |
| controllerManager.tolerations[1].effect | string | `"NoSchedule"` |  |
| controllerManager.tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| controllerManager.tolerations[1].operator | string | `"Exists"` |  |
| controllerManager.webhook.args[0] | string | `"--leader-elect"` |  |
| controllerManager.webhook.command[0] | string | `"/manager"` |  |
| controllerManager.webhook.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| controllerManager.webhook.containerSecurityContext.capabilities.drop[0] | string | `"ALL"` |  |
| controllerManager.webhook.image.repository | string | `"example.com/ovn-kubernetes-resource-injector"` |  |
| controllerManager.webhook.image.tag | string | `"v0.1.0"` |  |
| controllerManager.webhook.resources.limits.cpu | string | `"500m"` |  |
| controllerManager.webhook.resources.limits.memory | string | `"128Mi"` |  |
| controllerManager.webhook.resources.requests.cpu | string | `"10m"` |  |
| controllerManager.webhook.resources.requests.memory | string | `"64Mi"` |  |
| imagePullSecrets | list | `[]` |  |
| nadName | string | `"dpf-ovn-kubernetes"` |  |
| resourceName | string | `""` |  |

