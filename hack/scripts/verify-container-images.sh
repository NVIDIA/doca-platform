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

set -o errexit
set -o nounset
set -o pipefail

if [[ "${TRACE-0}" == "1" ]]; then
	set -o xtrace
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

: ${TRIVY:?variable is not set, should point to the trivy binary}
ARTIFACTS=${ARTIFACTS:-"$script_dir/../../artifacts"}
REPORT_DIRECTORY="${ARTIFACTS}/trivy"

# Ensure report directory exists
mkdir -p "${REPORT_DIRECTORY}"

FAILED=0

for IMAGE in $@; do
	echo "Scanning ${IMAGE} using trivy"

	image_name=$(basename $IMAGE)

	set +e
	"${TRIVY}" image -q --exit-code 1 --scanners vuln --ignore-unfixed --severity MEDIUM,HIGH,CRITICAL "${IMAGE}" 2>&1 | tee "${REPORT_DIRECTORY}/${image_name}.txt"
	trivy_scan_result=${PIPESTATUS[0]}
	set -e

	if [ "$trivy_scan_result" -ne "0" ]; then
		FAILED=1
		echo "❌ Check container image ${IMAGE} failed!"
	fi
done

BRed='\033[1;31m'
BGreen='\033[1;32m'
NC='\033[0m' # No Color

if [ "$FAILED" -ne "0" ]; then
	echo -e "${BRed}Check container images failed! There are vulnerabilities to be fixed${NC}"
	exit 1
fi

echo -e "${BGreen}Check container images passed! No vulnerability found${NC}"
