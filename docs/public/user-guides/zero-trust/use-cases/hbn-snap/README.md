---
title: "HBN and SNAP Storage in DPF Zero Trust"
---


> [!NOTE]
> Follow this guide from the source GitHub repo at [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform)
> and moving to the `docs/public/user-guides/zero-trust/use-cases/hbn-snap/README.md` for better formatting of
> the code.


This configuration provides instructions for deploying the NVIDIA DOCA Platform Framework (DPF) on high-performance,
bare-metal infrastructure in Zero Trust mode, utilizing DPU BMC and Redfish. It focuses on provisioning NVIDIA®
BlueField®-3 DPUs using DPF, installing the HBN DPUService on those DPUs and enabling SNAP Storage on those DPUs.

[[_TOC_]]

## Prerequisites

This guide should be run by cloning the repo from [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform)
and moving to the `docs/public/user-guides/zero-trust/use-cases/hbn-snap` directory.

The system is set up as described in the [prerequisites](../../prerequisites/README.md).

In addition, for this use case, the Top of Rack switch(ToR) must support BGP and EVPN, and should be configured to
support unnumbered BGP towards the two ports of the DPU, where HBN will act as peer, and advertise routes over BGP to
allow for ECMP from the DPU.
The storage traffic between the DPU and the remote storage system goes through a VXLAN interface.
Additional information about the required switch configuration can be found in the 
[Technology Preview for DPF Zero Trust (DPF-ZT) with SNAP DPU Service in virtio-fs mode](https://docs.nvidia.com/networking/display/public/sol/tp+for+dpf+zero+trust+(dpf-zt)+with+snap+dpu+service+in+virtio-fs+mode).

This guide includes examples for both SNAP Block (NVMe) and SNAP VirtioFS Storage.
Depending on the storage type you want to deploy, you need to ensure that the following prerequisites are met:

### SNAP Block (NVMe) Prerequisites

* An remote SPDK target should be set up to provide persistent storage for SNAP Block Storage.
* The SPDK target should be reachable from the DPUs.
* The management interface of the SPDK target should be reachable from the control plane nodes.
* Make sure to check [Host OS Configuration Section in SNAP service documentation](https://docs.nvidia.com/doca/sdk/snap-4-service-appendixes/index.html#src-4464939880_id-.SNAP4ServiceAppendixesv3.2.0LC-HostOSConfiguration) to validate the host OS configuration on the worker nodes.

### SNAP VirtioFS Prerequisites

* An external NFS server is required to provide persistent storage for SNAP VirtioFS.
* The NFS server must be reachable by both the SNAP DPU service and the nvidia-fs DPU plugin. 
* The NFS service must also be accessible from the DPF control plane nodes to ensure proper operation.
* Make sure to check [Host OS Configuration Section in SNAP VirtioFS service documentation](https://docs.nvidia.com/doca/sdk/doca-snap-virtio-fs-service-guide/index.html#src-4413882927_safe-id-aWQtLkRPQ0FTTkFQVmlydGlvZnNTZXJ2aWNlR3VpZGV2My4yLjBMQy1BcHBlbmRpeOKAk0hvc3RPU0NvbmZpZ3VyYXRpb25BcHBlbmRpeOKAk0hvc3RPU0NvbmZpZ3VyYXRpb24) to validate the host OS configuration on the worker nodes.

### Software Prerequisites

The following tools must be installed on the machine where the commands contained in this guide run:

* kubectl
* helm
* envsubst

## Installation Guide

This guide assumes that the setup includes only 2 workers with DPUs. If your setup has more than 2 workers, then you
will need to set additional variables to enable the rest of the DPUs.

### 0. Required Variables

The following variables are required by this guide. A sensible default is provided where it makes sense, but many will
be specific to the target infrastructure.

Commands in this guide are run in the same directory that contains this readme.

<details markdown="1"><summary><b>Environment variables file</b></summary>

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
export TAG=v26.4.0

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BFB_URL="https://content.mellanox.com/BlueField/BFBs/Ubuntu24.04/bf-bundle-3.4.0-92_26.04_ubuntu-24.04_64k_prod.bfb"

## IP_RANGE_START and IP_RANGE_END
## These define the IP range for DPU discovery via Redfish/BMC interfaces
## Example: If your DPUs have BMC IPs in range 192.168.1.100-110
## export IP_RANGE_START=192.168.1.100
## export IP_RANGE_END=192.168.1.110
export IP_RANGE_START=

export IP_RANGE_END=

# The password used for DPU BMC root login, must be the same for all DPUs
export BMC_ROOT_PASSWORD=

## Serial number of DPUs. If you have more than 2 DPUs, you will need to parameterize the system accordingly and expose
## additional variables.
## All serial numbers must be in lowercase.
export DPU1_SERIAL=

export DPU2_SERIAL=
```
</details>

Modify the variables in `manifests/00-env-vars/envvars.env` to fit your environment, then source the file:

```shell
source manifests/00-env-vars/envvars.env
```

### 1. DPF Operator Installation

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

A number of [environment variables](#0-required-variables) must be set before running this command.

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
  deploymentMode: zero-trust
  overrides:
    kubernetesAPIServerVIP: $TARGETCLUSTER_API_SERVER_HOST
    kubernetesAPIServerPort: $TARGETCLUSTER_API_SERVER_PORT
  dpuDetector:
    disable: true
  provisioningController:
    dmsTimeout: 900
    installInterface:
      installViaRedfish:
        skipDPUNodeDiscovery: false
  kamajiClusterManager:
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

<details markdown="1"><summary><b>DPUDiscovery to discover DPUDevices or DPUNodes</b></summary>

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
## Ensure bfb-registry pod is running.
kubectl wait --for=condition=ready --namespace dpf-operator-system pod/bfb-registry --timeout=600s
## Ensure bfb-registry service exists.
kubectl get svc bfb-registry --namespace dpf-operator-system
## Ensure the DPUCluster is ready for nodes to join.
kubectl wait --for=condition=ready --namespace dpu-cplane-tenant1 dpucluster --all
```

### 3. DPU Provisioning and Service Installation

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
kubectl apply -f manifests/03.1-dpudeployment-installation-nvme/credentials/
```

<details markdown="1"><summary><b>DPUServiceCredentialRequest for SPDK CSI Controller</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/credentials/spdk-csi-controller-dpuservicecredentialrequest.yaml)
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
  -f manifests/03.1-dpudeployment-installation-nvme/helm-values/snap-host-controller.yml
```

###### OCI Registry

For development purposes, if the $REGISTRY is an OCI Registry use this command:

```shell
helm upgrade --install -n dpf-operator-system snap-host-controller \
  $REGISTRY/dpf-storage --version=$TAG \
  --wait \
  -f manifests/03.1-dpudeployment-installation-nvme/helm-values/snap-host-controller.yml
```

<details markdown="1"><summary><b>SNAP Host Controller Helm values</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/helm-values/snap-host-controller.yml)
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

##### Install SPDK CSI Controller on the Host Cluster

Install the SPDK CSI Controller that runs on the host cluster for this scenario:

```shell
helm upgrade --install -n dpf-operator-system spdk-csi-controller \
  oci://ghcr.io/mellanox/dpf-storage-vendors-charts/spdk-csi-controller --version=v0.3.0 \
  --wait \
  -f manifests/03.1-dpudeployment-installation-nvme/helm-values/spdk-csi-controller.yml
```

<details markdown="1"><summary><b>SPDK CSI Controller Helm values</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/helm-values/spdk-csi-controller.yml)
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

> [!WARNING]
> In case more than 1 DPU exists per node, the relevant selector should be applied in the DPUDeployment
> to select the appropriate DPU. See [DPUDeployment - DPUs Configuration](../../../../developer-guides/api/dpudeployment.md#dpus-configuration)
> to understand more about the selectors.

```shell
cat manifests/03.1-dpudeployment-installation-nvme/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:
<details markdown="1"><summary><b>BFB to download Bluefield Bitstream to a shared volume</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/bfb.yaml)
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

<details markdown="1"><summary><b>HBN DPUFlavor to correctly configure the DPUs on provisioning</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/dpuflavor.yaml)
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
    - net.ifnames=0
    - biosdevname=0
    - iommu.passthrough=1
    - cgroup_no_v1=net_prio,net_cls
    - hugepagesz=2048kB
    - hugepages=2298
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
    - NVME_EMULATION_ENABLE=1
    - NVME_EMULATION_NUM_PF=1
    - PCI_SWITCH_EMULATION_ENABLE=1
    - PCI_SWITCH_EMULATION_NUM_PORT=32
    - LINK_TYPE_P1=ETH
    - LINK_TYPE_P2=ETH
  ovs:
    rawConfigScript: |
      _ovs-vsctl() {
        ovs-vsctl --timeout 15 "$@"
      }

      # Remove default OVS configuration on the DPU and ensure no leftovers on the OVS kernel side
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      ovs-appctl --timeout 15 dpctl/del-dp system@ovs-system || true

      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      _ovs-vsctl remove Open_vSwitch . other_config default-datapath-type || true

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

> [!NOTE]
> The DPUFlavor above does not set `ubuntu_PASSWORD` in `bfcfgParameters`. If omitted, DPF defaults to `ubuntu:ubuntu` credentials on the DPU. For production deployments, it is recommended to set a hashed password (see [BFCfg Parameters](../../../../developer-guides/api/dpuflavor.md#bfcfg-parameters)).

<details markdown="1"><summary><b>DPUDeployment to provision DPUs on worker nodes</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/dpudeployment.yaml)
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
      hold: true
    dpuSetStrategy:
      type: OnDelete
    dpuSets:
    - nameSuffix: "dpuset1"
      dpuAnnotations:
        storage.nvidia.com/preferred-dpu: "true"
      dpuNodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
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
    spdk-csi-controller-dpu:
      serviceTemplate: spdk-csi-controller-dpu
      serviceConfiguration: spdk-csi-controller-dpu
  serviceChains:
    switches:
      - ports:
        - serviceInterface:
            matchLabels:
              interface: p0
        - service:
            name: doca-hbn
            interface: p0_if
      - ports:
        - serviceInterface:
            matchLabels:
              interface: p1
        - service:
            name: doca-hbn
            interface: p1_if
      - ports:
        - serviceInterface:
            matchLabels:
              interface: pf0hpf
        - service:
            name: doca-hbn
            interface: pf0hpf_if
      - ports:
        - serviceInterface:
            matchLabels:
              interface: pf1hpf
        - service:
            name: doca-hbn
            interface: pf1hpf_if
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

<details markdown="1"><summary><b>DPUServiceConfig and DPUServiceTemplate to deploy HBN workloads to the DPUs</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/hbn-dpuserviceconfig.yaml)
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
          {"name": "iprequest", "interface": "ip_pf0hpf", "cni-args": {"poolNames": ["pool1"], "poolType": "cidrpool", "allocateDefaultGateway": true}},
          {"name": "iprequest", "interface": "ip_pf1hpf", "cni-args": {"poolNames": ["pool2"], "poolType": "cidrpool", "allocateDefaultGateway": true}}
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
            - hostnamePattern: "dpu-node-${DPU1_SERIAL}*"
              values:
                bgp_autonomous_system: 65101
            - hostnamePattern: "dpu-node-${DPU2_SERIAL}*"
              values:
                bgp_autonomous_system: 65201
          startupYAMLJ2: |
            - header:
                model: bluefield
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
                  p0_if,p1_if,pf0hpf_if,pf1hpf_if:
                    type: swp
                    link:
                      mtu: 9000
                  pf0hpf_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf0hpf.cidr }}: {}
                      vrf: {{ config.vrf1 }}
                  pf1hpf_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf1hpf.cidr }}: {}
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
  - name: pf0hpf_if
    network: mybrhbn
  - name: pf1hpf_if
    network: mybrhbn
  - name: snap_if
    network: mybrhbn
```

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/hbn-dpuservicetemplate.yaml)
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
      version: 3.4.0
      chart: doca-hbn
    values:
      image:
        repository: $HBN_NGC_IMAGE_URL
        tag: 3.4.0-doca3.4.0
      resources:
        memory: 6Gi
        nvidia.com/bf_sf: 5
```
</details>

<details markdown="1"><summary><b>DPUServiceInterfaces for physical ports on the DPU</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/physical-ifaces.yaml)
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
            interface: "p0"
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
            interface: "p1"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p1
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf0hpf
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "pf0hpf"
        spec:
          interfaceType: pf
          pf:
            pfID: 0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf1hpf
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "pf1hpf"
        spec:
          interfaceType: pf
          pf:
            pfID: 1
```
</details>

<details markdown="1"><summary><b>DPUServiceIPAM to set up IP Address Management on the DPUCluster</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/hbn-ipam.yaml)
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
    # These preallocations are not necessary. We specify them so that the validation commands are straightforward.
    allocations:
      dpu-node-${DPU1_SERIAL}-${DPU1_SERIAL}: 10.0.121.0/29
      dpu-node-${DPU2_SERIAL}-${DPU2_SERIAL}: 10.0.121.8/29
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
</details>

