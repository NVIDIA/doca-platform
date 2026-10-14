# spdk-csi-controller-chart

![Version: 0.0.1](https://img.shields.io/badge/Version-0.0.1-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.0.1](https://img.shields.io/badge/AppVersion-0.0.1-informational?style=flat-square)

A Helm chart for SPDK CSI controller

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| dpu.enabled | bool | `false` |  |
| dpu.rbacRoles.spdkCsiController.create | bool | `true` |  |
| dpu.rbacRoles.spdkCsiController.serviceAccount | string | `"spdk-csi-controller-sa"` |  |
| dpu.storageClass.name | string | `"spdkcsi-sc"` |  |
| dpu.storageClass.secretName | string | `"spdkcsi-secret"` |  |
| dpu.storageClass.secretNamespace | string | `"nvidia-storage"` |  |
| host.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| host.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| host.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| host.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| host.config.dpuClusterSecret | string | `""` |  |
| host.config.targets.nodes[0].name | string | `"localhost"` |  |
| host.config.targets.nodes[0].rpcURL | string | `"http://127.0.0.1:9009"` |  |
| host.config.targets.nodes[0].targetAddr | string | `"127.0.0.1"` |  |
| host.config.targets.nodes[0].targetType | string | `"nvme-tcp"` |  |
| host.enabled | bool | `false` |  |
| host.externalProvisioner.image.repository | string | `"registry.k8s.io/sig-storage/csi-provisioner"` |  |
| host.externalProvisioner.image.tag | string | `"v3.5.0"` |  |
| host.externalProvisioner.pullPolicy | string | `"IfNotPresent"` |  |
| host.externalProvisioner.resources | object | `{}` |  |
| host.externalProvisioner.securityContext.allowPrivilegeEscalation | bool | `false` |  |
| host.externalSnapshotter.image.repository | string | `"registry.k8s.io/sig-storage/csi-snapshotter"` |  |
| host.externalSnapshotter.image.tag | string | `"v6.2.2"` |  |
| host.externalSnapshotter.pullPolicy | string | `"IfNotPresent"` |  |
| host.externalSnapshotter.resources | object | `{}` |  |
| host.externalSnapshotter.securityContext.allowPrivilegeEscalation | bool | `false` |  |
| host.imagePullSecrets | list | `[]` |  |
| host.nodeSelector | object | `{}` |  |
| host.plugin.image.repository | string | `"example.com/spdkcsi"` |  |
| host.plugin.image.tag | string | `"v0.1.0"` |  |
| host.plugin.pullPolicy | string | `"IfNotPresent"` |  |
| host.plugin.resources | object | `{}` |  |
| host.plugin.securityContext.allowPrivilegeEscalation | bool | `false` |  |
| host.podAnnotations | object | `{}` |  |
| host.podLabels | object | `{}` |  |
| host.podSecurityContext | object | `{}` |  |
| host.replicas | int | `1` |  |
| host.tolerations[0].effect | string | `"NoSchedule"` |  |
| host.tolerations[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| host.tolerations[0].operator | string | `"Exists"` |  |
| host.tolerations[1].effect | string | `"NoSchedule"` |  |
| host.tolerations[1].key | string | `"node-role.kubernetes.io/master"` |  |
| host.tolerations[1].operator | string | `"Exists"` |  |

