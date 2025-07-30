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

For more information on how to write a Helm chart for a `DPUService`, see the [DPUService Development Guide](../developer-guides/dpuservice_development.md).

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

For more information on how to write a Helm chart for a `DPUService`, in order to expose ports, see the [DPUService development guide](../developer-guides/dpuservice_development.md).


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
[dpfctl](../other-operations/dpfctl.md) command line tool to gather information about the `DPUServices`
in the DPU cluster.

```bash
$ dpfctl describe dpuservices --show-conditions=all
NAME                                                  NAMESPACE            READY  REASON   SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig                   dpf-operator-system  True   Success  2h
│           ├─ImagePullSecretsReconciled                                   True   Success  3h
│           ├─SystemComponentsReady                                        True   Success  2h
│           └─SystemComponentsReconciled                                   True   Success  2h
├─DPUServiceCredentialRequests
│ └─DPUServiceCredentialRequest/servicesetcontroller  dpf-operator-system  True   Success  3h
│               ├─SecretReconciled                                         True   Success  3h
│               └─ServiceAccountReconciled                                 True   Success  3h
└─DPUServices
  ├─8 DPUServices...                                  dpf-operator-system  True   Success  3h      See flannel, multus, nvidia-k8s-ipam, ovs-cni, servicechainset-controller,
  │                                                                                                servicechainset-rbac-and-crds, sfc-controller, sriov-device-plugin
  ├─DPUService/blueman-l2xsl                          dpf-operator-system  False  Pending  2h      The following conditions are not ready: ApplicationsReady
  │             ├─ApplicationPrereqsReconciled                             True   Success  2h
  │             ├─ApplicationsReady                                        False  Pending  2h      Application is not ready (Sync: OutOfSync, Health: Healthy). Run 'kubectl describe application
  │             │                                                                                  dpu-cplane-tenant1-blueman-l2xsl -n dpf-operator-system' for details.
  │             ├─ApplicationsReconciled                                   True   Success  2h
  │             ├─ConfigPortsReconciled                                    True   Success  2h
  │             └─DPUServiceInterfaceReconciled                            True   Success  2h
  └─DPUService/dts-rrjlp                              dpf-operator-system  False  Pending  2h      The following conditions are not ready: ApplicationsReady
                ├─ApplicationPrereqsReconciled                             True   Success  2h
                ├─ApplicationsReady                                        False  Pending  2h      Application is not ready (Sync: OutOfSync, Health: Healthy). Run 'kubectl describe application
                │                                                                                  dpu-cplane-tenant1-dts-rrjlp -n dpf-operator-system' for details.
                ├─ApplicationsReconciled                                   True   Success  2h
                ├─ConfigPortsReconciled                                    True   Success  2h
                └─DPUServiceInterfaceReconciled                            True   Success  2h
```

## Critical DPUServices :Configuring DPUService for DPU Readiness
### Issue
DPF by design treats host worker nodes and DPU nodes separately in two different clusters. Pods running on host worker nodes rely on the pods (DPUService) running on the DPU. If for some reason a pod (DPUService) is not in a running state on the DPU (either it did not start or it was started at some point but is no longer running), the host needs to stop accepting new workloads to avoid further instability.

### Configuring a critical DPUService
Not every DPUService is considered critical. It is not ideal to taint the node when regular DPUService instances are not running. To be considered critical, a DPUService must meet the following requirement:

* The DPUService must have the label: `svc.dpu.nvidia.com/critical: ""`

This label tells the controller to treat the DPUService as critical for node readiness.

For example, `sriov-device-plugin` below is marked as a critical DPUService.

```yaml
kubectl get dpuservice sriov-device-plugin -n dpf-operator-system -o yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  creationTimestamp: "2025-06-19T07:51:46Z"
  finalizers:
  - dpu.nvidia.com/DPUService
  generation: 3
  labels:
    applyset.kubernetes.io/part-of: applyset-9XbQ4jCMXrB8b5F-kLrpO_qeoD1ZsDVozUW4QxCTkiU-v1
    dpu.nvidia.com/component: sriov-device-plugin
    svc.dpu.nvidia.com/critical: ""
```

### Node taint

If the pod corresponding to a critical DPUService is not running on the DPU, host worker nodes will be marked with a taint so that they do not accept new workloads.

