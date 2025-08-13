# sfc-controller

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| controllerManager.manager.args[0] | string | `"--leader-elect"` |  |
| controllerManager.manager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| controllerManager.manager.containerSecurityContext.capabilities.drop[0] | string | `"ALL"` |  |
| controllerManager.manager.image.repository | string | `"example.com/dpf-system"` |  |
| controllerManager.manager.image.tag | string | `"v0.1.0"` |  |
| controllerManager.manager.resources.limits.memory | string | `"512Mi"` |  |
| controllerManager.manager.resources.requests.cpu | string | `"100m"` |  |
| controllerManager.manager.resources.requests.memory | string | `"512Mi"` |  |
| controllerManager.manager.secureFlowDeletionTimeout | string | `"0s"` | Zero indicates feature is disabled (default). Set a non zero value to timeout duration and enable this feature. Kubernetes can cause the pod to restart earlier under connectivity constraints in which case flows are always cleaned up under this mode. |
| controllerManager.replicas | int | `1` |  |
| controllerManager.serviceAccount.annotations | object | `{}` |  |
| imagePullSecrets | list | `[]` |  |
| openvSwitchBinDir | string | `"/usr/bin"` |  |
| openvSwitchRunDir | string | `"/var/run/openvswitch"` |  |
| openvSwitchSharedLibrary64Dir | string | `""` |  |
| openvSwitchSharedLibraryDir | string | `"/lib"` |  |

