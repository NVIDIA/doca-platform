---
title: "DOCA XPlane & DOCA Weave (BlueField-4)"
---

This configuration provides instructions for deploying the NVIDIA DOCA Platform Framework (DPF) on high-performance,
bare-metal infrastructure in Zero Trust mode, utilizing DPU BMC and Redfish. It focuses on provisioning NVIDIA®
BlueField®-4 DPUs with DPF, and deploying DOCA XPlane and DOCA Weave to enable East/West (E/W) network isolation.

Deployment of additional DPU services, service interfaces, and service chains for North/South (N/S) communication is omitted.

This deployment guide demonstrates:
1. The installation of DPF in a zero-trust environment
2. Deployment of DOCA XPlane and DOCA Weave in a dual rail dual plane network topology
3. Configuration of network isolation - A single virtual network encompassing both rails

[[_TOC_]]

## Prerequisites

### General

* This guide should be run by cloning the repo and navigating to the directory of this README file.
* The system is set up as described in the [prerequisites](../../prerequisites/README.md).

### Hardware and fabric Prerequisites

* 2x Worker nodes with Bluefield-4 DPUs.
* 2x ConnectX-9 Smart-NICs per worker preconfigured with 2 Hardware planes and ASTRA.
* Network Fabric is configured with Spectrum-X network configuration.
  * 1 Rail
  * 2 Software planes
  * 2 Hardware Planes (for each Software plane)
  * Each NIC gets a `/31` address
  * Spectrum-X network subnets
    * overall network: `172.16.0.0/12`
    * software plane 0: `172.16.0.0/13`
    * software plane 1: `172.24.0.0/13`
    * rail networks:
      * rail 0 on software plane 0: `172.16.0.0/16`
      * rail 0 on software plane 1: `172.24.0.0/16`

> [!NOTE]
> There are multiple possible hardware and fabric setups. It is not feasible to cover them all in this guide.
> Instead, we aim to provide a basic example that can serve as a base for adjustment to fit the specific needs
> of your setup.

> [!NOTE]
> This guide deploys an IPv4 underlay and overlay. For an IPv6 fabric see
> [DOCA XPlane & DOCA Weave over IPv6](ipv6-deployment.md), which lists the values that change. Weave runs a single
> address family per deployment, there is no dual-stack mode.

Checkout [additional-consideration](additional-considerations.md) for more information.

### Software Prerequisites
Install the following tools on the machine where you will run the commands in this guide:

* kubectl
* helm
* envsubst
* sshpass

## Installation Guide

Commands in this guide are run in the same directory that contains this README.

> [!NOTE]
> In this guide we use a wrapper script over envsubst under `../../../../exec_envsubst.sh` which
> limits the variable substitution to values defined in `manifests/00-env-vars/envvars.env`.
> This is required so that the DPUFlavorTemplate is rendered correctly.

### 0. Required Variables

The following variables are required. Sensible defaults are provided where possible, but many values will be specific to your target infrastructure.

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
## allocated by DHCP. e.g 10.0.110.200
export DPUCLUSTER_VIP=

## Interface on which the DPUCluster load balancer will listen. Should be the management interface of the control plane
## node. e.g enp1s0
export DPUCLUSTER_INTERFACE=

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

## The DPF TAG is the version of the DPF components which will be deployed in this guide.
export TAG=v26.8.0

## Bluefield-4 OS image ISO URL
export BFSW_OS_ISO_URL=https://nbu-nfs.gtm.nvidia.com/auto/sw_mc_soc_release/doca_dpu/doca_3.3.0_vr_0.8/20260806/ISO/bf4-os-doca-bundle-3.3.0-407_26.01_ubuntu-24.04_64k.iso

## Bluefield-4 firmware bundle URL
export PLDM_PSID=MT_0000001775
export BFSW_FW_BUNDLE_URL=https://urm.nvidia.com/artifactory/sw-mlnx-bluefield-generic/bluefield4/PLDM/20260806.1/Dev/MT_0000001775/nvfw_BF4-DPU_0000_260806.17170601.1_custom_dbg-signed.fwpkg

## ConnectX-9 firmware URL
export CXFW_URL=https://nbu-nfs.gtm.nvidia.com/auto/mswg/release/host_fw/fw-4133/fw-4133-rel-82_48_1886-build-001/etc/bin/alternate_bin/fw-ConnectX9-rel-82_48_1886-900-9X91E-00EB-STA_CLOUD_ALT_Ax-UEFI-14.43.10-FlexBoot-3.9.101.bin

## ==== XPlane & Weave Vars ==== ##

## DPUFlavor Rail topology fields. 1 rail, 2 SW planes, 2 HW planes per SW plane
# Note: all entities below reside on the DPU

