# Examples for DPF Storage service

This folder contains example manifest for DPF Storage service.

- **snap-controller** - contains manifests to deploy SNAP Controller
- **snap-csi-plugin** - contains manifests to deploy SNAP CSI Plugin
- **snap-dpu** - contains manifests to deploy components for DPU cluster:
    - DOCA SNAP
    - SNAP Node Driver
    - Example NVIDIA Vendor Plugin (compatible with SPDK-CSI)
    - configuration files for DPU cluster (RBAC objects, `StoragePolicy CR`, `StorageVendor CR`, etc.)
- **spkd-csi** - contains DPF compatible Helm chart for SPDK-CSI and example manifests for it. The SPDK-CSI acts as an example Vendor Plugin for DPF storage subsystem.
- **workload** - contains example workload for the Host cluster


Please refer to [Integration with the DPF Storage Subsystem](../../../docs/public/developer-guides/storage.md) for more details about the DPF storage subsystem architecture and the roles of each component.

_Note: The manifests in this folder are provided as a starting point for users to customize for their specific use cases. For detailed information on how to configure and deploy DPF Storage services, refer to the [DPF documentation](../../../docs)._
