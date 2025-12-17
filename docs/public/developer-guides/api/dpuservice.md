---
title: "DPUService"
---

[TOC]

The `DPUService` CRD provides an API for DPF users to deploy applications on DPU
nodes using Helm charts.

## Example

The following is an example of a `DPUService` YAML for the blueman service:

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: doca-blueman-service
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: 1.0.8
      chart: doca-blueman
  serviceDaemonSet:
    updateStrategy:
      type: RollingUpdate
      rollingUpdate:
        maxUnavailable: 2
    labels:
      dpuservice.dpu.nvidia.com/name: doca-blueman-service
    annotations:
      dpuservice.dpu.nvidia.com/name: doca-blueman-service
```

In the above example:

* A `DPUService` named `doca-blueman-service` is created in the `dpf-operator-system` namespace.
* A HelmChart located at `https://helm.ngc.nvidia.com/nvidia/doca` with version `1.0.5`
  and chart `doca-blueman` is used to deploy the `DPUService`.
* A `DaemonSet` is deployed on target DPU nodes. We specify the update strategy,
  labels, and annotations for the `DaemonSet`.

## Managing Lifecycle of DPUServices

A [DPUDeployment](./dpudeployment.md) resource is the recommended way to deploy and manage `DPUServices`
in DPF. The `DPUDeployment` resource is a higher level abstraction that allows to
manage the lifecycle of `DPUServices` in the DPU cluster. It provides a way to deploy,
upgrade, validate dependencies and resources requirements for `DPUServices`.

## Writing a DPUService Spec

### Helm Chart Configuration

The `spec.helmChart` field is used to specify the Helm chart that will be used to 
deploy the `DPUService`. The `spec.helmChart.source` field is used to specify the Helm chart repository
URL, version, and chart name.

```yaml
spec:
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: 1.0.8
      chart: doca-blueman
```
The following fields are used to specify the Helm chart:

* `repoURL` - the URL of the Helm chart repository. `OCI` and `HTTP` repositories
  are supported.
* `version` - the version of the Helm chart to be used.
* `chart` - the name of the Helm chart.

It is possible to specify the path to the Helm chart in the repository with the
`spec.helmChart.source.path` field.

Optionally, the `spec.helmChart.source.releaseName` field can be used to specify
the release name of the released Helm chart.

### Service DaemonSet Values

The `spec.serviceDaemonSet` field is used to specify the configuration of the DaemonSet.
It represents Helm values to be used during the release of the Helm chart.
These values take precedence over the values defined in the Helm chart.

```yaml
spec:
  serviceDaemonSet:
    updateStrategy:
      type: RollingUpdate
      rollingUpdate:
        maxUnavailable: 2
    nodeSelector:
      nodeSelectorTerms:
        - matchExpressions:
          - key: "bfb"
            operator: In
            values: ["dk"]
    labels:
      "some-label": "some-value"
    annotations:
      "some-annotation": "some-value"
    resources:
      resources:
        memory: 6Gi
        nvidia.com/bf_sf: 3
```

The following fields are used to specify the DaemonSet configuration:

* `updateStrategy` - the update strategy for the DaemonSet. This should be a valid
  Kubernetes `DaemonSetUpdateStrategy` object.
* `nodeSelector` - the node selector for the DaemonSet. This field is used to
  select the nodes where the DaemonSet's Pods will be deployed.
* `labels` - the labels to be applied to the DaemonSet's Pods.
* `annotations` - the annotations to be applied to the DaemonSet's Pods.
* `resources` - the resource requests and limits for the DaemonSet's Pods.

For more information on how to write a Helm chart for a `DPUService`, see the [DPUService Development Guide](../services/dpuservice-development.md).

### DPUClusterSelector

The `spec.dpuClusterSelector` field is used to select which DPU clusters the
service is deployed to. It uses standard Kubernetes label selector syntax
(`matchLabels` and `matchExpressions`) to match against `DPUCluster` labels. If
not specified, the service is deployed to all DPU clusters.

