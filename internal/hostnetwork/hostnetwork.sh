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

set -ex -o pipefail

bridge_name="br-dpu"
pci_sys_dir="/sys/bus/pci/devices"
br_dpu_dir="/sys/class/net/${bridge_name}"
dpu_device_list=("0xa2dc" "0xa2d6")

# get PF from PCI address
get_net_devices_from_pci () {
    local pci_address=$1
    device_list=($(find "${pci_sys_dir}/${pci_address}/net" -mindepth 1 -maxdepth 1))
    for device_path in "${device_list[@]}"; do
        device=$(basename "${device_path}")
        echo ${device}
    done
}

bridge_check () {
    while true; do
        if [ -d "${br_dpu_dir}" ]; then
            echo "${bridge_name} is created"
            break
        else
            echo "${bridge_name} bridge does not exist"
            sleep 5
        fi
    done

    while true; do
        IP_COUNT=$(ip addr show $bridge_name | grep 'inet ' | wc -l)
        if [ $IP_COUNT -ge 1 ]; then
            break
        else
            echo "checking the ip address in ${bridge_name}"
            sleep 5
        fi
    done
}

create_VFs () {
    local pf_device=$1
    vf_num=$(cat /sys/class/net/${pf_device}/device/sriov_numvfs)
    if [ "$vf_num" -eq 0 ]; then
        echo ${num_of_vfs} > /sys/class/net/${pf_device}/device/sriov_numvfs
        echo "Set the number of VFs to ${num_of_vfs}."
    else
        echo "the num of vf: ${vf_num} is set before"
    fi
}

add_vf_to_bridge () {
    local pf_device=$1
    vf_device=$(find /sys/class/net/${pf_device}/device/virtfn0/net -mindepth 1 -maxdepth 1 -type d)
    if [ -n "${vf_device}" ]; then
        vf_name=$(basename ${vf_device})
        if ! ip link show master ${bridge_name} | grep -q ${vf_name}; then
            echo "Adding VF ${vf_name} to bridge ${bridge_name}"
            ip link set dev ${vf_name} master ${bridge_name}
            ip link set dev ${vf_name} mtu ${vf_mtu}
            ip link set dev ${vf_name} up
        else
            echo "VF ${vf_name} is already part of bridge ${bridge_name}"
        fi
    else
        echo "No VFs found for ${pf_device}"
    fi
}

delete_vf_from_bridge() {
    local pf_device=$1
    vf_device=$(find /sys/class/net/"${pf_device}"/device/virtfn0/net -mindepth 1 -maxdepth 1 -type d)
    if [ -z "${vf_device}" ]; then
        echo "No VF found, no need to delete VF from ${pf_device}"
        return 0
    fi

    vf_name=$(basename "${vf_device}")
    if ! ip link show master ${bridge_name} | grep -q "${vf_name}"; then
        echo "VF ${vf_name} is not connected to the bridge, no need to delete VF from ${pf_device}"
        return 0
    fi

    if ip link set "${vf_name}" nomaster; then
      echo "disconnected VF ${vf_name} from bridge"
      return 0
    else
      echo "failed to disconnect VF ${vf_name} from bridge"
      return 1
    fi
}

if [[ -z "$device_pci_address" ]]; then
    echo "device_pci_address environment does not exist"
    exit 1
fi

if [[ -z "${num_of_vfs}" ]]; then
  export num_of_vfs=16
fi


while true; do
    p0="${device_pci_address}.0"
    pf_device_p0=$(get_net_devices_from_pci ${p0})

    create_VFs ${pf_device_p0}

    p1="${device_pci_address}.1"
    pf_device_p1=$(get_net_devices_from_pci ${p1})
    if [ -d "${pci_sys_dir}/${p1}" ]; then
        deviceID=$(cat ${pci_sys_dir}/${p1}/device)
        for dpu_device in "${dpu_device_list[@]}"; do
            if [ "${dpu_device}" = "${deviceID}" ]; then
                create_VFs ${pf_device_p1}
                break
            fi
        done
    fi

    bridge_check

    # Add VF0 of the PF0 device to the bridge
    trap "delete_vf_from_bridge ${pf_device_p0}" EXIT
    add_vf_to_bridge ${pf_device_p0}

    touch /tmp/hostnetwork_succeed
    sleep 5
done
