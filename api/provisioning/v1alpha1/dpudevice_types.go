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
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// DPUDeviceKind is the kind of the DPUDevice object
	DPUDeviceKind = "DPUDevice"

	// DPUDeviceFinalizer is the finalizer used to prevent DpuDevice deletion while DPU is using it
	DPUDeviceFinalizer = DPUProvisioningPrefix + "dpudevice-protection"

	// BMCCredentialFinalizer is the finalizer added to per-device BMC credential secrets
	// to prevent accidental deletion while the DPUDevice depends on them.
	BMCCredentialFinalizer = DPUProvisioningPrefix + "bmc-credential"

	// DPUDeviceLabelSkipHWProvisioning indicates the device should skip
	// hardware-specific provisioning steps (install, FW config, reboot).
	DPUDeviceLabelSkipHWProvisioning = DPUProvisioningPrefix + "skip-hw-provisioning"

	// SPIFFEDeregistrationFinalizer is added to a SPIFFE-mode DPUDevice and held until its
	// per-DPU SPIRE ClusterStaticEntry CR has been removed from the K8s API. It enforces the
	// deletion-ordering invariant that the ClusterStaticEntry is GC'd before the DPUDevice,
	// so a reflashed DPU cannot race a stale identity entry.
	SPIFFEDeregistrationFinalizer = DPUProvisioningPrefix + "spiffe-deregistration"

	// RotateBMCServerCertificateAnnotation requests a manual rotation of the DPU BMC mTLS
	// server certificate. The annotation value is an opaque token (any non-empty string;
	// a timestamp or UUID is recommended). Rotation is triggered when the value differs from
	// status.bmcServerCertificate.observedManualTrigger; after a successful rotation the
	// controller copies the value into observedManualTrigger so the same trigger is not
	// processed twice.
	RotateBMCServerCertificateAnnotation = DPUProvisioningPrefix + "rotate-bmc-server-certificate"
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
	// ConditionBMCCredentialsReady reports the health of BMC credential resolution.
	ConditionBMCCredentialsReady conditions.ConditionType = "BMCCredentialsReady"
	// ConditionSPIFFEEntryReady reports whether the per-DPU SPIRE ClusterStaticEntry has been
	// registered and rendered (L1 composite mirror of the upstream entry status).
	ConditionSPIFFEEntryReady conditions.ConditionType = "SPIFFEEntryReady"
	// ConditionDpuDeviceBMCServerCertificateReady indicates the BMC mTLS server
	// certificate is installed, valid, and not within its renew-before window.
	ConditionDpuDeviceBMCServerCertificateReady conditions.ConditionType = "BMCServerCertificateReady"
	// ConditionDpuDeviceCATrustBundleReady indicates whether the desired CA trust bundle
	// has been reconciled to the device BMC truststore.
	ConditionDpuDeviceCATrustBundleReady conditions.ConditionType = "CATrustBundleReady"
)

// BMCServerCertificateReady condition reasons
const (
	// ReasonBMCServerCertificateRotating indicates a rotation is in progress (CSR generated / CR pending).
	ReasonBMCServerCertificateRotating = "BMCServerCertificateRotating"
	// ReasonBMCServerCertificateRotationFailed indicates the last rotation attempt failed.
	ReasonBMCServerCertificateRotationFailed = "BMCServerCertificateRotationFailed"
	// ReasonBMCServerCertificateUntrusted indicates the BMC presents a server certificate that
	// does not chain to the current DPF CA (or fails identity pinning). The controller re-runs
	// setUpMTLS over basic auth without clearing Initialized / factory-resetting the BMC.
	ReasonBMCServerCertificateUntrusted = "BMCServerCertificateUntrusted"
	// ReasonRedfishClientCertStale indicates the controller's Redfish client certificate does not
	// chain to the current DPF CA. The controller forces cert-manager to reissue it.
	ReasonRedfishClientCertStale = "RedfishClientCertStale"
)

