---
title: "Grafana Dashboards"
---

# DPF Grafana Dashboards

The DPF operator Helm chart packages Grafana dashboards as ConfigMaps that are deployed alongside the operator.
The dashboard JSON definitions are located in the Helm chart under [`deploy/charts/dpf-operator/dashboards/`](https://github.com/NVIDIA/doca-platform/tree/public-release-v26.4/deploy/charts/dpf-operator/dashboards).

[[_TOC_]]

## Deployment

When `grafanaDashboards.enabled` is set to `true` in the Helm values, the chart creates two ConfigMaps:

* `dpf-operator-grafana-dashboards`: contains the fleet health, DPU detail, and framework state dashboards
* `dpf-operator-grafana-debug-dashboards`: contains the framework performance dashboard, placed in the `debug` Grafana folder

Both ConfigMaps are labeled with `grafana_dashboard: "1"`, which allows the Grafana sidecar to automatically discover and load them. No manual import is required — once Grafana is running with the sidecar enabled, the dashboards appear automatically.

## Dashboards

### DPU Fleet Health

Provides a high-level overview of the entire DPU fleet including provisioning state, health conditions, and cluster status.

[View Dashboard JSON](https://github.com/NVIDIA/doca-platform/blob/public-release-v26.4/deploy/charts/dpf-operator/dashboards/doca-platform-dpu-fleet-health.json)

### DPU Detail

Provides detailed per-DPU information including resource utilization, networking, and service status.

[View Dashboard JSON](https://github.com/NVIDIA/doca-platform/blob/public-release-v26.4/deploy/charts/dpf-operator/dashboards/doca-platform-dpu-detail.json)

### Framework State

Tracks the state of DPF custom resources across the cluster including DPUSets, DPUServices, and provisioning objects.

[View Dashboard JSON](https://github.com/NVIDIA/doca-platform/blob/public-release-v26.4/deploy/charts/dpf-operator/dashboards/doca-platform-framework-state.json)

### Framework Performance

Monitors condition and phase transition performance of DPF custom resources such as DPUs, DPUServices, DPUClusters, and others.

[View Dashboard JSON](https://github.com/NVIDIA/doca-platform/blob/public-release-v26.4/deploy/charts/dpf-operator/dashboards/debug/doca-platform-framework-performance.json)
