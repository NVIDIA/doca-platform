---
title: "Using the DOCA Debug Image"
---

[[_TOC_]]

## Overview

Use the DOCA development container as a general-purpose debug image when you need a shell with DOCA-specific tooling
available on a host or DPU. The image can be used on x86_64 hosts, ARM hosts, and BlueField DPUs. The image version
should follow the DOCA release cadence used by the environment you are debugging. For example, use a `3.2.2` image with
a DOCA 3.2.2 based deployment.

The DOCA debug image includes DOCA-specific tools and related utilities such as `mstflint` and `ovs-tcpdump`. It is
useful when the required tooling is not installed directly on the target system or when you want to use tooling aligned
with the deployed DOCA release.

## Start the Debug Container

Run the container with privileged access and host networking:

```shell
podman run -it --privileged --net=host \
  -v /dev/hugepages:/dev/hugepages \
  -v /var/run/openvswitch:/var/run/openvswitch \
  -v /tmp/doca-debug:/tmp/doca-debug \
  nvcr.io/nvidia/doca/doca:<DOCA_VERSION>-devel-ubuntu24.04 \
  bash
```

For example, for DOCA 3.2.2 use `nvcr.io/nvidia/doca/doca:3.2.2-devel-ubuntu24.04`.

Both `--privileged` and `--net=host` are required for this workflow. Privileged mode allows the debug tools to access
target system devices and kernel interfaces, while host networking allows the container to use the target system
networking stack.

> **Security caution:** Use `--privileged` and `--net=host` only for short-lived debugging with trusted images and
> trusted operators. Remove the debug container after the investigation, and prefer narrower privileges or dedicated
> debug systems when they are sufficient.

## Example: Capture ovs-tcpdump

One example use case is capturing OVS traffic with `ovs-tcpdump`. From inside the container, run:

```shell
ovs-tcpdump -i <ovs-interface> -w /tmp/doca-debug/ovs-tcpdump.pcap
```

The `/tmp/doca-debug` mount in the example command writes the capture to the target system so it remains available
after the container exits.

## Kubernetes Pod Example

If the debug image must run as a Kubernetes pod, use a privileged pod with host networking:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: doca-debug
spec:
  hostNetwork: true
  # Optional: set a node selector when the debug pod must run on a specific node.
  # nodeSelector:
  #   kubernetes.io/hostname: <node-name>
  containers:
  - name: doca-debug
    image: nvcr.io/nvidia/doca/doca:<DOCA_VERSION>-devel-ubuntu24.04
    command: ["bash", "-lc", "sleep infinity"]
    securityContext:
      privileged: true
    volumeMounts:
    - name: hugepages
      mountPath: /dev/hugepages
    - name: openvswitch
      mountPath: /var/run/openvswitch
    - name: output
      mountPath: /tmp/doca-debug
  volumes:
  - name: hugepages
    hostPath:
      path: /dev/hugepages
      type: Directory
  - name: openvswitch
    hostPath:
      path: /var/run/openvswitch
      type: Directory
  - name: output
    hostPath:
      path: /tmp/doca-debug
      type: DirectoryOrCreate
```

## Additional Mount Points

The base command exposes huge pages through `/dev/hugepages`, exposes the OVS runtime socket through
`/var/run/openvswitch`, and persists debug output through `/tmp/doca-debug`. Depending on the specific debug tool,
additional mount points may be needed. For example:

* Firmware and device tools may need access to device nodes under `/dev` or other host paths.
* Tools that write logs, packet captures, or reports may need a host directory mounted for persistent output.

Add only the mount points required for the specific investigation.
