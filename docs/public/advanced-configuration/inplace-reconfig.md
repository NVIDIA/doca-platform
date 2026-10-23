---
title: "In-Place Reconfiguration on DPUFlavor Change"
---

[[_TOC_]]

By default, changing an already-provisioned DPU means deleting and recreating it: a full reprovisioning cycle with an OS reinstall. In-place reconfiguration lets an already-provisioned DPU pick up **config-only** [DPUFlavor](../developer-guides/api/dpuflavor.md) changes on the **same DPU object**, without deletion and without re-running the OS install.

A flavor edit by itself never triggers anything. Reconfiguration starts only when you explicitly annotate the DPUSet, and the DPUSet controller then walks every eligible DPU through a bounded rollout.

# What can be changed in place

All DPUFlavor spec fields are mutable **except** the install-time and quota fields below. Admission rejects any update that changes one of these fields, including a mixed update that also changes mutable fields; the whole update is rejected:

| Immutable field | Notes |
|---|---|
| `dpuResources`, `systemReservedResources` | Quota / resource sizing |
| `bfcfgParameters` | Boot configuration, applied at install time |
| `hostNetworkInterfaceConfigs` | Host networking layout |
| `scalableFunctions` | SR-IOV scalable functions |
| `configFiles` of type `cloud-init` (the default) | Written during OS install |
| `dpuMode` | Deprecated |

Everything else — `nvconfig`, `grub` kernel parameters, `sysctl`, `systemdServices`, `packages`, `containerdConfig`, `ovs`, `serviceReadiness`, agent-applied `configFiles`, and `ewNicConfigurations` — is mutable and can be applied in place. Create-time validators (for example the `configFiles` XOR rule and `nvconfig` key uniqueness) still run on updates.

# What happens during a round

