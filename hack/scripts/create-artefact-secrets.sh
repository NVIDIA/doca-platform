#!/usr/bin/env bash

# Copyright 2024 NVIDIA CORPORATION & AFFILIATES
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

: ${ZERO_TRUST:="false"}
# create-artefact-secrets.sh will create secrets required to pull images and helm charts from NGC or Gitlab if the relevant API KEY is set in the environment

## Ensure the script is idempotent, allowing safe re-execution even if the secret already exists.
kubectl -n dpf-operator-system delete secret dpf-helm-secret --ignore-not-found
kubectl -n dpf-operator-system delete secret dpf-pull-secret --ignore-not-found
kubectl -n dpf-operator-system delete secret bmc-shared-password --ignore-not-found

IMAGE_PULL_KEY="${IMAGE_PULL_KEY:-""}"

## Create a bmc-shared-password secret to be used by ZeroTrust
if [[ "$ZERO_TRUST" == "true" ]]; then
	BMC_ROOT_PASSWORD="${BMC_ROOT_PASSWORD:-""}"

	if [[ "$BMC_ROOT_PASSWORD" == "" ]]; then
		echo "BMC_ROOT_PASSWORD not set. Failed as creation of bmc-shared-password secret is not possible."
		exit 1
	fi
	kubectl create namespace dpf-operator-system || true
	kubectl create secret generic -n dpf-operator-system bmc-shared-password --from-literal=password=$BMC_ROOT_PASSWORD
fi

if [[ "$IMAGE_PULL_KEY" == "" ]]; then
	echo "IMAGE_PULL_KEY not set. Skipping imagePullSecret creation"
	exit 0
fi

REGISTRY_SERVER=$(echo $REGISTRY | cut -d'/' -f1)

## create namespace if it doesn't exist
kubectl create namespace dpf-operator-system --dry-run=client -o yaml | kubectl apply -f -

## Create a pull secret to be used for images in Kubernetes.
kubectl -n dpf-operator-system create secret docker-registry dpf-pull-secret --docker-server=$REGISTRY_SERVER --docker-username="\$oauthtoken" --docker-password=$IMAGE_PULL_KEY

## Create a pull secret to be used by ArgoCD for pulling helm charts
echo "
apiVersion: v1
kind: Secret
metadata:
  name: dpf-helm-secret
  namespace: dpf-operator-system
  labels:
    argocd.argoproj.io/secret-type: repository
stringData:
  name: dpf-helm
  url: $REGISTRY
  type: helm
  username: \$oauthtoken
  password: $IMAGE_PULL_KEY
" | kubectl apply -f -
