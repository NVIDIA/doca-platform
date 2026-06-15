---
title: "DPUServiceIPAM"
---

[[_TOC_]]

This document describes how a user can work with the `DPUServiceIPAM` Custom Resource. A `DPUServiceIPAM` object is
responsible for doing the IP Address Management in the DPU cluster, IPs which the rest of the DPF objects like
`DPUServiceChain` can consume. Under the hood, this object is using [nvidia-k8s-ipam](https://github.com/Mellanox/nvidia-k8s-ipam).

## Modes

The `DPUServiceIPAM` can operate in 2 distinct modes:

* `.spec.ipv4Subnet`: Share a subnet across all the nodes by splitting the subnet in smaller unique chunks per node. In
  this mode, there is a single gateway for the whole subnet the `DPUServiceIPAM` is managing.
* `.spec.ipv4Network`: Split a CIDR into a unique subnet per node. In this mode, each node has its own gateway.

## Created Child Custom Resources

As previously mentioned, the `DPUServiceIPAM` controller leverages the [nvidia-k8s-ipam](https://github.com/Mellanox/nvidia-k8s-ipam)
project. The controller creates either a [CIDRPool](https://github.com/Mellanox/nvidia-k8s-ipam?tab=readme-ov-file#cidrpool-cr)
or a [IPPool](https://github.com/Mellanox/nvidia-k8s-ipam#ippool-cr) Custom Resource in the underlying DPU cluster
depending on the mode the `DPUServiceIPAM` is configured.

Switching from one mode to another leads to the previous Custom Resource being deleted and the new one to be created.

Removing the pool does not affect running Pods. Readding the same pool, assuming that there was no node addition/deletion
in the DPU cluster, is a safe operation and will not lead to duplicate IP consumption by Pods.

In order to avoid duplicating docs related to the usage of each field, take a look at the [API Documentation](./api.md#dpuserviceipam).
Note that there are additional fields that can be used but in the interest of simplicity, this guide uses only the
required ones.

### Split subnet into chunk per node

To split a subnet into chunks per node, all of them sharing the same gateway, we have to create a Custom Resource like
this:

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: example-pool1
spec:
  metadata:
    labels:
      # Specifying a label here enables us to make use of this object in DPUServiceChain.
      svc.dpu.nvidia.com/pool: example-pool1
  ipv4Subnet:
    subnet: "192.168.0.0/20"
    gateway: "192.168.0.1"
    perNodeIPCount: 256
```

This configuration creates chunks of `256` available IPs for each node with gateway `192.168.0.1` for all of those
nodes.

### Split CIDR into subnet per node

To split a CIDR into a subnet per node each having a distinct gateway, we have to create a Custom Resource like this:

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: example-pool1
spec:
  metadata:
    labels:
      # Specifying a label here enables us to make use of this object in DPUServiceChain.
      svc.dpu.nvidia.com/pool: example-pool1
  ipv4Network:
    network: "192.168.0.0/20"
    # Optional field that usually makes sense to set, unless we use a point to point network (/31) and we want to use
    # both IPs.
    gatewayIndex: 1
    prefixSize: 24
```

This configuration creates subnets of size `/24` for each node with gateway the first IP in that subnet.

## Multi-DPUCluster Support

When a `DPUServiceIPAM` targets multiple DPU clusters, each cluster must receive a non-overlapping slice of the address
space. This is controlled by a single field added to each mode:

* `.spec.ipv4Subnet.blocksPerDPUCluster`: number of `perNodeIPCount`-sized blocks each DPUCluster receives.
* `.spec.ipv4Network.subnetsPerDPUCluster`: number of `prefixSize`-sized subnets each DPUCluster receives.

If either field is omitted and the `DPUServiceIPAM` matches more than one DPU cluster, the controller reports an error
in the status and does not reconcile until the field is populated.

### Subnet mode across multiple DPU clusters

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: example-pool1
spec:
  ipv4Subnet:
    subnet: "192.168.0.0/25"
    gateway: "192.168.0.1"
    perNodeIPCount: 6
    # /25 gives 128 IPs; minus network and broadcast = 126, allocatable = 21 full blocks of 6.
    # Each DPUCluster receives 10 blocks (60 IPs), leaving 1 block spare for a second cluster or future growth.
    blocksPerDPUCluster: 10
```

### Network mode across multiple DPU clusters

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: example-pool1
spec:
  ipv4Network:
    network: "192.168.0.0/20"
    gatewayIndex: 1
    prefixSize: 24
    # Each DPUCluster receives 4 /24 subnets.
    subnetsPerDPUCluster: 4
```

### How allocations are assigned

The controller computes non-overlapping allocations once per reconcile and stores the result in
`.status.dpuClusterAllocations` (keyed by DPUCluster `<namespace>/<name>`). The underlying CIDRPool or IPPool in each
DPU cluster is then configured with exclusion ranges covering everything outside that cluster's allocation, ensuring
nodes in different clusters never receive duplicate IPs.

Allocations are stable across reconciles: existing slices are preserved and only extended when
`blocksPerDPUCluster` / `subnetsPerDPUCluster` grows. If a DPU cluster is removed from the selector its
blocks are freed and may be reassigned.

### Advanced: per-cluster allocation using `dpuClusterSelector` and `excludeRanges`

> **Note:** This is an advanced use case. For most scenarios, prefer `blocksPerDPUCluster` /
> `subnetsPerDPUCluster` which handles allocation automatically and keeps `excludeRanges` in sync.

For cases where each DPU cluster requires a custom-sized allocation, create one `DPUServiceIPAM` per cluster using
`.spec.dpuClusterSelector` to pin each object to a single cluster. Both objects reference the **same parent
subnet/network** and use `excludeRanges` to carve out the portion that belongs to the other cluster — this is exactly
what the controller does automatically when `blocksPerDPUCluster` / `subnetsPerDPUCluster` is set with the
difference that the API does not allow for specific DPUCluster allocation in its spec.

```yaml
# Parent subnet: 10.0.0.0/22 (1024 IPs). First half → cluster A, second half → cluster B.

# Cluster A — receives 10.0.0.0–10.0.1.255; excludes cluster B's range.
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: pool-cluster-a
spec:
  dpuClusterSelector:
    matchLabels:
      kubernetes.io/cluster: cluster-a
  ipv4Subnet:
    subnet: "10.0.0.0/22"
    gateway: "10.0.0.1"
    perNodeIPCount: 256
    excludeRanges:
    - startIP: "10.0.2.0"
      endIP: "10.0.3.255"
---
# Cluster B — receives 10.0.2.0–10.0.3.255; excludes cluster A's range.
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: pool-cluster-b
spec:
  dpuClusterSelector:
    matchLabels:
      kubernetes.io/cluster: cluster-b
  ipv4Subnet:
    subnet: "10.0.0.0/22"
    gateway: "10.0.0.1"
    perNodeIPCount: 256
    excludeRanges:
    - startIP: "10.0.0.0"
      endIP: "10.0.1.255"
```

This approach gives full control over the address space assigned to each cluster at the cost of managing one object per
cluster and keeping the `excludeRanges` in sync manually. The `blocksPerDPUCluster` / `subnetsPerDPUCluster`
fields are not required when only one cluster is targeted.

## Consuming IPAM in `DPUServiceChain`

For our workload to consume an IP from the IPAM we have installed in the cluster, we need to create a `DPUServiceChain`
and reference the IPAM object via labels. Below is an example of 2 fake `DPUServices` that are part of the same chain
and each of them require an IP address from a particular pool. We will not go into details on the `DPUServiceInterface`
or `DPUService` objects here, but we provide reference examples to ensure that the `DPUServiceChain` is understandable.

The following diagram may help illustrate the objects that are involved.

```mermaid
graph TD
  DPUServiceChain --> |Looks up| DPUServiceInterface-Producer
  DPUServiceChain --> |Looks up| DPUServiceInterface-Consumer
  DPUServiceChain --> |Looks up| DPUServiceIPAM

  DPUService-Producer --> DPUServiceInterface-Producer
  DPUService-Consumer --> DPUServiceInterface-Consumer

  DPUServiceChain --> |Gets IP from IPAM| Pod-Producer
  DPUServiceChain --> |Gets IP from IPAM| Pod-Consumer
```

First, we need to create the `DPUServiceIPAM`. We can create the one specified in
[Split subnet into chunk per node](#split-subnet-into-chunk-per-node).

Order of applying those resources does not matter, but in the interest of making the flow easier to understand, we apply
some order.

Then, we apply the `DPUService` and its `DPUServiceInterface` resources for each of the service:

```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: producer
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            svc.dpu.nvidia.com/interface: app-iface
            svc.dpu.nvidia.com/service: producer
        spec:
          interfaceType: service
          service:
            interfaceName: app-iface
            network: mybrsfc
            serviceID: producer
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: producer
spec:
  serviceID: producer
  helmChart:
    source:
      repoURL: https://example.com/charts
      path: producer
      version: v0.0.1
    values:
      container1:
        resources:
          requests:
            nvidia.com/sf: 1
  interfaces:
  - producer


---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: consumer
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            svc.dpu.nvidia.com/interface: app-iface
            svc.dpu.nvidia.com/service: consumer
        spec:
          interfaceType: service
          service:
            interfaceName: app-iface
            network: mybrsfc
            serviceID: consumer
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: consumer
spec:
  serviceID: consumer
  helmChart:
    source:
      repoURL: https://example.com/charts
      path: consumer
      version: v0.0.1
    values:
      worker:
        resources:
          requests:
            nvidia.com/sf: 1
  interfaces:
  - consumer

```

Then we create the `DPUServiceChain`. In this `DPUServiceChain` we request that the 2 interfaces are part of the same
switch and each of the interfaces request an IP from the pool we have created above:

```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceChain
metadata:
  name: producer-consumer
spec:
  template:
    spec:
      template:
        spec:
          switches:
            - ports:
              - serviceInterface:
                  matchLabels:
                    svc.dpu.nvidia.com/interface: app-iface
                    svc.dpu.nvidia.com/service: producer
                  ipam:
                    # Reference to the label we have specified in the DPUServiceIPAM
                    matchLabels:
                      svc.dpu.nvidia.com/pool: pool1
              - serviceInterface:
                  matchLabels:
                    svc.dpu.nvidia.com/interface: app-iface
                    svc.dpu.nvidia.com/service: consumer
                  ipam:
                    # Reference to the label we have specified in the DPUServiceIPAM
                    matchLabels:
                      svc.dpu.nvidia.com/pool: pool1
```

Assuming that the Helm Charts above are valid and the respect the DPF Contract, the outcome of the above resources will
be that each node on the DPU cluster will have 2 Pods, one consumer and one producer. These Pods will have 2 interfaces
each, one primary and a secondary using a Scalable Function (SF). The SFs will have an IP configured on them from the
pool defined above, from the chunk that is allocated on the node.

## Limitations

* `DPUServiceIPAM` CRs must be created in `dpf-operator-system` namespace to take effect.
* Adding static allocations in a `DPUServiceIPAM` - that targets multiple DPU clusters - for only a subset of the DPUs
  that are supposed to join these DPU clusters and utilise this `DPUServiceIPAM`, can lead to DPU clusters with fewer
  allocatable IPs. Applying static allocations to all DPUs or to none avoids this issue.
