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

KUBECTL="${KUBECTL:-kubectl}"

NAMESPACE="dpf-operator-system"
ETCD_NAME="kamaji-etcd"
ETCD_IMAGE=""
COPY_IMAGE="busybox:1.36"
RCLONE_IMAGE="rclone/rclone:latest"
OUTPUT_FILE=""
RCLONE_CONFIG_FILE=""
RCLONE_DESTINATION=""
RCLONE_CONFIG_SECRET=""
RCLONE_CONFIG_KEY="rclone.conf"
RCLONE_DESTINATION_KEY="destination"
RCLONE_SECRET_DESTINATION=""
RCLONE_SECRET_CREATED=false
RCLONE_SECRET_PREEXISTED=false
CRON_SCHEDULE=""
CRONJOB_NAME=""
DRY_RUN=false

usage() {
	cat <<- EOF
		Usage: $0 [flags]

		Create a Kubernetes Job or CronJob that saves a Kamaji etcd snapshot to
		an emptyDir, creates a SHA-256 checksum, and either downloads both files
		with kubectl cp or uploads both files with rclone.

		Options:
		  --namespace NAME              Namespace containing the Kamaji etcd StatefulSet (default: ${NAMESPACE})
		  --etcd-name NAME              Kamaji etcd StatefulSet name (default: ${ETCD_NAME})
		  --etcd-image IMAGE            etcd image for snapshot containers. Defaults to the StatefulSet etcd image
		  --copy-image IMAGE            Copy container image (default: ${COPY_IMAGE}). Requires /bin/sh, sha256sum, mv, cat, tail, ls, and tar
		  --output-file FILE            Local snapshot destination (default: ./<job-name>.db)
		  --rclone-config-file FILE     Local rclone.conf to put in a Kubernetes Secret for upload
		  --rclone-destination DEST     Remote directory to copy to, for example backup:BUCKET/kamaji-etcd
		  --rclone-image IMAGE          rclone upload container image (default: ${RCLONE_IMAGE})
		  --cron-schedule SCHEDULE      Create or update a CronJob with this schedule. Requires rclone upload
		  --cronjob-name NAME           CronJob name when using --cron-schedule (default: ${ETCD_NAME}-backup)
		  --dry-run                     Print the Job or CronJob manifest instead of applying it
		  -h, --help                    Show this help

		Examples:
		  $0
		  $0 --namespace dpf-operator-system --etcd-name kamaji-etcd
		  $0 --output-file ./backups/kamaji-etcd.db
		  $0 --rclone-config-file ./rclone.conf --rclone-destination backup:BUCKET/kamaji-etcd
		  $0 --cron-schedule '0 2 * * *' --rclone-config-file ./rclone.conf --rclone-destination backup:BUCKET/kamaji-etcd
		  $0 --dry-run
	EOF
}

fail() {
	echo "error: $*" >&2
	exit 1
}

require_command() {
	command -v "$1" > /dev/null 2>&1 || fail "required command not found: $1"
}

require_arg() {
	local flag=$1
	local value=${2:-}

	[[ -n "$value" ]] || fail "${flag} requires a value"
}

parse_args() {
	while (($#)); do
		case "$1" in
		--namespace | -n)
			require_arg "$1" "${2:-}"
			NAMESPACE="$2"
			shift 2
			;;
		--etcd-name)
			require_arg "$1" "${2:-}"
			ETCD_NAME="$2"
			shift 2
			;;
		--etcd-image)
			require_arg "$1" "${2:-}"
			ETCD_IMAGE="$2"
			shift 2
			;;
		--copy-image)
			require_arg "$1" "${2:-}"
			COPY_IMAGE="$2"
			shift 2
			;;
		--rclone-image)
			require_arg "$1" "${2:-}"
			RCLONE_IMAGE="$2"
			shift 2
			;;
		--output-file)
			require_arg "$1" "${2:-}"
			OUTPUT_FILE="$2"
			shift 2
			;;
		--rclone-config-file)
			require_arg "$1" "${2:-}"
			RCLONE_CONFIG_FILE="$2"
			shift 2
			;;
		--rclone-destination)
			require_arg "$1" "${2:-}"
			RCLONE_DESTINATION="$2"
			shift 2
			;;
		--cron-schedule)
			require_arg "$1" "${2:-}"
			CRON_SCHEDULE="$2"
			shift 2
			;;
		--cronjob-name)
			require_arg "$1" "${2:-}"
			CRONJOB_NAME="$2"
			shift 2
			;;
		--dry-run)
			DRY_RUN=true
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			fail "unknown argument: $1"
			;;
		esac
	done
}

