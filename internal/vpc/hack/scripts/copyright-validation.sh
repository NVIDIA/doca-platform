#!/usr/bin/env bash
#
# SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: LicenseRef-NvidiaProprietary
#
# NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
# property and proprietary rights in and to this material, related
# documentation and any modifications thereto. Any use, reproduction,
# disclosure or distribution of this material and related documentation
# without an express license agreement from NVIDIA CORPORATION or
# its affiliates is strictly prohibited.
#

scripts_root=$(dirname $0)/..

BASE_REV="${BASE_REV:-"main"}"
COMPARISON_REV="${COMPARISON_REV:-"HEAD"}"

validate_copyrights() {
	local status=0
	local include_list="\.go$|\.sh$|Makefile.*"

	# Exclude vendored files
	local exclude_list=""

	# Check copyright is correct on all newly added files by checking for a leading `A` in git diff `--name-status`
	for file in $(git diff --name-status ${BASE_REV} ${COMPARISON_REV} | grep -E '^A' | awk '{print $2}' | grep -E "${include_list}" | grep -Ev "${exclude_list}"); do
		if ! grep -q "$(date +%Y) NVIDIA" "${file}"; then
			let status=$status+1
			echo "$file did not have the correct copyright notice"
		fi
	done

	return $status
}

print_copyrights() {

	echo ""
	echo "Please use the following copy right notice in the beginning of the failed files:"
	echo "----------------------------------------------------"
	echo "
  SPDX-FileCopyrightText: Copyright (c) $(date +%Y) NVIDIA CORPORATION & AFFILIATES. All rights reserved.
  SPDX-License-Identifier: LicenseRef-NvidiaProprietary

  NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
  property and proprietary rights in and to this material, related
  documentation and any modifications thereto. Any use, reproduction,
  disclosure or distribution of this material and related documentation
  without an express license agreement from NVIDIA CORPORATION or
  its affiliates is strictly prohibited.
"
	echo "----------------------------------------------------"
}

main() {
	local status=0

	if ! validate_copyrights; then
		echo "Failed to validate the projects copyrights!"
		print_copyrights
		return 1
	fi
}

global_status=0

if main; then
	echo "Copyright validation succeeded!"
else
	echo "Copyright validation failed!"
	global_status=1
fi

exit $global_status
