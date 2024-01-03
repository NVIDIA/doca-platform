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

CLUSTER_NAME="${CLUSTER_NAME:-"dpf-dev"}"
MINIKUBE_BIN="${MINIKUBE_BIN:-"unknown"}"
# Docker is used as the minikube driver to enable using hollow nodes.
MINIKUBE_DRIVER="${MINIKUBE_DRIVER:-"unknown"}"
USE_MINIKUBE_DOCKER="${USE_MINIKUBE_DOCKER:-"true"}"
MINIKUBE_CACHE_ADD_IMAGES="${MINIKUBE_CACHE_ADD_IMAGES:-"true"}"
NUM_NODES="${NUM_NODES:-"1"}"
NODE_MEMORY="${NODE_MEMORY:-"8g"}"
NODE_CPUS="${NODE_CPUS:-"4"}"
NODE_DISK="${NODE_DISK:-"100g"}"
MINIKUBE_CNI="${MINIKUBE_CNI:-kindnet}"
MINIKUBE_KUBERNETES_VERSION="${MINIKUBE_KUBERNETES_VERSION:-"v1.30.2"}"
MINIKUBE_DOCKER_MIRROR="${MINIKUBE_DOCKER_MIRROR:-"https://dockerhub.nvidia.com"}"
CERT_MANAGER_VERSION="v1.13.3"
ADD_CONTROL_PLANE_TAINTS="${ADD_CONTROL_PLANE_TAINTS:-"false"}"

## Detect the OS.
OS="unknown"
if [[ "${OSTYPE}" == "linux"* ]]; then
  OS="linux"
elif [[ "${OSTYPE}" == "darwin"* ]]; then
  OS="darwin"
fi

# Exit if the OS is not supported.
if [[ "$OS" == "unknown" ]]; then
  echo "os '$OSTYPE' not supported. Aborting." >&2
  exit 1
fi

## Set the driver used for minikube machines. By default this script will select the preferred VM driver for each OS.
## Users can override this using the MINIKUBE_DRIVER env variable.
if [[ "$MINIKUBE_DRIVER" == "unknown" ]]; then
  if [[ "${OSTYPE}" == "linux"* ]]; then
    MINIKUBE_DRIVER="kvm2"
  elif [[ "${OSTYPE}" == "darwin"* ]]; then
    MINIKUBE_DRIVER="qemu"
  fi
fi

MINIKUBE_ARGS="${MINIKUBE_ARGS:-"\
  --driver $MINIKUBE_DRIVER \
  --cpus=$NODE_CPUS \
  --memory=$NODE_MEMORY \
  --disk-size=$NODE_DISK \
  --nodes=$NUM_NODES \
  --cni $MINIKUBE_CNI \
  --kubernetes-version $MINIKUBE_KUBERNETES_VERSION \
  --registry-mirror="$MINIKUBE_DOCKER_MIRROR" \
  --preload=true \
  --addons metallb"}"

MINIKUBE_EXTRA_ARGS="${MINIKUBE_EXTRA_ARGS:-""}"

echo "Setting up Minikube cluster..."

## Exit early if the cluster already exists.
if [[ $($MINIKUBE_BIN status -p $CLUSTER_NAME  -f '{{.Name}}') == $CLUSTER_NAME ]]; then
  echo "Minikube cluster '$CLUSTER_NAME' found. Skipping cluster set-up"
  ## Set environment variables to use the Minikube VM docker build for
  if [[ "$USE_MINIKUBE_DOCKER" == "true" ]]; then
    echo "Setting environment variables to use $CLUSTER_NAME as docker build environment."
    eval $($MINIKUBE_BIN -p $CLUSTER_NAME docker-env)
  fi
  exit 0
fi

$MINIKUBE_BIN start --profile $CLUSTER_NAME $MINIKUBE_ARGS $MINIKUBE_EXTRA_ARGS

