---
title: "Service Readiness"
---

[[_TOC_]]

By default a DPU and its host boot independently, so the host can start workloads before the DPU's networking is ready
and leave early traffic without a data path.

The `Service Readiness` phase closes that gap. The provisioning controller can hold the DPU in the phase until a chosen
readiness gate reports `True`, and it can additionally hold the host at the start of its OS initialization until the
same gate is satisfied.

The host-side hold is **best effort** and depends on host BIOS configuration that DPF cannot see or report. Read
[Holding The Host At OS Initialization](#holding-the-host-at-os-initialization) before relying on it for boot ordering.
It also requires Zero Trust; elsewhere a DPUFlavor requesting it is rejected before provisioning starts, as described in
[Host Trusted Deployments](#host-trusted-deployments).

# Provisioning Flow

The DPU enters the `Service Readiness` phase after `DPU Cluster Config` and continues to `Node Effect Removal` once the
phase is satisfied.

Every DPU enters this phase regardless of deployment mode. Two independent things are checked there, and a DPU can be
subject to either, both, or neither:

1. **The phase gate**, enforced by the provisioning controller and active only when `spec.serviceReadiness.gate` is set.
   See [Choosing A Gate](#choosing-a-gate).
2. **The host OS init release**, performed by the DPU agent and required only when `spec.nvconfig` requests the hold
   with `DELAY_HOST_OS_INIT=0x3`. A DPU whose flavor does not request it does not wait: the agent skips immediately with
   reason `ReleaseNotRequired`. See
   [Holding The Host At OS Initialization](#holding-the-host-at-os-initialization).

With neither configured the phase is a pass-through.

# Configure The DPUFlavor

To hold the host, set the nvconfig parameter that enables the firmware hold. Optionally set the gate that both releases
the hold and blocks the phase. Since `DPUFlavor.spec` is immutable, enabling this on existing DPUs needs a new flavor
and reprovisioning, not an edit.

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: dpu-flavor-service-readiness
  namespace: dpf-operator-system
spec:
  nvconfig:
    - device: "*"
      parameters:
        - DELAY_HOST_OS_INIT=0x3
  serviceReadiness:
    gate: DPUServiceCriticalPodsReady
```

`gate` is an enum accepting either `DPUServiceCriticalPodsReady` or `OperationalReady`. Both the `serviceReadiness`
block and the `gate` field inside it are optional.

This block replaces `spec.hostOSInit.releaseAfter` from earlier pre-release builds. `spec.hostOSInit` no longer exists:
`kubectl apply` rejects a flavor that still sets it, and clients that do not validate strictly drop it silently. Move
the choice to `gate`, where `dpuServiceCriticalPodsReady` becomes `DPUServiceCriticalPodsReady` and `operationalReady`
becomes `OperationalReady`. Note that omitting the field no longer implies a gate.

# Choosing A Gate

Each gate names an entry of `DPU.status.operationalConditions`, and they differ only in how much readiness they demand.
`DPUServiceCriticalPodsReady` waits for exactly the services the host's traffic depends on. Use `OperationalReady` only
when the DPU must be fully healthy: it aggregates *all* operational conditions, including node problems, service
interfaces, service chains and **non-critical** pods. For what each condition covers, see
[DPU Operational Readiness](../operational-readiness/observability/guides/operational-readiness.md).

Leaving `gate` unset is not the same as picking a gate, and the difference is deliberate:

* **The phase does not block.** With no gate configured the DPU passes through `Service Readiness` without waiting on
  any operational condition.
* **A host hold is still released on `DPUServiceCriticalPodsReady`.** The agent falls back to that gate, because a hold
  with no gate at all would be released immediately and would defeat the purpose of holding the host.

So setting `gate` opts into the phase wait *and* points the host release at the same condition, while leaving it unset
preserves the previous behaviour.

> [!NOTE]
> When `gate` is set, the DPU cluster controller that computes `operationalConditions` does not watch the
> `ServiceReadiness` condition. A gate that becomes `True` is normally observed promptly through other events, but in
> the worst case it can take until the next resync, up to 10 minutes by default, for the phase to advance.

> [!WARNING]
> A gate has no timeout, so if it never becomes `True` the DPU stays in `Service Readiness` indefinitely. The host does
> **not** wait with it, so a persistent DPU-side problem presents as a DPU stuck in this phase alongside a host that
> booted anyway. Choose the narrowest gate that satisfies your ordering requirement.

# Holding The Host At OS Initialization

Everything in this section applies only when the flavor requests the firmware hold with `DELAY_HOST_OS_INIT=0x3`. The
phase gate is enforced by the provisioning controller and is unaffected by any of it, so a deployment that uses only the
gate can skip ahead to [Verification](#verification).

## The Host BIOS Must Allow The BlueField Option ROM To Run

The hold is not performed by DPF, and not even by the DPU on its own. It is performed by the DPU's UEFI expansion ROM
(Option ROM) executing inside the host's BIOS during boot. If the host BIOS does not run that Option ROM, the hold
cannot happen at any setting of `DELAY_HOST_OS_INIT`.

Some platforms ship with a BIOS list that disables Option ROMs for specific PCI devices. It typically appears under
`Platform Configuration` as `Option ROM Disabled Devices`, holding space-separated hex values that each encode
`(VendorId << 16) | DeviceId`. An entry matching the BlueField DPU disables exactly the Option ROM this feature depends
on. For reference, `0x15B3` is the NVIDIA vendor ID and `0xA2DF` is the BlueField-4 N/S NIC (ECPF) device ID, so
`0x15B3A2DF` in such a list refers to the BlueField-4 itself.

Verify the list through the platform's BIOS setup or vendor tooling before relying on this feature. Changing it is a
platform and BIOS-owner decision, and the setting takes effect only after a reboot.

> [!WARNING]
> When the BlueField Option ROM is disabled the host is never held and **DPF still reports success**, with no condition,
> event or error to indicate it. See
> [The Host Booted Immediately And DPF Reported Success](#the-host-booted-immediately-and-dpf-reported-success).

## The Hold Stops After About 20 Minutes

The expansion ROM does not wait indefinitely. While it holds the host it prints a banner on the host console of the form:

```text
EXPROM-UEFI: holding BIOS, waiting for DPU... 110 s elapsed (timeout 1200 s)
```

When that 1200-second timeout expires the expansion ROM stops waiting and the host continues booting without the DPU
being ready. The hold is therefore a best-effort ordering mechanism with a ceiling of roughly 20 minutes, not a
guarantee that the host will never boot ahead of the DPU.

## Host Trusted Deployments

Outside Zero Trust the DPU agent reaches the kube-apiserver through the host agent's proxy, which cannot run while the
host is held, so the hold could never be released. A DPU whose DPUFlavor requests it therefore stops in the `Pending`
phase, before any BFB is installed or any nvconfig is written, with `status.conditions[Pending]` set to `False` and
reason `DPUFlavorRequiresZeroTrustMode`:

```bash
kubectl -n dpf-operator-system get dpu <dpu-name> -o jsonpath='{.status.conditions[?(@.type=="Pending")]}'
```

Only the host hold is restricted this way; the phase gate works in either deployment mode.

# Verification

The agent records a terminal outcome for the host release under `status.agentStatus.hostOSInit`, where exactly one of
`skipped` or `succeeded` is set. A released DPU reports `succeeded` and echoes the gate that was in effect. A DPU that
did not need the hold reports `skipped` with reason `ReleaseNotRequired`.

```bash
kubectl -n dpf-operator-system get dpu <dpu-name> -o jsonpath='{.status.agentStatus.hostOSInit}'
```

That status field keeps the `hostOSInit` name deliberately: it reports the host hold alone, which is only one of the two
things the phase checks, whereas `spec.serviceReadiness` configures the phase as a whole.

> [!IMPORTANT]
> `succeeded` means the agent's release path completed. It does **not** prove the host was ever held. The agent reports
> `succeeded` whenever the hold register already reads released, and a host that was never held is indistinguishable
> from one that was held and then released. The only positive confirmation that ordering held is the `EXPROM-UEFI`
> banner on the host console during boot.

# Troubleshooting

While the DPU is in this phase its top-level `status.conditions[ServiceReadiness]` stays `False`:

```bash
kubectl -n dpf-operator-system get dpu <dpu-name> -o jsonpath='{.status.conditions[?(@.type=="ServiceReadiness")]}'
```

The reason narrows down which of the two checks is outstanding:

* `AwaitingServices` means the configured gate is not `True` yet. The message names the condition; investigate it on
  `status.operationalConditions`.
* `AwaitingAgent` means the gate is satisfied, or none was configured, and the phase is waiting for the DPU agent to
  report a terminal host OS init release.
* `FlavorNotFound` or `GetDPUFlavorError` means the DPU's `spec.dpuFlavor` could not be read, so the gate could not be
  evaluated at all. Check that the referenced DPUFlavor exists in the DPU's namespace.

When the reason is `AwaitingAgent`, the specific cause is on the `ReleaseHostOSInit` condition in
`status.agentStatus.conditions`, which the agent rewrites on every retry:

```bash
kubectl -n dpf-operator-system get dpu <dpu-name> -o jsonpath='{.status.agentStatus.conditions[?(@.type=="ReleaseHostOSInit")]}'
```

Until the release completes that condition is `False` with reason `FailedToExecute`, including while it is merely
waiting on the gate. The message identifies the cause:

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

## The Host Booted Immediately And DPF Reported Success

This failure reports no error. The symptoms are `status.agentStatus.hostOSInit.succeeded` set, the phase advanced
normally, and the host booted straight through with no `EXPROM-UEFI` banner on its console. The hold register reads
`mode=3, delay=0`: the parameter is active, but the host never entered the delay.

Two causes produce that reading. Either the host BIOS disabled the BlueField Option ROM so the expansion ROM never ran,
described in [the Option ROM requirement](#the-host-bios-must-allow-the-bluefield-option-rom-to-run), or the hold timed
out before the gate opened. Watch the host serial console during boot to tell them apart: no banner at all points at
the Option ROM, while a banner reaching `1200 s` points at a gate that took too long.

For the full `serviceReadiness` field reference see the
[DPUFlavor API documentation](../developer-guides/api/dpuflavor.md#servicereadiness).
