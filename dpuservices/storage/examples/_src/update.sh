#!/usr/bin/env bash

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

: ${YQ:?env not set}

# Static mappings for mode folders to section headers
declare -A MODE_HEADERS=(
	["non-trusted-host"]="### Non-Trusted Host Scenarios"
	["trusted-k8s-cluster"]="### Trusted Kubernetes Cluster Scenarios"
)

# Static mappings for use-case folders to subsection headers
declare -A USECASE_HEADERS=(
	["nvme-hotplug-pf"]="#### NVMe with hot-plug Physical Functions"
	["nvme-vf-on-static-pf"]="#### NVMe Virtual Functions on static Physical Functions"
	["nvme-static-pf"]="#### NVMe with static Physical Functions"
	["virtiofs-hotplug-pf"]="#### VirtioFS with hot-plug Physical Functions"
)

# Function to extract resource name and kind from YAML file
extract_yaml_metadata() {
	local yaml_file=$1
	local name
	local kind
	name=$(${YQ} e '.metadata.name' "$yaml_file")
	kind=$(${YQ} e '.kind' "$yaml_file")
	echo "${name} ${kind}"
}

# Function to rebuild the Available Scenarios section in README.md
rebuild_readme_scenarios() {
	local readme_file="../scenarios/README.md"
	local temp_file
	temp_file=$(mktemp)
	local scenarios_dir="../scenarios"

	# Check if README file exists
	if [[ ! -f "$readme_file" ]]; then
		echo "Error: README file $readme_file does not exist" >&2
		return 1
	fi

	# Check if start and end markers are present
	local start_marker="^\[update\.sh\]: <> (start)$"
	local end_marker="^\[update\.sh\]: <> (end)$"

	if ! grep -q "$start_marker" "$readme_file"; then
		echo "Error: Start marker '[update.sh]: <> (start)' not found in $readme_file" >&2
		return 1
	fi

	if ! grep -q "$end_marker" "$readme_file"; then
		echo "Error: End marker '[update.sh]: <> (end)' not found in $readme_file" >&2
		return 1
	fi

	# Copy everything before the start marker to temp file
	sed "/$start_marker/q" "$readme_file" > "$temp_file"

	# Generate the scenarios content
	local content_file
	content_file=$(mktemp)

	# Process each mode directory
	readarray -t mode_dirs < <(find "$scenarios_dir" -mindepth 1 -maxdepth 1 -type d -print0 | LC_ALL=C sort -z | tr '\0' '\n')
	for mode_dir in "${mode_dirs[@]}"; do
		if [[ -d "$mode_dir" && "$(basename "$mode_dir")" != "README.md" ]]; then
			local mode
			mode=$(basename "$mode_dir")
			local mode_header="${MODE_HEADERS[$mode]:-### $mode}"

			echo "$mode_header" >> "$content_file"
			echo "" >> "$content_file"

			# Process each use-case directory within the mode
			readarray -t usecase_dirs < <(find "$mode_dir" -mindepth 1 -maxdepth 1 -type d -print0 | LC_ALL=C sort -z | tr '\0' '\n')
			for usecase_dir in "${usecase_dirs[@]}"; do
				if [[ -d "$usecase_dir" ]]; then
					local usecase
					usecase=$(basename "$usecase_dir")
					local usecase_header="${USECASE_HEADERS[$usecase]:-#### $usecase}"

					{
						echo "$usecase_header"
						echo ""
						echo '```shell'
						echo "cat $mode/$usecase/*.yaml | envsubst | kubectl apply -f -"
						echo '```'
						echo ""
						echo "This will deploy the following objects:"
						echo ""
					} >> "$content_file"

					# Process each YAML file in the use-case directory
					readarray -t yaml_files < <(find "$usecase_dir" -maxdepth 1 -name "*.yaml" -type f -print0 | LC_ALL=C sort -z | tr '\0' '\n')
					for yaml_file in "${yaml_files[@]}"; do
						if [[ -f "$yaml_file" ]]; then
							local yaml_basename
							local resource_name
							local resource_kind
							yaml_basename=$(basename "$yaml_file")
							read -r resource_name resource_kind < <(extract_yaml_metadata "$yaml_file")

							# Create details section for each YAML file
							{
								echo "<details markdown=\"1\"><summary>$resource_kind $resource_name</summary>"
								echo ""
								echo "[embedmd]:#($mode/$usecase/$yaml_basename)"
								echo '```yaml'
								echo '```'
								echo "</details>"
								echo ""
							} >> "$content_file"
						fi
					done

					# Process workload YAML files if workload directory exists
					if [[ -d "$usecase_dir/workload" ]]; then
						{
							echo "##### Example Workloads"
							echo ""
							echo '```shell'
							echo "cat $mode/$usecase/workload/*.yaml | envsubst | kubectl apply -f -"
							echo '```'
							echo ""
							echo "This will deploy the following objects:"
							echo ""
						} >> "$content_file"

						readarray -t workload_yaml_files < <(find "$usecase_dir/workload" -maxdepth 1 -name "*.yaml" -type f -print0 2> /dev/null | LC_ALL=C sort -z | tr '\0' '\n')
						for yaml_file in "${workload_yaml_files[@]}"; do
							if [[ -f "$yaml_file" ]]; then
								local yaml_basename
								local resource_name
								local resource_kind
								yaml_basename=$(basename "$yaml_file")
								read -r resource_name resource_kind < <(extract_yaml_metadata "$yaml_file")

								# Create details section for each workload YAML file
								{
									echo "<details markdown=\"1\"><summary>$resource_kind $resource_name</summary>"
									echo ""
									echo "[embedmd]:#($mode/$usecase/workload/$yaml_basename)"
									echo '```yaml'
									echo '```'
									echo "</details>"
									echo ""
								} >> "$content_file"
							fi
						done
					fi
				fi
			done
		fi
	done

	# Add the generated content to temp file
	cat "$content_file" >> "$temp_file"

	# Add everything from the end marker onwards
	sed -n "/$end_marker/,\$p" "$readme_file" >> "$temp_file"

	# Clean up temporary content file
	rm "$content_file"

	# Replace the original README with the updated content
	mv "$temp_file" "$readme_file"
	echo "README.md scenarios section has been rebuilt"
}

