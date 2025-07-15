---
title: "DPU Provisioning via Redfish API"
---

[TOC]


DPF supports managing DPUs through Out-of-Band (Redfish) management.

## Prerequisites

The following requirements must be satisfied by the DPU to be managed via Redfish:

* The BMC firmware version of DPU must be 24.10 or higher
* The BMC of DPU must be reset to factory defaults before installing DPF
* The DPU OOB interface must be connected with DPF control plane

> **Note**: [DOCA Perftest Bootstrap](https://gitlab.com/nvidia/networking/bluefield/doca_perftest/doca_perftest_bootstrap/-/blob/main/roles/nc-update-bmc/tasks/main.yml?ref_type=heads) provides Ansible tasks for batch upgrading BMC and resetting BMC to factory defaults.

## DPF System Installation

Follow the [installation steps](../user-guides/README.md) to install the DPF system.

### DPF Operator Configuration

To enable provisioning via the Redfish interface, apply the following `DPFOperatorConfig`:

```yaml
---
apiVersion: operator.dpu.nvidia.com/v1alpha1 
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
  labels:
    app.kubernetes.io/name: dpf-operator
    app.kubernetes.io/instance: dpf-operator
spec:
  provisioningController:
    bfbPVCName: "bfb-pvc"
    installInterface:
      installViaRedfish:
        # Set this to the IP of one of your control plane nodes + 8080 port
        bfbRegistryAddress: "192.168.49.2:8080"
  kamajiClusterManager:
    disable: false
```

### Credentials

To authenticate with Redfish, provide a password for the BMC root user:

> **Note**: Refer to the [BlueField DPU Administrator Quick Start Guide](https://docs.nvidia.com/networking/display/bf3dpu/bluefield+dpu+administrator+quick+start+guide#src-2449222348_safe-id-Qmx1ZUZpZWxkRFBVQWRtaW5pc3RyYXRvclF1aWNrU3RhcnRHdWlkZS1TdGVwM-KAk0NoYW5nZURlZmF1bHRQYXNzd29yZA) for BMC password constraints.

Create the BMC password secret:

```bash
kubectl create secret generic -n dpf-operator-system bmc-shared-password --from-literal=password='ROOT_BMC_PASSWORD'
```

During the DPU provisioning process, DPF will update the passwords of all DPUs according to the provided credential. Note that the credential cannot be modified after creation.

## Create DPU Device

Create a `DPUDevice` resource for each DPU:

> **Note**: The `DPUDevice` is immutable, and creating a DPUDevice will not trigger DPU provisioning.

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice 
metadata:
  name: dpu-device-1
  namespace: dpf-operator-system
spec:
    bmcIp: 10.0.110.122
```

## Create DPU Node

Create a `DPUNode` resource for each host that has a DPU:

> **Note**: The `.spec.dpus` field contains the names of each DPUDevice attached to the node. Currently, DPF only supports setting a single DPU for each DPUNode.

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  labels:
    feature.node.kubernetes.io/dpu-enabled: "true"
  name: worker1
  namespace: dpf-operator-system
spec:
  dpus:
  - name: dpu-device-1
  nodeRebootMethod:
    external: {}
```

## DPU Provisioning

Use `DPUSet` to deploy DPUs, refer [DPUSet](../api/dpuset.md) for more detail. Example configuration:

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: dpuset
  namespace: dpf-operator-system
spec:
  dpuNodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  strategy:
    rollingUpdate:
      maxUnavailable: "10%"
    type: RollingUpdate
  dpuTemplate:
    spec:
      dpuFlavor: dpf-provisioning-hbn-ovn
      bfb:
        name: bf-bundle-new
      nodeEffect:
        noEffect: true
```

## External Host Reboot

In the Redfish scenario, DPF cannot manage the DPU's host machine. During the DPU provisioning process, when the DPU CR reaches the `rebooting` phase, manual power-cycling is required by the user. The power-cycle operation must be completed within two hours; otherwise, the DPU join cluster's secret will expire, causing DPU CR pending in `DPU Cluster Config` phase. After the worker node boots up, the `provisioning.dpu.nvidia.com/dpunode-external-reboot-required` annotation on the DPUNode must be manually removed.

## Deletion and clean up

Follow the [Deletion and clean up steps](../user-guides/ovn_only/README.md#7-deletion-and-clean-up) to uninstall the DPF system.

## Automated DPU discovery

1. First, create a YAML file for the DPUDiscovery resource. Let's call it `dpudiscovery.yaml`:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind:  DPUDiscovery
metadata:
  name: dpu-discovery-192.168.1-10
  namespace: dpf-operator-system
spec:
  # Define the IP range to scan
  ipRangeSpec:
    ipRange:
      startIP: "10.0.110.120"    # Replace with your start IP
      endIP:   "10.0.110.125"     # Replace with your end IP
  
  # Optional: Set scan interval
  scanInterval: "3m"
  # Optional: Set number of workers (default is 1 per 255 IPs)
  workers: 1
```

2. Apply the resource using kubectl:
```bash
kubectl apply -f dpudiscovery.yaml
```

3. Check the status of the crawler:
```bash
kubectl get dpudiscovery dpu-discovery-192.168.1-10 -o yaml
```

The DPU discovery will:
1. Start scanning the specified IP range
2. Create DPUDevice and DPUNode resources for any discovered DPUs
3. Continue scanning at the specified interval
4. Update its status with the last scan time and found DPUs

You can monitor the discovered DPUs with:
```bash
# List discovered DPU devices
kubectl get dpudevices

# List discovered DPU nodes
kubectl get dpunodes
```

