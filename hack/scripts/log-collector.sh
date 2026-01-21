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

# Collects logs from both the host cluster and all DPUClusters in real-time.
#
# Architecture:
#   1. Watcher: Deploys log-collector pods when DPUClusters become Ready
#   2. Host Stern: Captures all host cluster pod logs (excluding log-collectors)
#   3. DPUCluster Stern: Follows log-collector pods and extracts nested logs
#
# Log files written to:
#   $ARTIFACTS_DIR/logs/host-cluster/{namespace}/{pod}/{container}.log (host cluster)
#   $ARTIFACTS_DIR/logs/{dpucluster-name}/{namespace}/{pod}/{container}.log (DPU clusters)
#
# Usage:
#   ./log-collector.sh [command to run]
#
# Example:
#   ./log-collector.sh make test-e2e

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STERN=${STERN:-"stern"}
ARTIFACTS_DIR=${ARTIFACTS_DIR:-"artifacts"}
LOGS_DIR="${ARTIFACTS_DIR%/}/logs"
HOST_LOGS_DIR="${LOGS_DIR}/host-cluster"

# Clean the logs directory before starting
rm -rf "$LOGS_DIR"

echo "Starting log collection for host cluster and DPUClusters..."

# Start the DPUCluster watcher (deploys log-collector pods)
"${SCRIPT_DIR}/log-collector-dpucluster-watcher.sh" &
WATCHER_PID=$!

# Start stern for host cluster (excludes log-collector pods by name pattern)
$STERN --max-log-requests=500 --all-namespaces --exclude-pod 'log-collector-.+' -o json . 2> /dev/null \
	| while IFS= read -r line; do
		ns=$(jq -r '.namespace // empty' <<< "$line")
		pod=$(jq -r '.podName // empty' <<< "$line")
		container=$(jq -r '.containerName // empty' <<< "$line")
		log=$(jq -r '.message // empty' <<< "$line")

		[[ -z "$ns" ]] || [[ -z "$pod" ]] || [[ -z "$container" ]] && continue

		mkdir -p "$HOST_LOGS_DIR/$ns/$pod"
		printf '%s\n' "$log" >> "$HOST_LOGS_DIR/$ns/$pod/$container.log"
	done &
HOST_STERN_PID=$!

# Start stern for DPUCluster log extraction (follows log-collector pods)
# The log-collector pods output stern JSON format from the DPUCluster, so we parse it twice
$STERN --max-log-requests=500 --all-namespaces --selector app=log-collector -o json . 2> /dev/null \
	| while IFS= read -r line; do
		# Parse outer stern JSON to get pod name and nested log message
		pod=$(jq -r '.podName // empty' <<< "$line")
		[[ -z "$pod" ]] && continue

		# Extract DPUCluster name from log-collector pod name
		dpucluster_name=$(sed -E 's/^log-collector-(.+)-[0-9a-f]+-[a-z0-9]+$/\1/' <<< "$pod")
		[[ -z "$dpucluster_name" ]] && continue

		# Get the nested log message which itself is JSON
		nested_json=$(jq -r '.message // empty' <<< "$line")
		[[ -z "$nested_json" ]] && continue

		# Continue if no json string
		if ! jq -e . &> /dev/null <<< "$nested_json"; then
			continue
		fi

		# Parse nested stern JSON output from the DPU cluster
		dpu_ns=$(jq -r '.namespace // empty' <<< "$nested_json")
		dpu_pod=$(jq -r '.podName // empty' <<< "$nested_json")
		dpu_container=$(jq -r '.containerName // empty' <<< "$nested_json")
		dpu_log=$(jq -r '.message // empty' <<< "$nested_json")

		# Continue if any field is missing
		if [[ -z "$dpu_ns" ]] || [[ -z "$dpu_pod" ]] || [[ -z "$dpu_container" ]]; then
			continue
		fi

		log_dir="${LOGS_DIR}/${dpucluster_name}/${dpu_ns}/${dpu_pod}"
		mkdir -p "$log_dir"
		printf '%s\n' "$dpu_log" >> "$log_dir/${dpu_container}.log"
	done &
DPUCLUSTER_STERN_PID=$!

# Cleanup function
cleanup() {
	echo ""
	echo "Stopping log collection..."

	# Force kill all background processes (log streamers don't need graceful shutdown)
	kill -9 $HOST_STERN_PID $DPUCLUSTER_STERN_PID $WATCHER_PID 2> /dev/null || true

	echo "Log collection complete. Logs available in: ${LOGS_DIR}/"
}

trap cleanup EXIT

# Run the command passed to the script
"$@"
