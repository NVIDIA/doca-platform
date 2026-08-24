# spiffe-csi-driver

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.2.7](https://img.shields.io/badge/AppVersion-0.2.7-informational?style=flat-square)

SPIFFE CSI driver for DPUService workloads to mount the DPU agent Workload API

## Source Code

* <https://github.com/spiffe/helm-charts-hardened/tree/aabc73b1e51976d6115fa1291b71889010ee64f9/charts/spire/charts/spiffe-csi-driver>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"ghcr.io/spiffe/spiffe-csi-driver"` |  |
| image.tag | string | `"0.2.7@sha256:9dfe4f0caff096164fd921aa7f15319804df2fc0325f1d90bc793ef3256ab1cd"` |  |
| nodeDriverRegistrar.image.pullPolicy | string | `"IfNotPresent"` |  |
| nodeDriverRegistrar.image.repository | string | `"registry.k8s.io/sig-storage/csi-node-driver-registrar"` |  |
| nodeDriverRegistrar.image.tag | string | `"v2.9.4@sha256:ab6c4603d8da43d9f33361a5716552080e3f72badfad099d76fcf4b59e547d80"` |  |
| resources.driver.limits.cpu | string | `"100m"` |  |
| resources.driver.limits.memory | string | `"64Mi"` |  |
| resources.driver.requests.cpu | string | `"20m"` |  |
| resources.driver.requests.memory | string | `"32Mi"` |  |
| resources.registrar.limits.cpu | string | `"50m"` |  |
| resources.registrar.limits.memory | string | `"32Mi"` |  |
| resources.registrar.requests.cpu | string | `"10m"` |  |
| resources.registrar.requests.memory | string | `"16Mi"` |  |

