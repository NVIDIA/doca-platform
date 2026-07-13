---
title: "User-Managed Components"
---

[[_TOC_]]

## Introduction

The observability infrastructure components are deployed and managed by the user via Helm. These components run on the Host Cluster and provide metrics collection, log aggregation, and visualization for the entire DPF platform.

For installation instructions, see the [Helm Prerequisites](../../../getting-started/helm-prerequisites.md) guide.

## Components Overview

The monitoring stack consists of four main components:

* **Kube-Prometheus-Stack**: Unified stack with Prometheus (metrics storage), Grafana (visualization), and Prometheus Operator
* **Loki**: Log aggregation and storage backend
* **OpenTelemetry Collector**: Log collection from DPU clusters via OTLP
* **Kube-State-Metrics**: Exposes Host Cluster Kubernetes resource metrics

## Integrating with an Existing Monitoring Stack

Users with an existing Prometheus and Grafana deployment (not installed via the DPF Helm charts) can integrate with DPF by configuring the following integration points:

### Metrics Collection (Prometheus)

DPF exposes two types of metrics that Prometheus needs to scrape:

**1. Kube-State-Metrics (KSM)**

KSM exposes DPF Custom Resource metrics via a standard HTTP endpoint with a `ServiceMonitor`. If the existing Prometheus is managed by Prometheus Operator, ensure that:

1. The Prometheus Operator CRDs (`monitoring.coreos.com`) are installed in the cluster
2. The Prometheus instance is configured to discover ServiceMonitors in the `dpf-operator-system` namespace (via `serviceMonitorNamespaceSelector` and `serviceMonitorSelector`)

If Prometheus Operator is not used, configure a scrape job manually:

```yaml
scrape_configs:
  - job_name: 'dpf-kube-state-metrics'
    kubernetes_sd_configs:
      - role: service
        namespaces:
          names: ['dpf-operator-system']
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_label_app_kubernetes_io_name]
        regex: kube-state-metrics
        action: keep
```

**2. DPF Controller Metrics (secure endpoint)**

DPF controller pods expose metrics over HTTPS with bearer token authentication. Prometheus must authenticate using a ServiceAccount token and trust the cluster CA. The controllers are identified by the pod label `dpu.nvidia.com/component` matching `*-controller-manager` and expose metrics on a port named `metrics`.

```yaml
scrape_configs:
  - job_name: 'doca-platform-framework'
    scrape_interval: 15s
    metrics_path: /metrics
    scheme: https
    authorization:
      type: Bearer
      credentials_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    tls_config:
      ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
      insecure_skip_verify: true
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_dpu_nvidia_com_component]
        action: keep
        regex: ".*-controller-manager"
      - source_labels: [__meta_kubernetes_pod_container_port_name]
        action: keep
        regex: metrics
      - source_labels: [__meta_kubernetes_namespace]
        action: replace
        target_label: namespace
      - source_labels: [__meta_kubernetes_pod_name]
        action: replace
        target_label: pod
```

> [!NOTE]
> The Prometheus ServiceAccount must have permissions to access the `/metrics` endpoint on the controller pods. When using the DPF Helm chart reference configuration, this is handled by the `kube-prometheus-stack-prometheus` ServiceAccount.

### Dashboard Import (Grafana)

DPF dashboards are stored as JSON files in the Helm chart and deployed as ConfigMaps labeled with `grafana_dashboard: "1"`. There are two ways to import them:

**Option 1: Grafana Sidecar (automatic)**

