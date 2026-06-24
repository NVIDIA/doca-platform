---
title: "DPUFlavor"
---

[[_TOC_]]

## Overview

DPUFlavor is a Kubernetes Custom Resource Definition (CRD) that defines configuration templates for DPU system-level settings. It serves as a blueprint that specifies how DPUs should be configured during provisioning, including kernel parameters, firmware settings, OVS configuration, network interfaces, and resource allocation.

## API Version

- **API Group**: `provisioning.dpu.nvidia.com`
- **API Version**: `v1alpha1`
- **Kind**: `DPUFlavor`

## Key Features

- **Immutable Configuration**: Once created, the DPUFlavor spec cannot be modified to ensure consistency across DPU deployments
- **Comprehensive System Configuration**: Covers all aspects of DPU system configuration from boot parameters to runtime settings
- **Resource Management**: Defines resource requirements and allocation policies
- **Multiple DPU Modes**: Supports both standard DPU mode and zero-trust mode
- **Template Reusability**: Can be applied to multiple DPUs for consistent configuration

## API Reference

### DPUFlavorSpec

| Field | Type | Description |
|-------|------|--------------|
| `grub` | [DPUFlavorGrub](#dpuflavorgrub) | All the parameters will be set in `GRUB_CMDLINE_LINUX` grub configuration |
| `sysctl` | [DPUFlavorSysctl](#dpuflavorsysctl) | Kernel sysctl parameters which will be stored in `/etc/sysctl.d/99-dpf.conf` |
| `nvconfig` | [][NVConfig](#nvconfig) | The device configuration which will be applied by `mlxconfig` |
| `ovs` | [DPUFlavorOVS](#dpuflavorovs) | Open vSwitch configuration which will be executed via systemd service |
| `bfcfgParameters` | []string | Parameters for bf.cfg file |
| `configFiles` | [][ConfigFile](#configfile) | Custom configuration files. Users can use this configuration to overwrite files in the DPU file system or add content to existing files |
| `containerdConfig` | [ContainerdConfig](#containerdconfig) | ContainerdConfig contains the configuration for containerd |
| `dpuResources` | ResourceList | Minimum resources needed for BFB installation |
| `systemReservedResources` | ResourceList | Resources reserved for system use |
| `dpuMode` | [DpuModeType](#dpumodetype) | DPU operation mode. Defaults to `zero-trust` when using Redfish install interface, otherwise defaults to `dpu` |
| `hostNetworkInterfaceConfigs` | [][NetworkInterfaceConfig](#networkinterfaceconfig) | Host-side network interface configuration |

### DPUFlavorGrub

| Field | Type | Description |
|-------|------|--------------|
| `kernelParameters` | []string | Kernel boot parameters to be set in grub configuration |

### DPUFlavorSysctl

| Field | Type | Description |
|-------|------|--------------|
| `parameters` | []string | Sysctl parameters to be applied |

### NVConfig

| Field | Type | Description |
|-------|------|--------------|
| `device` | *string | Target device: `"*"` (all devices), `"p0"`/`"P0"` (port 0), or `"p1"`/`"P1"` (port 1). Case-insensitive. |
| `parameters` | []string | Firmware parameters in `KEY=VALUE` format. 1-32 params, max 200 chars each. |
| `hostPowerCycleRequired` | *bool | Whether host power cycle is needed after applying config(Deprecated) |

**Validation Constraints:**
- Maximum of 3 nvconfig entries per DPUFlavor (one per device: `*`, `p0`/`P0`, `p1`/`P1`)
- Wildcard device (`"*"`) must be the sole entry when specified (no mixing with port-specific entries)
- Device identifiers must be unique with case-insensitive matching (e.g., `p0` and `P0` are duplicates)
- Parameters: 1-32 entries per device, each formatted as `KEY=VALUE` with no spaces allowed

#### IB Mode to Ethernet Mode Configuration

**Example for single port DPU:**

```yaml
nvconfig:
  - device: '*'
    parameters:
      - LINK_TYPE_P1=ETH
```

**Example for dual port DPU:**

```yaml
nvconfig:
  - device: '*'
    parameters:
      - LINK_TYPE_P1=ETH
      - LINK_TYPE_P2=ETH
```

**Device-specific configuration (per-port):**

```yaml
nvconfig:
  - device: 'p0'
    parameters:
      - LINK_TYPE_P1=ETH
      - NUM_OF_VFS=8
  - device: 'p1'
    parameters:
      - LINK_TYPE_P1=IB
```

### DPUFlavorOVS

| Field | Type | Description |
|-------|------|--------------|
| `rawConfigScript` | string | Raw OVS configuration script |

### ConfigFile

| Field | Type | Description |
|-------|------|--------------|
| `path` | string | File path on the DPU |
| `operation` | [DPUFlavorFileOp](#dpuflavorfileop) | File operation type (`override` or `append`) |
| `raw` | string | File content |
| `permissions` | string | File permissions (e.g., `"0644"`) |

### ContainerdConfig

| Field | Type | Description |
|-------|------|--------------|
| `registryEndpoint` | string | Container registry endpoint |

### NetworkInterfaceConfig

| Field | Type | Description |
|-------|------|--------------|
| `mtu` | *int32 | MTU value (1280-9216) |
| `dhcp` | *bool | Enable DHCP configuration |
| `portNumber` | int32 | Port identifier (0 or 1) |
| `nvconfig` | *[NVConfig](#nvconfig) | Port-specific NVConfig settings |

## Enumerations

### DpuModeType

- `dpu`: Standard DPU mode
- `zero-trust`: Zero-trust security mode

**Default Behavior**: When `dpuMode` is not specified in the DPUFlavor, the system automatically determines the appropriate mode based on the DPF deployment configuration:
- If DPF is configured with Redfish install interface (Zero Trust deployment), defaults to `zero-trust`
- Otherwise, defaults to `dpu`

This ensures that DPUs are provisioned with the correct security mode matching the deployment type without requiring explicit configuration.

### DPUFlavorFileOp

- `override`: Replace file content entirely
- `append`: Append to existing file content

## Resource Management

### DPU Resources

The `dpuResources` field specifies the minimum resources required for a BFB with this flavor to be installed on a DPU. 
Using this field, the controller can understand if that flavor can be installed on a particular DPU. It should be set to the total amount of resources the system needs + the resources that should be made available for DPUServices to consume:

```yaml
dpuResources:
  cpu: 16
  memory: 16Gi
  nvidia.com/sf: 20
```

### System Reserved Resources

The `systemReservedResources` field indicates resources consumed by the system (OS, OVS, DPF system etc) and are not made available for DPUServices to consume. DPUServices can consume the difference between `dpuResources` and `systemReservedResources`. This field must not be specified if `dpuResources` are not specified.:

```yaml
systemReservedResources:
  cpu: 4
  memory: 4Gi
  nvidia.com/sf: 4
```

The difference between `dpuResources` and `systemReservedResources` is available for DPUServices.

## Examples

### HBN-OVN DPUFlavor

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: hbn-ovn
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
  dpuMode: dpu
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
  hostNetworkInterfaceConfigs:
  - dhcp: true
    mtu: 1500
    portNumber: 0
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
      _ovs-vsctl --may-exist add-br br-hbn
      _ovs-vsctl set bridge br-hbn datapath_type=netdev
      _ovs-vsctl set bridge br-hbn fail_mode=secure
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
```

## Best Practices

1. **Resource Planning**: Always specify `dpuResources` and `systemReservedResources` to ensure proper resource allocation
2. **Immutability**: Plan your configuration carefully as DPUFlavor specs cannot be modified after creation
3. **Testing**: Test DPUFlavor configurations in development environments before production deployment
4. **Documentation**: Document custom configurations and their purposes for team understanding
5. **IB Mode Conversion**: For DPUs initially in InfiniBand (IB) mode, always include `LINK_TYPE_P1=2` in nvconfig parameters to convert to Ethernet mode. For dual port DPUs, also add `LINK_TYPE_P2=2`
6. **NVConfig**: Use wildcard (`device: '*'`) for uniform configuration across all devices. Use device-specific entries only when per-device configuration is required
