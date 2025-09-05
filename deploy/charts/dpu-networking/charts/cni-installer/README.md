# cni-installer

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes that installs a daemonset which runs an image that installs CNIs

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| cniInstaller.args | list | `[]` |  |
| cniInstaller.image.repository | string | `"example.com/dpf-system"` |  |
| cniInstaller.image.tag | string | `"v0.1.0"` |  |
| cniInstaller.resources.limits.memory | string | `"128Mi"` |  |
| cniInstaller.resources.requests.cpu | string | `"100m"` |  |
| cniInstaller.resources.requests.memory | string | `"128Mi"` |  |
| cniInstaller.serviceAccount.annotations | object | `{}` |  |
| imagePullSecrets | list | `[]` |  |

