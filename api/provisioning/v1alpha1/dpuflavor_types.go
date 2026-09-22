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

// DPUFlavorSpec defines the content of DPUFlavor.
// Config-only fields (grub, sysctl, nvconfig, ovs, packages, systemdServices,
// containerdConfig, agent-applied configFiles, serviceReadiness, ewNicConfigurations,
// dma, scalableFunctions, virtualFunctions) may be updated in place.
// Immutable fields require a new DPUFlavor.
// +kubebuilder:validation:XValidation:rule="(!has(self.dpuMode) && !has(oldSelf.dpuMode)) || (has(self.dpuMode) && has(oldSelf.dpuMode) && self.dpuMode == oldSelf.dpuMode)",message="dpuMode is immutable"
// +kubebuilder:validation:XValidation:rule="(!has(self.bfcfgParameters) && !has(oldSelf.bfcfgParameters)) || (has(self.bfcfgParameters) && has(oldSelf.bfcfgParameters) && self.bfcfgParameters == oldSelf.bfcfgParameters)",message="bfcfgParameters is immutable"
// +kubebuilder:validation:XValidation:rule="(!has(self.dpuResources) && !has(oldSelf.dpuResources)) || (has(self.dpuResources) && has(oldSelf.dpuResources) && self.dpuResources == oldSelf.dpuResources)",message="dpuResources is immutable"
// +kubebuilder:validation:XValidation:rule="(!has(self.systemReservedResources) && !has(oldSelf.systemReservedResources)) || (has(self.systemReservedResources) && has(oldSelf.systemReservedResources) && self.systemReservedResources == oldSelf.systemReservedResources)",message="systemReservedResources is immutable"
// +kubebuilder:validation:XValidation:rule="(!has(self.hostNetworkInterfaceConfigs) && !has(oldSelf.hostNetworkInterfaceConfigs)) || (has(self.hostNetworkInterfaceConfigs) && has(oldSelf.hostNetworkInterfaceConfigs) && self.hostNetworkInterfaceConfigs == oldSelf.hostNetworkInterfaceConfigs)",message="hostNetworkInterfaceConfigs is immutable"
// +kubebuilder:validation:XValidation:rule="(has(self.configFiles) ? self.configFiles.filter(f, !has(f.type) || f.type == 'cloud-init') : []) == (has(oldSelf.configFiles) ? oldSelf.configFiles.filter(f, !has(f.type) || f.type == 'cloud-init') : [])",message="configFiles with type cloud-init (the default) are immutable"
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

	// ScalableFunctions is the list of SF groups to create on the DPU, or on the host when
	// hostDevice is set. Count is per selected device. Over-subscribe can trigger failures at
	// create time. Editing this field reprovisions the DPU. For backward compatibility, when
	// both this SF list and VF list are empty, SF counts are still derived from PF_TOTAL_SF
	// (removed in a future release).
	// +kubebuilder:validation:MaxItems=16
	// +listType=atomic
	// +optional
	ScalableFunctions []ScalableFunction `json:"scalableFunctions,omitempty"`

	// VirtualFunctions is the list of VF groups to create. Count is per selected
	// device. Groups ending up on the same device sum to a single `sriov_numvfs` and then
	// list order assigns contiguous index ranges. Over-subscribe can trigger failures at
	// create time. Editing this field reprovisions the DPU.
	// +kubebuilder:validation:MaxItems=16
	// +listType=atomic
	// +optional
	VirtualFunctions []VirtualFunction `json:"virtualFunctions,omitempty"`

	// DMA configures the SNAP DMA SF. The agent picks the ECPF; sfnum is 8000 and MAC is derived.
	// Ignored except on BlueField-4.
	// +optional
	DMA *DPUFlavorDMA `json:"dma,omitempty"`

	// OVS contains the OVS configuration for the DPUFlavor.
	// +optional
	OVS DPUFlavorOVS `json:"ovs,omitempty"`
	// BFCfgParameters are the parameters to be set in the bf.cfg file.
	// +optional
	BFCfgParameters []string `json:"bfcfgParameters,omitempty"`
	// ConfigFiles are the files to be written on the DPU.
	// +optional
	ConfigFiles []ConfigFile `json:"configFiles,omitempty"`
	// Packages are the packages to reconcile on the node.
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:rule="self.all(x, self.exists_one(y, x.name == y.name))",message="package names must be unique"
	// +optional
	Packages []PackageSpec `json:"packages,omitempty"`
	// SystemdServices are the systemd services to manage on the node.
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:rule="self.all(x, self.exists_one(y, x.name == y.name))",message="systemd service names must be unique"
	// +optional
	SystemdServices []SystemdServiceSpec `json:"systemdServices,omitempty"`
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

	// EWNicConfigurations lists per-NIC configuration for the E/W NICs.
	// Only the first entry is applied in this release; additional entries are ignored until a future
	// release adds multi-NIC support. The field is modeled as a list now so the API shape does not
	// need to change when multiple entries are supported.
	// +kubebuilder:validation:MaxItems=16
	// +listType=atomic
	// +optional
	EWNicConfigurations []NicConfiguration `json:"ewNicConfigurations,omitempty"`

	// serviceReadiness configures the Service Readiness phase.
	// +optional
	ServiceReadiness *ServiceReadiness `json:"serviceReadiness,omitempty"`
}

