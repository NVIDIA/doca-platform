---
title: "dpfctl dump"
---

[[_TOC_]]

The `dpfctl dump` command collects diagnostic dumps from DPF environments. Use it when an investigation requires
artifacts from infrastructure components that are not covered by Kubernetes resource logs alone.

## Usage

```sh
dpfctl dump [command] [flags]
```

Available Commands:

| Command | Description                                        |
|---------|----------------------------------------------------|
| bmc     | Create and download BlueField BMC diagnostic dumps |

## BMC Diagnostic Dumps

The `dpfctl dump bmc` command collects BlueField BMC diagnostic dumps for BMC targets resolved from `DPUDevice`
objects.

For each BMC target resolved from the selected `DPUDevice` objects, the command:

1. Reads the BMC endpoint from `status.bmcIp` / `status.bmcPort`, falling back to `spec.bmcIp` / `spec.bmcPort`.
2. Resolves the BMC password from the `status.bmcCredentialSecretName` or `spec.bmcCredentialSecretName` Secret.
3. Falls back to the shared `bmc-shared-password` Secret when no per-device Secret is configured.
4. Reads the unauthenticated Redfish root service to determine the BlueField generation.
5. Selects the Redfish username and the `Dump` log service paths that the generation uses.
6. Creates a Redfish diagnostic dump task per dump service and waits for each to complete.
7. Takes the newest entry in each dump service and downloads its archive along with the supporting Redfish JSON
   artifacts.

The generation is detected at runtime, so the same invocation works against BlueField-3 and BlueField-4. The username
and the Redfish resource paths are then taken from a per-generation table in the source, because the two generations
name their resources differently: BlueField-3 authenticates as `root` under `Managers/Bluefield_BMC`, BlueField-4 as
`admin` under `Managers/BlueField_BMC_0`.

The Manager dump is collected on every BMC. The System dump is collected only on BlueField-4, which is the generation
that exposes `DiagnosticType=CPUDiagnosticsData`; on BlueField-3 it is skipped and the reason is recorded in
`metadata.txt`.

If a future BMC renames a Redfish resource, the collection fails with the expected path in the error message and in
`metadata.txt`, and the per-generation table needs a new entry.

## Quick Start

Collect BMC dumps for all discovered `DPUDevice` objects:

```shell
dpfctl dump bmc --output-dir /tmp/bmc-dumps
```

Collect BMC dumps for specific `DPUDevice` objects:

```shell
dpfctl dump bmc --devices mt2610604vmk,mt2610604vnc --output-dir /tmp/bmc-dumps
```

Clear existing BMC dump entries before creating a new dump:

```shell
dpfctl dump bmc --clear-existing --output-dir /tmp/bmc-dumps
```

Collect from lab BMC endpoints that use self-signed certificates:

```shell
dpfctl dump bmc --insecure-skip-tls-verify --output-dir /tmp/bmc-dumps
```

## Requirements

Before running `dpfctl dump bmc`, verify the following:

* `dpfctl` can access the host cluster where `DPUDevice` objects are stored.
* Each target `DPUDevice` has a BMC IP address in `status.bmcIp` or `spec.bmcIp`.
* The machine or pod running `dpfctl` can reach each BMC endpoint over HTTPS.
* Each BMC endpoint presents a TLS certificate trusted by the machine or pod running `dpfctl`.
* The BMC credential Secret exists in the same namespace as the `DPUDevice`.
* The credential Secret contains a `password` key.

By default, `dpfctl dump bmc` reads `DPUDevice` objects and BMC credential Secrets from the `dpf-operator-system`
namespace. Use `--namespace` if your environment stores them elsewhere:

```shell
dpfctl dump bmc --namespace dpf-operator-system --output-dir /tmp/bmc-dumps
```

The Redfish username is detected from the BMC generation: `root` on BlueField-3 and `admin` on BlueField-4. Use
`--username` only when the BMC uses a different user:

```shell
dpfctl dump bmc --username admin --output-dir /tmp/bmc-dumps
```

## Output

If `--output-dir` is not provided, a timestamped directory such as `bmcdump-20260713-120000` is created in the
current working directory.

