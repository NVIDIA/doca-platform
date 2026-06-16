---
title: "DPUServices"
---

[[_TOC_]]

This section contains documentation for the DPU services that can be deployed and managed by the DOCA Platform Framework
(DPF). Each service has its own documentation, deployment guides, and configuration details:

* **Firefly**: DOCA Firefly service for secure PTP synchronization
    * [Firefly User Guide](firefly.md)
* **BlueMan**: DOCA BlueMan service for comprehensive DPU management
    * [BlueMan User Guide](blueman.md)
* **Telemetry Service (DTS)**: DOCA Telemetry Service for monitoring and metrics collection
    * [DTS User Guide](doca-telemetry-service.md)
* **Host-Based Networking (HBN)**: DOCA Host-Based Networking service for advanced networking capabilities
    * [HBN User Guide (Host Trusted)](../../user-guides/host-trusted/use-cases/hbn/README.md)
    * [HBN User Guide (Zero Trust)](../../user-guides/zero-trust/use-cases/hbn/README.md)
* **Argus**: DOCA Argus Service for Container Threat Detection
    * [Argus User Guide](argus.md)
* **Storage Services**: Storage-related services including SNAP and SPDK CSI for storage management and virtualization
* **OVN VPC Service**: DOCA VPC OVN service provides accelerated Virtual Private Cloud (VPC) networking with network
  isolation and advanced SDN capabilities using [Open Virtual Network (OVN)](https://www.ovn.org/en/)
    * [OVN VPC Deployment Guide](../../user-guides/zero-trust/use-cases/vpc/README.md)
    * [OVN VPC Service Guide](./ovnvpc/README.md)
* **Example Services**: Reference implementations and test services for development and validation

For implementation details of these services, refer to the `dpuservices` directory in the repository root.
