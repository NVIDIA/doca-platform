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

STERN=${STERN:-"stern"}
ARTIFACTS=${ARTIFACTS:-"artifacts"}
LOGS_DIR=$ARTIFACTS/main/Logs

# Clean the logs directory before starting the process.
rm -rf "$LOGS_DIR"

# Call stern to get the logs of all pods in the cluster and write them to files in the artifacts directory.
# Send the process to the background to be able to run the command passed to the script.
# Log files are written to the artifacts directory in the format: $ARTIFACTS/main/Logs/$namespace/$pod/$container.log
$STERN -A . 2>/dev/null \
| while read ns pod container log; do
  mkdir -p "$LOGS_DIR/$ns/$pod"
  echo $log >> "$LOGS_DIR/$ns/$pod/$container.log"
done &

# Get the pid of the stern process and kill it when the script exits.
STERN_PID=$!
trap "kill $STERN_PID" EXIT

# Call the command passed to the script.
"$@"
