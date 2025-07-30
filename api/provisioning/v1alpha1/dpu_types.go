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
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DPUKind is the kind of the DPU object
	DPUKind = "DPU"
)

// DPUGroupVersionKind is the GroupVersionKind of the DPU object
var DPUGroupVersionKind = GroupVersion.WithKind(DPUKind)

// DPUPhase describes current state of DPU.
// Only one of the following state may be specified.
// Default is Initializing.
// +kubebuilder:validation:Enum="Initializing";"Node Effect";"Pending";"Config FW Parameters";"Prepare BFB";"OS Installing";"DPU Cluster Config";"Host Network Configuration";"Ready";"Error";"Deleting";"Rebooting";"Initialize Interface";"Checking Host Reboot Required"
type DPUPhase string

// These are the valid statuses of DPU.
const (
	DPUFinalizer = "provisioning.dpu.nvidia.com/dpu-protection"

	// DPUInitializing is the first phase after the DPU is created.
	DPUInitializing DPUPhase = "Initializing"
	// DPUNodeEffect means the controller will handle the node effect provided by the user.
	DPUNodeEffect DPUPhase = "Node Effect"
	// DPUPending means the controller is waiting for the BFB to be ready.
	DPUPending DPUPhase = "Pending"
	// DPUPrepareBFB means the controller is preparing the BFB and bf.cfg to be installed to DPU
	DPUPrepareBFB DPUPhase = "Prepare BFB"
	// DPUConfigFWParameters means the controller will manipulate DPU firmware, e.g., set DPU mode, check firmware version
	DPUConfigFWParameters DPUPhase = "Config FW Parameters"
	// DPUInitializeInterface means the controller will intitialize the interface used to provision the DPUs, e.g., create the DMS pod, set up RedFish account.
	DPUInitializeInterface DPUPhase = "Initialize Interface"
	// DPUOSInstalling means the controller will provision the DPU through the DMS gNOI interface.
	DPUOSInstalling DPUPhase = "OS Installing"
	// DPUClusterConfig  means the node configuration and Kubernetes Node join procedure are in progress .
	DPUClusterConfig DPUPhase = "DPU Cluster Config"
	// DPUHostNetworkConfiguration means the host network configuration is running.
	DPUHostNetworkConfiguration DPUPhase = "Host Network Configuration"
	// DPUReady means the DPU is ready to use.
	DPUReady DPUPhase = "Ready"
	// DPUError means error occurred.
	DPUError DPUPhase = "Error"
	// DPUDeleting means the DPU CR will be deleted, controller will do some cleanup works.
	DPUDeleting DPUPhase = "Deleting"
	// DPURebooting means the host of DPU is rebooting.
	DPURebooting DPUPhase = "Rebooting"
	// DPUCheckingHostRebootNeed means the checking of whether the host required a reboot.
	DPUCheckingHostRebootNeed DPUPhase = "Checking Host Reboot Required"
)

type DPUConditionType string

const (
	DPUCondInitialized           DPUConditionType = "Initialized"
	DPUCondPending               DPUConditionType = "Pending"
	DPUCondBFBReady              DPUConditionType = "BFBReady"
	DPUCondNodeEffectReady       DPUConditionType = "NodeEffectReady"
	DPUCondBFBPrepared           DPUConditionType = "BFBPrepared"
	DPUCondInterfaceInitialized  DPUConditionType = "InterfaceInitialized"
	DPUCondFWConfigured          DPUConditionType = "FWConfigured"
	DPUCondOSInstalled           DPUConditionType = "OSInstalled"
	DPUCondCheckedHostRebootNeed DPUConditionType = "CheckedHostRebootNeed"
	DPUCondRebooted              DPUConditionType = "Rebooted"
	DPUCondHostNetworkReady      DPUConditionType = "HostNetworkReady"
	DPUCondDPUClusterReady       DPUConditionType = "DPUClusterReady"
	DPUCondDeleting              DPUConditionType = "Deleting"
	DPUCondReady                 DPUConditionType = "Ready"
	DPUCondError                 DPUConditionType = "Error"
)

type DPUConditionReason string

const (
	// DPUCondReasonModeUpdate is the reason for updating the DPU mode in gNOI interface
	DPUCondReasonModeUpdate DPUConditionReason = "SwitchFromNicMode"
)

type DPUInstallInterfaceType string

const (
	InstallViaGNOI    DPUInstallInterfaceType = "gNOI"
	InstallViaRedFish DPUInstallInterfaceType = "redfish"
	InstallViaMock    DPUInstallInterfaceType = "mock"
)

func (ct DPUConditionType) String() string {
	return string(ct)
}

var _ conditions.GetSet = &DPU{}

