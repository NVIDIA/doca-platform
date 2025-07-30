---
title: "OVN Kubernetes"
---

> Note:
> Follow this guide from the source GitHub repo at [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform) and moving to the `docs/public/user-guides/ovn_only/README.md` for better formatting of the code.

In this configuration OVN Kubernetes is offloaded to the DPU.

> [!WARNING]  
>
> Due to several known issues regarding the stability of this specific deployment 
> use-case (OVN Kubernetes), it should be considered a proof-of-concept in this release.  
>
> Please use it at your own risk!

[TOC]

## Prerequisites
This guide should be run by cloning the repo from [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform) and moving to the `docs/public/user-guides/ovn_only` directory.

The system is set up as described in the [system prerequisites](../prerequisites/prerequisites.md). The OVN Kubernetes deployment has these additional requirements: 

### Software prerequisites
This guide uses the following tools which must be installed on the machine where the commands contained in this guide run.

* kubectl
* helm
* envsubst

### Network prerequisites

#### Control plane Nodes

* Open vSwitch (OVS) packages installed - i.e. `openvswitch-switch` for Ubuntu 24.04
* out-of-band management port should be configured as OVS bridge port with "bridge-uplink" OVS metadata [This addresses a known issue](../../release-notes/v25.1.0.md#known-issues-and-limitations).
* DNS stub resolver should be disabled if using systemd resolvd

#### Worker Nodes

* Open vSwitch (OVS) packages not installed
* [Host networking must be set up as expected](../prerequisites/host-network-configuration-prerequisite.md)
* Only a single DPU uplink is used with this deployment (p0).
* All worker nodes are connected to the same L2 broadcast domain (VLAN) on the high-speed network.
* Host high-speed port (Host PF0) must have DHCP enabled.
    * MTU of the port should be statically set to 1500
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

#### Control plane Nodes

* Have the labels:
    * `"k8s.ovn.org/zone-name": $KUBERNETES_NODE_NAME`

#### Worker Nodes

* Have the labels:
    * `"k8s.ovn.org/dpu-host": ""`
    * `"k8s.ovn.org/zone-name": $KUBERNETES_NODE_NAME`
* Have the annotations:
    * `"k8s.ovn.org/remote-zone-migrated": $KUBERNETES_NODE_NAME`

#### Virtual functions

A number of virtual functions (VFs) will be created on hosts when provisioning DPUs. Certain of these VFs are marked for specific usage:

* The first VF (vf0) is used by provisioning components.
* The second VF (vf1) is used by ovn-kubernetes.
* The remaining VFs are allocated by SR-IOV Device Plugin. Each pod using OVN Kubernetes in DPU mode as its primary CNI will have one of these VFs injected at Pod creation time. 

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

## DPU_P0 is the name of the first port of the DPU. This name must be the same on all worker nodes.
export DPU_P0=

## DPU_P0_VF1 is the name of the second Virtual Function (VF) of the first port of the DPU. This name must be the same on all worker nodes.
## Note: The VF will be created after the DPU is provisioned and the phase "Host Network Configuration" is completed.
export DPU_P0_VF1=

## Interface/bridge on which the DPUCluster load balancer will listen. Should be the management interface/bridge of the control plane node.
export DPUCLUSTER_INTERFACE=

## IP address to the NFS server used as storage for the BFB.
export NFS_SERVER_IP=

## The repository URL for the NVIDIA Helm chart registry.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export HELM_REGISTRY_REPO_URL=https://helm.ngc.nvidia.com/nvidia/doca

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
export TAG=v25.4.0

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BLUEFIELD_BITSTREAM="https://content.mellanox.com/BlueField/BFBs/Ubuntu22.04/bf-bundle-3.0.0-135_25.04_ubuntu-22.04_prod.bfb"
```

### 1. CNI installation

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

### 2. DPF Operator installation

#### Install a CSI to back the DPUCluster etcd

In this guide the local-path-provisioner CSI from Rancher is used to back the etcd of the Kamaji based DPUCluster. This should be substituted for a reliable performant CNI to back etcd.

```shell
curl https://codeload.github.com/rancher/local-path-provisioner/tar.gz/v0.0.30 | tar -xz --strip=3 local-path-provisioner-0.0.30/deploy/chart/local-path-provisioner/
kubectl create ns local-path-provisioner
helm upgrade --install -n local-path-provisioner local-path-provisioner ./local-path-provisioner --version 0.0.30 -f ./manifests/02-dpf-operator-installation/helm-values/local-path-provisioner.yml
```
<details markdown="1"><summary>Local Path Provisioner Helm values</summary>

[embedmd]:#(manifests/02-dpf-operator-installation/helm-values/local-path-provisioner.yml)
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

#### Create storage required by the DPF Operator
A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
kubectl create namespace dpf-operator-system
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

Before deploying the DPF Operator, ensure that Helm is properly configured according to the [Helm prerequisites](../prerequisites/helm-prerequisites.md).  

> [!WARNING]
>
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

If the $REGISTRY is an HTTP Registry (default value) use this command:
```shell
helm repo add --force-update dpf-repository ${REGISTRY}
helm repo update
helm upgrade --install -n dpf-operator-system dpf-operator dpf-repository/dpf-operator --version=$TAG
```

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

### 3. DPF System installation
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

### 4. Install components to enable accelerated CNI nodes

OVN Kubernetes will accelerate traffic by attaching a VF to each pod using the primary CNI. This VF is used to offload flows to the DPU. This section details the components needed to connect pods to the offloaded OVN Kubernetes CNI.

#### Install Multus and SRIOV Network Operator using NVIDIA Network Operator

```shell
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia --force-update
helm upgrade --no-hooks --install --create-namespace --namespace nvidia-network-operator network-operator nvidia/network-operator --version 24.7.0 -f ./manifests/04-enable-accelerated-cni/helm-values/network-operator.yml
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

> If you want to learn more about `DPUDeployments`, feel free to check the [DPUDeployment documentation](../../api/dpudeployment.md).

#### Create the DPUDeployment, DPUServiceConfig, DPUServiceTemplate and other necessary objects

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
  name: bf-bundle
  namespace: dpf-operator-system
spec:
  url: $BLUEFIELD_BITSTREAM
```
</details>

<details markdown="1"><summary>DPUDeployment to provision DPUs on worker nodes</summary>

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
    bfb: bf-bundle
    flavor: dpf-provisioning-hbn-ovn
    dpuSets:
    - nameSuffix: "dpuset1"
      nodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
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

<details markdown="1"><summary>OVN DPUServiceConfig and DPUServiceTemplate to deploy OVN workloads to the DPUs</summary>

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

<details markdown="1"><summary>DOCA Telemetry Service DPUServiceConfig and DPUServiceTemplate to deploy DTS to the DPUs</summary>

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
      version: 1.0.7
      chart: doca-telemetry
```
</details>

<details markdown="1"><summary>Blueman DPUServiceConfig and DPUServiceTemplate to deploy Blueman to the DPUs</summary>

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

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Note that the DPUService name will have a random suffix. For example, `ovn-ovn-l2xsl`.

Verify the DPU and Service installation with:
```shell
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system dpuservices -l svc.dpu.nvidia.com/owned-by-dpudeployment=dpf-operator-system_ovn
## Ensure the DPUServiceIPAMs have been reconciled
kubectl wait --for=condition=DPUIPAMObjectReconciled --namespace dpf-operator-system dpuserviceipam --all
## Ensure the DPUServiceInterfaces have been reconciled
kubectl wait --for=condition=ServiceInterfaceSetReconciled --namespace dpf-operator-system dpuserviceinterface --all
## Ensure the DPUServiceChains have been reconciled
kubectl wait --for=condition=ServiceChainSetReconciled --namespace dpf-operator-system dpuservicechain --all
```

or with `dpfctl`:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
NAME                                        NAMESPACE            STATUS        REASON        SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig         dpf-operator-system  Ready: True   Success       2h
└─DPUDeployments
  └─DPUDeployment/ovn                       dpf-operator-system  Ready: True   Success       2h     
    ├─DPUServiceChains
    │ └─DPUServiceChain/ovn-wkdhz           dpf-operator-system  Ready: True   Success       2h
    ├─DPUServices  
    │ │ DPUService/ovn-blueman-4tlmp        dpf-operator-system  Ready: True   Success       2h 
    │ │ DPUService/ovn-dts-49282            dpf-operator-system  Ready: True   Success       2h 
    │ └─DPUService/ovn-ovn-l2xsl            dpf-operator-system  Ready: True   Success       2h     
    └─DPUSets
      └─DPUSet/hbn-only-dpuset1             dpf-operator-system
        ├─BFB/bf-bundle                     dpf-operator-system
        ├─DPU/c-234-181-120-125-0000-08-00  dpf-operator-system  Ready: True   DPUNodeReady  2h
        └─DPU/c-234-181-120-126-0000-08-00  dpf-operator-system  Ready: True   DPUNodeReady  2h
```

### 6. Test traffic

#### Add worker nodes to the cluster

At this point workers should be added to the cluster. Each worker node should be
configured in line with [the prerequisites](../prerequisites/prerequisites.md) and the specific [OVN Kubernetes prerequisites](#worker-nodes).

As workers are added to the cluster DPUs will be provisioned and DPUServices will begin to be spun up.

#### Deploy test pods 

```shell
kubectl apply -f manifests/06-test-traffic
```

OVN functionality can be tested by pinging between the pods and services deployed in the default namespace.

TODO: Add specific user commands to test traffic.

### 7. Deletion and clean up

For DPF deletion follows a specific order defined below. The OVN Kubernetes primary CNI can not be safely deleted from the cluster.

#### Delete the test pods
```shell
kubectl delete -f manifests/06-test-traffic --wait
```

#### Delete DPF CNI acceleration components
```shell
kubectl delete -f manifests/04-enable-accelerated-cni --wait
helm uninstall -n nvidia-network-operator network-operator --wait

## Note: Uninstalling OVN Kubernetes as primary CNI is not supported but this command must be run to remove the webhook and restore a functioning cluster.
helm uninstall -n ovn-kubernetes ovn-kubernetes-resource-injector --wait
```

#### Delete the DPF Operator system and DPF Operator

First we have to delete some DPUServiceInterfaces. This is necessary because of a known issue during uninstallation.

```shell
kubectl delete -n dpf-operator-system dpuserviceinterface p0 ovn --wait
```

Then we can delete the config and system namespace.

```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

#### Delete DPF Operator dependencies
```shell
helm uninstall -n local-path-provisioner local-path-provisioner --wait 
kubectl delete ns local-path-provisioner --wait 
helm uninstall -n cert-manager cert-manager --wait 
kubectl -n dpf-operator-system delete pvc bfb-pvc
kubectl delete pv bfb-pv
kubectl delete namespace dpf-operator-system dpu-cplane-tenant1 cert-manager nvidia-network-operator --wait
```

Note: there can be a race condition with deleting the underlying Kamaji cluster which runs the DPU cluster control plane in this guide. If that happens it may be necessary to remove finalizers manually from `DPUCluster` and `Datastore` objects.

## Limitations of DPF Setup

### Host network pod services

The Kubelet process on the Kubernetes nodes use the OOB interface IP address to register in Kubernetes. This means that the nodes have the OOB IP addresses as node IP addresses. This means that pods using host networking have the OOB IP address of the hosts as pod IP address. However, that interface is not accelerated. This means that any component using the addresses of the pods using host networking will not benefit from hardware acceleration and high-speed ports.

For example, this means that when creating a Kubernetes NodePort service selecting pods using host networking, even if the user uses the high-speed IP of the host, the traffic will not be accelerated. In order to solve this, it is possible to create dedicated endpointSlices that contain the host high-speed port IP addresses instead of OOB port IP addresses. This way, the entire path to the pods will be accelerated and benefit from high performances, if the user uses the high speed IP address of the host with the nodePort port. This requires the workload running on the pod with host networking to also listen on the high-speed port IP address.
