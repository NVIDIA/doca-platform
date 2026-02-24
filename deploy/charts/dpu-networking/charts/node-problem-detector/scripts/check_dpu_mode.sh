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

# Check if DPU is in DPU mode (not SEPARATED mode)
# Note: This requires mstflint/mlxconfig to be available in the container or on the host
if nsenter --target 1 --mount which mlxconfig > /dev/null 2>&1; then
	MODE=$(nsenter --target 1 --mount mlxconfig -d /dev/mst/mt*_pciconf0 q INTERNAL_CPU_MODEL | awk '/INTERNAL_CPU_MODEL/{print $2}')
	if [[ ! "$MODE" =~ "EMBEDDED" ]]; then
		echo "DPU is in $MODE mode, expected EMBEDDED mode"
		exit 1
	fi
fi
exit 0
