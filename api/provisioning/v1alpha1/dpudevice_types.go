/*
Copyright 2025 NVIDIA

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DPUDeviceKind is the kind of the DPUDevice object
	DPUDeviceKind = "DPUDevice"
)

// DPUDeviceGroupVersionKind is the GroupVersionKind of the DPUDevice object
var DPUDeviceGroupVersionKind = GroupVersion.WithKind(DPUDeviceKind)

// DPUDeviceSpec defines the content of DPUDevice
type DPUDeviceSpec struct {
	// PSID is the Product Serial ID of the device.
	// It's used to track the device's lifecycle and for inventory management.
	// This value is immutable and should not be changed once set.
	// Example: "MT_0001234567", "MT25066004C7"
	// +kubebuilder:validation:Pattern=`^MT_?[A-Z0-9]+$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="PSID is immutable"
	// +optional
	PSID *string `json:"psid,omitempty"`

	// SerialNumber is the serial number of the device.
	// It's used to track the device's lifecycle and for inventory management.
	// This value is immutable and should not be changed once set.
	// Example: "MT_0001234567", "MT25066004C7"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Serial Number is immutable"
	// +required
	SerialNumber string `json:"serialNumber,omitempty"`

	// OPN is the Ordering Part Number of the device.
	// It's used to track the device's compatibility with different software versions.
	// This value is immutable and should not be changed once set.
	// Example: "900-9D3B4-00SV-EA0"
	// +kubebuilder:validation:Pattern=`^\d{3}-[A-Z0-9]{5}-[A-Z0-9]{4}-[A-Z0-9]{3}$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="OPN is immutable"
	// +optional
	OPN *string `json:"opn,omitempty"`

	// BMCIP is the IP address of the BMC (Base Management Controller) on the device.
	// This is used for remote management and monitoring of the device.
	// This value is immutable and should not be changed once set.
	// Example: "10.1.2.3"
	// +kubebuilder:validation:Format=ipv4
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="BMC IP is immutable"
	// +optional
	BMCIP *string `json:"bmcIp,omitempty"`

	// NumberOfPFs is the number of PFs on the device.
	// This value is immutable and should not be changed once set.
	// Example: 1
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Number of PFs is immutable"
	// +optional
	NumberOfPFs *int `json:"numberOfPFs,omitempty"`

	// PF0Name is the name of the PF0 on the device.
	// This value is immutable and should not be changed once set.
	// Example: "eth0"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="PF0 Name is immutable"
	// +optional
	PF0Name *string `json:"pf0Name,omitempty"`
}

type DPUDeviceStatus struct {
	// PCIAddress is the PCI address of the device in the host system.
	// Example: "0000-03-00", "03-00"
	// +optional
	PCIAddress *string `json:"pciAddress,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// TODO: Add e2e test when we add scenarios that include creating our own DPUNode and DPUDevice objects
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63", message="name length can't be bigger than 63 chars"

// DPUDevice is the Schema for the dpudevices API
type DPUDevice struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUDeviceSpec   `json:"spec,omitempty"`
	Status DPUDeviceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DPUDeviceList contains a list of DPUDevices
type DPUDeviceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUDevice `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUDevice{}, &DPUDeviceList{})
}
