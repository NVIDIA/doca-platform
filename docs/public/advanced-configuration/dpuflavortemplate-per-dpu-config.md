---
title: "DPUFlavorTemplate: Per-DPU Flavor Rendering"
---

[[_TOC_]]

By default, a DPUSet or DPUDeployment references a single static [DPUFlavor](../developer-guides/api/dpuflavor.md) that is applied identically to every DPU under it. The DPUFlavorTemplate feature lets you point at a shared [DPUFlavorTemplate](../developer-guides/api/dpuflavortemplate.md) instead: a Go `text/template` body that is rendered **per DPU** against `DPUDevice.spec.values`. Each DPU ends up with its own generated DPUFlavor derived from the same template, so device-specific configuration (addresses, tags, per-port settings, node labels, …) does not require one hand-maintained DPUFlavor per DPU.

The consumer field is mutually exclusive with the static DPUFlavor reference on both DPUSet (`spec.dpuTemplate.spec.dpuFlavorTemplate`) and DPUDeployment (`spec.dpus.flavorTemplate`).

# Prerequisites

- A [DPUFlavorTemplate](../developer-guides/api/dpuflavortemplate.md) CR exists in the target namespace.
- `DPUDevice` objects exist for the DPUs you plan to provision (created by discovery or declaratively).
- Every key referenced by the template body has been written to each DPUDevice's `spec.values` (see below).
- The operator has permission to `patch` `DPUDevice` and to create the consumer resource (DPUSet or DPUDeployment).

# Populating `DPUDevice.spec.values`

`DPUDevice.spec.values` is a `*runtime.RawExtension` (arbitrary JSON). Discovery does NOT populate it; operators are responsible for setting it before the DPUSet controller renders for that DPU. The keys used in the template body must all be present in `spec.values` at render time — rendering runs with `missingkey=error`, so an unknown key fails the render and blocks that DPU's provisioning.

Set values via `kubectl patch`:

```bash
kubectl -n dpf-operator-system patch dpudevice dpu-0 \
  --type=merge -p '{"spec":{"values":{"rack":"A12","updateFirmware":"yes"}}}'
```

Or declaratively in a manifest:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: dpu-0
  namespace: dpf-operator-system
spec:
  values:
    rack: "A12"
    updateFirmware: "yes"
```

Populate every DPUDevice that will end up in the DPUSet before the DPUSet is created. Changing `spec.values` later on an existing template-mode DPU triggers a re-render (see [Generated DPUFlavor](../developer-guides/api/dpuflavortemplate.md#generated-dpuflavor) in the API reference).

# Wiring from a DPUSet

Set `spec.dpuTemplate.spec.dpuFlavorTemplate` and leave `dpuFlavor` unset (the CEL rule `has(self.dpuFlavor) != has(self.dpuFlavorTemplate)` rejects setting both):

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: my-dpuset
  namespace: dpf-operator-system
spec:
  strategy:
    type: RollingUpdate
  dpuTemplate:
    spec:
      bfb:
        name: bf-bundle
      dpuFlavorTemplate: per-device-label
      nodeEffect:
        drain: true
```

# Wiring from a DPUDeployment

Set `spec.dpus.flavorTemplate` and leave `flavor` unset:

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: my-deployment
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavorTemplate: per-device-label
    dpuSets:
      - nameSuffix: workers
        nodeSelector:
          matchLabels:
            feature.node.kubernetes.io/dpu-enabled: "true"
    dpuSetStrategy:
      type: RollingUpdate
    nodeEffect:
      drain: true
  # ... services and serviceChains as usual ...
