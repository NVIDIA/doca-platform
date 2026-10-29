# servicechainset-controller

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| chart.chart | string | `""` |  |
| chart.repoURL | string | `""` |  |
| chart.version | string | `""` |  |
| controllerManager.manager.args[0] | string | `"--leader-elect"` |  |
| controllerManager.manager.args[1] | string | `"--leader-election-namespace=$(POD_NAMESPACE)"` |  |
| controllerManager.manager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| controllerManager.manager.containerSecurityContext.capabilities.drop[0] | string | `"ALL"` |  |
| controllerManager.manager.image.repository | string | `"example.com/dpf-system"` |  |
| controllerManager.manager.image.tag | string | `"v0.1.0"` |  |
| controllerManager.manager.resources.limits.cpu | string | `"500m"` |  |
| controllerManager.manager.resources.limits.memory | string | `"128Mi"` |  |
| controllerManager.manager.resources.requests.cpu | string | `"10m"` |  |
| controllerManager.manager.resources.requests.memory | string | `"64Mi"` |  |
| controllerManager.replicas | int | `1` |  |
| controllerManager.serviceAccount.annotations | object | `{}` |  |
| dpucluster.name | string | `""` |  |
| dpucluster.namespace | string | `""` |  |
| env[0].name | string | `"POD_NAMESPACE"` |  |
| env[0].valueFrom.fieldRef.apiVersion | string | `"v1"` |  |
| env[0].valueFrom.fieldRef.fieldPath | string | `"metadata.namespace"` |  |
| env[1].name | string | `"KUBERNETES_SERVICE_HOST"` |  |
| env[1].valueFrom.secretKeyRef.key | string | `"KUBERNETES_SERVICE_HOST"` |  |
| env[1].valueFrom.secretKeyRef.name | string | `"servicechainset-controller-manager-credentials"` |  |
| env[2].name | string | `"KUBERNETES_SERVICE_PORT"` |  |
| env[2].valueFrom.secretKeyRef.key | string | `"KUBERNETES_SERVICE_PORT"` |  |
| env[2].valueFrom.secretKeyRef.name | string | `"servicechainset-controller-manager-credentials"` |  |
| forDPUCluster | bool | `false` |  |
| imagePullSecrets | list | `[]` |  |
| kubernetesClusterDomain | string | `"cluster.local"` |  |
| volumeMounts[0].mountPath | string | `"/var/run/secrets/kubernetes.io/serviceaccount"` |  |
| volumeMounts[0].name | string | `"tokenfile"` |  |
| volumeMounts[0].readOnly | bool | `true` |  |
| volumes[0].name | string | `"tokenfile"` |  |
| volumes[0].projected.sources[0].secret.items[0].key | string | `"TOKEN_FILE"` |  |
| volumes[0].projected.sources[0].secret.items[0].path | string | `"token"` |  |
| volumes[0].projected.sources[0].secret.name | string | `"servicechainset-controller-manager-credentials"` |  |
| volumes[0].projected.sources[1].secret.items[0].key | string | `"KUBERNETES_CA_DATA"` |  |
| volumes[0].projected.sources[1].secret.items[0].path | string | `"ca.crt"` |  |
| volumes[0].projected.sources[1].secret.name | string | `"servicechainset-controller-manager-credentials"` |  |

