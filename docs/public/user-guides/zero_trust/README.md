---
title: "Zero trust bare metal provisioning with Redfish"
---


> Note:
> Follow this guide from the source GitHub repo at [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform) and moving to the `docs/public/user-guides/zero_trust/README.md` for better formatting of the code.


This configuration provides instructions for deploying the NVIDIA DOCA Platform Framework (DPF) on high-performance, bare-metal infrastructure in Zero-Trust mode, utilizing DPU BMC and [Redfish](../../other-operations/redfish.md). It focuses on the setup and use of DPU-based services on NVIDIA® BlueField®-3 DPUs to deliver secure, isolated, and hardware-accelerated environments. 

[TOC]

## Prerequisites
This guide should be run by cloning the repo from [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform) and moving to the `docs/public/user-guides/zero_trust` directory.

The system is set up as described in the [system prerequisites](../prerequisites/prerequisites.md).  The HBN DPUService has the additional requirements:

### Software prerequisites

The following tools must be installed on the machine where the commands contained in this guide run:

* kubectl
* helm
* envsubst

### Kubernetes prerequisites

* control plane setup is complete before starting this guide
* CNI installed before starting this guide
* worker nodes are not added until indicated by this guide
* High-speed ports are used for secondary workload network and not for primary CNI

## Installation guide

DPF supports multiple deployment models. This guide focuses on the Zero Trust bare-metal deployment model. In this scenario:

* The DPU is managed through its Baseboard Management Controller (BMC)
* All management traffic occurs over the DPU's out-of-band (OOB) network
* The host is considered as an untrusted entity towards the data center network. The DPU acts as a barrier between the host and the network.
* The host sees the DPU as a standard NIC, with no access to the internal DPU management plane (Zero Trust Mode)

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

## The DPF REGISTRY is the Helm repository URL where the DPF Operator Chart resides.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export REGISTRY=https://helm.ngc.nvidia.com/nvidia/doca

## The DPF TAG is the version of the DPF components which will be deployed in this guide.
export TAG=v25.4.0

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BLUEFIELD_BITSTREAM="https://content.mellanox.com/BlueField/BFBs/Ubuntu22.04/bf-bundle-3.0.0-135_25.04_ubuntu-22.04_prod.bfb"

## IP_RANGE_START and IP_RANGE_END
## These define the IP range for DPU discovery via Redfish/BMC interfaces
## Example: If your DPUs have BMC IPs in range 192.168.1.100-110
## export IP_RANGE_START=192.168.1.100
## export IP_RANGE_END=192.168.1.110

## Start of DPUDiscovery IpRange
export IP_RANGE_START=

## End of DPUDiscovery IpRange
export IP_RANGE_END=

## Serial number of DPU1
export DPU1_SERIAL=

## Serial number of DPU2
export DPU2_SERIAL=
```

### 1. DPF Operator installation

#### Create storage required by the DPF Operator
A number of [environment variables](#0-required-variables) must be set before running this command.

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

#### Additional Dependencies

Before deploying the DPF Operator, ensure that Helm is properly configured according to the [Helm prerequisites](../prerequisites/helm-prerequisites.md).  

> [!WARNING]
> 
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


### 2. DPF system installation
This section involves creating the DPF system components and some basic infrastructure required for a functioning DPF-enabled cluster.

#### Deploy the DPF System components
A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
kubectl create ns dpu-cplane-tenant1
cat manifests/02-dpf-system-installation/*.yaml | envsubst | kubectl apply -f - 
```

This will create the following objects:
<details markdown="1"><summary>DPF Operator to install the DPF System components</summary>

[embedmd]:#(manifests/02-dpf-system-installation/operatorconfig.yaml)
```yaml
---
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  dpuDetector:
    disable: true
  provisioningController:
    bfbPVCName: "bfb-pvc"
    dmsTimeout: 900
  installInterface:
    installViaRedfish:
      # Set this to the IP of one of your control plane nodes + 8080 port
      bfbRegistryAddress: "$TARGETCLUSTER_API_SERVER_HOST:8080"
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
  # Define the IP range to scan
  ipRangeSpec:
    ipRange:
      startIP: $IP_RANGE_START
      endIP:   $IP_RANGE_END
  
  # Optional: Set scan interval
  scanInterval: "60m"
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

### 3. DPU Provisioning and Service Installation

In this step we deploy our DPUs and the services that will run on them.

The user is expected to create a DPUDeployment object that reflects a set of DPUServices
that should run on a set of DPUs.

> If you want to learn more about `DPUDeployments`, feel free to check the [DPUDeployment documentation](../../api/dpudeployment.md).

#### Create the DPUDeployment, DPUServiceConfig, DPUServiceTemplate and other necessary objects

A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
cat manifests/03-dpudeployment-installation/*.yaml | envsubst | kubectl apply -f - 
```

