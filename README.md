# DOCA Platform Framework

DOCA Platform Framework (DPF) is a system that orchestrates NVIDIA Data Processing Units (DPU) using a Kubernetes API. It provisions and manages DPU devices and orchestrates specialized DPUServices which run on those devices.

DPF manages DPUs installed in nodes that are part of a Kubernetes cluster.

- [System overview](docs/system_overview.md) contains a high level description of the components and functionality of DPF.

- [Use cases](docs/usecases/readme.md) show how to install DPF and what to use it for.

- [System architecture](docs/system.md) describes the workings of the DPF system components in detail.

## Hardware
DPF enables NVIDIA [Bluefield DPUs](https://www.nvidia.com/en-gb/networking/products/data-processing-unit/). These devices are installed in servers as PCI devices and handle network traffic through network ports. Bluefield DPUs have arm64 CPUs and run a standard Linux OS.

DPF supports all Bluefield 3 DPUs with 32GB of RAM.

## API reference

The DPF API is documented [here](docs/api.md).

## Contributing

This repository follows the following [conventions](CONVENTIONS).