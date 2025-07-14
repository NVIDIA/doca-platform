---
title: "Host Installation for non-Kubernetes Environments"
---

[TOC]

## Prerequisites

1. Configure the `br-dpu` bridge as described in the [network configuration prerequisite guide](../user-guides/prerequisites/host-network-configuration-prerequisite.md)

## Installation Steps

### 1. Install DOCA

```bash
# Set DOCA repository URL
export DOCA_URL="https://linux.mellanox.com/public/repo/doca/3.0.0/ubuntu24.04/x86_64/"

# Add NVIDIA Mellanox GPG key
curl https://linux.mellanox.com/public/repo/doca/GPG-KEY-Mellanox.pub | sudo gpg --dearmor > /etc/apt/trusted.gpg.d/GPG-KEY-Mellanox.pub

# Add DOCA repository
echo "deb [signed-by=/etc/apt/trusted.gpg.d/GPG-KEY-Mellanox.pub] $DOCA_URL ./" | sudo tee /etc/apt/sources.list.d/doca.list

# Update and install required packages
sudo apt-get update
sudo apt-get -y install doca-all doca-networking
```

### 2. Set Up Required Files
Make sure you have the following files under directory `/opt/dpf/`

<details markdown="1"><summary>hostnetwork.sh</summary>

[embedmd]:#(../../../internal/hostnetwork/hostnetwork.sh)
```sh
#!/bin/bash

#  2024 NVIDIA CORPORATION & AFFILIATES
#
#  Licensed under the Apache License, Version 2.0 (the License);
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an AS IS BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.

set -euo pipefail

# Define constants
readonly DEFAULT_NUM_OF_VFS=16
readonly BRIDGE_NAME="br-dpu"
readonly PCI_SYS_DIR="/sys/bus/pci/devices"
readonly BR_DPU_DIR="/sys/class/net/${BRIDGE_NAME}"
readonly DPU_DEVICE_LIST=("0xa2dc" "0xa2d6")
readonly DEFAULT_DELETE_FLAG="false"
readonly DEFAULT_BRIDGE_CHECK_FLAG="false"
readonly DEFAULT_RESTORE_VF_FLAG="false"
readonly SKIP_DEFAULT_ROUTE_CHECK_COND_FILE="/var/lib/dpf/dms/hostnetwork-skip-default-route-check"
readonly VF_CONFIG_FILE="/var/lib/dpf/dms/vf-config"
readonly DMS_DIR="/var/lib/dpf/dms"

# Print usage instructions
usage() {
	cat << EOF >&2
Usage: $(basename "$0") [OPTIONS]

Options:
    --serial_number <serial>       Serial number of the DPU device (e.g. "MT_0001234567", "MT25066004C7")
    --device_pci_address <address>  PCI address of the DPU device (e.g. "0000:03:00")
    --num_of_vfs <num>             Number of Virtual Functions to create (default: ${DEFAULT_NUM_OF_VFS})
    --control_plane_mtu <mtu>      MTU size for the control plane network (e.g. 1500)
    --delete                       Delete existing network configuration
    --check-bridge                 Check if bridge exists and is configured properly
    --restore-vf                   Restore VF configuration from ${VF_CONFIG_FILE}

Examples:
    # Configure networking with default values
    $(basename "$0") --device_pci_address 0000:03:00 --control_plane_mtu 1500

    # Configure with custom number of VFs and MTU
    $(basename "$0") --device_pci_address 0000:03:00 --num_of_vfs 8 --control_plane_mtu 9000

    # Delete existing configuration
    $(basename "$0") --device_pci_address 0000:03:00 --delete

    # Check if bridge exists and is configured properly
    $(basename "$0") --check-bridge

    # Restore VF configuration from file
    $(basename "$0") --restore-vf
EOF
	exit 1
}

# Log function
log() {
	echo "[hostnetwork] $1"
}

# Error function with improved formatting for log messages
error() {
	# Print error message with file name, function name, and line number
	echo "[hostnetwork] Error (${FUNCNAME[1]}:${BASH_LINENO[0]}): $1" >&2
	exit 1
}

# Parse command line arguments
parse_arguments() {
	allowed_arguments=(
		"--serial_number"
		"--device_pci_address"
		"--num_of_vfs"
		"--control_plane_mtu"
		"--delete"
		"--check-bridge"
		"--restore-vf"
		"--help"
	)

	while [[ $# -gt 0 ]]; do
		case $1 in
		--serial_number)
			serial_number=$2
			shift 2
			;;
		--device_pci_address)
			device_pci_address=$2
			shift 2
			;;
		--num_of_vfs)
			num_of_vfs=$2
			shift 2
			;;
		--control_plane_mtu)
			control_plane_mtu=$2
			shift 2
			;;
		--delete)
			delete_flag="true"
			shift
			;;
		--check-bridge)
			check_bridge_flag="true"
			shift
			;;
		--restore-vf)
			restore_vf_flag="true"
			shift
			;;
		--help)
			usage
			;;
		*)
			local is_valid_argument=false
			for arg in "${allowed_arguments[@]}"; do
				if [[ "$arg" == "$1" ]]; then
					is_valid_argument=true
					break
				fi
			done
			if [[ "$is_valid_argument" == false ]]; then
				log "Unknown option: $1" >&2
				usage
			fi
			;;
		esac
	done

	restore_vf_flag=${restore_vf_flag:-$DEFAULT_RESTORE_VF_FLAG}
	delete_flag=${delete_flag:-$DEFAULT_DELETE_FLAG}
	check_bridge_flag=${check_bridge_flag:-$DEFAULT_BRIDGE_CHECK_FLAG}
	# Check for mutually exclusive flags
	local flag_count=0
	[[ "${delete_flag}" == "true" ]] && ((flag_count++))
	[[ "${check_bridge_flag}" == "true" ]] && ((flag_count++))
	[[ "${restore_vf_flag}" == "true" ]] && ((flag_count++))
	if [[ $flag_count -gt 1 ]]; then
		error "Flags --delete, --check-bridge, and --restore-vf are mutually exclusive. Only one can be specified."
	fi

	# Handle restore VF configuration if flag is set
	if [[ "${restore_vf_flag}" == "true" ]]; then
		if [[ -f "${VF_CONFIG_FILE}" ]]; then
			# Check if any command line arguments were provided
			if [[ -n "${device_pci_address:-}" || -n "${num_of_vfs:-}" || -n "${control_plane_mtu:-}" ]]; then
				log "Warning: Command line arguments will take precedence over values from ${VF_CONFIG_FILE}"
			else
				# Source the config file to get the variables
				source "${VF_CONFIG_FILE}"
				if [[ -z "${device_pci_address:-}" || -z "${num_of_vfs:-}" || -z "${control_plane_mtu:-}" ]]; then
					error "Invalid VF config file. Missing required parameters."
				fi
				log "Restored configuration from ${VF_CONFIG_FILE}"
			fi
			return 0
		else
			log "VF config file ${VF_CONFIG_FILE} not found"
			exit 0
		fi
	fi

	# Ensure default values for arguments that were not provided
	serial_number=${serial_number:-}
	device_pci_address=${device_pci_address:-}
	num_of_vfs=${num_of_vfs:-$DEFAULT_NUM_OF_VFS}
	control_plane_mtu=${control_plane_mtu:-}
	if [ "$check_bridge_flag" == "true" ]; then
		return 0
	fi

	# Check required arguments and validate format
	if [[ -z "$device_pci_address" || ! "$device_pci_address" =~ ^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}$ ]]; then
		error "Invalid or missing device_pci_address. Expected format: xxxx:xx:xx where x is a hexadecimal character."
	fi

	if [ "$delete_flag" == "true" ]; then
		return 0
	fi

	if [[ -z "$control_plane_mtu" ]]; then
		error "control_plane_mtu is a required argument"
	fi
}

# Update control plane MTU
update_control_plane_mtu() {
	# Set MTU to the br-dpu.
	local bridge_mtu
	bridge_mtu=$(cat ${BR_DPU_DIR}/mtu)
	if [ "${bridge_mtu}" -ne "${control_plane_mtu}" ]; then
		if ! ip link set dev ${BRIDGE_NAME} mtu "${control_plane_mtu}"; then
			error "Failed to update bridge MTU"
		fi
	fi

	# Set MTU to the interfaces under br-dpu.
	for inf in "${BR_DPU_DIR}"/brif/*; do
		inf=$(basename "$inf")
		local inf_mtu
		inf_mtu=$(cat /sys/class/net/"${inf}"/mtu)
		if [ "${inf_mtu}" -ne "${control_plane_mtu}" ]; then
			if ! ip link set dev "${inf}" mtu "${control_plane_mtu}"; then
				error "Failed to update interface MTU"
			fi
		fi
	done
}

verify_default_route() {
	ip route show | grep "default" | grep "${BRIDGE_NAME}" || return 1
}

# Check if the bridge is ready
bridge_check() {
	local max_retries=10
	local retry_count=0

	while [ $retry_count -lt $max_retries ]; do
		if [ -d "${BR_DPU_DIR}" ]; then
			log "${BRIDGE_NAME} is created"
			break
		else
			if [ $retry_count -eq $((max_retries - 1)) ]; then
				error "${BRIDGE_NAME} bridge does not exist"
			fi
			log "Bridge not found, retrying in 5 seconds (attempt $((retry_count + 1))/$max_retries)"
			sleep 5
			retry_count=$((retry_count + 1))
			continue
		fi
	done

	if [ -f "${SKIP_DEFAULT_ROUTE_CHECK_COND_FILE}" ]; then
		log "condition file ${SKIP_DEFAULT_ROUTE_CHECK_COND_FILE} exists, skipping bridge default route check"
		return 0
	fi

	retry_count=0
	while [ $retry_count -lt $max_retries ]; do
		if verify_default_route; then
			return 0
		else
			if [ $retry_count -eq $((max_retries - 1)) ]; then
				error "Default route for bridge ${BRIDGE_NAME} does not exist"
			fi
			log "Default route not found, retrying in 5 seconds (attempt $((retry_count + 1))/$max_retries)"
			sleep 5
			retry_count=$((retry_count + 1))
		fi
	done
}

# Create VFs for a PF device
create_VFs() {
	local pf_device=$1
	local sn=$2
	local vf_num

	device_sn=$(lspci -vvs "${pf_device}" | grep SN | awk '{print $NF}' | tr -d '\n')
	if [ "${device_sn}" != "${sn}" ]; then
		error "Serial number mismatch for ${pf_device}. Expected ${sn}, got ${device_sn}"
	fi

	vf_num=$(cat ${PCI_SYS_DIR}/"${pf_device}"/sriov_numvfs)
	if [ "${vf_num}" -eq 0 ]; then
		if ! echo "${num_of_vfs}" > ${PCI_SYS_DIR}/"${pf_device}"/sriov_numvfs; then
			error "Failed to set the number of VFs ${num_of_vfs}"
		fi
		log "Set the number of VFs to ${num_of_vfs}."
	else
		log "The number of VFs is set before to ${vf_num}"
	fi
	return 0
}

# Add a VF to the bridge
add_vf_to_bridge() {
	local pf_device=$1
	local vf_device
	if ! vf_device=$(find ${PCI_SYS_DIR}/"${pf_device}"/virtfn0/net -mindepth 1 -maxdepth 1 -type d); then
		error "Failed to search for VF device in ${pf_device}"
	fi

	if [ -z "${vf_device}" ]; then
		error "No VFs found for ${pf_device}"
	fi

	local vf_name
	vf_name=$(basename "${vf_device}")
	if ! ip link show master "${BRIDGE_NAME}" | grep -q "${vf_name}"; then
		if ! ip link set dev "${vf_name}" master ${BRIDGE_NAME}; then
			error "Failed to add VF to bridge"
		fi
		if ! ip link set dev "${vf_name}" up; then
			error "Failed to bring VF up"
		fi
		log "Added VF ${vf_name} to bridge ${BRIDGE_NAME}"
		return 0
	else
		log "VF ${vf_name} is already part of bridge ${BRIDGE_NAME}"
		return 0
	fi
}

# Remove a VF from the bridge
delete_vf_from_bridge() {
	local pf_device=$1
	local vf_device
	if [ ! -d "${PCI_SYS_DIR}/${pf_device}/virtfn0/net" ]; then
		log "VF net directory ${PCI_SYS_DIR}/${pf_device}/virtfn0/net does not exist, no need to delete VF from ${pf_device}"
		return 0
	fi
	vf_device=$(find ${PCI_SYS_DIR}/"${pf_device}"/virtfn0/net -mindepth 1 -maxdepth 1 -type d 2> /dev/null)
	if [ -z "${vf_device}" ]; then
		log "No VF found, no need to delete VF from ${pf_device}"
		return 0
	fi

	local vf_name
	vf_name=$(basename "${vf_device}")
	if ! ip link show master ${BRIDGE_NAME} | grep -q "${vf_name}"; then
		log "VF ${vf_name} is not connected to the bridge, no need to delete VF from ${pf_device}"
		return 0
	fi

	if ip link set "${vf_name}" nomaster; then
		log "Disconnected VF ${vf_name} from bridge"
		return 0
	else
		error "Failed to disconnect VF ${vf_name} from bridge"
	fi
}

# Function to perform cleanup
cleanup() {
	log "Cleaning up..."
	if [[ -n "${device_pci_address:-}" ]]; then
		delete_vf_from_bridge "${device_pci_address}.0"
	fi
	if [[ -f "${VF_CONFIG_FILE}" ]]; then
		rm -f "${VF_CONFIG_FILE}"
		log "Removed VF configuration file ${VF_CONFIG_FILE}"
	fi
}

# Function that is used in trap to be called on different signals received
trap_handler() {
	cleanup
	# Need to return 1 so that the provisioning controller understands if the script was ran successfully or not
	exit 1
}

# Main program
main() {
	if ! parse_arguments "$@"; then
		exit 1
	fi

	# If the delete flag is set then perform cleanup and exit immediately.
	if [[ $delete_flag == "true" ]]; then
		cleanup
		exit 0
	fi

	# Call bridge_check and store its return code
	bridge_check
	local rc=$?

	# If check-bridge flag is set, return the exit code from bridge_check
	if [[ "${check_bridge_flag}" == "true" ]]; then
		return $rc
	fi

	if [[ "${restore_vf_flag}" != "true" ]]; then
		trap trap_handler INT TERM ILL FPE SEGV ALRM ERR EXIT
	fi

	if [[ -z "$serial_number" ]]; then
		error "serial_number is a required argument"
	fi

	local p0="${device_pci_address}.0"
	create_VFs "${p0}" "${serial_number}"

	local p1="${device_pci_address}.1"
	if [[ -d "${PCI_SYS_DIR}/${p1}" ]]; then
		local deviceID
		deviceID=$(cat "${PCI_SYS_DIR}/${p1}/device")
		for dpu_device in "${DPU_DEVICE_LIST[@]}"; do
			if [[ "${dpu_device}" == "${deviceID}" ]]; then
				create_VFs "${p1}" "${serial_number}"
				break
			fi
		done
	fi

	add_vf_to_bridge "${p0}"
	update_control_plane_mtu

	if [[ "${restore_vf_flag}" != "true" ]]; then
		if ! mkdir -p "${DMS_DIR}"; then
			error "Failed to create directory ${DMS_DIR}"
		fi
		# Save VF configuration to file for future restoration (overwrites existing file if any)
		cat > "${VF_CONFIG_FILE}" << EOF
serial_number=${serial_number}
device_pci_address=${device_pci_address}
num_of_vfs=${num_of_vfs}
control_plane_mtu=${control_plane_mtu}
EOF
		log "Saved VF configuration to ${VF_CONFIG_FILE}"
	fi

	log "Main function completed successfully"

	if [[ "${restore_vf_flag}" != "true" ]]; then
		trap - INT TERM ILL KILL FPE SEGV ALRM ERR EXIT
	fi

	return 0
}

main "$@"
```

