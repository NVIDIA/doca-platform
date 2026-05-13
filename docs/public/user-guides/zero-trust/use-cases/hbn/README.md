---
title: "HBN in DPF Zero Trust"
---


> [!NOTE]
> Follow this guide from the source GitHub repo at [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform)
> and moving to the `docs/public/user-guides/zero-trust/use-cases/hbn/README.md` for better formatting of
> the code.


This configuration provides instructions for deploying the NVIDIA DOCA Platform Framework (DPF) on high-performance,
bare-metal infrastructure in Zero Trust mode, utilizing DPU BMC and Redfish. It focuses on provisioning NVIDIA®
BlueField®-3 DPUs using DPF, installing the HBN DPUService on those DPUs and enabling workload traffic to pass through
HBN before leaving the DPU.

[TOC]

## Prerequisites

This guide should be run by cloning the repo from [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform)
and moving to the `docs/public/user-guides/zero-trust/use-cases/hbn` directory.

The system is set up as described in the [prerequisites](../../prerequisites/README.md).

In addition, for this use case, the Top of Rack switch(ToR) must support BGP and EVPN, and should be configured to
support unnumbered BGP towards the two ports of the DPU, where HBN will act as peer, and advertise routes over BGP to
allow for ECMP from the DPU. Additional information about how to do that can be found in the [RDG for DPF Zero Trust (DPF-ZT) with HBN DPU Service](https://docs.nvidia.com/networking/display/public/sol/rdg+for+dpf+zero+trust+(dpf-zt)+with+hbn+dpu+service).

### Software prerequisites

The following tools must be installed on the machine where the commands contained in this guide run:

* kubectl
* helm
* envsubst

## Installation guide

This guide assumes that the setup includes only 2 workers with DPUs. If your setup has more than 2 workers, then you
will need to set additional variables to enable the rest of the DPUs.

### 0. Required variables

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

## The DPF REGISTRY is the Helm repository URL where the DPF Operator Chart resides.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export REGISTRY=https://helm.ngc.nvidia.com/nvidia/doca

## The DPF TAG is the version of the DPF components which will be deployed in this guide.
export TAG=v25.10.1

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BFB_URL="https://content.mellanox.com/BlueField/BFBs/Ubuntu24.04/bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb"

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

### 1. DPF Operator installation

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
## Ensure bfb registry daemonset is available
kubectl rollout status daemonset --namespace dpf-operator-system bfb-registry
## Ensure the DPUCluster is ready for nodes to join.
kubectl wait --for=condition=ready --namespace dpu-cplane-tenant1 dpucluster --all
```

### 3. DPU Provisioning and Service Installation

There are 2 types of installation a user can do. The first one is using the PFs of the host and the second one is using
both PFs and VFs. You should choose the one that fits best on your use case.

In the following section, we provision our DPUs and the services tht will run on them. The user is expected to create a
DPUDeployment object that reflects a set of DPUServices that should run on a set of DPUs.

> If you want to learn more about `DPUDeployments`, feel free to check the [DPUDeployment documentation](../../../../developer-guides/api/dpudeployment.md).

#### Using PFs

In this scenario, the PF0 and PF1 are connected to separate VRFs which means that:
* PF0 on Host 1 will be able to communicate with PF0 on Host 2
* PF0 on Host 1 will not be able to communicate with PF1 on Host 1 and 2

* PF1 on Host 1 will be able to communicate with PF1 on Host 2
* PF1 on Host 1 will not be able to communicate with PF0 on Host 1 and 2

We make use of a PF on the host to test traffic.

##### Create the DPUDeployment, DPUServiceConfig, DPUServiceTemplate and other necessary objects

> [!WARNING]
> In case more than 1 DPU exists per node, the relevant selector should be applied in the DPUDeployment
> to select the appropriate DPU. See [DPUDeployment - DPUs Configuration](../../../../developer-guides/api/dpudeployment.md#dpus-configuration)
> to understand more about the selectors.

A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
cat manifests/03.1-dpudeployment-installation-pf/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:
<details markdown="1"><summary><b>BFB to download Bluefield Bitstream to a shared volume</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-pf/bfb.yaml)
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

[embedmd]:#(manifests/03.1-dpudeployment-installation-pf/hbn-dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: hbn-$TAG
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
      _ovs-vsctl --may-exist add-br br-hbn
      _ovs-vsctl set bridge br-hbn datapath_type=netdev
      _ovs-vsctl set bridge br-hbn fail_mode=secure
```
</details>

> [!NOTE]
> The DPUFlavor above does not set `ubuntu_PASSWORD` in `bfcfgParameters`. If omitted, DPF defaults to `ubuntu:ubuntu` credentials on the DPU. For production deployments, it is recommended to set a hashed password (see [BFCfg Parameters](../../../../developer-guides/api/dpuflavor.md#bfcfg-parameters)).

<details markdown="1"><summary><b>DPUDeployment to provision DPUs on worker nodes</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-pf/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: hbn
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle-$TAG
    flavor: hbn-$TAG
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
    doca-hbn:
      serviceTemplate: doca-hbn
      serviceConfiguration: doca-hbn
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
```
</details>

<details markdown="1"><summary><b>DPUServiceConfig and DPUServiceTemplate to deploy HBN workloads to the DPUs</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-pf/hbn-dpuserviceconfig.yaml)
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
```

[embedmd]:#(manifests/03.1-dpudeployment-installation-pf/hbn-dpuservicetemplate.yaml)
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
        nvidia.com/bf_sf: 4
```
</details>

<details markdown="1"><summary><b>DPUServiceInterfaces for physical ports on the DPU</b></summary>

[embedmd]:#(manifests/03.1-dpudeployment-installation-pf/physical-ifaces.yaml)
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

[embedmd]:#(manifests/03.1-dpudeployment-installation-pf/hbn-ipam.yaml)
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

[embedmd]:#(manifests/03.1-dpudeployment-installation-pf/hbn-loopback-ipam.yaml)
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

##### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Note that the DPUService name will have a random suffix. For example, `doca-hbn-l2xsl`.

Verify the DPU and Service installation with:

```shell
## Ensure the BFB is ready
kubectl wait --for=jsonpath='{.status.phase}'=Ready --namespace dpf-operator-system bfb bf-bundle-$TAG --timeout=600s
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system dpuservices -l svc.dpu.nvidia.com/owned-by-dpudeployment=dpf-operator-system_hbn
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
NAME                                                NAMESPACE            STATUS       REASON                SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig                 dpf-operator-system
│           ├─Ready                                                      False        Pending               17m    The following conditions are not ready:
│           │                                                                                                      * SystemComponentsReady
│           └─SystemComponentsReady                                      False        Error                 16m    System components must be ready for DPF Operator to continue:
│                                                                                                                    * nvidia-k8s-ipam: DPUService dpf-operator-system/nvidia-k8s-ipam is not ready
└─DPUDeployments
  └─DPUDeployment/hbn                               dpf-operator-system
    │           ├─Ready                                                  False        Pending               11m    The following conditions are not ready:
    │           │                                                                                                  * DPUSetsReady
    │           └─DPUSetsReady                                           False        Pending               11m    Objects are not ready:
    │                                                                                                              * dpf-operator-system/hbn-dpuset1
    ├─DPUServiceChains
    │ └─DPUServiceChain/hbn-8kkjz                   dpf-operator-system  Ready: True  Success               11m
    ├─DPUServiceInterfaces
    │ └─4 DPUServiceInterfaces...                   dpf-operator-system  Ready: True  Success               11m    See doca-hbn-p0-if-mcqp4, doca-hbn-p1-if-6x2hh, doca-hbn-pf0hpf-if-q9lvk, doca-hbn-pf1hpf-if-979t7
    ├─DPUSets
    │ └─DPUSet/hbn-dpuset1                          dpf-operator-system
    │   ├─BFB/bf-bundle                             dpf-operator-system  Ready: True  Ready                 13m    File: bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb, DOCA: 3.2.1
    │   └─DPUs
    │     ├─DPU/dpu-node-mt2402xz0f6v-mt2402xz0f6v  dpf-operator-system
    │     │             └─Ready                                          False        OS Installing         8m39s
    │     └─DPU/dpu-node-mt2404xz0c98-mt2404xz0c98  dpf-operator-system
    │                   └─Ready                                          False        OS Installing         8m39s
    └─Services
      ├─DPUServiceTemplates
      │ └─DPUServiceTemplate/doca-hbn               dpf-operator-system  Ready: True  Success               13m
      └─DPUServices
        └─DPUService/doca-hbn-jmj45                 dpf-operator-system  Ready: True  Success               11m
```

###### Releasing the Node Effect Hold

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

###### Making the DPUs Ready

In order to make the DPUs ready, we will need to manually power cycle the host. This operation should be done in the
most graceful manner by gracefully shutting down the Host and DPU, powering off the server and then powering it on to
avoid corruption. This should happen when the object gives us the signal. The described flow can be automated by the
admin depending on the infrastructure.

The following verification command may need to be run multiple times to ensure the expected reboot status is set.

```shell
## Ensure the DPUs report rebootStatus.reason=WaitingForManualPowerCycleOrReboot (this may take time)
kubectl wait --for=jsonpath='{.status.rebootStatus.reason}'=WaitingForManualPowerCycleOrReboot --namespace dpf-operator-system dpu --all
```

At this point, we have to power cycle the hosts. Once **all the hosts are back online**, we have to remove an annotation
from the DPUNodes. The user can choose to remove this annotation node by node but to make it simpler in this guide, we do
that all at once.

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
NAME                                   NAMESPACE            STATUS       REASON    SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig    dpf-operator-system  Ready: True  Success   8m19s
└─DPUDeployments
  └─DPUDeployment/hbn                  dpf-operator-system  Ready: True  Success   19s
    ├─DPUServiceChains
    │ └─DPUServiceChain/hbn-8kkjz      dpf-operator-system  Ready: True  Success   90m
    ├─DPUServiceInterfaces
    │ └─4 DPUServiceInterfaces...      dpf-operator-system  Ready: True  Success   48s    See doca-hbn-p0-if-mls69, doca-hbn-p1-if-dv6ds, doca-hbn-pf0hpf-if-q9lvk, doca-hbn-pf1hpf-if-979t7
    ├─DPUSets
    │ └─DPUSet/hbn-dpuset1             dpf-operator-system
    │   ├─BFB/bf-bundle                dpf-operator-system  Ready: True  Ready     91m    File: bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb, DOCA: 3.2.1
    │   └─DPUs
    │     └─2 DPUs...                  dpf-operator-system  Ready: True  DPUReady  25m    See dpu-node-mt2402xz0f6v-mt2402xz0f6v, dpu-node-mt2404xz0c98-mt2404xz0c98
    └─Services
      ├─DPUServiceTemplates
      │ └─DPUServiceTemplate/doca-hbn  dpf-operator-system  Ready: True  Success   91m
      └─DPUServices
        └─DPUService/doca-hbn-6rhsx    dpf-operator-system  Ready: True  Success   21s
```

##### Test Traffic

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

#### Using PFs + VFs

In this scenario, the PF0, PF1, VF10 of the PF0 and VF10 of the PF1 are connected to separate VRFs which means that:

* PF0 on Host 1 will be able to communicate with PF0 on Host 2
* PF0 on Host 1 will not be able to communicate with PF1 on Host 1 and 2
* PF0 on Host 1 will not be able to communicate with PF0VF10 on Host 1 and 2
* PF0 on Host 1 will not be able to communicate with PF1VF10 on Host 1 and 2

* PF1 on Host 1 will be able to communicate with PF1 on Host 2
* PF1 on Host 1 will not be able to communicate with PF0 on Host 1 and 2
* PF1 on Host 1 will not be able to communicate with PF0VF10 on Host 1 and 2
* PF1 on Host 1 will not be able to communicate with PF1VF10 on Host 1 and 2

* PF0VF10 on Host 1 will be able to communicate with PF0VF10 on Host 2
* PF0VF10 on Host 1 will not be able to communicate with PF0 on Host 1 and 2
* PF0VF10 on Host 1 will not be able to communicate with PF1 on Host 1 and 2
* PF0VF10 on Host 1 will not be able to communicate with PF1VF10 on Host 1 and 2

* PF1VF10 on Host 1 will be able to communicate with PF1VF10 on Host 2
* PF1VF10 on Host 1 will not be able to communicate with PF0 on Host 1 and 2
* PF1VF10 on Host 1 will not be able to communicate with PF1 on Host 1 and 2
* PF1VF10 on Host 1 will not be able to communicate with PF0VF10 on Host 1 and 2

We make use of a PF and a VF on the host to test traffic.

##### Create the DPUDeployment, DPUServiceConfig, DPUServiceTemplate and other necessary objects

A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
cat manifests/03.2-dpudeployment-installation-pf-vf/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:
<details markdown="1"><summary><b>BFB to download Bluefield Bitstream to a shared volume</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-pf-vf/bfb.yaml)
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

[embedmd]:#(manifests/03.2-dpudeployment-installation-pf-vf/hbn-dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: hbn-$TAG
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
      _ovs-vsctl --may-exist add-br br-hbn
      _ovs-vsctl set bridge br-hbn datapath_type=netdev
      _ovs-vsctl set bridge br-hbn fail_mode=secure
```
</details>

> [!NOTE]
> The DPUFlavor above does not set `ubuntu_PASSWORD` in `bfcfgParameters`. If omitted, DPF defaults to `ubuntu:ubuntu` credentials on the DPU. For production deployments, it is recommended to set a hashed password (see [BFCfg Parameters](../../../../developer-guides/api/dpuflavor.md#bfcfg-parameters)).

<details markdown="1"><summary><b>DPUDeployment to provision DPUs on worker nodes</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-pf-vf/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: hbn
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle-$TAG
    flavor: hbn-$TAG
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
    doca-hbn:
      serviceTemplate: doca-hbn
      serviceConfiguration: doca-hbn
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
        - serviceInterface:
            matchLabels:
              interface: pf0vf10
        - service:
            name: doca-hbn
            interface: pf0vf10_if
      - ports:
        - serviceInterface:
            matchLabels:
              interface: pf1vf10
        - service:
            name: doca-hbn
            interface: pf1vf10_if
```
</details>

<details markdown="1"><summary><b>DPUServiceConfig and DPUServiceTemplate to deploy HBN workloads to the DPUs</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-pf-vf/hbn-dpuserviceconfig.yaml)
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
          {"name": "iprequest", "interface": "ip_pf1hpf", "cni-args": {"poolNames": ["pool2"], "poolType": "cidrpool", "allocateDefaultGateway": true}},
          {"name": "iprequest", "interface": "ip_pf0vf10", "cni-args": {"poolNames": ["pool3"], "poolType": "cidrpool", "allocateDefaultGateway": true}},
          {"name": "iprequest", "interface": "ip_pf1vf10", "cni-args": {"poolNames": ["pool4"], "poolType": "cidrpool", "allocateDefaultGateway": true}}
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
                vrf3: GREEN
                vrf4: YELLOW
                l3vni1: 100001
                l3vni2: 100002
                l3vni3: 100003
                l3vni4: 100004
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
                  p0_if,p1_if,pf0vf10_if,pf1vf10_if,pf0hpf_if,pf1hpf_if:
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
                  pf0hpf_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf0hpf.cidr }}: {}
                      vrf: {{ config.vrf3 }}
                  pf1hpf_if:
                    ip:
                      address:
                        {{ ipaddresses.ip_pf1hpf.cidr }}: {}
                      vrf: {{ config.vrf4 }}
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
                  {{ config.vrf3 }}:
                    evpn:
                      enable: on
                      vni:
                        {{ config.l3vni3 }}: {}
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
                  {{ config.vrf4 }}:
                    evpn:
                      enable: on
                      vni:
                        {{ config.l3vni4 }}: {}
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
  - name: pf0hpf_if
    network: mybrhbn
  - name: pf1hpf_if
    network: mybrhbn
