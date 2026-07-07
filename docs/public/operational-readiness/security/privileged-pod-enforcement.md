---
title: "Privileged Pod Enforcement"
---

[[_TOC_]]

DPF restricts which DPUService workloads may run privileged containers in a DPUCluster. By
default, a workload managed by a DPUService may **not** run a container with
`securityContext.privileged: true` in the DPUCluster unless that DPUService explicitly opts in.

Enforcement is implemented with a Kubernetes
[`ValidatingAdmissionPolicy`](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/)
(VAP) that the DPUService controller installs into each DPUCluster. Enforcement is controlled by
the `DPFOperatorConfig.spec.security.privilegedPodEnforcement` field.

> [!NOTE]
> This page covers enforcement for workloads that run **in a DPUCluster** (DPUServices with
> `spec.deployInCluster: false` or unset). DPUServices that deploy in the host cluster
> (`spec.deployInCluster: true`) are out of scope and must not set `spec.security.privileged`.

# How it works

For every DPUCluster, the DPUService controller maintains a `ValidatingAdmissionPolicy`, a matching
`ValidatingAdmissionPolicyBinding`, and an allowlist ConfigMap. At admission time, any Pod created
by a DPUService-managed workload that requests a privileged container is denied unless the owning
DPUService is present in the allowlist. Denied requests are rejected with the condition message:

```text
Privileged containers are not allowed for this DPUService unless security.privileged is set to true.
```

Workloads are matched by the `svc.dpu.nvidia.com/service` label that the controller injects into the
resources it manages and into the pod templates of workload controllers (Deployment, DaemonSet,
ReplicaSet, StatefulSet, Job, CronJob). Regular, init, and ephemeral containers are all validated.

## Scope and limitations

> [!IMPORTANT]
> Only `securityContext.privileged: true` is gated. Other privilege-escalation vectors —
> `hostPID`, `hostIPC`, `hostNetwork`, `allowPrivilegeEscalation`, container capabilities, and
> `hostPath` volumes — are currently **not** gated by this policy.

Enforcement relies on the `svc.dpu.nvidia.com/service` label. If a Helm chart strips that label from
its pod template, the parent workload resource is admitted but its child Pods are denied at Pod
admission time.

# Opting a DPUService in to privileged pods

Set `spec.security.privileged` on the DPUService. For a DPUService that targets a DPUCluster
(`spec.deployInCluster: false`), this field **must be set explicitly** — the API rejects the object
if it is omitted.

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: my-privileged-service
  namespace: dpf-operator-system
spec:
  deployInCluster: false
  security:
    privileged: true   # allow privileged containers for this DPUService's workloads
  # ...
```

Set `privileged: false` for DPUServices that do not need privileged containers. Because the field is
required for DPUCluster-targeted services, this is an explicit, auditable decision per service.

> [!IMPORTANT]
> In this release, when `spec.security.privileged` is left unset — for example on a
> `DPUServiceTemplate` that does not set it, or on grandfathered DPUServices created before this
> validation existed — DPF currently defaults to **allowing** privileged workloads so that workloads
> carried over from earlier releases keep running. This default is planned to change to **deny** in a
> future release. Set `spec.security.privileged` explicitly now to avoid a behavior change on a later
> upgrade.

## Opting in from a DPUServiceTemplate

For charts that inherently require privilege, set `spec.security.privileged` on the
`DPUServiceTemplate`. The DPUDeployment controller propagates the template value to the generated
DPUServices that target DPUClusters.

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: my-privileged-template
  namespace: dpf-operator-system
spec:
  security:
    privileged: true
  # ...
```

For the full field reference, see the
[DPUService](../../developer-guides/api/dpuservice.md) and
[DPUServiceTemplate](../../developer-guides/api/dpudeployment.md) API documentation.

# Disabling enforcement globally

Privileged pod enforcement is controlled by
`DPFOperatorConfig.spec.security.privilegedPodEnforcement`, which **defaults to `true`** (enforcement
on). Setting it to `false` does **not** fully turn enforcement off — it switches the policy to
**audit-only**: privileged pods are still recorded in the Kubernetes audit log, they are just no
longer denied (see [what happens when disabled](#what-happens-when-disabled)). An operator can change
it live, without restarting any component, by setting the field to `false`:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  security:
    privilegedPodEnforcement: false   # stop denying privileged pods
```

The toggle takes effect promptly: the DPUService controller watches the `DPFOperatorConfig` and
reconciles when the resolved enforcement state changes.

## What happens when disabled

When `privilegedPodEnforcement: false`, the VAP, its binding, and the allowlist ConfigMap are **not
deleted**. Instead the binding's `validationActions` is switched from `Deny` to `Audit`:

* Privileged pods are **no longer denied** — they are only recorded in the Kubernetes audit log.
* The allowlist is kept populated, so the audit log flags only the pods that *would* have been
  denied while enforcement was on.

When re-enabled, the allowlist is authoritatively rebuilt from all DPUServices, the binding is
re-applied in `Deny` mode, and a dry-run probe confirms that privileged pods are actually denied
before the reconcile succeeds.

> [!IMPORTANT]
> While the policy is in audit-only mode, DPUCluster workloads can run privileged containers
> regardless of their `spec.security.privileged` setting. Switching to audit-only via
> `spec.security.privilegedPodEnforcement: false` makes sense in two cases:
>
> * **Temporary recovery** — the policy causes unexpected issues and you need to restore functionality
>   while investigating. Treat this as temporary: monitor the audit log for flagged pods, and re-enable
>   enforcement once the underlying issue is resolved.
> * **An adjacent control already covers it** — the DPUCluster already restricts privileged pods through
>   another mechanism (for example Pod Security admission or a third-party policy engine), so DPF's
>   enforcement would be redundant. In this case disabling it can be a permanent configuration choice.

The behaviour is fail-secure: enforcement is treated as enabled when `spec.security` is absent, when
`privilegedPodEnforcement` is unset, or when it is explicitly `true`.
