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

set -euo pipefail

# Default values
HELMFILE_FILE=""
HELM_CHART_VERSION=""
HELM_CHART_NAME="dpf-operator"
HELM_REPO_URL=""
CHART_NAME=""
HELMFILE_BIN="helmfile"
HELM_BIN="helm"
ENVIRONMENT=""
SELECTOR=""
STATE_VALUES_SET=""
COLLECT_ON_FAIL="true"
CLEANUP_ON_FAIL="false"
WAIT="true"
ARTIFACTS_DIR="${HELMFILE_ARTIFACTS_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../../artifacts/helmfile}"
YQ_BIN="${YQ:-"$(which yq || true)"}"
DPFDEV_BIN="${DPFDEV_BIN:-"$(which dpfdev || true)"}"

if ! command -v "$YQ_BIN" &> /dev/null; then
	echo "Error: yq not found. Please install yq and ensure it's in your PATH, or set the YQ environment variable to its location." >&2
	exit 1
fi

# Function to print usage
usage() {
	cat << EOF
Usage: $0 [OPTIONS]

Deploy helm dependencies using helmfile. Supports local files, OCI registries, and traditional Helm repositories.

OPTIONS:
    -f, --file FILE                     Path to helmfile (required for local and remote deployments)
    --repo-url URL                      Repository URL (supports both OCI and HTTPS)
                                        Examples:
                                        - OCI: oci://harbor.example.com/my-chart
                                        - HTTPS: https://charts.example.com
    --chart-name NAME                   Chart name (required for HTTPS repos, optional for OCI)
                                        - For OCI: used if not included in repo-url
                                        - For HTTPS: the chart name to pull from the repository
    --collect-resources-on-fail BOOL    Whether to collect resources on failure (default: true)
    --cleanup-on-fail BOOL              Whether to clean up resources on failure (default: false)
    --wait BOOL                         Whether to wait for resources to be ready (default: true)
    -v, --version VERSION               Chart version to pull from repository
    -n, --name NAME                     Chart name for extraction (default: dpf-operator)
    --environment NAME                  Defines the environment for the deployment (e.g., shared-fs)
    --selector SELECTOR                 Selector for filtering resources (e.g., "app=grafana" or "app!=prometheus")
    --state-values-set KEY=VALUE        Set a helmfile state value (e.g. kata.enabled=true)
    --helmfile-bin PATH                 Path to helmfile binary (default: helmfile)
    --helm-bin PATH                     Path to helm binary (default: helm)
    -h, --help                          Show this help message

EXAMPLES:
    # Deploy from local file
    $0 -f deploy/helmfiles/prereqs.yaml

    # Deploy from OCI registry (chart in URL)
    $0 --repo-url "oci://harbor.example.com/my-chart" -v "v1.0.0" -f "prereqs.yaml"

    # Deploy from OCI registry (chart as separate param)
    $0 --repo-url "oci://harbor.example.com" --chart-name "my-chart" -v "v1.0.0" -f "prereqs.yaml"

    # Deploy from HTTPS Helm repository
    $0 --repo-url "https://charts.example.com" --chart-name "dpf-operator" -v "v1.0.0" -f "prereqs.yaml"
EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
	case $1 in
	-f | --file)
		HELMFILE_FILE="$2"
		shift 2
		;;
	--repo-url)
		HELM_REPO_URL="$2"
		shift 2
		;;
	--chart-name)
		CHART_NAME="$2"
		shift 2
		;;
	--collect-resources-on-fail)
		COLLECT_ON_FAIL="$2"
		shift 2
		;;
	--cleanup-on-fail)
		CLEANUP_ON_FAIL="$2"
		shift 2
		;;
	--wait)
		WAIT="$2"
		shift 2
		;;
	-v | --version)
		HELM_CHART_VERSION="$2"
		shift 2
		;;
	-n | --name)
		HELM_CHART_NAME="$2"
		shift 2
		;;
	--environment)
		ENVIRONMENT="$2"
		shift 2
		;;
	--selector)
		SELECTOR="$2"
		shift 2
		;;
	--state-values-set)
		STATE_VALUES_SET="$2"
		shift 2
		;;
	--helmfile-bin)
		HELMFILE_BIN="$2"
		shift 2
		;;
	--helm-bin)
		HELM_BIN="$2"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "Unknown option: $1"
		usage
		exit 1
		;;
	esac
