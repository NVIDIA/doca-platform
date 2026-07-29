---
title: "Platform Support"
---

[[_TOC_]]

## Prerequisites

| Component  | Version   | Notes                                                                                                            |
|------------|-----------|------------------------------------------------------------------------------------------------------------------|
| Kubernetes | 1.33-1.36 | All currently supported upstream Kubernetes releases are supported                                               |
| Helm       | v3.5+     | For information and methods of Helm installation, please refer to the official [Helm Website](https://helm.sh/). |

## DPF Component Matrix

DPF uses the following components:

| Component                   | Origin          | Repository                                                         | Image Name                                    | Tag     | Notes    |
|-----------------------------|-----------------|--------------------------------------------------------------------|-----------------------------------------------|---------|----------|
| ArgoCD                      | Community (OSS) | quay.io/argoproj                                                   | argocd                                        | v3.3.0  |          |
| Cert Manager Controller     | Community (OSS) | quay.io/jetstack                                                   | cert-manager-controller                       | v1.19.3 |          |
| Flannel                     | Community (OSS) | docker.io/flannel                                                  | flannel                                       | v0.26.5 |          |
| Multus                      | Community (OSS) | ghcr.io/k8snetworkplumbingwg                                       | multus-cni                                    | v3.9.3  |          |
| NVIDIA K8s IPAM             | Community (OSS) | ghcr.io/mellanox                                                   | nvidia-k8s-ipam                               | v0.3.5  |          |
| NVIDIA Maintenance Operator | Community (OSS) | ghcr.io/mellanox                                                   | maintenance-operator                          | v0.3.0  |          |
| NVIDIA Network Operator     | Community (OSS) | nvcr.io/nvidia/cloud-native                                        | network-operator                              | v26.1.0 |          |
| Node Feature Discovery      | Community (OSS) | registry.k8s.io/nfd                                                | node-feature-discovery                        | v0.18.3 |          |
| SR-IOV Device Plugin        | Community (OSS) | ghcr.io/k8snetworkplumbingwg                                       | sriov-network-device-plugin                   | v3.11.0 |          |
| Etcd Defrag                 | Community (OSS) | ghcr.io/ahrtr                                                      | etcd-defrag                                   | v0.22.0 | Optional |
| Kamaji                      | Community (OSS) | ghcr.io/nvidia                                                     | kamaji                                        | v1.36.0-26.6.4 | Conditional |
| Local Path Provisioner      | Community (OSS) | docker.io/rancher                                                  | local-path-provisioner                        | v0.0.34 | Conditional |
| OpenBao                     | Community (OSS) | quay.io/openbao                                                    | openbao                                       | v2.5.5  | Optional |
| External Secrets            | Community (OSS) | ghcr.io/external-secrets                                           | external-secrets                              | v2.7.0  | Optional |
| Kube State Metrics          | Community (OSS) | registry.k8s.io/kube-state-metrics                                 | kube-state-metrics                            | v2.18.0 | Optional |
| Node Problem Detector       | Community (OSS) | registry.k8s.io/node-problem-detector                              | node-problem-detector                         | v1.35.1 | Optional |
| Prometheus                  | Community (OSS) | quay.io/prometheus                                                 | prometheus                                    | v2.54.1 | Optional |
| Grafana                     | Community (OSS) | docker.io/grafana                                                  | grafana                                       | 11.1.0  | Optional |

`Conditional` means the component is required for the default installation described in the user guides, but can be
replaced in custom deployments.

## Tested Network Adapters

The following NVIDIA BlueField 3 DPU models are recommended for DPF:

* [B3240](https://docs.nvidia.com/networking/display/bf3dpu/specifications#src-2449222537_Specifications-B3240DPUsSpecifications)
* [B3220](https://docs.nvidia.com/networking/display/bf3dpu/specifications#src-2449222537_Specifications-B3220DPUsSpecifications)
* [B3210](https://docs.nvidia.com/networking/display/bf3dpu/specifications#src-2449222537_Specifications-B3210DPUsSpecifications)

> [!NOTE]
> Some BlueField-3 DPU models require a PCIe ATX power cable. Install each DPU according to its adapter installation guide
> and connect the required power cable before starting DPF deployment. If the power cable is missing, the DPU may not boot
> or may not be discoverable through BMC or Redfish.

## BlueField BFB Support Matrix

The following matrix shows which BFB versions are supported with each DPF release.

| BFB Version | DPF v25.10 | DPF v26.4 |
|-------------|------------|-----------|
| 3.2 (LTS)   | ✅          | ☑️        |
| 3.4 (Apr)   | ❌          | ✅         |

* ✅: The BFB version is fully tested and supported with this DPF release.
* ☑️: DPUs running this BFB version are allowed to operate under this DPF release. However, in a support case,
  updating to the latest supported BFB will be required before further investigation.
* ❌: This combination is not supported.

> [!NOTE]
> Non-LTS BFB versions (3.3, 3.5) are only supported with their corresponding DPF release and are not carried forward.
> DPF supports the latest LTS BFB version and allows upgrades from LTS to LTS. Older LTS versions are permitted to
> operate but are considered unsupported. In a support case, updating to the latest supported BFB will be required
> before further investigation.

## BlueField DPU Requirements

| Component      | Minimum Version | Notes                                                                                                |
|----------------|-----------------|------------------------------------------------------------------------------------------------------|
| DPU Firmware   | 32.38.1002      | Required for DPU provisioning and management                                                         |
| MFT (Mellanox Firmware Tools) | 4.33.0-169 | Host-side tools required for DPU configuration and firmware management. Download from [NVIDIA Network Adapter Firmware Tools](https://network.nvidia.com/products/adapter-software/firmware-tools/) |

## Tested Operating Systems and Kubernetes Versions

NVIDIA DPF is validated with the oldest supported Kubernetes version. Newer Kubernetes versions in the supported range
are also supported.

| Operating System   | Kubernetes Version | Notes                               |
|--------------------|--------------------|-------------------------------------|
| Ubuntu 24.04 LTS   | 1.33               | Oldest supported Kubernetes version |

## Tested Container Runtimes

NVIDIA DPF has been validated in the following scenarios:

| Operating System | Containerd | CRI-O | Notes |
|------------------|------------|-------|-------|
| Ubuntu 24.04 LTS | Yes        | No    |       |

## Limitations

* **Socket Direct environments are not supported.** DPF does not currently support environments where NVIDIA Mellanox [Socket Direct](https://www.nvidia.com/en-us/networking/ethernet/socket-direct/) adapters are used. Socket Direct is a network adapter architecture that provides direct PCIe access from multiple CPU sockets to a single NIC, bypassing the inter-processor bus. DPUs in Socket Direct configurations are not tested or validated with DPF.
