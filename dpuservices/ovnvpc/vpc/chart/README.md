# dpf-vpc-ovn

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for DPF VPC subsystem

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| dpu.vpcOVNNode.containers.dhcpCNIDaemon.enabled | bool | `true` |  |
| dpu.vpcOVNNode.containers.dhcpCNIDaemon.image.pullPolicy | string | `"IfNotPresent"` |  |
| dpu.vpcOVNNode.containers.dhcpCNIDaemon.image.repository | string | `"example.com/vpc-system"` |  |
| dpu.vpcOVNNode.containers.dhcpCNIDaemon.image.tag | string | `"v0.1.0"` |  |
| dpu.vpcOVNNode.containers.vpcOVNNodeController.image.pullPolicy | string | `"IfNotPresent"` |  |
| dpu.vpcOVNNode.containers.vpcOVNNodeController.image.repository | string | `"example.com/vpc-system"` |  |
| dpu.vpcOVNNode.containers.vpcOVNNodeController.image.tag | string | `"v0.1.0"` |  |
| dpu.vpcOVNNode.containers.vpcOVNNodeController.securityContext | object | `{}` |  |
| dpu.vpcOVNNode.enabled | bool | `false` |  |
| dpu.vpcOVNNode.imagePullSecrets | list | `[]` |  |
| dpu.vpcOVNNode.initContainers.allocator.image.pullPolicy | string | `"IfNotPresent"` |  |
| dpu.vpcOVNNode.initContainers.allocator.image.repository | string | `"example.com/vpc-system"` |  |
| dpu.vpcOVNNode.initContainers.allocator.image.tag | string | `"v0.1.0"` |  |
| dpu.vpcOVNNode.initContainers.vpcOVNDpuProvisioner.env.ovnSbEndpoint | string | `"tcp:control_plane_ip_address:30642"` |  |
| dpu.vpcOVNNode.initContainers.vpcOVNDpuProvisioner.image.pullPolicy | string | `"IfNotPresent"` |  |
| dpu.vpcOVNNode.initContainers.vpcOVNDpuProvisioner.image.repository | string | `"example.com/vpc-system"` |  |
| dpu.vpcOVNNode.initContainers.vpcOVNDpuProvisioner.image.tag | string | `"v0.1.0"` |  |
| dpu.vpcOVNNode.ipRequests[0].name | string | `"vtep"` |  |
| dpu.vpcOVNNode.ipRequests[0].poolName | string | `""` |  |
| dpu.vpcOVNNode.ipRequests[1].name | string | `"gateway"` |  |
| dpu.vpcOVNNode.ipRequests[1].poolName | string | `""` |  |
| dpu.vpcOVNNode.podSecurityContext | object | `{}` |  |
| dpu.vpcOVNNode.tolerations | list | `[]` |  |
| dpu.vpcOVNNode.volumes.cniBinDir.path | string | `"/opt/cni/bin"` |  |
| dpu.vpcOVNNode.volumes.dhcpCNIBinDir.path | string | `"/opt/cni/bin"` |  |
| dpu.vpcOVNNode.volumes.dhcpDaemonSocketDir.path | string | `"/run/vpc/cni"` |  |
| dpu.vpcOVNNode.volumes.hostOpenvswitch.path | string | `"/var/run/openvswitch"` |  |
| dpu.vpcOVNNode.volumes.hostOpenvswitchEtc.path | string | `"/etc/openvswitch"` |  |
| dpu.vpcOVNNode.volumes.nvipamDaemonSocketDir.path | string | `"/var/lib/cni/nv-ipam"` |  |
| host.vpcOVNController.affinity | object | `{}` |  |
| host.vpcOVNController.enabled | bool | `false` |  |
| host.vpcOVNController.image.repository | string | `"example.com/vpc-system"` |  |
| host.vpcOVNController.image.tag | string | `"v0.1.0"` |  |
| host.vpcOVNController.imagePullPolicy | string | `"IfNotPresent"` |  |
| host.vpcOVNController.imagePullSecrets | list | `[]` |  |
| host.vpcOVNController.nodeSelector | object | `{}` |  |
| host.vpcOVNController.podAnnotations | object | `{}` |  |
| host.vpcOVNController.podLabels | object | `{}` |  |
| host.vpcOVNController.podSecurityContext | object | `{}` |  |
| host.vpcOVNController.replicas | int | `1` |  |
| host.vpcOVNController.securityContext | object | `{}` |  |
| host.vpcOVNController.tolerations[0].effect | string | `"NoSchedule"` |  |
| host.vpcOVNController.tolerations[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| host.vpcOVNController.tolerations[0].operator | string | `"Exists"` |  |
| host.vpcOVNController.tolerations[1].effect | string | `"NoSchedule"` |  |
| host.vpcOVNController.tolerations[1].key | string | `"node-role.kubernetes.io/master"` |  |
| host.vpcOVNController.tolerations[1].operator | string | `"Exists"` |  |
| imagePullSecrets | list | `[]` |  |
| serviceDaemonSet.annotations | object | `{}` |  |
| serviceDaemonSet.labels | object | `{}` |  |
| serviceDaemonSet.resources | object | `{}` |  |
| serviceDaemonSet.updateStrategy | object | `{}` |  |

