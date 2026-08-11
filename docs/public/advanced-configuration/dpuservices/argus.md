---
title: "DOCA Argus Service"
---

This documentation explains configuration and deployment of DOCA Argus service as DPUService in DPF.

Main Argus concepts are explained in the
[official DOCA Argus documentation](https://docs.nvidia.com/doca/sdk/doca+argus+service+guide/index.html).  
The official documentation provides a more comprehensive overview, DPUService users should consult it for detailed
explanation of service configuration.

The DOCA Argus usecase in DPF is container threat detection in AI workloads and microservices, utilizing a Bluefield DPU
to perform live machine introspection at the hardware level.

# Service Components

Argus component runs on the DPU and analyzes specific snippets of volatile memory directly, providing attested insights
into the operation of various workloads, whether they are bare-metal, virtualized, or containerized. By default, Argus
scans all systems of the host, so for scanning specific systems only refer to [official DOCA Argus documentation](https://docs.nvidia.com/doca/sdk/doca+argus+service+guide/index.html).

# IOMMU Kernel Parameters Requirements

## Virtualized environments (running inside a VM)

You must set **both** of the following kernel parameters on the **host**:

* `intel_iommu=on` **or** `amd_iommu=on`  
* `iommu=pt`

> Example: `intel_iommu=on iommu=pt`

## Bare-metal environments (running directly on hardware)

You have **two valid options**:

1. **Disable IOMMU completely**  
   - `intel_iommu=off` **or** `amd_iommu=off`

2. **Enable IOMMU with passthrough**  
   - `intel_iommu=on` **or** `amd_iommu=on`  
   - `iommu=pt`

For more details, refer to the [official NVIDIA DOCA Argus documentation](https://docs.nvidia.com/doca/sdk/doca+argus+service+guide/index.html).


# Configuration

Configuration files:
   
<details markdown="1"><summary><b>DPUServiceConfiguration</b></summary>

[embedmd]:#(../../../../dpuservices/argus/DPUServiceConfiguration.yaml)
```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: argus
  namespace: dpf-operator-system
spec:
  deploymentServiceName: argus
  serviceConfiguration:
    helmChart:
      values:
        config:
          isLocalPath: false
        containerImage: nvcr.io/nvidia/doca/doca_argus:1.5.0-doca3.5.0
```

</details>

<details markdown="1"><summary><b>DPUServiceTemplate</b></summary>

[embedmd]:#(../../../../dpuservices/argus/DPUServiceTemplate.yaml)
```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: argus
  namespace: dpf-operator-system
spec:
  deploymentServiceName: argus
  helmChart:
    source:
      chart: doca-argus
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: 1.5.0
```
</details>

The general resources are:

#### DPUFlavor

Defines the DPU flavor for the Argus service.
<details markdown="1"><summary><b>DPUFlavor</b></summary>

[embedmd]:#(../../../../dpuservices/argus/DPUFlavor.yaml)
```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: dpf-provisioning-argus
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
      - net.ifnames=0
      - biosdevname=0
      - iommu.passthrough=1
      - cgroup_no_v1=net_prio,net_cls
      - hugepagesz=2048kB
      - hugepages=250
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
```
</details>

> [!NOTE]
> The DPUFlavor above does not set `ubuntu_PASSWORD` in `bfcfgParameters`. If omitted, DPF defaults to `ubuntu:ubuntu` credentials on the DPU. For production deployments, it is recommended to set a hashed password (see [BFCfg Parameters](../../developer-guides/api/dpuflavor.md#bfcfg-parameters)).

#### DPUDeployment

Defines the DPUDeployment for the Argus service.
<details markdown="1"><summary><b>DPUDeployment</b></summary>

[embedmd]:#(../../../../dpuservices/argus/DPUDeployment.yaml)
```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: argus
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    dpuSets:
    - nameSuffix: dpuset-argus
      dpuNodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
    flavor: dpf-provisioning-argus
    nodeEffect:
      drain: true
    dpuSetStrategy:
      type: RollingUpdate
  serviceChains:
    switches:
    - ports:
      - serviceInterface:
          matchLabels:
            uplink: p0
    upgradePolicy:
      applyNodeEffect: true
  services:
    argus:
      serviceConfiguration: argus
      serviceTemplate: argus
```
</details>

# Configuration

[Official Argus documentation](https://docs.nvidia.com/doca/sdk/doca+argus+service+guide/index.html) explains
configuration options.

# DPUDeployment

The complete `DPUDeployment` configuration is in [DPUDeployment.yaml](#dpudeployment).

# Output

Argus offers multiple ways to get events, that includes logs to stdout, log files and telemtry records in json or syslog formats.
