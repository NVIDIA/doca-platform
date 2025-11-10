---
title: "Host Trusted Deployment"
---

[TOC]

## Overview

The **Host Trusted** deployment scenario represents DPF for Host Trusted environments,
where the DPU is treated as a **host accelerator card** and is **managed by the host**.
In this deployment model, host machines function as worker nodes within the DPF
management cluster, with the DPU serving as an integrated acceleration resource
that enhances the host's capabilities.

This architecture allows users to leverage standard Kubernetes APIs for managing
both host workloads and DPU services, creating an end to end Kubernetes-native experience.

The DOCA Management Service(DMS) is used to install and configure the DPU.

## Use Cases

The Host trusted model supports various use cases, each optimized for specific
networking and acceleration requirements.

Each use case may require tailored configurations on the DPU for optimal performance.
Below are the validated user guides with corresponding deployment guides.

### HBN and OVN Kubernetes

* [Host Based Networking and OVN Kubernetes](use-cases/hbn-ovnk/README.md)

| DPU Services                     | Comments                                                  |
|----------------------------------|-----------------------------------------------------------|
| DOCA Host-Based Networking (HBN) | Accelerates underlay BGP routing with ECMP from the DPU   |
| OVN Kubernetes with DPU offload  | Provides SDN overlay services and Kubernetes CNI offloads |
| DOCA Telemetry Service (DTS)     | Provides enhanced DPU telemetry                           |
| DOCA BlueMan                     | Offers a user-friendly GUI for DTS                        |

### OVN Kubernetes

* [OVN Kubernetes on its own](use-cases/ovnk/README.md)

| DPU Services                    | Comments                                                  |
|---------------------------------|-----------------------------------------------------------|
| OVN Kubernetes with DPU offload | Provides SDN overlay services and Kubernetes CNI offloads |
| DOCA Telemetry Service (DTS)    | Provides enhanced DPU telemetry                           |
| DOCA BlueMan                    | Offers a user-friendly GUI for DTS                        |

### HBN

* [Host Based Networking on its own](use-cases/hbn/README.md)

| DPU Services                     | Comments                                                           |
|----------------------------------|--------------------------------------------------------------------|
| DOCA Host-Based Networking (HBN) | Accelerates underlay BGP routing with ECMP and EVPN-based overlays |

### DPU Passthrough

* [DPU Passthrough](use-cases/passthrough/README.md)

No DPUServices are necessary for this use case.

### HBN and SNAP Storage

* [Host Based Networking and SNAP Storage](use-cases/hbn-snap/README.md)

| DPU Services                     | Comments                                                           |
|----------------------------------|--------------------------------------------------------------------|
| DOCA Host-Based Networking (HBN) | Accelerates underlay BGP routing with ECMP and EVPN-based overlays |
| DOCA SNAP                        | Provides shared storage for Kubernetes workloads                   |

### HBN and OVN Kubernetes and SNAP Storage

* [Host Based Networking and OVN Kubernetes and SNAP Storage](use-cases/hbn-ovnk-snap/README.md)

| DPU Services                     | Comments                                                           |
|----------------------------------|--------------------------------------------------------------------|
| DOCA Host-Based Networking (HBN) | Accelerates underlay BGP routing with ECMP and EVPN-based overlays |
| OVN Kubernetes with DPU offload  | Provides SDN overlay services and Kubernetes CNI offloads          |
| DOCA Telemetry Service (DTS)     | Provides enhanced DPU telemetry                                    |
| DOCA BlueMan                     | Offers a user-friendly GUI for DTS                                 |
| DOCA SNAP                        | Provides shared storage for Kubernetes workloads                   |

## Prerequisites

Before deploying DPF in Host Trusted mode, ensure you have the necessary hardware
and software prerequisites in place. This includes compatible DPU hardware, host
operating system configurations, and network settings. For detailed requirements,
refer to the [prerequisites guide](prerequisites/README.md).

## Support and Troubleshooting

For assistance with Host Trusted deployments:

* Review the specific use case documentation
* Check the [DPF troubleshooting guide](../../troubleshooting/README.md)
* Consult the [release notes](../../release-notes/README.md) for known issues
* Engage with the NVIDIA support team for further assistance

## Next Steps

Once you've familiarized yourself with the Host Trusted concept, proceed to:

1. Review the [prerequisites](prerequisites/README.md)
2. Select an appropriate [use case](use-cases/README.md)
3. Begin your DPF Host Trusted deployment
