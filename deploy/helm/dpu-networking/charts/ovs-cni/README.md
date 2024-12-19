# ovs-cni

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| arm64.nodeSelector."kubernetes.io/arch" | string | `"arm64"` |  |
| arm64.nodeSelector."kubernetes.io/os" | string | `"linux"` |  |
| arm64.ovsCniMarker.args[0] | string | `"-v"` |  |
| arm64.ovsCniMarker.args[1] | string | `"3"` |  |
| arm64.ovsCniMarker.args[2] | string | `"-logtostderr"` |  |
| arm64.ovsCniMarker.args[3] | string | `"-node-name"` |  |
| arm64.ovsCniMarker.args[4] | string | `"$(NODE_NAME)"` |  |
| arm64.ovsCniMarker.args[5] | string | `"-ovs-socket"` |  |
| arm64.ovsCniMarker.args[6] | string | `"unix:/host/var/run/openvswitch/db.sock"` |  |
| arm64.ovsCniMarker.args[7] | string | `"-healthcheck-interval=60"` |  |
| arm64.ovsCniMarker.containerSecurityContext.privileged | bool | `true` |  |
| arm64.ovsCniMarker.image.repository | string | `"example.com/ovs-cni-plugin"` |  |
| arm64.ovsCniMarker.image.tag | string | `"v0.1.0"` |  |
| arm64.ovsCniMarker.imagePullPolicy | string | `"Always"` |  |
| arm64.ovsCniMarker.resources.requests.cpu | string | `"10m"` |  |
| arm64.ovsCniMarker.resources.requests.memory | string | `"10Mi"` |  |
| arm64.ovsCniPlugin.args[0] | string | `"cp /ovs /host/opt/cni/bin/ovs && cp /ovs-mirror-producer /host/opt/cni/bin/ovs-mirror-producer && cp /ovs-mirror-consumer /host/opt/cni/bin/ovs-mirror-consumer\n"` |  |
| arm64.ovsCniPlugin.containerSecurityContext.privileged | bool | `true` |  |
| arm64.ovsCniPlugin.image.repository | string | `"example.com/ovs-cni-plugin"` |  |
| arm64.ovsCniPlugin.image.tag | string | `"v0.1.0"` |  |
| arm64.ovsCniPlugin.imagePullPolicy | string | `"Always"` |  |
| arm64.ovsCniPlugin.resources.requests.cpu | string | `"10m"` |  |
| arm64.ovsCniPlugin.resources.requests.memory | string | `"15Mi"` |  |
| imagePullSecrets | list | `[]` |  |
| kubernetesClusterDomain | string | `"cluster.local"` |  |
| marker.serviceAccount.annotations | object | `{}` |  |