## RAIL0_SWP0_HWP0_NETDEV points to the E/W NIC netdev on the DPU for Rail 0, SW Plane 0, HW Plane 0
## Example: A1p0
export RAIL0_SWP0_HWP0_NETDEV=
## RAIL0_SWP0_HWP1_NETDEV points to the E/W NIC netdev on the DPU for Rail 0 SW Plane 0, HW Plane 1
## Example: A1p1
export RAIL0_SWP0_HWP1_NETDEV=
## RAIL0_SWP1_HWP0_NETDEV points to the E/W NIC netdev on the DPU for Rail 0 SW Plane 1, HW Plane 0
## Example: A11p0
export RAIL0_SWP1_HWP0_NETDEV=
## RAIL0_SWP1_HWP1_NETDEV points to the E/W NIC netdev on the DPU for Rail 0 SW Plane 1, HW Plane 1
## Example: A11p1
export RAIL0_SWP1_HWP1_NETDEV=

## RAIL0_SWP0_PCI_ADDRESS points to the ECPF of the netdev identified by RAIL0_SWP0_HWP0_NETDEV
## Example: 0000:01:00.0
export RAIL0_SWP0_PCI_ADDRESS=
## RAIL0_SWP1_PCI_ADDRESS points to the ECPF of the netdev identified by RAIL0_SWP1_HWP0_NETDEV
## Example: 0001:01:00.0
export RAIL0_SWP1_PCI_ADDRESS=

## Rail IP address assignment per DPU identified by Serial Number (adjust to fit your DPUs)
## Used to set per-dpu values on the DPUDevice CR. consumed by the DPUFlavorTemplate

## Number of E/W NICs on the DPU
export NIC_DEVICE_COUNT=2

## Underlay address plan, consumed by the doca-weave-flow-controller underlayConfigMapData.
## The values below describe the IPv4 scheme of this guide, see ipv6-deployment.md for the IPv6 scheme.
## OVERLAY_NETWORK_PREFIX_LENGTH is the CIDR prefix length of each virtual network subnet
export OVERLAY_NETWORK_PREFIX_LENGTH=12
## SOFTWARE_PLANE_ID_BIT_LENGTH is the number of address bits identifying the software plane
export SOFTWARE_PLANE_ID_BIT_LENGTH=1
## RAIL_ID_BIT_LENGTH is the number of address bits identifying the rail within a software plane
export RAIL_ID_BIT_LENGTH=3

## RAIL_NETWORK_SWP0 is the prefix for the rail network over software plane 0
## Example: 172.16.0.0/13
export RAIL_NETWORK_SWP0=
## RAIL_NETWORK_SWP1 is the prefix for the rail network over software plane 1
## Example: 172.24.0.0/13
export RAIL_NETWORK_SWP1=

## Serial number of DPUs. If you have more than 2 DPUs, you will need to parameterize the system accordingly and expose
## additional variables.
## All serial numbers must be in lowercase.
## Example: mt2610604v1p
export DPU1_SERIAL=
## Example: 172.16.0.2/31
export DPU1_RAIL0_SWP0_IP=
## Example: 172.16.0.3
export DPU1_RAIL0_SWP0_GW=
## Example: 172.24.0.2/31
export DPU1_RAIL0_SWP1_IP=
## Example: 172.24.0.3
export DPU1_RAIL0_SWP1_GW=

## Example: mt2614604ee5
export DPU2_SERIAL=
## Example: 172.16.0.4/31
export DPU2_RAIL0_SWP0_IP=
## Example: 172.16.0.5
export DPU2_RAIL0_SWP0_GW=
## Example: 172.24.0.4/31
export DPU2_RAIL0_SWP1_IP=
## Example: 172.24.0.5
export DPU2_RAIL0_SWP1_GW=

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