// ScalableFunction is one group of SFs to create.
// +kubebuilder:validation:XValidation:rule="!has(self.options) || !has(self.options.macAddress) || self.count == 1",message="options.macAddress requires count == 1"
// +kubebuilder:validation:XValidation:rule="!has(self.hostDevice) || !self.hostDevice || !has(self.poolName)",message="poolName must not be set when hostDevice is true"
type ScalableFunction struct {
	// Count is SFs to create per selected device. With device "*", count 20 is 20 SFs
	// on each port. Zero creates nothing.
	// +kubebuilder:validation:Minimum=0
	// +required
	Count *int32 `json:"count,omitempty"`

	// Device is the target port: "*", a name such as p0, or a PCI address. Defaults to "*".
	// +kubebuilder:validation:Pattern=`^(\*|[pP][0-9]+|[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7])$`
	// +optional
	Device *string `json:"device,omitempty"`

	// HostDevice creates the SFs on the host (representors on the DPU). Implies
	// controller 1; options.controller overrides. Host SFs use the host firmware
	// budget and must not set poolName.
	// +optional
	HostDevice *bool `json:"hostDevice,omitempty"`

	// PoolName is the device-plugin resource (for example bf_sf). Unset: created in
	// hardware only. Forbidden with hostDevice.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9_.-]*$`
	// +optional
	PoolName *string `json:"poolName,omitempty"`

	// Options are creation settings for this group.
	// +optional
	Options *ScalableFunctionOptions `json:"options,omitempty"`
}

// ScalableFunctionOptions are per-group SF creation settings.
type ScalableFunctionOptions struct {
	// MACAddress pins the SF MAC (colon-separated 48-bit). Requires count 1.
	// +kubebuilder:validation:Pattern=`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`
	// +optional
	MACAddress *string `json:"macAddress,omitempty"`

	// SFNumStart is the first sfnum of the group; the rest are sequential from it.
	// Reserved before agent-numbered groups, which fill from 0 around them.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	// +optional
	SFNumStart *int32 `json:"sfNumStart,omitempty"`

	// Trusted creates the SFs as trusted functions.
	// +optional
	Trusted *bool `json:"trusted,omitempty"`

	// Controller is the external controller to create on. Overrides hostDevice.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=15
	// +optional
	Controller *int32 `json:"controller,omitempty"`

	// CPUList pins the SFs to CPUs in mlnx-sf list form, for example 0-3 or 0,2,4.
	// +kubebuilder:validation:MaxLength=200
	// +kubebuilder:validation:Pattern=`^[0-9]+(-[0-9]+)?(,[0-9]+(-[0-9]+)?)*$`
	// +optional
	CPUList *string `json:"cpuList,omitempty"`

	// DisableRoCE creates the SFs with RoCE disabled.
	// +optional
	DisableRoCE *bool `json:"disableRoCE,omitempty"`

	// DisableNetdev leaves the SFs without an ethernet netdev.
	// +optional
	DisableNetdev *bool `json:"disableNetdev,omitempty"`
}

