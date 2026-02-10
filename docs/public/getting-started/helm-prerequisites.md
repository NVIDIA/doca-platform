---
title: "Helm Prerequisites"
---

[TOC]

## Overview

The DPF Operator requires several prerequisite components to function properly in a Kubernetes environment. This
document provides comprehensive guidance on the Helm chart dependencies and their configuration values needed for a
successful DPF Operator deployment.

## Important Note

Starting with DPF v25.7, all Helm dependencies have been removed from the DPF chart. This means that **all dependencies
must be installed manually** before installing the DPF chart itself.

## Prerequisites Overview

The following table lists all required and optional Helm chart dependencies with their specific versions and purposes:

| Helm Chart               | Version | Description                                                                                    | Required | Post/Pre-installation |
|--------------------------|---------|------------------------------------------------------------------------------------------------|----------|-----------------------|
| [cert-manager]           | v1.19.3 | Certificate management for Kubernetes, provides automatic TLS certificate issuance and renewal | ✅        | Pre-installation      |
| [argo-cd]                | 9.4.1   | GitOps continuous delivery tool for Kubernetes, necessary for DPUService integration           | ✅        | Pre-installation      |
| [node-feature-discovery] | 0.18.3  | Discovers and advertises hardware features and capabilities of DPUs in the cluster             | ✅        | Pre-installation      |
| [maintenance-operator]   | 0.2.3   | Manages node maintenance operations and ensures graceful handling of node updates              | ✅        | Pre-installation      |
| [kamaji]                 | 1.2.0   | Kubernetes cluster management platform for creating and managing the DPU Kubernetes clusters   | ❌        | Pre-installation      |
| [local-path-provisioner] | 0.0.34  | Provides a local storage provisioner for Kubernetes, used for Kamaji etcd storage              | ❌        | Pre-installation      |
| [kube-state-metrics]     | 5.25.1  | Exposes DPF Operator related objects as metrics                                                | ❌        | Post-installation     |
| [kube-prometheus-stack]  | 80.4.1  | Complete monitoring stack with Prometheus and Grafana for collecting and visualizing metrics   | ❌        | Post-installation     |

Some of the components requires the DPF Operator to be installed before they can be installed.  
This is necessary for `kube-state-metrics` and `kube-prometheus-stack` (Grafana dashboards), because we rely on ConfigMaps created by the DPF Operator to
provide the necessary configuration for these components.

[cert-manager]: https://cert-manager.io/docs/installation/helm
[argo-cd]: https://argo-cd.readthedocs.io/en/stable/getting_started/
[node-feature-discovery]: https://github.com/kubernetes-sigs/node-feature-discovery/tree/master/deployment/helm/node-feature-discovery
[maintenance-operator]: https://github.com/Mellanox/maintenance-operator/tree/main/deployment/maintenance-operator-chart
[kamaji]: https://github.com/clastix/kamaji/tree/master/charts/kamaji
[local-path-provisioner]: https://github.com/rancher/local-path-provisioner/
[kube-state-metrics]: https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-state-metrics
[kube-prometheus-stack]: https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack
[helmfile]: https://helmfile.readthedocs.io/
[DPF repository]: https://github.com/nvidia/doca-platform/

## Installation Options

### Option 1: Using Helmfile

We provide a working [helmfile] configuration that can be used to install all dependencies with the correct values.  
The helmfiles are located at `deploy/helmfiles/` in the [DPF repository].

This approach ensures consistent deployment across different environments and simplifies the installation process.

### Option 2: Manual Installation

If you prefer to install dependencies manually, you can use the individual Helm chart values provided in the sections
below.

## Required Configuration Values

The following sections provide the specific Helm chart values that must be configured before installing each dependency.
These configurations ensure proper integration with the DPF Operator and optimal performance in your environment.

<details markdown="1"><summary>cert-manager</summary>

[embedmd]:#(../../../deploy/helmfiles/values/cert-manager.yaml)
```yaml
startupapicheck:
  enabled: false
crds:
  enabled: true
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
        - matchExpressions:
            - key: node-role.kubernetes.io/master
              operator: Exists
        - matchExpressions:
            - key: node-role.kubernetes.io/control-plane
              operator: Exists
tolerations:
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/control-plane
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/master
cainjector:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: node-role.kubernetes.io/master
                operator: Exists
          - matchExpressions:
              - key: node-role.kubernetes.io/control-plane
                operator: Exists
  tolerations:
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/control-plane
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/master
webhook:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: node-role.kubernetes.io/master
                operator: Exists
          - matchExpressions:
              - key: node-role.kubernetes.io/control-plane
                operator: Exists
  tolerations:
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/control-plane
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/master
```

