---
title: "DPF System Prerequisites for Host Trusted"
---

[[_TOC_]]

DPF makes a number of assumptions about the hardware, software and networking of the machines it runs on. Some of the
specific [user guides](../README.md) add their own requirements.

## Hardware Setup

There is a high availability control plane machines serving many worker nodes in a cluster running DPF.

### Control Plane Machines

Each control plane machine:

* May be virtualized
* x86_64 or ARM64 (aarch64) architecture
* 16 GB RAM
* 8 CPUs
* If DPUs are installed, they must be in NIC mode (see [Control Plane Nodes with BlueField DPUs](#control-plane-nodes-with-bluefield-dpus))

### Worker Machines

Each worker machine:

* Bare metal - no virtualization
* x86_64 architecture
* 16 GB RAM
* 8 CPUs
* Any number of DPUs

#### DPUs

* Bluefield 3
* 32 GB memory
* Flashed with NVIDIA BFB with DOCA version 2.5 or higher
* Out-of-band management port is not used
* PCIe ATX power cable connected when required by the DPU model

### Control Plane Nodes with BlueField DPUs

Control plane nodes with BlueField DPUs require two configuration steps:
1. **Hardware Configuration**: DPUs must be in NIC mode (Arm cores disabled)
2. **DPF Configuration**: Node selector to prevent DPF from provisioning control plane DPUs

<details markdown="1">
<summary><b>Prerequisites</b></summary>

The DPU NIC mode setup script (below) validates these requirements:

- Root/sudo access on control plane hosts
- MFT tools installed (`mst`, `mlxconfig`)
- ipmitool installed and IPMI accessible locally (BMC configured; `ipmi_devintf`, `ipmi_si` kernel modules loaded)
- BlueField DPUs present on the system
</details>

<details markdown="1">
<summary><b>Assumptions</b></summary>

The DPU NIC mode setup script (below) does not validate these (ensure they are met):

- Zero-Trust mode disabled (see troubleshooting if errors occur)
- Script run **before** Kubernetes deployment
- Host can reboot (script triggers a host cold power cycle via IPMI; expect downtime and impact to all DPUs/workloads on the node)
</details>

<details markdown="1">
<summary><b>DPU NIC Mode Setup Script</b></summary>

Locate and run the DPU NIC mode setup script on each control plane node:

```bash
# Copy the script from your local repository:
cp <repo-path>/hack/scripts/dpu-control-plane-setup.sh .
chmod +x dpu-control-plane-setup.sh
```

The script is available in the repository at: `hack/scripts/dpu-control-plane-setup.sh`

**Quick Start**:
```bash
# Check current DPU modes (dry run)
sudo ./dpu-control-plane-setup.sh --dry-run

# Configure and reboot (default)
sudo ./dpu-control-plane-setup.sh

# Configure without immediate reboot
sudo ./dpu-control-plane-setup.sh --no-reboot
```

**Options**:
- `--dry-run`: Check current DPU modes without making any changes
- `--no-reboot`: Configure DPUs but skip automatic reboot (you must reboot manually later)
- `--help`: Display usage information

**Verification**: After reboot, verify all DPUs are in NIC mode:
```bash
sudo ./dpu-control-plane-setup.sh --dry-run
# Should report: "All DPUs already in NIC mode"
```
</details>

<details markdown="1">
<summary><b>DPF Configuration: Prevent Provisioning on Control Plane Nodes</b></summary>

To prevent DPF from provisioning DPUs on control plane nodes, use node selectors.

**Option 1: DPUSet**
```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: dpuset-workers
  namespace: dpf-operator-system
spec:
  dpuNodeSelector:
    matchExpressions:
      - key: node-role.kubernetes.io/control-plane
        operator: DoesNotExist
  # ... other spec fields
```

**Option 2: DPUDeployment**
```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: my-deployment
spec:
  dpus:
    dpuSets:
      - nameSuffix: workers
        nodeSelector:
          matchExpressions:
            - key: node-role.kubernetes.io/control-plane
              operator: DoesNotExist
    # ... other dpus spec fields
  # ... services spec
```

**Verification**:
```bash
# Verify control plane nodes have the label
kubectl get nodes -L node-role.kubernetes.io/control-plane

# Verify DPUNodes inherited the label
kubectl get dpunode -n dpf-operator-system -L node-role.kubernetes.io/control-plane

# Verify no DPUs are created on control plane nodes
kubectl get dpu -n dpf-operator-system -o custom-columns=NAME:.metadata.name,NODE:.spec.nodeName
```
</details>

<details markdown="1">
<summary><b>Troubleshooting</b></summary>

**IPMI/BMC issues**:
- **IPMI not accessible**: 
  - Check kernel modules: `lsmod | grep -E 'ipmi_(devintf|si)'`
  - Test local BMC: `ipmitool -I open chassis power status`
  - Load modules if needed: `modprobe ipmi_devintf ipmi_si`
  - Verify BMC configuration via BIOS/UEFI settings

**MFT/MST issues**:
- **MFT tools missing**: Install from https://network.nvidia.com/products/adapter-software/firmware-tools/ (>=4.33.0-169)
- **MST service**: Ensure MST is running: `sudo mst start` and verify devices: `mst status`

**Other issues**:
- **Zero-Trust mode**: Disable via mlxprivhost/BMC/Redfish, then re-run script
- **Power cycle timeout/hang**: Manual power-cycle may be required
- **Labels missing on DPUNodes**: Verify K8s node labels, check provisioning-controller logs
</details>

<details markdown="1">
<summary><b>Automation Example (Ansible)</b></summary>

```yaml
- name: Configure control plane DPUs
  hosts: control_plane
  become: yes
  serial: 1
  tasks:
    - name: Run DPU setup
      shell: |
        cat > /tmp/dpu-setup.sh << 'EOF'
        [paste script]
        EOF
        chmod +x /tmp/dpu-setup.sh
        /tmp/dpu-setup.sh
```
</details>

## System Software Setup

### Worker Machines

* `In-Band Manageability Interface` enabled in BIOS
* rshim package is not installed

### Kubernetes

* Kubernetes 1.33 - 1.35
* Control plane nodes have the labels `"node-role.kubernetes.io/control-plane" : ""`

> [!NOTE]
> Only multi-master (high-availability) Kubernetes control planes are supported for production DPF deployments.
> Single-master clusters may be used only as a lab shortcut.

## Network Setup

* All nodes have full internet access - both from the host out-of-band and DPU high speed interfaces
* DPU high-speed ports (`p0`, `p1`) must be connected to the network. In host-trusted mode, the DPU communication
  channel (`br-comm-ch`) is established through a VF on the high-speed interface. If the high-speed ports are not
  connected, the DPU will fail to join the DPUCluster
* Virtual IP from the management subnet reserved for internal DPF usage
* The out-of-band management and high-speed networks are routable to each other
* The control plane nodes hosting the DPU control plane pods must be located on the same L2 broadcast domain
* The out-of-band management fabric on which control plane nodes are connected should allow MultiCast traffic (used for
  VRRP)
