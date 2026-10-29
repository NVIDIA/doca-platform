# dpf-operator

![Version: 25.7.0](https://img.shields.io/badge/Version-25.7.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v25.7.0](https://img.shields.io/badge/AppVersion-v25.7.0-informational?style=flat-square)

DPF Operator manages the lifecycle of a DOCA Platform Framework system.

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| controllerManager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| controllerManager.containerSecurityContext.capabilities.drop[0] | string | `"ALL"` |  |
| controllerManager.image.repository | string | `""` |  |
| controllerManager.image.tag | string | `""` |  |
| controllerManager.replicas | int | `1` |  |
| controllerManager.serviceAccount.annotations | object | `{}` |  |
| enableNodeFeatureRules | bool | `true` |  |
| grafanaDashboards.enabled | bool | `true` |  |
| imagePullSecrets | list | `[]` |  |
| isOpenshift | bool | `false` |  |
| kamajiEtcdDefrag.backoffLimit | int | `6` |  |
| kamajiEtcdDefrag.clientPort | int | `2379` |  |
| kamajiEtcdDefrag.defragRule | string | `"dbQuotaUsage > 0.8 || dbSize - dbSizeInUse > 200*1024*1024"` |  |
| kamajiEtcdDefrag.enabled | bool | `true` |  |
| kamajiEtcdDefrag.image | string | `"ghcr.io/ahrtr/etcd-defrag:v0.22.0"` |  |
| kamajiEtcdDefrag.releaseName | string | `"kamaji-etcd"` |  |
| kamajiEtcdDefrag.replicas | int | `3` |  |
| kamajiEtcdDefrag.schedule | string | `"0 0 * * *"` |  |
| kamajiEtcdDefrag.successfulJobsHistoryLimit | int | `3` |  |
| kubeStateMetricsCRDMetrics.enabled | bool | `true` |  |
| prometheusSecureMetrics.enabled | bool | `true` |  |
| tolerations[0].effect | string | `"NoSchedule"` |  |
| tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| tolerations[0].operator | string | `"Exists"` |  |
| tolerations[1].effect | string | `"NoSchedule"` |  |
| tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| tolerations[1].operator | string | `"Exists"` |  |