These verification commands may need to be run multiple times until the condition is met.

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
cat manifests/01-dpf-system-installation/*.yaml | ../../../../exec_envsubst.sh | kubectl apply -f -
```

This will create the following objects:
<details markdown="1"><summary><b>DPFOperatorConfig to install the DPF System components</b></summary>

[embedmd]:#(manifests/01-dpf-system-installation/operatorconfig.yaml)
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

[embedmd]:#(manifests/01-dpf-system-installation/dpucluster.yaml)
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

[embedmd]:#(manifests/01-dpf-system-installation/dpudiscovery.yaml)
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

These verification commands may need to be run multiple times until the condition is met.

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

### 3. Create BlueFieldSoftware and DPUFlavorTemplate

Create a BlueFieldSoftware and DPUFlavorTemplate to be used for the DPU provisioning process

```shell
cat manifests/02-bfsw-and-flavor/* | ../../../../exec_envsubst.sh | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary><b>BlueFieldSoftware and DPUFlavorTemplate</b></summary>

[embedmd]:#(manifests/02-bfsw-and-flavor/bluefieldsoftware.yaml)
```yaml

---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BlueFieldSoftware
metadata:
  name: bf4-$TAG
  namespace: dpf-operator-system
spec:
  osIso: $BFSW_OS_ISO_URL
  pldmFwBundle:
    $PLDM_PSID: $BFSW_FW_BUNDLE_URL
  nicFw: $CXFW_URL
```

[embedmd]:#(manifests/02-bfsw-and-flavor/dpuflavortemplate.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavorTemplate
metadata:
  name: xplane-weave-flavor-template-$TAG
  namespace: dpf-operator-system
spec:
  template: |
    spec:
      grub:
        kernelParameters:
          - console=hvc0
          - console=ttyAMA0
          - net.ifnames=0
          - biosdevname=0
          - iommu.passthrough=1
          - cgroup_no_v1=net_prio,net_cls
          - hugepagesz=2048kB
          - hugepages=500

      nvconfig:
        - device: "*"
          parameters:
            # DPU mode configuration
            - INTERNAL_CPU_MODEL=1
            - INTERNAL_CPU_OFFLOAD_ENGINE=0
            # Eswitch configuration
            - LAG_RESOURCE_ALLOCATION=1
            # Port configuration
            - LINK_TYPE_P1=ETH
            - LINK_TYPE_P2=ETH
            # SR-IOV and SF configuration
            - PF_BAR2_ENABLE=0
            - PER_PF_NUM_SF=1
            - PF_TOTAL_SF=20
            - PF_SF_BAR_SIZE=14
            - NUM_PF_MSIX_VALID=0
            - PF_NUM_PF_MSIX_VALID=1
            - PF_NUM_PF_MSIX=228
            - SRIOV_EN=1
            - NUM_OF_VFS=46

      scalableFunctions:
        - count: 20
          poolName: bf_sf

      ovs:
        rawConfigScript: |
          #!/bin/bash

          # Export common functions
          _ovs-vsctl() {
            ovs-vsctl --timeout 15 "$@"
          }
          export -f _ovs-vsctl

          # 1. Configure OVS
          /etc/mellanox/dpf-setup-ovs.sh

          # 2. Configure E/W underlay
          /etc/mellanox/dpf-setup-ew-underlay.sh

      bfcfgParameters:
        - UPDATE_ATF_UEFI=yes
        - UPDATE_DPU_OS=yes
        - WITH_NIC_FW_UPDATE=yes

      # ewNicConfigurations will change depending on the platform configuration
      ewNicConfigurations:
        - numVfs: 1
          linkType: Ethernet
          rawNvConfig:
            # SR-IOV and SF configuration
            - name: PF_BAR2_ENABLE
              value: "0"
            - name: PER_PF_NUM_SF
              value: "1"
            - name: PF_TOTAL_SF
              value: "20"
            - name: PF_SF_BAR_SIZE
              value: "14"
            - name: NUM_PF_MSIX_VALID
              value: "0"
            - name: PF_NUM_PF_MSIX_VALID
              value: "1"
            - name: PF_NUM_PF_MSIX
              value: "228"
            - name: SRIOV_EN
              value: "1"
            - name: NUM_OF_VFS
              value: "1"
            - name: VF_LOG_BAR_SIZE
              value: "5"
            # Hardware parsing and eswitch configuration
            - name: FLEX_PARSER_PROFILE_ENABLE
              value: "10"
            - name: LAG_RESOURCE_ALLOCATION
              value: "1"
            # Port configuration: breakout, HW plane configuration and link related
            - name: NUM_OF_PF
              value: "2"
            - name: MODULE_SPLIT_M0[0]
              value: "1"
            - name: MODULE_SPLIT_M0[1]
              value: "1"
            - name: MODULE_SPLIT_M0[2]
              value: "1"
            - name: MODULE_SPLIT_M0[3]
              value: "1"
            - name: MODULE_SPLIT_M0[4]
              value: "1"
            - name: MODULE_SPLIT_M0[5]
              value: "1"
            - name: MODULE_SPLIT_M0[6]
              value: "1"
            - name: MODULE_SPLIT_M0[7]
              value: "1"
            - name: MODULE_SPLIT_M0[8]
              value: "FF"
            - name: MODULE_SPLIT_M0[9]
              value: "FF"
            - name: MODULE_SPLIT_M0[10]
              value: "FF"
            - name: MODULE_SPLIT_M0[11]
              value: "FF"
            - name: MODULE_SPLIT_M0[12]
              value: "FF"
            - name: MODULE_SPLIT_M0[13]
              value: "FF"
            - name: MODULE_SPLIT_M0[14]
              value: "FF"
            - name: MODULE_SPLIT_M0[15]
              value: "FF"
            - name: NUM_OF_PLANES_P1
              value: "2"
            - name: LOAD_BALANCE_MODE_P1
              value: "2"
            - name: LINK_TYPE_P1
              value: "ETH"
            - name: KEEP_ETH_LINK_UP_P1
              value: "0"
            # RoCE configuration
            - name: TX_SCHEDULER_LOCALITY_MODE
              value: "2"
            - name: ROCE_ADAPTIVE_ROUTING_EN
              value: "1"
            - name: USER_PROGRAMMABLE_CC
              value: "1"
            - name: ROCE_CC_STEERING_EXT
              value: "2"
            - name: ROCE_RTT_RESP_DSCP_P1
              value: "48"
            - name: ROCE_RTT_RESP_DSCP_MODE_P1
              value: "1"
            # miscellaneous configuration
            - name: RDE_DISABLE
              value: "1"
      configFiles:
        - path: /etc/mellanox/dpf-setup-ovs.sh
          operation: override
          permissions: "0700"
          raw: |
            #!/bin/bash

            # Remove default OVS configuration on the DPU and ensure no leftovers on the OVS kernel side
            for i in $(seq 0 99); do
              ovs-vsctl --if-exists del-br "ovsbr${i}"
            done
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

            # ovs metrics for xplane and weave
            _ovs-vsctl set Open_vSwitch . \
              other_config:flow-metric-labels="to_plane,from_plane,device_name,group,plane" \
              other_config:doca-telemetry-interval="1000" \
              other_config:doca-telemetry-ipc="true" \
              other_config:doca-telemetry-source-id="xplane"

            _ovs-vsctl remove Open_vSwitch . other_config default-datapath-type || true
            if systemctl list-unit-files openvswitch-switch.service &>/dev/null; then
              systemctl restart openvswitch-switch
            elif systemctl list-unit-files openvswitch.service &>/dev/null; then
              systemctl restart openvswitch
            fi

            # Create br-sfc
            _ovs-vsctl --may-exist add-br br-sfc
            _ovs-vsctl set bridge br-sfc datapath_type=netdev
            _ovs-vsctl set bridge br-sfc fail_mode=secure

            # Pre plug p0 and p1 to br-sfc
            dpu_uplinks=(p0 p1)
            for uplink in "${dpu_uplinks[@]}"; do
              _ovs-vsctl --may-exist add-port br-sfc "$uplink"
              _ovs-vsctl set Interface "$uplink" type=dpdk
              _ovs-vsctl set Interface "$uplink" mtu_request=9216
              _ovs-vsctl set Port "$uplink" external_ids:dpf-type=physical
            done

        - path: /etc/mellanox/dpf-setup-ew-underlay.sh
          operation: override
          permissions: "0700"
          raw: |
            #!/bin/bash
            # Add rail bridges
            rail_bridges=(brcx-r0swp0 brcx-r0swp1)
            for bridge in "${rail_bridges[@]}"; do
              _ovs-vsctl --may-exist add-br "$bridge"
              _ovs-vsctl set bridge "$bridge" datapath_type=netdev
              _ovs-vsctl set bridge "$bridge" fail_mode=standalone
              _ovs-vsctl set Interface "$bridge" mtu_request=9216
            done

            # Add xplane bridge
            _ovs-vsctl --may-exist add-br br-xplane
            _ovs-vsctl set bridge br-xplane datapath_type=netdev
            _ovs-vsctl set bridge br-xplane fail_mode=secure

            # Map <rail_id>_<sw_plane_id>_<hw_plane_id> -> matching $RAIL<num>_SWP<NUM>_HWP<num>_NETDEV
            declare -A rail_plane_netdev=(
              [0_0_0]="$RAIL0_SWP0_HWP0_NETDEV"
              [0_0_1]="$RAIL0_SWP0_HWP1_NETDEV"
              [0_1_0]="$RAIL0_SWP1_HWP0_NETDEV"
              [0_1_1]="$RAIL0_SWP1_HWP1_NETDEV"
            )

            # one rail, two SW planes and two HW planes
            rail_ids=(0)
            sw_planes=(0 1)
            hw_planes=(0 1)

            # Construct xplane uplink ports for each rail and plane
            for rail_id in "${rail_ids[@]}"; do
              for sw_plane_id in "${sw_planes[@]}"; do
                for hw_plane_id in "${hw_planes[@]}"; do
                  netdev="${rail_plane_netdev[${rail_id}_${sw_plane_id}_${hw_plane_id}]}"
                  _ovs-vsctl --may-exist add-port br-xplane "$netdev"
                  _ovs-vsctl set Interface "$netdev" type=dpdk
                  _ovs-vsctl set Interface "$netdev" mtu_request=9216
                  _ovs-vsctl set Interface "$netdev" external_ids:xplane=true
                  _ovs-vsctl set Interface "$netdev" external_ids:xplane-group-id=rail${rail_id}swp${sw_plane_id}
                  _ovs-vsctl set Interface "$netdev" external_ids:xplane-uplink=true
                  _ovs-vsctl set Interface "$netdev" external_ids:xplane-plane-id=${hw_plane_id}
                done
              done
            done

            netplan apply

        - path: /etc/netplan/99-rails.yaml
          operation: override
          permissions: "0644"
          raw: |
            # IPs are generated via values set in the DPUDevice CR
            network:
              version: 2
              ethernets:
                brcx-r0swp0:
                  addresses:
                    - {{ .rail0_swp0_ip }}
                  routes:
                    - to: {{ .rail_network_swp0 }}
                      via: {{ .rail0_swp0_gw }}

                brcx-r0swp1:
                  addresses:
                    - {{ .rail0_swp1_ip }}
                  routes:
                    - to: {{ .rail_network_swp1 }}
                      via: {{ .rail0_swp1_gw }}

        - path: /etc/mellanox/mlnx-bf.conf
          operation: override
          permissions: "0644"
          raw: |
            ALLOW_SHARED_RQ="no"
            IPSEC_FULL_OFFLOAD="no"
            ENABLE_ESWITCH_MULTIPORT="yes"
            # dpu-agent controls sf creation; enable via spec.dma.enabled instead
            SNAP_DMA_SF=no

        - path: /etc/mellanox/mlnx-ovs.conf
          operation: override
          permissions: "0644"
          raw: |
            CREATE_OVS_BRIDGES="no"
            OVS_DOCA="yes"

        - path: /etc/mellanox/mlnx-sf.conf
          operation: override
          permissions: "0644"
          raw: ""
```

</details>

> [!NOTE]
> The DPUFlavorTemplate above does not set `ubuntu_PASSWORD` in `bfcfgParameters`. If omitted, DPF defaults to `ubuntu:ubuntu` credentials on the DPU. For production deployments, it is recommended to set a hashed password (see [BFCfg Parameters](../../../../developer-guides/api/dpuflavor.md#bfcfg-parameters)).

> [!NOTE]
> `manifests/misc/dpuflavortemplate-spcx.yaml` provides an alternative DPUFlavorTemplate (`xplane-weave-flavor-template-spcx-$TAG`) that additionally enables `spectrumXOptimized` E/W NIC configuration and ships the Spectrum-X `RA2.2-runtime-only` RoCE/adaptive-routing/congestion-control tuning file. Use it in place of the DPUFlavorTemplate above if your fabric requires Spectrum-X-optimized RoCE congestion control; reference it from `dpudeployment.yaml`'s `spec.dpus.flavorTemplate` instead of the default template.

#### Verification

Wait for the BlueFieldSoftware to be downloaded and ready before proceeding:

```shell
## Ensure the BlueFieldSoftware is ready
kubectl wait --for=jsonpath='{.status.phase}'=Ready --namespace dpf-operator-system bluefieldsoftware bf4-$TAG --timeout=600s
```

### 4. Populate DPUDevices with E/W Network values

In this step we will add E/W Network values to each DPUDevice. these will be used to generate
the correct underlay network configuration on each DPU respectively (values used in the created DPUFlavorTemplate).

```shell
# patch DPUDevices with the number on E/W NICs
kubectl -n dpf-operator-system patch dpudevice --type merge -p "{\"spec\": {\"nicDeviceCount\": $NIC_DEVICE_COUNT}}" $DPU1_SERIAL
kubectl -n dpf-operator-system patch dpudevice --type merge -p "{\"spec\": {\"nicDeviceCount\": $NIC_DEVICE_COUNT}}" $DPU2_SERIAL

# patch DPUDevices with E/W Network values
kubectl -n dpf-operator-system patch dpudevice $DPU1_SERIAL \
  --type=merge -p "{\"spec\":{\"values\":{\"rail0_swp0_ip\":\"$DPU1_RAIL0_SWP0_IP\",\"rail0_swp0_gw\":\"$DPU1_RAIL0_SWP0_GW\", \"rail_network_swp0\":\"$RAIL_NETWORK_SWP0\",
  \"rail0_swp1_ip\":\"$DPU1_RAIL0_SWP1_IP\",\"rail0_swp1_gw\":\"$DPU1_RAIL0_SWP1_GW\",\"rail_network_swp1\":\"$RAIL_NETWORK_SWP1\"}}}"
kubectl -n dpf-operator-system patch dpudevice $DPU2_SERIAL \
  --type=merge -p "{\"spec\":{\"values\":{\"rail0_swp0_ip\":\"$DPU2_RAIL0_SWP0_IP\",\"rail0_swp0_gw\":\"$DPU2_RAIL0_SWP0_GW\", \"rail_network_swp0\":\"$RAIL_NETWORK_SWP0\",
  \"rail0_swp1_ip\":\"$DPU2_RAIL0_SWP1_IP\",\"rail0_swp1_gw\":\"$DPU2_RAIL0_SWP1_GW\",\"rail_network_swp1\":\"$RAIL_NETWORK_SWP1\"}}}"
```

> [!NOTE]
> When allocating `/31` subnets for each of the underlay bridges from the overall subnet (`/12`),
> avoid allocating the first `/31` subnet. Certain dhcp clients, such as `systemd-networkd`, will
> not handle additional routes for the `/12` subnet properly.
> Example: For `172.16.0.0/12` do not allocate `172.16.0.0/31` to any of the DPU underlay bridges.

### 5. XPlane Weave Deployment

#### Deploy DPU cluster components using DPUDeployment

> [!WARNING]
> In case more than 1 DPU exists per node, the relevant selector should be applied in the DPUDeployment
> to select the appropriate DPU. See [DPUDeployment - DPUs Configuration](../../../../developer-guides/api/dpudeployment.md#dpus-configuration)
> to understand more about the selectors.

```shell
cat manifests/03-xplane-weave-dpudeployment/* | ../../../../exec_envsubst.sh | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary><b>XPlane Weave DPUDeployment</b></summary>

[embedmd]:#(manifests/03-xplane-weave-dpudeployment/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: xplane-weave
  namespace: dpf-operator-system
spec:
  dpus:
    astraEnabled: true
    blueFieldSoftware: bf4-${TAG}
    flavorTemplate: xplane-weave-flavor-template-${TAG}
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
    doca-xplane:
      serviceTemplate: doca-xplane
      serviceConfiguration: doca-xplane
    doca-weave-flow-controller:
      serviceTemplate: doca-weave-flow-controller
      serviceConfiguration: doca-weave-flow-controller
    doca-weave-dhcp-agent:
      serviceTemplate: doca-weave-dhcp-agent
      serviceConfiguration: doca-weave-dhcp-agent
  serviceChains:
    upgradePolicy:
      applyNodeEffect: false
    switches:
      - ports:
        - serviceInterface:
            matchLabels:
              interface: p-brcx-r0swp0-to-br-sfc
        - serviceInterface:
            matchLabels:
              interface: p-br-xplane-r0swp0-to-br-sfc
      - ports:
        - serviceInterface:
            matchLabels:
              interface: p-brcx-r0swp1-to-br-sfc
        - serviceInterface:
            matchLabels:
              interface: p-br-xplane-r0swp1-to-br-sfc
```

[embedmd]:#(manifests/03-xplane-weave-dpudeployment/dpuservicetemplate-doca-xplane.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-xplane
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-xplane
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: 3.5.0027
      chart: xplane
```

[embedmd]:#(manifests/03-xplane-weave-dpudeployment/dpuservicetemplate-doca-weave-dhcp-agent.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-weave-dhcp-agent
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-weave-dhcp-agent
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-weave
```

[embedmd]:#(manifests/03-xplane-weave-dpudeployment/dpuservicetemplate-doca-weave-flow-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-weave-flow-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-weave-flow-controller
  helmChart:
    source:
      repoURL: $REGISTRY
      version: $TAG
      chart: dpf-weave
```

[embedmd]:#(manifests/03-xplane-weave-dpudeployment/dpuserviceconfig-doca-xplane.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-xplane
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-xplane
  upgradePolicy:
    applyNodeEffect: false
```

[embedmd]:#(manifests/03-xplane-weave-dpudeployment/dpuserviceconfig-doca-weave-dhcp-agent.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-weave-dhcp-agent
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-weave-dhcp-agent
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    serviceDaemonSet:
      resources:
        nvidia.com/bf_sf: 2
    helmChart:
      values:
        weaveDHCPAgent:
          enabled: true
          dhcpNetworks:
            createNADs: true
            networks:
            - name: dhcp-r0swp0
              bridge: br-dhcp-r0swp0
              resourceName: nvidia.com/bf_sf
              interfaceName: r0swp0
            - name: dhcp-r0swp1
              bridge: br-dhcp-r0swp1
              resourceName: nvidia.com/bf_sf
              interfaceName: r0swp1
```

[embedmd]:#(manifests/03-xplane-weave-dpudeployment/dpuserviceconfig-doca-weave-flow-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-weave-flow-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-weave-flow-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    helmChart:
      values:
        weaveFlowController:
          enabled: true
          underlayConfigMapData:
            nicIDType: mac
            overlayNetworkPrefixLength: $OVERLAY_NETWORK_PREFIX_LENGTH
            softwarePlaneIDBitLength: $SOFTWARE_PLANE_ID_BIT_LENGTH
            railIDBitLength: $RAIL_ID_BIT_LENGTH
            interfaces:
              - pciAddress: $RAIL0_SWP0_PCI_ADDRESS
                underlayInterface: brcx-r0swp0
                overlayDHCPInterface: r0swp0
                dhcpBridgeName: br-dhcp-r0swp0
                dropBridgeName: br-drop-r0swp0
              - pciAddress: $RAIL0_SWP1_PCI_ADDRESS
                underlayInterface: brcx-r0swp1
                overlayDHCPInterface: r0swp1
                dhcpBridgeName: br-dhcp-r0swp1
                dropBridgeName: br-drop-r0swp1
```

[embedmd]:#(manifests/03-xplane-weave-dpudeployment/dpuserviceInterfaces-astra.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p-brcx-r0swp0-to-br-sfc
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "p-brcx-r0swp0-to-br-sfc"
        spec:
          interfaceType: patch
          patch:
            peerBridge: brcx-r0swp0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p-brcx-r0swp1-to-br-sfc
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "p-brcx-r0swp1-to-br-sfc"
        spec:
          interfaceType: patch
          patch:
            peerBridge: brcx-r0swp1
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p-br-xplane-r0swp0-to-br-sfc
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "p-br-xplane-r0swp0-to-br-sfc"
        spec:
          interfaceType: patch
          patch:
            peerBridge: br-xplane
            peerExternalIDs:
              xplane: "true"
              xplane-group-id: rail0swp0
              xplane-downlink: patch
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p-br-xplane-r0swp1-to-br-sfc
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "p-br-xplane-r0swp1-to-br-sfc"
        spec:
          interfaceType: patch
          patch:
            peerBridge: br-xplane
            peerExternalIDs:
              xplane: "true"
              xplane-group-id: rail0swp1
              xplane-downlink: patch
```

</details>

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

After releasing the hold, the DPUs will proceed through the remaining provisioning phases.

#### Make DPUs Ready

In order to make the DPUs ready, the host operation that DPF requests in `DPUNode.status.rebootMethod` must be
performed -- see
[DPUNode: Aggregated reboot method](../../../../developer-guides/api/dpunode.md#aggregated-reboot-method)
for what each value requires. With
[`nodeRebootMethod: external`](../../../../developer-guides/api/dpunode.md#reboot-methods) we perform it manually
when the DPU object gives us the signal; the described flow can be automated by the administrator depending on the
infrastructure.

These verification commands may need to be run multiple times as during provisioning a DPU may need to be rebooted
several times.

**1.** Wait for DPU `Rebooting` phase and `status.rebootStatus.reason=WaitingForManualPowerCycleOrReboot`

```shell
kubectl wait --timeout 1h --namespace dpf-operator-system dpu --all --for=jsonpath='{.status.phase}'=Rebooting
kubectl wait --timeout 1h --namespace dpf-operator-system dpu --all --for=jsonpath='{.status.rebootStatus.reason}'=WaitingForManualPowerCycleOrReboot
```

**2.** Check which operation each host needs - see
[DPUNode: Aggregated reboot method](../../../../developer-guides/api/dpunode.md#aggregated-reboot-method)

```shell
kubectl -n dpf-operator-system get dpunode -o custom-columns='NAME:.metadata.name,REBOOT_METHOD:.status.rebootMethod'
```

**3.** Perform that operation on the DPU worker hosts - manual operation by the user

```shell
# Example using ipmitool. Confirm that it performs the operation reported in status.rebootMethod
# on your platform -- on some platforms it does not, see the DPUNode reference above.
ipmitool -I lanplus -H <host-bmc-ip>  -U <username> -P <password>  chassis power cycle
```

**4.** Once all nodes have rebooted, remove `provisioning.dpu.nvidia.com/dpunode-external-reboot-required` annotation from `DPUNodes`

```shell
kubectl -n dpf-operator-system annotate dpunode --all provisioning.dpu.nvidia.com/dpunode-external-reboot-required-
```

**5.** Ensure `DPUs` are ready

```shell
kubectl wait --for=condition=ready --namespace dpf-operator-system dpus --all
```

#### Validate deployed DPUServices

These verification commands may need to be run multiple times until the condition is met.

```shell
kubectl wait --for=condition=ready --namespace dpf-operator-system dpudeployment xplane-weave
```

or with `dpfctl`:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
NAME                                                     NAMESPACE            STATUS       REASON    SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig                      dpf-operator-system  Ready: True  Success   6m43s
└─DPUDeployments
  └─DPUDeployment/xplane-weave                           dpf-operator-system  Ready: True  Success   3m40s
    ├─DPUServiceChains
    │ └─DPUServiceChain/xplane-weave-79d7l               dpf-operator-system  Ready: True  Success   7m12s
    ├─DPUSets
    │ └─DPUSet/xplane-weave-dpuset1                      dpf-operator-system  Ready: True  Success   3m40s
    │   ├─DPUNodes
    │   │ └─2 DPUNodes...                                dpf-operator-system  Ready: True  Ready     34m    See dpu-node-mt2610604v1p, dpu-node-mt2614604ee5
    │   └─DPUs
    │     └─2 DPUs...                                    dpf-operator-system  Ready: True  DPUReady  34m    See dpu-node-mt2610604v1p-mt2610604v1p, dpu-node-mt2614604ee5-mt2614604ee5
    └─Services
      ├─DPUServiceTemplates
      │ ├─DPUServiceTemplate/doca-weave-dhcp-agent       dpf-operator-system  Ready: True  Success   147m
      │ ├─DPUServiceTemplate/doca-weave-flow-controller  dpf-operator-system  Ready: True  Success   147m
      │ └─DPUServiceTemplate/doca-xplane                 dpf-operator-system  Ready: True  Success   147m
      └─DPUServices
        └─3 DPUServices...                               dpf-operator-system  Ready: True  Success   7m5s   See doca-weave-dhcp-agent-m7kx4, doca-weave-flow-controller-nrqd7, doca-xplane-4n5m7
```

### 6. Optional - Configure network isolation for E/W NICs and send traffic

**In this step we will:**

* Configure network isolation for E/W NICs on both workers with a single virtual network (VNI)
  * PF0 from each NIC will be used for attachment to the virtual network
* Obtain IPs using DHCP for those PFs
* Send traffic through those PFs

The following information is required (adapt to fit your setup):

```shell
# PF0 netdev name on the hosts (assuming same name on both workers)
NIC0_PF_NAME=ens6np0
NIC1_PF_NAME=ens5np0
# PF0 rep netdev name for NIC 0 and NIC 1 (on the DPU)
NIC0_PF0_REP=A1c1pf0
NIC1_PF0_REP=A11c1pf0

# weave-flow-controller pods scheduled on the DPUs
WEAVE_FLOW_CONTROLLER_PODS=$(kubectl get pods -n dpf-operator-system \
  -l app.kubernetes.io/name=weave-flow-controller \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort)

# virtual network configuration
VNET_OVERLAY_SUBNET=44.16.0.0/12
VNET_VNI=100
```

#### Setup network isolation using DOCA Weave

The commands below are executed against the DPU K8s cluster ([how to access dpucluster](additional-considerations.md#accessing-the-dpucluster)).

```shell
# Create the virtual network and attachments on each DPU
for pod in $WEAVE_FLOW_CONTROLLER_PODS; do
  kubectl exec -n dpf-operator-system "$pod" -- /vpcctl create-vnet --id shared-vnet --subnet-v4 "$VNET_OVERLAY_SUBNET" --vni "$VNET_VNI"
  kubectl exec -n dpf-operator-system "$pod" -- /vpcctl create-attachment --id "$NIC0_PF0_REP" --vnet-id shared-vnet --type pf --rep "$NIC0_PF0_REP"
  kubectl exec -n dpf-operator-system "$pod" -- /vpcctl create-attachment --id "$NIC1_PF0_REP" --vnet-id shared-vnet --type pf --rep "$NIC1_PF0_REP"
done
```

#### Obtain IPs using DHCP

We will place each NIC PF netdev interface into its own VRF to avoid conflicting routes between the two NIC interfaces.
This is done since both will have a route advertised by the DHCP server to the same `/12` overlay subnet as each interface may egress to the same overlay subnet.
Applications that are VRF aware can egress from any of those interfaces.

> [!NOTE]
> Weave overlay IPv4 leases are `/31`. When Spectrum-X **host-id is 0**, the overlay address is `*.*.*.0`
> (for example `44.16.0.0/31`). systemd-networkd then installs option 121 destinations with `scope host`
> instead of via the `/31` peer. Use NetworkManager on these data-path interfaces.
> See [Host DHCP clients for Weave overlay addressing and routes](dhcp-client-guidance.md).
> NetworkManager must be installed and running on the workers.
> The VRF masters need no address, the `networkmanager.passthrough` keys keep NetworkManager from giving them a
> `169.254.x.y/16` link-local address.

```shell
# Create the below netplan file on both worker nodes
cat > /etc/netplan/99-rails.yaml << EOF
network:
  version: 2
  ethernets:
    $NIC0_PF_NAME:
      renderer: NetworkManager
      dhcp4: true
      mtu: 9000
    $NIC1_PF_NAME:
      renderer: NetworkManager
      dhcp4: true
      mtu: 9000
  vrfs:
    rail0swp0:
      renderer: NetworkManager
      table: 100
      interfaces: [$NIC0_PF_NAME]
      networkmanager:
        passthrough:
          ipv4.method: "disabled"
          ipv4.link-local: "disabled"
          ipv6.method: "disabled"
    rail0swp1:
      renderer: NetworkManager
      table: 101
      interfaces: [$NIC1_PF_NAME]
      networkmanager:
        passthrough:
          ipv4.method: "disabled"
          ipv4.link-local: "disabled"
          ipv6.method: "disabled"
EOF
netplan apply

```

#### Send traffic

Get the assigned IP address from each interface and send traffic to the other node

**Example:**

```shell
# same rail sw plane 0
ip vrf exec rail0swp0 ping 44.16.0.4
# same rail sw plane 1
ip vrf exec rail0swp1 ping 44.24.0.4
```

#### Cleanup netplan files and configuration

```shell
rm /etc/netplan/99-rails.yaml
netplan apply
ip link del rail0swp0
ip link del rail0swp1
```

#### Teardown network isolation using DOCA Weave

The commands below are executed against the DPU K8s cluster ([how to access dpucluster](additional-considerations.md#accessing-the-dpucluster)).

```shell
# Cleanup the virtual network and attachments on each DPU
for pod in $WEAVE_FLOW_CONTROLLER_PODS; do
  kubectl exec -n dpf-operator-system "$pod" -- /vpcctl delete-attachment --id "$NIC0_PF0_REP"
  kubectl exec -n dpf-operator-system "$pod" -- /vpcctl delete-attachment --id "$NIC1_PF0_REP"
  kubectl exec -n dpf-operator-system "$pod" -- /vpcctl delete-vnet --id shared-vnet
done
```

## Uninstall

This section covers only the DPF related components and not the prerequisites as these must be managed by the administrator.

### 1. Remove XPlane Weave deployment resources

```shell
cat manifests/03-xplane-weave-dpudeployment/* | ../../../../exec_envsubst.sh | kubectl delete -f -
kubectl wait --timeout 5m --for=delete -n dpf-operator-system dpudeployment/xplane-weave
```

### 2. Remove DPF System and Operator Installation

```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

> [!NOTE]
> There can be a race condition with deleting the underlying Kamaji cluster which runs the DPU cluster control
> plane in this guide. If that happens it may be necessary to remove finalizers manually from `DPUCluster` and `Datastore`
> objects.
