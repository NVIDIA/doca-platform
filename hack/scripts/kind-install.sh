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

set -o nounset
set -o pipefail
set -o errexit

if [[ "${TRACE-0}" == "1" ]]; then
	set -o xtrace
fi

CLUSTER_NAME="${CLUSTER_NAME:-"dpf-dev"}"
KIND_BIN="${KIND_BIN:-"unknown"}"
KIND_KUBERNETES_VERSION="${KIND_KUBERNETES_VERSION:-"v1.32.8"}"
ADD_CONTROL_PLANE_TAINTS="${ADD_CONTROL_PLANE_TAINTS:-"false"}"
# Use a port in the lower static band of the NodePort range to avoid collisions with
# dynamically auto-assigned NodePorts, which are allocated from the upper band first.
# See https://github.com/kubernetes/enhancements/tree/master/keps/sig-network/3668-reserved-service-nodeport-range
DPUCLUSTER_NODE_PORT="30043"
KIND_CONFIG_FILE="/tmp/kind-config-${CLUSTER_NAME}.yaml"
# Registry mirror configuration (comma-separated list of host:target-endpoint)
# Example: "docker.io:https://registry-1.docker.io,gcr.io:https://mirror.gcr.io"
REGISTRY_MIRRORS="${REGISTRY_MIRRORS:-""}"

echo "Setting up Kind cluster..."

## Exit early if the cluster already exists.
if $KIND_BIN get clusters | grep -q "^${CLUSTER_NAME}$"; then
	echo "Kind cluster '$CLUSTER_NAME' found. Skipping cluster set-up"
	exit 0
fi

# Create Kind config file
cat > ${KIND_CONFIG_FILE} << EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ${CLUSTER_NAME}
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "/etc/containerd/certs.d"
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  # Configure control plane components to expose metrics on all interfaces (0.0.0.0)
  # This is required for Prometheus to scrape metrics from outside the node
  # See docs/public/advanced-configuration/observability-guide.md for more details
  - |
    kind: ClusterConfiguration
    controllerManager:
      extraArgs:
        bind-address: "0.0.0.0"
    scheduler:
      extraArgs:
        bind-address: "0.0.0.0"
    etcd:
      local:
        extraArgs:
          listen-metrics-urls: "http://0.0.0.0:2381"
  extraPortMappings:
  - containerPort: ${DPUCLUSTER_NODE_PORT}
    hostPort: ${DPUCLUSTER_NODE_PORT}
    protocol: TCP
  image: kindest/node:${KIND_KUBERNETES_VERSION}
EOF

# Create the cluster
$KIND_BIN create cluster --config=${KIND_CONFIG_FILE}

function docker_exec() {
	docker exec "${CLUSTER_NAME}-control-plane" bash -c "$*"
}

# Configure registry mirrors if specified
if [[ -n "$REGISTRY_MIRRORS" ]]; then
	echo "Configuring registry mirrors: $REGISTRY_MIRRORS"

	IFS=',' read -ra MIRROR_ENTRIES <<< "$REGISTRY_MIRRORS"
	for MIRROR in "${MIRROR_ENTRIES[@]}"; do
		HOST="${MIRROR%%:*}"
		MIRROR_URL="${MIRROR#*:}"
		echo "Setting up registry mirror for: Host: $HOST, Endpoint: $MIRROR_URL"

		docker_exec mkdir -p "/etc/containerd/certs.d/$HOST"
		docker_exec "cat >> /etc/containerd/certs.d/$HOST/hosts.toml << EOF
[host.\"$MIRROR_URL\"]
  capabilities = [\"pull\", \"resolve\"]
EOF"

	done

	# restart containerd to apply the new mirror configuration
	docker_exec systemctl restart containerd

	echo "Registry mirror configuration created"
fi

# Set control-plane and master taint if requested
if [[ "$ADD_CONTROL_PLANE_TAINTS" == "true" ]]; then
	# We have to patch those 2 Deployments to tolerate the control-plane and master taint.
	kubectl -n kube-system patch deploy coredns -p '{"spec":{"template":{"spec":{"tolerations":[{"key":"node-role.kubernetes.io/master","operator":"Exists","effect":"NoSchedule"},{"key":"node-role.kubernetes.io/control-plane","operator":"Exists","effect":"NoSchedule"}]}}}}'

	# No need to add taints for control-plane as they are already present in Kind
	# But we add the master taint for compatibility
	kubectl taint node "${CLUSTER_NAME}-control-plane" node-role.kubernetes.io/master:NoSchedule --overwrite
fi

# Install MetalLB
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.13.7/config/manifests/metallb-native.yaml

# If we're adding taints, patch the MetalLB controller to tolerate them
if [[ "$ADD_CONTROL_PLANE_TAINTS" == "true" ]]; then
	echo "Patching MetalLB controller deployment to tolerate control-plane and master taints..."
	kubectl -n metallb-system patch deployment controller -p '{"spec":{"template":{"spec":{"tolerations":[{"key":"node-role.kubernetes.io/master","operator":"Exists","effect":"NoSchedule"},{"key":"node-role.kubernetes.io/control-plane","operator":"Exists","effect":"NoSchedule"}]}}}}'
fi

# Wait for MetalLB controller deployment to exist
echo "Waiting for MetalLB controller deployment to be ready."
kubectl -n metallb-system rollout status deployment/controller --timeout=180s

# Create the MetalLB config.
kubectl get namespace metallb-system || kubectl create namespace metallb-system

# Configure MetalLB with a range of IPs from the kind docker network
DOCKER_NETWORK_CIDR=$(docker network inspect kind | jq -r '.[0].IPAM.Config[].Subnet' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+')
NETWORK_PREFIX=$(echo $DOCKER_NETWORK_CIDR | sed -E 's/([0-9]+\.[0-9]+\.[0-9]+)\.[0-9]+.*/\1/')
METALLB_IP_RANGE="${NETWORK_PREFIX}.200-${NETWORK_PREFIX}.250"

cat << EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: default
  namespace: metallb-system
spec:
  addresses:
  - ${METALLB_IP_RANGE}
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: default
  namespace: metallb-system
spec:
  ipAddressPools:
  - default
EOF

echo "Delete local-path-provisioner and standard storage class if they exist."
kubectl delete ns local-path-storage || true
kubectl delete sc standard || true

echo "Kind cluster '${CLUSTER_NAME}' setup complete."
