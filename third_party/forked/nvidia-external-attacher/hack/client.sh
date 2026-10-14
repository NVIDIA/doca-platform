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

set -o nounset
set -o pipefail
set -o errexit

if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <dpf_path> <branch>"
    echo "dpf_path: the path to the dpf repo"
    echo "branch: the branch of external-attacher repo to be used"
    exit 1
fi

DPF_DIR=$1
BRANCH=$2

CODE_DIR=$DPF_DIR/third_party/forked/nvidia-external-attacher
# copy nvidia storage CRD to external-attacher repo
CRD_SRC_DIR=$CODE_DIR/api/storage/v1alpha1
CRD_DET_DIR=$CODE_DIR/external-attacher/api/storage/v1alpha1
CLIENT_SRC_DIR=$CODE_DIR/client
CLIENT_DET_DIR=$CODE_DIR/external-attacher/client
mkdir -p "$CRD_DET_DIR"
mkdir -p "$CLIENT_DET_DIR"
cp -r "$CRD_SRC_DIR"/* "$CRD_DET_DIR"
cp -r "$CLIENT_SRC_DIR"/* "$CLIENT_DET_DIR"
find "$CODE_DIR"/external-attacher -type f \
  -exec sed -i 's|github.com/nvidia/doca-platform/third_party/forked/nvidia-external-attacher|github.com/kubernetes-csi/external-attacher|g' {} +

# apply the patch
PATCH_DIR=$CODE_DIR/hack/patches/$BRANCH
if [ ! -d "$PATCH_DIR" ]; then
  echo "Error: Directory '$PATCH_DIR' not found."
  exit 1
fi
PATCH_FILES=$(ls $PATCH_DIR/*.patch | sort -V)
if [ -z "$PATCH_FILES" ]; then
  echo "Error: No patch files found in '$PATCH_DIR'."
  exit 1
fi

cd $CODE_DIR/external-attacher
for PATCH_FILE in $PATCH_FILES; do
  echo "Applying patch: $PATCH_FILE"
  git apply "$PATCH_FILE"
  
  if [ $? -ne 0 ]; then
    echo "Error: Failed to apply patch $PATCH_FILE"
    exit 1
  fi
done

echo "All patches applied successfully!"