done

# Detect repository type (OCI or HTTPS)
REPO_TYPE=""
if [[ -n "$HELM_REPO_URL" ]]; then
	if [[ "$HELM_REPO_URL" =~ ^oci:// ]]; then
		REPO_TYPE="oci"
	elif [[ "$HELM_REPO_URL" =~ ^https?:// ]]; then
		REPO_TYPE="https"
	else
		echo "Error: --repo-url must start with 'oci://' or 'https://' (got: $HELM_REPO_URL)"
		usage
		exit 1
	fi

	# If -n is not explicitly set, derive extraction name from chart name or URL
	if [[ "$HELM_CHART_NAME" == "dpf-operator" ]]; then
		if [[ -n "$CHART_NAME" ]]; then
			# Use the chart name
			HELM_CHART_NAME="$CHART_NAME"
		elif [[ "$REPO_TYPE" == "oci" ]]; then
			# Extract chart name from OCI URL
			HELM_CHART_NAME=$(basename "$HELM_REPO_URL")
		fi
	fi
fi

# Validate required parameters
if [[ -z "$HELMFILE_FILE" && -z "$HELM_REPO_URL" ]]; then
	echo "Error: Either --file (for local) or --repo-url (for remote) must be specified"
	usage
	exit 1
fi

if [[ -n "$HELM_REPO_URL" && -z "$HELM_CHART_VERSION" ]]; then
	echo "Error: --version is required when using --repo-url"
	usage
	exit 1
fi

if [[ -n "$HELM_REPO_URL" && -z "$HELMFILE_FILE" ]]; then
	echo "Error: --file is required when using --repo-url"
	usage
	exit 1
fi

if [[ "$REPO_TYPE" == "https" && -z "$CHART_NAME" ]]; then
	echo "Error: --chart-name is required when using HTTPS repository"
	usage
	exit 1
fi

# Function to deploy from OCI registry
deploy_from_oci() {
	local ociUrl="$HELM_REPO_URL"
	# Append chart name if provided
	if [[ -n "$CHART_NAME" ]]; then
		ociUrl="${HELM_REPO_URL}/${CHART_NAME}"
	fi
	echo "Deploying from OCI registry: $ociUrl"
	deploy_from_chart "$ociUrl"
}

# Function to deploy from local file
deploy_from_local() {
	echo "Deploying from local file: $HELMFILE_FILE"

	# Check if file exists
	if [[ ! -f "$HELMFILE_FILE" ]]; then
		echo "Error: Helmfile not found: $HELMFILE_FILE"
		exit 1
	fi

	deploy_helmfile "$HELMFILE_FILE"
}

# Function to deploy from traditional Helm repository
deploy_from_repo() {
	echo "Deploying from Helm repository: $HELM_REPO_URL"

	# Derive repo name from URL by removing protocol and sanitizing
	# Example: https://helm.ngc.nvidia.com/nvidia/doca -> helm-ngc-nvidia-com-nvidia-doca
	local repoName=$(echo "$HELM_REPO_URL" | sed -e 's|^https\?://||' -e 's|[^a-zA-Z0-9]|-|g' -e 's|^-*||' -e 's|-*$||')

	# Add the repository (force add if it already exists)
	echo "Adding Helm repository: $repoName"
	"$HELM_BIN" repo add "$repoName" "$HELM_REPO_URL" --force-update

	# Update repositories
	echo "Updating Helm repositories"
	"$HELM_BIN" repo update

	# Pull the chart using the repo name and chart name
	deploy_from_chart "$repoName/$CHART_NAME"

	# Clean up: remove the temporary repository
	"$HELM_BIN" repo remove "$repoName" 2> /dev/null || true
}

# Generic function to deploy from chart (OCI or Helm repository)
deploy_from_chart() {
	local chartSource="$1"

	# Create temporary directory
	tmpDir=$(mktemp -d)
	trap 'rm -rf "$tmpDir"' EXIT

	# Pull the chart
	echo "Pulling chart: $chartSource"
	"$HELM_BIN" pull "$chartSource" --version "$HELM_CHART_VERSION" --untar --untardir "$tmpDir"

	# Determine helmfile path
	helmfilePath="$tmpDir/$HELM_CHART_NAME/$HELMFILE_FILE"

	# Check if helmfile exists
	if [[ ! -f "$helmfilePath" ]]; then
		echo "Error: Helmfile not found at expected path: $helmfilePath"
		echo "Available files in $tmpDir/$HELM_CHART_NAME/$(dirname "$HELMFILE_FILE"):"
		ls -la "$tmpDir/$HELM_CHART_NAME/$(dirname "$HELMFILE_FILE")" 2> /dev/null || echo "Directory not found"
		exit 1
	fi

	deploy_helmfile "$helmfilePath"
}

# Function to collect resources on failure
collect_resources_on_failure() {
	if [[ "$COLLECT_ON_FAIL" != "true" ]]; then
		return 0
	fi

	if ! command -v "$DPFDEV_BIN" &> /dev/null; then
		echo "Warning: dpfdev not found at '$DPFDEV_BIN', skipping resource collection" >&2
		return 0
	fi

	echo "Collecting resources to $ARTIFACTS_DIR..." >&2
	"$DPFDEV_BIN" collect --output-dir "$ARTIFACTS_DIR" ${HELMFILE_INVENTORY_DIR:+--inventory-dir "$HELMFILE_INVENTORY_DIR"} \
		|| echo "Warning: resource collection failed, continuing..." >&2
}

# Generic function to deploy helmfile
deploy_helmfile() {
	local helmfilePath="$1"

	# Make apply-crds.sh executable
	chmod +x "$(dirname "$helmfilePath")/apply-crds.sh"

	# Create temp helmfile with cleanupOnFail set based on parameter.
	# Cannot use process substitution `<()` here, the fd would close before command execution.
	# Creating a persistent temp file instead.
	# Select the document that has releases set and update it. The first document might be the environments document
	# which cannot exist in the same document as releases.
	${YQ_BIN} "select(has(\"releases\")) |= (.helmDefaults.cleanupOnFail=${CLEANUP_ON_FAIL} | .helmDefaults.wait=${WAIT})" "$helmfilePath" > "$helmfilePath.tmp"
	# Delete tmp helmfile on exit
	trap "rm -f $helmfilePath.tmp" EXIT

	# Build helmfile command
	local helmfile_cmd=(
		"$HELMFILE_BIN" apply
		--suppress-diff
		--skip-diff-on-install
		--concurrency 0
		--color
		--hide-notes
		--allow-no-matching-release
		--file "$helmfilePath.tmp"
		--helm-binary "$HELM_BIN"
	)

	# Add environment if specified
	if [[ -n "$ENVIRONMENT" ]]; then
		helmfile_cmd+=("--environment" "$ENVIRONMENT")
	fi

	# Add selector if specified
	if [[ -n "$SELECTOR" ]]; then
		helmfile_cmd+=("--selector" "$SELECTOR")
	fi

	# Add state values file if specified
	# Add state values set if specified
	if [[ -n "$STATE_VALUES_SET" ]]; then
		helmfile_cmd+=("--state-values-set" "$STATE_VALUES_SET")
	fi

	# Deploy using helmfile
	if ! "${helmfile_cmd[@]}"; then
		collect_resources_on_failure
		return 1
	fi
}

# Main execution
if [[ "$REPO_TYPE" == "oci" ]]; then
	deploy_from_oci
elif [[ "$REPO_TYPE" == "https" ]]; then
	deploy_from_repo
else
	deploy_from_local
fi
