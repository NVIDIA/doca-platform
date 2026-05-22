# dpf-operator

![Version: v0.1.0](https://img.shields.io/badge/Version-v0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v0.1.0](https://img.shields.io/badge/AppVersion-v0.1.0-informational?style=flat-square)

DPF Operator manages the lifecycle of a DOCA Platform Framework system.

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"node-role.kubernetes.io/master","operator":"Exists"}]},{"matchExpressions":[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists"}]}]}}}` | affinity controls scheduling of the controller Pod. Defaults pin the controller to control-plane nodes. |
| controllerManager | object | `{"containerSecurityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}},"image":{"repository":"","tag":""},"podSecurityContext":{"runAsNonRoot":true,"runAsUser":65532},"pullPolicy":"IfNotPresent","replicas":1,"serviceAccount":{"annotations":{}}}` | controllerManager configures the DPF Operator controller Deployment. |
| controllerManager.containerSecurityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}}` | containerSecurityContext sets the securityContext applied to the controller container. |
| controllerManager.image | object | `{"repository":"","tag":""}` | image overrides the controller container image. Defaults to the image baked into the chart release when left empty. |
| controllerManager.podSecurityContext | object | `{"runAsNonRoot":true,"runAsUser":65532}` | podSecurityContext sets the pod-level securityContext for the controller Pod. |
| controllerManager.pullPolicy | string | `"IfNotPresent"` | pullPolicy is the imagePullPolicy used for the controller container. |
| controllerManager.replicas | int | `1` | replicas is the number of controller Pods to run. |
| controllerManager.serviceAccount | object | `{"annotations":{}}` | serviceAccount configures the controller ServiceAccount. |
| controllerManager.serviceAccount.annotations | object | `{}` | annotations added to the controller ServiceAccount (e.g. for cloud workload identity). |
| deprecationWarnings | object | `{"enabled":true}` | deprecationWarnings enables ValidatingAdmissionPolicies that warn users when they set deprecated fields on DPF custom resources. Warnings are non-blocking and appear in kubectl output. |
| enableNodeFeatureRules | bool | `true` | enableNodeFeatureRules decides whether additional NodeFeatureRules for DPF will be created. Note: NFD must be installed to support this. |
| grafanaDashboards | object | `{"enabled":true}` | grafanaDashboards enables the Grafana dashboards for the DPF Operator. |
| imagePullSecrets | list | `[]` | imagePullSecrets is a list of Secret references used to pull controller and component images from private registries. |
| isOpenshift | bool | `false` | IsOpenShift templates resources - for example ClusterRoleBindings to SecurityContextConstraints - which are relevant when installing DPF using helm on OpenShift. |
| kamajiEtcdDefrag | object | `{"backoffLimit":6,"clientPort":2379,"defragRule":"dbQuotaUsage > 0.8 || dbSize - dbSizeInUse > 200*1024*1024","enabled":true,"image":"ghcr.io/ahrtr/etcd-defrag:v0.22.0@sha256:a7424de0a437f54d7565e96f8a50913e0ace05398bb5c10a77d5af5fc9bf9301","namespaceOverride":"","pullPolicy":"IfNotPresent","releaseName":"kamaji-etcd","replicas":3,"schedule":"0 0 * * *","successfulJobsHistoryLimit":3}` | kamajiEtcdDefrag enables the etcd-defrag job for Kamaji. This job is used to defragment the etcd database used by Kamaji. |
| kamajiEtcdDefrag.backoffLimit | int | `6` | Limit the number of retries on failure |
| kamajiEtcdDefrag.clientPort | int | `2379` | The client port of the etcd cluster. |
| kamajiEtcdDefrag.defragRule | string | `"dbQuotaUsage > 0.8 || dbSize - dbSizeInUse > 200*1024*1024"` | The defrag rule for the etcd-defrag job. See: https://github.com/ahrtr/etcd-defrag?tab=readme-ov-file#defragmentation-rule |
| kamajiEtcdDefrag.enabled | bool | `true` | enabled toggles the etcd-defrag CronJob. |
| kamajiEtcdDefrag.image | string | `"ghcr.io/ahrtr/etcd-defrag:v0.22.0@sha256:a7424de0a437f54d7565e96f8a50913e0ace05398bb5c10a77d5af5fc9bf9301"` | image is the container image used by the etcd-defrag CronJob. |
| kamajiEtcdDefrag.namespaceOverride | string | `""` | namespaceOverride allows to override the namespace where the etcd-defrag job will be deployed. |
| kamajiEtcdDefrag.pullPolicy | string | `"IfNotPresent"` | The image pull policy for the etcd-defrag job. |
| kamajiEtcdDefrag.releaseName | string | `"kamaji-etcd"` | releaseName is the name of the kamaji-etcd release name. If it is deployed by the kamaji chart itself it will be "kamaji-etcd". |
| kamajiEtcdDefrag.replicas | int | `3` | The replica count of the etcd cluster. |
| kamajiEtcdDefrag.schedule | string | `"0 0 * * *"` | The schedule for the etcd-defrag job. |
| kamajiEtcdDefrag.successfulJobsHistoryLimit | int | `3` | Keep only the X recent successful jobs. |
| kubeStateMetricsCRDMetrics | object | `{"enabled":true,"namespaceOverride":""}` | kubeStateMetricsCRDMetrics enables the kube-state-metrics custom resource state metrics. This is used to collect metrics for custom resources defined by the DPF Operator. |
| prometheusSecureMetrics | object | `{"enabled":true}` | prometheusSecureMetrics enables the secure metrics endpoint for Prometheus. This is used to expose metrics securely for Prometheus scraping. |
| tolerations | list | `[{"effect":"NoSchedule","key":"node-role.kubernetes.io/master","operator":"Exists"},{"effect":"NoSchedule","key":"node-role.kubernetes.io/control-plane","operator":"Exists"}]` | tolerations applied to the controller Pod. Defaults allow scheduling on tainted control-plane nodes. |

