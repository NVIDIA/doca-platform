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

import (
	nicconfigv1alpha1 "github.com/Mellanox/nic-configuration-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DPUFlavorKind is the kind of the DPUFlavor object
	DPUFlavorKind = "DPUFlavor"
)

// DPUFlavorGroupVersionKind is the GroupVersionKind of the DPUFlavor object
var DPUFlavorGroupVersionKind = GroupVersion.WithKind(DPUFlavorKind)

// DPUFlavorSpec defines the content of DPUFlavor
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="DPUFlavor spec is immutable"
type DPUFlavorSpec struct {
	// Grub contains the grub configuration for the DPUFlavor.
	// +optional
	Grub DPUFlavorGrub `json:"grub,omitempty"`
	// Sysctl contains the sysctl configuration for the DPUFlavor.
	// +optional
	Sysctl DPUFLavorSysctl `json:"sysctl,omitempty"`
	// NVConfig contains the device-specific configuration (firmware settings, device parameters).
	// Each entry specifies a device (wildcard '*', or port identifiers 'p0'/'P0'/'p1'/'P1') and its parameters.
	// If device is '*' or unspecified (defaults to '*'), it applies to all devices and must be the only entry.
	// Each device (including unspecified as '*') must be unique across all nvconfig entries (case-insensitive).
	// Validation enforces: device enum values, parameter format (KEY=VALUE), case-insensitive uniqueness, and size limits.
	// +kubebuilder:validation:MaxItems=3
	// +kubebuilder:validation:XValidation:rule="size(self) == 0 || !self.exists(x, has(x.device) && x.device == '*') || size(self) == 1",message="when device is '*', it must be the only nvconfig entry"
	// +kubebuilder:validation:XValidation:rule="size(self) == 0 || !self.exists(x, !has(x.device)) || size(self) == 1",message="when device is unspecified (defaults to '*'), it must be the only nvconfig entry"
	// +kubebuilder:validation:XValidation:rule="self.all(p1, self.exists_one(p2, (has(p1.device) ? p1.device.lowerAscii() : '*') == (has(p2.device) ? p2.device.lowerAscii() : '*')))",message="each nvconfig.device (including unspecified as '*') must be unique (case-insensitive)"
	// +listType=atomic
	// +optional
	NVConfig []NVConfig `json:"nvconfig,omitempty"`
	// OVS contains the OVS configuration for the DPUFlavor.
	// +optional
	OVS DPUFlavorOVS `json:"ovs,omitempty"`
	// BFCfgParameters are the parameters to be set in the bf.cfg file.
	// +optional
	BFCfgParameters []string `json:"bfcfgParameters,omitempty"`
	// ConfigFiles are the files to be written on the DPU.
	// +optional
	ConfigFiles []ConfigFile `json:"configFiles,omitempty"`
	// ContainerdConfig contains the configuration for containerd.
	// +optional
	ContainerdConfig ContainerdConfig `json:"containerdConfig,omitempty"`
	// DPUResources indicates the minimum amount of resources needed for a BFB with that flavor to be installed on a
	// DPU. Using this field, the controller can understand if that flavor can be installed on a particular DPU. It
	// should be set to the total amount of resources the system needs + the resources that should be made available for
	// DPUServices to consume.
	// +optional
	DPUResources corev1.ResourceList `json:"dpuResources,omitempty"`
	// SystemReservedResources indicates the resources that are consumed by the system (OS, OVS, DPF system etc) and are
	// not made available for DPUServices to consume. DPUServices can consume the difference between DPUResources and
	// SystemReservedResources. This field must not be specified if dpuResources are not specified.
	// +optional
	SystemReservedResources corev1.ResourceList `json:"systemReservedResources,omitempty"`

	// DpuMode is deprecated and no longer used by provisioning workflows.
	// Deployment mode is sourced from DPFOperatorConfig and exposed on DPU.status.deploymentMode.
	// +optional
	DpuMode DpuModeType `json:"dpuMode,omitempty"`

	// HostNetworkInterfaceConfigs contains the configuration for the host-side network interfaces.
	// +optional
	HostNetworkInterfaceConfigs []NetworkInterfaceConfig `json:"hostNetworkInterfaceConfigs,omitempty"`

	// EWNicConfigurations contains the configuration for the E/W NICs.
	// +optional
	EWNicConfigurations *NicConfiguration `json:"ewNicConfigurations,omitempty"`
}

