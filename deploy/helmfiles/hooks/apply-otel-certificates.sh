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

# Creates a cert-manager self-signed serving certificate for the management
# cluster OpenTelemetry Collector's TLS OTLP receivers. All node InternalIPs
# are added as IP SANs because DPU clusters reach the receivers via NodePorts
# on any management cluster node.
# DPUCLUSTER_VIP, when set, is added as an extra IP SAN. It is the keepalived
# VIP DPU clusters export through, and is not a node InternalIP.

set -euo pipefail

NAMESPACE="${1:-dpf-operator-system}"
CERTIFICATE_NAME="otel-gateway-server-tls"

IP_SANS="$(kubectl get nodes -o jsonpath='{range .items[*]}{range .status.addresses[?(@.type=="InternalIP")]}{"    - "}{.address}{"\n"}{end}{end}' | sort -u)"
if [[ -z "${IP_SANS}" ]]; then
	echo "No node InternalIPs found for the ${CERTIFICATE_NAME} certificate IP SANs" >&2
	exit 1
fi

if [[ -n "${DPUCLUSTER_VIP:-}" ]]; then
	IP_SANS="$(printf '%s\n    - %s' "${IP_SANS}" "${DPUCLUSTER_VIP}" | sort -u)"
fi

kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f - << EOF
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: otel-gateway-selfsigned-issuer
  namespace: ${NAMESPACE}
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ${CERTIFICATE_NAME}
  namespace: ${NAMESPACE}
spec:
  dnsNames:
    - opentelemetry-collector
    - opentelemetry-collector.${NAMESPACE}
    - opentelemetry-collector.${NAMESPACE}.svc
    - opentelemetry-collector.${NAMESPACE}.svc.cluster.local
  ipAddresses:
${IP_SANS}
  issuerRef:
    kind: Issuer
    name: otel-gateway-selfsigned-issuer
  secretName: ${CERTIFICATE_NAME}
EOF

echo "Waiting for certificate: ${NAMESPACE}/${CERTIFICATE_NAME}"
kubectl wait --for=condition=Ready --timeout=300s "certificate/${CERTIFICATE_NAME}" -n "${NAMESPACE}"
