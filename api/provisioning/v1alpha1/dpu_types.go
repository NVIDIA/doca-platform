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

	// DPUAgentConditionNVConfigApplied is reported when mlxconfig has been applied for the current boot.
	// It may appear on agentStatus.conditions (DPU Config phase) or agentStatus.preInstall.conditions
	// (best-effort Config FW Parameters phase during reprovision).
	DPUAgentConditionNVConfigApplied = "NVConfigApplied"

	// DPUAgentConditionSPIREWorkloadAttestorEnabled is reported on SPIFFE DPUs once the
	// SPIRE Kubernetes workload attestor is configured, which is what lets the SPIRE
	// agent issue SVIDs to pods.
	DPUAgentConditionSPIREWorkloadAttestorEnabled = "SPIREWorkloadAttestorEnabled"
)

// DPUGroupVersionKind is the GroupVersionKind of the DPU object
var DPUGroupVersionKind = GroupVersion.WithKind(DPUKind)

// DPUPhase describes current state of DPU.
// Only one of the following state may be specified.
// Default is Initializing.
// +kubebuilder:validation:Enum="Initializing";"Node Effect";"Pending";"Update Firmware";"Config FW Parameters";"Prepare BFB";"OS Installing";"DPU Config";"DPU Cluster Config";"Host Network Configuration";"Host OS Init Release";"Ready";"Error";"Deleting";"Rebooting";"Perform ARM Force Restart";"Initialize Interface";"Node Effect Removal";"Checking Host Reboot Required"
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
	// DPUUpdateFirmware means the controller will update the DPU firmware for BlueField4.
	DPUUpdateFirmware DPUPhase = "Update Firmware"
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
	// DPUHostOSInitRelease waits for the DPU agent to release host OS init when configured.
	DPUHostOSInitRelease DPUPhase = "Host OS Init Release"
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
	DPUCondDPUFlavorRendered      DPUConditionType = "DPUFlavorRendered"
	DPUCondNodeEffectReady        DPUConditionType = "NodeEffectReady"
	DPUCondBFBPrepared            DPUConditionType = "BFBPrepared"
	DPUCondInterfaceInitialized   DPUConditionType = "InterfaceInitialized"
	DPUCondFWConfigured           DPUConditionType = "FWConfigured"
	DPUCondFwBundleSubmitted      DPUConditionType = "FwBundleSubmitted"
	DPUCondFwBundleUpdated        DPUConditionType = "FwBundleUpdated"
	DPUCondBFBTransferred         DPUConditionType = "BFBTransferred"
	DPUCondIsoTransferred         DPUConditionType = "IsoTransferred"
	DPUCondConfigTransferred      DPUConditionType = "ConfigTransferred"
	DPUCondVirtualMediaInserted   DPUConditionType = "VirtualMediaInserted"
	DPUCondChangeBootTarget       DPUConditionType = "ChangeBootTarget"
	DPUCondChassisReset           DPUConditionType = "ChassisReset"
	DPUCondOSInstalled            DPUConditionType = "OSInstalled"
	DPUCondRebooted               DPUConditionType = "Rebooted"
	DPUCondHostNetworkReady       DPUConditionType = "HostNetworkReady"
	DPUCondDPUClusterReady        DPUConditionType = "DPUClusterReady"
	DPUCondDPUConfig              DPUConditionType = "DPUConfig"
	DPUCondHostOSInitRelease      DPUConditionType = "HostOSInitRelease"
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

