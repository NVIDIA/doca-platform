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
	"fmt"

	"github.com/nvidia/doca-platform/pkg/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DPUDeviceKind is the kind of the DPUDevice object
	DPUDeviceKind = "DPUDevice"

	// DPUDeviceFinalizer is the finalizer used to prevent DpuDevice deletion while DPU is using it
	DPUDeviceFinalizer = "provisioning.dpu.nvidia.com/dpudevice-protection"
)

// DPUDeviceGroupVersionKind is the GroupVersionKind of the DPUDevice object
var DPUDeviceGroupVersionKind = GroupVersion.WithKind(DPUDeviceKind)

// DPUDevice condition types
const (
	// ConditionDpuDeviceDiscovered indicates that the DPU has been discovered
	ConditionDpuDeviceDiscovered conditions.ConditionType = "Discovered"
	// ConditionDpuDeviceNodeAttached indicates that the DPU is attached to a node
	ConditionDpuDeviceNodeAttached conditions.ConditionType = "NodeAttached"
	// ConditionDpuDeviceResettingBMC indicates that the BMC is being reset to factory defaults
	ConditionDpuDeviceResettingBMC conditions.ConditionType = "ResettingBMC"
	// ConditionDpuDeviceInitialized indicates that the DPU interface has been initialized
	ConditionDpuDeviceInitialized conditions.ConditionType = "Initialized"
	// ConditionDpuDeviceError indicates that the DPUDevice has an error
	ConditionDpuDeviceError conditions.ConditionType = "Error"
	// ConditionDpuDeviceReady indicates that the DPUDevice is ready
	ConditionDpuDeviceReady conditions.ConditionType = "Ready"
)

var (
	// DPUDeviceConditions are conditions that can be set on a DPUDevice object.
	DPUDeviceConditions = []conditions.ConditionType{
		ConditionDpuDeviceDiscovered,
		ConditionDpuDeviceResettingBMC,
		ConditionDpuDeviceNodeAttached,
		ConditionDpuDeviceInitialized,
		ConditionDpuDeviceError,
		ConditionDpuDeviceReady,
	}
)

// DPUDeviceSpec defines the content of DPUDevice
type DPUDeviceSpec struct {
	// PSID is the Product Serial ID of the device.
	// It's used to track the device's lifecycle and for inventory management.
	// This value is immutable and should not be changed once set.
	// Example: "MT_0001234567", "MT25066004C7"
	//
	// Deprecated: This field is deprecated and will be removed in a future version. Use status.psid instead.
	// +kubebuilder:validation:Pattern=`^MT_?[A-Z0-9]+$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="PSID is immutable"
	// +optional
	PSID *string `json:"psid,omitempty"`

	// SerialNumber is the serial number of the device.
	// It's used to track the device's lifecycle and for inventory management.
	// This value is immutable and should not be changed once set.
	// Example: "MT_0001234567", "MT25066004C7"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Serial Number is immutable"
	// +kubebuilder:validation:MinLength=1
	// +required
	SerialNumber string `json:"serialNumber,omitempty"`

	// OPN is the Ordering Part Number of the device.
	// It's used to track the device's compatibility with different software versions.
	// This value is immutable and should not be changed once set.
	// Example: "900-9D3B4-00SV-EA0"
	//
	// Deprecated: This field is deprecated and will be removed in a future version. Use status.opn instead.
	// +kubebuilder:validation:Pattern=`^\d{3}-[A-Z0-9]{5}-[A-Z0-9]{4}-[A-Z0-9]{3}$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="OPN is immutable"
	// +optional
	OPN *string `json:"opn,omitempty"`

	// BMCIP is the IP address of the BMC (Base Management Controller) on the device.
	// This is used for remote management and monitoring of the device.
	// This value is immutable and should not be changed once set.
	// Example: "10.1.2.3"
	// +kubebuilder:validation:Format=ipv4
	// +optional
	BMCIP *string `json:"bmcIp,omitempty"`

	// BMCPort is the port number of the BMC (Base Management Controller) on the device.
	// This is used for remote management and monitoring of the device.
	// This value is immutable and should not be changed once set.
	// Example: 443
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="BMCPort is immutable"
	// +kubebuilder:default=443
	// +optional
	BMCPort *uint32 `json:"bmcPort,omitempty"`

	// NumberOfPFs is the number of PFs on the device.
	// This value is immutable and should not be changed once set.
	// Example: 1
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Number of PFs is immutable"
	// +optional
	NumberOfPFs *int `json:"numberOfPFs,omitempty"`

	// NICDeviceCount is the expected number of NIC devices used by dpu-agent provisioning.
	// Valid range is 1 to 8. When unspecified, it defaults to 8.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=8
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="NICDeviceCount is immutable"
	// +optional
	NICDeviceCount *int `json:"nicDeviceCount,omitempty"`

	// PF0Name is the name of the PF0 on the device.
	// This value is immutable and should not be changed once set.
	// Example: "eth0"
	//
	// Deprecated: This field is deprecated and will be removed in a future version. Use status.pf0Name instead.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="PF0 Name is immutable"
	// +optional
	PF0Name *string `json:"pf0Name,omitempty"`
}

