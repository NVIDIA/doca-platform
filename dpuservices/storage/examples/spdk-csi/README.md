This document describes how to build SPDK CSI image, helm chart and how to deploy SPDK CSI into the DPF environment through DPUservice.

[SPDK CSI plugin](https://github.com/spdk/spdk-csi) is a open source project which brings SPDK to Kubernetes. It provisions SPDK logical volumes on storage node dynamically and enables Pods to access SPDK storage backend through NVMe-oF or iSCSI.

_***Note: SPDK v24.01 is the last version that is compatible with the spdk-csi. 
The spdk-csi can't work with newer versions.***_

## Prerequisites

### Build image
[SPDK CSI plugin](https://github.com/spdk/spdk-csi) does not provide the released image. We need build the image from source code.

*Before building/pushing images and helm chart, it is recommended to set `CSI_IMAGE_REGISTRY` and `CSI_IMAGE_TAG` for the image.*

- `$ git clone https://github.com/spdk/spdk-csi.git`
- `$ make image`

### Push image
Use `docker push` command to push the SPDK CSI controller image to your image registry. 


### Build and helm chart

Run the following commands from the DPF repo root.

- `export HELM_REGISTRY=<your-registry>`>
- `export TAG=<version-for-spdk-csi-chart>`>
- `$ make helm-package-spdk-csi-controller`
- `$ make helm-push-spdk-csi-controller`
