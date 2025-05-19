---
title: "Storage Development Guide"
---

The DPF Storage Subsystem provides a framework for integrating 3rd-party storage plugins with the DPF system.
This document outlines the architecture and guidelines for developing a plugin that integrates with the
NVIDIA storage emulation service, called [DOCA SNAP](https://docs.nvidia.com/doca/sdk/doca+snap+services/index.html).

[TOC]

## Architecture

The DPF storage subsystem is built from a set of core components that are provided by NVIDIA, and vendor-specific components that are provided by the storage vendor.

The core components are shipped as part of the DPF system release and usually do not require any modifications from the end user.

The core components are:

* [SNAP CSI Plugin](#snap-csi-plugin)
* [SNAP Controller](#snap-controller)
* [SNAP Node Driver](#snap-node-driver)
* [DOCA SNAP](#doca-snap)

Vendor-specific components are:

* [Vendor CSI Plugin](#vendor-csi-plugin)
* [Vendor Plugin](#vendor-plugin)
* [Vendor xDev](#storage-vendor-xdev-module) (optional)

The high-level architecture of the storage sub-system is presented in the following diagram.

```mermaid
%%{
  init: {
    "theme": "neutral",
    "flowchart": {"wrap": true,"nodePadding": 1}
  }
}%%

flowchart-elk
    storage_system[Remote storage system]
    subgraph control_plane[Control-plane node]
        api_dpu[DPU Cluster K8S API]
        api_host[Host Clustser K8S API]
        snap_csi_plugin_controller[SNAP CSI Plugin<br>controller]
        snap_controller[SNAP controller]
        vendor_csi_plugin[Vendor CSI Plugin]
    end
    subgraph worker[Worker node]
        snap_csi_plugin_node[SNAP CSI Plugin<br>node]
        worker_kubelet[Kubelet]
        subgraph workload[Workload Pod]
          emulated_device[Emulated device]
        end
        subgraph dpu[DPU]
            dpu_kubelet[Kubelet]
            snap_node_driver[SNAP Node driver]
            subgraph doca_snap[DOCA SNAP]
              vendor_xdev[Vendor xDev]
            end
            vendor_plugin[Vendor Plugin]
        end
    end
    api_host <--> snap_csi_plugin_controller
    api_host <--> worker_kubelet
    api_dpu <--> snap_csi_plugin_controller 
    api_dpu <--> snap_controller
    api_dpu <--> vendor_csi_plugin
    api_dpu <--> snap_node_driver
    api_dpu <--> dpu_kubelet
    worker_kubelet <-->|CSI gRPC| snap_csi_plugin_node
    vendor_csi_plugin <-->|Management API| storage_system
    snap_node_driver <-->|SNAP RPC API| doca_snap
    snap_node_driver <-->|StoragePlugin gRPC API| vendor_plugin
    vendor_plugin <-->|SNAP RPC API| doca_snap
    doca_snap <-->|Storage transport| storage_system
    doca_snap <--> |emulate| emulated_device
    snap_csi_plugin_node <-->|discover/prepare| emulated_device

    style snap_csi_plugin_controller fill:#74b900
    style snap_controller fill:#74b900
    style vendor_csi_plugin fill:#c5e0cd
    style snap_csi_plugin_node fill:#74b900
    style snap_node_driver fill:#74b900
    style doca_snap fill:#74b900
    style vendor_plugin fill:#c5e0cd
    style vendor_xdev fill:#c5e0cd
    style api_host fill:#afdaed
    style api_dpu fill:#eddeaf
```


### End-to-End Flow Description

The following steps outline the end-to-end process for provisioning and attaching storage using the DPF storage subsystem:


1. **PVC Creation**: The user creates a **PersistentVolumeClaim (PVC)** object. The **PVC** references a storage class that specifies the [SNAP CSI Plugin](#snap-csi-plugin) as its provisioner.
   The storage class contains parameter that specify the name of a specific [StoragePolicy](#storagepolicy-crd) that exists in the DPU cluster.

2. **Volume Object Creation in DPU cluster**: The [SNAP CSI Plugin](#snap-csi-plugin) Controller in the host cluster handles **PVC** creation and creates a [Volume](#volume-crd) object on the DPU cluster. This object includes references to the [StoragePolicy](#storagepolicy-crd) and the requested volume parameters from the storage class.

3. **Storage Vendor Selection**: The [SNAP Controller](#snap-controller) in the DPU cluster detects the new [Volume](#volume-crd) object. It selects a [StorageVendor](#storagevendor-crd) that matches the policy specified in the [StoragePolicy](#storagepolicy-crd) resource. The controller creates a **PVC** in the DPU cluster that references the storage class of the selected [Vendor CSI plugin](#vendor-csi-plugin).

4. **Vendor PV Provisioning**: The [Vendor CSI plugin](#vendor-csi-plugin) controller detects the new **PVC**, provisions the underlying storage, and creates the corresponding **PersistentVolume (PV)** object.

5. **Volume Availability Update**: The [SNAP Controller](#snap-controller) detects the new **PV** and updates the status of the [Volume](#volume-crd) object to **Available**.

6. **PV Object Creation**: The [SNAP CSI Plugin](#snap-csi-plugin) Controller in the host cluster detects the status change of the [Volume](#volume-crd) object and creates the **PV** object in the host cluster.

7. **Volume Attachment Initiation**: The Kubernetes Controller Manager in the host cluster detects the **PV** object and creates a native Kubernetes **VolumeAttachment [storage.k8s.io/v1]** object to attach the volume to the user's pod.

8. **DPU Cluster VolumeAttachment Creation**: The [SNAP CSI Plugin](#snap-csi-plugin) Controller detects the new native Kubernetes **VolumeAttachment [storage.k8s.io/v1]** object and creates a corresponding [VolumeAttachment](#volumeattachment-crd) CR in the DPU cluster.

9. **SVVolumeAttachment Creation**: The [SNAP Controller](#snap-controller) detects the new [VolumeAttachment](#volumeattachment-crd) object in the DPU cluster. If the [Vendor CSI plugin](#vendor-csi-plugin) requires attachment, it creates an [SVVolumeAttachment](#svvolumeattachment-crd) object for the [Vendor CSI plugin](#vendor-csi-plugin).

10. **Vendor Volume Attachment**: The [Vendor CSI plugin](#vendor-csi-plugin) controller detects the new [SVVolumeAttachment](#svvolumeattachment-crd) object and exposes the volume on the underlying storage. Once complete, it updates the status to **Attached**.

11. **VolumeAttachment Status Update**: The [SNAP Controller](#snap-controller) sets the `storageAttached` status of the [VolumeAttachment](#volumeattachment-crd) to **True**.

12. **Storage Device Connection**: The [SNAP Node Driver](#snap-node-driver) on the DPU detects the pending [VolumeAttachment](#volumeattachment-crd) object and calls the [Vendor Plugin](#vendor-pluign) via the **StoragePlugin gRPC API** to connect the volume to the underlying storage.

13. **Vendor Plugin Device Setup**: The [Vendor Plugin](#vendor-pluign) connects the volume to the underlying storage (if required) and sets up the vendor-specific device (xDev) inside the [DOCA SNAP](#doca-snap) service.

14. **SNAP Process Volume Exposure**: The [SNAP Node Driver](#snap-node-driver) calls the [DOCA SNAP](#doca-snap) service to expose the volume to the host. Upon completion, the [SNAP Node Driver](#snap-node-driver) updates the DPU parameters in the status of the [VolumeAttachment](#volumeattachment-crd) and sets the `dpu.Attached` status to **True**.

15. **Host VolumeAttachment Update**: The [SNAP CSI Plugin](#snap-csi-plugin) Controller detects that the `dpu.Attached` status of the [VolumeAttachment](#volumeattachment-crd) was changed to **Attached** and updates the status of the native Kubernetes **VolumeAttachment [storage.k8s.io/v1]** object on the host cluster to **Attached**.

16. **Pod Volume Mounting**: The kubelet on the host node detects the native Kubernetes **VolumeAttachment [storage.k8s.io/v1]** object with status **Attached**. It calls the [SNAP CSI Plugin](#snap-csi-plugin) Node to attach the volume to the host and mounts the volume into the pod's namespace.


## Core components

### SNAP CSI Plugin

The **SNAP CSI Plugin** is a Kubernetes CSI plugin responsible for managing the lifecycle of storage resources within the host cluster. It enables the nodes within the host cluster to consume storage resources provisioned by the DPF storage subsystem.

The SNAP CSI plugin handles the creation and management of Kubernetes storage resources such as **Storage Classes**, **Persistent Volumes (PV)**, and other Kubernetes Storage objects.

The plugin communicates with the DPU cluster by creating specific custom resources in the DPU cluster. These resources, such as [Volume](#volume-crd) and [VolumeAttachment](#volumeattachment-crd), signal to other components in the DPU cluster that a volume should be created and attached to the host.

The SNAP CSI Plugin consists of a controller and a node component. The controller component is deployed on control-plane nodes, while the node component is deployed on worker nodes.
The node component is responsible for loading the necessary drivers, creating storage Virtual Functions (VFs) on the host, and performing the necessary operations to expose volumes to Pods.

```mermaid
%%{
  init: {
    "theme": "neutral",
    "flowchart": {"wrap": true,"nodePadding": 1}
  }
}%%

flowchart-elk LR
    subgraph worker[Worker node]
        subgraph snap_csi_plugin_node[SNAP CSI Plugin<br>node]
            snap_csi_plugin_node_sidecars[CSI Sidecars]
            snap_csi_plugin_node_plugin[Plugin]
        end
        worker_kubelet[Kubelet]
    end
    subgraph control_plane[Control-plane node]
        api_dpu[DPU Cluster K8S API]
        api_host[Host Clustser K8S API]
        subgraph snap_csi_plugin_controller[SNAP CSI Plugin<br>controller]
            snap_csi_plugin_controller_sidecars[CSI Sidecars]
            snap_csi_plugin_controller_plugin[Plugin]
        end
    end
    api_host <--> worker_kubelet
    api_dpu <--> |Volume CR <br> VolumeAttachemnt CR|snap_csi_plugin_controller_plugin
    api_host <-->|K8S Storage Resources| snap_csi_plugin_controller_sidecars
    snap_csi_plugin_controller_sidecars <-->|CSI gRPC|snap_csi_plugin_controller_plugin
    snap_csi_plugin_node_sidecars <-->|CSI gRPC|snap_csi_plugin_node_plugin
    worker_kubelet <-->|CSI gRPC|snap_csi_plugin_node_plugin

    style snap_csi_plugin_controller fill:#74b900
    style snap_csi_plugin_node fill:#74b900
    style api_host fill:#afdaed
    style api_dpu fill:#eddeaf
```

### SNAP Controller

The **SNAP Controller** implements the business logic of the DPF storage subsystem. Operating within the DPU cluster, it:

* Reconciles [Volume](#volume-crd) and [VolumeAttachment](#volumeattachment-crd) resources created by the SNAP CSI plugin.
* Creates the necessary Kubernetes resources to trigger the [Vendor CSI plugin](#vendor-csi-plugin) (that watches resources in the DPU cluster).
* Uses [StorageVendor](#storagevendor-crd) and [StoragePolicy](#storagepolicy-crd) custom resources to select the appropriate storage vendor and pass required parameters.

If the [Vendor CSI plugin](#vendor-csi-plugin) needs
[ControllerPublishVolume](https://github.com/container-storage-interface/spec/blob/master/spec.md#controllerpublishvolume)/
[ControllerUnpublishVolume](https://github.com/container-storage-interface/spec/blob/master/spec.md#controllerunpublishvolume) operations,
the SNAP Controller will create an [SVVolumeAttachment](#svvolumeattachment-crd) custom resource. This resource is similar to the native Kubernetes `VolumeAttachment` but is not handled by the Kubernetes controllers. Instead, it is managed by the NVIDIA External Attacher sidecar, that is drop-in replacement for upstream `external-attacher` sidecar.

```mermaid
%%{
  init: {
    "theme": "neutral",
    "flowchart": {"wrap": true,"nodePadding": 1}
  }
}%%

flowchart LR
    subgraph control_plane[Control-plane node]
        api_dpu[DPU Cluster K8S API]
        snap_controller[SNAP controller]
    end
    api_dpu <--> |Volume CR <br> VolumeAttachemnt CR <br> StorageVendor CR<br> StoragePolicy CR <br> SVVolumeAttachment CR <br> Core K8S Storage Objects|snap_controller

    style snap_controller fill:#74b900

    style api_dpu fill:#eddeaf
```

### SNAP Node Driver

The SNAP Node Driver runs on each DPU that is used to attach storage. Responsibilities include:

* Invoking the Vendor Plugin via the [StoragePlugin gRPC API](#storageplugin-api) to trigger the attachment of remote storage resources to the DOCA SNAP service.
* Interacting directly with the DOCA SNAP service via the **SNAP RPC API** to make storage resources available to the host.

```mermaid
%%{
  init: {
    "theme": "neutral",
    "flowchart": {"wrap": true,"nodePadding": 1}
  }
}%%

flowchart-elk TB
    storage_system[Remote storage system]
    subgraph control_plane[Control-plane node]
        api_dpu[DPU Cluster K8S API]
    end
    subgraph worker[Worker node]
        emulated_device[Emulated device]
        subgraph dpu[DPU]
            snap_node_driver[SNAP Node driver]
            subgraph doca_snap[DOCA SNAP]
              vendor_xdev[Vendor xDev]
            end
            vendor_plugin[Vendor Plugin]
        end
    end
    api_dpu <--> |Volume CR <br> VolumeAttachment CR| snap_node_driver
    snap_node_driver <-->|SNAP RPC API| doca_snap
    snap_node_driver <-->|StoragePlugin <br>gRPC API| vendor_plugin
    vendor_plugin <-->|SNAP <br>RPC API| doca_snap
    doca_snap <-->|Storage <br>transport| storage_system
    doca_snap <-->|Emulate| emulated_device

    style snap_node_driver fill:#74b900
    style doca_snap fill:#74b900
    style vendor_plugin fill:#c5e0cd
    style vendor_xdev fill:#c5e0cd
    style api_dpu fill:#eddeaf
```

### DOCA SNAP

NVIDIA DOCA SNAP technology encompasses a family of services that enable hardware-accelerated virtualization of local storage running on NVIDIA BlueField products.
The SNAP services present networked storage as local block or file system devices to the host, emulating local drives on the PCIe bus.
Additional details about the SNAP services can be found in the [DOCA SNAP documentation](https://docs.nvidia.com/doca/sdk/doca+snap+services/index.html).


## Vendor-specific components

### Vendor CSI Plugin

This is the controller part of the standard Kubernetes CSI driver of the storage vendor which is used for any Kubernetes deployment.
It should be used to create/delete/manage the volumes on the underlying storage system.


If the storage vendor requires to support the
[ControllerPublishVolume](https://github.com/container-storage-interface/spec/blob/master/spec.md#controllerpublishvolume)/
[ControllerUnpublishVolume](https://github.com/container-storage-interface/spec/blob/master/spec.md#controllerunpublishvolume) operations,
then a small adjustment in the controller deployment mechanism is required, due to the replacement of the Kubernetes native `VolumeAttachment [storage.k8s.io/v1]` object by the [SVVolumeAttachment](#svvolumeattachment-crd) CRD object.

The original Kubernetes `external-attacher` sidecar should be replaced by a new external-attacher sidecar, provided by NVIDIA, which monitors the [SVVolumeAttachment](#svvolumeattachment-crd) objects instead of the Kubernetes native `VolumeAttachment [storage.k8s.io/v1]` objects (see diagram below).
The new external-attacher sidecar calls the `ControllerPublish` and `ControllerUnpublish` functions of the CSI controller.


*[SPDK-CSI plugin](#spdk-csi-plugin) can be used as a reference Vendor CSI plugin*

```mermaid
%%{
  init: {
    "theme": "neutral",
    "flowchart": {"wrap": true,"nodePadding": 1}
  }
}%%

flowchart-elk LR
    subgraph control_plane[Control-plane node]
        api_dpu[DPU Cluster K8S API]
        subgraph vendor_csi_plugin[Vendor CSI Plugin]
            vendor_csi_plugin_nvidia_sidecar[NVIDIA External Attacher]
            vendor_csi_plugin_plugin[Plugin]
            vendor_csi_plugin_sidecar[CSI Sidecars]
        end
    end
    api_dpu <--> |SVVolumeAttachment CR |vendor_csi_plugin_nvidia_sidecar
    api_dpu <--> |Core K8S Storage Objects |vendor_csi_plugin_sidecar

    vendor_csi_plugin_nvidia_sidecar <--> |CSI gRPC| vendor_csi_plugin_plugin
    vendor_csi_plugin_sidecar <--> |CSI gRPC| vendor_csi_plugin_plugin

    style vendor_csi_plugin fill:#74b900
    style api_dpu fill:#eddeaf
```

An appropriate storage class that represents the CSI controller should be created on the DPU Kubernetes cluster (see example below). The reclaimPolicy field in the storage class MUST be set to Delete. The reason is that the reclaim policy is actually managed by the NVIDIA storage class.

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: example-vendor.example
  annotations:
    storageclass.kubernetes.io/is-default-class: "false"
provisioner: csi-driver.example-vendor.example
reclaimPolicy: Delete
```

In addition, a [StorageVendor](#storagevendor-crd) CR object should be created in the DPU  cluster, and the name of the storage vendor class should be set in the storageClassName parameter (see example below).

```yaml
apiVersion: storage.nvidia.com/v1alpha1
kind: StorageVendor
metadata:
  name: example-vendor
  namespace: nvidia-storage
spec:
  storageClassName: "example-vendor.example"
  pluginName: "example-vendor-plugin"
```
If the storage vendor requires to support the controller attach/detach API then an appropriate `CSIDriver` object that represents the storage vendor CSI driver should be created on the DPU Kubernetes cluster (see example below), with the property attacheRequired set to True.

This object is used by the [SNAP Controller](#snap-controller) to determine if [ControllerPublishVolume](https://github.com/container-storage-interface/spec/blob/master/spec.md#controllerpublishvolume)/
[ControllerUnpublishVolume](https://github.com/container-storage-interface/spec/blob/master/spec.md#controllerunpublishvolume) operations are supported by the storage vendor.

```yaml
apiVersion: storage.k8s.io/v1
kind: CSIDriver
metadata:
  name: csi-driver.example-vendor.example
spec:
  attachRequired: true // Indicates this CSI volume driver requires an attach operation
```


### Vendor Plugin

The concept of this component is very similar to the Kubernetes CSI node-driver.
It is responsible to translate the [StoragePlugin API](#storageplugin-api) into the storage vendor specific RPC calls.

The plugin requires direct access to the DPU.
It should expose a gRPC interface through a UNIX domain socket which should be shared through a HostPath volume on the DPU worker node.
This socket is used by the [SNAP Node Driver](#snap-node-driver) to make calls to this component.
It is responsible for creating the UNIX domain socket in the following shared folder: `/var/lib/nvidia/storage/snap/plugins/{plugin-name}/dpu.sock`, where `plugin-name` represent the name of the ***Vendor plugin***.

It interacts with the [Storage vendor xDev module](#storage-vendor-xdev-module), linked with the [DOCA SNAP](#doca-snap), through the SNAP UNIX domain socket which should be also shared through a HostPath volume on the DPU worker node.
The connections between the node components are described in the diagram below.

```mermaid
%%{
  init: {
    "theme": "neutral",
    "flowchart": {"wrap": true,"nodePadding": 1}
  }
}%%

flowchart-elk TB
    subgraph dpu[DPU]
        snap_node_driver[SNAP Node driver]
        subgraph doca_snap[DOCA SNAP]
            vendor_xdev[Vendor xDev]
        end
        vendor_plugin[Vendor Plugin]
        host_mount_providers[(Host path mount<br>*/var/lib/nvidia/<br>storage/snap/providers*)]
        host_mount_plugins[(Host path mount<br>*/var/lib/nvidia/<br>storage/snap/plugins*)]
    end
    snap_node_driver <-->|RPC API| host_mount_providers
    snap_node_driver <--->|StoragePlugin <br>gRPC API| host_mount_plugins
    vendor_plugin <-->|RPC API| host_mount_providers
    vendor_plugin <-->|StoragePlugin <br>gRPC API| host_mount_plugins
    doca_snap <--->|RPC API| host_mount_providers

    style snap_node_driver fill:#74b900
    style doca_snap fill:#74b900
    style vendor_plugin fill:#c5e0cd
    style vendor_xdev fill:#c5e0cd



```

*It is expected that each vendor provides their own plugin, or re-use the NVIDIA NVMe over Fabrics (NVMeoF) transport plugin that is provided as a reference.*

*The NVIDIA-provided plugin is compatible with the [SPDK-CSI plugin](#spdk-csi-plugin) that can be used as an example [Vendor CSI plugin](#vendor-csi-plugin).*

#### Storage vendor xDev module
This is an optional SPDK device module which provides storage-specific logic inside the SNAP service. If exists - it should be managed by the storage vendor DPU plugin component.
The module is deployed as a static or shared library along with the SNAP service.

## Custom Resource Definitions (CRDs)

The DPF storage subsystem utilizes several custom resources to manage storage policies, vendors, and volume attachments.
Below are the primary CRDs involved, along with examples.

These CRDs are described here for reference. End users are not expected to create these CRDs directly.
Refer to the DPUService below for examples on how to create [StoragePolicy](#storagepolicy-crd) and [StorageVendor](#storagevendor-crd) CRs.

[embedmd]:#(../../../dpuservices/storage/examples/snap-dpu/DPUService-configuration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: snap-configuration
  namespace: dpf-operator-system
spec:
  serviceID: snap-configuration
  helmChart:
    source:
      repoURL: oci://example.com/chart
      version: v0.1.0
      chart: dpf-storage
    values:
      dpu:
        deployCrds: true
        configuration:
          enabled: true
          storageVendors:
            - name: nvidia
              storageClassName: spdkcsi-sc
              pluginName: nvidia
          storagePolicies:
            - name: policy1
              storageVendors: ["nvidia"]
              storageSelectionAlg: "Random"
          # RBAC roles for clients from host cluster
          # serviceAccount names must be aligned with the serviceAccount names from DPUServiceCredentialRequest
          rbacRoles:
            snapCsiPlugin:
              serviceAccount: snap-csi-plugin-sa
            snapController:
              serviceAccount: snap-controller-sa
```

### StoragePolicy CRD

Defines a storage policy that maps between a policy and a list of storage vendors.

```yaml
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: StoragePolicy
metadata:
  name: example-storage-policy
spec:
  storageVendors:
    - vendor1
    - vendor2
  # supported modes are Random and LocalNVolumes
  storageSelectionAlg: LocalNVolumes
  storageParameters:
    parameter1: value1
    parameter2: value2
status:
  state: Valid
  message: "Storage policy is valid."
```

### StorageVendor CRD

Represents a storage vendor. Each storage vendor must have exactly one `StorageVendor` custom resource.

```yaml
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: StorageVendor
metadata:
  name: vendor1
spec:
  pluginName: vendor-plugin
  storageClassName: vendor-storage-class
```

### SVVolumeAttachment CRD

Captures the intent to attach/detach the specified volume to/from the specified node for the storage vendor.

```yaml
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: SVVolumeAttachment
metadata:
  name: sv-volume-attachment-example
spec:
  attacher: vendor-plugin
  nodeName: node01
  source:
    persistentVolumeName: pv-example
```

### VolumeAttachment CRD

Captures the intent to attach/detach the specified NVIDIA volume to/from the specified node.

```yaml
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: VolumeAttachment
metadata:
  name: nv-volume-attachment-example
spec:
  nodeName: node01
  source:
    volumeRef:
      name: volume-example
      namespace: default
  parameters:
    parameter1: value1
```

### Volume CRD

Represents a persistent volume on the DPU cluster, mapping between the tenant K8s PV object and the actual volume on the DPU cluster.

```yaml
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: Volume
metadata:
  name: volume-example
spec:
  request:
    capacityRange:
      request: "10Gi"
    accessModes:
      - ReadWriteOnce
    volumeMode: Filesystem
  storagePolicyRef:
    name: example-storage-policy
    namespace: default
  storageParameters:
    policy: example-storage-policy
    parameter1: value1
status:
  state: Available
```


## StoragePlugin API

Each vendor plugin must implement the following gRPC API to interact with the DPF storage subsystem:


### Services
```proto
syntax = "proto3";

package nvidia.storage.plugins.v1;

import "google/protobuf/wrappers.proto";

// The Identity service provides APIs to identify the plugin and verify its health.
service IdentityService {
  // GetPluginInfo returns the name and version of the plugin.
  rpc GetPluginInfo(GetPluginInfoRequest) returns (GetPluginInfoResponse);

  // Probe checks the health and readiness of the plugin.
  rpc Probe(ProbeRequest) returns (ProbeResponse);
}

// The StoragePlugin service provides APIs to manage storage devices.
service StoragePluginService {
  // StoragePluginGetCapabilities returns the capabilities supported by the plugin.
  rpc StoragePluginGetCapabilities(StoragePluginGetCapabilitiesRequest)
      returns (StoragePluginGetCapabilitiesResponse);

  // GetSNAPProvider retrieves the name of the SNAP provider used by the plugin.
  rpc GetSNAPProvider(GetSNAPProviderRequest) returns (GetSNAPProviderResponse);

  // CreateDevice creates a new storage device and exposes it.
  rpc CreateDevice(CreateDeviceRequest) returns (CreateDeviceResponse);

  // DeleteDevice removes a storage device.
  rpc DeleteDevice(DeleteDeviceRequest) returns (DeleteDeviceResponse);

  // GetDevice gets a storage device.
  rpc GetDevice(GetDeviceRequest) returns (GetDeviceResponse);

  // ListDevices list all devices information
  rpc ListDevices(ListDevicesRequest) returns (ListDevicesResponse);
}
```

All the RPC methods that are not implemented by the plugin MUST reply with the non-ok gRPC status code, `12 UNIMPLEMENTED`.

Any of the RPCs defined above may timeout and may be retried.
The [SNAP Node Driver](#snap-node-driver) may choose the maximum time it is willing to wait for a call, how long it waits between retries, and how many times it retries (these values are not negotiated between plugin and [SNAP Node Driver](#snap-node-driver)).

Idempotency requirements ensure that a retried call with the same fields continues where it left off when retried. The only way to cancel a call is to issue a "negation" call if one exists. For example, issue a DeleteDevice call to cancel a pending CreateDevice operation.

### GetPluginInfo

This method should return the name of the plugin (e.g., example-vendor-plugin.example) and the version of the plugin (e.g., 1.0). On success, the method should reply 0 OK in the gRPC status code.

If the plugin is unable to complete the `GetPluginInfo` call successfully, it MUST reply a non-ok gRPC code in the gRPC status.

**Return codes:**

| Number | Code | Description |
| :---: | :--- | :--- |
| 0 | OK | |
| 9 | FAILED_PRECONDITION | Plugin is unable to complete the call successfully |

```proto
// GetPluginInfoRequest is used to request the plugin's information.
message GetPluginInfoRequest {}

// GetPluginInfoResponse provides the plugin's name, version, and manifest details.
message GetPluginInfoResponse {
  // The name of the plugin. It must follow domain name notation format
  // (https://tools.ietf.org/html/rfc1035#section-2.3.1) and must be 63 characters
  // or less. This field is required.
  string name = 1;

  // The version of the plugin. This field is required.
  string vendor_version = 2;

  // The manifest provides additional opaque information about the plugin.
  // This field is required.
  map<string, string> manifest = 3;
}
```

### Probe
This API is called to verify that the plugin is in a healthy and ready state. If an unhealthy state is reported, via a non-success response, the CO (in our case Kubernetes) may take action with the intent to bring the plugin to a healthy state (e.g., restarting the plugin container).

The plugin should verify its internal state and returns 0 OK if the validation succeeds. If the plugin is still initializing, but is otherwise perfectly healthy, it shall return 0 OK with the ready field set to False.

**Return codes:**

| Number | Code | Description |
| :---: | :--- | :--- |
| 0 | OK | |
| 9 | FAILED_PRECONDITION | Plugin is unable to complete the call successfully |

```proto
// ProbeRequest is used to check the plugin's health and readiness.
message ProbeRequest {}

// ProbeResponse indicates the plugin's health and readiness status.
message ProbeResponse {
  // Ready indicates whether the plugin is healthy and ready.
  google.protobuf.BoolValue ready = 1;
}
```

### StoragePluginGetCapabilities
This API allows the SNAP Node-Driver to check the supported capabilities of the storage service provided by the plugin.

If the plugin is unable to complete the `StoragePluginGetCapabilities` call successfully, it MUST return a non-ok gRPC code in the gRPC status.

**Return codes:**

| Number | Code | Description |
| :---: | :--- | :--- |
| 0 | OK | |
| 9 | FAILED_PRECONDITION | Plugin is unable to complete the call successfully |

```proto
// StoragePluginGetCapabilitiesRequest is used to request the plugin's capabilities.
message StoragePluginGetCapabilitiesRequest {}

// StoragePluginGetCapabilitiesResponse provides the list of supported capabilities.
message StoragePluginGetCapabilitiesResponse {
  // Capabilities supported by the plugin.
  repeated StoragePluginServiceCapability capabilities = 1;
}

// StoragePluginServiceCapability describes a capability of the storage plugin service.
message StoragePluginServiceCapability {
  // RPC specifies an RPC capability type.
  message RPC {
    // Type defines the specific capability.
    enum Type {
      // UNSPECIFIED indicates an undefined capability.
      TYPE_UNSPECIFIED = 0;

      // CREATE_DELETE_BLOCK_DEVICE indicates support for block device creation and deletion.
      TYPE_CREATE_DELETE_BLOCK_DEVICE = 1;

      // CREATE_DELETE_FS_DEVICE indicates support for filesystem device creation and deletion.
      TYPE_CREATE_DELETE_FS_DEVICE = 2;

      // GET_DEVICE_STATS indicates support for retrieving device statistics.
      TYPE_GET_DEVICE_STATS = 3;

      // LIST_DEVICES indicates support for listing devices.
      TYPE_LIST_DEVICES = 4;
    }

    // The type of the capability.
    Type type = 1;
  }

  // The specific type of capability.
  oneof type {
    // Specifies the RPC capabilities of the service.
    RPC rpc = 1;
  }
}
```

### GetSNAPProvider
The API is called by the [SNAP Node Driver](#snap-node-driver) to retrieve the name of the SNAP provider used by the plugin.
If the plugin uses the default SNAP process (provided by NVIDIA) then an empty string should be returned. Otherwise, a unique name represents the SNAP process used by this plugin should be returned. The name is used to identify the UNIX domain socket used by both the plugin and the [SNAP Node Driver](#snap-node-driver) to communicate with the SNAP process.

If the plugin is unable to complete the `GetSNAPProvider` call successfully, it MUST return a non-ok gRPC code in the gRPC status.

**Return codes:**

| Number | Code | Description |
| :---: | :--- | :--- |
| 0 | OK | |
| 9 | FAILED_PRECONDITION | Plugin is unable to complete the call successfully |


```proto
// GetSNAPProviderRequest is used to retrieve the SNAP provider's name.
message GetSNAPProviderRequest {}

// GetSNAPProviderResponse provides the SNAP provider's name.
message GetSNAPProviderResponse {
  // The name of the SNAP provider. If this field is empty, the default provider
  // (e.g., NVIDIA) is used. This field is optional.
  string provider_name = 1;
}
```

### CreateDevice
The API is called by the [SNAP Node Driver](#snap-node-driver) prior to the volume being exposed by SNAP.

The operation must be idempotent. If the device corresponding to the volume_id is already created and is identical to the specified access_modes and volume_mode the plugin must reply 0 OK with the corresponding CreateDeviceResponse message.

Business logic required by the plugin:
1. Allocate a unique device name to be used by SNAP
2. Connect the device to the underlying storage system by using the storage vendor specific APIs (if needed).
3. Provide the device to the SNAP process by using the SNAP JSON-RPC APIs. For example: fsdev_aio_create aio0 /host-folder
4. Return the device name

If the plugin is unable to complete the `CreateDevice` call successfully, it MUST return a non-ok gRPC code in the gRPC status.

**Return codes:**

| Number | Code | Description |
| :---: | :--- | :--- |
| 0 | OK | |
| 9 | FAILED_PRECONDITION | Plugin is unable to complete the call successfully |


```proto
// CreateDeviceRequest is used to create a new storage device.
message CreateDeviceRequest {
  // The unique identifier for the volume.
  string volume_id = 1;

  // The access modes for the volume.
  repeated AccessMode access_modes = 2;

  // The volume mode, either Filesystem or Block. Default is Filesystem.
  string volume_mode = 3;

  // Static properties of the volume. This field is optional.
  map<string, string> publish_context = 4;

  // Static properties of the volume. This field is optional.
  map<string, string> volume_context = 5;

  // Static properties of the storage class. This field is optional.
  map<string, string> storage_parameters = 6;
}

// CreateDeviceResponse provides the details of the created device.
message CreateDeviceResponse {
  // The name of the created device (e.g., SPDK FSdev/Bdev name).
  string device_name = 1;
}

// AccessMode specifies how a volume can be accessed.
enum AccessMode {
  // ACCESS_MODE_UNSPECIFIED indicates an unspecified access mode.
  ACCESS_MODE_UNSPECIFIED = 0;

  // ACCESS_MODE_RWO indicates read/write on a single node.
  ACCESS_MODE_RWO = 1;

  // ACCESS_MODE_ROX indicates read-only on multiple nodes.
  ACCESS_MODE_ROX = 2;

  // ACCESS_MODE_RWX indicates read/write on multiple nodes.
  ACCESS_MODE_RWX = 3;

  // ACCESS_MODE_RWOP indicates read/write on a single pod.
  ACCESS_MODE_RWOP = 4;
}
```

### DeleteDevice
The API is called by the [SNAP Node Driver](#snap-node-driver) after the device has been deleted from SNAP.

The operation must be idempotent. If the device corresponding to the volume_id and the device_name does not exist, the plugin must reply 0 OK.

Business logic required by the plugin:
1. Remove the device from the SNAP process.
2. Disconnect the device from the underlying storage system by using the storage vendor specific APIs (if needed).

If the plugin is unable to complete the `DeleteDevice` call successfully, it MUST return a non-ok gRPC code in the gRPC status.

**Return codes:**

| Number | Code | Description |
| :---: | :--- | :--- |
| 0 | OK | |
| 9 | FAILED_PRECONDITION | Plugin is unable to complete the call successfully |


```proto
// DeleteDeviceRequest is used to delete a storage device.
message DeleteDeviceRequest {
  // The unique identifier for the volume.
  string volume_id = 1;

  // The name of the device to be deleted.
  string device_name = 2;
}

// DeleteDeviceResponse is the response for deleting a device.
message DeleteDeviceResponse {}
```

### ListDevices
A Storage vendor plugin must implement this API if it has LIST_DEVICES capability. The plugin shall return the information about all the devices that it knows about. If devices are created and/or deleted while the [SNAP Node Driver](#snap-node-driver) is concurrently paging through ListDevices results then it is possible that the [SNAP Node Driver](#snap-node-driver) may either witness duplicate devices in the list, not witness existing devices, or both. The [SNAP Node Driver](#snap-node-driver) shall not expect a consistent "view" of all devices when paging through the device list via multiple calls to ListDevices.

If the plugin is unable to complete the `ListDevices` call successfully, it MUST return a non-ok gRPC code in the gRPC status.

**Return codes:**

| Number | Code | Description |
| :---: | :--- | :--- |
| 0 | OK | |
| 10 | ABORTED | Indicates that starting_token is not valid |


```proto
message ListDevicesRequest {
  // number of entries that can be returned.
  int32 max_entries = 1;

  // A token to specify where to start paginating. Set this field to
  // `next_token` returned by a previous `ListDevices` call to get the
  // next page of entries.
  string starting_token = 2;
}

message ListDevicesResponse {

  message Entry {
    string volume_id = 1;
    string device_name = 2;
  }

  repeated Entry entries = 1;

  // This token allows the caller to get the next page of entries for
  // `ListDevices` request.
  string next_token = 2;
}

```

### GetDevice
A Storage vendor plugin must implement this API if it has GET_DEVICE_STATS capability. The plugin shall return the information about the device corresponding to the volume_id and the device_name.

If the device corresponding to the volume_id and the device_name does not exist, the plugin must reply 5 NOT_FOUND.

If the plugin is unable to complete the GetDevice call successfully, it MUST return a non-ok gRPC code in the gRPC status.

**Return codes:**

| Number | Code | Description |
| :---: | :--- | :--- |
| 0 | OK | |
| 5 | NOT_FOUND | The device was not found |
| 9 | FAILED_PRECONDITION | Plugin is unable to complete the call successfully |

```proto
// GetDeviceRequest is used to get information about the device
message GetDeviceRequest {
  // volume identifier
  string volume_id = 1;

  // The device name. For example, SPDK FSdev/Bdev name
  string device_name = 2;
}

message GetDeviceResponse {
  // list of access modes for the volume.
  repeated AccessMode access_modes = 1;

  // Indicates the volume mode. Either Filesystem or Block. Default value is Filesystem. This field is OPTIONAL
  string volume_mode = 2;

  // Opaque static publish properties of the volume. This field is OPTIONAL
  map<string, string> publish_context = 3;

  // Opaque static properties of the volume. This field is OPTIONAL
  map<string, string> volume_context = 4;

  // Opaque static properties of the storage class. This field is OPTIONAL
  map<string, string> storage_parameters = 5;
}
```


## SPDK-CSI plugin

> **Note:** this plugin is provided only as an example Vendor CSI Plugin implementation for demonstration purposes. It is not intended or supported for production use cases.

The SPDK-CSI plugin is not shipped as part of the DPF release. It is expected that the user will build the plugin from source code.
The DPF repository contains a specific helm chart for the SPDK-CSI.

The instructions for building the SPKD-CSI image and helm chart can be found at the [DPF repo](https://github.com/nvidia/doca-platform) under `dpuservices/storage/examples/spdk-csi/README.md`.

Example manifests to deploy the SPDK-CSI can be found at the [DPF repo](https://github.com/nvidia/doca-platform) under `dpuservices/storage/examples/spdk-csi/dpuservice`.

## Deployment

Example manifests for DPF storage subsystem components can be found [DPF repo](https://github.com/nvidia/doca-platform) under `dpuservices/storage/examples`.