is_rclone_upload() {
	[[ -n "$RCLONE_CONFIG_FILE" || -n "$RCLONE_DESTINATION" ]]
}

is_cronjob() {
	[[ -n "$CRON_SCHEDULE" ]]
}

validate_args() {
	if [[ -n "$CRONJOB_NAME" && -z "$CRON_SCHEDULE" ]]; then
		fail "--cronjob-name requires --cron-schedule"
	fi

	if is_cronjob && ! is_rclone_upload; then
		fail "--cron-schedule requires --rclone-config-file and --rclone-destination"
	fi

	if is_rclone_upload; then
		[[ -n "$RCLONE_CONFIG_FILE" ]] || fail "--rclone-config-file is required with --rclone-destination"
		[[ -n "$RCLONE_DESTINATION" ]] || fail "--rclone-destination is required with --rclone-config-file"
		[[ -f "$RCLONE_CONFIG_FILE" ]] || fail "rclone config file not found: ${RCLONE_CONFIG_FILE}"
		[[ -z "$OUTPUT_FILE" ]] || fail "--output-file cannot be used with --rclone-config-file"
		return
	fi
}

discover_rclone_remote() {
	local line
	local remote=""
	local remote_count=0

	while IFS= read -r line; do
		if [[ "$line" =~ ^[[:space:]]*\[([^]]+)\][[:space:]]*$ ]]; then
			remote="${BASH_REMATCH[1]}"
			((remote_count += 1))
		fi
	done < "$RCLONE_CONFIG_FILE"

	if ((remote_count == 1)); then
		echo "$remote"
		return
	fi

	fail "--rclone-destination must include the remote name when ${RCLONE_CONFIG_FILE} contains ${remote_count} remotes"
}

prepare_rclone_secret() {
	local remote

	if is_cronjob; then
		RCLONE_CONFIG_SECRET="${CRONJOB_NAME}-rclone"
	else
		RCLONE_CONFIG_SECRET="${JOB_NAME}-rclone"
	fi
	RCLONE_DESTINATION="${RCLONE_DESTINATION%/}"

	if [[ "$RCLONE_DESTINATION" == *:* ]]; then
		RCLONE_SECRET_DESTINATION="$RCLONE_DESTINATION"
		return
	fi

	remote=$(discover_rclone_remote)
	RCLONE_SECRET_DESTINATION="${remote}:${RCLONE_DESTINATION}"
}

create_rclone_secret() {
	echo "Creating rclone config Secret ${NAMESPACE}/${RCLONE_CONFIG_SECRET}..."
	if $KUBECTL -n "$NAMESPACE" get secret "$RCLONE_CONFIG_SECRET" > /dev/null 2>&1; then
		RCLONE_SECRET_PREEXISTED=true
	fi

	$KUBECTL -n "$NAMESPACE" create secret generic "$RCLONE_CONFIG_SECRET" \
		--from-file="${RCLONE_CONFIG_KEY}=${RCLONE_CONFIG_FILE}" \
		--from-literal="${RCLONE_DESTINATION_KEY}=${RCLONE_SECRET_DESTINATION}" \
		--dry-run=client -o yaml | $KUBECTL apply -f -
	RCLONE_SECRET_CREATED=true
}

