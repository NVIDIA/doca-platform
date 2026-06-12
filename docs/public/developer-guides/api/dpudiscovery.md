---
title: "DPUDiscovery"
---

[[_TOC_]]

The DPUDiscovery is a Kubernetes CRD that enables automatic discovery of DPU (Data Processing Unit) devices within specified IP ranges in the DOCA Platform Framework (DPF). It provides a scalable and efficient way to scan network ranges for DPU BMCs and automatically create DPUDevice resources.

## Overview

The DPUDiscovery resource automates the process of finding and registering DPU devices in your infrastructure. It scans specified IP ranges for DPU BMCs (Base Management Controllers) and creates corresponding DPUDevice resources for discovered devices. This eliminates the need for manual device registration and enables dynamic DPU management.

## Key Features

* **Automatic Discovery**: Scans IP ranges for DPU BMCs automatically
* **Configurable Scanning**: Customizable scan intervals and worker counts
* **Scalable**: Supports parallel scanning with configurable workers
* **Redfish Integration**: Uses Redfish protocol for DPU communication
* **Status Tracking**: Provides scan status and discovered device counts
* **Resource Creation**: Automatically creates DPUDevice resources for found devices

## DPUDiscovery Specification

### DPUDiscoverySpec

The `spec` section defines the discovery configuration:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ipRangeSpec` | IPRangeValidationSpec | Yes | IP range configuration for scanning |
| `scanInterval` | Duration | No | How often to perform scans (default: 1h) |
| `workers` | int | No | Number of workers for parallel scanning |

### IPRangeValidationSpec

Configuration for IP range validation and scanning:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ipRange` | IPRange | Yes | IP range to scan for DPU devices |

### IPRange

Defines the range of IP addresses to scan:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `startIP` | string | Yes | Starting IP address of the range |
| `endIP` | string | Yes | Ending IP address of the range |
| `port` | uint32 | No | BMC port to scan (default: 443) |

### DPUDiscoveryStatus

The `status` section contains discovery results and status:

| Field | Type | Description |
|-------|------|-------------|
| `lastScanTime` | Time | Timestamp of the last successful scan |
| `foundDPUs` | int | Number of DPU devices discovered |

## Example Usage

### Basic DPUDiscovery

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDiscovery
metadata:
  name: dpu-discovery-main
  namespace: dpf-operator-system
spec:
  ipRangeSpec:
    ipRange:
      startIP: "192.168.1.1"
      endIP: "192.168.1.254"
      port: 443
  scanInterval: "30m"
```

### DPUDiscovery with Custom Workers

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDiscovery
metadata:
  name: dpu-discovery-large-range
  namespace: dpf-operator-system
spec:
  ipRangeSpec:
    ipRange:
      startIP: "10.0.0.1"
      endIP: "10.0.255.254"
      port: 443
  scanInterval: "1h"
  workers: 10
```

### Multiple Discovery Ranges

You can create multiple DPUDiscovery resources for different network segments:

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDiscovery
metadata:
  name: dpu-discovery-management
  namespace: dpf-operator-system
spec:
  ipRangeSpec:
    ipRange:
      startIP: "192.168.100.1"
      endIP: "192.168.100.254"
      port: 443
  scanInterval: "15m"

---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDiscovery
metadata:
  name: dpu-discovery-production
  namespace: dpf-operator-system
spec:
  ipRangeSpec:
    ipRange:
      startIP: "10.10.0.1"
      endIP: "10.10.255.254"
      port: 443
  scanInterval: "1h"
  workers: 20
```

## Field Validation

### IP Address Validation
* **Format**: Must be valid IPv4 addresses
* **Pattern**: `^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
* **Restrictions**: 
  * Cannot be `0.0.0.0`
  * Only IPv4 supported (IPv6 not allowed)
  * Both startIP and endIP must be provided

### Port Validation
* **Range**: 1 to 65535
* **Default**: 443