// GetConditions returns the conditions of the DPUService.
func (s *DPU) GetConditions() []metav1.Condition {
	return s.Status.Conditions
}

// SetConditions sets the conditions of the DPUService.
func (s *DPU) SetConditions(conditions []metav1.Condition) {
	s.Status.Conditions = conditions
}

type K8sCluster struct {
	// Name is the name of the DPUs Kubernetes cluster
	// +kubebuilder:validation:XValidation:rule="self==oldSelf", message="Value is immutable"
	// +optional
	Name string `json:"name,omitempty"`
	// Namespace is the tenants namespace name where the Kubernetes cluster will be deployed
	// +kubebuilder:validation:XValidation:rule="self==oldSelf", message="Value is immutable"
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// NodeLabels define the labels that will be added to the nodes.
	// +optional
	NodeLabels map[string]string `json:"nodeLabels,omitempty"`
}

// DPUSpec defines the desired state of DPU
type DPUSpec struct {
	// Specifies the DPUNode this DPU belongs to
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="Value is immutable"
	// +required
	DPUNodeName string `json:"dpuNodeName"`

	// Specifies the name of the DPUDevice this DPU is associated with
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="Value is immutable"
	// +kubebuilder:validation:MinLength=1
	// +required
	DPUDeviceName string `json:"dpuDeviceName"`

	// Specifies name of the bfb CR to use for this DPU
	// +required
	BFB string `json:"bfb"`

	// The serial number of the DPU
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="Value is immutable"
	// +kubebuilder:validation:MinLength=1
	// +required
	SerialNumber string `json:"serialNumber"`

	// The PCI device related DPU
	// Example: "0000-03-00", "03-00"
	// +kubebuilder:validation:Pattern=`^([0-9a-fA-F]{4}[-])?[0-9a-fA-F]{2}[-][0-9a-fA-F]{2}$`
	// +optional
	PCIAddress *string `json:"pciAddress,omitempty"`

	// Specifies how changes to the DPU should affect the Node
	// +kubebuilder:default={drain: true}
	// +optional
	NodeEffect *NodeEffect `json:"nodeEffect,omitempty"`

	// Specifies details on the K8S cluster to join
	// +optional
	Cluster K8sCluster `json:"cluster,omitempty"`

	// DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU.
	// +optional
	DPUFlavor string `json:"dpuFlavor,omitempty"`

	// BMCIP is the ip address of the DPU BMC
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="Value is immutable"
	BMCIP string `json:"bmcIP,omitempty"`
}

// DPUStatus defines the observed state of DPU
type DPUStatus struct {
	// The current state of DPU.
	// +kubebuilder:default=Initializing
	// +required
	Phase DPUPhase `json:"phase"`

	// +optional
	Conditions []metav1.Condition `json:"conditions"`

	// BFBFile is the path to the BFB file
	// +optional
	BFBFile string `json:"bfbFile,omitempty"`

	// BFCFGFile is the path to the bf.cfg
	// +optional
	BFCFGFile string `json:"bfCFGFile,omitempty"`

	// bfb version of this DPU
	// +optional
	BFBVersion string `json:"bfbVersion,omitempty"`

	// DPF version used to install this DPU
	// +kubebuilder:validation:XValidation:rule="self==oldSelf", message="Value is immutable"
	// +optional
	DPFVersion *string `json:"dpfVersion,omitempty"`

	// pci device information of this DPU
	// +optional
	PCIDevice string `json:"pciDevice,omitempty"`

	// whether require reset of DPU
	// +optional
	RequiredReset *bool `json:"requiredReset,omitempty"`

	// the firmware information of DPU
	// +optional
	Firmware Firmware `json:"firmware,omitempty"`

	// The DPU node's IP addresses
	// +optional
	Addresses []corev1.NodeAddress `json:"addresses,omitempty"`

	// the name of the interface which will be used to install the bfb image,
	// and communicate with DPU, can be one of gNOI,redfish
	// +kubebuilder:validation:Enum=gNOI;redfish
	// +optional
	DPUInstallInterface *string `json:"dpuInstallInterface,omitempty"`
}

type Firmware struct {
	// BMC is the used BMC firmware version
	BMC string `json:"bmc,omitempty"`
	// NIC is the used NIC firmware version
	NIC string `json:"nic,omitempty"`
	// UEFI is the used UEFI firmware version
	UEFI string `json:"uefi,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="phase of the cluster"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPU is the Schema for the dpus API
type DPU struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec DPUSpec `json:"spec,omitempty"`

	// +kubebuilder:default={phase: Initializing}
	// +optional
	Status DPUStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DPUList contains a list of DPU
type DPUList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPU `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPU{}, &DPUList{})
}
