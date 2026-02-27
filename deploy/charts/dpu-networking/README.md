# dpu-networking

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for Kubernetes

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| file://charts/cni-installer | cni-installer | 0.1.0 |
| file://charts/kube-state-metrics | kube-state-metrics | 0.1.0 |
| file://charts/multus | multus | 0.1.0 |
| file://charts/node-problem-detector | node-problem-detector | 0.1.0 |
| file://charts/nvidia-k8s-ipam | nvidia-k8s-ipam | 0.1.0 |
| file://charts/opentelemetry-collector | opentelemetry-collector | 0.1.0 |
| file://charts/ovs-cni | ovs-cni | 0.1.0 |
| file://charts/servicechainset-controller | servicechainset-controller | 0.1.0 |
| file://charts/sfc-controller | sfc-controller | 0.1.0 |
| file://charts/sriov-device-plugin | sriov-device-plugin | 0.1.0 |
| https://flannel-io.github.io/flannel | flannel | v0.27.2 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| cni-installer.enabled | bool | `false` |  |
| flannel.enabled | bool | `false` |  |
| flannel.flannel.image.tag | string | `"v0.27.2@sha256:782c111d55e850e8038cdaf35454987e6c170ae9b54d5b12895b02d45c908caf"` |  |
| flannel.flannel.image_cni.tag | string | `"v1.7.1-flannel1@sha256:cb3176a2c9eae5fa0acd7f45397e706eacb4577dac33cad89f93b775ff5611df"` |  |
| flannel.podCidr | string | `"10.244.0.0/14"` |  |
| kube-state-metrics.enabled | bool | `false` |  |
| multus.enabled | bool | `false` |  |
| node-problem-detector.enabled | bool | `false` |  |
| nvidia-k8s-ipam.enabled | bool | `false` |  |
| opentelemetry-collector.enabled | bool | `false` |  |
| ovs-cni.enabled | bool | `false` |  |
| servicechainset-controller.enabled | bool | `false` |  |
| sfc-controller.enabled | bool | `false` |  |
| sriov-device-plugin.enabled | bool | `false` |  |

