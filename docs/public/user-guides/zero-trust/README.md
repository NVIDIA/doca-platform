---
title: "Zero Trust Deployment"
---

[TOC]

## Overview

The **Zero Trust** deployment scenario represents DPF for zero trust environments, where the DPU is treated as a **host
accelerator card** and is **managed by DPF via BMC and Redfish**. In this deployment model, host machines remain outside
the DPF management cluster, while the DPU serves as an integrated acceleration resource that enhances the host's
capabilities.

This architecture provides secure isolation between the management plane and host systems. DPU services are managed
through Kubernetes APIs within the DPF cluster, while host workloads operate independently on their respective machines.

Redfish is used to manage the DPU hardware, including firmware updates and configuration changes.

## Use Cases

The Zero Trust model supports various use cases, each optimized for specific networking and acceleration requirements.

Each use case may require tailored configurations on the DPU for optimal performance. Below are the validated user
guides with corresponding deployment guides.

### HBN

* [Host Based Networking](use-cases/hbn/README.md)

| DPU Services                     | Comments                                                           |
|----------------------------------|--------------------------------------------------------------------|
| DOCA Host-Based Networking (HBN) | Accelerates underlay BGP routing with ECMP and EVPN-based overlays |

### VPC

* [Virtual Private Cloud](use-cases/vpc/README.md)

| DPU Services       | Comments                                         |
|--------------------|--------------------------------------------------|
| VPC OVN-Controller | OVN-Controller for VPC management and networking |
| VPC OVN-Node       | OVN-Node for VPC networking and connectivity     |

### HBN and SNAP Storage

* [HBN and SNAP Storage](use-cases/hbn-snap/README.md)

| DPU Services                     | Comments                                                           |
|----------------------------------|--------------------------------------------------------------------|
| DOCA Host-Based Networking (HBN) | Accelerates underlay BGP routing with ECMP and EVPN-based overlays |
| DOCA SNAP                        | Provides SNAP storage services for the DPUs                        |

### Passthrough

* [Passthrough](use-cases/passthrough/README.md)

No DPUServices are necessary for this use case.

## Prerequisites

Before deploying DPF in zero trust mode, ensure you have the necessary hardware and software prerequisites in place.
This includes compatible DPU hardware, host operating system configurations, and network settings. For detailed
requirements, refer to the [prerequisites guide](prerequisites/README.md).

## Deployment Steps

To deploy DPF in zero trust mode:

1. **Setup Prerequisites**: Ensure all hardware, software, and network requirements are met
2. **Choose Use Case**: Select the appropriate deployment scenario based on your requirements
3. **Follow Guide**: Use the specific use case documentation for step-by-step deployment
4. **Verify Deployment**: Validate the installation and perform basic functionality tests

## Support and Troubleshooting

For assistance with zero trust deployments:

* Review the specific use case documentation
* Check the [DPF troubleshooting guide](../../troubleshooting/README.md)
* Consult the [release notes](../../release-notes/README.md) for known issues
* Engage with the NVIDIA support team for further assistance

## Next Steps

Once you are familiar with the zero trust concept, proceed to:

1. Review the [prerequisites](prerequisites/README.md).
2. Select an appropriate [use case](use-cases/README.md).
3. Begin your DPF zero trust deployment.