</details>

<details markdown="1"><summary>argo-cd</summary>

[embedmd]:#(../../../deploy/helmfiles/values/argo-cd.yaml)
```yaml
## Disable the ApplicationSet controller.
applicationSet:
  replicas: 0
dex:
  enabled: false
notifications:
  enabled: false
global:
  podLabels:
    ovn.dpu.nvidia.com/skip-injection: ""
  affinity:
    nodeAffinity:
      # -- Default node affinity rules. Either: `none`, `soft` or `hard`
      type: hard
      # -- Default match expressions for node affinity
      matchExpressions:
        - key: "node-role.kubernetes.io/control-plane"
          operator: Exists
  tolerations:
    - key: node-role.kubernetes.io/master
      operator: Exists
      effect: NoSchedule
    - key: node-role.kubernetes.io/control-plane
      operator: Exists
      effect: NoSchedule
redis:
  image:
    repository: mirror.gcr.io/redis
```

</details>

<details markdown="1"><summary>node-feature-discovery</summary>

[embedmd]:#(../../../deploy/helmfiles/values/node-feature-discovery.yaml)
```yaml
# Node Feature Discovery configuration
master:
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
  # Note: beginning with v0.18.3 the master toleration was dropped from the chart's default values.yaml.
  - key: "node-role.kubernetes.io/master"
    operator: "Equal"
    value: ""
    effect: "NoSchedule"
  - key: "node-role.kubernetes.io/control-plane"
    operator: "Equal"
    value: ""
    effect: "NoSchedule"
worker:
  enable: true
  hostNetwork: true
  tolerations:
    - key: node.kubernetes.io/not-ready
      operator: Exists
  config:
    sources:
      pci:
        deviceClassWhitelist:
          - "0200"
        deviceLabelFields:
          - "class"
          - "vendor"
          - "device"
gc:
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
```

</details>

<details markdown="1"><summary>maintenance-operator</summary>

[embedmd]:#(../../../deploy/helmfiles/values/maintenance-operator-chart.yaml)
```yaml
# Maintenance Operator Chart configuration
operatorConfig:
  maxParallelOperations: 60%
operator:
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
```

</details>

<details markdown="1"><summary>kamaji</summary>

[embedmd]:#(../../../deploy/helmfiles/values/kamaji.yaml)
```yaml
# Kamaji configuration
# Number of Kamaji controller replicas for High Availability
replicas: 2
resources: null
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
kamaji-etcd:
  persistentVolumeClaim:
    storageClassName: local-path
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
  jobs:
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
  datastore:
    enabled: true
    annotations:
      helm.sh/resource-policy: keep
    name: default
image:
  repository: ghcr.io/nvidia/kamaji
  tag: v1.34.0-25.9.3
  pullPolicy: Always
cfssl:
  image:
    tag: v1.6.5
```

</details>

<details markdown="1"><summary>local-path-provisioner</summary>

[embedmd]:#(../../../deploy/helmfiles/values/local-path-provisioner.yaml)
```yaml
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
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/control-plane
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/master
```

</details>

<details markdown="1"><summary>kube-state-metrics</summary>

[embedmd]:#(../../../deploy/helmfiles/values/kube-state-metrics.yaml)
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
volumes:
  - configMap:
      defaultMode: 420
      name: dpf-operator-customresourcestate-config
    name: customresourcestate-config
volumeMounts:
  - mountPath: /etc/customresourcestate
    name: customresourcestate-config
    readOnly: true
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

<details markdown="1"><summary>kube-prometheus-stack</summary>

