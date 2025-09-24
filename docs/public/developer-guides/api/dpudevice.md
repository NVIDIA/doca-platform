---
title: "DPUDevice"
---

[TOC]

The DPUDevice is a Kubernetes CRD that represents a physical DPU (Data Processing Unit) device that was discovered.
The DPUDevice contain all the information required to identify and provision the DPU by the DPU Controller.

## Overview

The DPUDevice resource serves as an inventory and management interface for physical DPU devices. It contains device-specific information such as serial numbers, product identifiers, BMC (Base Management Controller) details, and PCI addresses.
The DPUDevice is can be created automatically through discovery processes or manually by administrators.

## DPUDevice Specification

### DPUDeviceSpec

The `spec` section defines the desired configuration for the DPU device:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `serialNumber` | string | Yes | The serial number of the device for inventory management |
| `psid` | string | No | Product Serial ID (deprecated, use status.psid) |
| `opn` | string | No | Ordering Part Number (deprecated, use status.opn) |
| `bmcIp` | string | No | IP address of the BMC for remote management |
| `bmcPort` | uint32 | No | Port number for BMC communication (default: 443) |
| `numberOfPFs` | int | No | Number of Physical Functions on the device (default: 1) |
| `pf0Name` | string | No | Name of the first Physical Function |

### DPUDeviceStatus

The `status` section contains the observed state of the DPU device:

| Field | Type | Description |
|-------|------|-------------|
| `psid` | string | Product Serial ID discovered from the device |
| `serialNumber` | string | Serial number discovered from the device |
| `opn` | string | Ordering Part Number discovered from the device |
| `bmcIp` | string | BMC IP address discovered from the device |
| `bmcPort` | uint32 | BMC port discovered from the device |
| `pciAddress` | string | PCI address of the device in the host system |
| `pf0Mac` | string | MAC address of the first Physical Function |
| `conditions` | array | Array of condition objects describing device state |

## Conditions

The DPUDevice resource uses several condition types to track its state:

- **DpuDeviceDiscovered**: Indicates that the DPU has been discovered
- **DpuDeviceNodeAttached**: Indicates that the DPU is attached to a node
- **DpuDeviceInitialized**: Indicates that the DPU interface has been initialized
- **DpuDeviceError**: Indicates that the DPUDevice has an error
- **DpuDeviceReady**: Indicates that the DPUDevice is ready for use

## Example Usage

### Basic DPUDevice Creation
Determine the serial number of the DPUDevice. In zero-trust mode, serial number will be discovered from the BMC. In trusted mode, run: `lspci -vvs ${pci_address}  | grep "SN"`.

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: MT25066004C7
  namespace: dpf-operator-system
spec:
  serialNumber: "MT25066004C7"
  bmcIp: "10.1.2.3"
  numberOfPFs: 1
  pf0Name: "eth0"
```

## Lifecycle Management

### Creation
DPUDevice resources are typically created through:
* **Automatic Discovery**:
  * Zero-Trust: Via DPUDiscovery controller scanning IP ranges
  * Host-Trusted: Via dpudetector daemon on host nodes
* **Manual Creation**: By administrators with known device details
* **DPU Detection**: Via dpudetector daemon on host nodes

Firmware Update:
- In zero-trust mode, BMC firmware will be updated to the latest version.

### Updates
Most fields in DPUDevice are immutable once set. Only the following can be updated:
- Labels and annotations
- Status fields (managed by controllers)

### Deletion
DPUDevice resources are protected by a finalizer (`provisioning.dpu.nvidia.com/dpudevice-protection`) to prevent accidental deletion while the device is in use.

## Integration with Other Resources

### DPUNode
DPUDevice resources are referenced by DPUNode resources through the `dpus` field by their serial numbers:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  name: dpu-node-001
spec:
  dpus:
  - name: MT25066004C7
  - name: MT25066004C8
```

### DPU
DPU resources reference DPUDevice resources through the `dpuDeviceName` field:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPU
metadata:
  name: dpu-001
spec:
  dpuDeviceName: MT25066004C7
  dpuNodeName: dpu-node-001
  # ... other fields
```

## Monitoring and Troubleshooting

### Checking Device Status

```bash
# Get all DPUDevice resources
kubectl get dpudevices -n dpf-operator-system

# Get detailed information about a specific device
kubectl describe dpudevice MT25066004C7 -n dpf-operator-system

# Check device conditions
kubectl get dpudevice MT25066004C7 -n dpf-operator-system -o jsonpath='{.status.conditions}'
```

### Common Issues

* **Device Not Discovered when in Zero Trust setup**: Check if the device is reachable via BMC IP
* **Invalid Serial Number**: Ensure the serial number matches the required pattern
* **BMC Connection Issues**: Verify BMC IP and port configuration
* **PCI Address Not Found**: Check if the device is properly installed in the host

### Status Conditions

Monitor the following conditions for device health:

```bash
# Check if device is ready
kubectl get dpudevice MT25066004C7 -n dpf-operator-system -o jsonpath='{.status.conditions[?(@.type=="DpuDeviceReady")].status}'

# Check for errors
kubectl get dpudevice MT25066004C7 -n dpf-operator-system -o jsonpath='{.status.conditions[?(@.type=="DpuDeviceError")]}'
```

## Related Resources

* [DPUNode](dpunode.md) - Node-level DPU management
* [DPUDiscovery](dpudiscovery.md) - Automatic DPU discovery
* [DPU](dpudeployment.md) - DPU provisioning and deployment
* [DPUSet](dpuset.md) - Bulk DPU management
