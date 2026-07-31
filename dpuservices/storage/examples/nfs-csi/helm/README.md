# nfs-csi-controller-chart

![Version: 0.0.1](https://img.shields.io/badge/Version-0.0.1-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.0.1](https://img.shields.io/badge/AppVersion-0.0.1-informational?style=flat-square)

A Helm chart for NFS CSI controller

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| dpu.enabled | bool | `false` |  |
| dpu.rbacRoles.nfsCsiController.create | bool | `true` |  |
| dpu.rbacRoles.nfsCsiController.serviceAccount | string | `"nfs-csi-controller-sa"` |  |
| dpu.storageClasses | string | `nil` |  |
| host.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| host.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| host.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| host.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| host.config.dpuClusterSecret | string | `""` |  |
| host.dnsPolicy | string | `"ClusterFirstWithHostNet"` |  |
| host.enabled | bool | `false` |  |
| host.externalProvisioner.image.repository | string | `"registry.k8s.io/sig-storage/csi-provisioner"` |  |
| host.externalProvisioner.image.tag | string | `"v5.2.0"` |  |
| host.externalProvisioner.pullPolicy | string | `"IfNotPresent"` |  |
| host.externalProvisioner.resources.limits.memory | string | `"400Mi"` |  |
| host.externalProvisioner.resources.requests.cpu | string | `"10m"` |  |
| host.externalProvisioner.resources.requests.memory | string | `"20Mi"` |  |
| host.externalProvisioner.securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| host.externalResizer.image.repository | string | `"registry.k8s.io/sig-storage/csi-resizer"` |  |
| host.externalResizer.image.tag | string | `"v1.13.1"` |  |
| host.externalResizer.pullPolicy | string | `"IfNotPresent"` |  |
| host.externalResizer.resources.limits.memory | string | `"400Mi"` |  |
| host.externalResizer.resources.requests.cpu | string | `"10m"` |  |
| host.externalResizer.resources.requests.memory | string | `"20Mi"` |  |
| host.externalResizer.securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| host.externalSnapshotter.image.repository | string | `"registry.k8s.io/sig-storage/csi-snapshotter"` |  |
| host.externalSnapshotter.image.tag | string | `"v8.2.0"` |  |
| host.externalSnapshotter.pullPolicy | string | `"IfNotPresent"` |  |
| host.externalSnapshotter.resources.limits.memory | string | `"200Mi"` |  |
| host.externalSnapshotter.resources.requests.cpu | string | `"10m"` |  |
| host.externalSnapshotter.resources.requests.memory | string | `"20Mi"` |  |
| host.externalSnapshotter.securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| host.hostNetwork | bool | `true` |  |
| host.imagePullSecrets | list | `[]` |  |
| host.livenessProbe.image.repository | string | `"registry.k8s.io/sig-storage/livenessprobe"` |  |
| host.livenessProbe.image.tag | string | `"v2.15.0"` |  |
| host.livenessProbe.pullPolicy | string | `"IfNotPresent"` |  |
| host.livenessProbe.resources.limits.memory | string | `"100Mi"` |  |
| host.livenessProbe.resources.requests.cpu | string | `"10m"` |  |
| host.livenessProbe.resources.requests.memory | string | `"20Mi"` |  |
| host.livenessProbe.securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| host.nodeSelector | object | `{}` |  |
| host.plugin.image.repository | string | `"registry.k8s.io/sig-storage/nfsplugin"` |  |
| host.plugin.image.tag | string | `"v4.11.0"` |  |
| host.plugin.pullPolicy | string | `"IfNotPresent"` |  |
| host.plugin.resources.limits.memory | string | `"200Mi"` |  |
| host.plugin.resources.requests.cpu | string | `"10m"` |  |
| host.plugin.resources.requests.memory | string | `"20Mi"` |  |
| host.plugin.securityContext.allowPrivilegeEscalation | bool | `true` |  |
| host.plugin.securityContext.capabilities.add[0] | string | `"SYS_ADMIN"` |  |
| host.plugin.securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| host.plugin.securityContext.privileged | bool | `true` |  |
| host.podAnnotations | object | `{}` |  |
| host.podLabels | object | `{}` |  |
| host.podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| host.replicas | int | `1` |  |
| host.tolerations[0].effect | string | `"NoSchedule"` |  |
| host.tolerations[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| host.tolerations[0].operator | string | `"Exists"` |  |
| host.tolerations[1].effect | string | `"NoSchedule"` |  |
| host.tolerations[1].key | string | `"node-role.kubernetes.io/master"` |  |
| host.tolerations[1].operator | string | `"Exists"` |  |

