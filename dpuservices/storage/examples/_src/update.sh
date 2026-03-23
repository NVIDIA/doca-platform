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

# Static mappings for helm component names to friendly titles
declare -A HELM_COMPONENT_TITLES=(
	["snap-host-controller"]="SNAP Host Controller"
	["snap-csi-plugin-controller"]="SNAP CSI Plugin Controller"
	["spdk-csi-controller"]="SPDK CSI Controller"
	["nfs-csi-controller"]="NFS CSI Controller"
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
					} >> "$content_file"

					if [[ -d "$usecase_dir/credentials" ]]; then
						{
							echo "##### Create Vendor CSI Controller Credentials"
							echo ""
							echo "Create the credential requests for the host-cluster vendor CSI controllers before installing the charts:"
							echo ""
							echo '```shell'
							echo "kubectl apply -f $mode/$usecase/credentials/"
						} >> "$content_file"

						readarray -t credential_yaml_files < <(find "$usecase_dir/credentials" -maxdepth 1 -name "*.yaml" -type f -print0 2> /dev/null | LC_ALL=C sort -z | tr '\0' '\n')

						{
							echo '```'
							echo ""
							echo "This will create the following objects:"
							echo ""
						} >> "$content_file"

						for credential_yaml_file in "${credential_yaml_files[@]}"; do
							if [[ -f "$credential_yaml_file" ]]; then
								local credential_yaml_basename
								local credential_name
								local credential_kind
								credential_yaml_basename=$(basename "$credential_yaml_file")
								read -r credential_name credential_kind < <(extract_yaml_metadata "$credential_yaml_file")

								{
									echo "<details markdown=\"1\"><summary>$credential_kind $credential_name</summary>"
									echo ""
									echo "[embedmd]:#($mode/$usecase/credentials/$credential_yaml_basename)"
									echo '```yaml'
									echo '```'
									echo "</details>"
									echo ""
								} >> "$content_file"
							fi
						done
					fi

					# Process helm component directories if helm directory exists
					if [[ -d "$usecase_dir/helm" ]]; then
						local helm_component_dirs=()
						local component_dir
						for component_name in snap-host-controller snap-csi-plugin-controller spdk-csi-controller nfs-csi-controller; do
							component_dir="$usecase_dir/helm/$component_name"
							if [[ -d "$component_dir" ]]; then
								helm_component_dirs+=("$component_dir")
							fi
						done

						readarray -t extra_helm_component_dirs < <(find "$usecase_dir/helm" -mindepth 1 -maxdepth 1 -type d -print0 | LC_ALL=C sort -z | tr '\0' '\n')
						for component_dir in "${extra_helm_component_dirs[@]}"; do
							component_name=$(basename "$component_dir")
							case "$component_name" in
							snap-host-controller | snap-csi-plugin-controller | spdk-csi-controller | nfs-csi-controller) ;;
							*)
								helm_component_dirs+=("$component_dir")
								;;
							esac
						done

						for component_dir in "${helm_component_dirs[@]}"; do
							local component_name
							local component_title
							local component_description
							component_name=$(basename "$component_dir")
							component_title="${HELM_COMPONENT_TITLES[$component_name]:-$component_name}"
							if [[ "$component_name" == "snap-csi-plugin-controller" ]]; then
								component_description="Install the ${component_title} that runs on the host cluster for this scenario. The node part is deployed later with the DPUDeployment:"
							else
								component_description="Install the ${component_title} that runs on the host cluster for this scenario:"
							fi

							{
								echo "##### Install ${component_title} on the Host Cluster"
								echo ""
								echo "$component_description"
								echo ""
							} >> "$content_file"

							if [[ -f "$component_dir/install-http.txt" ]]; then
								{
									echo "**HTTP Registry**"
									echo ""
									echo 'If the `$DPF_CHART_REPO` is an HTTP Registry use this command:'
									echo ""
									echo '[embedmd]:# ('"$mode/$usecase/helm/$component_name/install-http.txt"' sh)'
									echo '```sh'
									echo '```'
									echo ""
								} >> "$content_file"
							fi

							if [[ -f "$component_dir/install-oci.txt" ]]; then
								{
									echo "**OCI Registry**"
									echo ""
									echo 'For development purposes, if the `$DPF_CHART_REPO` is an OCI Registry use this command:'
									echo ""
									echo '[embedmd]:# ('"$mode/$usecase/helm/$component_name/install-oci.txt"' sh)'
									echo '```sh'
									echo '```'
									echo ""
								} >> "$content_file"
							elif [[ -f "$component_dir/install.txt" ]]; then
								{
									echo '[embedmd]:# ('"$mode/$usecase/helm/$component_name/install.txt"' sh)'
									echo '```sh'
									echo '```'
									echo ""
								} >> "$content_file"
							fi

							if [[ -f "$component_dir/values.yaml" ]]; then
								{
									echo "<details markdown=\"1\"><summary>Helm values</summary>"
									echo ""
									echo "[embedmd]:#($mode/$usecase/helm/$component_name/values.yaml)"
									echo '```yaml'
									echo '```'
									echo "</details>"
									echo ""
								} >> "$content_file"
							fi
						done
					fi

					{
						echo "##### Apply DPU-side Storage Resources"
						echo ""
						if [[ -d "$usecase_dir/helm" ]]; then
							echo "After the host-cluster controllers are installed, apply the DPU-side resources for this scenario:"
						else
							echo "Apply the DPU-side resources for this scenario:"
						fi
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
	cp -r manifests/dpuservicenad/*.yaml "$target_dir"
	cp -r manifests/network/*.yaml "$target_dir"
	cp -r manifests/snap-node-driver/*.yaml "$target_dir"
	cp -r manifests/doca-snap/*.yaml "$target_dir"
}

# copy a helm component (install.txt + values.yaml) into the scenario
function copy_helm_component_with_values() {
	local target_dir=$1
	local component=$2
	local values_file=$3
	local scenario_dir
	scenario_dir=${target_dir#../scenarios/}
	mkdir -p "$target_dir/helm/$component"
	for install_file in manifests/helm/"$component"/install*.txt; do
		if [[ -f "$install_file" ]]; then
			local install_basename
			install_basename=$(basename "$install_file")
			sed "s|-f values.yaml|-f ${scenario_dir}/helm/${component}/values.yaml|" \
				"$install_file" > "$target_dir/helm/$component/$install_basename"
		fi
	done
	cp "manifests/helm/$component/$values_file" "$target_dir/helm/$component/values.yaml"
}

function copy_helm_component() {
	copy_helm_component_with_values "$1" "$2" values.yaml
}

function move_credential_requests() {
	local target_dir=$1
	readarray -t credential_files < <(find "$target_dir" -maxdepth 1 -name "*credentialrequest*.yaml" -type f -print0 2> /dev/null | LC_ALL=C sort -z | tr '\0' '\n')
	if [[ ${#credential_files[@]} -eq 0 ]]; then
		return
	fi

	mkdir -p "$target_dir/credentials"
	for credential_file in "${credential_files[@]}"; do
		mv "$credential_file" "$target_dir/credentials/"
	done
}

# apply manifests that are common for all block storage scenarios
function apply_block_common_manifests() {
	local target_dir=$1
	cp -r manifests/spdk-csi/*.yaml "$target_dir"
	cp -r manifests/block-storage-dpu-plugin/*.yaml "$target_dir"
	move_credential_requests "$target_dir"
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
copy_helm_component "$target_dir" snap-host-controller
apply_block_common_manifests "$target_dir"
copy_helm_component "$target_dir" spdk-csi-controller
cp -r manifests/doca-snap/nvme-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/non-trusted-host/*.yaml "$target_dir"
cp -r manifests/dpuflavor/nvme-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/workload/non-trusted-host/nvme-hotplug-pf/*.yaml "$target_dir/workload"

echo "Build examples for non-trusted-host/nvme-vf-on-static-pf"
target_dir="../scenarios/non-trusted-host/nvme-vf-on-static-pf"
mkdir -p "$target_dir/workload"

apply_common_manifests "$target_dir"
copy_helm_component "$target_dir" snap-host-controller
apply_block_common_manifests "$target_dir"
copy_helm_component "$target_dir" spdk-csi-controller
cp -r manifests/doca-snap/nvme-vf-on-static-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/non-trusted-host/*.yaml "$target_dir"
cp -r manifests/dpuflavor/nvme-vf-on-static-pf/*.yaml "$target_dir"
cp -r manifests/workload/non-trusted-host/nvme-vf-on-static-pf/*.yaml "$target_dir/workload"

echo "Build examples for non-trusted-host/virtiofs-hotplug-pf"
target_dir="../scenarios/non-trusted-host/virtiofs-hotplug-pf"
mkdir -p "$target_dir/workload"
apply_common_manifests "$target_dir"
copy_helm_component "$target_dir" snap-host-controller
copy_helm_component "$target_dir" nfs-csi-controller
cp -r manifests/nfs-csi/*.yaml "$target_dir"
move_credential_requests "$target_dir"
cp -r manifests/fs-storage-dpu-plugin/*.yaml "$target_dir"
cp -r manifests/dpudeployment/non-trusted-host/virtiofs-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/dpuflavor/virtiofs-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/workload/non-trusted-host/virtiofs-hotplug-pf/*.yaml "$target_dir/workload"

echo "Build examples for non-trusted-host/nvme-static-pf"
target_dir="../scenarios/non-trusted-host/nvme-static-pf"
mkdir -p "$target_dir/workload"
apply_common_manifests "$target_dir"
copy_helm_component "$target_dir" snap-host-controller
apply_block_common_manifests "$target_dir"
copy_helm_component "$target_dir" spdk-csi-controller
cp -r manifests/doca-snap/nvme-static-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/non-trusted-host/*.yaml "$target_dir"
cp -r manifests/dpuflavor/nvme-static-pf/*.yaml "$target_dir"
cp -r manifests/workload/non-trusted-host/nvme-static-pf/*.yaml "$target_dir/workload"

echo "Build examples for trusted-k8s-cluster/nvme-hotplug-pf"
target_dir="../scenarios/trusted-k8s-cluster/nvme-hotplug-pf"
mkdir -p "$target_dir/workload"
apply_common_manifests "$target_dir"
copy_helm_component "$target_dir" snap-host-controller
copy_helm_component_with_values "$target_dir" snap-csi-plugin-controller values-nvme.yaml
apply_block_common_manifests "$target_dir"
copy_helm_component "$target_dir" spdk-csi-controller
apply_trusted_host_manifests "$target_dir"
cp -r manifests/doca-snap/nvme-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/trusted-k8s-cluster/*.yaml "$target_dir"
cp -r manifests/dpuflavor/nvme-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/workload/trusted-k8s-cluster/nvme-hotplug-pf/*.yaml "$target_dir/workload"

echo "Build examples for trusted-k8s-cluster/nvme-vf-on-static-pf"
target_dir="../scenarios/trusted-k8s-cluster/nvme-vf-on-static-pf"
mkdir -p "$target_dir/workload"
apply_common_manifests "$target_dir"
copy_helm_component "$target_dir" snap-host-controller
copy_helm_component_with_values "$target_dir" snap-csi-plugin-controller values-nvme.yaml
apply_block_common_manifests "$target_dir"
copy_helm_component "$target_dir" spdk-csi-controller
apply_trusted_host_manifests "$target_dir"
cp -r manifests/doca-snap/nvme-vf-on-static-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/trusted-k8s-cluster/*.yaml "$target_dir"
cp -r manifests/dpuflavor/nvme-vf-on-static-pf/*.yaml "$target_dir"
cp -r manifests/workload/trusted-k8s-cluster/nvme-vf-on-static-pf/*.yaml "$target_dir/workload"

echo "Build examples for trusted-k8s-cluster/virtiofs-hotplug-pf"
target_dir="../scenarios/trusted-k8s-cluster/virtiofs-hotplug-pf"
mkdir -p "$target_dir/workload"
apply_common_manifests "$target_dir"
copy_helm_component "$target_dir" snap-host-controller
copy_helm_component_with_values "$target_dir" snap-csi-plugin-controller values-virtiofs.yaml
copy_helm_component "$target_dir" nfs-csi-controller
apply_trusted_host_manifests "$target_dir"
cp -r manifests/nfs-csi/*.yaml "$target_dir"
move_credential_requests "$target_dir"
cp -r manifests/fs-storage-dpu-plugin/*.yaml "$target_dir"
cp -r manifests/snap-csi-plugin/virtiofs-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/dpudeployment/trusted-k8s-cluster/virtiofs-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/dpuflavor/virtiofs-hotplug-pf/*.yaml "$target_dir"
cp -r manifests/workload/trusted-k8s-cluster/virtiofs-hotplug-pf/*.yaml "$target_dir/workload"

echo "Rebuilding README.md scenarios section"
rebuild_readme_scenarios
