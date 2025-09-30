---
title: "Platform Support"
---

[TOC]

## Prerequisites

| Component  | Version   | Notes                                                                                                            |
|------------|-----------|------------------------------------------------------------------------------------------------------------------|
| Kubernetes | 1.32-1.34 | All currently supported upstream Kubernetes releases are supported                                               |
| Helm       | v3.5+     | For information and methods of Helm installation, please refer to the official [Helm Website](https://helm.sh/). |

## DPF Component Dependencies

The following component versions are deployed as part of DPF:

| Component                   | Version | Notes    |
|-----------------------------|---------|----------|
| ArgoCD                      | v2.14.2 |          |
| Cert Manager                | v1.18.1 |          |
| Flannel                     | v0.26.5 |          |
| Multus                      | v3.9.3  |          |
| NVIDIA K8s IPAM             | v0.3.5  |          |
| NVIDIA Maintenance Operator | 0.2.0   |          |
| NVIDIA Network Operator     | v25.7.0 |          |
| Node Feature Discovery      | 0.17.1  |          |
| SR-IOV Device Plugin        | v3.9.0  |          |
| Etcd Defrag                 | v0.22.0 | Optional |
| Grafana                     | 8.4.6   | Optional |
| Kamaji                      | v1.34.0 | Optional |
| Keepalived                  | 2.0.20  | Optional |
| Kube State Metrics          | 5.25.1  | Optional |
| Local Path Provisioner      | 0.0.31  | Optional |
| Prometheus                  | 25.26.0 | Optional |

## Tested Network Adapters

The following NVIDIA BlueField 3 DPU models are recommended for DPF:

* [B3240](https://docs.nvidia.com/networking/display/bf3dpu/specifications#src-2449222537_Specifications-B3240DPUsSpecifications)
* [B3220](https://docs.nvidia.com/networking/display/bf3dpu/specifications#src-2449222537_Specifications-B3220DPUsSpecifications)
* [B3210](https://docs.nvidia.com/networking/display/bf3dpu/specifications#src-2449222537_Specifications-B3210DPUsSpecifications)

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
