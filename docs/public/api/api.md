---
title: API reference
---

# API Reference

## Packages
- [operator.dpu.nvidia.com/v1alpha1](#operatordpunvidiacomv1alpha1)
- [provisioning.dpu.nvidia.com/v1alpha1](#provisioningdpunvidiacomv1alpha1)
- [storage.dpu.nvidia.com/v1alpha1](#storagedpunvidiacomv1alpha1)
- [svc.dpu.nvidia.com/v1alpha1](#svcdpunvidiacomv1alpha1)
- [vpc.dpu.nvidia.com/v1alpha1](#vpcdpunvidiacomv1alpha1)


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
| `disable` _boolean_ | Disable ensures the BFB Registry is not deployed when set to true. |  |  |
| `port` _integer_ | Port is the port on which the BFB Registry will listen |  |  |




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


#### DPFOperatorConfigStatus



DPFOperatorConfigStatus defines the observed state of DPFOperatorConfig



_Appears in:_
- [DPFOperatorConfig](#dpfoperatorconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions exposes the current state of the OperatorConfig. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |
| `version` _string_ | Version is the version of the DPF Operator that is currently deployed. |  |  |


#### DPUDetectorConfiguration



DPUDetectorConfiguration is the configuration for the DPUDetector Component.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the component. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |




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
| `image` _[FlannelImages](#flannelimages)_ | Images overrides the container images used by flannel |  |  |
| `podCIDR` _string_ | PodCIDR is the pod cidr for flannel. |  |  |
| `deployInTargetCluster` _boolean_ | DeployInCluster deploys the component in the same cluster as the DPF Operator if set to true. |  |  |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the Flannel<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### FlannelImages







_Appears in:_
- [FlannelConfiguration](#flannelconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `flannelCNI` _string_ | FlannelCNI must be set if FlannelImages is set. |  | MinLength: 1 <br /> |
| `kubeFlannel` _string_ | KubeFlannel must be set if FlannelImages is set. |  | MinLength: 1 <br /> |


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
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)
- [SFCControllerConfiguration](#sfccontrollerconfiguration)
- [SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)
- [ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)
- [StaticClusterManagerConfiguration](#staticclustermanagerconfiguration)





#### InstallViaGNOI



InstallViaGNOI is the interface used to install the BFB via GNOI



_Appears in:_
- [ProvisioningInstallInterface](#provisioninginstallinterface)



#### InstallViaRedfish



InstallViaRedfish is the interface used to install the BFB via Redfish



_Appears in:_
- [ProvisioningInstallInterface](#provisioninginstallinterface)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bfbRegistryAddress` _string_ | BFBRegistryAddress is the address of the BFB Registry |  | MinLength: 1 <br /> |
| `bfbRegistry` _[BFBRegistryConfiguration](#bfbregistryconfiguration)_ | BFBRegistry is the configuration for the BFB Registry |  |  |


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
| `deployInTargetCluster` _boolean_ | DeployInCluster deploys the component in the same cluster as the DPF Operator if set to true. |  |  |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by Multus<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### NVIPAMConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by NVIPAM |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `deployInTargetCluster` _boolean_ | DeployInCluster deploys the component in the same cluster as the DPF Operator if set to true. |  |  |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by NVIPAM<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### Networking



Networking defines the networking configuration for the system components.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `controlPlaneMTU` _integer_ | ControlPlaneMTU is the MTU value to be set on the management network.<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1000 <br /> |
| `highSpeedMTU` _integer_ | HighSpeedMTU is the MTU value to be set on the high-speed interface.<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1000 <br /> |


#### OVSCNIConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the OVS CNI |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `deployInTargetCluster` _boolean_ | DeployInCluster deploys the component in the same cluster as the DPF Operator if set to true. |  |  |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the OVS CNI<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### Overrides



Overrides exposes a set of fields which impact the recommended behavior of the DPF Operator.
These fields should only be set for advanced use cases. The fields here have no stability guarantees.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `paused` _boolean_ | Paused disables all reconciliation of the DPFOperatorConfig when set to true. |  |  |
| `dpuCNIBinPath` _string_ | DPUCNIBinPath is the path at which the CNI binaries will be installed to on the DPU.<br />This is /opt/cni/bin by default.<br />This setting does not change where kubelet is configured to use the CNI from. |  |  |
| `dpuCNIPath` _string_ | DPUCNIConfigPath is the path to which the CNI config files will be installed on the DPU.<br />This is /etc/cni/net.d by default.<br />This setting does not change where kubelet is configured to read the CNI config from. |  |  |
| `dpuOpenvSwitchRunPath` _string_ | DPUOpenvSwitchPath is the path at which the openvSwitch run directory can be found on the DPU.<br />This is /var/run/openvswitch by default.<br />This setting does not change where components are installed. Installation location fixed in the BFB. |  |  |
| `dpuOpenvSwitchBinPath` _string_ | DPUOpenvSwitchBinPath is the path at which the openvSwitch bin directory can be found on the DPU node.<br />This is /usr/bin/ by default.<br />This setting does not change where components are installed. Installation location fixed in the BFB. |  |  |
| `dpuOpenvSwitchSystemSharedPath` _string_ | DPUOpenvSwitchSystemSharedLibPath is the path at which the system lib used by OVS components can be found on the DPU.<br />This is /lib by default.<br />This setting does not change where components are installed. Installation location fixed in the BFB. |  |  |
| `flannelSkipCNIConfigInstallation` _boolean_ | FlannelSkipCNIConfigInstallation controls whether Flannel should skip CNI config installation.<br />This is true by default, meaning Flannel does not manage its own CNI configuration.<br />Set to false if you want Flannel to install a CNI configuration. |  |  |
| `dpuOpenvSwitchSystemSharedLib64Path` _string_ | DPUOpenvSwitchSystemSharedLib64Path is the path at which the system lib64 used by OVS components can be found on the DPU.<br />If this field is not set, no lib64 volume mount will be configured in the SFC Controller component.<br />This setting does not change where components are installed. Installation location fixed in the BFB. |  | MinLength: 1 <br /> |
| `kubernetesAPIServerVIP` _string_ | KubernetesAPIServerVIP is the VIP the Kubernetes API server is accessible at.<br />This setting enables specific underlying components deployed directly or indirectly by the DPF Operator to reach<br />the Kubernetes API Server when the ClusterIP Kubernetes Service is not functional.<br />If set, it should be set to an IP to ensure that components work even if DNS is not available in the cluster. |  |  |
| `kubernetesAPIServerPort` _integer_ | KubernetesAPIServerPort is the port the Kubernetes API server is accessible at.<br />This setting is usually used together with the kubernetesAPIServerVIP setting. It enables specific underlying<br />components deployed directly or indirectly by the DPF Operator to reach the Kubernetes API Server when the<br />ClusterIP Kubernetes Service is not functional. |  |  |


#### ProvisioningControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the Provisioning controller |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `bfCFGTemplateConfigMap` _string_ | BFCFGTemplateConfigMap is the name of a configMap containing a template for the BF.cfg file used by the DPU controller.<br />By default the provisioning controller use a hardcoded BF.cfg e.g. https://github.com/NVIDIA/doca-platform/blob/release-v24.10/internal/provisioning/controllers/dpu/bfcfg/bf.cfg.template<br />Note: Replacing the bf.cfg is an advanced use case. The default bf.cfg is designed for most use cases. |  |  |
| `bfbPVCName` _string_ | BFBPersistentVolumeClaimName is the name of the PersistentVolumeClaim used by dpf-provisioning-controller |  | MinLength: 1 <br /> |
| `dmsTimeout` _integer_ | DMSTimeout is the max time in seconds within which a DMS API must respond, 0 is unlimited |  | Minimum: 1 <br /> |
| `customCASecretName` _string_ | CustomCASecretName indicates the name of the Kubernetes secret object<br />which containing the custom CA certificate |  |  |
| `installInterface` _[ProvisioningInstallInterface](#provisioninginstallinterface)_ | InstallInterface is the interface through which the BFB is installed |  |  |
| `maxDPUParallelInstallations` _integer_ | MaxDPUParallelInstallations specifies the maximum number of DPUs that can be provisioned concurrently. | 50 | Minimum: 1 <br /> |


#### ProvisioningInstallInterface



ProvisioningInstallInterface is the interface used to install the BFB



_Appears in:_
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `installViaGNOI` _[InstallViaGNOI](#installviagnoi)_ | InstallViaGNOI is the interface used to install the BFB via GNOI |  |  |
| `installViaRedfish` _[InstallViaRedfish](#installviaredfish)_ | InstallViaRedfish is the interface used to install the BFB via Redfish |  |  |


#### SFCControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the SFC Controller |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `deployInTargetCluster` _boolean_ | DeployInCluster deploys the component in the same cluster as the DPF Operator if set to true. |  |  |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the SFC Controller<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |
| `secureFlowDeletionTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#duration-v1-meta)_ | SecureFlowDeletionTimeout controls the timeout for which the API server is unreachable after which all the flows<br />are deleted to prevent unintended packet leaks. It has effect when is greater than zero.<br />Value must be in units accepted by Go time.ParseDuration https://golang.org/pkg/time/#ParseDuration. |  |  |


#### SRIOVDevicePluginConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the SRIOV Device Plugin |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `deployInTargetCluster` _boolean_ | DeployInCluster deploys the component in the same cluster as the DPF Operator if set to true. |  |  |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the SRIOV Device Plugin<br />The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid<br />OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br /> |


#### ServiceSetControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  |  |
| `image` _[Image](#image)_ | Image overrides the container image used by the ServiceSetController |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br /> |
| `deployInTargetCluster` _boolean_ | DeployInCluster deploys the component in the same cluster as the DPF Operator if set to true. |  |  |
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
- [DPUDevice](#dpudevice)
- [DPUDeviceList](#dpudevicelist)
- [DPUDiscovery](#dpudiscovery)
- [DPUDiscoveryList](#dpudiscoverylist)
- [DPUFlavor](#dpuflavor)
- [DPUFlavorList](#dpuflavorlist)
- [DPUList](#dpulist)
- [DPUNode](#dpunode)
- [DPUNodeList](#dpunodelist)
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
| `fileName` _string_ | Specifies the file name where the BFB is downloaded on the volume. |  | Pattern: `^[A-Za-z0-9\_\-\.]+\.bfb$` <br /> |
| `url` _string_ | The url of the bfb image to download. |  | Pattern: `^(http\|https)://.+$` <br /> |


#### BFBStatus



BFBStatus defines the observed state of BFB



_Appears in:_
- [BFB](#bfb)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `fileName` _string_ | Filename is the name of the file where the BFB can be accessed on its volume.<br />This is the same as `.spec.Filename` if set. |  |  |
| `phase` _[BFBPhase](#bfbphase)_ | The current state of BFB. | Initializing | Enum: [Initializing Downloading Ready Deleting Error] <br /> |
| `versions` _[BFBVersions](#bfbversions)_ | BFB versions - BSP, DOCA, UEFI and ATF<br />Holds detailed version information for each component within the BFB |  |  |


#### BFBVersions



BFBVersions represents the version information for BFB components.



_Appears in:_
- [BFBStatus](#bfbstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bsp` _string_ | BSP (Board Support Package) version.<br />This field stores the version of the BSP, which provides essential<br />support and drivers for the hardware platform. |  |  |
| `doca` _string_ | DOCA version<br />Specifies the version of NVIDIA's Data Center-on-a-Chip Architecture (DOCA),<br />a platform for developing applications on DPUs |  |  |
| `uefi` _string_ | UEFI (Unified Extensible Firmware Interface) version.<br />Indicates the UEFI firmware version, which is responsible for booting<br />the operating system and initializing hardware components |  |  |
| `atf` _string_ | ATF (Arm Trusted Firmware) version.<br />Contains the version of ATF, which provides a secure runtime environment |  |  |


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
| `kubeconfig` _string_ | Kubeconfig is the secret that contains the admin kubeconfig |  |  |
| `clusterEndpoint` _[ClusterEndpointSpec](#clusterendpointspec)_ | ClusterEndpoint contains configurations of the cluster entry point |  |  |


#### DPUClusterStatus



DPUClusterStatus defines the observed state of DPUCluster



_Appears in:_
- [DPUCluster](#dpucluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[ClusterPhase](#clusterphase)_ |  | Pending | Enum: [Pending Creating Ready NotReady Failed] <br /> |
| `version` _string_ | Version is the K8s control-plane version of the cluster |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ |  |  |  |






#### DPUDevice



DPUDevice is the Schema for the dpudevices API



_Appears in:_
- [DPUDeviceList](#dpudevicelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDevice` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUDeviceSpec](#dpudevicespec)_ |  |  |  |
| `status` _[DPUDeviceStatus](#dpudevicestatus)_ |  |  |  |


#### DPUDeviceList



DPUDeviceList contains a list of DPUDevices





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDeviceList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUDevice](#dpudevice) array_ |  |  |  |


#### DPUDeviceSpec



DPUDeviceSpec defines the content of DPUDevice



_Appears in:_
- [DPUDevice](#dpudevice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `psid` _string_ | PSID is the Product Serial ID of the device.<br />It's used to track the device's lifecycle and for inventory management.<br />This value is immutable and should not be changed once set.<br />Example: "MT_0001234567", "MT25066004C7" |  | Pattern: `^MT_?[A-Z0-9]+$` <br /> |
| `serialNumber` _string_ | SerialNumber is the serial number of the device.<br />It's used to track the device's lifecycle and for inventory management.<br />This value is immutable and should not be changed once set.<br />Example: "MT_0001234567", "MT25066004C7" |  |  |
| `opn` _string_ | OPN is the Ordering Part Number of the device.<br />It's used to track the device's compatibility with different software versions.<br />This value is immutable and should not be changed once set.<br />Example: "900-9D3B4-00SV-EA0" |  | Pattern: `^\d\{3\}-[A-Z0-9]\{5\}-[A-Z0-9]\{4\}-[A-Z0-9]\{3\}$` <br /> |
| `bmcIp` _string_ | BMCIP is the IP address of the BMC (Base Management Controller) on the device.<br />This is used for remote management and monitoring of the device.<br />This value is immutable and should not be changed once set.<br />Example: "10.1.2.3" |  | Format: ipv4 <br /> |
| `numberOfPFs` _integer_ | NumberOfPFs is the number of PFs on the device.<br />This value is immutable and should not be changed once set.<br />Example: 1 | 1 | Minimum: 1 <br /> |
| `pf0Name` _string_ | PF0Name is the name of the PF0 on the device.<br />This value is immutable and should not be changed once set.<br />Example: "eth0" |  |  |


#### DPUDeviceStatus







_Appears in:_
- [DPUDevice](#dpudevice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pciAddress` _string_ | PCIAddress is the PCI address of the device in the host system.<br />Example: "0000-03-00", "03-00" |  |  |


#### DPUDiscovery







_Appears in:_
- [DPUDiscoveryList](#dpudiscoverylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDiscovery` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUDiscoverySpec](#dpudiscoveryspec)_ |  |  |  |
| `status` _[DPUDiscoveryStatus](#dpudiscoverystatus)_ |  |  |  |


#### DPUDiscoveryList



DPUDiscoveryList contains a list of DPUDiscovery types





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUDiscoveryList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUDiscovery](#dpudiscovery) array_ |  |  |  |


#### DPUDiscoverySpec



DPUDiscoverySpec defines the desired state of DPUDiscovery



_Appears in:_
- [DPUDiscovery](#dpudiscovery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipRangeSpec` _[IPRangeValidationSpec](#iprangevalidationspec)_ | IPRange defines the range of IP addresses to scan |  |  |
| `scanInterval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#duration-v1-meta)_ | ScanInterval defines how often to perform the scan |  |  |
| `workers` _integer_ | Workers defines the number of workers to use for the scan (default 1 worker for each 255 IPs in the range) |  |  |


#### DPUDiscoveryStatus



DPUDiscoveryStatus defines the observed state of DPUDiscovery



_Appears in:_
- [DPUDiscovery](#dpudiscovery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `lastScanTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#time-v1-meta)_ | LastScanTime is the timestamp of the last successful scan |  |  |
| `foundDPUs` _integer_ | FoundDPUs is the list of discovered DPU BMC IPs |  |  |


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
| `dpuMode` _[DpuModeType](#dpumodetype)_ | Specifies the DPU Mode type: one of dpu,zero-trust | dpu | Enum: [dpu zero-trust] <br /> |




#### DPUList



DPUList contains a list of DPU





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPU](#dpu) array_ |  |  |  |


#### DPUNode



DPUNode is the Schema for the dpunodes API



_Appears in:_
- [DPUNodeList](#dpunodelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUNode` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUNodeSpec](#dpunodespec)_ |  |  |  |
| `status` _[DPUNodeStatus](#dpunodestatus)_ |  |  |  |






#### DPUNodeList



DPUNodeList contains a list of DPUNode





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `provisioning.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUNodeList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUNode](#dpunode) array_ |  |  |  |


#### DPUNodeSpec



DPUNodeSpec defines the desired state of DPUNode



_Appears in:_
- [DPUNode](#dpunode)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeRebootMethod` _[NodeRebootMethod](#noderebootmethod)_ | Defines the method for rebooting the host.<br />One of the following options can be chosen for this field:<br />   - "external": Reboot the host via an external means, not controlled by the<br />     DPU controller.<br />   - "script": Reboot the host by executing a custom script.<br />   - "gNOI": Use the DPU's DMS interface to reboot the host.<br />"gNOI" is the default value. | \{ gNOI:map[] \} |  |
| `nodeDMSAddress` _[DMSAddress](#dmsaddress)_ | The IP address and port where the DMS is exposed. Only applicable if dpuInstallInterface is set to gNOI. |  |  |
| `dpus` _[DPURef](#dpuref) array_ | A map containing names of each DPUDevice attached to the node. |  |  |


#### DPUNodeStatus



DPUNodeStatus defines the observed state of DPUNode



_Appears in:_
- [DPUNode](#dpunode)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions represent the latest available observations of an object's state. |  | Type: array <br /> |
| `dpuInstallInterface` _string_ | The name of the interface which will be used to install the bfb image, can be one of gNOI,redfish |  | Enum: [gNOI redfish] <br /> |
| `kubeNodeRef` _string_ | The name of the Kubernetes Node object that this DPUNode represents.<br />This field is optional and only relevant if the x86 host is part of the DPF Kubernetes cluster. |  |  |


#### DPUPhase

_Underlying type:_ _string_

DPUPhase describes current state of DPU.
Only one of the following state may be specified.
Default is Initializing.

_Validation:_
- Enum: [Initializing Node Effect Pending Config FW Parameters Prepare BFB OS Installing DPU Cluster Config Host Network Configuration Ready Error Deleting Rebooting Initialize Interface Checking Host Reboot Required]

_Appears in:_
- [DPUSetStatus](#dpusetstatus)
- [DPUStatus](#dpustatus)

| Field | Description |
| --- | --- |
| `Initializing` | DPUInitializing is the first phase after the DPU is created.<br /> |
| `Node Effect` | DPUNodeEffect means the controller will handle the node effect provided by the user.<br /> |
| `Pending` | DPUPending means the controller is waiting for the BFB to be ready.<br /> |
| `Prepare BFB` | DPUPrepareBFB means the controller is preparing the BFB and bf.cfg to be installed to DPU<br /> |
| `Config FW Parameters` | DPUConfigFWParameters means the controller will manipulate DPU firmware, e.g., set DPU mode, check firmware version<br /> |
| `Initialize Interface` | DPUInitializeInterface means the controller will intitialize the interface used to provision the DPUs, e.g., create the DMS pod, set up RedFish account.<br /> |
| `OS Installing` | DPUOSInstalling means the controller will provision the DPU through the DMS gNOI interface.<br /> |
| `DPU Cluster Config` | DPUClusterConfig  means the node configuration and Kubernetes Node join procedure are in progress .<br /> |
| `Host Network Configuration` | DPUHostNetworkConfiguration means the host network configuration is running.<br /> |
| `Ready` | DPUReady means the DPU is ready to use.<br /> |
| `Error` | DPUError means error occurred.<br /> |
| `Deleting` | DPUDeleting means the DPU CR will be deleted, controller will do some cleanup works.<br /> |
| `Rebooting` | DPURebooting means the host of DPU is rebooting.<br /> |
| `Checking Host Reboot Required` | DPUCheckingHostRebootNeed means the checking of whether the host required a reboot.<br /> |


#### DPURef







_Appears in:_
- [DPUNodeSpec](#dpunodespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name of the DPU device. |  | MinLength: 1 <br /> |


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
| `dpuNodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | Select the DPUNodes with specific labels |  |  |
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
| `type` _[StrategyType](#strategytype)_ | Can be "OnDelete" or "RollingUpdate". | OnDelete | Enum: [OnDelete RollingUpdate] <br /> |
| `rollingUpdate` _[RollingUpdateDPU](#rollingupdatedpu)_ | Rolling update config params. Present only if StrategyType = RollingUpdate. |  |  |


#### DPUSpec



DPUSpec defines the desired state of DPU



_Appears in:_
- [DPU](#dpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuNodeName` _string_ | Specifies the DPUNode this DPU belongs to |  |  |
| `dpuDeviceName` _string_ | Specifies the name of the DPUDevice this DPU is associated with |  | MinLength: 1 <br /> |
| `bfb` _string_ | Specifies name of the bfb CR to use for this DPU |  |  |
| `serialNumber` _string_ | The serial number of the DPU |  | MinLength: 1 <br /> |
| `pciAddress` _string_ | The PCI device related DPU<br />Example: "0000-03-00", "03-00" |  | Pattern: `^([0-9a-fA-F]\{4\}[-])?[0-9a-fA-F]\{2\}[-][0-9a-fA-F]\{2\}$` <br /> |
| `nodeEffect` _[NodeEffect](#nodeeffect)_ | Specifies how changes to the DPU should affect the Node | \{ drain:true \} |  |
| `cluster` _[K8sCluster](#k8scluster)_ | Specifies details on the K8S cluster to join |  |  |
| `dpuFlavor` _string_ | DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU. |  |  |
| `bmcIP` _string_ | BMCIP is the ip address of the DPU BMC |  |  |


#### DPUStatus



DPUStatus defines the observed state of DPU



_Appears in:_
- [DPU](#dpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[DPUPhase](#dpuphase)_ | The current state of DPU. | Initializing | Enum: [Initializing Node Effect Pending Config FW Parameters Prepare BFB OS Installing DPU Cluster Config Host Network Configuration Ready Error Deleting Rebooting Initialize Interface Checking Host Reboot Required] <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ |  |  |  |
| `bfbFile` _string_ | BFBFile is the path to the BFB file |  |  |
| `bfCFGFile` _string_ | BFCFGFile is the path to the bf.cfg |  |  |
| `bfbVersion` _string_ | bfb version of this DPU |  |  |
| `dpfVersion` _string_ | DPF version used to install this DPU |  |  |
| `pciDevice` _string_ | pci device information of this DPU |  |  |
| `requiredReset` _boolean_ | whether require reset of DPU |  |  |
| `firmware` _[Firmware](#firmware)_ | the firmware information of DPU |  |  |
| `addresses` _[NodeAddress](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#nodeaddress-v1-core) array_ | The DPU node's IP addresses |  |  |
| `dpuInstallInterface` _string_ | the name of the interface which will be used to install the bfb image,<br />and communicate with DPU, can be one of gNOI,redfish |  | Enum: [gNOI redfish] <br /> |


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
| `nodeEffect` _[NodeEffect](#nodeeffect)_ | Specifies how changes to the DPU should affect the Node | \{ drain:true \} |  |
| `cluster` _[ClusterSpec](#clusterspec)_ | Specifies details on the K8S cluster to join |  |  |
| `dpuFlavor` _string_ | DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU. |  |  |


#### DpuModeType

_Underlying type:_ _string_

DpuModeType defines the mode of the DPU

_Validation:_
- Enum: [dpu zero-trust]

_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description |
| --- | --- |
| `dpu` |  |
| `zero-trust` |  |


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



#### IPRange



IPRange represents a range of IP addresses to scan



_Appears in:_
- [IPRangeValidationSpec](#iprangevalidationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `startIP` _string_ |  |  | Pattern: `^((25[0-5]\|2[0-4][0-9]\|[01]?[0-9][0-9]?)\.)\{3\}(25[0-5]\|2[0-4][0-9]\|[01]?[0-9][0-9]?)$` <br /> |
| `endIP` _string_ |  |  | Pattern: `^((25[0-5]\|2[0-4][0-9]\|[01]?[0-9][0-9]?)\.)\{3\}(25[0-5]\|2[0-4][0-9]\|[01]?[0-9][0-9]?)$` <br /> |


#### IPRangeValidationSpec



IPRangeValidationSpec defines the desired state of IPRangeValidation
IPRange defines the IP range to validate



_Appears in:_
- [DPUDiscoverySpec](#dpudiscoveryspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipRange` _[IPRange](#iprange)_ |  |  |  |


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



NodeEffect is the effect the DPU has on Nodes during provisioning.
Only one of Taint, NoEffect, CustomLabel, Drain, CustomAction, Hold can be set.



_Appears in:_
- [DPUSpec](#dpuspec)
- [DPUTemplateSpec](#dputemplatespec)
- [DPUs](#dpus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `taint` _[Taint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#taint-v1-core)_ | Add specify taint on the DPU node |  |  |
| `noEffect` _boolean_ | Do not do any action on the DPU node |  |  |
| `customLabel` _object (keys:string, values:string)_ | Add specify labels on the DPU node |  |  |
| `drain` _boolean_ | Drain the K8s host node by NodeMaintenance operator |  |  |
| `customAction` _string_ | Name of a config map which contains a pod yaml definition to run which will apply the nodeEffect.<br />The pod is expected to exit when node effect is done, if pod terminates with error then DPU would move to an error phase.<br />The DPUNode's name will be exported as an environment variable, named as DPUNODE_NAME, to each container and init container in the pod.<br />The labels and annotations of DPUNode will be exported in `/etc/dpu/dpf-pod-info/labels` and `/etc/dpu/dpf-pod-info/annotations` accordingly; the volume name `dpf-pod-info` is used to mount the labels and annotations.<br />If any name confliction for env or volume, the controller will not export the name or labels/annotations of DPUNode accordingly. |  |  |
| `hold` _boolean_ | Places annotation `wait-for-external-nodeeffect` and waits for it to be removed<br />this is the default behavior in a non K8S environment |  |  |


#### NodeRebootMethod



NodeRebootMethod defines the desired reboot method



_Appears in:_
- [DPUNodeSpec](#dpunodespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `gNOI` _[GNOI](#gnoi)_ | Use the DPU's DMS interface to reboot the host. |  |  |
| `external` _[External](#external)_ | Reboot the host via an external means, not controlled by the DPU controller. |  |  |
| `script` _[Script](#script)_ | Reboot the host by executing a custom script. This field defined which ConfigMap store the custom script.<br />The ConfigMap should include a pod template of Job object under the `pod-template` key.<br />That pod template will be put in a Job object to be executed. |  |  |


#### RollingUpdateDPU



RollingUpdateDPU is the rolling update strategy for a DPUSet.



_Appears in:_
- [DPUSetStrategy](#dpusetstrategy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `maxUnavailable` _[IntOrString](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#intorstring-intstr-util)_ | MaxUnavailable is the maximum number of DPUs that can be unavailable during the update. |  |  |


#### Script







_Appears in:_
- [NodeRebootMethod](#noderebootmethod)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  | MinLength: 1 <br /> |


#### StrategyType

_Underlying type:_ _string_

StrategyType describes strategy to use to reprovision existing DPUs.
Default is "OnDelete".

_Validation:_
- Enum: [OnDelete RollingUpdate]

_Appears in:_
- [DPUSetStrategy](#dpusetstrategy)

| Field | Description |
| --- | --- |
| `OnDelete` | New DPU CR will only be created when you manually delete old DPU CR.<br /> |
| `RollingUpdate` | Gradually scale down the old DPUs and scale up the new one.<br /> |



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
- [StoragePolicy](#storagepolicy)
- [StoragePolicyList](#storagepolicylist)
- [StorageVendor](#storagevendor)
- [StorageVendorList](#storagevendorlist)
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
| `pciAddress` _string_ | PCI device address in the following format: (bus:device.function) |  |  |
| `deviceName` _string_ | The name of the device that was created by the storage vendor plugin |  |  |
| `nvmeAttrs` _[NVMEAttrs](#nvmeattrs)_ | The attributes of the emulated NVME function |  |  |
| `virtioFSAttrs` _[VirtioFSAttrs](#virtiofsattrs)_ | The attributes of the emulated VirtioFS function |  |  |


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
| `request` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#quantity-resource-api)_ |  |  |  |
| `limit` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#quantity-resource-api)_ |  |  |  |


#### DPUStoragePolicy



DPUStoragePolicy represents a DPUStoragePolicy CR



_Appears in:_
- [DPUStoragePolicyList](#dpustoragepolicylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUStoragePolicy` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUStoragePolicySpec](#dpustoragepolicyspec)_ |  |  |  |
| `status` _[DPUStoragePolicyStatus](#dpustoragepolicystatus)_ |  |  |  |


#### DPUStoragePolicyList



DPUStoragePolicyList contains a list of DPUStoragePolicy objects





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUStoragePolicyList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUStoragePolicy](#dpustoragepolicy) array_ |  |  |  |


#### DPUStoragePolicySpec



DPUStoragePolicySpec defines the desired state of DPUStoragePolicy



_Appears in:_
- [DPUStoragePolicy](#dpustoragepolicy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuStorageVendors` _string array_ | List of storage vendors |  | MinItems: 1 <br /> |
| `parameters` _object (keys:string, values:string)_ | Parameters supported by the policy | \{  \} |  |
| `selectionAlgorithm` _[SelectionAlgorithm](#selectionalgorithm)_ | Selection algorithm used to select DPUStorageVendor | NumberVolumes | Enum: [Random NumberVolumes] <br /> |


#### DPUStoragePolicyStatus



DPUStoragePolicyStatus defines the observed state of DPUStoragePolicy



_Appears in:_
- [DPUStoragePolicy](#dpustoragepolicy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Current service state conditions |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUStorageVendor



DPUStorageVendor represents a StorageVendor CR on the DPU cluster.



_Appears in:_
- [DPUStorageVendorList](#dpustoragevendorlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUStorageVendor` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUStorageVendorSpec](#dpustoragevendorspec)_ |  |  |  |
| `status` _[DPUStorageVendorStatus](#dpustoragevendorstatus)_ |  |  |  |


#### DPUStorageVendorList



DPUStorageVendorList contains a list of DPUStorageVendor





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUStorageVendorList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUStorageVendor](#dpustoragevendor) array_ |  |  |  |


#### DPUStorageVendorSpec



DPUStorageVendorSpec defines the desired state of DPUStorageVendor



_Appears in:_
- [DPUStorageVendor](#dpustoragevendor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageClassName` _string_ | Storage vendor class name, deployed on the DPU K8S cluster. |  | MinLength: 1 <br /> |
| `pluginName` _string_ | Storage vendor DPU plugin name |  | MinLength: 1 <br /> |


#### DPUStorageVendorStatus



DPUStorageVendorStatus defines the observed state of DPUStorageVendor



_Appears in:_
- [DPUStorageVendor](#dpustoragevendor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions defines current service state. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUVolume



DPUVolume represents a DPUVolume CR.



_Appears in:_
- [DPUVolumeList](#dpuvolumelist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVolume` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
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
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUVolumeAttachmentSpec](#dpuvolumeattachmentspec)_ |  |  |  |
| `status` _[DPUVolumeAttachmentStatus](#dpuvolumeattachmentstatus)_ |  |  |  |


#### DPUVolumeAttachmentList



DPUVolumeAttachmentList contains a list of DPUVolumeAttachment





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVolumeAttachmentList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUVolumeAttachment](#dpuvolumeattachment) array_ |  |  |  |


#### DPUVolumeAttachmentSpec



DPUVolumeAttachmentSpec defines the desired state of DPUVolumeAttachment



_Appears in:_
- [DPUVolumeAttachment](#dpuvolumeattachment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuNodeName` _string_ | DPUNodeName is the name of DPUNode object that represents the node to which the volume should<br />be attached |  | MinLength: 1 <br /> |
| `dpuVolumeName` _string_ | DPUVolumeName is the name of DPUVolume object that represents the volume to be attached |  | MinLength: 1 <br /> |
| `functionType` _[FunctionType](#functiontype)_ | FunctionType is the type of the emulated function that should be used to attach the volume |  | Enum: [pf vf] <br /> |
| `hotplugFunction` _boolean_ | HotplugFunction is a boolean flag that indicates if the emulated function should be hotplugged |  |  |


#### DPUVolumeAttachmentStatus



DPUVolumeAttachmentStatus defines the observed state of DPUVolumeAttachment



_Appears in:_
- [DPUVolumeAttachment](#dpuvolumeattachment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `controllerAttached` _boolean_ | Indicates the volume is successfully attached to by the Vendor CSI driver |  |  |
| `dpuAttached` _boolean_ | Indicates the volume is successfully attached to the node by DPU |  |  |
| `attachmentMetadata` _object (keys:string, values:string)_ | AttachmentMetadata contains the metadata of the volume attachment returned by the Vendor CSI driver |  |  |
| `dpu` _[AttachmentStatusDPU](#attachmentstatusdpu)_ | Details about the DPU attachment |  |  |
| `message` _string_ | The last error encountered during the attach operation, if any |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions defines current service state. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUVolumeList



DPUVolumeList contains a list of DPUVolume





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVolumeList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
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
| `dpuStoragePolicyName` _string_ | Name of the DPUStoragePolicyName object that will be used to create the volume. |  | MinLength: 1 <br /> |
| `parameters` _object (keys:string, values:string)_ | Additional parameters for the volume, these parameters are merged with the values from the DPUStoragePolicy object. | \{  \} |  |
| `accessModes` _[PersistentVolumeAccessMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#persistentvolumeaccessmode-v1-core) array_ | Access modes define how the volume can be mounted. These modes are directly passed to the<br />PersistentVolumeClaim created for the Vendor CSI Plugin selected by the DPUStoragePolicy. |  | MaxItems: 3 <br />MinItems: 1 <br /> |
| `resources` _[VolumeResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#volumeresourcerequirements-v1-core)_ | Resources represents the storage resources requested for the volume. These resource requirements<br />are directly passed to the PersistentVolumeClaim created for the Vendor CSI Plugin selected<br />by the DPUStoragePolicy. Since volume resizing is not supported, modifications to the resource request are prohibited. |  |  |
| `volumeMode` _[PersistentVolumeMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#persistentvolumemode-v1-core)_ | Volume mode defines how the volume should be mounted and used. This value is directly passed to the<br />PersistentVolumeClaim created for the Vendor CSI Plugin selected by the DPUStoragePolicy. | Filesystem | Enum: [Filesystem Block] <br /> |


#### DPUVolumeState



DPUVolumeState defines the state of the volume.



_Appears in:_
- [DPUVolumeStatus](#dpuvolumestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuCluster` _[ObjectReference](#objectreference)_ | DPUCluster contains the reference to the DPUCluster object that was selected for volume creation. |  |  |
| `parameters` _object (keys:string, values:string)_ | Parameters contains the final set of parameters for volume creation, computed by merging<br />the parameters from the DPUStoragePolicy object with user-provided parameters. |  |  |
| `selectedDPUStorageVendorName` _string_ | SelectedDPUStorageVendorName contains the name of the DPUStorageVendor object that was selected for volume creation. |  |  |
| `storageVendorPluginName` _string_ | StorageVendorPluginName contains the name of the storage vendor plugin deployed on the DPU cluster that was selected for volume creation. |  |  |
| `storageClassName` _string_ | StorageClassName contains the name of the storage class in the DPU cluster that was selected for volume creation. |  |  |
| `csiDriverName` _string_ | CSIDriverName contains the name of the CSI driver in the DPU cluster that was selected for volume creation. |  |  |
| `persistentVolumeClaimRef` _[ObjectReference](#objectreference)_ | PersistentVolumeClaimRef contains the reference to the PersistentVolumeClaim object in the DPU cluster that was created for the volume. |  |  |
| `volumeInfo` _[VolumeInfo](#volumeinfo)_ | VolumeInfo contains a subset of fields from the PersistentVolume object created in the DPU cluster |  |  |


#### DPUVolumeStatus



DPUVolumeStatus defines the observed state of DPUVolume



_Appears in:_
- [DPUVolume](#dpuvolume)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[DPUVolumePhase](#dpuvolumephase)_ | Phase of the volume |  | Enum: [Pending Bound] <br /> |
| `state` _[DPUVolumeState](#dpuvolumestate)_ | State of the volume. This field is managed by the controller. User usually do not need to set fields from this struct. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions defines current service state. |  |  |
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
| `functionType` _[FunctionType](#functiontype)_ | FunctionType is the type of the emulated function that should be used to attach the volume |  | Enum: [pf vf] <br /> |
| `hotplugFunction` _boolean_ | HotplugFunction is a boolean flag that indicates if the emulated function should be hotplugged |  |  |


#### NVMEAttrs



NVMEAttrs represents the attributes of the NVME emulated function



_Appears in:_
- [AttachmentStatusDPU](#attachmentstatusdpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespaceID` _integer_ | The namespace ID within the NVME controller |  |  |
| `namespaceUUID` _string_ | The NVMe namespace UUID |  |  |


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
- [DPUVolumeState](#dpuvolumestate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name specifies the name of the referenced object |  | MinLength: 1 <br /> |
| `namespace` _string_ | Namespace specifies the namespace where the referenced object exists |  | MinLength: 1 <br /> |


#### SVVolumeAttachment



SVVolumeAttachment captures the intent to attach/detach the specified Volume to/from the specified node.



_Appears in:_
- [SVVolumeAttachmentList](#svvolumeattachmentlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `SVVolumeAttachment` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[VolumeAttachmentSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#volumeattachmentspec-v1-storage)_ |  |  |  |
| `status` _[VolumeAttachmentStatus](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#volumeattachmentstatus-v1-storage)_ |  |  |  |


#### SVVolumeAttachmentList



SVVolumeAttachmentList contains a list of SVVolumeAttachment





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `SVVolumeAttachmentList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
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


#### StoragePolicy



StoragePolicy represents a storage policy which maps between policy into a list of storage vendors.
StoragePolicy is an internal API, it is not intended to be used by users.



_Appears in:_
- [StoragePolicyList](#storagepolicylist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `StoragePolicy` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[StoragePolicySpec](#storagepolicyspec)_ |  |  |  |
| `status` _[StoragePolicyStatus](#storagepolicystatus)_ |  |  |  |


#### StoragePolicyList



StoragePolicyList contains a list of StoragePolicy





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `StoragePolicyList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[StoragePolicy](#storagepolicy) array_ |  |  |  |


#### StoragePolicySpec



StoragePolicySpec defines the desired state of StoragePolicy



_Appears in:_
- [StoragePolicy](#storagepolicy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageVendors` _string array_ | List of storage vendors |  |  |
| `storageParameters` _object (keys:string, values:string)_ | List of storage parameters supported by the policy, values are string only |  |  |
| `storageSelectionAlg` _[StorageSelectionAlgType](#storageselectionalgtype)_ | Algorithm used to select the storage vendor. Default: LocalNVolumes | LocalNVolumes | Enum: [Random LocalNVolumes] <br /> |


#### StoragePolicyStatus



StoragePolicyStatus defines the observed state of StoragePolicy



_Appears in:_
- [StoragePolicy](#storagepolicy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `state` _[StorageVendorState](#storagevendorstate)_ | A storage policy is valid if all provided storage vendors have a StorageVendor object with a valid storage class object |  | Enum: [Valid Invalid] <br /> |
| `message` _string_ | Informative message when the state is invalid |  |  |


#### StorageSelectionAlgType

_Underlying type:_ _string_

StorageSelectionAlgType represents the type of storage selection algorithm

_Validation:_
- Enum: [Random LocalNVolumes]

_Appears in:_
- [StoragePolicySpec](#storagepolicyspec)

| Field | Description |
| --- | --- |
| `Random` | Random selection across the vendors defined in the StoragePolicy list.<br /> |
| `LocalNVolumes` | Load-balancing on the number of volumes belonging to the StoragePolicy.<br />The vendor (in the StoragePolicy list) with the minimal number of volumes should be selected.<br /> |


#### StorageVendor



StorageVendor represents a storage vendor.
Each storage vendor must have exactly one NVIDIA StorageVendor custom resource object.
StorageVendor is an internal API, it is not intended to be used by users.



_Appears in:_
- [StorageVendorList](#storagevendorlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `StorageVendor` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[StorageVendorSpec](#storagevendorspec)_ |  |  |  |


#### StorageVendorList



StorageVendorList contains a list of StorageVendor





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `StorageVendorList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[StorageVendor](#storagevendor) array_ |  |  |  |


#### StorageVendorSpec



StorageVendorSpec defines the desired state of StorageVendor



_Appears in:_
- [StorageVendor](#storagevendor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageClassName` _string_ | Storage vendor class name, deployed on the DPU K8S cluster. |  | MinLength: 1 <br /> |
| `pluginName` _string_ | Storage vendor DPU plugin name |  | MinLength: 1 <br /> |


#### StorageVendorState

_Underlying type:_ _string_

StorageVendorState represents the state of storage vendor

_Validation:_
- Enum: [Valid Invalid]

_Appears in:_
- [StoragePolicyStatus](#storagepolicystatus)

| Field | Description |
| --- | --- |
| `Valid` | Vaild means all provided storage vendors (storageVendors)<br />have a StorageVendor object with a valid storage class object<br /> |
| `Invalid` | Invalid means one or more provided storage vendors (storageVendors) do not<br />have a StorageVendor object with a valid storage class object<br /> |


#### VirtioFSAttrs



VirtioFSAttrs represents the attributes of the VirtioFS emulated function



_Appears in:_
- [AttachmentStatusDPU](#attachmentstatusdpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `filesystemTag` _string_ | Filesystem tag identified by SNAP on the host (used for the mount). Relevant for volume of type filesystem |  |  |


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
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
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
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[VolumeAttachmentSpec](#volumeattachmentspec)_ |  |  |  |
| `status` _[VolumeAttachmentStatus](#volumeattachmentstatus)_ |  |  |  |


#### VolumeAttachmentList



VolumeAttachmentList contains a list of VolumeAttachment





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `VolumeAttachmentList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
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
| `functionType` _[FunctionType](#functiontype)_ | FunctionType is the type of the emulated function that should be used to attach the volume |  | Enum: [pf vf] <br /> |
| `hotplugFunction` _boolean_ | HotplugFunction is a boolean flag that indicates if the emulated function should be hotplugged |  |  |


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
| `bdevAttrs` _[BdevAttrs](#bdevattrs)_ | The attributes of the underlying block device |  |  |
| `fsdevAttrs` _[FSdevAttrs](#fsdevattrs)_ | The attributes of the underlying filesystem device |  |  |


#### VolumeInfo



VolumeInfo represents a subset of fields from the PersistentVolume object that was created in the DPU cluster.
This struct is used to track and expose key volume information without carrying the full PersistentVolume object.



_Appears in:_
- [DPUVolumeState](#dpuvolumestate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `capacity` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#resourcelist-v1-core)_ | Actual capacity of the volume in the DPU cluster |  |  |
| `accessModes` _[PersistentVolumeAccessMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#persistentvolumeaccessmode-v1-core) array_ | Actual access modes of the volume in the DPU cluster |  |  |
| `volumeMode` _[PersistentVolumeMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#persistentvolumemode-v1-core)_ | Actual volume mode of the volume in the DPU cluster |  |  |
| `volumeAttributes` _object (keys:string, values:string)_ | VolumeAttributes from the PersistentVolume object in the DPU cluster<br />This field usually contains parameters returned by the Vendor CSI plugin on volume creation. |  |  |


#### VolumeList



VolumeList contains a list of Volume





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `storage.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `VolumeList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Volume](#volume) array_ |  |  |  |


#### VolumeRequest



VolumeRequest represents the volume's requirements



_Appears in:_
- [VolumeSpec](#volumespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `capacityRange` _[CapacityRange](#capacityrange)_ | The capacity of the required storage space in bytes |  |  |
| `accessModes` _[PersistentVolumeAccessMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#persistentvolumeaccessmode-v1-core) array_ | Contains the types of access modes required |  |  |
| `volumeMode` _[PersistentVolumeMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#persistentvolumemode-v1-core)_ | volumeMode defines what type of volume is required by the claim.<br />Value of Filesystem is implied when not included in claim spec. |  |  |


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
| `storageParameters` _object (keys:string, values:string)_ | List of storage parameters supported by the policy, values are string only |  |  |
| `request` _[VolumeRequest](#volumerequest)_ | The capacity of the required storage space in bytes |  |  |
| `storagePolicyRef` _[ObjectRef](#objectref)_ | Reference to the StoragePolicy object |  |  |
| `storagePolicyParameters` _object (keys:string, values:string)_ | List of storage parameters supported by the policy, values are string only |  |  |
| `volume` _[VolumeSpecDPU](#volumespecdpu)_ | Describe volume information in DPU cluster |  |  |


#### VolumeSpecDPU



VolumeSpecDPU describe volume information in DPU cluster



_Appears in:_
- [VolumeSpec](#volumespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `id` _string_ |  |  |  |
| `capacity` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#quantity-resource-api)_ |  |  |  |
| `accessModes` _[PersistentVolumeAccessMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#persistentvolumeaccessmode-v1-core) array_ |  |  |  |
| `reclaimPolicy` _[PersistentVolumeReclaimPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#persistentvolumereclaimpolicy-v1-core)_ |  |  | Enum: [Delete Retain] <br /> |
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
| `state` _[VolumeState](#volumestate)_ | The state of a Volume object |  | Enum: [InProgress Available] <br /> |



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


#### ConfigPort



ConfigPort defines the configuration of a single port within a DPUService.
Each port must have a unique name within the service.



_Appears in:_
- [ConfigPorts](#configports)
- [DPUServiceStatus](#dpuservicestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is a unique identifier for the port within the DPUService.<br />This name is used for reference inside the service. |  | MinLength: 1 <br />Pattern: `^[a-z0-9-]+$` <br /> |
| `port` _integer_ | Port is the port number that will be exposed by the service.<br />Must be within the valid range of TCP/UDP ports (1-65535). |  |  |
| `protocol` _[Protocol](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#protocol-v1-core)_ | Protocol specifies the transport protocol used by the port.<br />Supported values: TCP, UDP |  | Enum: [TCP UDP] <br /> |
| `nodePort` _integer_ | NodePort is the external port assigned on each node in the cluster.<br />If not set, Kubernetes will automatically allocate a NodePort.<br /><br />Constraints:<br />- Can only be set when ServiceType is "NodePort".<br />- Must be within the clusters valid NodePort range (Kubernetes default is 30000-32767). |  |  |


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
| `serviceType` _[ServiceType](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#servicetype-v1-core)_ | ServiceType specifies the type of Kubernetes Service to create.<br />All ports within this ConfigPorts will have the same ServiceType.<br />The value is immutable and cannot be changed after creation.<br />Supported values:<br />- "NodePort": Exposes ports externally on a node.<br />- "ClusterIP": Exposes ports internally within the cluster.<br />- "None": Internal-only service with no cluster IP.<br />Default: "NodePort" | NodePort | Enum: [NodePort ClusterIP None] <br /> |
| `ports` _[ConfigPort](#configport) array_ | Ports defines the list of port configurations that will be exposed by the DPUService.<br />Each port must specify a name, port number, and protocol.<br /><br />Constraints:<br />- If ServiceType is "NodePort", ports may optionally specify a NodePort.<br />- If ServiceType is "None" or "ClusterIP", ports **cannot** specify a NodePort. |  |  |


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
| `name` _string_ | Name is the name of the service as defined in the DPUDeployment Spec |  | MaxLength: 28 <br />MinLength: 1 <br /> |
| `interface` _string_ | Interface name is the name of the interface as defined in the DPUServiceTemplate |  | MaxLength: 28 <br />MinLength: 1 <br /> |
| `ipam` _[IPAM](#ipam)_ | IPAM defines the IPAM configuration that is configured in the Service Function Chain |  |  |


#### DPUDeploymentServiceConfiguration



DPUDeploymentServiceConfiguration describes the configuration of a particular Service



_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceTemplate` _string_ | ServiceTemplate is the name of the DPUServiceTemplate object to be used for this Service. It must be in the same<br />namespace as the DPUDeployment. |  |  |
| `serviceConfiguration` _string_ | ServiceConfiguration is the name of the DPUServiceConfiguration object to be used for this Service. It must be<br />in the same namespace as the DPUDeployment. |  |  |
| `dependsOn` _[LocalObjectDependency](#localobjectdependency) array_ | DependsOn is a list of local object dependencies that are required for this Service. |  | MinItems: 1 <br /> |


#### DPUDeploymentSpec



DPUDeploymentSpec defines the desired state of DPUDeployment



_Appears in:_
- [DPUDeployment](#dpudeployment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpus` _[DPUs](#dpus)_ | DPUs contains the DPU related configuration |  |  |
| `services` _object (keys:string, values:[DPUDeploymentServiceConfiguration](#dpudeploymentserviceconfiguration))_ | Services contains the DPUDeploymentService related configuration. The key is the deploymentServiceName and the value is its<br />configuration. All underlying objects must specify the same deploymentServiceName in order to be able to be consumed by the<br />DPUDeployment. |  |  |
| `serviceChains` _[ServiceChains](#servicechains)_ | ServiceChains contains the configuration related to the DPUServiceChains that the DPUDeployment creates. |  |  |
| `revisionHistoryLimit` _integer_ | The maximum number of revisions that can be retained during upgrades.<br />Defaults to 10. | 10 |  |


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
- [ServiceChains](#servicechains)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ports` _[DPUDeploymentPort](#dpudeploymentport) array_ | Ports contains the ports of the switch |  | MaxItems: 50 <br />MinItems: 1 <br /> |
| `serviceMTU` _integer_ | ServiceMTU of the switch<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1000 <br /> |


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
| `updateStrategy` _[DaemonSetUpdateStrategy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#daemonsetupdatestrategy-v1-apps)_ | UpdateStrategy specifies the DeaemonSet update strategy for the ServiceDaemonset. |  |  |
| `labels` _object (keys:string, values:string)_ | Labels specifies labels which are added to the ServiceDaemonSet. |  | MaxProperties: 50 <br /> |
| `annotations` _object (keys:string, values:string)_ | Annotations specifies annotations which are added to the ServiceDaemonSet. |  | MaxProperties: 50 <br /> |
| `resources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#resourcelist-v1-core)_ | Resources specifies resources which are added to the ServiceDaemonSet. |  |  |


#### DPUServiceConfigurationSpec



DPUServiceConfigurationSpec defines the desired state of DPUServiceConfiguration



_Appears in:_
- [DPUServiceConfiguration](#dpuserviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `deploymentServiceName` _string_ | DeploymentServiceName is the name of the DPU service this configuration refers to. It must match<br />.spec.deploymentServiceName of a DPUServiceTemplate object and one of the keys in .spec.services of a<br />DPUDeployment object. |  | MaxLength: 28 <br />MinLength: 1 <br /> |
| `serviceConfiguration` _[ServiceConfiguration](#serviceconfiguration)_ | ServiceConfiguration contains fields that are configured on the generated DPUService. |  |  |
| `interfaces` _[ServiceInterfaceTemplate](#serviceinterfacetemplate) array_ | Interfaces specifies the DPUServiceInterface to be generated for the generated DPUService. |  | MaxItems: 50 <br />MinItems: 1 <br /> |
| `upgradePolicy` _[UpgradePolicy](#upgradepolicy)_ | UpgradePolicy contains the configuration for the upgrade process | \{  \} |  |


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
| `type` _string_ | Type is the type of the secret that will be created.<br />The supported types are `kubeconfig` and `tokenFile`.<br />If `kubeconfig` is selected, the secret will contain a kubeconfig file,<br />that can be used to access the cluster.<br />If `tokenFile` is selected, the secret will contain a token file and several<br />environment variables that can be used to access the cluster. It can be used<br />with https://github.com/kubernetes/client-go/blob/v11.0.0/rest/config.go#L52<br />to create a client that will handle file rotation. |  | Enum: [kubeconfig tokenFile] <br /> |
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


#### DPUServiceNAD



DPUServiceNAD is the Schema for the dpuservicenads API.



_Appears in:_
- [DPUServiceNADList](#dpuservicenadlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceNAD` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUServiceNADSpec](#dpuservicenadspec)_ |  |  |  |
| `status` _[DPUServiceNADStatus](#dpuservicenadstatus)_ |  |  |  |


#### DPUServiceNADList



DPUServiceNADList contains a list of DPUServiceNAD.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceNADList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceNAD](#dpuservicenad) array_ |  |  |  |


#### DPUServiceNADSpec



DPUServiceNADSpec defines the desired state of DPUServiceNAD.



_Appears in:_
- [DPUServiceNAD](#dpuservicenad)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `resourceType` _string_ |  |  | Enum: [vf sf veth] <br /> |
| `bridge` _string_ |  |  |  |
| `serviceMTU` _integer_ |  |  |  |
| `ipam` _boolean_ |  |  |  |


#### DPUServiceNADStatus



DPUServiceNADStatus defines the observed state of DPUServiceNAD.



_Appears in:_
- [DPUServiceNAD](#dpuservicenad)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |


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
| `configPorts` _[ConfigPorts](#configports)_ | ConfigPorts defines the desired state of port configurations for a DPUService.<br />This struct determines how ports are exposed from the DPU to the host cluster.<br />A DPUService can only have a single ServiceType across all ports. |  |  |


#### DPUServiceStatus



DPUServiceStatus defines the observed state of DPUService



_Appears in:_
- [DPUService](#dpuservice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions defines current service state. |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |
| `configPorts` _object (keys:string, values:[ConfigPort](#configport))_ | ConfigPorts defines the observed state of the config ports.<br />It contains the actual port numbers that are exposed on the DPUService per cluster. |  |  |


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
| `deploymentServiceName` _string_ | DeploymentServiceName is the name of the DPU service this configuration refers to. It must match<br />.spec.deploymentServiceName of a DPUServiceConfiguration object and one of the keys in .spec.services of a<br />DPUDeployment object. |  | MaxLength: 28 <br />MinLength: 1 <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart reflects the Helm related configuration. The user is supposed to configure the values that are static<br />across any DPUServiceConfiguration used with this DPUServiceTemplate in a DPUDeployment. These values act as a<br />baseline and are merged with values specified in the DPUServiceConfiguration. In case of conflict, the<br />DPUServiceConfiguration values take precedence. |  |  |
| `resourceRequirements` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#resourcelist-v1-core)_ | ResourceRequirements contains the overall resources required by this particular service to run on a single node |  |  |


#### DPUServiceTemplateStatus



DPUServiceTemplateStatus defines the observed state of DPUServiceTemplate



_Appears in:_
- [DPUServiceTemplate](#dpuservicetemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |
| `versions` _object (keys:string, values:string)_ | Versions reflects the required versions the generated DPUService needs in order to function correctly. |  |  |


#### DPUSet



DPUSet contains configuration for the DPUSet to be created by the DPUDeployment



_Appears in:_
- [DPUs](#dpus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nameSuffix` _string_ | NameSuffix is the suffix to be added to the name of the DPUSet object created by the DPUDeployment. |  | MaxLength: 24 <br />MinLength: 1 <br /> |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | NodeSelector defines the nodes that the DPUSet should target |  |  |
| `dpuSelector` _object (keys:string, values:string)_ | DPUSelector defines the DPUs that the DPUSet should target |  |  |
| `dpuAnnotations` _object (keys:string, values:string)_ | DPUAnnotations is the annotations to be added to the DPU object created by the DPUSet. |  | MaxProperties: 50 <br /> |


#### DPUs



DPUs contains the DPU related configuration



_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bfb` _string_ | BFB is the name of the BFB object to be used in this DPUDeployment. It must be in the same namespace as the<br />DPUDeployment. |  |  |
| `flavor` _string_ | Flavor is the name of the DPUFlavor object to be used in this DPUDeployment. It must be in the same namespace as<br />the DPUDeployment. |  |  |
| `dpuSets` _[DPUSet](#dpuset) array_ | DPUSets contains configuration for each DPUSet that is going to be created by the DPUDeployment |  | MaxItems: 50 <br />MinItems: 1 <br /> |
| `nodeEffect` _[NodeEffect](#nodeeffect)_ | NodeEffect is the effect the DPU has on Nodes during provisioning. |  |  |


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

_Underlying type:_ _[struct{MatchLabels map[string]string "json:\"matchLabels\""; DefaultGateway *bool "json:\"defaultGateway,omitempty\""; SetDefaultRoute *bool "json:\"setDefaultRoute,omitempty\""}](#struct{matchlabels-map[string]string-"json:\"matchlabels\"";-defaultgateway-*bool-"json:\"defaultgateway,omitempty\"";-setdefaultroute-*bool-"json:\"setdefaultroute,omitempty\""})_

IPAM defines the IPAM configuration



_Appears in:_
- [DPUDeploymentService](#dpudeploymentservice)
- [ServiceIfc](#serviceifc)



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


#### LocalObjectDependency



LocalObjectDependency is a list of local object dependencies that are required for this Service.
The object must be part of the dpuDeployment `spec.services` list.



_Appears in:_
- [DPUDeploymentServiceConfiguration](#dpudeploymentserviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the object |  |  |
| `wait` _boolean_ | Wait defines if the object readiness should be waited for<br />If false, the reconciler will make sure that dependent objects are created in order but will not wait for<br />them to be ready. |  |  |


#### NamespacedName



NamespacedName contains enough information to locate the referenced Kubernetes resource object in any
namespace.



_Appears in:_
- [DPUServiceCredentialRequestSpec](#dpuservicecredentialrequestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name of the object. |  |  |
| `namespace` _string_ | Namespace of the object, if not provided the object will be looked up in<br />the same namespace as the referring object |  |  |


#### OVN



OVN defines the configuration for OVN interface type



_Appears in:_
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `externalBridge` _string_ | ExternalBridge is the name of the OVN bridge | br-ovn |  |


#### ObjectMeta



ObjectMeta holds metadata like labels and annotations.



_Appears in:_
- [DPUServiceCredentialRequestSpec](#dpuservicecredentialrequestspec)
- [DPUServiceIPAMSpec](#dpuserviceipamspec)
- [DPUServiceNADSpec](#dpuservicenadspec)
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
| `virtualNetwork` _string_ | VirtualNetwork is the VirtualNetwork name in the same namespace |  |  |


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

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### ServiceChains







_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `upgradePolicy` _[UpgradePolicy](#upgradepolicy)_ | UpgradePolicy contains the configuration for the upgrade process | \{  \} |  |
| `switches` _[DPUDeploymentSwitch](#dpudeploymentswitch) array_ | Switches is the list of switches that form the service chain |  | MaxItems: 50 <br />MinItems: 1 <br /> |


#### ServiceConfiguration



ServiceConfiguration contains fields that are configured on the generated DPUService.



_Appears in:_
- [DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `helmChart` _[ServiceConfigurationHelmChart](#serviceconfigurationhelmchart)_ | HelmChart reflects the Helm related configuration. The user is supposed to configure values specific to that<br />DPUServiceConfiguration used in a DPUDeployment and should not specify values that could be shared across multiple<br />DPUDeployments using different DPUServiceConfigurations. These values are merged with values specified in the<br />DPUServiceTemplate. In case of conflict, the DPUServiceConfiguration values take precedence. |  |  |
| `serviceDaemonSet` _[DPUServiceConfigurationServiceDaemonSetValues](#dpuserviceconfigurationservicedaemonsetvalues)_ | ServiceDaemonSet contains settings related to the underlying DaemonSet that is part of the Helm chart |  |  |
| `deployInCluster` _boolean_ | DeployInCluster indicates if the DPUService Helm Chart will be deployed on the Host cluster. Default to false. |  |  |
| `configPorts` _[ConfigPorts](#configports)_ | ConfigPorts defines the desired state of port configurations for a DPUService.<br />This struct determines how ports are exposed from the DPU to the host cluster.<br />A DPUService can only have a single ServiceType across all ports. |  |  |


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
| `resources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#resourcelist-v1-core)_ | Resources specifies resources which are added to the ServiceDaemonSet. |  |  |


#### ServiceDef



ServiceDef Identifies the service and network for the ServiceInterface



_Appears in:_
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceID` _string_ | ServiceID is the DPU Service Identifier |  |  |
| `network` _string_ | Network is the Network Attachment Definition in the form of "namespace/name"<br />or just "name" if the namespace is the same as the ServiceInterface. |  |  |
| `interfaceName` _string_ | The interface name |  |  |
| `virtualNetwork` _string_ | VirtualNetwork is the VirtualNetwork name in the same namespace |  |  |


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
| `template` _[ServiceInterfaceSpecTemplate](#serviceinterfacespectemplate)_ | Template holds the template for the serviceInterfaceSpec |  |  |


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
| `ovn` _[OVN](#ovn)_ | The OVN definition |  |  |


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

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### ServiceInterfaceTemplate



ServiceInterfaceTemplate contains the information related to an interface of the DPUService



_Appears in:_
- [DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the interface |  | MaxLength: 28 <br />MinLength: 1 <br /> |
| `network` _string_ | Network is the Network Attachment Definition in the form of "namespace/name"<br />or just "name" if the namespace is the same as the namespace the pod is running. |  |  |
| `virtualNetwork` _string_ | VirtualNetwork is the VirtualNetwork name in the same namespace |  |  |


#### Switch



Switch defines the switch configuration



_Appears in:_
- [ServiceChainSpec](#servicechainspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ports` _[Port](#port) array_ | Ports of the switch |  | MaxItems: 50 <br />MinItems: 1 <br /> |
| `serviceMTU` _integer_ | ServiceMTU of the switch<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1000 <br /> |


#### UpgradePolicy







_Appears in:_
- [DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)
- [ServiceChains](#servicechains)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `applyNodeEffect` _boolean_ | ApplyNodeEffect specifies if the node effect should be applied during the<br />upgrade. It signals the reconciler that this object upgrade is disruptive.<br />Hence a new revision of the object should be created and node effect should<br />be applied. | true |  |


#### VF



VF defines the VF configuration



_Appears in:_
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vfID` _integer_ | The VF ID |  |  |
| `pfID` _integer_ | The PF ID |  |  |
| `parentInterfaceRef` _string_ | The parent interface reference<br />TODO: Figure out what this field is supposed to be |  |  |
| `virtualNetwork` _string_ | VirtualNetwork is the VirtualNetwork name in the same namespace |  |  |


#### VLAN



VLAN defines the VLAN configuration



_Appears in:_
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vlanID` _integer_ | The VLAN ID |  |  |
| `parentInterfaceRef` _string_ | The parent interface reference<br />TODO: Figure out what this field is supposed to be |  |  |



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
| `dhcp` _boolean_ | DHCP if set, enables DHCP for the network |  |  |
| `subnet` _string_ | Subnet is the network subnet in CIDR format to use for DHCP. the first IP in the subnet is the gateway. |  |  |
| `excludeIPs` _[ExcludeIPsEntry](#excludeipsentry) array_ | ExcludeIPs are the IPs to exclude from DHCP allocation. |  |  |


#### BridgedNetworkIPAMSpec



BridgedNetworkIPAMSpec contains IPAM configuration for bridged network



_Appears in:_
- [BridgedNetworkSpec](#bridgednetworkspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipv4` _[BridgedNetworkIPAMIPv4Spec](#bridgednetworkipamipv4spec)_ | IPv4 contains the IPv4 IPAM configuration |  |  |


#### BridgedNetworkSpec



BridgedNetworkSpec contains configuration for bridged network



_Appears in:_
- [DPUVirtualNetworkSpec](#dpuvirtualnetworkspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipam` _[BridgedNetworkIPAMSpec](#bridgednetworkipamspec)_ | IPAM contains the IPAM configuration for the bridged network |  |  |


#### DPUVPC



DPUVPC is the Schema for the dpuvpc API



_Appears in:_
- [DPUVPCList](#dpuvpclist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVPC` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUVPCSpec](#dpuvpcspec)_ |  |  |  |
| `status` _[DPUVPCStatus](#dpuvpcstatus)_ |  |  |  |


#### DPUVPCList



DPUVPCList contains a list of DPUVPC





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVPCList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUVPC](#dpuvpc) array_ |  |  |  |


#### DPUVPCSpec



DPUVPCSpec defines the desired state of DPUVPCSpec



_Appears in:_
- [DPUVPC](#dpuvpc)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `tenant` _string_ | Tenant which owns the VPC. |  | MinLength: 1 <br /> |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | NodeSelector Selects the DPU Nodes with specific labels which belong to this VPC. |  |  |
| `isolationClassName` _string_ | IsolationClassName is the name of the isolation class to use for the VPC |  | MinLength: 1 <br /> |
| `interNetworkAccess` _boolean_ | InterNetworkAccess defines if virtual networks within the VPC are routed or not.<br />if set to false, communication between virtual networks is not allowed. |  |  |


#### DPUVPCStatus



DPUVPCStatus defines the observed state of DPUVPC



_Appears in:_
- [DPUVPC](#dpuvpc)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `virtualNetworks` _[VirtualNetworkStatus](#virtualnetworkstatus) array_ | VirtualNetworks contains the virtual networks that belong to this VPC |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### DPUVirtualNetwork



DPUVirtualNetwork is the Schema for the dpuvirtualnetwork API



_Appears in:_
- [DPUVirtualNetworkList](#dpuvirtualnetworklist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVirtualNetwork` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DPUVirtualNetworkSpec](#dpuvirtualnetworkspec)_ |  |  |  |
| `status` _[DPUVirtualNetworkStatus](#dpuvirtualnetworkstatus)_ |  |  |  |


#### DPUVirtualNetworkList



DPUVirtualNetworkList contains a list of DPUVirtualNetwork





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUVirtualNetworkList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUVirtualNetwork](#dpuvirtualnetwork) array_ |  |  |  |


#### DPUVirtualNetworkSpec



DPUVirtualNetworkSpec defines the desired state of DPUVirtualNetworkSpec



_Appears in:_
- [DPUVirtualNetwork](#dpuvirtualnetwork)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#labelselector-v1-meta)_ | NodeSelector Selects the DPU Nodes with specific labels which can belong to the virtual network. |  |  |
| `vpcName` _string_ | vpcName is the name of the DPUVPC the virtual network belongs within the same namespace. |  |  |
| `type` _[NetworkType](#networktype)_ | Type of the virtual network |  | Enum: [Bridged] <br /> |
| `externallyRouted` _boolean_ | ExternallyRouted defines if the virtual network can be routed externally |  |  |
| `masquerade` _boolean_ | Masquerade defines if the virtual network should masquerade the traffic before egressing to external networks.<br />valid only if ExternallyRouted is true | true |  |
| `bridgedNetwork` _[BridgedNetworkSpec](#bridgednetworkspec)_ | BridgedNetwork contains the bridged network configuration |  |  |


#### DPUVirtualNetworkStatus



DPUVirtualNetworkStatus defines the observed state of DPUVirtualNetwork



_Appears in:_
- [DPUVirtualNetwork](#dpuvirtualnetwork)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#condition-v1-meta) array_ | Conditions reflect the status of the object |  |  |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last time it was patched. |  |  |


#### ExcludeIPsEntry







_Appears in:_
- [BridgedNetworkIPAMIPv4Spec](#bridgednetworkipamipv4spec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ip` _string_ | IP is the IP address to exclude from DHCP allocation. must be part for the virtual network subnet. |  |  |
| `range` _[RangeEntry](#rangeentry)_ | Range is the range of IP addresses to exclude from DHCP allocation. must be part for the virtual network subnet. |  |  |


#### IsolationClass



IsolationClass is the Schema for the isolationclass API



_Appears in:_
- [IsolationClassList](#isolationclasslist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `IsolationClass` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[IsolationClassSpec](#isolationclassspec)_ |  |  |  |
| `status` _[IsolationClassStatus](#isolationclassstatus)_ |  |  |  |


#### IsolationClassList



IsolationClassList contains a list of IsolationClass





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `vpc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `IsolationClassList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[IsolationClass](#isolationclass) array_ |  |  |  |


#### IsolationClassSpec



IsolationClassSpec defines the configuration of IsolationClass



_Appears in:_
- [IsolationClass](#isolationclass)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `provisioner` _string_ | Provisioner indicates the type of the provisioner. |  |  |
| `parameters` _object (keys:string, values:string)_ | Parameters holds the parameters for the provisioner |  |  |


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
| `name` _string_ | the name of the virtual network |  |  |


