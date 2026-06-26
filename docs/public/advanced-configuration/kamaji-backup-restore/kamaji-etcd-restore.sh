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
SNAPSHOT_FILE=""
CONFIRM_RESTORE=false
DRY_RUN=false

usage() {
	cat <<- EOF
		Usage: $0 --snapshot-file FILE --confirm-restore [flags]

		Restore a Kamaji etcd StatefulSet from a local etcd snapshot.

		This is destructive. The script scales the etcd StatefulSet to zero,
		rewrites every etcd member PVC from the snapshot, and scales the
		StatefulSet back to its original replica count.

		Required:
		  --snapshot-file FILE  Local etcd snapshot produced by kamaji-etcd-backup.sh
		  --confirm-restore    Required for any non-dry-run restore

		Options:
		  --namespace, -n NAME     Namespace containing the Kamaji etcd StatefulSet (default: ${NAMESPACE})
		  --etcd-name NAME         Kamaji etcd StatefulSet name (default: ${ETCD_NAME})
		  --etcd-image IMAGE       etcd image for restore containers. Defaults to the StatefulSet etcd image
		  --copy-image IMAGE       Copy container image (default: ${COPY_IMAGE}). Requires /bin/sh, sleep, touch, rm, ls, and tar
		  --dry-run                Print restore Job manifests and the scale plan instead of applying them
		  -h, --help               Show this help

		Examples:
		  $0 --snapshot-file ./kamaji-etcd-backup.db --confirm-restore
		  $0 --namespace dpf-operator-system --etcd-name kamaji-etcd --snapshot-file ./backup.db --confirm-restore
		  $0 --snapshot-file ./backup.db --dry-run
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
		--snapshot-file | -f)
			require_arg "$1" "${2:-}"
			SNAPSHOT_FILE="$2"
			shift 2
			;;
		--confirm-restore)
			CONFIRM_RESTORE=true
			shift
			;;
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

discover_replicas() {
	REPLICAS=$($KUBECTL -n "$NAMESPACE" get statefulset "$ETCD_NAME" \
		-o jsonpath='{.spec.replicas}' 2> /dev/null || true)
	[[ -n "$REPLICAS" ]] || fail "could not discover replicas from StatefulSet ${NAMESPACE}/${ETCD_NAME}"
	[[ "$REPLICAS" =~ ^[0-9]+$ ]] || fail "unexpected replica count for StatefulSet ${NAMESPACE}/${ETCD_NAME}: ${REPLICAS}"
	((REPLICAS > 0)) || fail "StatefulSet ${NAMESPACE}/${ETCD_NAME} has no replicas"
}

discover_initial_cluster_token() {
	local command_args

	command_args=$($KUBECTL -n "$NAMESPACE" get statefulset "$ETCD_NAME" \
		-o jsonpath='{range .spec.template.spec.containers[?(@.name=="etcd")].command[*]}{@}{"\n"}{end}{range .spec.template.spec.containers[?(@.name=="etcd")].args[*]}{@}{"\n"}{end}' 2> /dev/null || true)
	INITIAL_CLUSTER_TOKEN=$(printf '%s\n' "$command_args" | sed -n 's/^--initial-cluster-token=//p' | head -n 1)
	INITIAL_CLUSTER_TOKEN="${INITIAL_CLUSTER_TOKEN:-kamaji}"
}

initial_cluster() {
	local members=()
	local i

	for ((i = 0; i < REPLICAS; i++)); do
		members+=("${ETCD_NAME}-${i}=https://${ETCD_NAME}-${i}.${ETCD_SERVICE}.${NAMESPACE}.svc.cluster.local:2380")
	done

	(
		IFS=,
		echo "${members[*]}"
	)
}

member_claim_name() {
	local index=$1

	echo "${DATA_VOLUME_CLAIM_PREFIX}-${ETCD_NAME}-${index}"
}

