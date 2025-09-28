#!/usr/bin/env bash

# -----------------------------------------------------------------------------
# Overview
# -----------------------------------------------------------------------------
# Entry point for running OVN Central inside Kubernetes. The script supports
# two modes:
# - Single-instance mode (ENABLE_HA=false): runs a standalone northd/ovsdb.
# - HA mode (ENABLE_HA=true): bootstraps or joins a 3-node RAFT cluster
#   deployed as a StatefulSet (e.g `in-cluster-ovn-central-0/1/2`) and starts northd.
#
# Key behavior
# - Requires the following env vars: POD_NAME, SERVICE_NAME, NAMESPACE,
#   ENABLE_HA, OVN_NB_PORT, OVN_SB_PORT.
# - Handles SIGTERM/SIGINT to gracefully leave RAFT for both NB/SB DBs and
#   stop `northd` and `ovsdb`.
# - In single mode: starts northd with insecure remote enabled and then blocks.
# - In HA mode:
#   1) Deletes any local NB/SB DB files to avoid stale state.
#   2) Builds cluster connection strings for NB/SB (in-cluster-ovn-central-0/1/2 on ports).
#   3) Prepares common `ovn-ctl` args bound to this pod's DNS name.
#   4) Detects an existing RAFT member by probing peer pods with
#      `ovn-appctl cluster/status` on the NB DB.
#      - If none found: this pod must be `in-cluster-ovn-central-0` and bootstraps
#        a new cluster.
#      - If found: uses the discovered pod IP as the remote RAFT address and
#        joins the existing cluster.
#   5) Starts `northd` with the appropriate cluster settings.
# - Finally tails forever to keep the container alive.
#
# Notes
# - RAFT membership detection uses the NB DB; the same remote IP is applied
#   for both NB and SB when joining.
# - Bootstrapping is restricted to pod-0 to keep a deterministic leader.
# -----------------------------------------------------------------------------

: "${POD_NAME:?POD_NAME not set}"
: "${SERVICE_NAME:?SERVICE_NAME not set}"
: "${NAMESPACE:?NAMESPACE not set}"
: "${ENABLE_HA:?ENABLE_HA not set}"
: "${OVN_NB_PORT:?OVN_NB_PORT not set}"
: "${OVN_SB_PORT:?OVN_SB_PORT not set}"

graceful_shutdown() {
	echo "Received shutdown signal, stopping OVN..."
	ovn-appctl -t /var/run/ovn/ovnnb_db.ctl cluster/leave OVN_Northbound || true
	ovn-appctl -t /var/run/ovn/ovnsb_db.ctl cluster/leave OVN_Southbound || true
	/usr/share/ovn/scripts/ovn-ctl stop_northd || true
	/usr/share/ovn/scripts/ovn-ctl stop_ovsdb || true
	exit 0
}
trap graceful_shutdown TERM INT

if [[ $ENABLE_HA == "false" ]]; then
	echo "--------------------------------"
	echo "Starting single ovn-central"
	echo "--------------------------------"
	echo "Starting northd process"
	echo "--------------------------------"
	/usr/share/ovn/scripts/ovn-ctl \
		--db-nb-create-insecure-remote=yes \
		--db-sb-create-insecure-remote=yes \
		start_northd

	tail -f /dev/null &
	wait $!
fi

echo "--------------------------------"
echo "Starting ovn-central in HA mode"
echo "--------------------------------"
# Ensure a completely fresh start by deleting any existing database files.
# This forces the pod to bootstrap or join as a new node without old state.
if [ -f /var/lib/ovn/ovnnb_db.db ]; then
	echo "Found existing ovnnb_db.db and ovnsb_db.db database files. Deleting to ensure fresh start..."
	rm -f /var/lib/ovn/ovnnb_db.db
	rm -f /var/lib/ovn/ovnsb_db.db
