---
title: "OVN Kubernetes"
---

> [!NOTE]
> Follow this guide from the source GitHub repo at [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform)
> and moving to the `docs/public/user-guides/host-trusted/use-cases/ovnk/README.md` for better formatting of the code.

In this configuration OVN Kubernetes is offloaded to the DPU.

[[_TOC_]]

## Prerequisites
This guide should be run by cloning the repo from [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform) and moving to the `docs/public/user-guides/ovn_only` directory.

The system is set up as described in the [system prerequisites](../../prerequisites/system.md). The OVN Kubernetes deployment has these additional requirements: 

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
* Only a single DPU uplink is used with this deployment (p0).
* All worker nodes are connected to the same L2 broadcast domain (VLAN) on the high-speed network.
* Host high-speed port (Host PF0) must have DHCP enabled.
* MTU of the port should be statically set to 1500
* **Note**: These settings can be configured automatically via DPUFlavor `hostNetworkInterfaceConfigs`
* An external DHCP Server should be used for the high-speed network:
    * The DHCP server must not assign a default gateway to the DHCP clients.
    * The DHCP server should assign a special route (option 121) for a "dummy" IP subnet with a next hop address of the actual default gateway router serving the high-speed network.
    * The special route (which is configurable) is used by DPF to inject the default gateway into the overlay network. By default, DPF is looking for the subnet 169.254.99.100/32 in the special route and extracts the gateway address.
    * The gateway value sent using option 121 should be calculated according to RFC3442 (An online calculator exists). For example, the value of "20:a9:fe:63:64:0a:00:7b:fe" represents a route to 169.254.99.100/32 via 10.0.123.254.

### Kubernetes prerequisites
* CNI not installed
* kube-proxy not installed
* coreDNS should be configured to run only on control plane nodes - e.g. using NodeAffinity.
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

* The first VF (vf0) is used by provisioning components
* The second VF (vf1) is used by ovn-kubernetes
* The remaining VFs are allocated by SR-IOV Device Plugin. Each pod using OVN Kubernetes in DPU mode as its primary CNI will have one of these VFs injected at Pod creation time

## Installation Guide

### 0. Required Variables

The following variables are required by this guide. A sensible default is provided where it makes sense, but many will be specific to the target infrastructure.

Commands in this guide are run in the same directory that contains this readme.

<details markdown="1"><summary><b>Environment variables file</b></summary>

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

## IP address range for VTEPs used by OVN Kubernetes. This should align with the VTEP CIDR used in the DHCP server that
## serves the high speed fabric. In configurations where different ranges are used per rack, the value should be set to
## the superset CIDR that includes all these ranges.
## This is a CIDR in the form e.g. 10.0.120.0/22
export VTEP_CIDR=

## Virtual IP used by the load balancer for the DPU Cluster. Must be a reserved IP from the management subnet and not allocated by DHCP.
export DPUCLUSTER_VIP=

## Interface/bridge on which the DPUCluster load balancer will listen. Should be the management interface/bridge of the control plane node.
export DPUCLUSTER_INTERFACE=

## The repository URL for the NVIDIA Helm chart registry.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export HELM_REGISTRY_REPO_URL=https://helm.ngc.nvidia.com/nvidia/doca

## The repository URL for the OVN-Kubernetes Helm chart.
## Usually this is the NVIDIA GHCR repository. For development purposes, this can be set to a different repository.
export OVN_KUBERNETES_REPO_URL=oci://ghcr.io/mellanox/charts

# OVN-Kubernetes chart tag
export OVN_KUBERNETES_CHART_TAG=v26.4.0

## POD_CIDR is the CIDR used for pods in the target Kubernetes cluster.
export POD_CIDR=10.233.64.0/18

## SERVICE_CIDR is the CIDR used for services in the target Kubernetes cluster.
## This is a CIDR in the form e.g. 10.10.10.0/24
export SERVICE_CIDR=10.233.0.0/18 

## The DPF REGISTRY is the Helm repository URL where the DPF Operator Chart resides.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export REGISTRY=https://helm.ngc.nvidia.com/nvidia/doca