member_restore_node() {
	local index=$1
	local claim_name
	local pv_name
	local node

	claim_name=$(member_claim_name "$index")
	node=$($KUBECTL -n "$NAMESPACE" get pvc "$claim_name" \
		-o jsonpath='{.metadata.annotations.volume\.kubernetes\.io/selected-node}' 2> /dev/null || true)
	if [[ -n "$node" ]]; then
		echo "$node"
		return
	fi

	pv_name=$($KUBECTL -n "$NAMESPACE" get pvc "$claim_name" -o jsonpath='{.spec.volumeName}' 2> /dev/null || true)
	if [[ -z "$pv_name" ]]; then
		return
	fi

	node=$($KUBECTL get pv "$pv_name" \
		-o jsonpath='{.metadata.annotations.local\.path\.provisioner/selected-node}' 2> /dev/null || true)
	if [[ -n "$node" ]]; then
		echo "$node"
		return
	fi

	$KUBECTL get pv "$pv_name" \
		-o jsonpath='{range .spec.nodeAffinity.required.nodeSelectorTerms[*].matchExpressions[?(@.key=="kubernetes.io/hostname")].values[*]}{@}{"\n"}{end}' 2> /dev/null \
		| sed -n '1p'
}

render_restore_node_affinity() {
	local node=$1

	if [[ -z "$node" ]]; then
		return
	fi

	cat <<- EOF
		      affinity:
		        nodeAffinity:
		          requiredDuringSchedulingIgnoredDuringExecution:
		            nodeSelectorTerms:
		            - matchExpressions:
		              - key: kubernetes.io/hostname
		                operator: In
		                values:
		                - ${node}
	EOF
}

render_restore_job() {
	local index=$1
	local restore_job_name="${RESTORE_JOB_PREFIX}-${index}"
	local member_name="${ETCD_NAME}-${index}"
	local claim_name
	local initial_cluster_value
	local restore_node

	claim_name=$(member_claim_name "$index")
	initial_cluster_value=$(initial_cluster)
	restore_node=$(member_restore_node "$index")

	cat <<- EOF
		apiVersion: batch/v1
		kind: Job
		metadata:
		  name: ${restore_job_name}
		  namespace: ${NAMESPACE}
		  labels:
		    app.kubernetes.io/name: kamaji-etcd-restore
		    app.kubernetes.io/component: restore
		    app.kubernetes.io/instance: ${ETCD_NAME}
		    apps.kubernetes.io/pod-index: "${index}"
		spec:
		  backoffLimit: 0
		  template:
		    metadata:
		      labels:
		        app.kubernetes.io/name: kamaji-etcd-restore
		        app.kubernetes.io/component: restore
		        app.kubernetes.io/instance: ${ETCD_NAME}
		        apps.kubernetes.io/pod-index: "${index}"
		        ovn.dpu.nvidia.com/skip-injection: ""
		    spec:
		      automountServiceAccountToken: false
		      restartPolicy: Never
		      tolerations:
		      - effect: NoSchedule
		        key: node-role.kubernetes.io/master
		        operator: Exists
		      - effect: NoSchedule
		        key: node-role.kubernetes.io/control-plane
		        operator: Exists
		$(render_restore_node_affinity "$restore_node")
		      initContainers:
		      - name: load-snapshot
		        image: ${COPY_IMAGE}
		        imagePullPolicy: IfNotPresent
		        command:
		        - /bin/sh
		        - -ec
		        - |
		          echo "Waiting for /restore/snapshot.db and /restore/.snapshot-uploaded."
		          while [ ! -f /restore/snapshot.db ] || [ ! -f /restore/.snapshot-uploaded ]; do
		            sleep 2
		          done
		          ls -lh /restore/snapshot.db
		        volumeMounts:
		        - name: restore
		          mountPath: /restore
		      - name: snapshot-status
		        image: ${ETCD_IMAGE}
		        imagePullPolicy: IfNotPresent
		        command:
		        - etcdutl
		        - --write-out=table
		        - snapshot
		        - status
		        - /restore/snapshot.db
		        volumeMounts:
		        - name: restore
		          mountPath: /restore
		      - name: clear-data
		        image: ${COPY_IMAGE}
		        imagePullPolicy: IfNotPresent
		        command:
		        - /bin/sh
		        - -ec
		        - |
		          rm -rf "${DATA_DIR}/member"
		        volumeMounts:
		        - name: data
		          mountPath: ${DATA_DIR}
		      containers:
		      - name: restore
		        image: ${ETCD_IMAGE}
		        imagePullPolicy: IfNotPresent
		        command:
		        - etcdutl
		        - snapshot
		        - restore
		        - /restore/snapshot.db
		        - --data-dir
		        - ${DATA_DIR}
		        - --name
		        - ${member_name}
		        - --initial-cluster
		        - ${initial_cluster_value}
		        - --initial-cluster-token
		        - ${INITIAL_CLUSTER_TOKEN}
		        - --initial-advertise-peer-urls
		        - https://${member_name}.${ETCD_SERVICE}.${NAMESPACE}.svc.cluster.local:2380
		        volumeMounts:
		        - name: restore
		          mountPath: /restore
		        - name: data
		          mountPath: ${DATA_DIR}
		      volumes:
		      - name: restore
		        emptyDir: {}
		      - name: data
		        persistentVolumeClaim:
		          claimName: ${claim_name}
	EOF
}

