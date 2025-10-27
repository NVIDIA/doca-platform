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

// DPUDiscoveryGroupVersionKind is the GroupVersionKind of the DPUDiscovery object
var DPUDiscoveryGroupVersionKind = GroupVersion.WithKind(DPUDiscoveryKind)

const (
	// DPUDiscoveryKind is the kind of the DPUDiscovery object
	DPUDiscoveryKind = "DPUDiscovery"
)

// DPUDiscovery is the custom resource for DPU discovery
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Last Scan",type="date",JSONPath=".status.lastScanTime"
// +kubebuilder:printcolumn:name="Found DPUs",type="integer",JSONPath=".status.foundDPUs"

type DPUDiscovery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUDiscoverySpec   `json:"spec,omitempty"`
	Status DPUDiscoveryStatus `json:"status,omitempty"`
}

// DPUDiscoverySpec defines the desired state of DPUDiscovery
type DPUDiscoverySpec struct {
	// IPRange defines the range of IP addresses to scan
	IPRangeSpec IPRangeValidationSpec `json:"ipRangeSpec"`
	// ScanInterval defines how often to perform the scan
	// +kubebuilder:default="1h"
	ScanInterval metav1.Duration `json:"scanInterval,omitempty"`
	// Workers defines the number of workers to use for the scan (default 1 worker for each 255 IPs in the range)
	// +optional
	Workers *int `json:"workers,omitempty"`
}

// DPUDiscoveryStatus defines the observed state of DPUDiscovery
type DPUDiscoveryStatus struct {
	// LastScanTime is the timestamp of the last successful scan
	LastScanTime *metav1.Time `json:"lastScanTime,omitempty"`
	// FoundDPUs is the list of discovered DPU BMC IPs
	FoundDPUs int `json:"foundDPUs,omitempty"`
}

// IPRange represents a range of IP addresses to scan
type IPRange struct {
	// StartIP is the starting IP address of the range
	// +kubebuilder:validation:Pattern=`^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
	// +kubebuilder:validation:XValidation:rule="size(self) > 0",message="StartIP cannot be empty"
	// +required

	StartIP string `json:"startIP"`

	// EndIP is the ending IP address of the range
	// +kubebuilder:validation:Pattern=`^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
	// +kubebuilder:validation:XValidation:rule="size(self) > 0",message="EndIP cannot be empty"
	// +required

	EndIP string `json:"endIP"`

	// +optional
	// Port defines the port to on which BMC is listening
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=443
	Port *uint32 `json:"port,omitempty"`
}

// IPRangeValidationSpec defines the desired state of IPRangeValidation
// IPRange defines the IP range to validate
// +kubebuilder:validation:XValidation:rule="size(self.ipRange.startIP) > 0 && size(self.ipRange.endIP) > 0",message="Both startIP and endIP must be provided"
// +kubebuilder:validation:XValidation:rule="self.ipRange.startIP != '0.0.0.0' && self.ipRange.endIP != '0.0.0.0'",message="IP addresses cannot be 0.0.0.0"
// +kubebuilder:validation:XValidation:rule="!self.ipRange.startIP.contains(':') && !self.ipRange.endIP.contains(':')",message="Only IPv4 addresses are supported (IPv6 not allowed)"
// +required
type IPRangeValidationSpec struct {
	IPRange IPRange `json:"ipRange"`
}

// +kubebuilder:object:root=true

// DPUDiscoveryList contains a list of DPUDiscovery types
type DPUDiscoveryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUDiscovery `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUDiscovery{}, &DPUDiscoveryList{})
}
