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
HELM_CHART_OCI=""
HELM_CHART_VERSION=""
HELM_CHART_NAME="dpf-operator"
HELM_REPO_URL=""
HELM_REPO_NAME=""
HELMFILE_BIN="helmfile"
HELM_BIN="helm"
ENVIRONMENT=""
SELECTOR=""

# Function to print usage
usage() {
	cat << EOF
Usage: $0 [OPTIONS]

Deploy helm dependencies using helmfile. Supports local files, OCI registries, and traditional Helm repositories.

OPTIONS:
    -f, --file FILE           Path to helmfile (required for local and OCI deployments)
    -r, --registry REGISTRY   OCI registry URL (e.g., oci://harbor.example.com/chart)
    -v, --version VERSION     Chart version to pull from registry
    -n, --name NAME           Chart name (default: dpf-operator)
    --repo-url URL            Traditional Helm repository URL
    --repo-name NAME          Repository name for traditional Helm repo (default: chart-repo)
    --environment NAME        Defines the environment for the deployment (e.g., shared-fs)
    --selector SELECTOR       Selector for filtering resources (e.g., "app=grafana" or "app!=prometheus")
    --helmfile-bin PATH       Path to helmfile binary (default: helmfile)
    --helm-bin PATH           Path to helm binary (default: helm)
    -h, --help                Show this help message

EXAMPLES:
    # Deploy from local file
    $0 -f deploy/helmfiles/prereqs.yaml

    # Deploy from OCI registry
    $0 -r "oci://harbor.example.com/chart" -v "v1.0.0" -f "prereqs.yaml"

    # Deploy from OCI registry with custom chart name
    $0 -r "oci://harbor.example.com/chart" -v "v1.0.0" -n "my-chart" -f "prereqs.yaml"

    # Deploy from traditional Helm repository
    $0 --repo-url "https://charts.example.com" --repo-name "my-repo" -f "prereqs.yaml"
EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
	case $1 in
	-f | --file)
		HELMFILE_FILE="$2"
		shift 2
		;;
	-r | --registry)
		HELM_CHART_OCI="$2"
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
	--repo-url)
		HELM_REPO_URL="$2"
		shift 2
		;;
	--repo-name)
		HELM_REPO_NAME="$2"
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

# Set default repo name if not provided
if [[ -n "$HELM_REPO_URL" && -z "$HELM_REPO_NAME" ]]; then
	echo "Error: --repo-name is required when using --repo-url"
	usage
	exit 1
fi

# Validate required parameters
if [[ -z "$HELM_CHART_OCI" && -z "$HELMFILE_FILE" && -z "$HELM_REPO_URL" ]]; then
	echo "Error: Either --registry, --file, or --repo-url must be specified"
	usage
	exit 1
fi

if [[ -n "$HELM_CHART_OCI" && -z "$HELM_CHART_VERSION" ]]; then
	echo "Error: --version is required when using --registry"
	usage
	exit 1
fi

if [[ -n "$HELM_CHART_OCI" && -z "$HELMFILE_FILE" ]]; then
	echo "Error: --file is required when using --registry"
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

# Function to deploy from OCI registry
deploy_from_oci() {
	echo "Deploying from OCI registry: $HELM_CHART_OCI"
	deploy_from_chart "$HELM_CHART_OCI"
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

	# Add the repository (force add if it already exists)
	echo "Adding Helm repository: $HELM_REPO_NAME"
	"$HELM_BIN" repo add "$HELM_REPO_NAME" "$HELM_REPO_URL" --force-update

	# Update repositories
	echo "Updating Helm repositories"
	"$HELM_BIN" repo update

	deploy_from_chart "$HELM_REPO_NAME/$HELM_CHART_NAME"
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

# Generic function to deploy helmfile
deploy_helmfile() {
	local helmfilePath="$1"

	# Make apply-crds.sh executable
	chmod +x "$(dirname "$helmfilePath")/apply-crds.sh"

	# Build helmfile command
	local helmfile_cmd=(
		"$HELMFILE_BIN" apply
		--suppress-diff
		--skip-diff-on-install
		--concurrency 0
		--color
		--hide-notes
		--allow-no-matching-release
		--file "$helmfilePath"
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

	# Deploy using helmfile
	"${helmfile_cmd[@]}"
}

# Main execution
if [[ -n "$HELM_CHART_OCI" ]]; then
	deploy_from_oci
elif [[ -n "$HELM_REPO_URL" ]]; then
	deploy_from_repo
else
	deploy_from_local
fi