restore_job_name() {
	local index=$1

	echo "${RESTORE_JOB_PREFIX}-${index}"
}

restore_job_pod() {
	local index=$1
	local job_name

	job_name=$(restore_job_name "$index")
	$KUBECTL -n "$NAMESPACE" get pods -l "job-name=${job_name}" -o jsonpath='{.items[0].metadata.name}' 2> /dev/null || true
}

wait_for_restore_pod_loader() {
	local index=$1
	local pod=""
	local started_at=""
	local attempt

	for attempt in $(seq 1 120); do
		pod=$(restore_job_pod "$index")
		if [[ -n "$pod" ]]; then
			started_at=$($KUBECTL -n "$NAMESPACE" get pod "$pod" \
				-o jsonpath='{.status.initContainerStatuses[?(@.name=="load-snapshot")].state.running.startedAt}' 2> /dev/null || true)
			if [[ -n "$started_at" ]]; then
				echo "$pod"
				return
			fi
		fi
		sleep 2
	done

	[[ -n "$pod" ]] && $KUBECTL -n "$NAMESPACE" describe pod "$pod" >&2 || true
	fail "timed out waiting for restore pod ${index} load-snapshot container to run"
}

wait_for_pod_delete_if_exists() {
	local pod=$1

	if $KUBECTL -n "$NAMESPACE" get pod "$pod" > /dev/null 2>&1; then
		$KUBECTL -n "$NAMESPACE" wait --for=delete "pod/${pod}" --timeout="$WAIT_TIMEOUT"
	fi
}

