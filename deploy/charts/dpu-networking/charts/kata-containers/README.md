# kata-containers

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

Kata Containers deployment for DPU nodes

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| containerd.configDir | string | `""` |  |
| containerd.configFileName | string | `"config-mlnx.toml"` |  |
| defaultShim | string | `"qemu"` |  |
| image.repository | string | `"quay.io/kata-containers/kata-deploy"` |  |
| image.tag | string | `"3.32.0@sha256:47c4cf29c9e8cf9bb7debab58761b672e1d4c93ee5e92db7b442ee00e7d561cc"` |  |
| imagePullSecrets | list | `[]` |  |
| nodeSelector | object | `{}` |  |
| serviceAccount.annotations | object | `{}` |  |
| shims.qemu.enabled | bool | `true` |  |