// VirtualFunction is one group of VFs to create.
// +kubebuilder:validation:XValidation:rule="!has(self.options) || !has(self.options.macAddress) || self.count == 1",message="options.macAddress requires count == 1"
type VirtualFunction struct {
	// Count is VFs to create per selected device. Groups ending up on the same device sum to
	// a single `sriov_numvfs` and then list order assigns contiguous index ranges. Zero creates nothing.
	// +kubebuilder:validation:Minimum=0
	// +required
	Count *int32 `json:"count,omitempty"`

	// Device is the target port: "*", a name such as p0, or a PCI address. Defaults to "*".
	// +kubebuilder:validation:Pattern=`^(\*|[pP][0-9]+|[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7])$`
	// +optional
	Device *string `json:"device,omitempty"`

	// PoolName is the device-plugin resource (for example bf_vf). Unset: created in
	// hardware only.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9_.-]*$`
	// +optional
	PoolName *string `json:"poolName,omitempty"`

	// Options are creation settings for this group.
	// +optional
	Options *VirtualFunctionOptions `json:"options,omitempty"`
}

// VirtualFunctionOptions are per-group VF creation settings.
type VirtualFunctionOptions struct {
	// MACAddress sets the VF MAC (colon-separated 48-bit). Requires count 1.
	// +kubebuilder:validation:Pattern=`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`
	// +optional
	MACAddress *string `json:"macAddress,omitempty"`
}

