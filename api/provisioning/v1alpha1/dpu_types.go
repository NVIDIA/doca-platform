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
// +kubebuilder:validation:Enum="Initializing";"Node Effect";"Pending";"Config FW Parameters";"Prepare BFB";"OS Installing";"DPU Config";"DPU Cluster Config";"Host Network Configuration";"Ready";"Error";"Deleting";"Rebooting";"Perform ARM Force Restart";"Initialize Interface";"Node Effect Removal";"Checking Host Reboot Required"
type DPUPhase string

// These are the valid statuses of DPU.
const (
	DPUFinalizer = "provisioning.dpu.nvidia.com/dpu-protection"

	// AnnotationArmRestartTracker stores ARM restart tracking state for Secure Boot configuration
	AnnotationArmRestartTracker = "provisioning.dpu.nvidia.com/arm-restart-tracker"

	// DPUInitializing is the first phase after the DPU is created.
	DPUInitializing DPUPhase = "Initializing"
	// DPUNodeEffect means the controller will handle the node effect provided by the user.
	DPUNodeEffect DPUPhase = "Node Effect"
	// DPUPending means the controller is waiting for the BFB to be ready.
	DPUPending DPUPhase = "Pending"
	// DPUPrepareBFB means the controller is preparing the BFB and bf.cfg to be installed to DPU
	DPUPrepareBFB DPUPhase = "Prepare BFB"
	// DPUConfig means the DPU agent will configure the DPU
	DPUConfig DPUPhase = "DPU Config"
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
	// DPUNodeEffectRemoval means the controller will remove the node effect from the DPU.
	DPUNodeEffectRemoval DPUPhase = "Node Effect Removal"
	// DPUReady means the DPU is ready to use.
	DPUReady DPUPhase = "Ready"
	// DPUError means error occurred.
	DPUError DPUPhase = "Error"
	// DPUDeleting means the DPU CR will be deleted, controller will do some cleanup works.
	DPUDeleting DPUPhase = "Deleting"
	// DPURebooting means the host of DPU is rebooting.
	DPURebooting DPUPhase = "Rebooting"
	// DPUPerformArmForceRestart means ARM ForceRestart operations are in progress for Secure Boot configuration.
	DPUPerformArmForceRestart DPUPhase = "Perform ARM Force Restart"
)

type DPUConditionType string

const (
	DPUCondInitialized            DPUConditionType = "Initialized"
	DPUCondPending                DPUConditionType = "Pending"
	DPUCondBFBReady               DPUConditionType = "BFBReady"
	DPUCondBlueFieldSoftwareReady DPUConditionType = "BlueFieldSoftwareReady"
	DPUCondDPUFlavorExists        DPUConditionType = "DPUFlavorExists"
	DPUCondNodeEffectReady        DPUConditionType = "NodeEffectReady"
	DPUCondBFBPrepared            DPUConditionType = "BFBPrepared"
	DPUCondInterfaceInitialized   DPUConditionType = "InterfaceInitialized"
	DPUCondFWConfigured           DPUConditionType = "FWConfigured"
	DPUCondBFBTransferred         DPUConditionType = "BFBTransferred"
	DPUCondOSInstalled            DPUConditionType = "OSInstalled"
	DPUConditionHostPowerCycle    DPUConditionType = "HostPowerCycle"
	DPUCondRebooted               DPUConditionType = "Rebooted"
	DPUCondHostNetworkReady       DPUConditionType = "HostNetworkReady"
	DPUCondDPUClusterReady        DPUConditionType = "DPUClusterReady"
	DPUCondNodeEffectRemoved      DPUConditionType = "NodeEffectRemoved"
	DPUCondDeleting               DPUConditionType = "Deleting"
	DPUCondReady                  DPUConditionType = "Ready"
	DPUCondError                  DPUConditionType = "Error"
	DPUCondArmForceRestarted      DPUConditionType = "ArmForceRestarted"
)

// DPUOperationalConditionType represents operational readiness condition types
type DPUOperationalConditionType string

const (
	// DPUOperationalCondReady is the summary condition indicating overall operational readiness
	DPUOperationalCondReady DPUOperationalConditionType = "OperationalReady"
	// DPUOperationalCondNodeProblemsReady indicates whether the DPU node has problems reported by node-problem-detector
	DPUOperationalCondNodeProblemsReady DPUOperationalConditionType = "NodeProblemsReady"
	// DPUOperationalCondDPUServiceCriticalPodsReady indicates whether critical DPU service pods are ready
	DPUOperationalCondDPUServiceCriticalPodsReady DPUOperationalConditionType = "DPUServiceCriticalPodsReady"
	// DPUOperationalCondDPUServiceNonCriticalPodsReady indicates whether non-critical DPU service pods are ready
	DPUOperationalCondDPUServiceNonCriticalPodsReady DPUOperationalConditionType = "DPUServiceNonCriticalPodsReady"
	// DPUOperationalCondDPUServiceInterfacesReady indicates whether DPU service interfaces are ready
	DPUOperationalCondDPUServiceInterfacesReady DPUOperationalConditionType = "DPUServiceInterfacesReady"
	// DPUOperationalCondDPUServiceChainsReady indicates whether DPU service chains are ready
	DPUOperationalCondDPUServiceChainsReady DPUOperationalConditionType = "DPUServiceChainsReady"
)