**1.** You edit the DPUFlavor (or, in template mode, the DPUFlavorTemplate or a `DPUDevice`'s `spec.values`). Nothing happens yet; the DPUs stay `Ready`.

**2.** You annotate the DPUSet with `provisioning.dpu.nvidia.com/inplace-reconfig=true`. This starts a **round**.

**3.** Member DPUs go through `Ready → Node Effect → DPU Config → (Host Network Configuration on host-trusted) → DPU Cluster Config → Service Readiness → Node Effect Removal → Ready`, at most `maxUnavailableDPUNodes` at a time. The install phases (`Initialize Interface`, `Config FW Parameters`, `Prepare BFB`, `OS Installing`) are skipped.

**4.** The DPU agent applies the new values on the DPU itself. The DPU object is never deleted; `metadata.uid` is unchanged.

**5.** When every member has completed the round, the DPUSet controller removes the annotation and sets `status.dpuReconfig.active=false`.

Some changes require a reset to take effect, and the round then includes a `Rebooting` leg on the same DPU object:

* An `nvconfig` change that requires a reset goes through `DPU Config → Rebooting → DPU Config` (host power cycle or system-level reset, depending on the platform).
* A `grub` kernel command-line change triggers a DPU OS warm reboot.

DPUs still running an old DPU agent (one that does not report `status.agentStatus.version`, for example one provisioned by an older DPF release) are skipped by in-place rounds and stay `Ready`. Reprovision them once to opt in.

## Triggering does not require every DPU to be Ready

You can annotate at any time. What each DPU does depends on its state when the round starts:

* A DPU that is `Ready` when the round starts runs the full in-place path above. This is the common case.
* A DPU still in its **first install** is never jumped. Neither case below re-enters `DPU Config` for the round, because the ongoing install already fetches the latest flavor:
  * A DPU already past `DPU Config` (`Host Network Configuration`, `DPU Cluster Config`, `Service Readiness`, `Node Effect Removal` or `Node Effect`) joins the round without consuming a `maxUnavailableDPUNodes` slot, and completes it by reaching `Ready`.
  * A DPU still in `DPU Config` or an earlier phase is stamped `completedRound` on its first `Ready`.
* A DPU created after the round started is not a member of that round at all; it is stamped `completedRound` on its first `Ready`.

# Usage

## Static DPUFlavor

```bash
# 1. Edit config-only fields of the flavor
kubectl -n dpf-operator-system edit dpuflavor <flavor-name>

# 2. (Optional) confirm all DPUs stay Ready — the edit alone changes nothing

# 3. Start an in-place round
kubectl -n dpf-operator-system annotate dpuset <dpuset-name> \
  provisioning.dpu.nvidia.com/inplace-reconfig=true
```

**4.** Watch the rollout:

**Using dpfctl (recommended):**

```bash
dpfctl describe all --show-resources=dpuset
```

**Using kubectl:**

```bash
kubectl -n dpf-operator-system get dpu -w
kubectl -n dpf-operator-system get dpuset <dpuset-name> -o jsonpath='{.status.dpuReconfig}'
```

## Template mode (DPUFlavorTemplate)

Edit the DPUFlavorTemplate body or a `DPUDevice`'s `spec.values`. The DPUSet controller patches the generated DPUFlavors in place; the DPUs stay `Ready`. Then annotate the DPUSet as above.

## DPUDeployment

DPUDeployment creates and manages the DPUSets for you. To trigger a round, annotate the generated DPUSet objects directly. They are named `<deployment-name>-<nameSuffix>`:

```bash
kubectl -n dpf-operator-system get dpuset
kubectl -n dpf-operator-system annotate dpuset <deployment-name>-<nameSuffix> \
  provisioning.dpu.nvidia.com/inplace-reconfig=true
```

Annotate every generated DPUSet to reconfigure all DPUs of the deployment, or only some of them to roll out a change group by group.

## Starting another round

Edit the flavor again and re-annotate. Each round increments `status.dpuReconfig.round`. Annotating without any flavor change is allowed and simply re-applies the current flavor.

# Observing progress

* Per DPUSet: `status.dpuReconfig` carries `round`, `active` and `startedAt`.
* Per DPU: `status.dpuReconfig` carries `inProgress`, `round` and `completedRound`. A DPU that finished the round has `completedRound == round`.
* Rollout concurrency is controlled by `spec.provisioningController.maxUnavailableDPUNodes` in the [DPFOperatorConfig](../developer-guides/api/dpfoperatorconfig.md) (integer, minimum 1, default 50).

# Aborting a round

Remove the annotation while a round is running:

```bash
kubectl -n dpf-operator-system annotate dpuset <dpuset-name> provisioning.dpu.nvidia.com/inplace-reconfig-
```

DPUs already in flight finish their reconfiguration and return to `Ready`; DPUs that have not started yet do not start.

# Recovering from a bad value

If a value fails to apply (for example an invalid `nvconfig` parameter), the affected DPUs stay in `DPU Config` and the annotation is **retained**, so the round is visibly unfinished. Fix the value at its source (for example with `kubectl edit`): the DPUFlavor for a static flavor, or the DPUFlavorTemplate or the `DPUDevice`'s `spec.values` in template mode. Do not edit a generated DPUFlavor directly, because the DPUSet controller rewrites it from the template. The agent re-fetches the latest flavor and continues to `Ready` on its own. Do **not** re-annotate, and do not delete the DPU.

# Notes and limitations

* Only the exact annotation value `"true"` starts a round. Any other value is ignored and left in place.
* Re-annotating while a round is already in flight is a no-op; wait until the controller removes the annotation before starting the next round.
* A flavor edit made during a round is picked up by DPUs that start later (or by the next round). It does not restart DPUs whose reconfiguration is already running — unless an operation is retrying after a failure, in which case the agent fetches the latest flavor and restarts its configuration run when the generation changes.
* Starting a round while a DPU is still in its first install does not skip the OS install. Its first `Ready` simply records the round as completed, because the first install already used the latest flavor.
* If a DPU is outdated for disruptive reasons (for example a BFB change), the disruptive path wins: the DPU is deleted and reprovisioned instead of reconfigured in place.
* `ovs.rawConfigScript` must be idempotent. DPF applies the script but does not tear down bridges, ports or flows left over from a previous script.
* Changing only the payload of an agent-referenced ConfigMap takes effect only after the next round.
* DPF verifies that values are applied, not that they are semantically correct for your workload.
