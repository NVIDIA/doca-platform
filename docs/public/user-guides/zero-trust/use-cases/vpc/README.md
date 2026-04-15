---
title: "OVN VPC Service Deployment Guide"
---

> [!NOTE]
> OVN VPC service is considered tech preview and is not recommended for
> production use.

This configuration provides instructions for deploying the NVIDIA DOCA Platform Framework (DPF) on high-performance,
bare-metal infrastructure in Zero Trust mode, utilizing DPU BMC and Redfish. It focuses on provisioning NVIDIA®
BlueField®-3 DPUs using DPF, Deploying VPC OVN Service and enabling hosts to communicate through an isolated VPC.

[TOC]

## Prerequisites
This guide should be run by cloning the repo from [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform)
and moving to the `docs/public/user-guides/zero-trust/use-cases/vpc` directory.

The system is set up as described in the [prerequisites](../../prerequisites/README.md).

### Software Prerequisites
Install the following tools on the machine where you will run the commands in this guide:

* kubectl
* helm
* envsubst

### Network Prerequisites

#### Worker Nodes

* Only a single DPU uplink is used with this deployment (p0)
* All worker nodes are connected to the same L2 broadcast domain (VLAN) on the high-speed network

## Installation Guide

Commands in this guide are run in the same directory that contains this readme.