type DPUInfoConditionType string

const (
	ReadyForReboot DPUInfoConditionType = "ReadyForReboot"
)

type DPUConditionMessage string

const (
	// DPUCondMessageModeUpdate is the message for updating the DPU mode in gNOI interface
	DPUCondMessageModeUpdate DPUConditionMessage = "DPU switches from NIC mode to DPU mode"
	// DPUCondMessageRebootFinishedForModeUpdate is the message for rebooting the DPU for mode update
	DPUCondMessageRebootFinishedForModeUpdate DPUConditionMessage = "Reboot finished for DPU mode update"
)

// NodeProblemDetectorConditionType represents Kubernetes Node condition types reported
// by node-problem-detector that indicate DPU node health. These conditions are monitored
// on DPUCluster nodes and aggregated into DPU operational readiness.
type NodeProblemDetectorConditionType string

// Keep in sync with deploy/charts/dpu-networking/charts/node-problem-detector/templates/configmap.yaml
const (
	// NPDConditionKernelDeadlock indicates the node is in a kernel deadlock state.
	NPDConditionKernelDeadlock NodeProblemDetectorConditionType = "KernelDeadlock"
	// NPDConditionReadonlyFilesystem indicates the filesystem is read-only.
	NPDConditionReadonlyFilesystem NodeProblemDetectorConditionType = "ReadonlyFilesystem"
	// NPDConditionOVSvSwitchdHealthy indicates the ovs-vswitchd service is running
	NPDConditionOVSvSwitchdHealthy NodeProblemDetectorConditionType = "OVSvSwitchdHealthy"
	// NPDConditionOVSDBHealthy indicates the ovsdb-server service is running
	NPDConditionOVSDBHealthy NodeProblemDetectorConditionType = "OVSDBHealthy"
	// NPDConditionOVSHealthy indicates OVS processes have not been OOM killed recently
	NPDConditionOVSHealthy NodeProblemDetectorConditionType = "OVSHealthy"
	// NPDConditionDPUModeCorrect indicates DPU is in the correct separated mode
	NPDConditionDPUModeCorrect NodeProblemDetectorConditionType = "DPUModeCorrect"
	// NPDConditionUplinkHealthy indicates the physical uplink interface is up
	NPDConditionUplinkHealthy NodeProblemDetectorConditionType = "UplinkHealthy"
	// NPDConditionSRIOVHealthy indicates SR-IOV VF representors are present
	NPDConditionSRIOVHealthy NodeProblemDetectorConditionType = "SRIOVHealthy"
	// NPDConditionMTUConfigured indicates network MTU is correctly configured
	NPDConditionMTUConfigured NodeProblemDetectorConditionType = "MTUConfigured"
)

// GetNodeProblemDetectorConditions returns all expected node-problem-detector condition types
// that should be present on DPUCluster nodes for health monitoring.
func GetNodeProblemDetectorConditions() []string {
	return []string{
		string(NPDConditionKernelDeadlock),
		string(NPDConditionReadonlyFilesystem),
		string(NPDConditionOVSvSwitchdHealthy),
		string(NPDConditionOVSDBHealthy),
		string(NPDConditionOVSHealthy),
		string(NPDConditionDPUModeCorrect),
		string(NPDConditionUplinkHealthy),
		string(NPDConditionSRIOVHealthy),
		string(NPDConditionMTUConfigured),
	}
}

type DPUInstallInterfaceType string

const (
	InstallViaGNOI      DPUInstallInterfaceType = "gNOI"
	InstallViaHostAgent DPUInstallInterfaceType = "hostAgent"
	InstallViaRedFish   DPUInstallInterfaceType = "redfish"
	InstallViaMock      DPUInstallInterfaceType = "mock"
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

	ClusterSpec `json:",inline"`
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

	// Specifies the name of the BlueFieldSoftware CR to use for this DPU
	// +optional
	BlueFieldSoftware string `json:"blueFieldSoftware,omitempty"`

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
	// +kubebuilder:validation:XValidation:rule="self==oldSelf", message="Value is immutable"
	// +kubebuilder:validation:MinLength=1
	// +required
	DPUFlavor string `json:"dpuFlavor"`

	// SecureBoot specifies whether UEFI Secure Boot should be enabled.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="Value is immutable"
	SecureBoot *bool `json:"secureBoot,omitempty"`

	// BMCIP is the ip address of the DPU BMC
	//
	// Deprecated: Use BMCIP from DPUDevice instead.
	// +optional
	BMCIP string `json:"bmcIP,omitempty"`
}

