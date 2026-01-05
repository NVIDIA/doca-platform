#!/bin/bash
#
# Copyright 2026 NVIDIA
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Switch BlueField DPUs to NIC mode from host
# Mode mapping (mlxconfig output):
#   ENABLED(0)  = DPU mode
#   DISABLED(1) = NIC mode
# Setting INTERNAL_CPU_OFFLOAD_ENGINE=1 switches to NIC mode
# Script performs cold power cycle via IPMI to apply changes

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
command -v mlxfwreset &> /dev/null || fail "Install MFT tools (mlxfwreset not found)"
command -v ipmitool &> /dev/null || fail "Install ipmitool (not found)"
# Always check IPMI access to provide early feedback
ipmitool -I open chassis status &> /dev/null || fail "IPMI not accessible" "Load IPMI kernel modules (ipmi_devintf, ipmi_si) or check BMC configuration"

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

# Show run mode
mode="NORMAL MODE: Configuration will be applied and host will power cycle"
$dry_run && mode="DRY RUN MODE: No changes will be made"
! $reboot && ! $dry_run && mode="NO-REBOOT MODE: Configuration will be applied but manual power cycle required"
echo "=== $mode ==="
echo

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

echo "Resetting DPU firmware to commit configuration changes..."
for dpu in $dpus; do
	echo "Resetting ${dpu}.0..."
	echo "  Running: mlxfwreset -d ${dpu}.0 --level 3 --skip_fsm_sync -y r"
	mlxfwreset -d "${dpu}.0" --level 3 --skip_fsm_sync -y r 2>&1 || {
		echo "Warning: Failed to reset ${dpu}.0 firmware"
	}
done

if ! $reboot; then
	if $changed; then
		echo "Config complete and DPU firmware reset."
	else
		echo "DPU firmware reset complete."
	fi
	echo "Power cycle required to apply changes."
	echo "Run: sudo ipmitool -I open chassis power cycle"
	exit 0
fi

echo "=========================================="
echo "WARNING: Host will power cycle now to apply DPU changes"
echo "=========================================="
ipmitool -I open chassis power cycle 2>&1 \
	|| fail "Power cycle failed (manual power cycle may be required)"
