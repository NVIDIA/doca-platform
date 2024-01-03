#!/bin/bash

#  2024 NVIDIA CORPORATION & AFFILIATES
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

scripts_root=$(dirname $0)/..

BASE_REV="${BASE_REV:-"main"}"
COMPARISON_REV="${COMPARISON_REV:-"HEAD"}"

validate_copyrights(){
    local status=0
    local include_list="\.go$|\.sh$|Makefile.*"

    # Exclude the files which are vendored from argoCD.
    local exclude_list="internal/argocd/api/|internal/dpuservice/utils/mergemaps.go|internal/kamaji/api/"

    # Check copyright is correct on all newly added files by checking for a leading `A` in git diff `--name-status`
    for file in $(git diff --name-status ${BASE_REV} ${COMPARISON_REV} | grep -E '^A' | awk '{print $2}' | grep -E "${include_list}" | grep -Ev "${exclude_list}");do
        if ! grep -q "$(date +%Y) NVIDIA" "${file}";then
            let status=$status+1
            echo "$file did not have the correct copyright notice"
        fi
    done

    return $status
}

print_copyrights(){

    echo ""
    echo "Please use the following copy right notice in the beginning of the failed files:"
    echo "----------------------------------------------------"
    echo "
  COPYRIGHT $(date +%Y) NVIDIA

  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

      http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.
"
    echo "----------------------------------------------------"
}

main(){
    local status=0

    if ! validate_copyrights;then
        echo "Failed to validate the projects copyrights!"
        print_copyrights
        return 1
    fi
}

global_status=0

if main;then
    echo "Copyright validation succeeded!"
else
    echo "Copyright validation failed!"
    global_status=1
fi

exit $global_status
