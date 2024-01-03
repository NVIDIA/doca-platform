# OVN Kubernetes with Host Based Networking

In this configuration OVN Kubernetes is offloaded to the DPU and combined with [NVIDIA Host Based Networking (HBN)](https://docs.nvidia.com/doca/sdk/nvidia+doca+hbn+service+guide/index.html).

## Prerequisites
The system is set up as described in the [system prerequisites](../../prerequisites.md).  The OVN Kubernetes with HBN use case has the additional requirements:

### Software prerequisites
This guide uses the following tools which must be installed where it is running.
- kubectl
- helm
- envsubst

### Network prerequisites
TODO: Clarify networking requirements.
For each worker node: DHCP allocation must be used for workers
- https://gitlab-master.nvidia.com/doca-platform-foundation/doca-platform-foundation/-/blob/main/docs/provisioning/host-network-configuration-prerequisite.md#integrating-to-cloud-init

### Kubernetes prerequisites
- CNI not installed
- kube-proxy not installed
- control plane setup is complete before starting this guide
- worker nodes are not added until indicated by this guide

#### Control plane Nodes
- Have the labels:
  - `"k8s.ovn.org/zone-name": $KUBERNETES_NODE_NAME`

#### Worker Nodes
- Have the labels:
  - `"k8s.ovn.org/dpu-host": ""`
  - `"k8s.ovn.org/zone-name": $KUBERNETES_NODE_NAME`
- Have the annotations:
  - `"k8s.ovn.org/remote-zone-migrated": $KUBERNETES_NODE_NAME`

#### Virtual functions
A number of virtual functions (VFs) will be created on hosts when provisioning DPUs. Certain of these VFs are marked for specific usage:
- The first VF (vf0) is used by provisioning components.
- The second VF (vf1) is used by ovn-kubernetes.
- The remaining VFs are allocated by SR-IOV Device Plugin. Each pod using OVN Kubernetes in DPU mode as its primary CNI will have one of these VFs injected at Pod creation time. 

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

## Virtual IP used by the load balancer for the DPU Cluster. Must be a reserved IP from the management subnet and not allocated by DHCP.
export DPUCLUSTER_VIP=

## DPU_P0 is the name of the first port of the DPU. This name must be the same on all worker nodes.
export DPU_P0=

## DPU_P0_VF1 is the name of the second Virtual Function (VF) of the first port of the DPU. This name must be the same on all worker nodes.
export DPU_P0_VF1=

## Interface on which the DPUCluster load balancer will listen. Should be the management interface of the control plane node.
export DPUCLUSTER_INTERFACE=

# IP address to the NFS server used as storage for the BFB.
export NFS_SERVER_IP=

# API key for accessing containers and helm charts from the NGC private repository.
export NGC_API_KEY=

## POD_CIDR is the CIDR used for pods in the target Kubernetes cluster.
export POD_CIDR=10.233.64.0/18

## SERVICE_CIDR is the CIDR used for services in the target Kubernetes cluster.
## This is a CIDR in the form e.g. 10.10.10.0/24
export SERVICE_CIDR=10.233.0.0/18 

## DPF_VERSION is the version of the DPF components which will be deployed in this use case guide.
export DPF_VERSION=v24.10.0-rc.4

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BLUEFIELD_BITSTREAM="http://nbu-nfs.mellanox.com/auto/sw_mc_soc_release/doca_dpu/doca_2.9.0/20241103.1/bfbs/pk/bf-bundle-2.9.0-80_24.10_ubuntu-22.04_prod.bfb"

```

### 1. CNI installation

OVN Kubernetes is used as the primary CNI for the cluster. On worker nodes the primary CNI will be accelerated by offloading work to the DPU. On control plane nodes OVN Kubernetes will run without offloading.

#### Create the Namespace

```shell
kubectl create ns ovn-kubernetes
```

#### Create image pull secret

The image pull secret is required when using a private registry to host images and helm charts. If using a public registry this section can be ignored.
```shell
kubectl -n ovn-kubernetes create secret docker-registry dpf-pull-secret --docker-server=nvcr.io --docker-username="\$oauthtoken" --docker-password=$NGC_API_KEY
````

#### Install OVN Kubernetes from the helm chart

Install the OVN Kubernetes CNI components from the helm chart. A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
envsubst < manifests/01-cni-installation/helm-values/ovn-kubernetes.yml | helm upgrade --install -n ovn-kubernetes ovn-kubernetes oci://nvcr.io/nvstaging/doca/ovn-kubernetes-chart --version $DPF_VERSION --values -
```

<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/01-cni-installation/helm-values/ovn-kubernetes.yml)
```yml
tags:
  ovn-kubernetes-resource-injector: false
global:
  imagePullSecretName: "dpf-pull-secret"
k8sAPIServer: https://$TARGETCLUSTER_API_SERVER_HOST:$TARGETCLUSTER_API_SERVER_PORT
ovnkube-node-dpu-host:
  nodeMgmtPortNetdev: $DPU_P0_VF1
  gatewayOpts: --gateway-interface=$DPU_P0
## Note this CIDR is followed by a trailing /24 which informs OVN Kubernetes on how to split the CIDR per node.
podNetwork: $POD_CIDR/24
serviceNetwork: $SERVICE_CIDR
ovn-kubernetes-resource-injector:
  resourceName: nvidia.com/bf3-p0-vfs
dpuServiceAccountNamespace: dpf-operator-system
```
</details>

#### Verification

Verify the CNI installation with:
```shell
## Ensure all pods in the ovn-kubernetes namespace are ready.
kubectl wait --for=condition=ready --namespace ovn-kubernetes pods --all
## Ensure all nodes in the cluster are ready.
kubectl wait --for=condition=ready nodes --all
```

### 2. DPF Operator installation

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
helm upgrade --install --create-namespace --namespace cert-manager cert-manager jetstack/cert-manager --version v1.16.1 -f ./manifests/02-dpf-operator-installation/helm-values/cert-manager.yml
```
<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/02-dpf-operator-installation/helm-values/cert-manager.yml)
```yml
startupapicheck:
  enabled: false
crds:
  enabled: true
tolerations:
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/control-plane
  - operator: Exists
    effect: NoSchedule
    key: node-role.kubernetes.io/master
cainjector:
  tolerations:
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/control-plane
    - operator: Exists
      effect: NoSchedule
      key: node-role.kubernetes.io/master
webhook:
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

```shell
curl https://codeload.github.com/rancher/local-path-provisioner/tar.gz/v0.0.30 | tar -xz --strip=3 local-path-provisioner-0.0.30/deploy/chart/local-path-provisioner/
kubectl create ns local-path-provisioner
helm install -n local-path-provisioner local-path-provisioner ./local-path-provisioner --version 0.0.30 -f ./manifests/02-dpf-operator-installation/helm-values/local-path-provisioner.yml
```
<details><summary>Expand for detailed helm values</summary>

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

#### Create secrets and storage required by the DPF Operator
A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
cat manifests/02-dpf-operator-installation/*.yaml | envsubst | kubectl apply -f - 
```

