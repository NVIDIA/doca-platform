# ovs-cni

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| arm64.image.repository | string | `"example.com/ovs-cni-plugin"` |  |
| arm64.image.tag | string | `"v0.1.0"` |  |
| arm64.nodeSelector."kubernetes.io/arch" | string | `"arm64"` |  |
| arm64.nodeSelector."kubernetes.io/os" | string | `"linux"` |  |
| arm64.ovsCniPlugin.args[0] | string | `"cp /ovs /host/opt/cni/bin/.ovs.tmp && \\\nmv /host/opt/cni/bin/.ovs.tmp /host/opt/cni/bin/ovs && \\\nexec sleep infinity\n"` |  |
| arm64.ovsCniPlugin.containerSecurityContext.privileged | bool | `true` |  |
| arm64.ovsCniPlugin.imagePullPolicy | string | `"IfNotPresent"` |  |
| arm64.ovsCniPlugin.resources.requests.cpu | string | `"10m"` |  |
| arm64.ovsCniPlugin.resources.requests.memory | string | `"15Mi"` |  |
| cniBinDir | string | `"/opt/cni/bin"` |  |
| imagePullSecrets | list | `[]` |  |
| kubernetesClusterDomain | string | `"cluster.local"` |  |