<details markdown="1"><summary><b>DPUServiceIPAM for the loopback interface in HBN</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/hbn-loopback-ipam.yaml)
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

<details markdown="1"><summary><b>DPUServiceIPAM for storage network</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/storage-ipam.yaml)
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

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/storage-dpuservicenad.yaml)
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

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/doca-snap-dpuserviceconfiguration.yaml)
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
              tag: 1.7.0-doca3.4.0
            snapRpcInitConf: |
              nvme_subsystem_create --nqn nqn.2022-10.io.nvda.nvme:0
  interfaces:
  - name: app_sf
    network: mybrsfc-storage
```

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/doca-snap-dpuservicetemplate.yaml)
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

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for Block Storage DPU Plugin</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/block-storage-dpu-plugin-dpuserviceconfiguration.yaml)
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

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/block-storage-dpu-plugin-dpuservicetemplate.yaml)
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

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for SNAP Node Driver</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/snap-node-driver-dpuserviceconfiguration.yaml)
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

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/snap-node-driver-dpuservicetemplate.yaml)
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

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for SPDK CSI Controller on DPU</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/spdk-csi-controller-dpu-dpuserviceconfiguration.yaml)
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

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/spdk-csi-controller-dpu-dpuservicetemplate.yaml)
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

<details markdown="1"><summary><b>Secret for SPDK CSI</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-nvme/spdk-csi-secret.yaml)
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
## Ensure the DPUs have the condition Initialized (this may take time)
kubectl wait --for=condition=Initialized --namespace dpf-operator-system dpu --all
```

