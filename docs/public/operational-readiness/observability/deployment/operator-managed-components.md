---
title: "DPF-Operator-Managed Components"
---

[[_TOC_]]

## Introduction

The components Kube-State-Metrics and Node-Problem-Detector are deployed by default and configured by the DPF operator via the `DPFOperatorConfig.spec.monitoring` field. OpenTelemetry Collector requires additional explicit endpoint configuration.

To disable all DPF-operator-managed monitoring components:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  monitoring:
    disable: true
```

## Kube-State-Metrics

`Kube-State-Metrics` (KSM) exposes metrics about Kubernetes object states. This DPF-operator-managed KSM instance monitors DPU cluster resources only.

For Host Cluster Kubernetes resource metrics, a separate KSM instance must be deployed by the user — see [User-Managed Components](user-managed-components.md#kube-state-metrics).

### Deployment Architecture

KSM is deployed with a split architecture:

1. **Host Cluster Deployment**: A single Deployment that connects remotely to each DPU cluster's API server to collect metrics
2. **DPU Cluster RBAC**: RBAC-only resources on each DPU cluster grant permissions for the Host Cluster KSM

### Monitored DPU Resources

KSM collects metrics for the following DPU custom resources:

**IPAM Resources:**

* `IPPool`: IP address pool status and allocation metrics
* `CIDRPool`: CIDR pool status and allocation metrics

**Service Function Chaining:**

* `ServiceChain`: Service chain status and configuration
* `ServiceChainSet`: Service chain set status
* `ServiceInterface`: Service interface status and health
* `ServiceInterfaceSet`: Service interface set status

**Kubernetes Resources:**

* `Pods`, `Deployments`, `DaemonSets`, and more. See [Kube-State-Metrics documentation](https://github.com/kubernetes/kube-state-metrics/tree/main/docs) for a complete list.

### Configuration

KSM is enabled by default. To disable KSM while keeping other monitoring components enabled:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  monitoring:
    kubeStateMetrics:
      disable: true
```

To customize KSM image and resources:

<details markdown="1"><summary><b>Expand for KSM Configuration Example</b></summary>

```yaml
spec:
  monitoring:
    kubeStateMetrics:
      daemon:
        image: "registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0"
        resources:
          limits:
            cpu: "200m"
            memory: "512Mi"
          requests:
            cpu: "100m"
            memory: "256Mi"
```

</details>

KSM metrics are automatically scraped by Prometheus via ServiceMonitor.

## Node-Problem-Detector

Node-Problem-Detector (NPD) monitors DPU node health and reports problems as Node conditions. It runs as a DaemonSet on each DPU cluster node. The DaemonSet is unprivileged: checks read the host sysfs, host process metadata and the persistent journal through read-only mounts, and the DPU cluster admission policy rejects privileged node-problem-detector pods.

### Health Checks

NPD includes DPU-specific health checks that run every 30 seconds:

| Condition Type          | Check Description                         |
|-------------------------|-------------------------------------------|
| `OVSvSwitchdHealthy`    | Verifies ovs-vswitchd process is running  |
| `OVSDBHealthy`          | Verifies ovsdb-server process is running  |
| `OVSHealthy`            | Checks for OVS process OOM kills          |
| `PFRepresentorsHealthy` | Verifies host PF representors are present |
| `UplinkHealthy`         | Checks physical uplink is operational     |
| `MTUConfigured`         | Validates network MTU configuration       |

Additionally, NPD monitors standard Kubernetes node problems (kernel deadlocks, read-only filesystems, disk pressure, OOM events). DPU mode (embedded vs separated) is enforced at provisioning time by the DPU agent and is not re-checked by NPD, since querying it requires privileged device access.

### Integration with DPU Status

Node conditions from NPD are aggregated into the DPU resource's `operationalConditions` field via the `NodeProblemsReady` condition, providing centralized visibility into node health.

### Configuration

NPD is enabled by default. To disable NPD while keeping other monitoring components enabled:

```yaml
spec:
  monitoring:
    nodeProblemDetector:
      disable: true
```

To customize NPD:

<details markdown="1"><summary><b>Expand for NPD Configuration Example</b></summary>

```yaml
spec:
  monitoring:
    nodeProblemDetector:
      daemon:
        image: "registry.k8s.io/node-problem-detector/node-problem-detector:v1.35.1"
        resources:
          limits:
            cpu: "200m"
            memory: "256Mi"
          requests:
            cpu: "100m"
            memory: "128Mi"
```

</details>

### Monitoring DPU Health

Node-Problem-Detector health checks are aggregated into the DPU's operational status. To monitor DPU health:

```bash
$ kubectl -n dpf-operator-system get dpu
NAME                   READY   OPERATIONAL   PHASE   AGE
worker1-mt2413xz0b67   True    True          Ready   73d
worker2-mt2413xz0b6w   True    True          Ready   73d
```

See [DPU Operational Readiness](../guides/operational-readiness.md) for more details on operational conditions and alerting.

NPD also exposes Prometheus metrics on port 20257.

## OpenTelemetry Collector

OpenTelemetry Collector (OTEL) forwards observability signals from DPU clusters to a user-specified endpoint. It can stream two independent signals, each enabled by its own endpoint:

* **Logs** — collected from DPU cluster pods and forwarded via OTLP.
* **Metrics** — container, pod, and node resource metrics scraped from every DPU node's kubelet and streamed to the management cluster, tagged with the originating DPUCluster name (the `cluster` label). This powers the [DPUService Performance dashboard](../dashboards/README.md#dpuservice-performance).

### Architecture

* **OTEL Collector DaemonSet**: Collects logs and/or metrics from the DPU cluster and forwards them to the configured endpoint(s), tagged with the DPU cluster name
* **OTEL Collector Endpoint**: Receives signals from DPU clusters via OTLP and exports to a backend (for metrics, typically Prometheus via its remote-write receiver)

### Configuration

OTEL Collector is **disabled by default**. It is enabled when at least one of `logging.endpoint` or `metrics.endpoint` is set; each signal is streamed only when its corresponding endpoint is configured.

Stream logs to an OTLP endpoint:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  monitoring:
    openTelemetryCollector:
      logging:
        endpoint: "http://<host-node-ip>:30050"
```

Stream DPU cluster workload metrics to the management cluster (both signals can be enabled together):

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  monitoring:
    openTelemetryCollector:
      metrics:
        endpoint: "http://<host-node-ip>:30050"
```

> [!NOTE]
> The endpoint can be any OTLP-compatible receiver (OpenTelemetry Collector, observability gateway, cloud service, etc.). For metrics streaming, the management cluster collector writes the received metrics into Prometheus, so the bundled kube-prometheus-stack values must enable the Prometheus remote-write receiver (`prometheus.prometheusSpec.enableRemoteWriteReceiver: true`). See [Helm Prerequisites](../../../getting-started/helm-prerequisites.md).

If using the OpenTelemetry Collector deployed via Helm values (default configuration), it uses NodePort 30050:

```bash
# Get Host Cluster node IP
kubectl get nodes -o wide

# Use format: http://<NODE_IP>:30050
```

<details markdown="1"><summary><b>Expand for Advanced OTEL Configuration</b></summary>

```yaml
spec:
  monitoring:
    openTelemetryCollector:
      logging:
        endpoint: "http://<host-node-ip>:30050"
      daemon:
        image: "otel/opentelemetry-collector-contrib:0.146.1"
        resources:
          limits:
            cpu: "500m"
            memory: "1Gi"
          requests:
            cpu: "200m"
            memory: "512Mi"
```

</details>
