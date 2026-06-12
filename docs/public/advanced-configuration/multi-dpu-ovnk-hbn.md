---
title: "Host Trusted Multi-DPU Support OVN-Kubernetes and HBN Services"
---

> [!NOTE] **Tech Preview**: This feature is currently in technical preview and may be subject to changes in 
> future releases.

This guide describes how to use the `dpuDeviceSelector` to target particular DPUs. This approach supports multiple DPUs
and distributes OVN-Kubernetes, HBN, BlueMan and DTS services across them, providing more granular control over which
DPUs run specific services and allowing for better resource allocation, service isolation, and  multi-DPU scalability.

[[_TOC_]]

## Overview

OVN-Kubernetes serves as the primary CNI in host clusters. To achieve hardware acceleration for host workload pods, 
OVN-Kubernetes is split into two components: a host-side component and a DPU-side component. The acceleration workflow 
operates as follows:

1. The OVN-Kubernetes resource injector (running on the host) adds VF resource requests to host workload pods based on 
   the `dpf-ovn-kubernetes` NetworkAttachmentDefinition (NAD).
2. The SR-IOV Device Plugin allocates a VF from the host's physical function and attaches it to the pod.
3. The OVN-Kubernetes host component communicates VF information to the DPU component via pod annotations.
4. The OVN-Kubernetes DPU component identifies the corresponding VF representor and adds it to the OVS bridge on the 
   DPU, enabling hardware acceleration.

### Multi-DPU Constraint

**In multi-DPU setups (one host worker node with two or more DPUs), both OVN-Kubernetes and HBN (Host-Based Networking) 
must run on the same single DPU.** OVN-Kubernetes manages a single OVS bridge per host, so running it on multiple DPUs 
would create conflicting bridge configurations. When deployed together in the HBN+OVNK use case, HBN must be colocated 
with OVN-Kubernetes because they are integrated through service chains, which operate on a single DPU.

To maintain hardware acceleration in multi-DPU environments, the `NodeSRIOVDevicePluginConfig` must be linked
only to the DPU where OVN-Kubernetes runs (via the `dpuAnnotations` field in the DPUDeployment). The config
uses PF indices to specify VF ranges, so it is not affected by PF name or PCI address changes.
This configuration ensures that:

- The SR-IOV Device Plugin exposes VFs exclusively from the DPU running OVN-Kubernetes
- When the resource injector adds VF requests to pods, the SR-IOV Device Plugin allocates VFs only from that DPU
- The OVN-Kubernetes DPU component can properly add VF representors to OVS on the same DPU

### Using dpuDeviceSelector for Multi-DPU Deployments

The [OVN Kubernetes with Host Based Networking](../user-guides/host-trusted/use-cases/hbn-ovnk/README.md) guide 
uses only `dpuNodeSelector` to target nodes with DPUs, which deploys all services (OVN-Kubernetes, HBN, DTS, Blueman) to 
all DPUs on those nodes. This creates conflicts when multiple DPUs are present.

The `dpuDeviceSelector` approach solves this problem by enabling precise DPU targeting. It allows you to:

1. **Run OVN-Kubernetes and HBN together on a single DPU** (critical requirement - they are integrated through service chains)
2. Deploy other services (e.g., DTS, Blueman) on different DPUs to distribute workload
3. Target specific DPUs using custom labels or auto-generated device labels
4. Achieve better resource isolation and performance optimization across multiple DPUs

## Prerequisites

