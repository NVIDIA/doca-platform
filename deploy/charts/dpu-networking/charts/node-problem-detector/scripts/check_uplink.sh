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

# Check that at least one physical uplink is up.
#
# The uplinks are the netdevs named p0..p3. DPF addresses them by this name in the
# DPUFlavor OVS configuration (e.g. ovs-vsctl set Interface p0 ...), so these names are
# the contract the data path already relies on. One uplink up is enough for a working
# physical path, so a second, intentionally-uncabled port does not fail the check.
#
# /sys in the container is the host sysfs mounted read-only, so no nsenter is needed.

found=0
for p in p0 p1 p2 p3; do
	dev="/sys/class/net/$p"
	[ -e "$dev" ] || continue
	found=1
	[ "$(cat "$dev/operstate" 2> /dev/null)" = "up" ] && exit 0
done

if [ "$found" -eq 0 ]; then
	echo "No physical uplink (p0-p3) found"
else
	echo "No physical uplink is up"
fi
exit 1
