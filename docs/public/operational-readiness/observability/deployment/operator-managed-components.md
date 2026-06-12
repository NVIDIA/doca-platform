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

Node-Problem-Detector (NPD) monitors DPU node health and reports problems as Node conditions. It runs as a DaemonSet on each DPU cluster node.

### Health Checks

NPD includes DPU-specific health checks that run every 30 seconds:

| Condition Type       | Check Description                           |
|----------------------|---------------------------------------------|
| `OVSvSwitchdHealthy` | Verifies ovs-vswitchd service is running    |
| `OVSDBHealthy`       | Verifies ovsdb-server service is running    |
| `OVSHealthy`         | Checks for OVS process OOM kills            |
| `SRIOVHealthy`       | Verifies SR-IOV VF representors are present |
| `UplinkHealthy`      | Checks physical uplink is operational       |
| `DPUModeCorrect`     | Verifies DPU is in embedded mode            |
| `MTUConfigured`      | Validates network MTU configuration         |

Additionally, NPD monitors standard Kubernetes node problems (kernel deadlocks, read-only filesystems, disk pressure, OOM events).

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

OpenTelemetry Collector (OTEL) provides centralized log collection from DPU clusters to a user-specified endpoint.

### Architecture

* **OTEL Collector DaemonSet**: Collects logs from DPU cluster pods and forwards to the configured endpoint, tagged with cluster name
* **OTEL Collector Endpoint**: Receives logs from DPU clusters via OTLP and exports to a backend

### Configuration

OTEL Collector is **disabled by default** and requires a logging endpoint configuration:

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
        endpoint: "http://<host-node-ip>:30318"
```

> [!NOTE]
> The endpoint can be any OTLP-compatible receiver (OpenTelemetry Collector, observability gateway, cloud service, etc.).

If using the OpenTelemetry Collector deployed via Helm values (default configuration), it uses NodePort 30318:

```bash
# Get Host Cluster node IP
kubectl get nodes -o wide

# Use format: http://<NODE_IP>:30318
```

<details markdown="1"><summary><b>Expand for Advanced OTEL Configuration</b></summary>

```yaml
spec:
  monitoring:
    openTelemetryCollector:
      logging:
        endpoint: "http://<host-node-ip>:30318"
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
