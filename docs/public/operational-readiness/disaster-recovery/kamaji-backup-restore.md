---
title: "Kamaji etcd Backup and Restore"
---

[TOC]

## Overview

The two helper scripts automate the etcd snapshot and restore flow. Run them as-is to avoid
hand-crafting the Jobs and `etcdctl`/`etcdutl` invocations, or use them as a starting point for your
own scripts. At a high level:

`./kamaji-etcd-backup.sh`, run with no arguments, performs these steps:

1. Discovers the Kamaji etcd StatefulSet, its headless Service, the etcd image, and the member endpoints.
2. Creates a Kubernetes Job in the etcd namespace.
3. The Job init container checks etcd endpoint status before the snapshot.
4. The Job init container runs `etcdctl snapshot save` into a shared `emptyDir`.
5. The `copy` container (using the rclone image when uploading) renames the snapshot, writes a SHA-256
   checksum, and either keeps the files for download or uploads them to the rclone destination.
6. The local script downloads both files with `kubectl cp`, or waits for the rclone upload to finish.
7. The local script verifies the downloaded snapshot against its checksum and saves the Job logs.
8. The local script deletes the temporary Job, and the rclone Secret for one-shot uploads.

`./kamaji-etcd-restore.sh --snapshot-file <file> --confirm-restore` performs these steps:

1. Discovers the etcd StatefulSet, Service, etcd image, replica count, and each member's PVC node.
2. Scales the etcd StatefulSet to `0` and waits for the member Pods to terminate.
3. Creates one restore Job per etcd member, pinned to that member's node.
4. Uploads the snapshot into each restore Job and triggers the restore.
5. Each restore Job validates the snapshot and runs `etcdutl snapshot restore` onto the member PVC.
6. Waits for every restore Job to complete.
7. Scales the StatefulSet back to its original replica count and waits for the rollout.

The sections below cover the full strategy, prerequisites, and step-by-step procedures in detail.

## Backup Strategy

A complete backup of Kamaji-backed DPUClusters has two layers, which can be backed up in either order:

