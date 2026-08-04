---
title: "Helm Prerequisites"
---

[[_TOC_]]

## Overview

The DPF Operator requires several prerequisite components to function properly in a Kubernetes environment. This
document provides comprehensive guidance on the Helm chart dependencies and their configuration values needed for a
successful DPF Operator deployment.

## Important Note

Starting with DPF v25.7, all Helm dependencies have been removed from the DPF chart. This means that **all dependencies
must be installed manually** before installing the DPF chart itself.

## Prerequisites Overview

The following table lists all required, conditional, and optional Helm chart dependencies with their specific versions
and purposes:

| Helm Chart                | Version | Description                                                                                    | Required | Post/Pre-installation |
|---------------------------|---------|------------------------------------------------------------------------------------------------|----------|-----------------------|
| [cert-manager]            | v1.19.3 | Certificate management for Kubernetes, provides automatic TLS certificate issuance and renewal | Yes      | Pre-installation      |
| [argo-cd]                 | 9.4.1   | GitOps continuous delivery tool for Kubernetes, necessary for DPUService integration           | Yes      | Pre-installation      |
| [node-feature-discovery]  | 0.18.3  | Discovers and advertises hardware features and capabilities of DPUs in the cluster             | Yes      | Pre-installation      |
| [maintenance-operator]    | 0.3.0   | Manages node maintenance operations and ensures graceful handling of node updates              | Yes      | Pre-installation      |
| [kamaji]                  | 1.4.0   | Kubernetes cluster management platform for creating and managing the DPU Kubernetes clusters   | Conditional | Pre-installation      |
| [local-path-provisioner]  | 0.0.34  | Provides the `local-path` storage class used by the default Kamaji etcd configuration          | Conditional | Pre-installation      |
| [openbao]                 | 0.28.4  | Secrets management service that can be used as a backend for secret storage workflows          | No       | Pre-installation      |
| [external-secrets]        | 2.7.0   | Synchronizes secrets from external secret stores into Kubernetes Secrets                       | No       | Pre-installation      |
| [kata-containers]         | 3.32.0  | Secure container runtime using lightweight VMs for workload isolation on host nodes            | Conditional | Pre-installation      |
| [kube-state-metrics]      | 5.25.1  | Exposes DPF Operator related objects as metrics                                                | No       | Post-installation     |
| [kube-prometheus-stack]   | 80.4.1  | Complete monitoring stack with Prometheus and Grafana for collecting and visualizing metrics   | No       | Post-installation     |
| [loki]                    | 6.53.0  | Kubernetes log aggregation and storage, integrates with Grafana                                | No       | Post-installation     |
| [opentelemetry-collector] | 0.146.0 | Collects and exports metrics, logs, and traces to observability backends                       | No       | Post-installation     |

`Conditional` means the component is required for the default installation described in the user guides, but can be
replaced in custom deployments.

Some of the components requires the DPF Operator to be installed before they can be installed.  
This is necessary for `kube-state-metrics` and `kube-prometheus-stack` (Grafana dashboards), because we rely on ConfigMaps created by the DPF Operator to
provide the necessary configuration for these components.