</details>

<details markdown="1"><summary>dmsinit.sh</summary>

[embedmd]:#(../../../internal/dmsinit/dmsinit.sh)
```sh
#!/bin/bash

# 2025 NVIDIA CORPORATION & AFFILIATES

# Licensed under the Apache License, Version 2.0 (the License);
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an AS IS BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

# Define constants
readonly DEFAULT_EXTERNAL_CERTIFICATE='none'
readonly DEFAULT_K8S_ENV='true'
readonly DEFAULT_DMS_IP='0.0.0.0'
readonly DEFAULT_DMS_PORT=9339
readonly DEFAULT_NAMESPACE='dpf-operator-system'
readonly DEFAULT_ISSUER='dpf-provisioning-issuer'
readonly DEFAULT_KUBERNETES_VERSION='1.32.0'
readonly DEFAULT_NODE_REBOOT_METHOD='gNOI'
readonly VALID_NODE_REBOOT_METHODS=('gNOI' 'external' 'script')
readonly PCISYS_DIR="/sys/bus/pci/devices"
readonly DEVICE_LIST=("0xa2dc" "0xa2d6") # BlueField-3 and BlueField-2 device IDs
readonly DEFAULT_DMS_CONF_DIR="/opt/dpf/dms"
readonly DEFAULT_PCI_ADDR_FILE="pci_addr"
readonly DEFAULT_DMS_CONF_FILE="dms.conf"
readonly DEFAULT_DMS_IMAGE_DIR="bfb"
readonly DEFAULT_DMS_SYSTEMD_FILE="dmsd.service"
readonly CMD_REGISTER="register"
readonly CMD_CHECK_RSHIM_NOT_OCCUPIED="check-rshim-not-occupied"
readonly VALID_CMDS=("$CMD_REGISTER" "$CMD_CHECK_RSHIM_NOT_OCCUPIED")
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly HOSTNETWORK_SCRIPT="$SCRIPT_DIR/hostnetwork.sh"
readonly DMSINIT_SCRIPT="$SCRIPT_DIR/dmsinit.sh"

# Global arrays for DPU discovery
declare -a pci_addr_list=()
declare -a pf0_name_list=()
declare -A number_of_pfs=()

# Preserve original arguments
ORIGINAL_ARGS=("$@")

# Log function
log() {
	echo "[dmsinit] $1"
}

# Error function
error() {
	echo "[dmsinit] Error (${FUNCNAME[1]}:${BASH_LINENO[0]}): $1" >&2
	exit 1
}

# Function to format PCI address
format_pci_address() {
	local pci_address=$1
	local remove_function_number=${pci_address%.*}
	local label_pci_addr=${remove_function_number//:/-} # Convert colons to hyphens
	# Output in format 0000-4b-00 (domain-bus-device)
	echo "$label_pci_addr"
}

# Function to get PF0 name
get_pf0_name() {
	local pci_address=$1
	local net_dir="${PCISYS_DIR}/${pci_address//-/:}/net"

	if [ -d "$net_dir" ]; then
		local pf0_name=$(ls "$net_dir" 2> /dev/null | head -n1)
		if [ -n "$pf0_name" ]; then
			echo "$pf0_name"
		else
			error "Error: No network interface found in $net_dir"
		fi
	else
		error "Error: Network directory not found: $net_dir"
	fi
}

store_pci_addresses() {
	local pci_addr_list=("$@")

	# Output PCI addresses to file for DMS
	log "Writing PCI addresses to $pci_addr_file"

	# Create directory if it doesn't exist
	mkdir -p "$(dirname "$pci_addr_file")"

	# Clear the file first
	> "$pci_addr_file"

	# Write each PCI address to the file
	for pci_addr in "${pci_addr_list[@]}"; do
		echo "$pci_addr" >> "$pci_addr_file"
	done

	log "Wrote ${#pci_addr_list[@]} PCI addresses to $pci_addr_file"
}

# Function to discover DPUs
discover_dpu() {
	# Clear arrays before use
	pci_addr_list=()
	pf0_name_list=()
	number_of_pfs=()

	# Read all PCI devices
	for device_dir in "${PCISYS_DIR}"/*; do
		if [ ! -d "$device_dir" ]; then
			continue
		fi

		# Read device ID
		if [ ! -f "${device_dir}/device" ]; then
			continue
		fi
		device_id=$(cat "${device_dir}/device" 2> /dev/null)

		# Check if device is in our list
		for known_device in "${DEVICE_LIST[@]}"; do
			if [ "$device_id" != "$known_device" ]; then
				continue
			fi

			# Get PCI address
			pci_name=$(basename "$device_dir")
			pci_addr=$(format_pci_address "$pci_name")

			# Skip if already discovered
			if [[ -n "${number_of_pfs["$pci_addr"]:-}" ]]; then
				# Increment number of PFs
				number_of_pfs["$pci_addr"]=$((${number_of_pfs["$pci_addr"]} + 1))
				log "update number of PFs for $pci_addr: ${number_of_pfs["$pci_addr"]}"
				continue
			fi

			# Get PF0 name
			if ! pf0_name=$(get_pf0_name "$pci_name"); then
				error "get_pf0_name failed for $pci_name"
			fi

			# Add PCI address to array
			pci_addr_list+=("$pci_addr")
			pf0_name_list+=("$pf0_name")
			number_of_pfs[$pci_addr]="1" # Initialize with 1 PF
			log "Discovered DPU:"
			log "  PCI Address: $pci_addr"
			log "  Device ID: $device_id"
			log "  PF0 Name: $pf0_name"
		done
	done
	# Create DPU devices array
	local dpu_count=${#pci_addr_list[@]}
	log "Total DPUs discovered: $dpu_count"

	# If no DPUs found, exit early
	if [ $dpu_count -eq 0 ]; then
		log "No DPUs found on this system"
		return
	fi

	store_pci_addresses "${pci_addr_list[@]}"
}

# Define functions
check_and_install_kubectl() {
	if ! command -v kubectl &> /dev/null; then
		log "kubectl not found, downloading and installing kubectl $DEFAULT_KUBERNETES_VERSION in /tmp/doca-bin"
		mkdir -p /tmp/doca-bin
		if ! curl -sSfL -o /tmp/doca-bin/kubectl "https://dl.k8s.io/release/v$DEFAULT_KUBERNETES_VERSION/bin/linux/amd64/kubectl"; then
			error "Failed to download kubectl"
		fi
		chmod +x /tmp/doca-bin/kubectl || error "Failed to set execute permissions for kubectl"
		export PATH="/tmp/doca-bin:$PATH"
	else
		log "kubectl already installed"
	fi
	if ! output=$($kubectl_cmd version 2>&1); then
		error "Failed to check kubectl version: $output"
	fi
}

check_resource_exists() {
	local resource_type=$1
	local resource_name=$2
	output=$($kubectl_cmd -n $namespace get -o json $resource_type $resource_name 2>&1)
	if [ $? -ne 0 ]; then
		return 1
	fi
	log "Resource $resource_name of type $resource_type already exists, skipping creation"
	return 0
}

create_certificate() {
	local secret_name="$dpu_node_name-server-secret"
	if ! check_resource_exists "Certificate" "$dpu_node_name-dms-server-cert"; then
		log "Creating certificate"
		yaml=$(
			cat << EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: $dpu_node_name-dms-server-cert
  namespace: $namespace
spec:
  secretName: $secret_name
  commonName: $dpu_node_name-dms-server-cert
  duration: 8760h
  renewBefore: 4360h
  issuerRef:
    name: $issuer
    kind: Issuer
  usages:
  - server auth
  ipAddresses:
  - "$dms_ip"
EOF
		)
		output=$($kubectl_cmd apply -f - <<< "$yaml" 2>&1)
		if [ $? -ne 0 ]; then
			error "Failed to create certificate $dpu_node_name-dms-server-cert: $output"
		fi
		log "Certificate $dpu_node_name-dms-server-cert created successfully"
	else
		log "Certificate $dpu_node_name-dms-server-cert already exists, skipping creation"
	fi

	# Wait until the secret is created and populated
	local max_attempts=30
	local attempt=0
	local ca_crt=""
	local tls_crt=""
	local tls_key=""

	while [ $attempt -lt $max_attempts ]; do
		# Extract each certificate component separately
		ca_crt=$($kubectl_cmd get secret -n $namespace "$secret_name" -o jsonpath='{.data.ca\.crt}' 2> /dev/null || true)
		tls_crt=$($kubectl_cmd get secret -n $namespace "$secret_name" -o jsonpath='{.data.tls\.crt}' 2> /dev/null || true)
		tls_key=$($kubectl_cmd get secret -n $namespace "$secret_name" -o jsonpath='{.data.tls\.key}' 2> /dev/null || true)

		# Check that all components are present and non-empty
		if [ -n "$ca_crt" ] && [ -n "$tls_crt" ] && [ -n "$tls_key" ]; then
			break
		fi

		log "Waiting for secret $secret_name to be fully populated... (attempt $((attempt + 1))/$max_attempts)"
		sleep 2
		((attempt++))
	done

	if [ -z "$ca_crt" ] || [ -z "$tls_crt" ] || [ -z "$tls_key" ]; then
		error "Timed out waiting for secret $secret_name to be fully populated. ca.crt: ${#ca_crt} chars, tls.crt: ${#tls_crt} chars, tls.key: ${#tls_key} chars"
	fi
	log "Certificate secret name: $secret_name"

	mkdir -p $dms_conf_dir/certs
	echo "$ca_crt" | base64 --decode > $dms_conf_dir/certs/ca.crt
	echo "$tls_crt" | base64 --decode > $dms_conf_dir/certs/tls.crt
	echo "$tls_key" | base64 --decode > $dms_conf_dir/certs/tls.key
}

get_serial_number() {
	local pci_addr=$1
	local pci_addr_for_lspci=$(echo $pci_addr | awk -F'-' '{print $2":"$3".0"}')
	local serial_number

	serial_number=$(lspci -vvs $pci_addr_for_lspci | grep SN | awk '{print $NF}' | tr -d '\n')
	if [ -z "$serial_number" ]; then
		error "Failed to get serial number for PCI address $pci_addr"
	fi
	echo "$serial_number"
}

create_dpu_device() {
	local pci_addr=$1
	local pf0_name=$2
	local number_of_pfs=$3
	local sn=$(get_serial_number $pci_addr)
	local name=$(echo "$sn" | tr '[:upper:]' '[:lower:]')

	if ! check_resource_exists "DPUDevice" "$name"; then
		log "Creating DPUDevice for PCI address $pci_addr"
		yaml=$(
			cat << EOF
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: $name
  namespace: $namespace
spec:
  serialNumber: $sn
  pf0Name: $pf0_name
  numberOfPFs: $number_of_pfs
EOF
		)

		output=$($kubectl_cmd apply -f - <<< "$yaml" 2>&1)
		if [ $? -ne 0 ]; then
			error "Failed to create DPUDevice $name: $output"
		fi
		log "DPUDevice $name applied successfully"
	fi
	output=$($kubectl_cmd patch dpudevice $name -n $namespace --subresource status --type=merge -p '{"status":{"pciAddress":"'"$pci_addr"'"}}' 2>&1)
	if [ $? -ne 0 ]; then
		error "Failed to update PCI address for DPUDevice $name: $output"
	fi
	log "Updated PCI address for DPUDevice $name: $pci_addr"
	# Add the DPUDevice name to the array for later use in DPUNode creation
	dpudevice_names+=("$name")
	log "Added $name to dpudevice_names array"
}

create_dpunode() {
	if ! check_resource_exists "DPUNode" "$dpu_node_name"; then
		log "Creating DPUNode"
		yaml=$(
			cat << EOF
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  name: $dpu_node_name
  namespace: $namespace
EOF
		)
		# in k8s env, the label is set by dpu-detector
		if [ "$k8s_env" != "true" ]; then
			yaml=$(
				cat << EOF
$yaml
  labels:
    feature.node.kubernetes.io/dpu-oob-bridge-configured: "true"
    feature.node.kubernetes.io/dpu-enabled: "true"
EOF
			)
		fi

		if [ -n "$kube_node_ref" ]; then
			uid=$($kubectl_cmd get node $kube_node_ref -o jsonpath='{.metadata.uid}')
			if [ -z "$uid" ]; then
				error "Failed to get UID for node $kube_node_ref"
			fi
			yaml=$(
				cat << EOF
$yaml
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: $kube_node_ref
    uid: $uid
    controller: true
EOF
			)
		fi

		yaml=$(
			cat << EOF
$yaml
spec:
  nodeRebootMethod:
EOF
		)
		if [ "$node_reboot_method" = "gNOI" ]; then
			yaml=$(
				cat << EOF
$yaml
    gNOI: {}
EOF
			)
		fi
		if [ "$node_reboot_method" = "external" ]; then
			yaml=$(
				cat << EOF
$yaml
    external: {}
EOF
			)
		fi
		if [ "$node_reboot_method" = "script" ]; then
			if ! declare -p script_name 2> /dev/null; then
				error "when script reboot method is used, the --script must be specified"
			fi
			yaml=$(
				cat << EOF
$yaml
    script:
      name: "$script_name"
EOF
			)
		fi

		if [ -n "$dms_ip" ] && [ -n "$dms_port" ]; then
			yaml=$(
				cat << EOF
$yaml
  nodeDMSAddress:
    ip: $dms_ip
    port: $dms_port
EOF
			)
		fi
		if [ ${#dpudevice_names[@]} -gt 0 ]; then
			yaml=$(
				cat << EOF
$yaml
  dpus:
EOF
			)
			for device in "${dpudevice_names[@]}"; do
				yaml=$(
					cat << EOF
$yaml
    - name: $device
EOF
				)
			done
		fi

		output=$($kubectl_cmd apply -f - <<< "$yaml" 2>&1)
		if [ $? -ne 0 ]; then
			error "Failed to create DPUNode $dpu_node_name: $output"
		fi
		log "DPUNode $dpu_node_name applied successfully"
	fi
}

# Function to get the first PCI address from file and format it to "0000:00:00.0"
get_the_first_pci_address() {
	local pci_file=$1
	local dms_pci

	# Read PCI address from file
	if [ -f "$pci_file" ]; then
		# Read the first line from the file and assign to dms_pci
		dms_pci=$(head -n 1 "$pci_file")
		if [ -z "$dms_pci" ]; then
			error "PCI address file exists but is empty"
		fi
	else
		error "PCI address file not found: $pci_file"
	fi
	# Replace hyphens with colons in the PCI address
	dms_pci=${dms_pci//-/:}
	echo "$dms_pci"
}

create_dms_config() {
	mkdir -p $dms_conf_dir
	if ! dms_pci=$(get_the_first_pci_address "$pci_addr_file"); then
		error "get_the_first_pci_address failed for $pci_addr_file"
	fi

	cat << EOF > $dms_conf_dir/$DEFAULT_DMS_CONF_FILE
-bind_address $dms_ip:$dms_port -v 99 -auth cert -ca $dms_conf_dir/certs/ca.crt -tls_key_file $dms_conf_dir/certs/tls.key -tls_cert_file $dms_conf_dir/certs/tls.crt -password admin -username admin -image_folder $dms_image_dir -target_pci $dms_pci -exec_timeout 900 -disable_unbind_at_activate -reboot_status_check none -debug_command=true
EOF
	log "DMS configuration created at $dms_conf_dir/$DEFAULT_DMS_CONF_FILE"
	log "DMS configuration: $(cat $dms_conf_dir/$DEFAULT_DMS_CONF_FILE)"

	cat << EOF > $dms_conf_dir/$DEFAULT_DMS_SYSTEMD_FILE
[Unit]
Description=DMS server
Wants=network.target
After=network-pre.target

[Service]
Type=exec

#in case we have a bash script file, which needs to be source'd before
Environment="DMS_CONF=$dms_conf_dir/$DEFAULT_DMS_CONF_FILE"

ExecStart=/usr/bin/sh -c '/opt/mellanox/doca/services/dms/dmsd \$([ -f \$DMS_CONF ] && cat \$DMS_CONF)'


#This prevents systemd from terminating dmsd prematurely before it
#finishes its termination routines.
TimeoutStopSec=30

Restart=on-failure
RestartSec=5

#the following configuration means, that if dmsd was restarted 5 times
#in the last 120 seconds, it will not be restarted again
#indication for a serious configuration issue
StartLimitIntervalSec=120
StartLimitBurst=5

#Error codes below are considered critical errors, and not causing systemd to restart
RestartPreventExitStatus=6 SIGABRT

# mounts everythig as RO, apart from /dev, /proc, and /sys
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true

# allow read/write access to BFB folder and hostnetwork config
ReadWritePaths=$dms_image_dir /var/lib

[Install]
WantedBy=multi-user.target
EOF
	log "DMS systemd service file created at $dms_conf_dir/$DEFAULT_DMS_SYSTEMD_FILE"
}

# Function to create and enable restore-vf systemd service
create_restore_vf_service() {
	local service_dir="/etc/systemd/system"
	local service_name="dpf-restore-vf.service"
	local service_file="${service_dir}/${service_name}"

	# Create the service file
	cat << EOF > "$service_file"
[Unit]
Description=DPF - Restore VF Configuration
After=network-online.target

[Service]
Type=exec
ExecStart=${HOSTNETWORK_SCRIPT} --restore-vf
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

	# Reload systemd to recognize the new service
	if ! systemctl daemon-reload; then
		error "Failed to reload systemd daemon"
	fi

	# Enable the service
	if ! systemctl enable $service_name; then
		error "Failed to enable $service_name"
	fi

	log "Created and enabled $service_name at $service_file"
}

create_dms_init_systemd_service() {
	local service_dir="/etc/systemd/system"
	local service_name="dmsinit.service"
	local service_file="${service_dir}/${service_name}"

	# Create the service file
	cat << EOF > "$service_file"
[Unit]
Description=DPF - Restore VF Configuration
After=network-online.target

[Service]
Type=oneshot
ExecStart=${DMSINIT_SCRIPT} ${ORIGINAL_ARGS[@]}
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

	# Reload systemd to recognize the new service
	if ! systemctl daemon-reload; then
		error "Failed to reload systemd daemon"
	fi

	# Enable the service
	if ! systemctl enable $service_name; then
		error "Failed to enable $service_name"
	fi

	log "Created and enabled $service_name at $service_file"
}

usage() {
	cat << EOF
Usage: $0 [options]

Options:
  --kubeconfig                       <path>      Path to the kubeconfig file
  --external-certificate             <cert>      External certificate (default: none)
  --kube-node-ref                    <ref>       Kube node reference
  --dms-ip                           <ip>        DMS IP address (default: 0.0.0.0)
  --dms-port                         <port>      DMS port (default: 9339)
  --k8s-env                          <bool>      Whether to use K8s environment (default: true)
  --issuer                           <issuer>    Issuer name (default: dpf-provisioning-issuer)
  --node-reboot-method               <method>    Node reboot method (default: gNOI). Valid options: gNOI, external, script
  --script-name                      <name>      The name of ConfigMap which includes the pod template for executing custom reboot script
  --dms-conf-dir DIR                DMS configuration directory (default: $DEFAULT_DMS_CONF_DIR)
  --dms-image-dir DIR                DMS image directory (default: $DEFAULT_DMS_IMAGE_DIR)
  --cmd                            <cmd>      Command (default: $CMD_REGISTER). Valid options: $VALID_CMDS
EOF
}

parse_arguments() {
	allowed_arguments=(
		"--kubeconfig"
		"--external-certificate"
		"--kube-node-ref"
		"--dms-ip"
		"--dms-port"
		"--k8s-env"
		"--issuer"
		"--node-reboot-method"
		"--script-name"
		"--namespace"
		"--dms-conf-dir"
		"--dms-image-dir"
		"--cmd"
	)

	# Initialize variables with defaults
	node_reboot_method=$DEFAULT_NODE_REBOOT_METHOD

	while [[ $# -gt 0 ]]; do
		case $1 in
		--kubeconfig)
			kubeconfig=$2
			shift 2
			;;
		--external-certificate)
			external_certificate=$2
			shift 2
			;;
		--kube-node-ref)
			kube_node_ref=$2
			shift 2
			;;
		--dms-ip)
			dms_ip=$2
			shift 2
			;;
		--dms-port)
			dms_port=$2
			shift 2
			;;
		--k8s-env)
			k8s_env=$2
			shift 2
			;;
		--namespace)
			namespace=$2
			shift 2
			;;
		--issuer)
			issuer=$2
			shift 2
			;;
		--node-reboot-method)
			node_reboot_method=$2
			if [[ ! " ${VALID_NODE_REBOOT_METHODS[@]} " =~ " ${node_reboot_method} " ]]; then
				error "Invalid node reboot method: $node_reboot_method. Valid options are: ${VALID_NODE_REBOOT_METHODS[*]}"
			fi
			shift 2
			;;
		--script-name)
			script_name=$2
			if [ "$node_reboot_method" != "script" ]; then
				error "--script-name is only used when the --node-reboot-method is script"
			fi
			shift 2
			;;
		--dms-conf-dir)
			dms_conf_dir="$2"
			shift 2
			;;
		--dms-image-dir)
			dms_image_dir="$2"
			shift 2
			;;
		--cmd)
			cmd=$2
			if [[ ! " ${VALID_CMDS[@]} " =~ " ${cmd} " ]]; then
				error "Invalid cmd: $cmd. Valid options are: ${VALID_CMDS[*]}"
			fi
			shift 2
			;;
		*)
			if [[ ! " ${allowed_arguments[@]} " =~ " $1 " ]]; then
				usage
				error "Unknown option: $1"
			fi
			;;
		esac
	done

	# Update variable assignments section
	external_certificate=${external_certificate:-$DEFAULT_EXTERNAL_CERTIFICATE}
	kube_node_ref=${kube_node_ref:-}
	dms_ip=${dms_ip:-$DEFAULT_DMS_IP}
	dms_port=${dms_port:-$DEFAULT_DMS_PORT}
	k8s_env=${k8s_env:-$DEFAULT_K8S_ENV}
	namespace=${namespace:-$DEFAULT_NAMESPACE}
	issuer=${issuer:-$DEFAULT_ISSUER}
	node_reboot_method=${node_reboot_method:-$DEFAULT_NODE_REBOOT_METHOD}
	kubeconfig=${kubeconfig:-}
	kubectl_cmd="kubectl"
	if [ -n "$kubeconfig" ]; then
		kubectl_cmd="kubectl --kubeconfig $kubeconfig"
	fi

	cmd=${cmd:-$CMD_REGISTER}
	if [ "$cmd" = "$CMD_REGISTER" ]; then
		if [ "$k8s_env" = true ]; then
			if [ -z "$kube_node_ref" ]; then
				error "in k8s env, kube-node-ref is required"
			fi
			dpu_node_name=$kube_node_ref
		else
			dpu_node_name=$(hostname | tr '[:upper:]' '[:lower:]')
		fi
	fi
	dpudevice_names=()

	# Set the dependent variables after dms_conf_dir is finalized
	dms_conf_dir=${dms_conf_dir:-"$DEFAULT_DMS_CONF_DIR"}
	pci_addr_file=${dms_conf_dir}/${DEFAULT_PCI_ADDR_FILE}
	dms_image_dir=${dms_image_dir:-"${dms_conf_dir}/${DEFAULT_DMS_IMAGE_DIR}"}
	log "DMS image directory: $dms_image_dir"
	mkdir -p $dms_image_dir
}

check_rshim_not_occupied() {
	local output=/dev/stderr

	# Check if any DPUs were discovered
	if [ ${#pci_addr_list[@]} -eq 0 ]; then
		error "No DPUs found on this system, exiting"
	fi

	# Check if termination-log exists and is writable
	if [ -f "/dev/termination-log" ]; then
		log "Using /dev/termination-log for output"
		output=/dev/termination-log
	fi

	if ! pci_addr=$(get_the_first_pci_address "$pci_addr_file"); then
		error "get_the_first_pci_address failed for $pci_addr_file"
	fi
	log "Get PCI address: $pci_addr"
	while read dev; do
		if echo 'DISPLAY_LEVEL 1' > "/dev/$dev/misc" && grep -q "$pci_addr" "/dev/$dev/misc"; then
			echo -n "$pci_addr" > "$output"
			exit 1
		fi
	done < <(ls /dev | egrep 'rshim.*[0-9]+')
}

check_bridge() {
	# Check if bridge is configured correctly
	log "Checking bridge br-dpu configuration"

	# Run hostnetwork script with check-bridge option
	if [ -x "$HOSTNETWORK_SCRIPT" ]; then
		log "Running $HOSTNETWORK_SCRIPT --check-bridge"
		if "$HOSTNETWORK_SCRIPT" --check-bridge; then
			log "Bridge check passed"
			return 0
		else
			local ret_code=$?
			log "Bridge check failed with return code $ret_code"
			return $ret_code
		fi
	else
		error "Hostnetwork script not found or not executable: $HOSTNETWORK_SCRIPT"
	fi
}

main() {
	log "Running cmd $cmd"

	if [ "$k8s_env" != "true" ]; then
		# in k8s env, this check is performed by dpu-detector
		if ! check_bridge; then
			error "Bridge br-dpu check failed"
		fi
	fi

	discover_dpu

	if [ "$cmd" = "$CMD_REGISTER" ]; then
		# Check if any DPUs were discovered
		if [ ${#pci_addr_list[@]} -eq 0 ]; then
			error "No DPUs found on this system, exiting"
		fi
		check_and_install_kubectl
		if [ "$external_certificate" = "none" ]; then
			create_certificate
		fi
		# Create DPU device objects
		for i in "${!pci_addr_list[@]}"; do
			local pci_addr="${pci_addr_list[$i]}"
			local pf0_name="${pf0_name_list[$i]}"
			local npfs="${number_of_pfs[$pci_addr]}"
			create_dpu_device "$pci_addr" "$pf0_name" "$npfs"
		done
		log "DPU devices created successfully"
		create_dpunode
		create_dms_config

		# Create and enable Systemd service only in non-Kubernetes environment
		if [ "$k8s_env" != true ]; then
			create_restore_vf_service
			create_dms_init_systemd_service
		else
			log "Skipping Systemd service creation in Kubernetes environment"
		fi

		# set grace_period
		# THIS IS ONLY A TEMPORARY WORKAROUND AND WILL BE REMOVED WITH THE JANUARY RELEASE!
		# This workaround addresses a firmware protection mechanism issue where devices
		# are not automatically recovered after a fatal error.
		# Fatal errors can occur during a SW_RESET, which is triggered by the DMS Pod (e.g., bfb-install).
		#
		# Workflow:
		# 1. Set the `grace_period` to 0 to bypass the firmware protection mechanism and ensure devices are recovered.
		# 2. Explicitly trigger a recovery operation on each device before proceeding with BF installation.
		#
		# Notes:
		# - Physical Functions (PFs) must be handled before Virtual Functions (VFs) to avoid errors.
		#   Improved sorting ensures PFs are processed first based on their device names.
		# - We will only handle `p0` and `p1` PFs (representing the first two functions of the PCI device).
		#   This is consistent with the behavior of the hostnetwork Pod, which also limits its handling to these PFs.
		# - All VFs associated with these PFs will also be handled and recovered, but only after their corresponding PFs
		#   have been successfully processed to ensure a stable recovery sequence.
		if ! pci_addr=$(get_the_first_pci_address "$pci_addr_file"); then
			error "get_the_first_pci_address failed for $pci_addr_file"
		fi
		log "Get PCI address: $pci_addr"
		readlink /sys/bus/pci/devices/"$pci_addr".[01] /sys/bus/pci/devices/"$pci_addr".[01]/virtfn* | xargs -n1 basename | sort -u | while read pci_device; do
			devlink health set pci/$pci_device reporter fw_fatal grace_period 0
			devlink health recover pci/$pci_device reporter fw_fatal
		done
	elif [ "$cmd" = "$CMD_CHECK_RSHIM_NOT_OCCUPIED" ]; then
		check_rshim_not_occupied
	fi

	log "Main function completed successfully"
}

parse_arguments "$@"

if ! main; then
	error "Failed to complete main function: $?"
fi
```
</details>

