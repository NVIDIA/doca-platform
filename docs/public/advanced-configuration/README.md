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
* [Provisioning CA Certificate Rotation](ca-certificate-rotation.md) - Runbook for rotating the provisioning CA with dual-trust convergence checks on BMC and DPU sides
* [Hostless DPU Provisioning](hostless-dpu-provisioning.md) - Configuration for provisioning DPUs without host-side discovery or host-side reboot handling
* [Secondary Network support for HBN-OVNK use case](secondary-networks/README.md) - Documentation for enabling secondary network support for Host Based Networking and OVN Kubernetes
* [Host Trusted Multi-DPU support OVN-Kubernetes and HBN Services](multi-dpu-ovnk-hbn.md) - Guide that describes how to target particular DPUs for provisioning and service orchestration of OVN-Kubernetes and HBN Services.
* [Per-DPU BMC Credentials](per-dpu-bmc-credentials.md) - Configuration for using unique BMC credentials per DPU device instead of the shared password
* [DPUFlavorTemplate: Per-DPU Flavor Rendering](dpuflavortemplate-per-dpu-config.md) - How to render a per-DPU DPUFlavor from a template using values in DPUDevice.spec.values
* [Kata Containers on Host Nodes](kata-containers.md) - Guide for running pods with Kata VM isolation on host worker nodes, including SR-IOV VF passthrough with DOCA hardware offload
* [Encrypted Secrets](encrypted-secrets.md) - Configuration for storing secret data in an external secret store and replicating it to the management cluster with External Secrets Operator
* [Kamaji Encryption at Rest](kamaji-encryption-at-rest.md) - Configuration for encrypting Secrets and ConfigMaps in Kamaji-managed DPU cluster etcd datastores
* [Telemetry Level Labels](telemetry-level-labels.md) - Per-DPU generation of the DOCA telemetry `level_labels.ini` from DPUDevice values with a DPUFlavorTemplate
* [Host OS Init Release](host-os-init-release.md) - Holding the host at the start of its OS initialization until the DPU reaches a chosen readiness gate, in Zero Trust deployments
