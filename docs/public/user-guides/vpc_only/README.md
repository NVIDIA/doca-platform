---
title: "OVN VPC Service Deployment Guide"
---

This document describes how to deploy the OVN VPC Service.

> [!NOTE]  
>
> OVN VPC service is considered tech preview and is not recommended for
> production use.

[TOC]

## Prerequisites
This guide should be run by cloning the repo from [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform) and moving to the `docs/public/user-guides/vpc_only` directory.

Set up your cluster and deploy DPF as described in the [DPF Zero-Trust RDG](https://docs.nvidia.com/networking/display/public/sol/rdg+for+dpf+zero+trust+(dpf-zt)) guide.
Deploy DPF according to the instructions for the Baremetal/Zero-Trust use case, up to `BFB` creation in
the DPU provisioning step. In addition, make sure to label `DPUNodes` as illustrated in the DPF Zero-Trust RDG.

This guide covers provisioning the DPUs and deploying the VPC services.
The OVN VPC service deployment has the following additional requirements:

### Software prerequisites
Install the following tools on the machine where you will run the commands in this guide:

* kubectl
* envsubst

### Additional system prerequisites

* A CNI (Container Network Interface) must be installed on the control plane cluster.

### Network prerequisites

#### Worker Nodes

* Only a single DPU uplink is used with this deployment (p0).
* All worker nodes are connected to the same L2 broadcast domain (VLAN) on the high-speed network.

## Installation guide

### 0. Required variables

The following variables are required. Sensible defaults are provided where possible, but many values will be specific to your target infrastructure.

Commands in this guide are run in the same directory that contains this readme.

[embedmd]:#(manifests/00-env-vars/envvars.env sh)
```sh
## IP Address through which ovn-central service (exposed as NodePort)
## is accessible. This can be a VIP or one of the control-plane node IP
## in the host k8s cluster.
## This should never include a scheme or a port.
## e.g. 10.10.10.10
export TARGETCLUSTER_OVN_CENTRAL_IP=

## IP address range for VTEPs used by VPC OVN Service on the high speed fabric.
## This is a CIDR in the form e.g. 20.20.0.0/16
export VTEP_CIDR=20.20.0.0/16

## The Gateway address of the VTEP subnet
## This is an IP in the form e.g. 20.20.0.1
export VTEP_GATEWAY=20.20.0.1

## IP address range for external network used by VPC OVN Service on the high speed fabric.
## This is a CIDR in the form e.g. 30.30.0.0/16
export EXTERNAL_CIDR=30.30.0.0/16

## The Gateway address of the external subnet
## This is an IP in the form e.g. 30.30.0.1
export EXTERNAL_GATEWAY=30.30.0.1

## The repository URL for the NVIDIA Helm chart registry.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export HELM_REGISTRY_REPO_URL=https://helm.ngc.nvidia.com/nvidia/doca

## The DPF TAG is the version of the DPF components which will be deployed in this guide.
export TAG=v25.7.0

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BLUEFIELD_BITSTREAM="https://content.mellanox.com/BlueField/BFBs/Ubuntu22.04/bf-bundle-3.0.0-135_25.04_ubuntu-22.04_prod.bfb"
```

Modify the variables to fit your environment, then source the file:

```shell
source manifests/00-env-vars/envvars.env
```

### 1. Create BFB and DPUFlavor

Create a BFB and DPUFlavor to be used for the DPU provisioning process

```shell
cat manifests/01-bfb-and-flavor/* | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>OVN VPC DPUDeployment</summary>

[embedmd]:#(manifests/01-bfb-and-flavor/bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle
  namespace: dpf-operator-system
spec:
  url: $BLUEFIELD_BITSTREAM
```

[embedmd]:#(manifests/01-bfb-and-flavor/dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: vpc-flavor
  namespace: dpf-operator-system
spec:
  dpuMode: zero-trust
  bfcfgParameters:
  - UPDATE_ATF_UEFI=yes
  - UPDATE_DPU_OS=yes
  - WITH_NIC_FW_UPDATE=yes
  configFiles:
  - operation: override
    path: /etc/mellanox/mlnx-bf.conf
    permissions: "0644"
    raw: |
      ALLOW_SHARED_RQ="no"
      IPSEC_FULL_OFFLOAD="no"
      ENABLE_ESWITCH_MULTIPORT="yes"
  - operation: override
    path: /etc/mellanox/mlnx-ovs.conf
    permissions: "0644"
    raw: |
      CREATE_OVS_BRIDGES="no"
      OVS_DOCA="yes"
  - operation: override
    path: /etc/mellanox/mlnx-sf.conf
    permissions: "0644"
    raw: ""
  grub:
    kernelParameters:
    - console=hvc0
    - console=ttyAMA0
    - earlycon=pl011,0x13010000
    - fixrttc
    - net.ifnames=0
    - biosdevname=0
    - iommu.passthrough=1
    - cgroup_no_v1=net_prio,net_cls
    - hugepagesz=2048kB
    - hugepages=3072
  nvconfig:
  - device: '*'
    parameters:
    - PF_BAR2_ENABLE=0
    - PER_PF_NUM_SF=1
    - PF_TOTAL_SF=20
    - PF_SF_BAR_SIZE=10
    - NUM_PF_MSIX_VALID=0
    - PF_NUM_PF_MSIX_VALID=1
    - PF_NUM_PF_MSIX=228
    - INTERNAL_CPU_MODEL=1
    - INTERNAL_CPU_OFFLOAD_ENGINE=0
    - SRIOV_EN=1
    - NUM_OF_VFS=46
    - LAG_RESOURCE_ALLOCATION=1
  ovs:
    rawConfigScript: |
      _ovs-vsctl() {
        ovs-vsctl --no-wait --timeout 15 "$@"
      }

      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical
```

</details>

### 2. OVN VPC deployment

The OVN VPC service consists of the following components:

1. **ovn-central**: Deployed in the target cluster (runs northd, sb_db, nb_db)
2. **ovn-controller**: Deployed in the DPU cluster
3. **vpc-ovn-controller**: VPC controller in the target cluster
4. **vpc-ovn-node**: VPC node agent in the DPU cluster

#### Deploy OVN VPC DPUDeployment

```shell
cat manifests/02-vpc-ovn-dpudeployment/* | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>OVN VPC DPUDeployment</summary>

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: vpc-ovn
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavor: vpc-flavor
    nodeEffect:
      noEffect: true
    dpuSets:
    - nameSuffix: "dpuset1"
      nodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
  services:
    ovn-central:
      serviceTemplate: ovn-central
      serviceConfiguration: ovn-central
    ovn-controller:
      serviceTemplate: ovn-controller
      serviceConfiguration: ovn-controller
    vpc-ovn-controller:
      serviceTemplate: vpc-ovn-controller
      serviceConfiguration: vpc-ovn-controller
    vpc-ovn-node:
      serviceTemplate: vpc-ovn-node
      serviceConfiguration: vpc-ovn-node
  serviceChains:
    switches:
      - ports:
        - serviceInterface:
            matchLabels:
              ovn.vpc.dpu.nvidia.com/interface: p0
        - serviceInterface:
            matchLabels:
              ovn.vpc.dpu.nvidia.com/interface: ovn-vtep-patch
        - serviceInterface:
            matchLabels:
              ovn.vpc.dpu.nvidia.com/interface: ovn-ext-patch
```

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpuserviceconfig-ovn-central.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: ovn-central
  namespace: dpf-operator-system
spec:
  deploymentServiceName: ovn-central
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        exposedPorts:
          ports:
            ovnnb: true
            ovnsb: true
        management:
          ovnCentral:
            enabled: true
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

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpuserviceconfig-ovn-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: ovn-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: ovn-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          ovnController:
            enabled: true
```

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpuserviceconfig-vpc-ovn-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: vpc-ovn-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: vpc-ovn-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        host:
          vpcOVNController:
            enabled: true
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

```

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpuserviceconfig-vpc-ovn-node.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: vpc-ovn-node
  namespace: dpf-operator-system
spec:
  deploymentServiceName: vpc-ovn-node
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          vpcOVNNode:
            enabled: true
            initContainers:
              vpcOVNDpuProvisioner:
                env:
                  ovnSbEndpoint: "tcp:$TARGETCLUSTER_OVN_CENTRAL_IP:30642"
            ipRequests:
              - name: "vtep"
                poolName: "vpc-ippool-vtep"
              - name: "gateway"
                poolName: "vpc-ippool-gateway"
```

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpuservicetemplate-ovn-central.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: ovn-central
  namespace: dpf-operator-system
spec:
  deploymentServiceName: ovn-central
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: $TAG
      chart: ovn-chart
```

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpuservicetemplate-ovn-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: ovn-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: ovn-controller
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: $TAG
      chart: ovn-chart
```

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpuservicetemplate-vpc-ovn-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: vpc-ovn-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: vpc-ovn-controller
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: $TAG
      chart: dpf-vpc-ovn
```

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpuservicetemplate-vpc-ovn-node.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: vpc-ovn-node
  namespace: dpf-operator-system
spec:
  deploymentServiceName: vpc-ovn-node
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: $TAG
      chart: dpf-vpc-ovn
```

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpuserviceipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: vpc-ippool-vtep
  namespace: dpf-operator-system
spec:
  metadata:
    labels:
      ovn.vpc.dpu.nvidia.com/pool: vpc-ippool-vtep
  ipv4Subnet:
    subnet: $VTEP_CIDR
    gateway: $VTEP_GATEWAY
    perNodeIPCount: 4
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: vpc-ippool-gateway
  namespace: dpf-operator-system
spec:
  metadata:
    labels:
      ovn.vpc.dpu.nvidia.com/pool: vpc-ippool-gateway
  ipv4Subnet:
    subnet: $EXTERNAL_CIDR
    gateway: $EXTERNAL_GATEWAY
    perNodeIPCount: 4
```

[embedmd]:#(manifests/02-vpc-ovn-dpudeployment/dpuserviceinterface.yaml)
```yaml
---
apiVersion: "svc.dpu.nvidia.com/v1alpha1"
kind: DPUServiceInterface
metadata:
  name: p0
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            ovn.vpc.dpu.nvidia.com/interface: "p0"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p0
---
apiVersion: "svc.dpu.nvidia.com/v1alpha1"
kind: DPUServiceInterface
metadata:
  name: ovn-vtep-patch
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            ovn.vpc.dpu.nvidia.com/interface: "ovn-vtep-patch"
        spec:
          interfaceType: ovn
          ovn:
            externalBridge: br-ovn-vtep
---
apiVersion: "svc.dpu.nvidia.com/v1alpha1"
kind: DPUServiceInterface
metadata:
  name: ovn-ext-patch
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            ovn.vpc.dpu.nvidia.com/interface: "ovn-ext-patch"
        spec:
          interfaceType: ovn
          ovn:
            externalBridge: br-ovn-ext
```

</details>

#### Manually reboot DPU worker hosts after provisioning has finished

In DPF Zero Trust use-case it is required to manually reboot the DPU worker hosts when the DPU provisioning process has completed.

You may need to run these verification commands multiple times until conditions are met.

1. Wait for DPU `OSInstalled` condition to become `ready`

```shell
kubectl wait --for=condition=OSInstalled --namespace dpf-operator-system dpu --all
```

2. Ensure `Rebooted` condition type has `reason=WaitingForManualPowerCycleOrReboot`

```shell
kubectl wait --namespace dpf-operator-system dpu --all --for=jsonpath='{.status.conditions[?(@.type=="Rebooted")].reason}'=WaitingForManualPowerCycleOrReboot
```

3. Reboot DPU worker hosts - manual operation by the user

4. Once all nodes have rebooted, remove `provisioning.dpu.nvidia.com/dpunode-external-reboot-required` annotation from `DPUNodes`

```shell
kubectl -n dpf-operator-system annotate dpunode --all provisioning.dpu.nvidia.com/dpunode-external-reboot-required-
```

5. Ensure `DPUs` are ready

```shell
kubectl wait --for=condition=ready --namespace dpf-operator-system dpus --all
```

#### Validate deployed DPUServices

You may need to run these verification commands multiple times until the condition is met.

```shell
kubectl wait --for=condition=ready --namespace dpf-operator-system dpudeployment vpc-ovn
```

or with `dpfctl`:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
NAME                                   NAMESPACE            STATUS        REASON    SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig    dpf-operator-system  Ready: True   Success   11m
└─DPUDeployments
  └─DPUDeployment/vpc-ovn              dpf-operator-system  Ready: True   Success   24m
    ├─DPUServiceChains
    │ └─DPUServiceChain/vpc-ovn-tjktv  dpf-operator-system  Ready: True   Success   57m
    ├─DPUServices
    │ └─4 DPUServices...               dpf-operator-system  Ready: True   Success   55m    See ovn-central-fdjg9, ovn-controller-bj85w, vpc-ovn-controller-f8qgn, vpc-ovn-node-7bhd8
    └─DPUSets
      └─DPUSet/vpc-ovn-dpuset1         dpf-operator-system
        ├─BFB/bf-bundle                dpf-operator-system  Ready: True   Ready     58m    File: bf-bundle-3.0.0-135_25.04_ubuntu-22.04_prod.bfb, DOCA: 2.11.0
        ├─DPU/worker1-0000-c8-00       dpf-operator-system  Ready: True   DPUReady  2m13s
        └─DPU/worker2-0000-c8-00       dpf-operator-system  Ready: True   DPUReady  2m30s
```

### 3. Additional VPC resources deployment

In this step, you will deploy the `IsolationClass` resource, which will be used by subsequent user-created `DPUVPC` and `DPUVirtualNetwork` resources.

#### Deploy IsolationClass

```shell
cat manifests/03-vpc-resources/* | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>Additional VPC Resources</summary>

[embedmd]:#(manifests/03-vpc-resources/ovn-isolation-class.yaml)
```yaml
---
apiVersion: vpc.dpu.nvidia.com/v1alpha1
kind: IsolationClass
metadata:
  name: ovn.vpc.dpu.nvidia.com
spec:
  provisioner: ovn.vpc.dpu.nvidia.com
  parameters:
    ovn-nb-endpoint: "tcp:$TARGETCLUSTER_OVN_CENTRAL_IP:30641"
    ovn-nb-reconnect-time: "5"
```

</details>

### 4. Optional - Test traffic

At this point, your cluster should be set up and ready with all VPC components.
In this step, you will deploy a single `DPUVPC`, a single `DPUVirtualNetwork`, and a single `DPUServiceInterface` referencing this virtual network. The `DPUServiceInterface` will be of type `VF`, selecting VF index 2.
The `DPUVPC`, `DPUVirtualNetwork`, and `DPUServiceInterface` will span all worker nodes.

Ensure you have SSH access to your worker hosts from the management or out-of-band (OOB) network.

#### Deploy test topology

```shell
cat manifests/04-optional-test-traffic/* | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>VPC Test Topology</summary>

[embedmd]:#(manifests/04-optional-test-traffic/vpc-topology.yaml)
```yaml
apiVersion: vpc.dpu.nvidia.com/v1alpha1
kind: DPUVPC
metadata:
  name: myvpc
  namespace: default
spec:
  tenant: foo
  isolationClassName: ovn.vpc.dpu.nvidia.com
  interNetworkAccess: true
  nodeSelector: {}
---
apiVersion: vpc.dpu.nvidia.com/v1alpha1
kind: DPUVirtualNetwork
metadata:
  name: testnet1
  namespace: default
spec:
  vpcName: myvpc
  type: Bridged
  externallyRouted: true
  masquerade: true
  bridgedNetwork:
    ipam:
      ipv4:
        dhcp: true
        subnet: 192.178.0.0/16
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: vf2-test
  namespace: default
spec:
  template:
    spec:
      template:
        spec:
          interfaceType: vf
          vf:
            pfID: 0
            vfID: 2
            virtualNetwork: testnet1
            parentInterfaceRef: ""
```

</details>


#### Validate deployed resources

```shell
kubectl wait --for=condition=ready dpuvpc myvpc
kubectl wait --for=condition=ready dpuvirtualnetwork testnet1
kubectl wait --for=condition=ready dpuserviceinterface vf2-test
```

#### Test traffic between hosts

* SSH into each node and run `dhclient` for the network device associated with VF index 2 to obtain a DHCP address.

An example output for a node named `node1`:
```shell
root@node1:~# dhclient -1 -v enp8s0f0v2
Internet Systems Consortium DHCP Client 4.4.3-P1
Copyright 2004-2022 Internet Systems Consortium.
All rights reserved.
For info, please visit https://www.isc.org/software/dhcp/

Listening on LPF/enp8s0f0v2/26:3a:60:48:81:cf
Sending on   LPF/enp8s0f0v2/26:3a:60:48:81:cf
Sending on   Socket/fallback
DHCPREQUEST for 192.178.0.2 on enp8s0f0v2 to 255.255.255.255 port 67 (xid=0x7cbe87ca)
DHCPACK of 192.178.0.2 from 192.178.0.1 (xid=0xca87be7c)
subnet-mask: 4 extra bytes
bound to 192.178.0.2 -- renewal in 1367 seconds.
```

Repeat this process on another node.

* Test connectivity by running traffic between nodes.

In the example below, the other node's VF2 interface was assigned the IP 192.178.0.3:
```shell
root@node1:~# ping 192.178.0.3
```

### 5. Deletion and clean up

To remove the VPC OVN deployment from the cluster, delete the created resources in reverse order.

```shell
cat manifests/04-optional-test-traffic/* | kubectl delete --wait -f -
cat manifests/03-vpc-resources/* | kubectl delete --wait -f -
cat manifests/02-vpc-ovn-dpudeployment/* | kubectl delete --wait -f -
cat manifests/01-bfb-and-flavor/* | kubectl delete --wait -f -
```
