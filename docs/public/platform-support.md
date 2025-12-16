---
title: "Platform Support"
---

[TOC]

## Prerequisites

| Component  | Version   | Notes                                                                                                            |
|------------|-----------|------------------------------------------------------------------------------------------------------------------|
| Kubernetes | 1.32-1.34 | All currently supported upstream Kubernetes releases are supported                                               |
| Helm       | v3.5+     | For information and methods of Helm installation, please refer to the official [Helm Website](https://helm.sh/). |

## DPF Component Matrix

DPF uses the following components:

| Component                   | Origin          | Repository                                                         | Image Name                                    | Tag     | Notes    |
|-----------------------------|-----------------|--------------------------------------------------------------------|-----------------------------------------------|---------|----------|
| ArgoCD                      | Community (OSS) | quay.io/argoproj                                                   | argocd                                        | v2.14.2 |          |
| Cert Manager Controller     | Community (OSS) | quay.io/jetstack                                                   | cert-manager-controller                       | v1.18.1 |          |
| Flannel                     | Community (OSS) | docker.io/flannel                                                  | flannel                                       | v0.26.5 |          |
| Multus                      | Community (OSS) | ghcr.io/k8snetworkplumbingwg                                       | multus-cni                                    | v3.9.3  |          |
| NVIDIA K8s IPAM             | Community (OSS) | ghcr.io/mellanox                                                   | nvidia-k8s-ipam                               | v0.3.5  |          |
| NVIDIA Maintenance Operator | Community (OSS) | ghcr.io/mellanox                                                   | maintenance-operator                          | v0.2.2  |          |
| NVIDIA Network Operator     | Community (OSS) | nvcr.io/nvidia/cloud-native                                        | network-operator                              | v25.7.0 |          |
| Node Feature Discovery      | Community (OSS) | registry.k8s.io/nfd                                                | node-feature-discovery                        | v0.17.1 |          |
| SR-IOV Device Plugin        | Community (OSS) | ghcr.io/k8snetworkplumbingwg                                       | sriov-network-device-plugin                   | v3.11.0  |          |
| Etcd Defrag                 | Community (OSS) | ghcr.io/ahrtr                                                      | etcd-defrag                                   | v0.22.0 | Optional |
| Kamaji                      | Community (OSS) | ghcr.io/clastix                                                    | kamaji                                        | v1.34.0 | Optional |
| Keepalived                  | Community (OSS) | docker.io/osixia                                                   | keepalived                                    | 2.0.20  | Optional |
| Local Path Provisioner      | Community (OSS) | docker.io/rancher                                                  | local-path-provisioner                        | v0.0.31 | Optional |
| Kube State Metrics          | Community (OSS) | registry.k8s.io/kube-state-metrics                                 | kube-state-metrics                            | v2.13.0 | Optional |
| Prometheus                  | Community (OSS) | quay.io/prometheus                                                 | prometheus                                    | v2.54.1 | Optional |
| Grafana                     | Community (OSS) | docker.io/grafana                                                  | grafana                                       | 11.1.0  | Optional |

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