// DPUStatus defines the observed state of DPU
type DPUStatus struct {
	// The current state of DPU.
	// +kubebuilder:default=Initializing
	// +required
	Phase DPUPhase `json:"phase"`

	// Conditions represents the provisioning lifecycle conditions.
	// +optional
	Conditions []metav1.Condition `json:"conditions"`

	// OperationalConditions represents aggregated operational readiness conditions.
	// These conditions reflect the runtime health and readiness of DPU services and node health,
	// separate from the provisioning lifecycle represented by Conditions.
	// +listType=map
	// +listMapKey=type
	// +optional
	OperationalConditions []metav1.Condition `json:"operationalConditions,omitempty"`

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
	// and communicate with DPU, can be one of hostAgent,redfish
	// +kubebuilder:validation:Enum=gNOI;hostAgent;redfish
	// +optional
	DPUInstallInterface *string `json:"dpuInstallInterface,omitempty"`

	// Indicates that node effect was triggered by post-provisioning label changes
	// +optional
	PostProvisioningNodeEffect *bool `json:"postProvisioningNodeEffect,omitempty"`

	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// The type of the DPU
	// +kubebuilder:validation:Enum=Unknown;BlueField2;BlueField3;BlueField4
	// +kubebuilder:default=Unknown
	// +optional
	DPUType DPUType `json:"dpuType,omitempty"`

	// AgentLastStartupTime is the time when the DPU agent was last started. This is copied from agentStatus.lastStartupTime.
	// +optional
	AgentLastStartupTime *metav1.Time `json:"agentLastStartupTime,omitempty"`

	// AgentStatus contains the information reported from inside the DPU
	// +optional
	AgentStatus *AgentStatus `json:"agentStatus,omitempty"`

	// The mode of the DPU
	// +kubebuilder:validation:Enum=dpu;nic
	// +kubebuilder:default=dpu
	// +optional
	DPUMode DpuModeType `json:"dpuMode,omitempty"`

	// SecureBoot indicates the current UEFI Secure Boot state.
	// +optional
	SecureBoot *SecureBootStatus `json:"secureBoot,omitempty"`

	// The task ID of the last task performed on the DPU BMC
	// +optional
	RedfishTaskID *string `json:"redfishTaskId,omitempty"`
}

type Firmware struct {
	// BMC is the used BMC firmware version
	BMC string `json:"bmc,omitempty"`
	// NIC is the used NIC firmware version
	NIC string `json:"nic,omitempty"`
	// UEFI is the used UEFI firmware version
	UEFI string `json:"uefi,omitempty"`
}

// RebootMethodType is the type of reset/reboot required after NVConfig or firmware changes.
// Set by the DPU agent. Values align with NVIDIA BlueField Reset and Reboot Procedures (mlxfwreset levels).
// +kubebuilder:validation:Enum=NoAction;PowerCycle;SystemReboot;SystemLevelReset;FirmwareReset
type RebootMethodType string

const (
	// RebootMethodNoAction indicates no reset or reboot is required.
	RebootMethodNoAction RebootMethodType = "NoAction"
	// RebootMethodPowerCycle indicates a full server power cycle (cold boot) is required.
	RebootMethodPowerCycle RebootMethodType = "PowerCycle"
	// RebootMethodSystemReboot firmware update without full server power cycle.
	RebootMethodSystemReboot RebootMethodType = "SystemReboot"
	// RebootMethodSystemLevelReset firmware configuration changes to take effect.
	RebootMethodSystemLevelReset RebootMethodType = "SystemLevelReset"
	// RebootMethodFirmwareReset driver restart and PCI reset.
	RebootMethodFirmwareReset RebootMethodType = "FirmwareReset"
)

type AgentStatus struct {
	// LastStartupTime is the time when the DPU was last started
	// +optional
	LastStartupTime *metav1.Time `json:"lastStartupTime,omitempty"`

	// InitialBootID is the boot ID of the DPU OS during the first boot
	InitialBootID *string `json:"initialBootID,omitempty"`

	// RebootMethod is the type of reset/reboot set by the DPU agent
	// See enum values in RebootMethodType.
	// No default is set intentionally: nil means "check not run or not applicable"
	// (e.g. legacy flow, or agent has not run the check yet);
	// a non-nil value means the check ran and this is the result.
	// +optional
	RebootMethod *RebootMethodType `json:"rebootMethod,omitempty"`

	// Conditions contains the conditions reported from inside the DPU
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Operational",type="string",JSONPath=`.status.operationalConditions[?(@.type=='OperationalReady')].status`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="phase of the dpu"
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