```yaml
kubectl get node host-01 -o yaml
spec:
  podCIDR: 10.244.1.0/24
  podCIDRs:
  - 10.244.1.0/24
  taints:
  - effect: NoSchedule
    key: dpu.nvidia.com/dpu-ready
```

```bash
kubectl get pods --kubeconfig=/tmp/dpukubeconfig -A
NAMESPACE             NAME                                                            READY   STATUS    RESTARTS      AGE
dpf-operator-system   dpu-cplane-tenant1-nvidia-k8s-ipam-controller-df56b65b4-p2476   1/1     Running   0             82m
dpf-operator-system   dpu-cplane-tenant1-nvidia-k8s-ipam-node-ds-drv78                1/1     Running   0             62m
dpf-operator-system   dpu-cplane-tenant1-ovn-dpu-ovn-kubernete-node-rmbml             7/7     Running   0             35m
dpf-operator-system   dpu-cplane-tenant1-ovs-cni-arm64-b5fqh                          1/1     Running   0             62m
dpf-operator-system   dpu-cplane-tenant1-sfc-controller-node-ds-fjsp5                 1/1     Running   0             62m
dpf-operator-system   kube-flannel-ds-5cctx                                           1/1     Running   1 (54m ago)   62m
dpf-operator-system   kube-multus-ds-nlbnd                                            1/1     Running   0             62m
kube-system           coredns-68d4f9776f-5p2qm                                        1/1     Running   0             82m
kube-system           coredns-68d4f9776f-8mxs9                                        1/1     Running   0             82m
kube-system           kube-proxy-jlh58                                                1/1     Running   0             62m
```

When the pod comes back up, the taint on the node will be removed and it will be ready to accept new customer workloads.

```yaml
kubectl get node host-01 -o yaml
spec:
  podCIDR: 10.244.1.0/24
  podCIDRs:
  - 10.244.1.0/24
```
```bash
kubectl get pods --kubeconfig=/tmp/dpukubeconfig -A
NAMESPACE             NAME                                                            READY   STATUS    RESTARTS       AGE
dpf-operator-system   dpu-cplane-tenant1-nvidia-k8s-ipam-controller-df56b65b4-p2476   1/1     Running   0              133m
dpf-operator-system   dpu-cplane-tenant1-nvidia-k8s-ipam-node-ds-drv78                1/1     Running   0              113m
dpf-operator-system   dpu-cplane-tenant1-ovn-dpu-ovn-kubernete-node-rmbml             7/7     Running   0              86m
dpf-operator-system   dpu-cplane-tenant1-ovs-cni-arm64-b5fqh                          1/1     Running   0              113m
dpf-operator-system   dpu-cplane-tenant1-sfc-controller-node-ds-fjsp5                 1/1     Running   0              113m
dpf-operator-system   kube-flannel-ds-5cctx                                           1/1     Running   1 (105m ago)   113m
dpf-operator-system   kube-multus-ds-nlbnd                                            1/1     Running   0              113m
dpf-operator-system   kube-sriov-device-plugin-shnd4                                  1/1     Running   0              38m
kube-system           coredns-68d4f9776f-5p2qm                                        1/1     Running   0              134m
kube-system           coredns-68d4f9776f-8mxs9                                        1/1     Running   0              134m
kube-system           kube-proxy-jlh58                                                1/1     Running   0              113m
```

### Troubleshooting
Users can grep for the `dpuready` string in the dpuservice-controller-manager pod logs:

```bash
kubectl logs dpuservice-controller-manager-56cdc86468-qvjdw -n dpf-operator-system | grep dpuready
I0619 09:39:35.905116       1 dpuready_controller.go:198] "Adding taint to node" controller="node" controllerGroup="" controllerKind="Node" Node="host-01" namespace="" name="host-01" reconcileID="6c4cf2cb-d271-4184-9e4c-da41e3fe62bc" nodeName="host-01"
I0619 09:46:01.170252       1 dpuready_controller.go:216] "Removing taint from node" controller="node" controllerGroup="" controllerKind="Node" Node="host-01" namespace="" name="host-01" reconcileID="445f574a-0a10-49c2-a1c2-7d6ebbca9d96" nodeName="host-01"
```

**Note:**
Adding/removing the taint on the node does not happen immediately at the moment. It happens as part of a periodic 10-minute loop. In the future, it will be changed to react instantly.

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
