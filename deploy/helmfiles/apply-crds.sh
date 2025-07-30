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

set -euo pipefail

CHART="$1"
VERSION="${2:-}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Preparing CRDs for chart: $CHART"
[[ -n "$VERSION" ]] && echo "Version: $VERSION"

# Pull and extract the chart
if [[ "$CHART" == oci://* ]]; then
	echo "Pulling OCI chart..."
	helm fetch --destination "$TMP_DIR" --untar "$CHART" --version "$VERSION"
else
	echo "Pulling repo chart..."
	if [[ -n "$VERSION" ]]; then
		helm pull "$CHART" --version "$VERSION" --untar --untardir "$TMP_DIR"
	else
		helm pull "$CHART" --untar --untardir "$TMP_DIR"
	fi
fi

CHART_DIR=$(find "$TMP_DIR" -mindepth 1 -maxdepth 1 -type d | head -n 1)

if [[ -d "$CHART_DIR/crds" ]]; then
	echo "Applying CRDs from $CHART_DIR/crds"
	kubectl apply -f "$CHART_DIR/crds" --server-side
else
	echo "No CRDs directory found in chart."
fi
