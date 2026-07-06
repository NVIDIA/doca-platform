---
title: "Kata Containers"
---

[Kata Containers](https://katacontainers.io/) is an open source project that runs each pod inside a
lightweight virtual machine, combining the operational model of containers with the hardware
isolation boundary of a hypervisor. DPF manages Kata Containers as a system DPUService deployed to
DPU clusters via [kata-deploy](https://github.com/kata-containers/kata-containers/tree/main/tools/packaging/kata-deploy).

# Overview

Kata Containers is disabled by default. It must be explicitly enabled in `DPFOperatorConfig`, after
which DPF deploys the `kata-containers` DPUService to DPU clusters. The DaemonSet installs Kata
binaries on each targeted DPU node, configures containerd, and creates `RuntimeClass` objects.
A `nodeSelector` controls which nodes within a DPU cluster run kata-deploy; nodes that do not match
are left untouched. Workloads opt into VM-level isolation by setting `runtimeClassName` in their
pod spec.

The following `RuntimeClass` is created by default:

| Name | Hypervisor |
|------|-----------|
| `kata-qemu` | QEMU (arm64) |

Nodes are labeled `katacontainers.io/kata-runtime: "true"` once the installation is complete.
The `RuntimeClass.scheduling.nodeSelector` ensures pods are only scheduled on those nodes.

# Enabling Kata Containers

Kata Containers is **disabled by default**. To enable it, set `disable: false` in your
`DPFOperatorConfig`:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  security:
    kata:
      disable: false
```

This deploys kata-deploy with the default settings (`qemu` shim,
`containerdConfigFileName: config-mlnx.toml`).

# Configuration

All fields under `spec.security.kata` are optional. For the full and up-to-date field reference,
refer to the [DPFOperatorConfig API documentation](../../developer-guides/api/api.md).

## Example: restrict to labelled nodes

```yaml
spec:
  security:
    kata:
      disable: false
      nodeSelector:
        kata-containers: "enabled"
```

Kata will only be installed on nodes with the label `kata-containers: enabled`. Nodes without
the label are not affected.

## Example: override the kata-deploy image

```yaml
spec:
  security:
    kata:
      disable: false
      daemon:
        image: my-registry.example.com/kata-deploy:3.30.0
```

# Running workloads with Kata isolation

Once DPFOperatorConfig enables Kata and the `kata-qemu` `RuntimeClass` has been created on the DPU
cluster, add `runtimeClassName: kata-qemu` to any pod spec.

## Basic pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kata-test
spec:
  runtimeClassName: kata-qemu
  containers:
  - name: test
    image: busybox
    command: ["uname", "-a"]
  restartPolicy: Never
```

## Pod with an SF network interface

To attach an SF to a Kata pod, request the `nvidia.com/bf_sf` resource and reference the
corresponding `NetworkAttachmentDefinition` via the `k8s.v1.cni.cncf.io/networks` annotation.
The SF netdev is moved into the pod namespace by the CNI plugin and presented to the VM as a
`virtio-net` NIC via the tcfilter+TAP path. 

**Warning**: SF passthrough is not supported with Kata. Network is not offloaded in this case.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kata-sf-test
  annotations:
    k8s.v1.cni.cncf.io/networks: '[{"name": "mybrhbn", "interface": "net1"}]'
spec:
  runtimeClassName: kata-qemu
  containers:
  - name: test
    image: busybox
    command: ["sleep", "infinity"]
    resources:
      requests:
        nvidia.com/bf_sf: "1"
      limits:
        nvidia.com/bf_sf: "1"
  restartPolicy: Never
```

The `RuntimeClass` automatically adds:

* `overhead.podFixed: {cpu: 250m, memory: 160Mi}` for scheduler resource accounting.
* `scheduling.nodeSelector: {katacontainers.io/kata-runtime: "true"}` so the pod is only
  placed on nodes where kata-deploy has completed installation.
