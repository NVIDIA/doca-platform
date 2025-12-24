---
title: "DPF System Prerequisites for Zero Trust"
---

[TOC]

DPF makes a number of assumptions about the hardware, software and networking of the machines it runs on. Some of the
specific [user guides](../README.md) add their own requirements.

## Hardware Setup

There are high availability control plane machines running DPF and workload machines.

### Control Plane Machines

Each control plane machine:

* May be virtualized
* x86_64 architecture
* 16 GB RAM
* 8 CPUs
* If DPUs are installed, they must be in NIC mode (see [Control Plane Nodes with BlueField DPUs](#control-plane-nodes-with-bluefield-dpus))

### Workload Machines

Each workload machine has the following characteristics:

* Bare metal - no virtualization
* Any number of DPUs

#### DPUs

* Bluefield 3
* 32 GB memory
* Flashed with NVIDIA BFB with DOCA version 2.5 or higher
* out-of-band management port must be connected to the management network

### Control Plane Nodes with BlueField DPUs

Control plane nodes with BlueField DPUs require two configuration steps:
1. **Hardware Configuration**: DPUs must be in NIC mode (Arm cores disabled)
2. **DPF Configuration**: Node selector to prevent DPF from provisioning control plane DPUs

<details markdown="1">
<summary><b>Prerequisites</b></summary>

The DPU NIC mode setup script (below) validates these requirements:

- Root/sudo access on control plane hosts
- MFT tools installed (`mlxconfig`, `mlxfwreset`)
- BlueField DPUs present on the system
</details>

<details markdown="1">
<summary><b>Assumptions</b></summary>

The DPU NIC mode setup script (below) does not validate these (ensure they are met):

- Zero-Trust mode disabled (see troubleshooting if errors occur)
- Script run **before** Kubernetes deployment
- Host can reboot (SLR level 4 triggers warm reboot)
</details>

<details markdown="1">
<summary><b>DPU NIC Mode Setup Script</b></summary>

Run this command on each control plane node to create and execute the DPU NIC mode setup script. Simply copy and paste the entire code block below:

```bash
cat > dpu-control-plane-setup.sh << 'SCRIPT_EOF'
#!/bin/bash
# Switch BlueField DPUs to NIC mode from host
# Mode mapping (mlxconfig output):
#   ENABLED(0)  = DPU mode
#   DISABLED(1) = NIC mode
# Setting INTERNAL_CPU_OFFLOAD_ENGINE=1 switches to NIC mode

set -euo pipefail

fail() {
	echo "Error: $1" >&2
	[[ -n "${2:-}" ]] && echo "$2" >&2
	exit 1
}

usage() {
	cat << EOF
Usage: $(basename "$0") [--dry-run] [--no-reboot]

Switch all BlueField DPUs on this host to NIC mode.

  --dry-run    Show current mode and planned changes only
  --no-reboot  Apply config but do not trigger host reboot
EOF
	exit 0
}

trap 'echo; echo "Interrupted" >&2; exit 130' SIGINT SIGTERM

dry_run=false
reboot=true

while [[ $# -gt 0 ]]; do
	case $1 in
	--dry-run) dry_run=true ;;
	--no-reboot) reboot=false ;;
	-h | --help) usage ;;
	*) fail "Unknown option: $1" ;;
	esac
	shift
done

[[ $EUID -eq 0 ]] || fail "Run as root"
command -v mst &> /dev/null || fail "Install MFT tools (mst not found)"
command -v mlxconfig &> /dev/null || fail "Install MFT tools (mlxconfig not found)"
if "$reboot"; then
	command -v mlxfwreset &> /dev/null || fail "Install MFT tools (mlxfwreset not found)"
fi

find_dpus() {
	local -a dpus=()
	for dev in /sys/bus/pci/devices/*/device; do
		[[ -f "$dev" ]] || continue
		local id
		id=$(tr -d '[:space:]' < "$dev" 2> /dev/null) || continue
		[[ $id =~ ^0xa2d(c|6)$ ]] || continue
		local addr base
		addr=$(basename "$(dirname "$dev")")
		base=${addr%.*}
		[[ " ${dpus[*]} " =~ " $base " ]] || dpus+=("$base")
	done
	echo "${dpus[@]}"
}

$dry_run && echo "=== DRY RUN: no changes will be made ==="

echo "Scanning PCI bus for BlueField DPUs..."
dpus=$(find_dpus)
[[ -z "$dpus" ]] && {
	echo "No BlueField DPUs found"
	exit 0
}
echo "Found $(wc -w <<< "$dpus") DPU(s): $dpus"

output=$(mst start 2>&1) || fail "Failed to start MST service" "$output"

changed=false
needs_reboot=false
for dpu in $dpus; do
	echo "Checking ${dpu}.0..."
	output=$(mlxconfig -d "${dpu}.0" -e q 2>&1) || fail "Cannot query mode for ${dpu}.0" "$output"
	mode_line=$(grep "INTERNAL_CPU_OFFLOAD_ENGINE" <<< "$output" || true)
	[[ -z "$mode_line" ]] && fail "INTERNAL_CPU_OFFLOAD_ENGINE not found for ${dpu}.0"
	echo "  $mode_line"

	# Parse Current (second-to-last) and Next Boot (last) columns
	# Note: A leading '*' may shift fields, so use NF-relative positions
	current=$(awk '{print $(NF-1)}' <<< "$mode_line")
	next=$(awk '{print $NF}' <<< "$mode_line")

	if [[ $current =~ DISABLED && $next =~ DISABLED ]]; then
		echo "  Status: NIC (Current & Next Boot)"
	elif [[ $next =~ DISABLED ]]; then
		echo "  Status: DPU now, NIC on next boot (reboot required)"
		needs_reboot=true
	else
		# Need to configure NIC mode (handles both current=NIC/next=DPU and current=DPU/next=DPU)
		if [[ $current =~ DISABLED ]]; then
			echo "  Status: NIC now, but DPU on next boot (forcing back to NIC mode)"
		else
			echo "  Status: DPU (will set NIC and reboot)"
		fi
		if $dry_run; then
			echo "  [DRY RUN] Would set INTERNAL_CPU_OFFLOAD_ENGINE=1"
		else
			echo "  Switching to NIC mode..."
			output=$(mlxconfig -d "${dpu}.0" -y set INTERNAL_CPU_OFFLOAD_ENGINE=1 2>&1) \
				|| fail "Failed to set NIC mode for ${dpu}.0" "$output"
			echo "  Configuration updated"
		fi
		changed=true
		needs_reboot=true
	fi
done

if ! $changed && ! $needs_reboot; then
	echo "All DPUs already in NIC mode"
	exit 0
fi

$dry_run && {
	echo "=== DRY RUN complete ==="
	exit 0
}

first_dpu=$(awk '{print $1}' <<< "$dpus")

if ! $reboot; then
	if $changed; then
		echo "Config complete. Reboot required to apply changes."
	else
		echo "Config already staged. Reboot required to apply."
	fi
	echo "Run: sudo mlxfwreset -d ${first_dpu}.0 -l 4 -y r --skip_fsm_sync"
	exit 0
fi

echo "=========================================="
echo "WARNING: Host will reboot now to apply DPU changes"
echo "=========================================="
mlxfwreset -d "${first_dpu}.0" -l 4 -y r --skip_fsm_sync 2>&1 \
	|| fail "Reset failed (manual power-cycle may be required)"
SCRIPT_EOF

chmod +x dpu-control-plane-setup.sh
echo "Script created: dpu-control-plane-setup.sh"
```

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
  namespace: dpf-operator-system
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

**DPU still in DPU mode after reboot** (shows `*` and "Next Boot=NIC" but Current still ENABLED):
- Warm reboot (level 4) may not apply firmware config on some BlueField firmware versions
- **Solution**: Perform a cold boot (power cycle the host)
- After power cycle, verify with `./dpu-control-plane-setup.sh --dry-run` - should show "NIC (Current and Next Boot)"

**Other issues**:
- **Zero-Trust mode**: Disable via mlxprivhost/BMC/Redfish, then re-run script
- **MFT tools missing**: Install from https://network.nvidia.com/products/adapter-software/firmware-tools/ (>=4.33.0-169)
- **SLR timeout/hang**: Manual power-cycle may be required
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

### Control Plane Machines

* NFS client packages - i.e. ` nfs-common`
* NFS server available with `/mnt/dpf_share` readable and writable by any user

### Kubernetes

* Kubernetes 1.32 - 1.34
* Control plane nodes have the labels `"node-role.kubernetes.io/control-plane" : ""`

## Network Setup

* All nodes must have internet access to be able to pull images - included the DPUs
* Virtual IP from the management subnet reserved for internal DPF usage
* The DPU out-of-band physical interface must be connected with the DPF control planes
* The control plane nodes hosting the DPU control plane pods must be located on the same L2 broadcast domain
* The out-of-band management fabric on which control plane nodes are connected should allow MultiCast traffic (used for
  VRRP)