or with `dpfctl`:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
```

##### Apply Storage Configuration

A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
cat manifests/04.1-storage-configuration-nvme/*.yaml | envsubst | kubectl apply -f -
```

This will create the following objects:

<details markdown="1"><summary><b>DPUStorageVendor for SPDK CSI</b></summary>

[embedmd]:#(manifests/04.1-storage-configuration-nvme/spdk-csi-dpustoragevendor.yaml)
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

[embedmd]:#(manifests/04.1-storage-configuration-nvme/policy-block-dpustoragepolicy.yaml)
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
  parameters:
    num_queues: "16"
```
</details>

Check that the objects are ready:

```shell
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpustoragevendors --all
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpustoragepolicies --all
```

##### Create static volume attachments

A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
cat manifests/05.1-storage-test-nvme/*.yaml | envsubst | kubectl apply -f -
```

This will create the following objects:

<details markdown="1"><summary><b>DPUVolumes for block storage</b></summary>

[embedmd]:#(manifests/05.1-storage-test-nvme/dpuvolume.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolume
metadata:
  name: test-volume-static-pf-${DPU1_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuStoragePolicyName: policy-block
  resources:
    requests:
      storage: 30Gi
  accessModes:
  - ReadWriteOnce
  volumeMode: Block
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolume
metadata:
  name: test-volume-static-pf-${DPU2_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuStoragePolicyName: policy-block
  resources:
    requests:
      storage: 30Gi
  accessModes:
  - ReadWriteOnce
  volumeMode: Block
```
</details>

<details markdown="1"><summary><b>DPUVolumeAttachments for block storage with static PFs</b></summary>

[embedmd]:#(manifests/05.1-storage-test-nvme/dpuvolumeattachment.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolumeAttachment
metadata:
  name: test-volume-attachment-static-pf-${DPU1_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuNodeName: dpu-node-${DPU1_SERIAL}
  dpuVolumeName: test-volume-static-pf-${DPU1_SERIAL}
  functionType: pf
  hotplugFunction: false
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolumeAttachment
metadata:
  name: test-volume-attachment-static-pf-${DPU2_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuNodeName: dpu-node-${DPU2_SERIAL}
  dpuVolumeName: test-volume-static-pf-${DPU2_SERIAL}
  functionType: pf
  hotplugFunction: false
```
</details>

At this stage, volumes can be created, but they will not be attached to the DPUs yet, because the DPUs are not ready.

Verify that the DPUVolume resources are in the Ready state:

```shell
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpuvolumes --all
```

> [!WARNING]
> Be sure to create DPUVolumeAttachments that are using static PFs *before* rebooting the hosts.
> If you reboot the hosts without creating these attachments, the hardware initialization process on the host can be significantly delayed, due to timeouts caused by partially initialized emulated NVMe controllers.
> Creating the attachments ahead of time ensures that SNAP services can complete initialization of the emulated NVMe controllers right after the DPUs become ready.

##### Releasing the Node Effect Hold

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

##### Making the DPUs Ready

In order to make the DPUs ready, we will need to manually power cycle the hosts. This operation should be done in the
most graceful manner by gracefully shutting down the Host and DPU, powering off the server and then powering it on to
avoid corruption. This should happen when the object gives us the signal. The described flow can be automated by the
admin depending on the infrastructure. The following verification command may need to be run multiple times to ensure the condition is met.

```shell
## Ensure the DPUs are in the Rebooting phase with rebootStatus.reason=WaitingForManualPowerCycleOrReboot
kubectl wait --for=jsonpath='{.status.rebootStatus.reason}'=WaitingForManualPowerCycleOrReboot --namespace dpf-operator-system dpu --all 
```

At this point, we have to power cycle the hosts.

>[!NOTE]
> For the SNAP Block (NVMe) scenario, you do not need to wait for the host to fully boot after a power cycle before removing the annotation below.
> On some server platforms, device initialization may be waiting for a long time until the SNAP service on the DPU becomes ready,
> so it is recommended to remove the annotation immediately after initiating the host power cycle.


```shell
kubectl annotate dpunodes -n dpf-operator-system --all provisioning.dpu.nvidia.com/dpunode-external-reboot-required-
```

After this is done, we should expect that all DPUs become Ready:

```shell
kubectl wait --for="jsonpath={.status.phase}=Ready" --namespace dpf-operator-system dpu --all
```

or with dpfctl:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
```


DPUVolumeAttachments that were created in the previous step should become Ready after some time:

```shell
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpuvolumeattachments --all
```

##### Test Block Storage

###### Test Block Storage with Static PFs

A number of [environment variables](#0-required-variables) must be set before running this command.

First we check DPUVolumes that were exposed through the static storage PFs.

On the host that is used to run this guide, get the PCI address of the DPUVolumeAttachment:

```shell
kubectl wait --for=condition=Ready dpuvolumeattachments/test-volume-attachment-static-pf-${DPU1_SERIAL} -n dpf-operator-system
kubectl wait --for=condition=Ready dpuvolumeattachments/test-volume-attachment-static-pf-${DPU2_SERIAL} -n dpf-operator-system
pci_address_1=$(printf "0000:%s" "$(kubectl get -n dpf-operator-system dpuvolumeattachments.storage.dpu.nvidia.com test-volume-attachment-static-pf-${DPU1_SERIAL} -o jsonpath='{.status.dpu.pciAddress}')")
pci_address_2=$(printf "0000:%s" "$(kubectl get -n dpf-operator-system dpuvolumeattachments.storage.dpu.nvidia.com test-volume-attachment-static-pf-${DPU2_SERIAL} -o jsonpath='{.status.dpu.pciAddress}')")
echo "Worker $DPU1_SERIAL NMMe PCI address: $pci_address_1"
echo "Worker $DPU2_SERIAL NMMe PCI address: $pci_address_2"
```

Connect to the worker nodes with DPUs and set pci address variable to point to the PCI address retrieved in the previous step.

```shell
pci_address=<set to the PCI address that you retrieved in the previous step>
```

Check the current driver for the device

```shell
ls -lah /sys/bus/pci/devices/"$pci_address"/driver
```

If the device has no driver, you may need to manually bind it.

```shell
echo nvme > /sys/bus/pci/devices/$pci_address/driver_override
echo $pci_address > /sys/bus/pci/drivers/nvme/bind
```

Find the block device name

```shell
block_dev_name=$(basename /sys/bus/pci/drivers/nvme/"$pci_address"/nvme/*/nvme*)
echo "Block device name: $block_dev_name"
```

Format the device to the filesystem or use it as a raw block device.

Example how to format the device with ext4 filesystem and perform I/O operations:

```shell
mkfs.ext4 /dev/"$block_dev_name"
mkdir -p /tmp/test-volume-static-pf
mount /dev/"$block_dev_name" /tmp/test-volume-static-pf

dd if=/dev/urandom of=/tmp/test-volume-static-pf/test.txt bs=1M count=1000 status=progress
```

```shell
umount /tmp/test-volume-static-pf
```

###### Test Block Storage with Hot-plug PFs

A number of [environment variables](#0-required-variables) must be set before running this command.

Create DPUVolume and DPUVolumeAttachment for hot-plug storage PFs :

```shell
cat manifests/05.1-storage-test-nvme/hotplug/*.yaml | envsubst | kubectl apply -f -
```

This will create the following objects:

<details markdown="1"><summary><b>DPUVolume for hot-plug storage PFs</b></summary>

[embedmd]:#(manifests/05.1-storage-test-nvme/hotplug/dpuvolume.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolume
metadata:
  name: test-volume-hotplug-pf-${DPU1_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuStoragePolicyName: policy-block
  resources:
    requests:
      storage: 30Gi
  accessModes:
  - ReadWriteOnce
  volumeMode: Block
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolume
metadata:
  name: test-volume-hotplug-pf-${DPU2_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuStoragePolicyName: policy-block
  resources:
    requests:
      storage: 30Gi
  accessModes:
  - ReadWriteOnce
  volumeMode: Block
```
</details>

<details markdown="1"><summary><b>DPUVolumeAttachment for hot-plug storage PFs</b></summary>

[embedmd]:#(manifests/05.1-storage-test-nvme/hotplug/dpuvolumeattachment.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolumeAttachment
metadata:
  name: test-volume-attachment-hotplug-pf-${DPU1_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuNodeName: dpu-node-${DPU1_SERIAL}
  dpuVolumeName: test-volume-hotplug-pf-${DPU1_SERIAL}
  functionType: pf
  hotplugFunction: true
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolumeAttachment
metadata:
  name: test-volume-attachment-hotplug-pf-${DPU2_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuNodeName: dpu-node-${DPU2_SERIAL}
  dpuVolumeName: test-volume-hotplug-pf-${DPU2_SERIAL}
  functionType: pf
  hotplugFunction: true
```
</details>


On the host that is used to run this guide, get the PCI address of the DPUVolumeAttachment:

```shell
kubectl wait --for=condition=Ready dpuvolumeattachments/test-volume-attachment-hotplug-pf-${DPU1_SERIAL} -n dpf-operator-system
kubectl wait --for=condition=Ready dpuvolumeattachments/test-volume-attachment-hotplug-pf-${DPU2_SERIAL} -n dpf-operator-system
pci_address_1=$(printf "0000:%s" "$(kubectl get -n dpf-operator-system dpuvolumeattachments.storage.dpu.nvidia.com test-volume-attachment-hotplug-pf-${DPU1_SERIAL} -o jsonpath='{.status.dpu.pciAddress}')")
pci_address_2=$(printf "0000:%s" "$(kubectl get -n dpf-operator-system dpuvolumeattachments.storage.dpu.nvidia.com test-volume-attachment-hotplug-pf-${DPU2_SERIAL} -o jsonpath='{.status.dpu.pciAddress}')")
echo "Worker $DPU1_SERIAL NMMe PCI address: $pci_address_1"
echo "Worker $DPU2_SERIAL NMMe PCI address: $pci_address_2"
```

After volumes are successfully attached repeat the steps from the [Test Block Storage with Static PFs](#test-block-storage-with-static-pfs) section.


#### SNAP VirtioFS

A number of [environment variables](#0-required-variables) must be set before running these commands.

##### Create Vendor CSI Controller Credentials

Create the credential request for the NFS CSI Controller before installing the chart:

```shell
kubectl apply -f manifests/03.2-dpudeployment-installation-virtiofs/credentials/
```

<details markdown="1"><summary><b>DPUServiceCredentialRequest for NFS CSI Controller</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/credentials/nfs-csi-controller-dpuservicecredentialrequest.yaml)
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
  -f manifests/03.2-dpudeployment-installation-virtiofs/helm-values/snap-host-controller.yml
```

###### OCI Registry

For development purposes, if the $REGISTRY is an OCI Registry use this command:

```shell
helm upgrade --install -n dpf-operator-system snap-host-controller \
  $REGISTRY/dpf-storage --version=$TAG \
  --wait \
  -f manifests/03.2-dpudeployment-installation-virtiofs/helm-values/snap-host-controller.yml
```

<details markdown="1"><summary><b>SNAP Host Controller Helm values</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/helm-values/snap-host-controller.yml)
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

##### Install NFS CSI Controller on the Host Cluster

Install the NFS CSI Controller that runs on the host cluster for this scenario:

```shell
helm upgrade --install -n dpf-operator-system nfs-csi-controller \
  oci://ghcr.io/mellanox/dpf-storage-vendors-charts/nfs-csi-controller --version=v0.2.0 \
  --wait \
  -f manifests/03.2-dpudeployment-installation-virtiofs/helm-values/nfs-csi-controller.yml
```

<details markdown="1"><summary><b>NFS CSI Controller Helm values</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/helm-values/nfs-csi-controller.yml)
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

> [!WARNING]
> In case more than 1 DPU exists per node, the relevant selector should be applied in the DPUDeployment
> to select the appropriate DPU. See [DPUDeployment - DPUs Configuration](../../../../developer-guides/api/dpudeployment.md#dpus-configuration)
> to understand more about the selectors.

```shell
cat manifests/03.2-dpudeployment-installation-virtiofs/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:
<details markdown="1"><summary><b>BFB to download Bluefield Bitstream to a shared volume</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/bfb.yaml)
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

<details markdown="1"><summary><b>DPUFlavor with VirtioFS Storage config</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/dpuflavor.yaml)
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
    - net.ifnames=0
    - biosdevname=0
    - iommu.passthrough=1
    - cgroup_no_v1=net_prio,net_cls
    - hugepagesz=2048kB
    - hugepages=2298
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
        ovs-vsctl --timeout 15 "$@"
      }

      # Remove default OVS configuration on the DPU and ensure no leftovers on the OVS kernel side
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      ovs-appctl --timeout 15 dpctl/del-dp system@ovs-system || true

      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      _ovs-vsctl remove Open_vSwitch . other_config default-datapath-type || true

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

> [!NOTE]
> The DPUFlavor above does not set `ubuntu_PASSWORD` in `bfcfgParameters`. If omitted, DPF defaults to `ubuntu:ubuntu` credentials on the DPU. For production deployments, it is recommended to set a hashed password (see [BFCfg Parameters](../../../../developer-guides/api/dpuflavor.md#bfcfg-parameters)).

<details markdown="1"><summary><b>DPUDeployment for VirtioFS Storage</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/dpudeployment.yaml)
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
      hold: true
    dpuSetStrategy:
      type: OnDelete
    dpuSets:
    - nameSuffix: "dpuset1"
      dpuAnnotations:
        storage.nvidia.com/preferred-dpu: "true"
      dpuNodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
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
    nfs-csi-controller-dpu:
      serviceTemplate: nfs-csi-controller-dpu
      serviceConfiguration: nfs-csi-controller-dpu
  serviceChains:
    switches:
      - ports:
        - serviceInterface:
            matchLabels:
              interface: p0
        - service:
            name: doca-hbn
            interface: p0_if
      - ports:
        - serviceInterface:
            matchLabels:
              interface: p1
        - service:
            name: doca-hbn
            interface: p1_if
      - ports:
        - serviceInterface:
            matchLabels:
              interface: pf0hpf
        - service:
            name: doca-hbn
            interface: pf0hpf_if
      - ports:
        - serviceInterface:
            matchLabels:
              interface: pf1hpf
        - service:
            name: doca-hbn
            interface: pf1hpf_if
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

<details markdown="1"><summary><b>DPUServiceIPAM</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/storage-ipam.yaml)
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

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for DOCA HBN</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/hbn-dpuserviceconfig.yaml)
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
          {"name": "iprequest", "interface": "ip_pf0hpf", "cni-args": {"poolNames": ["pool1"], "poolType": "cidrpool", "allocateDefaultGateway": true}},
          {"name": "iprequest", "interface": "ip_pf1hpf", "cni-args": {"poolNames": ["pool2"], "poolType": "cidrpool", "allocateDefaultGateway": true}}
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
            - hostnamePattern: "dpu-node-${DPU1_SERIAL}*"
              values:
                bgp_autonomous_system: 65101
            - hostnamePattern: "dpu-node-${DPU2_SERIAL}*"
              values:
                bgp_autonomous_system: 65201
          startupYAMLJ2: |
            - header:
                model: bluefield
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
                  p0_if,p1_if,pf0hpf_if,pf1hpf_if:
                    type: swp
                    link:
                      mtu: 9000
                  pf0hpf_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf0hpf.cidr }}: {}
                      vrf: {{ config.vrf1 }}
                  pf1hpf_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf1hpf.cidr }}: {}
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
  - name: pf0hpf_if
    network: mybrhbn
  - name: pf1hpf_if
    network: mybrhbn
  - name: snap_if
    network: mybrhbn
```

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/hbn-dpuservicetemplate.yaml)
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
      version: 3.4.0
      chart: doca-hbn
    values:
      image:
        repository: $HBN_NGC_IMAGE_URL
        tag: 3.4.0-doca3.4.0
      resources:
        memory: 6Gi
        nvidia.com/bf_sf: 5
```
</details>

<details markdown="1"><summary><b>DPUServiceNAD for storage services (no RDMA CNI chaining)</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/storage-dpuservicenad.yaml)
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

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/doca-snap-dpuserviceconfiguration.yaml)
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
              tag: 1.7.0-doca3.4.0
  interfaces:
  - name: app_sf
    network: mybrsfc-storage
```

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/doca-snap-dpuservicetemplate.yaml)
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

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/snap-node-driver-dpuserviceconfiguration.yaml)
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

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/snap-node-driver-dpuservicetemplate.yaml)
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

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/fs-storage-dpu-plugin-dpuserviceconfiguration.yaml)
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

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/fs-storage-dpu-plugin-dpuservicetemplate.yaml)
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

<details markdown="1"><summary><b>DPUServiceConfiguration and DPUServiceTemplate for NFS CSI Controller on DPU</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/nfs-csi-controller-dpu-dpuserviceconfiguration.yaml)
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

[embedmd]:#(manifests/03.2-dpudeployment-installation-virtiofs/nfs-csi-controller-dpu-dpuservicetemplate.yaml)
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
## Ensure the DPUs have the condition Initialized (this may take time)
kubectl wait --for=condition=Initialized --namespace dpf-operator-system dpu --all
```

or with `dpfctl`:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
```

##### Apply Storage Configuration

A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
cat manifests/04.2-storage-configuration-virtiofs/*.yaml | envsubst | kubectl apply -f -
```

This will create the following objects:

<details markdown="1"><summary><b>DPUStorageVendor for NFS CSI</b></summary>

[embedmd]:#(manifests/04.2-storage-configuration-virtiofs/nfs-csi-dpustoragevendor.yaml)
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

<details markdown="1"><summary><b>DPUStoragePolicy for filesystem storage</b></summary>

[embedmd]:#(manifests/04.2-storage-configuration-virtiofs/policy-fs-dpustoragepolicy.yaml)
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

Check that the objects are ready:

```shell
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpustoragevendors --all
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpustoragepolicies --all
```

##### Releasing the Node Effect Hold

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

##### Making the DPUs Ready

In order to make the DPUs ready, we will need to manually power cycle the hosts. This operation should be done in the
most graceful manner by gracefully shutting down the Host and DPU, powering off the server and then powering it on to
avoid corruption. This should happen when the object gives us the signal. The described flow can be automated by the
admin depending on the infrastructure. The following verification command may need to be run multiple times to ensure the condition is met.

```shell
## Ensure the DPUs are in the Rebooting phase with rebootStatus.reason=WaitingForManualPowerCycleOrReboot
kubectl wait --for=jsonpath='{.status.rebootStatus.reason}'=WaitingForManualPowerCycleOrReboot --namespace dpf-operator-system dpu --all
```

At this point, we have to power cycle the hosts.

```shell
kubectl annotate dpunodes -n dpf-operator-system --all provisioning.dpu.nvidia.com/dpunode-external-reboot-required-
```

After this is done, we should expect that all DPUs become Ready:

```shell
kubectl wait --for="jsonpath={.status.phase}=Ready" --namespace dpf-operator-system dpu --all
```

or with dpfctl:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
```

##### Test VirtioFS Storage

A number of [environment variables](#0-required-variables) must be set before running this command.

Create DPUVolume and DPUVolumeAttachment for VirtioFS storage:

```shell
cat manifests/05.2-storage-test-virtiofs/*.yaml | envsubst | kubectl apply -f -
```

This will create the following objects:

<details markdown="1"><summary><b>DPUVolume for VirtioFS storage</b></summary>

[embedmd]:#(manifests/05.2-storage-test-virtiofs/dpuvolume.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolume
metadata:
  name: test-volume-virtiofs-hotplug-pf-${DPU1_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuStoragePolicyName: policy-fs
  resources:
    requests:
      storage: 10Gi
  accessModes:
  - ReadWriteOnce
  volumeMode: Filesystem
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolume
metadata:
  name: test-volume-virtiofs-hotplug-pf-${DPU2_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuStoragePolicyName: policy-fs
  resources:
    requests:
      storage: 10Gi
  accessModes:
  - ReadWriteOnce
  volumeMode: Filesystem
```
</details>

<details markdown="1"><summary><b>DPUVolumeAttachment for VirtioFS storage</b></summary>

[embedmd]:#(manifests/05.2-storage-test-virtiofs/dpuvolumeattachment.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolumeAttachment
metadata:
  name: test-volume-attachment-virtiofs-hotplug-pf-${DPU1_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuNodeName: dpu-node-${DPU1_SERIAL}
  dpuVolumeName: test-volume-virtiofs-hotplug-pf-${DPU1_SERIAL}
  functionType: pf
  hotplugFunction: true
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolumeAttachment
metadata:
  name: test-volume-attachment-virtiofs-hotplug-pf-${DPU2_SERIAL}
  namespace: dpf-operator-system
spec:
  dpuNodeName: dpu-node-${DPU2_SERIAL}
  dpuVolumeName: test-volume-virtiofs-hotplug-pf-${DPU2_SERIAL}
  functionType: pf
  hotplugFunction: true
```
</details>

Verify that the DPUVolume resources are in the Ready state:

```shell
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpuvolumes --all
```

Wait for the DPUVolumeAttachments to become Ready:

```shell
kubectl wait --for=condition=Ready --namespace dpf-operator-system dpuvolumeattachments --all
```

On the host that is used to run this guide, get the VirtioFS tag name of the DPUVolumeAttachment:

```shell
kubectl wait --for=condition=Ready dpuvolumeattachments/test-volume-attachment-virtiofs-hotplug-pf-${DPU1_SERIAL} -n dpf-operator-system
kubectl wait --for=condition=Ready dpuvolumeattachments/test-volume-attachment-virtiofs-hotplug-pf-${DPU2_SERIAL} -n dpf-operator-system
tag_1=$(kubectl get -n dpf-operator-system dpuvolumeattachments.storage.dpu.nvidia.com test-volume-attachment-virtiofs-hotplug-pf-${DPU1_SERIAL} -o jsonpath='{.status.dpu.virtioFSAttrs.filesystemTag}')
tag_2=$(kubectl get -n dpf-operator-system dpuvolumeattachments.storage.dpu.nvidia.com test-volume-attachment-virtiofs-hotplug-pf-${DPU2_SERIAL} -o jsonpath='{.status.dpu.virtioFSAttrs.filesystemTag}')
echo "Worker $DPU1_SERIAL VirtioFS tag: $tag_1"
echo "Worker $DPU2_SERIAL VirtioFS tag: $tag_2"
```

Connect to the worker nodes with DPUs and set tag variable to point to the VirtioFS tag retrieved in the previous step.

```shell
tag=<set to the VirtioFS tag that you retrieved in the previous step>
```

Make sure the VirtioFS driver is loaded:

```shell
modprobe virtiofs
```

Create a directory to mount the volume:

```shell
mkdir -p /tmp/test-volume-virtiofs-hotplug-pf
```

Mount the volume:

```shell
mount -t virtiofs $tag /tmp/test-volume-virtiofs-hotplug-pf
```

Perform I/O operations to test the storage:

```shell
dd if=/dev/zero of=/tmp/test-volume-virtiofs-hotplug-pf/test.txt bs=1M count=1000 status=progress
```

Unmount the volume:

```shell
umount /tmp/test-volume-virtiofs-hotplug-pf
```

#### Test Network Traffic

Both Block and VirtioFS scenarios can be tested with the same steps.

After the DPUs are provisioned and the rest of the objects are Ready, we can test traffic by assigning an IP to the PF0
on the host for each DPU, and run a simple ping. Although the configuration is enabling both PFs, we focus on the PF0 for
testing traffic. Assuming the PF0 is named `ens5f0np0`:

On the host with DPU with serial number DPU1_SERIAL:

```shell
ip link set dev ens5f0np0 up
ip addr add 10.0.121.1/29 dev ens5f0np0
ip route add 10.0.121.0/24 dev ens5f0np0 via 10.0.121.2
```

On the host with DPU with serial number DPU2_SERIAL:

```shell
ip link set dev ens5f0np0 up
ip addr add 10.0.121.9/29 dev ens5f0np0
ip route add 10.0.121.0/24 dev ens5f0np0 via 10.0.121.10
```

On the host with DPU with serial number DPU1_SERIAL:

```shell
$ ping 10.0.121.9 -c3
PING 10.0.121.9 (10.0.121.9) 56(84) bytes of data.
64 bytes from 10.0.121.9: icmp_seq=1 ttl=64 time=0.387 ms
64 bytes from 10.0.121.9: icmp_seq=2 ttl=64 time=0.344 ms
64 bytes from 10.0.121.9: icmp_seq=3 ttl=64 time=0.396 ms

--- 10.0.121.9 ping statistics ---
3 packets transmitted, 3 received, 0% packet loss, time 2053ms
rtt min/avg/max/mdev = 0.344/0.375/0.396/0.022 ms
```

## Uninstall

This section covers only the DPF related components and not the prerequisites as these must be managed by the admin.

### Delete storage resources

>[!WARNING]
> Be sure to unmount all volumes from the worker nodes before deleting the DPUVolumeAttachments or the operation may fail.
> For NVMe attachments, it is recommended to unbind the device from the driver before deleting the DPUVolumeAttachment. `echo <pci_address> > /sys/bus/pci/drivers/nvme/unbind`

```shell
kubectl delete -n dpf-operator-system dpuvolumeattachments --all --wait
kubectl delete -n dpf-operator-system dpuvolumes --all --wait
kubectl delete -n dpf-operator-system dpustoragepolicies --all --wait
kubectl delete -n dpf-operator-system dpustoragevendors --all --wait
```

### Delete Storage Controllers from the Host Cluster

```shell
helm uninstall -n dpf-operator-system snap-host-controller --wait
# SNAP Block (NVMe) only:
helm uninstall -n dpf-operator-system spdk-csi-controller --wait
# SNAP VirtioFS only:
helm uninstall -n dpf-operator-system nfs-csi-controller --wait
```

### Delete the DPF Operator system and DPF Operator

```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

Note: there can be a race condition with deleting the underlying Kamaji cluster which runs the DPU cluster control
plane in this guide. If that happens it may be necessary to remove finalizers manually from `DPUCluster` and `Datastore`
objects.
