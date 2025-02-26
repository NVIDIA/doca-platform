# DOCA Platform Framework (DPF)

## System requirements

Before deploying DPF, ensure the following prerequisites are met:
- [DPF System Setup Prerequisites](prerequisites.md)
- [Host Network Configuration Prerequisites](host-network-configuration-prerequisite.md)

## User guide

DPF enables a range of deployment scenarios by combining specific DPU services into service chains. These chains provide flexibility and efficiency for complex networking workflows. 

Each service combination may require tailored configurations on the DPU for optimal performance. Below are the validated user guides with corresponding deployment guides.

### HBN and OVN

- [Host Based Networking and OVN Kubernetes](hbn_ovn/)

| DPU Services                    | Comments                                                  |
|---------------------------------|-----------------------------------------------------------|
| DOCA Host-Based Networking (HBN)| Accelerates underlay BGP routing with ECMP from the DPU   |
| OVN-Kubernetes with DPU offload | Provides SDN overlay services and Kubernetes CNI offloads |
| DOCA Telemetry Service (DTS)    | Provides enhanced DPU telemetry                           |
| DOCA BlueMan                    | Offers a user-friendly GUI for DTS                        |

### OVN Only

- [OVN Kubernetes on its own](ovn_only/)

| DPU Services                    | Comments                                                        |
|---------------------------------|-----------------------------------------------------------------|
| OVN-Kubernetes with DPU offload | Provides SDN overlay services and Kubernetes CNI offloads       |
| DOCA Telemetry Service (DTS)    | Provides enhanced DPU telemetry                                 |
| DOCA BlueMan                    | Offers a user-friendly GUI for DTS                              |

### HBN Only

- [Host Based Networking on its own](hbn_only/)

| DPU Services                    | Comments                                                            |
|---------------------------------|---------------------------------------------------------------------|
| DOCA Host-Based Networking (HBN)| Accelerates underlay BGP routing with ECMP and EVPN-based overlays  |
