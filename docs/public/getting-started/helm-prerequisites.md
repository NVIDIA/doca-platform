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
| [cert-manager]           | v1.18.1 | Certificate management for Kubernetes, provides automatic TLS certificate issuance and renewal | ✅        | Pre-installation      |
| [argo-cd]                | 7.8.2   | GitOps continuous delivery tool for Kubernetes, necessary for DPUService integration           | ✅        | Pre-installation      |
| [node-feature-discovery] | 0.17.1  | Discovers and advertises hardware features and capabilities of DPUs in the cluster             | ✅        | Pre-installation      |
| [maintenance-operator]   | 0.2.0   | Manages node maintenance operations and ensures graceful handling of node updates              | ✅        | Pre-installation      |
| [kamaji]                 | 1.1.0   | Kubernetes cluster management platform for creating and managing the DPU Kubernetes clusters   | ❌        | Pre-installation      |
| [local-path-provisioner] | 0.0.31  | Provides a local storage provisioner for Kubernetes, used for Kamaji etcd storage              | ❌        | Pre-installation      |
| [kube-state-metrics]     | 5.25.1  | Exposes DPF Operator related objects as metrics                                                | ❌        | Post-installation     |
| [grafana]                | 8.4.6   | Open-source analytics and monitoring platform for visualizing metrics and logs                 | ❌        | Post-installation     |
| [prometheus]             | 25.26.0 | Time-series database and monitoring system for collecting and querying metrics                 | ❌        | Doesn't matter        |

Some of the components requires the DPF Operator to be installed before they can be installed.  
This is necessary for `kube-state-metrics` and `grafana`, because we rely on ConfigMaps created by the DPF Operator to
provide the necessary configuration for these components.

[cert-manager]: https://cert-manager.io/docs/installation/helm
[argo-cd]: https://argo-cd.readthedocs.io/en/stable/getting_started/
[node-feature-discovery]: https://github.com/kubernetes-sigs/node-feature-discovery/tree/master/deployment/helm/node-feature-discovery
[maintenance-operator]: https://github.com/Mellanox/maintenance-operator/tree/main/deployment/maintenance-operator-chart
[kamaji]: https://github.com/clastix/kamaji/tree/master/charts/kamaji
[local-path-provisioner]: https://github.com/rancher/local-path-provisioner/
[kube-state-metrics]: https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-state-metrics
[grafana]: https://github.com/grafana/helm-charts/tree/main/charts/grafana
[prometheus]: https://github.com/prometheus-community/helm-charts/tree/main/charts/prometheus
[helmfile]: https://helmfile.readthedocs.io/
[DPF repository]: https://github.com/nvidia/doca-platform/

## Installation Options

### Option 1: Using Helmfile

We provide a working [helmfile] configuration that can be used to install all dependencies with the correct values.  
The helmfiles are located at `deploy/helmfiles/` in the [DPF repository].

This approach ensures consistent deployment across different environments and simplifies the installation process.

> [!NOTE] You have to install the `local-path-provisioner` manually. The current helmfile for the release v25.7.1 does not
> include it, but it is required for the Kamaji etcd storage.

You can install the `local-path-provisioner` it with the following command:

```bash
curl https://codeload.github.com/rancher/local-path-provisioner/tar.gz/v0.0.31 | tar -xz --strip=3 local-path-provisioner-0.0.31/deploy/chart/local-path-provisioner/
kubectl create ns local-path-provisioner
helm upgrade --install -n local-path-provisioner local-path-provisioner ./local-path-provisioner --version 0.0.31 \
  --set 'tolerations[0].key=node-role.kubernetes.io/control-plane' \
  --set 'tolerations[0].operator=Exists' \
  --set 'tolerations[0].effect=NoSchedule' \
  --set 'tolerations[1].key=node-role.kubernetes.io/master' \
  --set 'tolerations[1].operator=Exists' \
  --set 'tolerations[1].effect=NoSchedule'
```

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
  tag: v1.33.0-25.4.1
  pullPolicy: Always
cfssl:
  image:
    tag: v1.6.5
```

</details>

<details markdown="1"><summary>local-path-provosioner</summary>

```yaml
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
        - svc.dpu.nvidia.com
        - provisioning.dpu.nvidia.com
      resources:
        - dpuclusters
        - dpuclusters/status
        - dpudeployments
        - dpudeployments/status
        - dpuservicecredentialrequests
        - dpuservicecredentialrequests/status
        - dpuservices
        - dpuservices/status
        - dpuservicetemplates
        - dpuservicetemplates/status
        - dpfoperatorconfigs
        - dpfoperatorconfigs/status
        - dpuservicechains
        - dpuservicechains/status
        - dpuserviceinterfaces
        - dpuserviceinterfaces/status
        - dpuserviceipams
        - dpuserviceipams/status
        - dpus
        - dpus/status
        - bfbs
        - bfbs/status
      verbs: ["list", "watch"]
    - apiGroups: ["apiextensions.k8s.io"]
      resources: ["customresourcedefinitions"]
      verbs: ["list", "watch"]
```

</details>

<details markdown="1"><summary>grafana</summary>

[embedmd]:#(../../../deploy/helmfiles/values/grafana.yaml)
```yaml
# Grafana configuration
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
persistence:
  enabled: true
  storageClassName: local-path
datasources:
  datasources.yaml:
    apiVersion: 1
    datasources:
      - name: prometheus
        type: prometheus
        url: http://dpf-operator-prometheus-server
        access: proxy
        isDefault: true
dashboardProviders:
  dashboardproviders.yaml:
    apiVersion: 1
    providers:
      - name: 'default'
        orgId: 1
        folder: ''
        type: file
        disableDeletion: false
        editable: true
        options:
          path: /var/lib/grafana/dashboards/default
      - name: 'debug'
        orgId: 1
        folder: 'debug'
        type: file
        disableDeletion: false
        editable: true
        options:
          path: /var/lib/grafana/dashboards/debug
dashboardsConfigMaps:
  default: dpf-operator-grafana-dashboards
  debug: dpf-operator-grafana-debug-dashboards
```

</details>

<details markdown="1"><summary>prometheus</summary>

[embedmd]:#(../../../deploy/helmfiles/values/prometheus.yaml)
```yaml
# Prometheus configuration
server:
  persistentVolume:
    storageClass: local-path
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
alertmanager:
  enabled: false
prometheus-node-exporter:
  enabled: false
prometheus-pushgateway:
  enabled: false
kube-state-metrics:
  enabled: false
extraScrapeConfigs: |
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
```

</details>
