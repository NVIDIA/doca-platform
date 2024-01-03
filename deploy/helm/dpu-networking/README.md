# dpu-networking

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| file://charts/multus | multus | 0.1.0 |
| file://charts/nvidia-k8s-ipam | nvidia-k8s-ipam | 0.1.0 |
| file://charts/ovs-cni | ovs-cni | 0.1.0 |
| file://charts/ovs-helper | ovs-helper | 0.1.0 |
| file://charts/servicechainset-controller | servicechainset-controller | 0.1.0 |
| file://charts/sfc-controller | sfc-controller | 0.1.0 |
| file://charts/sriov-device-plugin | sriov-device-plugin | 0.1.0 |
| https://flannel-io.github.io/flannel | flannel | v0.25.1 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| flannel.enabled | bool | `false` |  |
| multus.enabled | bool | `false` |  |
| nvidia-k8s-ipam.enabled | bool | `false` |  |
| ovs-cni.enabled | bool | `false` |  |
| ovs-helper.enabled | bool | `false` |  |
| servicechainset-controller.enabled | bool | `false` |  |
| sfc-controller.enabled | bool | `false` |  |
| sriov-device-plugin.enabled | bool | `false` |  |

