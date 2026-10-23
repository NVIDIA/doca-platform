---
title: "Configure DPU Virtual Functions"
---

[[_TOC_]]

This guide explains how to configure DPU Virtual Functions (VFs) in a DPF Host Trusted environment.

The examples below configure **20 VFs per host Physical Function (PF)** and **8 VFs per DPU Embedded CPU Physical
Function (ECPF)** in firmware, and create 8 DPU VFs on port `p0`. Adjust these values for your environment. For
supported VF counts and hardware limits, see the
[NVIDIA BlueField SR-IOV documentation](https://networking-docs.nvidia.com/doca/archive/3-5-0/bluefield-sr-iov).

> [!IMPORTANT]
> Schedule a maintenance window for a manual host power cycle after configuring the host PFs.
> A host power cycle resets the BlueField device and interrupts both host and DPU workloads. Rebooting only the DPU OS
> does not apply these firmware settings. Follow your platform's
> [BlueField reset and reboot procedures](https://networking-docs.nvidia.com/doca/archive/3-5-0/nvidia-bluefield-reset-and-reboot-procedures).

## Why Manual Host Configuration Is Required

Enabling VFs on the DPU ECPF requires enabling per-function VF limits in firmware (`PF_NUM_OF_VF_VALID=1`). When
per-function limits are enabled, the firmware ignores the global `NUM_OF_VFS` parameter and requires `PF_NUM_OF_VF` to
be set individually on the DPU ECPF and on each host PF.

`spec.nvconfig` in a DPUFlavor runs on the DPU and applies `PF_NUM_OF_VF` only to the DPU ECPF. Until DPF supports
applying host firmware settings through `spec.hostNetworkInterfaceConfigs[].nvconfig`, you must configure
`PF_NUM_OF_VF` on the host PFs manually.

## Prerequisites

* A physical BlueField in DPU mode, managed by DPF in Host Trusted mode (DOCA 3.5.0 or later on the DPU).
* Administrative access to the host, DPU, management cluster, DPU cluster, and host power controls.
* `mlxconfig` installed on the host or available through the DPF `hostagent` container.

## 1. Configure the DPUFlavor

Merge the following settings into your DPUFlavor, keeping your existing configuration. If your flavor already has a
wildcard (`device: '*'`) entry under `spec.nvconfig`, append these parameters to that entry instead of adding a second
one.

```yaml
spec:
  nvconfig:
    - device: "*"
      force: true
      parameters:
        - SRIOV_EN=1
        - NUM_OF_VFS=20
        - PF_NUM_OF_VF_VALID=1
        - PF_NUM_OF_VF=8
  virtualFunctions:
    - device: p0
      count: 8
      poolName: bf_vf
```

* **`device: '*'` and `force: true`**: Applies the firmware settings to both ports on the DPU. `force: true` is required
  because `PF_NUM_OF_VF` is hidden in firmware until `PF_NUM_OF_VF_VALID=1` is active (see
  [Forcing parameters that firmware hides](../developer-guides/api/dpuflavor.md#forcing-parameters-that-firmware-hides)).
* **`PF_NUM_OF_VF_VALID=1` and `PF_NUM_OF_VF=8`**: Enables per-function VF limits and sets the DPU ECPF limit to 8 VFs
  per port.
* **`NUM_OF_VFS=20`**: Retained because the DPF host agent reads `NUM_OF_VFS` from the flavor to determine how many host
  VFs to create in sysfs, even though firmware now uses `PF_NUM_OF_VF` for the actual limit.
* **`virtualFunctions`**: Creates 8 DPU VFs on `p0` and exposes them in the DPU cluster as `nvidia.com/bf_vf`, matching
  the resource requested by a
  [DPUServiceNAD with `resourceType: vf`](../developer-guides/api/dpuservicenad.md#dynamic-resource-injection).

> [!NOTE]
> Setting `spec.virtualFunctions` disables automatic Scalable Function (SF) inference from `PF_TOTAL_SF`. If your
> workloads also use SFs, explicitly declare them under `spec.scalableFunctions` in the same DPUFlavor (see
> [Function counts and the firmware budget](../developer-guides/api/dpuflavor.md#function-counts-and-the-firmware-budget)).

Provision or reprovision the DPU with this flavor. Wait until DPU provisioning applies the NVConfig settings and
completes the required host power cycle. Note that provisioning may wait in the `Host Network Configuration` phase until
you complete the host configuration below.

## 2. Configure the Host VF Limits

On each required **host PF**, enable per-function VF limits (`PF_NUM_OF_VF_VALID=1`) and set the host VF limit
(`PF_NUM_OF_VF=20`). Until these settings are applied and activated, the host's `sriov_numvfs` sysfs entry may be
absent.

* On the host, identify the BlueField host PCI addresses (for example, `0000:89:00.0` and `0000:89:00.1`):

  ```bash
  lspci -D | grep -i mellanox
  ```

  > [!NOTE]
  > Host and DPU PCI bus addresses differ. Make sure you use the PCI addresses discovered on the host.

* Apply the firmware settings to each host PF using either of the following methods (replace `0000:89:00.0` and
  `0000:89:00.1` with your host PCI addresses):

  * **Directly on the host:**

    ```bash
    sudo mlxconfig -d 0000:89:00.0 -y s PF_NUM_OF_VF_VALID=1 PF_NUM_OF_VF=20
    sudo mlxconfig -d 0000:89:00.1 -y s PF_NUM_OF_VF_VALID=1 PF_NUM_OF_VF=20
    ```

  * **From the management cluster via the `hostagent` container:**

    ```bash
    kubectl -n dpf-operator-system exec "<hostagent-pod-name>" \
      -c hostagent -- mlxconfig -d 0000:89:00.0 -y s PF_NUM_OF_VF_VALID=1 PF_NUM_OF_VF=20
    kubectl -n dpf-operator-system exec "<hostagent-pod-name>" \
      -c hostagent -- mlxconfig -d 0000:89:00.1 -y s PF_NUM_OF_VF_VALID=1 PF_NUM_OF_VF=20
    ```

> [!IMPORTANT]
> Perform a **host power cycle** (including a BlueField device reset) to activate the host firmware settings. Wait for
> both the host and the DPU to boot and become reachable.

## 3. Create the Host VFs

After the host boots, verify whether DPF has already created the host VFs. If not, create them using one of the
following options:

* **Option A — Restart the host-agent pod (recommended):** From the management cluster, delete the host-agent/DMS pod
  using the target host's node name and the host-agent component label. DPF recreates the pod, which automatically
  configures the host VFs using `NUM_OF_VFS` from the DPUFlavor:

  ```bash
  kubectl -n dpf-operator-system delete pod --field-selector "spec.nodeName=<host-node-name>" \
    -l provisioning.dpu.nvidia.com/component=hostagent
  ```

* **Option B — Manually on the host:** Write the VF count directly to `sriov_numvfs` for each host PF:

  ```bash
  echo 20 | sudo tee /sys/bus/pci/devices/0000:89:00.0/sriov_numvfs
  echo 20 | sudo tee /sys/bus/pci/devices/0000:89:00.1/sriov_numvfs
  ```

## 4. Verify the DPU VFs

Once DPU provisioning completes, verify that the DPU node advertises the VF resource in the **DPU cluster**:

```bash
kubectl describe node "<dpu-node-name>"
```

Confirm that both `Capacity` and `Allocatable` include `nvidia.com/bf_vf: 8`.
