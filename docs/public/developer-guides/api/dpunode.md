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

The `pod-template` key must contain a Kubernetes `PodTemplateSpec` in YAML or JSON format.
Do **not** include `apiVersion` or `kind` -- the controller wraps the template inside a Job automatically.
The controller injects the `DPUNODE_NAME` environment variable, a `dpf-pod-info` downward-API volume,
and a control-plane toleration (`node-role.kubernetes.io/control-plane:NoSchedule`).
The Job is created with `backoffLimit: 3`, so Kubernetes automatically retries the pod up to 3 times
with exponential backoff before reporting a terminal failure.
If the Job still fails after those retries, see [Script reboot job failures and recovery](#script-reboot-job-failures-and-recovery).

#### Reboot context exposed to the script (v26.4)

Starting with DPF v26.4, the controller also propagates the aggregated reboot
intent reported by the DPU agent(s) into every container and init container of
the script pod, and stamps it as pod-template annotations. Custom reboot scripts
can use this signal to choose the right host action (for example: cold power
cycle vs warm reboot) without having to read the DPU API directly.

| Surface | Name | Meaning |
|---------|------|---------|
| Env var | `DPUNODE_NAME` | Name of the `DPUNode` the Job is reconciling. |
| Env var | `DPUNODE_REBOOT_METHOD` | Aggregated reboot method across the DPUs that have advanced to phase `Rebooting` for this DPUNode. The most disruptive method wins (priority: `PowerCycle` > `SystemLevelReset` > `SystemReboot` > `FirmwareReset` > `DPUWarmReboot` > `NoAction`). DPUs still in earlier provisioning phases (e.g. `DPUConfig`) are intentionally excluded so the value cannot be influenced by reboot methods that are still being negotiated. **The aggregate is never `Unknown`**: when no rebooting DPU has populated `status.agentStatus.rebootMethod`, the controller substitutes `SystemLevelReset` so the script always has an actionable signal. |
| Env var | `DPUNODE_REBOOT_METHODS_PER_DPU` | Comma-separated `<dpu-name>=<method>` mapping for every DPU in phase `Rebooting`, **sorted by DPU name** for stability. DPUs that have not reported a method appear as `<dpu-name>=Unknown` (this mapping is informational and is *not* defaulted, so scripts can still tell which DPUs have not reported). Empty when no DPU is in `Rebooting` phase yet. |
| Annotation | `provisioning.dpu.nvidia.com/reboot-method-aggregated` | Same value as `DPUNODE_REBOOT_METHOD`. Surfaced inside the pod through the existing `dpf-pod-info` downward-API mount at `/etc/dpf-pod-info/annotations`. |
| Annotation | `provisioning.dpu.nvidia.com/reboot-methods-per-dpu` | Same value as `DPUNODE_REBOOT_METHODS_PER_DPU`. The DPUNode controller is the source of truth and overwrites any user-provided value for these two keys in the pod template. |

The aggregate is guaranteed to be one of the seven non-`Unknown`
`RebootMethodType` values. `SystemLevelReset` is the default when the agent
has not reported yet -- it is the safe middle ground that triggers a
host-impacting reboot without escalating to a hard `PowerCycle`. Per-DPU
entries can still appear as `Unknown` and should be treated as "agent has
not reported for this DPU yet".

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: custom-reboot-script
  namespace: dpf-operator-system
data:
  pod-template: |
    spec:
      containers:
      - name: reboot-container
        image: ubuntu:20.04
        command: ["/bin/bash"]
        args:
        - -c
        - |
          echo "Performing custom reboot procedure for DPUNode $DPUNODE_NAME..."
          echo "Aggregated reboot method: $DPUNODE_REBOOT_METHOD"
          echo "Per-DPU methods: $DPUNODE_REBOOT_METHODS_PER_DPU"
          # DPUNODE_REBOOT_METHOD is guaranteed by the controller to be one of
          # the seven non-Unknown RebootMethodType values; SystemLevelReset is
          # the default when no DPU has reported yet.
          case "$DPUNODE_REBOOT_METHOD" in
            PowerCycle)
              # Hard power cycle via BMC / IPMI
              ;;
            SystemLevelReset|SystemReboot|FirmwareReset|DPUWarmReboot)
              # Warm reboot of the host OS is sufficient
              ;;
            NoAction)
              # Nothing to do; let the script exit 0
              ;;
            *)
              # Defensive fallback for unknown future RebootMethodType values.
              ;;
          esac
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
* **Script Execution Errors**: Verify ConfigMap and script syntax. See [Script reboot job failures and recovery](#script-reboot-job-failures-and-recovery) below.

### Script reboot job failures and recovery

The controller creates one Job per DPUNode in the **same namespace as the DPUNode**. The Job name is
`<dpunode-name>-script-job` (for example, DPUNode `dpu-node-003` uses Job `dpu-node-003-script-job`).

Kubernetes applies `backoffLimit` (default 3) before the Job is considered to have failed terminally.

**Observed behavior on failure**

* Affected DPUs stay in phase **`DPURebooting`** until the host reboot completes successfully and the
  **`Rebooted`** status condition becomes **`True`**.
* After a terminal Job failure, the **`Rebooted`** condition is set to **`False`** with reason **`RebootScriptFailed`**
  and a message that includes pod or Job failure details (for example, `BackoffLimitExceeded`).

**Recovery**

1. Inspect the Job and pods: `kubectl describe job <dpunode-name>-script-job -n <namespace>` and
   `kubectl logs job/<dpunode-name>-script-job -n <namespace>` (or the pod name shown in the Job events).
2. Fix the ConfigMap pod template or cluster permissions as needed.
3. Delete the failed Job. The controller recreates it on the next reconcile:

```bash
kubectl delete job <dpunode-name>-script-job -n <namespace>
```

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