If Grafana is deployed with the [sidecar container](https://github.com/kiwigrid/k8s-sidecar), configure it to watch for ConfigMaps with the `grafana_dashboard: "1"` label in the `dpf-operator-system` namespace. The DPF operator creates these ConfigMaps automatically.

**Option 2: Manual Import**

Download the dashboard JSON files from the [DPF Helm chart dashboards directory](https://github.com/NVIDIA/doca-platform/tree/public-main/deploy/charts/dpf-operator/dashboards) and import them via the Grafana UI or API. See the [Grafana Dashboards](../dashboards/README.md) page for details on available dashboards.

### Log Collection (Loki / OpenTelemetry)

DPU cluster logs are forwarded via OTLP by the DPF-operator-managed OpenTelemetry Collector on each DPU cluster. The target endpoint is configured in `DPFOperatorConfig.spec.monitoring.openTelemetryCollector.logging.endpoint`. Point this to any OTLP-compatible receiver (e.g., an existing OpenTelemetry Collector gateway, Grafana Alloy, or a cloud-hosted endpoint).

## Kube-Prometheus-Stack

The Kube-Prometheus-Stack provides a unified monitoring solution combining Prometheus Operator, Prometheus, and Grafana.

**DPF-Specific Configuration:**

* **Automatic Dashboard Discovery**: Grafana automatically discovers dashboards from ConfigMaps labeled with `grafana_dashboard: "1"`. The DPF operator creates these ConfigMaps automatically.
* **Multi-Cluster Support**: Configured with Grafana multicluster features and Prometheus cluster labeling (`cluster: management`)
* **ServiceMonitor Integration**: Automatically scrapes metrics from components with ServiceMonitor resources
* **Control Plane Scheduling**: Prometheus and Grafana are scheduled on control-plane nodes with appropriate tolerations

For usage and configuration details, see the [kube-prometheus-stack documentation](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack).

### Included Grafana Dashboards

Grafana includes both the default kube-prometheus-stack dashboards for Kubernetes monitoring and DPF-specific dashboards.

**Default Kubernetes Dashboards:**

* Node metrics, Pod metrics, Namespace resources, and more from kube-prometheus-stack

**DPF-Specific Dashboards:**

1. **DOCA Platform Framework State**: High-level overview of the operator and its controllers, highlighting key metrics such as resource status, condition states, and time to readiness
2. **Controller Runtime Dashboard**: Detailed metrics and visualizations for the controllers, including reconciliation times, queue depths, and error rates
3. **Kubernetes API Server Requests Dashboard**: Monitors requests made to the Kubernetes API server, helping identify performance bottlenecks or excessive API usage

All dashboards are automatically deployed when Grafana is enabled and accessible through the Grafana web UI under the "Dashboards" section.

### Grafana Admin Password

**Option 1: Set manually** in the Kube-Prometheus-Stack values file:

```yaml
grafana:
  adminPassword: <your-password>
```

**Option 2: Auto-generated password** - retrieve after deployment:

```bash
kubectl -n dpf-operator-system get secret kube-prometheus-stack-grafana \
  -ojsonpath='{.data.admin-password}' | base64 -d
```

### Storage Configuration

Persistence behavior depends on each chart component's values and the cluster's
default StorageClass. This repository uses the `local-path` provisioner
(`storageClassName: local-path`) for local clusters; for production, configure
an explicit `storageClassName`, appropriate capacity, and a backup/replication
strategy in the Kube-Prometheus-Stack values.

To configure storage, modify the storage configurations in the
Kube-Prometheus-Stack values file:

**Prometheus:**

```yaml
prometheus:
  prometheusSpec:
    storageSpec:
      volumeClaimTemplate:
        spec:
          storageClassName: <your-storage-class>
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 8Gi
```

**Grafana:**

```yaml
grafana:
  persistence:
    enabled: true
    storageClassName: <your-storage-class>
```

Make sure to replace `<your-storage-class>` with the appropriate storage class for your environment.

### Control Plane Metrics

By default, kube-controller-manager and kube-scheduler bind their metrics endpoints to `127.0.0.1`, and etcd exposes metrics on `http://127.0.0.1:2381`. To allow Prometheus to scrape them, configure these components to bind to `0.0.0.0`.

The kube-apiserver exposes metrics on its secure port and is typically reachable via its Service; ensure your ServiceMonitor is configured with the correct TLS settings (e.g., `tlsConfig` with a valid CA and `serverName`) rather than changing the apiserver bind address. Avoid using `insecureSkipVerify` unless strictly necessary.

**For kubeadm Clusters:**

```yaml
apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
controllerManager:
  extraArgs:
    bind-address: "0.0.0.0"
scheduler:
  extraArgs:
    bind-address: "0.0.0.0"
etcd:
  local:
    extraArgs:
      listen-metrics-urls: "http://0.0.0.0:2381"
```

### Multi-Cluster Configuration

The DPF monitoring stack supports monitoring both the Host Cluster and Tenant Control Planes (Kamaji) simultaneously.

**Grafana Multicluster Dashboard Support:**

* Enabled via `sidecar.dashboards.multicluster.global.enabled: true`
* Allows dashboards to display and filter metrics across multiple clusters using cluster labels

**Prometheus Cluster Labeling:**

All metrics scraped by the Host Cluster's Prometheus instance are automatically labeled with a `cluster` label to identify their source. This follows the approach described in the [Kamaji monitoring documentation](https://kamaji.clastix.io/guides/monitoring/#accessing-metrics).

The configuration uses two complementary approaches:

**1. ServiceMonitor Relabeling for Control Plane Components:**

The Host Cluster's control plane metrics are labeled via ServiceMonitor relabelings. Below is an example for `kubeApiServer` - add similar relabelings for all other built-in ServiceMonitors (coreDns, kubeProxy, kubeEtcd, kubeControllerManager, kubeScheduler, kubelet):

```yaml
# Add cluster label to all built-in ServiceMonitors for Host Cluster
kubeApiServer:
  serviceMonitor:
    relabelings:
      - action: replace
        targetLabel: cluster
        replacement: management
```

**2. Global External Labels for All Metrics:**

```yaml
prometheus:
  prometheusSpec:
    # Add cluster label to ALL metrics via external labels
    externalLabels:
      cluster: management
```

> [!NOTE]
> While control plane components have explicit `cluster: management` from ServiceMonitor relabelings (applied at scrape time), the `externalLabels` configuration ensures all metrics have the cluster label, including those without specific ServiceMonitor relabelings.

This ensures:
* **Host Cluster control plane** (kube-apiserver, kube-controller-manager, kube-scheduler) are labeled with `cluster: management`
* **All other Host Cluster metrics** (DPF operator, Kube-State-Metrics, kubelet, etc.) are also labeled with `cluster: management`
* **Kamaji tenant cluster control planes** can be scraped with their own cluster labels (e.g., `cluster: charlie`) following the [Kamaji ServiceMonitor examples](https://kamaji.clastix.io/guides/monitoring/#enable-metrics-scraping)

## Loki

Loki provides log aggregation and storage for both Host Cluster and DPU cluster logs.

**Log Sources:**

* **Host Cluster logs**: Collected by OpenTelemetry Collector (filelog receiver)
* **DPU Cluster logs**: Forwarded by DPU OpenTelemetry Collectors via OTLP

**Integration:**

* Logs are stored with cluster labels (`cluster: management` or `cluster: dpucluster-<name>`) for multi-cluster filtering
* Accessible via Grafana's Explore interface using LogQL queries

For querying and configuration details, see the [Loki documentation](https://grafana.com/docs/loki/latest/).

## OpenTelemetry Collector

The OpenTelemetry Collector on the Host Cluster runs as a DaemonSet with two purposes:

1. **OTLP Receiver**: Receives logs from DPU cluster OpenTelemetry Collectors (gRPC 4317, HTTP 4318, exposed via NodePort 30318)
2. **Local Log Collection**: Collects logs from Host Cluster pods via filelog receiver

**Exporters:**

* Logs to Loki via `otlphttp` exporter
* Metrics to Prometheus via `prometheusremotewrite` exporter

For configuration details, see the [OpenTelemetry Collector documentation](https://opentelemetry.io/docs/collector/).

## Kube-State-Metrics

The user-deployed Kube-State-Metrics instance monitors Host Cluster Kubernetes resources (Pods, Deployments, Nodes, etc.). This is separate from the operator-deployed Kube-State-Metrics that monitors DPU custom resources.

**Difference from DPF-Operator-Managed KSM:**

* **User-Managed KSM**: Monitors standard Kubernetes resources on Host Cluster
* **Operator KSM**: Monitors DPF custom resources (DPU, IPPool, ServiceChain) on DPU clusters

Both expose metrics to Prometheus via ServiceMonitor.

### Exposing DPF Custom Resource Metrics

The DPF operator Helm chart ships a `dpf-operator-customresourcestate-config` ConfigMap containing the CustomResourceState metric configuration for DPF CRDs. The shipped `kube-state-metrics` values already mount this ConfigMap and grant the required RBAC, so a default DPF installation works as-is.

If you bring your own `kube-state-metrics` installation, configure the KSM Pod to:

1. Mount the `dpf-operator-customresourcestate-config` ConfigMap as a file (`volumes` / `volumeMounts`)
2. Pass `--custom-resource-state-config-file` pointing at that file (`extraArgs`)
3. Grant `list`/`watch` RBAC on the DPF API groups and on `customresourcedefinitions` (`rbac.extraRules`)

A minimal Helm values snippet for the `kube-state-metrics` helm chart to wire this up looks like:

```yaml
volumes:
  - name: customresourcestate-config
    configMap:
      name: dpf-operator-customresourcestate-config
volumeMounts:
  - name: customresourcestate-config
    mountPath: /etc/customresourcestate
    readOnly: true
extraArgs:
  - --custom-resource-state-config-file=/etc/customresourcestate/config.yaml
rbac:
  extraRules:
    - apiGroups:
        - svc.dpu.nvidia.com
        - operator.dpu.nvidia.com
        - provisioning.dpu.nvidia.com
        - storage.dpu.nvidia.com
        - vpc.dpu.nvidia.com
      resources: ["*"]
      verbs: ["list", "watch"]
    - apiGroups: ["apiextensions.k8s.io"]
      resources: ["customresourcedefinitions"]
      verbs: ["list", "watch"]
```

For the full reference values shipped with DPF (including affinity, tolerations, ServiceMonitor, and label allowlist), see:

<details markdown="1"><summary><b>kube-state-metrics values</b></summary>

[embedmd]:#(../../../../../deploy/helmfiles/values/kube-state-metrics.yaml)
```yaml
# Kube State Metrics configuration
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
        - matchExpressions:
            - key: "node-role.kubernetes.io/master"
              operator: Exists
        - matchExpressions:
            - key: "node-role.kubernetes.io/control-plane"
              operator: Exists
tolerations:
  - key: node-role.kubernetes.io/master
    operator: Exists
    effect: NoSchedule
  - key: node-role.kubernetes.io/control-plane
    operator: Exists
    effect: NoSchedule
extraArgs:
  - --custom-resource-state-config-file=/etc/customresourcestate/config.yaml
  - --metric-labels-allowlist=pods=[svc.dpu.nvidia.com/service],daemonsets=[svc.dpu.nvidia.com/service],deployments=[svc.dpu.nvidia.com/service]
# Only run the collectors for resources that are relevant for observing DPF.
# These are all low-cardinality; the pod metrics are the main cardinality
# driver and are further filtered in the ServiceMonitor. The DPF resource
# state metrics come from the custom resource state config and are not
# affected by this list.
collectors:
  - cronjobs
  - daemonsets
  - deployments
  - endpoints
  - jobs
  - namespaces
  - nodes
  - persistentvolumeclaims
  - persistentvolumes
  - pods
  - replicasets
  - resourcequotas
  - services
  - statefulsets
volumes:
  - configMap:
      defaultMode: 420
      name: dpf-operator-customresourcestate-config
    name: customresourcestate-config
volumeMounts:
  - mountPath: /etc/customresourcestate
    name: customresourcestate-config
    readOnly: true
prometheus:
  monitor:
    enabled: true
    http:
      honorLabels: true
      # Keep the DPF custom resource state metrics and the kube_* metrics of
      # the low-cardinality resources. Pod metrics are the main cardinality
      # driver and are limited to the ones consumed by the DPF dashboards and
      # alert/recording rules.
      metricRelabelings:
        - sourceLabels: [__name__]
          action: keep
          regex: (bfb|dpfoperatorconfig|dpu[a-z]*)_.+|kube_pod_info|kube_pod_labels|kube_pod_status_phase|kube_pod_status_ready|kube_pod_container_info|kube_pod_container_status_restarts_total|kube_pod_container_status_waiting_reason|kube_(cronjob|daemonset|deployment|endpoint|job|namespace|node|persistentvolumeclaim|persistentvolume|replicaset|resourcequota|service|statefulset)(_.+)?
rbac:
  extraRules:
    - apiGroups:
        - svc.dpu.nvidia.com
        - operator.dpu.nvidia.com
        - provisioning.dpu.nvidia.com
        - storage.dpu.nvidia.com
        - vpc.dpu.nvidia.com
      resources:
        - '*'
      verbs: ["list", "watch"]
    - apiGroups: ["apiextensions.k8s.io"]
      resources: ["customresourcedefinitions"]
      verbs: ["list", "watch"]
```

</details>

The ConfigMap must reside in the same namespace as the `kube-state-metrics` Pod in this scenario. By default the DPF operator creates it in the release namespace (`dpf-operator-system`). If KSM runs in a different namespace, set `kubeStateMetricsCRDMetrics.namespaceOverride` in the DPF operator Helm values to match.

For more information, see the [Kube-State-Metrics documentation](https://github.com/kubernetes/kube-state-metrics/tree/main/docs).