```

[embedmd]:#(manifests/03.2-dpudeployment-installation-pf-vf/hbn-dpuservicetemplate.yaml)
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
        nvidia.com/bf_sf: 6
```
</details>

<details markdown="1"><summary><b>DPUServiceInterfaces for physical ports on the DPU</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-pf-vf/physical-ifaces.yaml)
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
  name: pf0vf10-rep
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "pf0vf10"
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
            interface: "pf1vf10"
        spec:
          interfaceType: vf
          vf:
            parentInterfaceRef: p1
            pfID: 1
            vfID: 10
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

[embedmd]:#(manifests/03.2-dpudeployment-installation-pf-vf/hbn-ipam.yaml)
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
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: pool3
  namespace: dpf-operator-system
spec:
  ipv4Network:
    network: "10.0.123.0/24"
    gatewayIndex: 2
    prefixSize: 29
    # These preallocations are not necessary. We specify them so that the validation commands are straightforward.
    allocations:
      dpu-node-${DPU1_SERIAL}-${DPU1_SERIAL}: 10.0.123.0/29
      dpu-node-${DPU2_SERIAL}-${DPU2_SERIAL}: 10.0.123.8/29
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: pool4
  namespace: dpf-operator-system
spec:
  ipv4Network:
    network: "10.0.124.0/24"
    gatewayIndex: 2
    prefixSize: 29
