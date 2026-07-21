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

# Check if ovs-vswitchd is running on the host.
# The pod runs with hostPID, so host processes are visible in /proc and
# pidof finds them by name without any privileges, unlike nsenter into the
# host mount namespace, which requires CAP_SYS_ADMIN.
# The node-problem-detector image ships pidof but not pgrep or ps.
if ! pidof ovs-vswitchd > /dev/null 2>&1; then
	echo "ovs-vswitchd is not running"
	exit 1
fi
exit 0