This deploys the following objects:

<details><summary>Secret for pulling images and helm charts</summary>

[embedmd]:#(manifests/02-dpf-operator-installation/helm-secret-dpf.yaml)
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
  persistentVolumeReclaimPolicy: Retain
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
envsubst < ./manifests/02-dpf-operator-installation/helm-values/dpf-operator.yml | helm upgrade --install -n dpf-operator-system dpf-operator oci://nvcr.io/nvstaging/doca/dpf-operator --version=$DPF_VERSION --values -
```

<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/02-dpf-operator-installation/helm-values/dpf-operator.yml)
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

Verify the DPF Operator installation with:
```shell
## Ensure the DPF Operator deployment is available.
kubectl wait --for=condition=Available --namespace dpf-operator-system deployment dpf-operator-controller-manager
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
<details><summary>DPF Operator to install the DPF System components</summary>

[embedmd]:#(manifests/03-dpf-system-installation/operatorconfig.yaml)
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

<details><summary>BFB to download Bluefield Bitstream to a shared volume</summary>

[embedmd]:#(manifests/03-dpf-system-installation/bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle
  namespace: dpf-operator-system
spec:
  fileName: "bf-bundle-2.9.0-80.bfb"
  url: $BLUEFIELD_BITSTREAM
```
</details>
<details><summary>DPUCluster to serve as Kubernetes control plane for DPU nodes</summary>

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
Verify the DPF System with:
```shell
## Ensure the provisioning and DPUService controller manager deployments are available.
kubectl wait --for=condition=Available --namespace dpf-operator-system deployment dpf-provisioning-controller-manager dpuservice-controller-manager
## Ensure all pods in the DPF Operator system are ready.
kubectl wait --for=condition=ready --namespace dpf-operator-system pods --all
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

<details><summary>Expand for detailed helm values</summary>

[embedmd]:#(manifests/04-enable-accelerated-cni/helm-values/network-operator.yml)
```yml
nfd:
  enabled: false
  deployNodeFeatureRules: false
sriovNetworkOperator:
  enabled: true
sriov-network-operator:
  crds:
    enabled: true
  sriovOperatorConfig:
    deploy: true
    configDaemonNodeSelector: null
```
</details>

#### Install the OVN Kubernetes resource injection webhook

