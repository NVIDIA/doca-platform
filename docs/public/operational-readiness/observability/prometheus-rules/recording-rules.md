---
title: "Recording Rule Examples"
---

# DPF Recording Rule Examples

[[_TOC_]]

Recording rules pre-compute the aggregations that the DPF dashboards and alerts depend on. Prometheus evaluates them on a schedule and stores the results as new time series, so panels and alert expressions can read a single cheap series instead of recomputing a multi-way join on every refresh. The manifests live in [`deploy/helmfiles/prometheus-rules/`](https://github.com/NVIDIA/doca-platform/tree/public-main/deploy/helmfiles/prometheus-rules) together with the alert rules.

The rule groups use a 30s `interval:`, which matches the default scrape interval for the kube-prometheus-stack and keeps recorded series aligned with their underlying samples. Every recorded series carries `labels.service: doca-platform-framework`, so it's filterable downstream.

> [!NOTE]
> The manifests already carry the `release: kube-prometheus-stack` label that the default kube-prometheus-stack `ruleSelector` matches; adjust or drop it in `metadata.labels` if your Prometheus instance selects rules differently.

## DPF Aggregates

Per-CRD ready/total counts and ratios, plus the pre-joined DPUService desired/healthy replica math that the fleet-health dashboard otherwise has to recompute on every refresh. The ready-ratio series feed the "Recording Rule Snapshots" row of the Alerts dashboard, and `dpf:dpuservice:not_ready_replicas` is what the `DPFDPUServiceReplicasNotHealthy` alert evaluates against — deploy this manifest before (or together with) `dpf-replica-health.yaml`, otherwise that alert never fires. Once these are emitted, the fleet-health dashboard's "Desired / Healthy / Not Ready" columns can also be migrated to read `dpf:dpuservice:desired_replicas`, `dpf:dpuservice:healthy_replicas`, and `dpf:dpuservice:not_ready_replicas` directly.

The `:ready` rules fall back to 0 via `or on() vector(0)` so the ratio keeps reporting (as 0) during a full outage instead of going stale, and the `:total` rules count the objects' `_created` series so objects that have not published conditions yet still count towards the denominator. The replica rules join and group on `cluster` as well, so the same DPUService running on several DPU clusters aggregates per cluster instead of failing the join with a many-to-many matching error.

Manifest: [`dpf-aggregates.yaml`](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/helmfiles/prometheus-rules/dpf-aggregates.yaml)

| Record | Records |
|--------|---------|
| `dpf:dpu:ready` | Number of DPUs with `Ready=True` |
| `dpf:dpu:total` | Total number of DPU objects |
| `dpf:dpu:ready_ratio` | Ready DPUs divided by total DPUs |
| `dpf:dpuservice:ready` | Number of DPUServices with `Ready=True` |
| `dpf:dpuservice:total` | Total number of DPUService objects |
| `dpf:dpuservice:ready_ratio` | Ready DPUServices divided by total DPUServices |
| `dpf:dpudeployment:ready` | Number of DPUDeployments with `Ready=True` |
| `dpf:dpudeployment:total` | Total number of DPUDeployment objects |
| `dpf:dpudeployment:ready_ratio` | Ready DPUDeployments divided by total DPUDeployments |
| `dpf:dpucluster:ready` | Number of DPUClusters with `Ready=True` |
| `dpf:dpucluster:total` | Total number of DPUCluster objects |
| `dpf:dpucluster:ready_ratio` | Ready DPUClusters divided by total DPUClusters |
| `dpf:dpuservice:desired_replicas` | Desired workload replicas per DPUService and cluster (DaemonSets and Deployments) |
| `dpf:dpuservice:healthy_replicas` | Ready workload replicas per DPUService and cluster (DaemonSets and Deployments) |
| `dpf:dpuservice:not_ready_replicas` | Desired minus ready replicas per DPUService and cluster |

## DPUCluster Control-Plane Aggregates

Histogram quantiles for API server request duration, etcd request duration, and kube-controller-manager work duration, plus the rolling API request rate, all scoped to DPU cluster control planes via `cluster!="management"`. The two request-duration quantiles are what the `DPFDPUClusterAPIServerLatencyHigh` and `DPFDPUClusterEtcdLatencyHigh` alerts evaluate against — deploy this manifest before (or together with) `dpucluster-control-plane.yaml`. The remaining series are cheap pre-aggregations for ad-hoc queries and custom dashboards.

Manifest: [`dpucluster-control-plane-aggregates.yaml`](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/helmfiles/prometheus-rules/dpucluster-control-plane-aggregates.yaml)

| Record | Records |
|--------|---------|
| `dpf:dpucluster:apiserver_request:rate5m` | apiserver request rate per DPUCluster and verb over 5 minutes |
| `dpf:dpucluster:apiserver_request_duration:p99` | apiserver p99 request duration per DPUCluster and verb |
| `dpf:dpucluster:etcd_request_duration:p99` | etcd p99 request duration per DPUCluster and operation |
| `dpf:dpucluster:workqueue_work_duration:p99` | kube-controller-manager p99 workqueue work duration per DPUCluster and queue |

## Deploying

The manifests live in [`deploy/helmfiles/prometheus-rules/`](https://github.com/NVIDIA/doca-platform/tree/public-main/deploy/helmfiles/prometheus-rules) together with the alert rules, and the `deploy/helmfiles/monitoring.yaml` helmfile applies them automatically via a `postsync` hook on the kube-prometheus-stack release. To deploy them into an existing Prometheus Operator setup manually:

```bash
kubectl apply --server-side -f deploy/helmfiles/prometheus-rules/
```

Confirm the new series exist:

```bash
kubectl -n dpf-operator-system port-forward svc/kube-prometheus-stack-prometheus 9090:9090
curl -sG http://localhost:9090/api/v1/query --data-urlencode 'query=dpf:dpu:ready_ratio'
```

A non-empty `result` array confirms the rule group has been picked up and evaluated at least once.
