# dpu-monitoring

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

RBAC allowing Prometheus to scrape the DPU cluster control plane metrics endpoints

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| deployDPUManifests | bool | `false` |  |
| rbac.serviceAccountName | string | `"dpu-monitoring"` |  |

