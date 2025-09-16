# ovn-chart

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v24.03.0](https://img.shields.io/badge/AppVersion-v24.03.0-informational?style=flat-square)

A Helm chart for OVN

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| dpu.ovnController.args[0] | string | `"-c"` |  |
| dpu.ovnController.args[1] | string | `"ovn-controller --pidfile --log-file=/var/log/ovn/ovn-controller.log && tail -f /dev/null"` |  |
| dpu.ovnController.command[0] | string | `"/bin/bash"` |  |
| dpu.ovnController.enabled | bool | `false` |  |
| dpu.ovnController.fullnameOverride | string | `""` |  |
| dpu.ovnController.hostOpenVSwitchDir | string | `"/var/run/openvswitch"` |  |
| dpu.ovnController.hostOvnLogDir | string | `"/var/log/ovn"` |  |
| dpu.ovnController.hostOvnRunDir | string | `"/var/run/ovn"` |  |
| dpu.ovnController.image.pullPolicy | string | `"IfNotPresent"` |  |
| dpu.ovnController.image.repository | string | `"example.com/ovn"` |  |
| dpu.ovnController.image.tag | string | `"v0.1.0"` |  |
| dpu.ovnController.imagePullSecrets | list | `[]` |  |
| dpu.ovnController.nameOverride | string | `""` |  |
| exposedPorts.labels | object | `{}` |  |
| exposedPorts.ports.ovnnb | bool | `false` |  |
| exposedPorts.ports.ovnsb | bool | `false` |  |
| imagePullSecrets | list | `[]` |  |
| management.ovnCentral.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| management.ovnCentral.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| management.ovnCentral.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| management.ovnCentral.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| management.ovnCentral.args[0] | string | `"-c"` |  |
| management.ovnCentral.args[1] | string | `"/usr/share/ovn/scripts/ovn-ctl --db-nb-create-insecure-remote=yes --db-sb-create-insecure-remote=yes start_northd && tail -f /dev/null"` |  |
| management.ovnCentral.command[0] | string | `"/bin/bash"` |  |
| management.ovnCentral.enabled | bool | `false` |  |
| management.ovnCentral.fullnameOverride | string | `""` |  |
| management.ovnCentral.hostOvnLibDir | string | `"/var/lib/ovn"` |  |
| management.ovnCentral.image.pullPolicy | string | `"IfNotPresent"` |  |
| management.ovnCentral.image.repository | string | `"example.com/ovn"` |  |
| management.ovnCentral.image.tag | string | `"v0.1.0"` |  |
| management.ovnCentral.imagePullSecrets | list | `[]` |  |
| management.ovnCentral.nameOverride | string | `""` |  |
| management.ovnCentral.replicaCount | int | `1` |  |
| management.ovnCentral.service.ovnnb.nodePort | int | `30641` |  |
| management.ovnCentral.service.ovnnb.port | int | `6641` |  |
| management.ovnCentral.service.ovnnb.protocol | string | `"TCP"` |  |
| management.ovnCentral.service.ovnnb.targetPort | int | `6641` |  |
| management.ovnCentral.service.ovnsb.nodePort | int | `30642` |  |
| management.ovnCentral.service.ovnsb.port | int | `6642` |  |
| management.ovnCentral.service.ovnsb.protocol | string | `"TCP"` |  |
| management.ovnCentral.service.ovnsb.targetPort | int | `6642` |  |
| management.ovnCentral.tolerations[0].effect | string | `"NoSchedule"` |  |
| management.ovnCentral.tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| management.ovnCentral.tolerations[0].operator | string | `"Exists"` |  |
| management.ovnCentral.tolerations[1].effect | string | `"NoSchedule"` |  |
| management.ovnCentral.tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| management.ovnCentral.tolerations[1].operator | string | `"Exists"` |  |
| serviceDaemonSet.annotations | object | `{}` |  |
| serviceDaemonSet.labels | object | `{}` |  |
| serviceDaemonSet.resources | object | `{}` |  |
| serviceDaemonSet.updateStrategy | object | `{}` |  |