[embedmd]:#(../../../deploy/helmfiles/values/kube-prometheus-stack.yaml)
```yaml
# kube-prometheus-stack configuration
#
# This configuration replaces the separate prometheus and grafana helm releases
# with a unified kube-prometheus-stack release that includes:
# - Prometheus Operator
# - Prometheus
# - Grafana
#
# Key features:
# - Grafana automatically discovers dashboards from ConfigMaps with label grafana_dashboard: "1"
# - The dpf-operator chart creates ConfigMaps with these labels for its dashboards
# - Prometheus datasource is automatically configured with uid: prometheus (matching dashboard expectations)
# - Both Prometheus and Grafana are scheduled on control-plane nodes with appropriate tolerations
#
# Note: kube-state-metrics is deployed separately and should be installed independently

kubeStateMetrics:
  enabled: false

nodeExporter:
  enabled: false

alertmanager:
  enabled: false

crds:
  enabled: true
  upgradeJob:
    enabled: false
    # If enabled, schedule CRD upgrade job on control-plane nodes
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

# Add cluster label to all built-in ServiceMonitors for management cluster
# These relabelings distinguish management cluster metrics from Kamaji tenant cluster metrics
coreDns:
  serviceMonitor:
    relabelings:
      - action: replace
        targetLabel: cluster
        replacement: management
kubeProxy:
  serviceMonitor:
    relabelings:
      - action: replace
        targetLabel: cluster
        replacement: management
kubeEtcd:
  serviceMonitor:
    relabelings:
      - action: replace
        targetLabel: cluster
        replacement: management
kubeApiServer:
  serviceMonitor:
    relabelings:
      - action: replace
        targetLabel: cluster
        replacement: management
kubeControllerManager:
  serviceMonitor:
    relabelings:
      - action: replace
        targetLabel: cluster
        replacement: management
kubeScheduler:
  serviceMonitor:
    relabelings:
      - action: replace
        targetLabel: cluster
        replacement: management
kubelet:
  serviceMonitor:
    relabelings:
      - action: replace
        targetLabel: cluster
        replacement: management

# Prometheus configuration
prometheus:

  prometheusSpec:
    # Add cluster label to ALL metrics via external labels
    # In modern Prometheus, these labels are visible in local queries
    externalLabels:
      cluster: management

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
    
    # Persistent volume configuration
    storageSpec:
      volumeClaimTemplate:
        spec:
          storageClassName: local-path
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 8Gi
    
    # Service account with permissions to scrape metrics
    serviceAccountName: kube-prometheus-stack-prometheus
    
    # Additional scrape configs for DPF Operator metrics
    additionalScrapeConfigs:
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
        # Add cluster label to ALL scraped metrics for Grafana multicluster support
        # This makes the cluster label visible in local queries (unlike externalLabels)
        # Note: The control plane components (kube-apiserver, kube-controller-manager, kube-scheduler)
        # already have cluster labels via their ServiceMonitor relabelings above
        metric_relabel_configs:
          - action: replace
            target_label: cluster
            replacement: management

    # Allow monitoring of all ServiceMonitors
    # Setting to {} alone isn't enough - need to disable the default helm values behavior
    serviceMonitorSelectorNilUsesHelmValues: false
    serviceMonitorSelector: {}
    
    # Allow monitoring of all namespaces
    serviceMonitorNamespaceSelector: {}
    
    # Allow monitoring of all PodMonitors
    podMonitorSelectorNilUsesHelmValues: false
    podMonitorSelector: {}
    podMonitorNamespaceSelector: {}

# Grafana configuration
grafana:
  enabled: true
  
  # Schedule grafana on control-plane nodes
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

  # Persistent volume configuration
  persistence:
    enabled: true
    storageClassName: local-path

  # Disable init container that changes ownership (causes issues with some storage classes)
  initChownData:
    enabled: false

  # Datasource configuration
  # kube-prometheus-stack automatically creates a Prometheus datasource with uid: prometheus
  # which matches what the dpf-operator dashboards expect

  # Sidecar configuration
  sidecar:
    # Datasources sidecar - provisions datasources from ConfigMaps/Secrets
    datasources:
      enabled: true
      # This is critical - without it, Grafana won't load datasources on startup
      defaultDatasourceEnabled: true
      # Note: The sidecar writes datasources but by default skips the initial reload (REQ_SKIP_INIT: true)
      # The lifecycle hook above handles triggering the initial reload

    # Dashboards sidecar - provisions dashboards from ConfigMaps
    dashboards:
      enabled: true
      # Label that the sidecar will look for in ConfigMaps
      label: grafana_dashboard
      labelValue: "1"
      # Search in dpf-operator-system namespace for dashboard ConfigMaps
      searchNamespace: dpf-operator-system
      # Use folder annotation to organize dashboards into folders
      folderAnnotation: grafana_folder
      # Allow the sidecar to create dashboard providers automatically
      provider:
        foldersFromFilesStructure: true
      # Enable multicluster dashboard support
      # This allows dashboards to display metrics from multiple clusters with proper cluster labels
      multicluster:
        global:
          enabled: true

# Prometheus Operator configuration
prometheusOperator:
  # Schedule operator on control-plane nodes
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
  
  # Admission webhooks configuration
  admissionWebhooks:
    # Patch job creates/patches webhook certificates
    patch:
      # Schedule patch job on control-plane nodes
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

  # Create CRDs
  createCustomResource: true
  
  # Prometheus operator resources
  resources:
    limits:
      cpu: 200m
      memory: 200Mi
    requests:
      cpu: 100m
      memory: 100Mi
```

</details>
