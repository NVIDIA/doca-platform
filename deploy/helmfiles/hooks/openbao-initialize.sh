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

# Initializes OpenBao when needed, stores the root token, and configures Kubernetes auth.

set -euo pipefail

if [[ "$#" -lt 3 || "$#" -gt 5 ]]; then
	echo "Usage: $0 <namespace> <pod-name> <root-token-secret-name> [<wait-timeout-seconds>] [<wait-interval-seconds>]" >&2
	exit 1
fi

NAMESPACE="$1"
POD_NAME="$2"
ROOT_TOKEN_SECRET_NAME="$3"
WAIT_TIMEOUT="${4:-600}"
WAIT_INTERVAL="${5:-5}"
WAIT_ATTEMPTS=$(((WAIT_TIMEOUT + WAIT_INTERVAL - 1) / WAIT_INTERVAL))
KUBERNETES_AUTH_PATH="${KUBERNETES_AUTH_PATH:-kubernetes}"

json_bool_field() {
	printf '%s' "$1" | tr -d '\n' | sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\(true\|false\).*/\1/p"
}

json_string_field() {
	printf '%s' "$1" | tr -d '\n' | sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"
}

bao_exec() {
	kubectl exec -n "${NAMESPACE}" "${POD_NAME}" -- "$@"
}

bao_status() {
	bao_exec bao status -format=json 2>&1 || true
}

wait_openbao_listening() {
	local i out
	i=1
	while [[ "${i}" -le "${WAIT_ATTEMPTS}" ]]; do
		out="$(bao_exec bao status 2>&1 || true)"
		if printf '%s' "${out}" | grep -qE '^(Initialized|Sealed|Seal Type)'; then
			echo "OpenBao listener is responding."
			return 0
		fi

		echo "Waiting for OpenBao listener... (${i}/${WAIT_ATTEMPTS})"
		i=$((i + 1))
		sleep "${WAIT_INTERVAL}"
	done

	echo "ERROR: OpenBao listener did not respond within ${WAIT_TIMEOUT}s" >&2
	return 1
}

wait_openbao_ready_for_auth() {
	local i status_json initialized sealed
	i=1
	while [[ "${i}" -le "${WAIT_ATTEMPTS}" ]]; do
		status_json="$(bao_status)"
		initialized="$(json_bool_field "${status_json}" initialized)"
		sealed="$(json_bool_field "${status_json}" sealed)"
		if [[ "${initialized}" == "true" && "${sealed}" == "false" ]]; then
			echo "OpenBao is initialized and unsealed."
			return 0
		fi

		echo "Waiting for OpenBao to be ready for auth configuration... (${i}/${WAIT_ATTEMPTS})"
		i=$((i + 1))
		sleep "${WAIT_INTERVAL}"
	done

	echo "ERROR: OpenBao was not ready for auth configuration within ${WAIT_TIMEOUT}s" >&2
	return 1
}

save_root_token_secret() {
	local init_json token root_token_file
	init_json="$1"
	token="$(json_string_field "${init_json}" root_token)"
	if [[ -z "${token}" ]]; then
		echo "ERROR: generated root token is missing." >&2
		return 1
	fi

	root_token_file="$(mktemp)"
	trap "rm -f '${root_token_file}'" RETURN
	printf '%s' "${token}" > "${root_token_file}"

	kubectl create secret generic "${ROOT_TOKEN_SECRET_NAME}" \
		-n "${NAMESPACE}" \
		--from-file=token="${root_token_file}" \
		--dry-run=client -o yaml | kubectl apply -f -
}

root_token() {
	local token
	token="$(kubectl get secret "${ROOT_TOKEN_SECRET_NAME}" -n "${NAMESPACE}" -o jsonpath='{.data.token}' | base64 -d)"
	if [[ -z "${token}" ]]; then
		echo "ERROR: root token secret ${NAMESPACE}/${ROOT_TOKEN_SECRET_NAME} is empty or missing token" >&2
		return 1
	fi
	printf '%s' "${token}"
}

bao_exec_authenticated() {
	printf '%s' "${ROOT_TOKEN}" | kubectl exec -i -n "${NAMESPACE}" "${POD_NAME}" -- \
		sh -c 'BAO_TOKEN="$(cat)" "$@"' _ "$@"
}

auth_mount_enabled() {
	local auth_list
	auth_list="$(bao_exec_authenticated bao auth list -format=json)"
	printf '%s' "${auth_list}" | tr -d '\n' | grep -q "\"${KUBERNETES_AUTH_PATH}/\""
}

configure_kubernetes_auth() {
	local ROOT_TOKEN
	wait_openbao_ready_for_auth
	ROOT_TOKEN="$(root_token)"

	if auth_mount_enabled; then
		echo "OpenBao Kubernetes auth mount is already enabled at: ${KUBERNETES_AUTH_PATH}/"
	else
		echo "Enabling OpenBao Kubernetes auth mount at: ${KUBERNETES_AUTH_PATH}/"
		bao_exec_authenticated bao auth enable -path="${KUBERNETES_AUTH_PATH}" kubernetes
	fi

	echo "Configuring OpenBao Kubernetes auth mount: ${KUBERNETES_AUTH_PATH}/"
	printf '%s' "${ROOT_TOKEN}" | kubectl exec -i -n "${NAMESPACE}" "${POD_NAME}" -- \
		env KUBERNETES_AUTH_PATH="${KUBERNETES_AUTH_PATH}" sh -c \
		'BAO_TOKEN="$(cat)" bao write "auth/${KUBERNETES_AUTH_PATH}/config" kubernetes_host="https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT}"'
}

initialize_if_needed() {
	local i status_output initialized init_json
	i=1
	initialized=""
	while [[ "${i}" -le "${WAIT_ATTEMPTS}" ]]; do
		status_output="$(bao_status)"
		initialized="$(json_bool_field "${status_output}" initialized)"
		if [[ "${initialized}" == "true" || "${initialized}" == "false" ]]; then
			break
		fi

		echo "Waiting for OpenBao initialization status... (${i}/${WAIT_ATTEMPTS})"
		i=$((i + 1))
		sleep "${WAIT_INTERVAL}"
	done

	case "${initialized}" in
	true)
		echo "OpenBao is already initialized; skipping initialization."
		;;
	false)
		echo "Initializing OpenBao..."
		init_json="$(bao_exec bao operator init -format=json)"
		save_root_token_secret "${init_json}"
		echo "Saved root token secret: ${NAMESPACE}/${ROOT_TOKEN_SECRET_NAME}"
		;;
	*)
		echo "ERROR: unable to determine OpenBao initialization status; initialized=${initialized:-unknown}" >&2
		echo "Last OpenBao status output: ${status_output:-<empty>}" >&2
		exit 1
		;;
	esac
}

main() {
	wait_openbao_listening
	initialize_if_needed
	configure_kubernetes_auth
}

main "$@"
