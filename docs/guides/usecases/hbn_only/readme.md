# Host Based Networking

In this configuration [NVIDIA Host Based Networking (HBN)](https://docs.nvidia.com/doca/sdk/nvidia+doca+hbn+service+guide/index.html) is installed as a DPUService.

<!-- toc -->
- [Prerequisites](#prerequisites)
  - [Software prerequisites](#software-prerequisites)
  - [Network prerequisites](#network-prerequisites)
  - [Kubernetes prerequisites](#kubernetes-prerequisites)
    - [Virtual functions](#virtual-functions)
- [Installation guide](#installation-guide)
  - [0. Required variables](#0-required-variables)
  - [1. DPF Operator installation](#1-dpf-operator-installation)
    - [Log in to private registries](#log-in-to-private-registries)
    - [Install cert-manager](#install-cert-manager)
    - [Install a CSI to back the DPUCluster etcd](#install-a-csi-to-back-the-dpucluster-etcd)
    - [Create secrets and storage required by the DPF Operator](#create-secrets-and-storage-required-by-the-dpf-operator)
    - [Deploy the DPF Operator](#deploy-the-dpf-operator)
    - [Verification](#verification)
  - [2. DPF system installation](#2-dpf-system-installation)
    - [Deploy the DPF System components](#deploy-the-dpf-system-components)
    - [Verification](#verification-1)
  - [3. Enable accelerated interfaces](#3-enable-accelerated-interfaces)
    - [Install Multus and SRIOV Network Operator using NVIDIA Network Operator](#install-multus-and-sriov-network-operator-using-nvidia-network-operator)
    - [Apply the NICClusterConfiguration and SriovNetworkNodePolicy](#apply-the-nicclusterconfiguration-and-sriovnetworknodepolicy)
    - [Verification](#verification-2)
  - [4. DPUService installation](#4-dpuservice-installation)
    - [Create the DPF provisioning and DPUService objects](#create-the-dpf-provisioning-and-dpuservice-objects)
    - [Verification](#verification-3)
  - [5. Deletion and clean up](#5-deletion-and-clean-up)
    - [Delete DPF CNI acceleration components](#delete-dpf-cni-acceleration-components)
    - [Delete the DPF Operator system and DPF Operator](#delete-the-dpf-operator-system-and-dpf-operator)
    - [Delete DPF Operator dependencies](#delete-dpf-operator-dependencies)
<!-- /toc -->

## Prerequisites
The system is set up as described in the [system prerequisites](../prerequisites.md).  The HBN DPUService has the additional requirements:

### Software prerequisites
This guide uses the following tools which must be installed where it is running.
- kubectl
- helm
- envsubst

### Network prerequisites
TODO: Clarify networking requirements.
- [Host networking must be set up as expected](../host-network-configuration-prerequisite.md)

### Kubernetes prerequisites
- control plane setup is complete before starting this guide
- CNI installed before starting this guide
- worker nodes are not added until indicated by this guide

#### Virtual functions
A number of virtual functions (VFs) will be created on hosts when provisioning DPUs. Certain of these VFs are marked for specific usage:
- The first VF (vf0) is used by provisioning components.
- The remaining VFs are allocated by SR-IOV Device Plugin.

## Installation guide

### 0. Required variables
The following variables are required by this guide. A sensible default is provided where it makes sense, but many will be specific to the target infrastructure.

Commands in this guide are run in the same directory that contains this readme.

```bash
## IP Address for the Kubernetes API server of the target cluster on which DPF is installed.
## This should never include a scheme or a port.
## e.g. 10.10.10.10
export TARGETCLUSTER_API_SERVER_HOST=

## Port for the Kubernetes API server of the target cluster on which DPF is installed.
export TARGETCLUSTER_API_SERVER_PORT=6443

## Virtual IP used by the load balancer for the DPU Cluster. Must be a reserved IP from the management subnet and not allocated by DHCP.
export DPUCLUSTER_VIP=

## DPU_P0 is the name of the first port of the DPU. This name must be the same on all worker nodes.
export DPU_P0=

## Interface on which the DPUCluster load balancer will listen. Should be the management interface of the control plane node.
export DPUCLUSTER_INTERFACE=

# IP address to the NFS server used as storage for the BFB.
export NFS_SERVER_IP=

# API key for accessing containers and helm charts from the NGC private repository.
# Note: This isn't technically required when using public images but is included here to demonstrate the secret flow in DPF when using images from a private registry.
export NGC_API_KEY=

## DPF_VERSION is the version of the DPF components which will be deployed in this use case guide.
export DPF_VERSION=v24.10.0-rc.5

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BLUEFIELD_BITSTREAM="https://content.mellanox.com/BlueField/BFBs/Ubuntu22.04/bf-bundle-2.9.1-30_24.11_ubuntu-22.04_prod.bfb"
```

### 1. DPF Operator installation

#### Log in to private registries
The login and secret is required when using a private registry to host images and helm charts. If using a public registry this section can be ignored.
```shell
kubectl create namespace dpf-operator-system
kubectl -n dpf-operator-system create secret docker-registry dpf-pull-secret --docker-server=nvcr.io --docker-username="\$oauthtoken" --docker-password=$NGC_API_KEY
helm registry login nvcr.io --username \$oauthtoken --password $NGC_API_KEY
```

#### Install cert-manager
Cert manager is a prerequisite which is used to provide certificates for webhooks used by DPF and its dependencies.

```shell
helm repo add jetstack https://charts.jetstack.io --force-update
helm upgrade --install --create-namespace --namespace cert-manager cert-manager jetstack/cert-manager --version v1.16.1 -f ./manifests/01-dpf-operator-installation/helm-values/cert-manager.yml
```
<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/01-dpf-operator-installation/helm-values/cert-manager.yml)
```yml
startupapicheck:
  enabled: false
crds:
  enabled: true
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
tolerations:
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/control-plane
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/master
cainjector:
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
  tolerations:
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/control-plane
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/master
webhook:
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
  tolerations:
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/control-plane
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/master
```
</details>

#### Install a CSI to back the DPUCluster etcd

In this guide the local-path-provisioner CSI from Rancher is used to back the etcd of the Kamaji based DPUCluster. This should be substituted for a reliable performant CNI to back etcd.

```shell
curl https://codeload.github.com/rancher/local-path-provisioner/tar.gz/v0.0.30 | tar -xz --strip=3 local-path-provisioner-0.0.30/deploy/chart/local-path-provisioner/
kubectl create ns local-path-provisioner
helm install -n local-path-provisioner local-path-provisioner ./local-path-provisioner --version 0.0.30 -f ./manifests/01-dpf-operator-installation/helm-values/local-path-provisioner.yml
```
<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/01-dpf-operator-installation/helm-values/local-path-provisioner.yml)
```yml
tolerations:
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/control-plane
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/master
```

</details>

#### Create secrets and storage required by the DPF Operator
A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
cat manifests/01-dpf-operator-installation/*.yaml | envsubst | kubectl apply -f - 
```

This deploys the following objects:

<details><summary>Secret for pulling images and helm charts</summary>

[embedmd]:#(manifests/01-dpf-operator-installation/helm-secret-dpf.yaml)
```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: ngc-doca-oci-helm
  namespace: dpf-operator-system
  labels:
    argocd.argoproj.io/secret-type: repository
stringData:
  name: nvstaging-doca-oci
  url: nvcr.io/nvstaging/doca
  type: helm
  ## Note `no_variable` here is used to ensure envsubst renders the correct username which is `$oauthtoken`
  username: $${no_variable}oauthtoken
  password: $NGC_API_KEY
---
apiVersion: v1
kind: Secret
metadata:
  name: ngc-doca-https-helm
  namespace: dpf-operator-system
  labels:
    argocd.argoproj.io/secret-type: repository
stringData:
  name: nvstaging-doca-https
  url: https://helm.ngc.nvidia.com/nvstaging/doca
  type: helm
  username: $${no_variable}oauthtoken
  password: $NGC_API_KEY
```
</details>

<details><summary>PersistentVolume and PersistentVolumeClaim for the provisioning controller</summary>

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
```
</details>

#### Deploy the DPF Operator

A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
envsubst < ./manifests/01-dpf-operator-installation/helm-values/dpf-operator.yml | helm upgrade --install -n dpf-operator-system dpf-operator oci://ghcr.io/nvidia/dpf-operator --version=$DPF_VERSION --values -
```

<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/01-dpf-operator-installation/helm-values/dpf-operator.yml)
```yml
imagePullSecrets:
  - name: dpf-pull-secret
kamaji-etcd:
  persistentVolumeClaim:
    storageClassName: local-path
node-feature-discovery:
  worker:
    extraEnvs:
      - name: "KUBERNETES_SERVICE_HOST"
        value: "$TARGETCLUSTER_API_SERVER_HOST"
      - name: "KUBERNETES_SERVICE_PORT"
        value: "$TARGETCLUSTER_API_SERVER_PORT"
```
</details>

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the DPF Operator installation with:
```shell
## Ensure the DPF Operator deployment is available.
kubectl rollout status deployment --namespace dpf-operator-system dpf-operator-controller-manager
## Ensure all pods in the DPF Operator system are ready.
kubectl wait --for=condition=ready --namespace dpf-operator-system pods --all
```


### 2. DPF system installation
This section involves creating the DPF system components and some basic infrastructure required for a functioning DPF-enabled cluster.

#### Deploy the DPF System components
A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
kubectl create ns dpu-cplane-tenant1
cat manifests/02-dpf-system-installation/*.yaml | envsubst | kubectl apply -f - 
```

This will create the following objects:
<details><summary>DPF Operator to install the DPF System components</summary>

[embedmd]:#(manifests/02-dpf-system-installation/operatorconfig.yaml)
```yaml
---
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  imagePullSecrets:
  - dpf-pull-secret
  provisioningController:
    bfbPVCName: "bfb-pvc"
    dmsTimeout: 900
  kamajiClusterManager:
    disable: false
```
</details>

<details><summary>DPUCluster to serve as Kubernetes control plane for DPU nodes</summary>

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
  version: v1.30.2
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

### 3. Enable accelerated interfaces

Traffic can be routed through HBN on the worker node by mounting the DPU physical interface into a pod.

#### Install Multus and SRIOV Network Operator using NVIDIA Network Operator

```shell
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia --force-update
helm upgrade --no-hooks --install --create-namespace --namespace nvidia-network-operator network-operator nvidia/network-operator --version 24.7.0 -f ./manifests/03-enable-accelerated-interfaces/helm-values/network-operator.yml
```

<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/03-enable-accelerated-interfaces/helm-values/network-operator.yml)
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

#### Apply the NICClusterConfiguration and SriovNetworkNodePolicy

```shell
cat manifests/03-enable-accelerated-interfaces/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details><summary>NICClusterPolicy for the NVIDIA Network Operator</summary>

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
      repository: ghcr.io/k8snetworkplumbingwg
      version: v3.9.3
```
</details>

<details><summary>SriovNetworkNodePolicy for the SR-IOV Network Operator</summary>

[embedmd]:#(manifests/03-enable-accelerated-interfaces/sriov_network_operator_policy.yaml)
```yaml
---
apiVersion: sriovnetwork.openshift.io/v1
kind: SriovNetworkNodePolicy
metadata:
  name: bf3-p0-vfs
  namespace: nvidia-network-operator
spec:
  mtu: 1500
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

Verify the DPF System with:
```shell
## Ensure the provisioning and DPUService controller manager deployments are available.
kubectl wait --for=condition=Ready --namespace nvidia-network-operator pods --all
## Expect the following Daemonsets to be successfully rolled out.
kubectl rollout status daemonset --namespace nvidia-network-operator kube-multus-ds sriov-network-config-daemon sriov-device-plugin 
```

### 4. DPUService installation

#### Create the DPF provisioning and DPUService objects

A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
cat manifests/04-dpuservice-installation/*.yaml | envsubst | kubectl apply -f - 
```


This will deploy the following objects:
<details><summary>BFB to download Bluefield Bitstream to a shared volume</summary>

[embedmd]:#(manifests/04-dpuservice-installation/bfb.yaml)
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
</details>

<details><summary>HBN DPUFlavor to correctly configure the DPUs on provisioning</summary>

[embedmd]:#(manifests/04-dpuservice-installation/hbn-dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: dpf-provisioning-hbn
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
  - operation: override
    path: /etc/mellanox/mlnx-ovs.conf
    permissions: "0644"
    raw: |
      CREATE_OVS_BRIDGES="no"
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
      ###### Temp workaround, should be fixed in hbn image (ovs-watcher) - Cannot work via flavor.
      #_ovs-vsctl --may-exist add-br br-hbn
      #_ovs-vsctl set bridge br-hbn datapath_type=netdev
      #_ovs-vsctl --may-exist add-port br-hbn vxlan0brhbn || true
      #_ovs-vsctl set int vxlan0brhbn type=vxlan options:remote_ip=flow ofport_request=1
      #_ovs-vsctl set int vxlan0brhbn options:explicit=true
      #_ovs-vsctl set int vxlan0brhbn options:tos=inherit
```
</details>

<details><summary>DPUSet to provision DPUs on worker nodes</summary>

[embedmd]:#(manifests/04-dpuservice-installation/dpuset.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: dpuset
  namespace: dpf-operator-system
spec:
  nodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  strategy:
    rollingUpdate:
      maxUnavailable: "10%"
    type: RollingUpdate
  dpuTemplate:
    spec:
      dpuFlavor: dpf-provisioning-hbn
      bfb:
        name: bf-bundle
      nodeEffect:
        taint:
          key: "dpu"
          value: "provisioning"
          effect: NoSchedule
      automaticNodeReboot: true
```
</details>

<details><summary>HBN DPUService to deploy HBN workloads to the DPUs</summary>

[embedmd]:#(manifests/04-dpuservice-installation/hbn-dpuservice.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: doca-hbn
  namespace: dpf-operator-system
spec:
  serviceID: doca-hbn
  interfaces:
  - p0-sf
  - p1-sf
  - host-pf0-sf
  - host-pf1-sf
  serviceDaemonSet:
    labels:
    annotations:
      k8s.v1.cni.cncf.io/networks: |-
        [
        {"name": "iprequest", "interface": "ip_lo", "cni-args": {"poolNames": ["loopback"], "poolType": "cidrpool"}},
        {"name": "iprequest", "interface": "ip_pf0hpf", "cni-args": {"poolNames": ["pool1"], "poolType": "cidrpool", "allocateDefaultGateway": true}},
        {"name": "iprequest", "interface": "ip_pf1hpf", "cni-args": {"poolNames": ["pool2"], "poolType": "cidrpool", "allocateDefaultGateway": true}}
        ]
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: 1.0.1
      chart: doca-hbn
    values:
      imagePullSecrets:
      - name: dpf-pull-secret
      image:
        repository: nvcr.io/nvidia/doca/doca_hbn
        tag: 2.4.1-doca2.9.1
      resources:
        memory: 6Gi
        nvidia.com/bf_sf: 4
      configuration:
        perDPUValuesYAML: |
          - hostnamePattern: "*"
            values:
              bgp_peer_group: hbn
              vrf1: RED
              vrf2: BLUE
              l2vni1: 10010
              l2vni2: 10020
              l3vni1: 100001
              l3vni2: 100002
          - hostnamePattern: "worker1*"
            values:
              vlan1: 11
              vlan2: 21
              bgp_autonomous_system: 65101
          - hostnamePattern: "worker2*"
            values:
              vlan1: 12
              vlan2: 22
              bgp_autonomous_system: 65201
        startupYAMLJ2: |
          - header:
              model: bluefield
              nvue-api-version: nvue_v1
              rev-id: 1.0
              version: HBN 2.4.0
          - set:
              bridge:
                domain:
                  br_default:
                    vlan:
                      {{ config.vlan1 }}:
                        vni:
                          {{ config.l2vni1 }}: {}
                      {{ config.vlan2 }}:
                        vni:
                          {{ config.l2vni2 }}: {}
              evpn:
                enable: on
                route-advertise: {}
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
                  bridge:
                    domain:
                      br_default:
                        access: {{ config.vlan1 }}
                pf1hpf_if:
                  bridge:
                    domain:
                      br_default:
                        access: {{ config.vlan2 }}
                vlan{{ config.vlan1 }}:
                  ip:
                    address:
                      {{ ipaddresses.ip_pf0hpf.cidr }}: {}
                    vrf: {{ config.vrf1 }}
                  vlan: {{ config.vlan1 }}
                vlan{{ config.vlan1 }},{{ config.vlan2 }}:
                  type: svi
                vlan{{ config.vlan2 }}:
                  ip:
                    address:
                      {{ ipaddresses.ip_pf1hpf.cidr }}: {}
                    vrf: {{ config.vrf2 }}
                  vlan: {{ config.vlan2 }}
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
                        l2vpn-evpn:
                          enable: on
                      autonomous-system: {{ config.bgp_autonomous_system }}
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
                            l2vpn-evpn:
                              enable: on
                          remote-as: external
                      router-id: {{ ipaddresses.ip_lo.ip }}
                {{ config.vrf1 }}:
                  evpn:
                    enable: on
                    vni:
                      {{ config.l3vni1 }}: {}
                  loopback:
                    ip:
                      address:
                        {{ ipaddresses.ip_lo.ip }}/32: {}
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
                      router-id: {{ ipaddresses.ip_lo.ip }}
                {{ config.vrf2 }}:
                  evpn:
                    enable: on
                    vni:
                      {{ config.l3vni2 }}: {}
                  loopback:
                    ip:
                      address:
                        {{ ipaddresses.ip_lo.ip }}/32: {}
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
                      router-id: {{ ipaddresses.ip_lo.ip }}
```
</details>

<details><summary>DPUServiceInterfaces for physical ports on the DPU</summary>

[embedmd]:#(manifests/04-dpuservice-installation/physical-ifaces.yaml)
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
  name: host-pf0-rep
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            hostpf: "host_pf0_rep"
        spec:
          interfaceType: pf
          pf:
            pfID: 0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: host-pf1-rep
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            hostpf: "host_pf1_rep"
        spec:
          interfaceType: pf
          pf:
            pfID: 1
```
</details>

<details><summary>HBN DPUServiceInterfaces to define the ports attached to HBN workloads on the DPU</summary>

[embedmd]:#(manifests/04-dpuservice-installation/hbn-ifaces.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: host-pf0-sf
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            svc.dpu.nvidia.com/interface: "host_pf0_sf"
            svc.dpu.nvidia.com/service: doca-hbn
        spec:
          interfaceType: service
          service:
            serviceID: doca-hbn
            network: mybrhbn
            ## NOTE: Interfaces inside the HBN pod must have the `_if` suffix due to a naming convention in HBN.
            interfaceName: pf0hpf_if
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: host-pf1-sf 
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            svc.dpu.nvidia.com/interface: "host_pf1_sf"
            svc.dpu.nvidia.com/service: doca-hbn
        spec:
          interfaceType: service
          service:
            serviceID: doca-hbn
            network: mybrhbn
            ## NOTE: Interfaces inside the HBN pod must have the `_if` suffix due to a naming convention in HBN.
            interfaceName: pf1hpf_if
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p0-sf
  namespace: dpf-operator-system
spec:
  template:
    spec:
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
            ## NOTE: Interfaces inside the HBN pod must have the `_if` suffix due to a naming convention in HBN.
            interfaceName: p0_if
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p1-sf
  namespace: dpf-operator-system
spec:
  template:
    spec:
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
            ## NOTE: Interfaces inside the HBN pod must have the `_if` suffix due to a naming convention in HBN.
            interfaceName: p1_if
```
</details>

<details><summary>DPUServiceFunctionChain to define the HBN ServiceFunctionChain</summary>

[embedmd]:#(manifests/04-dpuservice-installation/hbn-chain.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceChain
metadata:
  name: hbn-to-fabric
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
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
  name: host-to-hbn
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
      template:
        spec:
          switches:
            - ports:
              - serviceInterface:
                  matchLabels:
                    hostpf: "host_pf0_rep"
              - serviceInterface:
                  matchLabels:
                    svc.dpu.nvidia.com/service: doca-hbn
                    svc.dpu.nvidia.com/interface: "host_pf0_sf"
            - ports:
              - serviceInterface:
                  matchLabels:
                    hostpf: "host_pf1_rep"
              - serviceInterface:
                  matchLabels:
                    svc.dpu.nvidia.com/service: doca-hbn
                    svc.dpu.nvidia.com/interface: "host_pf1_sf"

```
</details>

<details><summary>DPUServiceIPAM to set up IP Address Management on the DPUCluster</summary>

[embedmd]:#(manifests/04-dpuservice-installation/hbn-ipam.yaml)
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
</details>

<details><summary>DPUServiceIPAM for the loopback interface in HBN</summary>

[embedmd]:#(manifests/04-dpuservice-installation/hbn-loopback-ipam.yaml)
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

Verify the DPUService installation with:
```shell
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system  dpuservices doca-hbn
## Ensure the DPUServiceIPAMs have been reconciled
kubectl wait --for=condition=DPUIPAMObjectReconciled --namespace dpf-operator-system dpuserviceipam --all
## Ensure the DPUServiceInterfaces have been reconciled
kubectl wait --for=condition=ServiceInterfaceSetReconciled --namespace dpf-operator-system dpuserviceinterface --all
## Ensure the DPUServiceChains have been reconciled
kubectl wait --for=condition=ServiceChainSetReconciled --namespace dpf-operator-system dpuservicechain --all
```


### 5. Deletion and clean up


For DPF deletion follows a specific order defined below. The OVN Kubernetes primary CNI can not be safely deleted from the cluster.

#### Delete DPF CNI acceleration components
```shell
kubectl delete -f manifests/03-enable-accelerated-interfaces --wait
helm uninstall -n nvidia-network-operator network-operator --wait
```

#### Delete the DPF Operator system and DPF Operator
```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

#### Delete DPF Operator dependencies
```shell
helm uninstall -n local-path-provisioner local-path-provisioner --wait 
kubectl delete ns local-path-provisioner --wait 
helm uninstall -n cert-manager cert-manager --wait 
kubectl -n dpf-operator-system delete secret docker-registry dpf-pull-secret --wait
kubectl delete pv bfb-pv
kubectl delete namespace dpf-operator-system dpu-cplane-tenant1 cert-manager nvidia-network-operator --wait
```

Note: there can be a race condition with deleting the underlying Kamaji cluster which runs the DPU cluster control plane in this guide. If that happens it may be necessary to remove finalizers manually from `DPUCluster` and `Datastore` objects.
