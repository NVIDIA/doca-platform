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

# Workaround to set the BFB image URL for our docs tests. In our other tests we rely on the env variable BFB_IMAGE_URL
# being set. However, in our user-guides we're using the env variable BFB_URL.
BFB_URL="$BFB_IMAGE_URL"
export BFB_URL

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
	echo "Waiting for DPUDeployment to have DPUSets reconciled..."
	kubectl wait --namespace "${NAMESPACE}" --timeout=15m dpudeployment --all \
		--for=condition=DPUSetsReconciled

	echo "Waiting for DPU resources to be created..."
	timeout 5m bash -c "until kubectl get dpu --namespace ${NAMESPACE} -o name 2>/dev/null | grep -q .; do sleep 2; done"

	echo "Waiting for DPUs to be installed..."
	kubectl wait --namespace "${NAMESPACE}" --timeout=30m dpu --all \
		--for=condition=OSInstalled

	echo "Waiting for DPUs to join the cluster..."
	kubectl wait --namespace "${NAMESPACE}" --timeout=20m dpu --all \
		--for=condition=DPUClusterReady

	echo "Waiting for DPUs to be operationally ready..."
	kubectl wait --namespace "${NAMESPACE}" --timeout=20m dpu --all \
		--for='jsonpath={.status.operationalConditions[?(@.type=="OperationalReady")].status}=True'
}

main() {
	deploy_components
	run_docs_test
	describe_all --show-resources=dpu
	wait_for_dpus
	describe_all
}

main