* Back up the Kamaji etcd datastore used by the tenant control planes. Use the
  [backup script](#backup-script) helper described below for the bundled Kamaji etcd
  StatefulSet. This captures the Kubernetes API data stored inside that etcd instance.
* Back up the Kamaji and DPF objects that live in the management cluster, such as
  `TenantControlPlane` resources, related Secrets, namespaces, and other objects required to
  recreate the Kamaji control plane. Follow the upstream
  [Kamaji backup and restore guide](https://kamaji.clastix.io/guides/backup-and-restore/) for that
  layer. The Kamaji guide uses Velero; see the [Velero documentation](https://velero.io/docs/latest/)
  for installation, backup storage location, backup, and restore details.

Do both backups on the same operational cadence and keep their timestamps together. The etcd
snapshot alone is not a full management-cluster backup, and a management-cluster object backup alone
does not replace the tenant control-plane datastore snapshot.

## Restore Strategy

A complete Kamaji restore covers the same two layers. They are not strictly sequential: the goal is to
keep the affected tenant control planes from reconciling against an empty or stale datastore while you
restore, so coordinate the two layers as described below.

* Restore the Kamaji etcd datastore snapshot with the [restore script](#restore-script) helper
  described below. This is the datastore restore step for the bundled Kamaji etcd StatefulSet. Stop or
  scale down the affected tenant control-plane writers before running it, and verify the snapshot
  checksum and etcd snapshot status first.
* Restore or recreate the Kamaji and DPF objects that live in the management cluster, such as
  `TenantControlPlane` resources, related Secrets, namespaces, and other Kamaji resources. Follow the
  upstream [Kamaji backup and restore guide](https://kamaji.clastix.io/guides/backup-and-restore/) for
  this layer, and use [Velero](https://velero.io/docs/latest/) or an equivalent management-cluster
  backup tool according to your environment.

In practice, restore or recreate the management-cluster objects with their control-plane replicas
stopped, restore the matching etcd snapshot, then allow the tenant control planes to reconcile and
verify the affected `DPUCluster` resources. The detailed [Restore](#restore) procedure below performs
these steps in the correct order.

## Scope

Kamaji is one of the options for providing the Kubernetes control plane of a `DPUCluster`. This guide
covers backup and restore only for DPF deployments that use Kamaji-backed `DPUCluster` resources.
Internally, each such `DPUCluster` is backed by a Kamaji `TenantControlPlane`, and Kamaji stores that
control-plane data in a `DataStore`. In DPF installations that use the bundled Kamaji etcd chart,
that `DataStore` is backed by an etcd StatefulSet such as `kamaji-etcd`.

The following procedure backs up only the data stored in that etcd instance. It does not back up node-local
state, node filesystems, container images, workload PVC contents, or any other persistent volumes in
the cluster. Back those up separately if they are required for disaster recovery; see the
[Velero documentation](https://velero.io/docs/latest/) for guidance on backing up Kubernetes
objects and persistent volumes.

The snapshot covers the whole selected etcd instance, not a single `DPUCluster`. If several
`DPUCluster` control planes use the same backing etcd instance, the backup contains all of them.

## Safety and Storage Guidance

Snapshots contain Kubernetes API objects and Secrets for every `DPUCluster` control plane in the
selected etcd instance. Keep copied `.db` files in encrypted durable storage outside the cluster and
limit access to operators who can administer the affected DPUClusters.

Restoring replaces all existing data in the selected etcd instance with the snapshot contents. Test
restore in a non-production environment before relying on the backup procedure. After a restore,
verify that every affected `DPUCluster` is Ready and that clients can access the restored DPUCluster
APIs.

## Prerequisites

The backup script needs permission to:

* read the Kamaji etcd StatefulSet, Service, and Secrets
* create and delete a temporary rclone configuration Secret when using one-shot rclone upload mode
* create or update a persistent rclone configuration Secret when using scheduled backup mode
* create, read, and delete backup Jobs and Pods in the namespace where etcd is running
* create or update backup CronJobs when using scheduled backup mode
* run `kubectl cp` and `kubectl exec` against the created Pods

The restore procedure needs permission to:

* read `DPUCluster` resources and their backing Kamaji `TenantControlPlane` resources
* read the Kamaji etcd StatefulSet, Service, PVCs, and bound PVs
* create, read, and delete restore Jobs and Pods in the namespace where etcd is running
* run `kubectl cp` and `kubectl exec` against the created Pods
* scale the Kamaji etcd StatefulSet during restore
* annotate and patch Kamaji `TenantControlPlane` resources during restore preparation and recovery

For local-download backups, `kubectl cp` requires `tar` in the copy container image. The backup Job
also uses the copy container to rename the fixed etcd snapshot file and create the checksum before
download. The default `busybox:1.36` image provides the required tools. The backup helper also
requires `sha256sum` on the local machine for local-download backups so it can verify the downloaded
snapshot against the downloaded checksum. The restore verification commands use `sha256sum` and
`etcdutl` on the local machine before running the restore helper.

When uploading directly to a remote storage target, the backup Job uses an rclone image instead of
the copy image. The default `rclone/rclone:latest` image must be pullable by the cluster. The helper
creates a temporary Secret from the provided local `rclone.conf` file and upload destination, mounts
that Secret into the backup Job, and deletes it after a successful one-shot upload. Scheduled
backups keep the rclone Secret so future CronJob runs can use it.

## Backup

> [!WARNING]
> The snapshot contains Kubernetes API objects and Secrets for every `DPUCluster` control plane in
> the selected etcd instance. Treat the `.db` file as highly sensitive: restrict access to operators
> who can administer those DPUClusters, store it only in encrypted durable storage outside the
> cluster, transmit it only over approved encrypted channels, and delete temporary local copies after
> the backup is secured and restore testing is complete. Do not commit snapshots to source control or
> attach them to support cases, bug reports, or logs.

The examples below assume you run the [backup script](#backup-script) and
[restore script](#restore-script).

Run the backup helper:

```bash
./kamaji-etcd-backup.sh
```

By default, the helper backs up the `kamaji-etcd` StatefulSet in the `dpf-operator-system`
namespace. It reads the Kubernetes Service from `spec.serviceName` and uses that Service for etcd
member DNS names. If your deployment uses a different namespace or StatefulSet name, pass them:

```bash
./kamaji-etcd-backup.sh \
  --namespace <namespace> \
  --etcd-name <statefulset-name>
```

The helper creates a Kubernetes Job that runs `etcdctl snapshot save` to write a fixed
`/backup/<etcd-name>.db` file. The transfer container then renames that file to a timestamped
snapshot name, writes `<snapshot-file>.sha256`, and either downloads both files with `kubectl cp` or
uploads both files with rclone. For local downloads, it verifies the downloaded snapshot against the
downloaded checksum before deleting the Job. With `--cron-schedule`, the helper creates or updates a
Kubernetes CronJob instead of a one-shot Job.

By default, the snapshot is written locally as:

```bash
./<job-name>.db
```

For each backup, keep these files together:

* `<snapshot-file>` - the etcd snapshot
* `<snapshot-file>.sha256` - SHA-256 checksum generated in the backup Pod for the snapshot
* `<snapshot-file>.txt` - backup Job logs

Use `--output-file` to choose a path:

```bash
./kamaji-etcd-backup.sh \
  --output-file ./backups/kamaji-etcd.db
```

### Backup Options

Inspect the generated Job or CronJob without creating it:

```bash
./kamaji-etcd-backup.sh --dry-run
```

`--dry-run` prints only the Kubernetes resource YAML. Redirect the output to a file if you need to
modify the generated manifest before applying it manually:

```bash
./kamaji-etcd-backup.sh --dry-run > kamaji-etcd-backup-job.yaml
```

The backup Job includes control-plane tolerations, DPF OVN skip-injection, and by default pod
affinity to run on the same node as the etcd pod used for the snapshot. The backup helper uses the
selected StatefulSet to discover the Kubernetes Service, endpoints, and image. It uses the bundled
TLS secret names derived from the StatefulSet name.

If the cluster cannot pull the default copy container image, pass `--copy-image`:

```bash
./kamaji-etcd-backup.sh \
  --copy-image <registry>/<image>:<tag>
```

The copy image must provide `/bin/sh`, `sha256sum`, `mv`, `cat`, `tail`, `ls`, and `tar`. The
`kubectl cp` download step requires `tar` in the container.

To upload directly from the backup Job with rclone, provide a local `rclone.conf` file and the
remote directory where the Job should copy the snapshot and checksum. The helper creates a temporary
Kubernetes Secret from those values so the Job does not receive credentials through plain manifest
fields.

Protect `rclone.conf` like other backup credentials when it contains access keys, and do not commit
it to source control.

For example, an S3-compatible rclone configuration can look like:

```ini
[backup]
type = s3
provider = Other
access_key_id = <access-key-id>
secret_access_key = <secret-access-key>
endpoint = <https://s3-endpoint.example.com>
region = us-east-1
```

Run the backup helper with the config file and destination:

```bash
./kamaji-etcd-backup.sh \
  --rclone-config-file ./rclone.conf \
  --rclone-destination backup:BUCKET/kamaji-etcd
```

If the config file contains exactly one remote, the remote name can be omitted from the destination:

```bash
./kamaji-etcd-backup.sh \
  --rclone-config-file ./rclone.conf \
  --rclone-destination BUCKET/kamaji-etcd
```

In rclone mode, the helper does not use `kubectl cp` for the snapshot. If the resolved destination
is `backup:BUCKET/kamaji-etcd`, the Job uploads:

* `backup:BUCKET/kamaji-etcd/<snapshot-file>`
* `backup:BUCKET/kamaji-etcd/<snapshot-file>.sha256`

The helper still writes the backup Job logs locally as `./<job-name>.txt`.

Use `--rclone-image` if the cluster must pull the rclone upload image from a different registry:

```bash
./kamaji-etcd-backup.sh \
  --rclone-config-file ./rclone.conf \
  --rclone-destination backup:BUCKET/kamaji-etcd \
  --rclone-image <registry>/<rclone-image>:<tag>
```

The rclone upload image must provide `/bin/sh`, `rclone`, `sha256sum`, `mv`, `date`, and `ls`. The
rclone Job completes after the snapshot and checksum are uploaded.

To create or update a scheduled backup CronJob, add `--cron-schedule`. Scheduled backups require
rclone upload mode because the helper cannot perform an unattended recurring `kubectl cp` download:

```bash
./kamaji-etcd-backup.sh \
  --cron-schedule '0 2 * * *' \
  --rclone-config-file ./rclone.conf \
  --rclone-destination backup:BUCKET/kamaji-etcd
```

By default, the CronJob is named `<etcd-name>-backup`, and its rclone Secret is named
`<cronjob-name>-rclone`. Use `--cronjob-name` to choose a different CronJob name:

```bash
./kamaji-etcd-backup.sh \
  --cron-schedule '0 2 * * *' \
  --cronjob-name kamaji-etcd-nightly-backup \
  --rclone-config-file ./rclone.conf \
  --rclone-destination backup:BUCKET/kamaji-etcd
```

Each CronJob run uses the same flow as a one-shot Job: the etcd container first creates a fixed
`/backup/<etcd-name>.db` snapshot, then the rclone container renames it to a timestamped snapshot
filename, uploads the snapshot and checksum to the configured destination, and exits. The helper
does not wait for CronJob runs or write local logs in scheduled mode. Rerun the helper to update the
CronJob schedule, destination, rclone configuration, or image.

Use `--etcd-image` only when the script cannot discover the etcd image from the StatefulSet or when
the cluster must pull it from a different registry:

```bash
./kamaji-etcd-backup.sh \
  --etcd-image <registry>/<etcd-image>:<tag>
```

The etcd image must provide `etcdctl`.

For non-standard layouts, generate the Job YAML with `--dry-run`, adjust the manifest, and apply it
manually.

## Restore

> [!WARNING]
> Restoring is destructive. The restore helper scales the etcd StatefulSet to `0`, rewrites every
> member PVC from the snapshot, and scales the StatefulSet back up. All existing data in the selected
> etcd instance will be replaced by the snapshot contents. Stop all writers that use the affected
> DPUCluster APIs and confirm the backing control planes are scaled down before running the restore.

This procedure assumes the selected etcd StatefulSet backs the only Kamaji `DataStore`, so every
Kamaji-backed `DPUCluster` is affected.

Set the target etcd StatefulSet and snapshot:

```bash
ETCD_NAMESPACE=dpf-operator-system
ETCD_STATEFULSET=kamaji-etcd
SNAPSHOT=./<job-name>.db
```

If the backup was uploaded with rclone, download both the snapshot and checksum to the same local
directory before running the restore helper. The example assumes rclone has a configured remote
named `backup`:

```bash
rclone copyto backup:BUCKET/kamaji-etcd/<snapshot-file> ./<snapshot-file>
rclone copyto backup:BUCKET/kamaji-etcd/<snapshot-file>.sha256 ./<snapshot-file>.sha256

SNAPSHOT=./<snapshot-file>
```

Verify the backup before doing any destructive restore steps:

```bash
SNAPSHOT_DIR="$(dirname "$SNAPSHOT")"
SNAPSHOT_BASENAME="$(basename "$SNAPSHOT")"

(
  cd "$SNAPSHOT_DIR"
  sha256sum -c "${SNAPSHOT_BASENAME}.sha256"
  etcdutl snapshot status "$SNAPSHOT_BASENAME" --write-out=table
)
```

The checksum check verifies that the snapshot file still matches the checksum generated in the
backup Pod after copying, storing, or transferring it. The `etcdutl snapshot status` check verifies
that the file is readable as an etcd snapshot. Both checks must pass before running the restore
helper with `--confirm-restore`.

Review the affected `DPUCluster` control planes:

```bash
kubectl get dpucluster -A \
  -o go-template='{{range .items}}{{if eq .spec.type "kamaji"}}{{.metadata.namespace}}/{{.metadata.name}}{{"\n"}}{{end}}{{end}}'
```

Scale down the backing Kamaji control planes for all affected `DPUCluster` resources and remember
their current replica counts:

```bash
DPUCLUSTER_REFS=$(kubectl get dpucluster -A \
  -o go-template='{{range .items}}{{if eq .spec.type "kamaji"}}{{.metadata.namespace}}/{{.metadata.name}}{{"\n"}}{{end}}{{end}}')

for dpucluster_ref in $DPUCLUSTER_REFS; do
  dpucluster_ns="${dpucluster_ref%/*}"
  dpucluster_name="${dpucluster_ref#*/}"
  replicas=$(kubectl -n "$dpucluster_ns" get tenantcontrolplane "$dpucluster_name" \
    -o jsonpath='{.spec.controlPlane.deployment.replicas}')

  kubectl -n "$dpucluster_ns" annotate tenantcontrolplane "$dpucluster_name" \
    restore.kamaji-etcd/original-replicas="${replicas}" \
    --overwrite

  kubectl -n "$dpucluster_ns" patch tenantcontrolplane "$dpucluster_name" --type=merge \
    -p '{"spec":{"controlPlane":{"deployment":{"replicas":0}}}}'
done
```

Wait for the generated control-plane Pods for each affected `DPUCluster` to disappear:

```bash
for dpucluster_ref in $DPUCLUSTER_REFS; do
  dpucluster_ns="${dpucluster_ref%/*}"
  dpucluster_name="${dpucluster_ref#*/}"
  while kubectl -n "$dpucluster_ns" get pod \
    -l "kamaji.clastix.io/component=deployment,kamaji.clastix.io/name=$dpucluster_name" \
    -o name | grep -q .; do
    sleep 2
  done
done
```

Review the restore plan and generated restore Jobs:

```bash
./kamaji-etcd-restore.sh \
  --namespace "$ETCD_NAMESPACE" \
  --etcd-name "$ETCD_STATEFULSET" \
  --snapshot-file "$SNAPSHOT" \
  --dry-run
```

`--dry-run` prints the restore scale plan and generated Job YAML without changing the cluster.

Before changing etcd, confirm that every `TenantControlPlane` has
`spec.controlPlane.deployment.replicas: 0` and that its generated Pods have terminated. The helper
does not discover or scale `TenantControlPlane` resources.

Run the restore:

```bash
./kamaji-etcd-restore.sh \
  --namespace "$ETCD_NAMESPACE" \
  --etcd-name "$ETCD_STATEFULSET" \
  --snapshot-file "$SNAPSHOT" \
  --confirm-restore
```

Scale the backing Kamaji control planes back up:

```bash
for dpucluster_ref in $DPUCLUSTER_REFS; do
  dpucluster_ns="${dpucluster_ref%/*}"
  dpucluster_name="${dpucluster_ref#*/}"
  replicas=$(kubectl -n "$dpucluster_ns" get tenantcontrolplane "$dpucluster_name" \
    -o jsonpath='{.metadata.annotations.restore\.kamaji-etcd/original-replicas}')

  kubectl -n "$dpucluster_ns" patch tenantcontrolplane "$dpucluster_name" --type=merge \
    -p "{\"spec\":{\"controlPlane\":{\"deployment\":{\"replicas\":${replicas:-1}}}}}"

  kubectl -n "$dpucluster_ns" rollout status deployment "$dpucluster_name" --timeout=10m

  kubectl -n "$dpucluster_ns" annotate tenantcontrolplane "$dpucluster_name" \
    restore.kamaji-etcd/original-replicas- || true
done
```

Wait for the affected `DPUCluster` resources to become Ready again:

```bash
for dpucluster_ref in $DPUCLUSTER_REFS; do
  dpucluster_ns="${dpucluster_ref%/*}"
  dpucluster_name="${dpucluster_ref#*/}"
  kubectl -n "$dpucluster_ns" wait --for=condition=ready dpucluster "$dpucluster_name" --timeout=10m
done
```

Delete the restore Jobs:

```bash
kubectl -n "$ETCD_NAMESPACE" delete job \
  -l "app.kubernetes.io/name=kamaji-etcd-restore,app.kubernetes.io/instance=${ETCD_STATEFULSET}"
```

### Restore Options

If etcd uses non-default names:

```bash
./kamaji-etcd-restore.sh \
  --namespace <namespace> \
  --etcd-name <statefulset-name> \
  --snapshot-file "$SNAPSHOT" \
  --confirm-restore
```

The restore helper reads the headless Service name, replica count, and etcd image from the selected
StatefulSet. It assumes the bundled Kamaji etcd chart layout: PVCs named
`data-<statefulset-name>-<index>` and an etcd data directory of `/var/run/etcd`.

If the cluster cannot pull the default copy container image, pass `--copy-image`:

```bash
./kamaji-etcd-restore.sh \
  --copy-image <registry>/<image>:<tag> \
  --snapshot-file "$SNAPSHOT" \
  --confirm-restore
```

The copy image must provide `/bin/sh`, `sleep`, `touch`, `rm`, `ls`, and `tar`. The `kubectl cp`
upload step requires `tar` in the container.

Use `--etcd-image` only when the script cannot discover the etcd image from the StatefulSet or when
the cluster must pull it from a different registry:

```bash
./kamaji-etcd-restore.sh \
  --etcd-image <registry>/<etcd-image>:<tag> \
  --snapshot-file "$SNAPSHOT" \
  --confirm-restore
```

The etcd image must provide `etcdutl` for `snapshot status` and `snapshot restore`.

The restore Jobs include control-plane tolerations, DPF OVN skip-injection, and automatic node
affinity from each member PVC or bound PV when it can be discovered. For non-standard chart layouts,
generate the Job YAML with `--dry-run`, adjust the manifests, and perform the scale and restore steps
manually.


## Scripts

### Backup script

The following script is the `kamaji-etcd-backup.sh`.

<details markdown="1"><summary><b>kamaji-etcd-backup.sh</b></summary>

[embedmd]:#(kamaji-etcd-backup.sh)
```sh
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
```

</details>

### Restore script

The following script is the `kamaji-etcd-restore.sh`.

<details markdown="1"><summary><b>kamaji-etcd-restore.sh</b></summary>

[embedmd]:#(kamaji-etcd-restore.sh)
```sh
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
```

</details>
