# ovs-helper

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| imagePullSecrets | list | `[]` |  |
| openvSwitchBinDir | string | `"/usr/bin"` |  |
| openvSwitchRunDir | string | `"/var/run/openvswitch"` |  |
| openvSwitchSharedLibraryDir | string | `"/lib"` |  |
| ovsHelper.image.repository | string | `"example.com/dpf-system"` |  |
| ovsHelper.image.tag | string | `"v0.1.0"` |  |

