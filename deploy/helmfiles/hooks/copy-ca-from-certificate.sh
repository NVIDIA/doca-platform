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

# Waits for a cert-manager Certificate and copies its CA bundle to ConfigMaps in target namespaces.

set -euo pipefail

if [[ "$#" -lt 4 ]]; then
	echo "Usage: $0 <certificate-namespace> <certificate-name> <configmap-name> <target-namespace> [<target-namespace> ...]" >&2
	exit 1
fi

CERTIFICATE_NAMESPACE="$1"
CERTIFICATE_NAME="$2"
CONFIGMAP_NAME="$3"
shift 3

echo "Waiting for certificate: ${CERTIFICATE_NAMESPACE}/${CERTIFICATE_NAME}"
kubectl wait --for=condition=Ready --timeout=300s "certificate/${CERTIFICATE_NAME}" -n "${CERTIFICATE_NAMESPACE}"

SECRET_NAME="$(kubectl get certificate "${CERTIFICATE_NAME}" -n "${CERTIFICATE_NAMESPACE}" -o jsonpath='{.spec.secretName}')"
CA_CERT_FILE="$(mktemp)"
trap 'rm -f "${CA_CERT_FILE}"' EXIT

kubectl get secret "${SECRET_NAME}" -n "${CERTIFICATE_NAMESPACE}" -o jsonpath='{.data.ca\.crt}' | base64 -d > "${CA_CERT_FILE}"
if [[ ! -s "${CA_CERT_FILE}" ]]; then
	echo "Secret ${CERTIFICATE_NAMESPACE}/${SECRET_NAME} does not contain ca.crt" >&2
	exit 1
fi

for namespace in "$@"; do
	echo "Copying CA certificate to namespace: ${namespace}"
	kubectl create namespace "${namespace}" --dry-run=client -o yaml | kubectl apply -f -
	kubectl create configmap "${CONFIGMAP_NAME}" -n "${namespace}" --from-file=ca.crt="${CA_CERT_FILE}" --dry-run=client -o yaml | kubectl apply -f -
done
