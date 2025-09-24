---
title: "DPUNode"
---

[TOC]

The DPUNode is a Kubernetes CRD that represents a physical host node containing one or more DPU (Data Processing Unit) devices in the DOCA Platform Framework (DPF). It provides node-level management capabilities for DPU provisioning, reboot control, and integration with Kubernetes clusters.

## Overview

The DPUNode resource serves as a bridge between physical host nodes and DPU devices, enabling centralized management of DPU provisioning and host operations. It defines how DPUs should be provisioned on a specific node and how the host should be managed during DPU operations.

## Key Features

* **Node-Level Management**: Manages DPU operations at the host node level
* **Reboot Control**: Configurable host reboot methods (gNOI, external, script)
* **DMS Integration**: Integration with Device Management Service (DMS)
* **DPU Association**: Links multiple DPU devices to a single node
* **Kubernetes Integration**: Optional integration with Kubernetes Node objects

## DPUNode Specification

### DPUNodeSpec

The `spec` section defines the desired configuration for the DPU node:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `nodeRebootMethod` | NodeRebootMethod | No | Method for rebooting the host (default: gNOI) |
| `nodeDMSAddress` | DMSAddress | No | IP and port for DMS communication |
| `dpus` | []DPURef | No | List of DPU devices attached to this node |

### NodeRebootMethod

Defines how the host should be rebooted during DPU operations:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `gNOI` | GNOI | No | Use DPU's DMS interface to reboot the host |
| `external` | External | No | Reboot via external means (not controlled by DPU controller) |
| `script` | Script | No | Reboot by executing a custom script |

### DMSAddress

Configuration for Device Management Service communication:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ip` | string | Yes | IP address in IPv4 format |
| `port` | uint16 | Yes | Port number (minimum: 1) |

### DPURef

Reference to a DPU device:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Name of the DPU device |

### DPUNodeStatus

The `status` section contains the observed state of the DPU node:

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | array | Array of condition objects describing node state |
| `dpuInstallInterface` | string | Interface used for DPU installation (gNOI or redfish) |
| `kubeNodeRef` | string | Name of the Kubernetes Node object (immutable) |
| `rebootInProgress` | bool | Indicates if the node is currently rebooting |

## Conditions

The DPUNode resource uses several condition types to track its state:

* **Ready**: The DPU node is ready for operations
* **InvalidDPUDetails**: The DPU details provided are invalid
* **DPUNodeRebootInProgress**: The DPUNode is in the process of rebooting
* **DPUUpdateInProgress**: The DPU is being updated
* **NeedHostAgentUpgrade**: The host agent needs to be upgraded
* **OOBBridgeConfigured**: The out-of-band bridge (br-dpu) is configured
* **RshimAvailable**: The rshim interface is available

## Example Usage

### Basic DPUNode with gNOI Reboot

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  name: dpu-node-001
  namespace: dpf-operator-system
spec:
  nodeRebootMethod:
    gNOI: {}
  nodeDMSAddress:
    ip: "192.168.1.100"
    port: 443
  dpus:
  - name: dpu-device-001
  - name: dpu-device-002
```

### DPUNode with External Reboot

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  name: dpu-node-002
  namespace: dpf-operator-system
spec:
  nodeRebootMethod:
    external: {}
  dpus:
  - name: dpu-device-003
```

### DPUNode with Custom Script Reboot

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  name: dpu-node-003
  namespace: dpf-operator-system
spec:
  nodeRebootMethod:
    script:
      name: custom-reboot-script
  dpus:
  - name: dpu-device-004
```

### Custom Reboot Script ConfigMap

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: custom-reboot-script
  namespace: dpf-operator-system
data:
  pod-template: |
    apiVersion: v1
    kind: Pod
    metadata:
      name: custom-reboot-pod
      namespace: dpf-operator-system
    spec:
      containers:
      - name: reboot-container
        image: ubuntu:20.04
        command: ["/bin/bash"]
        args:
        - -c
        - |
          echo "Performing custom reboot procedure..."
          # Add your custom reboot logic here
          # For example: IPMI commands, SSH to BMC, etc.
          sleep 10
          exit 0
      restartPolicy: Never
```

## Reboot Methods

### gNOI (Default)
Uses the DPU's Device Management Service interface to reboot the host. This is the recommended method for most deployments.

**Advantages:**
* Integrated with DPU management
* Reliable and consistent
* No external dependencies

**Requirements:**
* DMS must be accessible
* Valid DMS address configuration

### External
Reboots the host via external means not controlled by the DPU controller. This method requires manual intervention or external automation.

**Use Cases:**
* Custom power management systems
* IPMI-based reboots
* Cloud provider APIs

**Requirements:**
* External reboot mechanism must be available
* Manual intervention may be required

### Script
Executes a custom script to reboot the host. The script is defined in a ConfigMap and executed as a Kubernetes Job.

**Use Cases:**
* Custom reboot procedures
* Integration with existing automation
* Complex reboot workflows

**Requirements:**
* ConfigMap with pod template
* Script must exit successfully
* Proper RBAC permissions

## Integration with Kubernetes

### Node Association
DPUNode can optionally be associated with a Kubernetes Node object:

```yaml
status:
  kubeNodeRef: "worker-node-001"
```

This association enables:
* Node-level operations (draining, tainting)
* Integration with Kubernetes scheduling
* Resource management alignment

### Annotations
DPUNode supports the following annotation for external reboot requirements:

```yaml
metadata:
  annotations:
    provisioning.dpu.nvidia.com/dpunode-external-reboot-required: "true"
```

## Lifecycle Management

### Creation
DPUNode resources are typically created:
* **Manually**: By administrators for known nodes
* **Automatically**: Via discovery processes
* **Via DPUSet**: As part of bulk node management

### Updates
Most fields in DPUNode can be updated, but some restrictions apply:
* `kubeNodeRef` is immutable once set
* `dpus` list can be modified to add/remove devices

### Deletion
DPUNode resources are protected by a finalizer (`provisioning.dpu.nvidia.com/dpunode-protection`) to prevent deletion while DPUs are in use.

## Monitoring and Troubleshooting

### Checking Node Status

```bash
# Get all DPUNode resources
kubectl get dpunodes -n dpf-operator-system

# Get detailed information about a specific node
kubectl describe dpunode dpu-node-001 -n dpf-operator-system

# Check node conditions
kubectl get dpunode dpu-node-001 -n dpf-operator-system -o jsonpath='{.status.conditions}'
```

### Common Issues

* **Invalid DMS Address**: Verify IP and port configuration
* **DPU Not Found**: Ensure referenced DPUDevice resources exist
* **Reboot Failures**: Check reboot method configuration and permissions
* **Script Execution Errors**: Verify ConfigMap and script syntax

### Status Monitoring

```bash
# Check if node is ready
kubectl get dpunode dpu-node-001 -n dpf-operator-system -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'

# Check reboot status
kubectl get dpunode dpu-node-001 -n dpf-operator-system -o jsonpath='{.status.rebootInProgress}'

# Check install interface
kubectl get dpunode dpu-node-001 -n dpf-operator-system -o jsonpath='{.status.dpuInstallInterface}'
```

## Related Resources

* [DPUDevice](dpudevice.md) - Individual DPU device management
* [DPU](dpudeployment.md) - DPU provisioning and deployment
* [DPUSet](dpuset.md) - Bulk DPU and node management
* [DPUDiscovery](dpudiscovery.md) - Automatic DPU discovery