// Keep in sync with deploy/charts/dpu-networking/charts/node-problem-detector/templates/configmap.yaml
const (
	// NPDConditionKernelDeadlock indicates the node is in a kernel deadlock state.
	NPDConditionKernelDeadlock corev1.NodeConditionType = "KernelDeadlock"
	// NPDConditionReadonlyFilesystem indicates the filesystem is read-only.
	NPDConditionReadonlyFilesystem corev1.NodeConditionType = "ReadonlyFilesystem"
	// NPDConditionOVSvSwitchdHealthy indicates the ovs-vswitchd service is running
	NPDConditionOVSvSwitchdHealthy corev1.NodeConditionType = "OVSvSwitchdHealthy"
	// NPDConditionOVSDBHealthy indicates the ovsdb-server service is running
	NPDConditionOVSDBHealthy corev1.NodeConditionType = "OVSDBHealthy"
	// NPDConditionOVSHealthy indicates OVS processes have not been OOM killed recently
	NPDConditionOVSHealthy corev1.NodeConditionType = "OVSHealthy"
	// NPDConditionUplinkHealthy indicates the physical uplink interface is up
	NPDConditionUplinkHealthy corev1.NodeConditionType = "UplinkHealthy"
	// NPDConditionPFRepresentorsHealthy indicates host PF representors are present on the DPU eswitch
	NPDConditionPFRepresentorsHealthy corev1.NodeConditionType = "PFRepresentorsHealthy"
	// NPDConditionMTUConfigured indicates network MTU is correctly configured
	NPDConditionMTUConfigured corev1.NodeConditionType = "MTUConfigured"
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
		string(NPDConditionUplinkHealthy),
		string(NPDConditionPFRepresentorsHealthy),
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

// DeploymentMode describes the cluster deployment model for provisioning (zero-trust vs host-trusted).
// This type is intentionally duplicated from operator/v1alpha1.DeploymentMode: the provisioning API
// must not import the operator API group. Keep values and semantics aligned with
// DPFOperatorConfig.spec.deploymentMode.
type DeploymentMode string

const (
	DeploymentModeZeroTrust   DeploymentMode = "zero-trust"
	DeploymentModeHostTrusted DeploymentMode = "host-trusted"
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
// +kubebuilder:validation:XValidation:rule="has(self.bfb) != has(self.blueFieldSoftware)",message="exactly one of bfb or blueFieldSoftware must be set"
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
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	BFB *string `json:"bfb,omitempty"`

	// Specifies the name of the BlueFieldSoftware CR to use for this DPU
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	BlueFieldSoftware *string `json:"blueFieldSoftware,omitempty"`

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
	// +required
	NodeEffect NodeEffect `json:"nodeEffect"`

	// Specifies details on the K8S cluster to join
	// +optional
	Cluster K8sCluster `json:"cluster,omitempty"`

	// DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU.
	// +kubebuilder:validation:XValidation:rule="self==oldSelf", message="Value is immutable"
	// +kubebuilder:validation:MinLength=1
	// +required
	DPUFlavor string `json:"dpuFlavor"`

	// AstraEnabled indicates whether E/W NIC configuration (Astra) is enabled
	// +optional
	AstraEnabled *bool `json:"astraEnabled,omitempty"`

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

// Reasons reported on DPUStatus.Outdated.Reason. Exactly one of these is set
// at a time, corresponding to the first-precedence diverged template field.
const (
	DPUOutdatedReasonBFB               = "OutdatedBFB"
	DPUOutdatedReasonDPUFlavor         = "OutdatedDPUFlavor"
	DPUOutdatedReasonSecureBoot        = "OutdatedSecureBoot"
	DPUOutdatedReasonBlueFieldSoftware = "OutdatedBlueFieldSoftware"
)

// DPUOutdated reports that the DPU has drifted from its owning DPUSet's
// DPUTemplate in a way that requires the DPU to be reprovisioned. The struct
// is presence-based: when the DPU matches the template, DPUStatus.Outdated is
// nil. The DPUSet controller is the sole writer.
type DPUOutdated struct {
	// TimeStamp records when this drift was first observed for the current Reason.
	// It is preserved across reconciles as long as Reason is unchanged.
	// +required
	TimeStamp metav1.Time `json:"timeStamp"`

	// Reason is the machine-readable drift code (e.g. OutdatedBFB). When more
	// than one template field has drifted, Reason reports the first in fixed
	// precedence order: BFB -> DPUFlavor -> SecureBoot -> BlueFieldSoftware.
	// +required
	Reason string `json:"reason"`

	// Message is a human-readable summary that lists every drifted field,
	// e.g. "DPU template has changed (BFB: bfb-v1 -> bfb-v2, DPUFlavor: ...)".
	// +required
	Message string `json:"message"`
}

// DPUStatus defines the observed state of DPU
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.identityMode) || (has(self.identityMode) && self.identityMode == oldSelf.identityMode)",message="identityMode is stamp-once: can only transition from unset to a value"
type DPUStatus struct {
	// The current state of DPU.
	// +kubebuilder:default=Initializing
	// +optional
	Phase DPUPhase `json:"phase"`

	// PreviousPhase is the last non-empty Phase before the current Phase, set by the controller
	// when Phase transitions. It may be unset during early initialization (empty Phase) or until
	// the first transition from a non-empty Phase. Internal controller tracking only.
	// +optional
	PreviousPhase DPUPhase `json:"previousPhase,omitempty"`

	// Outdated, when present, indicates the DPU has drifted from its owning
	// DPUSet's DPUTemplate and needs to be reprovisioned. Set by the DPUSet
	// controller; absent when the DPU matches the template.
	// +optional
	Outdated *DPUOutdated `json:"outdated,omitempty"`

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
	// +optional
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

	// RebootStatus contains host reboot progress.
	// DPU controller derives user-facing DPUCondRebooted from this status.
	// +optional
	RebootStatus *RebootStatus `json:"rebootStatus,omitempty"`

	// The mode of the DPU
	// +kubebuilder:validation:Enum=dpu;nic
	// +kubebuilder:default=dpu
	// +optional
	DPUMode DpuModeType `json:"dpuMode,omitempty"`

	// DeploymentMode is copied from DPFOperatorConfig.spec.deploymentMode by the controller.
	// This field is read-only for users.
	// +kubebuilder:validation:Enum=zero-trust;host-trusted
	// +optional
	DeploymentMode DeploymentMode `json:"deploymentMode,omitempty"`

	// Hostless indicates that the DPU is attached to a system-managed synthetic
	// DPUNode rather than a physical host.
	// +optional
	Hostless bool `json:"hostless,omitempty"`

	// SecureBoot indicates the current UEFI Secure Boot state.
	// +optional
	SecureBoot *SecureBootStatus `json:"secureBoot,omitempty"`

	// IdentityMode records which authentication mechanism the DPU Agent uses to reach the
	// management-cluster kube-apiserver. Stamped exactly once by the DPU controller during phase
	// Initializing (nil guard); immutable thereafter. Pre-SPIFFE legacy DPUs have IdentityMode
	// unset (nil) which consumers MUST treat semantically as bootstrap-token.
	// +kubebuilder:validation:Enum=spiffe;bootstrap-token
	// +optional
	IdentityMode *IdentityMode `json:"identityMode,omitempty"`

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
// Set by the DPU agent. Most values align with NVIDIA BlueField Reset and Reboot Procedures (mlxfwreset levels).
// +kubebuilder:validation:Enum=Unknown;NoAction;PowerCycle;SystemReboot;SystemLevelReset;FirmwareReset;DPUWarmReboot;HostlessDPUReboot
type RebootMethodType string

const (
	// RebootMethodUnknown is the initial value set by the DPU agent on startup before
	// HandleReboot determines the actual method. It prevents the controller from acting
	// on a stale RebootMethod left over from a previous agent session.
	RebootMethodUnknown RebootMethodType = "Unknown"
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
	// RebootMethodDPUWarmReboot indicates the DPU OS is rebooting itself to apply
	// configuration changes (e.g. grub kernel parameters) that do not originate
	// from firmware or NVConfig. The provisioning controller should stay in the
	// current phase and wait for the agent to come back.
	RebootMethodDPUWarmReboot RebootMethodType = "DPUWarmReboot"
	// RebootMethodHostlessDPUReboot indicates a hostless DPU needs a DPU ARM
	// reboot performed by the provisioning controller through Redfish.
	RebootMethodHostlessDPUReboot RebootMethodType = "HostlessDPUReboot"
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

	// LastObservedPendingNVConfig stores the last pending NVConfig parameters seen
	// during reboot-method discovery on this boot. It is used on the next boot to
	// ignore repeated parameters that remained unchanged across boots.
	// +optional
	LastObservedPendingNVConfig *PendingNVConfigState `json:"lastObservedPendingNvconfig,omitempty"`

	// RebootSequenceCount is the length of the current non-NoAction RebootMethod sequence:
	// it increments on each agent run that reports a RebootMethod other than NoAction and
	// resets to 0 when the agent reports NoAction. Used with RebootMethod to bound host reboot loops.
	// +optional
	// +kubebuilder:validation:Minimum=0
	RebootSequenceCount *int32 `json:"rebootSequenceCount,omitempty"`

	// KubeletVersion represents the kubelet version running on the DPU.
	KubeletVersion *string `json:"kubeletVersion,omitempty"`

	// PreInstall holds agent-reported status for work done before OS install in the reprovisioning process.
	// +optional
	PreInstall *AgentPreInstallStatus `json:"preInstall,omitempty"`
	// TrustBundleHash is the bundle-hash value last applied by the DPU agent.
	// +optional
	TrustBundleHash *string `json:"trustBundleHash,omitempty"`

	// TrustBundleLastUpdateTime is when the trust bundle was last updated by the DPU agent.
	// +optional
	TrustBundleLastUpdateTime *metav1.Time `json:"trustBundleLastUpdateTime,omitempty"`

	// Conditions contains the conditions reported from inside the DPU
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Spiffe contains the SPIFFE heartbeat status reported by the DPU Agent when running in
	// SPIFFE identity mode.
	// +optional
	Spiffe *SpiffeStatus `json:"spiffe,omitempty"`

	// hostOSInit reports terminal host OS init release status from the DPU agent.
	// Unset while the agent is polling or has not reached ReleaseHostOSInit.
	// +optional
	HostOSInit *HostOSInitStatus `json:"hostOSInit,omitempty"`
}

// HostOSInitStatus is the agent-reported terminal status for host OS init release.
// +kubebuilder:validation:XValidation:rule="(has(self.skipped) ? 1 : 0) + (has(self.succeeded) ? 1 : 0) == 1",message="exactly one of skipped or succeeded must be set"
type HostOSInitStatus struct {
	// skipped indicates release was not required for this DPU.
	// +optional
	Skipped *HostOSInitSkipped `json:"skipped,omitempty"`
	// succeeded indicates host OS init was released or was already cleared.
	// +optional
	Succeeded *HostOSInitSucceeded `json:"succeeded,omitempty"`
}

// HostOSInitSkipped reports that host OS init release was not required.
type HostOSInitSkipped struct {
	// reason is a stable machine-readable outcome code.
	// +optional
	Reason *string `json:"reason,omitempty"`
	// message is a human-readable explanation.
	// +optional
	Message *string `json:"message,omitempty"`
}

// HostOSInitSucceeded reports successful host OS init release.
type HostOSInitSucceeded struct {
	// releaseAfter echoes the effective gate used for release.
	// +optional
	ReleaseAfter *HostOSInitReleaseAfter `json:"releaseAfter,omitempty"`
}

// IdentityMode records which authentication mechanism the DPU Agent uses to reach the
// management-cluster kube-apiserver. It is stamp-once (see DPUStatus.IdentityMode).
type IdentityMode string

const (
	// IdentityModeSpiffe indicates the DPU Agent authenticates with a SPIFFE-issued JWT-SVID.
	IdentityModeSpiffe IdentityMode = "spiffe"
	// IdentityModeBootstrapToken indicates the DPU Agent authenticates with a kubeadm bootstrap token.
	// An unset (nil) IdentityMode is treated semantically as bootstrap-token; no sentinel is declared
	// for the unset case deliberately, to force explicit handling by consumers.
	IdentityModeBootstrapToken IdentityMode = "bootstrap-token"
)

// SpiffeStatus is the DPU Agent's SPIFFE heartbeat sub-status.
type SpiffeStatus struct {
	// LastProbeTime is the wall-clock timestamp the DPU Agent recorded on its most recent successful
	// status report. It is informational and subject to DPU clock skew, so it is not a precise
	// liveness signal on its own.
	// +optional
	LastProbeTime *metav1.Time `json:"lastProbeTime,omitempty"`

	// LastProbeMessage is a structured one-line diagnostic for the most recent self-probe. Unset in
	// the steady-state happy path. Bounded to 256 chars (truncated agent-side).
	// +kubebuilder:validation:MaxLength=256
	// +optional
	LastProbeMessage *string `json:"lastProbeMessage,omitempty"`
}

// AgentPreInstallStatus is agent status for reprovisioning.
type AgentPreInstallStatus struct {
	// AgentReported is set with a timestamp when dpu-agent detects a new DPU CR is created for reprovisioning.
	// +optional
	AgentReported *metav1.Time `json:"agentReported,omitempty"`

	// Conditions contains pre-install conditions (e.g. NVConfigApplied for reprovisioning).
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type PendingNVConfigState struct {
	BootID  string                  `json:"bootID,omitempty"`
	Devices []PendingNVConfigDevice `json:"devices,omitempty"`
}

type PendingNVConfigDevice struct {
	Device  string                 `json:"device,omitempty"`
	Entries []PendingNVConfigEntry `json:"entries"`
}

type PendingNVConfigEntry struct {
	Name    string `json:"name,omitempty"`
	Default string `json:"default,omitempty"`
	Current string `json:"current,omitempty"`
	// NextBoot uses the "next_boot" so this type can be reused for parsing mlxfwrest output
	NextBoot string `json:"next_boot,omitempty"`
}

// RebootStatusPhase is the host reboot progress phase.
// +kubebuilder:validation:Enum=WaitForShutdown;Pending;Succeeded;Failed;Unknown
type RebootStatusPhase string

const (
	// RebootStatusWaitForShutdown means the host reboot is held until the DPU has
	// completed its graceful shutdown. Used in Zero Trust mode for a System Level
	// Reset so the External/Script host reboot is not triggered while the Arm OS is
	// still shutting down.
	RebootStatusWaitForShutdown RebootStatusPhase = "WaitForShutdown"
	// RebootStatusPending means reboot is requested but execution has not started yet
	// (for example, waiting for a manual external reboot trigger).
	RebootStatusPending RebootStatusPhase = "Pending"
	// RebootStatusSucceeded means reboot completed successfully.
	RebootStatusSucceeded RebootStatusPhase = "Succeeded"
	// RebootStatusFailed means reboot execution failed.
	RebootStatusFailed RebootStatusPhase = "Failed"
	// RebootStatusUnknown means reboot execution state cannot be determined.
	RebootStatusUnknown RebootStatusPhase = "Unknown"
)

// RebootStatus stores the host reboot execution status.
type RebootStatus struct {
	// Phase is the current host reboot progress.
	Phase RebootStatusPhase `json:"phase,omitempty"`
	// Method is the recommended reboot method.
	// +optional
	Method *RebootMethodType `json:"method,omitempty"`
	// Reason indicates machine-readable reason for current phase.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Message provides human-readable details for current phase.
	// +optional
	Message string `json:"message,omitempty"`
	// LastTransitionTime is the last update time for reboot status.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
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
