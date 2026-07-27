---
title: "dpfctl flavor-template"
---

[[_TOC_]]

The `dpfctl flavor-template` command provides utilities for authoring and previewing
[DPUFlavorTemplates](../../../advanced-configuration/dpuflavortemplate-per-dpu-config.md). A
DPUFlavorTemplate is a Go `text/template` body that the DPUSet controller renders **per DPU**
against `DPUDevice.spec.values`, producing a concrete `DPUFlavor` for each device.

The `render` subcommand runs that same rendering logic locally so you can preview and validate
the generated `DPUFlavor` before it is provisioned in the cluster.

## Usage

```sh
dpfctl flavor-template [command] [flags]
```

Available Commands:

| Command  | Description                                             |
|----------|---------------------------------------------------------|
| render   | Render a DPUFlavorTemplate into a concrete DPUFlavor    |

## render

`dpfctl flavor-template render` reads a local `DPUFlavorTemplate` file and a local `DPUDevice`
file, renders the template body against the device's `spec.values`, and prints the resulting
`DPUFlavor` as YAML to stdout.

The command operates entirely on local files and does **not** connect to a cluster, so no
kubeconfig is required. It runs the same renderer (`dpuflavortemplate.Render`) the DPUSet
controller uses, so the preview matches what the controller will generate.

### Usage

```sh
dpfctl flavor-template render -f <template-file> --dpudevice-file <device-file>
```

### Flags

| Flag                | Default | Description                                                                    |
|---------------------|---------|--------------------------------------------------------------------------------|
| `-f`, `--file`      |         | Path to a `DPUFlavorTemplate` YAML/JSON file (required).                        |
| `--dpudevice-file`  |         | Path to a `DPUDevice` YAML/JSON file whose `spec.values` render the template (required). |

Both flags are required; the command takes no positional arguments.

### Examples

Render a template against a `DPUDevice`'s values and print the generated `DPUFlavor`:

```shell
dpfctl flavor-template render -f template.yaml --dpudevice-file device.yaml
```

Preview what the controller will generate for a live `DPUDevice`, then validate the rendered
flavor against the API server without persisting it:

```shell
kubectl get dpudevice <name> -n <namespace> -o yaml > device.yaml
dpfctl flavor-template render -f template.yaml --dpudevice-file device.yaml | kubectl create --dry-run=server -f -
```

### Rendering semantics

The local renderer mirrors the in-cluster behavior:

- Rendering uses `missingkey=error`: every key referenced by the template body must be present
  in the `DPUDevice`'s `spec.values`, otherwise the render fails.
- `spec.dpuResources` and `spec.systemReservedResources` from the `DPUFlavorTemplate` are **not**
  templated. When set, they are stamped onto the generated flavor and override anything the
  rendered body produces.
- Nested-template actions (`{{define}}`, `{{block}}`, `{{template}}`) are rejected, and the
  template body and rendered output are size-bounded.

For the full feature description, wiring from a DPUSet or DPUDeployment, and diagnosing render
failures in the cluster, see
[DPUFlavorTemplate: Per-DPU Flavor Rendering](../../../advanced-configuration/dpuflavortemplate-per-dpu-config.md).

## Troubleshooting

If the render fails with `map has no entry for key "<key>"`, the template body references a key
that is missing from the `DPUDevice`'s `spec.values`. Add the key to the device file (or the live
`DPUDevice`) and retry.

If the render fails with `rendered template is not a valid DPUFlavor`, the rendered output does
not unmarshal into a `DPUFlavor` — check the template body's top-level shape and YAML validity.

If `kubectl create --dry-run=server` rejects the rendered flavor, the DPUFlavor admission webhook
or CRD schema found an invalid field; fix the offending value in the template body.
