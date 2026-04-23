---
title: "dpfctl sosreport"
---

[TOC]

The `dpfctl sosreport` command collects SOS reports from host and DPU cluster nodes using Kubernetes Jobs. It creates
privileged Jobs on target nodes that run the NVIDIA sosreport tool to collect system diagnostics. Reports can be
downloaded to your local machine or written to an NFS share.

## Usage

```sh
dpfctl sosreport [command] [flags]
```

Available Commands:

| Command  | Description                                          |
|----------|------------------------------------------------------|
| start    | Start SOS report collection Jobs on cluster nodes    |
| status   | Show status of SOS report Jobs                       |
| download | Download completed SOS reports to local disk         |
| collect  | Start, wait, and download SOS reports in one step    |
| cleanup  | Remove all SOS report resources (Jobs, pods, secrets)|

## Quick Start

The simplest way to collect SOS reports is to use the `collect` command, which orchestrates the full workflow
(start → wait → download → cleanup) in a single step:

```shell
dpfctl sosreport collect --output-dir /tmp/sos-reports
```

Alternatively, you can run each step manually:

```shell
# Start collection Jobs
dpfctl sosreport start

# Check progress (use -w to watch continuously)
dpfctl sosreport status -w

# Download completed reports
dpfctl sosreport download --output-dir /tmp/sos-reports

# Clean up resources
dpfctl sosreport cleanup
```

## Targeting Host or DPU Nodes

By default, SOS reports are collected from **all** nodes on both host and DPU clusters. Use the `--target` flag to
limit collection to a specific environment:

```shell
# Collect from host cluster nodes only
dpfctl sosreport collect --target host --output-dir /tmp/sos-reports

# Collect from DPU cluster nodes only
dpfctl sosreport collect --target dpu --output-dir /tmp/sos-reports
```

To collect from specific nodes (comma-separated):

```shell
dpfctl sosreport start --nodes worker-1,worker-2
```

To target nodes by label selector:

```shell
dpfctl sosreport start --node-selector node-role.kubernetes.io/worker=
```

Both `--nodes` and `--node-selector` can be combined with `--target` to narrow the scope further.

If you have multiple DPU clusters, use `--dpu-cluster` to target a specific one:

```shell
dpfctl sosreport start --target dpu --dpu-cluster dpu-cplane-tenant1
```

## NFS Output

Instead of downloading reports locally, you can write them directly to an NFS share:

```shell
dpfctl sosreport start --nfs-server 10.0.0.1 --nfs-path /exports/sos
```

A timestamped subdirectory (e.g., `sosreport-20260416-120000`) is created automatically. To write directly to the
specified path without a subdirectory:

```shell
dpfctl sosreport start --nfs-server 10.0.0.1 --nfs-path /exports/sos --nfs-no-subdir
```

## Archive

Use the `--archive` flag to create a single `.tar.gz` file of all downloaded reports, suitable for attaching to a
support ticket:

```shell
dpfctl sosreport collect --archive --output-dir /tmp/sos-reports
```

This creates `<output-dir>.tar.gz` (e.g., `/tmp/sos-reports.tar.gz`) alongside the output directory.

The `--archive` flag is also available on the `download` subcommand:

```shell
dpfctl sosreport download --archive --output-dir /tmp/sos-reports
```

## Case ID

A case ID is assigned to each collection run and used to label all created resources. By default, a timestamped ID
(e.g., `dpf-20260416-120000`) is generated. You can specify your own:

```shell
dpfctl sosreport collect --case-id CASE-12345 --output-dir /tmp/sos-reports
```

## Watching Status

Use `dpfctl sosreport status -w` to continuously monitor job progress. New lines are appended as job statuses change,
similar to `kubectl get -w`:

```shell
dpfctl sosreport status -w
```

The default refresh interval is 5 seconds. Use `-i` to change it:

```shell
dpfctl sosreport status -w -i 10
```

Both `status` and `cleanup` accept `--case-id` to filter by a specific collection run:

```shell
dpfctl sosreport status --case-id CASE-12345
dpfctl sosreport cleanup --case-id CASE-12345
```

## Global Flags

These flags apply to all `sosreport` subcommands:

| Flag             | Default                              | Description                                    |
|------------------|--------------------------------------|------------------------------------------------|
| `--target`         | `all`                                | Target environment: `host`, `dpu`, or `all`    |
| `--dpu-cluster`    |                                      | Specific DPUCluster name (defaults to all)     |
| `--nodes`          |                                      | Comma-separated node names (defaults to all)   |
| `--node-selector`  |                                      | Label selector to filter nodes                 |
| `--namespace`    | `default`                            | Namespace for Jobs and Secrets                 |
| `--image`        | `ghcr.io/nvidia/sosreport:latest`    | SOS report container image                     |