This guide is based on the [OVN Kubernetes with Host Based Networking](../user-guides/host-trusted/use-cases/hbn-ovnk/README.md) 
use case. Follow that guide until you reach the [DPU Provisioning and Service Installation](../user-guides/host-trusted/use-cases/hbn-ovnk/README.md#5-dpu-provisioning-and-service-installation) 
section where you are about to deploy the DPUDeployment. At that point, use this guide to modify the 
DPUDeployment for multi-DPU support instead of using the original single-DPU approach.

> [!IMPORTANT]
> **SR-IOV Device Plugin and OVN-Kubernetes Configuration Requirements**:  
> In multi-DPU setups, you must ensure that the `NodeSRIOVDevicePluginConfig` is linked only to the DPU
> running OVN-Kubernetes via the `dpuAnnotations` field in the DPUDeployment. The OVN-Kubernetes Helm
> chart uses `nodeMgmtPortDpResourceName` parameter to reference the management VF resource exposed by the device plugin,
> so no manual interface name configuration is needed.

1. **SR-IOV Device Plugin Configuration**: The `NodeSRIOVDevicePluginConfig` defines which VFs are exposed as device
plugin resources. It is linked to the correct DPU via the `noderesources.dpu.nvidia.com/nodesriovdevicepluginconfig`
annotation in the DPUDeployment's `dpuAnnotations`. Since the config uses PF indices (not PF names), it is not affected
by interface name changes after provisioning.

2. **OVN-Kubernetes Configuration**: The OVN-Kubernetes installation uses `nodeMgmtPortDpResourceName` to reference
the management VF by its device plugin resource name (e.g., `nvidia.com/ovnk-mgmt-vf`), and
`gatewayOpts: --gateway-interface=derive-from-mgmt-port` to automatically derive the gateway interface.

For detailed configuration examples, refer to the [OVN Kubernetes with Host Based Networking](../user-guides/host-trusted/use-cases/hbn-ovnk/README.md)
guide, specifically the [NodeSRIOVDevicePluginConfig](../user-guides/host-trusted/use-cases/hbn-ovnk/README.md#apply-the-nodesriovdevicepluginconfig)
and [OVN-Kubernetes Helm values](../user-guides/host-trusted/use-cases/hbn-ovnk/README.md#install-ovn-kubernetes-from-the-helm-chart) sections.

## Key Differences

### Node Selector

```yaml
dpuSets:
- nameSuffix: "dpuset1"
  dpuNodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
```

This targets all nodes that have the `feature.node.kubernetes.io/dpu-enabled: "true"` label, and all services 
run on all DPUs on those nodes.

### Both Node Selector and DPU Selector

```yaml
dpuSets:
- nameSuffix: "dpuset1"
  dpuNodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  dpuDeviceSelector:
    matchLabels:
      example.com/dpu-role: ovnk-hbn
```

This targets specific DPUs based on custom labels, allowing for precise service placement and
multi-DPU distribution.

## DPU Selection Labels

### Custom Labels (Recommended)

To ensure reliable and stable scheduling in complex multi-DPU environments, it is recommended to explicitly maintain
custom labels on DPUDevices and use them in `dpuDeviceSelector`. For example:

```bash
kubectl label dpudevice <dpudevice-name> -n dpf-operator-system example.com/dpu-role=ovnk-hbn
kubectl label dpudevice <dpudevice-name> -n dpf-operator-system example.com/dpu-role=other-services
```

> [!NOTE]
> The `example.com/dpu-role` label is used here as an example. Replace `example.com` with your organization's
> domain and adjust the label key and values to fit your environment.

Then use these labels in `dpuDeviceSelector`:

```yaml
dpuDeviceSelector:
  matchLabels:
    example.com/dpu-role: ovnk-hbn
```

The examples in this guide use `example.com/dpu-role` labels to demonstrate multi-DPU deployment with two DPUs:

- `example.com/dpu-role: ovnk-hbn` for the DPU running OVN-Kubernetes and HBN services
- `example.com/dpu-role: other-services` for the DPU running Blueman and DTS services

You can extend this pattern to support additional DPUs by creating more DPUDeployments with different
`dpuDeviceSelector` values. Remember that OVN-Kubernetes should only run on one DPU per host.

### Auto-generated DPUDevice Labels

DPUDevices are also automatically labeled with device-specific information that can be used in `dpuDeviceSelector`:

- `provisioning.dpu.nvidia.com/dpudevice-name`: The name of the DPUDevice
- `provisioning.dpu.nvidia.com/dpudevice-num-of-pfs`: The number of PFs on the DPU device
- `provisioning.dpu.nvidia.com/dpudevice-pciAddress`: The PCI address of the DPU device
- `provisioning.dpu.nvidia.com/dpudevice-pf0-name`: The name of PF0 on the DPU device
- `provisioning.dpu.nvidia.com/dpunode-name`: The name of the DPUNode the DPU is part of

Be aware of the following limitations before relying on auto-generated labels:

- **Label stability**: Labels like `dpudevice-pf0-name` and `dpudevice-pciAddress` reflect the device's current state.
  Certain firmware settings (for example, `PCI_SWITCH_EMULATION_ENABLE`) can change PF interface names and PCI addresses
  after DPU provisioning and host reboot. When this happens, the auto-generated labels may no longer match the values
  used in `dpuDeviceSelector`.
- **Heterogeneous clusters**: In clusters where nodes have different DPU configurations, auto-generated labels may
  produce inconsistent or unexpected selector matches.

## Example: Multi-DPU Deployment with dpuDeviceSelector

This section shows how to modify the existing DPUDeployment from the [OVN Kubernetes with Host Based 
Networking](../user-guides/host-trusted/use-cases/hbn-ovnk/README.md) guide and create an additional one for 
multi-DPU support.

### Step 1: Modify the Existing DPUDeployment for OVN and HBN Services

**Instead of creating a new DPUDeployment, modify the existing one from the OVN-Kubernetes HBN guide.** Simply 
add a `dpuDeviceSelector` to the existing `ovn-hbn` DPUDeployment:

> [!NOTE] We removed the serviceChains section for simplicity. You should keep the serviceChains section if you have
> defined any service chains in your original DPUDeployment.

**Modified DPUDeployment:**
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: ovn-hbn
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavor: hbn-ovn
    dpuSets:
      - nameSuffix: "dpuset1"
        dpuNodeSelector:
          matchLabels:
            feature.node.kubernetes.io/dpu-enabled: "true"
        dpuDeviceSelector:
          matchLabels:
            example.com/dpu-role: ovnk-hbn
        dpuAnnotations:
          noderesources.dpu.nvidia.com/nodesriovdevicepluginconfig: bf3-p0-vfs
  services:
    ovn:
      serviceTemplate: ovn
      serviceConfiguration: ovn
    hbn:
      serviceTemplate: hbn
      serviceConfiguration: hbn
  serviceChains:
...
```

### Step 2: Create Additional DPUDeployment for Blueman and DTS Services

Create a **new, additional** DPUDeployment for Blueman and DTS services that targets the second DPU. This new
DPUDeployment must use a different `dpuDeviceSelector` to target the second DPU.

#### Creating a Separate DPUFlavor

The second DPU requires a slightly different `DPUFlavor` without the `hostNetworkInterfaceConfigs` section. 

> [!NOTE]  
> **When to use `hostNetworkInterfaceConfigs`:**  
> The `hostNetworkInterfaceConfigs` section (which configures DHCP and MTU settings for host-side network 
> interfaces) is only needed for the DPU that handles host networking (the one running OVN-Kubernetes and HBN). 
> Since the second DPU only runs Blueman and DTS services, it does not need to configure the host network 
> interfaces, and therefore the `hostNetworkInterfaceConfigs` section should be omitted.

To create the new flavor, modify the existing `dpuflavor-hbn-ovn.yaml` file by removing the 
`hostNetworkInterfaceConfigs` section as shown below and save it as a new file (e.g., 
`dpuflavor-other-services.yaml`) with a new `metadata.name`:

> [!TIP]  
> The diff below shows the path from the documentation repository for reference. Use your actual file location 
> when making these changes.

```diff
--- a/docs/public/user-guides/host-trusted/use-cases/hbn-ovnk/manifests/05-dpudeployment-installation/dpuflavor-hbn-ovn.yaml
+++ b/docs/public/user-guides/host-trusted/use-cases/hbn-ovnk/manifests/05-dpudeployment-installation/dpuflavor-hbn-ovn.yaml
@@ -2,7 +2,7 @@
 apiVersion: provisioning.dpu.nvidia.com/v1alpha1
 kind: DPUFlavor
 metadata:
-  name: hbn-ovn
+  name: hbn-ovn-other-services
   namespace: dpf-operator-system
 spec:
   grub:
@@ -74,11 +74,6 @@ spec:
     - UPDATE_DPU_OS=yes
     - WITH_NIC_FW_UPDATE=yes

-  hostNetworkInterfaceConfigs:
-    - portNumber: 0
-      dhcp: true
-      mtu: 1500
-
   configFiles:
   - path: /etc/mellanox/mlnx-bf.conf
     operation: override
```

#### Creating the DPUDeployment

```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: blueman-dts
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavor: hbn-ovn-other-services
    dpuSets:
      - nameSuffix: "dpuset1"
        dpuNodeSelector:
          matchLabels:
            feature.node.kubernetes.io/dpu-enabled: "true"
        dpuDeviceSelector:
          matchLabels:
            example.com/dpu-role: other-services
  services:
    dts:
      serviceTemplate: dts
      serviceConfiguration: dts
    blueman:
      serviceTemplate: blueman
      serviceConfiguration: blueman
```

## Deployment Steps

### 1. Prepare Configuration Files

Create the modified DPUDeployment files based on the examples above.

### 2. Modify the Existing DPUDeployment

Update the existing `ovn-hbn` DPUDeployment to use `dpuDeviceSelector` along with `dpuNodeSelector`:

```bash
# Apply the modified DPUDeployment (replace the existing one)
kubectl apply -f dpudeployment-ovn-hbn-modified.yaml
```

### 3. Deploy Additional DPUDeployment for Blueman and DTS

Create and apply the new DPUDeployment for Blueman and DTS services:

```bash
kubectl apply -f dpudeployment-blueman-dts.yaml
```

### 4. Verify Deployment

You can monitor prevalidation status using either [dpfctl](../operational-readiness/troubleshooting/dpfctl/README.md) or kubectl:

**Using dpfctl (recommended):**

```bash
# Check overall DPF status
dpfctl describe all

# Check specific conditions for troubleshooting
dpfctl describe all --show-conditions=all
```

**Using kubectl:**

```bash
# Check DPUDeployments
kubectl get dpudeployments -n dpf-operator-system

# Check DPUServices
kubectl get dpuservices -n dpf-operator-system
```
