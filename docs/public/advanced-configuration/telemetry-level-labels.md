---
title: "Telemetry Level Labels"
---

[[_TOC_]]

DOCA telemetry services running on the DPU, such as the DOCA Telemetry Service (DTS), read
`/opt/mellanox/doca/telemetry/config-dpu/level_labels.ini` to enrich exported metrics with topology labels: the pod,
scalable unit (SU) and rack the DPU belongs to, and per-device labels such as rail, plane and the connected switch
port. These labels are different for every DPU, so they cannot be expressed in a static
[DPUFlavor](../developer-guides/api/dpuflavor.md) shared by all DPUs.

This guide shows how to store the topology information on each [DPUDevice](../developer-guides/api/dpudevice.md) and
generate the `level_labels.ini` file per DPU with a single DPUFlavorTemplate.

# How It Works

* `DPUDevice.spec.values` holds free-form per-device values provided by the orchestration platform.
* A DPUFlavorTemplate contains a DPUFlavor body with [Go template](https://pkg.go.dev/text/template) actions. For
  every DPU, DPF renders the template against the values of the DPUDevice the DPU is provisioned on and creates a
  generated DPUFlavor for that DPU. Values are referenced from the template root, for example `{{ .podId }}`.
* The rendered flavor contains a `configFiles` entry, so the file is written to the DPU filesystem during
  provisioning.

A DPU is reprovisioned with a newly rendered flavor when the template body or the device values change, following the
DPUSet rollout strategy. A change that renders to an identical flavor does not disrupt the DPU.

# Store the Topology on Each DPUDevice

Add the topology under `spec.values` of every DPUDevice. The structure is free-form and only needs to match what the
template references:

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: mt25066004c7
  namespace: dpf-operator-system
spec:
  serialNumber: "MT25066004C7"
  bmcIp: "10.1.2.3"
  numberOfPFs: 1
  values:
    podId: 1
    suId: 0
    rack: 0
    eastWestDevices:
    - device: "0000:03:00.0"
      deviceType: physical
      rail: 0
      plane: 0
      destination: leaf-p0-su00-r0
      destinationPort: swp1s1
    - device: "0000:03:00.1"
      deviceType: physical
      rail: 0
      plane: 1
      destination: leaf-p1-su00-r0
      destinationPort: swp1s1
    - device: "0000:03:00.4"
      deviceType: virtual
      rail: 0
```

# Create the DPUFlavorTemplate

The template below renders the `level_labels.ini` from the device values. Physical devices carry the full label set
including the connected switch, while virtual devices only carry rail and device type:

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavorTemplate
metadata:
  name: telemetry-level-labels
  namespace: dpf-operator-system
spec:
  template: |
    spec:
      configFiles:
      - path: /opt/mellanox/doca/telemetry/config-dpu/level_labels.ini
        operation: override
        permissions: "0644"
        raw: |
          [global_labels]
          pod={{ .podId }}
          su={{ .suId }}
          rack={{ .rack }}

          [device_labels]
          {{- range .eastWestDevices }}
          {{- if eq .deviceType "physical" }}
          {{ .device }}=rail|{{ .rail }}|plane|{{ .plane }}|device_type|{{ .deviceType }}|traffic_direction|east-west|destination|{{ .destination }}|destination_port|{{ .destinationPort }}
          {{- else }}
          {{ .device }}=rail|{{ .rail }}|device_type|{{ .deviceType }}|traffic_direction|east-west
          {{- end }}
          {{- end }}

          [device_mapping]

          [data_types_mapping]
          ethtool_event=device_name|netif
          ppcc_eth=device_name|mst
          ifconfig_event=device_name|netif
          amber_event=device_name|mst
```

> [!NOTE]
> The generated flavor replaces the static DPUFlavor for these DPUs, so the template body must contain your complete
> flavor configuration (grub, ovs, nvconfig, further config files and so on). Only the `configFiles` entry relevant
> for the telemetry labels is shown here.

Keep the following template rules in mind:

* Referencing a key that is absent from the device values fails the render instead of emitting `<no value>`. The
  failure is surfaced on the DPU through the `DPUFlavorRendered` condition.
* The `{{define}}`, `{{block}}` and `{{template}}` actions are rejected.
* The template body is limited to 1 MiB and the rendered output to 1.5 MiB.

# Reference the Template

A DPUFlavorTemplate is referenced instead of a static DPUFlavor, the two are mutually exclusive.

In a [DPUDeployment](../developer-guides/api/dpudeployment.md) via `spec.dpus.flavorTemplate`:

```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: telemetry
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavorTemplate: telemetry-level-labels
    ...
```

In a [DPUSet](../developer-guides/api/dpuset.md) via `spec.dpuTemplate.spec.dpuFlavorTemplate`:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: telemetry
  namespace: dpf-operator-system
spec:
  dpuTemplate:
    spec:
      bfb:
        name: bf-bundle
      dpuFlavorTemplate: telemetry-level-labels
      ...
```

# Rendered Result

For the DPUDevice above, the generated DPUFlavor writes the following file to
`/opt/mellanox/doca/telemetry/config-dpu/level_labels.ini` during provisioning:

```ini
[global_labels]
pod=1
su=0
rack=0

[device_labels]
0000:03:00.0=rail|0|plane|0|device_type|physical|traffic_direction|east-west|destination|leaf-p0-su00-r0|destination_port|swp1s1
0000:03:00.1=rail|0|plane|1|device_type|physical|traffic_direction|east-west|destination|leaf-p1-su00-r0|destination_port|swp1s1
0000:03:00.4=rail|0|device_type|virtual|traffic_direction|east-west

[device_mapping]

[data_types_mapping]
ethtool_event=device_name|netif
ppcc_eth=device_name|mst
ifconfig_event=device_name|netif
amber_event=device_name|mst
```

# Preview a Render

`dpfctl` can render a template locally against a DPUDevice file to validate it before applying:

```bash
# Render a template against a DPUDevice's values
dpfctl flavor-template render -f template.yaml --dpudevice-file device.yaml

# Preview what the controller will generate for a live DPUDevice, then validate it
kubectl get dpudevice <name> -n <namespace> -o yaml > device.yaml
dpfctl flavor-template render -f template.yaml --dpudevice-file device.yaml | kubectl create --dry-run=server -f -
```
