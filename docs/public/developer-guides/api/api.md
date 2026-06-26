---
title: "API reference"
---

# API Reference

## Packages
- [noderesources.dpu.nvidia.com/v1alpha1](#noderesourcesdpunvidiacomv1alpha1)
- [operator.dpu.nvidia.com/v1alpha1](#operatordpunvidiacomv1alpha1)
- [provisioning.dpu.nvidia.com/v1alpha1](#provisioningdpunvidiacomv1alpha1)
- [storage.dpu.nvidia.com/v1alpha1](#storagedpunvidiacomv1alpha1)
- [svc.dpu.nvidia.com/v1alpha1](#svcdpunvidiacomv1alpha1)
- [vpc.dpu.nvidia.com/v1alpha1](#vpcdpunvidiacomv1alpha1)


## noderesources.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the noderesources v1alpha1 API group

### Resource Types
- [NodeSRIOVDevicePluginConfig](#nodesriovdevicepluginconfig)
- [NodeSRIOVDevicePluginConfigList](#nodesriovdevicepluginconfiglist)



#### DevicePluginResource



DevicePluginResource defines a single device plugin resource configuration.



_Appears in:_
- [NodeSRIOVDevicePluginConfigSpec](#nodesriovdevicepluginconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the endpoint resource name for the device plugin.<br />Should contain only alphanumeric characters, underscores and hyphens.<br />The full extended resource name will be constructed as resource-prefix/name.<br />Example: pods_vf, ovnk_mgmt_vf |  | MinLength: 1 <br />Pattern: `^[a-zA-Z0-9_-]+$` <br />Required: \{\} <br /> |
| `resourcePrefix` _string_ | ResourcePrefix is the resource prefix used by the device plugin to prefix the resource name.<br />If not set, the default resource prefix will be used. |  | Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br />Optional: \{\} <br /> |
| `type` _[DevicePluginResourceType](#devicepluginresourcetype)_ | Type specifies the type of the device plugin resource. |  | Enum: [vf] <br />Required: \{\} <br /> |
| `options` _[DevicePluginResourceOptions](#devicepluginresourceoptions)_ | Options contains additional options for the device plugin resource. |  | Optional: \{\} <br /> |
| `ranges` _[VFRange](#vfrange) array_ | Ranges specifies the VF ranges on PFs to be included in this resource. |  | MinItems: 1 <br />Required: \{\} <br /> |


#### DevicePluginResourceOptions



DevicePluginResourceOptions contains additional options for a device plugin resource.



_Appears in:_
- [DevicePluginResource](#devicepluginresource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `isRdma` _boolean_ | IsRdma indicates whether RDMA is enabled for this resource. |  | Optional: \{\} <br /> |


#### DevicePluginResourceType

_Underlying type:_ _string_

DevicePluginResourceType specifies the type of the device plugin resource.

_Validation:_
- Enum: [vf]

_Appears in:_
- [DevicePluginResource](#devicepluginresource)

| Field | Description |
| --- | --- |
| `vf` | DevicePluginResourceTypeVF represents a Virtual Function resource.<br /> |


#### NodeSRIOVDevicePluginConfig



NodeSRIOVDevicePluginConfig is the Schema for the nodesriovdevicepluginconfigs API



_Appears in:_
- [NodeSRIOVDevicePluginConfigList](#nodesriovdevicepluginconfiglist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `noderesources.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `NodeSRIOVDevicePluginConfig` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[NodeSRIOVDevicePluginConfigSpec](#nodesriovdevicepluginconfigspec)_ |  |  |  |
| `status` _[NodeSRIOVDevicePluginConfigStatus](#nodesriovdevicepluginconfigstatus)_ |  |  |  |


#### NodeSRIOVDevicePluginConfigList



NodeSRIOVDevicePluginConfigList contains a list of NodeSRIOVDevicePluginConfig





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `noderesources.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `NodeSRIOVDevicePluginConfigList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[NodeSRIOVDevicePluginConfig](#nodesriovdevicepluginconfig) array_ |  |  |  |


#### NodeSRIOVDevicePluginConfigSpec



NodeSRIOVDevicePluginConfigSpec defines the desired state of NodeSRIOVDevicePluginConfig



_Appears in:_
- [NodeSRIOVDevicePluginConfig](#nodesriovdevicepluginconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devicePluginResources` _[DevicePluginResource](#devicepluginresource) array_ | DevicePluginResources is the list of device plugin resource configurations. |  | MinItems: 1 <br />Required: \{\} <br /> |


#### NodeSRIOVDevicePluginConfigStatus



NodeSRIOVDevicePluginConfigStatus defines the observed state of NodeSRIOVDevicePluginConfig



_Appears in:_
- [NodeSRIOVDevicePluginConfig](#nodesriovdevicepluginconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions exposes the current state of the NodeSRIOVDevicePluginConfig. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### VFRange



VFRange defines a range of Virtual Functions on a Physical Function.



_Appears in:_
- [DevicePluginResource](#devicepluginresource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pfIndex` _integer_ | PFIndex is the index of the Physical Function. |  | Minimum: 0 <br />Required: \{\} <br /> |
| `start` _integer_ | Start is the starting VF index (inclusive).<br />If not set, the range starts from VF 0. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `end` _integer_ | End is the ending VF index (inclusive).<br />If not set, the range extends to the last VF on the PF. |  | Minimum: 0 <br />Optional: \{\} <br /> |



## operator.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the operator v1alpha1 API group

### Resource Types
- [DPFOperatorConfig](#dpfoperatorconfig)
- [DPFOperatorConfigList](#dpfoperatorconfiglist)



#### BFBRegistryConfiguration







_Appears in:_
- [InstallViaRedfish](#installviaredfish)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the BFB Registry is not deployed when set to true. |  | Optional: \{\} <br /> |
| `port` _integer_ | Port is the port on which the BFB Registry will listen |  | Optional: \{\} <br /> |


#### BaseComponentConfig



BaseComponentConfig provides common configuration fields that can be embedded
by all component configurations to reduce code duplication.



_Appears in:_
- [CNIInstallerConfiguration](#cniinstallerconfiguration)
- [DPUDetectorConfiguration](#dpudetectorconfiguration)
- [DPUServiceControllerConfiguration](#dpuservicecontrollerconfiguration)
- [FlannelConfiguration](#flannelconfiguration)
- [KamajiClusterManagerConfiguration](#kamajiclustermanagerconfiguration)
- [KataContainersConfiguration](#katacontainersconfiguration)
- [KubeStateMetricsConfiguration](#kubestatemetricsconfiguration)
- [MultusConfiguration](#multusconfiguration)
- [NVIPAMConfiguration](#nvipamconfiguration)
- [NodeProblemDetectorConfiguration](#nodeproblemdetectorconfiguration)
- [NodeSRIOVDevicePluginControllerConfiguration](#nodesriovdeviceplugincontrollerconfiguration)
- [OVSCNIConfiguration](#ovscniconfiguration)
- [OpenTelemetryCollectorConfiguration](#opentelemetrycollectorconfiguration)
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)
- [SFCControllerConfiguration](#sfccontrollerconfiguration)
- [SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)
- [ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)
- [StaticClusterManagerConfiguration](#staticclustermanagerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |


#### BaseControllerConfig



BaseControllerConfig provides common configuration fields that can be embedded
by all controller configurations to reduce code duplication.



_Appears in:_
- [DPUServiceControllerConfiguration](#dpuservicecontrollerconfiguration)
- [KamajiClusterManagerConfiguration](#kamajiclustermanagerconfiguration)
- [NVIPAMConfiguration](#nvipamconfiguration)
- [NodeSRIOVDevicePluginControllerConfiguration](#nodesriovdeviceplugincontrollerconfiguration)
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)
- [ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)
- [StaticClusterManagerConfiguration](#staticclustermanagerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |


#### CNIInstallerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `installer` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Installer contains the configuration for the CNI-Installer component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |








#### DPFOperatorConfig



DPFOperatorConfig is the Schema for the dpfoperatorconfigs API



_Appears in:_
- [DPFOperatorConfigList](#dpfoperatorconfiglist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `operator.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPFOperatorConfig` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPFOperatorConfigSpec](#dpfoperatorconfigspec)_ |  |  |  |
| `status` _[DPFOperatorConfigStatus](#dpfoperatorconfigstatus)_ |  |  |  |


#### DPFOperatorConfigList



DPFOperatorConfigList contains a list of DPFOperatorConfig





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `operator.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPFOperatorConfigList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPFOperatorConfig](#dpfoperatorconfig) array_ |  |  |  |


#### DPFOperatorConfigSpec



DPFOperatorConfigSpec defines the desired state of DPFOperatorConfig



_Appears in:_
- [DPFOperatorConfig](#dpfoperatorconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `overrides` _[Overrides](#overrides)_ |  |  | Optional: \{\} <br /> |
| `networking` _[Networking](#networking)_ |  | \{ controlPlaneMTU:1500 \} | Optional: \{\} <br /> |
| `monitoring` _[MonitoringConfiguration](#monitoringconfiguration)_ | Monitoring is the configuration for monitoring resources. |  | Optional: \{\} <br /> |
| `security` _[SecurityConfiguration](#securityconfiguration)_ | Security groups security-related cluster settings. |  | Optional: \{\} <br /> |
| `imagePullSecrets` _string array_ | List of secret names which are used to pull images for DPF system components and DPUServices.<br />These secrets must be in the same namespace as the DPF Operator Config and should be created before the config is created.<br />System reconciliation will not proceed until these secrets are available. |  | Optional: \{\} <br /> |
| `deploymentMode` _[DeploymentMode](#deploymentmode)_ | DeploymentMode selects zero-trust vs host-trusted deployment alignment.<br />Required: operators must set this explicitly; provisioning controllers propagate this to DPU.status.deploymentMode. |  | Enum: [zero-trust host-trusted] <br />Required: \{\} <br /> |
| `dpuServiceController` _[DPUServiceControllerConfiguration](#dpuservicecontrollerconfiguration)_ | DPUServiceController is the configuration for the DPUServiceController |  | Optional: \{\} <br /> |
| `provisioningController` _[ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)_ | ProvisioningController is the configuration for the ProvisioningController |  |  |
| `serviceSetController` _[ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)_ | ServiceSetController is the configuration for the ServiceSetController |  | Optional: \{\} <br /> |
| `dpuDetector` _[DPUDetectorConfiguration](#dpudetectorconfiguration)_ | DPUDetector is the configuration for the DPUDetector. |  | Optional: \{\} <br /> |
| `multus` _[MultusConfiguration](#multusconfiguration)_ | Multus is the configuration for Multus |  | Optional: \{\} <br /> |
| `sriovDevicePlugin` _[SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)_ | SRIOVDevicePlugin is the configuration for the SRIOVDevicePlugin |  | Optional: \{\} <br /> |
| `flannel` _[FlannelConfiguration](#flannelconfiguration)_ | Flannel is the configuration for Flannel |  | Optional: \{\} <br /> |
| `ovsCNI` _[OVSCNIConfiguration](#ovscniconfiguration)_ | OVSCNI is the configuration for OVSCNI<br />Deprecated: OVS CNI is installed by CNIInstaller. Remove after 26.7 is released. |  | Optional: \{\} <br /> |
| `nvipam` _[NVIPAMConfiguration](#nvipamconfiguration)_ | NVIPAM is the configuration for NVIPAM |  | Optional: \{\} <br /> |
| `cniInstaller` _[CNIInstallerConfiguration](#cniinstallerconfiguration)_ | CNIInstaller is the configuration for the cni-installer |  | Optional: \{\} <br /> |
| `sfcController` _[SFCControllerConfiguration](#sfccontrollerconfiguration)_ | SFCController is the configuration for the SFCController |  | Optional: \{\} <br /> |
| `kamajiClusterManager` _[KamajiClusterManagerConfiguration](#kamajiclustermanagerconfiguration)_ | KamajiClusterManager is the configuration for the kamaji-cluster-manager |  | Optional: \{\} <br /> |
| `staticClusterManager` _[StaticClusterManagerConfiguration](#staticclustermanagerconfiguration)_ | StaticClusterManager is the configuration for the static-cluster-manager |  | Optional: \{\} <br /> |
| `nodeSRIOVDevicePluginController` _[NodeSRIOVDevicePluginControllerConfiguration](#nodesriovdeviceplugincontrollerconfiguration)_ | NodeSRIOVDevicePluginController is the configuration for the NodeSRIOVDevicePlugin controller.<br />This controller manages per-node SRIOV device plugin pods based on DPU configurations.<br />The controller is disabled by default. |  | Optional: \{\} <br /> |
| `kataContainers` _[KataContainersConfiguration](#katacontainersconfiguration)_ | KataContainers is the configuration for Kata Containers.<br />Kata Containers provides VM-based isolation for untrusted workloads on DPU nodes.<br />This component is disabled by default; set disable to false to enable. |  | Optional: \{\} <br /> |


#### DPFOperatorConfigStatus



DPFOperatorConfigStatus defines the observed state of DPFOperatorConfig



_Appears in:_
- [DPFOperatorConfig](#dpfoperatorconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions exposes the current state of the OperatorConfig. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |
| `version` _string_ | Version is the version of the DPF Operator that is currently deployed. |  |  |


#### DPUDetectorConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the DPUDetector Container.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `daemon` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `daemon` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Daemon contains the configuration for the DPU Detector component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |


#### DPUServiceControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the DPUService controller.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the DPU Service controller component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |
| `disableDPUReadyTaints` _boolean_ | DisableDPUReadyTaints disables the DPU ready taints feature in the DPU Service Controller.<br />This feature adds taints to the worker nodes when the DPU is not ready.<br />This is useful when the DPU is used for networking and the node should not be scheduled until the DPU is ready. |  | Optional: \{\} <br /> |


#### DefaultOverridesConfiguration







_Appears in:_
- [CNIInstallerConfiguration](#cniinstallerconfiguration)
- [DPUDetectorConfiguration](#dpudetectorconfiguration)
- [DPUServiceControllerConfiguration](#dpuservicecontrollerconfiguration)
- [KamajiClusterManagerConfiguration](#kamajiclustermanagerconfiguration)
- [KubeStateMetricsConfiguration](#kubestatemetricsconfiguration)
- [MultusConfiguration](#multusconfiguration)
- [NodeProblemDetectorConfiguration](#nodeproblemdetectorconfiguration)
- [NodeSRIOVDevicePluginControllerConfiguration](#nodesriovdeviceplugincontrollerconfiguration)
- [OVSCNIConfiguration](#ovscniconfiguration)
- [OpenTelemetryCollectorConfiguration](#opentelemetrycollectorconfiguration)
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)
- [SFCControllerConfiguration](#sfccontrollerconfiguration)
- [SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)
- [ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)
- [StaticClusterManagerConfiguration](#staticclustermanagerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the component.<br />This field is optional, and if not set, the component will use the default resource. |  | Optional: \{\} <br /> |


#### DeploymentMode

_Underlying type:_ _string_

DeploymentMode describes the cluster deployment model for DPU provisioning (zero-trust vs host-trusted).

_Validation:_
- Enum: [zero-trust host-trusted]

_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description |
| --- | --- |
| `zero-trust` | DeploymentModeZeroTrust requires provisioningController.installInterface.installViaRedfish<br /> |
| `host-trusted` | DeploymentModeHostTrusted allows provisioningController.installInterface.installViaHostAgent, or installViaGNOI<br /> |




#### FlannelCNI







_Appears in:_
- [FlannelConfiguration](#flannelconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |


#### FlannelConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `cni` _[FlannelCNI](#flannelcni)_ | CNI is the configuration for the Flannel CNI component.<br />It contains the image for the CNI init container.<br />Note: The resources for the CNI container are not configurable. |  | Optional: \{\} <br /> |
| `daemon` _[FlannelDaemon](#flanneldaemon)_ | Daemon is the configuration for the Flannel Daemon component.<br />It contains the image for the Flannel Daemon container and its resource requirements. |  | Optional: \{\} <br /> |
| `image` _[FlannelImages](#flannelimages)_ | Images overrides the container images used by flannel<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new fields `cni` and `daemon` instead. |  | Optional: \{\} <br /> |
| `podCIDR` _string_ | PodCIDR is the pod cidr for flannel. |  | Optional: \{\} <br /> |


#### FlannelDaemon







_Appears in:_
- [FlannelConfiguration](#flannelconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the component.<br />This field is optional, and if not set, the component will use the default resource. |  | Optional: \{\} <br /> |


#### FlannelImages







_Appears in:_
- [FlannelConfiguration](#flannelconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `flannelCNI` _string_ | FlannelCNI must be set if FlannelImages is set. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `kubeFlannel` _string_ | KubeFlannel must be set if FlannelImages is set. |  | MinLength: 1 <br />Required: \{\} <br /> |


#### HelmChart

_Underlying type:_ _string_

HelmChart is a reference to a helm chart.

_Validation:_
- Pattern: `^(oci://|https://).+$`

_Appears in:_
- [CNIInstallerConfiguration](#cniinstallerconfiguration)
- [FlannelConfiguration](#flannelconfiguration)
- [HelmComponentConfig](#helmcomponentconfig)
- [KataContainersConfiguration](#katacontainersconfiguration)
- [KubeStateMetricsConfiguration](#kubestatemetricsconfiguration)
- [MultusConfiguration](#multusconfiguration)
- [NVIPAMConfiguration](#nvipamconfiguration)
- [NodeProblemDetectorConfiguration](#nodeproblemdetectorconfiguration)
- [OVSCNIConfiguration](#ovscniconfiguration)
- [OpenTelemetryCollectorConfiguration](#opentelemetrycollectorconfiguration)
- [SFCControllerConfiguration](#sfccontrollerconfiguration)
- [SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)
- [ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)



#### HelmComponentConfig







_Appears in:_
- [CNIInstallerConfiguration](#cniinstallerconfiguration)
- [FlannelConfiguration](#flannelconfiguration)
- [KataContainersConfiguration](#katacontainersconfiguration)
- [KubeStateMetricsConfiguration](#kubestatemetricsconfiguration)
- [MultusConfiguration](#multusconfiguration)
- [NVIPAMConfiguration](#nvipamconfiguration)
- [NodeProblemDetectorConfiguration](#nodeproblemdetectorconfiguration)
- [OVSCNIConfiguration](#ovscniconfiguration)
- [OpenTelemetryCollectorConfiguration](#opentelemetrycollectorconfiguration)
- [SFCControllerConfiguration](#sfccontrollerconfiguration)
- [SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)
- [ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |




#### Image

_Underlying type:_ _string_

Image is a reference to a container image.

_Validation:_
- Pattern: `^((?:(?:(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]{0,127}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]{32,}))?$`

_Appears in:_
- [DPUDetectorConfiguration](#dpudetectorconfiguration)
- [DPUServiceControllerConfiguration](#dpuservicecontrollerconfiguration)
- [DefaultOverridesConfiguration](#defaultoverridesconfiguration)
- [FlannelCNI](#flannelcni)
- [FlannelDaemon](#flanneldaemon)
- [ImageComponentConfig](#imagecomponentconfig)
- [KamajiClusterManagerConfiguration](#kamajiclustermanagerconfiguration)
- [MultusConfiguration](#multusconfiguration)
- [NVIPAMConfiguration](#nvipamconfiguration)
- [NVIPAMController](#nvipamcontroller)
- [NVIPAMNode](#nvipamnode)
- [NodeSRIOVDevicePluginSettings](#nodesriovdevicepluginsettings)
- [OVSCNIConfiguration](#ovscniconfiguration)
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)
- [SFCControllerConfiguration](#sfccontrollerconfiguration)
- [SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)
- [ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)
- [StaticClusterManagerConfiguration](#staticclustermanagerconfiguration)



#### ImageComponentConfig



ImageComponentConfig provides common configuration fields that can be embedded
by all component configurations to reduce code duplication.



_Appears in:_
- [DefaultOverridesConfiguration](#defaultoverridesconfiguration)
- [FlannelCNI](#flannelcni)
- [FlannelDaemon](#flanneldaemon)
- [KataContainersConfiguration](#katacontainersconfiguration)
- [NVIPAMController](#nvipamcontroller)
- [NVIPAMNode](#nvipamnode)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |




#### InstallViaGNOI



InstallViaGNOI is the interface used to install the BFB via GNOI



_Appears in:_
- [ProvisioningInstallInterface](#provisioninginstallinterface)



#### InstallViaHostAgent



InstallViaHostAgent is the interface used to install the BFB



_Appears in:_
- [ProvisioningInstallInterface](#provisioninginstallinterface)



#### InstallViaRedfish



InstallViaRedfish is the interface used to install the BFB via Redfish



_Appears in:_
- [ProvisioningInstallInterface](#provisioninginstallinterface)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bfbRegistryAddress` _string_ | BFBRegistryAddress is the address of the BFB Registry<br />Deprecated: Use RegistryConfiguration instead. |  | MinLength: 1 <br /> |
| `bfbRegistry` _[BFBRegistryConfiguration](#bfbregistryconfiguration)_ | BFBRegistry is the configuration for the BFB Registry<br />Deprecated: Use RegistryConfiguration instead. |  | Optional: \{\} <br /> |
| `skipDPUNodeDiscovery` _boolean_ | SkipDPUNodeDiscovery is a flag to skip the DPU node discovery. | true | Optional: \{\} <br /> |


#### KamajiClusterManagerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the Kamaji Cluster Manager.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the Kamaji Cluster Manager component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |


#### KataContainersConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `daemon` _[ImageComponentConfig](#imagecomponentconfig)_ | Daemon contains the configuration for the kata-deploy component.<br />It contains the image for the kata-deploy container. |  | Optional: \{\} <br /> |
| `nodeSelector` _object (keys:string, values:string)_ | NodeSelector restricts which nodes kata-deploy runs on.<br />This is passed as the Helm chart's nodeSelector value. |  | Optional: \{\} <br /> |
| `shims` _[KataShim](#katashim) array_ | Shims selects which Kata hypervisor shims to enable.<br />Defaults to ["qemu"] if empty. |  | Enum: [qemu] <br />items:Enum: [qemu] <br />Optional: \{\} <br /> |
| `containerdConfigFileName` _string_ | ContainerdConfigFileName overrides the containerd config file name<br />on the target nodes. Defaults to "config-mlnx.toml". |  | Optional: \{\} <br /> |


#### KataShim

_Underlying type:_ _string_

KataShim identifies a Kata hypervisor shim variant.
Values must match the shim keys in the kata-deploy Helm chart's
`shims.<name>.enabled` values.
Only arm64-compatible shims are supported.

_Validation:_
- Enum: [qemu]

_Appears in:_
- [KataContainersConfiguration](#katacontainersconfiguration)

| Field | Description |
| --- | --- |
| `qemu` | KataShimQEMU is the QEMU hypervisor shim.<br /> |


#### KubeStateMetricsConfiguration







_Appears in:_
- [MonitoringConfiguration](#monitoringconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `daemon` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Daemon contains the configuration for the kube-state-metrics component.<br />It contains the image for kube-state-metrics and its resource requirements. |  | Optional: \{\} <br /> |


#### MonitoringConfiguration



MonitoringConfiguration defines the configuration for monitoring resources.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable controls whether monitoring resources are installed.<br />When enabled (default), the controller:<br />- Creates ServiceMonitors for Kamaji clusters to scrape control-plane metrics.<br />- Deploys kube-state-metrics as a DPUService to expose metrics for custom resources.<br />- Deploys node-problem-detector as a DaemonSet on DPU nodes to detect and report node-level problems.<br />- Deploys opentelemetry-collector as a DaemonSet on DPU nodes to collect and forward logs. |  | Optional: \{\} <br /> |
| `kubeStateMetrics` _[KubeStateMetricsConfiguration](#kubestatemetricsconfiguration)_ | KubeStateMetrics is the configuration for kube-state-metrics |  | Optional: \{\} <br /> |
| `nodeProblemDetector` _[NodeProblemDetectorConfiguration](#nodeproblemdetectorconfiguration)_ | NodeProblemDetector is the configuration for node-problem-detector |  | Optional: \{\} <br /> |
| `openTelemetryCollector` _[OpenTelemetryCollectorConfiguration](#opentelemetrycollectorconfiguration)_ | OpenTelemetryCollector is the configuration for opentelemetry-collector |  | Optional: \{\} <br /> |


#### MultusConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the Multus Container.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `cni` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `cni` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | CNI contains the configuration for the Multus CNI component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |


#### NVIPAMConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the NVIPAM controller.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[NVIPAMController](#nvipamcontroller)_ | Controller contains the configuration for the NVIPAM controller component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |
| `node` _[NVIPAMNode](#nvipamnode)_ | Node contains the configuration for the NVIPAM node component.<br />It contains the image for the node and its resource requirements. |  |  |


#### NVIPAMController







_Appears in:_
- [NVIPAMConfiguration](#nvipamconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the component.<br />This field is optional, and if not set, the component will use the default resource. |  | Optional: \{\} <br /> |


#### NVIPAMNode







_Appears in:_
- [NVIPAMConfiguration](#nvipamconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the component.<br />This field is optional, and if not set, the component will use the default resource. |  | Optional: \{\} <br /> |


#### Networking



Networking defines the networking configuration for the system components.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `controlPlaneMTU` _integer_ | ControlPlaneMTU is the MTU value to be set on the management network.<br />In zero-trust mode this value is applied to the DPU OOB interface (oob_net0), which does not<br />support jumbo frames; it must not exceed 1500 when deploymentMode is zero-trust.<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1280 <br />Optional: \{\} <br /> |
| `highSpeedMTU` _integer_ | HighSpeedMTU is the MTU value to be set on the high-speed interface.<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1280 <br />Optional: \{\} <br /> |
| `dpuNodeOOBBridgeName` _string_ | DPUNodeOOBBridgeName is the name of the Linux bridge on the host used for<br />out-of-band DPU management traffic. If not specified, defaults to "br-dpu".<br />This setting applies only to host-trusted deployments. | br-dpu | MaxLength: 15 <br />MinLength: 1 <br />Pattern: `^[a-z][a-z0-9-]*$` <br />Optional: \{\} <br /> |


#### NodeProblemDetectorConfiguration







_Appears in:_
- [MonitoringConfiguration](#monitoringconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `daemon` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Daemon contains the configuration for the node-problem-detector component.<br />It contains the image for node-problem-detector and its resource requirements. |  | Optional: \{\} <br /> |


#### NodeSRIOVDevicePluginControllerConfiguration



NodeSRIOVDevicePluginControllerConfiguration is the configuration for the NodeSRIOVDevicePlugin controller.
This controller manages per-node SRIOV device plugin pods based on DPU configurations.
The controller is disabled by default.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the NodeSRIOVDevicePlugin controller component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |
| `devicePlugin` _[NodeSRIOVDevicePluginSettings](#nodesriovdevicepluginsettings)_ | DevicePlugin contains the configuration for the SRIOV device plugin pods<br />managed by this controller. |  | Optional: \{\} <br /> |


#### NodeSRIOVDevicePluginSettings



NodeSRIOVDevicePluginSettings contains configuration for the SRIOV device plugin pods
managed by the NodeSRIOVDevicePlugin controller.



_Appears in:_
- [NodeSRIOVDevicePluginControllerConfiguration](#nodesriovdeviceplugincontrollerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ | Image overrides the container image for the SRIOV device plugin. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `initImage` _[Image](#image)_ | InitImage overrides the container image for the init container<br />that generates device plugin configuration. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `defaultResourcePrefix` _string_ | DefaultResourcePrefix is the default resource prefix for the SRIOV device plugin resources.<br />Defaults to "nvidia.com". |  | Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br />Optional: \{\} <br /> |


#### OVSCNIConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the OVS CNI.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `cni` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `cni` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | CNI contains the configuration for the OVS CNI component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |


#### OpenTelemetryCollectorConfiguration







_Appears in:_
- [MonitoringConfiguration](#monitoringconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `daemon` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Daemon contains the configuration for the opentelemetry-collector component.<br />It contains the image for opentelemetry-collector and its resource requirements. |  | Optional: \{\} <br /> |
| `logging` _[OpenTelemetryCollectorLoggingConfiguration](#opentelemetrycollectorloggingconfiguration)_ | Logging contains the configuration for the opentelemetry-collector logging component.<br />If not specified, logging will not be streamed. |  | Optional: \{\} <br /> |


#### OpenTelemetryCollectorLoggingConfiguration







_Appears in:_
- [OpenTelemetryCollectorConfiguration](#opentelemetrycollectorconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpoint` _string_ | Endpoint is the OTLP endpoint where the DPU cluster opentelemetry-collector sends data to.<br />This could be the management cluster's opentelemetry-collector endpoint.<br />If not specified, nothing will be forwarded from DPU clusters. |  | Required: \{\} <br /> |


#### Overrides



Overrides exposes a set of fields which impact the recommended behavior of the DPF Operator.
These fields should only be set for advanced use cases. The fields here have no stability guarantees.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `paused` _boolean_ | Paused disables all reconciliation of the DPFOperatorConfig when set to true. |  | Optional: \{\} <br /> |
| `dpuCNIBinPath` _string_ | DPUCNIBinPath is the path at which the CNI binaries will be installed to on the DPU.<br />This is /opt/cni/bin by default.<br />This setting does not change where kubelet is configured to use the CNI from. |  | Optional: \{\} <br /> |
| `dpuCNIPath` _string_ | DPUCNIConfigPath is the path to which the CNI config files will be installed on the DPU.<br />This is /etc/cni/net.d by default.<br />This setting does not change where kubelet is configured to read the CNI config from. |  | Optional: \{\} <br /> |
| `dpuOpenvSwitchRunPath` _string_ | DPUOpenvSwitchPath is the path at which the openvSwitch run directory can be found on the DPU.<br />This is /var/run/openvswitch by default.<br />This setting does not change where components are installed. Installation location fixed in the BFB. |  | Optional: \{\} <br /> |
| `dpuOpenvSwitchBinPath` _string_ | DPUOpenvSwitchBinPath is the path at which the openvSwitch bin directory can be found on the DPU node.<br />This is /usr/bin/ by default.<br />This setting does not change where components are installed. Installation location fixed in the BFB. |  | Optional: \{\} <br /> |
| `dpuOpenvSwitchSystemSharedPath` _string_ | DPUOpenvSwitchSystemSharedLibPath is the path at which the system lib used by OVS components can be found on the DPU.<br />This is /lib by default.<br />This setting does not change where components are installed. Installation location fixed in the BFB. |  | Optional: \{\} <br /> |
| `flannelSkipCNIConfigInstallation` _boolean_ | FlannelSkipCNIConfigInstallation controls whether Flannel should skip CNI config installation.<br />This is true by default, meaning Flannel does not manage its own CNI configuration.<br />Set to false if you want Flannel to install a CNI configuration. |  | Optional: \{\} <br /> |
| `dpuOpenvSwitchSystemSharedLib64Path` _string_ | DPUOpenvSwitchSystemSharedLib64Path is the path at which the system lib64 used by OVS components can be found on the DPU.<br />If this field is not set, no lib64 volume mount will be configured in the SFC Controller component.<br />This setting does not change where components are installed. Installation location fixed in the BFB. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `dpuLinkerCachePath` _string_ | DPULinkerCachePath is the path on the DPU at which the prebuilt dynamic-linker cache<br />file can be found. When set, this file is mounted read-only into the SFC Controller<br />container so that host OVS binaries can resolve shared libraries using the DPU's<br />linker configuration. If not set, no linker cache mount is added.<br />This setting does not change where components are installed. Installation location fixed in the BFB. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `dpuOptLibraryPath` _string_ | DPUOptLibraryPath is the path on the DPU at which an additional library directory<br />can be found. When set, this directory is mounted read-only into the SFC Controller<br />container. Useful on distributions that install vendor libraries outside the standard<br />paths (e.g. /usr/opt on RHCOS BFB). If not set, no additional library directory is mounted.<br />This setting does not change where components are installed. Installation location fixed in the BFB. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `kubernetesAPIServerVIP` _string_ | KubernetesAPIServerVIP is the VIP the Kubernetes API server is accessible at.<br />This setting enables specific underlying components deployed directly or indirectly by the DPF Operator to reach<br />the Kubernetes API Server when the ClusterIP Kubernetes Service is not functional.<br />If set, it should be set to an IP to ensure that components work even if DNS is not available in the cluster. |  | Optional: \{\} <br /> |
| `kubernetesAPIServerPort` _integer_ | KubernetesAPIServerPort is the port the Kubernetes API server is accessible at.<br />This setting is usually used together with the kubernetesAPIServerVIP setting. It enables specific underlying<br />components deployed directly or indirectly by the DPF Operator to reach the Kubernetes API Server when the<br />ClusterIP Kubernetes Service is not functional. |  | Optional: \{\} <br /> |
| `argoCDNamespace` _string_ | ArgoCDNamespace is the namespace where ArgoCD is deployed.<br />AppProjects and cluster secrets required by DPF will be created in this namespace.<br />Defaults to the namespace of the DPFOperatorConfig. |  | MaxLength: 63 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### ProvisioningControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the Provisioning controller.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the Provisioning controller component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |
| `bfCFGTemplateConfigMap` _string_ | BFCFGTemplateConfigMap is the name of a configMap containing a template for the BF.cfg file used by the DPU controller.<br />By default the provisioning controller use a hardcoded BF.cfg e.g. https://github.com/NVIDIA/doca-platform/blob/release-v24.10/internal/provisioning/controllers/dpu/bfcfg/bf.cfg.template<br />Note: Replacing the bf.cfg is an advanced use case. The default bf.cfg is designed for most use cases.<br />Deprecated: BFCFGTemplateConfigMap is deprecated and will be removed in a future release.<br />Use enableDynamicBFCFGTemplates instead for custom bf.cfg templates. |  | Optional: \{\} <br /> |
| `enableDynamicBFCFGTemplates` _boolean_ | EnableDynamicBFCFGTemplates enables runtime discovery of bf.cfg templates via ConfigMaps.<br />When enabled, the provisioning controller discovers ConfigMaps by matching labels for BFB<br />name/namespace and DPUCluster name/namespace. Mutually exclusive with bfCFGTemplateConfigMap. |  | Optional: \{\} <br /> |
| `bfbPVCName` _string_ | BFBPersistentVolumeClaimName is the name of the PersistentVolumeClaim used by dpf-provisioning-controller<br />If not provided, the controller will use local host storage (hostPath) |  | Optional: \{\} <br /> |
| `dmsTimeout` _integer_ | DMSTimeout is the max time in seconds within which a DMS API must respond, 0 is unlimited |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `customCASecretName` _string_ | CustomCASecretName indicates the name of the Kubernetes secret object<br />which containing the custom CA certificate |  | Optional: \{\} <br /> |
| `installInterface` _[ProvisioningInstallInterface](#provisioninginstallinterface)_ | InstallInterface is the interface through which the BFB is installed |  | Optional: \{\} <br /> |
| `registry` _[RegistryConfiguration](#registryconfiguration)_ | Registry is the configuration for the BFB Registry |  | Optional: \{\} <br /> |
| `maxDPUParallelInstallations` _integer_ | MaxDPUParallelInstallations specifies the maximum number of DPUs that can be provisioned concurrently.<br />A DPU is removed from the concurrent provisioning count as soon as it finishes the "OS Installing" phase and<br />enters the "Rebooting" phase of its provisioning lifecycle. | 50 | Minimum: 1 <br />Optional: \{\} <br /> |
| `multiDPUOperationsSyncWaitTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | MultiDPUOperationsSyncWaitTime is the wait time between DPUs sync operations on the same node.<br />It would take effect only on DPUNode objects which contain more than one DPU. | 30s | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `maxUnavailableDPUNodes` _integer_ | MaxUnavailableDPUNodes is the maximum number of DPUNodes that are unavailable during the node effect period. | 50 | Minimum: 1 <br />Optional: \{\} <br /> |
| `osInstallTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | OSInstallTimeout is the maximum time allowed for OS installation in zero-trust mode.<br />If the installation exceeds this timeout, the DPU will transition to an error state. | 60m | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `firmwareUpdateTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | FirmwareUpdateTimeout is the maximum time allowed for BF4 firmware update in zero-trust mode.<br />If the update exceeds this timeout, the DPU will transition to an error state.<br />When unset, the provisioning controller defaults to 45m. |  | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `nodeEffectRemovalTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | NodeEffectRemovalTimeout is the maximum time allowed for the Node Effect Removal phase.<br />If the DPUNodeMaintenance CR still has requestors after this timeout, the DPU will transition to an error state.<br />When set to "0s" (the default), the timeout is disabled and no time limit is enforced. | 0s | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `hostAgentDNSPolicy` _[DNSPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#dnspolicy-v1-core)_ | HostAgentDNSPolicy sets the DNS policy for the hostagent pod.<br />Valid values are 'ClusterFirstWithHostNet', 'ClusterFirst', 'Default' or 'None'.<br />Defaults to 'ClusterFirstWithHostNet'. |  | Enum: [ClusterFirstWithHostNet ClusterFirst Default None] <br />Optional: \{\} <br /> |


#### ProvisioningInstallInterface



ProvisioningInstallInterface is the interface used to install the BFB



_Appears in:_
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `installViaGNOI` _[InstallViaGNOI](#installviagnoi)_ | InstallViaGNOI is the interface used to install the BFB via GNOI<br />Deprecated: Use InstallViaHostAgent instead. |  | Optional: \{\} <br /> |
| `installViaHostAgent` _[InstallViaHostAgent](#installviahostagent)_ | InstallViaHostAgent is the interface used to install the BFB via HostAgent |  | Optional: \{\} <br /> |
| `installViaRedfish` _[InstallViaRedfish](#installviaredfish)_ | InstallViaRedfish is the interface used to install the BFB via Redfish |  | Optional: \{\} <br /> |


#### RegistryConfiguration







_Appears in:_
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `address` _string_ | Address is the address used to access the BFB Registry. The address must start with "http://".<br />By default, the BFB Registry can be accessed via its Service.<br />For non-kubernetes environments, this must be set due to the lack of kubelet on worker nodes.<br />For zero-trust environments, this must be set so that the BFB Registry can be accessed from DPU BMC.<br />Deprecated: Address is deprecated and will be removed in a future release. |  | Pattern: `^http://` <br />Optional: \{\} <br /> |
| `port` _integer_ | Port is the port on which the registry instances will listen<br />Deprecated: Address is deprecated and will be removed in a future release. |  | Maximum: 65535 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `loadBalancerAddress` _string_ | LoadBalancerAddress is the address of the load balancer for the BFB Registry which the hostagent/redfish use to fetch the BFB and generated bf.cfg.<br />To enable the load balancer, you need to deploy your own load balancer controller and configure the LoadBalancerAddress field.<br />Then check the bfb-registry nodeport service and make your load balancer controller to distribute the requests to the bfb-registry nodeport. |  | Pattern: `^http://` <br />Optional: \{\} <br /> |


#### ResourceComponentConfig



ResourceComponentConfig defines the resource requirements for a container.



_Appears in:_
- [DefaultOverridesConfiguration](#defaultoverridesconfiguration)
- [FlannelDaemon](#flanneldaemon)
- [NVIPAMController](#nvipamcontroller)
- [NVIPAMNode](#nvipamnode)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the component.<br />This field is optional, and if not set, the component will use the default resource. |  | Optional: \{\} <br /> |


#### ResourceRequirements







_Appears in:_
- [DefaultOverridesConfiguration](#defaultoverridesconfiguration)
- [FlannelDaemon](#flanneldaemon)
- [NVIPAMController](#nvipamcontroller)
- [NVIPAMNode](#nvipamnode)
- [ResourceComponentConfig](#resourcecomponentconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `requests` _[Resources](#resources)_ | Requests defines the resource requests for the component. |  |  |
| `limits` _[Resources](#resources)_ | Limits defines the resource limits for the component. |  |  |


#### Resources







_Appears in:_
- [ResourceRequirements](#resourcerequirements)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `cpu` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#quantity-resource-api)_ | CPU is the amount of CPU requested by the component. |  | Optional: \{\} <br /> |
| `memory` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#quantity-resource-api)_ | Memory is the amount of Memory requested by the component. |  | Optional: \{\} <br /> |




#### SFCControllerConfiguration



SFCControllerConfiguration intentionally does not embed BaseControllerConfig:
HA is achieved via per-node sharding (DaemonSet + node-local cache + per-node
reconcilers); each pod exclusively owns its node's state, which makes leader
election unnecessary.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the SFC controller.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the SFC controller component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |
| `secureFlowDeletionTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | SecureFlowDeletionTimeout controls the timeout for which the API server is unreachable after which all the flows<br />are deleted to prevent unintended packet leaks. It has effect when is greater than zero.<br />Value must be in units accepted by Go time.ParseDuration https://golang.org/pkg/time/#ParseDuration. |  | Optional: \{\} <br /> |


#### SPIFFEConfiguration



SPIFFEConfiguration is the per-cluster SPIFFE bootstrap parameter set



_Appears in:_
- [SecurityConfiguration](#securityconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `spireServerAddress` _string_ | SPIREServerAddress is the address of the pre-installed SPIRE Server in host:port form<br />(e.g. "spire-server.spire-system.svc:8081"). |  | MaxLength: 263 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `spireTrustDomain` _string_ | SPIRETrustDomain is the SPIRE-internal trust domain (e.g. "cs.internal") embedded in the<br />DPU Agent SVID URI. |  | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br />Required: \{\} <br /> |
| `kubeAPIAudience` _string_ | KubeAPIAudience is the audience claim the DPU Agent's JWT-SVID must carry; it must match an<br />entry in the kube-apiserver AuthenticationConfiguration.audiences[] (owned out-of-band). |  | MaxLength: 512 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `spireOIDCURL` _string_ | SPIREOIDCURL is the OIDC discovery (issuer) URL of the pre-installed SPIRE Server.<br />The matching kube-apiserver AuthenticationConfiguration.jwt[].issuer value is applied out-of-band. |  | MaxLength: 2048 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `trustBundle` _[SPIFFETrustBundleConfigMapReference](#spiffetrustbundleconfigmapreference)_ | trustBundle references a ConfigMap whose data["bundle.pem"] key holds the SPIRE trust<br />bundle in PEM form. |  | Required: \{\} <br /> |


#### SPIFFETrustBundleConfigMapReference



SPIFFETrustBundleConfigMapReference references the ConfigMap (by name and namespace)
whose data["bundle.pem"] key holds the SPIRE trust bundle in PEM form.



_Appears in:_
- [SPIFFEConfiguration](#spiffeconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the ConfigMap holding the SPIRE trust bundle. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `namespace` _string_ | Namespace is the namespace of the ConfigMap holding the SPIRE trust bundle. |  | MaxLength: 63 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### SRIOVDevicePluginConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the SRIOV Device Plugin container.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `deviceplugin` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `deviceplugin` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | DevicePlugin contains the configuration for the SRIOV Device Plugin component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |


#### SecurityConfiguration



SecurityConfiguration groups security-related cluster settings.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `spiffe` _[SPIFFEConfiguration](#spiffeconfiguration)_ | spiffe configures the SPIFFE-based DPU Agent identity flow. Edits are accepted post-bootstrap<br />but do NOT retro-apply to already-provisioned DPUs. |  | Optional: \{\} <br /> |


#### ServiceSetControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the ServiceChainSet Controller.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the ServiceChainSet controller component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |


#### StaticClusterManagerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the Static Cluster Manager.<br />Deprecated: This field is deprecated and will be removed with v26.7.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the Static Cluster Manager controller component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |



## provisioning.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the provisioning.dpu v1alpha1 API group

### Resource Types
- [BFB](#bfb)
- [BFBList](#bfblist)
- [BlueFieldSoftware](#bluefieldsoftware)
- [BlueFieldSoftwareList](#bluefieldsoftwarelist)
- [DPU](#dpu)
- [DPUCluster](#dpucluster)
- [DPUClusterList](#dpuclusterlist)
- [DPUDevice](#dpudevice)
- [DPUDeviceList](#dpudevicelist)
- [DPUDiscovery](#dpudiscovery)
- [DPUDiscoveryList](#dpudiscoverylist)
- [DPUFlavor](#dpuflavor)
- [DPUFlavorList](#dpuflavorlist)
- [DPUFlavorTemplate](#dpuflavortemplate)
- [DPUFlavorTemplateList](#dpuflavortemplatelist)
- [DPUList](#dpulist)
- [DPUNode](#dpunode)
- [DPUNodeList](#dpunodelist)
- [DPUNodeMaintenance](#dpunodemaintenance)
- [DPUNodeMaintenanceList](#dpunodemaintenancelist)
- [DPUSet](#dpuset)
- [DPUSetList](#dpusetlist)



#### Action







_Appears in:_
- [DPUs](#dpus)
- [NodeEffect](#nodeeffect)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `taint` _[Taint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#taint-v1-core)_ | Add specify taint on the DPU node |  | Optional: \{\} <br /> |
| `noEffect` _boolean_ | Do not do any action on the DPU node |  | Optional: \{\} <br /> |
| `customLabel` _object (keys:string, values:string)_ | Add specify labels on the DPU node |  | Optional: \{\} <br /> |
| `drain` _boolean_ | Drain the K8s host node by NodeMaintenance operator |  | Optional: \{\} <br /> |
| `customAction` _string_ | Name of a config map which contains a pod yaml definition to run which will apply the nodeEffect.<br />The pod is expected to exit when node effect is done, if pod terminates with error then DPU would move to an error phase.<br />The DPUNode's name will be exported as an environment variable, named as DPUNODE_NAME, to each container and init container in the pod.<br />The labels and annotations of DPUNode will be exported in `/etc/dpu/dpf-pod-info/labels` and `/etc/dpu/dpf-pod-info/annotations` accordingly; the volume name `dpf-pod-info` is used to mount the labels and annotations.<br />If any name confliction for env or volume, the controller will not export the name or labels/annotations of DPUNode accordingly. |  | Optional: \{\} <br /> |
| `hold` _boolean_ | Places annotation `wait-for-external-nodeeffect` and waits for it to be removed<br />this is the default behavior in a non K8S environment |  | Optional: \{\} <br /> |
| `force` _boolean_ | Force is the flag to indicate if the node effect should be applied immediately.<br />If true, dpfOperatorConfig.multiDPUOperationsSyncWaitTime and dpfOperatorConfig.maxUnavailableDPUNodes will be ignored when applying node effect for DPUNodeMaintenance CR | false | Optional: \{\} <br /> |


#### AgentStatus







_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `lastStartupTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastStartupTime is the time when the DPU was last started |  | Optional: \{\} <br /> |
| `initialBootID` _string_ | InitialBootID is the boot ID of the DPU OS during the first boot |  |  |
| `rebootMethod` _[RebootMethodType](#rebootmethodtype)_ | RebootMethod is the type of reset/reboot set by the DPU agent<br />See enum values in RebootMethodType.<br />No default is set intentionally: nil means "check not run or not applicable"<br />(e.g. legacy flow, or agent has not run the check yet);<br />a non-nil value means the check ran and this is the result. |  | Enum: [Unknown NoAction PowerCycle SystemReboot SystemLevelReset FirmwareReset DPUWarmReboot HostlessDPUReboot] <br />Optional: \{\} <br /> |
| `lastObservedPendingNvconfig` _[PendingNVConfigState](#pendingnvconfigstate)_ | LastObservedPendingNVConfig stores the last pending NVConfig parameters seen<br />during reboot-method discovery on this boot. It is used on the next boot to<br />ignore repeated parameters that remained unchanged across boots. |  | Optional: \{\} <br /> |
| `rebootSequenceCount` _integer_ | RebootSequenceCount is the length of the current non-NoAction RebootMethod sequence:<br />it increments on each agent run that reports a RebootMethod other than NoAction and<br />resets to 0 when the agent reports NoAction. Used with RebootMethod to bound host reboot loops. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `kubeletVersion` _string_ | KubeletVersion represents the kubelet version running on the DPU. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions contains the conditions reported from inside the DPU |  | Optional: \{\} <br /> |
| `spiffe` _[SpiffeStatus](#spiffestatus)_ | Spiffe contains the SPIFFE heartbeat status reported by the DPU Agent when running in<br />SPIFFE identity mode. |  | Optional: \{\} <br /> |


#### BFB



BFB is the Schema for the bfbs API



_Appears in:_
- [BFBList](#bfblist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `BFB` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[BFBSpec](#bfbspec)_ |  |  |  |
| `status` _[BFBStatus](#bfbstatus)_ |  | \{ phase:Initializing \} | Optional: \{\} <br /> |


#### BFBList



BFBList contains a list of BFB





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `BFBList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[BFB](#bfb) array_ |  |  |  |


#### BFBPhase

_Underlying type:_ _string_

BFBPhase describes current state of BFB CR.
Only one of the following state may be specified.
Default is Initializing.

_Validation:_
- Enum: [Initializing Downloading Ready Deleting Error]

_Appears in:_
- [BFBStatus](#bfbstatus)

| Field | Description |
| --- | --- |
| `Initializing` | BFB CR is created<br /> |
| `Downloading` | Downloading BFB file<br /> |
| `Ready` | Finished downloading BFB file, ready for DPU to use<br /> |
| `Deleting` | Delete BFB<br /> |
| `Error` | Error happens during BFB downloading<br /> |


#### BFBReference



BFBReference is a reference to a specific BFB



_Appears in:_
- [DPUTemplateSpec](#dputemplatespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Specifies name of the bfb CR to use for this DPU |  | MinLength: 1 <br /> |


#### BFBSpec



BFBSpec defines the content of the BFB



_Appears in:_
- [BFB](#bfb)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `fileName` _string_ | Specifies the file name where the BFB is downloaded on the volume. |  | Pattern: `^[A-Za-z0-9\_\-\.]+\.bfb$` <br />Optional: \{\} <br /> |
| `url` _string_ | The url of the bfb image to download. |  | Pattern: `^(http\|https)://.+$` <br />Required: \{\} <br /> |
| `versions` _[BFBVersions](#bfbversions)_ | Optionally specify BFB component versions. When set, these versions are<br />used directly in status instead of being extracted from the BFB file.<br />If set, all four fields (BSP, DOCA, UEFI, ATF) must be provided. |  | Optional: \{\} <br /> |


#### BFBStatus



BFBStatus defines the observed state of BFB



_Appears in:_
- [BFB](#bfb)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `fileName` _string_ | Filename is the name of the file where the BFB can be accessed on its volume.<br />This is the same as `.spec.Filename` if set. |  |  |
| `phase` _[BFBPhase](#bfbphase)_ | The current state of BFB. | Initializing | Enum: [Initializing Downloading Ready Deleting Error] <br />Required: \{\} <br /> |
| `versions` _[BFBVersions](#bfbversions)_ | BFB versions - BSP, DOCA, UEFI and ATF<br />Holds detailed version information for each component within the BFB |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions represent the latest available observations of BFB state |  | Optional: \{\} <br /> |


#### BFBVersions



BFBVersions represents the version information for BFB components.



_Appears in:_
- [BFBSpec](#bfbspec)
- [BFBStatus](#bfbstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bsp` _string_ | BSP (Board Support Package) version.<br />This field stores the version of the BSP, which provides essential<br />support and drivers for the hardware platform. |  | Optional: \{\} <br /> |
| `doca` _string_ | DOCA version<br />Specifies the version of NVIDIA's Data Center-on-a-Chip Architecture (DOCA),<br />a platform for developing applications on DPUs |  | Optional: \{\} <br /> |
| `uefi` _string_ | UEFI (Unified Extensible Firmware Interface) version.<br />Indicates the UEFI firmware version, which is responsible for booting<br />the operating system and initializing hardware components |  | Optional: \{\} <br /> |
| `atf` _string_ | ATF (Arm Trusted Firmware) version.<br />Contains the version of ATF, which provides a secure runtime environment |  | Optional: \{\} <br /> |


#### BlueFieldSoftware



BlueFieldSoftware is the Schema for the bluefieldsoftware API



_Appears in:_
- [BlueFieldSoftwareList](#bluefieldsoftwarelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `BlueFieldSoftware` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[BlueFieldSpec](#bluefieldspec)_ |  |  |  |
| `status` _[BlueFieldSoftwareStatus](#bluefieldsoftwarestatus)_ |  | \{ phase:Initializing \} | Optional: \{\} <br /> |


#### BlueFieldSoftwareList



BlueFieldSoftwareList contains a list of BlueFieldSoftware





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `BlueFieldSoftwareList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[BlueFieldSoftware](#bluefieldsoftware) array_ |  |  |  |


#### BlueFieldSoftwarePhase

_Underlying type:_ _string_

BlueFieldSoftwarePhase describes current state of BlueFieldSoftware CR.
Only one of the following state may be specified.
Default is Initializing.

_Validation:_
- Enum: [Initializing Downloading Extracting Ready Deleting Error]

_Appears in:_
- [BlueFieldSoftwareStatus](#bluefieldsoftwarestatus)

| Field | Description |
| --- | --- |
| `Initializing` | BlueFieldSoftware CR is created<br /> |
| `Downloading` | Downloading BlueFieldSoftware components<br /> |
| `Extracting` | Extracting BlueFieldSoftware components from downloaded bundle<br /> |
| `Ready` | Finished downloading BlueFieldSoftware components, ready for DPU to use<br /> |
| `Deleting` | Delete BlueFieldSoftware<br /> |
| `Error` | Error happens during BlueFieldSoftware downloading<br /> |


#### BlueFieldSoftwareReference



BlueFieldSoftwareReference is a reference to a specific BlueFieldSoftware



_Appears in:_
- [DPUTemplateSpec](#dputemplatespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Specifies name of the BlueFieldSoftware CR to use for this DPU |  | MinLength: 1 <br /> |


#### BlueFieldSoftwareStatus



BlueFieldSoftwareStatus defines the observed state of BlueFieldSoftware



_Appears in:_
- [BlueFieldSoftware](#bluefieldsoftware)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[BlueFieldSoftwarePhase](#bluefieldsoftwarephase)_ | The current state of BlueFieldSoftware. | Initializing | Enum: [Initializing Downloading Extracting Ready Deleting Error] <br />Required: \{\} <br /> |
| `versions` _[BluefieldSoftwareVersions](#bluefieldsoftwareversions)_ | Versions tracks the versions of the components |  | Optional: \{\} <br /> |
| `downloadedComponents` _[DownloadedComponents](#downloadedcomponents)_ | DownloadedComponents tracks which components have been successfully downloaded |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions represent the latest available observations of BlueFieldSoftware state |  | Optional: \{\} <br /> |


#### BlueFieldSpec



BlueFieldSpec defines the desired state of BlueFieldSoftware.



_Appears in:_
- [BlueFieldSoftware](#bluefieldsoftware)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pldmFwBundle` _string_ |  |  | Optional: \{\} <br /> |
| `osIso` _string_ |  |  | Optional: \{\} <br /> |


#### BluefieldSoftwareVersions



BluefieldSoftwareVersions defines the versions of various software components for a Bluefield device.



_Appears in:_
- [BlueFieldSoftwareStatus](#bluefieldsoftwarestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `fwBundleVersion` _string_ |  |  |  |
| `osISOVersion` _string_ |  |  |  |
| `astraNicFwVersion` _string_ |  |  |  |
| `bmcVersion` _string_ |  |  |  |
| `bmcErotVersion` _string_ |  |  |  |
| `sbiosVersion` _string_ |  |  |  |


#### ClusterEndpointSpec







_Appears in:_
- [DPUClusterSpec](#dpuclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `keepalived` _[KeepalivedSpec](#keepalivedspec)_ | Keepalived configures the keepalived that will be deployed for the cluster control-plane |  | Optional: \{\} <br /> |


#### ClusterPhase

_Underlying type:_ _string_

ClusterPhase describes current state of DPUCluster.
Only one of the following state may be specified.
Default is Pending.

_Validation:_
- Enum: [Pending Creating Ready NotReady Failed]

_Appears in:_
- [DPUClusterStatus](#dpuclusterstatus)

| Field | Description |
| --- | --- |
| `Pending` |  |
| `Creating` |  |
| `Ready` |  |
| `NotReady` |  |
| `Failed` |  |


#### ClusterSpec







_Appears in:_
- [DPUTemplateSpec](#dputemplatespec)
- [K8sCluster](#k8scluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeLabels` _object (keys:string, values:string)_ | NodeLabels specifies the labels to be added to the node. |  | Optional: \{\} <br /> |
| `nodeAnnotations` _object (keys:string, values:string)_ | NodeAnnotations specifies the annotations to be added to the node. |  | Optional: \{\} <br /> |
| `selector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Selector defines the selector of the DPUClusters the produced DPUs should join |  | Optional: \{\} <br /> |






#### ConfigFile







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `path` _string_ | Path is the path of the file to be written. |  | Optional: \{\} <br /> |
| `operation` _[DPUFlavorFileOp](#dpuflavorfileop)_ | Operation is the operation to be performed on the file. |  | Enum: [override append] <br />Optional: \{\} <br /> |
| `raw` _string_ | Raw is the raw content of the file. |  | Optional: \{\} <br /> |
| `permissions` _string_ | Permissions are the permissions to be set on the file. |  | Optional: \{\} <br /> |


#### ContainerdConfig







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `registryEndpoint` _string_ | RegistryEndpoint is the endpoint of the container registry. |  | Optional: \{\} <br /> |


#### DMSAddress



DMSAddress represents the IP and Port configuration for DMS.



_Appears in:_
- [DPUNodeSpec](#dpunodespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ip` _string_ | IP address in IPv4 format. |  | Format: ipv4 <br /> |
| `port` _integer_ | Port number. |  | Minimum: 1 <br /> |


#### DPU



DPU is the Schema for the dpus API



_Appears in:_
- [DPUList](#dpulist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPU` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUSpec](#dpuspec)_ |  |  |  |
| `status` _[DPUStatus](#dpustatus)_ |  | \{ phase:Initializing \} | Optional: \{\} <br /> |


#### DPUCluster



DPUCluster is the Schema for the dpuclusters API



_Appears in:_
- [DPUClusterList](#dpuclusterlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUCluster` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUClusterSpec](#dpuclusterspec)_ |  |  | Required: \{\} <br /> |
| `status` _[DPUClusterStatus](#dpuclusterstatus)_ |  | \{ phase:Pending \} | Optional: \{\} <br /> |


#### DPUClusterList



DPUClusterList contains a list of DPUCluster





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUClusterList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUCluster](#dpucluster) array_ |  |  |  |


#### DPUClusterSpec



DPUClusterSpec defines the desired state of DPUCluster



_Appears in:_
- [DPUCluster](#dpucluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type of the cluster with few supported values<br />static - existing cluster that is deployed by user. For DPUCluster of this type, the kubeconfig field must be set.<br />kamaji - DPF managed cluster. The kamaji-cluster-manager will create a DPU cluster on behalf of this CR.<br />$(others) - any string defined by ISVs, such type names must start with a prefix. |  | Pattern: `kamaji\|static\|[^/]+/.*` <br />Required: \{\} <br /> |
| `maxNodes` _integer_ | MaxNodes is the max amount of node in the cluster | 1000 | Maximum: 1000 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `kubeconfig` _string_ | Kubeconfig is the secret that contains the admin kubeconfig |  | Optional: \{\} <br /> |
| `clusterEndpoint` _[ClusterEndpointSpec](#clusterendpointspec)_ | ClusterEndpoint contains configurations of the cluster entry point |  | Optional: \{\} <br /> |


#### DPUClusterStatus



DPUClusterStatus defines the observed state of DPUCluster



_Appears in:_
- [DPUCluster](#dpucluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[ClusterPhase](#clusterphase)_ |  | Pending | Enum: [Pending Creating Ready NotReady Failed] <br /> |
| `version` _string_ | Version is the K8s control-plane version of the cluster |  | Optional: \{\} <br /> |
| `nodesCount` _integer_ | NodesCount is the number of DPUs assigned to the cluster |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ |  |  | Optional: \{\} <br /> |






#### DPUDevice



DPUDevice is the Schema for the dpudevices API



_Appears in:_
- [DPUDeviceList](#dpudevicelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDevice` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUDeviceSpec](#dpudevicespec)_ |  |  |  |
| `status` _[DPUDeviceStatus](#dpudevicestatus)_ |  |  |  |


#### DPUDeviceClusterSpec



DPUDeviceClusterSpec holds node labels and annotations propagated from DPUDevice to the DPU and cluster node.



_Appears in:_
- [DPUDeviceSpec](#dpudevicespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeLabels` _object (keys:string, values:string)_ | NodeLabels specifies labels to be added to the DPU cluster node for this device. |  | Optional: \{\} <br /> |
| `nodeAnnotations` _object (keys:string, values:string)_ | NodeAnnotations specifies annotations to be added to the DPU cluster node for this device. |  | Optional: \{\} <br /> |


#### DPUDeviceList



DPUDeviceList contains a list of DPUDevices





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDeviceList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUDevice](#dpudevice) array_ |  |  |  |


#### DPUDeviceSpec



DPUDeviceSpec defines the content of DPUDevice



_Appears in:_
- [DPUDevice](#dpudevice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `psid` _string_ | PSID is the Product Serial ID of the device.<br />It's used to track the device's lifecycle and for inventory management.<br />This value is immutable and should not be changed once set.<br />Example: "MT_0001234567", "MT25066004C7"<br />Deprecated: This field is deprecated and will be removed in a future version. Use status.psid instead. |  | Optional: \{\} <br /> |
| `serialNumber` _string_ | SerialNumber is the serial number of the device.<br />It's used to track the device's lifecycle and for inventory management.<br />This value is immutable and should not be changed once set.<br />Example: "MT_0001234567", "MT25066004C7" |  | MinLength: 1 <br />Required: \{\} <br /> |
| `opn` _string_ | OPN is the Ordering Part Number of the device.<br />It's used to track the device's compatibility with different software versions.<br />This value is immutable and should not be changed once set.<br />Example: "900-9D3B4-00SV-EA0"<br />Deprecated: This field is deprecated and will be removed in a future version. Use status.opn instead. |  | Optional: \{\} <br /> |
| `bmcIp` _string_ | BMCIP is the IP address of the BMC (Base Management Controller) on the device.<br />This is used for remote management and monitoring of the device.<br />Example: "10.1.2.3" |  | Format: ipv4 <br />Optional: \{\} <br /> |
| `bmcPort` _integer_ | BMCPort is the port number of the BMC (Base Management Controller) on the device.<br />This is used for remote management and monitoring of the device.<br />This value is immutable and should not be changed once set.<br />Example: 443 | 443 | Minimum: 1 <br />Optional: \{\} <br /> |
| `numberOfPFs` _integer_ | NumberOfPFs is the number of PFs on the device.<br />This value is immutable and should not be changed once set.<br />Example: 1 | 1 | Minimum: 1 <br />Optional: \{\} <br /> |
| `nicDeviceCount` _integer_ | NICDeviceCount is the expected number of NIC devices used by dpu-agent provisioning.<br />Valid range is 1 to 8. When unspecified, it defaults to 8. |  | Maximum: 8 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `pf0Name` _string_ | PF0Name is the name of the PF0 on the device.<br />This value is immutable and should not be changed once set.<br />Example: "eth0"<br />Deprecated: This field is deprecated and will be removed in a future version. Use status.pf0Name instead. |  | Optional: \{\} <br /> |
| `bmcCredentialSecretName` _string_ | BMCCredentialSecretName is the name of a Secret in the same namespace containing<br />per-device BMC credentials. The secret must contain a "password" key with the BMC credential value.<br />If specified, this password takes precedence over the shared bmc-shared-password secret. |  | Optional: \{\} <br /> |
| `cluster` _[DPUDeviceClusterSpec](#dpudeviceclusterspec)_ | Specifies details on the K8S cluster to join |  | Optional: \{\} <br /> |
| `values` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Values contains free-form per-device values used to render a DPUFlavorTemplate<br />into a concrete generated DPUFlavor for this device. |  | Optional: \{\} <br /> |


#### DPUDeviceStatus







_Appears in:_
- [DPUDevice](#dpudevice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `psid` _string_ | PSID is the Product Serial ID of the device.<br />It's used to track the device's lifecycle and for inventory management.<br />This value is discovered and should not be changed once set.<br />Example: "MT_0001234567", "MT25066004C7" |  | Optional: \{\} <br /> |
| `serialNumber` _string_ | SerialNumber is the serial number of the device.<br />It's used to track the device's lifecycle and for inventory management.<br />This value is discovered and should not be changed once set.<br />Example: "MT_0001234567", "MT25066004C7" |  | Optional: \{\} <br /> |
| `opn` _string_ | OPN is the Ordering Part Number of the device.<br />It's used to track the device's compatibility with different software versions.<br />This value is discovered and should not be changed once set.<br />Example: "900-9D3B4-00SV-EA0" |  | Optional: \{\} <br /> |
| `bmcIp` _string_ | BMCIP is the IP address of the BMC (Base Management Controller) on the device.<br />This is used for remote management and monitoring of the device.<br />This value is discovered and should not be changed once set.<br />Example: "10.1.2.3" |  | Format: ipv4 <br />Optional: \{\} <br /> |
| `bmcPort` _integer_ | BMCPort is the port number of the BMC (Base Management Controller) on the device.<br />This is used for remote management and monitoring of the device.<br />This value is immutable and should not be changed once set.<br />Example: 443 | 443 | Minimum: 1 <br />Optional: \{\} <br /> |
| `pciAddress` _string_ | PCIAddress is the PCI address of the device in the host system.<br />Example: "0000-03-00", "03-00" |  | Optional: \{\} <br /> |
| `pf0Name` _string_ | PF0Name is the name of the PF0 on the device.<br />Example: "eth0" |  | Optional: \{\} <br /> |
| `pf0Mac` _string_ | PF0MAC is the MAC address of the PF0 on the device.<br />Example: "00:00:00:00:00:00" |  | Pattern: `^([0-9A-Fa-f]\{2\}[:-])\{5\}([0-9A-Fa-f]\{2\})$` <br />Optional: \{\} <br /> |
| `dpuType` _[DPUType](#dputype)_ | DPUType is the type of the DPU. | Unknown | Enum: [Unknown BlueField2 BlueField3 BlueField4] <br />Optional: \{\} <br /> |
| `dpuMode` _[DpuModeType](#dpumodetype)_ | DPUMode is the mode of the DPU. | dpu | Enum: [dpu nic] <br />Optional: \{\} <br /> |
| `secureBoot` _[SecureBootStatus](#securebootstatus)_ | SecureBoot indicates the current UEFI Secure Boot state. |  | Optional: \{\} <br /> |
| `bmcCredentialSecretName` _string_ | BMCCredentialSecretName is the name of the Secret last used successfully for BMC authentication. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ |  |  | Optional: \{\} <br /> |


#### DPUDiscovery







_Appears in:_
- [DPUDiscoveryList](#dpudiscoverylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDiscovery` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUDiscoverySpec](#dpudiscoveryspec)_ |  |  |  |
| `status` _[DPUDiscoveryStatus](#dpudiscoverystatus)_ |  |  |  |


#### DPUDiscoveryList



DPUDiscoveryList contains a list of DPUDiscovery types





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDiscoveryList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUDiscovery](#dpudiscovery) array_ |  |  |  |


#### DPUDiscoverySpec



DPUDiscoverySpec defines the desired state of DPUDiscovery



_Appears in:_
- [DPUDiscovery](#dpudiscovery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipRangeSpec` _[IPRangeValidationSpec](#iprangevalidationspec)_ | IPRange defines the range of IP addresses to scan |  |  |
| `scanInterval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | ScanInterval defines how often to perform the scan | 1h |  |
| `workers` _integer_ | Workers defines the number of workers to use for the scan (default 1 worker for each 255 IPs in the range) |  | Optional: \{\} <br /> |


#### DPUDiscoveryStatus



DPUDiscoveryStatus defines the observed state of DPUDiscovery



_Appears in:_
- [DPUDiscovery](#dpudiscovery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `observedGeneration` _integer_ | ObservedGeneration is the generation of the spec that was last applied by a successful scan.<br />When spec (e.g. IP range) changes, reconciliation runs a scan immediately instead of waiting for the next interval. |  | Optional: \{\} <br /> |
| `lastScanTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastScanTime is the timestamp of the last successful scan |  |  |
| `foundDPUs` _integer_ | FoundDPUs is the list of discovered DPU BMC IPs |  |  |


#### DPUFLavorSysctl







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `parameters` _string array_ | Parameters are the sysctl parameters to be set. |  | Optional: \{\} <br /> |


#### DPUFlavor



DPUFlavor is the Schema for the dpuflavors API



_Appears in:_
- [DPUFlavorList](#dpuflavorlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUFlavor` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUFlavorSpec](#dpuflavorspec)_ |  |  |  |


#### DPUFlavorFileOp

_Underlying type:_ _string_

DPUFlavorFileOp defines the operation to be performed on the file

_Validation:_
- Enum: [override append]

_Appears in:_
- [ConfigFile](#configfile)

| Field | Description |
| --- | --- |
| `override` |  |
| `append` |  |


#### DPUFlavorGrub







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `kernelParameters` _string array_ | KernelParameters are the kernel parameters to be set in the grub configuration. |  | Optional: \{\} <br /> |


#### DPUFlavorList



DPUFlavorList contains a list of DPUFlavor





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUFlavorList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUFlavor](#dpuflavor) array_ |  |  |  |


#### DPUFlavorOVS







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `rawConfigScript` _string_ | RawConfigScript is the raw configuration script for OVS. |  | Optional: \{\} <br /> |


#### DPUFlavorSpec



DPUFlavorSpec defines the content of DPUFlavor



_Appears in:_
- [DPUFlavor](#dpuflavor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `grub` _[DPUFlavorGrub](#dpuflavorgrub)_ | Grub contains the grub configuration for the DPUFlavor. |  | Optional: \{\} <br /> |
| `sysctl` _[DPUFLavorSysctl](#dpuflavorsysctl)_ | Sysctl contains the sysctl configuration for the DPUFlavor. |  | Optional: \{\} <br /> |
| `nvconfig` _[NVConfig](#nvconfig) array_ | NVConfig contains the device-specific configuration (firmware settings, device parameters).<br />Each entry specifies a device (wildcard '*', or port identifiers 'p0'/'P0'/'p1'/'P1') and its parameters.<br />If device is '*' or unspecified (defaults to '*'), it applies to all devices and must be the only entry.<br />Each device (including unspecified as '*') must be unique across all nvconfig entries (case-insensitive).<br />Validation enforces: device enum values, parameter format (KEY=VALUE), case-insensitive uniqueness, and size limits. |  | MaxItems: 3 <br />Optional: \{\} <br /> |
| `ovs` _[DPUFlavorOVS](#dpuflavorovs)_ | OVS contains the OVS configuration for the DPUFlavor. |  | Optional: \{\} <br /> |
| `bfcfgParameters` _string array_ | BFCfgParameters are the parameters to be set in the bf.cfg file. |  | Optional: \{\} <br /> |
| `configFiles` _[ConfigFile](#configfile) array_ | ConfigFiles are the files to be written on the DPU. |  | Optional: \{\} <br /> |
| `packages` _[PackageSpec](#packagespec) array_ | Packages are the packages to reconcile on the node. |  | MaxItems: 64 <br />Optional: \{\} <br /> |
| `systemdServices` _[SystemdServiceSpec](#systemdservicespec) array_ | SystemdServices are the systemd services to manage on the node. |  | MaxItems: 64 <br />Optional: \{\} <br /> |
| `containerdConfig` _[ContainerdConfig](#containerdconfig)_ | ContainerdConfig contains the configuration for containerd. |  | Optional: \{\} <br /> |
| `dpuResources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | DPUResources indicates the minimum amount of resources needed for a BFB with that flavor to be installed on a<br />DPU. Using this field, the controller can understand if that flavor can be installed on a particular DPU. It<br />should be set to the total amount of resources the system needs + the resources that should be made available for<br />DPUServices to consume. |  | Optional: \{\} <br /> |
| `systemReservedResources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | SystemReservedResources indicates the resources that are consumed by the system (OS, OVS, DPF system etc) and are<br />not made available for DPUServices to consume. DPUServices can consume the difference between DPUResources and<br />SystemReservedResources. This field must not be specified if dpuResources are not specified. |  | Optional: \{\} <br /> |
| `dpuMode` _[DpuModeType](#dpumodetype)_ | DpuMode is deprecated and no longer used by provisioning workflows.<br />Deployment mode is sourced from DPFOperatorConfig and exposed on DPU.status.deploymentMode. |  | Enum: [dpu zero-trust nic] <br />Optional: \{\} <br /> |
| `hostNetworkInterfaceConfigs` _[NetworkInterfaceConfig](#networkinterfaceconfig) array_ | HostNetworkInterfaceConfigs contains the configuration for the host-side network interfaces. |  | Optional: \{\} <br /> |
| `ewNicConfigurations` _[NicConfiguration](#nicconfiguration) array_ | EWNicConfigurations lists per-NIC configuration for the E/W NICs.<br />Only the first entry is applied in this release; additional entries are ignored until a future<br />release adds multi-NIC support. The field is modeled as a list now so the API shape does not<br />need to change when multiple entries are supported. |  | MaxItems: 16 <br />Optional: \{\} <br /> |


#### DPUFlavorTemplate



DPUFlavorTemplate is the Schema for the dpuflavortemplates API



_Appears in:_
- [DPUFlavorTemplateList](#dpuflavortemplatelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUFlavorTemplate` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUFlavorTemplateSpec](#dpuflavortemplatespec)_ |  |  |  |


#### DPUFlavorTemplateList



DPUFlavorTemplateList contains a list of DPUFlavorTemplate





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUFlavorTemplateList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUFlavorTemplate](#dpuflavortemplate) array_ |  |  |  |


#### DPUFlavorTemplateSpec



DPUFlavorTemplateSpec defines the content of a DPUFlavorTemplate. The template body is
rendered per-DPU against DPUDevice.spec.values to produce a concrete DPUFlavor.



_Appears in:_
- [DPUFlavorTemplate](#dpuflavortemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `template` _string_ | Template is the DPUFlavor body as a YAML/JSON string with Go template actions<br />(delimited by double curly braces). It is rendered against DPUDevice.spec.values and the result is<br />unmarshalled into a typed DPUFlavor and validated by DPUFlavor admission when<br />the generated flavor is created. It must NOT contain dpuResources or<br />systemReservedResources; those are provided by the structured fields below. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `dpuResources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | DPUResources is resource-fitting metadata mirrored from DPUFlavor. It is NOT<br />templated: when set it is stamped onto every generated DPUFlavor and takes<br />precedence over anything in the rendered body. |  | Optional: \{\} <br /> |
| `systemReservedResources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | SystemReservedResources is resource-fitting metadata mirrored from DPUFlavor. It is<br />NOT templated: when set it is stamped onto every generated DPUFlavor and takes<br />precedence over anything in the rendered body. |  | Optional: \{\} <br /> |






#### DPUList



DPUList contains a list of DPU





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPU](#dpu) array_ |  |  |  |


#### DPUNode



DPUNode is the Schema for the dpunodes API



_Appears in:_
- [DPUNodeList](#dpunodelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUNode` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUNodeSpec](#dpunodespec)_ |  |  |  |
| `status` _[DPUNodeStatus](#dpunodestatus)_ |  |  |  |






#### DPUNodeList



DPUNodeList contains a list of DPUNode





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUNodeList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUNode](#dpunode) array_ |  |  |  |


#### DPUNodeMaintenance



DPUNodeMaintenance is the Schema for the dpunodemaintenances API



_Appears in:_
- [DPUNodeMaintenanceList](#dpunodemaintenancelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUNodeMaintenance` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUNodeMaintenanceSpec](#dpunodemaintenancespec)_ |  |  |  |
| `status` _[DPUNodeMaintenanceStatus](#dpunodemaintenancestatus)_ |  |  |  |


#### DPUNodeMaintenanceList



DPUNodeMaintenanceList contains a list of DPUNodeMaintenance





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUNodeMaintenanceList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUNodeMaintenance](#dpunodemaintenance) array_ |  |  |  |


#### DPUNodeMaintenanceSpec



DPUNodeMaintenanceSpec is the specification of the DPUNodeMaintenance object



_Appears in:_
- [DPUNodeMaintenance](#dpunodemaintenance)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuNodeName` _string_ | DPUNodeName is the name of the DPUNode that is being maintained. |  | Required: \{\} <br /> |
| `nodeEffect` _[NodeEffect](#nodeeffect)_ | NodeEffect is the effect to be applied to the node. |  | Optional: \{\} <br /> |
| `requestor` _string array_ | Requestor is the list of consumers for the maintenance. |  | Optional: \{\} <br /> |


#### DPUNodeMaintenanceStatus



DPUNodeMaintenanceStatus defines the observed state of DPUNodeMaintenance



_Appears in:_
- [DPUNodeMaintenance](#dpunodemaintenance)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `nodeEffectSyncStartTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | NodeEffectSyncStartTime is the time when the node effect sync started. |  | Optional: \{\} <br /> |
| `multiDPUOperationsSyncWaitTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | MultiDPUOperationsSyncWaitTime  is the wait time between DPUs on the same node. |  | Optional: \{\} <br /> |
| `maxUnavailableDPUNodes` _integer_ | MaxUnavailableDPUNodes is the maximum number of DPUNodes that are unavailable during the node effect period. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### DPUNodeSpec



DPUNodeSpec defines the desired state of DPUNode



_Appears in:_
- [DPUNode](#dpunode)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeRebootMethod` _[NodeRebootMethod](#noderebootmethod)_ | Defines the method for rebooting the host.<br />One of the following options can be chosen for this field:<br />   - "external": Reboot the host via an external means, not controlled by the<br />     DPU controller.<br />   - "script": Reboot the host by executing a custom script.<br />   - "hostAgent": Use the host agent to reboot the host.<br />   - "none": Do not reboot a host through the DPUNode.<br />"hostAgent" is the default value. | \{ hostAgent:map[] \} | Optional: \{\} <br /> |
| `nodeDMSAddress` _[DMSAddress](#dmsaddress)_ | The IP address and port where the DMS is exposed. Only applicable if dpuInstallInterface is set to gNOI.<br />Deprecated: this field is no longer used. |  | Optional: \{\} <br /> |
| `dpus` _[DPURef](#dpuref) array_ | A map containing names of each DPUDevice attached to the node. |  | Optional: \{\} <br /> |


#### DPUNodeStatus



DPUNodeStatus defines the observed state of DPUNode



_Appears in:_
- [DPUNode](#dpunode)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions represent the latest available observations of an object's state. |  | Type: array <br />Optional: \{\} <br /> |
| `dpuInstallInterface` _string_ | The name of the interface which will be used to install the bfb image, can be one of hostAgent,redfish |  | Enum: [gNOI hostAgent redfish] <br />Optional: \{\} <br /> |
| `kubeNodeRef` _string_ | The name of the Kubernetes Node object that this DPUNode represents.<br />This field is optional and only relevant if the x86 host is part of the DPF Kubernetes cluster. |  | Optional: \{\} <br /> |
| `rebootInProgress` _boolean_ | RebootInProgress indicates if the node is in the process of rebooting. |  | Optional: \{\} <br /> |
| `rebootMethod` _[RebootMethodType](#rebootmethodtype)_ | RebootMethod is the host-level reboot method recommended by child DPUs in<br />DPURebooting phase, aggregated by priority (most disruptive wins, ties broken<br />by ascending DPU name):<br />PowerCycle > SystemLevelReset > SystemReboot > HostlessDPUReboot > FirmwareReset > DPUWarmReboot > NoAction > Unknown.<br />Stamped once at least one DPU reports a method, preserved across the<br />rebooting -> idle transition, and cleared with DPUNodeRebootInProgress<br />when the DPUNode loses all its DPUs. |  | Enum: [Unknown NoAction PowerCycle SystemReboot SystemLevelReset FirmwareReset DPUWarmReboot HostlessDPUReboot] <br />Optional: \{\} <br /> |




#### DPUOutdated



DPUOutdated reports that the DPU has drifted from its owning DPUSet's
DPUTemplate in a way that requires the DPU to be reprovisioned. The struct
is presence-based: when the DPU matches the template, DPUStatus.Outdated is
nil. The DPUSet controller is the sole writer.



_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `timeStamp` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | TimeStamp records when this drift was first observed for the current Reason.<br />It is preserved across reconciles as long as Reason is unchanged. |  | Required: \{\} <br /> |
| `reason` _string_ | Reason is the machine-readable drift code (e.g. OutdatedBFB). When more<br />than one template field has drifted, Reason reports the first in fixed<br />precedence order: BFB -> DPUFlavor -> SecureBoot -> BlueFieldSoftware. |  | Required: \{\} <br /> |
| `message` _string_ | Message is a human-readable summary that lists every drifted field,<br />e.g. "DPU template has changed (BFB: bfb-v1 -> bfb-v2, DPUFlavor: ...)". |  | Required: \{\} <br /> |


#### DPUPhase

_Underlying type:_ _string_

DPUPhase describes current state of DPU.
Only one of the following state may be specified.
Default is Initializing.

_Validation:_
- Enum: [Initializing Node Effect Pending Update Firmware Config FW Parameters Prepare BFB OS Installing DPU Config DPU Cluster Config Host Network Configuration Ready Error Deleting Rebooting Perform ARM Force Restart Initialize Interface Node Effect Removal Checking Host Reboot Required]

_Appears in:_
- [DPUSetStatus](#dpusetstatus)
- [DPUStatus](#dpustatus)

| Field | Description |
| --- | --- |
| `Initializing` | DPUInitializing is the first phase after the DPU is created.<br /> |
| `Node Effect` | DPUNodeEffect means the controller will handle the node effect provided by the user.<br /> |
| `Pending` | DPUPending means the controller is waiting for the BFB to be ready.<br /> |
| `Prepare BFB` | DPUPrepareBFB means the controller is preparing the BFB and bf.cfg to be installed to DPU<br /> |
| `Update Firmware` | DPUUpdateFirmware means the controller will update the DPU firmware for BlueField4.<br /> |
| `DPU Config` | DPUConfig means the DPU agent will configure the DPU<br /> |
| `Config FW Parameters` | DPUConfigFWParameters means the controller will manipulate DPU firmware, e.g., set DPU mode, check firmware version<br /> |
| `Initialize Interface` | DPUInitializeInterface means the controller will intitialize the interface used to provision the DPUs, e.g., create the DMS pod, set up RedFish account.<br /> |
| `OS Installing` | DPUOSInstalling means the controller will provision the DPU through the DMS gNOI interface.<br /> |
| `DPU Cluster Config` | DPUClusterConfig  means the node configuration and Kubernetes Node join procedure are in progress .<br /> |
| `Host Network Configuration` | DPUHostNetworkConfiguration means the host network configuration is running.<br /> |
| `Node Effect Removal` | DPUNodeEffectRemoval means the controller will remove the node effect from the DPU.<br /> |
| `Ready` | DPUReady means the DPU is ready to use.<br /> |
| `Error` | DPUError means error occurred.<br /> |
| `Deleting` | DPUDeleting means the DPU CR will be deleted, controller will do some cleanup works.<br /> |
| `Rebooting` | DPURebooting means the host of DPU is rebooting.<br /> |
| `Perform ARM Force Restart` | DPUPerformArmForceRestart means ARM ForceRestart operations are in progress for Secure Boot configuration.<br /> |


#### DPURef







_Appears in:_
- [DPUNodeSpec](#dpunodespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name of the DPU device. |  | MinLength: 1 <br />Required: \{\} <br /> |


#### DPUSet



DPUSet is the Schema for the dpusets API



_Appears in:_
- [DPUSetList](#dpusetlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUSet` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUSetSpec](#dpusetspec)_ |  |  |  |
| `status` _[DPUSetStatus](#dpusetstatus)_ |  |  |  |


#### DPUSetList



DPUSetList contains a list of DPUSet





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUSetList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUSet](#dpuset) array_ |  |  |  |


#### DPUSetSpec



DPUSetSpec defines the desired state of DPUSet



_Appears in:_
- [DPUSet](#dpuset)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `strategy` _[DPUSetStrategy](#dpusetstrategy)_ | The rolling update strategy to use to updating existing DPUs with new ones. |  | Required: \{\} <br /> |
| `dpuNodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Select the DPUNodes with specific labels |  | Optional: \{\} <br /> |
| `dpuSelector` _object (keys:string, values:string)_ | Select the DPU with specific labels<br />Deprecated: This field is deprecated and will be removed with v26.7.0. Use DPUDeviceSelector instead. |  | Optional: \{\} <br /> |
| `dpuDeviceSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUDeviceSelector defines the selector for DPUDevices that the DPUSet should target and should create a DPU for. |  | Optional: \{\} <br /> |
| `dpuTemplate` _[DPUTemplate](#dputemplate)_ | Object that describes the DPU that will be created if insufficient replicas are detected |  | Required: \{\} <br /> |


#### DPUSetStatus



DPUSetStatus defines the observed state of DPUSet



_Appears in:_
- [DPUSet](#dpuset)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuStatistics` _object (keys:[DPUPhase](#dpuphase), values:integer)_ | DPUStatistics is a map of DPUPhase to the number of DPUs in that phase. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUSetStrategy







_Appears in:_
- [DPUSetSpec](#dpusetspec)
- [DPUs](#dpus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[StrategyType](#strategytype)_ | Can be "OnDelete" or "RollingUpdate". |  | Enum: [OnDelete RollingUpdate] <br />Required: \{\} <br /> |
| `rollingUpdate` _[RollingUpdateDPU](#rollingupdatedpu)_ | Rolling update config params. Present only if StrategyType = RollingUpdate. |  | Optional: \{\} <br /> |


#### DPUSpec



DPUSpec defines the desired state of DPU



_Appears in:_
- [DPU](#dpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuNodeName` _string_ | Specifies the DPUNode this DPU belongs to |  | Required: \{\} <br /> |
| `dpuDeviceName` _string_ | Specifies the name of the DPUDevice this DPU is associated with |  | MinLength: 1 <br />Required: \{\} <br /> |
| `bfb` _string_ | Specifies name of the bfb CR to use for this DPU |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `blueFieldSoftware` _string_ | Specifies the name of the BlueFieldSoftware CR to use for this DPU |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `serialNumber` _string_ | The serial number of the DPU |  | MinLength: 1 <br />Required: \{\} <br /> |
| `pciAddress` _string_ | The PCI device related DPU<br />Example: "0000-03-00", "03-00" |  | Pattern: `^([0-9a-fA-F]\{4\}[-])?[0-9a-fA-F]\{2\}[-][0-9a-fA-F]\{2\}$` <br />Optional: \{\} <br /> |
| `nodeEffect` _[NodeEffect](#nodeeffect)_ | Specifies how changes to the DPU should affect the Node |  | Required: \{\} <br /> |
| `cluster` _[K8sCluster](#k8scluster)_ | Specifies details on the K8S cluster to join |  | Optional: \{\} <br /> |
| `dpuFlavor` _string_ | DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `astraEnabled` _boolean_ | AstraEnabled indicates whether E/W NIC configuration (Astra) is enabled |  | Optional: \{\} <br /> |
| `secureBoot` _boolean_ | SecureBoot specifies whether UEFI Secure Boot should be enabled. |  | Optional: \{\} <br /> |
| `bmcIP` _string_ | BMCIP is the ip address of the DPU BMC<br />Deprecated: Use BMCIP from DPUDevice instead. |  | Optional: \{\} <br /> |


#### DPUStatus



DPUStatus defines the observed state of DPU



_Appears in:_
- [DPU](#dpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[DPUPhase](#dpuphase)_ | The current state of DPU. | Initializing | Enum: [Initializing Node Effect Pending Update Firmware Config FW Parameters Prepare BFB OS Installing DPU Config DPU Cluster Config Host Network Configuration Ready Error Deleting Rebooting Perform ARM Force Restart Initialize Interface Node Effect Removal Checking Host Reboot Required] <br />Required: \{\} <br /> |
| `previousPhase` _[DPUPhase](#dpuphase)_ | PreviousPhase is the last non-empty Phase before the current Phase, set by the controller<br />when Phase transitions. It may be unset during early initialization (empty Phase) or until<br />the first transition from a non-empty Phase. Internal controller tracking only. |  | Enum: [Initializing Node Effect Pending Update Firmware Config FW Parameters Prepare BFB OS Installing DPU Config DPU Cluster Config Host Network Configuration Ready Error Deleting Rebooting Perform ARM Force Restart Initialize Interface Node Effect Removal Checking Host Reboot Required] <br />Optional: \{\} <br /> |
| `outdated` _[DPUOutdated](#dpuoutdated)_ | Outdated, when present, indicates the DPU has drifted from its owning<br />DPUSet's DPUTemplate and needs to be reprovisioned. Set by the DPUSet<br />controller; absent when the DPU matches the template. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions represents the provisioning lifecycle conditions. |  | Optional: \{\} <br /> |
| `operationalConditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | OperationalConditions represents aggregated operational readiness conditions.<br />These conditions reflect the runtime health and readiness of DPU services and node health,<br />separate from the provisioning lifecycle represented by Conditions. |  | Optional: \{\} <br /> |
| `bfbFile` _string_ | BFBFile is the path to the BFB file |  | Optional: \{\} <br /> |
| `bfCFGFile` _string_ | BFCFGFile is the path to the bf.cfg |  | Optional: \{\} <br /> |
| `bfbVersion` _string_ | bfb version of this DPU |  | Optional: \{\} <br /> |
| `dpfVersion` _string_ | DPF version used to install this DPU |  | Optional: \{\} <br /> |
| `pciDevice` _string_ | pci device information of this DPU |  | Optional: \{\} <br /> |
| `requiredReset` _boolean_ | whether require reset of DPU |  | Optional: \{\} <br /> |
| `firmware` _[Firmware](#firmware)_ | the firmware information of DPU |  | Optional: \{\} <br /> |
| `addresses` _[NodeAddress](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#nodeaddress-v1-core) array_ | The DPU node's IP addresses |  | Optional: \{\} <br /> |
| `dpuInstallInterface` _string_ | the name of the interface which will be used to install the bfb image,<br />and communicate with DPU, can be one of hostAgent,redfish |  | Enum: [gNOI hostAgent redfish] <br />Optional: \{\} <br /> |
| `postProvisioningNodeEffect` _boolean_ | Indicates that node effect was triggered by post-provisioning label changes |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |
| `dpuType` _[DPUType](#dputype)_ | The type of the DPU | Unknown | Enum: [Unknown BlueField2 BlueField3 BlueField4] <br />Optional: \{\} <br /> |
| `agentLastStartupTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | AgentLastStartupTime is the time when the DPU agent was last started. This is copied from agentStatus.lastStartupTime. |  | Optional: \{\} <br /> |
| `agentStatus` _[AgentStatus](#agentstatus)_ | AgentStatus contains the information reported from inside the DPU |  | Optional: \{\} <br /> |
| `rebootStatus` _[RebootStatus](#rebootstatus)_ | RebootStatus contains host reboot progress.<br />DPU controller derives user-facing DPUCondRebooted from this status. |  | Optional: \{\} <br /> |
| `dpuMode` _[DpuModeType](#dpumodetype)_ | The mode of the DPU | dpu | Enum: [dpu nic] <br />Optional: \{\} <br /> |
| `deploymentMode` _[DeploymentMode](#deploymentmode)_ | DeploymentMode is copied from DPFOperatorConfig.spec.deploymentMode by the controller.<br />This field is read-only for users. |  | Enum: [zero-trust host-trusted] <br />Optional: \{\} <br /> |
| `hostless` _boolean_ | Hostless indicates that the DPU is attached to a system-managed synthetic<br />DPUNode rather than a physical host. |  | Optional: \{\} <br /> |
| `secureBoot` _[SecureBootStatus](#securebootstatus)_ | SecureBoot indicates the current UEFI Secure Boot state. |  | Optional: \{\} <br /> |
| `identityMode` _[IdentityMode](#identitymode)_ | IdentityMode records which authentication mechanism the DPU Agent uses to reach the<br />management-cluster kube-apiserver. Stamped exactly once by the DPU controller during phase<br />Initializing (nil guard); immutable thereafter. Pre-SPIFFE legacy DPUs have IdentityMode<br />unset (nil) which consumers MUST treat semantically as bootstrap-token. |  | Enum: [spiffe bootstrap-token] <br />Optional: \{\} <br /> |
| `redfishTaskId` _string_ | The task ID of the last task performed on the DPU BMC |  | Optional: \{\} <br /> |


#### DPUTemplate



DPUTemplate is a template for DPU



_Appears in:_
- [DPUSetSpec](#dpusetspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `annotations` _object (keys:string, values:string)_ | Annotations specifies annotations which are added to the DPU. |  |  |
| `spec` _[DPUTemplateSpec](#dputemplatespec)_ | Spec specifies the DPU specification. |  |  |


#### DPUTemplateSpec



DPUTemplateSpec defines the desired provisioning template for DPUs.



_Appears in:_
- [DPUTemplate](#dputemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bfb` _[BFBReference](#bfbreference)_ | Specifies a BFB CR |  | Optional: \{\} <br /> |
| `blueFieldSoftware` _[BlueFieldSoftwareReference](#bluefieldsoftwarereference)_ | Specifies a BlueFieldSoftware CR |  | Optional: \{\} <br /> |
| `nodeEffect` _[NodeEffect](#nodeeffect)_ | Specifies how changes to the DPU should affect the Node |  | Required: \{\} <br /> |
| `cluster` _[ClusterSpec](#clusterspec)_ | Specifies details on the K8S cluster to join |  | Optional: \{\} <br /> |
| `dpuFlavor` _string_ | DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU.<br />Mutually exclusive with DPUFlavorTemplate. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `dpuFlavorTemplate` _string_ | DPUFlavorTemplate is the name of a DPUFlavorTemplate that is rendered per-DPU<br />(against DPUDevice.spec.values) into a generated DPUFlavor. Mutually exclusive<br />with DPUFlavor. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `astraEnabled` _boolean_ | AstraEnabled indicates whether E/W NIC configuration (Astra) is enabled |  | Optional: \{\} <br /> |
| `secureBoot` _boolean_ | SecureBoot specifies whether UEFI Secure Boot should be enabled. |  | Optional: \{\} <br /> |


#### DPUType

_Underlying type:_ _string_





_Appears in:_
- [DPUDeviceStatus](#dpudevicestatus)
- [DPUStatus](#dpustatus)

| Field | Description |
| --- | --- |
| `Unknown` |  |
| `BlueField2` |  |
| `BlueField3` |  |
| `BlueField4` |  |


#### DeploymentMode

_Underlying type:_ _string_

DeploymentMode describes the cluster deployment model for provisioning (zero-trust vs host-trusted).
This type is intentionally duplicated from operator/v1alpha1.DeploymentMode: the provisioning API
must not import the operator API group. Keep values and semantics aligned with
DPFOperatorConfig.spec.deploymentMode.

_Validation:_
- Enum: [zero-trust host-trusted]

_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description |
| --- | --- |
| `zero-trust` |  |
| `host-trusted` |  |


#### DownloadedComponents



DownloadedComponents tracks which components have been downloaded



_Appears in:_
- [BlueFieldSoftwareStatus](#bluefieldsoftwarestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pldmFwBundle` _string_ |  |  |  |
| `osIso` _string_ |  |  |  |
| `astraNicFw` _string_ |  |  |  |


#### DpuModeType

_Underlying type:_ _string_

DpuModeType defines the mode of the DPU

_Validation:_
- Enum: [dpu zero-trust nic]

_Appears in:_
- [DPUDeviceStatus](#dpudevicestatus)
- [DPUFlavorSpec](#dpuflavorspec)
- [DPUStatus](#dpustatus)

| Field | Description |
| --- | --- |
| `dpu` |  |
| `nic` |  |
| `zero-trust` | ZeroTrustMode is deprecated and kept for backward compatibility with DPUFlavor.spec.dpuMode.<br />Deprecated: DPUFlavor.spec.dpuMode is deprecated; use DPFOperatorConfig.spec.deploymentMode.<br /> |


#### External







_Appears in:_
- [NodeRebootMethod](#noderebootmethod)



#### Firmware







_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bmc` _string_ | BMC is the used BMC firmware version |  |  |
| `nic` _string_ | NIC is the used NIC firmware version |  |  |
| `uefi` _string_ | UEFI is the used UEFI firmware version |  |  |


#### GNOI







_Appears in:_
- [NodeRebootMethod](#noderebootmethod)



#### HostAgent







_Appears in:_
- [NodeRebootMethod](#noderebootmethod)



#### IPRange



IPRange represents a range of IP addresses to scan



_Appears in:_
- [IPRangeValidationSpec](#iprangevalidationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `startIP` _string_ |  |  | Pattern: `^((25[0-5]\|2[0-4][0-9]\|[01]?[0-9][0-9]?)\.)\{3\}(25[0-5]\|2[0-4][0-9]\|[01]?[0-9][0-9]?)$` <br />Required: \{\} <br /> |
| `endIP` _string_ |  |  | Pattern: `^((25[0-5]\|2[0-4][0-9]\|[01]?[0-9][0-9]?)\.)\{3\}(25[0-5]\|2[0-4][0-9]\|[01]?[0-9][0-9]?)$` <br />Required: \{\} <br /> |
| `port` _integer_ | Port defines the port to on which BMC is listening | 443 | Maximum: 65535 <br />Minimum: 1 <br />Optional: \{\} <br /> |


#### IPRangeValidationSpec



IPRangeValidationSpec defines the desired state of IPRangeValidation
IPRange defines the IP range to validate



_Appears in:_
- [DPUDiscoverySpec](#dpudiscoveryspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipRange` _[IPRange](#iprange)_ |  |  |  |


#### IdentityMode

_Underlying type:_ _string_

IdentityMode records which authentication mechanism the DPU Agent uses to reach the
management-cluster kube-apiserver. It is stamp-once (see DPUStatus.IdentityMode).



_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description |
| --- | --- |
| `spiffe` | IdentityModeSpiffe indicates the DPU Agent authenticates with a SPIFFE-issued JWT-SVID.<br /> |
| `bootstrap-token` | IdentityModeBootstrapToken indicates the DPU Agent authenticates with a kubeadm bootstrap token.<br />An unset (nil) IdentityMode is treated semantically as bootstrap-token; no sentinel is declared<br />for the unset case deliberately, to force explicit handling by consumers.<br /> |


#### K8sCluster







_Appears in:_
- [DPUSpec](#dpuspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the DPUs Kubernetes cluster |  | Optional: \{\} <br /> |
| `namespace` _string_ | Namespace is the tenants namespace name where the Kubernetes cluster will be deployed |  | Optional: \{\} <br /> |
| `nodeLabels` _object (keys:string, values:string)_ | NodeLabels specifies the labels to be added to the node. |  | Optional: \{\} <br /> |
| `nodeAnnotations` _object (keys:string, values:string)_ | NodeAnnotations specifies the annotations to be added to the node. |  | Optional: \{\} <br /> |
| `selector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Selector defines the selector of the DPUClusters the produced DPUs should join |  | Optional: \{\} <br /> |


#### KeepalivedSpec







_Appears in:_
- [ClusterEndpointSpec](#clusterendpointspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vip` _string_ | VIP is the virtual IP owned by the keepalived instances |  |  |
| `virtualRouterID` _integer_ | VirtualRouterID is the virtual_router_id in keepalived.conf |  | Maximum: 255 <br />Minimum: 1 <br /> |
| `interface` _string_ | Interface specifies on which interface the VIP should be assigned |  | MinLength: 1 <br /> |
| `nodeSelector` _object (keys:string, values:string)_ | NodeSelector is used to specify a subnet of control plane nodes to deploy keepalived instances.<br />Note: keepalived instances are always deployed on control plane nodes |  | Optional: \{\} <br /> |


#### NVConfig







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)
- [NetworkInterfaceConfig](#networkinterfaceconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `device` _string_ | Device is the device to which the configuration applies. If not specified, the configuration applies to all.<br />Supported values: "*" (wildcard for all devices), "p0"/"P0" (port 0), "p1"/"P1" (port 1). Case-insensitive. |  | Enum: [* p0 p1 P0 P1] <br />Optional: \{\} <br /> |
| `parameters` _string array_ | Parameters are the parameters to be set for the device. |  | MaxItems: 32 <br />MinItems: 1 <br />items:MaxLength: 200 <br />items:Pattern: `^[^=\s]+=[^\s]*$` <br />Optional: \{\} <br /> |


#### NetworkInterfaceConfig



NetworkInterfaceConfig defines the configuration for a network interface



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `mtu` _integer_ | MTU is the MTU value to be set on the network interface. |  | Maximum: 9216 <br />Minimum: 1280 <br />Optional: \{\} <br /> |
| `dhcp` _boolean_ | DHCP is the DHCP configuration for the network interface. |  | Optional: \{\} <br /> |
| `portNumber` _integer_ | PortNumber identifies which port this configuration applies to. |  | Maximum: 1 <br />Minimum: 0 <br />Required: \{\} <br /> |
| `nvconfig` _[NVConfig](#nvconfig)_ | NVConfig contains port-specific configuration for this network interface.<br />This configuration is applied in addition to the global NVConfig settings in DPUFlavorSpec.<br />Both global and per-interface NVConfig settings can coexist without collision. |  | Optional: \{\} <br /> |


#### NicConfiguration



NicConfiguration is a set of configurations for the NICs



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `numVfs` _integer_ | Number of VFs to be configured |  | Required: \{\} <br /> |
| `linkType` _[LinkTypeEnum](#linktypeenum)_ | LinkType to be configured, Ethernet\|Infiniband |  | Enum: [Ethernet Infiniband] <br />Required: \{\} <br /> |
| `spectrumXOptimized` _[SpectrumXOptimizedSpec](#spectrumxoptimizedspec)_ | Spectrum-X optimization settings. Works only with linkType==Ethernet && numVfs==0. Other optimizations must be skipped or disabled. RawNvConfig must be empty. |  |  |
| `rawNvConfig` _NvConfigParam array_ | List of arbitrary nv config parameters |  |  |


#### NodeEffect



NodeEffect is the effect the DPU has on Nodes during provisioning.
Only one of Taint, NoEffect, CustomLabel, Drain, CustomAction, Hold can be set.



_Appears in:_
- [DPUNodeMaintenanceSpec](#dpunodemaintenancespec)
- [DPUSpec](#dpuspec)
- [DPUTemplateSpec](#dputemplatespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `taint` _[Taint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#taint-v1-core)_ | Add specify taint on the DPU node |  | Optional: \{\} <br /> |
| `noEffect` _boolean_ | Do not do any action on the DPU node |  | Optional: \{\} <br /> |
| `customLabel` _object (keys:string, values:string)_ | Add specify labels on the DPU node |  | Optional: \{\} <br /> |
| `drain` _boolean_ | Drain the K8s host node by NodeMaintenance operator |  | Optional: \{\} <br /> |
| `customAction` _string_ | Name of a config map which contains a pod yaml definition to run which will apply the nodeEffect.<br />The pod is expected to exit when node effect is done, if pod terminates with error then DPU would move to an error phase.<br />The DPUNode's name will be exported as an environment variable, named as DPUNODE_NAME, to each container and init container in the pod.<br />The labels and annotations of DPUNode will be exported in `/etc/dpu/dpf-pod-info/labels` and `/etc/dpu/dpf-pod-info/annotations` accordingly; the volume name `dpf-pod-info` is used to mount the labels and annotations.<br />If any name confliction for env or volume, the controller will not export the name or labels/annotations of DPUNode accordingly. |  | Optional: \{\} <br /> |
| `hold` _boolean_ | Places annotation `wait-for-external-nodeeffect` and waits for it to be removed<br />this is the default behavior in a non K8S environment |  | Optional: \{\} <br /> |
| `force` _boolean_ | Force is the flag to indicate if the node effect should be applied immediately.<br />If true, dpfOperatorConfig.multiDPUOperationsSyncWaitTime and dpfOperatorConfig.maxUnavailableDPUNodes will be ignored when applying node effect for DPUNodeMaintenance CR | false | Optional: \{\} <br /> |
| `applyOnLabelChange` _boolean_ | Apply node effect when labels change on the DPU object<br />When set to true, label changes in Ready state will trigger node effect logic | false | Optional: \{\} <br /> |
| `nodeMaintenanceAdditionalRequestors` _string array_ | Additional requestors to be added to the NvidiaNodeMaintenance CR when Drain is selected |  | Optional: \{\} <br /> |


#### NodeRebootMethod



NodeRebootMethod defines the desired reboot method



_Appears in:_
- [DPUNodeSpec](#dpunodespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `gNOI` _[GNOI](#gnoi)_ | Use the DPU's DMS interface to reboot the host.<br />Deprecated: Use HostAgent instead. |  | Optional: \{\} <br /> |
| `hostAgent` _[HostAgent](#hostagent)_ | Use the HostAgent to reboot the host. |  | Optional: \{\} <br /> |
| `external` _[External](#external)_ | Reboot the host via an external means, not controlled by the DPU controller. |  | Optional: \{\} <br /> |
| `script` _[Script](#script)_ | Reboot the host by executing a custom script. This field defined which ConfigMap store the custom script.<br />The ConfigMap should include a pod template of Job object under the `pod-template` key.<br />That pod template will be put in a Job object to be executed. |  | Optional: \{\} <br /> |
| `none` _[None](#none)_ | Do not reboot a host through the DPUNode. Used for hostless devices where<br />DPF manages the DPU reboot directly through Redfish. |  | Optional: \{\} <br /> |


#### None







_Appears in:_
- [NodeRebootMethod](#noderebootmethod)



#### PackageSpec



PackageSpec defines a package to reconcile on the node.



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the package name. |  | MaxLength: 253 <br />MinLength: 1 <br /> |
| `version` _[PackageVersionSpec](#packageversionspec)_ | Version constrains the package version.<br />If empty, any installed version satisfies the spec. |  | Optional: \{\} <br /> |
| `repoFileRef` _string_ | RepoFileRef constrains package resolution to a specific repository file available on the node.<br />If empty, any configured repository may satisfy the package.<br />If specified, only the referenced repository file may provide candidates.<br />If that repository file does not provide the package or requested version, the dpu-agent flow does not continue. |  | Optional: \{\} <br /> |


#### PackageVersionMatchPolicy

_Underlying type:_ _string_

PackageVersionMatchPolicy defines how a package version constraint is evaluated.



_Appears in:_
- [PackageVersionSpec](#packageversionspec)

| Field | Description |
| --- | --- |
| `Exact` | PackageVersionMatchExact requires the installed package version to equal Value.<br /> |
| `AtLeast` | PackageVersionMatchAtLeast requires the installed package version to be greater than or equal to Value.<br /> |


#### PackageVersionSpec



PackageVersionSpec defines a package version constraint.



_Appears in:_
- [PackageSpec](#packagespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `value` _string_ | Value is the package version to compare against. |  | MinLength: 1 <br /> |
| `matchPolicy` _[PackageVersionMatchPolicy](#packageversionmatchpolicy)_ | MatchPolicy controls how Value is matched.<br />If omitted, AtLeast is used. | AtLeast | Enum: [Exact AtLeast] <br />Optional: \{\} <br /> |


#### PendingNVConfigDevice







_Appears in:_
- [PendingNVConfigState](#pendingnvconfigstate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `device` _string_ |  |  |  |
| `entries` _[PendingNVConfigEntry](#pendingnvconfigentry) array_ |  |  |  |


#### PendingNVConfigEntry







_Appears in:_
- [PendingNVConfigDevice](#pendingnvconfigdevice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  |  |
| `default` _string_ |  |  |  |
| `current` _string_ |  |  |  |
| `next_boot` _string_ | NextBoot uses the "next_boot" so this type can be reused for parsing mlxfwrest output |  |  |


#### PendingNVConfigState







_Appears in:_
- [AgentStatus](#agentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bootID` _string_ |  |  |  |
| `devices` _[PendingNVConfigDevice](#pendingnvconfigdevice) array_ |  |  |  |


#### RebootMethodType

_Underlying type:_ _string_

RebootMethodType is the type of reset/reboot required after NVConfig or firmware changes.
Set by the DPU agent. Most values align with NVIDIA BlueField Reset and Reboot Procedures (mlxfwreset levels).

_Validation:_
- Enum: [Unknown NoAction PowerCycle SystemReboot SystemLevelReset FirmwareReset DPUWarmReboot HostlessDPUReboot]

_Appears in:_
- [AgentStatus](#agentstatus)
- [DPUNodeStatus](#dpunodestatus)
- [RebootStatus](#rebootstatus)

| Field | Description |
| --- | --- |
| `Unknown` | RebootMethodUnknown is the initial value set by the DPU agent on startup before<br />HandleReboot determines the actual method. It prevents the controller from acting<br />on a stale RebootMethod left over from a previous agent session.<br /> |
| `NoAction` | RebootMethodNoAction indicates no reset or reboot is required.<br /> |
| `PowerCycle` | RebootMethodPowerCycle indicates a full server power cycle (cold boot) is required.<br /> |
| `SystemReboot` | RebootMethodSystemReboot firmware update without full server power cycle.<br /> |
| `SystemLevelReset` | RebootMethodSystemLevelReset firmware configuration changes to take effect.<br /> |
| `FirmwareReset` | RebootMethodFirmwareReset driver restart and PCI reset.<br /> |
| `DPUWarmReboot` | RebootMethodDPUWarmReboot indicates the DPU OS is rebooting itself to apply<br />configuration changes (e.g. grub kernel parameters) that do not originate<br />from firmware or NVConfig. The provisioning controller should stay in the<br />current phase and wait for the agent to come back.<br /> |
| `HostlessDPUReboot` | RebootMethodHostlessDPUReboot indicates a hostless DPU needs a DPU ARM<br />reboot performed by the provisioning controller through Redfish.<br /> |


#### RebootStatus



RebootStatus stores the host reboot execution status.



_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[RebootStatusPhase](#rebootstatusphase)_ | Phase is the current host reboot progress. |  | Enum: [Pending Succeeded Failed Unknown] <br /> |
| `method` _[RebootMethodType](#rebootmethodtype)_ | Method is the recommended reboot method. |  | Enum: [Unknown NoAction PowerCycle SystemReboot SystemLevelReset FirmwareReset DPUWarmReboot HostlessDPUReboot] <br />Optional: \{\} <br /> |
| `reason` _string_ | Reason indicates machine-readable reason for current phase. |  | Optional: \{\} <br /> |
| `message` _string_ | Message provides human-readable details for current phase. |  | Optional: \{\} <br /> |
| `lastTransitionTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastTransitionTime is the last update time for reboot status. |  | Optional: \{\} <br /> |


#### RebootStatusPhase

_Underlying type:_ _string_

RebootStatusPhase is the host reboot progress phase.

_Validation:_
- Enum: [Pending Succeeded Failed Unknown]

_Appears in:_
- [RebootStatus](#rebootstatus)

| Field | Description |
| --- | --- |
| `Pending` | RebootStatusPending means reboot is requested but execution has not started yet<br />(for example, waiting for a manual external reboot trigger).<br /> |
| `Succeeded` | RebootStatusSucceeded means reboot completed successfully.<br /> |
| `Failed` | RebootStatusFailed means reboot execution failed.<br /> |
| `Unknown` | RebootStatusUnknown means reboot execution state cannot be determined.<br /> |


#### RollingUpdateDPU



RollingUpdateDPU is the rolling update strategy for a DPUSet.



_Appears in:_
- [DPUSetStrategy](#dpusetstrategy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `maxUnavailable` _[IntOrString](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#intorstring-intstr-util)_ | MaxUnavailable is the maximum number of DPUs that can be unavailable during the update.<br />Deprecated: This field is deprecated and will be removed with v26.7.0. |  | Optional: \{\} <br /> |


#### Script







_Appears in:_
- [NodeRebootMethod](#noderebootmethod)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  | MinLength: 1 <br />Required: \{\} <br /> |


#### SecureBootStatus



SecureBootStatus represents the UEFI Secure Boot configuration status on the DPU.



_Appears in:_
- [DPUDeviceStatus](#dpudevicestatus)
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ | Enabled indicates whether UEFI Secure Boot is currently enabled on the DPU. |  | Optional: \{\} <br /> |


#### SpiffeStatus



SpiffeStatus is the DPU Agent's SPIFFE heartbeat sub-status.



_Appears in:_
- [AgentStatus](#agentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `lastProbeTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastProbeTime is the wall-clock timestamp the DPU Agent recorded on its most recent successful<br />status report. It is informational and subject to DPU clock skew, so it is not a precise<br />liveness signal on its own. |  | Optional: \{\} <br /> |
| `lastProbeMessage` _string_ | LastProbeMessage is a structured one-line diagnostic for the most recent self-probe. Unset in<br />the steady-state happy path. Bounded to 256 chars (truncated agent-side). |  | MaxLength: 256 <br />Optional: \{\} <br /> |


#### StrategyType

_Underlying type:_ _string_

StrategyType describes strategy to use to reprovision existing DPUs.

_Validation:_
- Enum: [OnDelete RollingUpdate]

_Appears in:_
- [DPUSetStrategy](#dpusetstrategy)

| Field | Description |
| --- | --- |
| `OnDelete` | New DPU CR will only be created when you manually delete old DPU CR.<br /> |
| `RollingUpdate` | Gradually scale down the old DPUs and scale up the new one.<br /> |


#### SystemdServiceOperation

_Underlying type:_ _string_

SystemdServiceOperation defines the operation to perform on a systemd service.

_Validation:_
- Enum: [Start Enable EnableAndStart]

_Appears in:_
- [SystemdServiceSpec](#systemdservicespec)

| Field | Description |
| --- | --- |
| `Start` | SystemdServiceStart starts the service without enabling it at boot.<br /> |
| `Enable` | SystemdServiceEnable enables the service at boot without starting it immediately.<br /> |
| `EnableAndStart` | SystemdServiceEnableAndStart enables the service at boot and starts it immediately (equivalent to systemctl enable --now).<br /> |


#### SystemdServiceSpec



SystemdServiceSpec defines a systemd service to manage on the node.



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the systemd service name. |  | MaxLength: 253 <br />MinLength: 1 <br /> |
| `operation` _[SystemdServiceOperation](#systemdserviceoperation)_ | Operation is the systemd operation to perform on the service. |  | Enum: [Start Enable EnableAndStart] <br /> |


#### UpgradePolicy



UpgradePolicy is the policy for the upgrade of the DPUSet.



_Appears in:_
- [NodeEffect](#nodeeffect)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `applyOnLabelChange` _boolean_ | Apply node effect when labels change on the DPU object<br />When set to true, label changes in Ready state will trigger node effect logic | false | Optional: \{\} <br /> |
| `nodeMaintenanceAdditionalRequestors` _string array_ | Additional requestors to be added to the NvidiaNodeMaintenance CR when Drain is selected |  | Optional: \{\} <br /> |



## storage.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the storage v1alpha1 API group

### Resource Types
- [DPUStoragePolicy](#dpustoragepolicy)
- [DPUStoragePolicyList](#dpustoragepolicylist)
- [DPUStorageVendor](#dpustoragevendor)
- [DPUStorageVendorList](#dpustoragevendorlist)
- [DPUVolume](#dpuvolume)
- [DPUVolumeAttachment](#dpuvolumeattachment)
- [DPUVolumeAttachmentList](#dpuvolumeattachmentlist)
- [DPUVolumeList](#dpuvolumelist)
- [SVVolumeAttachment](#svvolumeattachment)
- [SVVolumeAttachmentList](#svvolumeattachmentlist)
- [Volume](#volume)
- [VolumeAttachment](#volumeattachment)
- [VolumeAttachmentList](#volumeattachmentlist)
- [VolumeList](#volumelist)



#### AttachmentStatusDPU



AttachmentStatusDPU describe the information of DPU volume



_Appears in:_
- [DPUVolumeAttachmentStatus](#dpuvolumeattachmentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pciAddress` _string_ | PCI device address in the following format: (bus:device.function) |  | Optional: \{\} <br /> |
| `deviceName` _string_ | The name of the device that was created by the storage vendor plugin |  | Optional: \{\} <br /> |
| `nvmeAttrs` _[NVMEAttrs](#nvmeattrs)_ | The attributes of the emulated NVME function |  | Optional: \{\} <br /> |
| `virtioFSAttrs` _[VirtioFSAttrs](#virtiofsattrs)_ | The attributes of the emulated VirtioFS function |  | Optional: \{\} <br /> |


#### BdevAttrs



BdevAttrs represents the attributes of the underlying block device



_Appears in:_
- [VolumeAttachmentStatusDPU](#volumeattachmentstatusdpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nvmeNsID` _integer_ | The namespace ID within the NVME controller |  |  |
| `nvmeUUID` _string_ | The nvme namespace UUID |  |  |


#### CSIReference



CSIReference reference to CSI object



_Appears in:_
- [VolumeSpecDPU](#volumespecdpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `csiDriverName` _string_ |  |  |  |
| `storageClassName` _string_ |  |  |  |
| `pvcRef` _[ObjectRef](#objectref)_ |  |  |  |


#### CapacityRange



CapacityRange represents the capacity of the required storage space in bytes



_Appears in:_
- [VolumeRequest](#volumerequest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `request` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#quantity-resource-api)_ |  |  |  |
| `limit` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#quantity-resource-api)_ |  |  |  |


#### DPUStoragePolicy



DPUStoragePolicy represents a DPUStoragePolicy CR



_Appears in:_
- [DPUStoragePolicyList](#dpustoragepolicylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUStoragePolicy` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUStoragePolicySpec](#dpustoragepolicyspec)_ |  |  |  |
| `status` _[DPUStoragePolicyStatus](#dpustoragepolicystatus)_ |  |  |  |


#### DPUStoragePolicyList



DPUStoragePolicyList contains a list of DPUStoragePolicy objects





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUStoragePolicyList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUStoragePolicy](#dpustoragepolicy) array_ |  |  |  |


#### DPUStoragePolicySpec



DPUStoragePolicySpec defines the desired state of DPUStoragePolicy



_Appears in:_
- [DPUStoragePolicy](#dpustoragepolicy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuStorageVendors` _string array_ | List of storage vendors |  | MinItems: 1 <br />Required: \{\} <br /> |
| `parameters` _object (keys:string, values:string)_ | Parameters supported by the policy | \{  \} | Optional: \{\} <br /> |
| `selectionAlgorithm` _[SelectionAlgorithm](#selectionalgorithm)_ | Selection algorithm used to select DPUStorageVendor | NumberVolumes | Enum: [Random NumberVolumes] <br />Optional: \{\} <br /> |


#### DPUStoragePolicyStatus



DPUStoragePolicyStatus defines the observed state of DPUStoragePolicy



_Appears in:_
- [DPUStoragePolicy](#dpustoragepolicy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Current service state conditions |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUStorageVendor



DPUStorageVendor represents a StorageVendor CR on the DPU cluster.



_Appears in:_
- [DPUStorageVendorList](#dpustoragevendorlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUStorageVendor` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUStorageVendorSpec](#dpustoragevendorspec)_ |  |  |  |
| `status` _[DPUStorageVendorStatus](#dpustoragevendorstatus)_ |  |  |  |


#### DPUStorageVendorList



DPUStorageVendorList contains a list of DPUStorageVendor





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUStorageVendorList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUStorageVendor](#dpustoragevendor) array_ |  |  |  |


#### DPUStorageVendorSpec



DPUStorageVendorSpec defines the desired state of DPUStorageVendor



_Appears in:_
- [DPUStorageVendor](#dpustoragevendor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageClassName` _string_ | Storage vendor class name, deployed on the DPU K8S cluster. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `pluginName` _string_ | Storage vendor DPU plugin name |  | MinLength: 1 <br />Required: \{\} <br /> |


#### DPUStorageVendorStatus



DPUStorageVendorStatus defines the observed state of DPUStorageVendor



_Appears in:_
- [DPUStorageVendor](#dpustoragevendor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuClusters` _[ObjectReference](#objectreference) array_ | DPUClusters is the list of clusters on which the DPUStorageVendor is deployed. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUVolume



DPUVolume represents a DPUVolume CR.



_Appears in:_
- [DPUVolumeList](#dpuvolumelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVolume` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUVolumeSpec](#dpuvolumespec)_ |  |  |  |
| `status` _[DPUVolumeStatus](#dpuvolumestatus)_ |  |  |  |


#### DPUVolumeAttachment



DPUVolumeAttachment represents a Volume CR on the DPU cluster.



_Appears in:_
- [DPUVolumeAttachmentList](#dpuvolumeattachmentlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVolumeAttachment` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUVolumeAttachmentSpec](#dpuvolumeattachmentspec)_ |  |  |  |
| `status` _[DPUVolumeAttachmentStatus](#dpuvolumeattachmentstatus)_ |  |  |  |


#### DPUVolumeAttachmentList



DPUVolumeAttachmentList contains a list of DPUVolumeAttachment





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVolumeAttachmentList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUVolumeAttachment](#dpuvolumeattachment) array_ |  |  |  |


#### DPUVolumeAttachmentSpec



DPUVolumeAttachmentSpec defines the desired state of DPUVolumeAttachment



_Appears in:_
- [DPUVolumeAttachment](#dpuvolumeattachment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuNodeName` _string_ | DPUNodeName is the name of DPUNode object that represents the node to which the volume should<br />be attached |  | MinLength: 1 <br />Required: \{\} <br /> |
| `dpuVolumeName` _string_ | DPUVolumeName is the name of DPUVolume object that represents the volume to be attached |  | MinLength: 1 <br />Required: \{\} <br /> |
| `functionType` _[FunctionType](#functiontype)_ | FunctionType is the type of the emulated function that should be used to attach the volume |  | Enum: [pf vf] <br />Required: \{\} <br /> |
| `hotplugFunction` _boolean_ | HotplugFunction is a boolean flag that indicates if the emulated function should be hotplugged |  | Required: \{\} <br /> |


#### DPUVolumeAttachmentStatus



DPUVolumeAttachmentStatus defines the observed state of DPUVolumeAttachment



_Appears in:_
- [DPUVolumeAttachment](#dpuvolumeattachment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `controllerAttached` _boolean_ | Indicates the volume is successfully attached to by the Vendor CSI driver |  |  |
| `dpuAttached` _boolean_ | Indicates the volume is successfully attached to the node by DPU |  | Optional: \{\} <br /> |
| `attachmentMetadata` _object (keys:string, values:string)_ | AttachmentMetadata contains the metadata of the volume attachment returned by the Vendor CSI driver |  | Optional: \{\} <br /> |
| `dpu` _[AttachmentStatusDPU](#attachmentstatusdpu)_ | Details about the DPU attachment |  | Optional: \{\} <br /> |
| `message` _string_ | The last error encountered during the attach operation, if any |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUVolumeList



DPUVolumeList contains a list of DPUVolume





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVolumeList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUVolume](#dpuvolume) array_ |  |  |  |


#### DPUVolumePhase

_Underlying type:_ _string_





_Appears in:_
- [DPUVolumeStatus](#dpuvolumestatus)

| Field | Description |
| --- | --- |
| `Pending` | used for DPUVolume that are not yet bound to a volume in the DPU cluster<br /> |
| `Bound` | used for DPUVolume that are bound to a volume in the DPU cluster<br /> |


#### DPUVolumeSpec



DPUVolumeSpec defines the desired state of DPUVolume



_Appears in:_
- [DPUVolume](#dpuvolume)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuStoragePolicyName` _string_ | Name of the DPUStoragePolicyName object that will be used to create the volume. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `parameters` _object (keys:string, values:string)_ | Additional parameters for the volume, these parameters are merged with the values from the DPUStoragePolicy object. | \{  \} | Optional: \{\} <br /> |
| `accessModes` _[PersistentVolumeAccessMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumeaccessmode-v1-core) array_ | Access modes define how the volume can be mounted. These modes are directly passed to the<br />PersistentVolumeClaim created for the Vendor CSI Plugin selected by the DPUStoragePolicy. |  | MaxItems: 3 <br />MinItems: 1 <br />Required: \{\} <br /> |
| `resources` _[VolumeResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#volumeresourcerequirements-v1-core)_ | Resources represents the storage resources requested for the volume. These resource requirements<br />are directly passed to the PersistentVolumeClaim created for the Vendor CSI Plugin selected<br />by the DPUStoragePolicy. Since volume resizing is not supported, modifications to the resource request are prohibited. |  | Required: \{\} <br /> |
| `volumeMode` _[PersistentVolumeMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumemode-v1-core)_ | Volume mode defines how the volume should be mounted and used. This value is directly passed to the<br />PersistentVolumeClaim created for the Vendor CSI Plugin selected by the DPUStoragePolicy. | Filesystem | Enum: [Filesystem Block] <br />Optional: \{\} <br /> |


#### DPUVolumeState



DPUVolumeState defines the state of the volume.



_Appears in:_
- [DPUVolumeStatus](#dpuvolumestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuCluster` _[ObjectReference](#objectreference)_ | DPUCluster contains the reference to the DPUCluster object that was selected for volume creation. |  | Optional: \{\} <br /> |
| `parameters` _object (keys:string, values:string)_ | Parameters contains the final set of parameters for volume creation, computed by merging<br />the parameters from the DPUStoragePolicy object with user-provided parameters. |  | Optional: \{\} <br /> |
| `selectedDPUStorageVendorName` _string_ | SelectedDPUStorageVendorName contains the name of the DPUStorageVendor object that was selected for volume creation. |  | Optional: \{\} <br /> |
| `storageVendorPluginName` _string_ | StorageVendorPluginName contains the name of the storage vendor plugin deployed on the DPU cluster that was selected for volume creation. |  | Optional: \{\} <br /> |
| `storageClassName` _string_ | StorageClassName contains the name of the storage class in the DPU cluster that was selected for volume creation. |  | Optional: \{\} <br /> |
| `csiDriverName` _string_ | CSIDriverName contains the name of the CSI driver in the DPU cluster that was selected for volume creation. |  | Optional: \{\} <br /> |
| `persistentVolumeClaimRef` _[ObjectReference](#objectreference)_ | PersistentVolumeClaimRef contains the reference to the PersistentVolumeClaim object in the DPU cluster that was created for the volume. |  | Optional: \{\} <br /> |
| `volumeInfo` _[VolumeInfo](#volumeinfo)_ | VolumeInfo contains a subset of fields from the PersistentVolume object created in the DPU cluster |  | Optional: \{\} <br /> |


#### DPUVolumeStatus



DPUVolumeStatus defines the observed state of DPUVolume



_Appears in:_
- [DPUVolume](#dpuvolume)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[DPUVolumePhase](#dpuvolumephase)_ | Phase of the volume |  | Enum: [Pending Bound] <br />Optional: \{\} <br /> |
| `state` _[DPUVolumeState](#dpuvolumestate)_ | State of the volume. This field is managed by the controller. User usually do not need to set fields from this struct. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### FSdevAttrs



FSdevAttrs represents the attributes of the underlying filesystem device



_Appears in:_
- [VolumeAttachmentStatusDPU](#volumeattachmentstatusdpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `filesystemTag` _string_ | Filesystem tag identified by SNAP on the host (used for the mount). Relevant for volume of type filesystem |  |  |


#### FunctionType

_Underlying type:_ _string_





_Appears in:_
- [DPUVolumeAttachmentSpec](#dpuvolumeattachmentspec)
- [FunctionTypeConfig](#functiontypeconfig)
- [VolumeAttachmentSpec](#volumeattachmentspec)

| Field | Description |
| --- | --- |
| `pf` | FunctionTypePF is the PF function type<br /> |
| `vf` | FunctionTypeVF is the VF function type<br /> |


#### FunctionTypeConfig



FunctionTypeConfig is the configuration for the emulated function that should be used to attach the volume



_Appears in:_
- [DPUVolumeAttachmentSpec](#dpuvolumeattachmentspec)
- [VolumeAttachmentSpec](#volumeattachmentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `functionType` _[FunctionType](#functiontype)_ | FunctionType is the type of the emulated function that should be used to attach the volume |  | Enum: [pf vf] <br />Required: \{\} <br /> |
| `hotplugFunction` _boolean_ | HotplugFunction is a boolean flag that indicates if the emulated function should be hotplugged |  | Required: \{\} <br /> |


#### NVMEAttrs



NVMEAttrs represents the attributes of the NVME emulated function



_Appears in:_
- [AttachmentStatusDPU](#attachmentstatusdpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespaceID` _integer_ | The namespace ID within the NVME controller |  | Optional: \{\} <br /> |
| `namespaceUUID` _string_ | The NVMe namespace UUID |  | Optional: \{\} <br /> |


#### ObjectRef



ObjectRef reference to the object



_Appears in:_
- [CSIReference](#csireference)
- [VolumeAttachmentSpec](#volumeattachmentspec)
- [VolumeSource](#volumesource)
- [VolumeSpec](#volumespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `kind` _string_ |  |  |  |
| `apiVersion` _string_ |  |  |  |
| `name` _string_ |  |  |  |
| `namespace` _string_ |  |  |  |


#### ObjectReference



ObjectReference represents a reference to a Kubernetes object.



_Appears in:_
- [DPUStorageVendorStatus](#dpustoragevendorstatus)
- [DPUVolumeState](#dpuvolumestate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name specifies the name of the referenced object |  | MinLength: 1 <br />Required: \{\} <br /> |
| `namespace` _string_ | Namespace specifies the namespace where the referenced object exists |  | MinLength: 1 <br />Required: \{\} <br /> |


#### SVVolumeAttachment



SVVolumeAttachment captures the intent to attach/detach the specified Volume to/from the specified node.



_Appears in:_
- [SVVolumeAttachmentList](#svvolumeattachmentlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `SVVolumeAttachment` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[VolumeAttachmentSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#volumeattachmentspec-v1-storage)_ |  |  |  |
| `status` _[VolumeAttachmentStatus](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#volumeattachmentstatus-v1-storage)_ |  |  |  |


#### SVVolumeAttachmentList



SVVolumeAttachmentList contains a list of SVVolumeAttachment





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `SVVolumeAttachmentList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[SVVolumeAttachment](#svvolumeattachment) array_ |  |  |  |


#### SelectionAlgorithm

_Underlying type:_ _string_

SelectionAlgorithm represents the storage selection algorithm type



_Appears in:_
- [DPUStoragePolicySpec](#dpustoragepolicyspec)

| Field | Description |
| --- | --- |
| `Random` | Random selection across the vendors defined in the StoragePolicy list.<br /> |
| `NumberVolumes` | Load-balancing on the number of volumes belonging to the StoragePolicy.<br />The vendor (in the DPUStoragePolicy list) with the minimal number of volumes should be selected.<br /> |


#### VirtioFSAttrs



VirtioFSAttrs represents the attributes of the VirtioFS emulated function



_Appears in:_
- [AttachmentStatusDPU](#attachmentstatusdpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `filesystemTag` _string_ | Filesystem tag identified by SNAP on the host (used for the mount). Relevant for volume of type filesystem |  | Optional: \{\} <br /> |


#### Volume



Volume represents a persistent volume on the DPU cluster.
It maps between the tenant K8S persistent volume (PV) object on the tenant cluster into the actual volume on the DPU cluster.
Volume is an internal API, it is not intended to be used by users.



_Appears in:_
- [VolumeList](#volumelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `Volume` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[VolumeSpec](#volumespec)_ |  |  |  |
| `status` _[VolumeStatus](#volumestatus)_ |  |  |  |


#### VolumeAttachment



VolumeAttachment captures the intent to attach/detach the specified NV-Volume to/from the specified node.
VolumeAttachment is an internal API, it is not intended to be used by users.



_Appears in:_
- [VolumeAttachmentList](#volumeattachmentlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `VolumeAttachment` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[VolumeAttachmentSpec](#volumeattachmentspec)_ |  |  |  |
| `status` _[VolumeAttachmentStatus](#volumeattachmentstatus)_ |  |  |  |


#### VolumeAttachmentList



VolumeAttachmentList contains a list of VolumeAttachment





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `VolumeAttachmentList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[VolumeAttachment](#volumeattachment) array_ |  |  |  |


#### VolumeAttachmentSpec



VolumeAttachmentSpec defines the desired state of VolumeAttachment



_Appears in:_
- [VolumeAttachment](#volumeattachment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeName` _string_ | The name of the node that the volume should be attached to |  |  |
| `source` _[VolumeSource](#volumesource)_ | Reference to the NV-Volume object |  |  |
| `volumeAttachmentRef` _[ObjectRef](#objectref)_ | Reference to the SV-VolumeAttachment object |  |  |
| `parameters` _object (keys:string, values:string)_ | Opaque static publish properties of the volume returned by the plugin |  |  |
| `functionType` _[FunctionType](#functiontype)_ | FunctionType is the type of the emulated function that should be used to attach the volume |  | Enum: [pf vf] <br />Required: \{\} <br /> |
| `hotplugFunction` _boolean_ | HotplugFunction is a boolean flag that indicates if the emulated function should be hotplugged |  | Required: \{\} <br /> |


#### VolumeAttachmentStatus



VolumeAttachmentStatus defines the observed state of VolumeAttachment



_Appears in:_
- [VolumeAttachment](#volumeattachment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageAttached` _boolean_ | Indicates the volume is successfully attached to the target storage system |  |  |
| `message` _string_ | The last error encountered during the attach operation, if any |  |  |
| `dpu` _[VolumeAttachmentStatusDPU](#volumeattachmentstatusdpu)_ | Details about the DPU attachment |  |  |


#### VolumeAttachmentStatusDPU



VolumeAttachmentStatusDPU describe the information of DPU volume



_Appears in:_
- [VolumeAttachmentStatus](#volumeattachmentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `attached` _boolean_ | Indicates the volume is successfully attached to the DPU node |  |  |
| `pciDeviceAddress` _string_ | PCI device address in the following format: (bus:device.function) |  |  |
| `deviceName` _string_ | The name of the device that was created by the storage vendor plugin |  |  |
| `bdevAttrs` _[BdevAttrs](#bdevattrs)_ | The attributes of the underlying block device |  | Optional: \{\} <br /> |
| `fsdevAttrs` _[FSdevAttrs](#fsdevattrs)_ | The attributes of the underlying filesystem device |  | Optional: \{\} <br /> |


#### VolumeInfo



VolumeInfo represents a subset of fields from the PersistentVolume object that was created in the DPU cluster.
This struct is used to track and expose key volume information without carrying the full PersistentVolume object.



_Appears in:_
- [DPUVolumeState](#dpuvolumestate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `volumeName` _string_ | VolumeName contains the name of the PersistentVolume object in the DPU cluster |  | Optional: \{\} <br /> |
| `capacity` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | Actual capacity of the volume in the DPU cluster |  | Optional: \{\} <br /> |
| `accessModes` _[PersistentVolumeAccessMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumeaccessmode-v1-core) array_ | Actual access modes of the volume in the DPU cluster |  | Optional: \{\} <br /> |
| `volumeMode` _[PersistentVolumeMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumemode-v1-core)_ | Actual volume mode of the volume in the DPU cluster |  | Optional: \{\} <br /> |
| `volumeAttributes` _object (keys:string, values:string)_ | VolumeAttributes from the PersistentVolume object in the DPU cluster<br />This field usually contains parameters returned by the Vendor CSI plugin on volume creation. |  | Optional: \{\} <br /> |


#### VolumeList



VolumeList contains a list of Volume





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `VolumeList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Volume](#volume) array_ |  |  |  |


#### VolumeRequest



VolumeRequest represents the volume's requirements



_Appears in:_
- [VolumeSpec](#volumespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `capacityRange` _[CapacityRange](#capacityrange)_ | The capacity of the required storage space in bytes |  | Optional: \{\} <br /> |
| `accessModes` _[PersistentVolumeAccessMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumeaccessmode-v1-core) array_ | Contains the types of access modes required |  | Optional: \{\} <br /> |
| `volumeMode` _[PersistentVolumeMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumemode-v1-core)_ | volumeMode defines what type of volume is required by the claim.<br />Value of Filesystem is implied when not included in claim spec. |  | Optional: \{\} <br /> |


#### VolumeSource



VolumeSource references to the NV-Volume object



_Appears in:_
- [VolumeAttachmentSpec](#volumeattachmentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `volumeRef` _[ObjectRef](#objectref)_ | Reference to the NV-Volume object |  |  |


#### VolumeSpec



VolumeSpec defines the desired state of Volume



_Appears in:_
- [Volume](#volume)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageParameters` _object (keys:string, values:string)_ | List of storage parameters supported by the policy, values are string only |  | Optional: \{\} <br /> |
| `request` _[VolumeRequest](#volumerequest)_ | The capacity of the required storage space in bytes |  | Required: \{\} <br /> |
| `storagePolicyRef` _[ObjectRef](#objectref)_ | Reference to the StoragePolicy object |  | Optional: \{\} <br /> |
| `storagePolicyParameters` _object (keys:string, values:string)_ | List of storage parameters supported by the policy, values are string only |  | Optional: \{\} <br /> |
| `volume` _[VolumeSpecDPU](#volumespecdpu)_ | Describe volume information in DPU cluster |  | Optional: \{\} <br /> |


#### VolumeSpecDPU



VolumeSpecDPU describe volume information in DPU cluster



_Appears in:_
- [VolumeSpec](#volumespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `id` _string_ |  |  |  |
| `capacity` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#quantity-resource-api)_ |  |  |  |
| `accessModes` _[PersistentVolumeAccessMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumeaccessmode-v1-core) array_ |  |  |  |
| `reclaimPolicy` _[PersistentVolumeReclaimPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumereclaimpolicy-v1-core)_ |  |  | Enum: [Delete Retain] <br /> |
| `storageVendorName` _string_ |  |  |  |
| `storageVendorPluginName` _string_ |  |  |  |
| `volumeAttributes` _object (keys:string, values:string)_ |  |  |  |
| `csiReference` _[CSIReference](#csireference)_ |  |  |  |


#### VolumeState

_Underlying type:_ _string_

VolumeState represents the state of volume

_Validation:_
- Enum: [InProgress Available]

_Appears in:_
- [VolumeStatus](#volumestatus)

| Field | Description |
| --- | --- |
| `InProgress` | InProgress means the some of related resource is still in progress<br /> |
| `Available` | Available means that all related resources are created<br /> |


#### VolumeStatus



VolumeStatus defines the observed state of Volume



_Appears in:_
- [Volume](#volume)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `state` _[VolumeState](#volumestate)_ | The state of a Volume object |  | Enum: [InProgress Available] <br />Optional: \{\} <br /> |



## svc.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the svc.dpf v1alpha1 API group





Package v1alpha1 contains API Schema definitions for the sfc v1alpha1 API group

### Resource Types
- [DPUDeployment](#dpudeployment)
- [DPUDeploymentList](#dpudeploymentlist)
- [DPUService](#dpuservice)
- [DPUServiceChain](#dpuservicechain)
- [DPUServiceChainList](#dpuservicechainlist)
- [DPUServiceConfiguration](#dpuserviceconfiguration)
- [DPUServiceConfigurationList](#dpuserviceconfigurationlist)
- [DPUServiceCredentialRequest](#dpuservicecredentialrequest)
- [DPUServiceCredentialRequestList](#dpuservicecredentialrequestlist)
- [DPUServiceIPAM](#dpuserviceipam)
- [DPUServiceIPAMList](#dpuserviceipamlist)
- [DPUServiceInterface](#dpuserviceinterface)
- [DPUServiceInterfaceList](#dpuserviceinterfacelist)
- [DPUServiceList](#dpuservicelist)
- [DPUServiceNAD](#dpuservicenad)
- [DPUServiceNADList](#dpuservicenadlist)
- [DPUServiceTemplate](#dpuservicetemplate)
- [DPUServiceTemplateList](#dpuservicetemplatelist)
- [NodeServiceInterfaces](#nodeserviceinterfaces)
- [NodeServiceInterfacesList](#nodeserviceinterfaceslist)
- [ServiceChain](#servicechain)
- [ServiceChainList](#servicechainlist)
- [ServiceChainSet](#servicechainset)
- [ServiceChainSetList](#servicechainsetlist)
- [ServiceInterface](#serviceinterface)
- [ServiceInterfaceList](#serviceinterfacelist)
- [ServiceInterfaceSet](#serviceinterfaceset)
- [ServiceInterfaceSetList](#serviceinterfacesetlist)



#### ApplicationSource



ApplicationSource specifies the source of the Helm chart.



_Appears in:_
- [HelmChart](#helmchart)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `repoURL` _string_ | RepoURL specifies the URL to the repository that contains the application Helm chart.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Required: \{\} <br /> |
| `path` _string_ | Path is the location of the chart inside the repo. |  | Optional: \{\} <br /> |
| `version` _string_ | Version is a semver tag for the Chart's version. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `chart` _string_ | Chart is the name of the helm chart. |  | Optional: \{\} <br /> |
| `releaseName` _string_ | ReleaseName is the name to give to the release generate from the DPUService. |  | Optional: \{\} <br /> |


#### CNIPlugin



CNIPlugin defines a CNI plugin to be used in a chained CNI configuration.
When multiple CNI plugins are specified in ChainedCNIs, they are executed in order
after the base OVS CNI plugin to provide additional network functionality.



_Appears in:_
- [DPUServiceNADSpec](#dpuservicenadspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type specifies the CNI plugin type to be used in the chain.<br />Currently only "rdma" is supported, which enables RDMA capabilities for the network interface. |  | Enum: [rdma] <br />Required: \{\} <br /> |
| `config` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Config contains optional plugin-specific configuration as raw JSON.<br />The configuration is merged into the CNI plugin configuration. |  | Optional: \{\} <br /> |


#### ConfigPort



ConfigPort defines the configuration of a single port within a DPUService.
Each port must have a unique name within the service.



_Appears in:_
- [ConfigPorts](#configports)
- [DPUServiceStatus](#dpuservicestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is a unique identifier for the port within the DPUService.<br />This name is used for reference inside the service. |  | MinLength: 1 <br />Pattern: `^[a-z0-9-]+$` <br />Required: \{\} <br /> |
| `port` _integer_ | Port is the port number that will be exposed by the service.<br />Must be within the valid range of TCP/UDP ports (1-65535). |  | Required: \{\} <br /> |
| `protocol` _[Protocol](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#protocol-v1-core)_ | Protocol specifies the transport protocol used by the port.<br />Supported values: TCP, UDP |  | Enum: [TCP UDP] <br />Required: \{\} <br /> |
| `nodePort` _integer_ | NodePort is the external port assigned on each node in the cluster.<br />If not set, Kubernetes will automatically allocate a NodePort.<br />Constraints:<br />- Can only be set when ServiceType is "NodePort".<br />- Must be within the clusters valid NodePort range (Kubernetes default is 30000-32767). |  | Optional: \{\} <br /> |


#### ConfigPorts



ConfigPorts defines the desired state of port configurations for a DPUService.
This struct determines how ports are exposed from the DPU to the host cluster.
A DPUService can only have a single ServiceType across all ports.

Validation:
- If any port has a NodePort assigned, ServiceType **must** be "NodePort".



_Appears in:_
- [DPUServiceSpec](#dpuservicespec)
- [ServiceConfiguration](#serviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceType` _[ServiceType](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#servicetype-v1-core)_ | ServiceType specifies the type of Kubernetes Service to create.<br />All ports within this ConfigPorts will have the same ServiceType.<br />The value is immutable and cannot be changed after creation.<br />Supported values:<br />- "NodePort": Exposes ports externally on a node.<br />- "ClusterIP": Exposes ports internally within the cluster.<br />- "None": Internal-only service with no cluster IP.<br />Default: "NodePort" | NodePort | Enum: [NodePort ClusterIP None] <br />Required: \{\} <br /> |
| `ports` _[ConfigPort](#configport) array_ | Ports defines the list of port configurations that will be exposed by the DPUService.<br />Each port must specify a name, port number, and protocol.<br />Constraints:<br />- If ServiceType is "NodePort", ports may optionally specify a NodePort.<br />- If ServiceType is "None" or "ClusterIP", ports **cannot** specify a NodePort. |  | Required: \{\} <br /> |


#### DPUClusterAllocation



DPUClusterAllocation contains the IP range allocations for a specific DPUCluster.



_Appears in:_
- [DPUServiceIPAMStatus](#dpuserviceipamstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuCluster` _string_ | DPUCluster is the NamespacedName of the DPUCluster in the format `<namespace>/<name>`. |  |  |
| `ipRanges` _[IPRange](#iprange) array_ | IPRanges contains the IP ranges allocated to this DPUCluster. |  | Optional: \{\} <br /> |


#### DPUDeployment



DPUDeployment is an object that describes a set of DPUServices and a DPUServiceChain that run a on a set of DPUs with
a given BFB or BlueFieldSoftware and DPUFlavor.



_Appears in:_
- [DPUDeploymentList](#dpudeploymentlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDeployment` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUDeploymentSpec](#dpudeploymentspec)_ |  |  |  |
| `status` _[DPUDeploymentStatus](#dpudeploymentstatus)_ |  |  |  |


#### DPUDeploymentList



DPUDeploymentList contains a list of DPUDeployment





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDeploymentList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUDeployment](#dpudeployment) array_ |  |  |  |


#### DPUDeploymentPort



DPUDeploymentPort defines how a port can be configured



_Appears in:_
- [DPUDeploymentSwitch](#dpudeploymentswitch)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `service` _[DPUDeploymentService](#dpudeploymentservice)_ | Service holds configuration that helps configure the Service Function Chain and identify a port associated with<br />a DPUService |  | Optional: \{\} <br /> |
| `serviceInterface` _[ServiceIfc](#serviceifc)_ | ServiceInterface holds configuration that helps configure the Service Function Chain and identify a user defined<br />port |  | Optional: \{\} <br /> |


#### DPUDeploymentService



DPUDeploymentService is the struct used for referencing an interface.



_Appears in:_
- [DPUDeploymentPort](#dpudeploymentport)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the service as defined in the DPUDeployment Spec |  | MaxLength: 28 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `interface` _string_ | Interface name is the name of the interface as defined in the DPUServiceConfiguration |  | MaxLength: 15 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `ipam` _[IPAM](#ipam)_ | IPAM defines the IPAM configuration that is configured in the Service Function Chain |  | Optional: \{\} <br /> |


#### DPUDeploymentServiceConfiguration



DPUDeploymentServiceConfiguration describes the configuration of a particular Service



_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceTemplate` _string_ | ServiceTemplate is the name of the DPUServiceTemplate object to be used for this Service. It must be in the same<br />namespace as the DPUDeployment. |  |  |
| `serviceConfiguration` _string_ | ServiceConfiguration is the name of the DPUServiceConfiguration object to be used for this Service. It must be<br />in the same namespace as the DPUDeployment. |  |  |
| `dependsOn` _[LocalObjectDependency](#localobjectdependency) array_ | DependsOn is a list of local object dependencies that are required for this Service. |  | MinItems: 1 <br />Optional: \{\} <br /> |


#### DPUDeploymentSpec



DPUDeploymentSpec defines the desired state of DPUDeployment



_Appears in:_
- [DPUDeployment](#dpudeployment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpus` _[DPUs](#dpus)_ | DPUs contains the DPU related configuration |  | Required: \{\} <br /> |
| `services` _object (keys:string, values:[DPUDeploymentServiceConfiguration](#dpudeploymentserviceconfiguration))_ | Services contains the DPUDeploymentService related configuration. The key is the deploymentServiceName and the value is its<br />configuration. All underlying objects must specify the same deploymentServiceName in order to be able to be consumed by the<br />DPUDeployment. |  | MaxProperties: 50 <br />MinProperties: 1 <br />Required: \{\} <br /> |
| `serviceChains` _[ServiceChains](#servicechains)_ | ServiceChains contains the configuration related to the DPUServiceChains that the DPUDeployment creates. |  | Optional: \{\} <br /> |
| `revisionHistoryLimit` _integer_ | The maximum number of revisions that can be retained during upgrades.<br />Defaults to 10. | 10 | Minimum: 1 <br />Optional: \{\} <br /> |


#### DPUDeploymentStatus



DPUDeploymentStatus defines the observed state of DPUDeployment



_Appears in:_
- [DPUDeployment](#dpudeployment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUDeploymentSwitch



DPUDeploymentSwitch holds the ports that are connected in switch topology



_Appears in:_
- [ServiceChains](#servicechains)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ports` _[DPUDeploymentPort](#dpudeploymentport) array_ | Ports contains the ports of the switch |  | MaxItems: 50 <br />MinItems: 1 <br />Required: \{\} <br /> |
| `serviceMTU` _integer_ | ServiceMTU of the switch<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1280 <br />Optional: \{\} <br /> |


#### DPUService



DPUService is the Schema for the dpuservices API



_Appears in:_
- [DPUServiceList](#dpuservicelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUService` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceSpec](#dpuservicespec)_ |  |  |  |
| `status` _[DPUServiceStatus](#dpuservicestatus)_ |  |  |  |


#### DPUServiceChain



DPUServiceChain is the Schema for the DPUServiceChain API



_Appears in:_
- [DPUServiceChainList](#dpuservicechainlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceChain` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceChainSpec](#dpuservicechainspec)_ |  |  |  |
| `status` _[DPUServiceChainStatus](#dpuservicechainstatus)_ |  |  |  |


#### DPUServiceChainList



DPUServiceChainList contains a list of DPUServiceChain





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceChainList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceChain](#dpuservicechain) array_ |  |  |  |


#### DPUServiceChainSpec



DPUServiceChainSpec defines the desired state of DPUServiceChainSpec



_Appears in:_
- [DPUServiceChain](#dpuservicechain)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Select the Clusters with specific labels, ServiceChainSet CRs will be created only for these Clusters<br />Deprecated: This field is deprecated and will be removed with v26.7.0. Use DPUClusterSelector instead. |  | Optional: \{\} <br /> |
| `dpuClusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUClusterSelector determines in which clusters the DPUServiceChain controller should apply the configuration. |  | Optional: \{\} <br /> |
| `template` _[ServiceChainSetSpecTemplate](#servicechainsetspectemplate)_ | Template describes the ServiceChainSet that will be created for each selected Cluster. |  |  |


#### DPUServiceChainStatus



DPUServiceChainStatus defines the observed state of DPUServiceChain



_Appears in:_
- [DPUServiceChain](#dpuservicechain)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUServiceConfiguration



DPUServiceConfiguration is the Schema for the dpuserviceconfigurations API. This object is intended to be used in
conjunction with a DPUDeployment object. This object is the template from which the DPUService will be created. It
contains all configuration options from the user to be provided to the service itself via the helm chart values.
This object doesn't allow configuration of nodeSelector and resources in purpose as these are delegated to the
DPUDeployment and DPUServiceTemplate accordingly.



_Appears in:_
- [DPUServiceConfigurationList](#dpuserviceconfigurationlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceConfiguration` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)_ |  |  |  |
| `status` _[DPUServiceConfigurationStatus](#dpuserviceconfigurationstatus)_ |  |  |  |


#### DPUServiceConfigurationList



DPUServiceConfigurationList contains a list of DPUServiceConfiguration





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceConfigurationList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceConfiguration](#dpuserviceconfiguration) array_ |  |  |  |


#### DPUServiceConfigurationServiceDaemonSetValues



DPUServiceConfigurationServiceDaemonSetValues reflects the Helm related configuration



_Appears in:_
- [ServiceConfiguration](#serviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `updateStrategy` _[DaemonSetUpdateStrategy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#daemonsetupdatestrategy-v1-apps)_ | UpdateStrategy specifies the DeaemonSet update strategy for the ServiceDaemonset. |  | Optional: \{\} <br /> |
| `labels` _object (keys:string, values:string)_ | Labels specifies labels which are added to the ServiceDaemonSet. |  | MaxProperties: 50 <br />Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ | Annotations specifies annotations which are added to the ServiceDaemonSet. |  | MaxProperties: 50 <br />Optional: \{\} <br /> |
| `resources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | Resources specifies resources which are added to the ServiceDaemonSet. |  | Optional: \{\} <br /> |


#### DPUServiceConfigurationSpec



DPUServiceConfigurationSpec defines the desired state of DPUServiceConfiguration



_Appears in:_
- [DPUServiceConfiguration](#dpuserviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `deploymentServiceName` _string_ | DeploymentServiceName is the name of the DPU service this configuration refers to. It must match<br />.spec.deploymentServiceName of a DPUServiceTemplate object and one of the keys in .spec.services of a<br />DPUDeployment object. |  | MaxLength: 28 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `serviceConfiguration` _[ServiceConfiguration](#serviceconfiguration)_ | ServiceConfiguration contains fields that are configured on the generated DPUService. |  | Optional: \{\} <br /> |
| `interfaces` _[ServiceInterfaceTemplate](#serviceinterfacetemplate) array_ | Interfaces specifies the DPUServiceInterface to be generated for the generated DPUService. |  | MaxItems: 50 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `upgradePolicy` _[UpgradePolicy](#upgradepolicy)_ | UpgradePolicy contains the configuration for the upgrade process | \{  \} | Required: \{\} <br /> |


#### DPUServiceConfigurationStatus



DPUServiceConfigurationStatus defines the observed state of DPUServiceConfiguration



_Appears in:_
- [DPUServiceConfiguration](#dpuserviceconfiguration)



#### DPUServiceCredentialRequest



DPUServiceCredentialRequest is the Schema for the dpuserviceCredentialRequests API



_Appears in:_
- [DPUServiceCredentialRequestList](#dpuservicecredentialrequestlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceCredentialRequest` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceCredentialRequestSpec](#dpuservicecredentialrequestspec)_ |  |  |  |
| `status` _[DPUServiceCredentialRequestStatus](#dpuservicecredentialrequeststatus)_ |  |  |  |


#### DPUServiceCredentialRequestList



DPUServiceCredentialRequestList contains a list of DPUServiceCredentialRequest





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceCredentialRequestList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceCredentialRequest](#dpuservicecredentialrequest) array_ |  |  |  |


#### DPUServiceCredentialRequestSpec



DPUServiceCredentialRequestSpec defines the desired state of DPUServiceCredentialRequest



_Appears in:_
- [DPUServiceCredentialRequest](#dpuservicecredentialrequest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceAccount` _[NamespacedName](#namespacedname)_ | ServiceAccount defines the needed information to create the service account. |  | Required: \{\} <br /> |
| `duration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | Duration is the duration for which the token will be valid.<br />Value must be in units accepted by Go time.ParseDuration https://golang.org/pkg/time/#ParseDuration.<br />e.g. "1h", "1m", "1s", "1ms", "1.5h", "2h45m".<br />Value duration must not be less than 10 minutes.<br />**Note:** The maximum TTL for a token is 24 hours, after which the token<br />will be rotated. |  | Pattern: `^([0-9]+(\.[0-9]+)?(ms\|s\|m\|h))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `targetCluster` _[NamespacedName](#namespacedname)_ | TargetCluster defines the target cluster where the service account will<br />be created, and where a token for that service account will be requested.<br />If not provided, the token will be requested for the same cluster where<br />the DPUServiceCredentialRequest object is created. |  | Optional: \{\} <br /> |
| `type` _string_ | Type is the type of the secret that will be created.<br />The supported types are `kubeconfig` and `tokenFile`.<br />If `kubeconfig` is selected, the secret will contain a kubeconfig file,<br />that can be used to access the cluster.<br />If `tokenFile` is selected, the secret will contain a token file and several<br />environment variables that can be used to access the cluster. It can be used<br />with https://github.com/kubernetes/client-go/blob/v11.0.0/rest/config.go#L52<br />to create a client that will handle file rotation. |  | Enum: [kubeconfig tokenFile] <br />Required: \{\} <br /> |
| `secret` _[NamespacedName](#namespacedname)_ | Secret defines the needed information to create the secret.<br />The secret will be of the type specified in the `spec.type` field. |  | Required: \{\} <br /> |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |


#### DPUServiceCredentialRequestStatus



DPUServiceCredentialRequestStatus defines the observed state of DPUServiceCredentialRequest



_Appears in:_
- [DPUServiceCredentialRequest](#dpuservicecredentialrequest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  | Optional: \{\} <br /> |
| `serviceAccount` _string_ | ServiceAccount is the namespaced name of the ServiceAccount resource created by<br />the controller for the DPUServiceCredentialRequest. |  |  |
| `targetCluster` _string_ | TargetCluster is the cluster where the service account was created.<br />It has to be persisted in the status to be able to delete the service account<br />when the DPUServiceCredentialRequest is updated. |  | Optional: \{\} <br /> |
| `expirationTimestamp` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | ExpirationTimestamp is the time when the token will expire. |  | Optional: \{\} <br /> |
| `issuedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | IssuedAt is the time when the token was issued. |  | Optional: \{\} <br /> |
| `secret` _string_ | Sercet is the namespaced name of the Secret resource created by the controller for<br />the DPUServiceCredentialRequest. |  |  |


#### DPUServiceIPAM



DPUServiceIPAM is the Schema for the dpuserviceipams API



_Appears in:_
- [DPUServiceIPAMList](#dpuserviceipamlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceIPAM` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceIPAMSpec](#dpuserviceipamspec)_ |  |  |  |
| `status` _[DPUServiceIPAMStatus](#dpuserviceipamstatus)_ |  |  |  |


#### DPUServiceIPAMList



DPUServiceIPAMList contains a list of DPUServiceIPAM





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceIPAMList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceIPAM](#dpuserviceipam) array_ |  |  |  |


#### DPUServiceIPAMSpec



DPUServiceIPAMSpec defines the desired state of DPUServiceIPAM



_Appears in:_
- [DPUServiceIPAM](#dpuserviceipam)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `ipv4Network` _[IPV4Network](#ipv4network)_ | IPV4Network is the configuration related to splitting a network into subnets per node, each with their own gateway. |  |  |
| `ipv4Subnet` _[IPV4Subnet](#ipv4subnet)_ | IPV4Subnet is the configuration related to splitting a subnet into blocks per node. In this setup, there is a<br />single gateway. |  |  |
| `clusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | ClusterSelector determines in which clusters the DPUServiceIPAM controller should apply the configuration.<br />Deprecated: This field is deprecated and will be removed with v26.7.0. Use DPUClusterSelector instead. |  | Optional: \{\} <br /> |
| `dpuClusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUClusterSelector determines in which clusters the DPUServiceIPAM controller should apply the configuration. |  | Optional: \{\} <br /> |
| `nodeSelector` _[NodeSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#nodeselector-v1-core)_ | NodeSelector determines in which DPU nodes the DPUServiceIPAM controller should apply the configuration. |  |  |


#### DPUServiceIPAMStatus



DPUServiceIPAMStatus defines the observed state of DPUServiceIPAM



_Appears in:_
- [DPUServiceIPAM](#dpuserviceipam)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |
| `dpuClusterAllocations` _[DPUClusterAllocation](#dpuclusterallocation) array_ | DPUClusterAllocations contains the IPV4Network/IPV4Subnet allocations per DPUCluster as calculated by the controller. |  | Optional: \{\} <br /> |


#### DPUServiceInterface



DPUServiceInterface is the Schema for the DPUServiceInterface API



_Appears in:_
- [DPUServiceInterfaceList](#dpuserviceinterfacelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceInterface` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceInterfaceSpec](#dpuserviceinterfacespec)_ |  |  |  |
| `status` _[DPUServiceInterfaceStatus](#dpuserviceinterfacestatus)_ |  |  |  |


#### DPUServiceInterfaceList



DPUServiceInterfaceList contains a list of DPUServiceInterface





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceInterfaceList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceInterface](#dpuserviceinterface) array_ |  |  |  |


#### DPUServiceInterfaceSpec



DPUServiceInterfaceSpec defines the desired state of DPUServiceInterfaceSpec



_Appears in:_
- [DPUServiceInterface](#dpuserviceinterface)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Select the Clusters with specific labels, ServiceInterfaceSet CRs will be created only for these Clusters<br />Deprecated: This field is deprecated and will be removed with v26.7.0. Use DPUClusterSelector instead. |  | Optional: \{\} <br /> |
| `dpuClusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUClusterSelector determines in which clusters the DPUServiceInterface controller should apply the configuration. |  | Optional: \{\} <br /> |
| `template` _[ServiceInterfaceSetSpecTemplate](#serviceinterfacesetspectemplate)_ | Template describes the ServiceInterfaceSet that will be created for each selected Cluster. |  |  |


#### DPUServiceInterfaceStatus



DPUServiceInterfaceStatus defines the observed state of DPUServiceInterface



_Appears in:_
- [DPUServiceInterface](#dpuserviceinterface)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUServiceList



DPUServiceList contains a list of DPUService





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUService](#dpuservice) array_ |  |  |  |


#### DPUServiceNAD



DPUServiceNAD is the Schema for the dpuservicenads API.



_Appears in:_
- [DPUServiceNADList](#dpuservicenadlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceNAD` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceNADSpec](#dpuservicenadspec)_ |  |  |  |
| `status` _[DPUServiceNADStatus](#dpuservicenadstatus)_ |  |  |  |


#### DPUServiceNADList



DPUServiceNADList contains a list of DPUServiceNAD.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceNADList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceNAD](#dpuservicenad) array_ |  |  |  |


#### DPUServiceNADSpec



DPUServiceNADSpec defines the desired state of DPUServiceNAD.



_Appears in:_
- [DPUServiceNAD](#dpuservicenad)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuClusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUClusterSelector determines in which clusters the DPUServiceNAD controller should apply the configuration. |  | Optional: \{\} <br /> |
| `resourceType` _string_ | ResourceType specifies the type of network resource to allocate for pods using this NAD.<br />- "vf": Virtual Function (SR-IOV VF) from the DPU's physical ports<br />- "sf": Scalable Function from the DPU (maps to nvidia.com/bf_sf or nvidia.com/bf_sf_trusted)<br />- "veth": Virtual Ethernet pair (no device plugin resource required)<br />The resource type determines which SR-IOV device plugin resource will be requested. |  | Enum: [vf sf veth] <br />Required: \{\} <br /> |
| `bridge` _string_ | Bridge specifies the name of the OVS bridge to which the network interface will be connected.<br />This bridge name is used in the CNI configuration for the OVS plugin. |  | Optional: \{\} <br /> |
| `serviceMTU` _integer_ | ServiceMTU specifies the MTU size in bytes for the network interface.<br />This value is passed to the OVS CNI plugin and determines the maximum packet size.<br />If there is a DPUServiceChain that references an interface that is part of this network,<br />then the MTU that is defined in the DPUServiceChain takes precedence.<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1280 <br />Optional: \{\} <br /> |
| `ipam` _boolean_ | IPAM enables IP Address Management for the network interfaces attached to this network<br />When set to true, a DPUServiceChain that references the DPUServiceInterface that has<br />requested this network must be created and include the relevant IPAM information. See<br />DPUServiceChain documentation for more.<br />When set to false, the network interfaces attached to this network will not get an IP |  | Optional: \{\} <br /> |
| `chainedCNIs` _[CNIPlugin](#cniplugin) array_ | ChainedCNIs specifies additional CNI plugins to be chained after the base OVS plugin.<br />When specified, the NAD will use the CNI chaining format with the OVS plugin as the<br />first plugin, followed by the plugins defined in this list.<br />This allows adding capabilities like RDMA support on top of the base network interface.<br />If empty, the NAD uses a single OVS plugin configuration (backward compatible format). |  | Optional: \{\} <br /> |


#### DPUServiceNADStatus



DPUServiceNADStatus defines the observed state of DPUServiceNAD.



_Appears in:_
- [DPUServiceNAD](#dpuservicenad)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |




#### DPUServiceSecurity



DPUServiceSecurity contains security-related settings for a DPUService or
DPUServiceTemplate.



_Appears in:_
- [DPUServiceSpec](#dpuservicespec)
- [DPUServiceTemplateSpec](#dpuservicetemplatespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `privileged` _boolean_ | Privileged, when set to true, allows workloads governed by this security<br />setting to run containers with `securityContext.privileged: true` in the<br />DPUCluster. When set to false, a ValidatingAdmissionPolicy in the<br />DPUCluster rejects such workloads. On DPUService objects, this field must<br />be unset when deployInCluster is true. For DPUServices that target a<br />DPUCluster, Privileged must be set explicitly. When set on a<br />DPUServiceTemplate, the DPUDeployment controller propagates the value to<br />generated DPUServices that target DPUClusters.<br />Scope of the policy enforcement:<br />  - Only `securityContext.privileged: true` is gated. Other<br />    privilege-escalation vectors (hostPID, hostIPC, hostNetwork,<br />    allowPrivilegeEscalation, capabilities, hostPath volumes) are NOT<br />    gated by this field.<br />  - Enforcement matches workloads via the<br />    `svc.dpu.nvidia.com/service` label. The controller adds this<br />    label to the resources it manages, and to pod templates of<br />    workload-controller resources (Deployment, DaemonSet, etc.). If a<br />    Helm chart strips that label from the pod template, the parent<br />    resource will be admitted but the child Pods will be denied at<br />    Pod admission time. |  | Optional: \{\} <br /> |


#### DPUServiceSpec



DPUServiceSpec defines the desired state of DPUService



_Appears in:_
- [DPUService](#dpuservice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuClusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Select the Clusters with specific labels, Applications will be created only for these Clusters |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart reflects the Helm related configuration |  | Required: \{\} <br /> |
| `serviceID` _string_ | ServiceID is the ID of the service that the DPUService is associated with. |  | Optional: \{\} <br /> |
| `serviceDaemonSet` _[ServiceDaemonSetValues](#servicedaemonsetvalues)_ | ServiceDaemonSet specifies the configuration for the ServiceDaemonSet. |  | Optional: \{\} <br /> |
| `deployInCluster` _boolean_ | DeployInCluster indicates if the DPUService Helm Chart will be deployed on<br />the Host cluster. Default to false. |  | Optional: \{\} <br /> |
| `interfaces` _string array_ | Interfaces specifies the DPUServiceInterface names that the DPUService<br />uses in the same namespace. |  | MaxItems: 50 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `paused` _boolean_ | Paused indicates that the DPUService is paused.<br />Underlying resources are also paused when this is set to true.<br />No deletion of resources will occur when this is set to true. |  | Optional: \{\} <br /> |
| `configPorts` _[ConfigPorts](#configports)_ | ConfigPorts defines the desired state of port configurations for a DPUService.<br />This struct determines how ports are exposed from the DPU to the host cluster.<br />A DPUService can only have a single ServiceType across all ports. |  | Optional: \{\} <br /> |
| `security` _[DPUServiceSecurity](#dpuservicesecurity)_ | Security contains security-related settings for the DPUService. |  | Optional: \{\} <br /> |


#### DPUServiceStatus



DPUServiceStatus defines the observed state of DPUService



_Appears in:_
- [DPUService](#dpuservice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  | Optional: \{\} <br /> |
| `configPorts` _object (keys:string, values:[ConfigPort](#configport))_ | ConfigPorts defines the observed state of the config ports.<br />It contains the actual port numbers that are exposed on the DPUService per cluster. |  | Optional: \{\} <br /> |
| `serviceID` _string_ | ServiceID is the ID of the service that the DPUService is associated with.<br />This is set when the DPUService is created. |  | Optional: \{\} <br /> |


#### DPUServiceTemplate



DPUServiceTemplate is the Schema for the DPUServiceTemplate API. This object is intended to be used in
conjunction with a DPUDeployment object. This object is the template from which the DPUService will be created. It
contains configuration options related to resources required by the service to be deployed. The rest of the
configuration options must be defined in a DPUServiceConfiguration object.



_Appears in:_
- [DPUServiceTemplateList](#dpuservicetemplatelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceTemplate` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceTemplateSpec](#dpuservicetemplatespec)_ |  |  |  |
| `status` _[DPUServiceTemplateStatus](#dpuservicetemplatestatus)_ |  |  |  |


#### DPUServiceTemplateList



DPUServiceTemplateList contains a list of DPUServiceTemplate





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceTemplateList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceTemplate](#dpuservicetemplate) array_ |  |  |  |


#### DPUServiceTemplateSpec



DPUServiceTemplateSpec defines the desired state of DPUServiceTemplate



_Appears in:_
- [DPUServiceTemplate](#dpuservicetemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `deploymentServiceName` _string_ | DeploymentServiceName is the name of the DPU service this configuration refers to. It must match<br />.spec.deploymentServiceName of a DPUServiceConfiguration object and one of the keys in .spec.services of a<br />DPUDeployment object. |  | MaxLength: 28 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart reflects the Helm related configuration. The user is supposed to configure the values that are static<br />across any DPUServiceConfiguration used with this DPUServiceTemplate in a DPUDeployment. These values act as a<br />baseline and are merged with values specified in the DPUServiceConfiguration. In case of conflict, the<br />DPUServiceConfiguration values take precedence. |  | Required: \{\} <br /> |
| `resourceRequirements` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | ResourceRequirements contains the overall resources required by this particular service to run on a single node |  | Optional: \{\} <br /> |
| `security` _[DPUServiceSecurity](#dpuservicesecurity)_ | Security contains security-related settings for the generated DPUService.<br />These settings are inherent to the service and apply to all deployments<br />using this template. Specifically, Privileged should be set on charts<br />that need privileged containers (e.g. for direct hardware access). |  | Optional: \{\} <br /> |


#### DPUServiceTemplateStatus



DPUServiceTemplateStatus defines the observed state of DPUServiceTemplate



_Appears in:_
- [DPUServiceTemplate](#dpuservicetemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  | Optional: \{\} <br /> |
| `versions` _object (keys:string, values:string)_ | Versions reflects the required versions the generated DPUService needs in order to function correctly. |  | Optional: \{\} <br /> |


#### DPUSet



DPUSet contains configuration for the DPUSet to be created by the DPUDeployment



_Appears in:_
- [DPUs](#dpus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nameSuffix` _string_ | NameSuffix is the suffix to be added to the name of the DPUSet object created by the DPUDeployment. |  | MaxLength: 24 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | NodeSelector defines the nodes that the DPUSet should target<br />Deprecated: This field is deprecated and will be removed with v26.7.0. Use DPUNodeSelector instead. |  | Optional: \{\} <br /> |
| `dpuSelector` _object (keys:string, values:string)_ | DPUSelector defines the DPUs that the DPUSet should target<br />Deprecated: This field is deprecated and will be removed with v26.7.0. Use DPUDeviceSelector instead. |  | Optional: \{\} <br /> |
| `dpuNodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUNodeSelector defines the selector for DPUNodes that the DPUSet should target and should create a DPU for. |  | Optional: \{\} <br /> |
| `dpuDeviceSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUDeviceSelector defines the selector for DPUDevices that the DPUSet should target and should create a DPU for. |  | Optional: \{\} <br /> |
| `dpuClusterSelector` _object (keys:string, values:string)_ | DPUClusterSelector defines the selector for DPUClusters that the DPUs created by the DPUSets created by the<br />DPUDeployment should join<br />require multiple DPUServices, DPUServiceInterfaces, and DPUServiceChains to be created so that we can mathematically<br />cover the union of all the selectors across all the DPUSets. |  | Optional: \{\} <br /> |
| `dpuAnnotations` _object (keys:string, values:string)_ | DPUAnnotations is the annotations to be added to the DPU object created by the DPUSet. |  | MaxProperties: 50 <br />Optional: \{\} <br /> |


#### DPUs



DPUs contains the DPU related configuration



_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bfb` _string_ | BFB is the name of the BFB object to be used in this DPUDeployment. It must be in the same namespace as the<br />DPUDeployment. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `blueFieldSoftware` _string_ | BlueFieldSoftware is the name of the BlueFieldSoftware object to be used in this DPUDeployment. It must be in the<br />same namespace as the DPUDeployment. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `flavor` _string_ | Flavor is the name of the DPUFlavor object to be used in this DPUDeployment. It must be in the same namespace as<br />the DPUDeployment. |  | Required: \{\} <br /> |
| `astraEnabled` _boolean_ | AstraEnabled indicates whether E/W NIC configuration (Astra) is enabled |  | Optional: \{\} <br /> |
| `dpuSets` _[DPUSet](#dpuset) array_ | DPUSets contains configuration for each DPUSet that is going to be created by the DPUDeployment |  | MaxItems: 50 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `nodeEffect` _[Action](#action)_ | NodeEffect is the effect the DPU has on Nodes during provisioning. |  | Required: \{\} <br /> |
| `dpuSetStrategy` _[DPUSetStrategy](#dpusetstrategy)_ | DPUSetStrategy is the strategy to use for the DPUSets created by the DPUDeployment. |  | Required: \{\} <br /> |
| `secureBoot` _boolean_ | SecureBoot specifies whether UEFI Secure Boot should be enabled. |  | Optional: \{\} <br /> |


#### HelmChart



HelmChart reflects the helm related configuration



_Appears in:_
- [DPUServiceSpec](#dpuservicespec)
- [DPUServiceTemplateSpec](#dpuservicetemplatespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `source` _[ApplicationSource](#applicationsource)_ | Source specifies information about the Helm chart |  | Required: \{\} <br /> |
| `values` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Values specifies Helm values to be passed to Helm template, defined as a map. This takes precedence over Values. |  | Optional: \{\} <br /> |




#### IPRange







_Appears in:_
- [DPUClusterAllocation](#dpuclusterallocation)
- [IPV4Network](#ipv4network)
- [IPV4Subnet](#ipv4subnet)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `startIP` _string_ | StartIP is the start of the range. |  | Required: \{\} <br /> |
| `endIP` _string_ | EndIP is the end of the range. |  | Required: \{\} <br /> |


#### IPV4Network



IPV4Network describes the configuration relevant to splitting a network into subnet per node (i.e. different gateway and
broadcast IP per node).



_Appears in:_
- [DPUServiceIPAMSpec](#dpuserviceipamspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `network` _string_ | Network is the CIDR from which subnets should be allocated per node. |  |  |
| `gatewayIndex` _integer_ | GatewayIndex determines which IP in the subnet extracted from the CIDR should be the gateway IP. For point to<br />point networks (/31), one needs to leave this empty to make use of both the IPs. |  |  |
| `prefixSize` _integer_ | PrefixSize is the size of the subnet that should be allocated per node. |  |  |
| `exclusions` _string array_ | Exclusions is a list of IPs that should be excluded when splitting the CIDR into subnets per node.<br />Deprecated: This field is deprecated and will be removed with v26.10.0. Use ExcludeRanges instead. |  |  |
| `excludeRanges` _[IPRange](#iprange) array_ | ExcludeRanges is a list of IP ranges that should be excluded from the allocation.<br />startIP and endIP are part of the Excluded range. |  |  |
| `allocations` _object (keys:string, values:string)_ | Allocations describes the subnets that should be assigned in each DPU node. |  |  |
| `defaultGateway` _boolean_ | DefaultGateway adds gateway as default gateway in the routes list if true. |  |  |
| `routes` _[Route](#route) array_ | Routes is the static routes list using the gateway specified in the spec. |  |  |
| `subnetsPerDPUCluster` _integer_ | SubnetsPerDPUCluster is the number of PrefixSize-sized subnets each DPUCluster should receive.<br />When specified, the controller will take care of assigning non-overlapping subnets part of the Network in each<br />DPUCluster that the DPUServiceIPAM is targeting. Leave empty in case you want the whole Network to be consumed<br />by a single DPUCluster. |  | Optional: \{\} <br /> |


#### IPV4Subnet



IPV4Subnet describes the configuration relevant to splitting a subnet to a subnet block per node (i.e. same gateway
and broadcast IP across all nodes).



_Appears in:_
- [DPUServiceIPAMSpec](#dpuserviceipamspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `subnet` _string_ | Subnet is the CIDR from which blocks should be allocated per node |  |  |
| `gateway` _string_ | Gateway is the IP in the subnet that should be the gateway of the subnet. |  |  |
| `perNodeIPCount` _integer_ | PerNodeIPCount is the number of IPs that should be allocated per node. |  |  |
| `excludeRanges` _[IPRange](#iprange) array_ | ExcludeRanges is a list of IP ranges that should be excluded from the allocation.<br />startIP and endIP are part of the Excluded range. |  |  |
| `defaultGateway` _boolean_ | if true, add gateway as default gateway in the routes list<br />DefaultGateway adds gateway as default gateway in the routes list if true. |  |  |
| `routes` _[Route](#route) array_ | Routes is the static routes list using the gateway specified in the spec. |  |  |
| `blocksPerDPUCluster` _integer_ | BlocksPerDPUCluster is the number of PerNodeIPCount-sized blocks each DPUCluster should receive.<br />When specified, the controller will take care of assigning non-overlapping IP blocks part of the Subnet in each<br />DPUCluster that the DPUServiceIPAM is targeting. Leave empty in case you want the whole Subnet to be consumed by<br />a single DPUCluster. |  | Optional: \{\} <br /> |


#### InterfaceEntry



InterfaceEntry defines a single service interface entry within a NodeServiceInterfaces object.



_Appears in:_
- [NodeServiceInterfacesSpec](#nodeserviceinterfacesspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name uniquely identifies this entry within the NodeServiceInterfaces object.<br />Format: `<namespace>_<service-interface-set-name>`. The underscore separator is collision-free<br />because Kubernetes namespace and resource names follow DNS subdomain rules<br />and can never contain an underscore. Both components are<br />bounded to 63 chars, so the full name is at most 127 chars. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `terminating` _boolean_ | Terminating indicates this entry is marked for removal. Cleanup may<br />require one or more reconcilers. ResourceReleased=True is set only<br />after all required cleanup conditions are satisfied in status. |  |  |
| `labels` _object (keys:string, values:string)_ | Labels used for ServiceChain matchLabels resolution. |  | MaxProperties: 50 <br />Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ | Annotations carry operational metadata for this entry. |  | MaxProperties: 50 <br />Optional: \{\} <br /> |
| `interfaceType` _string_ | InterfaceType is the type of the interface. |  | Enum: [vlan physical pf vf ovn patch service] <br />Required: \{\} <br /> |
| `physical` _[Physical](#physical)_ | Physical is the physical interface definition. |  | Optional: \{\} <br /> |
| `vlan` _[VLAN](#vlan)_ | Vlan is the VLAN definition. |  | Optional: \{\} <br /> |
| `vf` _[VF](#vf)_ | VF is the VF definition. |  | Optional: \{\} <br /> |
| `pf` _[PF](#pf)_ | PF is the PF definition. |  | Optional: \{\} <br /> |
| `service` _[ServiceDef](#servicedef)_ | Service is the service definition. |  | Optional: \{\} <br /> |
| `ovn` _[OVN](#ovn)_ | OVN is the OVN definition. |  | Optional: \{\} <br /> |
| `patch` _[PatchDef](#patchdef)_ | Patch is the patch definition. |  | Optional: \{\} <br /> |


#### InterfaceEntryStatus



InterfaceEntryStatus records reconciliation state for a single interface entry.



_Appears in:_
- [NodeServiceInterfacesStatus](#nodeserviceinterfacesstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name matches the spec entry name. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `params` _object (keys:string, values:string)_ | Params carries controller-specific information for handling reconcile/release of the matching InterfaceEntry in spec.<br />Multiple reconcilers may write distinct keys safely as long as they do not share keys. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions may be written by multiple reconcilers for the same entry. |  | Optional: \{\} <br /> |


#### LocalObjectDependency



LocalObjectDependency is a list of local object dependencies that are required for this Service.
The object must be part of the dpuDeployment `spec.services` list.



_Appears in:_
- [DPUDeploymentServiceConfiguration](#dpudeploymentserviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the object |  | Required: \{\} <br /> |


#### NICSelectorSpec



NICSelectorSpec defines how a NIC is selected



_Appears in:_
- [PF](#pf)
- [VF](#vf)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[NICSelectorType](#nicselectortype)_ | Type is the type of selector to be used to identify the NIC. |  | Enum: [dpu pci] <br />Required: \{\} <br /> |
| `pci` _[PCISelector](#pciselector)_ | PCI is the PCI selector. valid only for selector type pci |  | Optional: \{\} <br /> |
| `controllerNumber` _integer_ | ControllerNumber specifies the controller number that will be used to find the matching representor on the DPU.<br />A value of 0 targets the local controller, >=1 targets external controllers with the specified number. If unspecified controller number 1 is used.<br />In case of a DPU/NIC with socket direct or MultiHost, the user should specify the controller number explicitly |  | Maximum: 255 <br />Minimum: 0 <br />Optional: \{\} <br /> |


#### NICSelectorType

_Underlying type:_ _string_

NICSelectorType is the type of NIC selector



_Appears in:_
- [NICSelectorSpec](#nicselectorspec)

| Field | Description |
| --- | --- |
| `dpu` | NICSelectorTypeDPU selects the DPU NIC<br /> |
| `pci` | NICSelectorTypePCI selects NIC according to the provided PCI address of one of the NIC's Embedded CPU PFs (ECPFs)<br /> |


#### NamespacedName



NamespacedName contains enough information to locate the referenced Kubernetes resource object in any
namespace.



_Appears in:_
- [DPUServiceCredentialRequestSpec](#dpuservicecredentialrequestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name of the object. |  | Required: \{\} <br /> |
| `namespace` _string_ | Namespace of the object, if not provided the object will be looked up in<br />the same namespace as the referring object |  | Optional: \{\} <br /> |


#### NodeServiceInterfaces



NodeServiceInterfaces is the Schema for the nodeserviceinterfaces API.



_Appears in:_
- [NodeServiceInterfacesList](#nodeserviceinterfaceslist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `NodeServiceInterfaces` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[NodeServiceInterfacesSpec](#nodeserviceinterfacesspec)_ |  |  |  |
| `status` _[NodeServiceInterfacesStatus](#nodeserviceinterfacesstatus)_ |  |  |  |


#### NodeServiceInterfacesList



NodeServiceInterfacesList contains a list of NodeServiceInterfaces





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `NodeServiceInterfacesList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[NodeServiceInterfaces](#nodeserviceinterfaces) array_ |  |  |  |


#### NodeServiceInterfacesSpec



NodeServiceInterfacesSpec defines the desired state of NodeServiceInterfaces.



_Appears in:_
- [NodeServiceInterfaces](#nodeserviceinterfaces)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `node` _string_ | Node is the name of the DPU node this object represents. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `type` _string_ | Type identifies which controller domain owns this NSI shard.<br />Examples: "sfc", "vpc-my-provisioner". |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `interfaces` _[InterfaceEntry](#interfaceentry) array_ | Interfaces is the list of service interface entries for this node. |  | MaxItems: 256 <br />Optional: \{\} <br /> |


#### NodeServiceInterfacesStatus



NodeServiceInterfacesStatus defines the observed state of NodeServiceInterfaces.



_Appears in:_
- [NodeServiceInterfaces](#nodeserviceinterfaces)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect aggregate status. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration is the last observed generation of the NodeServiceInterfaces object. |  | Optional: \{\} <br /> |
| `interfaceStatuses` _[InterfaceEntryStatus](#interfaceentrystatus) array_ | InterfaceStatuses tracks per-entry reconciliation state. Written by<br />the managing controller. The params map carries controller-specific<br />release information needed during terminating/release handling. |  | Optional: \{\} <br /> |


#### OVN



OVN defines the configuration for OVN interface type



_Appears in:_
- [InterfaceEntry](#interfaceentry)
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `externalBridge` _string_ | ExternalBridge is the name of the OVN bridge | br-ovn | Optional: \{\} <br /> |


#### ObjectMeta



ObjectMeta holds metadata like labels and annotations.



_Appears in:_
- [DPUServiceCredentialRequestSpec](#dpuservicecredentialrequestspec)
- [DPUServiceIPAMSpec](#dpuserviceipamspec)
- [ServiceChainSetSpecTemplate](#servicechainsetspectemplate)
- [ServiceChainSpecTemplate](#servicechainspectemplate)
- [ServiceInterfaceSetSpecTemplate](#serviceinterfacesetspectemplate)
- [ServiceInterfaceSpecTemplate](#serviceinterfacespectemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `labels` _object (keys:string, values:string)_ | Labels is a map of string keys and values. |  | Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ | Annotations is a map of string keys and values. |  | Optional: \{\} <br /> |


#### PCISelector

_Underlying type:_ _[struct{Address string "json:\"address,omitzero\""}](#struct{address-string-"json:\"address,omitzero\""})_

PCISelector selects NIC by PCI address



_Appears in:_
- [NICSelectorSpec](#nicselectorspec)



#### PF



PF defines the PF configuration



_Appears in:_
- [InterfaceEntry](#interfaceentry)
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nicSelector` _[NICSelectorSpec](#nicselectorspec)_ | NICSelector defines the NIC selected for the PF interface |  | Optional: \{\} <br /> |
| `pfID` _integer_ | The PF ID |  | Maximum: 255 <br />Minimum: 0 <br />Required: \{\} <br /> |
| `virtualNetwork` _string_ | VirtualNetwork is the VirtualNetwork name in the same namespace |  | Optional: \{\} <br /> |


#### PatchDef



PatchDef defines the configuration for Patch interface type



_Appears in:_
- [InterfaceEntry](#interfaceentry)
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `peerBridge` _string_ | PeerBridge is the name of the bridge to which the patch port is connected.<br />This bridge must be created before the ServiceInterface is created. |  | Required: \{\} <br /> |
| `peerPatchName` _string_ | PeerPatchName is the name of the patch port on the peer bridge.<br />If not set, it is auto-generated in the format: `p_<bridgeA>_to_<bridgeB>_<hash>`<br />where bridge names have hyphens removed and `<hash>` is an 8-character FNV-1a hash<br />derived from the ServiceInterface's namespace/name.<br />Example: p_brovn_to_brsfc_7aea60f7 (for bridges br-ovn and br-sfc). |  | Optional: \{\} <br /> |
| `peerExternalIDs` _object (keys:string, values:string)_ | PeerExternalIDs are the external IDs used to identify the peer patch port. |  | Optional: \{\} <br /> |


#### Physical



Physical Identifies a physical interface



_Appears in:_
- [InterfaceEntry](#interfaceentry)
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `interfaceName` _string_ | The interface name |  | Required: \{\} <br /> |


#### Port



Port defines the port configuration



_Appears in:_
- [Switch](#switch)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceInterface` _[ServiceIfc](#serviceifc)_ |  |  | Required: \{\} <br /> |


#### Route



Route contains static route parameters



_Appears in:_
- [IPV4Network](#ipv4network)
- [IPV4Subnet](#ipv4subnet)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dst` _string_ | The destination of the route, in CIDR notation |  |  |


#### ServiceChain



ServiceChain is the Schema for the servicechains API



_Appears in:_
- [ServiceChainList](#servicechainlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceChain` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ServiceChainSpec](#servicechainspec)_ |  |  |  |
| `status` _[ServiceChainStatus](#servicechainstatus)_ |  |  |  |


#### ServiceChainList



ServiceChainList contains a list of ServiceChain





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceChainList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ServiceChain](#servicechain) array_ |  |  |  |


#### ServiceChainSet



ServiceChainSet is the Schema for the servicechainsets API



_Appears in:_
- [ServiceChainSetList](#servicechainsetlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceChainSet` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ServiceChainSetSpec](#servicechainsetspec)_ |  |  |  |
| `status` _[ServiceChainSetStatus](#servicechainsetstatus)_ |  |  |  |


#### ServiceChainSetList



ServiceChainSetList contains a list of ServiceChainSet





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceChainSetList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ServiceChainSet](#servicechainset) array_ |  |  |  |


#### ServiceChainSetSpec



ServiceChainSetSpec defines the desired state of ServiceChainSet



_Appears in:_
- [ServiceChainSet](#servicechainset)
- [ServiceChainSetSpecTemplate](#servicechainsetspectemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Select the Nodes with specific labels, ServiceChain CRs will be created<br />only for these Nodes |  | Optional: \{\} <br /> |
| `template` _[ServiceChainSpecTemplate](#servicechainspectemplate)_ | ServiceChainSpecTemplate holds the template for the ServiceChainSpec |  | Required: \{\} <br /> |


#### ServiceChainSetSpecTemplate



ServiceChainSetSpecTemplate describes the data a ServiceChainSet should have when created from a template.



_Appears in:_
- [DPUServiceChainSpec](#dpuservicechainspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `spec` _[ServiceChainSetSpec](#servicechainsetspec)_ |  |  |  |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |


#### ServiceChainSetStatus



ServiceChainSetStatus defines the observed state of ServiceChainSet



_Appears in:_
- [ServiceChainSet](#servicechainset)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |
| `numberApplied` _integer_ | The number of nodes where the service chain is applied and is supposed to be applied. |  |  |
| `numberReady` _integer_ | The number of nodes where the service chain is applied and ready. |  |  |


#### ServiceChainSpec



ServiceChainSpec defines the desired state of ServiceChain



_Appears in:_
- [ServiceChain](#servicechain)
- [ServiceChainSpecTemplate](#servicechainspectemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `node` _string_ | Node where this ServiceChain applies to |  | Optional: \{\} <br /> |
| `switches` _[Switch](#switch) array_ | The switches of the ServiceChain, order is significant |  | MaxItems: 50 <br />MinItems: 1 <br />Required: \{\} <br /> |


#### ServiceChainSpecTemplate



ServiceChainSpecTemplate defines the template from which ServiceChainSpecs
are created



_Appears in:_
- [ServiceChainSetSpec](#servicechainsetspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `spec` _[ServiceChainSpec](#servicechainspec)_ | ServiceChainSpec is the spec for the ServiceChainSpec |  | Required: \{\} <br /> |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |


#### ServiceChainStatus



ServiceChainStatus defines the observed state of ServiceChain



_Appears in:_
- [ServiceChain](#servicechain)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### ServiceChains







_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `upgradePolicy` _[UpgradePolicy](#upgradepolicy)_ | UpgradePolicy contains the configuration for the upgrade process | \{  \} | Required: \{\} <br /> |
| `switches` _[DPUDeploymentSwitch](#dpudeploymentswitch) array_ | Switches is the list of switches that form the service chain |  | MaxItems: 50 <br />MinItems: 1 <br />Required: \{\} <br /> |


#### ServiceConfiguration



ServiceConfiguration contains fields that are configured on the generated DPUService.



_Appears in:_
- [DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `helmChart` _[ServiceConfigurationHelmChart](#serviceconfigurationhelmchart)_ | HelmChart reflects the Helm related configuration. The user is supposed to configure values specific to that<br />DPUServiceConfiguration used in a DPUDeployment and should not specify values that could be shared across multiple<br />DPUDeployments using different DPUServiceConfigurations. These values are merged with values specified in the<br />DPUServiceTemplate. In case of conflict, the DPUServiceConfiguration values take precedence. |  | Optional: \{\} <br /> |
| `serviceDaemonSet` _[DPUServiceConfigurationServiceDaemonSetValues](#dpuserviceconfigurationservicedaemonsetvalues)_ | ServiceDaemonSet contains settings related to the underlying DaemonSet that is part of the Helm chart |  | Optional: \{\} <br /> |
| `deployInCluster` _boolean_ | DeployInCluster indicates if the DPUService Helm Chart will be deployed on the Host cluster. Default to false. |  | Optional: \{\} <br /> |
| `configPorts` _[ConfigPorts](#configports)_ | ConfigPorts defines the desired state of port configurations for a DPUService.<br />This struct determines how ports are exposed from the DPU to the host cluster.<br />A DPUService can only have a single ServiceType across all ports. |  | Optional: \{\} <br /> |


#### ServiceConfigurationHelmChart



ServiceConfigurationHelmChart reflects the helm related configuration



_Appears in:_
- [ServiceConfiguration](#serviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `values` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Values specifies Helm values to be passed to Helm template, defined as a map. This takes precedence over Values. |  | Optional: \{\} <br /> |


#### ServiceDaemonSetValues



ServiceDaemonSetValues specifies the configuration for the ServiceDaemonSet.



_Appears in:_
- [DPUServiceSpec](#dpuservicespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[NodeSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#nodeselector-v1-core)_ | NodeSelector specifies which Nodes to deploy the ServiceDaemonSet to. |  | Optional: \{\} <br /> |
| `updateStrategy` _[DaemonSetUpdateStrategy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#daemonsetupdatestrategy-v1-apps)_ | UpdateStrategy specifies the DeaemonSet update strategy for the ServiceDaemonset. |  | Optional: \{\} <br /> |
| `labels` _object (keys:string, values:string)_ | Labels specifies labels which are added to the ServiceDaemonSet. |  | Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ | Annotations specifies annotations which are added to the ServiceDaemonSet. |  | Optional: \{\} <br /> |
| `resources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | Resources specifies resources which are added to the ServiceDaemonSet. |  | Optional: \{\} <br /> |


#### ServiceDef



ServiceDef Identifies the service and network for the ServiceInterface



_Appears in:_
- [InterfaceEntry](#interfaceentry)
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceID` _string_ | ServiceID is the DPU Service Identifier |  | Required: \{\} <br /> |
| `network` _string_ | Network is the Network Attachment Definition in the form of "namespace/name"<br />or just "name" if the namespace is the same as the ServiceInterface. |  | Required: \{\} <br /> |
| `interfaceName` _string_ | The interface name |  | MaxLength: 15 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `virtualNetwork` _string_ | VirtualNetwork is the VirtualNetwork name in the same namespace |  | Optional: \{\} <br /> |


#### ServiceIfc



ServiceIfc defines the service interface configuration



_Appears in:_
- [DPUDeploymentPort](#dpudeploymentport)
- [Port](#port)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `matchLabels` _object (keys:string, values:string)_ | Labels matching service interface |  | MaxProperties: 50 <br />MinProperties: 1 <br />Required: \{\} <br /> |
| `ipam` _[IPAM](#ipam)_ | IPAM defines the IPAM configuration when referencing a serviceInterface of type 'service' |  | Optional: \{\} <br /> |


#### ServiceInterface



ServiceInterface is the Schema for the serviceinterfaces API



_Appears in:_
- [ServiceInterfaceList](#serviceinterfacelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceInterface` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ServiceInterfaceSpec](#serviceinterfacespec)_ |  |  |  |
| `status` _[ServiceInterfaceStatus](#serviceinterfacestatus)_ |  |  |  |


#### ServiceInterfaceList



ServiceInterfaceList contains a list of ServiceInterface





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceInterfaceList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ServiceInterface](#serviceinterface) array_ |  |  |  |


#### ServiceInterfaceSet



ServiceInterfaceSet is the Schema for the serviceinterfacesets API



_Appears in:_
- [ServiceInterfaceSetList](#serviceinterfacesetlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceInterfaceSet` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ServiceInterfaceSetSpec](#serviceinterfacesetspec)_ |  |  |  |
| `status` _[ServiceInterfaceSetStatus](#serviceinterfacesetstatus)_ |  |  |  |


#### ServiceInterfaceSetList



ServiceInterfaceSetList contains a list of ServiceInterfaceSet





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceInterfaceSetList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ServiceInterfaceSet](#serviceinterfaceset) array_ |  |  |  |


#### ServiceInterfaceSetSpec



ServiceInterfaceSetSpec defines the desired state of ServiceInterfaceSet



_Appears in:_
- [ServiceInterfaceSet](#serviceinterfaceset)
- [ServiceInterfaceSetSpecTemplate](#serviceinterfacesetspectemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Select the Nodes with specific labels, ServiceInterface CRs will be<br />created only for these Nodes |  | Optional: \{\} <br /> |
| `template` _[ServiceInterfaceSpecTemplate](#serviceinterfacespectemplate)_ | Template holds the template for the serviceInterfaceSpec |  | Required: \{\} <br /> |


#### ServiceInterfaceSetSpecTemplate



ServiceInterfaceSetSpecTemplate describes the data a ServiceInterfaceSet should have when created from a template.



_Appears in:_
- [DPUServiceInterfaceSpec](#dpuserviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `spec` _[ServiceInterfaceSetSpec](#serviceinterfacesetspec)_ |  |  |  |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |


#### ServiceInterfaceSetStatus



ServiceInterfaceSetStatus defines the observed state of ServiceInterfaceSet



_Appears in:_
- [ServiceInterfaceSet](#serviceinterfaceset)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |
| `numberApplied` _integer_ | The number of nodes where the service chain is applied and is supposed to be applied. |  |  |
| `numberReady` _integer_ | The number of nodes where the service chain is applied and ready. |  |  |


#### ServiceInterfaceSpec



ServiceInterfaceSpec defines the desired state of ServiceInterface



_Appears in:_
- [ServiceInterface](#serviceinterface)
- [ServiceInterfaceSpecTemplate](#serviceinterfacespectemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `node` _string_ | Node where this interface exists |  | Optional: \{\} <br /> |
| `interfaceType` _string_ | The interface type ("vlan", "physical", "pf", "vf", "ovn", "patch", "service") |  | Enum: [vlan physical pf vf ovn patch service] <br />Required: \{\} <br /> |
| `physical` _[Physical](#physical)_ | The physical interface definition |  | Optional: \{\} <br /> |
| `vlan` _[VLAN](#vlan)_ | The VLAN definition |  | Optional: \{\} <br /> |
| `vf` _[VF](#vf)_ | The VF definition |  | Optional: \{\} <br /> |
| `pf` _[PF](#pf)_ | The PF definition |  | Optional: \{\} <br /> |
| `service` _[ServiceDef](#servicedef)_ | The Service definition |  | Optional: \{\} <br /> |
| `ovn` _[OVN](#ovn)_ | The OVN definition<br />Deprecated: This field is deprecated and will be removed with v26.10.0.<br />Migrate to interfaceType="patch" with spec.patch.peerBridge and spec.patch.peerPatchName instead. |  | Optional: \{\} <br /> |
| `patch` _[PatchDef](#patchdef)_ | The Patch definition |  | Optional: \{\} <br /> |


#### ServiceInterfaceSpecTemplate



ServiceInterfaceSpecTemplate defines the template from which ServiceInterfaceSpecs
are created



_Appears in:_
- [ServiceInterfaceSetSpec](#serviceinterfacesetspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `spec` _[ServiceInterfaceSpec](#serviceinterfacespec)_ | ServiceInterfaceSpec is the spec for the ServiceInterfaceSpec |  | Required: \{\} <br /> |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |


#### ServiceInterfaceStatus



ServiceInterfaceStatus defines the observed state of ServiceInterface



_Appears in:_
- [ServiceInterface](#serviceinterface)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### ServiceInterfaceTemplate



ServiceInterfaceTemplate contains the information related to an interface of the DPUService



_Appears in:_
- [DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the interface |  | MaxLength: 15 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `network` _string_ | Network is the Network Attachment Definition in the form of "namespace/name"<br />or just "name" if the namespace is the same as the namespace the pod is running. |  | Required: \{\} <br /> |
| `virtualNetwork` _string_ | VirtualNetwork is the VirtualNetwork name in the same namespace |  | Optional: \{\} <br /> |


#### Switch



Switch defines the switch configuration



_Appears in:_
- [ServiceChainSpec](#servicechainspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ports` _[Port](#port) array_ | Ports of the switch |  | MaxItems: 50 <br />MinItems: 1 <br />Required: \{\} <br /> |
| `serviceMTU` _integer_ | ServiceMTU of the switch<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1280 <br />Optional: \{\} <br /> |


#### UpgradePolicy







_Appears in:_
- [DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)
- [ServiceChains](#servicechains)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `applyNodeEffect` _boolean_ | ApplyNodeEffect specifies if the node effect should be applied during the<br />upgrade. It signals the reconciler that this object upgrade is disruptive.<br />Hence a new revision of the object should be created and node effect should<br />be applied. | true | Optional: \{\} <br /> |


#### VF



VF defines the VF configuration



_Appears in:_
- [InterfaceEntry](#interfaceentry)
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nicSelector` _[NICSelectorSpec](#nicselectorspec)_ | NICSelector defines the NIC selected for the VF interface |  | Optional: \{\} <br /> |
| `vfID` _integer_ | The VF ID |  | Maximum: 255 <br />Minimum: 0 <br />Required: \{\} <br /> |
| `pfID` _integer_ | The PF ID |  | Maximum: 255 <br />Minimum: 0 <br />Required: \{\} <br /> |
| `parentInterfaceRef` _string_ | The parent interface reference |  | Optional: \{\} <br /> |
| `virtualNetwork` _string_ | VirtualNetwork is the VirtualNetwork name in the same namespace |  | Optional: \{\} <br /> |


#### VLAN



VLAN defines the VLAN configuration



_Appears in:_
- [InterfaceEntry](#interfaceentry)
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vlanID` _integer_ | The VLAN ID |  | Required: \{\} <br /> |
| `parentInterfaceRef` _string_ | The parent interface reference |  | Required: \{\} <br /> |



## vpc.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the storage v1alpha1 API group




### Resource Types
- [DPUVPC](#dpuvpc)
- [DPUVPCList](#dpuvpclist)
- [DPUVirtualNetwork](#dpuvirtualnetwork)
- [DPUVirtualNetworkList](#dpuvirtualnetworklist)
- [IsolationClass](#isolationclass)
- [IsolationClassList](#isolationclasslist)



#### BridgedNetworkIPAMIPv4Spec



BridgedNetworkIPAMIPv4Spec contains IPv4 IPAM configuration for bridged network



_Appears in:_
- [BridgedNetworkIPAMSpec](#bridgednetworkipamspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dhcp` _boolean_ | DHCP if set, enables DHCP for the network |  | Required: \{\} <br /> |
| `subnet` _string_ | Subnet is the network subnet in CIDR format to use for DHCP. the first IP in the subnet is the gateway. |  | Required: \{\} <br /> |
| `excludeIPs` _[ExcludeIPsEntry](#excludeipsentry) array_ | ExcludeIPs are the IPs to exclude from DHCP allocation. |  | Optional: \{\} <br /> |


#### BridgedNetworkIPAMSpec



BridgedNetworkIPAMSpec contains IPAM configuration for bridged network



_Appears in:_
- [BridgedNetworkSpec](#bridgednetworkspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipv4` _[BridgedNetworkIPAMIPv4Spec](#bridgednetworkipamipv4spec)_ | IPv4 contains the IPv4 IPAM configuration |  | Optional: \{\} <br /> |


#### BridgedNetworkSpec



BridgedNetworkSpec contains configuration for bridged network



_Appears in:_
- [DPUVirtualNetworkSpec](#dpuvirtualnetworkspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipam` _[BridgedNetworkIPAMSpec](#bridgednetworkipamspec)_ | IPAM contains the IPAM configuration for the bridged network |  | Optional: \{\} <br /> |


#### DPUVPC



DPUVPC is the Schema for the dpuvpc API



_Appears in:_
- [DPUVPCList](#dpuvpclist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVPC` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUVPCSpec](#dpuvpcspec)_ |  |  |  |
| `status` _[DPUVPCStatus](#dpuvpcstatus)_ |  |  |  |


#### DPUVPCList



DPUVPCList contains a list of DPUVPC





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVPCList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUVPC](#dpuvpc) array_ |  |  |  |


#### DPUVPCSpec



DPUVPCSpec defines the desired state of DPUVPCSpec



_Appears in:_
- [DPUVPC](#dpuvpc)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `tenant` _string_ | Tenant which owns the VPC. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | NodeSelector Selects the DPU Nodes with specific labels which belong to this VPC. |  | Optional: \{\} <br /> |
| `isolationClassName` _string_ | IsolationClassName is the name of the isolation class to use for the VPC |  | MinLength: 1 <br />Required: \{\} <br /> |
| `interNetworkAccess` _boolean_ | InterNetworkAccess defines if virtual networks within the VPC are routed or not.<br />if set to false, communication between virtual networks is not allowed. |  | Required: \{\} <br /> |


#### DPUVPCStatus



DPUVPCStatus defines the observed state of DPUVPC



_Appears in:_
- [DPUVPC](#dpuvpc)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `virtualNetworks` _[VirtualNetworkStatus](#virtualnetworkstatus) array_ | VirtualNetworks contains the virtual networks that belong to this VPC |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  | Optional: \{\} <br /> |


#### DPUVirtualNetwork



DPUVirtualNetwork is the Schema for the dpuvirtualnetwork API



_Appears in:_
- [DPUVirtualNetworkList](#dpuvirtualnetworklist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVirtualNetwork` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUVirtualNetworkSpec](#dpuvirtualnetworkspec)_ |  |  |  |
| `status` _[DPUVirtualNetworkStatus](#dpuvirtualnetworkstatus)_ |  |  |  |


#### DPUVirtualNetworkList



DPUVirtualNetworkList contains a list of DPUVirtualNetwork





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVirtualNetworkList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUVirtualNetwork](#dpuvirtualnetwork) array_ |  |  |  |


#### DPUVirtualNetworkSpec



DPUVirtualNetworkSpec defines the desired state of DPUVirtualNetworkSpec



_Appears in:_
- [DPUVirtualNetwork](#dpuvirtualnetwork)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | NodeSelector Selects the DPU Nodes with specific labels which can belong to the virtual network. |  | Optional: \{\} <br /> |
| `vpcName` _string_ | vpcName is the name of the DPUVPC the virtual network belongs within the same namespace. |  | Required: \{\} <br /> |
| `type` _[NetworkType](#networktype)_ | Type of the virtual network |  | Enum: [Bridged] <br />Required: \{\} <br /> |
| `externallyRouted` _boolean_ | ExternallyRouted defines if the virtual network can be routed externally |  | Required: \{\} <br /> |
| `masquerade` _boolean_ | Masquerade defines if the virtual network should masquerade the traffic before egressing to external networks.<br />valid only if ExternallyRouted is true | true | Optional: \{\} <br /> |
| `bridgedNetwork` _[BridgedNetworkSpec](#bridgednetworkspec)_ | BridgedNetwork contains the bridged network configuration |  | Optional: \{\} <br /> |


#### DPUVirtualNetworkStatus



DPUVirtualNetworkStatus defines the observed state of DPUVirtualNetwork



_Appears in:_
- [DPUVirtualNetwork](#dpuvirtualnetwork)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  | Optional: \{\} <br /> |


#### ExcludeIPsEntry







_Appears in:_
- [BridgedNetworkIPAMIPv4Spec](#bridgednetworkipamipv4spec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ip` _string_ | IP is the IP address to exclude from DHCP allocation. must be part for the virtual network subnet. |  | Optional: \{\} <br /> |
| `range` _[RangeEntry](#rangeentry)_ | Range is the range of IP addresses to exclude from DHCP allocation. must be part for the virtual network subnet. |  | Optional: \{\} <br /> |


#### IsolationClass



IsolationClass is the Schema for the isolationclass API



_Appears in:_
- [IsolationClassList](#isolationclasslist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `IsolationClass` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[IsolationClassSpec](#isolationclassspec)_ |  |  |  |
| `status` _[IsolationClassStatus](#isolationclassstatus)_ |  |  |  |


#### IsolationClassList



IsolationClassList contains a list of IsolationClass





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `IsolationClassList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[IsolationClass](#isolationclass) array_ |  |  |  |


#### IsolationClassSpec



IsolationClassSpec defines the configuration of IsolationClass



_Appears in:_
- [IsolationClass](#isolationclass)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `provisioner` _string_ | Provisioner indicates the type of the provisioner. |  | Required: \{\} <br /> |
| `parameters` _object (keys:string, values:string)_ | Parameters holds the parameters for the provisioner |  | Optional: \{\} <br /> |


#### IsolationClassStatus



IsolationClassStatus defines the status of IsolationClass



_Appears in:_
- [IsolationClass](#isolationclass)



#### NetworkType

_Underlying type:_ _string_

NetworkType represents the type of the virtual network

_Validation:_
- Enum: [Bridged]

_Appears in:_
- [DPUVirtualNetworkSpec](#dpuvirtualnetworkspec)

| Field | Description |
| --- | --- |
| `Bridged` | BridgedVirtualNetworkType represents a bridged virtual network<br /> |


#### RangeEntry

_Underlying type:_ _[struct{Start string "json:\"start\""; End string "json:\"end\""}](#struct{start-string-"json:\"start\"";-end-string-"json:\"end\""})_

RangeEntry contains a range of IP addresses



_Appears in:_
- [ExcludeIPsEntry](#excludeipsentry)



#### VirtualNetworkStatus



VirtualNetworkStatus is the status of a virtual network



_Appears in:_
- [DPUVPCStatus](#dpuvpcstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | the name of the virtual network |  | Required: \{\} <br /> |


