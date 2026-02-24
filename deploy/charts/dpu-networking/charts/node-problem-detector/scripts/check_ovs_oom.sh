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

# Check for recent OOM kills of OVS processes
if nsenter --target 1 --mount journalctl --since "5 minutes ago" 2> /dev/null | grep "Out of memory: Killed process" | grep -q -e ovs-vswitchd -e ovsdb-server; then
	echo "OVS process was killed by OOM in the last 5 minutes"
	exit 1
fi
exit 0
