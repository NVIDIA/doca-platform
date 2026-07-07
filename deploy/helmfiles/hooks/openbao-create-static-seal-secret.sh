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

# Creates the OpenBao static seal Secret if it does not already exist.

set -euo pipefail

if [[ "$#" -ne 3 ]]; then
	echo "Usage: $0 <namespace> <secret-name> <secret-key>" >&2
	exit 1
fi

NAMESPACE="$1"
SECRET_NAME="$2"
SECRET_KEY="$3"

echo "Ensuring namespace exists: ${NAMESPACE}"
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

if kubectl get secret "${SECRET_NAME}" -n "${NAMESPACE}" > /dev/null 2>&1; then
	echo "Secret ${NAMESPACE}/${SECRET_NAME} already exists; leaving it unchanged."
	exit 0
fi

echo "Creating static OpenBao seal secret: ${NAMESPACE}/${SECRET_NAME}"
STATIC_SEAL_KEY="$(dd if=/dev/urandom bs=32 count=1 2> /dev/null | base64 | tr -d '\n')"

kubectl create secret generic "${SECRET_NAME}" \
	-n "${NAMESPACE}" \
	--from-literal="${SECRET_KEY}=${STATIC_SEAL_KEY}" \
	--dry-run=client -o yaml | kubectl apply -f -