## Set control-plane and master taint to be able to test tolerations and nodeAffinities.
if [[ "$ADD_CONTROL_PLANE_TAINTS" == "true" ]]; then
  ## Wait for storage-provisioner first. This is just a Pod and no Deployment or such.
  kubectl wait --for=condition=ready pod/storage-provisioner --timeout=300s -n kube-system
  ## We have to patch those 2 Deployments to tolerate the control-plane and master taint.
  kubectl -n metallb-system patch deploy controller -p '{"spec":{"template":{"spec":{"tolerations":[{"key":"node-role.kubernetes.io/master","operator":"Exists","effect":"NoSchedule"},{"key":"node-role.kubernetes.io/control-plane","operator":"Exists","effect":"NoSchedule"}]}}}}'
  kubectl -n kube-system patch deploy coredns -p '{"spec":{"template":{"spec":{"tolerations":[{"key":"node-role.kubernetes.io/master","operator":"Exists","effect":"NoSchedule"},{"key":"node-role.kubernetes.io/control-plane","operator":"Exists","effect":"NoSchedule"}]}}}}'
  kubectl taint node "${CLUSTER_NAME}" node-role.kubernetes.io/control-plane:NoSchedule
  kubectl taint node "${CLUSTER_NAME}" node-role.kubernetes.io/master:NoSchedule
fi

imagesToCache="
quay.io/jetstack/cert-manager-controller:v1.13.3
cfssl/cfssl:latest
quay.io/argoproj/argocd:v2.11.1
clastix/kubectl:v1.29
public.ecr.aws/docker/library/redis:7.2.4-alpine
quay.io/metallb/speaker:v0.9.6
quay.io/metallb/controller:v0.9.6
gcr.io/k8s-minikube/storage-provisioner:v5
quay.io/jetstack/cert-manager-cainjector:v1.13.3
"

if [[ "$MINIKUBE_CACHE_ADD_IMAGES" == "true" ]]; then
  echo "Adding images to the minikube cache."
  for image in $imagesToCache;
    do ${MINIKUBE_BIN} -p ${CLUSTER_NAME} --stderrthreshold=1 cache add $image ;
  done
fi

## Update the MetalLB configuration to give it some IPs to give out. Take them from the same range as minikube uses.
MINIKUBE_IP=$($MINIKUBE_BIN ip -p $CLUSTER_NAME)
MINIKUBE_IP_NETWORK=$(echo $MINIKUBE_IP | sed -E 's/([0-9]+\.[0-9]+\.[0-9]+)\.[0-9]+/\1/')
MINIKUBE_LB_RANGE="${MINIKUBE_IP_NETWORK}.20-${MINIKUBE_IP_NETWORK}.29"

cat <<EOF | kubectl --context $CLUSTER_NAME apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: metallb-system
  name: config
data:
  config: |
    address-pools:
    - name: default
      protocol: layer2
      addresses: [${MINIKUBE_LB_RANGE}]
EOF

## Install cert-manager
helm repo add jetstack https://charts.jetstack.io --force-update
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.crds.yaml"
cat <<EOF | helm upgrade --install cert-manager --create-namespace --namespace cert-manager --version "${CERT_MANAGER_VERSION}" -f - jetstack/cert-manager
tolerations:
  - key: node-role.kubernetes.io/master
    operator: Exists
    effect: NoSchedule
  - key: node-role.kubernetes.io/control-plane
    operator: Exists
    effect: NoSchedule
cainjector:
  tolerations:
    - key: node-role.kubernetes.io/master
      operator: Exists
      effect: NoSchedule
    - key: node-role.kubernetes.io/control-plane
      operator: Exists
      effect: NoSchedule
webhook:
  tolerations:
    - key: node-role.kubernetes.io/master
      operator: Exists
      effect: NoSchedule
    - key: node-role.kubernetes.io/control-plane
      operator: Exists
      effect: NoSchedule
startupapicheck:
  enabled: false
EOF

echo "Waiting for cert-manager deployment to be ready."
kubectl -n cert-manager rollout status deploy cert-manager-webhook --timeout=180s

## Set environment variables to use the Minikube VM docker build for
if [[ "$USE_MINIKUBE_DOCKER" == "true" ]]; then
  echo "Setting environment variables to use $CLUSTER_NAME as docker build environment."
  eval $($MINIKUBE_BIN -p $CLUSTER_NAME docker-env)
fi