## The DPF TAG is the version of the DPF components which will be deployed in this guide.
export TAG=v26.4.0

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BFB_URL="https://content.mellanox.com/BlueField/BFBs/Ubuntu24.04/bf-bundle-3.4.0-92_26.04_ubuntu-24.04_64k_prod.bfb"
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
envsubst < manifests/01-cni-installation/helm-values/ovn-kubernetes.yml | helm upgrade --install -n ovn-kubernetes ovn-kubernetes ${OVN_KUBERNETES_REPO_URL}/ovn-kubernetes-chart --version ${OVN_KUBERNETES_CHART_TAG} --values -
```

<details markdown="1"><summary><b>OVN-Kubernetes Helm values</b></summary>

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
  nodeMgmtPortDpResourceName: nvidia.com/ovnk-mgmt-vf
  dpuServiceAccountNamespace: dpf-operator-system
gatewayOpts: --gateway-interface=derive-from-mgmt-port
k8sAPIServer: https://$TARGETCLUSTER_API_SERVER_HOST:$TARGETCLUSTER_API_SERVER_PORT
## Note this CIDR is followed by a trailing /24 which informs OVN Kubernetes on how to split the CIDR per node.
podNetwork: $POD_CIDR/24
serviceNetwork: $SERVICE_CIDR
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

#### Dependencies

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
<details markdown="1"><summary><b>DPFOperatorConfig to install the DPF System components</b></summary>

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
    dmsTimeout: 900
  kamajiClusterManager:
    disable: false
  nodeSRIOVDevicePluginController:
    disable: false
```
</details>

<details markdown="1"><summary><b>DPUCluster to serve as Kubernetes control plane for DPU nodes</b></summary>

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

### 4. Install Components to Enable Accelerated CNI Nodes

OVN Kubernetes will accelerate traffic by attaching a VF to each pod using the primary CNI. This VF is used to offload flows to the DPU. This section details the components needed to connect pods to the offloaded OVN Kubernetes CNI.

#### Install Multus using NVIDIA Network Operator

```shell
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia --force-update
helm upgrade --no-hooks --install --create-namespace --namespace nvidia-network-operator network-operator nvidia/network-operator --version 26.1.0 -f ./manifests/04-enable-accelerated-cni/helm-values/network-operator.yml
```

<details markdown="1"><summary><b>NVIDIA Network Operator Helm values</b></summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/helm-values/network-operator.yml)
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

#### Install the OVN Kubernetes resource injection webhook

The OVN Kubernetes resource injection webhook injected each pod scheduled to a worker node with a request for a VF and a Network Attachment Definition. This webhook is part of the same helm chart as the other components of the OVN Kubernetes CNI. Here it is installed by adjusting the existing helm installation to add the webhook component to the installation. 

```shell
envsubst < manifests/04-enable-accelerated-cni/helm-values/ovn-kubernetes.yml | helm upgrade --install -n ovn-kubernetes ovn-kubernetes-resource-injector ${OVN_KUBERNETES_REPO_URL}/ovn-kubernetes-chart --version ${OVN_KUBERNETES_CHART_TAG} --values -
```

<details markdown="1"><summary><b>OVN Kubernetes Resource Injector Helm values</b></summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/helm-values/ovn-kubernetes.yml)
```yml
ovn-kubernetes-resource-injector:
  ## Enable the ovn-kubernetes-resource-injector
  enabled: true
```
</details>

#### Apply the NICClusterPolicy

```shell
kubectl apply -f manifests/04-enable-accelerated-cni/nic_cluster_policy.yaml
```

This will deploy the following object:

<details markdown="1"><summary><b>NICClusterPolicy for the NVIDIA Network Operator</b></summary>

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
      repository: nvcr.io/nvidia/mellanox
      version: network-operator-v26.1.0
```
</details>

#### Apply the NodeSRIOVDevicePluginConfig

The NodeSRIOVDevicePluginConfig defines which VFs on the DPU physical functions are exposed as SR-IOV device plugin resources on the host node. The DPF Operator's NodeSRIOVDevicePluginController (enabled in the DPFOperatorConfig) manages the SR-IOV device plugin pods based on this configuration.

```shell
kubectl apply -f manifests/04-enable-accelerated-cni/nodesriovdevicepluginconfig.yaml
```

<details markdown="1"><summary><b>NodeSRIOVDevicePluginConfig for VFs on PF0</b></summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/nodesriovdevicepluginconfig.yaml)
```yaml
---
apiVersion: noderesources.dpu.nvidia.com/v1alpha1
kind: NodeSRIOVDevicePluginConfig
metadata:
  name: bf3-p0-vfs
  namespace: dpf-operator-system
spec:
  devicePluginResources:
    - name: ovnk-mgmt-vf
      type: vf
      ranges:
        - pfIndex: 0
          start: 1
          end: 1
    - name: bf3-p0-vfs
      type: vf
      options:
        isRdma: true
      ranges:
        - pfIndex: 0
          start: 2
          end: 45
```
</details>

The `NodeSRIOVDevicePluginConfig` is linked to DPUs via the `noderesources.dpu.nvidia.com/nodesriovdevicepluginconfig` annotation on the DPU object. This annotation is set in the DPUDeployment's `dpuAnnotations` field.

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify that the accelerated CNI is enabled with:

```shell
## Ensure all pods in the nvidia-network-operator namespace are ready.
kubectl wait --for=condition=Ready --namespace nvidia-network-operator pods --all
## Expect the Multus Daemonset to be successfully rolled out.
kubectl rollout status daemonset --namespace nvidia-network-operator kube-multus-ds
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

