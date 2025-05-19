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
    cat <<EOF >&2
Usage: $(basename "$0") [OPTIONS]

Options:
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
    # Perform cleanup if device_pci_address exists and is valid
    if [[ -n "$device_pci_address" && "$device_pci_address" =~ ^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}$ ]]; then
        cleanup
    fi
    exit 1
}

# Parse command line arguments
parse_arguments() {
    allowed_arguments=(
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
            if [ $retry_count -eq $((max_retries-1)) ]; then
                error "${BRIDGE_NAME} bridge does not exist"
            fi
            log "Bridge not found, retrying in 5 seconds (attempt $((retry_count+1))/$max_retries)"
            sleep 5
            retry_count=$((retry_count+1))
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
            if [ $retry_count -eq $((max_retries-1)) ]; then
                error "Default route for bridge ${BRIDGE_NAME} does not exist"
            fi
            log "Default route not found, retrying in 5 seconds (attempt $((retry_count+1))/$max_retries)"
            sleep 5
            retry_count=$((retry_count+1))
        fi
    done
}


# Create VFs for a PF device
create_VFs() {
    local pf_device=$1
    local vf_num
    vf_num=$(cat ${PCI_SYS_DIR}/"${pf_device}"/sriov_numvfs)
    if [ "${vf_num}" -eq 0 ]; then
        if ! echo "${num_of_vfs}" >${PCI_SYS_DIR}/"${pf_device}"/sriov_numvfs; then
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
    vf_device=$(find ${PCI_SYS_DIR}/"${pf_device}"/virtfn0/net -mindepth 1 -maxdepth 1 -type d 2>/dev/null)
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

    trap trap_handler INT TERM ILL FPE SEGV ALRM ERR EXIT

    local p0="${device_pci_address}.0"
    create_VFs "${p0}"

    local p1="${device_pci_address}.1"
    if [[ -d "${PCI_SYS_DIR}/${p1}" ]]; then
        local deviceID
        deviceID=$(cat "${PCI_SYS_DIR}/${p1}/device")
        for dpu_device in "${DPU_DEVICE_LIST[@]}"; do
            if [[ "${dpu_device}" == "${deviceID}" ]]; then
                create_VFs "${p1}"
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
device_pci_address=${device_pci_address}
num_of_vfs=${num_of_vfs}
control_plane_mtu=${control_plane_mtu}
EOF
        log "Saved VF configuration to ${VF_CONFIG_FILE}"
    fi

    log "Main function completed successfully"

    trap - INT TERM ILL KILL FPE SEGV ALRM ERR EXIT

    return 0
}

main "$@"
