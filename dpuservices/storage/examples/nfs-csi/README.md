This document describes how to build and deploy NFS CSI helm chart into the DPF environment through DPUService.

The chart implements DPF-specific deployment logic for the NFS CSI controller.

[NFS CSI plugin](https://github.com/kubernetes-csi/csi-driver-nfs) is an open source project which provides NFS CSI driver for Kubernetes. 
It provisions NFS volumes dynamically and enables Pods to access NFS storage backend.

The NFS CSI plugin uses upstream images from `registry.k8s.io/sig-storage/nfsplugin` - no custom image building is required.

## Prerequisites

### Build and push helm chart

Run the following commands from the DPF repo root.

```
export HELM_REGISTRY=<your-registry>
export TAG=<version-for-nfs-csi-chart>
make helm-package-nfs-csi-controller
make helm-push-nfs-csi-controller
```