```

The DPUDeployment controller propagates `flavorTemplate` onto the DPUSets it generates. Every DPU in the deployment renders the same template against its own DPUDevice values.

# Observing the generated DPUFlavor

The DPUSet controller creates a generated DPUFlavor per DPU. The generated flavor is named identically to the DPU (`generatedFlavor.Name == dpu.Name`) and lives in the same namespace as the DPUSet.

List the generated flavors by DPU:

```bash
kubectl -n dpf-operator-system get dpu -o name \
  | while read dpu; do
      name=${dpu##*/}
      echo "--- ${name} ---"
      kubectl -n dpf-operator-system get dpuflavor "${name}" -o yaml
    done
```

Each template-mode DPU carries three labels that identify and version-track the render:

| Label                                                     | Meaning |
|-----------------------------------------------------------|---------|
| `provisioning.dpu.nvidia.com/dpuflavortemplate-name`      | Name of the source DPUFlavorTemplate |
| `provisioning.dpu.nvidia.com/dpuflavortemplate-hash`      | Short hash of the render inputs (template body + `dpuResources` + `systemReservedResources`) |
| `provisioning.dpu.nvidia.com/dpudevice-values-hash`       | Short hash of `DPUDevice.spec.values` at render time |

The hash labels are the render's version identifiers: if either hash no longer matches the current inputs (e.g. the template body changed, or `DPUDevice.spec.values` was updated), the DPU is considered outdated and is re-rendered on the next reconcile (per the DPUSet `strategy.type`).

# Diagnosing render failures

Render failures are surfaced on the DPU itself, not on the DPUFlavorTemplate. In both cases below the failure is recorded on the DPU so it is visible to `kubectl get dpu`:

- DPU condition `DPUFlavorRendered=False`, with the reason distinguishing the two modes.
- Annotation `provisioning.dpu.nvidia.com/render-failed-reason` (`RenderFailedOnCreate` or `RenderFailedOnUpdate`).
- Annotation `provisioning.dpu.nvidia.com/render-failed-message` carrying the underlying error.

Inspect:

```bash
kubectl -n dpf-operator-system get dpu -o wide
kubectl -n dpf-operator-system describe dpu <name>
```

Common error signatures (apply to both modes):

| Error text | Cause |
|------------|-------|
| `failed to render template: template: dpuflavortemplate:X:Y: executing "dpuflavortemplate" at <.key>: map has no entry for key "key"` | Template body references a key that is missing from `DPUDevice.spec.values` |
| `failed to parse template: ...` | Template syntax error (unmatched `{{ }}`, unknown action, …) |
| `rendered template is not a valid DPUFlavor: ...` | Rendered output does not unmarshal into a DPUFlavor (top-level shape wrong, invalid YAML, …) |
| `DPUFlavor.spec is invalid: ...` (from admission) | Rendered flavor rejected by the DPUFlavor admission webhook or CRD schema |

## Create-time failure (`RenderFailedOnCreate`)

The render (or its admission) failed when the DPU was first provisioned, so the generated DPUFlavor was never created.

- **Phase:** the DPU is created directly in `Error`.
- **Condition:** `DPUFlavorRendered=False`, reason `RenderFailedOnCreate`.

**Recovery:** this is a deterministic per-flavor failure — the DPUSet controller does not silently retry the same render. Fix the underlying cause (populate the missing key in `DPUDevice.spec.values`, correct the template body, or adjust admission-rejected fields). Once the template or values change, the render-input hash changes and the DPU is marked outdated: under a `RollingUpdate` strategy it is reprovisioned automatically; under `OnDelete` delete the DPU CR to trigger a fresh create with the corrected render.

## Update-time failure (`RenderFailedOnUpdate`)

The render inputs changed *after* the DPU already existed and the new render (or its admission dry-run) failed. The existing DPU and its current generated DPUFlavor are left untouched.

- **Phase:** unchanged — the DPU keeps running and does **not** enter `Error`. `DPUFlavorRendered=False` can appear on an otherwise `Ready` DPU.
- **Condition:** `DPUFlavorRendered=False`, reason `RenderFailedOnUpdate`.

**Recovery:** non-disruptive and self-healing. Fix the template or `DPUDevice.spec.values`; the controller re-evaluates on the next reconcile and clears the condition automatically once the render succeeds — **no DPU deletion is required** to clear the failure. If the corrected render differs from the flavor the DPU is currently running, the DPU is then treated as outdated and reprovisioned per strategy (automatic under `RollingUpdate`; delete the DPU CR under `OnDelete`).

# Limitations

- **`dpuResources` / `systemReservedResources` are not templated.** They are set on the `DPUFlavorTemplate` spec directly and are stamped onto every generated flavor, overriding anything the rendered body may contain.
- **`missingkey=error`.** Every key referenced by the template body must be present in `DPUDevice.spec.values` at render time. There is no fallback default.
- **`DPUDevice.spec.values` must be populated by the operator.** Discovery does not fill it. Populate it before the DPUSet is created, or before you switch an existing DPUSet from a static DPUFlavor to a DPUFlavorTemplate.
- **Consumer XOR.** Setting both `dpuFlavor` and `dpuFlavorTemplate` on the same consumer resource is rejected by CEL admission (`has(self.dpuFlavor) != has(self.dpuFlavorTemplate)`).
- **Create-time render failures are terminal per DPU.** Once a DPU is in `Error` with `RenderFailedOnCreate`, the same DPU is not silently re-rendered. After fixing the inputs, the DPUSet controller reprovisions the affected DPUs automatically under a `RollingUpdate` strategy (delete + recreate). Under an `OnDelete` strategy the operator must delete the DPU CR manually to trigger a fresh create with the corrected render.