> [!WARNING]
> `dpuClusterSelector` cannot be set when `deployInCluster` is
> `true`. When deploying to the host cluster, cluster selection is not applicable.

Example:

```yaml
spec:
  dpuClusterSelector:
    matchExpressions:
      - key: region
        operator: In
        values:
          - us-west-1
          - us-west-2
```

### Deploy a DPUService in the Host Cluster

It is possible to deploy a `DPUService` in the host cluster, i.e. the cluster where
the DPF operator is running. This can be done by setting the `spec.deployInCluster`
field in the `DPUService` manifest to `true`.

### DPUServiceInterface dependencies

A `DPUService` can have dependencies on `DPUServiceInterface` resources. These resources
represent interfaces that are required by the `DPUService` to function properly.

The `DPUServiceInterface` resources must exist for the `DPUService` to be deployed
successfully. If the `DPUServiceInterface` resources are not found in the cluster,
the `DPUService` will not be deployed and a condition will be set to `false` with a
reason on the `status` field of the `DPUService` resource.

To define a dependency `DPUServiceInterface` resources, the `spec.interfaces` field
of the `DPUService` resource must be set to the names of the `DPUServiceInterfaces`.

```yaml
spec:
  interfaces:
    - p0-sif
    - p1-if
    - app-if
```

### ConfigPorts

A `DPUService` workload can expose multiple ports to the host cluster. The `configPorts`
field is used to specify the ports that should be exposed from the DPU nodes to the host
cluster.

```yaml
spec:
  configPorts:
    serviceType: NodePort
    ports:
      - name: port1
        protocol: TCP
        nodePort: 30001
      - name: port2
        protocol: TCP
        nodePort: 30000
```

The `spec.configPorts.serviceType` field defines the type of service to be created
for the `DPUService`. It can be `NodePort`, `ClusterIP`, or `None` with `NodePort` being
the default value.

The `spec.configPorts.ports` field is used to specify the ports that should be exposed
from the DPU nodes to the host cluster. Each port should have the following fields:

* `name` - the name of the port. This field is a unique identifier and must reference
  a port defined in the Helm chart.
* `port` - the port that will be exposed on the host cluster for use by in-cluster
  workloads.
* `protocol` - the protocol to be used for the port. It can be `TCP` or `UDP`.
* `nodePort` - the external port to be exposed on the host cluster. This field is optional
  and is used only when the `spec.configPorts.serviceType` field is set to `NodePort`.

Upon reconciliation, the `DPUService` controller will retrieve the list of services
that exist in the DPU cluster for this `DPUService`. It will retrieve the ports
that are matching the ports defined in the `spec.configPorts.ports` field. It uses
the `name` field to match the ports. If the ports are found, the `nodePort` values
will be collected. The controller will then make the collected `nodePorts` accessible
from the host cluster by creating a new `Service` resource.

For more information on how to write a Helm chart for a `DPUService`, in order to expose ports, see the [DPUService development guide](../services/dpuservice-development.md).


## DPUService Update

The `DPUService` resource can be updated by changing the fields in the `spec` section
of the resource. The controller will detect the changes and update the `DPUService`
accordingly. The `DPUService` will not be recreated during the update process.

## Working with DPUServices

### Waiting for Ready

When a `DPUService` is created, it may take some time for the workload to be ready.

It is possible to wait for a `DPUService` to be ready by using the `kubectl wait` command:

```bash
$ kubectl wait --for=condition=Ready dpuservice/<dpuservice-name> -n <namespace>
```

### Suspending and Resuming

#### Suspending a DPUService

The need to temporarily suspend a `DPUService` may arise when performing maintenance
or troubleshooting. The `DPUService` can be suspended by setting the `spec.paused`
field to `true`.

```yaml
spec:
  paused: true
```

Or by using the `kubectl patch` command:

```bash
$ kubectl patch dpuservice <dpuservice-name> \
-n <namespace> \ 
--field-manager=dpf-client-side-apply \
--type='json' \
-p='[{"op": "replace", "path": "/spec/paused", "value": true}]'
```

