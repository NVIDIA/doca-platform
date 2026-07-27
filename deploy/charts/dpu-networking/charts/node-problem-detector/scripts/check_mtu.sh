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

# Check that at least one physical uplink is configured with a jumbo MTU (>= 9000).
#
# The uplinks are the netdevs named p0..p3. DPF addresses them by this name in the
# DPUFlavor OVS configuration (ovs-vsctl set Interface p0 mtu_request=9216), so these
# names are the contract the data path already relies on. The threshold is >= 9000
# rather than exactly 9216 to tolerate the gap between the uplink MTU and the
# tenant/service MTU (tunnel headers). One uplink jumbo is enough, so a second,
# intentionally-unused port at the default MTU does not fail the check. If no uplink
# exists the uplink check covers that.
#
# /sys in the container is the host sysfs mounted read-only, so no nsenter is needed.

found=0
for p in p0 p1 p2 p3; do
	dev="/sys/class/net/$p"
	[ -e "$dev" ] || continue
	found=1
	mtu=$(cat "$dev/mtu" 2> /dev/null)
	[ -n "$mtu" ] && [ "$mtu" -ge 9000 ] && exit 0
done

# No uplink present: defer to the uplink check.
[ "$found" -eq 0 ] && exit 0

echo "No physical uplink configured with MTU >= 9000"
exit 1
