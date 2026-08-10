---
title: "DPUFlavorTemplate"
---

[[_TOC_]]

## Overview

DPUFlavorTemplate is a Kubernetes Custom Resource Definition (CRD) whose `spec.template` body is a Go [`text/template`](https://pkg.go.dev/text/template) that renders **per DPU** into a concrete [DPUFlavor](dpuflavor.md). Rendering runs against `DPUDevice.spec.values`, so a single template can express device-specific configuration (addresses, tags, hostnames, per-port settings, …) without maintaining one static DPUFlavor per DPU.

A DPUFlavorTemplate is referenced by either a [DPUSet](dpuset.md) or a [DPUDeployment](dpudeployment.md), mutually exclusive with the static DPUFlavor reference. When a DPU is created, the DPUSet controller renders the template against the target `DPUDevice.spec.values` and creates a generated DPUFlavor that the DPU then consumes.

For a task-oriented walkthrough (populating values, wiring from DPUSet/DPUDeployment, diagnosing render failures), see [DPUFlavorTemplate: Per-DPU Flavor Rendering](../../advanced-configuration/dpuflavortemplate-per-dpu-config.md).

## API Version

- **API Group**: `provisioning.dpu.nvidia.com`
- **API Version**: `v1alpha1`
- **Kind**: `DPUFlavorTemplate`

## Key Features

- **Per-DPU rendering**: one template produces a distinct DPUFlavor per DPU, using values pulled from that DPU's DPUDevice.
- **Mutually exclusive with DPUFlavor** at the consumer level: `DPUSet.spec.dpuTemplate.spec.dpuFlavorTemplate` or `DPUDeployment.spec.dpus.flavorTemplate`, never both a template and a static flavor.
- **Non-templated resource-fitting fields**: `dpuResources` and `systemReservedResources` are set on the template spec itself (not templated in the body) and are stamped onto every generated flavor.
- **Admission validation after render**: each rendered flavor is validated by the same DPUFlavor admission that guards hand-authored flavors.
- **Deterministic re-render**: the DPUSet controller hashes the template body and the DPUDevice values on the DPU; a re-render happens only when either input changes.

## API Reference

### DPUFlavorTemplateSpec

| Field                     | Type                                                       | Description |
|---------------------------|------------------------------------------------------------|-------------|
| `template`                | string                                                     | The DPUFlavor body as a YAML string with Go template actions (`{{ ... }}`). Rendered per DPU against `DPUDevice.spec.values`, then unmarshalled into a [DPUFlavor](dpuflavor.md) and validated by DPUFlavor admission. Any `dpuResources` or `systemReservedResources` present in the body are overwritten by the top-level fields on this spec (they are not rejected). Required, `MinLength=1`. |
| `dpuResources`            | [ResourceList](https://pkg.go.dev/k8s.io/api/core/v1#ResourceList) | Not templated. Stamped onto every generated flavor's `spec.dpuResources`; takes precedence over anything in the rendered body. Optional. |
| `systemReservedResources` | [ResourceList](https://pkg.go.dev/k8s.io/api/core/v1#ResourceList) | Not templated. Stamped onto every generated flavor's `spec.systemReservedResources`; takes precedence over anything in the rendered body. Must not exceed `dpuResources`. Optional. |

## Template Rendering

DPUFlavorTemplate uses Go's [`text/template`](https://pkg.go.dev/text/template) package. Placeholders resolve against `DPUDevice.spec.values`, which is arbitrary JSON decoded into a `map[string]interface{}` for the render.

### Rules

- **Delimiters**: standard `{{ }}` Go template syntax.
- **`missingkey=error`**: referencing a key not present in `DPUDevice.spec.values` fails the render. There is no fallback to empty string. Every key referenced in the template body must be present on every DPUDevice this template will be applied to.
- **Rendered output shape**: after render, the string must unmarshal into a valid [DPUFlavor](dpuflavor.md). The root of the rendered YAML is the DPUFlavor object (typically starting with `spec:`).
- **Resource-fitting precedence**: if the rendered body contains `dpuResources` or `systemReservedResources`, they are overwritten by the values from `DPUFlavorTemplateSpec.dpuResources` / `systemReservedResources` before the generated flavor is created. This keeps resource fitting out of per-device rendering.
- **Admission**: the generated flavor is validated by the DPUFlavor admission webhook and CRD schema. A rejection (Invalid or BadRequest) is treated the same as a render failure — see [Diagnosing render failures](../../advanced-configuration/dpuflavortemplate-per-dpu-config.md#diagnosing-render-failures).

### Populating `DPUDevice.spec.values`

`DPUDevice.spec.values` is a `*runtime.RawExtension` (arbitrary JSON). It is not populated by discovery; operators are responsible for setting it before the DPUSet controller renders for that DPU (i.e. before the DPU is created). See the [advanced-configuration how-to](../../advanced-configuration/dpuflavortemplate-per-dpu-config.md#populating-dpudevicespecvalues) for an example.

## Generated DPUFlavor

For each DPU that references a DPUFlavorTemplate, the DPUSet controller creates a generated DPUFlavor named identically to the DPU (`generatedFlavor.Name == dpu.Name`). The DPU consumes it via its own `spec.dpuFlavor` field.

The DPU carries three labels that identify and version-track the rendering:

| Label                                                     | Meaning |
|-----------------------------------------------------------|---------|
| `provisioning.dpu.nvidia.com/dpuflavortemplate-name`      | Name of the source DPUFlavorTemplate |
| `provisioning.dpu.nvidia.com/dpuflavortemplate-hash`      | Short hash of the render inputs (template body + `dpuResources` + `systemReservedResources`) |
| `provisioning.dpu.nvidia.com/dpudevice-values-hash`       | Short hash of `DPUDevice.spec.values` at render time |

A re-render is triggered only when one of the hash labels no longer matches the current inputs (e.g. the template body changed, or `DPUDevice.spec.values` was updated). Under a `RollingUpdate` DPUSet strategy the affected DPUs are reprovisioned automatically; under `OnDelete` the operator deletes the DPU CR to trigger a fresh render on the new DPU.

## Consumer References

| Consumer | Field | Mutually exclusive with |
|----------|-------|-------------------------|
| [DPUSet](dpuset.md) | `spec.dpuTemplate.spec.dpuFlavorTemplate` | `spec.dpuTemplate.spec.dpuFlavor` |
| [DPUDeployment](dpudeployment.md) | `spec.dpus.flavorTemplate` | `spec.dpus.flavor` |

The XOR is enforced by CEL validation at admission time: exactly one of the static reference or the template reference must be set on the consumer resource.

## Examples

### Minimal template

The template body must render to a valid DPUFlavor. A minimal template with no placeholders still works and produces the same flavor for every DPU (no reason to use a template in that case — this is just the shape):

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavorTemplate
metadata:
  name: minimal
  namespace: dpf-operator-system
spec:
  template: |
    spec:
      grub:
        kernelParameters:
          - console=hvc0
```

### Templated `bfcfgParameters`

Emit a firmware-update flag whose value comes from each DPUDevice:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavorTemplate
metadata:
  name: per-device-fw
  namespace: dpf-operator-system
spec:
  template: |
    spec:
      bfcfgParameters:
        - WITH_NIC_FW_UPDATE={{ .updateFirmware }}
        - UPDATE_ATF_UEFI={{ .updateFirmware }}
```

Matching `DPUDevice.spec.values`:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: dpu-0
spec:
  values:
    updateFirmware: "yes"
```

### Templated node-labeling script

Render a per-DPU node-labeling script that emits a tenant-Node label (see [DPU Node Label Scripts](dpuflavor.md#dpu-node-label-scripts) in the DPUFlavor doc for the mechanism):

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavorTemplate
metadata:
  name: per-device-label
  namespace: dpf-operator-system
spec:
  template: |
    spec:
      configFiles:
        - operation: override
          path: /var/lib/dpf/dpuagent/node-label-scripts/rack.sh
          permissions: "0700"
          raw: |
            #!/bin/bash
            echo "rack={{ .rack }}"
```

Matching `DPUDevice.spec.values`:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: dpu-0
spec:
  values:
    rack: "A12"
```

Result: the tenant-Node backed by `dpu-0` carries the label `scripts.dpu.nvidia.com/rack=A12`.

### Stamped resource fitting

Resource-fitting fields are not templated:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavorTemplate
metadata:
  name: fitted
  namespace: dpf-operator-system
spec:
  dpuResources:
    cpu: "16"
    memory: 16Gi
    nvidia.com/sf: 20
  systemReservedResources:
    cpu: "4"
    memory: 4Gi
    nvidia.com/sf: 4
  template: |
    spec:
      grub:
        kernelParameters:
          - iommu.passthrough={{ .iommu }}
```

Every generated flavor carries the same `dpuResources`/`systemReservedResources` block from the template spec, while `grub.kernelParameters` is rendered per DPU from the value under `.iommu`.

## Best Practices

1. **Document every referenced key**: alongside the template, list which `DPUDevice.spec.values` keys it expects. Missing keys fail the render with `missingkey=error`.
2. **Keep templates deterministic**: prefer inert defaults where possible; avoid conditionals that change the DPUFlavor structure between DPUs unless you have a clear reason.
3. **Do not template resource fitting**: put `dpuResources` and `systemReservedResources` on `DPUFlavorTemplateSpec`, not in the template body — the values in the body are overwritten.
4. **Populate `DPUDevice.spec.values` before creating the DPU**: for the initial provisioning, ensure every referenced key exists on the DPUDevice before the DPUSet is created. See the [how-to](../../advanced-configuration/dpuflavortemplate-per-dpu-config.md#populating-dpudevicespecvalues).
5. **Test the render locally**: because rendering is a pure function of `spec.template` and `DPUDevice.spec.values`, you can validate a template against sample values using Go `text/template` before applying it to a cluster.
