---
title: "Zero Trust Advanced Configuration"
---

[TOC]

This section includes advanced configuration and additional information for the Zero Trust use case.

# DPU Discovery and DPUNode and DPUDevice Object Creation

DPF provides two approaches for discovering and creating DPU resources:

1. **Automated Discovery**: Using `DPUDiscovery` to automatically scan for DPUs and create `DPUDevice` and `DPUNode` resources.
2. **Manual Creation**: Manually creating `DPUDevice` and `DPUNode` resources for each DPU.

You can choose either approach based on your deployment requirements. Automated discovery is recommended for larger
deployments, while manual creation provides more control for smaller or specific configurations.

## Automated DPU Discovery

DPUDiscovery enables automatic discovery of DPU devices and nodes by scanning specified IP ranges. This approach automatically
creates `DPUDevice` and `DPUNode` resources for any discovered DPUs.

**1.** First, create a YAML file for the DPUDiscovery resource. Let's call it `dpudiscovery.yaml`:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDiscovery
metadata:
  name: dpu-discovery-192.168.1-10
  namespace: dpf-operator-system
spec:
  # Define the IP range to scan
  ipRangeSpec:
    ipRange:
      startIP: "10.0.110.120"    # Replace with your start IP
      endIP: "10.0.110.125"     # Replace with your end IP
  
  # Optional: Set scan interval
  scanInterval: "3m"
  # Optional: Set number of workers (default is 1 per 255 IPs)
  workers: 1
```

**2.** Apply the resource using kubectl:

```bash
kubectl apply -f dpudiscovery.yaml
```

**3.** Check the status of the crawler:

```bash
kubectl get dpudiscovery dpu-discovery-192.168.1-10 -o yaml
```

The DPU discovery will:

1. Start scanning the specified IP range
2. Create DPUDevice and DPUNode\* resources for any discovered DPUs
3. Continue scanning at the specified interval
4. Update its status with the last scan time and found DPUs

You can monitor the discovered DPUs with:

```bash
# List discovered DPU devices
kubectl get dpudevices

# List discovered DPU nodes
kubectl get dpunodes
```

\* DPUDiscovery will skip the creation of a DPUNode if there is an existing one with the spec.dpus field containing the
DPUDevices serial number.

### Limitations

* When using autodiscovery for DPUNodes, the created DPUNodes will be named after `dpunode-<DPU_SERIAL_NUMBER>`. In case
    the HBN DPUService is used in conjuction with this DPU provisioning mode, the HBN configuration needs to be adjusted
    to match the discovered nodes accordingly.

## Manual DPU Resource Creation

If you prefer to manually create DPU resources or need more control over the creation process, you can create `DPUDevice`
and `DPUNode` resources manually.

### Creating DPUDevice manually

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

### Creating a DPUNode manually

Create a `DPUNode` resource for each host that has a DPU:

> [!NOTE]
> The `.spec.dpus` field contains the names of each DPUDevice attached to the node. Currently, DPF only supports
> setting a single DPU for each DPUNode.

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

# External Host Reboot

In the Zero Trust scenario, DPF cannot manage the DPU's host machine. During the DPU provisioning process, when the DPU
CR reaches the `rebooting` phase, manual power-cycling is required by the user. The power-cycle operation must be
completed within two hours; otherwise, the DPU join cluster's secret will expire, causing DPU CR pending in `DPU Cluster
Config` phase. After the worker node boots up, the `provisioning.dpu.nvidia.com/dpunode-external-reboot-required`
annotation on the DPUNode must be manually removed.
