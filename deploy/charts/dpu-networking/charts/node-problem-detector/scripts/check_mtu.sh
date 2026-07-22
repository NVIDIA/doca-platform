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

# Check whether p0 is configured with an MTU of at least 9000.
# /sys in the container is the host sysfs mounted read-only, and sysfs net
# entries reflect the network namespace of the mount, i.e. the host, so no
# nsenter is needed. Passes when p0 does not exist, the uplink check covers
# that case.
CURRENT_MTU=$(cat /sys/class/net/p0/mtu 2> /dev/null)
if [ -n "$CURRENT_MTU" ] && [ "$CURRENT_MTU" -lt 9000 ]; then
	echo "MTU is $CURRENT_MTU, expected 9000 or higher"
	exit 1
fi
exit 0
