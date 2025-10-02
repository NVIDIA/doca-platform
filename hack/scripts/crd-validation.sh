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

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Add the projects bin/ dir to PATH to find dpfdev binary
export PATH="$script_dir/../../bin:$PATH"

# Mandatory environment variables
: ${CRDIFY_BASE_REF:?"env not set"}
: ${CRDIFY_COMPARE_REF:?"env not test"}

# Optional environment variables with defaults
: ${CRDIFY_ORIGIN:="origin"}
: ${CRDIFY_CRD_DIR:="deploy/charts/dpf-operator/templates/crds"}
: ${CRDIFY_CONFIG:?"$script_dir/../../crdify.yaml"}

# fetch both branches
git fetch "$CRDIFY_ORIGIN" "$CRDIFY_BASE_REF:$CRDIFY_BASE_REF"
if [ $CRDIFY_COMPARE_REF != "HEAD" ]; then
	git fetch "$CRDIFY_ORIGIN" "$CRDIFY_COMPARE_REF:$CRDIFY_COMPARE_REF"
fi

if ! command -v dpfdev &> /dev/null; then
	echo 'Error: dpfdev could not be found in the tools PATH. Install it via `make dpfdev-binary` first.'
	exit 1
fi

# make glob expansion produce no results if directory empty
shopt -s nullglob

validation_failed=0

set +e
for file in "$CRDIFY_CRD_DIR"/*; do
	[ -f "$file" ] || continue

	# get simple CRD name (first match)
	crd_name=$(awk '/^  name:/{print $2; exit}' "$file" || true)
	echo "ℹ️ Validating CRD ${crd_name:-<unknown>} ($file)"

	# check existence in target CRDIFY_BASE_REF and in CRDIFY_COMPARE_REF
	if ! git cat-file -e "$CRDIFY_BASE_REF:$file" 2> /dev/null; then
		echo "⚠️  Skipping: $file does not exist in $CRDIFY_BASE_REF"
		echo
		continue
	fi

	if ! git cat-file -e "$CRDIFY_COMPARE_REF:$file" 2> /dev/null; then
		echo "⚠️  Skipping: $file does not exist in $CRDIFY_COMPARE_REF"
		echo
		continue
	fi

	if [ "$CRDIFY_COMPARE_REF" = "HEAD" ]; then
		compare_path="file://$file"
	else
		compare_path="git://$CRDIFY_COMPARE_REF?path=$file"
	fi

	# Run crdify with deprecation and allow-list support
	dpfdev crdify \
		--allow-removal-deprecations \
		--enable-allow-list \
		--config "$CRDIFY_CONFIG" \
		"git://$CRDIFY_BASE_REF?path=$file" \
		"$compare_path"

	if [ $? -ne 0 ]; then
		echo "❌ CRD validation failed for $crd_name ($file)"
		validation_failed=1
	fi

	echo
done
set -e

if [ $validation_failed -ne 0 ]; then
	echo "❌ One or more CRD validations failed."
	exit 1
fi
echo "✅ CRD validation completed."
