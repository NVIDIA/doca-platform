---
title: "Alert Rule Examples"
---

# DPF Alert Rule Examples

[[_TOC_]]

This page documents the reference `PrometheusRule` manifests covering DPF component health, DPUCluster control plane performance, and DPF operator self-health. The manifests live in [`deploy/helmfiles/prometheus-rules/`](https://github.com/NVIDIA/doca-platform/tree/public-main/deploy/helmfiles/prometheus-rules); tune the thresholds for your environment before enabling paging.

> [!NOTE]
> These rules assume kube-prometheus-stack (or any Prometheus Operator deployment) is installed and configured to discover `PrometheusRule` resources in the `dpf-operator-system` namespace. The manifests already carry the `release: kube-prometheus-stack` label that the default kube-prometheus-stack `ruleSelector` matches; adjust or drop it in `metadata.labels` if your Prometheus instance selects rules differently.

> [!NOTE]
> See [Recording Rule Examples](recording-rules.md) for the companion recording-rule manifests. The `DPFDPUServiceReplicasNotHealthy`, `DPFDPUClusterAPIServerLatencyHigh`, and `DPFDPUClusterEtcdLatencyHigh` alerts evaluate recorded series, so they never fire unless the matching recording rules are deployed as well.

Every rule carries `labels.service: doca-platform-framework` so DPF alerts and series can be filtered downstream (e.g. `ALERTS{service="doca-platform-framework"}` in Prometheus, an Alertmanager route matcher, or a Grafana `alertName: "DPF"` prefix filter).

Suggested threshold defaults, tune per environment:

| Alert | Threshold | Default |
|-------|-----------|---------|
| `DPFDPUClusterAPIServer5xxRate` | 5xx req/s | `1` |
| `DPFDPUClusterAPIServerLatencyHigh` | apiserver p99 (s) | `1` |
| `DPFDPUClusterEtcdLatencyHigh` | etcd p99 (s) | `0.1` |
| `DPFDPUClusterWorkqueueBacklog` | workqueue depth | `100` |
| `DPFControllerReconcileErrors` | reconcile error ratio | `0.1` |
| `DPFControllerReconcileLatencyHigh` | reconcile p99 (s) | `30` |
| `DPFOperatorPodCrashLooping` | restarts in 15m | `3` |
| `DPFDPUServiceCredentialExpiringSoon` | remaining fraction of token lifetime | `0.1` |

## DPF Component Health

`status.conditions.Ready` alerts across the core DPF custom resources, exposed by kube-state-metrics through the `customresourcestate` configuration shipped in the operator chart. Each alert fires when the matching object stops reporting `Ready=True` for the configured duration, covering both `Ready=False` and `Ready=Unknown`. `DPFDPUNotOperational` fires when a provisioned DPU's runtime degrades, and `DPFDPUNodeMaintenanceStuck` catches maintenance windows that fail to converge in 30 minutes. Every expression aggregates with `max by(namespace, name)` so the alert identity only depends on the object; without that, a kube-state-metrics restart would change the scrape labels, reset the `for:` timers, and re-page for ongoing issues.

Manifest: [`dpf-component-health.yaml`](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/helmfiles/prometheus-rules/dpf-component-health.yaml)

| Alert | Severity | For | Fires when |
|-------|----------|-----|------------|
| `DPFOperatorConfigNotReady` | critical | 10m | The singleton DPFOperatorConfig does not report `Ready=True` |
| `DPFDPUNotReady` | warning | 15m | A DPU does not report `Ready=True` |
| `DPFDPUNotOperational` | critical | 5m | A provisioned DPU does not report `OperationalReady=True` |
| `DPFDPUClusterNotReady` | critical | 10m | A DPUCluster does not report `Ready=True` |
| `DPFDPUDeploymentNotReady` | warning | 10m | A DPUDeployment does not report `Ready=True` |
| `DPFDPUSetNotReady` | warning | 10m | A DPUSet does not report `Ready=True` |
| `DPFDPUServiceNotReady` | warning | 10m | A DPUService does not report `Ready=True` |
| `DPFDPUNodeMaintenanceStuck` | warning | 30m | A DPUNodeMaintenance has not reached `NodeEffectApplied=True` |
| `DPFDPUNodeNotReady` | warning | 10m | A DPUNode does not report `Ready=True` |
| `DPFDPUServiceChainNotReady` | warning | 10m | A DPUServiceChain does not report `Ready=True` |
| `DPFDPUServiceInterfaceNotReady` | warning | 10m | A DPUServiceInterface does not report `Ready=True` |
| `DPFDPUServiceIPAMNotReady` | warning | 10m | A DPUServiceIPAM does not report `Ready=True` |
| `DPFDPUVPCNotReady` | warning | 10m | A DPUVPC does not report `Ready=True` |
| `DPFDPUVirtualNetworkNotReady` | warning | 10m | A DPUVirtualNetwork does not report `Ready=True` |
| `DPFDPUVolumeNotReady` | warning | 10m | A DPUVolume does not report `Ready=True` |
| `DPFDPUVolumeAttachmentNotReady` | warning | 10m | A DPUVolumeAttachment does not report `Ready=True` |

## DPF Replica Health

`DPFDPUServiceReplicasNotHealthy` watches the DaemonSet/Deployment desired-vs-ready replica diff per DPUService by reading the `dpf:dpuservice:not_ready_replicas` recorded series. It strictly requires the `dpf-aggregates` manifest from [Recording Rule Examples](recording-rules.md): without it the series does not exist and the alert silently never fires.

Manifest: [`dpf-replica-health.yaml`](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/helmfiles/prometheus-rules/dpf-replica-health.yaml)

| Alert | Severity | For | Fires when |
|-------|----------|-----|------------|
| `DPFDPUServiceReplicasNotHealthy` | warning | 10m | A DPUService workload (DaemonSet or Deployment) has fewer ready replicas than desired |

## DPUCluster Control Plane

Health and performance of the Kubernetes control planes running on top of Kamaji (one per DPUCluster). DPU cluster control planes are scraped under the same job names as the management cluster (`apiserver`, `kube-controller-manager`), so the DPUCluster alerts filter with `cluster!="management"` to avoid firing for the management control plane under a misleading name. The two latency alerts read recorded series and require the `dpucluster-control-plane-aggregates` manifest from [Recording Rule Examples](recording-rules.md).

Manifest: [`dpucluster-control-plane.yaml`](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/helmfiles/prometheus-rules/dpucluster-control-plane.yaml)

| Alert | Severity | For | Fires when |
|-------|----------|-----|------------|
| `DPFDPUClusterAPIServer5xxRate` | warning | 5m | A DPUCluster apiserver returns more than 1 req/s of 5xx responses |
| `DPFDPUClusterAPIServerLatencyHigh` | warning | 10m | A DPUCluster apiserver p99 request latency exceeds 1s per verb |
| `DPFDPUClusterEtcdLatencyHigh` | warning | 10m | A DPUCluster etcd p99 request latency exceeds 0.1s per operation |
| `DPFDPUClusterWorkqueueBacklog` | warning | 10m | A DPUCluster kube-controller-manager workqueue holds more than 100 items |

## DPF Operator Self-Health

Alerts about the DPF operator pods and the metrics pipeline itself. When these fire, expect every other DPF alert to go stale: `DPFOperatorMetricsAbsent` catches the scrape job disappearing entirely (which `up == 0` alone cannot), and `DPFKubeStateMetricsDown` catches the kube-state-metrics outage that silently mutes all component-health and lifecycle alerts. The pod alerts select by the stable `dpf-operator-controller-manager-.*` pod-name prefix, so they work with a default kube-state-metrics install (pod labels are only exported when explicitly allowlisted). The two controller-runtime alerts watch the reconcile behavior of every DPF controller scraped under `job="doca-platform-framework"` on the management cluster.

Manifest: [`dpf-operator-self.yaml`](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/helmfiles/prometheus-rules/dpf-operator-self.yaml)

| Alert | Severity | For | Fires when |
|-------|----------|-----|------------|
| `DPFOperatorPodNotReady` | critical | 5m | The DPF operator pod is not Ready |
| `DPFOperatorPodCrashLooping` | warning | 0m | The DPF operator pod restarts more than 3 times in 15 minutes |
| `DPFOperatorMetricsDown` | critical | 5m | Prometheus cannot scrape a DPF controller-manager pod (`up == 0`) |
| `DPFOperatorMetricsAbsent` | critical | 10m | No scrape targets exist at all for `job="doca-platform-framework"` |
| `DPFKubeStateMetricsDown` | critical | 5m | kube-state-metrics is unreachable or has no scrape targets |
| `DPFControllerReconcileErrors` | warning | 15m | More than 10% of a DPF controller's reconciles fail |
| `DPFControllerReconcileLatencyHigh` | warning | 10m | A DPF controller's p99 reconcile time exceeds 30s |

## DPF Lifecycle Stalls

Long-window alerts that catch DPUs, BFBs, and DPUClusters stuck in lifecycle phases, plus DPUService credentials approaching expiry. `DPFDPUErrorPhase` covers the terminal `Error` phase explicitly: a DPU stays there until fixed or deleted, so it always needs operator attention.

Manifest: [`dpf-lifecycle.yaml`](https://github.com/NVIDIA/doca-platform/blob/public-main/deploy/helmfiles/prometheus-rules/dpf-lifecycle.yaml)

| Alert | Severity | For | Fires when |
|-------|----------|-----|------------|
| `DPFDPUProvisioningStuck` | warning | 1h | A DPU sits in a non-terminal phase (not `Ready`, `Error`, or `Deleting`) |
| `DPFDPUErrorPhase` | warning | 15m | A DPU is in the terminal `Error` phase (provisioning failed) |
| `DPFDPUDeletingStuck` | warning | 1h | A DPU sits in the `Deleting` phase |
| `DPFBFBStuck` | warning | 30m | A BFB sits in the `Downloading` or `Error` phase |
| `DPFDPUClusterPhaseStuck` | warning | 1h | A DPUCluster sits in the `Pending`, `Creating`, `NotReady`, or `Failed` phase |
| `DPFDPUServiceCredentialExpiringSoon` | warning | 15m | A DPUServiceCredentialRequest token has less than 10% of its lifetime left, meaning the automatic rotation (due at 20% remaining) is overdue |

## Deploying

The manifests live in [`deploy/helmfiles/prometheus-rules/`](https://github.com/NVIDIA/doca-platform/tree/public-main/deploy/helmfiles/prometheus-rules). The `deploy/helmfiles/monitoring.yaml` helmfile applies them automatically via a `postsync` hook on the kube-prometheus-stack release. To deploy them into an existing Prometheus Operator setup manually:

```bash
kubectl apply --server-side -f deploy/helmfiles/prometheus-rules/
```

Confirm Prometheus picked the rules up:

```bash
kubectl -n dpf-operator-system port-forward svc/kube-prometheus-stack-prometheus 9090:9090
# then visit http://localhost:9090/alerts and http://localhost:9090/rules
```

Filter active alerts by the common label every rule carries:

```promql
ALERTS{service="doca-platform-framework"}
```