**Note**: When a `DPUService` is suspended, the controller will not reconcile the resource
until the `spec.paused` field is set to `false`. This means that the `DPUService`
can neither be updated nor deleted while it is suspended.

#### Resuming a DPUService

To resume a suspended `DPUService`, set the `spec.paused` field to `false`.

```yaml
spec:
  paused: false
```

Or by using the `kubectl patch` command:

```bash
$ kubectl patch dpuservice <dpuservice-name> \
-n <namespace> \
--field-manager=dpf-client-side-apply \
--type='json' \
-p='[{"op": "replace", "path": "/spec/paused", "value": false}]'
```

### Debugging DPUServices

There are several ways to debug `DPUServices` in DPF. The recommended way is to use the
[dpfctl](../../troubleshooting/dpfctl.md) command line tool to gather information about the `DPUServices`
in the DPU cluster.

```bash
$ dpfctl describe dpuservices --show-conditions=all
NAME                                                  NAMESPACE            STATUS       REASON   SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig                   dpf-operator-system
│           ├─Ready                                                        True         Success  28h
│           ├─ImagePullSecretsReconciled                                   True         Success  5d1h
│           ├─PreUpgradeValidationReady                                    True         Success  5d1h
│           ├─SystemComponentsReady                                        True         Success  28h
│           └─SystemComponentsReconciled                                   True         Success  5d1h
├─DPUServiceCredentialRequests
│ └─DPUServiceCredentialRequest/servicesetcontroller  dpf-operator-system
│               ├─Ready                                                    True         Success  5d1h
│               ├─SecretReconciled                                         True         Success  5d1h
│               └─ServiceAccountReconciled                                 True         Success  5d1h
├─DPUServices
│ └─4 DPUServices...                                  dpf-operator-system  Ready: True  Success  3d7h   See ovn-central-9558p, ovn-controller-v5bkr, vpc-ovn-controller-7sbp6, vpc-ovn-node-r84zn
└─System Components                                   dpf-operator-system
  └─DPUServices
    └─8 DPUServices...                                dpf-operator-system  Ready: True  Success  5d1h   See flannel, multus, nvidia-k8s-ipam, ovs-cni, servicechainset-controller,
                                                                                                        servicechainset-rbac-and-crds, sfc-controller, sriov-device-plugin
```

## Configuring Critical DPUService for DPU Readiness
### Overview
In DPF architecture, host worker nodes and DPUs run in separate Kubernetes clusters. Some host workloads rely on corresponding DPU pods (DPUServices). If a critical DPUService is missing or not running on the DPU, the host should stop accepting new workloads to avoid instability.

To enforce this, DPF introduces a mechanism that taints the host node when a critical DPUService is not running.

### How to Mark a DPUService as Critical
To mark a DPUService as critical, add the following label:
`svc.dpu.nvidia.com/critical: ""`

Example:
`kubectl label dpuservice sriov-device-plugin svc.dpu.nvidia.com/critical="" -n dpf-operator-system`

To remove the critical label, use:
`kubectl label dpuservice sriov-device-plugin svc.dpu.nvidia.com/critical- -n dpf-operator-system`

### Example Manifest Snippet
```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: sriov-device-plugin
  namespace: dpf-operator-system
  labels:
    svc.dpu.nvidia.com/critical: ""
```

### Taint Behavior
When a pod of a critical DPUService is not in a Running state on the DPU, the corresponding host node is tainted:
```yaml
spec:
  taints:
  - effect: NoSchedule
    key: dpu.nvidia.com/dpu-ready
```
When the pod becomes `Ready` again, the taint is removed.

### Recovery Process
Once the failed critical DPUService pod is running again, the following occurs:

* The controller detects this change during its next reconciliation loop.
* The `dpu.nvidia.com/dpu-ready` taint is removed.
* Host node resumes scheduling workloads.

### Troubleshooting
To check controller logs, use:

```
kubectl logs dpuservice-controller-manager-<pod> -n dpf-operator-system | grep dpuready
```