delete_rclone_secret() {
	if [[ "$RCLONE_SECRET_CREATED" != true ]]; then
		return
	fi

	if [[ "$RCLONE_SECRET_PREEXISTED" == true ]]; then
		echo "Keeping pre-existing rclone config Secret ${NAMESPACE}/${RCLONE_CONFIG_SECRET}."
		return
	fi

	echo "Deleting rclone config Secret ${NAMESPACE}/${RCLONE_CONFIG_SECRET}..."
	$KUBECTL -n "$NAMESPACE" delete secret "$RCLONE_CONFIG_SECRET" --ignore-not-found
	RCLONE_SECRET_CREATED=false
}

normalize_endpoints() {
	local raw=$1
	local endpoint
	local normalized=()

	raw="${raw//,/ }"
	for endpoint in $raw; do
		if [[ "$endpoint" == http://* || "$endpoint" == https://* ]]; then
			normalized+=("$endpoint")
		else
			normalized+=("https://${endpoint}")
		fi
	done

	(
		IFS=,
		echo "${normalized[*]}"
	)
}

discover_statefulset() {
	$KUBECTL -n "$NAMESPACE" get statefulset "$ETCD_NAME" > /dev/null 2>&1 \
		|| fail "could not find Kamaji etcd StatefulSet ${NAMESPACE}/${ETCD_NAME}"
}

discover_service() {
	ETCD_SERVICE=$($KUBECTL -n "$NAMESPACE" get statefulset "$ETCD_NAME" \
		-o jsonpath='{.spec.serviceName}' 2> /dev/null || true)
	[[ -n "$ETCD_SERVICE" ]] || fail "could not discover serviceName from StatefulSet ${NAMESPACE}/${ETCD_NAME}"

	$KUBECTL -n "$NAMESPACE" get service "$ETCD_SERVICE" > /dev/null 2>&1 \
		|| fail "Service ${NAMESPACE}/${ETCD_SERVICE} referenced by StatefulSet ${NAMESPACE}/${ETCD_NAME} was not found"
}

discover_image() {
	local image

	if [[ -n "$ETCD_IMAGE" ]]; then
		return
	fi

	image=$($KUBECTL -n "$NAMESPACE" get statefulset "$ETCD_NAME" \
		-o jsonpath='{.spec.template.spec.containers[?(@.name=="etcd")].image}' 2> /dev/null || true)

	if [[ -z "$image" ]]; then
		image=$($KUBECTL -n "$NAMESPACE" get statefulset "$ETCD_NAME" \
			-o jsonpath='{.spec.template.spec.containers[0].image}' 2> /dev/null || true)
	fi

	ETCD_IMAGE="$image"
}

discover_endpoints_from_statefulset() {
	local replicas
	local i
	local endpoints=()

	if [[ -n "${ENDPOINTS//[[:space:]]/}" ]]; then
		return
	fi
	ENDPOINTS=""

	replicas=$($KUBECTL -n "$NAMESPACE" get statefulset "$ETCD_NAME" -o jsonpath='{.spec.replicas}' 2> /dev/null || true)
	[[ -n "$replicas" ]] || fail "could not discover endpoints from StatefulSet ${NAMESPACE}/${ETCD_NAME}"
	[[ "$replicas" =~ ^[0-9]+$ ]] || fail "unexpected replica count for StatefulSet ${NAMESPACE}/${ETCD_NAME}: ${replicas}"
	((replicas > 0)) || fail "StatefulSet ${NAMESPACE}/${ETCD_NAME} has no replicas"

	for ((i = 0; i < replicas; i++)); do
		endpoints+=("${ETCD_NAME}-${i}.${ETCD_SERVICE}.${NAMESPACE}.svc.cluster.local:2379")
	done

	ENDPOINTS="${endpoints[*]}"
}

endpoint_pod_name() {
	local endpoint=$1
	local host

	host="${endpoint#*://}"
	host="${host%%/*}"
	host="${host%%:*}"
	echo "${host%%.*}"
}

discover_colocation_pod() {
	local snapshot_endpoint="${ENDPOINTS%%,*}"
	local pod_name

	if [[ -n "$COLOCATION_POD" ]]; then
		return
	fi

	pod_name=$(endpoint_pod_name "$snapshot_endpoint")
	if [[ -n "$pod_name" ]] && $KUBECTL -n "$NAMESPACE" get pod "$pod_name" > /dev/null 2>&1; then
		COLOCATION_POD="$pod_name"
		return
	fi

	if $KUBECTL -n "$NAMESPACE" get pod "${ETCD_NAME}-0" > /dev/null 2>&1; then
		COLOCATION_POD="${ETCD_NAME}-0"
	fi
}

render_colocation_affinity() {
	if [[ -z "$COLOCATION_POD" ]]; then
		return
	fi

	cat <<- EOF
		      affinity:
		        podAffinity:
		          requiredDuringSchedulingIgnoredDuringExecution:
		          - labelSelector:
		              matchExpressions:
		              - key: statefulset.kubernetes.io/pod-name
		                operator: In
		                values:
		                - ${COLOCATION_POD}
		            topologyKey: kubernetes.io/hostname
	EOF
}

yaml_quote() {
	local value=$1

	value="${value//\\/\\\\}"
	value="${value//\"/\\\"}"
	printf '"%s"' "$value"
}

indent_lines() {
	local prefix=$1
	local line

	while IFS= read -r line; do
		printf '%s%s\n' "$prefix" "$line"
	done
}

render_snapshot_container() {
	local snapshot_endpoint=$1

	cat <<- EOF
		      - name: snapshot
		        image: ${ETCD_IMAGE}
		        imagePullPolicy: IfNotPresent
		        command:
		        - etcdctl
		        - snapshot
		        - save
		        - /backup/${ETCD_NAME}.db
		        env:
		        - name: ETCDCTL_API
		          value: "3"
		        - name: ETCDCTL_ENDPOINTS
		          value: "${snapshot_endpoint}"
		        - name: ETCDCTL_CACERT
		          value: /opt/certs/ca/ca.crt
		        - name: ETCDCTL_CERT
		          value: /opt/certs/client/tls.crt
		        - name: ETCDCTL_KEY
		          value: /opt/certs/client/tls.key
		        volumeMounts:
		        - name: backup
		          mountPath: /backup
		        - name: ca
		          mountPath: /opt/certs/ca
		          readOnly: true
		        - name: client-certs
		          mountPath: /opt/certs/client
		          readOnly: true
	EOF
}

render_backup_snapshot_file_env() {
	local snapshot_file=""

	if ! is_cronjob; then
		snapshot_file="$SNAPSHOT_FILE"
	fi

	cat <<- EOF
		        - name: BACKUP_SNAPSHOT_FILE
		          value: $(yaml_quote "$snapshot_file")
	EOF
}

render_transfer_container() {
	if is_rclone_upload; then
		cat <<- EOF
			      - name: copy
			        image: ${RCLONE_IMAGE}
			        imagePullPolicy: IfNotPresent
			        command:
			        - /bin/sh
			        - -ec
			        - |
			          : "\${RCLONE_DESTINATION:?rclone destination Secret value is empty}"
			          destination="\${RCLONE_DESTINATION%/}"
			          source_snapshot_file="${ETCD_NAME}.db"
			          if [ -n "\${BACKUP_SNAPSHOT_FILE:-}" ]; then
			            snapshot_file="\${BACKUP_SNAPSHOT_FILE}"
			          else
			            timestamp="\$(date -u +%Y%m%dT%H%M%SZ 2>/dev/null || printf '%s' "\${POD_NAME}")"
			            snapshot_file="${ETCD_NAME}-\${timestamp}.db"
			          fi
			          snapshot_uri="\${destination}/\${snapshot_file}"
			          checksum_uri="\${snapshot_uri}.sha256"
			          cd /backup
			          mv "\${source_snapshot_file}" "\${snapshot_file}"
			          printf '%s\n' "\${snapshot_file}" > snapshot-file
			          sha256sum "\${snapshot_file}" > "\${snapshot_file}.sha256"
			          ls -lh "\${snapshot_file}" "\${snapshot_file}.sha256"
			          rclone copyto "\${snapshot_file}" "\${snapshot_uri}"
			          rclone copyto "\${snapshot_file}.sha256" "\${checksum_uri}"
			          rclone lsjson --stat "\${snapshot_uri}" > /dev/null
			          rclone lsjson --stat "\${checksum_uri}" > /dev/null
			          echo "Snapshot uploaded to \${snapshot_uri}"
			          echo "Checksum uploaded to \${checksum_uri}"
			        env:
			        - name: POD_NAME
			          valueFrom:
			            fieldRef:
			              fieldPath: metadata.name
			$(render_backup_snapshot_file_env)
			        - name: RCLONE_CONFIG
			          value: /etc/rclone/rclone.conf
			        - name: RCLONE_DESTINATION
			          valueFrom:
			            secretKeyRef:
			              name: ${RCLONE_CONFIG_SECRET}
			              key: ${RCLONE_DESTINATION_KEY}
			        volumeMounts:
			        - name: backup
			          mountPath: /backup
			        - name: rclone-config
			          mountPath: /etc/rclone
			          readOnly: true
		EOF
		return
	fi

	cat <<- EOF
		      - name: copy
		        image: ${COPY_IMAGE}
		        imagePullPolicy: IfNotPresent
		        command:
		        - /bin/sh
		        - -ec
		        - |
		          cd /backup
		          source_snapshot_file="${ETCD_NAME}.db"
		          if [ -n "\${BACKUP_SNAPSHOT_FILE:-}" ]; then
		            snapshot_file="\${BACKUP_SNAPSHOT_FILE}"
		          else
		            timestamp="\$(date -u +%Y%m%dT%H%M%SZ 2>/dev/null || printf '%s' "\${POD_NAME}")"
		            snapshot_file="${ETCD_NAME}-\${timestamp}.db"
		          fi
		          mv "\${source_snapshot_file}" "\${snapshot_file}"
		          printf '%s\n' "\${snapshot_file}" > snapshot-file
		          sha256sum "\${snapshot_file}" > "\${snapshot_file}.sha256"
		          ls -lh "\${snapshot_file}" "\${snapshot_file}.sha256"
		          echo "Snapshot and checksum are ready in /backup."
		          echo "Keeping the pod alive until the backup Job is deleted."
		          tail -f /dev/null
		        env:
		        - name: POD_NAME
		          valueFrom:
		            fieldRef:
		              fieldPath: metadata.name
		$(render_backup_snapshot_file_env)
		        volumeMounts:
		        - name: backup
		          mountPath: /backup
	EOF
}

render_rclone_config_volume() {
	if ! is_rclone_upload; then
		return
	fi

	cat <<- EOF
		      - name: rclone-config
		        secret:
		          secretName: ${RCLONE_CONFIG_SECRET}
		          items:
		          - key: ${RCLONE_CONFIG_KEY}
		            path: rclone.conf
	EOF
}

render_job_spec() {
	local snapshot_endpoint="${ENDPOINTS%%,*}"

	cat <<- EOF
		  backoffLimit: 0
		  template:
		    metadata:
		      labels:
		        app.kubernetes.io/name: kamaji-etcd-backup
		        app.kubernetes.io/component: backup
		        ovn.dpu.nvidia.com/skip-injection: ""
		    spec:
		      automountServiceAccountToken: false
		      # Keep app processes off PID 1 so the keep-alive copy container
		      # (tail -f /dev/null) exits promptly on SIGTERM when the Job is deleted.
		      shareProcessNamespace: true
		      restartPolicy: Never
		      tolerations:
		      - effect: NoSchedule
		        key: node-role.kubernetes.io/master
		        operator: Exists
		      - effect: NoSchedule
		        key: node-role.kubernetes.io/control-plane
		        operator: Exists
		$(render_colocation_affinity)
		      initContainers:
		      - name: endpoint-status
		        image: ${ETCD_IMAGE}
		        imagePullPolicy: IfNotPresent
		        command:
		        - etcdctl
		        - endpoint
		        - status
		        - --write-out=table
		        env:
		        - name: ETCDCTL_API
		          value: "3"
		        - name: ETCDCTL_ENDPOINTS
		          value: "${ENDPOINTS}"
		        - name: ETCDCTL_CACERT
		          value: /opt/certs/ca/ca.crt
		        - name: ETCDCTL_CERT
		          value: /opt/certs/client/tls.crt
		        - name: ETCDCTL_KEY
		          value: /opt/certs/client/tls.key
		        volumeMounts:
		        - name: ca
		          mountPath: /opt/certs/ca
		          readOnly: true
		        - name: client-certs
		          mountPath: /opt/certs/client
		          readOnly: true
		$(render_snapshot_container "$snapshot_endpoint")
		      containers:
		$(render_transfer_container)
		      volumes:
		      - name: backup
		        emptyDir: {}
		      - name: ca
		        secret:
		          secretName: ${CA_SECRET}
		          items:
		          - key: ca.crt
		            path: ca.crt
		      - name: client-certs
		        secret:
		          secretName: ${CLIENT_SECRET}
		          items:
		          - key: tls.crt
		            path: tls.crt
		          - key: tls.key
		            path: tls.key
		$(render_rclone_config_volume)
	EOF
}

render_job() {
	cat <<- EOF
		apiVersion: batch/v1
		kind: Job
		metadata:
		  name: ${JOB_NAME}
		  namespace: ${NAMESPACE}
		  labels:
		    app.kubernetes.io/name: kamaji-etcd-backup
		    app.kubernetes.io/component: backup
		spec:
		$(render_job_spec)
	EOF
}

render_cronjob() {
	cat <<- EOF
		apiVersion: batch/v1
		kind: CronJob
		metadata:
		  name: ${CRONJOB_NAME}
		  namespace: ${NAMESPACE}
		  labels:
		    app.kubernetes.io/name: kamaji-etcd-backup
		    app.kubernetes.io/component: backup
		spec:
		  schedule: $(yaml_quote "$CRON_SCHEDULE")
		  concurrencyPolicy: Forbid
		  successfulJobsHistoryLimit: 3
		  failedJobsHistoryLimit: 3
		  jobTemplate:
		    spec:
		$(render_job_spec | indent_lines "    ")
	EOF
}

render_backup_resource() {
	if is_cronjob; then
		render_cronjob
	else
		render_job
	fi
}

wait_for_backup_pod() {
	local pod=""
	local attempt

	for attempt in $(seq 1 60); do
		pod=$($KUBECTL -n "$NAMESPACE" get pods -l "job-name=${JOB_NAME}" \
			-o jsonpath='{.items[0].metadata.name}' 2> /dev/null || true)
		if [[ -n "$pod" ]]; then
			break
		fi
		sleep 2
	done

	[[ -n "$pod" ]] || fail "timed out waiting for backup pod for Job ${NAMESPACE}/${JOB_NAME}"
	echo "$pod"
}

wait_for_backup_ready() {
	local pod=$1

	echo "Waiting for snapshot to finish and backup container to become Ready..."
	if ! $KUBECTL -n "$NAMESPACE" wait --for=condition=Ready "pod/${pod}" --timeout="$WAIT_TIMEOUT"; then
		fail "The backup container did not become Ready after ${WAIT_TIMEOUT}."
	fi
}

read_backup_snapshot_file() {
	local pod=$1
	local snapshot_file

	snapshot_file=$($KUBECTL -n "$NAMESPACE" exec "$pod" -c copy -- cat /backup/snapshot-file 2> /dev/null || true)
	snapshot_file="${snapshot_file%%$'\n'*}"
	[[ -n "$snapshot_file" ]] || fail "could not read generated snapshot filename from backup pod ${pod}"

	SNAPSHOT_FILE="$snapshot_file"
}

wait_for_rclone_upload() {
	local pod=$1
	local complete
	local failed
	local attempt

	echo "Waiting for rclone upload Job to complete..."
	for attempt in $(seq 1 300); do
		complete=$($KUBECTL -n "$NAMESPACE" get job "$JOB_NAME" \
			-o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2> /dev/null || true)
		if [[ "$complete" == "True" ]]; then
			return
		fi

		failed=$($KUBECTL -n "$NAMESPACE" get job "$JOB_NAME" \
			-o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2> /dev/null || true)
		if [[ "$failed" == "True" ]]; then
			write_backup_logs "$pod"
			delete_rclone_secret
			fail "The rclone upload Job failed. Logs written to ${LOG_FILE}."
		fi

		sleep 2
	done

	write_backup_logs "$pod"
	delete_rclone_secret
	fail "Timed out waiting for rclone upload Job ${NAMESPACE}/${JOB_NAME} to complete after ${WAIT_TIMEOUT}. Logs written to ${LOG_FILE}."
}

download_backup_snapshot() {
	local pod=$1

	read_backup_snapshot_file "$pod"

	echo
	echo "Downloading backup snapshot from pod ${pod} to ${OUTPUT_FILE}..."
	mkdir -p "$(dirname "$OUTPUT_FILE")"
	if ! $KUBECTL -n "$NAMESPACE" cp -c copy "${pod}:/backup/${SNAPSHOT_FILE}" "$OUTPUT_FILE"; then
		fail "Failed to download backup snapshot. The copy container is still running."
	fi

	CHECKSUM_FILE="${OUTPUT_FILE}.sha256"
	echo "Downloading backup checksum from pod ${pod} to ${CHECKSUM_FILE}..."
	if ! $KUBECTL -n "$NAMESPACE" cp -c copy "${pod}:/backup/${SNAPSHOT_FILE}.sha256" "$CHECKSUM_FILE"; then
		fail "Failed to download backup checksum. The copy container is still running."
	fi

	verify_backup_checksum
	write_backup_logs "$pod"

	echo "Deleting the backup Job..."
	$KUBECTL -n "$NAMESPACE" delete job "${JOB_NAME}"
}

handle_rclone_upload() {
	local pod=$1

	echo "Backup snapshot uploaded to ${RCLONE_SECRET_DESTINATION}/${SNAPSHOT_FILE}"
	echo "Backup checksum uploaded to ${RCLONE_SECRET_DESTINATION}/${SNAPSHOT_FILE}.sha256"

	write_backup_logs "$pod"

	echo "Deleting the backup Job..."
	$KUBECTL -n "$NAMESPACE" delete job "${JOB_NAME}"
	delete_rclone_secret
}

verify_backup_checksum() {
	local checksum
	local output_base
	local output_dir

	if ! read -r checksum _ < "$CHECKSUM_FILE"; then
		fail "Downloaded backup checksum is empty or unreadable: ${CHECKSUM_FILE}"
	fi
	[[ -n "$checksum" ]] || fail "Downloaded backup checksum is empty: ${CHECKSUM_FILE}"

	output_base="$(basename "$OUTPUT_FILE")"
	output_dir="$(dirname "$OUTPUT_FILE")"
	printf '%s  %s\n' "$checksum" "$output_base" > "$CHECKSUM_FILE"

	echo "Verifying downloaded backup snapshot with ${CHECKSUM_FILE}..."
	if ! (
		cd "$output_dir"
		sha256sum -c "${output_base}.sha256"
	); then
		fail "Downloaded backup snapshot failed checksum verification. The copy container is still running."
	fi
	echo "Backup checksum verified: ${CHECKSUM_FILE}"
}

write_backup_logs() {
	local pod=$1
	local container

	if [[ -z "${LOG_FILE:-}" ]]; then
		if [[ -n "$OUTPUT_FILE" ]]; then
			LOG_FILE="${OUTPUT_FILE}.txt"
		else
			LOG_FILE="./${JOB_NAME}.txt"
		fi
	fi

	echo "Writing backup logs to ${LOG_FILE}..."
	: > "$LOG_FILE"

	for container in endpoint-status snapshot copy; do
		{
			echo "===== container: ${container} ====="
			if ! $KUBECTL -n "$NAMESPACE" logs "$pod" -c "$container"; then
				echo "failed to read logs for container ${container}" >&2
			fi
			echo
		} >> "$LOG_FILE" 2>&1
	done
	echo "Backup logs written to: ${LOG_FILE}"
}

main() {
	parse_args "$@"
	if [[ -n "$CRON_SCHEDULE" && -z "$CRONJOB_NAME" ]]; then
		CRONJOB_NAME="${ETCD_NAME}-backup"
	fi
	validate_args
	require_command "$KUBECTL"

	if is_cronjob; then
		JOB_NAME="$CRONJOB_NAME"
	else
		JOB_NAME="${ETCD_NAME}-backup-$(date -u +%Y%m%d%H%M%S)"
	fi
	SNAPSHOT_FILE="${ETCD_NAME}-$(date -u +%Y%m%dT%H%M%SZ).db"
	WAIT_TIMEOUT="10m"
	ETCD_SERVICE=""
	ENDPOINTS=""
	CA_SECRET="${ETCD_NAME}-certs"
	CLIENT_SECRET="${ETCD_NAME}-root-client-certs"
	COLOCATION_POD=""

	discover_statefulset
	discover_service

	discover_image
	[[ -n "$ETCD_IMAGE" ]] || fail "could not discover etcd image from StatefulSet ${NAMESPACE}/${ETCD_NAME}"

	discover_endpoints_from_statefulset
	ENDPOINTS=$(normalize_endpoints "$ENDPOINTS")
	[[ -n "$ENDPOINTS" ]] || fail "could not discover etcd endpoints"
	discover_colocation_pod
	if [[ -z "$OUTPUT_FILE" ]]; then
		OUTPUT_FILE="./${JOB_NAME}.db"
	fi

	if is_rclone_upload; then
		OUTPUT_FILE=""
		prepare_rclone_secret
	fi

	if [[ "$DRY_RUN" == true ]]; then
		render_backup_resource
		return
	fi

	if ! is_rclone_upload; then
		require_command sha256sum
	fi

	if is_rclone_upload; then
		create_rclone_secret
	fi

	if ! render_backup_resource | $KUBECTL apply -f -; then
		if ! is_cronjob || [[ "$RCLONE_SECRET_PREEXISTED" != true ]]; then
			delete_rclone_secret
		fi
		fail "Failed to create backup resource ${NAMESPACE}/${JOB_NAME}"
	fi

	if is_cronjob; then
		echo "Created or updated CronJob ${NAMESPACE}/${CRONJOB_NAME}"
		echo "CronJob schedule: ${CRON_SCHEDULE}"
		echo "Rclone config Secret: ${NAMESPACE}/${RCLONE_CONFIG_SECRET}"
		echo "Backup destination: ${RCLONE_SECRET_DESTINATION}/<snapshot-file>"
		return
	fi

	echo "Created Job ${NAMESPACE}/${JOB_NAME}"
	pod=$(wait_for_backup_pod)
	if is_rclone_upload; then
		wait_for_rclone_upload "$pod"
		handle_rclone_upload "$pod"
	else
		wait_for_backup_ready "$pod"
		download_backup_snapshot "$pod"
	fi
	echo
	echo "Backup logs written to: ${LOG_FILE}"
	if is_rclone_upload; then
		echo "Backup checksum uploaded to: ${RCLONE_SECRET_DESTINATION}/${SNAPSHOT_FILE}.sha256"
		echo "Backup snapshot uploaded to: ${RCLONE_SECRET_DESTINATION}/${SNAPSHOT_FILE}"
	else
		echo "Backup checksum written to: ${CHECKSUM_FILE}"
		echo "Backup snapshot downloaded to: ${OUTPUT_FILE}"
	fi
}

main "$@"