```
</details>

<details markdown="1"><summary><b>DPUServiceIPAM for the loopback interface in HBN</b></summary>

[embedmd]:#(manifests/03.2-dpudeployment-installation-pf-vf/hbn-loopback-ipam.yaml)
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

##### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Note that the DPUService name will have a random suffix. For example, `doca-hbn-l2xsl`.

Verify the DPU and Service installation with:

```shell
## Ensure the BFB is ready
kubectl wait --for=jsonpath='{.status.phase}'=Ready --namespace dpf-operator-system bfb bf-bundle-$TAG --timeout=600s
## Ensure the DPUServices are created and have been reconciled.
kubectl wait --for=condition=ApplicationsReconciled --namespace dpf-operator-system dpuservices -l svc.dpu.nvidia.com/owned-by-dpudeployment=dpf-operator-system_hbn
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
NAME                                                NAMESPACE            STATUS       REASON         SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig                 dpf-operator-system
│           ├─Ready                                                      False        Pending        3m13s  The following conditions are not ready:
│           │                                                                                               * SystemComponentsReady
│           └─SystemComponentsReady                                      False        Error          2m28s  System components must be ready for DPF Operator to continue:
│                                                                                                             * nvidia-k8s-ipam: DPUService dpf-operator-system/nvidia-k8s-ipam is not ready
└─DPUDeployments
  └─DPUDeployment/hbn                               dpf-operator-system
    │           ├─Ready                                                  False        Pending        77s    The following conditions are not ready:
    │           │                                                                                           * DPUSetsReady
    │           └─DPUSetsReady                                           False        Pending        79s    Objects are not ready:
    │                                                                                                       * dpf-operator-system/hbn-dpuset1
    ├─DPUServiceChains
    │ └─DPUServiceChain/hbn-5zgs4                   dpf-operator-system  Ready: True  Success        79s
    ├─DPUServiceInterfaces
    │ └─6 DPUServiceInterfaces...                   dpf-operator-system  Ready: True  Success        79s    See doca-hbn-p0-if-w6f6b, doca-hbn-p1-if-p7565, doca-hbn-pf0hpf-if-wb84j, doca-hbn-pf0vf10-if-mr6fj,
    │                                                                                                       doca-hbn-pf1hpf-if-cnbz8, doca-hbn-pf1vf10-if-7r6r6
    ├─DPUSets
    │ └─DPUSet/hbn-dpuset1                          dpf-operator-system
    │   ├─BFB/bf-bundle                             dpf-operator-system  Ready: True  Ready          105s   File: bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb, DOCA: 3.2.1
    │   └─DPUs
    │     ├─DPU/dpu-node-mt2402xz0f6v-mt2402xz0f6v  dpf-operator-system
    │     │             └─Ready                                          False        OS Installing  72s
    │     └─DPU/dpu-node-mt2404xz0c98-mt2404xz0c98  dpf-operator-system
    │                   └─Ready                                          False        OS Installing  69s
    └─Services
      ├─DPUServiceTemplates
      │ └─DPUServiceTemplate/doca-hbn               dpf-operator-system  Ready: True  Success        104s
      └─DPUServices
        └─DPUService/doca-hbn-bjqbh                 dpf-operator-system  Ready: True  Success        77s