See [Running Argo CD in a separate namespace](#running-argo-cd-in-a-separate-namespace) for the configuration required to utilise ArgoCD running in a different namespace.

See [Running Kube-State-Metrics in a separate namespace](#running-kube-state-metrics-in-a-separate-namespace) for the configuration required when KSM runs outside `dpf-operator-system`.

[cert-manager]: https://cert-manager.io/docs/installation/helm
[argo-cd]: https://argo-cd.readthedocs.io/en/stable/getting_started/
[node-feature-discovery]: https://github.com/kubernetes-sigs/node-feature-discovery/tree/master/deployment/helm/node-feature-discovery
[maintenance-operator]: https://github.com/Mellanox/maintenance-operator/tree/main/deployment/maintenance-operator-chart
[kamaji]: https://github.com/clastix/kamaji/tree/master/charts/kamaji
[local-path-provisioner]: https://github.com/rancher/local-path-provisioner/
[openbao]: https://openbao.org/docs/platform/k8s/helm/
[external-secrets]: https://external-secrets.io/latest/
[kata-containers]: https://github.com/kata-containers/kata-containers
[kube-state-metrics]: https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-state-metrics
[kube-prometheus-stack]: https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack
[loki]: https://github.com/grafana/loki/
[opentelemetry-collector]: https://github.com/open-telemetry/opentelemetry-collector-contrib/
[helmfile]: https://helmfile.readthedocs.io/
[DPF repository]: https://github.com/nvidia/doca-platform/

## Running Argo CD in a separate namespace

DPF supports running Argo CD in a namespace other than `dpf-operator-system`. When Argo CD is installed outside
`dpf-operator-system`, ensure that `dpf-operator-system` is included in the Argo CD Helm value
`configs.params.application.namespaces` (or an equivalent configuration) so Argo CD reconciles Applications in
`dpf-operator-system`. Also set `spec.overrides.argoCDNamespace` in the `DPFOperatorConfig` to the namespace where
Argo CD is installed. See the
[DPFOperatorConfig guide](../developer-guides/api/dpfoperatorconfig.md#argo-cd-namespace) for an example.

## Running Kube-State-Metrics in a separate namespace

The shipped `kube-state-metrics` values are preconfigured to expose DPF custom resource metrics and work as-is
when KSM runs in `dpf-operator-system`. If you deploy KSM in a different namespace, set
`kubeStateMetricsCRDMetrics.namespaceOverride` in the DPF operator Helm values so the CustomResourceState
ConfigMap lands next to the KSM Pod. See
[Exposing DPF Custom Resource Metrics](../operational-readiness/observability/deployment/user-managed-components.md#exposing-dpf-custom-resource-metrics)
for more details.

## Installation Options

### Option 1: Using Helmfile

We provide a working [helmfile] configuration that can be used to install all dependencies with the correct values.  
The helmfiles are located at `deploy/helmfiles/` in the [DPF repository].

This approach ensures consistent deployment across different environments and simplifies the installation process.

> [!NOTE]
> The default Helmfile installs both `kamaji` and `local-path-provisioner`. Apply the required changes to files in
> `deploy/helmfiles/` if you do not want to install these components, or if you want to replace the default `local-path`
> storage class with another storage class for Kamaji etcd.

### Option 2: Manual Installation

If you prefer to install dependencies manually, you can use the individual Helm chart values provided in the sections
below.

## Required Configuration Values

The following section provides the specific Helm chart values that must be configured before installing each dependency.
These configurations ensure proper integration with the DPF Operator and optimal performance in your environment.

**Helm Chart Values**

<details markdown="1"><summary><b>cert-manager</b></summary>

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

<details markdown="1"><summary><b>openbao</b></summary>

> [!NOTE]
> Applying the values below is not sufficient to bootstrap a usable OpenBao instance. A few extra bootstrap actions are
> required: create the static seal key before installation, initialize OpenBao after installation, and configure the
> Kubernetes auth mount. Refer to the Helmfile hooks in the DPF repository for an example of this flow.
>
> [!WARNING]
> In the provided bootstrap flow, the static seal key is stored in the `openbao/openbao-static-seal` Kubernetes Secret to
> support automatic unseal, and the generated root token is stored in the `openbao/openbao-root-token` Kubernetes Secret.
> Treat access to these secrets as full administrative access to OpenBao. For production deployments, restrict RBAC access
> to the `openbao` namespace, enable Kubernetes secret encryption at rest, and consider replacing the static seal and
> stored root token workflow with an external key management and token lifecycle process that matches your security
> requirements. Review the
> [OpenBao production deployment checklist](https://openbao.org/docs/platform/k8s/helm/run/#production-deployment-checklist)
> before using OpenBao in production.

[embedmd]:#(../../../deploy/helmfiles/values/openbao.yaml)
```yaml
global:
  tlsDisable: false

server:
  authDelegator:
    enabled: true
  dataStorage:
    storageClass: local-path
  standalone:
    config: |
      ui = true

      listener "tcp" {
        address = "[::]:8200"
        cluster_address = "[::]:8201"
        tls_cert_file = "/openbao/tls/tls.crt"
        tls_key_file = "/openbao/tls/tls.key"
      }

      storage "file" {
        path = "/openbao/data"
      }

      seal "static" {
        current_key_id = "static-key-1"
        current_key = "file:///openbao/static-seal/unseal.key"
      }
  volumes:
    - name: openbao-server-tls
      secret:
        secretName: openbao-server-tls
    - name: openbao-static-seal
      secret:
        secretName: openbao-static-seal
  volumeMounts:
    - name: openbao-server-tls
      mountPath: /openbao/tls
      readOnly: true
    - name: openbao-static-seal
      mountPath: /openbao/static-seal
      readOnly: true
  extraEnvironmentVars:
    BAO_CACERT: /openbao/tls/ca.crt
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

injector:
  enabled: false

csi:
  enabled: false

extraObjects:
  - apiVersion: cert-manager.io/v1
    kind: Issuer
    metadata:
      name: openbao-selfsigned-issuer
    spec:
      selfSigned: {}
  - apiVersion: cert-manager.io/v1
    kind: Certificate
    metadata:
      name: openbao-server-tls
    spec:
      dnsNames:
        - localhost
        - "{{ include \"openbao.fullname\" . }}"
        - "{{ include \"openbao.fullname\" . }}.{{ include \"openbao.namespace\" . }}"
        - "{{ include \"openbao.fullname\" . }}.{{ include \"openbao.namespace\" . }}.svc"
        - "{{ include \"openbao.fullname\" . }}.{{ include \"openbao.namespace\" . }}.svc.cluster.local"
      ipAddresses:
        - 127.0.0.1
      issuerRef:
        kind: Issuer
        name: openbao-selfsigned-issuer
      secretName: openbao-server-tls
```

</details>

<details markdown="1"><summary><b>external-secrets</b></summary>

[embedmd]:#(../../../deploy/helmfiles/values/external-secrets.yaml)
```yaml
installCRDs: true

extraObjects:
  - apiVersion: cert-manager.io/v1
    kind: Issuer
    metadata:
      name: external-secrets-selfsigned-issuer
      namespace: "{{ .Release.Namespace }}"
    spec:
      selfSigned: {}

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

webhook:
  certManager:
    enabled: true
    cert:
      issuerRef:
        group: cert-manager.io
        kind: Issuer
        name: external-secrets-selfsigned-issuer
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

certController:
  create: false
```

</details>

<details markdown="1"><summary><b>argo-cd</b></summary>

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
configs:
  params:
    # Argo CD can be deployed to a different namespace.
    # Setting namespaces to dpf-operator-system ensures Argo CD reconciles applications in that namespace.
    application.namespaces: dpf-operator-system
```

</details>

<details markdown="1"><summary><b>node-feature-discovery</b></summary>

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

<details markdown="1"><summary><b>maintenance-operator</b></summary>

[embedmd]:#(../../../deploy/helmfiles/values/maintenance-operator-chart.yaml)
```yaml
# Maintenance Operator Chart configuration
operatorConfig:
  deploy: true
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

<details markdown="1"><summary><b>kamaji</b></summary>

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
  selfSignedCertificates:
    enabled: true
  certManager:
    enabled: false
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
    cfssl:
      tag: v1.6.5@sha256:c9018c2ddf0b1f8dbef166057cc751d1becd5c3b0b7014cb9fe06972f725106f
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
  tag: v1.36.0-26.6.4
  pullPolicy: Always
```

</details>

<details markdown="1"><summary><b>local-path-provisioner</b></summary>

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

<details markdown="1"><summary><b>kube-state-metrics</b></summary>

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
          regex: dpf_(bfb|dpfoperatorconfig|dpu[a-z]*)_.+|kube_pod_info|kube_pod_labels|kube_pod_status_phase|kube_pod_status_ready|kube_pod_container_info|kube_pod_container_status_restarts_total|kube_pod_container_status_waiting_reason|kube_(cronjob|daemonset|deployment|endpoint|job|namespace|node|persistentvolumeclaim|persistentvolume|replicaset|resourcequota|service|statefulset)(_.+)?
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

<details markdown="1"><summary><b>kube-prometheus-stack</b></summary>

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

# Do not create the chart's built-in default alert and recording rules. The
# metric allowlists below drop many of their inputs (e.g. kube_pod_owner,
# apiserver_request_sli_duration_seconds, scheduler recording-rule inputs), so
# they would evaluate over empty vectors and never fire. DPF ships its own
# curated alert and recording rules instead.
defaultRules:
  create: false

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
    # Keep only the metrics consumed by the DPF dashboards and alert/recording
    # rules, mirroring the allowlist applied to the DPU cluster control planes
    # in the kamaji cluster manager (getServiceMonitorResource).
    # Note: setting metricRelabelings replaces the chart default, so the chart
    # default histogram bucket drop rule is re-applied after the keep rule,
    # restricted to the histogram families the keep rule lets through.
    metricRelabelings:
      - action: keep
        regex: apiserver_request_total|apiserver_request_duration_seconds_(bucket|sum|count)|apiserver_current_inflight_requests|apiserver_longrunning_requests|apiserver_storage_size_bytes|apiserver_storage_objects|etcd_requests_total|etcd_request_errors_total|etcd_request_duration_seconds_(bucket|sum|count)|process_cpu_seconds_total|process_resident_memory_bytes|process_start_time_seconds
        sourceLabels:
          - __name__
      - action: drop
        regex: (etcd_request|apiserver_request)_duration_seconds_bucket;(0\.15|0\.2|0\.3|0\.35|0\.4|0\.45|0\.6|0\.7|0\.8|0\.9|1\.25|1\.5|1\.75|2|3|3\.5|4|4\.5|6|7|8|9|15|25|30|50)(\.0)?
        sourceLabels:
          - __name__
          - le
kubeControllerManager:
  serviceMonitor:
    relabelings:
      - action: replace
        targetLabel: cluster
        replacement: management
    # Keep only the metrics consumed by the DPF dashboards and alert/recording
    # rules, mirroring the allowlist applied to the DPU cluster control planes.
    metricRelabelings:
      - action: keep
        regex: workqueue_(depth|adds_total|retries_total|queue_duration_seconds_(bucket|sum|count)|work_duration_seconds_(bucket|sum|count))|rest_client_requests_total|leader_election_master_status|process_cpu_seconds_total|process_resident_memory_bytes|process_start_time_seconds
        sourceLabels:
          - __name__
kubeScheduler:
  serviceMonitor:
    relabelings:
      - action: replace
        targetLabel: cluster
        replacement: management
    # Keep only the metrics consumed by the DPF dashboards and alert/recording
    # rules, mirroring the allowlist applied to the DPU cluster control planes.
    metricRelabelings:
      - action: keep
        regex: scheduler_pending_pods|scheduler_schedule_attempts_total|scheduler_scheduling_attempt_duration_seconds_(bucket|sum|count)|process_cpu_seconds_total|process_resident_memory_bytes|process_start_time_seconds
        sourceLabels:
          - __name__
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

    # Enable the remote-write receiver (--web.enable-remote-write-receiver) so the
    # OpenTelemetry Collector can push DPU cluster metrics into Prometheus via its
    # prometheusremotewrite exporter.
    enableRemoteWriteReceiver: true

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
          - source_labels: [__meta_kubernetes_namespace]
            action: replace
            target_label: namespace
          - source_labels: [__meta_kubernetes_pod_name]
            action: replace
            target_label: pod
        # Add cluster label to ALL scraped metrics for Grafana multicluster support
        # This makes the cluster label visible in local queries (unlike externalLabels)
        # Note: The control plane components (kube-apiserver, kube-controller-manager, kube-scheduler)
        # already have cluster labels via their ServiceMonitor relabelings above
        metric_relabel_configs:
          # Keep only the metrics consumed by the DPF dashboards and alert/recording
          # rules. The DPF controllers expose the standard controller-runtime metric
          # set; DPF resource state metrics come from kube-state-metrics instead.
          # The go_* and process_* runtime gauges are kept wholesale for the debug
          # dashboards; they are a handful of series per controller pod.
          - source_labels: [__name__]
            action: keep
            regex: (controller_runtime_.+|workqueue_.+|rest_client_requests_total|leader_election_master_status|certwatcher_read_certificate_errors_total|process_.+|go_.+)
          # Prefix all kept metrics with dpf_ so the DPF controller metrics are
          # namespaced as DPF metrics, consistent with the kube-state-metrics and
          # DPU cluster control-plane (Kamaji) metrics.
          - source_labels: [__name__]
            action: replace
            regex: (.+)
            target_label: __name__
            replacement: dpf_${1}
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

    # Load PrometheusRule resources from all namespaces regardless of labels
    # (e.g. the DPF alert/recording rules from deploy/helmfiles/prometheus-rules/)
    ruleSelectorNilUsesHelmValues: false
    ruleSelector: {}
    ruleNamespaceSelector: {}

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

  # Additional datasources
  additionalDataSources:
    - name: Loki
      type: loki
      uid: loki
      access: proxy
      url: http://loki.dpf-operator-system.svc.cluster.local:3100
      isDefault: false
      editable: true
      jsonData:
        maxLines: 1000
        derivedFields:
          # Automatically extract trace IDs from logs (if present)
          - datasourceName: Tempo
            matcherRegex: "traceID=(\\w+)"
            name: TraceID
            url: "$${__value.raw}"

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

<details markdown="1"><summary><b>loki</b></summary>

[embedmd]:#(../../../deploy/helmfiles/values/loki.yaml)
```yaml
# Loki configuration for management cluster
# This deployment receives logs from OpenTelemetry Collectors running on both
# the management cluster and DPU clusters

deploymentMode: SingleBinary

loki:
  auth_enabled: false

  commonConfig:
    replication_factor: 1

  # Enable OTLP ingestion
  server:
    http_listen_port: 3100
    grpc_listen_port: 9095
    log_level: info

  storage:
    type: 'filesystem'

  schemaConfig:
    configs:
      - from: "2024-01-01"
        store: tsdb
        object_store: filesystem
        schema: v13
        index:
          prefix: loki_index_
          period: 24h

  # Limits configuration (includes OTLP config)
  limits_config:
    retention_period: 168h  # 7 days
    max_query_series: 10000
    max_query_lookback: 720h  # 30 days
    ingestion_rate_mb: 50
    ingestion_burst_size_mb: 100
    per_stream_rate_limit: 10MB
    per_stream_rate_limit_burst: 20MB
    allow_structured_metadata: true
    otlp_config:
      resource_attributes:
        attributes_config:
          - action: index_label
            attributes:
              - k8s.namespace.name
              - k8s.pod.name
              - k8s.container.name
              - cluster

# Single binary mode configuration
singleBinary:
  replicas: 1

  # Schedule on control-plane nodes
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

  # Resources
  resources:
    limits:
      cpu: 1000m
      memory: 2Gi
    requests:
      cpu: 500m
      memory: 1Gi

  # Persistence
  persistence:
    enabled: true
    storageClass: local-path
    size: 10Gi

# Gateway disabled - not needed in SingleBinary mode
# All access goes directly to the Loki service on port 3100
gateway:
  enabled: false

# Read/Write components (disabled in single binary mode)
read:
  replicas: 0

write:
  replicas: 0

backend:
  replicas: 0

# Disable components not needed in single binary mode
chunksCache:
  enabled: false

resultsCache:
  enabled: false

# Monitoring configuration
monitoring:
  serviceMonitor:
    enabled: true
    labels:
      release: kube-prometheus-stack

  selfMonitoring:
    enabled: false
    grafanaAgent:
      installOperator: false

# Test configuration
test:
  enabled: false

# Loki canary (synthetic log generator for testing)
lokiCanary:
  enabled: false
```

</details>


<details markdown="1"><summary><b>opentelemetry-collector</b></summary>

[embedmd]:#(../../../deploy/helmfiles/values/opentelemetry-collector.yaml)
```yaml
# OpenTelemetry Collector configuration for management cluster
# This collector receives logs and metrics from:
# 1. Local management cluster pods (via filelog receiver)
# 2. OpenTelemetry Collectors running on DPU clusters (via OTLP receiver)

mode: daemonset

# Image configuration (required as of chart version 0.145.0)
# Use contrib distribution for Loki exporter support
image:
  repository: otel/opentelemetry-collector-contrib
  tag: ""  # defaults to chart appVersion

# Run on all management cluster nodes to collect logs
# Tolerations allow running on control-plane nodes
tolerations:
  - key: node-role.kubernetes.io/master
    operator: Exists
    effect: NoSchedule
  - key: node-role.kubernetes.io/control-plane
    operator: Exists
    effect: NoSchedule

# Presets for Kubernetes integration
presets:
  logsCollection:
    enabled: true
    includeCollectorLogs: true
  kubernetesAttributes:
    enabled: true
    extractAllPodLabels: true
    extractAllPodAnnotations: false

# OpenTelemetry Collector configuration
config:
  receivers:
    # OTLP receiver for logs and metrics from DPU clusters
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317
        http:
          endpoint: 0.0.0.0:4318

  processors:
    batch:
      timeout: 10s
      send_batch_size: 1024

    memory_limiter:
      check_interval: 5s
      limit_mib: 1024
      spike_limit_mib: 256

    k8sattributes:
      auth_type: "serviceAccount"
      passthrough: false
      extract:
        metadata:
          - k8s.namespace.name
          - k8s.deployment.name
          - k8s.statefulset.name
          - k8s.daemonset.name
          - k8s.cronjob.name
          - k8s.job.name
          - k8s.node.name
          - k8s.pod.name
          - k8s.pod.uid
          - k8s.pod.start_time

    # Add management cluster label (only if not already set by DPU cluster)
    resource:
      attributes:
        - key: cluster
          value: "management"
          action: insert

    # Kamaji control plane pods run here but belong to the DPU cluster named by
    # their kamaji.clastix.io/name label. Runs before the resource processor
    # below, whose insert action then leaves the cluster attribute untouched.
    transform/kamaji:
      error_mode: ignore
      log_statements:
        - context: log
          statements:
            - set(resource.attributes["cluster"], resource.attributes["kamaji.clastix.io/name"])
                where resource.attributes["kamaji.clastix.io/name"] != nil
            # Audit events share the kube-apiserver stdout with its klog output.
            # Tag them for `| log_type="k8s-audit"`.
            - set(log.attributes["log.type"], "k8s-audit")
                where IsMatch(log.body, "\"apiVersion\":\"audit\\.k8s\\.io/")

  exporters:
    # Export logs to Loki via OTLP (directly to Loki, bypassing gateway)
    otlphttp/loki:
      endpoint: http://loki:3100/otlp
      tls:
        insecure: true

    # Export metrics to Prometheus (via remote write)
    prometheusremotewrite:
      endpoint: http://kube-prometheus-stack-prometheus.dpf-operator-system.svc.cluster.local:9090/api/v1/write
      resource_to_telemetry_conversion:
        enabled: true

    # Debug exporter for troubleshooting
    debug:
      verbosity: basic
      sampling_initial: 5
      sampling_thereafter: 200

  service:
    pipelines:
      logs:
        receivers: [otlp, filelog]
        processors: [memory_limiter, k8sattributes, transform/kamaji, resource, batch]
        exporters: [otlphttp/loki, debug]

      metrics:
        receivers: [otlp]
        processors: [memory_limiter, k8sattributes, resource, batch]
        exporters: [prometheusremotewrite, debug]

# Resources for the collector deployment
resources:
  limits:
    cpu: 500m
    memory: 1Gi
  requests:
    cpu: 200m
    memory: 512Mi

# Service configuration
# Use NodePort to allow DPU clusters to reach this collector
service:
  enabled: true
  type: NodePort

# Ports configuration
ports:
  otlp:
    enabled: true
    containerPort: 4317
    servicePort: 4317
    protocol: TCP
  otlp-http:
    enabled: true
    containerPort: 4318
    servicePort: 4318
    # Fixed NodePort for DPU clusters to use. Chosen from the lower static band of
    # the NodePort range to avoid collisions with dynamically auto-assigned NodePorts,
    # which are allocated from the upper band first.
    # See https://github.com/kubernetes/enhancements/tree/master/keps/sig-network/3668-reserved-service-nodeport-range
    nodePort: 30050
    protocol: TCP
  metrics:
    enabled: true
    containerPort: 8888
    servicePort: 8888
    protocol: TCP

# ServiceAccount configuration
serviceAccount:
  create: true
  name: opentelemetry-collector

# ClusterRole permissions
clusterRole:
  create: true
  rules:
    - apiGroups: [""]
      resources: ["pods", "namespaces", "nodes"]
      verbs: ["get", "list", "watch"]
    - apiGroups: ["apps"]
      resources: ["replicasets", "deployments", "daemonsets", "statefulsets"]
      verbs: ["get", "list", "watch"]
    - apiGroups: ["batch"]
      resources: ["jobs", "cronjobs"]
      verbs: ["get", "list", "watch"]

# ServiceMonitor for Prometheus monitoring
serviceMonitor:
  enabled: true
  metricsEndpoints:
    - port: metrics
      # Keep only the collector self-observability metrics (pipeline throughput,
      # queue state, resource usage) and the Prometheus handler error counter.
      metricRelabelings:
        - sourceLabels: [__name__]
          action: keep
          regex: otelcol_.+|promhttp_metric_handler_errors_total
```

</details>
