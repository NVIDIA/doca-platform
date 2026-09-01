---
title: "DPF Compatibility Policy"
---

# DPF Compatibility Policy

This guide defines the compatibility and upgrade policy for the DOCA Platform Framework (DPF). It
explains which combinations of DPF Operator, BlueField Bootstream (BFB), and DOCA Services are
supported together, how to move between releases safely, and what DPF guarantees across an upgrade.

DPF ships four General Availability (GA) releases per year and aligns with the DOCA release cadence,
including the annual Long Term Support (LTS) train. This guide is version independent: it refers to
releases by their position in the annual cycle (January, April, July, October) and uses relative
notation such as `GAn-1` and `LTS-1`, rather than naming version numbers, so the policy stays valid
across cycles. For the version numbers that apply to the release you run, see
[Platform Support](../../platform-support.md).

[[_TOC_]]

## Compatibility commitments

DPF upgrades on two independent tracks:

* the **DPF Operator** on the host cluster
* the coupled **BFB + DOCA Service bundle** on each DPU

Most of this policy follows from that split. The commitments below apply within the boundaries set
out in [Scope](#scope).

**What DPF guarantees:**

* **Progressive upgrades.** You can upgrade the DPF Operator without taking DPUs offline. DPUs are
  reprovisioned independently, at their own pace, because the Operator supports DPU workers one
  release behind it.
* **A predictable reprovisioning cadence.** BFB LTS users reprovision roughly once a year, BFB GA
  users roughly once a quarter.
* **Manifest compatibility.** Existing DPF manifests stay compatible across releases within the same
  major API version. Fields are removed only in the first release of a year (usually the January
  release), after being deprecated no later than half a year or two releases before (usually the
  preceding July release).
* **A tested service and BFB pairing.** Upgrading a BFB and its DOCA Services together to the same
  train is the fully tested combination, including staying on the HBN (Host-Based Networking) LTS
  branch with the matching BFB LTS.
* **Per-DPU rollback.** Rolling back to the previous BFB and service bundle is supported per DPU.

**What DPF does not guarantee:**

* Running a DOCA Service on a BFB version other than the one it was released with.
* Any BFB and service combination across the annual cycle boundary (an old BFB with new-cycle
  services).
* DPF Operator downgrade.
* Skipping DPF releases during upgrade.
* Support investigation while a DPU sits in the rollout-only state. You will be asked to finish
  moving it to a supported combination first.
* Support on an older patch within a BFB LTS train. You must be on the latest patch of the LTS train.

## Scope

* This policy covers DPF running in DPU mode on BlueField-3 and BlueField-4 devices. BlueField-2 and
  NIC mode are out of scope.
* Where no policy is defined for a given combination, that combination is untested and carries no
  compatibility guarantee.
* DPF has no LTS release of its own. Throughout this guide, LTS refers exclusively to the BFB and
  DOCA Service train, not to the DPF Operator.
* BFB and DOCA Services are treated as a coupled unit. They are always deployed and upgraded together
  through a `DPUDeployment`.

## Terminology

| Term              | Description                                                                            |
| :---------------- | :------------------------------------------------------------------------------------ |
| BFB               | BlueField Bootstream, the firmware and OS image installed on a BlueField-3 DPU         |
| BlueFieldSoftware | The BlueField-4 equivalent of a BFB: the OS ISO and firmware bundle installed on a DPU |
| DOCA Service      | A service (such as HBN) deployed onto DPUs by DPF, versioned together with its BFB     |
| DPF Operator      | The DPF control plane, running on the host cluster and managing all DPUs               |
| DPU worker        | A provisioned DPU node running a BFB and its DOCA Services                             |
| GA                | General Availability, a quarterly DPF release (January, April, July, October)          |
| GAn / GAn-1       | The current GA release (GAn) and the immediately preceding GA release (GAn-1)          |
| LTS               | Long Term Support, the annual (October) DOCA and BFB train                             |
| LTS-1             | The LTS train immediately preceding the current one                                    |
| n-1               | The immediately preceding release in the same release train                            |
| Reprovision       | Re-flashing a DPU with a BFB and re-deploying its DOCA Services                        |

Throughout this guide, a component "at release N" means the version that shipped with DPF release N.
For a DPU worker, that is its BFB train together with the DOCA Services coupled to it. BFB is used as
the general term for the DPU image, and everything said about a BFB applies equally to the
`BlueFieldSoftware` used on BlueField-4.

> [!NOTE]
> NVIDIA maintains an LTS BFB for three years. That is the lifecycle of the BFB itself, not the DPF
> compatibility window. DPF supports a given BFB LTS train for about 15 months, from the October
> release that introduces it to the following October release. See
> [BFB and DOCA Service support window](#bfb-and-doca-service-support-window).

## Release cadence and versioning

DPF ships four GA releases per year, each aligned with the matching DOCA train. The October release
targets the new annual BFB LTS train and begins a fresh yearly cycle.

| DPF release | Aligns with  | BFB target               | Breaking changes                |
| :---------- | :----------- | :----------------------- | :------------------------------ |
| January     | DOCA GA (Q1) | New GA BFB train         | Allowed (the only such release) |
| April       | DOCA GA (Q2) | New GA BFB train         | Not allowed, additive only      |
| July        | DOCA GA (Q3) | New GA BFB train         | Not allowed, additive only      |
| October     | DOCA LTS     | New annual BFB LTS train | Not allowed, additive only      |

Every release except January is additive only. Breaking changes are confined to the January release,
for the reasons described in [API and behavior stability](#api-and-behavior-stability).

> [!NOTE]
> The months name the usual cadence, and an individual release may shift. Where this guide says
> "January", read "the first release of a year", and where it says "July", read "two releases
> before it". The position in the cycle is what the policy depends on, not the calendar month.

## How DPF is upgraded

DPF has two layers that upgrade independently, on separate schedules:

* The **DPF Operator** (the control plane) runs on the host cluster and manages every DPU.
* Each **DPU worker** runs a BFB together with its coupled DOCA Services.

Understanding these two tracks is the key to planning any upgrade.

| Aspect          | DPF Operator                             | BFB + DOCA Services                          |
| :-------------- | :--------------------------------------- | :------------------------------------------- |
| Where it runs   | Host cluster (control plane)             | DPU workers                                  |
| Upgraded by     | In-place Helm upgrade                    | Reprovisioning the DPU                       |
| Upgrade step    | One DPF release at a time (n-1)          | One BFB train at a time                      |
| Typical cadence | Each DPF release you adopt               | BFB LTS about yearly, BFB GA about quarterly |
| Version skew    | Runs up to one release ahead of its DPUs | May run one release behind the Operator      |
| Rollback        | Not supported                            | Supported per DPU                            |

Two rules make this model work:

* **The control plane may run ahead of its workers.** A DPF Operator at release N stays backward
  compatible with DPU workers at the previous release (N-1). You upgrade the Operator first, then
  reprovision DPUs at your own pace.
* **BFB and DOCA Services move together.** A DOCA Service version is always deployed alongside the BFB
  it was released with. Running a DOCA Service on a different BFB is untested.

## Upgrading the DPF Operator

### Upgrade one release at a time (n-1)

The DPF Operator can be upgraded only one release at a time. Skipping a DPF release is not supported.

Each release may add APIs, migrate fields, or complete a deprecation, and these steps must be applied
in order. Each release may also raise the Kubernetes minor version of the DPU cluster control plane.
Kubernetes control planes must be upgraded one minor version at a time under the
[Kubernetes version skew policy](https://kubernetes.io/releases/version-skew-policy/), which likewise
blocks skipping a DPF release.

You do not have to work this out yourself. Before an upgrade proceeds, the Operator validates the
version skew across every DPU cluster, comparing each ready DPU's kubelet against the cluster's API
server, and blocks the upgrade if the policy would be violated.

A full annual upgrade path therefore looks like this:

```text
DPF-Oct -> DPF-Jan -> DPF-Apr -> DPF-Jul -> DPF-Oct (next year)
```

The rule applies to the release, not to its patches. DPF versions are numbered
`year.month.patch`, and patch upgrades within the same release are always allowed, so you can take a
fix such as `YY.M.0` to `YY.M.1` at any time without it counting as a hop.

> [!IMPORTANT]
> The n-1 policy applies to the DPF Operator itself, independent of the BFB running on the DPUs. Even
> when DPUs stay on an older BFB LTS, the DPF Operator must still pass through each release in order.

### Mixed-version clusters

During a rolling upgrade the cluster is temporarily in a mixed state, which DPF explicitly supports:

* A **DPF Operator at release N** supports **DPU workers at release N-1**. The control plane can be one
  release ahead of the DPUs it manages.
* While a DPU still runs the previous BFB, it operates with the feature set of the previous DPF
  release. New features that depend on the newer BFB become available on that node only after it is
  reprovisioned. Each such feature documents its minimum requirement as a (DPF release, BFB train)
  pair.

### Upgrading across multiple releases

To advance more than one DPF release, choose one of two paths.

**Option 1: Sequential rolling upgrade (no full cluster downtime).**
Upgrade one release at a time while keeping most of the cluster operational. Any hop that also moves
the DPUs to a new BFB train requires a full DPU rollout and a reboot of every DPU node before the next
hop begins. Hops that stay within a supported BFB train, as an LTS user's hops do through a year, need
no rollout in between. Availability is preserved throughout, at the cost of one power cycle per
rollout.

```text
N -> N+1 (full DPU rollout + reboot) -> N+2 (full DPU rollout + reboot) -> ...
```

**Option 2: Offline upgrade (single power cycle).**
If full cluster downtime is acceptable, complete several DPF hops with a single DPU rollout at the
end.

1. Take DPUs out of service by removing `DPUDeployments`, or by narrowing `dpuNodeSelector` so that it
   matches no nodes, leaving no workloads scheduled on the DPUs.
2. Upgrade DPF one release at a time (N to N+1 to N+2 and so on), with no DPU provisioning between
   steps.
3. After reaching the final DPF release, perform a single DPU rollout to the matching BFB train and
   power cycle the DPUs.
4. Restore `DPUDeployments` to bring services back online.

```text
DPUs offline -> DPF N -> N+1 -> ... -> N+x -> single DPU rollout + power cycle -> DPUs online
```

Between steps 2 and 3 the DPUs run a BFB train that the newer DPF releases no longer support, which is
an untested state. Taking the DPUs out of service in step 1 is what keeps Option 2 within supported
combinations, so the cluster downtime is not optional.

### Downgrade is not supported

DPF Operator downgrade is not supported once an upgrade has been applied. An upgrade may advance CRD
schemas and migrate stored resources, and reverting those changes safely is not feasible. If you hit
an issue after an upgrade, roll forward to the next DPF release rather than attempting a downgrade.

Rolling back the BFB and DOCA Service bundle is a separate matter and is supported per DPU. See
[Rolling back a DPU](#rolling-back-a-dpu).

## BFB and DOCA Service support window

Each DPF release fully supports the matching DOCA BFB train, with BFB and services at the same
version. Alongside it, the release also supports the immediately preceding GA train and the current
BFB LTS train, so DPUs can be rolled forward progressively rather than all at once.

The matrix below follows one full annual cycle. It is version independent. For the concrete versions
of the release you run, see the
[BlueField BFB Support Matrix](../../platform-support.md#bluefield-bfb-support-matrix).

| BFB train             | DPF-Oct (prev) | DPF-Jan | DPF-Apr  | DPF-Jul  | DPF-Oct  |
|:----------------------|:--------------:|:-------:|:--------:|:--------:|:--------:|
| BFB LTS-1 + services  |     **✓**      | **✓**   |  **✓**   |  **✓**   |  **✓**   |
| BFB GA-Jan + services |       -        | **✓**   | **✓\***  |    -     |    -     |
| BFB GA-Apr + services |       -        |    -    |  **✓**   | **✓\***  |    -     |
| BFB GA-Jul + services |       -        |    -    |    -     |  **✓**   | **✓\***  |
| BFB LTS + services    |       -        |    -    |    -     |    -     |  **✓**   |

**Legend:**

* **✓** **supported**: fully tested with this DPF release. You can stay on this combination for as
  long as the train remains in its support window.
* **✓\*** **rollout only**: allowed to operate while you move DPUs forward, tested with the feature set
  of the previous DPF release. This is a transitional state, not a resting place: reprovision before
  the next DPF release, and expect to be asked to update first if you open a support case.
* **-** **unsupported**: reprovisioning to a supported combination is required.

**How to read the matrix.** Each row is one BFB train's support window across the year. A GA train is
fully supported in its own quarter and then rollout only for one more release. An LTS train instead
stays fully supported for the whole cycle, in parallel with the shorter GA windows, which is what lets
you stay on it without reprovisioning. The top row follows a single LTS train, the one introduced at
the October release that opens the cycle. It is the current LTS for its year and becomes `LTS-1` when
the next LTS ships in the last column, which is where its window ends: about 15 months, or five DPF
releases, from October to October. Past that point it is unsupported and the DPU must be
reprovisioned. What that means for your own schedule is covered in
[How often you reprovision](#how-often-you-reprovision).

> [!NOTE]
> Within a BFB LTS train, only the latest patch release is supported. If you run an older patch of the
> same LTS train, update to the latest patch before opening a support case.

### How often you reprovision

The DPU rollout cadence depends on which BFB train you run:

* **BFB LTS users** reprovision **once per year**, at the October release, when the new BFB LTS becomes
  available. A BFB LTS is supported across all four DPF releases of its year and the following October
  release, so no reprovision is required for any hop in between. Applying an LTS patch (for example, a
  security fix) requires an additional rollout whenever you take it.
* **BFB GA (non-LTS) users** reprovision **roughly once per quarter**. Each BFB GA is supported for
  only two consecutive DPF releases (one fully supported, then one rollout only). Reprovision to the
  next BFB train before advancing to the DPF release where your current BFB is no longer supported.

### Rolling back a DPU

BFB and DOCA Service rollback is supported per DPU. Because reprovisioning re-flashes the DPU and
re-deploys the service containers, rolling back to the previous BFB and service bundle is safe for any
combination the matrix above still shows as supported at your DPF release. Rollback is independent of
the DPF Operator, which cannot be downgraded.

## DOCA Service compatibility

### Interoperability during a rolling upgrade

A rolling upgrade briefly runs two versions of the same DOCA Service side by side across DPU nodes, so
every DOCA Service deployed by DPF must keep working across that transition. NVIDIA's built-in
services already meet this requirement, so it concerns you mainly if you develop or package your own
DOCA Service:

* The current GA version of a service (for example, HBN GAn) interoperates correctly with the previous
  GA version (HBN GAn-1) across DPU nodes in the same cluster.
* At a BFB LTS boundary, the current update of the new LTS train interoperates correctly with the
  latest update of the previous LTS train.
* Third-party services that run multiple instances across DPUs (for example, load balancer nodes on
  separate DPUs) are held to the same requirement.

> [!NOTE]
> A service that cannot run alongside its previous version across DPU nodes cannot be upgraded without
> downtime, and is therefore not supported for rolling upgrades.

### Service versioning and LTS

DOCA Services do not have LTS releases of their own, and a service version always matches its BFB
train. **HBN is the exception.** HBN maintains a separate LTS branch tested against the matching BFB
LTS. Staying on the BFB LTS with the HBN LTS branch is a fully tested combination.

## API and behavior stability

You may run several DPF upgrades between DPU rollouts. BFB LTS users move through all four DPF
releases of a year without reprovisioning, and an offline upgrade
([Option 2](#upgrading-across-multiple-releases)) applies several DPF upgrades in a row before a single
rollout at the end. Keeping this safe requires DPF APIs to stay backward compatible across releases,
so breaking changes are strictly limited in both scope and timing.

### Non-breaking versus breaking changes

**Non-breaking changes** may be made in any DPF release, with no deprecation window:

* Adding a new optional field.
* Relaxing validation (widening a constraint, making a required field optional, or adding allowed
  values).

**Breaking changes** are permitted only in a January release and follow a two-phase deprecation
process. They include:

* Removing a field or resource.
* Tightening validation (making an optional field required, or removing allowed values).
* Renaming or moving a field (treated as removing the old name and adding a new one).
* Changing a field type in an incompatible way.

### The deprecation window

A field or resource is marked deprecated no later than the July release preceding its removal. The
deprecation is recorded in the CRD schema, names the release that will remove the field, and the user
guides stop referencing it. The field keeps working until it is removed in the named January release,
which ships with migration guidance.

```text
Deprecated: no later than DPF-Jul  ->  Still works across DPF-Jul and DPF-Oct  ->  Removed: DPF-Jan
```

Because deprecation lands by July and removal happens no earlier than the following January, the
deprecation window always spans at least two releases.

> [!NOTE]
> If you adopt a new BFB LTS at the October release, you may keep the same configuration for a full
> year. The October release therefore never breaks an existing configuration, and any planned removal
> is already marked deprecated in the CRD schema before you set that yearly configuration up.

### Runtime surfaces used by DOCA Services

During an October upgrade, DPUs and their DOCA Services still run the previous version while the new
DPF Operator is already live on the control plane. Removing a DPF behavior that running services
depend on would break them mid-upgrade. Any DPF surface that a DOCA Service or chart consumes at
runtime, such as config ports, controller-injected annotations, or labels read through the downward
API, is therefore changed or removed only in a January release, never at October.

### Finding deprecated fields in your manifests

Start with the `DPFOperatorConfig`. It exposes a status condition that reports deprecated field usage
across your applied manifests, so you can see everything that needs migrating before the next January
release in one place, without reading release notes.

To inspect a single field, the deprecation notices are also recorded in the CRD schemas, so
`kubectl explain` reports them straight from the cluster:

```shell
$ kubectl explain dpudeployment.spec.dpus.dpuSets.dpuSelector
GROUP:      svc.dpu.nvidia.com
KIND:       DPUDeployment
VERSION:    v1alpha1

FIELD: dpuSelector <map[string]string>


DESCRIPTION:
    DPUSelector defines the DPUs that the DPUSet should target

    Deprecated: This field is deprecated and will be removed with vYY.1.0. Use
    DPUDeviceSelector instead.
```

The notice on a real field names the concrete release that removes it. `vYY.1.0` above stands in for
that release, because this guide does not name version numbers.

Migrate any field carrying such a notice before you upgrade to the release it names.

## See also

* [Platform Support](../../platform-support.md): concrete component, BFB, and Kubernetes versions for
  the release you run.
* [DPF Operator Upgrade Guide](README.md): the step-by-step procedure for performing an upgrade,
  including prevalidation checks.
