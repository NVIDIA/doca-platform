# nvidia-k8s-ipam

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.3.5](https://img.shields.io/badge/AppVersion-0.3.5-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| cniBinDir | string | `"/opt/cni/bin"` |  |
| cniConfDir | string | `"/etc/cni/net.d"` |  |
| imagePullSecrets | list | `[]` |  |
| nvIpam.controller.resources.limits.memory | string | `"300Mi"` |  |
| nvIpam.controller.resources.requests.cpu | string | `"100m"` |  |
| nvIpam.controller.resources.requests.memory | string | `"300Mi"` |  |
| nvIpam.fullnameOverride | string | `""` |  |
| nvIpam.image.repository | string | `"ghcr.io/mellanox/nvidia-k8s-ipam"` |  |
| nvIpam.image.tag | string | `"v0.4.0@sha256:4b984dd345754e28ff964a209d76bff71ed31bb392b25aa5ddccf3925ac4e2ce"` |  |
| nvIpam.nameOverride | string | `""` |  |
| nvIpam.node.resources.limits.cpu | string | `"300m"` |  |
| nvIpam.node.resources.limits.memory | string | `"400Mi"` |  |
| nvIpam.node.resources.requests.cpu | string | `"100m"` |  |
| nvIpam.node.resources.requests.memory | string | `"50Mi"` |  |
| nvIpam.pullPolicy | string | `"IfNotPresent"` |  |

