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

# Check if SR-IOV VF representors are present.
# /sys in the container is the host sysfs mounted read-only, and sysfs net
# entries reflect the network namespace of the mount, i.e. the host, so no
# nsenter is needed.
# grep -c always prints a count (including 0), appending a fallback via
# `|| echo 0` would duplicate it and break the integer comparison.
VF_COUNT=$(ls /sys/class/net/ 2> /dev/null | grep -c '^pf0vf')
if [ "${VF_COUNT:-0}" -eq 0 ]; then
	echo "No VF representors found (pf0vf*)"
	exit 1
fi
exit 0