Each BMC target gets its own subdirectory. The subdirectory name includes the BMC IP address, the BMC port when it is
not the default `443`, and the credential Secret name.

Example output layout:

Each collected dump gets its own directory: `manager` for the Manager dump and `system` for the System dump.

```text
/tmp/bmc-dumps/
└── 10.0.110.122-bmc-shared-password/
    ├── metadata.txt
    ├── manager/
    │   ├── create-dump-task.json
    │   ├── task-final.json
    │   ├── dump-entries.json
    │   └── log_dump.tar.zst
    └── system/
        ├── create-dump-task.json
        ├── task-final.json
        ├── dump-entries.json
        └── log_dump.tar.zst
```

The `system` directory is absent on BlueField-3, which has no CPU diagnostics dump. A directory is created only once
a dump writes into it, so a unit that fails before producing anything leaves nothing behind; the reason is recorded in
`metadata.txt`.

Both dumps are zstd-compressed tarballs:

```shell
tar --use-compress-program=unzstd -xf manager/log_dump.tar.zst
```

If extraction fails, run `file` on the archive to see what it actually is;
the contents are still intact, only the name is wrong.

The files contain:

| File                    | Description                                                                            |
|-------------------------|----------------------------------------------------------------------------------------|
| `metadata.txt`          | BMC endpoint, related `DPUDevice` names, credential Secret, and what the collector discovered, chose and skipped |
| `create-dump-task.json` | Redfish response from dump task creation                                               |
| `task-final.json`       | Final Redfish task status                                                              |
| `dump-entries.json`     | Redfish dump entry list used to find the downloaded archive                            |
| `log_dump.tar.zst`      | Downloaded dump archive (zstd)                                                         |

## Running from the Operator Pod

You can run `dpfctl dump bmc` from the DPF Operator pod when you want to use the version bundled with the running
operator:

```shell
kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- \
  /dpfctl dump bmc --output-dir /tmp/bmc-dumps
```

When running this way, the output directory is inside the container filesystem. Copy the directory out of the pod if
you need to attach the dump to an issue or support case.

For example, copy the output directory from the operator pod to your local machine:

```shell
kubectl -n dpf-operator-system cp \
  deploy/dpf-operator-controller-manager:/tmp/bmc-dumps ./bmc-dumps
```

## Flags

| Flag                         | Default               | Description                                                           |
|------------------------------|-----------------------|-----------------------------------------------------------------------|
| `--namespace`                | `dpf-operator-system` | Namespace containing `DPUDevice` objects and BMC credential Secrets   |
| `--devices`                  |                       | Comma-separated `DPUDevice` names; defaults to all discovered devices |
| `--username`                 | auto-detected         | BlueField BMC Redfish username; defaults to `root` on BF3 and `admin` on BF4 |
| `--timeout`                  | `30m`                 | Timeout for each BMC diagnostic dump task                             |
| `--request-timeout`          | `30s`                 | Timeout for each Redfish HTTP request                                 |
| `--output-dir`               | `bmcdump-<timestamp>` | Local directory for downloaded dumps                                  |
| `--clear-existing`           | `false`               | Clear existing BMC dump entries before creating a new dump            |
| `--insecure-skip-tls-verify` | `false`               | Skip BMC TLS certificate verification                                 |

## Troubleshooting

If collection fails with `401 Unauthorized`, verify that the credential Secret referenced by the `DPUDevice` contains
the current BMC password in the `password` key.

If collection fails with a TLS certificate error in a lab environment that uses self-signed BMC certificates, use
`--insecure-skip-tls-verify`. Do not use this flag on untrusted networks.

If no BMC targets are found, verify that the `DPUDevice` objects have `status.bmcIp` or `spec.bmcIp` populated:

```shell
kubectl -n dpf-operator-system get dpudevices \
  -o custom-columns=NAME:.metadata.name,BMC_IP:.status.bmcIp,BMC_SECRET:.status.bmcCredentialSecretName
```

If the command times out, verify that the host running `dpfctl` can reach the BMC network and that the BMC Redfish API
is responsive.
