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

NAMESPACE="${1:-dpf-operator-system}"
DASHBOARD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/dashboards"

echo "Applying Grafana dashboards from ${DASHBOARD_DIR} to namespace ${NAMESPACE}"

FROM_FILE_ARGS=()
for f in "${DASHBOARD_DIR}"/*.json; do
	FROM_FILE_ARGS+=("--from-file=$(basename "$f")=$f")
done

# The grafana_dashboard label and grafana_folder annotation make the Grafana
# sidecar pick the ConfigMap up and place the dashboards in the DPF folder.
kubectl create configmap dpf-monitoring-grafana-dashboards \
	--namespace "${NAMESPACE}" \
	"${FROM_FILE_ARGS[@]}" \
	--dry-run=client -o yaml \
	| kubectl label --local -f - grafana_dashboard="1" --dry-run=client -o yaml \
	| kubectl annotate --local -f - grafana_folder="DOCA Platform Framework" --dry-run=client -o yaml \
	| kubectl apply --server-side -f -
