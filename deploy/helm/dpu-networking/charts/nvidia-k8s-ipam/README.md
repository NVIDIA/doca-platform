# nvidia-k8s-ipam

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.3.5](https://img.shields.io/badge/AppVersion-0.3.5-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| imagePullSecrets | list | `[]` |  |
| nvIpam.fullnameOverride | string | `""` |  |
| nvIpam.image.repository | string | `"ghcr.io/mellanox/nvidia-k8s-ipam"` |  |
| nvIpam.image.tag | string | `"v0.3.5"` |  |
| nvIpam.nameOverride | string | `""` |  |
| nvIpam.pullPolicy | string | `"IfNotPresent"` |  |