### Scan Interval
* **Format**: Kubernetes Duration format (e.g., "30m", "1h", "2h30m")
* **Default**: "1h"

### Workers
* **Default**: Calculated as 1 worker per 255 IPs in the range
* **Minimum**: 1 worker
* **Maximum**: No explicit limit (limited by cluster resources)

## Discovery Process

### Scanning Workflow

* **IP Range Calculation**: Determines the number of IPs to scan
* **Worker Allocation**: Allocates workers based on configuration or defaults
* **Parallel Scanning**: Workers scan IP ranges in parallel
* **Redfish Communication**: Uses Redfish protocol to communicate with BMCs
* **Device Detection**: Identifies DPU devices and extracts information
* **Resource Creation**: Creates DPUDevice resources for discovered devices
* **Status Update**: Updates discovery status with results

### Worker Scaling

The discovery controller automatically calculates the optimal number of workers:

```go
const ipPerWorker = 255
workers = int((end-start)/uint32(ipPerWorker)) + 1
if workers < 1 {
    workers = 1
}
```

This ensures efficient scanning without overwhelming the cluster resources.

## Configuration Requirements

### DPFOperatorConfig

DPUDiscovery requires specific configuration in the DPFOperatorConfig:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpf-operator-config
  namespace: dpf-operator-system
spec:
  provisioningController:
    installInterface:
      installViaRedfish:
        enabled: true
        skipDPUNodeDiscovery: true  # Set to false to create DPUNode by DPUDiscovery process
```

### Redfish Configuration

The discovery process uses Redfish protocol for BMC communication. Ensure:

* **Redfish is enabled** in DPFOperatorConfig
* **BMC credentials** are properly configured
* **Network connectivity** to BMC IPs is available
* **Firewall rules** allow Redfish traffic (typically port 443)

## Monitoring and Troubleshooting

### Checking Discovery Status

```bash
# Get all DPUDiscovery resources
kubectl get dpudiscoveries -n dpf-operator-system

# Get detailed information about discovery
kubectl describe dpudiscovery dpu-discovery-main -n dpf-operator-system

# Check discovery status
kubectl get dpudiscovery dpu-discovery-main -n dpf-operator-system -o jsonpath='{.status}'
```

### Monitoring Scan Progress

```bash
# Check last scan time
kubectl get dpudiscovery dpu-discovery-main -n dpf-operator-system -o jsonpath='{.status.lastScanTime}'

# Check number of found DPUs
kubectl get dpudiscovery dpu-discovery-main -n dpf-operator-system -o jsonpath='{.status.foundDPUs}'

# Watch discovery status
kubectl get dpudiscoveries -n dpf-operator-system -w
```

### Common Issues

* **No DPUs Found**: 
   - Verify IP range configuration
   - Check network connectivity to BMCs
   - Ensure Redfish is enabled and configured

* **Scan Failures**:
   - Check DPFOperatorConfig settings
   - Verify Redfish credentials (`bmc-shared-password` secret)
   - Review controller logs

### Controller Logs

```bash
# Check discovery controller logs
kubectl logs -n dpf-operator-system deployment/dpf-operator-controller-manager | grep -i discovery
```

## Integration with Other Resources

### DPUDevice Creation
DPUDiscovery automatically creates DPUDevice resources for discovered devices:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: <discovered-serial>
  namespace: dpf-operator-system
spec:
  serialNumber: "<discovered-serial>"
  # ... other discovered fields
```

### DPUNode Integration
When `skipDpuNodeDiscovery` is false, discovery can also create DPUNode resources for discovered devices.

## Related Resources

* [DPUDevice](dpudevice.md) - Individual DPU device management
* [DPUNode](dpunode.md) - Node-level DPU management
* [DPFOperatorConfig](dpfoperatorconfig.md) - Operator configuration
* [DPU](dpudeployment.md) - DPU provisioning and deployment
