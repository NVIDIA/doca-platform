---
title: "Grafana Dashboards"
---

# DPF Grafana Dashboards

The DPF operator Helm chart packages Grafana dashboards as ConfigMaps that are deployed alongside the operator.
The dashboard JSON definitions are located in the Helm chart under [`deploy/charts/dpf-operator/dashboards/`](https://github.com/NVIDIA/doca-platform/tree/public-main/deploy/charts/dpf-operator/dashboards).

[TOC]

## Deployment

When `grafanaDashboards.enabled` is set to `true` in the Helm values, the chart creates two ConfigMaps:

* `dpf-operator-grafana-dashboards`: contains the fleet health, DPU detail, framework state, and DPUCluster control plane performance dashboards. The ConfigMap carries the annotation `grafana_folder: "DOCA Platform Framework"` so the Grafana sidecar groups all of these dashboards in a dedicated **DOCA Platform Framework** folder.
* `dpf-operator-grafana-debug-dashboards`: contains the controller-runtime and framework performance dashboards. The annotation `grafana_folder: "DOCA Platform Framework/debug"` nests them in a `debug` subfolder under **DOCA Platform Framework**.

Both ConfigMaps are labeled with `grafana_dashboard: "1"`, which allows the Grafana sidecar to automatically discover and load them. No manual import is required, once Grafana is running with the sidecar enabled, the dashboards appear automatically.

## Dashboards

### DPU Fleet Health

Provides a high-level overview of the entire DPU fleet including provisioning state, health conditions, DPUService readiness (desired, healthy, not-ready replicas), and cluster status.

[View Dashboard JSON](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/charts/dpf-operator/dashboards/doca-platform-framework-dpu-fleet-health.json)

### DPU Detail

Provides detailed per-DPU information including resource utilization, networking, service status, and the list of pods scheduled on the selected DPU node (with DPUService attribution and readiness).

[View Dashboard JSON](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/charts/dpf-operator/dashboards/doca-platform-framework-dpu-detail.json)

### Framework State

Tracks the state of DPF custom resources across the cluster including DPUSets, DPUServices (with per-service node count), DPUDeployments, and provisioning objects.

[View Dashboard JSON](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/charts/dpf-operator/dashboards/doca-platform-framework-state.json)

### DPUCluster Control Plane Performance

Surfaces the health and performance of the Kubernetes control plane running inside a DPUCluster: API server request rate and latency, inflight requests, active watch connections, controller-manager workqueue depth and latency, scheduler pending pods and attempt outcomes, and etcd request latency and storage size. Select the target DPUCluster via the `cluster` variable at the top of the dashboard.

[View Dashboard JSON](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/charts/dpf-operator/dashboards/doca-platform-framework-dpu-control-plane-performance.json)

### Framework Performance

Monitors condition and phase transition performance of DPF custom resources such as DPUs, DPUServices, DPUClusters, and others.

[View Dashboard JSON](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/charts/dpf-operator/dashboards/debug/doca-platform-framework-performance.json)