This will deploy the following objects:
<details markdown="1"><summary>BFB to download Bluefield Bitstream to a shared volume</summary>

[embedmd]:#(manifests/03-dpudeployment-installation/bfb.yaml)
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

<details markdown="1"><summary>HBN DPUFlavor to correctly configure the DPUs on provisioning</summary>

[embedmd]:#(manifests/03-dpudeployment-installation/hbn-dpuflavor.yaml)
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
      _ovs-vsctl set Interface p0 mtu_request=9216
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical
```
</details>

<details markdown="1"><summary>DPUDeployment to provision DPUs on worker nodes</summary>

[embedmd]:#(manifests/03-dpudeployment-installation/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: hbn-only
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavor: dpf-provisioning-hbn
    dpuSets:
    - nameSuffix: "dpuset1"
      nodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
  services:
    doca-hbn:
      serviceTemplate: doca-hbn
      serviceConfiguration: doca-hbn
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
```
</details>

<details markdown="1"><summary>DPUServiceConfig and DPUServiceTemplate to deploy HBN workloads to the DPUs</summary>

[embedmd]:#(manifests/03-dpudeployment-installation/hbn-dpuserviceconfig.yaml)
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
            - hostnamePattern: "dpunode-${DPU1_SERIAL}"
              values:
                bgp_autonomous_system: 65101
            - hostnamePattern: "dpunode-${DPU2_SERIAL}"
              values:
                bgp_autonomous_system: 65201
          startupYAMLJ2: |
            - header:
                model: bluefield
                nvue-api-version: nvue_v1
                rev-id: 1.0
                version: HBN 2.4.0
            - set:
                evpn:
                  enable: on
                  route-advertise: {}
                interface:
                  lo:
                    ip:
                      address:
                        {{ ipaddresses.ip_lo.ip }}/32: {}
                    type: loopback
                  p0_if,p1_if,pf0vf10_if,pf1vf10_if:
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
```

[embedmd]:#(manifests/03-dpudeployment-installation/hbn-dpuservicetemplate.yaml)
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
      version: 1.0.2
      chart: doca-hbn
    values:
      image:
        repository: $HBN_NGC_IMAGE_URL
        tag: 3.0.0-doca3.0.0
      resources:
        memory: 6Gi
        nvidia.com/bf_sf: 4
```
</details>

<details markdown="1"><summary>DPUServiceInterfaces for physical ports on the DPU</summary>

[embedmd]:#(manifests/03-dpudeployment-installation/physical-ifaces.yaml)
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

<details markdown="1"><summary>DPUServiceIPAM to set up IP Address Management on the DPUCluster</summary>

[embedmd]:#(manifests/03-dpudeployment-installation/hbn-ipam.yaml)
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

<details markdown="1"><summary>DPUServiceIPAM for the loopback interface in HBN</summary>

[embedmd]:#(manifests/03-dpudeployment-installation/hbn-loopback-ipam.yaml)
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

Note that the DPUService name will have a random suffix. For example, `doca-hbn-l2xsl`.

Verify the DPU and Service installation with:

```shell
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system dpuservices -l svc.dpu.nvidia.com/owned-by-dpudeployment=dpf-operator-system_hbn-only
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
  └─DPUDeployment/hbn-only                  dpf-operator-system  Ready: True   Success       2h     
    ├─DPUServiceChains
    │ └─DPUServiceChain/hbn-only-wkdhz      dpf-operator-system  Ready: True   Success       2h
    ├─DPUServices   
    │ └─DPUService/doca-hbn-l2xsl           dpf-operator-system  Ready: True   Success       2h     
    └─DPUSets
      └─DPUSet/hbn-only-dpuset1             dpf-operator-system
        ├─BFB/bf-bundle                     dpf-operator-system
        ├─DPU/c-234-181-120-125-0000-08-00  dpf-operator-system  Ready: True   DPUNodeReady  2h
        └─DPU/c-234-181-120-126-0000-08-00  dpf-operator-system  Ready: True   DPUNodeReady  2h
```