fi
OVN_NORTHD_NB_DB="tcp:${SERVICE_NAME}-0.${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local:${OVN_NB_PORT},tcp:${SERVICE_NAME}-1.${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local:${OVN_NB_PORT},tcp:${SERVICE_NAME}-2.${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local:${OVN_NB_PORT}"
OVN_NORTHD_SB_DB="tcp:${SERVICE_NAME}-0.${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local:${OVN_SB_PORT},tcp:${SERVICE_NAME}-1.${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local:${OVN_SB_PORT},tcp:${SERVICE_NAME}-2.${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local:${OVN_SB_PORT}"
echo "ovn-northd-nb-db=${OVN_NORTHD_NB_DB}"
echo "ovn-northd-sb-db=${OVN_NORTHD_SB_DB}"
echo "--------------------------------"

# Common ovn-ctl parameters
common_args=(
	--db-nb-addr="${POD_NAME}.${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local"
	--db-sb-addr="${POD_NAME}.${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local"
	--db-nb-cluster-local-addr="${POD_NAME}.${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local"
	--db-sb-cluster-local-addr="${POD_NAME}.${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local"
	--db-nb-create-insecure-remote=yes
	--db-sb-create-insecure-remote=yes
	--ovn-northd-nb-db="${OVN_NORTHD_NB_DB}"
	--ovn-northd-sb-db="${OVN_NORTHD_SB_DB}"
)

get_first_ovn_raft_member() {
	pod_list=($(kubectl get pod -n "$NAMESPACE" -o=jsonpath='{.items[*].metadata.name}' | grep -o "${SERVICE_NAME}[^ ]\+" || true))

	for pod in "${pod_list[@]}"; do
		if kubectl exec -n "$NAMESPACE" "$pod" -- \
			ovn-appctl -t /var/run/ovn/ovnnb_db.ctl cluster/status OVN_Northbound 2> /dev/null | grep -q "Status: cluster member"; then
			ip=$(kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.status.podIP}')
			echo "$pod $ip"
			return 0
		fi
	done
	return 1
}

# Determine cluster state and start OVN-central accordingly:
# - If no existing OVN-central RAFT members found, bootstrap a new cluster using `in-cluster-ovn-central-0` as leader
# - If an existing cluster is found, join it by connecting to an active member pod detected with `get_first_ovn_raft_member()`
# Both scenarios start the northd process with appropriate cluster configuration
# If the current leader dies, it rejoins as a cluster member

newCluster=false

result=$(get_first_ovn_raft_member)
if [[ -z "$result" ]]; then
	echo "No RAFT member pod found, starting new cluster"
	echo "--------------------------------"
	# if not found, we need to bootstrap a new cluster
	newCluster=true
fi

cluster_pod_ip=""
cluster_pod_name=""

# If a pod is found, it will be "cluster_pod_name cluster_pod_ip"
if [[ -n "$result" ]]; then
	cluster_pod_name=$(echo "$result" | awk '{print $1}')
	cluster_pod_ip=$(echo "$result" | awk '{print $2}')
fi

# If a new cluster is detected, we need to bootstrap with pod-0
if [[ $newCluster == true ]]; then
	echo "bootstrapping new cluster with the bootstrap pod"
	echo "POD_NAME: ${POD_NAME}"
	echo "SERVICE_NAME: ${SERVICE_NAME}"
	echo "NAMESPACE: ${NAMESPACE}"
	echo "--------------------------------"
	# we always bootstrap with pod-0 as the leader in a stetfulset deployment
	if [[ $POD_NAME != "${SERVICE_NAME}-0" ]]; then
		echo "expected pod ${SERVICE_NAME}-0, but got ${POD_NAME}"
		exit 1
	fi
	# OVN central leader
	echo "OVN central leader"
	echo "--------------------------------"
	echo "Starting northd process"
	echo "--------------------------------"
	/usr/share/ovn/scripts/ovn-ctl \
		"${common_args[@]}" \
		start_northd
else
	# Cluster already exists, we need to join the cluster
	echo "OVN central raft cluster already exists, connecting to existing pod in the cluster"
	echo "Joining the cluster with cluster pod: ${cluster_pod_name} and cluster pod ip: ${cluster_pod_ip}"
	echo "Starting northd process"
	echo "--------------------------------"
	# we need to join the raft cluster
	/usr/share/ovn/scripts/ovn-ctl \
		"${common_args[@]}" \
		--db-nb-cluster-remote-addr=${cluster_pod_ip} \
		--db-sb-cluster-remote-addr=${cluster_pod_ip} \
		start_northd
fi

tail -f /dev/null &
wait $!
