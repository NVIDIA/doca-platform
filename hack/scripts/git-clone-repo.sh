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

# git-clone-repo will 'git clone' a repo from the NVIDIA internal gitlab. It will use ssh by default or use a gitlab token and authenticated http if set.
# This script is fragile and highly tied to the current setup of the internal gitlab.

GITLAB_TOKEN="${GITLAB_TOKEN:-""}"
REPO="${1:-""}"
DIR="${2:-""}"
REVISION="${3:-""}"
# This script takes the following args:
#   $REPO - git repo to clone in form ssh://git@gitlab-master.nvidia.com:12051/doca-platform-foundation/ovn-kubernetes.git
#   $DIR - directory to clone the repo to.
#   $REVISION - revision of package to clone. If empty this will be the HEAD of the default branch.

# Exit if required arguments are not set.
if [[ "$REPO" == "" || "$DIR" == "" ]]; then
  echo "git-clone-repo.sh requires 2 arguments:
  1. git repo to clone in form ssh://git@gitlab-master.nvidia.com:12051/doca-platform-foundation/ovn-kubernetes.git
  2. destination for the git repo to be cloned to." >&2
  exit 1
fi

TMPDIR="$DIR"-tmp
# Ensure a temporary directory from any previous run is cleaned up.
rm -rf "$TMPDIR"

SERVER=$(echo $REPO | cut -d'@' -f 2 | cut -d':' -f 1)
PROJECT=$(echo $REPO  | cut -d':' -f 3 | cut -d'/' -f 2-)

if [ -z "${GITLAB_TOKEN}" ];
then
  echo "Cloning repo using ssh authentication"
  git clone "$REPO" "$TMPDIR"
else
  echo "Cloning repo using authenticated https with GitLab token"
  git clone "https://user:""$GITLAB_TOKEN"@"$SERVER"/"$PROJECT" $TMPDIR
fi

## Check out the passed revision if it is set.
if [ ! -z "${REVISION}" ];
then
  echo "Checking out revision " $REVISION
  cd $TMPDIR && git reset --hard "$REVISION"
else
  cd $TMPDIR && echo "No revision set. Using HEAD@"$(git rev-parse HEAD)
fi


mv "$TMPDIR" "$DIR"
rm -rf "$TMPDIR"