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

# Watches for new DPUClusters and automatically deploys stern collector pods for them.

set -euo pipefail

POLL_INTERVAL=${POLL_INTERVAL:-5}
STERN_IMAGE=${STERN_IMAGE:-"ghcr.io/stern/stern:1.33.1"}

cleanup() {
	kubectl delete deployments -A -l app=log-collector --ignore-not-found=true > /dev/null 2>&1 || true
}

trap cleanup EXIT

while true; do
	# Get all Ready DPUClusters with kubeconfig set
	dpuclusters=$(kubectl get dpuclusters -A -o json 2> /dev/null \
		| jq -r '.items[] | select(.status.phase=="Ready" and .spec.kubeconfig != null and .spec.kubeconfig != "") | "\(.metadata.namespace):\(.metadata.name):\(.spec.kubeconfig)"' 2> /dev/null)

	while IFS=':' read -r ns name kubeconfig_secret; do
		[[ -z "$ns" ]] || [[ -z "$name" ]] && continue

		deployment_name="log-collector-${name}"

		# Check if deployment already exists
		if kubectl get deployment "$deployment_name" -n "$ns" > /dev/null 2>&1; then
			continue
		fi

		echo "Deploying stern collector for DPUCluster: ${ns}/${name}"

		kubectl apply -f - > /dev/null 2>&1 << EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: log-collector-${name}
  namespace: ${ns}
  labels:
    app: log-collector
    dpu.nvidia.com/dpucluster: ${name}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: log-collector
      dpu.nvidia.com/dpucluster: ${name}
  template:
    metadata:
      labels:
        app: log-collector
        dpu.nvidia.com/dpucluster: ${name}
      annotations:
        dpucluster.namespace: ${ns}
        dpucluster.name: ${name}
    spec:
      containers:
      - name: stern
        image: ${STERN_IMAGE}
        args:
        - --max-log-requests=500
        - --all-namespaces
        - --output=json
        - .
        env:
        - name: KUBECONFIG
          value: /etc/kubeconfig/super-admin.conf
        volumeMounts:
        - name: kubeconfig
          mountPath: /etc/kubeconfig
          readOnly: true
        resources:
          requests:
            memory: "64Mi"
            cpu: "50m"
          limits:
            memory: "256Mi"
            cpu: "200m"
      volumes:
      - name: kubeconfig
        secret:
          secretName: ${kubeconfig_secret}
      restartPolicy: Always
      tolerations:
        - effect: NoSchedule
          key: node-role.kubernetes.io/master
          operator: Exists
        - effect: NoSchedule
          key: node-role.kubernetes.io/control-plane
          operator: Exists
EOF
	done <<< "$dpuclusters"

	sleep "$POLL_INTERVAL"
done
