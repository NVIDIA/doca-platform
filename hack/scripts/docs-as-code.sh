#!/usr/bin/env bash

# Copyright 2026 NVIDIA
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

: "${ARTIFACTS:?must be set}"
mkdir -p "${ARTIFACTS}"

MODE="${1:?MODE required}"
USECASE="${2:?USECASE required}"

NAMESPACE="dpf-operator-system"

deploy_components() {
	echo "Deploying components..."
	HELMFILE_SELECTOR="app!=node-feature-discovery" make test-deploy-helmfile
	HELMFILE_WAIT=false HELMFILE_SELECTOR="app=node-feature-discovery" make test-deploy-helmfile
	kubectl apply -f - <<- EOF
		apiVersion: v1
		kind: PersistentVolumeClaim
		metadata:
		  name: bfb-pvc
		  namespace: dpf-operator-system
		spec:
		  storageClassName: "local-path"
		  accessModes:
		  - ReadWriteOnce
		  resources:
		    requests:
		      storage: 10Gi
	EOF
}

run_docs_test() {
	./hack/scripts/log-collector.sh \
		./bin/dpfdev test docs \
		--file "docs/public/user-guides/${MODE}/use-cases/${USECASE}/README.md" \
		--tags oci \
		--junit "${ARTIFACTS}/e2e-junit.xml" \
		--verbose
}

describe_all() {
	kubectl -n "${NAMESPACE}" exec deploy/dpf-operator-controller-manager -- /dpfctl describe all --grouping=false $@
}

wait_for_dpus() {
	echo "Waiting for DPUs to be ready..."
	kubectl wait \
		--for=condition=ready \
		--namespace "${NAMESPACE}" \
		dpu --all \
		--timeout=30m

	echo "Waiting for DPUs to be operationally ready..."
	kubectl wait \
		--for='jsonpath={.status.operationalConditions[?(@.type=="OperationalReady")].status}=True' \
		--namespace "${NAMESPACE}" \
		dpu --all \
		--timeout=30m
}

main() {
	deploy_components
	run_docs_test
	describe_all --show-resources=dpu
	wait_for_dpus
	describe_all
}

main
