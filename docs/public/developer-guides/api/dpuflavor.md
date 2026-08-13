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
- **Cluster deployment mode**: Zero-trust vs host-trusted is configured on `DPFOperatorConfig` and reflected on `DPU` status (`deploymentMode`), not on `DPUFlavor`
- **Template Reusability**: Can be applied to multiple DPUs for consistent configuration

## API Reference

### DPUFlavorSpec

| Field | Type | Description |
|-------|------|--------------|
| `grub` | [DPUFlavorGrub](#dpuflavorgrub) | All the parameters will be set in `GRUB_CMDLINE_LINUX` grub configuration |
| `sysctl` | [DPUFlavorSysctl](#dpuflavorsysctl) | Kernel sysctl parameters which will be stored in `/etc/sysctl.d/99-dpf.conf` |
| `nvconfig` | [][NVConfig](#nvconfig) | The device configuration which will be applied by `mlxconfig` |
| `ovs` | [DPUFlavorOVS](#dpuflavorovs) | Open vSwitch configuration which will be executed via systemd service |
| `bfcfgParameters` | []string | Parameters for the bf.cfg file. See [BFCfg Parameters](#bfcfg-parameters) for important parameters |
| `configFiles` | [][ConfigFile](#configfile) | Custom configuration files. Users can use this configuration to overwrite files in the DPU file system or add content to existing files |
| `containerdConfig` | [ContainerdConfig](#containerdconfig) | ContainerdConfig contains the configuration for containerd |
| `dpuResources` | ResourceList | Minimum resources needed for BFB installation |
| `systemReservedResources` | ResourceList | Resources reserved for system use |
| `hostNetworkInterfaceConfigs` | [][NetworkInterfaceConfig](#networkinterfaceconfig) | Host-side network interface configuration |
| `scalableFunctions` | [ScalableFunctions](#scalablefunctions) | Configures Scalable Functions (SFs) created on the DPU |
| `hostOSInit` | [HostOSInit](#hostosinit) | Selects the readiness gate that releases the host from the firmware host-OS-init hold |

### HostOSInit

Configures when the DPU agent releases the host from the `DELAY_HOST_OS_INIT` firmware hold.

| Field | Type | Description |
|-------|------|--------------|
| `releaseAfter` | [HostOSInitReleaseAfter](#hostosinitreleaseafter) | Selects the release gate. Defaults to `dpuServiceCriticalPodsReady` when unset |

The hold itself is enabled by setting `DELAY_HOST_OS_INIT=0x3` in [NVConfig](#nvconfig), not by this field, and is only
supported in Zero Trust. For the generated field reference see [API Reference](api.md), and for configuration guidance,
gate selection, and troubleshooting see
[Host OS Init Release](../../advanced-configuration/host-os-init-release.md).

### HostOSInitReleaseAfter

A one-of selector: exactly one field must be set, and each is an empty object that only marks the choice.

| Field | Type | Description |
|-------|------|--------------|
| `dpuServiceCriticalPodsReady` | HostOSInitGate | Releases the host once `DPU.status.operationalConditions[DPUServiceCriticalPodsReady]` is `True`. Applied by default when `releaseAfter` is unset |
| `operationalReady` | HostOSInitGate | Releases the host once `DPU.status.operationalConditions[OperationalReady]` is `True`, which also requires node problems, service interfaces, service chains and non-critical pods to be healthy |

### ScalableFunctions

| Field | Type | Description |
|-------|------|--------------|
| `dma` | [DMAScalableFunction](#dmascalablefunction) | Configures the DMA SF used by SNAP on BlueField-4 socket-direct systems |

### DMAScalableFunction

| Field | Type | Description |
|-------|------|--------------|
| `enabled` | *bool | **Enables DMA SF creation by the dpu-agent.** The presence of the `dma` struct alone does not enable it; `enabled` must be set explicitly. Only takes effect on BlueField-4 socket-direct systems |
| `sfNum` | *int32 | The DMA SF's number. Defaults to `8000` when unset. Only takes effect on BlueField-4 socket-direct systems |
| `macAddress` | *string | Pins the DMA SF's MAC (canonical colon-separated 48-bit form). Defaults to a deterministic, vendor-compatible derivation when unset |

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

### DPUFlavorFileOp

- `override`: Replace file content entirely
- `append`: Append to existing file content

## BFCfg Parameters

The `bfcfgParameters` field accepts a list of `KEY=VALUE` strings that are written directly into the
`bf.cfg` file consumed by the BFB installer. Common parameters include:

| Parameter | Description |
|-----------|-------------|
| `UPDATE_ATF_UEFI` | Update ATF/UEFI firmware during provisioning (`yes`/`no`) |
| `UPDATE_DPU_OS` | Update the DPU operating system (`yes`/`no`) |
| `WITH_NIC_FW_UPDATE` | Update NIC firmware during provisioning (`yes`/`no`) |
| `ubuntu_PASSWORD` | Password hash for the `ubuntu` admin account on the DPU. **If not set, DPF defaults to the well-known `ubuntu:ubuntu` credentials.** It is strongly recommended to set this to a unique hashed password for every deployment |

> [!NOTE]
> `ubuntu_PASSWORD` is filtered out of the generated `bf.cfg` and applied via cloud-init instead; all other parameters are written directly into the `bf.cfg` file.

To generate a SHA-512 password hash:

```bash
openssl passwd -6 'YourPassword'
```

Always use `-6` (SHA-512). Do **not** use `-1` (MD5), which is considered insecure.

For the full list of supported `bf.cfg` parameters, see the
[BlueField BSP documentation](https://networking-docs.nvidia.com/bsp/4.15.0/customizing-bluefield-software-deployment).

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

## DPU Node Label Scripts

The DPU agent can run executable files on the DPU ARM and report their output as labels on the corresponding DPU cluster Node. This allows DPU-side hardware or software properties to be surfaced into Kubernetes scheduling decisions without any host-side tooling.

### How it works

1. On every provisioning run, the DPU agent scans the directory `/var/lib/dpf/dpuagent/node-label-scripts/` on the DPU.
2. Each regular, executable file in that directory is run with a 30-second timeout. Shell scripts and compiled binaries are supported.
3. Each non-empty line the file prints on stdout must have the form `<label-key-suffix>=<label-value>` and produces one Node label. The key suffix is namespaced under the `scripts.dpu.nvidia.com/` prefix. A single file can therefore report any number of labels.
4. Labels from previous runs that are no longer emitted are automatically deleted from the Node.

**Requirements:**

- Every non-empty stdout line must contain a `=`. Blank lines are ignored, and leading/trailing whitespace around the key and the value is trimmed.
- The key suffix must be a valid Kubernetes label key suffix (alphanumeric, `-`, `_`, `.`; max 63 characters).
- The value must be a valid Kubernetes label value.
- The file name is not part of the label key and has no naming constraints.
- The file must be a regular file with at least one executable bit set (`chmod +x`).
- Directories in the scripts directory are silently ignored.
- If the same label key is emitted more than once, the last occurrence wins. Files run in sorted file name order, and within a file later lines win over earlier ones.

### Deploying scripts via DPUFlavor

Use the `configFiles` field to place executable files into the default directory during provisioning:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: my-flavor
  namespace: dpf-operator-system
spec:
  configFiles:
    - operation: override
      path: /var/lib/dpf/dpuagent/node-label-scripts/network-info
      permissions: "0700"
      raw: |
        #!/bin/bash
        echo "test-label=some-data"
        echo "link-speed=200G"
```

This example produces the Node labels `scripts.dpu.nvidia.com/test-label=some-data` and `scripts.dpu.nvidia.com/link-speed=200G` on the DPU cluster Node.

> [!NOTE]
> Executables run during the dpuagent provisioning step on the DPU ARM, as root, with access to the DPU's hardware interfaces.

## Best Practices

1. **Resource Planning**: Always specify `dpuResources` and `systemReservedResources` to ensure proper resource allocation
2. **Immutability**: Plan your configuration carefully as DPUFlavor specs cannot be modified after creation
3. **Testing**: Test DPUFlavor configurations in development environments before production deployment
4. **Documentation**: Document custom configurations and their purposes for team understanding
5. **IB Mode Conversion**: For DPUs initially in InfiniBand (IB) mode, always include `LINK_TYPE_P1=ETH` in nvconfig parameters to convert to Ethernet mode. For dual port DPUs, also add `LINK_TYPE_P2=ETH`
6. **NVConfig**: Use wildcard (`device: '*'`) for uniform configuration across all devices. Use device-specific entries only when per-device configuration is required
7. **DPU Credentials**: Set `ubuntu_PASSWORD` in `bfcfgParameters` to a strong hashed password. If omitted, the DPU is provisioned with default `ubuntu:ubuntu` credentials.
