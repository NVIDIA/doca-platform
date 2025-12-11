---
title: "OVN Kubernetes with Host Based Networking and SNAP Block Storage"
---

> [!NOTE]
> Follow this guide from the source GitHub repo at [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform)
> and moving to the `docs/public/user-guides/host-trusted/use-cases/hbn-ovnk-snap/README.md` for better formatting of the code.

In this configuration OVN Kubernetes is offloaded to the DPU and combined with [NVIDIA Host Based Networking (HBN)](https://docs.nvidia.com/doca/sdk/doca+hbn+service+guide/index.html) and [DOCA SNAP](https://docs.nvidia.com/doca/sdk/doca+snap+services/index.html) for Block (NVMe) storage.

[TOC]

## Prerequisites

This guide should be run by cloning the repo from [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform) and moving to the `docs/public/user-guides/host-trusted/use-cases/hbn-ovnk-snap` directory.

The system is set up as described in the [system prerequisites](../../prerequisites/system.md).  The OVN Kubernetes with HBN and SNAP Block Storage use case has the additional requirements:

### DPU Prerequisites

* Bluefield 3 with 32GB of RAM

### Infrastructure Prerequisites

* A remote SPDK target should be set up to provide persistent storage for SNAP Block Storage
* The SPDK target should be reachable from the DPUs
* The management interface of the SPDK target should be reachable from the control plane nodes
* Make sure to check [Host OS Configuration Section in SNAP service documentation](https://docs.nvidia.com/doca/sdk/snap-4+service+appendixes/index.html#src-4198714790_id-.SNAP4ServiceAppendixesv3.1.0-HostOSConfiguration) to validate the host OS configuration on the worker nodes

### Software Prerequisites

This guide uses the following tools which must be installed on the machine where the commands contained in this guide run.

* kubectl
* helm
* envsubst

### Network Prerequisites

#### Control Plane Nodes

* Open vSwitch (OVS) packages installed - i.e. `openvswitch-switch` for Ubuntu 24.04
* out-of-band management port should be configured as OVS bridge port with "bridge-uplink" OVS metadata [This addresses a known issue](../../../../release-notes/v25.1.0.md#known-issues-and-limitations).
* DNS stub resolver should be disabled if using systemd resolvd

#### Worker Nodes

* Open vSwitch (OVS) packages not installed
* Host high-speed port (Host PF0) must have DHCP enabled
* MTU of the port should be statically set to 1500
* **Note**: These settings can be configured automatically via DPUFlavor `hostNetworkInterfaceConfigs`

### Kubernetes Prerequisites

* CNI not installed
* kube-proxy not installed
* coreDNS should be configured to run only on control plane nodes - e.g. using NodeAffinity. [This addresses a known issue](../../../../release-notes/v25.1.0.md#known-issues-and-limitations).
* control plane setup is complete before starting this guide
* worker nodes are not added until indicated by this guide

#### Control Plane Nodes

* Have the labels:
    * `"k8s.ovn.org/zone-name": $KUBERNETES_NODE_NAME`

#### Worker Nodes

* Have the labels:
    * `"k8s.ovn.org/dpu-host": ""`
    * `"k8s.ovn.org/zone-name": $KUBERNETES_NODE_NAME`
* Have the annotations:
    * `"k8s.ovn.org/remote-zone-migrated": $KUBERNETES_NODE_NAME`

#### Virtual Functions

A number of virtual functions (VFs) will be created on hosts when provisioning DPUs. Certain of these VFs are marked for specific usage:

* The first VF (vf0) is used by provisioning components.
* The second VF (vf1) is used by ovn-kubernetes.
* The remaining VFs are allocated by SR-IOV Device Plugin. Each pod using OVN Kubernetes in DPU mode as its primary CNI will have one of these VFs injected at Pod creation time. 

## Installation Guide

### 0. Required Variables

The following variables are required by this guide. A sensible default is provided where it makes sense, but many will be specific to the target infrastructure.

Commands in this guide are run in the same directory that contains this readme.

<details markdown="1"><summary>Environment variables file</summary>

[embedmd]:# (manifests/00-env-vars/envvars.env sh)
```sh
## IP Address for the Kubernetes API server of the target cluster on which DPF is installed.
## This should never include a scheme or a port.
## e.g. 10.10.10.10
export TARGETCLUSTER_API_SERVER_HOST=

## Port for the Kubernetes API server of the target cluster on which DPF is installed.
export TARGETCLUSTER_API_SERVER_PORT=6443

## IP address range for hosts in the target cluster on which DPF is installed.
## This is a CIDR in the form e.g. 10.10.10.0/24
export TARGETCLUSTER_NODE_CIDR=

## Virtual IP used by the load balancer for the DPU Cluster. Must be a reserved IP from the management subnet and not allocated by DHCP.
export DPUCLUSTER_VIP=

## DPU_P0 is the name of the first port of the DPU. This name must be the same on all worker nodes.
export DPU_P0=

## DPU_P0_VF1 is the name of the second Virtual Function (VF) of the first port of the DPU. This name must be the same on all worker nodes.
## Note: The VF will be created after the DPU is provisioned and the phase "Host Network Configuration" is completed.
export DPU_P0_VF1=

## Interface on which the DPUCluster load balancer will listen. Should be the management interface of the control plane node.
export DPUCLUSTER_INTERFACE=

## IP address to the NFS server used as storage for the BFB.
export NFS_SERVER_IP=

## The repository URL for the NVIDIA Helm chart registry.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export HELM_REGISTRY_REPO_URL=https://helm.ngc.nvidia.com/nvidia/doca

## The repository URL for the HBN container image.
## Usually this is the NVIDIA NGC registry. For development purposes, this can be set to a different repository.
export HBN_NGC_IMAGE_URL=nvcr.io/nvidia/doca/doca_hbn

## The repository URL for the SNAP VFS container image.
## Usually this is the NVIDIA NGC registry. For development purposes, this can be set to a different repository.
export SNAP_NGC_IMAGE_URL=nvcr.io/nvidia/doca/doca_vfs

## The repository URL for the OVN-Kubernetes Helm chart.
## Usually this is the NVIDIA GHCR repository. For development purposes, this can be set to a different repository.
export OVN_KUBERNETES_REPO_URL=oci://ghcr.io/nvidia

## POD_CIDR is the CIDR used for pods in the target Kubernetes cluster.
export POD_CIDR=10.233.64.0/18

## SERVICE_CIDR is the CIDR used for services in the target Kubernetes cluster.
## This is a CIDR in the form e.g. 10.10.10.0/24
export SERVICE_CIDR=10.233.0.0/18

## The DPF REGISTRY is the Helm repository URL where the DPF Operator Chart resides.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export REGISTRY=https://helm.ngc.nvidia.com/nvidia/doca

## The DPF TAG is the version of the DPF components which will be deployed in this guide.
export TAG=v25.7.0

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BFB_URL="https://content.mellanox.com/BlueField/BFBs/Ubuntu22.04/bf-bundle-3.1.0-76_25.07_ubuntu-22.04_prod.bfb"
```
</details>

Modify the variables in `manifests/00-env-vars/envvars.env` to fit your environment, then source the file:

```shell
source manifests/00-env-vars/envvars.env
```

### 1. CNI Installation

OVN Kubernetes is used as the primary CNI for the cluster. On worker nodes the primary CNI will be accelerated by offloading work to the DPU. On control plane nodes OVN Kubernetes will run without offloading.

#### Create the Namespace

```shell
kubectl create ns ovn-kubernetes
```

#### Install OVN Kubernetes from the helm chart

Install the OVN Kubernetes CNI components from the helm chart. A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
envsubst < manifests/01-cni-installation/helm-values/ovn-kubernetes.yml | helm upgrade --install -n ovn-kubernetes ovn-kubernetes ${OVN_KUBERNETES_REPO_URL}/ovn-kubernetes-chart --version $TAG --values -
```

<details markdown="1"><summary>OVN-Kubernetes Helm values</summary>

[embedmd]:#(manifests/01-cni-installation/helm-values/ovn-kubernetes.yml)
```yml
commonManifests:
  enabled: true
nodeWithoutDPUManifests:
  enabled: true
controlPlaneManifests:
  enabled: true
nodeWithDPUManifests:
  enabled: true
  nodeMgmtPortNetdev: $DPU_P0_VF1
  dpuServiceAccountNamespace: dpf-operator-system
gatewayOpts: --gateway-interface=$DPU_P0
## Note this CIDR is followed by a trailing /24 which informs OVN Kubernetes on how to split the CIDR per node.
podNetwork: $POD_CIDR/24
serviceNetwork: $SERVICE_CIDR
k8sAPIServer: https://$TARGETCLUSTER_API_SERVER_HOST:$TARGETCLUSTER_API_SERVER_PORT
```
</details>

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the CNI installation with:
```shell
## Ensure all nodes in the cluster are ready.
kubectl wait --for=condition=ready nodes --all
## Ensure all pods in the ovn-kubernetes namespace are ready.
kubectl wait --for=condition=ready --namespace ovn-kubernetes pods --all --timeout=300s
```

### 2. DPF Operator Installation


#### Create storage required by the DPF Operator
A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
kubectl create ns dpf-operator-system
cat manifests/02-dpf-operator-installation/*.yaml | envsubst | kubectl apply -f - 
```

This deploys the following objects:

<details markdown="1"><summary>PersistentVolume and PersistentVolumeClaim for the provisioning controller</summary>

[embedmd]:#(manifests/02-dpf-operator-installation/nfs-storage-for-bfb-dpf-ga.yaml)
```yaml
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: bfb-pv
spec:
  capacity:
    storage: 10Gi
  volumeMode: Filesystem
  accessModes:
    - ReadWriteMany
  nfs: 
    path: /mnt/dpf_share/bfb
    server: $NFS_SERVER_IP
  persistentVolumeReclaimPolicy: Delete
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: bfb-pvc
  namespace: dpf-operator-system
spec:
  accessModes:
  - ReadWriteMany
  resources:
    requests:
      storage: 10Gi
  volumeMode: Filesystem
  storageClassName: ""
```
</details>

#### Additional Dependencies

Before deploying the DPF Operator, ensure that Helm is properly configured according to the [Helm prerequisites](../../../../getting-started/helm-prerequisites.md).  

> [!WARNING]
> This is a critical prerequisite step that must be completed for the DPF Operator to function properly.

After applying the additional dependencies you MUST ensure that the KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT
environment variables are set in the node-feature-discovery-worker DaemonSet.  
NFD needs to target the VIP because it needs to be up before cluster services can work.

Example commands to set the environment variables:

```shell
kubectl -n dpf-operator-system set env daemonset/node-feature-discovery-worker \
KUBERNETES_SERVICE_HOST=$TARGETCLUSTER_API_SERVER_HOST \
KUBERNETES_SERVICE_PORT=$TARGETCLUSTER_API_SERVER_PORT
```

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

### 3. DPF System Installation
This section involves creating the DPF system components and some basic infrastructure required for a functioning DPF-enabled cluster.

#### Deploy the DPF System components
A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
kubectl create ns dpu-cplane-tenant1
cat manifests/03-dpf-system-installation/*.yaml | envsubst | kubectl apply -f - 
```

This will create the following objects:
<details markdown="1"><summary>DPF Operator to install the DPF System components</summary>

[embedmd]:#(manifests/03-dpf-system-installation/operatorconfig.yaml)
```yaml
---
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  overrides:
    kubernetesAPIServerVIP: $TARGETCLUSTER_API_SERVER_HOST
    kubernetesAPIServerPort: $TARGETCLUSTER_API_SERVER_PORT
  provisioningController:
    bfbPVCName: "bfb-pvc"
    dmsTimeout: 900
  kamajiClusterManager:
    disable: false
```
</details>

<details markdown="1"><summary>DPUCluster to serve as Kubernetes control plane for DPU nodes</summary>

[embedmd]:#(manifests/03-dpf-system-installation/dpucluster.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUCluster
metadata:
  name: dpu-cplane-tenant1
  namespace: dpu-cplane-tenant1
spec:
  type: kamaji
  maxNodes: 10
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

### 4. Install Components to Enable Accelerated CNI Nodes

OVN Kubernetes will accelerate traffic by attaching a VF to each pod using the primary CNI. This VF is used to offload flows to the DPU. This section details the components needed to connect pods to the offloaded OVN Kubernetes CNI.

#### Install Multus and SRIOV Network Operator using NVIDIA Network Operator

```shell
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia --force-update
helm upgrade --no-hooks --install --create-namespace --namespace nvidia-network-operator network-operator nvidia/network-operator --version 25.7.0 -f ./manifests/04-enable-accelerated-cni/helm-values/network-operator.yml
```

<details markdown="1"><summary>NVIDIA Network Operator Helm values</summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/helm-values/network-operator.yml)
```yml
nfd:
  enabled: false
  deployNodeFeatureRules: false
sriovNetworkOperator:
  enabled: true
sriov-network-operator:
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
  crds:
    enabled: true
  sriovOperatorConfig:
    deploy: true
    configDaemonNodeSelector: null
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

#### Install the OVN Kubernetes resource injection webhook

The OVN Kubernetes resource injection webhook injected each pod scheduled to a worker node with a request for a VF and a Network Attachment Definition. This webhook is part of the same helm chart as the other components of the OVN Kubernetes CNI. Here it is installed by adjusting the existing helm installation to add the webhook component to the installation. 

```shell
envsubst < manifests/04-enable-accelerated-cni/helm-values/ovn-kubernetes.yml | helm upgrade --install -n ovn-kubernetes ovn-kubernetes-resource-injector ${OVN_KUBERNETES_REPO_URL}/ovn-kubernetes-chart --version $TAG --values -
```

<details markdown="1"><summary>OVN Kubernetes Resource Injector Helm values</summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/helm-values/ovn-kubernetes.yml)
```yml
ovn-kubernetes-resource-injector:
  ## Enable the ovn-kubernetes-resource-injector
  enabled: true
```
</details>

#### Apply the NICClusterConfiguration and SriovNetworkNodePolicy

```shell
cat manifests/04-enable-accelerated-cni/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>NICClusterPolicy for the NVIDIA Network Operator</summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/nic_cluster_policy.yaml)
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
      repository: ghcr.io/k8snetworkplumbingwg
      version: v3.9.3
```
</details>

<details markdown="1"><summary>SriovNetworkNodePolicy for the SR-IOV Network Operator</summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/sriov_network_operator_policy.yaml)
```yaml
---
apiVersion: sriovnetwork.openshift.io/v1
kind: SriovNetworkNodePolicy
metadata:
  name: bf3-p0-vfs
  namespace: nvidia-network-operator
spec:
  nicSelector:
    deviceID: "a2dc"
    vendor: "15b3"
    pfNames:
    - $DPU_P0#2-45
  nodeSelector:
    node-role.kubernetes.io/worker: ""
  numVfs: 46
  resourceName: bf3-p0-vfs
  isRdma: true
  externallyManaged: true
  deviceType: netdevice
  linkType: eth

```
</details>

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify that the accelerated CNI is enabled with:
```shell
## Ensure the nvidia-network-operator pods are ready.
kubectl wait --for=condition=Ready --namespace nvidia-network-operator pods --all
## Expect the following Daemonsets to be successfully rolled out.
kubectl rollout status daemonset --namespace nvidia-network-operator kube-multus-ds sriov-network-config-daemon sriov-device-plugin 
## Expect the network injector to be successfully rolled out.
kubectl rollout status deployment --namespace ovn-kubernetes ovn-kubernetes-resource-injector
```

### 5. DPU Provisioning and Service Installation

In this step we deploy our DPUs and the services that will run on them.

The user is expected to create a DPUDeployment object that reflects a set of DPUServices
that should run on a set of DPUs.

> If you want to learn more about `DPUDeployments`, feel free to check the [DPUDeployment documentation](../../../../developer-guides/api/dpudeployment.md).

#### Create the DPUDeployment, DPUServiceConfig, DPUServiceTemplate and other necessary objects

> [!WARNING]
> In case more than 1 DPU exists per node, the relevant selector should be applied in the DPUDeployment
> to select the appropriate DPU. See [DPUDeployment - DPUs Configuration](../../../../developer-guides/api/dpudeployment.md#dpus-configuration)
> to understand more about the selectors.

A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
cat manifests/05-dpudeployment-installation/*.yaml | envsubst | kubectl apply -f - 
```

This will deploy the following objects:

<details markdown="1"><summary>BFB to download Bluefield Bitstream to a shared volume</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/bfb.yaml)
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

<details markdown="1"><summary>HBN OVN SNAP DPUFlavor to correctly configure the DPUs on provisioning</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: hbn-ovnk-snap-nvme-$TAG
  namespace: dpf-operator-system
spec:
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
    - device: "*"
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
        - NVME_EMULATION_ENABLE=1
        - NVME_EMULATION_NUM_PF=1
        - NVME_EMULATION_NUM_VF=125
        - NVME_EMULATION_NUM_MSIX=2
        - LINK_TYPE_P1=ETH
        - LINK_TYPE_P2=ETH
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
      _ovs-vsctl set Open_vSwitch . other_config:ctl-pipe-size=1024
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-br br-hbn
      _ovs-vsctl set bridge br-hbn datapath_type=netdev
      _ovs-vsctl set bridge br-hbn fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Interface p0 mtu_request=9216
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical

      _ovs-vsctl set Open_vSwitch . external-ids:ovn-bridge-datapath-type=netdev
      _ovs-vsctl --may-exist add-br br-ovn
      _ovs-vsctl set bridge br-ovn datapath_type=netdev
      _ovs-vsctl br-set-external-id br-ovn bridge-id br-ovn
      _ovs-vsctl br-set-external-id br-ovn bridge-uplink puplinkbrovntobrsfc
      _ovs-vsctl --may-exist add-port br-ovn pf0hpf
      _ovs-vsctl set Interface pf0hpf type=dpdk
      _ovs-vsctl set Interface pf0hpf mtu_request=9216

  bfcfgParameters:
    - UPDATE_ATF_UEFI=yes
    - UPDATE_DPU_OS=yes
    - WITH_NIC_FW_UPDATE=yes

  hostNetworkInterfaceConfigs:
    - portNumber: 0
      dhcp: true
      mtu: 1500

  configFiles:
  - path: /etc/mellanox/mlnx-bf.conf
    operation: override
    raw: |
        ALLOW_SHARED_RQ="no"
        IPSEC_FULL_OFFLOAD="no"
        ENABLE_ESWITCH_MULTIPORT="yes"
        RDMA_SET_NETNS_EXCLUSIVE="no"
    permissions: "0644"
  - path: /etc/mellanox/mlnx-ovs.conf
    operation: override
    raw: |
        CREATE_OVS_BRIDGES="no"
        OVS_DOCA="yes"
    permissions: "0644"
  - path: /etc/mellanox/mlnx-sf.conf
    operation: override
    raw: ""
    permissions: "0644"
```
</details>

<details markdown="1"><summary>DPUDeployment to provision DPUs on worker nodes with SNAP Block Storage</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: hbn-ovnk-snap-nvme
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle-$TAG
    flavor: hbn-ovnk-snap-nvme-$TAG
    dpuSets:
    - nameSuffix: "dpuset1"
      nodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
  services:
    ovn:
      serviceTemplate: ovn
      serviceConfiguration: ovn
    hbn:
      serviceTemplate: hbn
      serviceConfiguration: hbn
    dts:
      serviceTemplate: dts
      serviceConfiguration: dts
    blueman:
      serviceTemplate: blueman
      serviceConfiguration: blueman
    snap-csi-plugin:
      serviceTemplate: snap-csi-plugin
      serviceConfiguration: snap-csi-plugin
    snap-host-controller:
      serviceTemplate: snap-host-controller
      serviceConfiguration: snap-host-controller
    snap-node-driver:
      serviceTemplate: snap-node-driver
      serviceConfiguration: snap-node-driver
    doca-snap:
      serviceTemplate: doca-snap
      serviceConfiguration: doca-snap
    block-storage-dpu-plugin:
      serviceTemplate: block-storage-dpu-plugin
      serviceConfiguration: block-storage-dpu-plugin
    spdk-csi-controller:
      serviceTemplate: spdk-csi-controller
      serviceConfiguration: spdk-csi-controller
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
            name: hbn
            interface: p0_if
      - ports:
        - serviceInterface:
            matchLabels:
              uplink: p1
        - service:
            name: hbn
            interface: p1_if
      - ports:
        - serviceInterface:
            matchLabels:
              port: ovn
        - service:
            name: hbn
            interface: pf2dpu2_if
      - ports:
        - service:
            name: doca-snap
            interface: app_sf
            ipam:
              matchLabels:
                svc.dpu.nvidia.com/pool: storage-pool
        - service:
            name: hbn
            interface: snap_if
```
</details>

<details markdown="1"><summary>OVN DPUServiceConfiguration and DPUServiceTemplate to deploy OVN workloads to the DPUs</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_ovn.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: ovn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "ovn"
  serviceConfiguration:
    helmChart:
      values:
        k8sAPIServer: https://$TARGETCLUSTER_API_SERVER_HOST:$TARGETCLUSTER_API_SERVER_PORT
        podNetwork: $POD_CIDR/24
        serviceNetwork: $SERVICE_CIDR
        dpuManifests:
          kubernetesSecretName: "ovn-dpu" # user needs to populate based on DPUServiceCredentialRequest
          vtepCIDR: "10.0.120.0/22" # user needs to populate based on DPUServiceIPAM
          hostCIDR: $TARGETCLUSTER_NODE_CIDR # user needs to populate
          ipamPool: "pool1" # user needs to populate based on DPUServiceIPAM
          ipamPoolType: "cidrpool" # user needs to populate based on DPUServiceIPAM
          ipamVTEPIPIndex: 0
          ipamPFIPIndex: 1
```

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_ovn.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: ovn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "ovn"
  helmChart:
    source:
      repoURL: $OVN_KUBERNETES_REPO_URL
      chart: ovn-kubernetes-chart
      version: $TAG
    values:
      commonManifests:
        enabled: true
      dpuManifests:
        enabled: true
      leaseNamespace: "ovn-kubernetes"
      gatewayOpts: "--gateway-interface=br-ovn"
```
</details>

<details markdown="1"><summary>HBN DPUServiceConfiguration and DPUServiceTemplate to deploy HBN workloads to the DPUs</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_hbn.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: hbn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "hbn"
  serviceConfiguration:
    serviceDaemonSet:
      annotations:
        k8s.v1.cni.cncf.io/networks: |-
          [
          {"name": "iprequest", "interface": "ip_lo", "cni-args": {"poolNames": ["loopback"], "poolType": "cidrpool"}},
          {"name": "iprequest", "interface": "ip_pf2dpu2", "cni-args": {"poolNames": ["pool1"], "poolType": "cidrpool", "allocateDefaultGateway": true}}
          ]
    helmChart:
      values:
        configuration:
          perDPUValuesYAML: |
            - hostnamePattern: "*"
              values:
                bgp_peer_group: hbn
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
                version: HBN 2.4.0
            - set:
                evpn:
                  enable: on
                nve:
                 vxlan:
                  enable: on
                  source:
                    address: {{ ipaddresses.ip_lo.ip }}
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
                  p0_if,p1_if,snap_if:
                    type: swp
                    link:
                      mtu: 9000
                  pf2dpu2_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf2dpu2.cidr }}: {}
                    type: swp
                    link:
                      mtu: 9000
                  snap_if:
                    bridge:
                      domain:
                        br_default:
                          access: 10
                  vlan10:
                    type: svi
                    vlan: 10
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
                          l2vpn-evpn:
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
                            address-family:
                              ipv4-unicast:
                                enable: on
                              ipv6-unicast:
                                enable: on
                              l2vpn-evpn:
                                enable: on
                            remote-as: external
  interfaces:
    ## NOTE: Interfaces inside the HBN pod must have the `_if` suffix due to a naming convention in HBN.
  - name: p0_if
    network: mybrhbn
  - name: p1_if
    network: mybrhbn
  - name: pf2dpu2_if
    network: mybrhbn
  - name: snap_if
    network: mybrhbn
```

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_hbn.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: hbn
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "hbn"
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: 1.0.3
      chart: doca-hbn
    values:
      image:
        repository: $HBN_NGC_IMAGE_URL
        tag: 3.1.0-doca3.1.0
      resources:
        memory: 6Gi
        nvidia.com/bf_sf: 4
```
</details>

<details markdown="1"><summary>DOCA Telemetry Service DPUServiceConfiguration and DPUServiceTemplate to deploy DTS to the DPUs</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_dts.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: dts
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "dts"
```

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_dts.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: dts
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "dts"
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: 1.23.4
      chart: doca-telemetry
```
</details>

<details markdown="1"><summary>Blueman DPUServiceConfiguration and DPUServiceTemplate to deploy Blueman to the DPUs</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_blueman.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: blueman
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "blueman"
```

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_blueman.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: blueman
  namespace: dpf-operator-system
spec:
  deploymentServiceName: "blueman"
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: 1.0.8
      chart: doca-blueman
```
</details>

<details markdown="1"><summary>DOCA SNAP DPUServiceConfiguration and DPUServiceTemplate for NVMe emulation</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_doca-snap.yaml)
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
              nvme_controller_create --nqn nqn.2022-10.io.nvda.nvme:0 --ctrl NVMeCtrl1 --pf_id 0 --admin_only
  interfaces:
    - name: app_sf
      network: mybrsfc
```

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_doca-snap.yaml)
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

<details markdown="1"><summary>SNAP CSI Plugin DPUServiceConfiguration and DPUServiceTemplate</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_snap-csi-plugin.yaml)
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
            controller:
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
            node:
              hostNetwork: true
```

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_snap-csi-plugin.yaml)
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

<details markdown="1"><summary>SNAP Host Controller DPUServiceConfiguration and DPUServiceTemplate</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_snap-host-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-host-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-host-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
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

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_snap-host-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-host-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-host-controller
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>SNAP Node Driver DPUServiceConfiguration and DPUServiceTemplate</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_snap-node-driver.yaml)
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

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_snap-node-driver.yaml)
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

<details markdown="1"><summary>Block Storage DPU Plugin DPUServiceConfiguration and DPUServiceTemplate</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_block-storage-dpu-plugin.yaml)
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

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_block-storage-dpu-plugin.yaml)
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

<details markdown="1"><summary>SPDK CSI Controller DPUServiceConfiguration and DPUServiceTemplate (Host Cluster)</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_spdk-csi-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: spdk-csi-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
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

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_spdk-csi-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: spdk-csi-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller
  helmChart:
    source:
      repoURL: oci://ghcr.io/mellanox/dpf-storage-vendors-charts
      version: v0.3.0
      chart: spdk-csi-controller
```
</details>

<details markdown="1"><summary>SPDK CSI Controller DPU DPUServiceConfiguration and DPUServiceTemplate (DPU Cluster)</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfiguration_spdk-csi-controller-dpu.yaml)
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

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicetemplate_spdk-csi-controller-dpu.yaml)
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

<details markdown="1"><summary>DPUServiceIPAM for SNAP storage networking</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/storage-ipam.yaml)
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
    perNodeIPCount: 4
```
</details>

<details markdown="1"><summary>Secret for SPDK CSI credentials</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/secret_spdk-csi.yaml)
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

<details markdown="1"><summary>SPDK CSI Controller DPUServiceCredentialRequest</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuservicecredentialrequest_spdk-csi-controller.yaml)
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

<details markdown="1"><summary>OVN DPUServiceCredentialRequest to allow cross cluster communication</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/ovn-credentials.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: ovn-dpu
  namespace: dpf-operator-system
spec:
  serviceAccount:
    name: ovn-dpu
    namespace: dpf-operator-system
  duration: 24h
  type: tokenFile
  secret:
    name: ovn-dpu
    namespace: dpf-operator-system
  metadata:
    labels:
      dpu.nvidia.com/image-pull-secret: ""
```
</details>

<details markdown="1"><summary>DPUServiceInterfaces for physical ports on the DPU</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/physical-ifaces.yaml)
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
```
</details>

<details markdown="1"><summary>OVN DPUServiceInterface to define the ports attached to OVN workloads on the DPU</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/ovn-iface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: ovn
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            port: ovn
        spec:
          interfaceType: ovn
```
</details>

<details markdown="1"><summary>DPUServiceIPAM to set up IP Address Management on the DPUCluster</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/hbn-ovn-ipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: pool1
  namespace: dpf-operator-system
spec:
  ipv4Network:
    network: "10.0.120.0/22"
    gatewayIndex: 3
    prefixSize: 29
```
</details>

<details markdown="1"><summary>DPUServiceIPAM for the loopback interface in HBN</summary>

[embedmd]:#(manifests/05-dpudeployment-installation/hbn-loopback-ipam.yaml)
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
</details>

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Note that the DPUService name will have a random suffix. For example, `hbn-vs6mj`. Use the correct name for the verification.

Verify the DPU and Service installation with:
```shell
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system dpuservices -l svc.dpu.nvidia.com/owned-by-dpudeployment=dpf-operator-system_hbn-ovnk-snap-nvme
## Ensure the DPUServiceIPAMs have been reconciled
kubectl wait --for=condition=DPUIPAMObjectReconciled --namespace dpf-operator-system dpuserviceipam --all
## Ensure the DPUServiceInterfaces have been reconciled
kubectl wait --for=condition=ServiceInterfaceSetReconciled --namespace dpf-operator-system dpuserviceinterface --all
## Ensure the DPUServiceChains have been reconciled
kubectl wait --for=condition=ServiceChainSetReconciled --namespace dpf-operator-system dpuservicechain --all
```

### 6. Test Traffic

#### Add worker nodes to the cluster

At this point workers should be added to the cluster. Each worker node should be configured in line with [the prerequisites](../../prerequisites/system.md).
As workers are added to the cluster DPUs will be provisioned and DPUServices will begin to be spun up.

You can verify the status of the DPUDeployment and its components with the following command:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
```

#### Deploy test pods

```shell
kubectl apply -f manifests/06-test-traffic
```

HBN and OVN functionality can be tested by pinging between the pods and services deployed in the default namespace.

### 7. Apply Storage Configuration

```shell
kubectl apply -f manifests/07-storage-configuration
```

This will create the following objects:

<details markdown="1"><summary>DPUStorageVendor for SPDK CSI</summary>

[embedmd]:#(manifests/07-storage-configuration/dpustoragevendor_spdk-csi.yaml)
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

<details markdown="1"><summary>DPUStoragePolicy for block storage</summary>

[embedmd]:#(manifests/07-storage-configuration/dpustoragepolicy_policy-block.yaml)
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

Wait for the objects to be ready:

```shell
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpustoragevendors --all
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpustoragepolicies --all
```

### 8. Test Storage Workload

This section walks you through deploying a workload that utilizes storage resources provided by the SNAP service.

```shell
kubectl apply -f manifests/08-test-storage
```

This will create the following objects:

<details markdown="1"><summary>StorageClass for SNAP NVMe VF</summary>

[embedmd]:#(manifests/08-test-storage/storageclass.yaml)
```yaml
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: snap-nvme-vf
provisioner: csi.snap.nvidia.com
parameters:
  policy: "policy-block"
  functionType: "vf"
  hotplugFunction: "false"
```
</details>

<details markdown="1"><summary>StatefulSet with NVMe VF block storage</summary>

[embedmd]:#(manifests/08-test-storage/sts-block.yaml)
```yaml
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: storage-test-pod-nvme-vf
spec:
  serviceName: "storage-test-pod-nvme-vf"
  podManagementPolicy: "Parallel"
  replicas: 1
  selector:
    matchLabels:
      app: storage-test-pod-nvme-vf
  template:
    metadata:
      labels:
        app: storage-test-pod-nvme-vf
    spec:
      containers:
      - name: test
        image: registry.k8s.io/nginx-slim:0.21
        volumeDevices:
          - name: vol1
            devicePath: /dev/xvda
  volumeClaimTemplates:
  - metadata:
      name: vol1
    spec:
      accessModes: [ "ReadWriteOnce" ]
      volumeMode: Block
      storageClassName: snap-nvme-vf
      resources:
        requests:
          storage: 1Gi
```
</details>

#### Verification

Wait for the pod to be in running state:

```shell
kubectl wait --for=condition=ready pod -l app=storage-test-pod-nvme-vf --timeout=300s
```

Verify the block device is available in the pod:

```shell
kubectl exec -it storage-test-pod-nvme-vf-0 -- ls -l /dev/xvda
```

You can test read/write operations on the block device:

```shell
## Write test data to the block device
kubectl exec -it storage-test-pod-nvme-vf-0 -- sh -c "echo 'test data' | dd of=/dev/xvda bs=512 count=1"

## Read back the test data
kubectl exec -it storage-test-pod-nvme-vf-0 -- dd if=/dev/xvda bs=512 count=1 2>/dev/null
```

## Uninstall

This section describes how to clean up the cluster after the DPF setup has been completed. 
It is important to follow the steps in the correct order to ensure that all 
components are removed cleanly and that the cluster remains functional.

### Delete the storage test resources
```shell
kubectl delete -f manifests/08-test-storage --wait --ignore-not-found=true
# delete all PVCs created by StatefulSet
kubectl delete pvc --selector=app=storage-test-pod-nvme-vf --wait -n default
kubectl delete -n dpf-operator-system dpuvolumeattachment --all --wait
kubectl delete -n dpf-operator-system dpuvolume --all --wait
# delete storage configuration
kubectl delete -f manifests/07-storage-configuration --wait --ignore-not-found=true
```

### Delete the network test pods
```shell
kubectl delete -f manifests/06-test-traffic --wait --ignore-not-found=true
```

### Delete DPF CNI acceleration components
```shell
kubectl delete -f manifests/04-enable-accelerated-cni --wait --ignore-not-found=true
helm uninstall -n nvidia-network-operator network-operator --wait

## Note: Uninstalling OVN Kubernetes as primary CNI is not supported but this command must be run to remove the webhook and restore a functioning cluster.
helm uninstall -n ovn-kubernetes ovn-kubernetes-resource-injector --wait
```

### Delete the DPF Operator system and DPF Operator

First we have to delete some DPUServiceInterfaces. This is necessary because of a known issue during uninstallation.

```shell
kubectl delete -n dpf-operator-system dpuserviceinterface p0 p1 ovn --wait
```

Then we can delete the config and system namespace.

```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

### Delete DPF Operator PVC
```shell
kubectl -n dpf-operator-system delete pvc bfb-pvc
kubectl delete pv bfb-pv
```

Note: there can be a race condition with deleting the underlying Kamaji cluster which runs the DPU cluster control plane in this guide. If that happens it may be necessary to remove finalizers manually from `DPUCluster` and `Datastore` objects.

## Limitations of DPF Setup

### Host network pod services

The Kubelet process on the Kubernetes nodes use the OOB interface IP address to register in Kubernetes. This means that the nodes have the OOB IP addresses as node IP addresses. This means that pods using host networking have the OOB IP address of the hosts as pod IP address. However, that interface is not accelerated. This means that any component using the addresses of the pods using host networking will not benefit from hardware acceleration and high-speed ports.

For example, this means that when creating a Kubernetes NodePort service selecting pods using host networking, even if the user uses the high-speed IP of the host, the traffic will not be accelerated. In order to solve this, it is possible to create dedicated endpointSlices that contain the host high-speed port IP addresses instead of OOB port IP addresses. This way, the entire path to the pods will be accelerated and benefit from high performances, if the user uses the high speed IP address of the host with the nodePort port. This requires the workload running on the pod with host networking to also listen on the high-speed port IP address.