* a kubeconfig to access the K8s cluster where DPF is deployed

### 3. Initialize DMS

```bash
# Configure environment variables
export KUBECONFIG=/opt/dpf/kube.config
export DMS_IP=$(ip -o -4 addr show $(ip route show default | grep -Po '(?<=dev )[^ ]+') | head -1 | awk '{print $4}' | cut -d/ -f1)

# Set the reboot method - options: 'gNOI', 'external', or 'script'
# Note: gNOI is not fully supported right now. Working on it
export REBOOT_METHOD="external"

# Run DMS initialization script with preserved environment variables
sudo -E /opt/dpf/dmsinit.sh --dms-ip ${DMS_IP} --k8s-env false --node-reboot-method ${REBOOT_METHOD} --kubeconfig ${KUBECONFIG}
```


### 4. Configure and Start System Services

```bash
# Copy the DMS service definition
sudo cp /opt/dpf/dms/dmsd.service /etc/systemd/system/

# Enable and start the rshim service
sudo systemctl enable rshim
sudo systemctl start rshim

# Enable and start the DMS service
sudo systemctl enable dmsd
sudo systemctl start dmsd
```

## Verification

To verify the installation is working correctly:

```bash
# Check services status
sudo systemctl status rshim
sudo systemctl status dmsd
```

## External Host Reboot

When your worker hosts are not managed by Kubernetes, you will need to perform a manual reboot step during the DPU provisioning process:

1. When the DPU reaches the `rebooting` phase, you will need to manually power-cycle the worker host.

2. After the worker node has rebooted, remove the `provisioning.dpu.nvidia.com/dpunode-external-reboot-required` annotation from the DPUNode using kubectl:

```bash
kubectl annotate dpunode <node-name> provisioning.dpu.nvidia.com/dpunode-external-reboot-required-
```

This step is only required for hosts that are not managed by Kubernetes.
