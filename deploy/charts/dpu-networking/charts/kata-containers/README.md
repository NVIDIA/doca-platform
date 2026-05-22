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
| image.tag | string | `"3.30.0@sha256:b31cf13addbaf49af9e211bf6ab38335299015a754e2bc0341aa4ba47d8cb395"` |  |
| imagePullSecrets | list | `[]` |  |
| nodeSelector | object | `{}` |  |
| serviceAccount.annotations | object | `{}` |  |
| shims.qemu.enabled | bool | `true` |  |