```

###### Releasing the Node Effect Hold

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

###### Making the DPUs Ready

In order to make the DPUs ready, we will need to manually power cycle the host. This operation should be done in the
most graceful manner by gracefully shutting down the Host and DPU, powering off the server and then powering it on to
avoid corruption. This should happen when the object gives us the signal. The described flow can be automated by the
admin depending on the infrastructure.

The following verification command may need to be run multiple times to ensure the expected reboot status is set.

```shell
## Ensure the DPUs report rebootStatus.reason=WaitingForManualPowerCycleOrReboot (this may take time)
kubectl wait --for=jsonpath='{.status.rebootStatus.reason}'=WaitingForManualPowerCycleOrReboot --namespace dpf-operator-system dpu --all
```

At this point, we have to power cycle the hosts. Once **all the hosts are back online**, we have to remove an annotation
from the DPUNodes. The user can choose to remove this annotation node by node but to make it simpler in this guide, we do
that all at once.

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
NAME                                   NAMESPACE            STATUS       REASON    SINCE  MESSAGE
NAME                                   NAMESPACE            STATUS       REASON    SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig    dpf-operator-system  Ready: True  Success   6m5s
└─DPUDeployments
  └─DPUDeployment/hbn                  dpf-operator-system  Ready: True  Success   2s
    ├─DPUServiceChains
    │ └─DPUServiceChain/hbn-5zgs4      dpf-operator-system  Ready: True  Success   36s
    ├─DPUServiceInterfaces
    │ └─6 DPUServiceInterfaces...      dpf-operator-system  Ready: True  Success   6s     See doca-hbn-p0-if-w6f6b, doca-hbn-p1-if-p7565, doca-hbn-pf0hpf-if-wb84j, doca-hbn-pf0vf10-if-mr6fj,
    │                                                                                     doca-hbn-pf1hpf-if-cnbz8, doca-hbn-pf1vf10-if-7r6r6
    ├─DPUSets
    │ └─DPUSet/hbn-dpuset1             dpf-operator-system
    │   ├─BFB/bf-bundle                dpf-operator-system  Ready: True  Ready     28m    File: bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb, DOCA: 3.2.1
    │   └─DPUs
    │     └─2 DPUs...                  dpf-operator-system  Ready: True  DPUReady  5m52s  See dpu-node-mt2402xz0f6v-mt2402xz0f6v, dpu-node-mt2404xz0c98-mt2404xz0c98
    └─Services
      ├─DPUServiceTemplates
      │ └─DPUServiceTemplate/doca-hbn  dpf-operator-system  Ready: True  Success   28m
      └─DPUServices
        └─DPUService/doca-hbn-bjqbh    dpf-operator-system  Ready: True  Success   3s
```

