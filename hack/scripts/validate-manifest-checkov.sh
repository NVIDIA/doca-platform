#!/usr/bin/env bash

#  Copyright 2026 NVIDIA
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

if [[ "${TRACE-0}" == "1" ]]; then
	set -o xtrace
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Add the projects bin/ dir to PATH to find dpfdev binary
export PATH="$script_dir/../../bin:$PATH"
# Add the projects hack/tools/bin/ dir to PATH to find yq binary
export PATH="$script_dir/../../hack/tools/bin:$PATH"

: ${RENDERED_MANIFEST:?variable is not set}
: ${MANIFEST_NAME:?variable is not set}

# Optional environment variables with defaults
ARTIFACTS=${ARTIFACTS:-"$script_dir/../../artifacts"}
CHECKOV_CONFIG="${CHECKOV_CONFIG:-"$script_dir/../../.checkov.yaml"}"
CHECKOV_REPORT_DIRECTORY="${ARTIFACTS}/checkov-reports"
CHECKOV_REPORT_FILE="${CHECKOV_REPORT_DIRECTORY}/${MANIFEST_NAME}.json"

# The directory to mount when running checkov so the rendered manifest is available.
VOLUME_MOUNT_DIRECTORY="$(dirname "${RENDERED_MANIFEST}")"

if ! command -v dpfdev &> /dev/null; then
	echo 'Error: dpfdev could not be found in the tools PATH. Install it via `make dpfdev-binary` first.'
	exit 1
fi

# make glob expansion produce no results if directory empty
shopt -s nullglob

# Ensure report directory exists
mkdir -p "${CHECKOV_REPORT_DIRECTORY}"

# Cleanup potential old report
rm -f "${CHECKOV_REPORT_FILE}"

validation_failed=0

# Run checkov for the manifest
make checkov-run CHECKOV_DATA_DIR="${VOLUME_MOUNT_DIRECTORY}" \
	CHECKOV_CHECKS="$(yq '.checks | join ","' < "${CHECKOV_CONFIG}")" \
	CHECKOV_COMMAND="-f /data/$(basename "${RENDERED_MANIFEST}") --framework kubernetes --output json" \
	CHECKOV_OUTPUT_FILE="${CHECKOV_REPORT_FILE}"

# Run dpfdev checkov to analyze the checkov report and filter out excluded findings
dpfdev checkov --chart-name "${MANIFEST_NAME}" --config "${CHECKOV_CONFIG}" --report "${CHECKOV_REPORT_FILE}"

if [ $? -ne 0 ]; then
	echo "❌ checkov manifest validation failed for ${MANIFEST_NAME}"
	exit 1
fi

echo "✅ checkov manifest validation completed for ${MANIFEST_NAME}"

exit 0
