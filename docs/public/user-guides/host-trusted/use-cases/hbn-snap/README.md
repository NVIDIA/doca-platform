---
title: "SNAP Storage with Host Based Networking"
---


> [!NOTE]
> Follow this guide from the source GitHub repo at [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform)
> and moving to the `docs/public/user-guides/host-trusted/use-cases/hbn-snap/README.md` for better formatting of the code.


In this configuration [DOCA SNAP](https://docs.nvidia.com/doca/sdk/doca+snap+services/index.html) is installed as a DPUService and combined with [NVIDIA Host Based Networking (HBN)](https://docs.nvidia.com/doca/sdk/doca+hbn+service+guide/index.html).

This guide includes examples for both SNAP Block (NVMe) and SNAP VirtioFS storage scenarios.

[TOC]

## Prerequisites
This guide should be run by cloning the repo from [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform) and moving to the `docs/public/user-guides/host-trusted/use-cases/hbn-snap` directory.

The system is set up as described in the [system prerequisites](../../prerequisites/system.md).
The HBN with SNAP storage use case has the additional requirements:

### SNAP Block (NVMe) Prerequisites

* A remote SPDK target should be set up to provide persistent storage for SNAP Block Storage
* The SPDK target should be reachable from the DPUs
* The management interface of the SPDK target should be reachable from the control plane nodes
* Make sure to check [Host OS Configuration Section in SNAP service documentation](https://docs.nvidia.com/doca/sdk/snap-4-service-appendixes/index.html#src-4464939880_id-.SNAP4ServiceAppendixesv3.2.0LC-HostOSConfiguration) to validate the host OS configuration on the worker nodes

### SNAP VirtioFS Prerequisites

* An external NFS server is required to provide persistent storage for SNAP VirtioFS
* The NFS server must be reachable by both the SNAP DPU service and the nvidia-fs DPU plugin
* The NFS service must also be accessible from the DPF control plane nodes to ensure proper operation
* Make sure to check [Host OS Configuration Section in SNAP VirtioFS service documentation](https://docs.nvidia.com/doca/sdk/doca-snap-virtio-fs-service-guide/index.html#src-4413882927_safe-id-aWQtLkRPQ0FTTkFQVmlydGlvZnNTZXJ2aWNlR3VpZGV2My4yLjBMQy1BcHBlbmRpeOKAk0hvc3RPU0NvbmZpZ3VyYXRpb25BcHBlbmRpeOKAk0hvc3RPU0NvbmZpZ3VyYXRpb24) to validate the host OS configuration on the worker nodes

### Software Prerequisites

This guide uses the following tools which must be installed on the machine where the commands contained in this guide run.

* kubectl
* helm
* envsubst

### Kubernetes Prerequisites

* control plane setup is complete before starting this guide
* CNI installed before starting this guide
* worker nodes are not added until indicated by this guide
* High-speed ports are used for secondary workload network and not for primary CNI

#### Virtual Functions

A number of virtual functions (VFs) will be created on hosts when provisioning DPUs. Certain of these VFs are marked for specific usage:

* The first VF (vf0) is used by provisioning components.
* The remaining VFs are allocated by SR-IOV Device Plugin.

## Installation Guide

### 0. Required Variables

The following variables are required by this guide. A sensible default is provided where it makes sense, but many will be specific to the target infrastructure.

Commands in this guide are run in the same directory that contains this readme.

<details markdown="1"><summary><b>Environment variables file</b></summary>

[embedmd]:# (manifests/00-env-vars/envvars.env sh)
```sh
## Virtual IP used by the load balancer for the DPU Cluster. Must be a reserved IP from the management subnet and not allocated by DHCP.
export DPUCLUSTER_VIP=

## Interface on which the DPUCluster load balancer will listen. Should be the management interface of the control plane node.
export DPUCLUSTER_INTERFACE=

## The repository URL for the NVIDIA Helm chart registry.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export HELM_REGISTRY_REPO_URL=https://helm.ngc.nvidia.com/nvidia/doca

## The repository URL for the HBN container image.
## Usually this is the NVIDIA NGC registry. For development purposes, this can be set to a different repository.
export HBN_NGC_IMAGE_URL=nvcr.io/nvidia/doca/doca_hbn

## The repository URL for the SNAP VFS container image.
## Usually this is the NVIDIA NGC registry. For development purposes, this can be set to a different repository.
export SNAP_NGC_IMAGE_URL=nvcr.io/nvidia/doca/doca_vfs

## The DPF REGISTRY is the Helm repository URL where the DPF Operator Chart resides.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export REGISTRY=https://helm.ngc.nvidia.com/nvidia/doca

## The DPF TAG is the version of the DPF components which will be deployed in this guide.
export TAG=v25.10.1

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BFB_URL="https://content.mellanox.com/BlueField/BFBs/Ubuntu24.04/bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb"
```
</details>



Modify the variables in `manifests/00-env-vars/envvars.env` to fit your environment, then source the file:

```shell
source manifests/00-env-vars/envvars.env
```

### 1. DPF Operator Installation

#### Dependencies

Before deploying the DPF Operator, ensure that Helm is properly configured according to the [Helm prerequisites](../../../../getting-started/helm-prerequisites.md).

> [!WARNING]
> This is a critical prerequisite step that must be completed for the DPF Operator to function properly.

#### Deploy the DPF Operator

A number of [environment variables](#0-required-variables) must be set before running this command.

##### HTTP Registry (default)

If the $REGISTRY is an HTTP Registry (default value) use this command:
```shell
helm repo add --force-update dpf-repository ${REGISTRY}
helm repo update
helm upgrade --install -n dpf-operator-system dpf-operator dpf-repository/dpf-operator --version=$TAG
```

---

##### OCI Registry

For development purposes, if the $REGISTRY is an OCI Registry use this command:
```shell
helm upgrade --install -n dpf-operator-system dpf-operator $REGISTRY/dpf-operator --version=$TAG
```

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the DPF Operator installation with:
```shell
## Ensure the DPF Operator deployment is available.
kubectl rollout status deployment --namespace dpf-operator-system dpf-operator-controller-manager
## Ensure all pods in the DPF Operator system are ready.
kubectl wait --for=condition=ready --namespace dpf-operator-system pods --all
```


### 2. DPF System Installation
This section involves creating the DPF system components and some basic infrastructure required for a functioning DPF-enabled cluster.

#### Deploy the DPF System components
A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
kubectl create ns dpu-cplane-tenant1
cat manifests/02-dpf-system-installation/*.yaml | envsubst | kubectl apply -f - 
```

This will create the following objects:
<details markdown="1"><summary><b>DPFOperatorConfig to install the DPF System components</b></summary>

[embedmd]:#(manifests/02-dpf-system-installation/operatorconfig.yaml)
```yaml
---
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  deploymentMode: host-trusted
  provisioningController:
    dmsTimeout: 900
  kamajiClusterManager:
    disable: false
  nodeSRIOVDevicePluginController:
    disable: false
```
</details>

<details markdown="1"><summary><b>DPUCluster to serve as Kubernetes control plane for DPU nodes</b></summary>

[embedmd]:#(manifests/02-dpf-system-installation/dpucluster.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUCluster
metadata:
  name: dpu-cplane-tenant1
  namespace: dpu-cplane-tenant1
spec:
  type: kamaji
  maxNodes: 1000
  clusterEndpoint:
    # deploy keepalived instances on the nodes that match the given nodeSelector.
    keepalived:
      # interface on which keepalived will listen. Should be the oob interface of the control plane node.
      interface: $DPUCLUSTER_INTERFACE
      # Virtual IP reserved for the DPU Cluster load balancer. Must not be allocatable by DHCP.
      vip: $DPUCLUSTER_VIP
      # virtualRouterID must be in range [1,255], make sure the given virtualRouterID does not duplicate with any existing keepalived process running on the host
      virtualRouterID: 126
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
```
</details>


#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the DPF System with:
```shell
## Ensure the provisioning and DPUService controller manager deployments are available.
kubectl rollout status deployment --namespace dpf-operator-system dpf-provisioning-controller-manager dpuservice-controller-manager
## Ensure all other deployments in the DPF Operator system are Available.
kubectl rollout status deployment --namespace dpf-operator-system 
## Ensure the DPUCluster is ready for nodes to join.
kubectl wait --for=condition=ready --namespace dpu-cplane-tenant1 dpucluster --all
```

### 3. Install Prerequisites for Accelerated Network

#### Install Multus using NVIDIA Network Operator

```shell
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia --force-update
helm upgrade --no-hooks --install --create-namespace --namespace nvidia-network-operator network-operator nvidia/network-operator --version 26.1.0 -f ./manifests/03-enable-accelerated-interfaces/helm-values/network-operator.yml
```

<details markdown="1"><summary><b>NVIDIA Network Operator Helm values</b></summary>

[embedmd]:#(manifests/03-enable-accelerated-interfaces/helm-values/network-operator.yml)
```yml
nfd:
  enabled: false
  deployNodeFeatureRules: false
operator:
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
```
</details>

#### Apply the NicClusterPolicy

```shell
kubectl apply -f manifests/03-enable-accelerated-interfaces/nic_cluster_policy.yaml
```

This will deploy the following object:

<details markdown="1"><summary><b>NICClusterPolicy for the NVIDIA Network Operator</b></summary>

[embedmd]:#(manifests/03-enable-accelerated-interfaces/nic_cluster_policy.yaml)
```yaml
---
apiVersion: mellanox.com/v1alpha1
kind: NicClusterPolicy
metadata:
  name: nic-cluster-policy
spec:
  secondaryNetwork:
    multus:
      image: multus-cni
      imagePullSecrets: []
      repository: nvcr.io/nvidia/mellanox
      version: network-operator-v26.1.0
```
</details>

#### Apply the NodeSRIOVDevicePluginConfig

The NodeSRIOVDevicePluginConfig defines which VFs on the DPU physical functions are exposed as SR-IOV device plugin resources on the host node. The DPF Operator's NodeSRIOVDevicePluginController (enabled in the DPFOperatorConfig) manages the SR-IOV device plugin pods based on this configuration.

```shell
kubectl apply -f manifests/03-enable-accelerated-interfaces/nodesriovdevicepluginconfig.yaml
```

<details markdown="1"><summary><b>NodeSRIOVDevicePluginConfig for VFs on PF0 and PF1</b></summary>

[embedmd]:#(manifests/03-enable-accelerated-interfaces/nodesriovdevicepluginconfig.yaml)
```yaml
---
apiVersion: noderesources.dpu.nvidia.com/v1alpha1
kind: NodeSRIOVDevicePluginConfig
metadata:
  name: bf3-vfs
  namespace: dpf-operator-system
spec:
  devicePluginResources:
    - name: bf3-p0-vfs
      type: vf
      options:
        isRdma: true
      ranges:
        - pfIndex: 0
          start: 2
          end: 45
    - name: bf3-p1-vfs
      type: vf
      options:
        isRdma: true
      ranges:
        - pfIndex: 1
          start: 2
          end: 45
```
</details>

The `NodeSRIOVDevicePluginConfig` is linked to DPUs via the `noderesources.dpu.nvidia.com/nodesriovdevicepluginconfig` annotation on the DPU object. This annotation is set in the DPUDeployment's `dpuAnnotations` field.

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the accelerated network prerequisites with:
```shell
## Ensure all pods in the nvidia-network-operator namespace are ready.
kubectl wait --for=condition=Ready --namespace nvidia-network-operator pods --all
## Expect the Multus Daemonset to be successfully rolled out.
kubectl rollout status daemonset --namespace nvidia-network-operator kube-multus-ds
```

### 4. DPU Provisioning and Service Installation

In this section, you will provision your DPUs and deploy the required services. You will need to create a `DPUDeployment` object that defines which `DPUServices` should be installed on each selected DPU. This provides a flexible way to specify and manage the services that run on your DPUs.

> If you want to learn more about `DPUDeployments`, check the [DPUDeployment documentation](../../../../developer-guides/api/dpudeployment.md).

This guide includes examples for both SNAP Block (NVMe) and SNAP VirtioFS Storage.
Please refer to the relevant sections below and follow the instructions to deploy the desired storage type.

> [!NOTE]
> Storage use-cases set `RDMA_SET_NETNS_EXCLUSIVE="no"` in the DPUFlavor, putting the DPU in shared RDMA
> mode. The default SFC NAD (`mybrsfc`) enables RDMA for SF interfaces, which is not compatible with
> shared RDMA mode. All services deployed on a DPU provisioned with a storage flavor that use SF
> interfaces must reference a NAD without RDMA. A custom DPUServiceNAD (`mybrsfc-storage`) is included
> in the manifests below for this reason.

#### SNAP Block (NVMe)

A number of [environment variables](#0-required-variables) must be set before running these commands.

##### Create Vendor CSI Controller Credentials

Create the credential request for the SPDK CSI Controller before installing the chart:

```shell
kubectl apply -f manifests/04.1-dpudeployment-installation-nvme/credentials/
```

<details markdown="1"><summary><b>DPUServiceCredentialRequest for SPDK CSI Controller</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/credentials/spdk-csi-controller-dpuservicecredentialrequest.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: spdk-csi-controller-credentials
  namespace: dpf-operator-system
spec:
  duration: 10m
  serviceAccount:
    name: spdk-csi-controller-sa
    namespace: dpf-operator-system
  targetCluster:
    name: dpu-cplane-tenant1
    namespace: dpu-cplane-tenant1
  type: tokenFile
  secret:
    name: spdk-csi-controller-dpu-cluster-credentials
    namespace: dpf-operator-system
```
</details>

##### Install SNAP Host Controller on the Host Cluster

Install the SNAP Host Controller that runs on the host cluster for this scenario:

###### HTTP Registry (default)

If the $REGISTRY is an HTTP Registry (default value) use this command:

```shell
helm repo add --force-update dpf-repository ${REGISTRY}
helm repo update
helm upgrade --install -n dpf-operator-system snap-host-controller \
  dpf-repository/dpf-storage --version=$TAG \
  --wait \
  -f manifests/04.1-dpudeployment-installation-nvme/helm-values/snap-host-controller.yml
```

###### OCI Registry

For development purposes, if the $REGISTRY is an OCI Registry use this command:

```shell
helm upgrade --install -n dpf-operator-system snap-host-controller \
  $REGISTRY/dpf-storage --version=$TAG \
  --wait \
  -f manifests/04.1-dpudeployment-installation-nvme/helm-values/snap-host-controller.yml
```

<details markdown="1"><summary><b>SNAP Host Controller Helm values</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/helm-values/snap-host-controller.yml)
```yml
host:
  snapHostController:
    enabled: true
    config:
      targetNamespace: dpf-operator-system
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
</details>

##### Install SNAP CSI Plugin Controller on the Host Cluster

Install the SNAP CSI Plugin Controller that runs on the host cluster for this scenario. The node part is deployed later with the DPUDeployment:

###### HTTP Registry (default)

If the $REGISTRY is an HTTP Registry (default value) use this command:

```shell
helm repo add --force-update dpf-repository ${REGISTRY}
helm repo update
helm upgrade --install -n dpf-operator-system snap-csi-plugin \
  dpf-repository/dpf-storage --version=$TAG \
  --wait \
  -f manifests/04.1-dpudeployment-installation-nvme/helm-values/snap-csi-plugin-controller.yml
```

###### OCI Registry

For development purposes, if the $REGISTRY is an OCI Registry use this command:

```shell
helm upgrade --install -n dpf-operator-system snap-csi-plugin \
  $REGISTRY/dpf-storage --version=$TAG \
  --wait \
  -f manifests/04.1-dpudeployment-installation-nvme/helm-values/snap-csi-plugin-controller.yml
```

<details markdown="1"><summary><b>SNAP CSI Plugin Controller Helm values</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/helm-values/snap-csi-plugin-controller.yml)
```yml
host:
  snapCsiPlugin:
    enabled: true
    emulationMode: "nvme"
    controller:
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
</details>

##### Install SPDK CSI Controller on the Host Cluster

Install the SPDK CSI Controller that runs on the host cluster for this scenario:

```shell
helm upgrade --install -n dpf-operator-system spdk-csi-controller \
  oci://ghcr.io/mellanox/dpf-storage-vendors-charts/spdk-csi-controller --version=v0.3.0 \
  --wait \
  -f manifests/04.1-dpudeployment-installation-nvme/helm-values/spdk-csi-controller.yml
```

<details markdown="1"><summary><b>SPDK CSI Controller Helm values</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/helm-values/spdk-csi-controller.yml)
```yml
host:
  enabled: true
  config:
    targets:
      nodes:
        # name of the target
        - name: spdk-target
          # management address
          rpcURL: http://10.0.110.25:8000
          # type of the target, e.g. nvme-tcp, nvme-rdma
          targetType: nvme-rdma
          # target service IP
          targetAddr: 10.0.124.1
    # required parameter, name of the secret that contains connection
    # details to access the DPU cluster.
    # this secret should be created by the DPUServiceCredentialRequest API.
    dpuClusterSecret: spdk-csi-controller-dpu-cluster-credentials
```
</details>

##### Apply DPU-side Storage Resources

```shell
cat manifests/04.1-dpudeployment-installation-nvme/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary><b>BFB to download Bluefield Bitstream to a shared volume</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle-$TAG
  namespace: dpf-operator-system
spec:
  url: $BFB_URL
```
</details>

<details markdown="1"><summary><b>HBN + SNAP NVMe DPUFlavor to configure DPUs on provisioning</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: hbn-snap-nvme-$TAG
  namespace: dpf-operator-system
spec:
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
      RDMA_SET_NETNS_EXCLUSIVE="no"
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
    - hugepages=5120
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
    - PCI_SWITCH_EMULATION_ENABLE=1
    - PCI_SWITCH_EMULATION_NUM_PORT=32
    - NVME_EMULATION_ENABLE=1
    - NVME_EMULATION_NUM_PF=0
    - LINK_TYPE_P1=ETH
    - LINK_TYPE_P2=ETH
  ovs:
    rawConfigScript: |
      _ovs-vsctl() {
        ovs-vsctl --no-wait --timeout 15 "$@"
      }

      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      if systemctl list-unit-files openvswitch-switch.service &>/dev/null; then
        systemctl restart openvswitch-switch
      elif systemctl list-unit-files openvswitch.service &>/dev/null; then
        systemctl restart openvswitch
      fi
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Interface p0 mtu_request=9216
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical
      _ovs-vsctl --may-exist add-port br-sfc p1
      _ovs-vsctl set Interface p1 type=dpdk
      _ovs-vsctl set Interface p1 mtu_request=9216
      _ovs-vsctl set Port p1 external_ids:dpf-type=physical
      _ovs-vsctl --may-exist add-br br-hbn
      _ovs-vsctl set bridge br-hbn datapath_type=netdev
      _ovs-vsctl set bridge br-hbn fail_mode=secure
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for DOCA HBN</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/hbn-dpuserviceconfig.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-hbn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "doca-hbn"
  serviceConfiguration:
    serviceDaemonSet:
      annotations:
        k8s.v1.cni.cncf.io/networks: |-
          [
          {"name": "iprequest", "interface": "ip_lo", "cni-args": {"poolNames": ["loopback"], "poolType": "cidrpool"}},
          {"name": "iprequest", "interface": "ip_pf0vf10", "cni-args": {"poolNames": ["pool1"], "poolType": "cidrpool", "allocateDefaultGateway": true}},
          {"name": "iprequest", "interface": "ip_pf1vf10", "cni-args": {"poolNames": ["pool2"], "poolType": "cidrpool", "allocateDefaultGateway": true}}
          ]
    helmChart:
      values:
        configuration:
          perDPUValuesYAML: |
            - hostnamePattern: "*"
              values:
                bgp_peer_group: hbn
                vrf1: RED
                vrf2: BLUE
                l3vni1: 100001
                l3vni2: 100002
            - hostnamePattern: "worker1*"
              values:
                bgp_autonomous_system: 65101
            - hostnamePattern: "worker2*"
              values:
                bgp_autonomous_system: 65201
          startupYAMLJ2: |
            - header:
                model: BLUEFIELD
                nvue-api-version: nvue_v1
                rev-id: 1.0
                version: HBN 3.0.0
            - set:
                evpn:
                  enable: on
                  route-advertise: {}
                bridge:
                  domain:
                    br_default:
                      vlan:
                        '10':
                          vni:
                            '10': {}
                interface:
                  lo:
                    ip:
                      address:
                        {{ ipaddresses.ip_lo.ip }}/32: {}
                    type: loopback
                  p0_if,p1_if,pf0vf10_if,pf1vf10_if,snap_if:
                    type: swp
                    link:
                      mtu: 9000
                  pf0vf10_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf0vf10.cidr }}: {}
                      vrf: {{ config.vrf1 }}
                  pf1vf10_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf1vf10.cidr }}: {}
                      vrf: {{ config.vrf2 }}
                  snap_if:
                    bridge:
                      domain:
                        br_default:
                          access: 10
                  vlan10:
                    type: svi
                    vlan: 10
                nve:
                  vxlan:
                    arp-nd-suppress: on
                    enable: on
                    source:
                      address: {{ ipaddresses.ip_lo.ip }}
                router:
                  bgp:
                    enable: on
                    graceful-restart:
                      mode: full
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
                            multipaths:
                              ebgp: 16
                          l2vpn-evpn:
                            enable: on
                        autonomous-system: {{ config.bgp_autonomous_system }}
                        enable: on
                        neighbor:
                          p0_if:
                            peer-group: {{ config.bgp_peer_group }}
                            type: unnumbered
                            address-family:
                              l2vpn-evpn:
                                enable: on
                                add-path-tx: off
                          p1_if:
                            peer-group: {{ config.bgp_peer_group }}
                            type: unnumbered
                            address-family:
                              l2vpn-evpn:
                                enable: on
                                add-path-tx: off
                        path-selection:
                          multipath:
                            aspath-ignore: on
                        peer-group:
                          {{ config.bgp_peer_group }}:
                            address-family:
                              ipv4-unicast:
                                enable: on
                              l2vpn-evpn:
                                enable: on
                            remote-as: external
                        router-id: {{ ipaddresses.ip_lo.ip }}
                  {{ config.vrf1 }}:
                    evpn:
                      enable: on
                      vni:
                        {{ config.l3vni1 }}: {}
                    router:
                      bgp:
                        address-family:
                          ipv4-unicast:
                            enable: on
                            redistribute:
                              connected:
                                enable: on
                            route-export:
                              to-evpn:
                                enable: on
                        autonomous-system: {{ config.bgp_autonomous_system }}
                        enable: on
                  {{ config.vrf2 }}:
                    evpn:
                      enable: on
                      vni:
                        {{ config.l3vni2 }}: {}
                    router:
                      bgp:
                        address-family:
                          ipv4-unicast:
                            enable: on
                            redistribute:
                              connected:
                                enable: on
                            route-export:
                              to-evpn:
                                enable: on
                        autonomous-system: {{ config.bgp_autonomous_system }}
                        enable: on

  interfaces:
  - name: p0_if
    network: mybrhbn
  - name: p1_if
    network: mybrhbn
  - name: pf0vf10_if
    network: mybrhbn
  - name: pf1vf10_if
    network: mybrhbn
  - name: snap_if
    network: mybrhbn
```

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/hbn-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-hbn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "doca-hbn"
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: 1.0.5
      chart: doca-hbn
    values:
      image:
        repository: $HBN_NGC_IMAGE_URL
        tag: 3.2.1-doca3.2.1
      resources:
        memory: 6Gi
        nvidia.com/bf_sf: 5
```
</details>

<details markdown="1"><summary><b>DPUServiceInterfaces for physical ports and VFs on the DPU</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/physical-ifaces.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
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
            uplink: "p0"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p1
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            uplink: "p1"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p1
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf0vf10-rep
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            vf: "pf0vf10"
        spec:
          interfaceType: vf
          vf:
            parentInterfaceRef: p0
            pfID: 0
            vfID: 10
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf1vf10-rep
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            vf: "pf1vf10"
        spec:
          interfaceType: vf
          vf:
            parentInterfaceRef: p1
            pfID: 1
            vfID: 10
```
</details>

<details markdown="1"><summary><b>DPUServiceIPAMs</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/hbn-ipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: pool1
  namespace: dpf-operator-system
spec:
  ipv4Network:
    network: "10.0.121.0/24"
    gatewayIndex: 2
    prefixSize: 29
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: pool2
  namespace: dpf-operator-system
spec:
  ipv4Network:
    network: "10.0.122.0/24"
    gatewayIndex: 2
    prefixSize: 29
```

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/hbn-loopback-ipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: loopback
  namespace: dpf-operator-system
spec:
  ipv4Network:
    network: "11.0.0.0/24"
    prefixSize: 32
```

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/storage-ipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: storage-pool
  namespace: dpf-operator-system
spec:
  metadata:
    labels:
      svc.dpu.nvidia.com/pool: storage-pool
  ipv4Subnet:
    subnet: "10.0.124.0/24"
    gateway: "10.0.124.1"
    perNodeIPCount: 8
```
</details>

<details markdown="1"><summary><b>DPUServiceNAD for storage services (no RDMA CNI chaining)</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/storage-dpuservicenad.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceNAD
metadata:
  name: mybrsfc-storage
  namespace: dpf-operator-system
spec:
  resourceType: sf
  ipam: true
  bridge: "br-sfc"
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for DOCA SNAP (NVMe)</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/doca-snap-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          docaSnap:
            enabled: true
            image:
              repository: $SNAP_NGC_IMAGE_URL
              tag: 1.5.0-doca3.2.0
            snapRpcInitConf: |
              nvme_subsystem_create --nqn nqn.2022-10.io.nvda.nvme:0
  interfaces:
  - name: app_sf
    network: mybrsfc-storage
```

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/doca-snap-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-storage
    values:
      serviceDaemonSet:
        resources:
          memory: "2Gi"
          hugepages-2Mi: "4Gi"
          cpu: "8"
          nvidia.com/bf_sf: 1
  resourceRequirements:
    memory: "2Gi"
    hugepages-2Mi: "4Gi"
    cpu: "8"
    nvidia.com/bf_sf: 1
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for SNAP Node Driver</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/snap-node-driver-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          deployCrds: true
          snapNodeDriver:
            enabled: true
```

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/snap-node-driver-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-storage
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for Block Storage DPU Plugin</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/block-storage-dpu-plugin-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: block-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: block-storage-dpu-plugin
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          blockStorageVendorDpuPlugin:
            enabled: true
```

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/block-storage-dpu-plugin-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: block-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: block-storage-dpu-plugin
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-storage
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for SNAP CSI Plugin (NVMe)</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/snap-csi-plugin-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-csi-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-csi-plugin
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        host:
          snapCsiPlugin:
            enabled: true
            emulationMode: "nvme"
            node:
              enabled: true
```

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/snap-csi-plugin-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-csi-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-csi-plugin
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-storage
```
</details>

<details markdown="1"><summary><b>Secret for SPDK CSI Controller</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/spdk-csi-secret.yaml)
```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: spdkcsi-secret
  namespace: dpf-operator-system
  labels:
    # this label enables replication of the secret from the host to the dpu cluster
    dpu.nvidia.com/image-pull-secret: ""
stringData:
  # name field in the "rpcTokens" list should match name of the
  # spdk target from DPUService.helmChart.values.host.config.targets.nodes
  secret.json: |-
    {
      "rpcTokens": [
        {
          "name": "spdk-target",
          "username": "exampleuser",
          "password": "examplepassword"
        }
      ]
    }
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for SPDK CSI Controller on DPU</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/spdk-csi-controller-dpu-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: spdk-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller-dpu
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          enabled: true
          storageClass:
            # the name of the storage class that will be created for spdk-csi,
            # this StorageClass name should be used in the StorageVendor settings
            name: spdkcsi-sc
            # name of the secret that contains credentials for the remote SPDK target,
            # content of the secret is injected during CreateVolume request
            secretName: spdkcsi-secret
            # namespace of the secret with credentials for the remote SPDK target
            secretNamespace: dpf-operator-system
          rbacRoles:
            spdkCsiController:
              # the name of the service account for spdk-csi-controller
              # this value must be aligned with the value from the DPUServiceCredentialRequest
              serviceAccount: spdk-csi-controller-sa
```

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/spdk-csi-controller-dpu-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: spdk-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller-dpu
  helmChart:
    source:
      repoURL: oci://ghcr.io/mellanox/dpf-storage-vendors-charts
      version: v0.3.0
      chart: spdk-csi-controller
```
</details>

<details markdown="1"><summary><b>DPUDeployment to provision DPUs on worker nodes</b></summary>

[embedmd]:#(manifests/04.1-dpudeployment-installation-nvme/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: hbn-snap
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle-$TAG
    flavor: hbn-snap-nvme-$TAG
    nodeEffect:
      drain: true
    dpuSets:
    - nameSuffix: "dpuset1"
      dpuAnnotations:
        storage.nvidia.com/preferred-dpu: "true"
        noderesources.dpu.nvidia.com/nodesriovdevicepluginconfig: bf3-vfs
      dpuNodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
    dpuSetStrategy:
      type: RollingUpdate
  services:
    doca-hbn:
      serviceTemplate: doca-hbn
      serviceConfiguration: doca-hbn
    snap-node-driver:
      serviceTemplate: snap-node-driver
      serviceConfiguration: snap-node-driver
    doca-snap:
      serviceTemplate: doca-snap
      serviceConfiguration: doca-snap
    block-storage-dpu-plugin:
      serviceTemplate: block-storage-dpu-plugin
      serviceConfiguration: block-storage-dpu-plugin
    snap-csi-plugin:
      serviceTemplate: snap-csi-plugin
      serviceConfiguration: snap-csi-plugin
    spdk-csi-controller-dpu:
      serviceTemplate: spdk-csi-controller-dpu
      serviceConfiguration: spdk-csi-controller-dpu
  serviceChains:
    switches:
      - ports:
        - serviceInterface:
            matchLabels:
              uplink: p0
        - service:
            name: doca-hbn
            interface: p0_if
      - ports:
        - serviceInterface:
            matchLabels:
              uplink: p1
        - service:
            name: doca-hbn
            interface: p1_if
      - ports:
        - serviceInterface:
            matchLabels:
              vf: pf0vf10
        - service:
            name: doca-hbn
            interface: pf0vf10_if
      - ports:
        - serviceInterface:
            matchLabels:
              vf: pf1vf10
        - service:
            name: doca-hbn
            interface: pf1vf10_if
      - ports:
        - service:
            name: doca-snap
            interface: app_sf
            ipam:
              matchLabels:
                svc.dpu.nvidia.com/pool: storage-pool
        - service:
            name: doca-hbn
            interface: snap_if
```
</details>

##### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

```shell
## Ensure the BFB is ready
kubectl wait --for=jsonpath='{.status.phase}'=Ready --namespace dpf-operator-system bfb bf-bundle-$TAG --timeout=600s
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system dpuservices -l svc.dpu.nvidia.com/owned-by-dpudeployment=dpf-operator-system_hbn-snap
## Ensure the DPUServiceIPAMs have been reconciled
kubectl wait --for=condition=DPUIPAMObjectReconciled --namespace dpf-operator-system dpuserviceipam --all
## Ensure the DPUServiceInterfaces have been reconciled
kubectl wait --for=condition=ServiceInterfaceSetReconciled --namespace dpf-operator-system dpuserviceinterface --all
## Ensure the DPUServiceChains have been reconciled
kubectl wait --for=condition=ServiceChainSetReconciled --namespace dpf-operator-system dpuservicechain --all
```

#### SNAP VirtioFS

A number of [environment variables](#0-required-variables) must be set before running these commands.

##### Create Vendor CSI Controller Credentials

Create the credential request for the NFS CSI Controller before installing the chart:

```shell
kubectl apply -f manifests/04.2-dpudeployment-installation-virtiofs/credentials/
```

<details markdown="1"><summary><b>DPUServiceCredentialRequest for NFS CSI Controller</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/credentials/nfs-csi-controller-dpuservicecredentialrequest.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: nfs-csi-controller-credentials
  namespace: dpf-operator-system
spec:
  duration: 24h
  serviceAccount:
    name: nfs-csi-controller-sa
    namespace: dpf-operator-system
  targetCluster:
    name: dpu-cplane-tenant1
    namespace: dpu-cplane-tenant1
  type: tokenFile
  secret:
    name: nfs-csi-controller-dpu-cluster-credentials
    namespace: dpf-operator-system
```
</details>

##### Install SNAP Host Controller on the Host Cluster

Install the SNAP Host Controller that runs on the host cluster for this scenario:

###### HTTP Registry (default)

If the $REGISTRY is an HTTP Registry (default value) use this command:

```shell
helm repo add --force-update dpf-repository ${REGISTRY}
helm repo update
helm upgrade --install -n dpf-operator-system snap-host-controller \
  dpf-repository/dpf-storage --version=$TAG \
  --wait \
  -f manifests/04.2-dpudeployment-installation-virtiofs/helm-values/snap-host-controller.yml
```

###### OCI Registry

For development purposes, if the $REGISTRY is an OCI Registry use this command:

```shell
helm upgrade --install -n dpf-operator-system snap-host-controller \
  $REGISTRY/dpf-storage --version=$TAG \
  --wait \
  -f manifests/04.2-dpudeployment-installation-virtiofs/helm-values/snap-host-controller.yml
```

<details markdown="1"><summary><b>SNAP Host Controller Helm values</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/helm-values/snap-host-controller.yml)
```yml
host:
  snapHostController:
    enabled: true
    config:
      targetNamespace: dpf-operator-system
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
</details>

##### Install SNAP CSI Plugin Controller on the Host Cluster

Install the SNAP CSI Plugin Controller that runs on the host cluster for this scenario. The node part is deployed later with the DPUDeployment:

###### HTTP Registry (default)

If the $REGISTRY is an HTTP Registry (default value) use this command:

```shell
helm repo add --force-update dpf-repository ${REGISTRY}
helm repo update
helm upgrade --install -n dpf-operator-system snap-csi-plugin \
  dpf-repository/dpf-storage --version=$TAG \
  --wait \
  -f manifests/04.2-dpudeployment-installation-virtiofs/helm-values/snap-csi-plugin-controller.yml
```

###### OCI Registry

For development purposes, if the $REGISTRY is an OCI Registry use this command:

```shell
helm upgrade --install -n dpf-operator-system snap-csi-plugin \
  $REGISTRY/dpf-storage --version=$TAG \
  --wait \
  -f manifests/04.2-dpudeployment-installation-virtiofs/helm-values/snap-csi-plugin-controller.yml
```

<details markdown="1"><summary><b>SNAP CSI Plugin Controller Helm values</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/helm-values/snap-csi-plugin-controller.yml)
```yml
host:
  snapCsiPlugin:
    enabled: true
    emulationMode: "virtiofs"
    controller:
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
</details>

##### Install NFS CSI Controller on the Host Cluster

Install the NFS CSI Controller that runs on the host cluster for this scenario:

```shell
helm upgrade --install -n dpf-operator-system nfs-csi-controller \
  oci://ghcr.io/mellanox/dpf-storage-vendors-charts/nfs-csi-controller --version=v0.2.0 \
  --wait \
  -f manifests/04.2-dpudeployment-installation-virtiofs/helm-values/nfs-csi-controller.yml
```

<details markdown="1"><summary><b>NFS CSI Controller Helm values</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/helm-values/nfs-csi-controller.yml)
```yml
host:
  enabled: true
  config:
    # required parameter, name of the secret that contains connection
    # details to access the DPU cluster.
    # this secret should be created by the DPUServiceCredentialRequest API.
    dpuClusterSecret: nfs-csi-controller-dpu-cluster-credentials
```
</details>

##### Apply DPU-side Storage Resources

```shell
cat manifests/04.2-dpudeployment-installation-virtiofs/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary><b>BFB to download BlueField Bitstream to a shared volume</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle-$TAG
  namespace: dpf-operator-system
spec:
  url: $BFB_URL
```
</details>

<details markdown="1"><summary><b>SNAP VirtioFS with HBN DPUFlavor to configure DPUs on provisioning</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: hbn-snap-virtiofs-$TAG
  namespace: dpf-operator-system
spec:
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
      RDMA_SET_NETNS_EXCLUSIVE="no"
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
    - hugepages=5120
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
    - PCI_SWITCH_EMULATION_ENABLE=1
    - PCI_SWITCH_EMULATION_NUM_PORT=32
    - VIRTIO_FS_EMULATION_ENABLE=1
    - VIRTIO_FS_EMULATION_NUM_PF=0
    - LINK_TYPE_P1=ETH
    - LINK_TYPE_P2=ETH
  ovs:
    rawConfigScript: |
      _ovs-vsctl() {
        ovs-vsctl --no-wait --timeout 15 "$@"
      }

      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      if systemctl list-unit-files openvswitch-switch.service &>/dev/null; then
        systemctl restart openvswitch-switch
      elif systemctl list-unit-files openvswitch.service &>/dev/null; then
        systemctl restart openvswitch
      fi
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Interface p0 mtu_request=9216
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical
      _ovs-vsctl --may-exist add-port br-sfc p1
      _ovs-vsctl set Interface p1 type=dpdk
      _ovs-vsctl set Interface p1 mtu_request=9216
      _ovs-vsctl set Port p1 external_ids:dpf-type=physical
      _ovs-vsctl --may-exist add-br br-hbn
      _ovs-vsctl set bridge br-hbn datapath_type=netdev
      _ovs-vsctl set bridge br-hbn fail_mode=secure
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for DOCA HBN</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/hbn-dpuserviceconfig.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-hbn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "doca-hbn"
  serviceConfiguration:
    serviceDaemonSet:
      annotations:
        k8s.v1.cni.cncf.io/networks: |-
          [
          {"name": "iprequest", "interface": "ip_lo", "cni-args": {"poolNames": ["loopback"], "poolType": "cidrpool"}},
          {"name": "iprequest", "interface": "ip_pf0vf10", "cni-args": {"poolNames": ["pool1"], "poolType": "cidrpool", "allocateDefaultGateway": true}},
          {"name": "iprequest", "interface": "ip_pf1vf10", "cni-args": {"poolNames": ["pool2"], "poolType": "cidrpool", "allocateDefaultGateway": true}}
          ]
    helmChart:
      values:
        configuration:
          perDPUValuesYAML: |
            - hostnamePattern: "*"
              values:
                bgp_peer_group: hbn
                vrf1: RED
                vrf2: BLUE
                l3vni1: 100001
                l3vni2: 100002
            - hostnamePattern: "worker1*"
              values:
                bgp_autonomous_system: 65101
            - hostnamePattern: "worker2*"
              values:
                bgp_autonomous_system: 65201
          startupYAMLJ2: |
            - header:
                model: BLUEFIELD
                nvue-api-version: nvue_v1
                rev-id: 1.0
                version: HBN 3.0.0
            - set:
                evpn:
                  enable: on
                  route-advertise: {}
                bridge:
                  domain:
                    br_default:
                      vlan:
                        '10':
                          vni:
                            '10': {}
                interface:
                  lo:
                    ip:
                      address:
                        {{ ipaddresses.ip_lo.ip }}/32: {}
                    type: loopback
                  p0_if,p1_if,pf0vf10_if,pf1vf10_if,snap_if:
                    type: swp
                    link:
                      mtu: 9000
                  pf0vf10_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf0vf10.cidr }}: {}
                      vrf: {{ config.vrf1 }}
                  pf1vf10_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf1vf10.cidr }}: {}
                      vrf: {{ config.vrf2 }}
                  snap_if:
                    bridge:
                      domain:
                        br_default:
                          access: 10
                  vlan10:
                    type: svi
                    vlan: 10
                nve:
                  vxlan:
                    arp-nd-suppress: on
                    enable: on
                    source:
                      address: {{ ipaddresses.ip_lo.ip }}
                router:
                  bgp:
                    enable: on
                    graceful-restart:
                      mode: full
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
                            multipaths:
                              ebgp: 16
                          l2vpn-evpn:
                            enable: on
                        autonomous-system: {{ config.bgp_autonomous_system }}
                        enable: on
                        neighbor:
                          p0_if:
                            peer-group: {{ config.bgp_peer_group }}
                            type: unnumbered
                            address-family:
                              l2vpn-evpn:
                                enable: on
                                add-path-tx: off
                          p1_if:
                            peer-group: {{ config.bgp_peer_group }}
                            type: unnumbered
                            address-family:
                              l2vpn-evpn:
                                enable: on
                                add-path-tx: off
                        path-selection:
                          multipath:
                            aspath-ignore: on
                        peer-group:
                          {{ config.bgp_peer_group }}:
                            address-family:
                              ipv4-unicast:
                                enable: on
                              l2vpn-evpn:
                                enable: on
                            remote-as: external
                        router-id: {{ ipaddresses.ip_lo.ip }}
                  {{ config.vrf1 }}:
                    evpn:
                      enable: on
                      vni:
                        {{ config.l3vni1 }}: {}
                    router:
                      bgp:
                        address-family:
                          ipv4-unicast:
                            enable: on
                            redistribute:
                              connected:
                                enable: on
                            route-export:
                              to-evpn:
                                enable: on
                        autonomous-system: {{ config.bgp_autonomous_system }}
                        enable: on
                  {{ config.vrf2 }}:
                    evpn:
                      enable: on
                      vni:
                        {{ config.l3vni2 }}: {}
                    router:
                      bgp:
                        address-family:
                          ipv4-unicast:
                            enable: on
                            redistribute:
                              connected:
                                enable: on
                            route-export:
                              to-evpn:
                                enable: on
                        autonomous-system: {{ config.bgp_autonomous_system }}
                        enable: on

  interfaces:
  - name: p0_if
    network: mybrhbn
  - name: p1_if
    network: mybrhbn
  - name: pf0vf10_if
    network: mybrhbn
  - name: pf1vf10_if
    network: mybrhbn
  - name: snap_if
    network: mybrhbn
```

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/hbn-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-hbn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "doca-hbn"
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: 1.0.5
      chart: doca-hbn
    values:
      image:
        repository: $HBN_NGC_IMAGE_URL
        tag: 3.2.1-doca3.2.1
      resources:
        memory: 6Gi
        nvidia.com/bf_sf: 5
```
</details>

<details markdown="1"><summary><b>DPUServiceNAD for storage services (no RDMA CNI chaining)</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/storage-dpuservicenad.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceNAD
metadata:
  name: mybrsfc-storage
  namespace: dpf-operator-system
spec:
  resourceType: sf
  ipam: true
  bridge: "br-sfc"
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for DOCA SNAP</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/doca-snap-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          docaSnap:
            enabled: true
            env:
              XLIO_ENABLED: "0"
            image:
              repository: $SNAP_NGC_IMAGE_URL
              tag: 1.5.0-doca3.2.0
  interfaces:
  - name: app_sf
    network: mybrsfc-storage
```

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/doca-snap-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-storage
    values:
      serviceDaemonSet:
        resources:
          memory: "2Gi"
          hugepages-2Mi: "4Gi"
          cpu: "8"
          nvidia.com/bf_sf: 1
  resourceRequirements:
    memory: "2Gi"
    hugepages-2Mi: "4Gi"
    cpu: "8"
    nvidia.com/bf_sf: 1
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for SNAP Node Driver</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/snap-node-driver-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          deployCrds: true
          snapNodeDriver:
            enabled: true
```

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/snap-node-driver-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-storage
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for FS Storage DPU Plugin</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/fs-storage-dpu-plugin-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: fs-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: fs-storage-dpu-plugin
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          fsStorageVendorDpuPlugin:
            enabled: true
  interfaces:
    - name: app_sf
      network: mybrsfc-storage
```

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/fs-storage-dpu-plugin-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: fs-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: fs-storage-dpu-plugin
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-storage
    values:
      serviceDaemonSet:
        resources:
          nvidia.com/bf_sf: 1
  resourceRequirements:
    nvidia.com/bf_sf: 1
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for SNAP CSI Plugin</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/snap-csi-plugin-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-csi-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-csi-plugin
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        host:
          snapCsiPlugin:
            enabled: true
            emulationMode: "virtiofs"
            node:
              enabled: true
```

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/snap-csi-plugin-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-csi-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-csi-plugin
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-storage
```
</details>

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for NFS CSI Controller (DPU)</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/nfs-csi-controller-dpu-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: nfs-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: nfs-csi-controller-dpu
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          enabled: true
          storageClasses:
            # List of storage classes to be created for nfs-csi
            # These StorageClass names should be used in the StorageVendor settings
            - name: nfs-csi
              parameters:
                server: 10.0.124.1
                share: /srv/nfs/share
          rbacRoles:
            nfsCsiController:
              # the name of the service account for nfs-csi-controller
              # this value must be aligned with the value from the DPUServiceCredentialRequest
              serviceAccount: nfs-csi-controller-sa
```

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/nfs-csi-controller-dpu-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: nfs-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: nfs-csi-controller-dpu
  helmChart:
    source:
      repoURL: oci://ghcr.io/mellanox/dpf-storage-vendors-charts
      version: v0.2.0
      chart: nfs-csi-controller
```
</details>

<details markdown="1"><summary><b>DPUServiceIPAMs</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/hbn-ipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: pool1
  namespace: dpf-operator-system
spec:
  ipv4Network:
    network: "10.0.121.0/24"
    gatewayIndex: 2
    prefixSize: 29
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: pool2
  namespace: dpf-operator-system
spec:
  ipv4Network:
    network: "10.0.122.0/24"
    gatewayIndex: 2
    prefixSize: 29
```

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/hbn-loopback-ipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: loopback
  namespace: dpf-operator-system
spec:
  ipv4Network:
    network: "11.0.0.0/24"
    prefixSize: 32
```

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/storage-ipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: storage-pool
  namespace: dpf-operator-system
spec:
  metadata:
    labels:
      svc.dpu.nvidia.com/pool: storage-pool
  ipv4Subnet:
    subnet: "10.0.124.0/24"
    gateway: "10.0.124.1"
    perNodeIPCount: 8
```
</details>

<details markdown="1"><summary><b>DPUServiceInterfaces for physical ports and VFs on the DPU</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/physical-ifaces.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
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
            uplink: "p0"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p1
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            uplink: "p1"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p1
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf0vf10-rep
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            vf: "pf0vf10"
        spec:
          interfaceType: vf
          vf:
            parentInterfaceRef: p0
            pfID: 0
            vfID: 10
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf1vf10-rep
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            vf: "pf1vf10"
        spec:
          interfaceType: vf
          vf:
            parentInterfaceRef: p1
            pfID: 1
            vfID: 10
```
</details>

<details markdown="1"><summary><b>DPUDeployment to provision DPUs on worker nodes</b></summary>

[embedmd]:#(manifests/04.2-dpudeployment-installation-virtiofs/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: hbn-snap
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle-$TAG
    flavor: hbn-snap-virtiofs-$TAG
    nodeEffect:
      drain: true
    dpuSets:
    - nameSuffix: "dpuset1"
      dpuAnnotations:
        storage.nvidia.com/preferred-dpu: "true"
        noderesources.dpu.nvidia.com/nodesriovdevicepluginconfig: bf3-vfs
      dpuNodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
    dpuSetStrategy:
      type: RollingUpdate
  services:
    doca-hbn:
      serviceTemplate: doca-hbn
      serviceConfiguration: doca-hbn
    snap-node-driver:
      serviceTemplate: snap-node-driver
      serviceConfiguration: snap-node-driver
    doca-snap:
      serviceTemplate: doca-snap
      serviceConfiguration: doca-snap
    fs-storage-dpu-plugin:
      serviceTemplate: fs-storage-dpu-plugin
      serviceConfiguration: fs-storage-dpu-plugin
    snap-csi-plugin:
      serviceTemplate: snap-csi-plugin
      serviceConfiguration: snap-csi-plugin
    nfs-csi-controller-dpu:
      serviceTemplate: nfs-csi-controller-dpu
      serviceConfiguration: nfs-csi-controller-dpu
  serviceChains:
    switches:
      - ports:
        - serviceInterface:
            matchLabels:
              uplink: p0
        - service:
            name: doca-hbn
            interface: p0_if
      - ports:
        - serviceInterface:
            matchLabels:
              uplink: p1
        - service:
            name: doca-hbn
            interface: p1_if
      - ports:
        - serviceInterface:
            matchLabels:
              vf: pf0vf10
        - service:
            name: doca-hbn
            interface: pf0vf10_if
      - ports:
        - serviceInterface:
            matchLabels:
              vf: pf1vf10
        - service:
            name: doca-hbn
            interface: pf1vf10_if
      - ports:
        - service:
            name: doca-snap
            interface: app_sf
            ipam:
              matchLabels:
                svc.dpu.nvidia.com/pool: storage-pool
        - service:
            name: fs-storage-dpu-plugin
            interface: app_sf
            ipam:
              matchLabels:
                svc.dpu.nvidia.com/pool: storage-pool
        - service:
            name: doca-hbn
            interface: snap_if
```
</details>

##### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Note that the DPUService name will have a random suffix. For example, `doca-hbn-l2xsl`.

Verify the DPU and Service installation with:

```shell
## Ensure the BFB is ready
kubectl wait --for=jsonpath='{.status.phase}'=Ready --namespace dpf-operator-system bfb bf-bundle-$TAG --timeout=600s
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system dpuservices -l svc.dpu.nvidia.com/owned-by-dpudeployment=dpf-operator-system_hbn-snap
## Ensure the DPUServiceIPAMs have been reconciled
kubectl wait --for=condition=DPUIPAMObjectReconciled --namespace dpf-operator-system dpuserviceipam --all
## Ensure the DPUServiceInterfaces have been reconciled
kubectl wait --for=condition=ServiceInterfaceSetReconciled --namespace dpf-operator-system dpuserviceinterface --all
## Ensure the DPUServiceChains have been reconciled
kubectl wait --for=condition=ServiceChainSetReconciled --namespace dpf-operator-system dpuservicechain --all
```


### 5. Add Worker Nodes and Apply Network Configuration

At this point workers should be added to the cluster. Each worker node should be configured in line with [the prerequisites](../../prerequisites/system.md).
As workers are added to the cluster DPUs will be provisioned and DPUServices will begin to be spun up.

You can verify the status of the DPUDeployment and its components with the following command:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
```

#### Apply network configuration

This section creates `NetworkAttachmentDefinition` objects that reference VF 10 from both PFs by interface name.
The PCI addresses and names of the network VFs on the host side are likely to change after DPU provisioning, because the DPUFlavor includes the `PCI_SWITCH_EMULATION_ENABLE` firmware setting.

Before applying the network configuration, you need to identify the new names of the network VFs on the host side and set the following environment variables:

```shell
# contains the name of the network VF 10 on P0 on the host side, e.g. enp8s0f0v10
export DPU_P0_VF10_NAME=<REPLACE_WITH_INTERFACE_NAME>
# contains the name of the network VF 10 on P1 on the host side, e.g. enp8s0f1v10
export DPU_P1_VF10_NAME=<REPLACE_WITH_INTERFACE_NAME>
```

```shell
cat manifests/05-network-configuration/*.yaml | envsubst | kubectl apply -f -
```

This will create the following objects:

<details markdown="1"><summary><b>NetworkAttachmentDefinition for host-device VFs</b></summary>

[embedmd]:#(manifests/05-network-configuration/nad-hostdev.yaml)
```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: hostdev-pf0vf10-worker1
spec:
  config: '{
    "cniVersion": "0.3.1",
    "name": "hostpf0vf10",
    "type": "host-device",
    "device": "$DPU_P0_VF10_NAME",
    "ipam": {
        "type": "static",
        "addresses": [
          {
            "address": "10.0.121.1/29"
          }
        ],
        "routes": [
          {
            "dst": "10.0.121.8/29",
            "gw": "10.0.121.2"
          }
        ]
    }
  }'
---
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: hostdev-pf1vf10-worker1
spec:
  config: '{
    "cniVersion": "0.3.1",
    "name": "hostpf1vf10",
    "type": "host-device",
    "device": "$DPU_P1_VF10_NAME",
    "ipam": {
        "type": "static",
        "addresses": [
          {
            "address": "10.0.122.1/29"
          }
        ],
        "routes": [
          {
            "dst": "10.0.122.8/29",
            "gw": "10.0.122.2"
          }
        ]
    }
  }'
---
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: hostdev-pf0vf10-worker2
spec:
  config: '{
    "cniVersion": "0.3.1",
    "name": "hostpf0vf10",
    "type": "host-device",
    "device": "$DPU_P0_VF10_NAME",
    "ipam": {
        "type": "static",
        "addresses": [
          {
            "address": "10.0.121.9/29"
          }
        ],
        "routes": [
          {
            "dst": "10.0.121.0/29",
            "gw": "10.0.121.10"
          }
        ]
    }
  }'
---
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: hostdev-pf1vf10-worker2
spec:
  config: '{
    "cniVersion": "0.3.1",
    "name": "hostpf1vf10",
    "type": "host-device",
    "device": "$DPU_P1_VF10_NAME",
    "ipam": {
        "type": "static",
        "addresses": [
          {
            "address": "10.0.122.9/29"
          }
        ],
        "routes": [
          {
            "dst": "10.0.122.0/29",
            "gw": "10.0.122.10"
          }
        ]
    }
  }'
```
</details>

#### Verify network

```shell
kubectl apply -f manifests/06-network-test
```

HBN functionality can be tested by pinging between the pods and services deployed in the default namespace.

### 6. Storage Configuration

#### SNAP Block (NVMe)

##### Apply storage configuration

```shell
cat manifests/07.1-storage-configuration-nvme/*.yaml | envsubst | kubectl apply -f -
```

This will create the following objects:

<details markdown="1"><summary><b>DPUStorageVendor for SPDK CSI</b></summary>

[embedmd]:#(manifests/07.1-storage-configuration-nvme/spdk-csi-dpustoragevendor.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStorageVendor
metadata:
  name: spdk-csi
  namespace: dpf-operator-system
spec:
  storageClassName: spdkcsi-sc
  pluginName: nvidia-block
```
</details>

<details markdown="1"><summary><b>DPUStoragePolicy for block storage</b></summary>

[embedmd]:#(manifests/07.1-storage-configuration-nvme/policy-block-dpustoragepolicy.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStoragePolicy
metadata:
  name: policy-block
  namespace: dpf-operator-system
spec:
  dpuStorageVendors:
    - spdk-csi
  selectionAlgorithm: "NumberVolumes"
  parameters: {}
```
</details>

Validate the DPUStorageVendor and DPUStoragePolicy objects are ready:

```shell
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpustoragevendors --all
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpustoragepolicies --all
```

##### Run Workloads to test NVMe block storage

Deploy storage test pods that request a block volume from SNAP NVMe.

```shell
kubectl apply -f manifests/08.1-storage-test-nvme
```

Storage functionality can be tested by verifying the block device is attached inside the pod and performing I/O.


#### SNAP VirtioFS

```shell
cat manifests/07.2-storage-configuration-virtiofs/*.yaml | envsubst | kubectl apply -f -
```

This will create the following objects:

<details markdown="1"><summary><b>DPUStorageVendor for NFS CSI</b></summary>

[embedmd]:#(manifests/07.2-storage-configuration-virtiofs/nfs-csi-dpustoragevendor.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStorageVendor
metadata:
  name: nfs-csi
  namespace: dpf-operator-system
spec:
  storageClassName: nfs-csi
  pluginName: nvidia-fs
```
</details>

<details markdown="1"><summary><b>DPUStoragePolicy for filesystem policy</b></summary>

[embedmd]:#(manifests/07.2-storage-configuration-virtiofs/policy-fs-dpustoragepolicy.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStoragePolicy
metadata:
  name: policy-fs
  namespace: dpf-operator-system
spec:
  dpuStorageVendors:
    - nfs-csi
  selectionAlgorithm: "NumberVolumes"
  parameters: {}
```
</details>

Validate the DPUStorageVendor and DPUStoragePolicy objects are ready:

```shell
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpustoragevendors --all
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpustoragepolicies --all
```

##### Run Workloads to test VirtioFS storage

Deploy storage test pods that mount a storage volume provided by SNAP VirtioFS.

```shell
kubectl apply -f manifests/08.2-storage-test-virtiofs
```

Storage functionality can be tested by writing and reading data to the mounted volume.

## Uninstall
This section describes how to clean up the DPF components installed in this guide.
It is recommended to run this section only after the DPF Operator and DPUCluster are no longer needed.

### Delete test workloads

```shell
kubectl delete -f manifests/06-network-test --wait --ignore-not-found=true
kubectl delete -f manifests/08.2-storage-test-virtiofs --wait --ignore-not-found=true
kubectl delete -f manifests/08.1-storage-test-nvme --wait --ignore-not-found=true
# delete all PVCs created by StatefulSets
kubectl delete pvc --selector=app=storage-test-pod-nvme-hotplug-pf --wait -n default --ignore-not-found=true
kubectl delete pvc --selector=app=storage-test-pod-virtiofs-hotplug-pf --wait -n default --ignore-not-found=true
kubectl delete -n dpf-operator-system dpuvolumeattachment --all --wait
kubectl delete -n dpf-operator-system dpuvolume --all --wait
# delete storage configuration
kubectl delete -f manifests/07.1-storage-configuration-nvme --wait --ignore-not-found=true
kubectl delete -f manifests/07.2-storage-configuration-virtiofs --wait --ignore-not-found=true
```

### Delete Storage Controllers from the Host Cluster

```shell
helm uninstall -n dpf-operator-system snap-host-controller --wait
helm uninstall -n dpf-operator-system snap-csi-plugin --wait
# SNAP Block (NVMe) only:
helm uninstall -n dpf-operator-system spdk-csi-controller --wait
# SNAP VirtioFS only:
helm uninstall -n dpf-operator-system nfs-csi-controller --wait
```

### Delete DPF CNI acceleration components
```shell
kubectl delete -f manifests/05-network-configuration --wait --ignore-not-found=true
kubectl delete -f manifests/03-enable-accelerated-interfaces --wait --ignore-not-found=true
helm uninstall -n nvidia-network-operator network-operator --wait
```

### Delete the DPF Operator system and DPF Operator
```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

Note: there can be a race condition with deleting the underlying Kamaji cluster
which runs the DPU cluster control plane in this guide. If that happens it may be
necessary to remove finalizers manually from `DPUCluster` and `Datastore` objects.
