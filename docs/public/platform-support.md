---
title: "Platform Support"
---

[TOC]

## Prerequisites

| Component  | Version | Notes                                                                                                            |
|------------|---------|------------------------------------------------------------------------------------------------------------------|
| Kubernetes | 1.30    |                                                                                                                  |
| Helm       | v3.5+   | For information and methods of Helm installation, please refer to the official [Helm Website](https://helm.sh/). |

## DPF Component Dependencies

The following component versions are deployed as part of DPF:

| Component                   | Version | Notes    |
|-----------------------------|---------|----------|
| ArgoCD                      | 7.8.2   |          |
| Node Feature Discovery      | 0.17.1  | Optional |
| Kamaji etcd                 | 0.9.0   | Optional |
| Kamaji                      | 1.0.0   | Optional |
| NVIDIA Maintenance Operator | 0.2.0   |          |
| Kube State Metrics          | 5.25.1  | Optional |
| Grafana                     | 8.4.6   | Optional |
| Prometheus                  | 25.26.0 | Optional |
| Parca                       | 4.19.0  | Optional |
| Multus                      | v3.9.3  |          |
| SR-IOV Device Plugin        | v3.6.2  |          |
| NVIDIA K8s IPAM             | v0.3.5  |          |
| Flannel                     | v0.26.5 |          |


## Tested Network Adapters

The following NVIDIA BlueField 3 DPU models are recommended for DPF:
- [B3240](https://docs.nvidia.com/networking/display/bf3dpu/specifications#src-2449222537_Specifications-B3240DPUsSpecifications)
- [B3220](https://docs.nvidia.com/networking/display/bf3dpu/specifications#src-2449222537_Specifications-B3220DPUsSpecifications)
- [B3210](https://docs.nvidia.com/networking/display/bf3dpu/specifications#src-2449222537_Specifications-B3210DPUsSpecifications)

## Tested Operating Systems and Kubernetes Versions

NVIDIA DPF has been validated in the following scenarios:

| Operating System | Kubernetes | Notes |
|------------------|------------|-------|
| Ubuntu 24.04 LTS | 1.30       |       |

## Tested Container Runtimes

NVIDIA DPF has been validated in the following scenarios:

| Operating System | Containerd | CRI-O | Notes |
|------------------|------------|-------|-------|
| Ubuntu 24.04 LTS | Yes        | No    |       |
