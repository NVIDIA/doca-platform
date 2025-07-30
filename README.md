# DOCA Platform Framework (DPF)

## Overview
DOCA Platform Framework (DPF) is a system that provisions and orchestrates [NVIDIA BlueField DPUs](https://www.nvidia.com/en-gb/networking/products/data-processing-unit/) in cloud environments.

## Key Features
- **Kubernetes Integration**: Streamlines DPU provisioning and management via Kubernetes APIs.
- **DPU Service Orchestration**: Enables efficient deployment and orchestration of services on DPUs.
- **Cluster Management**: Simplifies the management of DPUs within Kubernetes clusters.

## Hardware support
DPF supports dual port NVIDIA BlueField-3 DPUs.

BlueField DPUs are installed as PCI devices in servers to manage various types of network traffic, such as data center communication and storage traffic, through dedicated ports. They feature arm64 CPUs running Linux OS and [NVIDIA DOCA Software](https://developer.nvidia.com/networking/doca) components.

Refer to [Prerequisites](docs/public/user-guides/prerequisites/prerequisites.md) for detailed hardware requirements.

## Documentation

- [System overview](docs/public/architecture/system_overview.md): High-level description of DPF components and functionality.

- [System architecture](docs/public/architecture/component_description.md): Detailed explanation of DPF components and their interactions.

- [User guide](docs/public/user-guides/README.md): Guides on installing and utilizing DPF effectively for specific use-cases.

- [Release notes](docs/public/release-notes/README.md): Features and known issues for specific DPF versions.

## API reference

Comprehensive DPF API documentation is available [here](docs/public/api/README.md).