The OVN Kubernetes resource injection webhook injected each pod scheduled to a worker node with a request for a VF and a Network Attachment Definition. This webhook is part of the same helm chart as the other components of the OVN Kubernetes CNI. Here it is installed by adjusting the existing helm installation to add the webhook component to the installation. 

```shell
helm upgrade --install -n ovn-kubernetes ovn-kubernetes oci://nvcr.io/nvstaging/doca/ovn-kubernetes-chart --version $DPF_VERSION --set tags.ovn-kubernetes-resource-injector=true --reuse-values=true
```

#### Apply the NICClusterConfiguration and SriovNetworkNodePolicy

```shell
kubectl apply -f manifests/04-enable-accelerated-cni/
```

This will deploy the following objects:

<details><summary>NICClusterPolicy for the NVIDIA Network Operator etcd</summary>

[embedmd]:#(manifests/05-dpuservice-installation/nic_cluster_policy.yaml)
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

[embedmd]:#(manifests/05-dpuservice-installation/sriov_network_operator_policy.yaml)
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
Verify the DPF System with:
```shell
## Ensure the provisioning and DPUService controller manager deployments are available.
kubectl wait --for=condition=Ready --namespace nvidia-network-operator pods --all
## Expect the following Daemonsets to be created but have zero replicas. Pods will be deployed by these Daemonsets when workers are added.
kubectl wait ds --for=jsonpath='{.status.numberReady}'=0 --namespace nvidia-network-operator kube-multus-ds sriov-network-config-daemon sriov-device-plugin 
```

### 5. DPUService installation

#### Label the image pull secret

The image pull secret is required when using a private registry to host images and helm charts. If using a public registry this section can be ignored.
```shell
kubectl -n ovn-kubernetes label secret dpf-pull-secret dpu.nvidia.com/image-pull-secret=""
```

#### Create the DPF provisioning and DPUService objects