Example:

```
I0619 09:39:35.905116 dpuready_controller.go:198] Adding taint to node Node="host-01"
I0619 09:46:01.170252 dpuready_controller.go:216] Removing taint from node Node="host-01"
```

### Limitations
* Tainting is based on a 10-minute periodic reconciliation.
* Only pods for critical DPUServices labeled as svc.dpu.nvidia.com/critical are considered.
* Taint is only removed once the pod is Ready.
* Feature currently does not trigger instant reconciliation (planned for future releases).

### Quick Reference

| Item | Description |
|------|-------------|
| Label key | `svc.dpu.nvidia.com/critical` |
| Taint key | `dpu.nvidia.com/dpu-ready` |
| Taint effect | `NoSchedule` |
| Taint behavior | Applied if a critical DPUService pod is not running on the DPU |
| Recovery | Taint is removed once the pod is running and ready |
| Check logs with | `kubectl logs \| grep dpuready` |
| Reconciliation interval | Every 10 minutes (will become event-driven in next release) |

### Final Notes
This feature is essential for ensuring host-side stability when critical DPU infrastructure is missing or unhealthy. Proper labeling and understanding of the DPU-host relationship are required for effective use of this functionality.

## Dividing the cluster into several zones (DEPRECATED - Use DPUDeployment instead)

For a better control of maintenance and down-time, the cluster can be logically divided into several "zones".
Each zone can have its own set of DPU services, that can be upgraded individually, affecting only the specific zone.
The creation of zones for DPU services is done by adding labels on the nodes in the DPU cluster and then using them with the DPU service YAML:


Create a specific DPU Set for worker nodes labeled as "e2e.servers/dk=true",
by adding the "cluster -> nodeLabels" section, assign their DPUs the label "bfb=dk" (on the DPU cluster).

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: dpuset-dk
  namespace: dpf-operator-system
spec:
  dpuNodeSelector:
    matchLabels:
      e2e.servers/dk: "true"
  strategy:
  rollingUpdate:
    maxUnavailable: "10%"
  type: RollingUpdate
  dpuTemplate:
  spec:
    dpuFlavor: dpf-provisioning-hbn-ovn
    bfb:
    name: bf-bundle-dk-ga
    nodeEffect:
    taint:
      key: "dpu"
      value: "provisioning"
      effect: NoSchedule
    cluster:
    nodeLabels:
      bfb: "dk"
```
Then use the assigned label to create an HBN DPU Service for these specific nodes (under the "nodeSelector" section):

```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: doca-hbn-dk
  namespace: dpf-operator-system
spec:
  serviceID: doca-hbn
  interfaces:
  - p0-sf-dk
  - p1-sf-dk
  - app-sf-dk
  serviceDaemonSet:
  nodeSelector:
    nodeSelectorTerms:
    - matchExpressions:
    - key: "bfb"
        operator: In
      values: ["dk"]
  annotations:
    k8s.v1.cni.cncf.io/networks: |-
    [
    {"name": "iprequest", "interface": "ip_lo", "cni-args": {"poolNames": ["loopback"], "poolType": "cidrpool"}},
    {"name": "iprequest", "interface": "ip_pf2dpu3", "cni-args": {"poolNames": ["pool1"], "poolType": "cidrpool", "allocateDefaultGateway": true}}
        ]
  helmChart:
  source:
    repoURL: https://helm.ngc.nvidia.com/nvidia/doca
    version: 1.0.1
    chart: doca-hbn
  values:
    image:
    repository: nvcr.io/nvidia/doca/doca_hbn
    tag: 2.4.1-doca2.9.1
    resources:
    memory: 6Gi
    nvidia.com/bf_sf: 3
    configuration:
    perDPUValuesYAML: |
    - hostnamePattern: "*"
      values:
        bgp_autonomous_system: 65111
        bgp_peer_group: hbn
    startupYAMLJ2: |
      - header:
        model: BLUEFIELD
        nvue-api-version: nvue_v1
        rev-id: 1.0
        version: HBN 2.4.0
      - set:
        interface:
        lo:
          ip:
          address:
            {{ ipaddresses.ip_lo.ip }}/32: {}
          type: loopback
        p0_if,p1_if:
          type: swp
          link:
          mtu: 9000
        pf2dpu3_if:
          ip:
          address:
            {{ ipaddresses.ip_pf2dpu3.cidr }}: {}
          type: swp
          link:
          mtu: 9000
        router:
        bgp:
          autonomous-system: {{ config.bgp_autonomous_system }}
          enable: on
          graceful-restart:
          mode: full
          router-id: {{ ipaddresses.ip_lo.ip }}
        vrf:
        default:
          router:
          bgp:
            address-family:
            ipv4-unicast:
              enable: on
              redistribute:
              connected:
                enable: on
            ipv6-unicast:
              enable: on
              redistribute:
              connected:
                enable: on
            enable: on
            neighbor:
            p0_if:
              peer-group: {{ config.bgp_peer_group }}
              type: unnumbered
            p1_if:
              peer-group: {{ config.bgp_peer_group }}
              type: unnumbered
            path-selection:
            multipath:
              aspath-ignore: on
            peer-group:
            {{ config.bgp_peer_group }}:
              remote-as: external