<details markdown="1"><summary><b>BFB to download Bluefield Bitstream to a shared volume</b></summary>

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

<details markdown="1"><summary><b>OVN DPUFlavor to correctly configure the DPUs on provisioning</b></summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuflavor-ovn.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: ovnk-$TAG
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
      - hugepages=3072
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
      _ovs-vsctl set Open_vSwitch . other_config:doca-congestion-threshold=60
      _ovs-vsctl set Open_vSwitch . other_config:flow-limit=500000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload-ct-unidir-udp-enabled=true
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

      # Activate DOCA for OVNK
      _ovs-vsctl set Open_vSwitch . external-ids:ovn-bridge-datapath-type=netdev
      # setup ovnkube managed bridge, br-dpu (this corresponds to br-ex on ovnk docs)
      _ovs-vsctl --may-exist add-br br-dpu
      _ovs-vsctl br-set-external-id br-dpu bridge-id br-dpu
      _ovs-vsctl br-set-external-id br-dpu bridge-uplink pbrdputobrovn
      _ovs-vsctl set bridge br-dpu datapath_type=netdev
      _ovs-vsctl --may-exist add-port br-dpu pf0hpf
      _ovs-vsctl set Interface pf0hpf mtu_request=9216
      _ovs-vsctl set Interface pf0hpf type=dpdk

      # Create OVS bridge (br-ovn) in between the SC managed bridge and OVNK
      _ovs-vsctl --may-exist add-br br-ovn
      _ovs-vsctl set bridge br-ovn datapath_type=netdev
      _ovs-vsctl --may-exist add-port br-ovn pbrovntobrdpu
      _ovs-vsctl --may-exist add-port br-dpu pbrdputobrovn

      # Patch br-ovn and br-dpu together
      _ovs-vsctl set Interface pbrovntobrdpu type=patch options:peer=pbrdputobrovn
      _ovs-vsctl set Interface pbrdputobrovn type=patch options:peer=pbrovntobrdpu

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

<details markdown="1"><summary><b>DPUDeployment to provision DPUs on worker nodes</b></summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: ovn
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle-$TAG
    flavor: ovnk-$TAG
    nodeEffect:
      drain: true
    dpuSets:
    - nameSuffix: "dpuset1"
      dpuNodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
      dpuAnnotations:
        noderesources.dpu.nvidia.com/nodesriovdevicepluginconfig: bf3-p0-vfs
    dpuSetStrategy:
      type: RollingUpdate
  services:
    ovn:
      serviceTemplate: ovn
      serviceConfiguration: ovn
    dts:
      serviceTemplate: dts
      serviceConfiguration: dts
    blueman:
      serviceTemplate: blueman
      serviceConfiguration: blueman
  serviceChains:
    switches:
      - ports:
        - serviceInterface:
            matchLabels:
              uplink: p0
        - serviceInterface:
            matchLabels:
              port: ovn
```
</details>

<details markdown="1"><summary><b>OVN DPUServiceConfig and DPUServiceTemplate to deploy OVN workloads to the DPUs</b></summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfig_ovn.yaml)
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
          vtepCIDR: $VTEP_CIDR
          hostCIDR: $TARGETCLUSTER_NODE_CIDR
          externalDHCP: true
          gatewayDiscoveryNetwork: "169.254.99.100/32" # This is a "dummy" subnet used to get the default gateway address from DHCP server (via option 121)
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
      version: $OVN_KUBERNETES_CHART_TAG
    values:
      commonManifests:
        enabled: true
      dpuManifests:
        enabled: true
      leaseNamespace: "ovn-kubernetes"
      gatewayOpts: "--gateway-interface=br-dpu"
```
</details>

<details markdown="1"><summary><b>DOCA Telemetry Service DPUServiceConfig and DPUServiceTemplate to deploy DTS to the DPUs</b></summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfig_dts.yaml)
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
      version: 1.25.5
      chart: doca-telemetry
```
</details>

<details markdown="1"><summary><b>Blueman DPUServiceConfig and DPUServiceTemplate to deploy Blueman to the DPUs</b></summary>

[embedmd]:#(manifests/05-dpudeployment-installation/dpuserviceconfig_blueman.yaml)
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

<details markdown="1"><summary><b>OVN DPUServiceCredentialRequest to allow cross cluster communication</b></summary>

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

<details markdown="1"><summary><b>DPUServiceInterfaces for physical ports on the DPU</b></summary>

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
```
</details>

<details markdown="1"><summary><b>OVN DPUServiceInterface to define the ports attached to OVN workloads on the DPU</b></summary>

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
          interfaceType: patch
          patch:
            peerBridge: br-ovn
