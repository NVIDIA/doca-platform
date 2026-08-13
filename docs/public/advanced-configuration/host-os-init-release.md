---
title: "Host OS Init Release"
---

[[_TOC_]]

By default a DPU and its host boot independently, so the host can start workloads before the DPU's networking is ready
and leave early traffic without a data path.

Host OS init release closes that gap. The DPU firmware holds the host at the start of its OS initialization, and the DPU
agent clears the hold only after a chosen DPU readiness gate reports `True`.

This feature requires Zero Trust. Elsewhere a DPUFlavor requesting the hold is rejected before provisioning starts; see
[Host Trusted Deployments](#host-trusted-deployments).

# Configure The DPUFlavor

Set the nvconfig parameter that enables the firmware hold, and optionally choose the gate that releases it. Since
`DPUFlavor.spec` is immutable, enabling this on existing DPUs needs a new flavor and reprovisioning, not an edit.

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: dpu-flavor-host-os-init
  namespace: dpf-operator-system
spec:
  nvconfig:
    - device: "*"
      parameters:
        - DELAY_HOST_OS_INIT=0x3
  hostOSInit:
    releaseAfter:
      dpuServiceCriticalPodsReady: {}
```

`releaseAfter` is a one-of selector: exactly one of `dpuServiceCriticalPodsReady` or `operationalReady` may be set, and
each is an empty object that only marks the choice. The `hostOSInit` block itself is optional.

# Choosing A Gate

Both gates read a condition from `DPU.status.operationalConditions`, and differ only in how much readiness they demand.
`dpuServiceCriticalPodsReady` is the default, applied when `hostOSInit` or `releaseAfter` is omitted, and waits for
exactly the services the host's traffic depends on. Use `operationalReady` only when the host must not boot until the DPU
is fully healthy: it aggregates *all* operational conditions, including node problems, service interfaces, service chains
and **non-critical** pods. For what each condition covers, see
[DPU Operational Readiness](../operational-readiness/observability/guides/operational-readiness.md).

> [!WARNING]
> Neither gate has a timeout. If the gate never becomes `True` the agent waits indefinitely and the host never begins its
> OS initialization, so a persistent DPU-side problem presents as a host that will not boot. Choose the narrowest gate
> that satisfies your ordering requirement.

# Provisioning Flow

The DPU enters the `Host OS Init Release` phase after `DPU Cluster Config` and stays there until the agent has released
the host, then continues to `Node Effect Removal`.

Every DPU enters this phase regardless of deployment mode. A DPU whose flavor does not request the hold does not wait
there: the agent skips immediately with reason `ReleaseNotRequired`.

# Host Trusted Deployments

Outside Zero Trust the DPU agent reaches the kube-apiserver through the host agent's proxy, which cannot run while the
host is held, so the hold could never be released. A DPU whose DPUFlavor requests it therefore stops in the `Pending`
phase, before any BFB is installed or any nvconfig is written, with `status.conditions[Pending]` set to `False` and
reason `DPUFlavorRequiresZeroTrustMode`:

```bash
kubectl -n dpf-operator-system get dpu <dpu-name> -o jsonpath='{.status.conditions[?(@.type=="Pending")]}'
```

The `Host OS Init Release` phase is therefore inert in Host Trusted: with the hold rejected up front, a Host Trusted
DPU can only reach the phase with a flavor that does not request it, and always skips.

# Verification

The agent records a terminal outcome under `status.agentStatus.hostOSInit`, where exactly one of `skipped` or
`succeeded` is set. A released DPU reports `succeeded` and echoes the configured gate, so this also confirms which gate
was in effect. A DPU that did not need the hold reports `skipped` with reason `ReleaseNotRequired`.

```bash
kubectl -n dpf-operator-system get dpu <dpu-name> -o jsonpath='{.status.agentStatus.hostOSInit}'
```

# Troubleshooting

While the DPU is in this phase its top-level `status.conditions[HostOSInitRelease]` stays `False` with reason
`AwaitingAgent`, regardless of the cause:

```bash
kubectl -n dpf-operator-system get dpu <dpu-name> -o jsonpath='{.status.conditions[?(@.type=="HostOSInitRelease")]}'
```

That condition only confirms the DPU is waiting on the agent. The specific reason is on the `ReleaseHostOSInit`
condition in `status.agentStatus.conditions`, which the agent rewrites on every retry:

```bash
kubectl -n dpf-operator-system get dpu <dpu-name> -o jsonpath='{.status.agentStatus.conditions[?(@.type=="ReleaseHostOSInit")]}'
```

Until the release completes the condition is `False` with reason `FailedToExecute`, including while it is merely waiting
on the gate. The message identifies the cause:

* `waiting for DPUServiceCriticalPodsReady` or `waiting for OperationalReady` means the DPU is gated rather than broken.
  Investigate the gating condition on `status.operationalConditions`.
* `DELAY_HOST_OS_INIT did not activate on PCI <pci> after reboot` means the host was warm-rebooted instead of
  power-cycled, so the parameter is staged but not active.
* `unexpected HOST_OS_INIT_CTRL state on PCI <pci>: clear=... delay=... mode=...` most often means `mode` is not `3`,
  so `DELAY_HOST_OS_INIT` is not active on this boot and the host was never held. A power cycle is needed to apply it.
* A message beginning `mlxreg -d <pci> --reg_name HOST_OS_INIT_CTRL --get:` means the register could not be read. Check
  that MFT is installed on the DPU and that the firmware matches [Platform Support](../platform-support.md).
* `mlxreg command failed` means the register could not be written. The message carries the `mlxreg` stderr.
* `host OS init remains held on PCI <pci> after mlxreg release` means the write succeeded but the register read back as
  still held, which indicates a firmware-level problem.

For the full `hostOSInit` field reference see the
[DPUFlavor API documentation](../developer-guides/api/dpuflavor.md#hostosinit).
