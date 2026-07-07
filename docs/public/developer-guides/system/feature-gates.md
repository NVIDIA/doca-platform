# Feature Gates

DPF uses Kubernetes-style feature gates to guard functionality that is
experimental, changes security posture, or needs an operator-controlled
off-switch. The gate definitions live in `internal/features/features.go`
and are shared by every DPF binary.

## Lifecycle stages

Feature gates move through stages before being removed:

| Stage | Default | Meaning |
|-------|---------|---------|
| `Alpha` | `false` | Experimental. Off by default. May be removed without notice. |
| `Beta` | `true` | Mostly stable. On by default. Breaking changes discouraged. |
| `GA` | `true` | Stable. The gate should be deleted in the next release. |

Expected promotion path: **Alpha → Beta → GA → remove gate entirely**.

## Using a gate in code

Check the gate with `features.Gates.Enabled` at the point where behaviour
should diverge:

```go
if features.Gates.Enabled(features.MyNewFeature) {
    // feature-specific path
}
```

## Using a gate in tests

Use `featuregatetesting.SetFeatureGateDuringTest` — do **not** mutate
`MutableGates` directly, as that leaks state across tests:

```go
import "k8s.io/component-base/featuregate/testing"

featuregatetesting.SetFeatureGateDuringTest(t, features.MutableGates, features.MyNewFeature, true)
```

## Runtime configuration

Pass `--feature-gates=MyNewFeature=true` to the operator or controller
binary. Both binaries register the same gate names, so the same flag value
works for both without translation.

Attempting to disable a GA gate at runtime is a startup error — that is
intentional; GA gates should be removed from the codebase, not toggled off.

## Removing a gate (GA → deletion)

Once a gate reaches GA and its old code path has been cleaned up:

1. Delete the constant and the `defaultDPFFeatureGates` entry from
   `internal/features/features.go`.
2. Remove all `features.Gates.Enabled(features.MyNewFeature)` call sites
   and any dead code that was behind the gate.
3. If a compatibility shim was added during the migration window (e.g. a
   `// TODO: change to false after vX.Y` default constant), remove it too.

## When to use a feature gate

**Use a gate when:**

- The feature changes admission or security posture (e.g. a new
  ValidatingAdmissionPolicy).
- The feature is not yet stable and might need to be reverted in the field.
- A newly enabled-by-default feature could break an existing deployment.
  The gate lets operators disable it while they migrate, without needing a
  downgrade.

Prefer a feature gate over an API field (e.g. a new field on `DPFOperatorConfig`)
for temporary options. API fields become part of the public contract and removing
them is a breaking change; feature gates are explicitly designed to be promoted
and then deleted.

**Do not use a gate when:**

- The change is a bug fix or a purely internal refactor.
- The functionality is always-on and there is no plausible reason to
  disable it.
- The change is additive and non-breaking (new API fields, new metrics).