A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
cat manifests/05-dpuservice-installation/*.yaml | envsubst | kubectl apply -f - 
```

This will deploy the following objects:
<details><summary>DPUSet to provision DPUs on worker nodes</summary>

[embedmd]:#(manifests/05-dpuservice-installation/dpuset.yaml)
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
      dpuFlavor: dpf-provisioning-hbn-ovn
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

<details><summary>OVN DPUService to deploy OVN workloads to the DPUs</summary>

[embedmd]:#(manifests/05-dpuservice-installation/ovn-dpuservice.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: ovn-dpu
  namespace: dpf-operator-system 
spec:
  helmChart:
    source:
      repoURL: oci://nvcr.io/nvstaging/doca
      chart: ovn-kubernetes-chart
      version: $DPF_VERSION
    values:
      tags:
        ovn-kubernetes-resource-injector: false
        ovnkube-node-dpu: true
        ovnkube-node-dpu-host: false
        ovnkube-single-node-zone: false
        ovnkube-control-plane: false
      k8sAPIServer: https://$TARGETCLUSTER_API_SERVER_HOST:$TARGETCLUSTER_API_SERVER_PORT
      podNetwork: $POD_CIDR/24
      serviceNetwork: $SERVICE_CIDR
      global:
        gatewayOpts: "--gateway-interface=br-ovn --gateway-uplink-port=puplinkbrovn"
        imagePullSecretName: dpf-pull-secret
      ovnkube-node-dpu:
        kubernetesSecretName: "ovn-dpu" # user needs to populate based on DPUServiceCredentialRequest
        vtepCIDR: "10.0.120.0/22" # user needs to populate based on DPUServiceIPAM
        hostCIDR: $TARGETCLUSTER_NODE_CIDR
        ipamPool: "pool1" # user needs to populate based on DPUServiceIPAM
        ipamPoolType: "cidrpool" # user needs to populate based on DPUServiceIPAM
        ipamVTEPIPIndex: 0
        ipamPFIPIndex: 1
```

</details>

<details><summary>HBN DPUService to deploy HBN workloads to the DPUs</summary>

[embedmd]:#(manifests/05-dpuservice-installation/hbn-dpuservice.yaml)
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
  - app-sf
  serviceDaemonSet:
    annotations:
      k8s.v1.cni.cncf.io/networks: |-
        [
        {"name": "iprequest", "interface": "ip_lo", "cni-args": {"poolNames": ["loopback"], "poolType": "cidrpool"}},
        {"name": "iprequest", "interface": "ip_pf2dpu2", "cni-args": {"poolNames": ["pool1"], "poolType": "cidrpool", "allocateDefaultGateway": true}}
        ]
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvstaging/doca
      version: 1.0.0-dev.1
      chart: doca-hbn
    values:
      imagePullSecrets:
      - name: dpf-pull-secret
      image:
        repository: nvcr.io/nvstaging/doca/doca_hbn
        tag: 2.dev.200-doca2.9.0
      resources:
        memory: 6Gi
        nvidia.com/bf_sf: 3
      configuration:
        perDPUValuesYAML: |
          - hostnamePattern: "*"
            values:
              bgp_autonomous_system: 65111
              bgp_peer_group: hbn
        startupYAMLJ2: |
          - header:
              model: BLUEFIELD
              nvue-api-version: nvue_v1
              rev-id: 1.0
              version: HBN 2.4.0
          - set:
              interface:
                lo:
                  ip:
                    address:
                      {{ ipaddresses.ip_lo.ip }}/32: {}
                  type: loopback
                p0_if,p1_if:
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
                          remote-as: external
```
</details>

<details><summary>DOCA Telemetry Service DPUService to deploy DTS to the DPUs</summary>

[embedmd]:#(manifests/05-dpuservice-installation/dts-dpuservice.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: doca-telemetry-service
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvstaging/doca
      version: 0.2.2
      chart: doca-telemetry
```
</details>

<details><summary>Blueman DPUService to deploy Blueman to the DPUs</summary>

[embedmd]:#(manifests/05-dpuservice-installation/blueman-dpuservice.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: doca-blueman-service
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvstaging/doca
      version: 1.0.3
      chart: doca-blueman
    values:
      imagePullSecrets:
      - name: dpf-pull-secret
```
</details>

<details><summary>OVN DPUServiceCredentialRequest to allow cross cluster communication</summary>

[embedmd]:#(manifests/05-dpuservice-installation/ovn-credentials.yaml)
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

<details><summary>DPUServiceInterfaces for physical ports on the DPU</summary>

[embedmd]:#(manifests/05-dpuservice-installation/physical-ifaces.yaml)
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

<details><summary>OVN DPUServiceInterface to define the ports attached to OVN workloads on the DPU</summary>

[embedmd]:#(manifests/05-dpuservice-installation/ovn-iface.yaml)
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


<details><summary>HBN DPUServiceInterfaces to define the ports attached to HBN workloads on the DPU</summary>

[embedmd]:#(manifests/05-dpuservice-installation/hbn-ifaces.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: app-sf 
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            svc.dpu.nvidia.com/interface: "app_sf"
            svc.dpu.nvidia.com/service: doca-hbn
        spec:
          interfaceType: service
          service:
            serviceID: doca-hbn
            network: mybrhbn
            interfaceName: pf2dpu2_if
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
            interfaceName: p1_if
```
</details>

<details><summary>DPUServiceFunctionChain to define the HBN-OVN ServiceFunctionChain</summary>

[embedmd]:#(manifests/05-dpuservice-installation/hbn-ovn-chain.yaml)
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
  name: ovn-to-hbn
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        spec:
          switches:
            - ports:
              - serviceInterface:
                  matchLabels:
                    svc.dpu.nvidia.com/service: doca-hbn
                    svc.dpu.nvidia.com/interface: "app_sf"
              - serviceInterface:
                  matchLabels:
                    port: ovn
```
</details>

<details><summary>DPUServiceIPAM to set up IP Address Management on the DPUCluster</summary>

[embedmd]:#(manifests/05-dpuservice-installation/hbn-ovn-ipam.yaml)
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

<details><summary>DPUServiceIPAM for the loopback interface in HBN</summary>

[embedmd]:#(manifests/05-dpuservice-installation/hbn-loopback-ipam.yaml)
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

Verify the DPUService installation with:
```shell
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system  dpuservices doca-blueman-service doca-hbn doca-telemetry-service
## Ensure the DPUServiceIPAMs have been reconciled
kubectl wait --for=condition=DPUIPAMObjectReconciled --namespace dpf-operator-system dpuserviceipam --all

## Ensure the DPUServiceInterfaces have been reconciled
kubectl wait --for=condition=ServiceInterfaceSetReconciled --namespace dpf-operator-system dpuserviceinterface --all

## Ensure the DPUServiceChains have been reconciled
kubectl wait --for=condition=ServiceChainSetReconciled --namespace dpf-operator-system dpuservicechain --all
```

### 6 Test traffic
#### Add worker nodes to the cluster

At this point workers should be added to the cluster. Each worker node should be configured in line with [the prerequisites](../../prerequisites.md) and the specific [OVN Kubernetes prerequisites](#worker-nodes).

As workers are added to the cluster DPUs will be provisioned and DPUServices will begin to be spun up.

#### Deploy test pods 

```shell
kubectl apply -f manifests/06-test-traffic
```

HBN and OVN functionality can be tested by pinging between the pods and services deployed in the default namespace.

TODO: Add specific user commands to test traffic.
