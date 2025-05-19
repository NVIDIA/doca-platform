This document describes how to build SPDK CSI image, helm chart and how to deploy SPDK CSI into the DPF environment through DPUservice.

[SPDK CSI plugin](https://github.com/spdk/spdk-csi) is a open source project which brings SPDK to Kubernetes. It provisions SPDK logical volumes on storage node dynamically and enables Pods to access SPDK storage backend through NVMe-oF or iSCSI.

_***Note: SPDK v24.01 is the last version that is compatible with the spdk-csi. 
The spdk-csi can't work with newer versions.***_

## Prerequisites

### Build image
[SPDK CSI plugin](https://github.com/spdk/spdk-csi) does not provide the released image. We need build the image from source code.

*Beofre building/pushing images and helm chart, it is recommended to set `CSI_IMAGE_REGISTRY` and `CSI_IMAGE_TAG` for the image.*

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

### Deploy SPDK CSI controller through DPUService

#### Create DPUServiceCredentialRequest in host cluster

`dpuservice/DPUServiceCredentialRequest.yaml` is an example of a `DPUServiceCredentialRequest` which requests credentials
to access a cluster. Need update the `namespace` base on the namespace where the `DPUCluster` is in your DPF environment. 

- `$ kubectl apply -f dpuservice/DPUServiceCredentialRequest.yaml`

#### Create DPUService in host cluster

`dpuservice/DPUService.yaml` is an example of a `DPUService` which requests credentials
to access a cluster. Need update the `namespace` base on the namespace where the `DPUCluster` is in your DPF environment. Also need update the `spec.helmChart.values.host.config` according to your spdk storage.

Run the following commands from the directory where this file is located.

- `$ kubectl apply -f dpuservice/DPUService.yaml`
- `$ kubectl apply -f dpuservice/DPUService-dpu.yaml`
