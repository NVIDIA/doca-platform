# API Reference

## Packages
- [operator.dpu.nvidia.com/v1alpha1](#operatordpunvidiacomv1alpha1)
- [provisioning.dpu.nvidia.com/v1alpha1](#provisioningdpunvidiacomv1alpha1)
- [svc.dpu.nvidia.com/v1alpha1](#svcdpunvidiacomv1alpha1)


## operator.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the operator v1alpha1 API group

### Resource Types
- [DPFOperatorConfig](#dpfoperatorconfig)
- [DPFOperatorConfigList](#dpfoperatorconfiglist)





#### DPFOperatorConfig



DPFOperatorConfig is the Schema for the dpfoperatorconfigs API



_Appears in:_
- [DPFOperatorConfigList](#dpfoperatorconfiglist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `operator.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPFOperatorConfig` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPFOperatorConfigSpec](#dpfoperatorconfigspec)_ |  |  |  |
| `status` _[DPFOperatorConfigStatus](#dpfoperatorconfigstatus)_ |  |  |  |


#### DPFOperatorConfigList



DPFOperatorConfigList contains a list of DPFOperatorConfig





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `operator.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPFOperatorConfigList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPFOperatorConfig](#dpfoperatorconfig) array_ |  |  |  |


#### DPFOperatorConfigSpec



DPFOperatorConfigSpec defines the desired state of DPFOperatorConfig



_Appears in:_
- [DPFOperatorConfig](#dpfoperatorconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `overrides` _[Overrides](#overrides)_ |  |  |  |
| `networking` _[Networking](#networking)_ |  | \{ controlPlaneMTU:1500 \} |  |
| `imagePullSecrets` _string array_ | List of secret names which are used to pull images for DPF system components and DPUServices.<br />These secrets must be in the same namespace as the DPF Operator Config and should be created before the config is created.<br />System reconciliation will not proceed until these secrets are available. |  |  |
| `dpuServiceController` _[DPUServiceControllerConfiguration](#dpuservicecontrollerconfiguration)_ | DPUServiceController is the configuration for the DPUServiceController |  |  |
| `provisioningController` _[ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)_ | ProvisioningController is the configuration for the ProvisioningController |  |  |
| `serviceSetController` _[ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)_ | ServiceSetController is the configuration for the ServiceSetController |  |  |
| `dpuDetector` _[DPUDetectorConfiguration](#dpudetectorconfiguration)_ | DPUDetector is the configuration for the DPUDetector. |  |  |
| `multus` _[MultusConfiguration](#multusconfiguration)_ | Multus is the configuration for Multus |  |  |
| `sriovDevicePlugin` _[SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)_ | SRIOVDevicePlugin is the configuration for the SRIOVDevicePlugin |  |  |
| `flannel` _[FlannelConfiguration](#flannelconfiguration)_ | Flannel is the configuration for Flannel |  |  |
| `ovsCNI` _[OVSCNIConfiguration](#ovscniconfiguration)_ | OVSCNI is the configuration for OVSCNI |  |  |
| `nvipam` _[NVIPAMConfiguration](#nvipamconfiguration)_ | NVIPAM is the configuration for NVIPAM |  |  |
| `sfcController` _[SFCControllerConfiguration](#sfccontrollerconfiguration)_ | SFCController is the configuration for the SFCController |  |  |
| `kamajiClusterManager` _[KamajiClusterManagerConfiguration](#kamajiclustermanagerconfiguration)_ | KamajiClusterManager is the configuration for the kamaji-cluster-manager |  |  |
| `staticClusterManager` _[StaticClusterManagerConfiguration](#staticclustermanagerconfiguration)_ | StaticClusterManager is the configuration for the static-cluster-manager |  |  |
| `ovsHelper` _[OVSHelperConfiguration](#ovshelperconfiguration)_ | OVSHelper is the configuration for the OVSHelper |  |  |


#### DPFOperatorConfigStatus



DPFOperatorConfigStatus defines the observed state of DPFOperatorConfig



_Appears in:_
- [DPFOperatorConfig](#dpfoperatorconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions exposes the current state of the OperatorConfig. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUDetectorCollectors







_Appears in:_
- [DPUDetectorConfiguration](#dpudetectorconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `psID` _boolean_ | PSID enables collecting PSID information for DPUs on nodes. |  |  |


#### DPUDetectorConfiguration



DPUDetectorConfiguration is the configuration for the DPUDetector Component.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the component. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `collectors` _[DPUDetectorCollectors](#dpudetectorcollectors)_ | Collectors enables or disables specific collectors. |  |  |


#### DPUServiceControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the DPUService controller |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |


#### FlannelConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the Flannel<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### HelmChart

_Underlying type:_ _string_

HelmChart is a reference to a helm chart.

_Validation:_
- Pattern: `^(oci://|https://).+$`

_Appears in:_
- [FlannelConfiguration](#flannelconfiguration)
- [MultusConfiguration](#multusconfiguration)
- [NVIPAMConfiguration](#nvipamconfiguration)
- [OVSCNIConfiguration](#ovscniconfiguration)
- [OVSHelperConfiguration](#ovshelperconfiguration)
- [SFCControllerConfiguration](#sfccontrollerconfiguration)
- [SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)
- [ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)





#### Image

_Underlying type:_ _string_

Image is a reference to a container image.
Validation is the same as the implementation at https://github.com/containers/image/blob/93fa49b0f1fb78470512e0484012ca7ad3c5c804/docker/reference/regexp.go

_Validation:_
- Pattern: `^((?:(?:(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]{0,127}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]{32,}))?$`

_Appears in:_
- [DPUDetectorConfiguration](#dpudetectorconfiguration)
- [DPUServiceControllerConfiguration](#dpuservicecontrollerconfiguration)
- [KamajiClusterManagerConfiguration](#kamajiclustermanagerconfiguration)
- [MultusConfiguration](#multusconfiguration)
- [NVIPAMConfiguration](#nvipamconfiguration)
- [OVSCNIConfiguration](#ovscniconfiguration)
- [OVSHelperConfiguration](#ovshelperconfiguration)
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)
- [SFCControllerConfiguration](#sfccontrollerconfiguration)
- [SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)
- [ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)
- [StaticClusterManagerConfiguration](#staticclustermanagerconfiguration)





#### KamajiClusterManagerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the HostedControlPlaneManager. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |


#### MultusConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by Multus |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by Multus<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### NVIPAMConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by NVIPAM |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by NVIPAM<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### Networking



Networking defines the networking configuration for the system components.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `controlPlaneMTU` _integer_ | ControlPlaneMTU is the MTU value to be set on the management network.<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 0 <br /> |
| `highSpeedMTU` _integer_ | HighSpeedMTU is the MTU value to be set on the high-speed interface.<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 0 <br /> |


#### OVSCNIConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the OVS CNI |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the OVS CNI<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### OVSHelperConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the OVS Helper |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the OVS Helper<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### Overrides



Overrides exposes a set of fields which impact the recommended behavior of the DPF Operator.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `paused` _boolean_ | Paused disables all reconciliation of the DPFOperatorConfig when set to true. |  |  |


#### ProvisioningControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the Provisioning controller |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `bfbPVCName` _string_ | BFBPersistentVolumeClaimName is the name of the PersistentVolumeClaim used by dpf-provisioning-controller |  | MinLength: 1 <br /> |
| `dmsTimeout` _integer_ | DMSTimeout is the max time in seconds within which a DMS API must respond, 0 is unlimited |  | Minimum: 1 <br /> |


#### SFCControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the SFC Controller |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the SFC Controller<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### SRIOVDevicePluginConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the SRIOV Device Plugin |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the SRIOV Device Plugin<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### ServiceSetControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the ServiceSetController |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### StaticClusterManagerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image is the container image used by the StaticControlPlaneManager<br />Image overrides the container image used by the HostedControlPlaneManager. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |



## provisioning.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the provisioning.dpu v1alpha1 API group

### Resource Types
- [BFB](#bfb)
- [BFBList](#bfblist)
- [DPU](#dpu)
- [DPUCluster](#dpucluster)
- [DPUClusterList](#dpuclusterlist)
- [DPUFlavor](#dpuflavor)
- [DPUFlavorList](#dpuflavorlist)
- [DPUList](#dpulist)
- [DPUSet](#dpuset)
- [DPUSetList](#dpusetlist)



#### BFB



BFB is the Schema for the bfbs API



_Appears in:_
- [BFBList](#bfblist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `BFB` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[BFBSpec](#bfbspec)_ |  |  |  |
| `status` _[BFBStatus](#bfbstatus)_ |  | \{ phase:Initializing \} |  |


#### BFBList



BFBList contains a list of BFB





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `BFBList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
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
| `name` _string_ | Specifies name of the bfb CR to use for this DPU |  |  |


#### BFBSpec



BFBSpec defines the content of the BFB



_Appears in:_
- [BFB](#bfb)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `fileName` _string_ | Specifies the file name which is used to download the BFB on the volume or<br />use "namespace-CRD name" in case it is omitted. |  | Pattern: `^[A-Za-z0-9\_\-\.]+\.bfb$` <br /> |
| `url` _string_ | The url of the bfb image to download. |  | Pattern: `^(http\|https)://.+\.bfb$` <br /> |


#### BFBStatus



BFBStatus defines the observed state of BFB



_Appears in:_
- [BFB](#bfb)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[BFBPhase](#bfbphase)_ | The current state of BFB. | Initializing | Enum: [Initializing Downloading Ready Deleting Error] <br /> |


#### ClusterEndpointSpec







_Appears in:_
- [DPUClusterSpec](#dpuclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `keepalived` _[KeepalivedSpec](#keepalivedspec)_ | Keepalived configures the keepalived that will be deployed for the cluster control-plane |  |  |


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

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeLabels` _object (keys:string, values:string)_ | NodeLabels specifies the labels to be added to the node. |  |  |






#### ConfigFile







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `path` _string_ | Path is the path of the file to be written. |  |  |
| `operation` _[DPUFlavorFileOp](#dpuflavorfileop)_ | Operation is the operation to be performed on the file. |  | Enum: [override append] <br /> |
| `raw` _string_ | Raw is the raw content of the file. |  |  |
| `permissions` _string_ | Permissions are the permissions to be set on the file. |  |  |


#### ContainerdConfig







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `registryEndpoint` _string_ | RegistryEndpoint is the endpoint of the container registry. |  |  |


#### DPU



DPU is the Schema for the dpus API



_Appears in:_
- [DPUList](#dpulist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPU` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUSpec](#dpuspec)_ |  |  |  |
| `status` _[DPUStatus](#dpustatus)_ |  | \{ phase:Initializing \} |  |


#### DPUCluster



DPUCluster is the Schema for the dpuclusters API



_Appears in:_
- [DPUClusterList](#dpuclusterlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUCluster` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUClusterSpec](#dpuclusterspec)_ |  |  |  |
| `status` _[DPUClusterStatus](#dpuclusterstatus)_ |  | \{ phase:Pending \} |  |


#### DPUClusterList



DPUClusterList contains a list of DPUCluster





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUClusterList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUCluster](#dpucluster) array_ |  |  |  |


#### DPUClusterSpec



DPUClusterSpec defines the desired state of DPUCluster



_Appears in:_
- [DPUCluster](#dpucluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type of the cluster with few supported values<br />static - existing cluster that is deployed by user. For DPUCluster of this type, the kubeconfig field must be set.<br />kamaji - DPF managed cluster. The kamaji-cluster-manager will create a DPU cluster on behalf of this CR.<br />$(others) - any string defined by ISVs, such type names must start with a prefix. |  | Pattern: `kamaji\|static\|[^/]+/.*` <br /> |
| `maxNodes` _integer_ | MaxNodes is the max amount of node in the cluster | 1000 | Maximum: 1000 <br />Minimum: 1 <br /> |
| `version` _string_ | Version is the K8s control-plane version of the cluster |  |  |
| `kubeconfig` _string_ | Kubeconfig is the secret that contains the admin kubeconfig |  |  |
| `clusterEndpoint` _[ClusterEndpointSpec](#clusterendpointspec)_ | ClusterEndpoint contains configurations of the cluster entry point |  |  |


#### DPUClusterStatus



DPUClusterStatus defines the observed state of DPUCluster



_Appears in:_
- [DPUCluster](#dpucluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[ClusterPhase](#clusterphase)_ |  | Pending | Enum: [Pending Creating Ready NotReady Failed] <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ |  |  |  |




#### DPUFLavorSysctl







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `parameters` _string array_ | Parameters are the sysctl parameters to be set. |  |  |


#### DPUFlavor



DPUFlavor is the Schema for the dpuflavors API



_Appears in:_
- [DPUFlavorList](#dpuflavorlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUFlavor` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
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
| `kernelParameters` _string array_ | KernelParameters are the kernel parameters to be set in the grub configuration. |  |  |


#### DPUFlavorList



DPUFlavorList contains a list of DPUFlavor





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUFlavorList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUFlavor](#dpuflavor) array_ |  |  |  |


#### DPUFlavorNVConfig







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `device` _string_ | Device is the device to which the configuration applies. If not specified, the configuration applies to all. |  |  |
| `parameters` _string array_ | Parameters are the parameters to be set for the device. |  |  |
| `hostPowerCycleRequired` _boolean_ | HostPowerCycleRequired indicates if the host needs to be power cycled after applying the configuration. |  |  |


#### DPUFlavorOVS







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `rawConfigScript` _string_ | RawConfigScript is the raw configuration script for OVS. |  |  |


#### DPUFlavorSpec



DPUFlavorSpec defines the content of DPUFlavor



_Appears in:_
- [DPUFlavor](#dpuflavor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `grub` _[DPUFlavorGrub](#dpuflavorgrub)_ | Grub contains the grub configuration for the DPUFlavor. |  |  |
| `sysctl` _[DPUFLavorSysctl](#dpuflavorsysctl)_ | Sysctl contains the sysctl configuration for the DPUFlavor. |  |  |
| `nvconfig` _[DPUFlavorNVConfig](#dpuflavornvconfig) array_ | NVConfig contains the configuration for the DPUFlavor. |  |  |
| `ovs` _[DPUFlavorOVS](#dpuflavorovs)_ | OVS contains the OVS configuration for the DPUFlavor. |  |  |
| `bfcfgParameters` _string array_ | BFCfgParameters are the parameters to be set in the bf.cfg file. |  |  |
| `configFiles` _[ConfigFile](#configfile) array_ | ConfigFiles are the files to be written on the DPU. |  |  |
| `containerdConfig` _[ContainerdConfig](#containerdconfig)_ | ContainerdConfig contains the configuration for containerd. |  |  |
| `dpuResources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#resourcelist-v1-core)_ | DPUResources indicates the minimum amount of resources needed for a BFB with that flavor to be installed on a<br />DPU. Using this field, the controller can understand if that flavor can be installed on a particular DPU. It<br />should be set to the total amount of resources the system needs + the resources that should be made available for<br />DPUServices to consume. |  |  |
| `systemReservedResources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#resourcelist-v1-core)_ | SystemReservedResources indicates the resources that are consumed by the system (OS, OVS, DPF system etc) and are<br />not made available for DPUServices to consume. DPUServices can consume the difference between DPUResources and<br />SystemReservedResources. This field must not be specified if dpuResources are not specified. |  |  |


#### DPUList



DPUList contains a list of DPU





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPU](#dpu) array_ |  |  |  |


#### DPUPhase

_Underlying type:_ _string_

DPUPhase describes current state of DPU.
Only one of the following state may be specified.
Default is Initializing.

_Validation:_
- Enum: [Initializing Node Effect Pending DMS Deployment OS Installing DPU Cluster Config Host Network Configuration Ready Error Deleting Rebooting]

_Appears in:_
- [DPUSetStatus](#dpusetstatus)
- [DPUStatus](#dpustatus)

| Field | Description |
| --- | --- |
| `Initializing` | DPUInitializing is the first phase after the DPU is created.<br /> |
| `Node Effect` | DPUNodeEffect means the controller will handle the node effect provided by the user.<br /> |
| `Pending` | DPUPending means the controller is waiting for the BFB to be ready.<br /> |
| `DMS Deployment` | DPUDMSDeployment means the controller will create the DMS pod and proxy pod.<br /> |
| `OS Installing` | DPUOSInstalling means the controller will provision the DPU through the DMS gNOI interface.<br /> |
| `DPU Cluster Config` | DPUClusterConfig  means the node configuration and Kubernetes Node join procedure are in progress .<br /> |
| `Host Network Configuration` | DPUHostNetworkConfiguration means the host network configuration is running.<br /> |
| `Ready` | DPUReady means the DPU is ready to use.<br /> |
| `Error` | DPUError means error occurred.<br /> |
| `Deleting` | DPUDeleting means the DPU CR will be deleted, controller will do some cleanup works.<br /> |
| `Rebooting` | DPURebooting means the host of DPU is rebooting.<br /> |


#### DPUSet



DPUSet is the Schema for the dpusets API



_Appears in:_
- [DPUSetList](#dpusetlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUSet` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUSetSpec](#dpusetspec)_ |  |  |  |
| `status` _[DPUSetStatus](#dpusetstatus)_ |  |  |  |


#### DPUSetList



DPUSetList contains a list of DPUSet





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUSetList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUSet](#dpuset) array_ |  |  |  |


#### DPUSetSpec



DPUSetSpec defines the desired state of DPUSet



_Appears in:_
- [DPUSet](#dpuset)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `strategy` _[DPUSetStrategy](#dpusetstrategy)_ | The rolling update strategy to use to updating existing DPUs with new ones. |  |  |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | Select the Nodes with specific labels |  |  |
| `dpuSelector` _object (keys:string, values:string)_ | Select the DPU with specific labels |  |  |
| `dpuTemplate` _[DPUTemplate](#dputemplate)_ | Object that describes the DPU that will be created if insufficient replicas are detected |  |  |


#### DPUSetStatus



DPUSetStatus defines the observed state of DPUSet



_Appears in:_
- [DPUSet](#dpuset)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuStatistics` _object (keys:[DPUPhase](#dpuphase), values:integer)_ | DPUStatistics is a map of DPUPhase to the number of DPUs in that phase. |  |  |


#### DPUSetStrategy







_Appears in:_
- [DPUSetSpec](#dpusetspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[StrategyType](#strategytype)_ | Can be "Recreate" or "RollingUpdate". | Recreate | Enum: [Recreate RollingUpdate] <br /> |
| `rollingUpdate` _[RollingUpdateDPU](#rollingupdatedpu)_ | Rolling update config params. Present only if StrategyType = RollingUpdate. |  |  |


#### DPUSpec



DPUSpec defines the desired state of DPU



_Appears in:_
- [DPU](#dpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeName` _string_ | Specifies Node this DPU belongs to |  |  |
| `bfb` _string_ | Specifies name of the bfb CR to use for this DPU |  |  |
| `pciAddress` _string_ | The PCI device related DPU |  |  |
| `nodeEffect` _[NodeEffect](#nodeeffect)_ | Specifies how changes to the DPU should affect the Node | \{ drain:map[automaticNodeReboot:true] \} |  |
| `cluster` _[K8sCluster](#k8scluster)_ | Specifies details on the K8S cluster to join |  |  |
| `dpuFlavor` _string_ | DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU. |  |  |
| `automaticNodeReboot` _boolean_ | Specifies if the DPU controller should automatically reboot the node on upgrades,<br />this field is intended for advanced cases that don’t use draining but want to reboot the host based with custom logic | true |  |


#### DPUStatus



DPUStatus defines the observed state of DPU



_Appears in:_
- [DPU](#dpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[DPUPhase](#dpuphase)_ | The current state of DPU. | Initializing | Enum: [Initializing Node Effect Pending DMS Deployment OS Installing DPU Cluster Config Host Network Configuration Ready Error Deleting Rebooting] <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ |  |  |  |
| `bfbVersion` _string_ | bfb version of this DPU |  |  |
| `pciDevice` _string_ | pci device information of this DPU |  |  |
| `requiredReset` _boolean_ | whether require reset of DPU |  |  |
| `firmware` _[Firmware](#firmware)_ | the firmware information of DPU |  |  |


#### DPUTemplate



DPUTemplate is a template for DPU



_Appears in:_
- [DPUSetSpec](#dpusetspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `annotations` _object (keys:string, values:string)_ | Annotations specifies annotations which are added to the DPU. |  |  |
| `spec` _[DPUTemplateSpec](#dputemplatespec)_ | Spec specifies the DPU specification. |  |  |


#### DPUTemplateSpec







_Appears in:_
- [DPUTemplate](#dputemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bfb` _[BFBReference](#bfbreference)_ | Specifies a BFB CR |  |  |
| `nodeEffect` _[NodeEffect](#nodeeffect)_ | Specifies how changes to the DPU should affect the Node | \{ drain:map[automaticNodeReboot:true] \} |  |
| `cluster` _[ClusterSpec](#clusterspec)_ | Specifies details on the K8S cluster to join |  |  |
| `dpuFlavor` _string_ | DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU. |  |  |
| `automaticNodeReboot` _boolean_ | Specifies if the DPU controller should automatically reboot the node on upgrades,<br />this field is intended for advanced cases that don’t use draining but want to reboot the host based with custom logic | true |  |


#### Drain



Drain the K8s host node by NodeMaintenance operator



_Appears in:_
- [NodeEffect](#nodeeffect)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `automaticNodeReboot` _boolean_ | Specifies if the DPU controller should automatically reboot the node on upgrades | true |  |


#### Firmware







_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bmc` _string_ | BMC is the used BMC firmware version |  |  |
| `nic` _string_ | NIC is the used NIC firmware version |  |  |
| `uefi` _string_ | UEFI is the used UEFI firmware version |  |  |


#### K8sCluster







_Appears in:_
- [DPUSpec](#dpuspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the DPUs Kubernetes cluster |  |  |
| `namespace` _string_ | Namespace is the tenants namespace name where the Kubernetes cluster will be deployed |  |  |
| `nodeLabels` _object (keys:string, values:string)_ | NodeLabels define the labels that will be added to the nodes. |  |  |


#### KeepalivedSpec







_Appears in:_
- [ClusterEndpointSpec](#clusterendpointspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vip` _string_ | VIP is the virtual IP owned by the keepalived instances |  |  |
| `virtualRouterID` _integer_ | VirtualRouterID is the virtual_router_id in keepalived.conf |  | Maximum: 255 <br />Minimum: 1 <br /> |
| `interface` _string_ | Interface specifies on which interface the VIP should be assigned |  | MinLength: 1 <br /> |
| `nodeSelector` _object (keys:string, values:string)_ | NodeSelector is used to specify a subnet of control plane nodes to deploy keepalived instances.<br />Note: keepalived instances are always deployed on control plane nodes |  |  |


#### NodeEffect







_Appears in:_
- [DPUSpec](#dpuspec)
- [DPUTemplateSpec](#dputemplatespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `taint` _[Taint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#taint-v1-core)_ | Add specify taint on the DPU node |  |  |
| `noEffect` _boolean_ | Do not do any action on the DPU node |  |  |
| `customLabel` _object (keys:string, values:string)_ | Add specify labels on the DPU node |  |  |
| `drain` _[Drain](#drain)_ | Drain the K8s host node by NodeMaintenance operator |  |  |


#### RollingUpdateDPU



RollingUpdateDPU is the rolling update strategy for a DPUSet.



_Appears in:_
- [DPUSetStrategy](#dpusetstrategy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `maxUnavailable` _[IntOrString](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#intorstring-intstr-util)_ | MaxUnavailable is the maximum number of DPUs that can be unavailable during the update. |  |  |


#### StrategyType

_Underlying type:_ _string_

StrategyType describes strategy to use to reprovision existing DPUs.
Default is "Recreate".

_Validation:_
- Enum: [Recreate RollingUpdate]

_Appears in:_
- [DPUSetStrategy](#dpusetstrategy)

| Field | Description |
| --- | --- |
| `Recreate` | Delete all the existing DPUs before creating new ones.<br /> |
| `RollingUpdate` | Gradually scale down the old DPUs and scale up the new one.<br /> |



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
- [DPUServiceTemplate](#dpuservicetemplate)
- [DPUServiceTemplateList](#dpuservicetemplatelist)
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
| `repoURL` _string_ | RepoURL specifies the URL to the repository that contains the application Helm chart.<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |
| `path` _string_ | Path is the location of the chart inside the repo. |  |  |
| `version` _string_ | Version is a semver tag for the Chart's version. |  | MinLength: 1 <br /> |
| `chart` _string_ | Chart is the name of the helm chart. |  |  |
| `releaseName` _string_ | ReleaseName is the name to give to the release generate from the DPUService. |  |  |


#### DPUDeployment



DPUDeployment is the Schema for the dpudeployments API. This object connects DPUServices with specific BFBs and
DPUServiceChains.



_Appears in:_
- [DPUDeploymentList](#dpudeploymentlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDeployment` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUDeploymentSpec](#dpudeploymentspec)_ |  |  |  |
| `status` _[DPUDeploymentStatus](#dpudeploymentstatus)_ |  |  |  |


#### DPUDeploymentList



DPUDeploymentList contains a list of DPUDeployment





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDeploymentList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUDeployment](#dpudeployment) array_ |  |  |  |


#### DPUDeploymentPort



DPUDeploymentPort defines how a port can be configured



_Appears in:_
- [DPUDeploymentSwitch](#dpudeploymentswitch)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `service` _[DPUDeploymentService](#dpudeploymentservice)_ | Service holds configuration that helps configure the Service Function Chain and identify a port associated with<br />a DPUService |  |  |
| `serviceInterface` _[ServiceIfc](#serviceifc)_ | ServiceInterface holds configuration that helps configure the Service Function Chain and identify a user defined<br />port |  |  |


#### DPUDeploymentService



DPUDeploymentService is the struct used for referencing an interface.



_Appears in:_
- [DPUDeploymentPort](#dpudeploymentport)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the service as defined in the DPUDeployment Spec |  |  |
| `interface` _string_ | Interface name is the name of the interface as defined in the DPUServiceTemplate |  |  |
| `ipam` _[IPAM](#ipam)_ | IPAM defines the IPAM configuration that is configured in the Service Function Chain |  |  |


#### DPUDeploymentServiceConfiguration



DPUDeploymentServiceConfiguration describes the configuration of a particular Service



_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceTemplate` _string_ | ServiceTemplate is the name of the DPUServiceTemplate object to be used for this Service. It must be in the same<br />namespace as the DPUDeployment. |  |  |
| `serviceConfiguration` _string_ | ServiceConfiguration is the name of the DPUServiceConfiguration object to be used for this Service. It must be<br />in the same namespace as the DPUDeployment. |  |  |


#### DPUDeploymentSpec



DPUDeploymentSpec defines the desired state of DPUDeployment



_Appears in:_
- [DPUDeployment](#dpudeployment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpus` _[DPUs](#dpus)_ | DPUs contains the DPU related configuration |  |  |
| `services` _object (keys:string, values:[DPUDeploymentServiceConfiguration](#dpudeploymentserviceconfiguration))_ | Services contains the DPUDeploymentService related configuration. The key is the deploymentServiceName and the value is its<br />configuration. All underlying objects must specify the same deploymentServiceName in order to be able to be consumed by the<br />DPUDeployment. |  |  |
| `serviceChains` _[DPUDeploymentSwitch](#dpudeploymentswitch) array_ | ServiceChains contains the configuration related to the DPUServiceChains that the DPUDeployment creates. |  | MaxItems: 50 <br />MinItems: 1 <br /> |


#### DPUDeploymentStatus



DPUDeploymentStatus defines the observed state of DPUDeployment



_Appears in:_
- [DPUDeployment](#dpudeployment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUDeploymentSwitch



DPUDeploymentSwitch holds the ports that are connected in switch topology



_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ports` _[DPUDeploymentPort](#dpudeploymentport) array_ | Ports contains the ports of the switch |  | MaxItems: 50 <br />MinItems: 1 <br /> |


#### DPUService



DPUService is the Schema for the dpuservices API



_Appears in:_
- [DPUServiceList](#dpuservicelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUService` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
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
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceChainSpec](#dpuservicechainspec)_ |  |  |  |
| `status` _[DPUServiceChainStatus](#dpuservicechainstatus)_ |  |  |  |


#### DPUServiceChainList



DPUServiceChainList contains a list of DPUServiceChain





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceChainList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceChain](#dpuservicechain) array_ |  |  |  |


#### DPUServiceChainSpec



DPUServiceChainSpec defines the desired state of DPUServiceChainSpec



_Appears in:_
- [DPUServiceChain](#dpuservicechain)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | Select the Clusters with specific labels, ServiceChainSet CRs will be created only for these Clusters |  |  |
| `template` _[ServiceChainSetSpecTemplate](#servicechainsetspectemplate)_ | Template describes the ServiceChainSet that will be created for each selected Cluster. |  |  |


#### DPUServiceChainStatus



DPUServiceChainStatus defines the observed state of DPUServiceChain



_Appears in:_
- [DPUServiceChain](#dpuservicechain)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
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
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)_ |  |  |  |
| `status` _[DPUServiceConfigurationStatus](#dpuserviceconfigurationstatus)_ |  |  |  |


#### DPUServiceConfigurationList



DPUServiceConfigurationList contains a list of DPUServiceConfiguration





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceConfigurationList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceConfiguration](#dpuserviceconfiguration) array_ |  |  |  |


#### DPUServiceConfigurationServiceDaemonSetValues



DPUServiceConfigurationServiceDaemonSetValues reflects the Helm related configuration



_Appears in:_
- [ServiceConfiguration](#serviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `labels` _object (keys:string, values:string)_ | Labels specifies labels which are added to the ServiceDaemonSet. |  |  |
| `annotations` _object (keys:string, values:string)_ | Annotations specifies annotations which are added to the ServiceDaemonSet. |  |  |


#### DPUServiceConfigurationSpec



DPUServiceConfigurationSpec defines the desired state of DPUServiceConfiguration



_Appears in:_
- [DPUServiceConfiguration](#dpuserviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `deploymentServiceName` _string_ | DeploymentServiceName is the name of the DPU service this configuration refers to. It must match<br />.spec.deploymentServiceName of a DPUServiceTemplate object and one of the keys in .spec.services of a<br />DPUDeployment object. |  |  |
| `serviceConfiguration` _[ServiceConfiguration](#serviceconfiguration)_ | ServiceConfiguration contains fields that are configured on the generated DPUService. |  |  |
| `interfaces` _[ServiceInterfaceTemplate](#serviceinterfacetemplate) array_ | Interfaces specifies the DPUServiceInterface to be generated for the generated DPUService. |  | MaxItems: 50 <br />MinItems: 1 <br /> |


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
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceCredentialRequestSpec](#dpuservicecredentialrequestspec)_ |  |  |  |
| `status` _[DPUServiceCredentialRequestStatus](#dpuservicecredentialrequeststatus)_ |  |  |  |


#### DPUServiceCredentialRequestList



DPUServiceCredentialRequestList contains a list of DPUServiceCredentialRequest





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceCredentialRequestList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceCredentialRequest](#dpuservicecredentialrequest) array_ |  |  |  |


#### DPUServiceCredentialRequestSpec



DPUServiceCredentialRequestSpec defines the desired state of DPUServiceCredentialRequest



_Appears in:_
- [DPUServiceCredentialRequest](#dpuservicecredentialrequest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceAccount` _[NamespacedName](#namespacedname)_ | ServiceAccount defines the needed information to create the service account. |  |  |
| `duration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#duration-v1-meta)_ | Duration is the duration for which the token will be valid.<br />Value must be in units accepted by Go time.ParseDuration https://golang.org/pkg/time/#ParseDuration.<br />e.g. "1h", "1m", "1s", "1ms", "1.5h", "2h45m".<br />Value duration must not be less than 10 minutes.<br />**Note:** The maximum TTL for a token is 24 hours, after which the token<br />will be rotated. |  | Pattern: `^([0-9]+(\.[0-9]+)?(ms\|s\|m\|h))+$` <br />Type: string <br /> |
| `targetCluster` _[NamespacedName](#namespacedname)_ | TargetCluster defines the target cluster where the service account will<br />be created, and where a token for that service account will be requested.<br />If not provided, the token will be requested for the same cluster where<br />the DPUServiceCredentialRequest object is created. |  |  |
| `type` _string_ | Type is the type of the secret that will be created.<br />The supported types are `kubeconfig` and `tokenFile`.<br />If `kubeconfig` is selected, the secret will contain a kubeconfig file,<br />that can be used to access the cluster.<br />If `tokenFile` is selected, the secret will contain a token file and serveral<br />environment variables that can be used to access the cluster. It can be used<br />with https://github.com/kubernetes/client-go/blob/v11.0.0/rest/config.go#L52<br />to create a client that will hanle file rotation. |  | Enum: [kubeconfig tokenFile] <br /> |
| `secret` _[NamespacedName](#namespacedname)_ | Secret defines the needed information to create the secret.<br />The secret will be of the type specified in the `spec.type` field. |  |  |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |


#### DPUServiceCredentialRequestStatus



DPUServiceCredentialRequestStatus defines the observed state of DPUServiceCredentialRequest



_Appears in:_
- [DPUServiceCredentialRequest](#dpuservicecredentialrequest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions defines current service state. |  |  |
| `serviceAccount` _string_ | ServiceAccount is the namespaced name of the ServiceAccount resource created by<br />the controller for the DPUServiceCredentialRequest. |  |  |
| `targetCluster` _string_ | TargetCluster is the cluster where the service account was created.<br />It has to be persisted in the status to be able to delete the service account<br />when the DPUServiceCredentialRequest is updated. |  |  |
| `expirationTimestamp` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | ExpirationTimestamp is the time when the token will expire. |  |  |
| `issuedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | IssuedAt is the time when the token was issued. |  |  |
| `secret` _string_ | Sercet is the namespaced name of the Secret resource created by the controller for<br />the DPUServiceCredentialRequest. |  |  |


#### DPUServiceIPAM



DPUServiceIPAM is the Schema for the dpuserviceipams API



_Appears in:_
- [DPUServiceIPAMList](#dpuserviceipamlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceIPAM` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceIPAMSpec](#dpuserviceipamspec)_ |  |  |  |
| `status` _[DPUServiceIPAMStatus](#dpuserviceipamstatus)_ |  |  |  |


#### DPUServiceIPAMList



DPUServiceIPAMList contains a list of DPUServiceIPAM





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceIPAMList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
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
| `clusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | ClusterSelector determines in which clusters the DPUServiceIPAM controller should apply the configuration. |  |  |
| `nodeSelector` _[NodeSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#nodeselector-v1-core)_ | NodeSelector determines in which DPU nodes the DPUServiceIPAM controller should apply the configuration. |  |  |


#### DPUServiceIPAMStatus



DPUServiceIPAMStatus defines the observed state of DPUServiceIPAM



_Appears in:_
- [DPUServiceIPAM](#dpuserviceipam)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUServiceInterface



DPUServiceInterface is the Schema for the DPUServiceInterface API



_Appears in:_
- [DPUServiceInterfaceList](#dpuserviceinterfacelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceInterface` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceInterfaceSpec](#dpuserviceinterfacespec)_ |  |  |  |
| `status` _[DPUServiceInterfaceStatus](#dpuserviceinterfacestatus)_ |  |  |  |


#### DPUServiceInterfaceList



DPUServiceInterfaceList contains a list of DPUServiceInterface





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceInterfaceList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceInterface](#dpuserviceinterface) array_ |  |  |  |


#### DPUServiceInterfaceSpec



DPUServiceInterfaceSpec defines the desired state of DPUServiceInterfaceSpec



_Appears in:_
- [DPUServiceInterface](#dpuserviceinterface)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | Select the Clusters with specific labels, ServiceInterfaceSet CRs will be created only for these Clusters |  |  |
| `template` _[ServiceInterfaceSetSpecTemplate](#serviceinterfacesetspectemplate)_ | Template describes the ServiceInterfaceSet that will be created for each selected Cluster. |  |  |


#### DPUServiceInterfaceStatus



DPUServiceInterfaceStatus defines the observed state of DPUServiceInterface



_Appears in:_
- [DPUServiceInterface](#dpuserviceinterface)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions defines current service state. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUServiceList



DPUServiceList contains a list of DPUService





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUService](#dpuservice) array_ |  |  |  |


#### DPUServiceSpec



DPUServiceSpec defines the desired state of DPUService



_Appears in:_
- [DPUService](#dpuservice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart reflects the Helm related configuration |  |  |
| `serviceID` _string_ | ServiceID is the ID of the service that the DPUService is associated with. |  |  |
| `serviceDaemonSet` _[ServiceDaemonSetValues](#servicedaemonsetvalues)_ | ServiceDaemonSet specifies the configuration for the ServiceDaemonSet. |  |  |
| `deployInCluster` _boolean_ | DeployInCluster indicates if the DPUService Helm Chart will be deployed on<br />the Host cluster. Default to false. |  |  |
| `interfaces` _string array_ | Interfaces specifies the DPUServiceInterface names that the DPUService<br />uses in the same namespace. |  | MaxItems: 50 <br />MinItems: 1 <br /> |
| `paused` _boolean_ | Paused indicates that the DPUService is paused.<br />Underlying resources are also paused when this is set to true.<br />No deletion of resources will occur when this is set to true. |  |  |


#### DPUServiceStatus



DPUServiceStatus defines the observed state of DPUService



_Appears in:_
- [DPUService](#dpuservice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions defines current service state. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


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
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceTemplateSpec](#dpuservicetemplatespec)_ |  |  |  |
| `status` _[DPUServiceTemplateStatus](#dpuservicetemplatestatus)_ |  |  |  |


#### DPUServiceTemplateList



DPUServiceTemplateList contains a list of DPUServiceTemplate





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceTemplateList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceTemplate](#dpuservicetemplate) array_ |  |  |  |


#### DPUServiceTemplateSpec



DPUServiceTemplateSpec defines the desired state of DPUServiceTemplate



_Appears in:_
- [DPUServiceTemplate](#dpuservicetemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `deploymentServiceName` _string_ | DeploymentServiceName is the name of the DPU service this configuration refers to. It must match<br />.spec.deploymentServiceName of a DPUServiceConfiguration object and one of the keys in .spec.services of a<br />DPUDeployment object. |  |  |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart reflects the Helm related configuration. The user is supposed to configure the values that are static<br />across any DPUServiceConfiguration used with this DPUServiceTemplate in a DPUDeployment. These values act as a<br />baseline and are merged with values specified in the DPUServiceConfiguration. In case of conflict, the<br />DPUServiceConfiguration values take precedence. |  |  |
| `resourceRequirements` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#resourcelist-v1-core)_ | ResourceRequirements contains the overall resources required by this particular service to run on a single node |  |  |


#### DPUServiceTemplateStatus



DPUServiceTemplateStatus defines the observed state of DPUServiceTemplate



_Appears in:_
- [DPUServiceTemplate](#dpuservicetemplate)



#### DPUSet



DPUSet contains configuration for the DPUSet to be created by the DPUDeployment



_Appears in:_
- [DPUs](#dpus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nameSuffix` _string_ | NameSuffix is the suffix to be added to the name of the DPUSet object created by the DPUDeployment. |  | MaxLength: 63 <br />MinLength: 1 <br /> |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | NodeSelector defines the nodes that the DPUSet should target |  |  |
| `dpuSelector` _object (keys:string, values:string)_ | DPUSelector defines the DPUs that the DPUSet should target |  |  |
| `dpuAnnotations` _object (keys:string, values:string)_ | DPUAnnotations is the annotations to be added to the DPU object created by the DPUSet. |  |  |


#### DPUs



DPUs contains the DPU related configuration



_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bfb` _string_ | BFB is the name of the BFB object to be used in this DPUDeployment. It must be in the same namespace as the<br />DPUDeployment. |  |  |
| `flavor` _string_ | Flavor is the name of the DPUFlavor object to be used in this DPUDeployment. It must be in the same namespace as<br />the DPUDeployment. |  |  |
| `dpuSets` _[DPUSet](#dpuset) array_ | DPUSets contains configuration for each DPUSet that is going to be created by the DPUDeployment |  | MaxItems: 50 <br />MinItems: 1 <br /> |


#### HelmChart



HelmChart reflects the helm related configuration



_Appears in:_
- [DPUServiceSpec](#dpuservicespec)
- [DPUServiceTemplateSpec](#dpuservicetemplatespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `source` _[ApplicationSource](#applicationsource)_ | Source specifies information about the Helm chart |  |  |
| `values` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#rawextension-runtime-pkg)_ | Values specifies Helm values to be passed to Helm template, defined as a map. This takes precedence over Values. |  |  |


#### IPAM



IPAM defines the IPAM configuration



_Appears in:_
- [DPUDeploymentService](#dpudeploymentservice)
- [ServiceIfc](#serviceifc)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `matchLabels` _object (keys:string, values:string)_ | Labels matching service IPAM |  | MaxProperties: 50 <br />MinProperties: 1 <br /> |
| `defaultGateway` _boolean_ | DefaultGateway adds gateway as default gateway in the routes list if true. |  |  |
| `setDefaultRoute` _boolean_ | SetDefaultRoute adds a default route to the routing table if true. |  |  |


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
| `exclusions` _string array_ | Exclusions is a list of IPs that should be excluded when splitting the CIDR into subnets per node. |  |  |
| `allocations` _object (keys:string, values:string)_ | Allocations describes the subnets that should be assigned in each DPU node. |  |  |
| `defaultGateway` _boolean_ | DefaultGateway adds gateway as default gateway in the routes list if true. |  |  |
| `routes` _[Route](#route) array_ | Routes is the static routes list using the gateway specified in the spec. |  |  |


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
| `defaultGateway` _boolean_ | if true, add gateway as default gateway in the routes list<br />DefaultGateway adds gateway as default gateway in the routes list if true. |  |  |
| `routes` _[Route](#route) array_ | Routes is the static routes list using the gateway specified in the spec. |  |  |


#### NamespacedName



NamespacedName contains enough information to locate the referenced Kubernetes resource object in any
namespace.



_Appears in:_
- [DPUServiceCredentialRequestSpec](#dpuservicecredentialrequestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name of the object. |  |  |
| `namespace` _string_ | Namespace of the object, if not provided the object will be looked up in<br />the same namespace as the referring object |  |  |


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
| `labels` _object (keys:string, values:string)_ | Labels is a map of string keys and values. |  |  |
| `annotations` _object (keys:string, values:string)_ | Annotations is a map of string keys and values. |  |  |


#### PF



PF defines the PF configuration



_Appears in:_
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pfID` _integer_ | The PF ID |  |  |


#### Physical



Physical Identifies a physical interface



_Appears in:_
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `interfaceName` _string_ | The interface name |  |  |


#### Port



Port defines the port configuration



_Appears in:_
- [Switch](#switch)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceInterface` _[ServiceIfc](#serviceifc)_ |  |  |  |


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
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ServiceChainSpec](#servicechainspec)_ |  |  |  |
| `status` _[ServiceChainStatus](#servicechainstatus)_ |  |  |  |


#### ServiceChainList



ServiceChainList contains a list of ServiceChain





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceChainList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ServiceChain](#servicechain) array_ |  |  |  |


#### ServiceChainSet



ServiceChainSet is the Schema for the servicechainsets API



_Appears in:_
- [ServiceChainSetList](#servicechainsetlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceChainSet` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ServiceChainSetSpec](#servicechainsetspec)_ |  |  |  |
| `status` _[ServiceChainSetStatus](#servicechainsetstatus)_ |  |  |  |


#### ServiceChainSetList



ServiceChainSetList contains a list of ServiceChainSet





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceChainSetList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ServiceChainSet](#servicechainset) array_ |  |  |  |


#### ServiceChainSetSpec



ServiceChainSetSpec defines the desired state of ServiceChainSet



_Appears in:_
- [ServiceChainSet](#servicechainset)
- [ServiceChainSetSpecTemplate](#servicechainsetspectemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | Select the Nodes with specific labels, ServiceChain CRs will be created<br />only for these Nodes |  |  |
| `template` _[ServiceChainSpecTemplate](#servicechainspectemplate)_ | ServiceChainSpecTemplate holds the template for the ServiceChainSpec |  |  |


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
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
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
| `node` _string_ | Node where this ServiceChain applies to |  |  |
| `switches` _[Switch](#switch) array_ | The switches of the ServiceChain, order is significant |  | MaxItems: 50 <br />MinItems: 1 <br /> |


#### ServiceChainSpecTemplate



ServiceChainSpecTemplate defines the template from which ServiceChainSpecs
are created



_Appears in:_
- [ServiceChainSetSpec](#servicechainsetspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `spec` _[ServiceChainSpec](#servicechainspec)_ | ServiceChainSpec is the spec for the ServiceChainSpec |  |  |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |


#### ServiceChainStatus



ServiceChainStatus defines the observed state of ServiceChain



_Appears in:_
- [ServiceChain](#servicechain)



#### ServiceConfiguration



ServiceConfiguration contains fields that are configured on the generated DPUService.



_Appears in:_
- [DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `helmChart` _[ServiceConfigurationHelmChart](#serviceconfigurationhelmchart)_ | HelmChart reflects the Helm related configuration. The user is supposed to configure values specific to that<br />DPUServiceConfiguration used in a DPUDeployment and should not specify values that could be shared across multiple<br />DPUDeployments using different DPUServiceConfigurations. These values are merged with values specified in the<br />DPUServiceTemplate. In case of conflict, the DPUServiceConfiguration values take precedence. |  |  |
| `serviceDaemonSet` _[DPUServiceConfigurationServiceDaemonSetValues](#dpuserviceconfigurationservicedaemonsetvalues)_ | ServiceDaemonSet contains settings related to the underlying DaemonSet that is part of the Helm chart |  |  |
| `deployInCluster` _boolean_ | TODO: Add nodeEffect<br />DeployInCluster indicates if the DPUService Helm Chart will be deployed on the Host cluster. Default to false. |  |  |


#### ServiceConfigurationHelmChart



ServiceConfigurationHelmChart reflects the helm related configuration



_Appears in:_
- [ServiceConfiguration](#serviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `values` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#rawextension-runtime-pkg)_ | Values specifies Helm values to be passed to Helm template, defined as a map. This takes precedence over Values. |  |  |


#### ServiceDaemonSetValues



ServiceDaemonSetValues specifies the configuration for the ServiceDaemonSet.



_Appears in:_
- [DPUServiceSpec](#dpuservicespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[NodeSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#nodeselector-v1-core)_ | NodeSelector specifies which Nodes to deploy the ServiceDaemonSet to. |  |  |
| `updateStrategy` _[DaemonSetUpdateStrategy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#daemonsetupdatestrategy-v1-apps)_ | UpdateStrategy specifies the DeaemonSet update strategy for the ServiceDaemonset. |  |  |
| `labels` _object (keys:string, values:string)_ | Labels specifies labels which are added to the ServiceDaemonSet. |  |  |
| `annotations` _object (keys:string, values:string)_ | Annotations specifies annotations which are added to the ServiceDaemonSet. |  |  |


#### ServiceDef



ServiceDef Identifes the service and network for the ServiceInterface



_Appears in:_
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceID` _string_ | ServiceID is the DPU Service Identifier |  |  |
| `network` _string_ | Network is the Network Attachment Definition in the form of "namespace/name"<br />or just "name" if the namespace is the same as the ServiceInterface. |  |  |
| `interfaceName` _string_ | The interface name |  |  |


#### ServiceIfc



ServiceIfc defines the service interface configuration



_Appears in:_
- [DPUDeploymentPort](#dpudeploymentport)
- [Port](#port)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `matchLabels` _object (keys:string, values:string)_ | Labels matching service interface |  | MaxProperties: 50 <br />MinProperties: 1 <br /> |
| `ipam` _[IPAM](#ipam)_ | IPAM defines the IPAM configuration when referencing a serviceInterface of type 'service' |  |  |


#### ServiceInterface



ServiceInterface is the Schema for the serviceinterfaces API



_Appears in:_
- [ServiceInterfaceList](#serviceinterfacelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceInterface` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ServiceInterfaceSpec](#serviceinterfacespec)_ |  |  |  |
| `status` _[ServiceInterfaceStatus](#serviceinterfacestatus)_ |  |  |  |


#### ServiceInterfaceList



ServiceInterfaceList contains a list of ServiceInterface





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceInterfaceList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ServiceInterface](#serviceinterface) array_ |  |  |  |


#### ServiceInterfaceSet



ServiceInterfaceSet is the Schema for the serviceinterfacesets API



_Appears in:_
- [ServiceInterfaceSetList](#serviceinterfacesetlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceInterfaceSet` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ServiceInterfaceSetSpec](#serviceinterfacesetspec)_ |  |  |  |
| `status` _[ServiceInterfaceSetStatus](#serviceinterfacesetstatus)_ |  |  |  |


#### ServiceInterfaceSetList



ServiceInterfaceSetList contains a list of ServiceInterfaceSet





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `ServiceInterfaceSetList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[ServiceInterfaceSet](#serviceinterfaceset) array_ |  |  |  |


#### ServiceInterfaceSetSpec



ServiceInterfaceSetSpec defines the desired state of ServiceInterfaceSet



_Appears in:_
- [ServiceInterfaceSet](#serviceinterfaceset)
- [ServiceInterfaceSetSpecTemplate](#serviceinterfacesetspectemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | Select the Nodes with specific labels, ServiceInterface CRs will be<br />created only for these Nodes |  |  |
| `template` _[ServiceInterfaceSpecTemplate](#serviceinterfacespectemplate)_ | Template holds the template for the erviceInterfaceSpec |  |  |


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
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
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
| `node` _string_ | Node where this interface exists |  |  |
| `interfaceType` _string_ | The interface type ("vlan", "physical", "pf", "vf", "ovn", "service") |  | Enum: [vlan physical pf vf ovn service] <br /> |
| `physical` _[Physical](#physical)_ | The physical interface definition |  |  |
| `vlan` _[VLAN](#vlan)_ | The VLAN definition |  |  |
| `vf` _[VF](#vf)_ | The VF definition |  |  |
| `pf` _[PF](#pf)_ | The PF definition |  |  |
| `service` _[ServiceDef](#servicedef)_ | The Service definition |  |  |


#### ServiceInterfaceSpecTemplate



ServiceInterfaceSpecTemplate defines the template from which ServiceInterfaceSpecs
are created



_Appears in:_
- [ServiceInterfaceSetSpec](#serviceinterfacesetspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `spec` _[ServiceInterfaceSpec](#serviceinterfacespec)_ | ServiceInterfaceSpec is the spec for the ServiceInterfaceSpec |  |  |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |


#### ServiceInterfaceStatus



ServiceInterfaceStatus defines the observed state of ServiceInterface



_Appears in:_
- [ServiceInterface](#serviceinterface)



#### ServiceInterfaceTemplate



ServiceInterfaceTemplate contains the information related to an interface of the DPUService



_Appears in:_
- [DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the interface |  |  |
| `network` _string_ | Network is the Network Attachment Definition in the form of "namespace/name"<br />or just "name" if the namespace is the same as the namespace the pod is running. |  |  |


#### Switch



Switch defines the switch configuration



_Appears in:_
- [ServiceChainSpec](#servicechainspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ports` _[Port](#port) array_ | Ports of the switch |  | MaxItems: 50 <br />MinItems: 1 <br /> |


#### VF



VF defines the VF configuration



_Appears in:_
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vfID` _integer_ | The VF ID |  |  |
| `pfID` _integer_ | The PF ID |  |  |
| `parentInterfaceRef` _string_ | The parent interface reference<br />TODO: Figure out what this field is supposed to be |  |  |


#### VLAN



VLAN defines the VLAN configuration



_Appears in:_
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vlanID` _integer_ | The VLAN ID |  |  |
| `parentInterfaceRef` _string_ | The parent interface reference<br />TODO: Figure out what this field is supposed to be |  |  |


