#!/bin/bash

#  2025 NVIDIA CORPORATION & AFFILIATES
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

# This scipts is responsible to build examples for each supported storage scenario from the common manifests
# Note: the script removes all scenarios and rebuilds them from scratch

set -euo pipefail

# apply manifests that are common for all scenarios
function apply_common_manifests() {
	local target_dir=$1
	cp -r manifests/bfb/*.yaml $target_dir
	cp -r manifests/network/*.yaml $target_dir
	cp -r manifests/snap-configuration/*.yaml $target_dir
	cp -r manifests/snap-controller/*.yaml $target_dir
	cp -r manifests/snap-host-controller/*.yaml $target_dir
	cp -r manifests/snap-node-driver/*.yaml $target_dir
	cp -r manifests/doca-snap/*.yaml $target_dir
}

# apply manifests that are common for all block storage scenarios
function apply_block_common_manifests() {
	local target_dir=$1
	cp -r manifests/spdk-csi/*.yaml $target_dir
	cp -r manifests/block-storage-dpu-plugin/*.yaml $target_dir
}

# apply manifests that are common for all trusted host scenarios
function apply_trusted_host_manifests() {
	local target_dir=$1
	cp -r manifests/snap-csi-plugin/*.yaml $target_dir
}

echo "Removing everything from ../scenarios folder except README.md"
find ../scenarios -mindepth 1 -not -name "README.md" -delete

echo "Build examples for non-trusted-host/nvme-hotplug-pf"
target_dir="../scenarios/non-trusted-host/nvme-hotplug-pf"
mkdir -p $target_dir/workload

apply_common_manifests $target_dir
apply_block_common_manifests $target_dir
cp -r manifests/doca-snap/nvme-hotplug-pf/*.yaml $target_dir
cp -r manifests/dpudeployment/non-trusted-host/*.yaml $target_dir
cp -r manifests/dpuflavor/nvme-hotplug-pf/*.yaml $target_dir
cp -r manifests/workload/non-trusted-host/nvme-hotplug-pf/*.yaml $target_dir/workload

echo "Build examples for non-trusted-host/nvme-vf-on-static-pf"
target_dir="../scenarios/non-trusted-host/nvme-vf-on-static-pf"
mkdir -p $target_dir/workload

apply_common_manifests $target_dir
apply_block_common_manifests $target_dir
cp -r manifests/doca-snap/nvme-vf-on-static-pf/*.yaml $target_dir
cp -r manifests/dpudeployment/non-trusted-host/*.yaml $target_dir
cp -r manifests/dpuflavor/nvme-vf-on-static-pf/*.yaml $target_dir
cp -r manifests/workload/non-trusted-host/nvme-vf-on-static-pf/*.yaml $target_dir/workload

echo "Build examples for non-trusted-host/virtiofs-hotplug-pf"
target_dir="../scenarios/non-trusted-host/virtiofs-hotplug-pf"
mkdir -p $target_dir/workload
apply_common_manifests $target_dir
cp -r manifests/nfs-csi/*.yaml $target_dir
cp -r manifests/fs-storage-dpu-plugin/*.yaml $target_dir
cp -r manifests/dpudeployment/non-trusted-host/virtiofs-hotplug-pf/*.yaml $target_dir
cp -r manifests/dpuflavor/virtiofs-hotplug-pf/*.yaml $target_dir
cp -r manifests/workload/non-trusted-host/virtiofs-hotplug-pf/*.yaml $target_dir/workload

echo "Build examples for trusted-k8s-cluster/nvme-vf-on-static-pf"
target_dir="../scenarios/trusted-k8s-cluster/nvme-vf-on-static-pf"
mkdir -p $target_dir/workload
apply_common_manifests $target_dir
apply_block_common_manifests $target_dir
apply_trusted_host_manifests $target_dir
cp -r manifests/doca-snap/nvme-vf-on-static-pf/*.yaml $target_dir
cp -r manifests/dpudeployment/trusted-k8s-cluster/*.yaml $target_dir
cp -r manifests/dpuflavor/nvme-vf-on-static-pf/*.yaml $target_dir
cp -r manifests/workload/trusted-k8s-cluster/nvme-vf-on-static-pf/*.yaml $target_dir/workload
