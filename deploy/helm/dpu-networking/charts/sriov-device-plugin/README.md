# sriov-device-plugin

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| imagePullSecrets | list | `[]` |  |
| kubeSriovDevicePlugin.kubeSriovdp.args[0] | string | `"--log-dir=sriovdp"` |  |
| kubeSriovDevicePlugin.kubeSriovdp.args[1] | string | `"--log-level=10"` |  |
| kubeSriovDevicePlugin.kubeSriovdp.containerSecurityContext.privileged | bool | `true` |  |
| kubeSriovDevicePlugin.kubeSriovdp.image.repository | string | `"ghcr.io/k8snetworkplumbingwg/sriov-network-device-plugin"` |  |
| kubeSriovDevicePlugin.kubeSriovdp.image.tag | string | `"v3.6.2"` |  |
| kubeSriovDevicePlugin.kubeSriovdp.imagePullPolicy | string | `"IfNotPresent"` |  |
| kubeSriovDevicePlugin.kubeSriovdp.resources.limits.cpu | string | `"1"` |  |
| kubeSriovDevicePlugin.kubeSriovdp.resources.limits.memory | string | `"200Mi"` |  |
| kubeSriovDevicePlugin.kubeSriovdp.resources.requests.cpu | string | `"250m"` |  |
| kubeSriovDevicePlugin.kubeSriovdp.resources.requests.memory | string | `"40Mi"` |  |
| kubernetesClusterDomain | string | `"cluster.local"` |  |
| sriovDevicePlugin.serviceAccount.annotations | object | `{}` |  |
| sriovdpConfig.configJson | string | `"{\n    \"resourceList\": [{\n        \"resourceName\": \"bf_sf\",\n        \"resourcePrefix\": \"nvidia.com\",\n        \"deviceType\": \"auxNetDevice\",\n        \"selectors\": [{\n            \"vendors\": [\"15b3\"],\n            \"pfNames\": [\"p0#1-100\"],\n            \"auxTypes\": [\"sf\"]\n        }]\n    }]\n}"` |  |

