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
	// NVConfig contains the configuration for the DPUFlavor.
	// +optional
	NVConfig []DPUFlavorNVConfig `json:"nvconfig,omitempty"`
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

type DPUFlavorNVConfig struct {
	// Device is the device to which the configuration applies. If not specified, the configuration applies to all.
	// +optional
	Device *string `json:"device,omitempty"`
	// Parameters are the parameters to be set for the device.
	// +optional
	Parameters []string `json:"parameters,omitempty"`
	// HostPowerCycleRequired indicates if the host needs to be power cycled after applying the configuration.
	// +optional
	HostPowerCycleRequired *bool `json:"hostPowerCycleRequired,omitempty"`
}

type DPUFlavorOVS struct {
	// RawConfigScript is the raw configuration script for OVS.
	// +optional
	RawConfigScript string `json:"rawConfigScript,omitempty"`
}

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

// +kubebuilder:object:root=true
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
