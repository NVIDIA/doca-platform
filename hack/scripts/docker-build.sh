#!/usr/bin/env bash
#
# Copyright 2025 NVIDIA
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# This script wraps docker buildx build commands with optional JSON logging.
# It passes through all arguments to docker buildx build and captures structured logs on both success and failure.

set -euo pipefail

# Configuration from environment with defaults
: "${DOCKER_BUILD_LOGGING:=false}"
: "${ARTIFACTS_DIR:=./artifacts}"

# Execute the docker buildx build command with all passed arguments
set +e
docker buildx build "$@"
BUILD_EXIT=$?
set -e

# Save structured logs from docker buildx if enabled
if [[ "$DOCKER_BUILD_LOGGING" == "true" ]]; then
	mkdir -p "$ARTIFACTS_DIR"

	# Extract image:tag from arguments (look for -t or --tag)
	IMAGE_TAG=""
	# Parse through a copy of the arguments
	while [[ $# -gt 0 ]]; do
		case "$1" in
		-t | --tag)
			shift
			IMAGE_TAG="$1"
			break
			;;
		esac
		shift
	done

	# Generate log name from image:tag, replacing all punctuation with dashes
	LOG_NAME=$(echo "$IMAGE_TAG" | sed 's/[:\/.@]/-/g' | sed 's/(?:--)+/-/g')
	if [[ -z "$LOG_NAME" ]]; then
		# Do not return an error here - we should continue the build even if we miss the logs.
		echo "Error: Could not extract image:tag from docker args and build log file name" >&2
		exit $BUILD_EXIT
	fi

	LOG_FILE="${ARTIFACTS_DIR}/docker-build-log-${LOG_NAME}.json"
	docker buildx history logs --progress=rawjson 1> "$LOG_FILE" 2>&1 || true
fi

# Exit with original build exit code
exit $BUILD_EXIT
