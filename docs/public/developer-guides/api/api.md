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

Package v1alpha1 contains API Schema definitions for the noderesources
v1alpha1 API group

### Resource Types
- [NodeSRIOVDevicePluginConfig](#nodesriovdevicepluginconfig)
- [NodeSRIOVDevicePluginConfigList](#nodesriovdevicepluginconfiglist)



#### DevicePluginResource



DevicePluginResource defines a single device plugin resource configuration.



_Appears in:_
- [NodeSRIOVDevicePluginConfigSpec](#nodesriovdevicepluginconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the endpoint resource name for the device plugin. Should contain<br />only alphanumeric characters, underscores and hyphens. The full extended<br />resource name will be constructed as resource-prefix/name.<br />Example: pods_vf, ovnk_mgmt_vf, pods_sf |  | MinLength: 1 <br />Pattern: `^[a-zA-Z0-9_-]+$` <br />Required: \{\} <br /> |
| `resourcePrefix` _string_ | ResourcePrefix is the resource prefix used by the device plugin to prefix<br />the resource name. If not set, the default resource prefix will be used. |  | Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br />Optional: \{\} <br /> |
| `type` _[DevicePluginResourceType](#devicepluginresourcetype)_ | Type specifies the type of the device plugin resource. |  | Enum: [vf sf] <br />Required: \{\} <br /> |
| `options` _[DevicePluginResourceOptions](#devicepluginresourceoptions)_ | Options contains additional options for the device plugin resource. |  | Optional: \{\} <br /> |
| `ranges` _[FunctionRange](#functionrange) array_ | Ranges specifies the function ranges on PFs to be included in this<br />resource. For type sf, each range must set both start and end. |  | MaxItems: 1024 <br />MinItems: 1 <br />Required: \{\} <br /> |


#### DevicePluginResourceOptions



DevicePluginResourceOptions contains additional options for a
device plugin resource.



_Appears in:_
- [DevicePluginResource](#devicepluginresource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `isRdma` _boolean_ | IsRdma indicates whether RDMA is enabled for this resource. |  | Optional: \{\} <br /> |
| `needVhostNet` _boolean_ | NeedVhostNet mounts /dev/vhost-net and /dev/net/tun with the allocated<br />devices. Defaults to false. |  | Optional: \{\} <br /> |


#### DevicePluginResourceType

_Underlying type:_ _string_

DevicePluginResourceType specifies the type of the device plugin resource.

_Validation:_
- Enum: [vf sf]

_Appears in:_
- [DevicePluginResource](#devicepluginresource)

| Field | Description |
| --- | --- |
| `vf` | DevicePluginResourceTypeVF represents a Virtual Function resource.<br /> |
| `sf` | DevicePluginResourceTypeSF represents a Scalable Function resource.<br /> |


#### FunctionRange



FunctionRange defines a range of functions (Virtual Functions or Scalable
Functions) on a Physical Function.



_Appears in:_
- [DevicePluginResource](#devicepluginresource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pfIndex` _integer_ | PFIndex is the index of the Physical Function. |  | Minimum: 0 <br />Required: \{\} <br /> |
| `start` _integer_ | Start is the starting function index (inclusive).<br />If not set, the range starts from index 0.<br />Required when type is sf. SF ranges are contiguous: every index from<br />start to end (inclusive) must exist on the PF. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `end` _integer_ | End is the ending function index (inclusive). If not set, the range<br />extends to the last function of this type on the PF. Required when type<br />is sf. SF ranges are contiguous: every index from start to end<br />(inclusive) must exist on the PF. |  | Minimum: 0 <br />Optional: \{\} <br /> |


#### NodeSRIOVDevicePluginConfig



NodeSRIOVDevicePluginConfig is the Schema for the
nodesriovdevicepluginconfigs API



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



NodeSRIOVDevicePluginConfigList contains a list of
NodeSRIOVDevicePluginConfig





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `noderesources.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `NodeSRIOVDevicePluginConfigList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[NodeSRIOVDevicePluginConfig](#nodesriovdevicepluginconfig) array_ |  |  |  |


#### NodeSRIOVDevicePluginConfigSpec



NodeSRIOVDevicePluginConfigSpec defines the desired state of
NodeSRIOVDevicePluginConfig



_Appears in:_
- [NodeSRIOVDevicePluginConfig](#nodesriovdevicepluginconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `devicePluginResources` _[DevicePluginResource](#devicepluginresource) array_ | DevicePluginResources is the list of device plugin<br />resource configurations. |  | MaxItems: 64 <br />MinItems: 1 <br />Required: \{\} <br /> |


#### NodeSRIOVDevicePluginConfigStatus



NodeSRIOVDevicePluginConfigStatus defines the observed state of
NodeSRIOVDevicePluginConfig



_Appears in:_
- [NodeSRIOVDevicePluginConfig](#nodesriovdevicepluginconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions exposes the current state of the NodeSRIOVDevicePluginConfig. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |



## operator.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the operator v1alpha1
API group

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
- [CoreDNSConfiguration](#corednsconfiguration)
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
- [VaultKMSConfiguration](#vaultkmsconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |


#### BaseControllerConfig



BaseControllerConfig provides common configuration fields that can be
embedded by all controller configurations to reduce code duplication.



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
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `installer` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Installer contains the configuration for the CNI-Installer component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |


#### CertManagementAnchor



CertManagementAnchor identifies the cert-manager issuer a certificate
authority is anchored to. Every field is recorded as resolved, with the
defaults cert-manager applies already filled in, so that comparing two
anchors cannot turn on whether one of them left a field out.



_Appears in:_
- [CertManagementStatus](#certmanagementstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name of the issuer. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `kind` _string_ | Kind of the issuer, either Issuer for one in the namespace of the<br />DPFOperatorConfig or ClusterIssuer for a cluster scoped one. |  | Enum: [Issuer ClusterIssuer] <br />Optional: \{\} <br /> |
| `group` _string_ | Group of the issuer, which is the API group of cert-manager. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### CertManagementConfiguration



CertManagementConfiguration configures the trust bundle that the consumers
of the provisioning certificates of DPF validate peers against.

The certificate authority those certificates chain up to is not configured
here. It is chosen once for the whole PKI through the
certificateAuthority.issuerRef value of the dpf-operator Helm chart, which
the chart stamps onto the webhook intermediate CA it creates. DPF reads the
anchor back from there, so the platform and webhook chains cannot end up
anchored to different authorities, and the serving certificates of the
webhooks are covered by the same choice even though the chart creates them
before a DPFOperatorConfig exists.



_Appears in:_
- [SecurityConfiguration](#securityconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `trustBundleConfigMapName` _string_ | TrustBundleConfigMapName is the ConfigMap holding the CA certificate(s)<br />that DPF components validate their peers against. Who maintains its<br />content depends on the authority the PKI is anchored to, see the<br />External Certificate Authority guide.<br />The name is fixed once the config exists, enforced by a rule on the spec<br />rather than on this field, which cannot see a rename that adds or<br />removes it. | dpf-ca-trust-bundle | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br />Optional: \{\} <br /> |


#### CertManagementStatus



CertManagementStatus records what the operator has rolled out for the
provisioning PKI, as opposed to what it is being asked to roll out.



_Appears in:_
- [SecurityStatus](#securitystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `anchor` _[CertManagementAnchor](#certmanagementanchor)_ | Anchor is the certificate authority the provisioning PKI has been rolled<br />out against, recorded once the platform intermediate CA has been applied<br />under it and every peer that has to validate the chain below it trusts<br />that chain.<br />It is what the anchor chosen in the dpf-operator Helm chart is compared<br />against, so a change of authority is reported for as long as the rollout<br />of it is outstanding rather than only by the reconcile that<br />first observed it.<br />Nothing rolled out yet is reported by leaving it out altogether. |  | Optional: \{\} <br /> |
| `trustBundleHash` _string_ | TrustBundleHash is the hash the CA trust bundle carried when Anchor was<br />recorded as rolled out, refreshed by every reconcile that rolls the same<br />authority out again.<br />It is the reference point a change of authority is measured against.<br />cert-manager reissues the platform intermediate CA on its own schedule,<br />so the bundle published at the moment such a change is observed is still<br />the one every peer already trusts, and convergence measured against it<br />would call the rotation over before a single peer had seen the new chain.<br />Recording it stops there for as long as the rotation is outstanding,<br />which leaves it holding exactly that bundle.<br />No bundle published yet is reported by leaving it out altogether. |  | MaxLength: 128 <br />MinLength: 1 <br />Optional: \{\} <br /> |






#### ConfigMapKeyRef



ConfigMapKeyRef selects a single key from a ConfigMap living in the same
namespace as the DPFOperatorConfig.



_Appears in:_
- [VaultKMSTLS](#vaultkmstls)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the ConfigMap. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `key` _string_ | Key is the key within the ConfigMap data to select. |  | MinLength: 1 <br />Required: \{\} <br /> |




#### CoreDNSConfiguration



CoreDNSConfiguration is the configuration for CoreDNS serving DPU clusters.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `deployment` _[CoreDNSDeployment](#corednsdeployment)_ | Deployment contains the configuration for the CoreDNS deployment.<br />It contains the image for the CoreDNS container and its resource<br />requirements. |  | Optional: \{\} <br /> |
| `upstreamNameservers` _string_ | UpstreamNameservers is passed to the CoreDNS forward plugin for names<br />outside the cluster domain. It accepts the same space-separated<br />nameserver or resolv.conf path syntax as the CoreDNS chart. |  | MinLength: 1 <br />Optional: \{\} <br /> |


#### CoreDNSDeployment



CoreDNSDeployment contains the configuration for the CoreDNS
deployment container.



_Appears in:_
- [CoreDNSConfiguration](#corednsconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the<br />component. This field is optional, and if not set, the component will use<br />the default resource. |  | Optional: \{\} <br /> |


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
| `security` _[SecurityConfiguration](#securityconfiguration)_ | Security groups configuration for security-related components managed by<br />the DPF Operator. |  | Optional: \{\} <br /> |
| `imagePullSecrets` _string array_ | List of secret names which are used to pull images for DPF system<br />components and DPUServices. These secrets must be in the same namespace<br />as the DPF Operator Config and should be created before the config is<br />created. System reconciliation will not proceed until these secrets<br />are available. |  | Optional: \{\} <br /> |
| `deploymentMode` _[DeploymentMode](#deploymentmode)_ | DeploymentMode selects zero-trust vs host-trusted deployment alignment.<br />Required: operators must set this explicitly; provisioning controllers<br />propagate this to DPU.status.deploymentMode. |  | Enum: [zero-trust host-trusted] <br />Required: \{\} <br /> |
| `dpuServiceController` _[DPUServiceControllerConfiguration](#dpuservicecontrollerconfiguration)_ | DPUServiceController is the configuration for the DPUServiceController |  | Optional: \{\} <br /> |
| `provisioningController` _[ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)_ | ProvisioningController is the configuration for the<br />ProvisioningController |  |  |
| `serviceSetController` _[ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)_ | ServiceSetController is the configuration for the ServiceSetController |  | Optional: \{\} <br /> |
| `dpuDetector` _[DPUDetectorConfiguration](#dpudetectorconfiguration)_ | DPUDetector is the configuration for the DPUDetector. |  | Optional: \{\} <br /> |
| `multus` _[MultusConfiguration](#multusconfiguration)_ | Multus is the configuration for Multus |  | Optional: \{\} <br /> |
| `sriovDevicePlugin` _[SRIOVDevicePluginConfiguration](#sriovdevicepluginconfiguration)_ | SRIOVDevicePlugin is the configuration for the SRIOVDevicePlugin |  | Optional: \{\} <br /> |
| `flannel` _[FlannelConfiguration](#flannelconfiguration)_ | Flannel is the configuration for Flannel |  | Optional: \{\} <br /> |
| `ovsCNI` _[OVSCNIConfiguration](#ovscniconfiguration)_ | OVSCNI is the configuration for OVSCNI<br />Deprecated: OVS CNI is installed by CNIInstaller. Remove in 27.1. |  | Optional: \{\} <br /> |
| `nvipam` _[NVIPAMConfiguration](#nvipamconfiguration)_ | NVIPAM is the configuration for NVIPAM |  | Optional: \{\} <br /> |
| `cniInstaller` _[CNIInstallerConfiguration](#cniinstallerconfiguration)_ | CNIInstaller is the configuration for the cni-installer |  | Optional: \{\} <br /> |
| `coreDNS` _[CoreDNSConfiguration](#corednsconfiguration)_ | CoreDNS is the configuration for CoreDNS serving Kamaji DPU clusters with<br />a Keepalived endpoint. |  | Optional: \{\} <br /> |
| `sfcController` _[SFCControllerConfiguration](#sfccontrollerconfiguration)_ | SFCController is the configuration for the SFCController |  | Optional: \{\} <br /> |
| `kamajiClusterManager` _[KamajiClusterManagerConfiguration](#kamajiclustermanagerconfiguration)_ | KamajiClusterManager is the configuration for the kamaji-cluster-manager |  | Optional: \{\} <br /> |
| `staticClusterManager` _[StaticClusterManagerConfiguration](#staticclustermanagerconfiguration)_ | StaticClusterManager is the configuration for the static-cluster-manager |  | Optional: \{\} <br /> |
| `nodeSRIOVDevicePluginController` _[NodeSRIOVDevicePluginControllerConfiguration](#nodesriovdeviceplugincontrollerconfiguration)_ | NodeSRIOVDevicePluginController is the configuration for the<br />NodeSRIOVDevicePlugin controller. This controller manages per-node SRIOV<br />device plugin pods based on DPU configurations. The controller is<br />disabled by default. |  | Optional: \{\} <br /> |


#### DPFOperatorConfigStatus



DPFOperatorConfigStatus defines the observed state of DPFOperatorConfig



_Appears in:_
- [DPFOperatorConfig](#dpfoperatorconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions exposes the current state of the OperatorConfig. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |
| `version` _string_ | Version is the version of the DPF Operator that is currently deployed. |  | Optional: \{\} <br /> |
| `targetVersion` _string_ | TargetVersion is the version of the DPF Operator that is being deployed.<br />It differs from Version while an upgrade is in progress. |  | Optional: \{\} <br /> |
| `security` _[SecurityStatus](#securitystatus)_ | Security records what the operator has rolled out for the<br />security-related components it manages. It is grouped the way<br />spec.security groups their configuration, so that what was asked for and<br />what is in effect are read at the same path. |  | Optional: \{\} <br /> |


#### DPUClusterImagesConfiguration



DPUClusterImagesConfiguration configures the images of Kamaji-managed
DPU clusters.



_Appears in:_
- [KamajiClusterManagerConfiguration](#kamajiclustermanagerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `hostedControlPlaneRepository` _string_ | HostedControlPlaneRepository is the image repository the hosted control<br />plane images of the DPU cluster are pulled from: kube-apiserver,<br />kube-controller-manager and kube-scheduler. For example, for the<br />repository example.com/k8s, kube-apiserver is pulled from<br />example.com/k8s/kube-apiserver. This is registry.k8s.io by default. |  | MaxLength: 255 <br />Pattern: `^(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*$` <br />Optional: \{\} <br /> |
| `addonsRepository` _string_ | AddonsRepository is the image repository the addon images of the DPU<br />cluster, e.g. kube-proxy, are pulled from. For example, for the<br />repository example.com/k8s, kube-proxy is pulled from<br />example.com/k8s/kube-proxy. The Kamaji defaults are used if this is<br />not set. |  | MaxLength: 255 <br />Pattern: `^(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*$` <br />Optional: \{\} <br /> |


#### DPUDetectorConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the DPUDetector Container.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `daemon` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `daemon` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Daemon contains the configuration for the DPU Detector component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |


#### DPUServiceControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `disableTracing` _boolean_ | DisableTracing stops the controller from exporting trace spans although<br />tracing is configured. The controller then also stops passing the trace<br />context on to the objects it creates, so a trace ends at this controller. |  | Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the DPUService controller.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the DPU Service controller<br />component. It contains the image for the controller and its resource<br />requirements. |  | Optional: \{\} <br /> |
| `disableDPUReadyTaints` _boolean_ | DisableDPUReadyTaints is a full taint kill-switch for the DPUReady<br />controller. When set to true, no taint managed by this controller<br />(NoSchedule for critical DPUServices, or NoExecute for HostNetworkReady)<br />is added, removed, or otherwise touched on host worker nodes. |  | Optional: \{\} <br /> |
| `disableHostNetworkReadyNoExecuteTaints` _boolean_ | DisableHostNetworkReadyNoExecuteTaints disables NoExecute taints on host<br />worker nodes based on HostNetworkReady. When unset or true, the feature<br />is disabled (safe default). Set to false to enable NoExecute tainting<br />when HostNetworkReady != True. |  | Optional: \{\} <br /> |
| `defaultServiceDaemonSetMaxUnavailable` _integer_ | DefaultServiceDaemonSetMaxUnavailable specifies the fallback<br />maxUnavailable for ServiceDaemonSet rolling updates. It defaults to 30000<br />and is applied when serviceDaemonSet.updateStrategy is omitted or its<br />rollingUpdate.maxUnavailable is unset and maxSurge is unset or zero. If<br />maxSurge is nonzero, an omitted maxUnavailable is left unset. Explicit<br />maxUnavailable values and OnDelete strategies are preserved. Lower this<br />value to limit the number of unavailable pods during updates. |  | Minimum: 1 <br />Optional: \{\} <br /> |


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
- [VaultKMSConfiguration](#vaultkmsconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the<br />component. This field is optional, and if not set, the component will use<br />the default resource. |  | Optional: \{\} <br /> |


#### DeploymentMode

_Underlying type:_ _string_

DeploymentMode describes the cluster deployment model for DPU provisioning
(zero-trust vs host-trusted).

_Validation:_
- Enum: [zero-trust host-trusted]

_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description |
| --- | --- |
| `zero-trust` | DeploymentModeZeroTrust requires<br />provisioningController.installInterface.installViaRedfish<br /> |
| `host-trusted` | DeploymentModeHostTrusted allows<br />provisioningController.installInterface.installViaHostAgent, or<br />installViaGNOI<br /> |




#### EtcdEncryptionAtRestConfiguration



EtcdEncryptionAtRestConfiguration is the per-cluster
encryption-at-rest selector for Kamaji clusters.



_Appears in:_
- [KamajiClusterManagerConfiguration](#kamajiclustermanagerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `provider` _[EtcdEncryptionAtRestProvider](#etcdencryptionatrestprovider)_ | Provider selects the encryption-at-rest provider. |  | Enum: [staticKey vaultKMS] <br />Required: \{\} <br /> |
| `staticKey` _[StaticKeyConfiguration](#statickeyconfiguration)_ | StaticKey configures the staticKey provider. It is required when provider<br />is staticKey and must not be set otherwise. |  | Optional: \{\} <br /> |


#### EtcdEncryptionAtRestProvider

_Underlying type:_ _string_

EtcdEncryptionAtRestProvider selects the etcd encryption-at-rest provider.

_Validation:_
- Enum: [staticKey vaultKMS]

_Appears in:_
- [EtcdEncryptionAtRestConfiguration](#etcdencryptionatrestconfiguration)

| Field | Description |
| --- | --- |
| `staticKey` | EtcdEncryptionProviderStaticKey encrypts etcd data with an AES-GCM key<br />rendered inline into the encryption config.<br /> |
| `vaultKMS` | EtcdEncryptionProviderVaultKMS encrypts etcd data via the KMS v2 plugin<br />served by the vaultKMS component.<br /> |


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
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `cni` _[FlannelCNI](#flannelcni)_ | CNI is the configuration for the Flannel CNI component.<br />It contains the image for the CNI init container.<br />Note: The resources for the CNI container are not configurable. |  | Optional: \{\} <br /> |
| `daemon` _[FlannelDaemon](#flanneldaemon)_ | Daemon is the configuration for the Flannel Daemon component. It contains<br />the image for the Flannel Daemon container and its resource requirements. |  | Optional: \{\} <br /> |
| `image` _[FlannelImages](#flannelimages)_ | Images overrides the container images used by flannel<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new fields `cni` and `daemon` instead. |  | Optional: \{\} <br /> |
| `podCIDR` _string_ | PodCIDR is the pod cidr for flannel. |  | Optional: \{\} <br /> |


#### FlannelDaemon







_Appears in:_
- [FlannelConfiguration](#flannelconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the<br />component. This field is optional, and if not set, the component will use<br />the default resource. |  | Optional: \{\} <br /> |


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
- [CoreDNSConfiguration](#corednsconfiguration)
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
- [CoreDNSConfiguration](#corednsconfiguration)
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
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |




#### Image

_Underlying type:_ _string_

Image is a reference to a container image. If neither a tag nor a digest is
set, the tag and digest of the component's default image are used.

_Validation:_
- Pattern: `^((?:(?:(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]{0,127}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]{32,}))?$`

_Appears in:_
- [CoreDNSDeployment](#corednsdeployment)
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



ImageComponentConfig provides common configuration fields that can be
embedded by all component configurations to reduce code duplication.



_Appears in:_
- [CoreDNSDeployment](#corednsdeployment)
- [DefaultOverridesConfiguration](#defaultoverridesconfiguration)
- [FlannelCNI](#flannelcni)
- [FlannelDaemon](#flanneldaemon)
- [KataContainersConfiguration](#katacontainersconfiguration)
- [NVIPAMController](#nvipamcontroller)
- [NVIPAMNode](#nvipamnode)
- [SPIFFECSIDriverConfiguration](#spiffecsidriverconfiguration)

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
| `discoveredDPUDeviceBMCFactoryResetPolicy` _[BMCFactoryResetPolicy](#bmcfactoryresetpolicy)_ | DiscoveredDPUDeviceBMCFactoryResetPolicy is the BMC factory reset policy<br />DPUDiscovery sets on the DPUDevices it creates. It is applied at creation<br />time only: changing it does not affect DPUDevices that already exist, and<br />it is not consulted when a DPUDevice is reconciled. When unset, the<br />discovery controller uses OnInitialization. |  | Enum: [OnInitialization Never] <br />Optional: \{\} <br /> |


#### KamajiClusterManagerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the Kamaji Cluster Manager.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the Kamaji Cluster Manager<br />component. It contains the image for the controller and its resource<br />requirements. |  | Optional: \{\} <br /> |
| `etcdEncryptionAtRest` _[EtcdEncryptionAtRestConfiguration](#etcdencryptionatrestconfiguration)_ | EtcdEncryptionAtRest configures encryption at rest for the etcd datastore<br />of Kamaji-managed DPU clusters. The provider selection is applied only<br />when a Kamaji cluster is first created and is not changed for existing<br />clusters. |  | Optional: \{\} <br /> |
| `dpuClusterImages` _[DPUClusterImagesConfiguration](#dpuclusterimagesconfiguration)_ | DPUClusterImages configures the images of Kamaji-managed DPU clusters. It<br />is applied only when a Kamaji cluster is first created and is not changed<br />for existing clusters. |  | Optional: \{\} <br /> |


#### KataContainersConfiguration







_Appears in:_
- [SecurityConfiguration](#securityconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
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
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `daemon` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Daemon contains the configuration for the kube-state-metrics component.<br />It contains the image for kube-state-metrics and its resource<br />requirements. |  | Optional: \{\} <br /> |


#### MonitoringConfiguration



MonitoringConfiguration defines the configuration for monitoring resources.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable controls whether monitoring resources are installed. When enabled<br />(default), the controller:<br />- Creates ServiceMonitors for Kamaji clusters to scrape<br />  control-plane metrics.<br />- Deploys kube-state-metrics as a DPUService to expose metrics for<br />  custom resources.<br />- Deploys node-problem-detector as a DaemonSet on DPU nodes to detect and<br />  report node-level problems.<br />- Deploys opentelemetry-collector as a DaemonSet on DPU nodes to collect<br />  and forward logs. |  | Optional: \{\} <br /> |
| `kubeStateMetrics` _[KubeStateMetricsConfiguration](#kubestatemetricsconfiguration)_ | KubeStateMetrics is the configuration for kube-state-metrics |  | Optional: \{\} <br /> |
| `nodeProblemDetector` _[NodeProblemDetectorConfiguration](#nodeproblemdetectorconfiguration)_ | NodeProblemDetector is the configuration for node-problem-detector |  | Optional: \{\} <br /> |
| `openTelemetryCollector` _[OpenTelemetryCollectorConfiguration](#opentelemetrycollectorconfiguration)_ | OpenTelemetryCollector is the configuration for opentelemetry-collector |  | Optional: \{\} <br /> |
| `tracing` _[TracingConfiguration](#tracingconfiguration)_ | Tracing configures how the DPF controllers export trace spans. It applies<br />even when disable is true. If not specified, the controllers of the<br />management cluster export none. |  | Optional: \{\} <br /> |


#### MultusConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the Multus Container.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `cni` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `cni` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | CNI contains the configuration for the Multus CNI component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |


#### NVIPAMConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the NVIPAM controller.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[NVIPAMController](#nvipamcontroller)_ | Controller contains the configuration for the NVIPAM controller<br />component. It contains the image for the controller and its resource<br />requirements. |  | Optional: \{\} <br /> |
| `node` _[NVIPAMNode](#nvipamnode)_ | Node contains the configuration for the NVIPAM node component.<br />It contains the image for the node and its resource requirements. |  |  |


#### NVIPAMController







_Appears in:_
- [NVIPAMConfiguration](#nvipamconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the<br />component. This field is optional, and if not set, the component will use<br />the default resource. |  | Optional: \{\} <br /> |


#### NVIPAMNode







_Appears in:_
- [NVIPAMConfiguration](#nvipamconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ |  |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the<br />component. This field is optional, and if not set, the component will use<br />the default resource. |  | Optional: \{\} <br /> |


#### Networking



Networking defines the networking configuration for the system components.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `controlPlaneMTU` _integer_ | ControlPlaneMTU is the MTU value to be set on the management network. In<br />zero-trust mode this value is applied to the DPU OOB interface<br />(oob_net0), which does not support jumbo frames; it must not exceed 1500<br />when deploymentMode is zero-trust. The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1280 <br />Optional: \{\} <br /> |
| `highSpeedMTU` _integer_ | HighSpeedMTU is the MTU value to be set on the high-speed interface.<br />The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1280 <br />Optional: \{\} <br /> |
| `dpuNodeOOBBridgeName` _string_ | DPUNodeOOBBridgeName is the name of the Linux bridge on the host used for<br />out-of-band DPU management traffic. If not specified, defaults to<br />"br-dpu". This setting applies only to host-trusted deployments. | br-dpu | MaxLength: 15 <br />MinLength: 1 <br />Pattern: `^[a-z][a-z0-9-]*$` <br />Optional: \{\} <br /> |


#### NodeProblemDetectorConfiguration







_Appears in:_
- [MonitoringConfiguration](#monitoringconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `daemon` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Daemon contains the configuration for the node-problem-detector<br />component. It contains the image for node-problem-detector and its<br />resource requirements. |  | Optional: \{\} <br /> |


#### NodeSRIOVDevicePluginControllerConfiguration



NodeSRIOVDevicePluginControllerConfiguration is the configuration for the
NodeSRIOVDevicePlugin controller. This controller manages per-node SRIOV
device plugin pods based on DPU configurations. The controller is disabled
by default.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `disableTracing` _boolean_ | DisableTracing stops the controller from exporting trace spans although<br />tracing is configured. The controller then also stops passing the trace<br />context on to the objects it creates, so a trace ends at this controller. |  | Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the NodeSRIOVDevicePlugin<br />controller component. It contains the image for the controller and its<br />resource requirements. |  | Optional: \{\} <br /> |
| `devicePlugin` _[NodeSRIOVDevicePluginSettings](#nodesriovdevicepluginsettings)_ | DevicePlugin contains the configuration for the SRIOV device plugin pods<br />managed by this controller. |  | Optional: \{\} <br /> |


#### NodeSRIOVDevicePluginSettings



NodeSRIOVDevicePluginSettings contains configuration for the SRIOV device
plugin pods managed by the NodeSRIOVDevicePlugin controller.



_Appears in:_
- [NodeSRIOVDevicePluginControllerConfiguration](#nodesriovdeviceplugincontrollerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _[Image](#image)_ | Image overrides the container image for the SRIOV device plugin. If<br />neither a tag nor a digest is set, the tag and digest of the default<br />image are used. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `initImage` _[Image](#image)_ | InitImage overrides the container image for the init container that<br />generates device plugin configuration. If neither a tag nor a digest is<br />set, the tag and digest of the default image are used. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `defaultResourcePrefix` _string_ | DefaultResourcePrefix is the default resource prefix for the SRIOV device<br />plugin resources. Defaults to "nvidia.com". |  | Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br />Optional: \{\} <br /> |


#### OVSCNIConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the OVS CNI.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `cni` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `cni` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | CNI contains the configuration for the OVS CNI component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |


#### OpenTelemetryCollectorCASecretReference



OpenTelemetryCollectorCASecretReference references a Secret that contains the
PEM-encoded CA certificate bundle used to verify the endpoint's TLS
certificate.



_Appears in:_
- [OpenTelemetryCollectorExportConfiguration](#opentelemetrycollectorexportconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the Secret holding the CA certificate bundle. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `namespace` _string_ | Namespace is the namespace of the Secret holding the CA certificate<br />bundle. If unset, the DPFOperatorConfig namespace is used. |  | MaxLength: 63 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `key` _string_ | Key is the Secret data key that holds the PEM-encoded CA certificate<br />bundle. If unset, "ca.crt" is used, matching the key that cert-manager<br />writes. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### OpenTelemetryCollectorConfiguration







_Appears in:_
- [MonitoringConfiguration](#monitoringconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `daemon` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Daemon contains the configuration for the opentelemetry-collector<br />component. It contains the image for opentelemetry-collector and its<br />resource requirements. |  | Optional: \{\} <br /> |
| `logging` _[OpenTelemetryCollectorExportConfiguration](#opentelemetrycollectorexportconfiguration)_ | Logging contains the configuration for the opentelemetry-collector<br />logging component. If not specified, logging will not be streamed. |  | Optional: \{\} <br /> |
| `metrics` _[OpenTelemetryCollectorExportConfiguration](#opentelemetrycollectorexportconfiguration)_ | Metrics contains the configuration for the opentelemetry-collector<br />metrics component. If not specified, metrics will not be streamed from<br />DPU clusters. |  | Optional: \{\} <br /> |
| `tracing` _[OpenTelemetryCollectorExportConfiguration](#opentelemetrycollectorexportconfiguration)_ | Tracing is where the DPU clusters export their trace spans. The collector<br />forwards the spans it receives locally, and the sfc-controller sends its<br />own. The DPUs must reach it, so use the address of a management cluster<br />node, not a cluster-local Service. If not specified, none are exported. |  | Optional: \{\} <br /> |


#### OpenTelemetryCollectorExportConfiguration



OpenTelemetryCollectorExportConfiguration configures where and how an
opentelemetry signal (logs, metrics or traces) is exported. It is shared by
OpenTelemetryCollectorConfiguration.Logging (the DPU cluster
opentelemetry-collector's log export destination), .Metrics (its metric
export destination) and .Tracing (its trace export destination).



_Appears in:_
- [OpenTelemetryCollectorConfiguration](#opentelemetrycollectorconfiguration)
- [TracingConfiguration](#tracingconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpoint` _string_ | Endpoint is the OTLP endpoint data is sent to. This could be the<br />management cluster's opentelemetry-collector endpoint. If not specified,<br />nothing will be exported for this signal. For the http transport the<br />endpoint must include the scheme, e.g. "https://host:4318". For the grpc<br />transport the endpoint is "host:4317", optionally prefixed with a scheme<br />("https://host:4317") to enforce TLS. |  | Required: \{\} <br /> |
| `transport` _[OpenTelemetryCollectorTransport](#opentelemetrycollectortransport)_ | Transport is the OTLP transport used to export data to the endpoint. |  | Enum: [http grpc] <br />Optional: \{\} <br /> |
| `caSecretRef` _[OpenTelemetryCollectorCASecretReference](#opentelemetrycollectorcasecretreference)_ | CASecretRef references a Secret that contains the PEM-encoded CA<br />certificate bundle (under the "ca.crt" key) used to verify the endpoint's<br />TLS certificate. Set it when the endpoint serves a certificate issued by<br />a private CA. The Secret may live in any namespace, for example<br />alongside the endpoint's cert-manager Certificate; if its namespace is<br />empty, the DPFOperatorConfig namespace is used. If not specified, TLS<br />endpoints are verified against the system CA pool. Changes to the Secret<br />content are applied on the next reconciliation of the DPFOperatorConfig. |  | Optional: \{\} <br /> |


#### OpenTelemetryCollectorTransport

_Underlying type:_ _string_

OpenTelemetryCollectorTransport is the OTLP transport used to export data to
the endpoint.



_Appears in:_
- [OpenTelemetryCollectorExportConfiguration](#opentelemetrycollectorexportconfiguration)

| Field | Description |
| --- | --- |
| `http` | OpenTelemetryCollectorTransportHTTP exports data via OTLP/HTTP.<br /> |
| `grpc` | OpenTelemetryCollectorTransportGRPC exports data via OTLP/gRPC.<br /> |


#### Overrides



Overrides exposes a set of fields which impact the recommended behavior of
the DPF Operator. These fields should only be set for advanced use cases.
The fields here have no stability guarantees.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `paused` _boolean_ | Paused disables all reconciliation of the DPFOperatorConfig when set to<br />true. |  | Optional: \{\} <br /> |
| `dpuCNIBinPath` _string_ | DPUCNIBinPath is the path at which the CNI binaries will be installed to<br />on the DPU. This is /opt/cni/bin by default. This setting does not change<br />where kubelet is configured to use the CNI from. |  | Optional: \{\} <br /> |
| `dpuCNIPath` _string_ | DPUCNIConfigPath is the path to which the CNI config files will be<br />installed on the DPU. This is /etc/cni/net.d by default. This setting<br />does not change where kubelet is configured to read the CNI config from. |  | Optional: \{\} <br /> |
| `dpuOpenvSwitchRunPath` _string_ | DPUOpenvSwitchPath is the path at which the openvSwitch run directory can<br />be found on the DPU. This is /var/run/openvswitch by default. This<br />setting does not change where components are installed. Installation<br />location fixed in the BFB. |  | Optional: \{\} <br /> |
| `dpuOpenvSwitchBinPath` _string_ | DPUOpenvSwitchBinPath is the path at which the openvSwitch bin directory<br />can be found on the DPU node. This is /usr/bin/ by default. This setting<br />does not change where components are installed. Installation location<br />fixed in the BFB. |  | Optional: \{\} <br /> |
| `dpuOpenvSwitchSystemSharedPath` _string_ | DPUOpenvSwitchSystemSharedLibPath is the path at which the system lib<br />used by OVS components can be found on the DPU. This is /lib by default.<br />This setting does not change where components are installed. Installation<br />location fixed in the BFB. |  | Optional: \{\} <br /> |
| `flannelSkipCNIConfigInstallation` _boolean_ | FlannelSkipCNIConfigInstallation controls whether Flannel should skip CNI<br />config installation. This is true by default, meaning Flannel does not<br />manage its own CNI configuration. Set to false if you want Flannel to<br />install a CNI configuration. |  | Optional: \{\} <br /> |
| `dpuOpenvSwitchSystemSharedLib64Path` _string_ | DPUOpenvSwitchSystemSharedLib64Path is the path at which the system lib64<br />used by OVS components can be found on the DPU. If this field is not set,<br />no lib64 volume mount will be configured in the SFC Controller component.<br />This setting does not change where components are installed. Installation<br />location fixed in the BFB. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `dpuLinkerCachePath` _string_ | DPULinkerCachePath is the path on the DPU at which the prebuilt<br />dynamic-linker cache file can be found. When set, this file is mounted<br />read-only into the SFC Controller container so that host OVS binaries<br />can resolve shared libraries using the DPU's linker configuration. If<br />not set, no linker cache mount is added. This setting does not change<br />where components are installed. Installation location fixed in the BFB. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `dpuOptLibraryPath` _string_ | DPUOptLibraryPath is the path on the DPU at which an additional library<br />directory can be found. When set, this directory is mounted read-only<br />into the SFC Controller container. Useful on distributions that install<br />vendor libraries outside the standard paths (e.g. /usr/opt on RHCOS<br />BFB). If not set, no additional library directory is mounted. This<br />setting does not change where components are installed. Installation<br />location fixed in the BFB. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `kubernetesAPIServerVIP` _string_ | KubernetesAPIServerVIP is the VIP the Kubernetes API server is accessible<br />at. This setting enables specific underlying components deployed<br />directly or indirectly by the DPF Operator to reach the Kubernetes API<br />Server when the ClusterIP Kubernetes Service is not functional. If set,<br />it should be set to an IP to ensure that components work even if DNS is<br />not available in the cluster. |  | Optional: \{\} <br /> |
| `kubernetesAPIServerPort` _integer_ | KubernetesAPIServerPort is the port the Kubernetes API server is<br />accessible at. This setting is usually used together with the<br />kubernetesAPIServerVIP setting. It enables specific underlying<br />components deployed directly or indirectly by the DPF Operator to reach<br />the Kubernetes API Server when the ClusterIP Kubernetes Service is<br />not functional. |  | Optional: \{\} <br /> |
| `argoCDNamespace` _string_ | ArgoCDNamespace is the namespace where ArgoCD is deployed. AppProjects<br />and cluster secrets required by DPF will be created in this namespace.<br />Defaults to the namespace of the DPFOperatorConfig. |  | MaxLength: 63 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `provisioningIssuerCASecretName` _string_ | ProvisioningIssuerCASecretName overrides the CA secret of the<br />provisioning Issuer during a rotation. The provisioning certificates are<br />signed by the platform intermediate CA now, and a rotation of it is<br />driven by cert-manager rather than by repointing an Issuer, so this only<br />still pins the provisioning Issuer of the CA chain the platform<br />intermediate CA is taking over from, on a cluster the upgrade has not<br />finished migrating. It has no effect once that chain is gone, which<br />is every cluster installed since.<br />Deprecated: This field is deprecated and will be removed with v31.0.0. |  | MinLength: 1 <br />Optional: \{\} <br /> |


#### ProvisioningControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `disableTracing` _boolean_ | DisableTracing stops the controller from exporting trace spans although<br />tracing is configured. The controller then also stops passing the trace<br />context on to the objects it creates, so a trace ends at this controller. |  | Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the Provisioning controller.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the Provisioning controller<br />component. It contains the image for the controller and its resource<br />requirements. |  | Optional: \{\} <br /> |
| `bfCFGTemplateConfigMap` _string_ | BFCFGTemplateConfigMap is the name of a configMap containing a template<br />for the BF.cfg file used by the DPU controller. By default the<br />provisioning controller use a hardcoded BF.cfg e.g.<br />https://github.com/NVIDIA/doca-platform/blob/release-v24.10/internal/provisioning/controllers/dpu/bfcfg/bf.cfg.template<br />Note: Replacing the bf.cfg is an advanced use case. The default bf.cfg is<br />designed for most use cases.<br />Deprecated: BFCFGTemplateConfigMap is deprecated and will be removed in<br />a future release. Use enableDynamicBFCFGTemplates instead for custom<br />bf.cfg templates. |  | Optional: \{\} <br /> |
| `enableDynamicBFCFGTemplates` _boolean_ | EnableDynamicBFCFGTemplates enables runtime discovery of bf.cfg templates<br />via ConfigMaps. When enabled, the provisioning controller discovers<br />ConfigMaps by matching labels for BFB name/namespace and DPUCluster<br />name/namespace. Mutually exclusive with bfCFGTemplateConfigMap. |  | Optional: \{\} <br /> |
| `bfbPVCName` _string_ | BFBPersistentVolumeClaimName is the name of the PersistentVolumeClaim<br />used by dpf-provisioning-controller If not provided, the controller will<br />use local host storage (hostPath) |  | Optional: \{\} <br /> |
| `dmsTimeout` _integer_ | DMSTimeout is the max time in seconds within which a DMS API must<br />respond, 0 is unlimited |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `customCASecretName` _string_ | CustomCASecretName indicates the name of the Kubernetes secret object<br />which containing the custom CA certificate |  | Optional: \{\} <br /> |
| `installInterface` _[ProvisioningInstallInterface](#provisioninginstallinterface)_ | InstallInterface is the interface through which the BFB is installed |  | Optional: \{\} <br /> |
| `registry` _[RegistryConfiguration](#registryconfiguration)_ | Registry is the configuration for the BFB Registry |  | Optional: \{\} <br /> |
| `maxDPUParallelInstallations` _integer_ | MaxDPUParallelInstallations specifies the maximum number of DPUs that<br />can be provisioned concurrently. A DPU is removed from the concurrent<br />provisioning count as soon as it finishes the "OS Installing" phase and<br />enters the "Rebooting" phase of its provisioning lifecycle. | 50 | Minimum: 1 <br />Optional: \{\} <br /> |
| `dpuMaxConcurrentReconciles` _integer_ | DPUMaxConcurrentReconciles specifies the maximum number of DPU objects<br />that the provisioning DPU controller may reconcile concurrently.<br />Reconciles for the same DPU remain serialized by the controller<br />workqueue. When unset, the provisioning controller defaults to 1,<br />matching the previous single-worker behavior. Raise this to overlap<br />I/O-bound provisioning of different DPUs. | 1 | Minimum: 1 <br />Optional: \{\} <br /> |
| `multiDPUOperationsSyncWaitTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | MultiDPUOperationsSyncWaitTime is the wait time between DPUs sync<br />operations on the same node. It would take effect only on DPUNode objects<br />which contain more than one DPU. | 30s | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `maxUnavailableDPUNodes` _integer_ | MaxUnavailableDPUNodes is the maximum number of DPUNodes that are<br />unavailable during the node effect period. It is also used as the maximum<br />number of unavailable DPUs when controlling DPUSet rolling-update<br />deletion. | 50 | Minimum: 1 <br />Optional: \{\} <br /> |
| `osInstallRetries` _integer_ | OSInstallRetries is the maximum number of retryable OS installation<br />attempts in zero-trust mode before the DPU transitions to Error.<br />Attempts are counted in-process and reset on controller restart. When<br />unset, the provisioning controller defaults to 2. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `osInstallTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | OSInstallTimeout is the maximum time allowed for OS installation. If the<br />installation exceeds this timeout, the DPU will transition to an error<br />state. When unset, the provisioning controller defaults to 60m. |  | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `nodeJoinTokenTTL` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | NodeJoinTokenTTL is the lifetime of the kubeadm bootstrap token created<br />for a DPU node to join its DPUCluster. When unset, the provisioning<br />controller defaults to 3h. |  | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `firmwareUpdateTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | FirmwareUpdateTimeout is the maximum time allowed for BF4 firmware update<br />in zero-trust mode. If the update exceeds this timeout, the DPU will<br />transition to an error state. When unset, the provisioning controller<br />defaults to 45m. |  | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `preInstallAgentRegistrationTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | PreInstallAgentRegistrationTimeout is how long Initializing waits for the<br />in-band dpu-agent to set preInstall.agentReported on a recreated DPU CR<br />(reprovision). When the timeout elapses, provisioning continues without<br />agent-assisted pre-install for this cycle. | 30s | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `nodeEffectRemovalTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | NodeEffectRemovalTimeout is the maximum time allowed for the Node Effect<br />Removal phase. If the DPUNodeMaintenance CR still has requestors after<br />this timeout, the DPU will transition to an error state. When unset, the<br />provisioning controller defaults to 0s (timeout disabled). |  | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `hostAgentDNSPolicy` _[DNSPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#dnspolicy-v1-core)_ | HostAgentDNSPolicy sets the DNS policy for the hostagent pod. Valid<br />values are 'ClusterFirstWithHostNet', 'ClusterFirst', 'Default' or<br />'None'. Defaults to 'ClusterFirstWithHostNet'. |  | Enum: [ClusterFirstWithHostNet ClusterFirst Default None] <br />Optional: \{\} <br /> |
| `bmcServerCertRenewBefore` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | BMCServerCertRenewBefore is how long before expiry DPF rotates the DPU<br />BMC mTLS server certificate. When unset, the provisioning controller<br />defaults to 720h (30 days). |  | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |


#### ProvisioningInstallInterface



ProvisioningInstallInterface is the interface used to install the BFB



_Appears in:_
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `installViaGNOI` _[InstallViaGNOI](#installviagnoi)_ | InstallViaGNOI is the interface used to install the BFB via GNOI<br />Deprecated: Use InstallViaHostAgent instead. |  | Optional: \{\} <br /> |
| `installViaHostAgent` _[InstallViaHostAgent](#installviahostagent)_ | InstallViaHostAgent is the interface used to install the BFB via<br />HostAgent |  | Optional: \{\} <br /> |
| `installViaRedfish` _[InstallViaRedfish](#installviaredfish)_ | InstallViaRedfish is the interface used to install the BFB via Redfish |  | Optional: \{\} <br /> |


#### RegistryConfiguration







_Appears in:_
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `address` _string_ | Address is the address used to access the BFB Registry. The address must<br />start with "http://" or "https://". By default, the BFB Registry can be<br />accessed via its Service. For non-kubernetes environments, this must be<br />set due to the lack of kubelet on worker nodes. For zero-trust<br />environments, this must be set so that the BFB Registry can be accessed<br />from DPU BMC.<br />Deprecated: Address is deprecated and will be removed in a future<br />release. |  | Pattern: `^https?://` <br />Optional: \{\} <br /> |
| `port` _integer_ | Port is the port on which the registry instances will listen<br />Deprecated: Port is deprecated and will be removed in a future release. |  | Maximum: 65535 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `loadBalancerAddress` _string_ | LoadBalancerAddress is the address of the load balancer for the BFB<br />Registry which the hostagent/redfish use to fetch the BFB and generated<br />bf.cfg. To enable the load balancer, you need to deploy your own load<br />balancer controller and configure the LoadBalancerAddress field. Then<br />check the bfb-registry nodeport service and make your load balancer<br />controller to distribute the requests to the bfb-registry nodeport. |  | Pattern: `^https?://` <br />Optional: \{\} <br /> |


#### ResourceComponentConfig



ResourceComponentConfig defines the resource requirements for a container.



_Appears in:_
- [CoreDNSDeployment](#corednsdeployment)
- [DefaultOverridesConfiguration](#defaultoverridesconfiguration)
- [FlannelDaemon](#flanneldaemon)
- [NVIPAMController](#nvipamcontroller)
- [NVIPAMNode](#nvipamnode)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `resources` _[ResourceRequirements](#resourcerequirements)_ | Resources defines the memory and CPU resource requests and limits for the<br />component. This field is optional, and if not set, the component will use<br />the default resource. |  | Optional: \{\} <br /> |


#### ResourceRequirements







_Appears in:_
- [CoreDNSDeployment](#corednsdeployment)
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
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `disableTracing` _boolean_ | DisableTracing stops the controller from exporting trace spans although<br />tracing is configured. The controller then also stops passing the trace<br />context on to the objects it creates, so a trace ends at this controller. |  | Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the SFC controller.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the SFC controller component.<br />It contains the image for the controller and its resource requirements. |  | Optional: \{\} <br /> |
| `secureFlowDeletionTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | SecureFlowDeletionTimeout controls the timeout for which the API server<br />is unreachable after which all the flows are deleted to prevent<br />unintended packet leaks. It has effect when is greater than zero. Value<br />must be in units accepted by Go time.ParseDuration<br />https://golang.org/pkg/time/#ParseDuration. |  | Optional: \{\} <br /> |


#### SPIFFECSIDriverConfiguration



SPIFFECSIDriverConfiguration is the configuration for the SPIFFE CSI driver.
It is deployed whenever SPIFFE is configured, so it cannot be disabled
on its own.



_Appears in:_
- [SPIFFEConfiguration](#spiffeconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `driver` _[ImageComponentConfig](#imagecomponentconfig)_ | driver contains the configuration for the SPIFFE CSI driver container.<br />It contains the image for the container. |  | Optional: \{\} <br /> |
| `nodeDriverRegistrar` _[ImageComponentConfig](#imagecomponentconfig)_ | nodeDriverRegistrar contains the configuration for the<br />node-driver-registrar container that registers the SPIFFE CSI driver with<br />the kubelet. It contains the image for the container. |  | Optional: \{\} <br /> |


#### SPIFFEConfiguration



SPIFFEConfiguration is the per-cluster SPIFFE bootstrap parameter set



_Appears in:_
- [SecurityConfiguration](#securityconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `spireServerAddress` _string_ | SPIREServerAddress is the address of the pre-installed SPIRE Server in<br />host:port form (e.g. "spire-server.spire-system.svc:8081"). |  | MaxLength: 263 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `spireTrustDomain` _string_ | SPIRETrustDomain is the SPIRE-internal trust domain (e.g. "cs.internal")<br />embedded in the DPU Agent SVID URI. |  | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` <br />Required: \{\} <br /> |
| `dpuAgentSPIFFEIDTemplate` _string_ | DPUAgentSPIFFEIDTemplate renders the local SPIFFE workload identity<br />registered with SPIRE. It uses Go text/template syntax and receives<br />TrustDomain, normalized SerialNumber, DPUMeta, DPUSpec, DPUDeviceMeta,<br />and DPUDeviceSpec. Metadata labels and annotations can be accessed with<br />the built-in index function. The rendered identity must use<br />SPIRETrustDomain and depend on the DPU serial. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `dpuAgentExchangedSPIFFEIDTemplate` _string_ | DPUAgentExchangedSPIFFEIDTemplate renders the post-exchange SPIFFE ID<br />subject. It receives the same Go template data as<br />DPUAgentSPIFFEIDTemplate. The rendered identity may use a different trust<br />domain and must depend on the DPU serial. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `dpuServiceSPIFFEIDTemplate` _string_ | DPUServiceSPIFFEIDTemplate renders the identity registered with SPIRE for<br />a DPUService that opts in through its own spec.security.spiffe. It<br />receives TrustDomain, normalized SerialNumber, Namespace and ServiceID,<br />plus DPUMeta, DPUSpec, DPUServiceMeta and DPUServiceSpec. The rendered<br />identity must use SPIRETrustDomain and depend on the namespace, the<br />service ID and the DPU serial, which together identify one DPUService<br />workload. Dropping any of them hands a single SVID to distinct workloads,<br />and nothing detects that later: SPIRE keys entries on the identity, the<br />parent and the selectors, so two DPUServices differing only in namespace<br />produce two entries carrying the same identity. A label cannot stand in<br />for the namespace, since nothing ties one to the other. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `dpuServiceExchangedSPIFFEIDTemplate` _string_ | DPUServiceExchangedSPIFFEIDTemplate renders the post-exchange DPUService<br />subject. It receives the same template data as DPUServiceSPIFFEIDTemplate<br />and may use a different trust domain. DPF renders and validates it so the<br />identity layout is declared in one place, but does not consume it: unlike<br />the DPU Agent, a DPUService identity is never presented back to DPF. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `kubeAPIAudience` _string_ | KubeAPIAudience is the audience claim the DPU Agent's JWT-SVID must<br />carry; it must match an entry in the kube-apiserver<br />AuthenticationConfiguration.audiences[] (owned out-of-band). |  | MaxLength: 512 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `tokenExchangeEndpoint` _string_ | tokenExchangeEndpoint exchanges the SPIRE JWT-SVID before the DSX SPIFFE<br />Helper writes it. When omitted, the DSX SPIFFE Helper writes the SPIRE<br />JWT-SVID directly. The returned token's audience must match<br />kubeAPIAudience, or the kube-apiserver rejects the DPU Agent. Only https:<br />the JWT-SVID is sent here as a bearer credential. |  | MaxLength: 2048 <br />Pattern: `^https://[^[:space:]"\\]+$` <br />Optional: \{\} <br /> |
| `spireOIDCURL` _string_ | SPIREOIDCURL is the OIDC discovery (issuer) URL of the pre-installed<br />SPIRE Server. The matching kube-apiserver<br />AuthenticationConfiguration.jwt[].issuer value is applied out-of-band. |  | MaxLength: 2048 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `spireControllerManagerClassName` _string_ | spireControllerManagerClassName selects the SPIRE controller-manager<br />instance that renders DPF ClusterStaticEntries. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `trustBundle` _[SPIFFETrustBundleConfigMapReference](#spiffetrustbundleconfigmapreference)_ | trustBundle references a ConfigMap holding the initial SPIRE trust<br />bundle. |  | Required: \{\} <br /> |
| `csiDriver` _[SPIFFECSIDriverConfiguration](#spiffecsidriverconfiguration)_ | csiDriver contains the configuration for the SPIFFE CSI driver deployed<br />to the DPU clusters. |  | Optional: \{\} <br /> |


#### SPIFFETrustBundleConfigMapReference



SPIFFETrustBundleConfigMapReference references a ConfigMap containing the
initial SPIRE trust bundle.



_Appears in:_
- [SPIFFEConfiguration](#spiffeconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the ConfigMap holding the SPIRE trust bundle. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `namespace` _string_ | Namespace is the namespace of the ConfigMap holding the SPIRE trust<br />bundle. |  | MaxLength: 63 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `format` _[SPIFFETrustBundleFormat](#spiffetrustbundleformat)_ | Format selects data["bundle.pem"] or data["bundle.spiffe"] and the<br />matching SPIRE Agent parser. | pem | Enum: [pem spiffe] <br />Optional: \{\} <br /> |


#### SPIFFETrustBundleFormat

_Underlying type:_ _string_

SPIFFETrustBundleFormat is a SPIRE Agent initial trust bundle format.



_Appears in:_
- [SPIFFETrustBundleConfigMapReference](#spiffetrustbundleconfigmapreference)

| Field | Description |
| --- | --- |
| `pem` |  |
| `spiffe` |  |


#### SRIOVDevicePluginConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the SRIOV Device Plugin<br />container.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `deviceplugin` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `deviceplugin` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | DevicePlugin contains the configuration for the SRIOV Device Plugin<br />component. It contains the image for the controller and its resource<br />requirements. |  | Optional: \{\} <br /> |
| `configInit` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | ConfigInit contains the configuration for the SR-IOV Device Plugin<br />config-init container. It contains the image for the init container and<br />its resource requirements. |  | Optional: \{\} <br /> |


#### SecretKeyRef



SecretKeyRef selects a single key from a Secret living in the same namespace
as the DPFOperatorConfig.



_Appears in:_
- [StaticKeyConfiguration](#statickeyconfiguration)
- [VaultKMSJWTAuth](#vaultkmsjwtauth)
- [VaultKMSTokenAuth](#vaultkmstokenauth)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the Secret. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `key` _string_ | Key is the key within the Secret data to select. |  | MinLength: 1 <br />Required: \{\} <br /> |


#### SecurityConfiguration



SecurityConfiguration groups configuration for security-related
configurations managed by the DPF Operator.



_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `privilegedPodEnforcement` _boolean_ | PrivilegedPodEnforcement controls whether privileged pods are rejected<br />unless explicitly allowed by the workload API. The DPUService controller<br />currently implements this by applying the PrivilegedPodEnforcement<br />ValidatingAdmissionPolicy to DPUService workloads.<br />Setting it to false does not fully opt out of enforcement: the policy and<br />its binding are kept, but the binding is switched from Deny to Audit, so<br />privileged pods are no longer denied and are only recorded in the audit<br />log. The allowlist is kept populated so the audit log only flags pods<br />that would otherwise be denied.<br />The objects are intentionally not deleted to avoid a Kubernetes paramRef<br />informer bug (https://github.com/kubernetes/kubernetes/issues/133827).<br />Defaults to true. | true | Optional: \{\} <br /> |
| `kata` _[KataContainersConfiguration](#katacontainersconfiguration)_ | Kata is the configuration for Kata Containers. Kata Containers provides<br />VM-based isolation for untrusted workloads on DPU nodes. This component<br />is disabled by default; set disable to false to enable. |  | Optional: \{\} <br /> |
| `spiffe` _[SPIFFEConfiguration](#spiffeconfiguration)_ | spiffe configures the SPIFFE-based DPU Agent identity flow. Edits are<br />accepted post-bootstrap but do NOT retro-apply to<br />already-provisioned DPUs. |  | Optional: \{\} <br /> |
| `vaultKMS` _[VaultKMSConfiguration](#vaultkmsconfiguration)_ | VaultKMS is the configuration for the standalone Vault/OpenBao KMS plugin<br />component. It is deployed as a DaemonSet on control-plane nodes and is<br />disabled by default. The plugin is used for encryption at rest<br />for DPUClusters. |  | Optional: \{\} <br /> |
| `certManagement` _[CertManagementConfiguration](#certmanagementconfiguration)_ | CertManagement configures the certificate authority that the provisioning<br />certificates of DPF chain up to, and the trust bundle their consumers<br />validate peers against. If unset, DPF signs them with a self-signed CA<br />of its own. |  | Optional: \{\} <br /> |


#### SecurityStatus



SecurityStatus records what the operator has rolled out for the
security-related components it manages, mirroring SecurityConfiguration
on the spec.



_Appears in:_
- [DPFOperatorConfigStatus](#dpfoperatorconfigstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `certManagement` _[CertManagementStatus](#certmanagementstatus)_ | CertManagement records what the operator has rolled out for the<br />provisioning PKI. |  | Optional: \{\} <br /> |


#### ServiceSetControllerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `disableTracing` _boolean_ | DisableTracing stops the controller from exporting trace spans although<br />tracing is configured. The controller then also stops passing the trace<br />context on to the objects it creates, so a trace ends at this controller. |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart overrides the helm chart used by the ServiceSet controller. The<br />URL must begin with either 'oci://' or 'https://', ensuring it points to<br />a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the ServiceChainSet<br />Controller.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the ServiceChainSet controller<br />component. It contains the image for the controller and its resource<br />requirements. |  | Optional: \{\} <br /> |


#### StaticClusterManagerConfiguration







_Appears in:_
- [DPFOperatorConfigSpec](#dpfoperatorconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the controller deployment.<br />Used for High Availability via leader election. | 2 | Maximum: 3 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `image` _[Image](#image)_ | Image overrides the container image used by the Static Cluster Manager.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use the new field `controller` instead. |  | Pattern: `^((?:(?:(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]\|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*\|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]\|__\|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]\{0,127\}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]\{32,\}))?$` <br />Optional: \{\} <br /> |
| `controller` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Controller contains the configuration for the Static Cluster Manager<br />controller component. It contains the image for the controller and its<br />resource requirements. |  | Optional: \{\} <br /> |


#### StaticKeyConfiguration



StaticKeyConfiguration configures the staticKey encryption-at-rest provider.



_Appears in:_
- [EtcdEncryptionAtRestConfiguration](#etcdencryptionatrestconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `keySecretRef` _[SecretKeyRef](#secretkeyref)_ | KeySecretRef selects the AES-GCM key from a Secret in the<br />DPFOperatorConfig namespace. The referenced Secret value must be<br />base64-encoded AES key text whose decoded length is 16, 24, or 32 bytes.<br />For Kubernetes manifests, use stringData.key with the output of<br />`openssl rand -base64 32`. For External Secrets, configure the external<br />value or template so the resulting Kubernetes Secret data decodes to that<br />base64 text, not to raw key bytes. The referenced key is used as the<br />desired static key source. Changing the referenced Secret value triggers<br />automatic rotation for existing staticKey-encrypted Kamaji clusters. The<br />per-cluster rendered encryption configuration must be backed up together<br />with the cluster etcd backup because Kubernetes encrypted data references<br />encryption config key names. |  | Required: \{\} <br /> |
| `automaticRotationDisabled` _boolean_ | AutomaticRotationDisabled disables automatic staticKey rotation for<br />existing Kamaji clusters. In-flight rotations stop at the next stable<br />checkpoint; encryption at rest remains enabled. |  | Optional: \{\} <br /> |


#### TracingConfiguration



TracingConfiguration configures how the DPF controllers export their trace
spans. The endpoint is used by the controllers of the management cluster, so
it can be a cluster-local Service.



_Appears in:_
- [MonitoringConfiguration](#monitoringconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `otlp` _[OpenTelemetryCollectorExportConfiguration](#opentelemetrycollectorexportconfiguration)_ | Otlp is the OTLP export configuration of the controllers of the<br />management cluster, so its endpoint can be a cluster-local Service. If<br />not specified, these controllers export no trace spans. |  | Optional: \{\} <br /> |
| `disableControllerTraces` _boolean_ | DisableControllerTraces stops all DPF controllers from exporting trace<br />spans and keeps the configuration. It takes precedence over<br />disableTracing of the individual controllers. The collector still<br />forwards the spans it receives locally. |  | Optional: \{\} <br /> |
| `recordIdleReconciles` _boolean_ | RecordIdleReconciles exports the spans of idle reconciles. A reconcile<br />is idle when it starts at the resourceVersion the last successful<br />reconcile of the object left behind, for example a requeue or a resync.<br />An idle reconcile that fails or changes its object is always exported.<br />When not set, idle reconciles are not exported. |  | Optional: \{\} <br /> |
| `recordUnchangedReconciles` _boolean_ | RecordUnchangedReconciles exports the spans of reconciles that changed<br />nothing on their object, did not fail and have no child spans. Such a<br />span only says that the reconcile ran. When not set, they are not<br />exported. |  | Optional: \{\} <br /> |
| `samplingPercent` _integer_ | SamplingPercent is the share of the traces generated by the DPF<br />controllers that is exported, in percent. The decision is made per trace,<br />so all spans of a change are kept or dropped together. When not set, all<br />of them are kept. |  | Maximum: 100 <br />Minimum: 0 <br />Optional: \{\} <br /> |
| `rolloutTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | RolloutTimeout is how long a change may take to become ready before its<br />rollout span is exported as timed out. When not set, it is one hour. Set<br />it to 0s to turn the timeout off, so a rollout ends once the change is<br />ready or a newer change replaces it. |  | Format: duration <br />Pattern: `^([0-9]+(h\|m\|s\|ms\|us\|µs\|ns))+$` <br />Type: string <br />Optional: \{\} <br /> |


#### TracingControllerConfig



TracingControllerConfig is embedded by the controllers that can opt out of
exporting trace spans.



_Appears in:_
- [DPUServiceControllerConfiguration](#dpuservicecontrollerconfiguration)
- [NodeSRIOVDevicePluginControllerConfiguration](#nodesriovdeviceplugincontrollerconfiguration)
- [ProvisioningControllerConfiguration](#provisioningcontrollerconfiguration)
- [SFCControllerConfiguration](#sfccontrollerconfiguration)
- [ServiceSetControllerConfiguration](#servicesetcontrollerconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disableTracing` _boolean_ | DisableTracing stops the controller from exporting trace spans although<br />tracing is configured. The controller then also stops passing the trace<br />context on to the objects it creates, so a trace ends at this controller. |  | Optional: \{\} <br /> |


#### VaultKMSAppRoleAuth



VaultKMSAppRoleAuth configures the AppRole auth method using a
single merged Secret.



_Appears in:_
- [VaultKMSAuth](#vaultkmsauth)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretName` _string_ | SecretName is the name of the Secret holding the AppRole role ID and<br />secret ID. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `authEngineMountPath` _string_ | AuthEngineMountPath optionally overrides the Vault auth engine mount<br />path. It is not the transit mount. |  | MaxLength: 512 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `roleIDKey` _string_ | RoleIDKey is the Secret data key holding the AppRole role ID. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `secretIDKey` _string_ | SecretIDKey is the Secret data key holding the AppRole secret ID. |  | MinLength: 1 <br />Required: \{\} <br /> |


#### VaultKMSAuth



VaultKMSAuth configures the Vault/OpenBao auth method. Exactly one auth
block matching method must be set.



_Appears in:_
- [VaultKMSConfiguration](#vaultkmsconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `method` _[VaultKMSAuthMethod](#vaultkmsauthmethod)_ | Method selects the Vault auth method. |  | Enum: [token approle userpass kubernetes jwt] <br />Required: \{\} <br /> |
| `token` _[VaultKMSTokenAuth](#vaultkmstokenauth)_ | Token configures token auth. |  | Optional: \{\} <br /> |
| `appRole` _[VaultKMSAppRoleAuth](#vaultkmsapproleauth)_ | AppRole configures AppRole auth. |  | Optional: \{\} <br /> |
| `userpass` _[VaultKMSUserpassAuth](#vaultkmsuserpassauth)_ | Userpass configures userpass auth. |  | Optional: \{\} <br /> |
| `kubernetes` _[VaultKMSKubernetesAuth](#vaultkmskubernetesauth)_ | Kubernetes configures Kubernetes auth. |  | Optional: \{\} <br /> |
| `jwt` _[VaultKMSJWTAuth](#vaultkmsjwtauth)_ | JWT configures JWT auth. |  | Optional: \{\} <br /> |


#### VaultKMSAuthMethod

_Underlying type:_ _string_

VaultKMSAuthMethod selects the Vault/OpenBao auth method used by
the KMS plugin.

_Validation:_
- Enum: [token approle userpass kubernetes jwt]

_Appears in:_
- [VaultKMSAuth](#vaultkmsauth)

| Field | Description |
| --- | --- |
| `token` | VaultKMSAuthMethodToken authenticates using a Vault token.<br /> |
| `approle` | VaultKMSAuthMethodAppRole authenticates using the AppRole auth method.<br /> |
| `userpass` | VaultKMSAuthMethodUserpass authenticates using the userpass auth method.<br /> |
| `kubernetes` | VaultKMSAuthMethodKubernetes authenticates using the Kubernetes<br />auth method.<br /> |
| `jwt` | VaultKMSAuthMethodJWT authenticates using the JWT auth method.<br /> |


#### VaultKMSConfiguration



VaultKMSConfiguration configures the standalone Vault/OpenBao KMS plugin
component. The component is deployed as a DaemonSet on control-plane nodes
and is disabled by default. The plugin is used for encryption at rest for
DPUClusters.



_Appears in:_
- [SecurityConfiguration](#securityconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable ensures the component is not deployed when set to true. |  | Optional: \{\} <br /> |
| `daemon` _[DefaultOverridesConfiguration](#defaultoverridesconfiguration)_ | Daemon contains the image and resource overrides for the KMS plugin<br />DaemonSet. |  | Optional: \{\} <br /> |
| `tls` _[VaultKMSTLS](#vaultkmstls)_ | TLS configures TLS settings used to connect to Vault/OpenBao. |  | Optional: \{\} <br /> |
| `auth` _[VaultKMSAuth](#vaultkmsauth)_ | Auth configures how the plugin authenticates to Vault/OpenBao. |  | Required: \{\} <br /> |
| `tokenCheckIntervalSeconds` _integer_ | TokenCheckIntervalSeconds optionally overrides how often the plugin<br />checks and renews the current Vault token, in seconds. This is an<br />advanced setting. The plugin default should work for most environments.<br />Must be at least 5 seconds. |  | Minimum: 5 <br />Optional: \{\} <br /> |
| `loginTimeoutSeconds` _integer_ | LoginTimeoutSeconds optionally overrides the maximum time for one Vault<br />token check cycle, including authentication, in seconds. This is an<br />advanced setting. The plugin default should work for most environments.<br />Must be at least 1 second. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `address` _string_ | Address is the Vault/OpenBao server address.<br />WARNING: Changing this field does not automatically rotate the encryption<br />key or re-encrypt existing DPU cluster secrets. Do not change it while<br />active DPU clusters depend on this KMS plugin unless the new endpoint<br />provides access to the key material used by the previous endpoint.<br />Otherwise, those clusters will be unable to decrypt their existing<br />secrets, causing an outage. |  | MaxLength: 2048 <br />MinLength: 1 <br />Pattern: `^https://.+$` <br />Required: \{\} <br /> |
| `transit` _[VaultKMSTransit](#vaultkmstransit)_ | Transit configures the Vault Transit secrets engine used for<br />encrypt/decrypt.<br />WARNING: Changing this field does not automatically rotate the encryption<br />key or re-encrypt existing DPU cluster secrets. Do not change it while<br />active DPU clusters depend on this KMS plugin unless the new Transit<br />configuration provides access to all key material used by the previous<br />configuration. Otherwise, those clusters will be unable to decrypt their<br />existing secrets, causing an outage. |  | Required: \{\} <br /> |
| `namespace` _string_ | Namespace optionally configures the Vault/OpenBao namespace used for<br />requests. This is a Vault/OpenBao namespace, not a Kubernetes namespace.<br />WARNING: Changing this field does not automatically rotate the encryption<br />key or re-encrypt existing DPU cluster secrets. Do not change it while<br />active DPU clusters depend on this KMS plugin unless the new namespace<br />provides access to the key material used by the previous namespace.<br />Otherwise, those clusters will be unable to decrypt their existing<br />secrets, causing an outage. |  | MaxLength: 512 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### VaultKMSJWTAuth



VaultKMSJWTAuth configures the JWT auth method.



_Appears in:_
- [VaultKMSAuth](#vaultkmsauth)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `role` _string_ | Role is the Vault JWT auth role name. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `jwtSecretRef` _[SecretKeyRef](#secretkeyref)_ | JWTSecretRef selects the JWT presented to Vault from a Secret in the<br />DPFOperatorConfig namespace. |  | Required: \{\} <br /> |
| `authEngineMountPath` _string_ | AuthEngineMountPath optionally overrides the Vault auth engine mount<br />path. It is not the transit mount. |  | MaxLength: 512 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### VaultKMSKubernetesAuth



VaultKMSKubernetesAuth configures the Kubernetes auth method.



_Appears in:_
- [VaultKMSAuth](#vaultkmsauth)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `role` _string_ | Role is the Vault Kubernetes auth role name (not a Kubernetes RBAC role). |  | MinLength: 1 <br />Required: \{\} <br /> |
| `audience` _string_ | Audience optionally sets the audience for the projected Kubernetes<br />service account token. Use this when the Vault Kubernetes auth role is<br />configured with bound audiences. |  | MaxLength: 512 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `authEngineMountPath` _string_ | AuthEngineMountPath optionally overrides the Vault auth engine mount<br />path. It is not the transit mount. |  | MaxLength: 512 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### VaultKMSTLS



VaultKMSTLS configures TLS settings for the connection to Vault/OpenBao.



_Appears in:_
- [VaultKMSConfiguration](#vaultkmsconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `caConfigMapRef` _[ConfigMapKeyRef](#configmapkeyref)_ | CACertConfigMapRef selects a CA bundle key from a ConfigMap used to<br />verify the Vault/OpenBao server certificate. It is mounted as a file. |  | Optional: \{\} <br /> |


#### VaultKMSTokenAuth



VaultKMSTokenAuth configures the token auth method.



_Appears in:_
- [VaultKMSAuth](#vaultkmsauth)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `tokenSecretRef` _[SecretKeyRef](#secretkeyref)_ | TokenSecretRef selects the Vault token from a Secret in the<br />DPFOperatorConfig namespace. |  | Required: \{\} <br /> |


#### VaultKMSTransit



VaultKMSTransit configures the Vault Transit secrets engine.



_Appears in:_
- [VaultKMSConfiguration](#vaultkmsconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `keyName` _string_ | KeyName is the Transit key used for encrypt and decrypt operations. |  | MinLength: 1 <br />Pattern: `^\w(([\w-.]+)?\w)?$` <br />Required: \{\} <br /> |
| `mount` _string_ | Mount is the Transit secrets engine mount path. Defaults to "transit". | transit | MaxLength: 512 <br />MinLength: 1 <br />Pattern: `^/?[^/\s][^\s]*$` <br />Optional: \{\} <br /> |


#### VaultKMSUserpassAuth



VaultKMSUserpassAuth configures the userpass auth method using a
single merged Secret.



_Appears in:_
- [VaultKMSAuth](#vaultkmsauth)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretName` _string_ | SecretName is the name of the Secret holding the username and password. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `authEngineMountPath` _string_ | AuthEngineMountPath optionally overrides the Vault auth engine mount<br />path. It is not the transit mount. |  | MaxLength: 512 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `usernameKey` _string_ | UsernameKey is the Secret data key holding the username. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `passwordKey` _string_ | PasswordKey is the Secret data key holding the password. |  | MinLength: 1 <br />Required: \{\} <br /> |



## provisioning.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the provisioning.dpu
v1alpha1 API group

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



Action defines the mutually exclusive node effect applied during
provisioning. Exactly one of taint, noEffect=true, drain=true, non-empty
customLabel, non-empty customAction, or hold=true must be set.



_Appears in:_
- [DPUs](#dpus)
- [NodeEffect](#nodeeffect)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `taint` _[Taint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#taint-v1-core)_ | Add specify taint on the DPU node |  | Optional: \{\} <br /> |
| `noEffect` _boolean_ | Do not do any action on the DPU node |  | Optional: \{\} <br /> |
| `customLabel` _object (keys:string, values:string)_ | Add specify labels on the DPU node |  | Optional: \{\} <br /> |
| `drain` _boolean_ | Drain the K8s host node by NodeMaintenance operator |  | Optional: \{\} <br /> |
| `customAction` _string_ | Name of a config map which contains a pod yaml definition to run which<br />will apply the nodeEffect. The pod is expected to exit when node effect<br />is done, if pod terminates with error then DPU would move to an error<br />phase. The DPUNode's name will be exported as an environment variable,<br />named as DPUNODE_NAME, to each container and init container in the pod.<br />The labels and annotations of DPUNode will be exported in<br />`/etc/dpu/dpf-pod-info/labels` and `/etc/dpu/dpf-pod-info/annotations`<br />accordingly; the volume name `dpf-pod-info` is used to mount the labels<br />and annotations. If any name confliction for env or volume, the<br />controller will not export the name or labels/annotations of DPUNode<br />accordingly. |  | Optional: \{\} <br /> |
| `hold` _boolean_ | Places annotation `wait-for-external-nodeeffect` and waits for it to be<br />removed this is the default behavior in a non K8S environment |  | Optional: \{\} <br /> |
| `force` _boolean_ | Force is the flag to indicate if the node effect should be applied<br />immediately. If true, dpfOperatorConfig.multiDPUOperationsSyncWaitTime<br />and dpfOperatorConfig.maxUnavailableDPUNodes will be ignored when<br />applying node effect for DPUNodeMaintenance CR | false | Optional: \{\} <br /> |


#### AgentPreInstallStatus



AgentPreInstallStatus is agent status for reprovisioning.



_Appears in:_
- [AgentStatus](#agentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `agentReported` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | AgentReported is set with a timestamp when dpu-agent detects a new DPU<br />CR is created for reprovisioning. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions contains pre-install conditions (e.g. NVConfigApplied for<br />reprovisioning). |  | Optional: \{\} <br /> |


#### AgentStatus







_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `lastStartupTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastStartupTime is the time when the DPU was last started |  | Optional: \{\} <br /> |
| `initialBootID` _string_ | InitialBootID is the boot ID of the DPU OS during the first boot |  |  |
| `rebootMethod` _[RebootMethodType](#rebootmethodtype)_ | RebootMethod is the type of reset/reboot set by the DPU agent<br />See enum values in RebootMethodType.<br />No default is set intentionally: nil means "check not run or not<br />applicable" (e.g. legacy flow, or agent has not run the check yet); a<br />non-nil value means the check ran and this is the result. |  | Enum: [Unknown NoAction PowerCycle SystemReboot SystemLevelReset FirmwareReset DPUWarmReboot] <br />Optional: \{\} <br /> |
| `lastObservedPendingNvconfig` _[PendingNVConfigState](#pendingnvconfigstate)_ | LastObservedPendingNVConfig stores the last pending NVConfig parameters<br />seen during reboot-method discovery on this boot. It is used on the next<br />boot to ignore repeated parameters that remained unchanged across boots. |  | Optional: \{\} <br /> |
| `rebootSequenceCount` _integer_ | RebootSequenceCount is the length of the current non-NoAction<br />RebootMethod sequence: it increments on each agent run that reports a<br />RebootMethod other than NoAction and resets to 0 when the agent reports<br />NoAction. Used with RebootMethod to bound host reboot loops. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `kubeletVersion` _string_ | KubeletVersion represents the kubelet version running on the DPU. |  |  |
| `version` _string_ | Version is set by an agent that can GET the live DPUFlavor and start a<br />second Run(). Empty means an old agent; do not start in-place reconfig<br />for this DPU. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `appliedFlavorGeneration` _integer_ | AppliedFlavorGeneration is the metadata.generation of the DPUFlavor this<br />Run() applied. Generation advances only with the spec, so it identifies<br />the applied spec, not metadata-only writes. Debug only. It is not used to<br />decide whether to start in-place reconfig. Zero means the agent has not<br />reported an applied flavor yet. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `preInstall` _[AgentPreInstallStatus](#agentpreinstallstatus)_ | PreInstall holds agent-reported status for work done before OS install<br />in the reprovisioning process. |  | Optional: \{\} <br /> |
| `trustBundleHash` _string_ | TrustBundleHash is the bundle-hash value last applied by the DPU agent. |  | Optional: \{\} <br /> |
| `trustBundleLastUpdateTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | TrustBundleLastUpdateTime is when the trust bundle was last updated by<br />the DPU agent. |  | Optional: \{\} <br /> |
| `clock` _[ClockStatus](#clockstatus)_ | Clock is the DPU clock compared against the host clock, recorded while<br />the DPU agent was acquiring its identity. A DPU whose clock is skewed<br />cannot verify the API server certificate, so it never obtains the<br />identity it would need to report this itself. Set by the host agent, in<br />trusted-host mode only. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions contains the conditions reported from inside the DPU |  | Optional: \{\} <br /> |
| `spiffe` _[SpiffeStatus](#spiffestatus)_ | Spiffe contains the SPIFFE heartbeat status reported by the DPU Agent<br />when running in SPIFFE identity mode. |  | Optional: \{\} <br /> |
| `hostOSInit` _[HostOSInitStatus](#hostosinitstatus)_ | hostOSInit reports terminal host OS init release status from the DPU<br />agent. Unset while the agent is polling or has not<br />reached ReleaseHostOSInit. |  | Optional: \{\} <br /> |
| `ewnicRuntimeConfig` _[EWNICRuntimeConfigStatus](#ewnicruntimeconfigstatus)_ | ewnicRuntimeConfig reports E/W NIC runtime configuration on this boot.<br />The DPU agent sets it when NIC provisioning starts; when set, the<br />controller waits for runtime configuration before leaving DPU Config.<br />Unset means runtime configuration is not required. |  | Optional: \{\} <br /> |


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
| `fileName` _string_ | Filename is the name of the file where the BFB can be accessed on its<br />volume. This is the same as `.spec.Filename` if set. |  | Optional: \{\} <br /> |
| `phase` _[BFBPhase](#bfbphase)_ | The current state of BFB. | Initializing | Enum: [Initializing Downloading Ready Deleting Error] <br />Optional: \{\} <br /> |
| `versions` _[BFBVersions](#bfbversions)_ | BFB versions - BSP, DOCA, UEFI and ATF<br />Holds detailed version information for each component within the BFB |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the<br />last time it was patched. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions represent the latest available observations of BFB state |  | Optional: \{\} <br /> |


#### BFBVersions



BFBVersions represents the version information for BFB components.



_Appears in:_
- [BFBSpec](#bfbspec)
- [BFBStatus](#bfbstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bsp` _string_ | BSP (Board Support Package) version.<br />This field stores the version of the BSP, which provides essential<br />support and drivers for the hardware platform. |  | Optional: \{\} <br /> |
| `doca` _string_ | DOCA version<br />Specifies the version of NVIDIA's Data Center-on-a-Chip Architecture<br />(DOCA), a platform for developing applications on DPUs |  | Optional: \{\} <br /> |
| `uefi` _string_ | UEFI (Unified Extensible Firmware Interface) version.<br />Indicates the UEFI firmware version, which is responsible for booting<br />the operating system and initializing hardware components |  | Optional: \{\} <br /> |
| `atf` _string_ | ATF (Arm Trusted Firmware) version.<br />Contains the version of ATF, which provides a secure runtime environment |  | Optional: \{\} <br /> |


#### BMCFactoryResetPolicy

_Underlying type:_ _string_

BMCFactoryResetPolicy controls whether DPF resets a DPU BMC to factory
defaults while initializing the DPUDevice. The enum and default are declared
on each field that uses it, so they show up in the CRD schema at that field.



_Appears in:_
- [DPUDeviceSpec](#dpudevicespec)
- [InstallViaRedfish](#installviaredfish)

| Field | Description |
| --- | --- |
| `OnInitialization` | BMCFactoryResetPolicyOnInitialization resets the BMC to factory defaults<br />once, while the DPUDevice is being initialized.<br /> |
| `Never` | BMCFactoryResetPolicyNever never resets the BMC.<br /> |


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
| `phase` _[BlueFieldSoftwarePhase](#bluefieldsoftwarephase)_ | The current state of BlueFieldSoftware. | Initializing | Enum: [Initializing Downloading Extracting Ready Deleting Error] <br />Optional: \{\} <br /> |
| `versions` _[BluefieldSoftwareVersions](#bluefieldsoftwareversions)_ | Versions tracks the versions of the components |  | Optional: \{\} <br /> |
| `downloadedComponents` _[DownloadedComponents](#downloadedcomponents)_ | DownloadedComponents tracks which components have been successfully<br />downloaded |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the<br />last time it was patched. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions represent the latest available observations of<br />BlueFieldSoftware state |  | Optional: \{\} <br /> |


#### BlueFieldSpec



BlueFieldSpec defines the desired state of BlueFieldSoftware.



_Appears in:_
- [BlueFieldSoftware](#bluefieldsoftware)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `osIso` _string_ | OS ISO points to the OS ISO used by DPU OS installation flow. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `pldmFwBundle` _object (keys:string, values:string)_ | PldmFwBundle maps each PSID to the PLDM firmware bundle URL used for<br />that DPU model's baseline firmware updates. Each PSID's bundle is a<br />complete PLDM package for that device. Keys are matched<br />case-insensitively against the BMC-reported DPUDevice status PSID. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `platformPldmFwBundle` _string_ | PlatformPldmFwBundle points to the platform PLDM firmware bundle used<br />for E/W NIC firmware updates. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `nicFw` _string_ | NicFw points to the NIC firmware binary used for E/W NIC firmware<br />updates. Use this when a specific NIC firmware binary is required and<br />is not included in the platform PLDM firmware bundle.<br />In production, prefer using PlatformPldmFwBundle. |  | MinLength: 1 <br />Optional: \{\} <br /> |


#### BluefieldDeviceVersions



BluefieldDeviceVersions holds firmware versions extracted from a single
PSID's PLDM bundle.



_Appears in:_
- [BluefieldSoftwareVersions](#bluefieldsoftwareversions)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bmcVersion` _string_ | BMCVersion is the DPU BMC firmware version shipped in the bundle. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `bmcErotVersion` _string_ | BMCErotVersion is the BMC ERoT (External Root of Trust) firmware<br />version shipped in the bundle. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `sbiosVersion` _string_ | SBIOSVersion is the DPU SBIOS firmware version shipped in the bundle. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `bfNicFwVersion` _string_ | BFNicFwVersion is the BlueField NIC firmware version shipped in the<br />bundle. |  | MinLength: 1 <br />Optional: \{\} <br /> |


#### BluefieldSoftwareVersions



BluefieldSoftwareVersions defines the versions of various software
components for a Bluefield device.



_Appears in:_
- [BlueFieldSoftwareStatus](#bluefieldsoftwarestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `doca` _string_ | DOCA is the formatted, user-facing DOCA version derived from the OS ISO. |  | Optional: \{\} <br /> |
| `osISOVersion` _string_ | OSISOVersion is the raw DOCA version for the OS ISO, taken from the ISO<br />filename |  | Optional: \{\} <br /> |
| `ewNicFwVersion` _string_ | EWNicFwVersion is the E/W NIC firmware version taken from the platform<br />PLDM bundle. It is not keyed by PSID because a BlueFieldSoftware<br />carries a single platform bundle. |  |  |
| `bluefieldSoftwareVersions` _object (keys:string, values:[BluefieldDeviceVersions](#bluefielddeviceversions))_ | BluefieldSoftwareVersions maps each PSID to the firmware versions from<br />that device's PLDM bundle. Keys are matched case-insensitively against<br />the BMC-reported DPUDevice PSID. |  | Optional: \{\} <br /> |


#### CertificateStatus



CertificateStatus reports the rotation state of a DPF-managed certificate.



_Appears in:_
- [DPUDeviceStatus](#dpudevicestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `notAfter` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | NotAfter is the expiry time of the certificate currently installed. It is<br />taken from the issued certificate at rotation time. |  | Optional: \{\} <br /> |
| `issuedForBMCIP` _string_ | IssuedForBMCIP is the BMC address the installed certificate was issued<br />for. When it differs from status.bmcIp, the identity bound into the<br />certificate no longer matches the address the controller talks to and<br />the certificate must be re-established. It is empty on devices<br />provisioned before this field existed; the controller records it on the<br />next successful rotation or expiry backfill. |  | Optional: \{\} <br /> |
| `lastRotationTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastRotationTime is the time DPF last successfully rotated<br />the certificate. |  | Optional: \{\} <br /> |
| `observedManualTrigger` _string_ | ObservedManualTrigger records the value of the manual rotation annotation<br />that was last honored, so the same trigger is not processed twice. |  | Optional: \{\} <br /> |


#### ClockStatus



ClockStatus holds two readings of the same instant, taken on the DPU and
on the host.



_Appears in:_
- [AgentStatus](#agentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | dpuTime is the DPU clock as reported by the DPU agent. |  |  |
| `hostTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | hostTime is the host clock when that report arrived. The host is the<br />trusted side of the comparison, so a difference is attributed to the DPU. |  |  |


#### ClusterEndpointSpec







_Appears in:_
- [DPUClusterSpec](#dpuclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `keepalived` _[KeepalivedSpec](#keepalivedspec)_ | Keepalived configures the keepalived that will be deployed for the<br />cluster control-plane |  | Optional: \{\} <br /> |


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
| `selector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Selector defines the selector of the DPUClusters the produced DPUs<br />should join |  | Optional: \{\} <br /> |






#### ConfigFile



ConfigFile describes a file materialized from inline raw content or external
contentFrom.



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[ConfigFileType](#configfiletype)_ | Type controls when the file content is materialized.<br />cloud-init files use raw inline content and are written during<br />cloud-init. agent-applied files use contentFrom and are written later<br />by dpu-agent. Defaults to cloud-init when omitted. | cloud-init | Enum: [cloud-init agent-applied] <br />Optional: \{\} <br /> |
| `path` _string_ | Path is the path of the file to be written. |  | Required: \{\} <br /> |
| `operation` _[DPUFlavorFileOp](#dpuflavorfileop)_ | Operation is the operation to be performed on the file. |  | Enum: [override append] <br />Optional: \{\} <br /> |
| `raw` _string_ | Raw is the inline file content.<br />Supported only when type is cloud-init. When type is omitted, type<br />defaults to cloud-init and raw must be set. |  | Optional: \{\} <br /> |
| `contentFrom` _[ConfigFileContentSource](#configfilecontentsource)_ | ContentFrom references external content for the file.<br />Supported only when type is agent-applied. |  | Optional: \{\} <br /> |
| `permissions` _string_ | Permissions are the permissions to be set on the file. |  | Optional: \{\} <br /> |


#### ConfigFileContentSource







_Appears in:_
- [ConfigFile](#configfile)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `configMapKeyRef` _[ConfigMapKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#configmapkeyselector-v1-core)_ | ConfigMapKeyRef selects a key from a ConfigMap in the DPU namespace. |  | Optional: \{\} <br /> |


#### ConfigFileType

_Underlying type:_ _string_

ConfigFileType defines when a config file is materialized.

_Validation:_
- Enum: [cloud-init agent-applied]

_Appears in:_
- [ConfigFile](#configfile)

| Field | Description |
| --- | --- |
| `cloud-init` |  |
| `agent-applied` |  |


#### ContainerdConfig







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `registryEndpoint` _string_ | RegistryEndpoint is the endpoint of the container registry. |  | Optional: \{\} <br /> |
| `sandboxImage` _string_ | SandboxImage overrides the containerd sandbox (pause) image on the DPU,<br />e.g. "registry.example.com/pause:3.9". When unset, the sandbox image<br />shipped with the BFB is kept. |  | MaxLength: 512 <br />MinLength: 1 <br />Optional: \{\} <br /> |


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


#### DPUClusterEtcdEncryptionAtRestStatus



DPUClusterEtcdEncryptionAtRestStatus defines the observed
encryption-at-rest state for a DPUCluster.



_Appears in:_
- [DPUClusterStatus](#dpuclusterstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `provider` _string_ | Provider is the committed encryption-at-rest provider for the cluster. |  | Enum: [staticKey vaultKMS] <br />Required: \{\} <br /> |
| `staticKey` _[DPUClusterStaticKeyEncryptionStatus](#dpuclusterstatickeyencryptionstatus)_ | StaticKey exposes staticKey-specific observed state. |  | Optional: \{\} <br /> |


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
| `type` _string_ | Type of the cluster with few supported values<br />static - existing cluster that is deployed by user. For DPUCluster of<br />this type, the kubeconfig field must be set.<br />kamaji - DPF managed cluster. The kamaji-cluster-manager will create a<br />DPU cluster on behalf of this CR.<br />$(others) - any string defined by ISVs, such type names must start with<br />a prefix. |  | Pattern: `kamaji\|static\|[^/]+/.*` <br />Required: \{\} <br /> |
| `maxNodes` _integer_ | MaxNodes is the max amount of node in the cluster | 1000 | Maximum: 3000 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `kubeconfig` _string_ | Kubeconfig is the secret that contains the admin kubeconfig |  | Optional: \{\} <br /> |
| `clusterEndpoint` _[ClusterEndpointSpec](#clusterendpointspec)_ | ClusterEndpoint contains configurations of the cluster entry point |  | Optional: \{\} <br /> |


#### DPUClusterStaticKeyEncryptionStatus



DPUClusterStaticKeyEncryptionStatus defines observed staticKey
encryption-at-rest state.



_Appears in:_
- [DPUClusterEtcdEncryptionAtRestStatus](#dpuclusteretcdencryptionatreststatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `activeKeyRef` _[ObservedSecretKeyRef](#observedsecretkeyref)_ | ActiveKeyRef is the source Secret observed for the currently active key.<br />This field is informational and must not be used by controllers to<br />select desired key material. |  | Optional: \{\} <br /> |


#### DPUClusterStatus



DPUClusterStatus defines the observed state of DPUCluster



_Appears in:_
- [DPUCluster](#dpucluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[ClusterPhase](#clusterphase)_ |  | Pending | Enum: [Pending Creating Ready NotReady Failed] <br />Optional: \{\} <br /> |
| `version` _string_ | Version is the K8s control-plane version of the cluster |  | Optional: \{\} <br /> |
| `nodesCount` _integer_ | NodesCount is the number of DPUs assigned to the cluster |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `etcdEncryptionAtRest` _[DPUClusterEtcdEncryptionAtRestStatus](#dpuclusteretcdencryptionatreststatus)_ | EtcdEncryptionAtRest exposes the observed encryption-at-rest state for<br />the cluster. |  | Optional: \{\} <br /> |
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



DPUDeviceClusterSpec holds node labels and annotations propagated from
DPUDevice to the DPU and cluster node.



_Appears in:_
- [DPUDeviceSpec](#dpudevicespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeLabels` _object (keys:string, values:string)_ | NodeLabels specifies labels to be added to the DPU cluster node for<br />this device. |  | Optional: \{\} <br /> |
| `nodeAnnotations` _object (keys:string, values:string)_ | NodeAnnotations specifies annotations to be added to the DPU cluster node<br />for this device. |  | Optional: \{\} <br /> |


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
| `psid` _string_ | PSID is the Product Serial ID of the device.<br />It's used to track the device's lifecycle and for inventory management.<br />This value is immutable and should not be changed once set.<br />Example: "MT_0001234567", "MT25066004C7"<br />Deprecated: This field is deprecated and will be removed in a future<br />version. Use status.psid instead. |  | Optional: \{\} <br /> |
| `serialNumber` _string_ | SerialNumber is the serial number of the device.<br />It's used to track the device's lifecycle and for inventory management.<br />This value is immutable and should not be changed once set.<br />Example: "MT_0001234567", "MT25066004C7" |  | MinLength: 1 <br />Required: \{\} <br /> |
| `opn` _string_ | OPN is the Ordering Part Number of the device. It's used to track the<br />device's compatibility with different software versions. This value is<br />immutable and should not be changed once set.<br />Example: "900-9D3B4-00SV-EA0"<br />Deprecated: This field is deprecated and will be removed in a future<br />version. Use status.opn instead. |  | Optional: \{\} <br /> |
| `bmcIp` _string_ | BMCIP is the IP address of the BMC (Base Management Controller) on the<br />device. This is used for remote management and monitoring of the device.<br />When this value changes, the controller re-establishes secure<br />communication with the BMC and re-issues the IP-bound BMC server<br />certificate without reprovisioning the DPU.<br />Example: "10.1.2.3" |  | Format: ipv4 <br />Optional: \{\} <br /> |
| `bmcPort` _integer_ | BMCPort is the port number of the BMC (Base Management Controller) on the<br />device. This is used for remote management and monitoring of the device.<br />This value is immutable and should not be changed once set.<br />Example: 443 | 443 | Minimum: 1 <br />Optional: \{\} <br /> |
| `numberOfPFs` _integer_ | NumberOfPFs is the number of PFs on the device.<br />This value is immutable and should not be changed once set.<br />Example: 1 | 1 | Minimum: 1 <br />Optional: \{\} <br /> |
| `nicDeviceCount` _integer_ | NICDeviceCount is the expected number of NIC devices used by dpu-agent<br />provisioning. Valid range is 1 to 8. When unspecified, it defaults to 8. |  | Maximum: 8 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `pf0Name` _string_ | PF0Name is the name of the PF0 on the device.<br />This value is immutable and should not be changed once set.<br />Example: "eth0"<br />Deprecated: This field is deprecated and will be removed in a future<br />version. Use status.pf0Name instead. |  | Optional: \{\} <br /> |
| `bmcCredentialSecretName` _string_ | BMCCredentialSecretName is the name of a Secret in the same namespace<br />containing per-device BMC credentials. The secret must contain a<br />"password" key with the BMC credential value. If specified, this password<br />takes precedence over the shared bmc-shared-password secret. |  | Optional: \{\} <br /> |
| `bmcFactoryResetPolicy` _[BMCFactoryResetPolicy](#bmcfactoryresetpolicy)_ | BMCFactoryResetPolicy controls whether DPF resets this DPU BMC to factory<br />defaults while initializing the DPUDevice. The reset clears all BMC<br />configuration, including network settings, so the BMC must obtain its<br />address via DHCP.<br />- OnInitialization (default): reset the BMC once, while the DPUDevice is<br />  initialized.<br />- Never: never reset.<br />The value is honored only until the factory reset step finishes; changing<br />it after BMCFactoryResetReady is True has no effect. The policy applies<br />only when the install interface is Redfish. Other install interfaces<br />never reset the BMC; BMCFactoryResetReady reports FactoryResetSkipped<br />unless a reset was already recorded under Redfish. | OnInitialization | Enum: [OnInitialization Never] <br />Optional: \{\} <br /> |
| `cluster` _[DPUDeviceClusterSpec](#dpudeviceclusterspec)_ | Specifies details on the K8S cluster to join |  | Optional: \{\} <br /> |
| `values` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Values contains free-form per-device values used to render a<br />DPUFlavorTemplate into a concrete generated DPUFlavor for this device. |  | Optional: \{\} <br /> |


#### DPUDeviceStatus







_Appears in:_
- [DPUDevice](#dpudevice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `psid` _string_ | PSID is the Product Serial ID of the device.<br />It's used to track the device's lifecycle and for inventory management.<br />This value is discovered and should not be changed once set.<br />Example: "MT_0001234567", "MT25066004C7" |  | Optional: \{\} <br /> |
| `serialNumber` _string_ | SerialNumber is the serial number of the device.<br />It's used to track the device's lifecycle and for inventory management.<br />This value is discovered and should not be changed once set.<br />Example: "MT_0001234567", "MT25066004C7" |  | Optional: \{\} <br /> |
| `opn` _string_ | OPN is the Ordering Part Number of the device. It's used to track the<br />device's compatibility with different software versions. This value is<br />discovered and should not be changed once set.<br />Example: "900-9D3B4-00SV-EA0" |  | Optional: \{\} <br /> |
| `bmcIp` _string_ | BMCIP is the IP address of the BMC (Base Management Controller) on the<br />device. This is used for remote management and monitoring of the device.<br />The controller synchronizes this value from spec.bmcIp after reconciling<br />an address change.<br />Example: "10.1.2.3" |  | Format: ipv4 <br />Optional: \{\} <br /> |
| `bmcPort` _integer_ | BMCPort is the port number of the BMC (Base Management Controller) on the<br />device. This is used for remote management and monitoring of the device.<br />This value is immutable and should not be changed once set.<br />Example: 443 | 443 | Minimum: 1 <br />Optional: \{\} <br /> |
| `pciAddress` _string_ | PCIAddress is the PCI address of the device in the host system.<br />Example: "0000-03-00", "03-00" |  | Optional: \{\} <br /> |
| `pf0Name` _string_ | PF0Name is the name of the PF0 on the device.<br />Example: "eth0" |  | Optional: \{\} <br /> |
| `pf0Mac` _string_ | PF0MAC is the MAC address of the PF0 on the device.<br />Example: "00:00:00:00:00:00" |  | Pattern: `^([0-9A-Fa-f]\{2\}[:-])\{5\}([0-9A-Fa-f]\{2\})$` <br />Optional: \{\} <br /> |
| `dpuType` _[DPUType](#dputype)_ | DPUType is the type of the DPU. | Unknown | Enum: [Unknown BlueField2 BlueField3 BlueField4] <br />Optional: \{\} <br /> |
| `dpuMode` _[DpuModeType](#dpumodetype)_ | DPUMode is the mode of the DPU. | dpu | Enum: [dpu zero-trust nic] <br />Optional: \{\} <br /> |
| `secureBoot` _[SecureBootStatus](#securebootstatus)_ | SecureBoot indicates the current UEFI Secure Boot state. |  | Optional: \{\} <br /> |
| `bmcCredentialSecretName` _string_ | BMCCredentialSecretName is the name of the Secret last used successfully<br />for BMC authentication. |  | Optional: \{\} <br /> |
| `bmcFactoryResetRequestTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | BMCFactoryResetRequestTime is when DPF submitted ResetToDefaults to the<br />BMC. It is nil until the request has been accepted, and once set it is<br />never cleared: it is what keeps the BMC from being reset again for<br />this DPUDevice. |  | Optional: \{\} <br /> |
| `bmcServerCertificate` _[CertificateStatus](#certificatestatus)_ | BMCServerCertificate reports the BMC mTLS server certificate<br />rotation state. |  | Optional: \{\} <br /> |
| `caTrustBundle` _[TrustBundleStatus](#trustbundlestatus)_ | CATrustBundle stores trust bundle reconciliation progress<br />for the DPUDevice. |  | Optional: \{\} <br /> |
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
| `workers` _integer_ | Workers defines the number of workers to use for the scan (default 1<br />worker for each 255 IPs in the range) |  | Optional: \{\} <br /> |


#### DPUDiscoveryStatus



DPUDiscoveryStatus defines the observed state of DPUDiscovery



_Appears in:_
- [DPUDiscovery](#dpudiscovery)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `observedGeneration` _integer_ | ObservedGeneration is the generation of the spec that was last applied<br />by a successful scan. When spec (e.g. IP range) changes, reconciliation<br />runs a scan immediately instead of waiting for the next interval. |  | Optional: \{\} <br /> |
| `lastScanTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastScanTime is the timestamp of the last successful scan |  | Optional: \{\} <br /> |
| `foundDPUs` _integer_ | FoundDPUs is the list of discovered DPU BMC IPs |  | Optional: \{\} <br /> |


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


#### DPUFlavorDMA



DPUFlavorDMA configures the SNAP DMA SF. The agent picks the ECPF; sfnum is
8000 and MAC is derived.



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ | Enabled creates the SNAP DMA SF on BlueField-4 socket-direct systems. |  | Optional: \{\} <br /> |


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
| `kernelParameters` _string array_ | KernelParameters are the kernel parameters to be set in the grub<br />configuration. |  | Optional: \{\} <br /> |


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
| `rawConfigScript` _string_ | RawConfigScript is the raw configuration script for OVS.<br />The DPU agent runs this script once per boot. A restart of the agent in<br />the same boot does not re-run it; a reboot or power cycle does. |  | Optional: \{\} <br /> |


#### DPUFlavorSpec



DPUFlavorSpec defines the content of DPUFlavor.
Config-only fields (grub, sysctl, nvconfig, ovs, packages, systemdServices,
containerdConfig, agent-applied configFiles, serviceReadiness,
ewNicConfigurations, dma, virtualFunctions) may be updated in place.
Immutable fields require a new DPUFlavor.



_Appears in:_
- [DPUFlavor](#dpuflavor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `grub` _[DPUFlavorGrub](#dpuflavorgrub)_ | Grub contains the grub configuration for the DPUFlavor. |  | Optional: \{\} <br /> |
| `sysctl` _[DPUFLavorSysctl](#dpuflavorsysctl)_ | Sysctl contains the sysctl configuration for the DPUFlavor. |  | Optional: \{\} <br /> |
| `nvconfig` _[NVConfig](#nvconfig) array_ | NVConfig contains the device-specific configuration (firmware settings,<br />device parameters). Each entry specifies a device (wildcard '*', or port<br />identifiers 'p0'/'P0'/'p1'/'P1') and its parameters. If device is '*' or<br />unspecified (defaults to '*'), it applies to all devices and must be the<br />only entry. Each device (including unspecified as '*') must be unique<br />across all nvconfig entries (case-insensitive). Validation enforces:<br />device enum values, parameter format (KEY=VALUE), case-insensitive<br />uniqueness, and size limits. |  | MaxItems: 3 <br />Optional: \{\} <br /> |
| `scalableFunctions` _[ScalableFunction](#scalablefunction) array_ | ScalableFunctions is the list of SF groups to create on the DPU, or on<br />the host when hostDevice is set. Count is per selected device.<br />Over-subscribe can trigger failures at create time. This field is<br />immutable. Changing it requires a new DPUFlavor. For backward<br />compatibility, when both this SF list and VF list are empty, SF counts<br />are still derived from PF_TOTAL_SF (removed in a future release). |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `virtualFunctions` _[VirtualFunction](#virtualfunction) array_ | VirtualFunctions is the list of VF groups to create. Count is per<br />selected device. Groups ending up on the same device sum to a single<br />`sriov_numvfs` and then list order assigns contiguous index ranges.<br />Over-subscribe can trigger failures at create time. Editing this field<br />reprovisions the DPU. |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `dma` _[DPUFlavorDMA](#dpuflavordma)_ | DMA configures the SNAP DMA SF. The agent picks the ECPF; sfnum is 8000<br />and MAC is derived. Ignored except on BlueField-4. |  | Optional: \{\} <br /> |
| `ovs` _[DPUFlavorOVS](#dpuflavorovs)_ | OVS contains the OVS configuration for the DPUFlavor. |  | Optional: \{\} <br /> |
| `bfcfgParameters` _string array_ | BFCfgParameters are the parameters to be set in the bf.cfg file. |  | Optional: \{\} <br /> |
| `configFiles` _[ConfigFile](#configfile) array_ | ConfigFiles are the files to be written on the DPU. |  | Optional: \{\} <br /> |
| `packages` _[PackageSpec](#packagespec) array_ | Packages are the packages to reconcile on the node. |  | MaxItems: 64 <br />Optional: \{\} <br /> |
| `systemdServices` _[SystemdServiceSpec](#systemdservicespec) array_ | SystemdServices are the systemd services to manage on the node. |  | MaxItems: 64 <br />Optional: \{\} <br /> |
| `containerdConfig` _[ContainerdConfig](#containerdconfig)_ | ContainerdConfig contains the configuration for containerd. |  | Optional: \{\} <br /> |
| `dpuResources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | DPUResources indicates the minimum amount of resources needed for a BFB<br />with that flavor to be installed on a DPU. Using this field, the<br />controller can understand if that flavor can be installed on a<br />particular DPU. It should be set to the total amount of resources the<br />system needs + the resources that should be made available for<br />DPUServices to consume. |  | Optional: \{\} <br /> |
| `systemReservedResources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | SystemReservedResources indicates the resources that are consumed by<br />the system (OS, OVS, DPF system etc) and are not made available for<br />DPUServices to consume. DPUServices can consume the difference between<br />DPUResources and SystemReservedResources. This field must not be<br />specified if dpuResources are not specified. |  | Optional: \{\} <br /> |
| `dpuMode` _[DpuModeType](#dpumodetype)_ | DpuMode is deprecated and no longer used by provisioning workflows.<br />Deployment mode is sourced from DPFOperatorConfig and exposed on<br />DPU.status.deploymentMode. |  | Enum: [dpu zero-trust nic] <br />Optional: \{\} <br /> |
| `hostNetworkInterfaceConfigs` _[NetworkInterfaceConfig](#networkinterfaceconfig) array_ | HostNetworkInterfaceConfigs contains the configuration for the host-side<br />network interfaces. |  | Optional: \{\} <br /> |
| `ewNicConfigurations` _[NicConfiguration](#nicconfiguration) array_ | EWNicConfigurations lists per-NIC configuration for the E/W NICs.<br />Only the first entry is applied in this release; additional entries are<br />ignored until a future release adds multi-NIC support. The field is<br />modeled as a list now so the API shape does not need to change when<br />multiple entries are supported. |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `serviceReadiness` _[ServiceReadiness](#servicereadiness)_ | serviceReadiness configures the Service Readiness phase. |  | Optional: \{\} <br /> |


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



DPUFlavorTemplateSpec defines the content of a DPUFlavorTemplate. The
template body is rendered per-DPU against DPUDevice.spec.values to produce a
concrete DPUFlavor.



_Appears in:_
- [DPUFlavorTemplate](#dpuflavortemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `template` _string_ | Template is the DPUFlavor body as a YAML/JSON string with Go template<br />actions (delimited by double curly braces). It is rendered against<br />DPUDevice.spec.values and the result is unmarshalled into a typed<br />DPUFlavor and validated by DPUFlavor admission when the generated<br />flavor is created. Referencing a key that is absent from the device<br />values fails the render, and the "define", "block" and "template"<br />actions are rejected. It should not contain dpuResources or<br />systemReservedResources: the structured fields below take precedence<br />and, when set, override anything the rendered body contains. |  | MaxLength: 1048576 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `dpuResources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | DPUResources is resource-fitting metadata mirrored from DPUFlavor. It is<br />NOT templated: when set it is stamped onto every generated DPUFlavor and<br />takes precedence over anything in the rendered body. |  | Optional: \{\} <br /> |
| `systemReservedResources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | SystemReservedResources is resource-fitting metadata mirrored from<br />DPUFlavor. It is NOT templated: when set it is stamped onto every<br />generated DPUFlavor and takes precedence over anything in the rendered<br />body. |  | Optional: \{\} <br /> |






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
| `multiDPUOperationsSyncWaitTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | MultiDPUOperationsSyncWaitTime  is the wait time between DPUs on the<br />same node. |  | Optional: \{\} <br /> |
| `maxUnavailableDPUNodes` _integer_ | MaxUnavailableDPUNodes is the maximum number of DPUNodes that are<br />unavailable during the node effect period. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### DPUNodeSpec



DPUNodeSpec defines the desired state of DPUNode



_Appears in:_
- [DPUNode](#dpunode)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeRebootMethod` _[NodeRebootMethod](#noderebootmethod)_ | Defines the method for rebooting the host.<br />One of the following options can be chosen for this field:<br />   - "external": Reboot the host via an external means, not controlled<br />     by the DPU controller.<br />   - "script": Reboot the host by executing a custom script.<br />   - "hostAgent": Use the host agent to reboot the host.<br />   - "none": Do not reboot a host through the DPUNode.<br />"hostAgent" is the default value. | \{ hostAgent:map[] \} | Optional: \{\} <br /> |
| `nodeDMSAddress` _[DMSAddress](#dmsaddress)_ | The IP address and port where the DMS is exposed. Only applicable if<br />dpuInstallInterface is set to gNOI.<br />Deprecated: this field is no longer used. |  | Optional: \{\} <br /> |
| `dpus` _[DPURef](#dpuref) array_ | A map containing names of each DPUDevice attached to the node. |  | Optional: \{\} <br /> |


#### DPUNodeStatus



DPUNodeStatus defines the observed state of DPUNode



_Appears in:_
- [DPUNode](#dpunode)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions represent the latest available observations of an object's<br />state. |  | Type: array <br />Optional: \{\} <br /> |
| `dpuInstallInterface` _string_ | The name of the interface which will be used to install the bfb image,<br />can be one of hostAgent,redfish |  | Enum: [gNOI hostAgent redfish] <br />Optional: \{\} <br /> |
| `kubeNodeRef` _string_ | The name of the Kubernetes Node object that this DPUNode represents.<br />This field is optional and only relevant if the x86 host is part of the<br />DPF Kubernetes cluster. |  | Optional: \{\} <br /> |
| `rebootInProgress` _boolean_ | RebootInProgress indicates if the node is in the process of rebooting. |  | Optional: \{\} <br /> |
| `rebootMethod` _[RebootMethodType](#rebootmethodtype)_ | RebootMethod is the host operation required by the child DPUs in<br />DPURebooting phase: PowerCycle or SystemLevelReset. |  | Enum: [Unknown NoAction PowerCycle SystemReboot SystemLevelReset FirmwareReset DPUWarmReboot] <br />Optional: \{\} <br /> |




#### DPUOutdated



DPUOutdated reports that the DPU has drifted from its owning DPUSet's
DPUTemplate in a way that requires the DPU to be reprovisioned. The struct
is presence-based: when the DPU matches the template, DPUStatus.Outdated is
nil. The DPUSet controller is the sole writer.



_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `timeStamp` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | TimeStamp records when this drift was first observed for the current<br />Reason. It is preserved across reconciles as long as Reason is unchanged. |  | Required: \{\} <br /> |
| `reason` _string_ | Reason is the machine-readable drift code (e.g. OutdatedBFB). When more<br />than one template field has drifted, Reason reports the first in fixed<br />precedence order: BFB -> DPUFlavor -> SecureBoot -> BlueFieldSoftware. |  | Required: \{\} <br /> |
| `message` _string_ | Message is a human-readable summary that lists every drifted field,<br />e.g. "DPU template has changed (BFB: bfb-v1 -> bfb-v2, DPUFlavor: ...)". |  | Required: \{\} <br /> |


#### DPUPhase

_Underlying type:_ _string_

DPUPhase describes current state of DPU.
Only one of the following state may be specified.
Default is Initializing.

_Validation:_
- Enum: [Initializing Node Effect Pending Update Firmware Config FW Parameters Prepare BFB OS Installing DPU Config DPU Cluster Config Host Network Configuration Service Readiness Ready Error Deleting Rebooting Perform ARM Force Restart Initialize Interface Node Effect Removal Checking Host Reboot Required]

_Appears in:_
- [DPUSetStatus](#dpusetstatus)
- [DPUStatus](#dpustatus)

| Field | Description |
| --- | --- |
| `Initializing` | DPUInitializing is the first phase after the DPU is created.<br /> |
| `Node Effect` | DPUNodeEffect means the controller will handle the node effect<br />provided by the user.<br /> |
| `Pending` | DPUPending means the controller is waiting for the BFB to be ready.<br /> |
| `Prepare BFB` | DPUPrepareBFB means the controller is preparing the BFB and bf.cfg to<br />be installed to DPU<br /> |
| `Update Firmware` | DPUUpdateFirmware means the controller will update the DPU firmware<br />for BlueField4.<br /> |
| `DPU Config` | DPUConfig means the DPU agent will configure the DPU<br /> |
| `Config FW Parameters` | DPUConfigFWParameters means the controller will manipulate DPU firmware,<br />e.g., set DPU mode, check firmware version<br /> |
| `Initialize Interface` | DPUInitializeInterface means the controller will intitialize the<br />interface used to provision the DPUs, e.g., create the DMS pod, set up<br />RedFish account.<br /> |
| `OS Installing` | DPUOSInstalling means the controller will provision the DPU through<br />the DMS gNOI interface.<br /> |
| `DPU Cluster Config` | DPUClusterConfig  means the node configuration and Kubernetes Node join<br />procedure are in progress .<br /> |
| `Host Network Configuration` | DPUHostNetworkConfiguration means the host network configuration<br />is running.<br /> |
| `Service Readiness` | DPUServiceReadiness waits for DPU services to become ready before the DPU<br />is put into service. When the flavor configures a gate the phase blocks<br />on it; it also waits for the DPU agent to release a host OS init hold<br />when one was requested.<br /> |
| `Node Effect Removal` | DPUNodeEffectRemoval means the controller will remove the node effect<br />from the DPU.<br /> |
| `Ready` | DPUReady means the DPU is ready to use.<br /> |
| `Error` | DPUError means error occurred.<br /> |
| `Deleting` | DPUDeleting means the DPU CR will be deleted, controller will do<br />some cleanup works.<br /> |
| `Rebooting` | DPURebooting means the host of DPU is rebooting.<br /> |
| `Perform ARM Force Restart` | DPUPerformArmForceRestart means ARM ForceRestart operations are in<br />progress for Secure Boot configuration.<br /> |


#### DPUReconfigStatus



DPUReconfigStatus is the in-place reconfig state of one DPU.
DPUSet is the only writer.



_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `inProgress` _boolean_ | InProgress permits the DPU controller to jump to Node Effect. It is<br />cleared when this DPU is Ready again for this round. Do not set it<br />together with Outdated. |  | Optional: \{\} <br /> |
| `round` _integer_ | Round is the DPUSet round assigned when InProgress was set.<br />Zero means InProgress has not been set for a round. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `completedRound` _integer_ | CompletedRound is the last round that reached Ready. Zero means this DPU<br />has not finished an in-place round. While it equals DPUSet<br />status.dpuReconfig.round, this DPU is not started again for the<br />same annotation. |  | Minimum: 1 <br />Optional: \{\} <br /> |


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


#### DPUSetReconfigStatus



DPUSetReconfigStatus is one annotation round of in-place DPU reconfig.
A nil DPUSetStatus.DPUReconfig means no round has started. The controller
sets round, active, and startedAt together.



_Appears in:_
- [DPUSetStatus](#dpusetstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `round` _integer_ | Round identifies the current or last round. It increases by one when a<br />new annotation round starts and is not reset when the annotation is<br />removed. Zero means no round has started. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `active` _boolean_ | Active is true while this annotation round is not finished. |  | Optional: \{\} <br /> |
| `startedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | StartedAt is when this round became active. It distinguishes a DPU that<br />was already Ready before the round from one that first becomes Ready<br />during it. The controller sets it with round and active, and never<br />leaves it zero. |  | Optional: \{\} <br /> |


#### DPUSetSpec



DPUSetSpec defines the desired state of DPUSet



_Appears in:_
- [DPUSet](#dpuset)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `strategy` _[DPUSetStrategy](#dpusetstrategy)_ | The rolling update strategy to use to updating existing DPUs with new<br />ones. |  | Required: \{\} <br /> |
| `dpuNodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Select the DPUNodes with specific labels |  | Optional: \{\} <br /> |
| `dpuSelector` _object (keys:string, values:string)_ | Select the DPU with specific labels<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use DPUDeviceSelector instead. |  | Optional: \{\} <br /> |
| `dpuDeviceSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUDeviceSelector defines the selector for DPUDevices that the DPUSet<br />should target and should create a DPU for. |  | Optional: \{\} <br /> |
| `dpuTemplate` _[DPUTemplate](#dputemplate)_ | Object that describes the DPU that will be created if insufficient<br />replicas are detected |  | Required: \{\} <br /> |


#### DPUSetStatus



DPUSetStatus defines the observed state of DPUSet



_Appears in:_
- [DPUSet](#dpuset)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuStatistics` _object (keys:[DPUPhase](#dpuphase), values:integer)_ | DPUStatistics is a map of DPUPhase to the number of DPUs in that phase. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the<br />last time it was patched. |  | Optional: \{\} <br /> |
| `dpuReconfig` _[DPUSetReconfigStatus](#dpusetreconfigstatus)_ | DPUReconfig is the in-place reconfig round owned by the DPUSet<br />controller. The user starts a round with annotation<br />provisioning.dpu.nvidia.com/inplace-reconfig=true.<br />Nil means no round has started. |  | Optional: \{\} <br /> |


#### DPUSetStrategy







_Appears in:_
- [DPUSetSpec](#dpusetspec)
- [DPUs](#dpus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[StrategyType](#strategytype)_ | Can be "OnDelete" or "RollingUpdate". |  | Enum: [OnDelete RollingUpdate] <br />Required: \{\} <br /> |
| `rollingUpdate` _[RollingUpdateDPU](#rollingupdatedpu)_ | Rolling update config params. Present only if StrategyType =<br />RollingUpdate. |  | Optional: \{\} <br /> |


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
| `dpuFlavor` _string_ | DPUFlavor is the name of the DPUFlavor that will be used to<br />deploy the DPU. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `astraEnabled` _boolean_ | AstraEnabled indicates whether E/W NIC configuration (Astra) is enabled |  | Optional: \{\} <br /> |
| `secureBoot` _boolean_ | SecureBoot specifies whether UEFI Secure Boot should be enabled. |  | Optional: \{\} <br /> |
| `bmcIP` _string_ | BMCIP is the ip address of the DPU BMC<br />Deprecated: Use BMCIP from DPUDevice instead. |  | Optional: \{\} <br /> |


#### DPUStatus



DPUStatus defines the observed state of DPU



_Appears in:_
- [DPU](#dpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[DPUPhase](#dpuphase)_ | The current state of DPU. | Initializing | Enum: [Initializing Node Effect Pending Update Firmware Config FW Parameters Prepare BFB OS Installing DPU Config DPU Cluster Config Host Network Configuration Service Readiness Ready Error Deleting Rebooting Perform ARM Force Restart Initialize Interface Node Effect Removal Checking Host Reboot Required] <br />Optional: \{\} <br /> |
| `previousPhase` _[DPUPhase](#dpuphase)_ | PreviousPhase is the last non-empty Phase before the current Phase, set<br />by the controller when Phase transitions. It may be unset during early<br />initialization (empty Phase) or until the first transition from a<br />non-empty Phase. Internal controller tracking only. |  | Enum: [Initializing Node Effect Pending Update Firmware Config FW Parameters Prepare BFB OS Installing DPU Config DPU Cluster Config Host Network Configuration Service Readiness Ready Error Deleting Rebooting Perform ARM Force Restart Initialize Interface Node Effect Removal Checking Host Reboot Required] <br />Optional: \{\} <br /> |
| `outdated` _[DPUOutdated](#dpuoutdated)_ | Outdated, when present, indicates the DPU has drifted from its owning<br />DPUSet's DPUTemplate and needs to be reprovisioned. Set by the DPUSet<br />controller; absent when the DPU matches the template. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions represents the provisioning lifecycle conditions. |  | Optional: \{\} <br /> |
| `operationalConditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | OperationalConditions represents aggregated operational readiness<br />conditions. These conditions reflect the runtime health and readiness of<br />DPU services and node health, separate from the provisioning lifecycle<br />represented by Conditions. |  | Optional: \{\} <br /> |
| `bfbFile` _string_ | BFBFile is the path to the BFB file |  | Optional: \{\} <br /> |
| `bfCFGFile` _string_ | BFCFGFile is the path to the bf.cfg |  | Optional: \{\} <br /> |
| `bfbVersion` _string_ | bfb version of this DPU |  | Optional: \{\} <br /> |
| `dpfVersion` _string_ | DPF version used to install this DPU |  | Optional: \{\} <br /> |
| `pciDevice` _string_ | pci device information of this DPU |  | Optional: \{\} <br /> |
| `requiredReset` _boolean_ | whether require reset of DPU |  | Optional: \{\} <br /> |
| `firmware` _[Firmware](#firmware)_ | the firmware information of DPU |  | Optional: \{\} <br /> |
| `addresses` _[NodeAddress](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#nodeaddress-v1-core) array_ | The DPU node's IP addresses |  | Optional: \{\} <br /> |
| `dpuInstallInterface` _string_ | the name of the interface which will be used to install the bfb image,<br />and communicate with DPU, can be one of hostAgent,redfish |  | Enum: [gNOI hostAgent redfish] <br />Optional: \{\} <br /> |
| `postProvisioningNodeEffect` _boolean_ | Indicates that node effect was triggered by<br />post-provisioning label changes |  | Optional: \{\} <br /> |
| `postProvisioningDPUConfig` _boolean_ | PostProvisioningDPUConfig must be set before entering Node Effect for an<br />in-place flavor reconfig, so Node Effect exits to DPU Config instead of<br />Initialize Interface. The DPU controller owns this field. If both this<br />and PostProvisioningNodeEffect would be set, only this one is set. |  | Optional: \{\} <br /> |
| `dpuReconfig` _[DPUReconfigStatus](#dpureconfigstatus)_ | DPUReconfig is the in-place reconfig round state for this DPU. DPUSet<br />writes it. The DPU controller reads inProgress and does not write these<br />fields. Nil means no round has been assigned. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object<br />the last time it was patched. |  | Optional: \{\} <br /> |
| `dpuType` _[DPUType](#dputype)_ | The type of the DPU | Unknown | Enum: [Unknown BlueField2 BlueField3 BlueField4] <br />Optional: \{\} <br /> |
| `agentLastStartupTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | AgentLastStartupTime is the time when the DPU agent was last started.<br />This is copied from agentStatus.lastStartupTime. |  | Optional: \{\} <br /> |
| `agentStatus` _[AgentStatus](#agentstatus)_ | AgentStatus contains the information reported from inside the DPU |  | Optional: \{\} <br /> |
| `rebootStatus` _[RebootStatus](#rebootstatus)_ | RebootStatus contains host reboot progress.<br />DPU controller derives user-facing DPUCondRebooted from this status. |  | Optional: \{\} <br /> |
| `dpuMode` _[DpuModeType](#dpumodetype)_ | The mode of the DPU | dpu | Enum: [dpu zero-trust nic] <br />Optional: \{\} <br /> |
| `deploymentMode` _[DeploymentMode](#deploymentmode)_ | DeploymentMode is copied from DPFOperatorConfig.spec.deploymentMode by<br />the controller. This field is read-only for users. |  | Enum: [zero-trust host-trusted] <br />Optional: \{\} <br /> |
| `hostless` _boolean_ | Hostless indicates that the DPU is attached to a system-managed synthetic<br />DPUNode rather than a physical host. |  | Optional: \{\} <br /> |
| `secureBoot` _[SecureBootStatus](#securebootstatus)_ | SecureBoot indicates the current UEFI Secure Boot state. |  | Optional: \{\} <br /> |
| `identityMode` _[IdentityMode](#identitymode)_ | IdentityMode records which authentication mechanism the DPU Agent uses to<br />reach the management-cluster kube-apiserver. Stamped exactly once by<br />the DPU controller during phase Initializing (nil guard); immutable<br />thereafter. Pre-SPIFFE legacy DPUs have IdentityMode unset (nil) which<br />consumers MUST treat semantically as bootstrap-token. |  | Enum: [spiffe bootstrap-token] <br />Optional: \{\} <br /> |
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
| `dpuFlavor` _string_ | DPUFlavor is the name of the DPUFlavor that will be used to deploy the<br />DPU. Mutually exclusive with DPUFlavorTemplate. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `dpuFlavorTemplate` _string_ | DPUFlavorTemplate is the name of a DPUFlavorTemplate that is rendered<br />per-DPU (against DPUDevice.spec.values) into a generated DPUFlavor.<br />Mutually exclusive with DPUFlavor. |  | MinLength: 1 <br />Optional: \{\} <br /> |
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

DeploymentMode describes the cluster deployment model for provisioning
(zero-trust vs host-trusted). This type is intentionally duplicated from
operator/v1alpha1.DeploymentMode: the provisioning API must not import the
operator API group. Keep values and semantics aligned with
DPFOperatorConfig.spec.deploymentMode.



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
| `pldmFwBundle` _object (keys:string, values:string)_ | PldmFwBundle maps each PSID to the local path of its downloaded PLDM<br />firmware bundle. Keys are matched case-insensitively against the<br />BMC-reported DPUDevice PSID. |  |  |
| `platformPldmFwBundle` _string_ | PlatformPldmFwBundle is the local path of the downloaded platform PLDM<br />firmware bundle. |  |  |
| `osIso` _string_ | OsIso is the local path of the downloaded OS ISO. |  |  |
| `nicFw` _string_ | NicFw is the local path of the E/W NIC firmware binary, either<br />downloaded from spec.nicFw or unpacked from the platform PLDM firmware<br />bundle. |  |  |


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
| `zero-trust` | ZeroTrustMode is deprecated and kept for backward compatibility with<br />DPUFlavor.spec.dpuMode.<br />Deprecated: DPUFlavor.spec.dpuMode is deprecated; use<br />DPFOperatorConfig.spec.deploymentMode.<br /> |


#### EWNICDeviceFailure



EWNICDeviceFailure describes an E/W NIC device whose runtime
configuration apply failed.



_Appears in:_
- [EWNICRuntimeConfigStatus](#ewnicruntimeconfigstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pciAddress` _string_ | pciAddress is the device's PCI address without the function number<br />(Domain:Bus:Device, e.g. 0000:03:00), shared by all ports of the device. |  | Required: \{\} <br /> |
| `reason` _string_ | reason is a CamelCase reason for the failure,<br />e.g. NoCarrier or ApplyFailed. |  | Required: \{\} <br /> |
| `message` _string_ | message is the error returned for the device, truncated by the DPU agent. |  | Optional: \{\} <br /> |


#### EWNICRuntimeConfigStatus



EWNICRuntimeConfigStatus reports the result of E/W NIC runtime configuration.
configuredDevices + len(failedDevices) < discoveredDevices means the first
runtime configuration pass over all devices has not finished yet. An unset
count means 0.



_Appears in:_
- [AgentStatus](#agentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `discoveredDevices` _integer_ | discoveredDevices is the number of E/W NIC devices found on this boot. |  | Optional: \{\} <br /> |
| `configuredDevices` _integer_ | configuredDevices is the number of devices whose last runtime<br />configuration apply succeeded. |  | Optional: \{\} <br /> |
| `failedDevices` _[EWNICDeviceFailure](#ewnicdevicefailure) array_ | failedDevices lists devices whose last runtime configuration<br />apply failed. |  | Optional: \{\} <br /> |


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



#### HostOSInitSkipped



HostOSInitSkipped reports that host OS init release was not required.



_Appears in:_
- [HostOSInitStatus](#hostosinitstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `reason` _string_ | reason is a stable machine-readable outcome code. |  | Optional: \{\} <br /> |
| `message` _string_ | message is a human-readable explanation. |  | Optional: \{\} <br /> |


#### HostOSInitStatus



HostOSInitStatus is the agent-reported terminal status for host OS
init release.



_Appears in:_
- [AgentStatus](#agentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `skipped` _[HostOSInitSkipped](#hostosinitskipped)_ | skipped indicates release was not required for this DPU. |  | Optional: \{\} <br /> |
| `succeeded` _[HostOSInitSucceeded](#hostosinitsucceeded)_ | succeeded indicates host OS init was released or was already cleared. |  | Optional: \{\} <br /> |


#### HostOSInitSucceeded



HostOSInitSucceeded reports successful host OS init release.



_Appears in:_
- [HostOSInitStatus](#hostosinitstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `gate` _[ServiceReadinessGate](#servicereadinessgate)_ | gate echoes the effective gate used for release. |  | Enum: [DPUServiceCriticalPodsReady OperationalReady] <br />Optional: \{\} <br /> |


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

IdentityMode records which authentication mechanism the DPU Agent uses to
reach the management-cluster kube-apiserver. It is immutable once set
(see DPUStatus.IdentityMode).



_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description |
| --- | --- |
| `spiffe` | IdentityModeSpiffe indicates the DPU Agent authenticates with a<br />SPIFFE-issued JWT-SVID.<br /> |
| `bootstrap-token` | IdentityModeBootstrapToken indicates the DPU Agent authenticates with a<br />kubeadm bootstrap token. An unset (nil) IdentityMode is treated<br />semantically as bootstrap-token; no sentinel is declared for the unset<br />case deliberately, to force explicit handling by consumers.<br /> |


#### K8sCluster







_Appears in:_
- [DPUSpec](#dpuspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the DPUs Kubernetes cluster |  | Optional: \{\} <br /> |
| `namespace` _string_ | Namespace is the tenants namespace name where the Kubernetes cluster<br />will be deployed |  | Optional: \{\} <br /> |
| `nodeLabels` _object (keys:string, values:string)_ | NodeLabels specifies the labels to be added to the node. |  | Optional: \{\} <br /> |
| `nodeAnnotations` _object (keys:string, values:string)_ | NodeAnnotations specifies the annotations to be added to the node. |  | Optional: \{\} <br /> |
| `selector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Selector defines the selector of the DPUClusters the produced DPUs<br />should join |  | Optional: \{\} <br /> |


#### KeepalivedSpec







_Appears in:_
- [ClusterEndpointSpec](#clusterendpointspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vip` _string_ | VIP is the virtual IP owned by the keepalived instances |  |  |
| `virtualRouterID` _integer_ | VirtualRouterID is the virtual_router_id in keepalived.conf |  | Maximum: 255 <br />Minimum: 1 <br /> |
| `interface` _string_ | Interface specifies on which interface the VIP should be assigned |  | MinLength: 1 <br /> |
| `nodeSelector` _object (keys:string, values:string)_ | NodeSelector is used to specify a subnet of control plane nodes to<br />deploy keepalived instances.<br />Note: keepalived instances are always deployed on control plane nodes |  | Optional: \{\} <br /> |


#### NVConfig







_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)
- [NetworkInterfaceConfig](#networkinterfaceconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `device` _string_ | Device is the device to which the configuration applies. If not<br />specified, the configuration applies to all. Supported values: "*"<br />(wildcard for all devices), "p0"/"P0" (port 0), "p1"/"P1" (port 1).<br />Case-insensitive. |  | Enum: [* p0 p1 P0 P1] <br />Optional: \{\} <br /> |
| `parameters` _string array_ | Parameters are the parameters to be set for the device.<br />DELAY_HOST_OS_INIT=ENABLE_USER (0x3) holds the host at UEFI and is<br />rejected by the DPU agent outside zero-trust, where the agent needs the<br />host to reach the kube-apiserver. |  | MaxItems: 64 <br />MinItems: 1 <br />items:MaxLength: 200 <br />items:Pattern: `^[^=\s]+=[^\s]*$` <br />Optional: \{\} <br /> |
| `force` _boolean_ | force applies the parameters with `mlxconfig --force` and skips the<br />`mlxconfig q` filter, so parameters firmware does not yet expose are<br />applied now instead of deferred to a later reboot. Required when a<br />parameter is gated behind another parameter in the same batch. Requires<br />DOCA 3.5.0 or later; on an older DOCA version the operation fails.<br />`--force` skips validation for the whole batch, so an invalid value is<br />applied silently. Ignored under<br />`spec.hostNetworkInterfaceConfigs[].nvconfig`. |  | Optional: \{\} <br /> |


#### NetworkInterfaceConfig



NetworkInterfaceConfig defines the configuration for a network interface



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `mtu` _integer_ | MTU is the MTU value to be set on the network interface. |  | Maximum: 9216 <br />Minimum: 1280 <br />Optional: \{\} <br /> |
| `dhcp` _boolean_ | DHCP is the DHCP configuration for the network interface. |  | Optional: \{\} <br /> |
| `portNumber` _integer_ | PortNumber identifies which port this configuration applies to. |  | Maximum: 1 <br />Minimum: 0 <br />Required: \{\} <br /> |
| `nvconfig` _[NVConfig](#nvconfig)_ | NVConfig contains port-specific configuration for this network interface.<br />This configuration is applied in addition to the global NVConfig<br />settings in DPUFlavorSpec. Both global and per-interface NVConfig<br />settings can coexist without collision. |  | Optional: \{\} <br /> |


#### NicConfiguration



NicConfiguration is a set of configurations for the NICs



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `numVfs` _integer_ | Number of VFs to be configured |  | Required: \{\} <br /> |
| `linkType` _[LinkTypeEnum](https://github.com/Mellanox/nic-configuration-operator/blob/main/docs/api-reference.md#linktypeenum-string-alias)_ | LinkType to be configured, Ethernet\|Infiniband. Required unless<br />networkBay is configured; for Network Bay the link type is governed by<br />the system configuration and must not be set. |  | Enum: [Ethernet Infiniband] <br />Optional: \{\} <br /> |
| `spectrumXOptimized` _[SpectrumXOptimizedSpec](https://github.com/Mellanox/nic-configuration-operator/blob/main/docs/api-reference.md#spectrumxoptimizedspec)_ | Spectrum-X optimization settings. Works only with linkType==Ethernet &&<br />numVfs==1. RawNvConfig parameters, if provided, are merged as overrides<br />on top of Spectrum-X calculated params. |  |  |
| `rawNvConfig` _[NvConfigParam](https://github.com/Mellanox/nic-configuration-operator/blob/main/docs/api-reference.md#nvconfigparam) array_ | List of arbitrary nv config parameters |  |  |
| `networkBay` _[NetworkBaySpec](https://github.com/Mellanox/nic-configuration-operator/blob/main/docs/api-reference.md#networkbayspec)_ | NetworkBay configures a ConnectX-9 Network Bay card (per-ASIC<br />set_system_conf). Allowed only for ConnectX-9 (nicType 1025). |  | Optional: \{\} <br /> |
| `force` _boolean_ | Force passes `--force` to mlxconfig set commands. When set, the daemon<br />applies the nv config batch and set_system_conf with --force, letting<br />mlxconfig accept a batch it would otherwise refuse due to implicit<br />parameter dependencies. | false | Optional: \{\} <br /> |


#### NodeEffect



NodeEffect is the effect the DPU has on Nodes during provisioning.
Only one of Taint, NoEffect, CustomLabel, Drain, CustomAction, Hold can be
set.



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
| `customAction` _string_ | Name of a config map which contains a pod yaml definition to run which<br />will apply the nodeEffect. The pod is expected to exit when node effect<br />is done, if pod terminates with error then DPU would move to an error<br />phase. The DPUNode's name will be exported as an environment variable,<br />named as DPUNODE_NAME, to each container and init container in the pod.<br />The labels and annotations of DPUNode will be exported in<br />`/etc/dpu/dpf-pod-info/labels` and `/etc/dpu/dpf-pod-info/annotations`<br />accordingly; the volume name `dpf-pod-info` is used to mount the labels<br />and annotations. If any name confliction for env or volume, the<br />controller will not export the name or labels/annotations of DPUNode<br />accordingly. |  | Optional: \{\} <br /> |
| `hold` _boolean_ | Places annotation `wait-for-external-nodeeffect` and waits for it to be<br />removed this is the default behavior in a non K8S environment |  | Optional: \{\} <br /> |
| `force` _boolean_ | Force is the flag to indicate if the node effect should be applied<br />immediately. If true, dpfOperatorConfig.multiDPUOperationsSyncWaitTime<br />and dpfOperatorConfig.maxUnavailableDPUNodes will be ignored when<br />applying node effect for DPUNodeMaintenance CR | false | Optional: \{\} <br /> |
| `applyOnLabelChange` _boolean_ | Apply node effect when labels change on the DPU object<br />When set to true, label changes in Ready state will trigger node effect<br />logic | false | Optional: \{\} <br /> |
| `nodeMaintenanceAdditionalRequestors` _string array_ | Additional requestors to be added to the NvidiaNodeMaintenance CR when<br />Drain is selected |  | Optional: \{\} <br /> |


#### NodeRebootMethod



NodeRebootMethod defines the desired reboot method



_Appears in:_
- [DPUNodeSpec](#dpunodespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `gNOI` _[GNOI](#gnoi)_ | Use the DPU's DMS interface to reboot the host.<br />Deprecated: Use HostAgent instead. |  | Optional: \{\} <br /> |
| `hostAgent` _[HostAgent](#hostagent)_ | Use the HostAgent to reboot the host. |  | Optional: \{\} <br /> |
| `external` _[External](#external)_ | Reboot the host via an external means, not controlled by the DPU<br />controller. |  | Optional: \{\} <br /> |
| `script` _[Script](#script)_ | Reboot the host by executing a custom script. This field defined which<br />ConfigMap store the custom script. The ConfigMap should include a pod<br />template of Job object under the `pod-template` key. That pod template<br />will be put in a Job object to be executed. |  | Optional: \{\} <br /> |
| `none` _[None](#none)_ | Do not reboot a host through the DPUNode. Used for hostless devices where<br />DPF manages the DPU reboot directly through Redfish. |  | Optional: \{\} <br /> |


#### None







_Appears in:_
- [NodeRebootMethod](#noderebootmethod)



#### ObservedSecretKeyRef



ObservedSecretKeyRef identifies an observed source Secret version.



_Appears in:_
- [DPUClusterStaticKeyEncryptionStatus](#dpuclusterstatickeyencryptionstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the Secret. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `key` _string_ | Key is the key within the Secret data. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `namespace` _string_ | Namespace is the namespace of the Secret. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `uid` _string_ | UID is the UID of the Secret. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `resourceVersion` _string_ | ResourceVersion is the resourceVersion of the Secret. |  | MinLength: 1 <br />Required: \{\} <br /> |


#### PackageSpec



PackageSpec defines a package to reconcile on the node.



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the package name. |  | MaxLength: 253 <br />MinLength: 1 <br /> |
| `version` _[PackageVersionSpec](#packageversionspec)_ | Version constrains the package version.<br />If empty, any installed version satisfies the spec. |  | Optional: \{\} <br /> |
| `repoFileRef` _string_ | RepoFileRef constrains package resolution to a specific repository file<br />available on the node.<br />If empty, any configured repository may satisfy the package.<br />If specified, only the referenced repository file may provide<br />candidates.<br />If that repository file does not provide the package or requested<br />version, the dpu-agent flow does not continue. |  | Optional: \{\} <br /> |


#### PackageVersionMatchPolicy

_Underlying type:_ _string_

PackageVersionMatchPolicy defines how a package version constraint is
evaluated.



_Appears in:_
- [PackageVersionSpec](#packageversionspec)

| Field | Description |
| --- | --- |
| `Exact` | PackageVersionMatchExact requires the installed package version to equal<br />Value.<br /> |
| `AtLeast` | PackageVersionMatchAtLeast requires the installed package version to be<br />greater than or equal to Value.<br /> |


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
| `next_boot` _string_ | NextBoot uses the "next_boot" so this type can be reused for parsing<br />mlxfwrest output |  |  |


#### PendingNVConfigState







_Appears in:_
- [AgentStatus](#agentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bootID` _string_ |  |  |  |
| `devices` _[PendingNVConfigDevice](#pendingnvconfigdevice) array_ |  |  |  |


#### RebootMethodType

_Underlying type:_ _string_

RebootMethodType is the type of reset/reboot required after NVConfig or
firmware changes. Set by the DPU agent. Most values align with NVIDIA
BlueField Reset and Reboot Procedures (mlxfwreset levels).

_Validation:_
- Enum: [Unknown NoAction PowerCycle SystemReboot SystemLevelReset FirmwareReset DPUWarmReboot]

_Appears in:_
- [AgentStatus](#agentstatus)
- [DPUNodeStatus](#dpunodestatus)
- [RebootStatus](#rebootstatus)

| Field | Description |
| --- | --- |
| `Unknown` | RebootMethodUnknown is the initial value set by the DPU agent on startup<br />before HandleReboot determines the actual method. It prevents the<br />controller from acting on a stale RebootMethod left over from a previous<br />agent session.<br /> |
| `NoAction` | RebootMethodNoAction indicates no reset or reboot is required.<br /> |
| `PowerCycle` | RebootMethodPowerCycle indicates a full server power cycle (cold boot)<br />is required.<br /> |
| `SystemReboot` | RebootMethodSystemReboot firmware update without full server power cycle.<br /> |
| `SystemLevelReset` | RebootMethodSystemLevelReset firmware configuration changes to<br />take effect.<br /> |
| `FirmwareReset` | RebootMethodFirmwareReset driver restart and PCI reset.<br /> |
| `DPUWarmReboot` | RebootMethodDPUWarmReboot indicates the DPU OS is rebooting itself to<br />apply configuration changes (e.g. grub kernel parameters) that do not<br />originate from firmware or NVConfig. The provisioning controller should<br />stay in the current phase and wait for the agent to come back.<br /> |


#### RebootStatus



RebootStatus stores the host reboot execution status.



_Appears in:_
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[RebootStatusPhase](#rebootstatusphase)_ | Phase is the current host reboot progress. |  | Enum: [WaitForShutdown Pending Succeeded Failed Unknown] <br /> |
| `method` _[RebootMethodType](#rebootmethodtype)_ | Method is the recommended reboot method. |  | Enum: [Unknown NoAction PowerCycle SystemReboot SystemLevelReset FirmwareReset DPUWarmReboot] <br />Optional: \{\} <br /> |
| `reason` _string_ | Reason indicates machine-readable reason for current phase. |  | Optional: \{\} <br /> |
| `message` _string_ | Message provides human-readable details for current phase. |  | Optional: \{\} <br /> |
| `lastTransitionTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastTransitionTime is the last update time for reboot status. |  | Optional: \{\} <br /> |


#### RebootStatusPhase

_Underlying type:_ _string_

RebootStatusPhase is the host reboot progress phase.

_Validation:_
- Enum: [WaitForShutdown Pending Succeeded Failed Unknown]

_Appears in:_
- [RebootStatus](#rebootstatus)

| Field | Description |
| --- | --- |
| `WaitForShutdown` | RebootStatusWaitForShutdown means the host reboot is held until the DPU<br />has completed its graceful shutdown. Used in Zero Trust mode for a System<br />Level Reset so the External/Script host reboot is not triggered while the<br />Arm OS is still shutting down.<br /> |
| `Pending` | RebootStatusPending means reboot is requested but execution has not<br />started yet (for example, waiting for a manual external reboot trigger).<br /> |
| `Succeeded` | RebootStatusSucceeded means reboot completed successfully.<br /> |
| `Failed` | RebootStatusFailed means reboot execution failed.<br /> |
| `Unknown` | RebootStatusUnknown means reboot execution state cannot be determined.<br /> |


#### RollingUpdateDPU



RollingUpdateDPU is the rolling update strategy for a DPUSet.



_Appears in:_
- [DPUSetStrategy](#dpusetstrategy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `maxUnavailable` _[IntOrString](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#intorstring-intstr-util)_ | MaxUnavailable is the maximum number of DPUs that can be unavailable<br />during the update.<br />Deprecated: This field is deprecated and will be removed with v31.0.0. |  | Optional: \{\} <br /> |


#### ScalableFunction



ScalableFunction is one group of SFs to create.



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `count` _integer_ | Count is SFs to create per selected device. With device "*", count 20 is<br />20 SFs on each port. Zero creates nothing. |  | Minimum: 0 <br />Required: \{\} <br /> |
| `device` _string_ | Device is the target port: "*", a name such as p0, or a PCI address.<br />Defaults to "*". |  | Pattern: `^(\*\|[pP][0-9]+\|[0-9a-fA-F]\{4\}:[0-9a-fA-F]\{2\}:[0-9a-fA-F]\{2\}\.[0-7])$` <br />Optional: \{\} <br /> |
| `hostDevice` _boolean_ | HostDevice creates the SFs on the host (representors on the DPU).<br />Implies controller 1; options.controller overrides. Host SFs use the<br />host firmware budget and must not set poolName. |  | Optional: \{\} <br /> |
| `poolName` _string_ | PoolName is the device-plugin resource (for example bf_sf). Unset:<br />created in hardware only. Forbidden with hostDevice. |  | MaxLength: 63 <br />Pattern: `^[A-Za-z0-9][A-Za-z0-9_.-]*$` <br />Optional: \{\} <br /> |
| `options` _[ScalableFunctionOptions](#scalablefunctionoptions)_ | Options are creation settings for this group. |  | Optional: \{\} <br /> |


#### ScalableFunctionOptions



ScalableFunctionOptions are per-group SF creation settings.



_Appears in:_
- [ScalableFunction](#scalablefunction)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `macAddress` _string_ | MACAddress pins the SF MAC (colon-separated 48-bit). Requires count 1. |  | Pattern: `^([0-9A-Fa-f]\{2\}:)\{5\}[0-9A-Fa-f]\{2\}$` <br />Optional: \{\} <br /> |
| `sfNumStart` _integer_ | SFNumStart is the first sfnum of the group; the rest are sequential from<br />it. Reserved before agent-numbered groups, which fill from 0 around<br />them. |  | Maximum: 65535 <br />Minimum: 0 <br />Optional: \{\} <br /> |
| `trusted` _boolean_ | Trusted creates the SFs as trusted functions. |  | Optional: \{\} <br /> |
| `controller` _integer_ | Controller is the external controller to create on. Overrides hostDevice. |  | Maximum: 15 <br />Minimum: 0 <br />Optional: \{\} <br /> |
| `cpuList` _string_ | CPUList pins the SFs to CPUs in mlnx-sf list form, for example 0-3 or<br />0,2,4. |  | MaxLength: 200 <br />Pattern: `^[0-9]+(-[0-9]+)?(,[0-9]+(-[0-9]+)?)*$` <br />Optional: \{\} <br /> |
| `disableRoCE` _boolean_ | DisableRoCE creates the SFs with RoCE disabled. |  | Optional: \{\} <br /> |
| `disableNetdev` _boolean_ | DisableNetdev leaves the SFs without an ethernet netdev. |  | Optional: \{\} <br /> |


#### Script







_Appears in:_
- [NodeRebootMethod](#noderebootmethod)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  | MinLength: 1 <br />Required: \{\} <br /> |


#### SecureBootStatus



SecureBootStatus represents the UEFI Secure Boot configuration status on
the DPU.



_Appears in:_
- [DPUDeviceStatus](#dpudevicestatus)
- [DPUStatus](#dpustatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ | Enabled indicates whether UEFI Secure Boot is currently enabled on<br />the DPU. |  | Optional: \{\} <br /> |


#### ServiceReadiness



ServiceReadiness configures the Service Readiness provisioning phase.



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `gate` _[ServiceReadinessGate](#servicereadinessgate)_ | gate is the DPU.status.operationalConditions entry that must be True<br />before the DPU leaves the Service Readiness phase. When unset the phase<br />does not wait, and a host hold requested via DELAY_HOST_OS_INIT is<br />released on DPUServiceCriticalPodsReady. |  | Enum: [DPUServiceCriticalPodsReady OperationalReady] <br />Optional: \{\} <br /> |


#### ServiceReadinessGate

_Underlying type:_ _string_

ServiceReadinessGate names a DPU operational condition. Values match
DPUOperationalConditionType.

_Validation:_
- Enum: [DPUServiceCriticalPodsReady OperationalReady]

_Appears in:_
- [HostOSInitSucceeded](#hostosinitsucceeded)
- [ServiceReadiness](#servicereadiness)

| Field | Description |
| --- | --- |
| `DPUServiceCriticalPodsReady` | GateDPUServiceCriticalPodsReady waits for<br />DPU.status.operationalConditions[DPUServiceCriticalPodsReady] == True.<br /> |
| `OperationalReady` | GateOperationalReady waits for<br />DPU.status.operationalConditions[OperationalReady] == True.<br /> |


#### SpiffeStatus



SpiffeStatus is the DPU Agent's SPIFFE heartbeat sub-status.



_Appears in:_
- [AgentStatus](#agentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `lastProbeTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastProbeTime is the wall-clock timestamp the DPU Agent recorded on its<br />most recent successful status report. It is informational and subject to<br />DPU clock skew, so it is not a precise liveness signal on its own. |  | Optional: \{\} <br /> |
| `lastProbeMessage` _string_ | LastProbeMessage is a structured one-line diagnostic for the most recent<br />self-probe. Unset in the steady-state happy path. Bounded to 256 chars<br />(truncated agent-side). |  | MaxLength: 256 <br />Optional: \{\} <br /> |


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

SystemdServiceOperation defines the operation to perform on a systemd
service.

_Validation:_
- Enum: [Start Enable EnableAndStart]

_Appears in:_
- [SystemdServiceSpec](#systemdservicespec)

| Field | Description |
| --- | --- |
| `Start` | SystemdServiceStart starts the service without enabling it at boot.<br /> |
| `Enable` | SystemdServiceEnable enables the service at boot without starting it<br />immediately.<br /> |
| `EnableAndStart` | SystemdServiceEnableAndStart enables the service at boot and starts it<br />immediately (equivalent to systemctl enable --now).<br /> |


#### SystemdServiceSpec



SystemdServiceSpec defines a systemd service to manage on the node.



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the systemd service name. |  | MaxLength: 253 <br />MinLength: 1 <br /> |
| `operation` _[SystemdServiceOperation](#systemdserviceoperation)_ | Operation is the systemd operation to perform on the service. |  | Enum: [Start Enable EnableAndStart] <br /> |


#### TrustBundleStatus



TrustBundleStatus reports reconciliation progress for a CA trust bundle.



_Appears in:_
- [DPUDeviceStatus](#dpudevicestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `observedBundleHash` _string_ | ObservedBundleHash records the last successfully applied bundle hash. |  | Optional: \{\} <br /> |
| `lastUpdateTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | LastUpdateTime is when the last successful reconciliation completed. |  | Optional: \{\} <br /> |


#### UpgradePolicy



UpgradePolicy is the policy for the upgrade of the DPUSet.



_Appears in:_
- [NodeEffect](#nodeeffect)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `applyOnLabelChange` _boolean_ | Apply node effect when labels change on the DPU object<br />When set to true, label changes in Ready state will trigger node effect<br />logic | false | Optional: \{\} <br /> |
| `nodeMaintenanceAdditionalRequestors` _string array_ | Additional requestors to be added to the NvidiaNodeMaintenance CR when<br />Drain is selected |  | Optional: \{\} <br /> |


#### VirtualFunction



VirtualFunction is one group of VFs to create.



_Appears in:_
- [DPUFlavorSpec](#dpuflavorspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `count` _integer_ | Count is VFs to create per selected device. Groups ending up on the same<br />device sum to a single `sriov_numvfs` and then list order assigns<br />contiguous index ranges. Zero creates nothing. |  | Minimum: 0 <br />Required: \{\} <br /> |
| `device` _string_ | Device is the target port: "*", a name such as p0, or a PCI address.<br />Defaults to "*". |  | Pattern: `^(\*\|[pP][0-9]+\|[0-9a-fA-F]\{4\}:[0-9a-fA-F]\{2\}:[0-9a-fA-F]\{2\}\.[0-7])$` <br />Optional: \{\} <br /> |
| `poolName` _string_ | PoolName is the device-plugin resource (for example bf_vf). Unset:<br />created in hardware only. |  | MaxLength: 63 <br />Pattern: `^[A-Za-z0-9][A-Za-z0-9_.-]*$` <br />Optional: \{\} <br /> |
| `options` _[VirtualFunctionOptions](#virtualfunctionoptions)_ | Options are creation settings for this group. |  | Optional: \{\} <br /> |


#### VirtualFunctionOptions



VirtualFunctionOptions are per-group VF creation settings.



_Appears in:_
- [VirtualFunction](#virtualfunction)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `macAddress` _string_ | MACAddress sets the VF MAC (colon-separated 48-bit). Requires count 1. |  | Pattern: `^([0-9A-Fa-f]\{2\}:)\{5\}[0-9A-Fa-f]\{2\}$` <br />Optional: \{\} <br /> |



## storage.dpu.nvidia.com/v1alpha1

Package v1alpha1 contains API Schema definitions for the storage v1alpha1 API
group

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
| `funcVUID` _string_ | The VUID of the emulated function. For a VF, this is the parent PF VUID. |  | Optional: \{\} <br /> |
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
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Current service state conditions |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


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
| `dpuClusters` _[ObjectReference](#objectreference) array_ | DPUClusters is the list of clusters on which the<br />DPUStorageVendor is deployed. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


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
| `dpuNodeName` _string_ | DPUNodeName is the name of DPUNode object that represents the node to<br />which the volume should be attached |  | MinLength: 1 <br />Required: \{\} <br /> |
| `dpuVolumeName` _string_ | DPUVolumeName is the name of DPUVolume object that represents the volume<br />to be attached |  | MinLength: 1 <br />Required: \{\} <br /> |
| `functionType` _[FunctionType](#functiontype)_ | FunctionType is the type of the emulated function that should be used to<br />attach the volume |  | Enum: [pf vf] <br />Required: \{\} <br /> |
| `hotplugFunction` _boolean_ | HotplugFunction is a boolean flag that indicates if the emulated function<br />should be hotplugged |  | Required: \{\} <br /> |


#### DPUVolumeAttachmentStatus



DPUVolumeAttachmentStatus defines the observed state of DPUVolumeAttachment



_Appears in:_
- [DPUVolumeAttachment](#dpuvolumeattachment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `controllerAttached` _boolean_ | Indicates the volume is successfully attached to by the Vendor CSI driver |  | Optional: \{\} <br /> |
| `dpuAttached` _boolean_ | Indicates the volume is successfully attached to the node by DPU |  | Optional: \{\} <br /> |
| `attachmentMetadata` _object (keys:string, values:string)_ | AttachmentMetadata contains the metadata of the volume attachment<br />returned by the Vendor CSI driver |  | Optional: \{\} <br /> |
| `dpu` _[AttachmentStatusDPU](#attachmentstatusdpu)_ | Details about the DPU attachment |  | Optional: \{\} <br /> |
| `message` _string_ | The last error encountered during the attach operation, if any |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


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
| `dpuStoragePolicyName` _string_ | Name of the DPUStoragePolicyName object that will be used to create the<br />volume. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `parameters` _object (keys:string, values:string)_ | Additional parameters for the volume, these parameters are merged with<br />the values from the DPUStoragePolicy object. | \{  \} | Optional: \{\} <br /> |
| `accessModes` _[PersistentVolumeAccessMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumeaccessmode-v1-core) array_ | Access modes define how the volume can be mounted. These modes are<br />directly passed to the PersistentVolumeClaim created for the Vendor CSI<br />Plugin selected by the DPUStoragePolicy. |  | MaxItems: 3 <br />MinItems: 1 <br />Required: \{\} <br /> |
| `resources` _[VolumeResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#volumeresourcerequirements-v1-core)_ | Resources represents the storage resources requested for the volume.<br />These resource requirements are directly passed to the<br />PersistentVolumeClaim created for the Vendor CSI Plugin selected by the<br />DPUStoragePolicy. Since volume resizing is not supported, modifications<br />to the resource request are prohibited. |  | Required: \{\} <br /> |
| `volumeMode` _[PersistentVolumeMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumemode-v1-core)_ | Volume mode defines how the volume should be mounted and used. This value<br />is directly passed to the PersistentVolumeClaim created for the Vendor<br />CSI Plugin selected by the DPUStoragePolicy. | Filesystem | Enum: [Filesystem Block] <br />Optional: \{\} <br /> |


#### DPUVolumeState



DPUVolumeState defines the state of the volume.



_Appears in:_
- [DPUVolumeStatus](#dpuvolumestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuCluster` _[ObjectReference](#objectreference)_ | DPUCluster contains the reference to the DPUCluster object that was<br />selected for volume creation. |  | Optional: \{\} <br /> |
| `parameters` _object (keys:string, values:string)_ | Parameters contains the final set of parameters for volume creation,<br />computed by merging the parameters from the DPUStoragePolicy object with<br />user-provided parameters. |  | Optional: \{\} <br /> |
| `selectedDPUStorageVendorName` _string_ | SelectedDPUStorageVendorName contains the name of the DPUStorageVendor<br />object that was selected for volume creation. |  | Optional: \{\} <br /> |
| `storageVendorPluginName` _string_ | StorageVendorPluginName contains the name of the storage vendor plugin<br />deployed on the DPU cluster that was selected for volume creation. |  | Optional: \{\} <br /> |
| `storageClassName` _string_ | StorageClassName contains the name of the storage class in the DPU<br />cluster that was selected for volume creation. |  | Optional: \{\} <br /> |
| `csiDriverName` _string_ | CSIDriverName contains the name of the CSI driver in the DPU cluster that<br />was selected for volume creation. |  | Optional: \{\} <br /> |
| `persistentVolumeClaimRef` _[ObjectReference](#objectreference)_ | PersistentVolumeClaimRef contains the reference to the<br />PersistentVolumeClaim object in the DPU cluster that was created for<br />the volume. |  | Optional: \{\} <br /> |
| `volumeInfo` _[VolumeInfo](#volumeinfo)_ | VolumeInfo contains a subset of fields from the PersistentVolume object<br />created in the DPU cluster |  | Optional: \{\} <br /> |


#### DPUVolumeStatus



DPUVolumeStatus defines the observed state of DPUVolume



_Appears in:_
- [DPUVolume](#dpuvolume)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _[DPUVolumePhase](#dpuvolumephase)_ | Phase of the volume |  | Enum: [Pending Bound] <br />Optional: \{\} <br /> |
| `state` _[DPUVolumeState](#dpuvolumestate)_ | State of the volume. This field is managed by the controller. User<br />usually do not need to set fields from this struct. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


#### FSdevAttrs



FSdevAttrs represents the attributes of the underlying filesystem device



_Appears in:_
- [VolumeAttachmentStatusDPU](#volumeattachmentstatusdpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `filesystemTag` _string_ | Filesystem tag identified by SNAP on the host (used for the mount).<br />Relevant for volume of type filesystem |  |  |


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



FunctionTypeConfig is the configuration for the emulated function that should
be used to attach the volume



_Appears in:_
- [DPUVolumeAttachmentSpec](#dpuvolumeattachmentspec)
- [VolumeAttachmentSpec](#volumeattachmentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `functionType` _[FunctionType](#functiontype)_ | FunctionType is the type of the emulated function that should be used to<br />attach the volume |  | Enum: [pf vf] <br />Required: \{\} <br /> |
| `hotplugFunction` _boolean_ | HotplugFunction is a boolean flag that indicates if the emulated function<br />should be hotplugged |  | Required: \{\} <br /> |


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



SVVolumeAttachment captures the intent to attach/detach the specified Volume
to/from the specified node.



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
| `NumberVolumes` | Load-balancing on the number of volumes belonging to the StoragePolicy.<br />The vendor (in the DPUStoragePolicy list) with the minimal number of<br />volumes should be selected.<br /> |


#### VirtioFSAttrs



VirtioFSAttrs represents the attributes of the VirtioFS emulated function



_Appears in:_
- [AttachmentStatusDPU](#attachmentstatusdpu)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `filesystemTag` _string_ | Filesystem tag identified by SNAP on the host (used for the mount).<br />Relevant for volume of type filesystem |  | Optional: \{\} <br /> |


#### Volume



Volume represents a persistent volume on the DPU cluster. It maps between
the tenant K8S persistent volume (PV) object on the tenant cluster into the
actual volume on the DPU cluster. Volume is an internal API, it is not
intended to be used by users.



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



VolumeAttachment captures the intent to attach/detach the specified NV-Volume
to/from the specified node. VolumeAttachment is an internal API, it is not
intended to be used by users.



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
| `functionType` _[FunctionType](#functiontype)_ | FunctionType is the type of the emulated function that should be used to<br />attach the volume |  | Enum: [pf vf] <br />Required: \{\} <br /> |
| `hotplugFunction` _boolean_ | HotplugFunction is a boolean flag that indicates if the emulated function<br />should be hotplugged |  | Required: \{\} <br /> |


#### VolumeAttachmentStatus



VolumeAttachmentStatus defines the observed state of VolumeAttachment



_Appears in:_
- [VolumeAttachment](#volumeattachment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageAttached` _boolean_ | Indicates the volume is successfully attached to the target storage<br />system |  | Optional: \{\} <br /> |
| `message` _string_ | The last error encountered during the attach operation, if any |  | Optional: \{\} <br /> |
| `dpu` _[VolumeAttachmentStatusDPU](#volumeattachmentstatusdpu)_ | Details about the DPU attachment |  | Optional: \{\} <br /> |


#### VolumeAttachmentStatusDPU



VolumeAttachmentStatusDPU describe the information of DPU volume



_Appears in:_
- [VolumeAttachmentStatus](#volumeattachmentstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `attached` _boolean_ | Indicates the volume is successfully attached to the DPU node |  |  |
| `pciDeviceAddress` _string_ | PCI device address in the following format: (bus:device.function) |  |  |
| `deviceName` _string_ | The name of the device that was created by the storage vendor plugin |  |  |
| `funcVUID` _string_ | The VUID of the emulated function. For a VF, this is the parent PF VUID. |  |  |
| `bdevAttrs` _[BdevAttrs](#bdevattrs)_ | The attributes of the underlying block device |  | Optional: \{\} <br /> |
| `fsdevAttrs` _[FSdevAttrs](#fsdevattrs)_ | The attributes of the underlying filesystem device |  | Optional: \{\} <br /> |


#### VolumeInfo



VolumeInfo represents a subset of fields from the PersistentVolume object
that was created in the DPU cluster. This struct is used to track and expose
key volume information without carrying the full PersistentVolume object.



_Appears in:_
- [DPUVolumeState](#dpuvolumestate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `volumeName` _string_ | VolumeName contains the name of the PersistentVolume object in the DPU<br />cluster |  | Optional: \{\} <br /> |
| `capacity` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | Actual capacity of the volume in the DPU cluster |  | Optional: \{\} <br /> |
| `accessModes` _[PersistentVolumeAccessMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumeaccessmode-v1-core) array_ | Actual access modes of the volume in the DPU cluster |  | Optional: \{\} <br /> |
| `volumeMode` _[PersistentVolumeMode](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#persistentvolumemode-v1-core)_ | Actual volume mode of the volume in the DPU cluster |  | Optional: \{\} <br /> |
| `volumeAttributes` _object (keys:string, values:string)_ | VolumeAttributes from the PersistentVolume object in the DPU cluster<br />This field usually contains parameters returned by the Vendor CSI plugin<br />on volume creation. |  | Optional: \{\} <br /> |


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
| `storageParameters` _object (keys:string, values:string)_ | List of storage parameters supported by the policy, values are string<br />only |  | Optional: \{\} <br /> |
| `request` _[VolumeRequest](#volumerequest)_ | The capacity of the required storage space in bytes |  | Required: \{\} <br /> |
| `storagePolicyRef` _[ObjectRef](#objectref)_ | Reference to the StoragePolicy object |  | Optional: \{\} <br /> |
| `storagePolicyParameters` _object (keys:string, values:string)_ | List of storage parameters supported by the policy, values are string<br />only |  | Optional: \{\} <br /> |
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

Package v1alpha1 contains API Schema definitions for the svc.dpf v1alpha1 API
group





Package v1alpha1 contains API Schema definitions for the sfc v1alpha1 API
group

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
| `repoURL` _string_ | RepoURL specifies the URL to the repository that contains the application<br />Helm chart. The URL must begin with either 'oci://' or 'https://',<br />ensuring it points to a valid OCI registry or a web-based repository. |  | Pattern: `^(oci://\|https://).+$` <br />Required: \{\} <br /> |
| `path` _string_ | Path is the location of the chart inside the repo. |  | Optional: \{\} <br /> |
| `version` _string_ | Version is a semver tag for the Chart's version. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `chart` _string_ | Chart is the name of the helm chart. |  | Optional: \{\} <br /> |
| `releaseName` _string_ | ReleaseName is the name to give to the release generate from<br />the DPUService. |  | Optional: \{\} <br /> |


#### CNIPlugin



CNIPlugin defines a CNI plugin to be used in a chained CNI configuration.
When multiple CNI plugins are specified in ChainedCNIs, they are executed in
order after the base OVS CNI plugin to provide additional network
functionality.



_Appears in:_
- [DPUServiceNADSpec](#dpuservicenadspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type specifies the CNI plugin type to be used in the chain. Currently<br />only "rdma" is supported, which enables RDMA capabilities for the network<br />interface. |  | Enum: [rdma] <br />Required: \{\} <br /> |
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
| `nodePort` _integer_ | NodePort is the external port assigned on each node in the cluster.<br />If not set, Kubernetes will automatically allocate a NodePort.<br />Constraints:<br />- Can only be set when ServiceType is "NodePort".<br />- Must be within the clusters valid NodePort range<br />  (Kubernetes default is 30000-32767). |  | Optional: \{\} <br /> |


#### ConfigPorts



ConfigPorts defines the desired state of port configurations for a
DPUService. This struct determines how ports are exposed from the DPU to the
host cluster. A DPUService can only have a single ServiceType across
all ports.

Validation:
- If any port has a NodePort assigned, ServiceType **must** be "NodePort".



_Appears in:_
- [DPUServiceSpec](#dpuservicespec)
- [ServiceConfiguration](#serviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceType` _[ServiceType](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#servicetype-v1-core)_ | ServiceType specifies the type of Kubernetes Service to create.<br />All ports within this ConfigPorts will have the same ServiceType.<br />The value is immutable and cannot be changed after creation.<br />Supported values:<br />- "NodePort": Exposes ports externally on a node.<br />- "ClusterIP": Exposes ports internally within the cluster.<br />- "None": Internal-only service with no cluster IP.<br />Default: "NodePort" | NodePort | Enum: [NodePort ClusterIP None] <br />Required: \{\} <br /> |
| `ports` _[ConfigPort](#configport) array_ | Ports defines the list of port configurations that will be exposed by the<br />DPUService. Each port must specify a name, port number, and protocol.<br />Constraints:<br />- If ServiceType is "NodePort", ports may optionally specify a NodePort.<br />- If ServiceType is "None" or "ClusterIP", ports **cannot**<br />  specify a NodePort. |  | Required: \{\} <br /> |


#### DPUClusterAllocation



DPUClusterAllocation contains the IP range allocations for a
specific DPUCluster.



_Appears in:_
- [DPUServiceIPAMStatus](#dpuserviceipamstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuCluster` _string_ | DPUCluster is the NamespacedName of the DPUCluster in the format<br />`<namespace>/<name>`. |  |  |
| `ipRanges` _[IPRange](#iprange) array_ | IPRanges contains the IP ranges allocated to this DPUCluster. |  | Optional: \{\} <br /> |


#### DPUDeployment



DPUDeployment is an object that describes a set of DPUServices and a
DPUServiceChain that run a on a set of DPUs with a given BFB or
BlueFieldSoftware and DPUFlavor.



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
| `service` _[DPUDeploymentService](#dpudeploymentservice)_ | Service holds configuration that helps configure the Service Function<br />Chain and identify a port associated with a DPUService |  | Optional: \{\} <br /> |
| `serviceInterface` _[ServiceIfc](#serviceifc)_ | ServiceInterface holds configuration that helps configure the Service<br />Function Chain and identify a user defined port |  | Optional: \{\} <br /> |


#### DPUDeploymentService



DPUDeploymentService is the struct used for referencing an interface.



_Appears in:_
- [DPUDeploymentPort](#dpudeploymentport)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the service as defined in the DPUDeployment Spec |  | MaxLength: 28 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `interface` _string_ | Interface name is the name of the interface as defined in<br />the DPUServiceConfiguration |  | MaxLength: 15 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `ipam` _[IPAM](#ipam)_ | IPAM defines the IPAM configuration that is configured in<br />the Service Function Chain |  | Optional: \{\} <br /> |


#### DPUDeploymentServiceConfiguration



DPUDeploymentServiceConfiguration describes the configuration of a
particular Service



_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceTemplate` _string_ | ServiceTemplate is the name of the DPUServiceTemplate object to be used<br />for this Service. It must be in the same namespace as the DPUDeployment. |  |  |
| `serviceConfiguration` _string_ | ServiceConfiguration is the name of the DPUServiceConfiguration object to<br />be used for this Service. It must be in the same namespace as<br />the DPUDeployment. |  |  |
| `dependsOn` _[LocalObjectDependency](#localobjectdependency) array_ | DependsOn is a list of local object dependencies that are required for<br />this Service. |  | MinItems: 1 <br />Optional: \{\} <br /> |


#### DPUDeploymentSpec



DPUDeploymentSpec defines the desired state of DPUDeployment



_Appears in:_
- [DPUDeployment](#dpudeployment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpus` _[DPUs](#dpus)_ | DPUs contains the DPU related configuration |  | Required: \{\} <br /> |
| `services` _object (keys:string, values:[DPUDeploymentServiceConfiguration](#dpudeploymentserviceconfiguration))_ | Services contains the DPUDeploymentService related configuration. The key<br />is the deploymentServiceName and the value is its configuration. All<br />underlying objects must specify the same deploymentServiceName in order<br />to be able to be consumed by the DPUDeployment. |  | MaxProperties: 50 <br />MinProperties: 1 <br />Required: \{\} <br /> |
| `serviceChains` _[ServiceChains](#servicechains)_ | ServiceChains contains the configuration related to the DPUServiceChains<br />that the DPUDeployment creates. |  | Optional: \{\} <br /> |
| `revisionHistoryLimit` _integer_ | The maximum number of revisions that can be retained during upgrades.<br />Defaults to 10. | 10 | Minimum: 1 <br />Optional: \{\} <br /> |


#### DPUDeploymentStatus



DPUDeploymentStatus defines the observed state of DPUDeployment



_Appears in:_
- [DPUDeployment](#dpudeployment)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


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
| `clusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Select the Clusters with specific labels, ServiceChainSet CRs will be<br />created only for these Clusters<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use DPUClusterSelector instead. |  | Optional: \{\} <br /> |
| `dpuClusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUClusterSelector determines in which clusters the DPUServiceChain<br />controller should apply the configuration. |  | Optional: \{\} <br /> |
| `template` _[ServiceChainSetSpecTemplate](#servicechainsetspectemplate)_ | Template describes the ServiceChainSet that will be created for each<br />selected Cluster. |  |  |


#### DPUServiceChainStatus



DPUServiceChainStatus defines the observed state of DPUServiceChain



_Appears in:_
- [DPUServiceChain](#dpuservicechain)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


#### DPUServiceConfiguration



DPUServiceConfiguration is the Schema for the dpuserviceconfigurations API.
This object is intended to be used in conjunction with a DPUDeployment
object. This object is the template from which the DPUService will be
created. It contains all configuration options from the user to be provided
to the service itself via the helm chart values. This object doesn't allow
configuration of nodeSelector and resources in purpose as these are
delegated to the DPUDeployment and DPUServiceTemplate accordingly.



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



DPUServiceConfigurationServiceDaemonSetValues reflects the Helm related
configuration



_Appears in:_
- [ServiceConfiguration](#serviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `updateStrategy` _[DaemonSetUpdateStrategy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#daemonsetupdatestrategy-v1-apps)_ | UpdateStrategy specifies the DeaemonSet update strategy for the<br />ServiceDaemonset. |  | Optional: \{\} <br /> |
| `labels` _object (keys:string, values:string)_ | Labels specifies labels which are added to the ServiceDaemonSet. |  | MaxProperties: 50 <br />Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ | Annotations specifies annotations which are added to the<br />ServiceDaemonSet. |  | MaxProperties: 50 <br />Optional: \{\} <br /> |
| `resources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | Resources specifies resources which are added to the ServiceDaemonSet. |  | Optional: \{\} <br /> |


#### DPUServiceConfigurationSpec



DPUServiceConfigurationSpec defines the desired state of
DPUServiceConfiguration



_Appears in:_
- [DPUServiceConfiguration](#dpuserviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `deploymentServiceName` _string_ | DeploymentServiceName is the name of the DPU service this configuration<br />refers to. It must match .spec.deploymentServiceName of a<br />DPUServiceTemplate object and one of the keys in .spec.services of a<br />DPUDeployment object. |  | MaxLength: 28 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `serviceConfiguration` _[ServiceConfiguration](#serviceconfiguration)_ | ServiceConfiguration contains fields that are configured on the generated<br />DPUService. |  | Optional: \{\} <br /> |
| `interfaces` _[ServiceInterfaceTemplate](#serviceinterfacetemplate) array_ | Interfaces specifies the DPUServiceInterface to be generated for the<br />generated DPUService. |  | MaxItems: 50 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `upgradePolicy` _[UpgradePolicy](#upgradepolicy)_ | UpgradePolicy contains the configuration for the upgrade process | \{  \} | Required: \{\} <br /> |


#### DPUServiceConfigurationStatus



DPUServiceConfigurationStatus defines the observed state of
DPUServiceConfiguration



_Appears in:_
- [DPUServiceConfiguration](#dpuserviceconfiguration)



#### DPUServiceCredentialRequest



DPUServiceCredentialRequest is the Schema for the
dpuserviceCredentialRequests API



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



DPUServiceCredentialRequestList contains a list of
DPUServiceCredentialRequest





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `svc.dpu.nvidia.com/v1alpha1` | | |
| `kind` _string_ | `DPUServiceCredentialRequestList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DPUServiceCredentialRequest](#dpuservicecredentialrequest) array_ |  |  |  |


#### DPUServiceCredentialRequestSpec



DPUServiceCredentialRequestSpec defines the desired state of
DPUServiceCredentialRequest



_Appears in:_
- [DPUServiceCredentialRequest](#dpuservicecredentialrequest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceAccount` _[NamespacedName](#namespacedname)_ | ServiceAccount defines the needed information to create the service<br />account. |  | Required: \{\} <br /> |
| `duration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | Duration is the duration for which the token will be valid. Value must be<br />in units accepted by Go time.ParseDuration<br />https://golang.org/pkg/time/#ParseDuration. e.g. "1h", "1m", "1s", "1ms",<br />"1.5h", "2h45m". Value duration must not be less than 10 minutes.<br />**Note:** The maximum TTL for a token is 24 hours, after which the token<br />will be rotated. |  | Pattern: `^([0-9]+(\.[0-9]+)?(ms\|s\|m\|h))+$` <br />Type: string <br />Optional: \{\} <br /> |
| `targetCluster` _[NamespacedName](#namespacedname)_ | TargetCluster defines the target cluster where the service account will<br />be created, and where a token for that service account will be requested.<br />If not provided, the token will be requested for the same cluster where<br />the DPUServiceCredentialRequest object is created. |  | Optional: \{\} <br /> |
| `type` _string_ | Type is the type of the secret that will be created. The supported types<br />are `kubeconfig` and `tokenFile`. If `kubeconfig` is selected, the secret<br />will contain a kubeconfig file, that can be used to access the cluster.<br />If `tokenFile` is selected, the secret will contain a token file and<br />several environment variables that can be used to access the cluster. It<br />can be used with<br />https://github.com/kubernetes/client-go/blob/v11.0.0/rest/config.go#L52<br />to create a client that will handle file rotation. |  | Enum: [kubeconfig tokenFile] <br />Required: \{\} <br /> |
| `secret` _[NamespacedName](#namespacedname)_ | Secret defines the needed information to create the secret.<br />The secret will be of the type specified in the `spec.type` field. |  | Required: \{\} <br /> |
| `metadata` _[ObjectMeta](#objectmeta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |


#### DPUServiceCredentialRequestStatus



DPUServiceCredentialRequestStatus defines the observed state of
DPUServiceCredentialRequest



_Appears in:_
- [DPUServiceCredentialRequest](#dpuservicecredentialrequest)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  | Optional: \{\} <br /> |
| `serviceAccount` _string_ | ServiceAccount is the namespaced name of the ServiceAccount resource<br />created by the controller for the DPUServiceCredentialRequest. |  | Optional: \{\} <br /> |
| `targetCluster` _string_ | TargetCluster is the cluster where the service account was created. It<br />has to be persisted in the status to be able to delete the service<br />account when the DPUServiceCredentialRequest is updated. |  | Optional: \{\} <br /> |
| `targetClusterUID` _string_ | TargetClusterUID is the UID of the DPUCluster the credentials were last<br />issued against. Used to detect cluster recreation with the same name and<br />force credential refresh. |  | Optional: \{\} <br /> |
| `expirationTimestamp` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | ExpirationTimestamp is the time when the token will expire. |  | Optional: \{\} <br /> |
| `issuedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | IssuedAt is the time when the token was issued. |  | Optional: \{\} <br /> |
| `secret` _string_ | Sercet is the namespaced name of the Secret resource created by the<br />controller for the DPUServiceCredentialRequest. |  | Optional: \{\} <br /> |


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
| `ipv4Network` _[Network](#network)_ | IPV4Network is the configuration related to splitting a network into<br />subnets per node, each with their own gateway.<br />Deprecated: Use Network instead. |  |  |
| `ipv4Subnet` _[Subnet](#subnet)_ | IPV4Subnet is the configuration related to splitting a subnet into blocks<br />per node. In this setup, there is a single gateway.<br />Deprecated: Use Subnet instead. |  |  |
| `network` _[Network](#network)_ | Network is the configuration for splitting an IPv4 or IPv6 network into<br />subnets per node, each with their own gateway. |  | Optional: \{\} <br /> |
| `subnet` _[Subnet](#subnet)_ | Subnet is the configuration for splitting an IPv4 or IPv6 subnet into<br />blocks per node. In this setup, there is a single gateway. |  | Optional: \{\} <br /> |
| `clusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | ClusterSelector determines in which clusters the DPUServiceIPAM<br />controller should apply the configuration.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use DPUClusterSelector instead. |  | Optional: \{\} <br /> |
| `dpuClusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUClusterSelector determines in which clusters the DPUServiceIPAM<br />controller should apply the configuration. |  | Optional: \{\} <br /> |
| `nodeSelector` _[NodeSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#nodeselector-v1-core)_ | NodeSelector determines in which DPU nodes the DPUServiceIPAM controller<br />should apply the configuration. |  |  |


#### DPUServiceIPAMStatus



DPUServiceIPAMStatus defines the observed state of DPUServiceIPAM



_Appears in:_
- [DPUServiceIPAM](#dpuserviceipam)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |
| `dpuClusterAllocations` _[DPUClusterAllocation](#dpuclusterallocation) array_ | DPUClusterAllocations contains the configured network or subnet<br />allocations per DPUCluster as calculated by the controller. A<br />DPUServiceIPAM contains exactly one address-family configuration, so<br />every entry has that family. |  | Optional: \{\} <br /> |


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
| `clusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Select the Clusters with specific labels, ServiceInterfaceSet CRs will be<br />created only for these Clusters<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use DPUClusterSelector instead. |  | Optional: \{\} <br /> |
| `dpuClusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUClusterSelector determines in which clusters the DPUServiceInterface<br />controller should apply the configuration. |  | Optional: \{\} <br /> |
| `template` _[ServiceInterfaceSetSpecTemplate](#serviceinterfacesetspectemplate)_ | Template describes the ServiceInterfaceSet that will be created for each<br />selected Cluster. |  |  |


#### DPUServiceInterfaceStatus



DPUServiceInterfaceStatus defines the observed state of DPUServiceInterface



_Appears in:_
- [DPUServiceInterface](#dpuserviceinterface)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


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
| `dpuClusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUClusterSelector determines in which clusters the DPUServiceNAD<br />controller should apply the configuration. |  | Optional: \{\} <br /> |
| `resourceType` _string_ | ResourceType specifies the type of network resource to allocate for pods<br />using this NAD.<br />- "vf": Virtual Function (SR-IOV VF) from the DPU's physical ports<br />- "sf": Scalable Function from the DPU (maps to nvidia.com/bf_sf or<br />  nvidia.com/bf_sf_trusted)<br />- "veth": Virtual Ethernet pair (no device plugin resource required)<br />The resource type determines which SR-IOV device plugin resource will be<br />requested. |  | Enum: [vf sf veth] <br />Required: \{\} <br /> |
| `bridge` _string_ | Bridge specifies the name of the OVS bridge to which the network<br />interface will be connected. This bridge name is used in the CNI<br />configuration for the OVS plugin. |  | Optional: \{\} <br /> |
| `serviceMTU` _integer_ | ServiceMTU specifies the MTU size in bytes for the network interface.<br />This value is passed to the OVS CNI plugin and determines the maximum<br />packet size. If there is a DPUServiceChain that references an interface<br />that is part of this network, then the MTU that is defined in the<br />DPUServiceChain takes precedence. The default is 1500. | 1500 | Maximum: 9216 <br />Minimum: 1280 <br />Optional: \{\} <br /> |
| `ipam` _boolean_ | IPAM enables IP Address Management for the network interfaces attached to<br />this network When set to true, a DPUServiceChain that references the<br />DPUServiceInterface that has requested this network must be created and<br />include the relevant IPAM information. See DPUServiceChain documentation<br />for more. When set to false, the network interfaces attached to this<br />network will not get an IP |  | Optional: \{\} <br /> |
| `chainedCNIs` _[CNIPlugin](#cniplugin) array_ | ChainedCNIs specifies additional CNI plugins to be chained after the base<br />OVS plugin. When specified, the NAD will use the CNI chaining format with<br />the OVS plugin as the first plugin, followed by the plugins defined in<br />this list. This allows adding capabilities like RDMA support on top of<br />the base network interface. If empty, the NAD uses a single OVS plugin<br />configuration (backward compatible format). |  | Optional: \{\} <br /> |


#### DPUServiceNADStatus



DPUServiceNADStatus defines the observed state of DPUServiceNAD.



_Appears in:_
- [DPUServiceNAD](#dpuservicenad)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |




#### DPUServiceSPIFFE



DPUServiceSPIFFE opts a DPUService into SPIFFE workload identity.
Intentionally empty: the opt-in is the field's presence, and per-service
knobs are deferred until the NKE PoC settles on-DPU workload attestation.



_Appears in:_
- [DPUServiceSecurity](#dpuservicesecurity)



#### DPUServiceSecurity



DPUServiceSecurity contains security-related settings for a DPUService or
DPUServiceTemplate.



_Appears in:_
- [DPUServiceSpec](#dpuservicespec)
- [DPUServiceTemplateSpec](#dpuservicetemplatespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `privileged` _boolean_ | Privileged, when set to true, allows workloads governed by this security<br />setting to run containers with `securityContext.privileged: true` in the<br />DPUCluster. When set to false, a ValidatingAdmissionPolicy in the<br />DPUCluster rejects such workloads. On DPUService objects, this field must<br />be unset when deployInCluster is true. For DPUServices that target a<br />DPUCluster, Privileged must be set explicitly. When set on a<br />DPUServiceTemplate, the DPUDeployment controller propagates the value to<br />generated DPUServices that target DPUClusters.<br />Scope of the policy enforcement:<br />  - Only `securityContext.privileged: true` is gated. Other<br />    privilege-escalation vectors (hostPID, hostIPC, hostNetwork,<br />    allowPrivilegeEscalation, capabilities, hostPath volumes) are NOT<br />    gated by this field.<br />  - Enforcement matches workloads via the<br />    `svc.dpu.nvidia.com/service` label. The controller adds this<br />    label to the resources it manages, and to pod templates of<br />    workload-controller resources (Deployment, DaemonSet, etc.). If a<br />    Helm chart strips that label from the pod template, the parent<br />    resource will be admitted but the child Pods will be denied at<br />    Pod admission time. |  | Optional: \{\} <br /> |
| `spiffe` _[DPUServiceSPIFFE](#dpuservicespiffe)_ | SPIFFE opts this DPUService into SPIFFE workload identity. When set, one<br />SPIRE ClusterStaticEntry is registered per targeted DPU, parented to that<br />DPU's SPIRE agent, so the service's pods can obtain an SVID. The identity<br />defaults to<br />`spiffe://<trustDomain>/dpu/<serial>/service/<namespace>/<serviceID>` and<br />is configurable through `DPFOperatorConfig.spec.security.spiffe`. The<br />namespace qualifies the service ID, which is only unique within one.<br />Presence-gated: `spiffe: \{\}` enables it. It has no sub-fields yet.<br />Requires `DPFOperatorConfig.spec.security.spiffe`, and only covers DPUs<br />in SPIFFE identity mode (`DPU.status.identityMode: Spiffe`);<br />bootstrap-token DPUs are skipped. Both are reported on the<br />SPIFFEEntriesReady condition rather than rejected at admission. Must not<br />be set when deployInCluster is true, since in-cluster DPUServices run on<br />the host and have no per-DPU SPIRE agent to parent to.<br />When set on a DPUServiceTemplate, the DPUDeployment controller propagates<br />it to generated DPUServices that target DPUClusters. |  | Optional: \{\} <br /> |


#### DPUServiceSpec



DPUServiceSpec defines the desired state of DPUService



_Appears in:_
- [DPUService](#dpuservice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dpuClusterSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | Select the Clusters with specific labels, Applications will be created<br />only for these Clusters |  | Optional: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart reflects the Helm related configuration |  | Required: \{\} <br /> |
| `serviceID` _string_ | ServiceID is the ID of the service that the DPUService is<br />associated with. |  | Optional: \{\} <br /> |
| `serviceDaemonSet` _[ServiceDaemonSetValues](#servicedaemonsetvalues)_ | ServiceDaemonSet specifies the configuration for the ServiceDaemonSet. |  | Optional: \{\} <br /> |
| `deployInCluster` _boolean_ | DeployInCluster indicates if the DPUService Helm Chart will be deployed<br />on the Host cluster. Default to false. |  | Optional: \{\} <br /> |
| `interfaces` _string array_ | Interfaces specifies the DPUServiceInterface names that the DPUService<br />uses in the same namespace. |  | MaxItems: 50 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `paused` _boolean_ | Paused indicates that the DPUService is paused.<br />Underlying resources are also paused when this is set to true.<br />No deletion of resources will occur when this is set to true. |  | Optional: \{\} <br /> |
| `configPorts` _[ConfigPorts](#configports)_ | ConfigPorts defines the desired state of port configurations for a<br />DPUService. This struct determines how ports are exposed from the DPU to<br />the host cluster. A DPUService can only have a single ServiceType across<br />all ports. |  | Optional: \{\} <br /> |
| `security` _[DPUServiceSecurity](#dpuservicesecurity)_ | Security contains security-related settings for the DPUService. |  | Optional: \{\} <br /> |


#### DPUServiceStatus



DPUServiceStatus defines the observed state of DPUService



_Appears in:_
- [DPUService](#dpuservice)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions defines current service state. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |
| `configPorts` _object (keys:string, values:[ConfigPort](#configport))_ | ConfigPorts defines the observed state of the config ports. It contains<br />the actual port numbers that are exposed on the DPUService per cluster. |  | Optional: \{\} <br /> |
| `serviceID` _string_ | ServiceID is the ID of the service that the DPUService is associated<br />with. This is set when the DPUService is created. |  | Optional: \{\} <br /> |


#### DPUServiceTemplate



DPUServiceTemplate is the Schema for the DPUServiceTemplate API. This object
is intended to be used in conjunction with a DPUDeployment object. This
object is the template from which the DPUService will be created. It contains
configuration options related to resources required by the service to be
deployed. The rest of the configuration options must be defined in a
DPUServiceConfiguration object.



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
| `deploymentServiceName` _string_ | DeploymentServiceName is the name of the DPU service this configuration<br />refers to. It must match .spec.deploymentServiceName of a<br />DPUServiceConfiguration object and one of the keys in .spec.services of a<br />DPUDeployment object. |  | MaxLength: 28 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `helmChart` _[HelmChart](#helmchart)_ | HelmChart reflects the Helm related configuration. The user is supposed<br />to configure the values that are static across any<br />DPUServiceConfiguration used with this DPUServiceTemplate in a<br />DPUDeployment. These values act as a baseline and are merged with values<br />specified in the DPUServiceConfiguration. In case of conflict, the<br />DPUServiceConfiguration values take precedence. |  | Required: \{\} <br /> |
| `resourceRequirements` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | ResourceRequirements contains the overall resources required by this<br />particular service to run on a single node |  | Optional: \{\} <br /> |
| `security` _[DPUServiceSecurity](#dpuservicesecurity)_ | Security contains security-related settings for the generated DPUService.<br />These settings are inherent to the service and apply to all deployments<br />using this template. Specifically, Privileged should be set on charts<br />that need privileged containers (e.g. for direct hardware access). |  | Optional: \{\} <br /> |


#### DPUServiceTemplateStatus



DPUServiceTemplateStatus defines the observed state of DPUServiceTemplate



_Appears in:_
- [DPUServiceTemplate](#dpuservicetemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |
| `versions` _object (keys:string, values:string)_ | Versions reflects the required versions the generated DPUService needs in<br />order to function correctly. |  | Optional: \{\} <br /> |


#### DPUSet



DPUSet contains configuration for the DPUSet to be created by
the DPUDeployment



_Appears in:_
- [DPUs](#dpus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nameSuffix` _string_ | NameSuffix is the suffix to be added to the name of the DPUSet object<br />created by the DPUDeployment. |  | MaxLength: 24 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | NodeSelector defines the nodes that the DPUSet should target<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use DPUNodeSelector instead. |  | Optional: \{\} <br /> |
| `dpuSelector` _object (keys:string, values:string)_ | DPUSelector defines the DPUs that the DPUSet should target<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use DPUDeviceSelector instead. |  | Optional: \{\} <br /> |
| `dpuNodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUNodeSelector defines the selector for DPUNodes that the DPUSet should<br />target and should create a DPU for. |  | Optional: \{\} <br /> |
| `dpuDeviceSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | DPUDeviceSelector defines the selector for DPUDevices that the DPUSet<br />should target and should create a DPU for. |  | Optional: \{\} <br /> |
| `dpuClusterSelector` _object (keys:string, values:string)_ | DPUClusterSelector defines the selector for DPUClusters that the DPUs<br />created by the DPUSets created by the DPUDeployment should join |  | Optional: \{\} <br /> |
| `dpuAnnotations` _object (keys:string, values:string)_ | DPUAnnotations is the annotations to be added to the DPU object created<br />by the DPUSet. |  | MaxProperties: 50 <br />Optional: \{\} <br /> |


#### DPUs



DPUs contains the DPU related configuration



_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bfb` _string_ | BFB is the name of the BFB object to be used in this DPUDeployment. It<br />must be in the same namespace as the DPUDeployment. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `blueFieldSoftware` _string_ | BlueFieldSoftware is the name of the BlueFieldSoftware object to be used<br />in this DPUDeployment. It must be in the same namespace as<br />the DPUDeployment. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `flavor` _string_ | Flavor is the name of the DPUFlavor object to be used in this<br />DPUDeployment. It must be in the same namespace as the DPUDeployment.<br />Exactly one of flavor or flavorTemplate must be set (mutually exclusive). |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `flavorTemplate` _string_ | FlavorTemplate is the name of the DPUFlavorTemplate object to be rendered<br />per-DPU (against each DPUDevice.spec.values) into a generated DPUFlavor.<br />It must be in the same namespace as the DPUDeployment. Exactly one of<br />flavor or flavorTemplate must be set (mutually exclusive). |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `astraEnabled` _boolean_ | AstraEnabled indicates whether E/W NIC configuration (Astra) is enabled |  | Optional: \{\} <br /> |
| `dpuSets` _[DPUSet](#dpuset) array_ | DPUSets contains configuration for each DPUSet that is going to be<br />created by the DPUDeployment |  | MaxItems: 50 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `nodeEffect` _[Action](#action)_ | NodeEffect is the effect the DPU has on Nodes during provisioning. |  | Required: \{\} <br /> |
| `dpuSetStrategy` _[DPUSetStrategy](#dpusetstrategy)_ | DPUSetStrategy is the strategy to use for the DPUSets created by<br />the DPUDeployment. |  | Required: \{\} <br /> |
| `secureBoot` _boolean_ | SecureBoot specifies whether UEFI Secure Boot should be enabled. |  | Optional: \{\} <br /> |


#### HelmChart



HelmChart reflects the helm related configuration



_Appears in:_
- [DPUServiceSpec](#dpuservicespec)
- [DPUServiceTemplateSpec](#dpuservicetemplatespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `source` _[ApplicationSource](#applicationsource)_ | Source specifies information about the Helm chart |  | Required: \{\} <br /> |
| `values` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Values specifies Helm values to be passed to Helm template, defined as a<br />map. This takes precedence over Values. |  | Optional: \{\} <br /> |




#### IPRange







_Appears in:_
- [DPUClusterAllocation](#dpuclusterallocation)
- [Network](#network)
- [Subnet](#subnet)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `startIP` _string_ | StartIP is the start of the range. |  | Required: \{\} <br /> |
| `endIP` _string_ | EndIP is the end of the range. |  | Required: \{\} <br /> |


#### InterfaceEntry



InterfaceEntry defines a single service interface entry within a
NodeServiceInterfaces object.



_Appears in:_
- [NodeServiceInterfacesSpec](#nodeserviceinterfacesspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name uniquely identifies this entry within the NodeServiceInterfaces<br />object. Format: `<namespace>_<service-interface-set-name>`. The<br />underscore separator is collision-free because Kubernetes namespace and<br />resource names follow DNS subdomain rules and can never contain an<br />underscore. Both components are bounded to 63 chars, so the full name is<br />at most 127 chars. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
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



InterfaceEntryStatus records reconciliation state for a single
interface entry.



_Appears in:_
- [NodeServiceInterfacesStatus](#nodeserviceinterfacesstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name matches the spec entry name. |  | MinLength: 1 <br />Required: \{\} <br /> |
| `observedSpecHash` _string_ | ObservedSpecHash is the SpecHash of the InterfaceEntry this status was<br />reconciled against. Consumers treat entry conditions as fresh only when<br />this matches the current spec entry hash, so sibling mutations that bump<br />NSI metadata.generation do not invalidate unrelated entries. Empty is not<br />a valid hash; omit the field until the entry has been reconciled. |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `params` _object (keys:string, values:string)_ | Params carries controller-specific information for handling<br />reconcile/release of the matching InterfaceEntry in spec. Multiple<br />reconcilers may write distinct keys safely as long as they do not<br />share keys. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions may be written by multiple reconcilers for the same entry. |  | Optional: \{\} <br /> |


#### LocalObjectDependency



LocalObjectDependency is a list of local object dependencies that are
required for this Service. The object must be part of the dpuDeployment
`spec.services` list.



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
| `controllerNumber` _integer_ | ControllerNumber specifies the controller number that will be used to<br />find the matching representor on the DPU. A value of 0 targets the local<br />controller, >=1 targets external controllers with the specified number.<br />If unspecified controller number 1 is used. In case of a DPU/NIC with<br />socket direct or MultiHost, the user should specify the controller<br />number explicitly |  | Maximum: 255 <br />Minimum: 0 <br />Optional: \{\} <br /> |


#### NICSelectorType

_Underlying type:_ _string_

NICSelectorType is the type of NIC selector



_Appears in:_
- [NICSelectorSpec](#nicselectorspec)

| Field | Description |
| --- | --- |
| `dpu` | NICSelectorTypeDPU selects the DPU NIC<br /> |
| `pci` | NICSelectorTypePCI selects NIC according to the provided PCI address of<br />one of the NIC's Embedded CPU PFs (ECPFs)<br /> |


#### NamespacedName



NamespacedName contains enough information to locate the referenced
Kubernetes resource object in any namespace.



_Appears in:_
- [DPUServiceCredentialRequestSpec](#dpuservicecredentialrequestspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name of the object. |  | Required: \{\} <br /> |
| `namespace` _string_ | Namespace of the object, if not provided the object will be looked up in<br />the same namespace as the referring object |  | Optional: \{\} <br /> |


#### Network



Network describes the configuration for splitting a network into a subnet per
node with a different gateway per node.



_Appears in:_
- [DPUServiceIPAMSpec](#dpuserviceipamspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `network` _string_ | Network is the CIDR from which subnets should be allocated per node. |  |  |
| `gatewayIndex` _integer_ | GatewayIndex determines which IP in the subnet extracted from the CIDR<br />should be the gateway IP. For point to point networks (/31 or /127), one<br />needs to leave this empty to make use of both IPs. |  |  |
| `prefixSize` _integer_ | PrefixSize is the size of the subnet that should be allocated per node. |  |  |
| `exclusions` _string array_ | Exclusions is a list of IPs that should be excluded when splitting the<br />CIDR into subnets per node.<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Use ExcludeRanges instead. |  |  |
| `excludeRanges` _[IPRange](#iprange) array_ | ExcludeRanges is a list of IP ranges that should be excluded from the<br />allocation. startIP and endIP are part of the Excluded range. |  |  |
| `allocations` _object (keys:string, values:string)_ | Allocations describes the subnets that should be assigned in each<br />DPU node. |  |  |
| `defaultGateway` _boolean_ | DefaultGateway adds gateway as default gateway in the routes<br />list if true. |  |  |
| `routes` _[Route](#route) array_ | Routes is the static routes list using the gateway specified in the spec. |  |  |
| `subnetsPerDPUCluster` _integer_ | SubnetsPerDPUCluster is the number of PrefixSize-sized subnets each<br />DPUCluster should receive. When specified, the controller will take care<br />of assigning non-overlapping subnets part of the Network in each<br />DPUCluster that the DPUServiceIPAM is targeting. Leave empty in case you<br />want the whole Network to be consumed by a single DPUCluster. |  | Optional: \{\} <br /> |


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



NodeServiceInterfacesStatus defines the observed state of
NodeServiceInterfaces.



_Appears in:_
- [NodeServiceInterfaces](#nodeserviceinterfaces)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect aggregate status. |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration is the last observed generation of the<br />NodeServiceInterfaces object. |  | Optional: \{\} <br /> |
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
| `peerBridge` _string_ | PeerBridge is the name of the bridge to which the patch port is<br />connected. This bridge must be created before the ServiceInterface is<br />created. |  | Required: \{\} <br /> |
| `peerPatchName` _string_ | PeerPatchName is the name of the patch port on the peer bridge. If not<br />set, it is auto-generated in the format:<br />`p_<bridgeA>_to_<bridgeB>_<hash>` where bridge names have hyphens removed<br />and `<hash>` is an 8-character FNV-1a hash derived from the<br />ServiceInterface's namespace/name. Example: p_brovn_to_brsfc_7aea60f7<br />(for bridges br-ovn and br-sfc). |  | Optional: \{\} <br /> |
| `peerExternalIDs` _object (keys:string, values:string)_ | PeerExternalIDs are the external IDs used to identify the peer patch<br />port. |  | Optional: \{\} <br /> |


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
- [Network](#network)
- [Subnet](#subnet)

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



ServiceChainSetSpecTemplate describes the data a ServiceChainSet should have
when created from a template.



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
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |
| `numberApplied` _integer_ | The number of nodes where the service chain is applied and is supposed to<br />be applied. |  | Optional: \{\} <br /> |
| `numberReady` _integer_ | The number of nodes where the service chain is applied and ready. |  | Optional: \{\} <br /> |


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
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


#### ServiceChains







_Appears in:_
- [DPUDeploymentSpec](#dpudeploymentspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `upgradePolicy` _[UpgradePolicy](#upgradepolicy)_ | UpgradePolicy contains the configuration for the upgrade process | \{  \} | Required: \{\} <br /> |
| `switches` _[DPUDeploymentSwitch](#dpudeploymentswitch) array_ | Switches is the list of switches that form the service chain |  | MaxItems: 50 <br />MinItems: 1 <br />Required: \{\} <br /> |


#### ServiceConfiguration



ServiceConfiguration contains fields that are configured on the generated
DPUService.



_Appears in:_
- [DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `helmChart` _[ServiceConfigurationHelmChart](#serviceconfigurationhelmchart)_ | HelmChart reflects the Helm related configuration. The user is supposed<br />to configure values specific to that DPUServiceConfiguration used in a<br />DPUDeployment and should not specify values that could be shared across<br />multiple DPUDeployments using different DPUServiceConfigurations. These<br />values are merged with values specified in the DPUServiceTemplate. In<br />case of conflict, the DPUServiceConfiguration values take precedence. |  | Optional: \{\} <br /> |
| `serviceDaemonSet` _[DPUServiceConfigurationServiceDaemonSetValues](#dpuserviceconfigurationservicedaemonsetvalues)_ | ServiceDaemonSet contains settings related to the underlying DaemonSet<br />that is part of the Helm chart |  | Optional: \{\} <br /> |
| `deployInCluster` _boolean_ | DeployInCluster indicates if the DPUService Helm Chart will be deployed<br />on the Host cluster. Default to false. |  | Optional: \{\} <br /> |
| `configPorts` _[ConfigPorts](#configports)_ | ConfigPorts defines the desired state of port configurations for a<br />DPUService. This struct determines how ports are exposed from the DPU to<br />the host cluster. A DPUService can only have a single ServiceType across<br />all ports. |  | Optional: \{\} <br /> |


#### ServiceConfigurationHelmChart



ServiceConfigurationHelmChart reflects the helm related configuration



_Appears in:_
- [ServiceConfiguration](#serviceconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `values` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Values specifies Helm values to be passed to Helm template, defined as a<br />map. This takes precedence over Values. |  | Optional: \{\} <br /> |


#### ServiceDaemonSetValues



ServiceDaemonSetValues specifies the configuration for the ServiceDaemonSet.



_Appears in:_
- [DPUServiceSpec](#dpuservicespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[NodeSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#nodeselector-v1-core)_ | NodeSelector specifies which Nodes to deploy the ServiceDaemonSet to. |  | Optional: \{\} <br /> |
| `updateStrategy` _[DaemonSetUpdateStrategy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#daemonsetupdatestrategy-v1-apps)_ | UpdateStrategy specifies the DeaemonSet update strategy for the<br />ServiceDaemonset. |  | Optional: \{\} <br /> |
| `labels` _object (keys:string, values:string)_ | Labels specifies labels which are added to the ServiceDaemonSet. |  | Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ | Annotations specifies annotations which are added to the<br />ServiceDaemonSet. |  | Optional: \{\} <br /> |
| `resources` _[ResourceList](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcelist-v1-core)_ | Resources specifies resources which are added to the ServiceDaemonSet. |  | Optional: \{\} <br /> |


#### ServiceDef



ServiceDef Identifies the service and network for the ServiceInterface



_Appears in:_
- [InterfaceEntry](#interfaceentry)
- [ServiceInterfaceSpec](#serviceinterfacespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceID` _string_ | ServiceID is the DPU Service Identifier |  | Required: \{\} <br /> |
| `network` _string_ | Network is the Network Attachment Definition in the form of<br />"namespace/name" or just "name" if the namespace is the same as the<br />ServiceInterface. |  | Required: \{\} <br /> |
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
| `ipam` _[IPAM](#ipam)_ | IPAM defines the IPAM configuration when referencing a serviceInterface<br />of type 'service' |  | Optional: \{\} <br /> |


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



ServiceInterfaceSetSpecTemplate describes the data a ServiceInterfaceSet
should have when created from a template.



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
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |
| `numberApplied` _integer_ | The number of nodes where the service chain is applied and is supposed to<br />be applied. |  | Optional: \{\} <br /> |
| `numberReady` _integer_ | The number of nodes where the service chain is applied and ready. |  | Optional: \{\} <br /> |


#### ServiceInterfaceSpec



ServiceInterfaceSpec defines the desired state of ServiceInterface



_Appears in:_
- [ServiceInterface](#serviceinterface)
- [ServiceInterfaceSpecTemplate](#serviceinterfacespectemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `node` _string_ | Node where this interface exists |  | Optional: \{\} <br /> |
| `interfaceType` _string_ | The interface type ("vlan", "physical", "pf", "vf", "ovn", "patch",<br />"service") |  | Enum: [vlan physical pf vf ovn patch service] <br />Required: \{\} <br /> |
| `physical` _[Physical](#physical)_ | The physical interface definition |  | Optional: \{\} <br /> |
| `vlan` _[VLAN](#vlan)_ | The VLAN definition |  | Optional: \{\} <br /> |
| `vf` _[VF](#vf)_ | The VF definition |  | Optional: \{\} <br /> |
| `pf` _[PF](#pf)_ | The PF definition |  | Optional: \{\} <br /> |
| `service` _[ServiceDef](#servicedef)_ | The Service definition |  | Optional: \{\} <br /> |
| `ovn` _[OVN](#ovn)_ | The OVN definition<br />Deprecated: This field is deprecated and will be removed with v31.0.0.<br />Migrate to interfaceType="patch" with spec.patch.peerBridge and<br />spec.patch.peerPatchName instead. |  | Optional: \{\} <br /> |
| `patch` _[PatchDef](#patchdef)_ | The Patch definition |  | Optional: \{\} <br /> |


#### ServiceInterfaceSpecTemplate



ServiceInterfaceSpecTemplate defines the template from which
ServiceInterfaceSpecs are created



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
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


#### ServiceInterfaceTemplate



ServiceInterfaceTemplate contains the information related to an interface of
the DPUService



_Appears in:_
- [DPUServiceConfigurationSpec](#dpuserviceconfigurationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the interface |  | MaxLength: 15 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `network` _string_ | Network is the Network Attachment Definition in the form of<br />"namespace/name" or just "name" if the namespace is the same as the<br />namespace the pod is running. |  | Required: \{\} <br /> |
| `virtualNetwork` _string_ | VirtualNetwork is the VirtualNetwork name in the same namespace |  | Optional: \{\} <br /> |


#### Subnet



Subnet describes the configuration for splitting a subnet into blocks per
node with the same gateway across all nodes.



_Appears in:_
- [DPUServiceIPAMSpec](#dpuserviceipamspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `subnet` _string_ | Subnet is the CIDR from which blocks should be allocated per node |  |  |
| `gateway` _string_ | Gateway is the IP in the subnet that should be the gateway of the subnet. |  |  |
| `perNodeIPCount` _integer_ | PerNodeIPCount is the number of IPs that should be allocated per node. |  |  |
| `excludeRanges` _[IPRange](#iprange) array_ | ExcludeRanges is a list of IP ranges that should be excluded from the<br />allocation. startIP and endIP are part of the Excluded range. |  |  |
| `defaultGateway` _boolean_ | if true, add gateway as default gateway in the routes list DefaultGateway<br />adds gateway as default gateway in the routes list if true. |  |  |
| `routes` _[Route](#route) array_ | Routes is the static routes list using the gateway specified in the spec. |  |  |
| `blocksPerDPUCluster` _integer_ | BlocksPerDPUCluster is the number of PerNodeIPCount-sized blocks each<br />DPUCluster should receive. When specified, the controller will take care<br />of assigning non-overlapping IP blocks part of the Subnet in each<br />DPUCluster that the DPUServiceIPAM is targeting. Leave empty in case you<br />want the whole Subnet to be consumed by a single DPUCluster. |  | Optional: \{\} <br /> |


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
| `applyNodeEffect` _boolean_ | ApplyNodeEffect specifies if the node effect should be applied during the<br />upgrade. It signals the reconciler that this object upgrade is<br />disruptive. Hence a new revision of the object should be created and node<br />effect should be applied. | true | Optional: \{\} <br /> |


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

Package v1alpha1 contains API Schema definitions for the storage v1alpha1 API
group




### Resource Types
- [DPUVPC](#dpuvpc)
- [DPUVPCList](#dpuvpclist)
- [DPUVirtualNetwork](#dpuvirtualnetwork)
- [DPUVirtualNetworkList](#dpuvirtualnetworklist)
- [IsolationClass](#isolationclass)
- [IsolationClassList](#isolationclasslist)



#### BridgedNetworkIPAMIPv4Spec



BridgedNetworkIPAMIPv4Spec contains IPv4 IPAM configuration for
bridged network



_Appears in:_
- [BridgedNetworkIPAMSpec](#bridgednetworkipamspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `dhcp` _boolean_ | DHCP if set, enables DHCP for the network |  | Required: \{\} <br /> |
| `subnet` _string_ | Subnet is the network subnet in CIDR format to use for DHCP. the first<br />IP in the subnet is the gateway. |  | Required: \{\} <br /> |
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
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | NodeSelector Selects the DPU Nodes with specific labels which belong to<br />this VPC. |  | Optional: \{\} <br /> |
| `isolationClassName` _string_ | IsolationClassName is the name of the isolation class to use for the VPC |  | MinLength: 1 <br />Required: \{\} <br /> |
| `interNetworkAccess` _boolean_ | InterNetworkAccess defines if virtual networks within the VPC are routed<br />or not. if set to false, communication between virtual networks is not<br />allowed. |  | Required: \{\} <br /> |


#### DPUVPCStatus



DPUVPCStatus defines the observed state of DPUVPC



_Appears in:_
- [DPUVPC](#dpuvpc)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `virtualNetworks` _[VirtualNetworkStatus](#virtualnetworkstatus) array_ | VirtualNetworks contains the virtual networks that belong to this VPC |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


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
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#labelselector-v1-meta)_ | NodeSelector Selects the DPU Nodes with specific labels which can<br />belong to the virtual network. |  | Optional: \{\} <br /> |
| `vpcName` _string_ | vpcName is the name of the DPUVPC the virtual network belongs within<br />the same namespace. |  | Required: \{\} <br /> |
| `type` _[NetworkType](#networktype)_ | Type of the virtual network |  | Enum: [Bridged] <br />Required: \{\} <br /> |
| `externallyRouted` _boolean_ | ExternallyRouted defines if the virtual network can be routed externally |  | Required: \{\} <br /> |
| `masquerade` _boolean_ | Masquerade defines if the virtual network should masquerade the traffic<br />before egressing to external networks. valid only if ExternallyRouted<br />is true | true | Optional: \{\} <br /> |
| `bridgedNetwork` _[BridgedNetworkSpec](#bridgednetworkspec)_ | BridgedNetwork contains the bridged network configuration |  | Optional: \{\} <br /> |


#### DPUVirtualNetworkStatus



DPUVirtualNetworkStatus defines the observed state of DPUVirtualNetwork



_Appears in:_
- [DPUVirtualNetwork](#dpuvirtualnetwork)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#condition-v1-meta) array_ | Conditions reflect the status of the object |  | Optional: \{\} <br /> |
| `observedGeneration` _integer_ | ObservedGeneration records the Generation observed on the object the last<br />time it was patched. |  | Optional: \{\} <br /> |


#### ExcludeIPsEntry







_Appears in:_
- [BridgedNetworkIPAMIPv4Spec](#bridgednetworkipamipv4spec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ip` _string_ | IP is the IP address to exclude from DHCP allocation. must be part<br />for the virtual network subnet. |  | Optional: \{\} <br /> |
| `range` _[RangeEntry](#rangeentry)_ | Range is the range of IP addresses to exclude from DHCP allocation.<br />must be part for the virtual network subnet. |  | Optional: \{\} <br /> |


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