// DPUFlavorDMA configures the SNAP DMA SF. The agent picks the ECPF; sfnum is 8000 and MAC is derived.
type DPUFlavorDMA struct {
	// Enabled creates the SNAP DMA SF on BlueField-4 socket-direct systems.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// DMAEnabled reports whether spec.dma.enabled is set.
func (f *DPUFlavor) DMAEnabled() bool {
	return f != nil && f.Spec.DMA != nil && f.Spec.DMA.Enabled != nil && *f.Spec.DMA.Enabled
}

// ServiceReadiness configures the Service Readiness provisioning phase.
type ServiceReadiness struct {
	// gate is the DPU.status.operationalConditions entry that must be True before the DPU leaves
	// the Service Readiness phase. When unset the phase does not wait, and a host hold requested
	// via DELAY_HOST_OS_INIT is released on DPUServiceCriticalPodsReady.
	// +optional
	Gate ServiceReadinessGate `json:"gate,omitempty"`
}

// ServiceReadinessGate names a DPU operational condition. Values match DPUOperationalConditionType.
// +kubebuilder:validation:Enum=DPUServiceCriticalPodsReady;OperationalReady
type ServiceReadinessGate string

const (
	// GateDPUServiceCriticalPodsReady waits for
	// DPU.status.operationalConditions[DPUServiceCriticalPodsReady] == True.
	GateDPUServiceCriticalPodsReady ServiceReadinessGate = "DPUServiceCriticalPodsReady"
	// GateOperationalReady waits for DPU.status.operationalConditions[OperationalReady] == True.
	GateOperationalReady ServiceReadinessGate = "OperationalReady"
)

// ConfiguredGate returns the gate set on the flavor, or "" when it does not set one. The empty
// string is outside the enum, so it is unambiguously "unset". Callers decide what unset means for
// them: the Service Readiness phase does not wait, while ReleaseGate substitutes a default.
func (f *DPUFlavor) ConfiguredGate() ServiceReadinessGate {
	if f == nil || f.Spec.ServiceReadiness == nil {
		return ""
	}
	return f.Spec.ServiceReadiness.Gate
}

// ReleaseGate is the condition the DPU agent waits for before releasing a host OS init hold.
// It falls back to DPUServiceCriticalPodsReady when gate is unset, because a hold with no gate
// would release immediately and defeat the purpose of holding the host at all. This fallback is
// specific to releasing a hold; do not reuse it to decide whether a phase should block.
func (f *DPUFlavor) ReleaseGate() ServiceReadinessGate {
	if gate := f.ConfiguredGate(); gate != "" {
		return gate
	}
	return GateDPUServiceCriticalPodsReady
}

// FirstEWNicConfiguration returns the E/W NIC configuration used by provisioning in this release.
// Only index 0 of Spec.EWNicConfigurations is honored; further entries are reserved for future multi-NIC support.
func (s *DPUFlavorSpec) FirstEWNicConfiguration() *NicConfiguration {
	if s == nil || len(s.EWNicConfigurations) == 0 {
		return nil
	}
	return &s.EWNicConfigurations[0]
}

// NicConfiguration is a set of configurations for the NICs
// +kubebuilder:validation:XValidation:rule="!(has(self.spectrumXOptimized) && self.spectrumXOptimized.enabled) || ((!has(self.linkType) || self.linkType == 'Ethernet') && self.numVfs == 1)",message="spectrumXOptimized can be enabled only when linkType=='Ethernet' (or unset for Network Bay) and numVfs==1"
// +kubebuilder:validation:XValidation:rule="has(self.networkBay) || has(self.linkType)",message="linkType is required unless networkBay is configured"
// +kubebuilder:validation:XValidation:rule="!has(self.networkBay) || !has(self.linkType)",message="linkType must not be set when networkBay is configured (the Network Bay link type is governed by the system configuration)"
// +kubebuilder:validation:XValidation:rule="!has(self.networkBay) || self.networkBay.conf != \"\"",message="networkBay.conf must not be empty"
type NicConfiguration struct {
	// Number of VFs to be configured
	// +required
	NumVfs int `json:"numVfs"`
	// LinkType to be configured, Ethernet|Infiniband. Required unless networkBay is configured;
	// for Network Bay the link type is governed by the system configuration and must not be set.
	// +kubebuilder:validation:Enum=Ethernet;Infiniband
	// +optional
	LinkType nicconfigv1alpha1.LinkTypeEnum `json:"linkType,omitempty"`
	// Spectrum-X optimization settings. Works only with linkType==Ethernet && numVfs==1. RawNvConfig parameters, if provided, are merged as overrides on top of Spectrum-X calculated params.
	SpectrumXOptimized *nicconfigv1alpha1.SpectrumXOptimizedSpec `json:"spectrumXOptimized,omitempty"`
	// List of arbitrary nv config parameters
	RawNvConfig []nicconfigv1alpha1.NvConfigParam `json:"rawNvConfig,omitempty"`
	// NetworkBay configures a ConnectX-9 Network Bay card (per-ASIC set_system_conf). Allowed only for ConnectX-9 (nicType 1025).
	// +optional
	NetworkBay *nicconfigv1alpha1.NetworkBaySpec `json:"networkBay,omitempty"`
	// Force passes `--force` to mlxconfig set commands. When set, the daemon
	// applies the nv config batch and set_system_conf with --force, letting
	// mlxconfig accept a batch it would otherwise refuse due to implicit
	// parameter dependencies.
	// +optional
	// +kubebuilder:default:=false
	Force bool `json:"force,omitempty"`
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
	// DELAY_HOST_OS_INIT=ENABLE_USER (0x3) holds the host at UEFI and is rejected by the DPU agent
	// outside zero-trust, where the agent needs the host to reach the kube-apiserver.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:Pattern=`^[^=\s]+=[^\s]*$`
	// +kubebuilder:validation:items:MaxLength=200
	// +listType=atomic
	// +optional
	Parameters []string `json:"parameters,omitempty"`
	// force applies the parameters with `mlxconfig --force` and skips the `mlxconfig q` filter,
	// so parameters firmware does not yet expose are applied now instead of deferred to a later
	// reboot. Required when a parameter is gated behind another parameter in the same batch.
	// Requires DOCA 3.5.0 or later; on an older DOCA version the operation fails.
	// `--force` skips validation for the whole batch, so an invalid value is applied silently.
	// Ignored under `spec.hostNetworkInterfaceConfigs[].nvconfig`.
	// +optional
	Force *bool `json:"force,omitempty"`
}

type DPUFlavorOVS struct {
	// RawConfigScript is the raw configuration script for OVS.
	// The DPU agent runs this script once per boot. A restart of the agent in the
	// same boot does not re-run it; a reboot or power cycle does.
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

// ConfigFileType defines when a config file is materialized.
// +kubebuilder:validation:Enum=cloud-init;agent-applied
type ConfigFileType string

const (
	ConfigFileTypeCloudInit    ConfigFileType = "cloud-init"
	ConfigFileTypeAgentApplied ConfigFileType = "agent-applied"
)

// ConfigFile describes a file materialized from inline raw content or external contentFrom.
// +kubebuilder:validation:XValidation:rule="has(self.raw) != has(self.contentFrom)",message="exactly one of raw or contentFrom must be specified"
// +kubebuilder:validation:XValidation:rule="!has(self.contentFrom) || has(self.contentFrom.configMapKeyRef)",message="contentFrom.configMapKeyRef must be specified"
// +kubebuilder:validation:XValidation:rule="!has(self.type) || self.type != 'cloud-init' || has(self.raw)",message="type cloud-init supports raw only"
// +kubebuilder:validation:XValidation:rule="!has(self.type) || self.type != 'agent-applied' || has(self.contentFrom)",message="type agent-applied supports contentFrom only"
// +kubebuilder:validation:XValidation:rule="has(self.type) || has(self.raw)",message="type defaults to cloud-init, which requires raw content"
type ConfigFile struct {
	// Type controls when the file content is materialized.
	// cloud-init files use raw inline content and are written during cloud-init.
	// agent-applied files use contentFrom and are written later by dpu-agent.
	// Defaults to cloud-init when omitted.
	// +kubebuilder:default=cloud-init
	// +optional
	Type *ConfigFileType `json:"type,omitempty"`
	// Path is the path of the file to be written.
	// +required
	Path string `json:"path"`
	// Operation is the operation to be performed on the file.
	// +optional
	Operation DPUFlavorFileOp `json:"operation,omitempty"`
	// Raw is the inline file content.
	// Supported only when type is cloud-init. When type is omitted, type defaults
	// to cloud-init and raw must be set.
	// +optional
	Raw *string `json:"raw,omitempty"`
	// ContentFrom references external content for the file.
	// Supported only when type is agent-applied.
	// +optional
	ContentFrom *ConfigFileContentSource `json:"contentFrom,omitempty"`
	// Permissions are the permissions to be set on the file.
	// +optional
	Permissions string `json:"permissions,omitempty"`
}

type ConfigFileContentSource struct {
	// ConfigMapKeyRef selects a key from a ConfigMap in the DPU namespace.
	// +optional
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
}

// PackageSpec defines a package to reconcile on the node.
type PackageSpec struct {
	// Name is the package name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// Version constrains the package version.
	// If empty, any installed version satisfies the spec.
	// +optional
	Version *PackageVersionSpec `json:"version,omitempty"`
	// RepoFileRef constrains package resolution to a specific repository file available on the node.
	// If empty, any configured repository may satisfy the package.
	// If specified, only the referenced repository file may provide candidates.
	// If that repository file does not provide the package or requested version, the dpu-agent flow does not continue.
	// +optional
	RepoFileRef string `json:"repoFileRef,omitempty"`
}

// PackageVersionSpec defines a package version constraint.
type PackageVersionSpec struct {
	// Value is the package version to compare against.
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
	// MatchPolicy controls how Value is matched.
	// If omitted, AtLeast is used.
	// +kubebuilder:validation:Enum=Exact;AtLeast
	// +kubebuilder:default=AtLeast
	// +optional
	MatchPolicy PackageVersionMatchPolicy `json:"matchPolicy,omitempty"`
}

// PackageVersionMatchPolicy defines how a package version constraint is evaluated.
type PackageVersionMatchPolicy string

const (
	// PackageVersionMatchExact requires the installed package version to equal Value.
	PackageVersionMatchExact PackageVersionMatchPolicy = "Exact"

	// PackageVersionMatchAtLeast requires the installed package version to be greater than or equal to Value.
	PackageVersionMatchAtLeast PackageVersionMatchPolicy = "AtLeast"
)

// SystemdServiceOperation defines the operation to perform on a systemd service.
// +kubebuilder:validation:Enum=Start;Enable;EnableAndStart
type SystemdServiceOperation string

const (
	// SystemdServiceStart starts the service without enabling it at boot.
	SystemdServiceStart SystemdServiceOperation = "Start"

	// SystemdServiceEnable enables the service at boot without starting it immediately.
	SystemdServiceEnable SystemdServiceOperation = "Enable"

	// SystemdServiceEnableAndStart enables the service at boot and starts it immediately (equivalent to systemctl enable --now).
	SystemdServiceEnableAndStart SystemdServiceOperation = "EnableAndStart"
)

// SystemdServiceSpec defines a systemd service to manage on the node.
type SystemdServiceSpec struct {
	// Name is the systemd service name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// Operation is the systemd operation to perform on the service.
	Operation SystemdServiceOperation `json:"operation"`
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