# apply manifests that are common for all scenarios
function apply_common_manifests() {
	local target_dir=$1
	cp -r manifests/bfb/*.yaml "$target_dir"
	cp -r manifests/network/*.yaml "$target_dir"
	cp -r manifests/snap-host-controller/*.yaml "$target_dir"
	cp -r manifests/snap-node-driver/*.yaml "$target_dir"
	cp -r manifests/doca-snap/*.yaml "$target_dir"
}

# apply manifests that are common for all block storage scenarios
function apply_block_common_manifests() {
	local target_dir=$1
	cp -r manifests/spdk-csi/*.yaml "$target_dir"
	cp -r manifests/block-storage-dpu-plugin/*.yaml "$target_dir"
}

# apply manifests that are common for all trusted host scenarios
function apply_trusted_host_manifests() {
	local target_dir=$1
	cp -r manifests/snap-csi-plugin/*.yaml "$target_dir"
}

echo "Removing everything from ../scenarios folder except README.md"
find ../scenarios -mindepth 1 -not -name "README.md" -delete

echo "Build examples for non-trusted-host/nvme-hotplug-pf"
target_dir="../scenarios/non-trusted-host/nvme-hotplug-pf"
mkdir -p "$target_dir/workload"

apply_common_manifests "$target_dir"
apply_block_common_manifests "$target_dir"
cp -r manifests/doca-snap/nvme-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/non-trusted-host/*.yaml "$target_dir"
cp -r manifests/dpuflavor/nvme-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/workload/non-trusted-host/nvme-hotplug-pf/*.yaml "$target_dir/workload"

echo "Build examples for non-trusted-host/nvme-vf-on-static-pf"
target_dir="../scenarios/non-trusted-host/nvme-vf-on-static-pf"
mkdir -p "$target_dir/workload"

apply_common_manifests "$target_dir"
apply_block_common_manifests "$target_dir"
cp -r manifests/doca-snap/nvme-vf-on-static-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/non-trusted-host/*.yaml "$target_dir"
cp -r manifests/dpuflavor/nvme-vf-on-static-pf/*.yaml "$target_dir"
cp -r manifests/workload/non-trusted-host/nvme-vf-on-static-pf/*.yaml "$target_dir/workload"

echo "Build examples for non-trusted-host/virtiofs-hotplug-pf"
target_dir="../scenarios/non-trusted-host/virtiofs-hotplug-pf"
mkdir -p "$target_dir/workload"
apply_common_manifests "$target_dir"
cp -r manifests/nfs-csi/*.yaml "$target_dir"
cp -r manifests/fs-storage-dpu-plugin/*.yaml "$target_dir"
cp -r manifests/dpudeployment/non-trusted-host/virtiofs-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/dpuflavor/virtiofs-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/workload/non-trusted-host/virtiofs-hotplug-pf/*.yaml "$target_dir/workload"

echo "Build examples for non-trusted-host/nvme-static-pf"
target_dir="../scenarios/non-trusted-host/nvme-static-pf"
mkdir -p "$target_dir/workload"
apply_common_manifests "$target_dir"
apply_block_common_manifests "$target_dir"
cp -r manifests/doca-snap/nvme-static-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/non-trusted-host/*.yaml "$target_dir"
cp -r manifests/dpuflavor/nvme-static-pf/*.yaml "$target_dir"
cp -r manifests/workload/non-trusted-host/nvme-static-pf/*.yaml "$target_dir/workload"

echo "Build examples for trusted-k8s-cluster/nvme-hotplug-pf"
target_dir="../scenarios/trusted-k8s-cluster/nvme-hotplug-pf"
mkdir -p "$target_dir/workload"
apply_common_manifests "$target_dir"
apply_block_common_manifests "$target_dir"
apply_trusted_host_manifests "$target_dir"
cp -r manifests/doca-snap/nvme-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/trusted-k8s-cluster/*.yaml "$target_dir"
cp -r manifests/dpuflavor/nvme-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/workload/trusted-k8s-cluster/nvme-hotplug-pf/*.yaml "$target_dir/workload"

echo "Build examples for trusted-k8s-cluster/nvme-vf-on-static-pf"
target_dir="../scenarios/trusted-k8s-cluster/nvme-vf-on-static-pf"
mkdir -p "$target_dir/workload"
apply_common_manifests "$target_dir"
apply_block_common_manifests "$target_dir"
apply_trusted_host_manifests "$target_dir"
cp -r manifests/doca-snap/nvme-vf-on-static-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/trusted-k8s-cluster/*.yaml "$target_dir"
cp -r manifests/dpuflavor/nvme-vf-on-static-pf/*.yaml "$target_dir"
cp -r manifests/workload/trusted-k8s-cluster/nvme-vf-on-static-pf/*.yaml "$target_dir/workload"

echo "Build examples for trusted-k8s-cluster/virtiofs-hotplug-pf"
target_dir="../scenarios/trusted-k8s-cluster/virtiofs-hotplug-pf"
mkdir -p "$target_dir/workload"
apply_common_manifests "$target_dir"
apply_trusted_host_manifests "$target_dir"
cp -r manifests/nfs-csi/*.yaml "$target_dir"
cp -r manifests/fs-storage-dpu-plugin/*.yaml "$target_dir"
cp -r manifests/snap-csi-plugin/virtiofs-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/trusted-k8s-cluster/virtiofs-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/dpuflavor/virtiofs-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/workload/trusted-k8s-cluster/virtiofs-hotplug-pf/*.yaml "$target_dir/workload"

echo "Rebuilding README.md scenarios section"
rebuild_readme_scenarios
