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

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Add the projects bin/ dir to PATH to find dpfdev binary
export PATH="$script_dir/../../bin:$PATH"

# Mandatory environment variables
: ${CRDIFY_COMPARE_REF:?"env not set"}

# Optional environment variables with defaults
: ${CRDIFY_ORIGIN:="origin"}
: ${CRDIFY_CRD_DIR:="deploy/charts/dpf-operator/templates/crds"}
: ${CRDIFY_CONFIG:?"$script_dir/../../crdify.yaml"}

# Resolves to the release branch immediately before $CI_MERGE_REQUEST_TARGET_BRANCH_NAME:
#   - target == main         -> the newest release-vX.Y branch
#   - target == release-vX.Y -> the release-vX.Y branch immediately before it
#   - the oldest release branch that exists, or anything else -> prints nothing
resolve_previous_release_branch() {
	local target="$1" branches base
	branches="$(git ls-remote --heads --refs "$CRDIFY_ORIGIN" \
		| awk '{print $2}' \
		| sed 's#refs/heads/##' \
		| grep -E '^release-v[0-9]+\.[0-9]+$' \
		| sort -V || true)"
	[ -z "$branches" ] && return 0

	if [ "$target" = "main" ]; then
		echo "$branches" | tail -n1
		return 0
	fi

	base="$(echo "$branches" | grep -B1 -Fx "$target" | head -n1 || true)"
	[ "$base" = "$target" ] && return 0
	echo "$base"
}

# Resolves the latest stable release tag: the highest vX.Y.Z tag on
# CRDIFY_ORIGIN, excluding pre-releases (-rc/-beta/-alpha) and any other
# non-semver tags.
resolve_latest_stable_tag() {
	git ls-remote --tags --refs "$CRDIFY_ORIGIN" \
		| awk '{print $2}' \
		| sed 's#refs/tags/##' \
		| grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
		| sort -V \
		| tail -n1 || true
}

# Resolves the release branch of the second-newest entry in
# docs/public/release-notes. The newest entry is written ahead of its release
# branch being cut (it documents the release currently in progress on main),
# so the second-newest is the latest release branch that actually exists.
# Only used locally when neither CRDIFY_BASE_REF nor
# CI_MERGE_REQUEST_TARGET_BRANCH_NAME is set; falls back to
# resolve_latest_stable_tag otherwise, including when the derived branch
# doesn't actually exist on CRDIFY_ORIGIN.
resolve_latest_release_branch_from_docs() {
	local versions second_latest branch
	versions="$(ls -1 "$script_dir/../../docs/public/release-notes" 2> /dev/null \
		| grep -E '^v[0-9]+\.[0-9]+\.[0-9]+\.md$' \
		| sed -E 's/^v([0-9]+\.[0-9]+)\.[0-9]+\.md$/\1/' \
		| sort -V -u || true)"
	[ "$(echo "$versions" | wc -l)" -ge 2 ] || return 0

	second_latest="$(echo "$versions" | tail -n2 | head -n1)"
	branch="release-v${second_latest}"

	# Only trust the derived branch if it actually exists on the remote.
	if git ls-remote --heads --exit-code --refs "$CRDIFY_ORIGIN" "$branch" > /dev/null 2>&1; then
		echo "$branch"
	fi
}

# CRDIFY_BASE_REF defaults to: on an MR pipeline, the previous supported
# release branch relative to CI_MERGE_REQUEST_TARGET_BRANCH_NAME; otherwise,
# the latest release branch per docs/public/release-notes; otherwise (or if
# either lookup comes up empty) the latest stable release tag.
if [ -z "${CRDIFY_BASE_REF:-}" ]; then
	if [ -n "${CI_MERGE_REQUEST_TARGET_BRANCH_NAME:-}" ]; then
		CRDIFY_BASE_REF="$(resolve_previous_release_branch "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME")"
	else
		CRDIFY_BASE_REF="$(resolve_latest_release_branch_from_docs)"
	fi
	if [ -z "${CRDIFY_BASE_REF:-}" ]; then
		CRDIFY_BASE_REF="$(resolve_latest_stable_tag)"
	fi
	: ${CRDIFY_BASE_REF:?"could not resolve a base ref from $CRDIFY_ORIGIN"}
	echo "ℹ️ CRDIFY_BASE_REF was not set, resolved to: $CRDIFY_BASE_REF"
fi

# Fetch into FETCH_HEAD rather than refs/heads/$CRDIFY_BASE_REF: writing into
# a local branch ref fails if that branch happens to be checked out (e.g.
# running this from within a release-vX.Y checkout or worktree), and can also
# fail to fast-forward a diverged local branch of the same name.
git fetch "$CRDIFY_ORIGIN" "$CRDIFY_BASE_REF"
crdify_base_sha="$(git rev-parse FETCH_HEAD)"

if [ "$CRDIFY_COMPARE_REF" != "HEAD" ]; then
	git fetch "$CRDIFY_ORIGIN" "$CRDIFY_COMPARE_REF"
	crdify_compare_sha="$(git rev-parse FETCH_HEAD)"
else
	crdify_compare_sha="HEAD"
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
	if ! git cat-file -e "$crdify_base_sha:$file" 2> /dev/null; then
		echo "⚠️  Skipping: $file does not exist in $CRDIFY_BASE_REF"
		echo
		continue
	fi

	if ! git cat-file -e "$crdify_compare_sha:$file" 2> /dev/null; then
		echo "⚠️  Skipping: $file does not exist in $CRDIFY_COMPARE_REF"
		echo
		continue
	fi

	if [ "$CRDIFY_COMPARE_REF" = "HEAD" ]; then
		compare_path="file://$file"
	else
		compare_path="git://$crdify_compare_sha?path=$file"
	fi

	# Run crdify with deprecation and allow-list support
	dpfdev crdify \
		--allow-removal-deprecations \
		--enable-allow-list \
		--config "$CRDIFY_CONFIG" \
		"git://$crdify_base_sha?path=$file" \
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
	echo "To re-run the test locally, run:"
	echo "  CRDIFY_BASE_REF=$CRDIFY_BASE_REF make verify-crdify"
	exit 1
fi
echo "✅ CRD validation completed."
