---
title: "Advanced Configuration"
---

[[_TOC_]]

This page contains advanced configuration documentation for the DOCA Platform Framework (DPF). These guides cover advanced topics and configurations for power users and administrators.

## Documentation Sections

* [DPU Services](dpuservices/README.md) - Documentation for DPF DPU services including Argus, Blueman, Firefly, DOCA Telemetry Service, and Kata Containers
* [Host Trusted with Non Kubernetes Workers](host-installation-for-non-k8s-env/README.md) - Installation guides for DPF Host Trusted with workers that are not part of a Kubernetes cluster
* [Using Private Registries](using-private-registries.md) - Configuration for using private container registries
* [Zero Trust Advanced Configuration](zero-trust-advanced-configuration.md) - Advanced configuration for the DPF Zero Trust
* [Hostless DPU Provisioning](hostless-dpu-provisioning.md) - Configuration for provisioning DPUs without host-side discovery or host-side reboot handling
* [Secondary Network support for HBN-OVNK use case](secondary-networks/README.md) - Documentation for enabling secondary network support for Host Based Networking and OVN Kubernetes
* [Host Trusted Multi-DPU support OVN-Kubernetes and HBN Services](multi-dpu-ovnk-hbn.md) - Guide that describes how to target particular DPUs for provisioning and service orchestration of OVN-Kubernetes and HBN Services.
* [Per-DPU BMC Credentials](per-dpu-bmc-credentials.md) - Configuration for using unique BMC credentials per DPU device instead of the shared password
* [Kata Containers on Host Nodes](kata-containers.md) - Guide for running pods with Kata VM isolation on host worker nodes, including SR-IOV VF passthrough with DOCA hardware offload
