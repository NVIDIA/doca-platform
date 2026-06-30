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

# Read manifests from stdin, render them through envsubst substituting ONLY the
# variables defined in manifests/00-env-vars/envvars.env, and write the result to
# stdout. Any other shell-style tokens (e.g. ${i}, "$@", $(...)) are left
# untouched.
#
# The env file is expected to be sourced by the caller before running this
# script, e.g.:
#   source manifests/00-env-vars/envvars.env
#
# Usage:
#   cat <files> | ./exec_envsubst.sh [path-to-env-file]
#
# Example:
#   cat manifests/01-dpf-system-installation/*.yaml | ./exec_envsubst.sh | kubectl apply -f -

set -euo pipefail

# env file path is optional, default to manifests/00-env-vars/envvars.env
envfile="${1:-manifests/00-env-vars/envvars.env}"

[[ -f "$envfile" ]] || {
	echo "error: env file not found: $envfile" >&2
	exit 1
}

# 1. Collect the exported variable names declared in the env file and construct the shell format allowlist string for envsubst
#    The format string is of the form "${VAR1} ${VAR2} ...".
shell_format="$(
	awk '/^export / { print $2 }' "$envfile" \
		| cut -d= -f1 \
		| sed 's/^/${/; s/$/}/' \
		| xargs
)"

[[ -n "$shell_format" ]] || {
	echo "error: no exported variables found in $envfile" >&2
	exit 1
}

# 2. Read stdin, substitute only the allowlisted vars, write to stdout.
echo "info: substituting only: ${shell_format}" >&2
envsubst "$shell_format"