##### Test Traffic

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

In addition, we can test traffic by assigning an IP to the 10th VF of PF0 on the host for each DPU, and run a simple ping.
We could use any VF, but the DPUDeployment and DPUServiceInterface will need to be adjusted accordingly. First thing to
do is to create the VFs on the hosts where the each DPU belongs to:

```shell
echo 12 > /sys/class/net/ens5f0np0/device/sriov_numvfs
```

Then, assuming the VF is named `ens5f0v10`:

On the host with DPU with serial number DPU1_SERIAL:

```shell
ip link set dev ens5f0v10 up
ip addr add 10.0.123.1/29 dev ens5f0v10
ip route add 10.0.123.0/24 dev ens5f0v10 via 10.0.123.2
```

On the host with DPU with serial number DPU2_SERIAL:

```shell
ip link set dev ens5f0v10 up
ip addr add 10.0.123.9/29 dev ens5f0v10
ip route add 10.0.123.0/24 dev ens5f0v10 via 10.0.123.10
```

On the host with DPU with serial number DPU1_SERIAL:

```shell
$ ping 10.0.123.9 -c3
PING 10.0.123.9 (10.0.123.9) 56(84) bytes of data.
64 bytes from 10.0.123.9: icmp_seq=1 ttl=64 time=0.387 ms
64 bytes from 10.0.123.9: icmp_seq=2 ttl=64 time=0.344 ms
64 bytes from 10.0.123.9: icmp_seq=3 ttl=64 time=0.396 ms

--- 10.0.123.9 ping statistics ---
3 packets transmitted, 3 received, 0% packet loss, time 2053ms
rtt min/avg/max/mdev = 0.344/0.375/0.396/0.022 ms
```

## Uninstall

This section covers only the DPF related components and not the prerequisites as these must be managed by the admin.

### Delete the DPF Operator system and DPF Operator

```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

Note: there can be a race condition with deleting the underlying Kamaji cluster which runs the DPU cluster control
plane in this guide. If that happens it may be necessary to remove finalizers manually from `DPUCluster` and `Datastore`
objects.
