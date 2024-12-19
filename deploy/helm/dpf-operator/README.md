# dpf-operator

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

DPF Operator manages the lifecycle of a DOCA Platform Framework system.

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://argoproj.github.io/argo-helm | argo-cd | 7.6.8 |
| https://clastix.github.io/charts | kamaji | 1.0.0 |
| https://clastix.github.io/charts | kamaji-etcd | 0.9.0 |
| https://grafana.github.io/helm-charts | grafana | 8.4.6 |
| https://kubernetes-sigs.github.io/node-feature-discovery/charts | node-feature-discovery | 0.16.5 |
| https://prometheus-community.github.io/helm-charts | kube-state-metrics | 5.25.1 |
| https://prometheus-community.github.io/helm-charts | prometheus | 25.26.0 |
| oci://ghcr.io/mellanox | maintenance-operator-chart | 0.1.1 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| argo-cd.dex.enabled | bool | `false` |  |
| argo-cd.enabled | bool | `true` |  |
| argo-cd.global.affinity.nodeAffinity.matchExpressions | list | `[{"key":"node-role.kubernetes.io/master","operator":"Exists"},{"key":"node-role.kubernetes.io/control-plane","operator":"Exists"}]` | Default match expressions for node affinity |
| argo-cd.global.affinity.nodeAffinity.type | string | `"soft"` | Default node affinity rules. Either: `none`, `soft` or `hard` |
| argo-cd.global.podLabels."ovn.dpu.nvidia.com/skip-injection" | string | `""` |  |
| argo-cd.global.tolerations[0].effect | string | `"NoSchedule"` |  |
| argo-cd.global.tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| argo-cd.global.tolerations[0].operator | string | `"Exists"` |  |
| argo-cd.global.tolerations[1].effect | string | `"NoSchedule"` |  |
| argo-cd.global.tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| argo-cd.global.tolerations[1].operator | string | `"Exists"` |  |
| argo-cd.notifications.enabled | bool | `false` |  |
| controllerManager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` |  |
| controllerManager.containerSecurityContext.capabilities.drop[0] | string | `"ALL"` |  |
| controllerManager.image.repository | string | `"example.com/dpf-system"` |  |
| controllerManager.image.tag | string | `"v0.1.0"` |  |
| controllerManager.replicas | int | `1` |  |
| controllerManager.serviceAccount.annotations | object | `{}` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".apiVersion | int | `1` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[0].disableDeletion | bool | `false` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[0].editable | bool | `true` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[0].folder | string | `""` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[0].name | string | `"default"` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[0].options.path | string | `"/var/lib/grafana/dashboards/default"` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[0].orgId | int | `1` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[0].type | string | `"file"` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[1].disableDeletion | bool | `false` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[1].editable | bool | `true` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[1].folder | string | `"debug"` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[1].name | string | `"debug"` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[1].options.path | string | `"/var/lib/grafana/dashboards/debug"` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[1].orgId | int | `1` |  |
| grafana.dashboardProviders."dashboardproviders.yaml".providers[1].type | string | `"file"` |  |
| grafana.dashboardsConfigMaps.debug | string | `"dpf-operator-grafana-debug-dashboards"` |  |
| grafana.dashboardsConfigMaps.default | string | `"dpf-operator-grafana-dashboards"` |  |
| grafana.datasources."datasources.yaml".apiVersion | int | `1` |  |
| grafana.datasources."datasources.yaml".datasources[0].access | string | `"proxy"` |  |
| grafana.datasources."datasources.yaml".datasources[0].isDefault | bool | `true` |  |
| grafana.datasources."datasources.yaml".datasources[0].name | string | `"prometheus"` |  |
| grafana.datasources."datasources.yaml".datasources[0].type | string | `"prometheus"` |  |
| grafana.datasources."datasources.yaml".datasources[0].url | string | `"http://dpf-operator-prometheus-server"` |  |
| grafana.enabled | bool | `false` |  |
| grafana.persistence.enabled | bool | `true` |  |
| imagePullSecrets | list | `[]` |  |
| kamaji-etcd.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| kamaji-etcd.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| kamaji-etcd.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| kamaji-etcd.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| kamaji-etcd.autoCompactionRetention | int | `0` |  |
| kamaji-etcd.datastore.annotations."helm.sh/resource-policy" | string | `"keep"` |  |
| kamaji-etcd.datastore.enabled | bool | `true` |  |
| kamaji-etcd.datastore.name | string | `"default"` |  |
| kamaji-etcd.enabled | bool | `true` |  |
| kamaji-etcd.tolerations[0].effect | string | `"NoSchedule"` |  |
| kamaji-etcd.tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| kamaji-etcd.tolerations[0].operator | string | `"Exists"` |  |
| kamaji-etcd.tolerations[1].effect | string | `"NoSchedule"` |  |
| kamaji-etcd.tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| kamaji-etcd.tolerations[1].operator | string | `"Exists"` |  |
| kamaji.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| kamaji.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| kamaji.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| kamaji.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| kamaji.cfssl.image.tag | string | `"v1.6.5"` |  |
| kamaji.datastore.enabled | bool | `false` |  |
| kamaji.datastore.nameOverride | string | `"default"` |  |
| kamaji.enabled | bool | `true` |  |
| kamaji.etcd.deploy | bool | `false` |  |
| kamaji.image.pullPolicy | string | `"IfNotPresent"` |  |
| kamaji.tolerations[0].effect | string | `"NoSchedule"` |  |
| kamaji.tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| kamaji.tolerations[0].operator | string | `"Exists"` |  |
| kamaji.tolerations[1].effect | string | `"NoSchedule"` |  |
| kamaji.tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| kamaji.tolerations[1].operator | string | `"Exists"` |  |
| kube-state-metrics.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| kube-state-metrics.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| kube-state-metrics.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| kube-state-metrics.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| kube-state-metrics.enabled | bool | `false` |  |
| kube-state-metrics.extraArgs[0] | string | `"--custom-resource-state-config-file=/etc/customresourcestate/config.yaml"` |  |
| kube-state-metrics.rbac.extraRules[0].apiGroups[0] | string | `"svc.dpu.nvidia.com"` |  |
| kube-state-metrics.rbac.extraRules[0].apiGroups[1] | string | `"operator.dpu.nvidia.com"` |  |
| kube-state-metrics.rbac.extraRules[0].apiGroups[2] | string | `"svc.dpu.nvidia.com"` |  |
| kube-state-metrics.rbac.extraRules[0].apiGroups[3] | string | `"provisioning.dpu.nvidia.com"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[0] | string | `"dpuclusters"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[10] | string | `"dpuservicechains"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[11] | string | `"dpuservicechains/status"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[12] | string | `"dpuserviceinterfaces"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[13] | string | `"dpuserviceinterfaces/status"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[14] | string | `"dpuserviceipams"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[15] | string | `"dpuserviceipams/status"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[16] | string | `"dpus"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[17] | string | `"dpus/status"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[18] | string | `"bfbs"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[19] | string | `"bfbs/status"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[1] | string | `"dpuclusters/status"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[2] | string | `"dpudeployments"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[3] | string | `"dpudeployments/status"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[4] | string | `"dpuservicecredentialrequests"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[5] | string | `"dpuservicecredentialrequests/status"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[6] | string | `"dpuservices"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[7] | string | `"dpuservices/status"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[8] | string | `"dpfoperatorconfigs"` |  |
| kube-state-metrics.rbac.extraRules[0].resources[9] | string | `"dpfoperatorconfigs/status"` |  |
| kube-state-metrics.rbac.extraRules[0].verbs[0] | string | `"list"` |  |
| kube-state-metrics.rbac.extraRules[0].verbs[1] | string | `"watch"` |  |
| kube-state-metrics.rbac.extraRules[1].apiGroups[0] | string | `"apiextensions.k8s.io"` |  |
| kube-state-metrics.rbac.extraRules[1].resources[0] | string | `"customresourcedefinitions"` |  |
| kube-state-metrics.rbac.extraRules[1].verbs[0] | string | `"list"` |  |
| kube-state-metrics.rbac.extraRules[1].verbs[1] | string | `"watch"` |  |
| kube-state-metrics.tolerations[0].effect | string | `"NoSchedule"` |  |
| kube-state-metrics.tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| kube-state-metrics.tolerations[0].operator | string | `"Exists"` |  |
| kube-state-metrics.tolerations[1].effect | string | `"NoSchedule"` |  |
| kube-state-metrics.tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| kube-state-metrics.tolerations[1].operator | string | `"Exists"` |  |
| kube-state-metrics.volumeMounts[0].mountPath | string | `"/etc/customresourcestate"` |  |
| kube-state-metrics.volumeMounts[0].name | string | `"customresourcestate-config"` |  |
| kube-state-metrics.volumeMounts[0].readOnly | bool | `true` |  |
| kube-state-metrics.volumes[0].configMap.defaultMode | int | `420` |  |
| kube-state-metrics.volumes[0].configMap.name | string | `"dpf-operator-customresourcestate-config"` |  |
| kube-state-metrics.volumes[0].name | string | `"customresourcestate-config"` |  |
| maintenance-operator-chart.enabled | bool | `true` |  |
| maintenance-operator-chart.operator.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| maintenance-operator-chart.operator.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| maintenance-operator-chart.operator.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| maintenance-operator-chart.operator.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| maintenance-operator-chart.operator.tolerations[0].effect | string | `"NoSchedule"` |  |
| maintenance-operator-chart.operator.tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| maintenance-operator-chart.operator.tolerations[0].operator | string | `"Exists"` |  |
| maintenance-operator-chart.operator.tolerations[1].effect | string | `"NoSchedule"` |  |
| maintenance-operator-chart.operator.tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| maintenance-operator-chart.operator.tolerations[1].operator | string | `"Exists"` |  |
| maintenance-operator-chart.operatorConfig.maxParallelOperations | string | `"60%"` |  |
| node-feature-discovery.enabled | bool | `true` |  |
| node-feature-discovery.gc.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| node-feature-discovery.gc.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| node-feature-discovery.gc.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| node-feature-discovery.gc.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| node-feature-discovery.gc.tolerations[0].effect | string | `"NoSchedule"` |  |
| node-feature-discovery.gc.tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| node-feature-discovery.gc.tolerations[0].operator | string | `"Exists"` |  |
| node-feature-discovery.gc.tolerations[1].effect | string | `"NoSchedule"` |  |
| node-feature-discovery.gc.tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| node-feature-discovery.gc.tolerations[1].operator | string | `"Exists"` |  |
| node-feature-discovery.master.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key | string | `"node-role.kubernetes.io/master"` |  |
| node-feature-discovery.master.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator | string | `"Exists"` |  |
| node-feature-discovery.master.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| node-feature-discovery.master.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[1].matchExpressions[0].operator | string | `"Exists"` |  |
| node-feature-discovery.worker.config.sources.pci.deviceClassWhitelist[0] | string | `"0200"` |  |
| node-feature-discovery.worker.config.sources.pci.deviceLabelFields[0] | string | `"class"` |  |
| node-feature-discovery.worker.config.sources.pci.deviceLabelFields[1] | string | `"vendor"` |  |
| node-feature-discovery.worker.config.sources.pci.deviceLabelFields[2] | string | `"device"` |  |
| node-feature-discovery.worker.enable | bool | `true` |  |
| node-feature-discovery.worker.hostNetwork | bool | `true` |  |
| node-feature-discovery.worker.tolerations[0].key | string | `"node.kubernetes.io/not-ready"` |  |
| node-feature-discovery.worker.tolerations[0].operator | string | `"Exists"` |  |
| prometheus.alertmanager.enabled | bool | `false` |  |
| prometheus.enabled | bool | `false` |  |
| prometheus.extraScrapeConfigs | string | `"- job_name: 'doca-platform-framework'\n  # 15s is a bit often for production but helps to get metrics quicker for development.\n  scrape_interval: 15s\n  metrics_path: /metrics\n  scheme: https\n  authorization:\n    type: Bearer\n    credentials_file: /var/run/secrets/kubernetes.io/serviceaccount/token\n  tls_config:\n    ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt\n    insecure_skip_verify: true\n  kubernetes_sd_configs:\n    - role: pod\n  relabel_configs:\n    - source_labels: [__meta_kubernetes_pod_label_dpu_nvidia_com_component]\n      action: keep\n      regex: \".*-controller-manager\"\n    - source_labels: [__meta_kubernetes_pod_container_port_name]\n      action: keep\n      regex: metrics\n"` |  |
| prometheus.kube-state-metrics.enabled | bool | `false` |  |
| prometheus.prometheus-node-exporter.enabled | bool | `false` |  |
| prometheus.prometheus-pushgateway.enabled | bool | `false` |  |
| tolerations[0].effect | string | `"NoSchedule"` |  |
| tolerations[0].key | string | `"node-role.kubernetes.io/master"` |  |
| tolerations[0].operator | string | `"Exists"` |  |
| tolerations[1].effect | string | `"NoSchedule"` |  |
| tolerations[1].key | string | `"node-role.kubernetes.io/control-plane"` |  |
| tolerations[1].operator | string | `"Exists"` |  |