run_restore() {
	local original_replicas=$REPLICAS
	local index
	local pod
	local job_name

	echo "Scaling ${NAMESPACE}/${ETCD_NAME} to 0 replicas..."
	$KUBECTL -n "$NAMESPACE" scale statefulset "$ETCD_NAME" --replicas=0

	for ((index = 0; index < original_replicas; index++)); do
		wait_for_pod_delete_if_exists "${ETCD_NAME}-${index}"
	done

	echo "Creating restore Jobs and uploading snapshot to each member..."
	for ((index = 0; index < original_replicas; index++)); do
		render_restore_job "$index" | $KUBECTL apply -f -
		pod=$(wait_for_restore_pod_loader "$index")
		echo "Uploading snapshot to ${pod}..."
		$KUBECTL -n "$NAMESPACE" cp -c load-snapshot "$SNAPSHOT_FILE" "${pod}:/restore/snapshot.db"
		$KUBECTL -n "$NAMESPACE" exec "$pod" -c load-snapshot -- touch /restore/.snapshot-uploaded
	done

	echo "Waiting for restore Jobs to complete..."
	for ((index = 0; index < original_replicas; index++)); do
		job_name=$(restore_job_name "$index")
		if ! $KUBECTL -n "$NAMESPACE" wait --for=condition=complete "job/${job_name}" --timeout="$WAIT_TIMEOUT"; then
			pod=$(restore_job_pod "$index")
			[[ -n "$pod" ]] && $KUBECTL -n "$NAMESPACE" logs "$pod" -c restore >&2 || true
			echo "error: restore Job ${NAMESPACE}/${job_name} did not complete." >&2
			echo "The etcd StatefulSet ${NAMESPACE}/${ETCD_NAME} is still scaled to 0 replicas; it was left unchanged." >&2
			echo "Investigate the failed Job, then retry the restore or scale the StatefulSet back manually with:" >&2
			echo "  ${KUBECTL} -n ${NAMESPACE} scale statefulset ${ETCD_NAME} --replicas=${original_replicas}" >&2
			exit 1
		fi
	done

	echo "Scaling ${NAMESPACE}/${ETCD_NAME} back to ${original_replicas} replicas..."
	$KUBECTL -n "$NAMESPACE" scale statefulset "$ETCD_NAME" --replicas="$original_replicas"
	$KUBECTL -n "$NAMESPACE" rollout status "statefulset/${ETCD_NAME}" --timeout="$WAIT_TIMEOUT"

	echo
	echo "Restore complete. Delete restore Jobs with:"
	echo "  ${KUBECTL} -n ${NAMESPACE} delete job -l app.kubernetes.io/name=kamaji-etcd-restore,app.kubernetes.io/instance=${ETCD_NAME}"
}

print_dry_run() {
	local index

	cat >&2 <<- EOF
		Restore plan:
		  Namespace: ${NAMESPACE}
		  StatefulSet: ${ETCD_NAME}
		  Headless Service: ${ETCD_SERVICE}
		  Replicas: ${REPLICAS}
		  Snapshot: ${SNAPSHOT_FILE}
		  etcd image: ${ETCD_IMAGE}
		  Data dir: ${DATA_DIR}
		  PVCs: ${DATA_VOLUME_CLAIM_PREFIX}-${ETCD_NAME}-<index>

		No resources will be changed. Generated restore Job manifests follow.
	EOF

	for ((index = 0; index < REPLICAS; index++)); do
		echo "---"
		render_restore_job "$index"
	done
}

main() {
	parse_args "$@"
	require_command "$KUBECTL"

	[[ -n "$SNAPSHOT_FILE" ]] || fail "--snapshot-file is required"
	if [[ "$DRY_RUN" != true ]]; then
		[[ -f "$SNAPSHOT_FILE" ]] || fail "snapshot file does not exist: ${SNAPSHOT_FILE}"
		[[ -r "$SNAPSHOT_FILE" ]] || fail "snapshot file is not readable: ${SNAPSHOT_FILE}"
	fi

	WAIT_TIMEOUT="10m"
	DATA_DIR="/var/run/etcd"
	DATA_VOLUME_CLAIM_PREFIX="data"
	RESTORE_JOB_PREFIX="${ETCD_NAME}-restore-$(date -u +%Y%m%d%H%M%S)"

	discover_statefulset
	discover_service

	discover_image
	[[ -n "$ETCD_IMAGE" ]] || fail "could not discover etcd image from StatefulSet ${NAMESPACE}/${ETCD_NAME}"

	discover_replicas
	discover_initial_cluster_token

	if [[ "$DRY_RUN" == true ]]; then
		print_dry_run
		return
	fi

	if [[ "$CONFIRM_RESTORE" != true ]]; then
		fail "restore is destructive; rerun with --confirm-restore after stopping Kamaji tenant control-plane writers"
	fi

	run_restore
}

main "$@"