```

You can do the same for the additional required YAMLs (interfaces and chains):

```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: app-sf-dk
  namespace: dpf-operator-system
spec:
  template:
  spec:
    nodeSelector:
    matchLabels:
      bfb: "dk"
    template:
    metadata:
      labels:
      svc.dpu.nvidia.com/interface: "app_sf"
      svc.dpu.nvidia.com/service: doca-hbn
    spec:
      interfaceType: service
      service:
      serviceID: doca-hbn
      network: mybrhbn
      interfaceName: pf2dpu3_if
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p0-sf-dk
  namespace: dpf-operator-system
spec:
  template:
  spec:
    nodeSelector:
    matchLabels:
      bfb: "dk"
    template:
    metadata:
      labels:
      svc.dpu.nvidia.com/interface: "p0_sf"
      svc.dpu.nvidia.com/service: doca-hbn
    spec:
      interfaceType: service
      service:
      serviceID: doca-hbn
      network: mybrhbn
      interfaceName: p0_if
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p1-sf-dk
  namespace: dpf-operator-system
spec:
  template:
  spec:
    nodeSelector:
    matchLabels:
      bfb: "dk"
    template:
    metadata:
      labels:
      svc.dpu.nvidia.com/interface: "p1_sf"
      svc.dpu.nvidia.com/service: doca-hbn
    spec:
      interfaceType: service
      service:
      serviceID: doca-hbn
      network: mybrhbn
      interfaceName: p1_if
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceChain
metadata:
  name: hbn-to-fabric-dk
  namespace: dpf-operator-system
spec:
  template:
  spec:
    nodeSelector:
    matchLabels:
      bfb: "dk"
    template:
    spec:
      switches:
      - ports:
        - serviceInterface:
          matchLabels:
          uplink: p0
        - serviceInterface:
          matchLabels:
          svc.dpu.nvidia.com/service: doca-hbn
          svc.dpu.nvidia.com/interface: "p0_sf"
      - ports:
        - serviceInterface:
          matchLabels:
          uplink: p1
        - serviceInterface:
          matchLabels:
          svc.dpu.nvidia.com/service: doca-hbn
          svc.dpu.nvidia.com/interface: "p1_sf"
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceChain
metadata:
  name: ovn-to-hbn-dk
  namespace: dpf-operator-system
spec:
  template:
  spec:
    nodeSelector:
    matchLabels:
      bfb: "dk"
    template:
    spec:
      switches:
      - ports:
        - serviceInterface:
          matchLabels:
          svc.dpu.nvidia.com/service: doca-hbn
          svc.dpu.nvidia.com/interface: "app_sf"
        - serviceInterface:
          matchLabels:
          port: ovn
```
