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

set -o nounset
set -o pipefail
set -o errexit

if [[ "${TRACE-0}" == "1" ]]; then
    set -o xtrace
fi

CLUSTER_NAME="${CLUSTER_NAME:-"dpf-test"}"
MINIKUBE_BIN="${MINIKUBE_BIN:-"unknown"}"
ARTIFACTS_DIR="${ARTIFACTS_DIR:-""}"

## This script takes a look at the artifacts of an end-to-end test run and searches pod events to find which images have been pulled during this test run.
## It explicitly excludes images which are from NVIDIA local repositories to ensure images-under-test are not cached.
imagesToPull=$(grep -nr 'note\: Pulling image \".*\"' ${ARTIFACTS_DIR}/main/Events/Pod | grep -v harbor.mellanox.com | grep -v nvcr.io | grep -v gitlab-master.nvidia.com | grep -v localhost:5000 | cut -d '"' -f2)

## Cache each image in the list in minikube.
for image in $imagesToPull;
  do ${MINIKUBE_BIN} -p ${CLUSTER_NAME} --stderrthreshold=1 cache add $image ;
done