// NicConfiguration is a set of configurations for the NICs
// +kubebuilder:validation:XValidation:rule="!(has(self.spectrumXOptimized) && self.spectrumXOptimized.enabled) || (self.linkType == 'Ethernet' && self.numVfs == 1)",message="spectrumXOptimized can be enabled only when linkType=='Ethernet' and numVfs==1"
// +kubebuilder:validation:XValidation:rule="!(has(self.spectrumXOptimized) && self.spectrumXOptimized.enabled) || !has(self.rawNvConfig) || size(self.rawNvConfig) == 0",message="when spectrumXOptimized is enabled, rawNvConfig must be empty"
type NicConfiguration struct {
	// Number of VFs to be configured
	// +required
	NumVfs int `json:"numVfs"`
	// LinkType to be configured, Ethernet|Infiniband
	// +kubebuilder:validation:Enum=Ethernet;Infiniband
	// +required
	LinkType nicconfigv1alpha1.LinkTypeEnum `json:"linkType"`
	// Spectrum-X optimization settings. Works only with linkType==Ethernet && numVfs==0. Other optimizations must be skipped or disabled. RawNvConfig must be empty.
	SpectrumXOptimized *nicconfigv1alpha1.SpectrumXOptimizedSpec `json:"spectrumXOptimized,omitempty"`
	// List of arbitrary nv config parameters
	RawNvConfig []nicconfigv1alpha1.NvConfigParam `json:"rawNvConfig,omitempty"`
}

type DPUFlavorGrub struct {
	// KernelParameters are the kernel parameters to be set in the grub configuration.
	// +optional
	KernelParameters []string `json:"kernelParameters,omitempty"`
}

type DPUFLavorSysctl struct {
	// Parameters are the sysctl parameters to be set.
	// +optional
	Parameters []string `json:"parameters,omitempty"`
}

type NVConfig struct {
	// Device is the device to which the configuration applies. If not specified, the configuration applies to all.
	// Supported values: "*" (wildcard for all devices), "p0"/"P0" (port 0), "p1"/"P1" (port 1). Case-insensitive.
	// +kubebuilder:validation:Enum={"*","p0","p1","P0","P1"}
	// +optional
	Device *string `json:"device,omitempty"`
	// Parameters are the parameters to be set for the device.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:Pattern=`^[^=\s]+=[^\s]*$`
	// +kubebuilder:validation:items:MaxLength=200
	// +listType=atomic
	// +optional
	Parameters []string `json:"parameters,omitempty"`
}

type DPUFlavorOVS struct {
	// RawConfigScript is the raw configuration script for OVS.
	// +optional
	RawConfigScript string `json:"rawConfigScript,omitempty"`
}

// DpuModeType defines the mode of the DPU
// +kubebuilder:validation:Enum=dpu;zero-trust;nic
type DpuModeType string

const (
	DpuMode DpuModeType = "dpu"
	NicMode DpuModeType = "nic"
	// ZeroTrustMode is deprecated and kept for backward compatibility with DPUFlavor.spec.dpuMode.
	// Deprecated: DPUFlavor.spec.dpuMode is deprecated; use DPFOperatorConfig.spec.deploymentMode.
	ZeroTrustMode DpuModeType = "zero-trust"
)

// DPUFlavorFileOp defines the operation to be performed on the file
// +kubebuilder:validation:Enum=override;append
type DPUFlavorFileOp string

const (
	FileOverride DPUFlavorFileOp = "override"
	FileAppend   DPUFlavorFileOp = "append"
)

type ConfigFile struct {
	// Path is the path of the file to be written.
	// +optional
	Path string `json:"path,omitempty"`
	// Operation is the operation to be performed on the file.
	// +optional
	Operation DPUFlavorFileOp `json:"operation,omitempty"`
	// Raw is the raw content of the file.
	// +optional
	Raw string `json:"raw,omitempty"`
	// Permissions are the permissions to be set on the file.
	// +optional
	Permissions string `json:"permissions,omitempty"`
}

type ContainerdConfig struct {
	// RegistryEndpoint is the endpoint of the container registry.
	// +optional
	RegistryEndpoint string `json:"registryEndpoint,omitempty"`
}

// NetworkInterfaceConfig defines the configuration for a network interface
type NetworkInterfaceConfig struct {
	// MTU is the MTU value to be set on the network interface.
	// +kubebuilder:validation:Minimum=1280
	// +kubebuilder:validation:Maximum=9216
	// +optional
	MTU *int32 `json:"mtu,omitempty"`

	// DHCP is the DHCP configuration for the network interface.
	// +optional
	DHCP *bool `json:"dhcp,omitempty"`

	// PortNumber identifies which port this configuration applies to.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +required
	PortNumber int32 `json:"portNumber"`

	// NVConfig contains port-specific configuration for this network interface.
	// This configuration is applied in addition to the global NVConfig settings in DPUFlavorSpec.
	// Both global and per-interface NVConfig settings can coexist without collision.
	// +optional
	NVConfig *NVConfig `json:"nvconfig,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// DPUFlavor is the Schema for the dpuflavors API
type DPUFlavor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec DPUFlavorSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// DPUFlavorList contains a list of DPUFlavor
type DPUFlavorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUFlavor `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUFlavor{}, &DPUFlavorList{})
}