```
</details>

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Note that the DPUService name will have a random suffix. For example, `ovn-ovn-l2xsl`.

Verify the DPU and Service installation with:

```shell
## Ensure the BFB is ready
kubectl wait --for=jsonpath='{.status.phase}'=Ready --namespace dpf-operator-system bfb bf-bundle-$TAG --timeout=600s
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system dpuservices -l svc.dpu.nvidia.com/owned-by-dpudeployment=dpf-operator-system_ovn
## Ensure the DPUServiceIPAMs have been reconciled
kubectl wait --for=condition=DPUIPAMObjectReconciled --namespace dpf-operator-system dpuserviceipam --all
## Ensure the DPUServiceInterfaces have been reconciled
kubectl wait --for=condition=ServiceInterfaceSetReconciled --namespace dpf-operator-system dpuserviceinterface --all
## Ensure the DPUServiceChains have been reconciled
kubectl wait --for=condition=ServiceChainSetReconciled --namespace dpf-operator-system dpuservicechain --all
```

### 6. Test Traffic

#### Add worker nodes to the cluster

At this point workers should be added to the cluster. Each worker node should be configured in line with [the prerequisites](../../prerequisites/README.md).
As workers are added to the cluster DPUs will be provisioned and DPUServices will begin to be spun up.

You can verify the status of the DPUDeployment and its components with the following command:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
NAME                                        NAMESPACE            STATUS        REASON        SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig         dpf-operator-system  Ready: True   Success       2h
└─DPUDeployments
  └─DPUDeployment/ovn                       dpf-operator-system  Ready: True   Success       2h     
    ├─DPUServiceChains
    │ └─DPUServiceChain/ovn-wkdhz           dpf-operator-system  Ready: True   Success       2h
    ├─DPUSets
    │ └─DPUSet/hbn-only-dpuset1             dpf-operator-system
    │   ├─BFB/bf-bundle                     dpf-operator-system    Ready: True   Ready       2h     File: bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb, DOCA: 3.2.1
    │   └─DPUs
    │     └─2 DPUs...                       dpf-operator-system    Ready: True   DPUReady    2h     See dpu-node-mt2310xz03lr-mt2310xz03lr, dpu-node-mt2310xz03m2-mt2310xz03m2
    └─Services
      ├─DPUServiceTemplates
      │ └─4 DPUServiceTemplates...          dpf-operator-system    Ready: True   Success     2h     See ovn-blueman, ovn-dts, ovn-ovn
      └─DPUServices
        └─4 DPUServices...                  dpf-operator-system    Ready: True   Success     2h     See ovn-blueman-4tlmp, ovn-dts-49282, ovn-ovn-l2xsl
```

#### Deploy test pods 

```shell
kubectl apply -f manifests/06-test-traffic
```

OVN functionality can be tested by pinging between the pods and services deployed in the default namespace.

## Uninstall

This section describes how to clean up the cluster after the DPF setup is no longer needed.
It is important to follow the order of deletion to ensure that all components are
removed cleanly and that the cluster remains functional.

### Delete the test pods

```shell
kubectl delete -f manifests/06-test-traffic --wait
```

### Delete DPF CNI acceleration components

```shell
kubectl delete -f manifests/04-enable-accelerated-cni --wait
helm uninstall -n nvidia-network-operator network-operator --wait

## Note: Uninstalling OVN Kubernetes as primary CNI is not supported but this command must be run to remove the webhook and restore a functioning cluster.
helm uninstall -n ovn-kubernetes ovn-kubernetes-resource-injector --wait
```

### Delete the DPF Operator system and DPF Operator

First we have to delete some DPUServiceInterfaces. This is necessary because of a known issue during uninstallation.

```shell
kubectl delete -n dpf-operator-system dpuserviceinterface p0 ovn --wait
```

Then we can delete the config and system namespace.

```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

Note: there can be a race condition with deleting the underlying Kamaji cluster which runs the DPU cluster control plane in this guide. If that happens it may be necessary to remove finalizers manually from `DPUCluster` and `Datastore` objects.

## Limitations of DPF Setup

### Host network pod services

The Kubelet process on the Kubernetes nodes use the OOB interface IP address to register in Kubernetes. This means that the nodes have the OOB IP addresses as node IP addresses. This means that pods using host networking have the OOB IP address of the hosts as pod IP address. However, that interface is not accelerated. This means that any component using the addresses of the pods using host networking will not benefit from hardware acceleration and high-speed ports.

For example, this means that when creating a Kubernetes NodePort service selecting pods using host networking, even if the user uses the high-speed IP of the host, the traffic will not be accelerated. In order to solve this, it is possible to create dedicated endpointSlices that contain the host high-speed port IP addresses instead of OOB port IP addresses. This way, the entire path to the pods will be accelerated and benefit from high performances, if the user uses the high speed IP address of the host with the nodePort port. This requires the workload running on the pod with host networking to also listen on the high-speed port IP address.
