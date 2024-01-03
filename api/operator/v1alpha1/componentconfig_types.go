/*
Copyright 2024 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

const (
	ProvisioningControllerName = "provisioning-controller"
	DPUServiceControllerName   = "dpuservice-controller"
	ServiceSetControllerName   = "servicechainset-controller"
	DPUDetectorName            = "dpudetector"
	FlannelName                = "flannel"
	MultusName                 = "multus"
	SRIOVDevicePluginName      = "sriov-device-plugin"
	OVSCNIName                 = "ovs-cni"
	NVIPAMName                 = "nvidia-k8s-ipam"
	SFCControllerName          = "sfc-controller"
	KamajiClusterManagerName   = "kamaji-cluster-manager"
	StaticClusterManagerName   = "static-cluster-manager"
	OVSHelperName              = "ovs-helper"
)

func (c *DPFOperatorConfig) ComponentConfigs() []ComponentConfig {
	out := []ComponentConfig{}

	out = append(out, c.Spec.ProvisioningController)

	if c.Spec.DPUServiceController != nil {
		out = append(out, c.Spec.DPUServiceController)
	}
	if c.Spec.DPUDetector != nil {
		out = append(out, c.Spec.DPUDetector)
	}
	if c.Spec.Flannel != nil {
		out = append(out, c.Spec.Flannel)
	}
	if c.Spec.Multus != nil {
		out = append(out, c.Spec.Multus)
	}
	if c.Spec.SRIOVDevicePlugin != nil {
		out = append(out, c.Spec.SRIOVDevicePlugin)
	}
	if c.Spec.NVIPAM != nil {
		out = append(out, c.Spec.NVIPAM)
	}
	if c.Spec.SFCController != nil {
		out = append(out, c.Spec.SFCController)
	}
	if c.Spec.OVSCNI != nil {
		out = append(out, c.Spec.OVSCNI)
	}
	if c.Spec.ServiceSetController != nil {
		out = append(out, c.Spec.ServiceSetController)
	}
	if c.Spec.KamajiClusterManager != nil {
		out = append(out, c.Spec.KamajiClusterManager)
	}
	if c.Spec.StaticClusterManager != nil {
		out = append(out, c.Spec.StaticClusterManager)
	}
	if c.Spec.OVSHelper != nil {
		out = append(out, c.Spec.OVSHelper)
	}
	return out
}

// ComponentConfig defines the shared config for all components deployed by the DPF Operator.
// +kubebuilder:object:generate=false
type ComponentConfig interface {
	Name() string
	Disabled() bool
}

// ImageComponentConfig is the shared config for helm components.
//
// +kubebuilder:object:generate=false
type ImageComponentConfig interface {
	GetImage() *string
}

// Image is a reference to a container image.
// +kubebuilder:validation:Pattern= `^((?:(?:(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]{0,127}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]{32,}))?$`
// Validation is the same as the implementation at https://github.com/containers/image/blob/93fa49b0f1fb78470512e0484012ca7ad3c5c804/docker/reference/regexp.go
type Image *string

// HelmComponentConfig is the shared config for helm components.
//
// +kubebuilder:object:generate=false
type HelmComponentConfig interface {
	GetHelmChart() *string
}

// HelmChart is a reference to a helm chart.
// +kubebuilder:validation:Pattern=`^(oci://|https://).+$`
// +optional
type HelmChart *string

type ProvisioningControllerConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable"`

	//Image overrides the container image used by the Provisioning controller
	// +optional
	Image Image `json:"image,omitempty"`

	// BFBPersistentVolumeClaimName is the name of the PersistentVolumeClaim used by dpf-provisioning-controller
	// +kubebuilder:validation:MinLength=1
	BFBPersistentVolumeClaimName string `json:"bfbPVCName"`

	// DMSTimeout is the max time in seconds within which a DMS API must respond, 0 is unlimited
	// +kubebuilder:validation:Minimum=1
	// +optional
	DMSTimeout *int `json:"dmsTimeout,omitempty"`
}

func (c ProvisioningControllerConfiguration) Name() string {
	return ProvisioningControllerName
}

func (c ProvisioningControllerConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c ProvisioningControllerConfiguration) GetImage() *string {
	return c.Image
}

type DPUServiceControllerConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Image overrides the container image used by the DPUService controller
	// +optional
	Image Image `json:"image,omitempty"`
}

func (c *DPUServiceControllerConfiguration) Name() string {
	return DPUServiceControllerName
}

func (c *DPUServiceControllerConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *DPUServiceControllerConfiguration) GetImage() *string {
	return c.Image
}

// DPUDetectorConfiguration is the configuration for the DPUDetector Component.
type DPUDetectorConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`
	// Image overrides the container image used by the component.
	// +optional
	Image Image `json:"image,omitempty"`
	// Collectors enables or disables specific collectors.
	// +optional
	Collectors *DPUDetectorCollectors `json:"collectors,omitempty"`
}

func (c *DPUDetectorConfiguration) GetImage() *string {
	return c.Image
}

type DPUDetectorCollectors struct {
	// PSID enables collecting PSID information for DPUs on nodes.
	// +optional
	PSID *bool `json:"psID,omitempty"`
}

func (c *DPUDetectorConfiguration) Name() string {
	return DPUDetectorName
}

func (c *DPUDetectorConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

type KamajiClusterManagerConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Image overrides the container image used by the HostedControlPlaneManager.
	// +optional
	Image Image `json:"image,omitempty"`
}

func (c *KamajiClusterManagerConfiguration) Name() string {
	return KamajiClusterManagerName
}

func (c *KamajiClusterManagerConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *KamajiClusterManagerConfiguration) GetImage() *string {
	return c.Image
}

type StaticClusterManagerConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Image is the container image used by the StaticControlPlaneManager
	// Image overrides the container image used by the HostedControlPlaneManager.
	// +optional
	Image Image `json:"image,omitempty"`
}

func (c *StaticClusterManagerConfiguration) Name() string {
	return StaticClusterManagerName
}

func (c *StaticClusterManagerConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *StaticClusterManagerConfiguration) GetImage() *string {
	return c.Image
}

type ServiceSetControllerConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Image overrides the container image used by the ServiceSetController
	// +optional
	Image Image `json:"image,omitempty"`

	// HelmChart overrides the helm chart used by the ServiceSet controller.
	// The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid
	// OCI registry or a web-based repository.
	// +optional
	HelmChart HelmChart `json:"helmChart,omitempty"`
}

func (c *ServiceSetControllerConfiguration) Name() string {
	return ServiceSetControllerName
}

func (c *ServiceSetControllerConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *ServiceSetControllerConfiguration) GetImage() *string {
	return c.Image
}

func (c *ServiceSetControllerConfiguration) GetHelmChart() *string {
	return c.HelmChart
}

type FlannelConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// HelmChart overrides the helm chart used by the Flannel
	// The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid
	// OCI registry or a web-based repository.
	// +optional
	HelmChart HelmChart `json:"helmChart,omitempty"`
}

func (c *FlannelConfiguration) Name() string {
	return FlannelName
}

func (c *FlannelConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *FlannelConfiguration) GetHelmChart() *string {
	return c.HelmChart
}

type MultusConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Image overrides the container image used by Multus
	// +optional
	Image Image `json:"image,omitempty"`

	// HelmChart overrides the helm chart used by Multus
	// The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid
	// OCI registry or a web-based repository.
	// +optional
	HelmChart HelmChart `json:"helmChart,omitempty"`
}

func (c *MultusConfiguration) Name() string {
	return MultusName
}

func (c *MultusConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *MultusConfiguration) GetImage() *string {
	return c.Image
}

func (c *MultusConfiguration) GetHelmChart() *string {
	return c.HelmChart
}

type NVIPAMConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Image overrides the container image used by NVIPAM
	// +optional
	Image Image `json:"image,omitempty"`

	// HelmChart overrides the helm chart used by NVIPAM
	// The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid
	// OCI registry or a web-based repository.
	// +optional
	HelmChart HelmChart `json:"helmChart,omitempty"`
}

func (c *NVIPAMConfiguration) Name() string {
	return NVIPAMName
}

func (c *NVIPAMConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *NVIPAMConfiguration) GetImage() *string {
	return c.Image
}

func (c *NVIPAMConfiguration) GetHelmChart() *string {
	return c.HelmChart
}

type SRIOVDevicePluginConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Image overrides the container image used by the SRIOV Device Plugin
	// +optional
	Image Image `json:"image,omitempty"`

	// HelmChart overrides the helm chart used by the SRIOV Device Plugin
	// The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid
	// OCI registry or a web-based repository.
	// +optional
	HelmChart HelmChart `json:"helmChart,omitempty"`
}

func (c *SRIOVDevicePluginConfiguration) Name() string {
	return SRIOVDevicePluginName
}

func (c *SRIOVDevicePluginConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *SRIOVDevicePluginConfiguration) GetImage() *string {
	return c.Image
}

func (c *SRIOVDevicePluginConfiguration) GetHelmChart() *string {
	return c.HelmChart
}

type OVSCNIConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Image overrides the container image used by the OVS CNI
	// +optional
	Image Image `json:"image,omitempty"`

	// HelmChart overrides the helm chart used by the OVS CNI
	// The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid
	// OCI registry or a web-based repository.
	// +optional
	HelmChart HelmChart `json:"helmChart,omitempty"`
}

func (c *OVSCNIConfiguration) Name() string {
	return OVSCNIName
}

func (c *OVSCNIConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *OVSCNIConfiguration) GetImage() *string {
	return c.Image
}

func (c *OVSCNIConfiguration) GetHelmChart() *string {
	return c.HelmChart
}

type SFCControllerConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Image overrides the container image used by the SFC Controller
	// +optional
	Image Image `json:"image,omitempty"`

	// HelmChart overrides the helm chart used by the SFC Controller
	// The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid
	// OCI registry or a web-based repository.
	// +optional
	HelmChart HelmChart `json:"helmChart,omitempty"`
}

func (c *SFCControllerConfiguration) Name() string {
	return SFCControllerName
}

func (c *SFCControllerConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *SFCControllerConfiguration) GetImage() *string {
	return c.Image
}

func (c *SFCControllerConfiguration) GetHelmChart() *string {
	return c.HelmChart
}

type OVSHelperConfiguration struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Image overrides the container image used by the OVS Helper
	// +optional
	Image Image `json:"image,omitempty"`

	// HelmChart overrides the helm chart used by the OVS Helper
	// The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid
	// OCI registry or a web-based repository.
	// +optional
	HelmChart HelmChart `json:"helmChart,omitempty"`
}

func (c *OVSHelperConfiguration) Name() string {
	return OVSHelperName
}

func (c *OVSHelperConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

func (c *OVSHelperConfiguration) GetImage() *string {
	return c.Image
}

func (c *OVSHelperConfiguration) GetHelmChart() *string {
	return c.HelmChart
}
