---
title: "Isolated workload with Kata Containers"
---

[[_TOC_]]

This guide describes the prerequisites and configuration options for running isolated workloads with Kata Containers on host worker nodes.

Host-side Kata is installed directly on host workers as a [Helm prerequisite](../getting-started/helm-prerequisites.md). Pods request VM-level isolation
by setting `runtimeClassName` in their pod spec.

## Installation

Kata Containers must be installed on the host nodes before creating Kata pods. See [Kata Containers documentation](https://kata-containers.github.io/kata-containers/) for more details.

It is possible to install it via the DPF Helmfile. See [Helm Prerequisites](../getting-started/helm-prerequisites.md) for more details.

## Configuration for VF cold-plug

`cold_plug_vfio` mode is supported for Kata Containers. This mode allows to cold-plug an SR-IOV Virtual Function directly into the Kata VM using `vfio_mode: guest-kernel`.

This enable passthrough of the VF to the VM, binding the VF to the guest kernel driver (`mlx5_core`, `mlx5_ib`) and providing hardware offload without virtio overhead.

The following node-level configuration is required before creating Kata VF passthrough pods:


| Requirement   | Description                                                       |
| ------------- | ----------------------------------------------------------------- |
| IOMMU enabled | The host kernel must boot with `intel_iommu=on` or `amd_iommu=on` |
| VFs created   | SR-IOV VFs must exist on the BF3 PF (`sriov_numvfs`)              |


The following configuration needs to be set as drop-in configuration for the `kata-qemu` and any other custom RuntimeClass.

```toml
[hypervisor.qemu]
cold_plug_vfio = "root-port"
pcie_root_port = 2

[runtime]
vfio_mode = "guest-kernel"
static_sandbox_resource_mgmt = true
sandbox_cgroup_only = true
```

The `kata-qemu` RuntimeClass and its drop-in configuration are automatically installed by `kata-deploy` when using the DPF Helmfile.

## Running Pods with Kata Isolation

Add `runtimeClassName` to the pod spec to opt into VM-level isolation. The `RuntimeClass`
automatically adds a `scheduling.nodeSelector` that restricts the pod to nodes where kata-deploy
has completed.

### Basic pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kata-test
spec:
  runtimeClassName: kata-qemu
  containers:
  - name: test
    image: nicolaka/netshoot
    command: ["sleep", "infinity"]
```

Verify the pod is running inside a VM:

```bash
kubectl exec kata-test -- uname -r      # guest kernel version
kubectl exec kata-test -- dmesg | head  # QEMU/KVM boot messages
```