// BMCCredentialsReady condition reasons
const (
	// ReasonCATrustBundleSynced indicates CA trust bundle is successfully synced to BMC.
	ReasonCATrustBundleSynced = "CATrustBundleSynced"
	// ReasonCATrustBundleSyncing indicates CA trust bundle sync is in progress.
	ReasonCATrustBundleSyncing = "CATrustBundleSyncing"
	// ReasonCredentialsValid indicates the credential secret is valid and authentication succeeded.
	ReasonCredentialsValid = "CredentialsValid"
	// ReasonCredentialsSecretNotFound indicates the referenced secret does not exist.
	ReasonCredentialsSecretNotFound = "CredentialsSecretNotFound"
	// ReasonCredentialsSecretInvalid indicates the secret exists but is malformed.
	ReasonCredentialsSecretInvalid = "CredentialsSecretInvalid"
	// ReasonBMCAuthenticationFailed indicates the password was rejected by the BMC.
	ReasonBMCAuthenticationFailed = "BMCAuthenticationFailed"
	// ReasonModeSwitchNotAllowed indicates an attempt to switch from per-device to shared mode.
	ReasonModeSwitchNotAllowed = "ModeSwitchNotAllowed"
	// ReasonCATrustBundleSyncFailed indicates CA trust bundle sync to BMC failed.
	ReasonCATrustBundleSyncFailed = "CATrustBundleSyncFailed"
	// ReasonCATrustBundleUnavailable indicates desired CA trust bundle is unavailable or invalid.
	ReasonCATrustBundleUnavailable = "CATrustBundleUnavailable"
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
		ConditionBMCCredentialsReady,
		ConditionSPIFFEEntryReady,
		ConditionDpuDeviceBMCServerCertificateReady,
		ConditionDpuDeviceCATrustBundleReady,
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
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="OPN is immutable"
	// +optional
	OPN *string `json:"opn,omitempty"`

	// BMCIP is the IP address of the BMC (Base Management Controller) on the device.
	// This is used for remote management and monitoring of the device.
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

	// BMCCredentialSecretName is the name of a Secret in the same namespace containing
	// per-device BMC credentials. The secret must contain a "password" key with the BMC credential value.
	// If specified, this password takes precedence over the shared bmc-shared-password secret.
	// +optional
	BMCCredentialSecretName *string `json:"bmcCredentialSecretName,omitempty"`

	// Specifies details on the K8S cluster to join
	// +optional
	Cluster *DPUDeviceClusterSpec `json:"cluster,omitempty"`

	// Values contains free-form per-device values used to render a DPUFlavorTemplate
	// into a concrete generated DPUFlavor for this device.
	// +optional
	Values *runtime.RawExtension `json:"values,omitempty"`
}

// DPUDeviceClusterSpec holds node labels and annotations propagated from DPUDevice to the DPU and cluster node.
type DPUDeviceClusterSpec struct {
	// NodeLabels specifies labels to be added to the DPU cluster node for this device.
	// +optional
	NodeLabels map[string]string `json:"nodeLabels,omitempty"`
	// NodeAnnotations specifies annotations to be added to the DPU cluster node for this device.
	// +optional
	NodeAnnotations map[string]string `json:"nodeAnnotations,omitempty"`
}

type DPUDeviceStatus struct {
	// PSID is the Product Serial ID of the device.
	// It's used to track the device's lifecycle and for inventory management.
	// This value is discovered and should not be changed once set.
	// Example: "MT_0001234567", "MT25066004C7"
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

	// BMCCredentialSecretName is the name of the Secret last used successfully for BMC authentication.
	// +optional
	BMCCredentialSecretName *string `json:"bmcCredentialSecretName,omitempty"`

	// BMCServerCertificate reports the BMC mTLS server certificate rotation state.
	// +optional
	BMCServerCertificate *CertificateStatus `json:"bmcServerCertificate,omitempty"`
	// CATrustBundle stores trust bundle reconciliation progress for the DPUDevice.
	// +optional
	CATrustBundle *TrustBundleStatus `json:"caTrustBundle,omitempty"`

	// +optional
	Conditions []metav1.Condition `json:"conditions"`
}

// CertificateStatus reports the rotation state of a DPF-managed certificate.
type CertificateStatus struct {
	// NotAfter is the expiry time of the certificate currently installed. It is taken
	// from the issued certificate at rotation time.
	// +optional
	NotAfter *metav1.Time `json:"notAfter,omitempty"`

	// LastRotationTime is the time DPF last successfully rotated the certificate.
	// +optional
	LastRotationTime *metav1.Time `json:"lastRotationTime,omitempty"`

	// ObservedManualTrigger records the value of the manual rotation annotation that
	// was last honored, so the same trigger is not processed twice.
	// +optional
	ObservedManualTrigger *string `json:"observedManualTrigger,omitempty"`
}

// TrustBundleStatus reports reconciliation progress for a CA trust bundle.
type TrustBundleStatus struct {
	// ObservedBundleHash records the last successfully applied bundle hash.
	// +optional
	ObservedBundleHash *string `json:"observedBundleHash,omitempty"`
	// LastUpdateTime is when the last successful reconciliation completed.
	// +optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`
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