> [NOTE!]
> This deployment guide assumes that two subnets are available in the network
> for use by the VPC service for tunneled and external traffic. It is possible to use a single
> subnet for both traffic types with minor modifications to the deployment manifests.
> Refer to [OVN VPC Deployment](#4-ovn-vpc-deployment) for more information

### 0. Required Variables

The following variables are required. Sensible defaults are provided where possible, but many values will be specific to your target infrastructure.

<details markdown="1"><summary>Environment variables file</summary>

[embedmd]:# (manifests/00-env-vars/envvars.env sh)
```sh
## IP Address for the Kubernetes API server of the target cluster on which DPF is installed.
## This should never include a scheme or a port.
## e.g. 10.10.10.10
export TARGETCLUSTER_API_SERVER_HOST=

## Port for the Kubernetes API server of the target cluster on which DPF is installed.
## e.g. 6443
export TARGETCLUSTER_API_SERVER_PORT=

## Virtual IP used by the load balancer for the DPU Cluster. Must be a reserved IP from the management subnet and not
## allocated by DHCP.
export DPUCLUSTER_VIP=

## Interface on which the DPUCluster load balancer will listen. Should be the management interface of the control plane
## node.
export DPUCLUSTER_INTERFACE=

## IP address to the NFS server used as storage for the BFB.
export NFS_SERVER_IP=

## The DPF REGISTRY is the Helm repository URL where the DPF Operator Chart resides.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export REGISTRY=https://helm.ngc.nvidia.com/nvidia/doca

## The repository URL for the NVIDIA Helm chart registry.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export HELM_REGISTRY_REPO_URL=https://helm.ngc.nvidia.com/nvidia/doca

## IP_RANGE_START and IP_RANGE_END
## These define the IP range for DPU discovery via Redfish/BMC interfaces
## Example: If your DPUs have BMC IPs in range 192.168.1.100-110
## export IP_RANGE_START=192.168.1.100
## export IP_RANGE_END=192.168.1.110
export IP_RANGE_START=

export IP_RANGE_END=

# The password used for DPU BMC root login, must be the same for all DPUs
export BMC_ROOT_PASSWORD=

## IP Address through which ovn-central service (exposed as NodePort)
## is accessible. This can be a VIP or one of the control-plane node IP
## in the host k8s cluster.
## This should never include a scheme or a port.
## e.g. 10.10.10.10
export TARGETCLUSTER_OVN_CENTRAL_IP=${TARGETCLUSTER_API_SERVER_HOST}

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

#### Create storage required by the DPF Operator

```shell
kubectl create ns dpf-operator-system
cat manifests/01-dpf-operator-installation/*.yaml | envsubst | kubectl apply -f -
```

This deploys the following objects:

<details markdown="1"><summary>PersistentVolume and PersistentVolumeClaim for the provisioning controller</summary>

[embedmd]:#(manifests/01-dpf-operator-installation/nfs-storage-for-bfb-dpf-ga.yaml)
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

#### Create DPU BMC shared password secret

In Zero Trust mode, provisioning DPUs requires authentication with Redfish. In order to do that,
you must set the same root password to access the BMC for all DPUs DPF is going to manage.

For more information on how to set the BMC root password refer to [BlueField DPU Administrator Quick Start Guide](https://docs.nvidia.com/networking/display/bf3dpu/bluefield+dpu+administrator+quick+start+guide#src-2449222348_safe-id-Qmx1ZUZpZWxkRFBVQWRtaW5pc3RyYXRvclF1aWNrU3RhcnRHdWlkZS1TdGVwM-KAk0NoYW5nZURlZmF1bHRQYXNzd29yZA)

The password is provided to DPF by creating the following secret:

```shell
kubectl create secret generic -n dpf-operator-system bmc-shared-password --from-literal=password=$BMC_ROOT_PASSWORD
```

#### Additional Dependencies

Before deploying the DPF Operator, ensure that Helm is properly configured according to the
[Helm prerequisites](../../../../getting-started/helm-prerequisites.md).

> [!WARNING]
> This is a critical prerequisite step that must be completed for the DPF Operator to function properly.

#### Deploy the DPF Operator

##### HTTP Registry (default)

If the $REGISTRY is an HTTP Registry (default value) use this command:
```shell
helm repo add --force-update dpf-repository ${REGISTRY}
helm repo update
helm upgrade --install -n dpf-operator-system dpf-operator dpf-repository/dpf-operator --version=$TAG
```

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

This section involves creating the DPF system components and some basic infrastructure required for a functioning
DPF-enabled cluster.

#### Deploy the DPF System components

```shell
kubectl create ns dpu-cplane-tenant1
cat manifests/02-dpf-system-installation/*.yaml | envsubst | kubectl apply -f -
```

This will create the following objects:
<details markdown="1"><summary>DPFOperatorConfig to install the DPF System components</summary>

[embedmd]:#(manifests/02-dpf-system-installation/operatorconfig.yaml)
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
  dpuDetector:
    disable: true
  provisioningController:
    bfbPVCName: "bfb-pvc"
    dmsTimeout: 900
    registry:
      # Set this to the IP of one of your control plane nodes + 8080 port
      address: "http://$TARGETCLUSTER_API_SERVER_HOST:8080"
    installInterface:
      installViaRedfish:
        skipDPUNodeDiscovery: false
  kamajiClusterManager:
    disable: false
```
</details>

<details markdown="1"><summary>DPUCluster to serve as Kubernetes control plane for DPU nodes</summary>

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

<details markdown="1"><summary>DPUDiscovery to discover DPUDevices or DPUNodes</summary>

[embedmd]:#(manifests/02-dpf-system-installation/dpudiscovery.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDiscovery
metadata:
  name: dpu-discovery
  namespace: dpf-operator-system
spec:
  ipRangeSpec:
    ipRange:
      startIP: $IP_RANGE_START
      endIP: $IP_RANGE_END
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
## Ensure bfb registry daemonset is available
kubectl rollout status daemonset --namespace dpf-operator-system bfb-registry
## Ensure the DPUCluster is ready for nodes to join.
kubectl wait --for=condition=ready --namespace dpu-cplane-tenant1 dpucluster --all
```

### 3. Create BFB and DPUFlavor

Create a BFB and DPUFlavor to be used for the DPU provisioning process

```shell
cat manifests/03-bfb-and-flavor/* | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>OVN VPC DPUDeployment</summary>

[embedmd]:#(manifests/03-bfb-and-flavor/bfb.yaml)
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

[embedmd]:#(manifests/03-bfb-and-flavor/dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: vpc-flavor-$TAG
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
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Interface p0 mtu_request=9216
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical
```

</details>

### 4. OVN VPC Deployment

The OVN VPC service consists of the following components:

1. **ovn-central**: OVN central(runs northd, sb_db, nb_db) deployed in the target cluster
2. **ovn-controller**: OVN controller deployed in the DPU cluster
3. **vpc-ovn-controller**: VPC controller deployed in the target cluster
4. **vpc-ovn-node**: VPC node agent in the DPU cluster

#### Deploy target cluster components using Helm

##### Using HTTP Registry (default)

```shell
helm repo add --force-update ovn-vpc-repository ${HELM_REGISTRY_REPO_URL}
helm repo update
helm upgrade --install -n dpf-operator-system ovn-central ovn-vpc-repository/ovn-chart \
  --version=$TAG --wait -f manifests/04.1-vpc-ovn-target-cluster/helm-values/ovn-central.yaml
helm upgrade --install -n dpf-operator-system vpc-ovn-controller ovn-vpc-repository/dpf-vpc-ovn \
  --version=$TAG --wait -f manifests/04.1-vpc-ovn-target-cluster/helm-values/vpc-ovn-controller.yaml
```

##### Using OCI Registry

For development purposes, if the $HELM_REGISTRY_REPO_URL is an OCI Registry use this command:
```shell
helm upgrade --install -n dpf-operator-system ovn-central $HELM_REGISTRY_REPO_URL/ovn-chart \
  --version=$TAG --wait -f manifests/04.1-vpc-ovn-target-cluster/helm-values/ovn-central.yaml
helm upgrade --install -n dpf-operator-system vpc-ovn-controller $HELM_REGISTRY_REPO_URL/dpf-vpc-ovn \
  --version=$TAG --wait -f manifests/04.1-vpc-ovn-target-cluster/helm-values/vpc-ovn-controller.yaml
```

<details markdown="1"><summary>OVN Central Helm Values</summary>

[embedmd]:#(manifests/04.1-vpc-ovn-target-cluster/helm-values/ovn-central.yaml)
```yaml
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

</details>

<details markdown="1"><summary>VPC OVN Controller Helm Values</summary>

[embedmd]:#(manifests/04.1-vpc-ovn-target-cluster/helm-values/vpc-ovn-controller.yaml)
```yaml
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

</details>

#### Deploy DPU cluster components using DPUDeployment

> [!WARNING]
> In case more than 1 DPU exists per node, the relevant selector should be applied in the DPUDeployment
> to select the appropriate DPU. See [DPUDeployment - DPUs Configuration](../../../../developer-guides/api/dpudeployment.md#dpus-configuration)
> to understand more about the selectors.

```shell
cat manifests/04.2-vpc-ovn-dpudeployment/* | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>OVN VPC DPUDeployment</summary>

[embedmd]:#(manifests/04.2-vpc-ovn-dpudeployment/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: vpc-ovn
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle-$TAG
    flavor: vpc-flavor-$TAG
    nodeEffect:
      hold: true
    dpuSetStrategy:
      type: OnDelete
    dpuSets:
    - nameSuffix: "dpuset1"
      dpuNodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
  services:
    ovn-controller:
      serviceTemplate: ovn-controller
      serviceConfiguration: ovn-controller
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
              ovn.vpc.dpu.nvidia.com/interface: ovn-ext-patch
```

[embedmd]:#(manifests/04.2-vpc-ovn-dpudeployment/dpuserviceconfig-ovn-controller.yaml)
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

[embedmd]:#(manifests/04.2-vpc-ovn-dpudeployment/dpuserviceconfig-vpc-ovn-node.yaml)
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
                allocateIPWithIndex: 1
              - name: "gateway"
                poolName: "vpc-ippool-gateway"
                allocateIPWithIndex: 1
```

[embedmd]:#(manifests/04.2-vpc-ovn-dpudeployment/dpuservicetemplate-ovn-controller.yaml)
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

[embedmd]:#(manifests/04.2-vpc-ovn-dpudeployment/dpuservicetemplate-vpc-ovn-node.yaml)
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

[embedmd]:#(manifests/04.2-vpc-ovn-dpudeployment/dpuserviceipam.yaml)
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

[embedmd]:#(manifests/04.2-vpc-ovn-dpudeployment/dpuserviceinterface.yaml)
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
          interfaceType: patch
          patch:
            peerBridge: br-ovn-ext
```

</details>

> [NOTE!]
> In the above deployment we assume separate network subnets for VTEP(tunneled) and external networks.
> In case its desirable to use only a single network subnet for both traffic types (tunneled, external),
> simply modify `vpc-ovn-node` DPUServiceConfiguration to reference the same IP pool under `ipRequests` field.
> 
> **Example:**
>   ```yaml
>   ipRequests:
>     - name: "vtep"
>       poolName: "vpc-ippool-vtep"
>       allocateIPWithIndex: 1
>     - name: "gateway"
>       poolName: "vpc-ippool-vtep"
>       allocateIPWithIndex: 2
>   ```

#### Releasing the Node Effect Hold

Since the DPUDeployment is configured with `nodeEffect.hold: true`, the DPUs will pause at the "Node Effect"
phase and wait for external action before proceeding with provisioning. This gives the administrator control
over when the node effect is applied.

To check that DPUNodeMaintenance objects have been created and are in the hold state:

```shell
kubectl get dpunodemaintenances -n dpf-operator-system
```

Once you are ready for provisioning to proceed, release the hold by setting the annotation on the
DPUNodeMaintenance objects to `"false"`. You can do this per-node or all at once:

```shell
kubectl annotate --overwrite dpunodemaintenances -n dpf-operator-system --all provisioning.dpu.nvidia.com/wait-for-external-nodeeffect=false
```

After releasing the hold, the DPUs will proceed through the remaining provisioning phases (BFB installation,
OS installation, etc.).

#### Make DPUs Ready

In order to make the DPUs ready, we will need to manually power cycle the host. This operation should be done in the
most graceful manner by gracefully shutting down the Host and DPU, powering off the server and then powering it on to
avoid corruption. This should happen when the DPU object gives us the signal. The described flow can be automated by the
administrator depending on the infrastructure.

The following verification commands may need to be run multiple times to ensure the condition is met.

**1.** Wait for DPU `OSInstalled` condition to become `ready`

```shell
kubectl wait --for=condition=OSInstalled --namespace dpf-operator-system dpu --all
```

**2.** Ensure `Rebooted` condition type has `reason=WaitingForManualPowerCycleOrReboot`

```shell
kubectl wait --namespace dpf-operator-system dpu --all --for=jsonpath='{.status.conditions[?(@.type=="Rebooted")].reason}'=WaitingForManualPowerCycleOrReboot
```

**3.** Power cycle DPU worker hosts - manual operation by the user

**4.** Once all nodes have rebooted, remove `provisioning.dpu.nvidia.com/dpunode-external-reboot-required` annotation from `DPUNodes`

```shell
kubectl -n dpf-operator-system annotate dpunode --all provisioning.dpu.nvidia.com/dpunode-external-reboot-required-
```

**5.** Ensure `DPUs` are ready

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
        ├─BFB/bf-bundle                dpf-operator-system  Ready: True   Ready     58m    File: bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb, DOCA: 3.2.1
        ├─DPU/worker1-0000-c8-00       dpf-operator-system  Ready: True   DPUReady  2m13s
        └─DPU/worker2-0000-c8-00       dpf-operator-system  Ready: True   DPUReady  2m30s
```

### 5. Additional VPC Resources Deployment

In this step, you will deploy the `IsolationClass` resource, which will be used by subsequent user-created `DPUVPC` and `DPUVirtualNetwork` resources.

#### Deploy IsolationClass

```shell
cat manifests/05-vpc-resources/* | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>Additional VPC Resources</summary>

[embedmd]:#(manifests/05-vpc-resources/ovn-isolation-class.yaml)
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
    ovn-sb-endpoint: "tcp:$TARGETCLUSTER_OVN_CENTRAL_IP:30642"
```

</details>

### 6. Optional - Test Traffic

At this point, your cluster should be set up and ready with all VPC components.

In this section we will demonstrate how to connect a host to VPC in two ways.

1. Using Host PFs (The DPU's host facing PCI physical functions)
2. Using Host PFs and VFs (The DPU's host facing PCI physical and virtual functions)

#### 1. Using Host PFs

In this step, we will deploy the following VPC objects:

* One `DPUVPC` named `myvpc`
* One `DPUVirtualNetwork` named `pfnet` in `myvpc` VPC
* One `DPUServiceInterface` of type `PF`, referencing `pfnet` virtual network. 
  * for DPU PF 0
  * spanning all worker nodes

**Outcome**:
Hosts will be able to get DHCP from VPC on DPU PF 0 and communicate
with each other and external networks.

Ensure you have SSH access to your worker hosts from the management or out-of-band (OOB) network.

##### Deploy test topology

```shell
cat manifests/06-optional-test-traffic/vpc-topology-pf-only.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>VPC Test Topology</summary>

[embedmd]:#(manifests/06-optional-test-traffic/vpc-topology-pf-only.yaml)
```yaml
---
apiVersion: vpc.dpu.nvidia.com/v1alpha1
kind: DPUVPC
metadata:
  name: myvpc
  namespace: default
spec:
  tenant: foo
  isolationClassName: ovn.vpc.dpu.nvidia.com
  interNetworkAccess: false
  nodeSelector: {}
---
apiVersion: vpc.dpu.nvidia.com/v1alpha1
kind: DPUVirtualNetwork
metadata:
  name: pfnet
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
        subnet: 10.100.0.0/16
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf0
  namespace: default
spec:
  template:
    spec:
      template:
        spec:
          interfaceType: pf
          pf:
            pfID: 0
            virtualNetwork: pfnet
```

</details>


##### Validate deployed resources

```shell
kubectl wait --for=condition=ready dpuvpc myvpc
kubectl wait --for=condition=ready dpuvirtualnetwork pfnet
kubectl wait --for=condition=ready dpuserviceinterface pf0
```

##### Test traffic between hosts

* SSH into each node and run `dhclient` for the network device associated with PF index 0 to obtain a DHCP address.

An example output for a node named `node1` and PF 0 network interface `enp8s0f0`:
```shell
root@node1:~# ip link set enp8s0f0 up
root@node1:~# dhclient -1 -v enp8s0f0
Internet Systems Consortium DHCP Client 4.4.3-P1
Copyright 2004-2022 Internet Systems Consortium.
All rights reserved.
For info, please visit https://www.isc.org/software/dhcp/

Listening on LPF/enp8s0f0/26:3a:60:48:81:cf
Sending on   LPF/enp8s0f0/26:3a:60:48:81:cf
Sending on   Socket/fallback
DHCPREQUEST for 10.100.0.2 on enp8s0f0 to 255.255.255.255 port 67 (xid=0x7cbe87ca)
DHCPACK of 10.100.0.2 from 10.100.0.1 (xid=0xca87be7c)
bound to 10.100.0.2 -- renewal in 1367 seconds.
```

Repeat this process on another node.

* Test connectivity by running traffic between nodes.

In the example below, the other node's PF 0 network interface was assigned the IP 10.100.0.3:
```shell
root@node1:~# ping 10.100.0.3
```

#### 2. Using Host PFs and VFs

In this step, we will deploy the following VPC objects:

* One `DPUVPC` named `myvpc`
* One `DPUVirtualNetwork` named `pfnet` in `myvpc` VPC
* One `DPUVirtualNetwork` named `vfnet` in `myvpc` VPC
* One `DPUServiceInterface` of type `PF`, referencing `pfnet` virtual network. 
  * for DPU PF 0
  * spanning all worker nodes
* Two `DPUServiceInterface` of type `VF`, referencing `vfnet` virtual network. 
  * for VF indexes 0,1 of PF 0
  * spanning all worker nodes

**Outcome**:
Hosts will be able to get DHCP from VPC on the configured DPU PFs and VFs and communicate
in the following manner:

1. PFs can communicate with other PFs
2. VFs can communicate with other VFs
3. PFs cannot communicate with VFs
4. PFs and VFs can access external network

Ensure you have SSH access to your worker hosts from the management or out-of-band (OOB) network.

##### Create SR-IOV virtual functions for each DPU
Login to each host and create SR-IOV virtual functions(VFs)

Example for creating VFs on node1, do the same on the other node. the DPU is assumed to have PCI address of `0000:08:00.0`
```shell
root@node1:~# echo 2 > /sys/bus/pci/devices/0000:08:00.0/sriov_numvfs
```

##### Deploy test topology

```shell
cat manifests/06-optional-test-traffic/* | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>VPC Test Topology</summary>

[embedmd]:#(manifests/06-optional-test-traffic/vpc-topology-pf-only.yaml)
```yaml
---
apiVersion: vpc.dpu.nvidia.com/v1alpha1
kind: DPUVPC
metadata:
  name: myvpc
  namespace: default
spec:
  tenant: foo
  isolationClassName: ovn.vpc.dpu.nvidia.com
  interNetworkAccess: false
  nodeSelector: {}
---
apiVersion: vpc.dpu.nvidia.com/v1alpha1
kind: DPUVirtualNetwork
metadata:
  name: pfnet
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
        subnet: 10.100.0.0/16
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf0
  namespace: default
spec:
  template:
    spec:
      template:
        spec:
          interfaceType: pf
          pf:
            pfID: 0
            virtualNetwork: pfnet
```

[embedmd]:#(manifests/06-optional-test-traffic/vpc-topology-vf-addon.yaml)
```yaml
---
apiVersion: vpc.dpu.nvidia.com/v1alpha1
kind: DPUVirtualNetwork
metadata:
  name: vfnet
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
        subnet: 10.200.0.0/16
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: vf0
  namespace: default
spec:
  template:
    spec:
      template:
        spec:
          interfaceType: vf
          vf:
            pfID: 0
            vfID: 0
            virtualNetwork: vfnet
            parentInterfaceRef: ""
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: vf1
  namespace: default
spec:
  template:
    spec:
      template:
        spec:
          interfaceType: vf
          vf:
            pfID: 0
            vfID: 1
            virtualNetwork: vfnet
            parentInterfaceRef: ""
```

</details>


##### Validate deployed resources

```shell
kubectl wait --for=condition=ready dpuvpc myvpc
kubectl wait --for=condition=ready dpuvirtualnetwork pfnet vfnet
kubectl wait --for=condition=ready dpuserviceinterface pf0 vf0 vf1
```

##### Test traffic between hosts

In this section we will demonstrate how to request DHCP for a VF interfaces and run basic traffic between
VFs on different hosts.

To do the same for PF interfaces refer to [Test traffic between hosts](#test-traffic-between-hosts) of the previous section.

* SSH into each node and run `dhclient` for the network device associated with VF index 0 to obtain a DHCP address.

An example output for a node named `node1` and VF 0 network interface `enp8s0f0`:
```shell
# send dhcp request
root@node1:~# ip link set enp8s0f0v0 up
root@node1:~# dhclient -1 -v enp8s0f0v0
Internet Systems Consortium DHCP Client 4.4.3-P1
Copyright 2004-2022 Internet Systems Consortium.
All rights reserved.
For info, please visit https://www.isc.org/software/dhcp/

Listening on LPF/enp8s0f0v0/26:3a:60:48:81:cf
Sending on   LPF/enp8s0f0v0/26:3a:60:48:81:cf
Sending on   Socket/fallback
DHCPREQUEST for 10.200.0.2 on enp8s0f0v0 to 255.255.255.255 port 67 (xid=0x7cbe87ca)
DHCPACK of 10.200.0.2 from 10.200.0.1 (xid=0xca87be7c)
bound to 10.200.0.2 -- renewal in 1367 seconds.
```

Repeat this process for the second VF on this node and on another node.

* Test connectivity by running traffic between nodes.

In the example below, the other node's VF 0 network interface was assigned the IP 10.200.0.3:
```shell
root@node1:~# ping 10.200.0.3
```

## Uninstall

This section covers only the DPF related components and not the prerequisites as these must be managed by the administrator.

### 1. Remove VPC Resources from the Cluster

```shell
cat manifests/06-optional-test-traffic/* | kubectl delete --wait -f -
cat manifests/05-vpc-resources/* | kubectl delete --wait -f -
```

### 2. Remove Deployed OVN VPC charts

```shell
helm uninstall -n dpf-operator-system vpc-ovn-controller --wait
helm uninstall -n dpf-operator-system ovn-central --wait
```

### 3. Remove DPF System and Operator Installation

```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

### 4. Delete DPF Operator PVC

```shell
kubectl -n dpf-operator-system delete pvc bfb-pvc
kubectl delete pv bfb-pv
```

> [!NOTE]
> There can be a race condition with deleting the underlying Kamaji cluster which runs the DPU cluster control
> plane in this guide. If that happens it may be necessary to remove finalizers manually from `DPUCluster` and `Datastore`
> objects.
