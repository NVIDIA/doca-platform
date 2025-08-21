---
title: "DOCA Platform Framework"
---

[TOC]

> [!NOTE] The source for this documentation is the GitHub repo at [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform/) under `docs/public/`.

## Introduction

DOCA Platform Framework (DPF) is a system that provisions and orchestrates [NVIDIA BlueField DPUs](https://www.nvidia.com/en-gb/networking/products/data-processing-unit/) in cloud environments.

### Key Features

* **Kubernetes Integration**: Streamlines DPU provisioning and management via Kubernetes APIs
* **DPU Service Orchestration**: Enables efficient deployment and orchestration of services on DPUs
* **Cluster Management**: Simplifies the management of DPUs within Kubernetes clusters

### Hardware Support

DPF supports dual port NVIDIA BlueField-3 DPUs. BlueField DPUs are installed as PCI devices in servers to manage various types of network traffic, such as data center communication and storage traffic, through dedicated ports. They feature arm64 CPUs running Linux OS and [NVIDIA DOCA Software](https://developer.nvidia.com/networking/doca) components.

The DOCA Platform Framework (DPF) documentation is organized into the following sections:

* [Getting Started](getting-started/README.md) - Quick start guide and initial setup instructions
* [API](developer-guides/api/README.md) - API reference documentation
* [Architecture](developer-guides/architecture/README.md) - System architecture and design documentation
* [Storage Developer Guide](developer-guides/services/storage.md) - Documentation for Storage development and configuration
* [DPUServices](advanced-configuration/dpuservices/README.md) - Documentation for DPF DPU services
* [DPUService Developer Guide](developer-guides/services/dpuservice-development.md) - Documentation for DPUService development and configuration
* [Troubleshooting](troubleshooting/README.md) - Documentation for troubleshooting and debugging DPF
* [User Guides](user-guides/README.md) - Documentation for DPF users
* [Release Notes](release-notes/README.md) - Release notes and version history
* [Platform Support](platform-support.md) - Hardware requirements and supported platforms
