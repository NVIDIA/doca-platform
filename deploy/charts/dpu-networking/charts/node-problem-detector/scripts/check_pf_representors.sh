#!/usr/bin/env bash

#  2026 NVIDIA CORPORATION & AFFILIATES
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

# Check that every BlueField PF exposes its host PF representor on the DPU eswitch.
#
# DPF's invariant is one host PF representor per BlueField eswitch-manager PCI
# function (internal/provisioning/utils/pci/pci.go: "Each ECPF has exactly one host
# PF representor"). Dual-socket hosts (BF4) expose each (port x controller) as its own
# PCI function with its own representor, so the counts stay equal for single-socket
# (BF3) and dual-socket (BF4) without hardcoding a number. A shortfall of representors
# means the eswitch is not fully in switchdev mode or a host controller's representor
# is missing.
#
# Functions and ports are classified by stable sysfs attributes, not interface names
# (which differ across BF3/BF4 and driver versions):
#   BlueField PF function : /sys/bus/pci/devices/<addr>/device is a BlueField ID
#       (0xa2d6 BF2, 0xa2dc BF3, 0xa2df BF4) and the function is not a VF (no physfn
#       symlink). Keep the ID list in sync with pci.go.
#   Host PF representor   : /sys/class/net/<dev>/phys_switch_id non-empty (eswitch port)
#       and phys_port_name matches ^(c[0-9]+)?pf[0-9]+$ (pf0, c1pf0). This excludes
#       uplinks (^p[0-9]+$) and VF/SF representors (...pf<M>vf<K> / ...sf<K>).
#
# /sys in the container is the host sysfs mounted read-only, so no nsenter is needed.

bf_pfs=0
for pci in /sys/bus/pci/devices/*; do
	case "$(cat "$pci/device" 2> /dev/null)" in
	0xa2d6 | 0xa2dc | 0xa2df) ;;
	*) continue ;;
	esac
	# VFs have a physfn symlink; count physical functions only.
	[ -e "$pci/physfn" ] && continue
	bf_pfs=$((bf_pfs + 1))
done

# A host PF is identified by its phys_port_name (controller and PF, e.g. c1pf0). The same
# name can appear on several netdevs, so dedup by name: the invariant is one representor
# per BlueField PF function, so the distinct name count is what must match bf_pfs, not the
# raw netdev count.
declare -A pf_seen
for dev in /sys/class/net/*; do
	# Only eswitch (switchdev) ports have a non-empty phys_switch_id.
	[ -n "$(cat "$dev/phys_switch_id" 2> /dev/null)" ] || continue
	name=$(cat "$dev/phys_port_name" 2> /dev/null)
	[[ "$name" =~ ^(c[0-9]+)?pf[0-9]+$ ]] && pf_seen["$name"]=1
done
pf_reps=${#pf_seen[@]}

# No host PF representor at all means the eswitch is not in switchdev mode. This check is
# device-agnostic, so it still fires on a BlueField whose PCI ID is not listed above.
if [ "$pf_reps" -eq 0 ]; then
	echo "No host PF representors found on the DPU eswitch"
	exit 1
fi

# On a recognized BlueField card, expect one host PF representor per PF function; fewer
# means a host controller's representor is missing. More is tolerated: it never indicates
# a missing representor.
if [ "$bf_pfs" -gt 0 ] && [ "$pf_reps" -lt "$bf_pfs" ]; then
	echo "Host PF representors ($pf_reps) fewer than BlueField PFs ($bf_pfs)"
	exit 1
fi
exit 0
