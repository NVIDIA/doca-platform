---
title: "DPUServiceNAD"
---

[TOC]

## Overview

The `DPUServiceNAD` Custom Resource Definition (CRD) allows you to create custom NetworkAttachmentDefinitions (NADs) for
your DPU services. These NADs define how secondary network interfaces are configured for pods running on DPUs. NetworkAttachmentDefinition
is a CRD defined by multus and used for setting up the secondary interface for the pod. Pod spec provides which NAD to
use as a configuration in annotation `k8s.v1.cni.cncf.io/networks: <nadName>`. Multus CNI consumes the annotation, fetches
the NAD configuration, and invokes the relevant CNI binary with the configuration given in the NAD to facilitate the
creation of a secondary interface on the pod. The following pages are recommended (as a 101 guide) if the user is not
familiar with NAD or Multus.

https://github.com/k8snetworkplumbingwg/multus-cni/blob/master/docs/how-to-use.md

https://github.com/k8snetworkplumbingwg/multus-cni/blob/master/docs/how-to-use.md#create-network-attachment-definition

https://github.com/k8snetworkplumbingwg/network-resources-injector?tab=readme-ov-file#network-resources-injection-example


## Installation

DPUServiceNAD is automatically installed as part of the DPF operator installation. No additional installation steps are required.

## Usage

### Creating a DPUServiceNAD

Here's an example of creating a basic DPUServiceNAD with SF (Scalable Function):

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceNAD
metadata:
  name: mynad
  namespace: my-namespace
  labels:
    labelTest: labelTestValue
  annotations:
    annotTest: annotTestValue
spec:
  resourceType: "sf"           # Resource type (sf, vf, or veth)
  bridge: "test-ovsbridge"     # OVS bridge name
  mtu: 1500                    # MTU size
  ipam: true                   # Enable IPAM
```

The DPUServiceNAD controller underneath takes care of converting this DPUServiceNAD CRD into a NAD CRD object and applying
it to all the DPU Clusters. Users can choose their preferred resource (sf, vf, or veth), custom OVS bridge, MTU, and IPAM.
It is mandatory for OVS bridge to exist beforehand, our controllers do not create the ovs bridge. This CRD allows users
to choose their own OVS bridge along with their preferred MTU/resource type for their workload's secondary interface.

### Using DPUServiceNAD with DPUServiceInterface

To use your DPUServiceNAD in a DPUServiceInterface:

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: dpu-service-interface-with-custom-nad
  namespace: my-namespace
spec:
  template:
    spec:
      template:
        spec:
          interfaceType: service
          service:
            serviceID: "service-one"
            network: "mynad"           # Reference to your DPUServiceNAD
            interfaceName: "net1"
```

### Dynamic Resource Injection

When you use a DPUServiceNAD with a DPUServiceInterface in a DPUService, the system automatically injects the appropriate
resources based on the resource type:

* **1.** For `sf` (service function) type:
    * If we have the `dpuservicenad.svc.dpu.nvidia.com/use-trusted-sfs` annotation in DPUServiceNAD, then we inject the trusted_sf `nvidia.com/bf_sf_trusted` resource.
    * If the above annotation is not present, we inject the default sf `nvidia.com/bf_sf` resource.
* **2.** For `vf` (virtual function) type:
    * Injects `nvidia.com/bf_vf` resource
* **3.** For `veth` type:
    * No resource injection

Example of an ArgoApplication(which is created from the DPUService which is using the NAD):

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  finalizers:
  - resources-finalizer.argocd.argoproj.io
  name: dpu-cluster-1-example-grb87
  namespace: dpf-operator-system
spec:
  destination:
    name: dpu-cluster-1
    namespace: dpf-operator-system
  project: doca-platform-project-dpu
  source:
    chart: hello-world
    helm:
      valuesObject:
        serviceDaemonSet:
          annotations:
            annKey: annVal
            k8s.v1.cni.cncf.io/networks: '[{"name":"mynad","interface":"dpu-service-interface-with-custom-nad","cni-args":null}]'
          labels:
            labelKey: labelVal
          resources:
            nvidia.com/bf_sf: "1"
```

`nvidia.com/bf_sf` resource is dynamically injected by the controller based on the DPUServiceNAD and DPUServiceInterface
configuration that dpuService is using. The system will automatically merge the injected resources with any existing
resources in the ArgoCD application (which is created from DPUService) object's serviceDaemonSet as shown in the above
example.

### Chained CNIs

DPUServiceNAD allows chaining CNIs to combine the DPF OVS CNI with additional CNI plugins to provide enhanced networking
capabilities. This is particularly useful when you need to chain RDMA (Remote Direct Memory Access) CNI with the DPF OVS
CNI.

Here's how to setup a DPUServiceNAD that provides SFs and RDMA devices on the Pods and connects the representors to the
`test-ovsbridge`:

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceNAD
metadata:
  name: rdma-chained-nad
  namespace: my-namespace
spec:
  resourceType: "sf"
  bridge: "test-ovsbridge"
  mtu: 1500
  chainedCNIs:
  - type: rdma
```