type DPUDeviceStatus struct {
	// PSID is the Product Serial ID of the device.
	// It's used to track the device's lifecycle and for inventory management.
	// This value is discovered and should not be changed once set.
	// Example: "MT_0001234567", "MT25066004C7"
	// +kubebuilder:validation:Pattern=`^MT_?[A-Z0-9]+$`
	// +optional
	PSID *string `json:"psid,omitempty"`

	// SerialNumber is the serial number of the device.
	// It's used to track the device's lifecycle and for inventory management.
	// This value is discovered and should not be changed once set.
	// Example: "MT_0001234567", "MT25066004C7"
	// +optional
	SerialNumber *string `json:"serialNumber,omitempty"`

	// OPN is the Ordering Part Number of the device.
	// It's used to track the device's compatibility with different software versions.
	// This value is discovered and should not be changed once set.
	// Example: "900-9D3B4-00SV-EA0"
	// +optional
	OPN *string `json:"opn,omitempty"`

	// BMCIP is the IP address of the BMC (Base Management Controller) on the device.
	// This is used for remote management and monitoring of the device.
	// This value is discovered and should not be changed once set.
	// Example: "10.1.2.3"
	// +kubebuilder:validation:Format=ipv4
	// +optional
	BMCIP *string `json:"bmcIp,omitempty"`

	// BMCPort is the port number of the BMC (Base Management Controller) on the device.
	// This is used for remote management and monitoring of the device.
	// This value is immutable and should not be changed once set.
	// Example: 443
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=443
	// +optional
	BMCPort *uint32 `json:"bmcPort,omitempty"`

	// PCIAddress is the PCI address of the device in the host system.
	// Example: "0000-03-00", "03-00"
	// +optional
	PCIAddress *string `json:"pciAddress,omitempty"`

	// PF0Name is the name of the PF0 on the device.
	// Example: "eth0"
	// +optional
	PF0Name *string `json:"pf0Name,omitempty"`

	// PF0MAC is the MAC address of the PF0 on the device.
	// Example: "00:00:00:00:00:00"
	// +kubebuilder:validation:Pattern=`^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$`
	// +optional
	PF0MAC *string `json:"pf0Mac,omitempty"`

	// DPUType is the type of the DPU.
	// +kubebuilder:validation:Enum=Unknown;BlueField2;BlueField3;BlueField4
	// +kubebuilder:default=Unknown
	// +optional
	DPUType DPUType `json:"dpuType,omitempty"`

	// DPUMode is the mode of the DPU.
	// +kubebuilder:validation:Enum=dpu;nic
	// +kubebuilder:default=dpu
	// +optional
	DPUMode DpuModeType `json:"dpuMode,omitempty"`

	// SecureBoot indicates the current UEFI Secure Boot state.
	// +optional
	SecureBoot *SecureBootStatus `json:"secureBoot,omitempty"`

	// +optional
	Conditions []metav1.Condition `json:"conditions"`
}

// SecureBootStatus represents the UEFI Secure Boot configuration status on the DPU.
type SecureBootStatus struct {
	// Enabled indicates whether UEFI Secure Boot is currently enabled on the DPU.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

type DPUType string

const (
	DPUTypeUnknown    DPUType = "Unknown"
	DPUTypeBlueField2 DPUType = "BlueField2"
	DPUTypeBlueField3 DPUType = "BlueField3"
	DPUTypeBlueField4 DPUType = "BlueField4"
)

var _ conditions.GetSet = &DPUDevice{}

// GetConditions returns the conditions of the DPUDevice.
func (d *DPUDevice) GetConditions() []metav1.Condition {
	if d.Status.Conditions == nil {
		return []metav1.Condition{}
	}
	return d.Status.Conditions
}

// SetConditions sets the conditions of the DPUDevice.
func (d *DPUDevice) SetConditions(conditions []metav1.Condition) {
	d.Status.Conditions = conditions
}

func (d *DPUDevice) BMCAddress() string {
	if d.Status.BMCIP == nil || d.Status.BMCPort == nil {
		return ""
	}

	return fmt.Sprintf("https://%s:%d", *d.Status.BMCIP, *d.Status.BMCPort)
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// TODO: Add e2e test when we add scenarios that include creating our own DPUNode and DPUDevice objects
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63", message="name length can't be bigger than 63 chars"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='ConditionDpuDeviceReady')].status`

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
