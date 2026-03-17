#!/usr/bin/env bash

#  2026 NVIDIA CORPORATION & AFFILIATES
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

envfile="${1:-manifests/00-env-vars/envvars.env}"
missing=()
rows=()

while IFS= read -r line; do
	[[ "$line" == export\ * ]] || continue
	key="${line#export }"
	key="${key%%=*}"
	key="${key// /}"

	if [[ -z "${!key:-}" ]]; then
		missing+=("$key")
		rows+=("  ✗ $key")
	else
		rows+=("  ✓ $key = ${!key:-}")
	fi
done < "$envfile"

# Print the results in a table format
printf "%s\n" "${rows[@]}" | column -t

echo
if ((${#missing[@]} > 0)); then
	echo "✗ Error: ${#missing[@]} required variable(s) not set"
	exit 1
fi
echo "✓ All required environment variables are set!"
