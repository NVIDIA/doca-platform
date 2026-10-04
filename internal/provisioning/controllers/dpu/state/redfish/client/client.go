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

package client

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/go-resty/resty/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// managerIDPlaceholder is the token substituted with the resolved BMC manager ID in Manager-scoped
// Redfish API paths.
const managerIDPlaceholder = "{MANAGER_ID}"

const (
	APIChangePasswd                 = "redfish/v1/AccountService/Accounts/{USER}"
	APICheckBMCFW                   = "redfish/v1/UpdateService/FirmwareInventory/{BMC_FW_ID}"
	APICheckBMCEROTFW               = "redfish/v1/UpdateService/FirmwareInventory/BlueField_FW_ERoT_BMC_0"
	APICheckDpuBoardFW              = "redfish/v1/UpdateService/FirmwareInventory/DPU_BOARD"
	APICheckDPUBSP                  = "redfish/v1/UpdateService/FirmwareInventory/DPU_BSP"
	APICheckDPUNIC                  = "redfish/v1/UpdateService/FirmwareInventory/{DPU_NIC_ID}"
	APICheckDPUOS                   = "redfish/v1/UpdateService/FirmwareInventory/DPU_OS"
	APICheckDPUUEFI                 = "redfish/v1/UpdateService/FirmwareInventory/{DPU_UEFI_ID}"
	APICheckPendingBundle           = "redfish/v1/UpdateService/FirmwareInventory/Pending_Bundle"
	APICheckPendingBMCFW            = "redfish/v1/UpdateService/FirmwareInventory/BlueField_FW_BMC_0_pending"
	APICheckPendingBMCEROTFW        = "redfish/v1/UpdateService/FirmwareInventory/BlueField_FW_ERoT_BMC_0_pending"
	APICheckPendingDPUUEFI          = "redfish/v1/UpdateService/FirmwareInventory/BlueField_FW_CPU_0_pending"
	APICheckPendingDPUNIC           = "redfish/v1/UpdateService/FirmwareInventory/BlueField_FW_NIC_0_pending"
	APICheckOSImage                 = "redfish/v1/UpdateService/FirmwareInventory/BlueField_OS_Image_CPU_0"
	APICheckConfigImage             = "redfish/v1/UpdateService/FirmwareInventory/BlueField_OS_Config_CPU_0"
	APIInstallBFB                   = "redfish/v1/UpdateService/Actions/UpdateService.SimpleUpdate"
	APIGetVirtualMedia              = "redfish/v1/Managers/{MANAGER_ID}/VirtualMedia/{MEDIA_ID}"
	APIInsertVirtualMedia           = "redfish/v1/Managers/{MANAGER_ID}/VirtualMedia/{MEDIA_ID}/Actions/VirtualMedia.InsertMedia"
	APIEjectVirtualMedia            = "redfish/v1/Managers/{MANAGER_ID}/VirtualMedia/{MEDIA_ID}/Actions/VirtualMedia.EjectMedia"
	APIUpdateFW                     = "redfish/v1/UpdateService"
	APICheckProgress                = "redfish/v1/TaskService/Tasks"
	APIGetManagers                  = "redfish/v1/Managers"
	APIGetSystems                   = "redfish/v1/Systems"
	APIGetBMCManager                = "redfish/v1/Managers/{MANAGER_ID}"
	APIFactoryResetBMC              = "redfish/v1/Managers/{MANAGER_ID}/Actions/Manager.ResetToDefaults"
	APIResetBMC                     = "redfish/v1/Managers/{MANAGER_ID}/Actions/Manager.Reset"
	APIEnableBMCRshim               = "redfish/v1/Managers/Bluefield_BMC/Oem/Nvidia"
	APIGetSystem                    = "redfish/v1/Systems/{SYSTEM_ID}"
	APIBluefieldSettings            = "redfish/v1/Systems/{SYSTEM_ID}/Settings"
	APIDisableHostRshim             = "redfish/v1/Systems/Bluefield/Oem/Nvidia/Actions/HostRshim.Set"
	APIInstallCert                  = "redfish/v1/Managers/{MANAGER_ID}/Truststore/Certificates"
	APIServerCert                   = "redfish/v1/Managers/{MANAGER_ID}/NetworkProtocol/HTTPS/Certificates/1"
	APIReplaceCert                  = "redfish/v1/CertificateService/Actions/CertificateService.ReplaceCertificate"
	APIUpdateBluefieldFWMultipart   = "redfish/v1/UpdateService/update-multipart"
	APIActivatePendingBundle        = "redfish/v1/UpdateService/Actions/UpdateService.Activate"
	APIGetBios                      = "redfish/v1/Systems/{SYSTEM_ID}/Bios"
	APISetBiosSettings              = "redfish/v1/Systems/{SYSTEM_ID}/Bios/Settings"
	APISetMode                      = "/redfish/v1/Systems/Bluefield/Oem/Nvidia/Actions/Mode.Set"
	APIGenerateCSR                  = "redfish/v1/CertificateService/Actions/CertificateService.GenerateCSR"
	APIEnableMTLS                   = "redfish/v1/AccountService"
	APIProductDescription           = "redfish/v1/Systems/{SYSTEM_ID}/Oem/Nvidia"
	APIGetChassis                   = "redfish/v1/Chassis/{CHASSIS_ID}"
	APIChassisReset                 = "redfish/v1/Chassis/{CHASSIS_ID}/Actions/Oem/NvidiaChassis.Reset"
	APIGetNetworkDeviceFunctions    = "redfish/v1/Chassis/Card1/NetworkAdapters/NvidiaNetworkAdapter/NetworkDeviceFunctions/{PF_ID}"
	APIGetNetworkDeviceFunctionsBF4 = "redfish/v1/Chassis/BlueField_0/NetworkAdapters/BlueField_NIC_0/NetworkDeviceFunctions/{PF_ID}"
	APIRootService                  = "redfish/v1"
	APISystemRoot                   = APIRootService + "/Systems/{SYSTEM_ID}"
	APISecureBoot                   = APISystemRoot + "/SecureBoot"
	APIResetSystem                  = APISystemRoot + "/Actions/ComputerSystem.Reset"
	// APISOCForceReset resets the SoC/NIC without waiting for host PERST.
	// Used for hostless (CMX) reboot so NVConfig takes effect.
	APISOCForceReset = APISystemRoot + "/Oem/Nvidia/SOC.ForceReset"
	// APIGetSELEntries is the Redfish System Event Log entries collection. The BMC
	// records sensor-threshold events (e.g., 12V_ATX low) here, which we surface
	// as best-effort hints when an install task fails.
	// Reference: https://docs.nvidia.com/networking/display/bfswtroubleshooting/bmc
	APIGetSELEntries = APISystemRoot + "/LogServices/SEL/Entries"

	// APIHostPrivilegeConfigSettings is the Settings URI for host privilege configuration (BF4).
	APIHostPrivilegeConfigSettings = APIRootService + "/Chassis/BlueField_0/NetworkAdapters/BlueField_NIC_0/Oem/Nvidia/HostPrivilegeConfig/Settings"

	// CATrustBundleConfigMap is the ConfigMap containing trust bundle PEM set.
	CATrustBundleConfigMap = "dpf-ca-trust-bundle"
	// CATrustBundleKey is the data key containing PEM certificate bundle.
	CATrustBundleKey = "ca.crt"
	// Issuer is a cert-manager Issuer deployed by DPF
	Issuer = "dpf-provisioning-issuer"
	// ClientCertSecret and ClientCertSecretBF4 are created by the cert-manager Certificates deployed
	// by DPF and mounted into the provisioning controller at the client-cert directory (see
	// DefaultClientCertDir and the --redfish-client-cert-dir flag). The controller reads the client
	// key pair from those mounted files, not from the Kubernetes API.
	ClientCertSecret    = "dpf-provisioning-redfish-client-secret"
	ClientCertSecretBF4 = "dpf-provisioning-redfish-client-secret-bf4"
)

const (
	BF3BMCUser = "root"
	BF4BMCUser = "admin"
	// BF4ServiceUser is the BF4 ssh-only account. It has no Redfish session of its own, but its
	// password is settable at the Redfish account path on firmware that exposes it.
	BF4ServiceUser       = "service"
	BMCPasswordSecret    = "bmc-shared-password"
	BMCSharedPasswordKey = "password"
	BMCDefaultPassword   = "0penBmc"
	httpsPrefix          = "https://"
)

// VersionInfo contains the version information responded by RedFish API
type VersionInfo struct {
	Version string
}

// TaskInfo contains the task information responded by RedFish API
type TaskInfo struct {
	ID         string `json:"Id,omitempty"`
	TaskState  string
	TaskStatus string
}

// TaskProgress contains the task progress information responded by RedFish API
type TaskProgress struct {
	Messages        []map[string]interface{}
	PercentComplete int
	TaskState       string
	TaskStatus      string
}

// SELEntry is a single entry from the BMC System Event Log
// (LogServices/SEL/Entries). Only the fields we actually consume are typed;
// other fields (Severity, Created, etc.) are decoded by JSON tags but unused
// today. Reference:
// https://docs.nvidia.com/networking/display/bfswtroubleshooting/bmc
type SELEntry struct {
	ID          string   `json:"Id"`
	Created     string   `json:"Created"`
	Severity    string   `json:"Severity"`
	Message     string   `json:"Message"`
	MessageID   string   `json:"MessageId"`
	MessageArgs []string `json:"MessageArgs"`
	Resolution  string   `json:"Resolution"`
}

// SELEntries is the wrapper for the LogServices/SEL/Entries collection.
type SELEntries struct {
	Members []SELEntry `json:"Members"`
}

// SecureBootState represents the Secure Boot enabled/disabled state.
// Corresponds to Redfish SecureBootCurrentBoot property.
type SecureBootState string

const (
	// SecureBootStateEnabled indicates Secure Boot was active on the current boot.
	SecureBootStateEnabled SecureBootState = "Enabled"
)

// SecureBootInfo represents the Redfish SecureBoot resource.
// Field names match DMTF Redfish specification for traceability.
// See: https://redfish.dmtf.org/schemas/v1/SecureBoot.v1_1_0.json
type SecureBootInfo struct {
	// SecureBootCurrentBoot indicates the current boot's Secure Boot state.
	// This is read-only and reflects the actual hardware state.
	SecureBootCurrentBoot SecureBootState `json:"SecureBootCurrentBoot"`

	// SecureBootEnable controls whether Secure Boot will be active on next boot.
	// This is the persistent firmware setting that can be configured.
	SecureBootEnable bool `json:"SecureBootEnable"`
}

// IsCurrentlyActive returns true if Secure Boot is active on the current boot.
func (s *SecureBootInfo) IsCurrentlyActive() bool {
	return s.SecureBootCurrentBoot == SecureBootStateEnabled
}

// ResetRequest for DPU ARM restart operations
type ResetRequest struct {
	ResetType string `json:"ResetType"` // "ForceRestart", "GracefulRestart", "PowerCycle"
}

// Bios information from Redfish API
type Bios struct {
	Attributes BiosAttributes
}

type BiosAttributes struct {
	HostPrivilegeLevel HostPrivilegeLevelType
	NicMode            NicModeType
}

type HostPrivilegeLevelType string

const (
	Privileged HostPrivilegeLevelType = "Privileged"
	Restricted HostPrivilegeLevelType = "Restricted"
)

type NicModeType string

const (
	DpuMode NicModeType = "DpuMode"
	NicMode NicModeType = "NicMode"
)

// ExtendedInfo contains the information responded by RedFish API
type ExtendedInfo struct {
	MessageExtendedInfo []MessageExtendedInfo `json:"@Message.ExtendedInfo,omitempty"`
}

type Managers struct {
	Members []Manager `json:"Members,omitempty"`
}

type Manager struct {
	ODataID string `json:"@odata.id,omitempty"`
}

type odataRef struct {
	ODataID string `json:"@odata.id,omitempty"`
}

// TruststoreCollection is the Redfish truststore certificate collection response.
type TruststoreCollection struct {
	Members []odataRef `json:"Members,omitempty"`
}

// TruststoreCertificate is the Redfish certificate resource response.
type TruststoreCertificate struct {
	CertificateString string `json:"CertificateString,omitempty"`
}

// TruststoreCert contains truststore member URI and certificate fingerprint.
type TruststoreCert struct {
	URI         string
	Fingerprint string
}

type Systems struct {
	Members []System `json:"Members,omitempty"`
}

type System struct {
	ODataID string `json:"@odata.id,omitempty"`
}

type Settings struct {
	Boot BootSettings `json:"Boot,omitempty"`
}

type BootSettings struct {
	BootSourceOverrideTarget  string   `json:"BootSourceOverrideTarget,omitempty"`
	BootSourceOverrideMode    string   `json:"BootSourceOverrideMode,omitempty"`
	BootSourceOverrideEnabled string   `json:"BootSourceOverrideEnabled,omitempty"`
	BootOrder                 []string `json:"BootOrder,omitempty"`
	AutomaticRetryConfig      string   `json:"AutomaticRetryConfig,omitempty"`
}

type VirtualMedia struct {
	Inserted   bool     `json:"Inserted"`
	Image      string   `json:"Image"`
	MediaTypes []string `json:"MediaTypes"`
}

// MessageExtendedInfo contains the Message.ExtendedInfo responded by RedFish API
type MessageExtendedInfo struct {
	ODataType       string `json:"@odata.type,omitempty"`
	Message         string
	MessageArgs     json.RawMessage
	MessageID       string `json:"MessageId,omitempty"`
	MessageSeverity string
	Resolution      string
}

// RedfishError is the standard DMTF Redfish error response body, carrying the
// error payload under the top-level "error" key. ExtendedInfo reuses
// MessageExtendedInfo so all Redfish message decoding shares one schema.
type RedfishError struct {
	Error struct {
		Code         string                `json:"code"`
		Message      string                `json:"message"`
		ExtendedInfo []MessageExtendedInfo `json:"@Message.ExtendedInfo"`
	} `json:"error"`
}

// ErrorMessages parses a Redfish error response body and returns its
// human-readable messages. A single error may carry several
// @Message.ExtendedInfo entries; each entry's Message (+ MessageId, + BMC
// Resolution when present) is returned in order. Falls back to the top-level
// error.message. Returns nil when body is not a parseable Redfish error, so
// callers can fall back to the raw body.
func ErrorMessages(body string) []string {
	var re RedfishError
	if json.Unmarshal([]byte(body), &re) != nil {
		return nil
	}
	msgs := make([]string, 0, len(re.Error.ExtendedInfo))
	for _, info := range re.Error.ExtendedInfo {
		if info.Message == "" {
			continue
		}
		m := info.Message
		if info.MessageID != "" {
			m += fmt.Sprintf(" (%s)", info.MessageID)
		}
		if info.Resolution != "" {
			m += ". BMC Resolution: " + info.Resolution
		}
		msgs = append(msgs, m)
	}
	if len(msgs) == 0 && re.Error.Message != "" {
		msgs = append(msgs, re.Error.Message)
	}
	if len(msgs) == 0 {
		return nil
	}
	return msgs
}

// ProductSpecInfo contains the product specification information responded by RedFish API
type ProductSpecInfo struct {
	Description *string      `json:"Description,omitempty"`
	Mode        *NicModeType `json:"Mode,omitempty"`
}

type RootServiceInfo struct {
	Product string `json:"Product,omitempty"`
}

func (r *RootServiceInfo) IsBF4() bool {
	return strings.Contains(strings.ToUpper(r.Product), "B4") || strings.Contains(strings.ToUpper(r.Product), "BLUEFIELD-4")
}

type SystemInfo struct {
	AssetTag     *string      `json:"AssetTag,omitempty"`
	BootProgress BootProgress `json:"BootProgress,omitempty"`
	// PowerState is the Redfish ComputerSystem power state. On BF4 a DPU Arm
	// that has completed a graceful shutdown reports "Paused" ("Off" is another
	// possible off value). Used to detect that the DPU Arm has powered off.
	PowerState string `json:"PowerState,omitempty"`
	// Status carries the Redfish resource state. Status.State == "StandbyOffline"
	// is the purpose-built signal that the DPU Arm OS is down/offline.
	Status SystemStatus `json:"Status,omitempty"`
}

type SystemStatus struct {
	State  string `json:"State,omitempty"`
	Health string `json:"Health,omitempty"`
}

type BootProgress struct {
	OemLastState string `json:"OemLastState,omitempty"`
	LastState    string `json:"LastState,omitempty"`
}

// Client is a Redfish client
type Client struct {
	*resty.Client
	readOnce sync.Once
	reader   *resty.Client
	IsBF4    bool
}

type BmcManager struct {
	DateTime        string `json:"DateTime,omitempty"`
	FirmwareVersion string `json:"FirmwareVersion,omitempty"`
	LastResetTime   string `json:"LastResetTime,omitempty"`
}

func (c *Client) GetBmcManager(ctx context.Context) (*resty.Response, *BmcManager, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	managerID, err := getBMCManagerID(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	return read[BmcManager](ctx, c, strings.Replace(APIGetBMCManager, "{MANAGER_ID}", *managerID, 1))
}

// ChangeBMCPassword sets newPassword on every BMC account DPF manages, so that no managed account
// keeps the factory default password. On BF3 that is the Redfish user (root); on BF4 it is the
// Redfish user (admin) and the ssh-only service account.
//
// The order is load-bearing in both directions. A BMC still holding the factory default password
// grants a session that may only change its own account's password, which rules out reaching the
// service account first. And once the Redfish user's password has changed, this client's
// credentials are stale, so the service account is patched over a freshly authenticated client.
//
// The Redfish user's response is returned so callers keep their existing status handling.
// For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/connecting+to+bmc+interfaces#src-704886267_ConnectingtoBMCInterfaces-ChangingDefaultPassword
func (c *Client) ChangeBMCPassword(ctx context.Context, newPassword string, user string) (*resty.Response, *ExtendedInfo, error) {
	resp, info, err := c.SetRedfishUserPassword(user, newPassword)
	if err != nil || !PasswordChangeAccepted(resp) || !c.IsBF4 {
		return resp, info, err
	}

	// BF4 handling - re-authenticate as the Redfish user after changing its password, and set the
	// service account password over the freshly authenticated client.
	reAuthenticated, err := NewBasicAuthClient(ctx, c.BaseURL, user, newPassword)
	if err != nil {
		return resp, info, fmt.Errorf("failed to re-authenticate as %q after changing its password: %w", user, err)
	}
	defer reAuthenticated.CloseIdleConnections()
	if err := reAuthenticated.SetServiceAccountPassword(ctx, newPassword); err != nil {
		return resp, info, err
	}
	return resp, info, nil
}

// SetRedfishUserPassword changes the password of the Redfish user only (root on BF3, admin on BF4),
// leaving other managed accounts alone. Callers that want every managed account hardened should use
// ChangeBMCPassword instead.
func (c *Client) SetRedfishUserPassword(user, newPassword string) (*resty.Response, *ExtendedInfo, error) {
	return c.patchAccountPassword(user, newPassword)
}

// SetServiceAccountPassword sets the password of the BF4 ssh-only service account. It is a no-op on
// BF3, which has no such account, and tolerates a 404 on BF4 so DPF keeps working against BMC
// firmware that predates Redfish support for that account.
func (c *Client) SetServiceAccountPassword(ctx context.Context, newPassword string) error {
	if !c.IsBF4 {
		return nil
	}

	resp, _, err := c.patchAccountPassword(BF4ServiceUser, newPassword)
	if err != nil {
		return fmt.Errorf("failed to set the password of BMC account %q: %w", BF4ServiceUser, err)
	}
	if PasswordChangeAccepted(resp) {
		log.FromContext(ctx).Info("SetServiceAccountPassword: success", "account", BF4ServiceUser)
		return nil
	}
	switch resp.StatusCode() {
	case http.StatusNotFound:
		log.FromContext(ctx).Info("BMC does not expose the ssh-only service account over Redfish; leaving it untouched",
			"account", BF4ServiceUser)
		return nil
	default:
		return AccountPasswordError(BF4ServiceUser, resp)
	}
}

// PasswordChangeAccepted reports whether the BMC applied an account PATCH. BMCs answer 200 with a
// body in practice, but a PATCH that returns nothing is just as validly a 204, and every password
// path treats the two the same.
func PasswordChangeAccepted(resp *resty.Response) bool {
	return resp.StatusCode() == http.StatusOK || resp.StatusCode() == http.StatusNoContent
}

// patchAccountPassword PATCHes a single Redfish account with a new password. A rejection can carry
// a body that is not a Redfish payload, so a decode failure yields a nil ExtendedInfo instead of an
// error: the caller decides on the status code.
func (c *Client) patchAccountPassword(user, newPassword string) (*resty.Response, *ExtendedInfo, error) {
	resp, err := c.Client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{
			"Password": newPassword,
		}).
		Patch(strings.Replace(APIChangePasswd, "{USER}", user, 1))
	if err != nil {
		return nil, nil, err
	}
	var info ExtendedInfo
	if json.Unmarshal(resp.Body(), &info) != nil {
		return resp, nil, nil
	}
	return resp, &info, nil
}

// AccountPasswordError renders a BMC rejection of a password change as an actionable error naming
// the account and quoting the BMC's own reason (typically an account policy violation), instead of
// a bare status code.
func AccountPasswordError(user string, resp *resty.Response) error {
	if msgs := ErrorMessages(string(resp.Body())); len(msgs) > 0 {
		return fmt.Errorf("BMC rejected the password for account %q: %s", user, strings.Join(msgs, "; "))
	}
	return fmt.Errorf("BMC rejected the password for account %q: unexpected BMC status: %s", user, resp.Status())
}

// InstallCert installs the given certificate, making the certificate trusted by BMC
func (c *Client) InstallCert(ctx context.Context, caCert string) (*resty.Response, *ExtendedInfo, error) {
	managerID, err := getBMCManagerID(ctx, c)
	if err != nil {
		return nil, nil, err
	}

	caCertJSON := map[string]interface{}{
		"CertificateString": caCert,
		"CertificateType":   "PEM",
	}
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(caCertJSON).
			Post(strings.Replace(APIInstallCert, managerIDPlaceholder, *managerID, 1))
	})
}

// ListTruststoreCerts lists BMC truststore certificates and computes SHA-256
// fingerprints from certificate raw bytes.
func (c *Client) ListTruststoreCerts(ctx context.Context) ([]TruststoreCert, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	managerID, err := getBMCManagerID(ctx, c)
	if err != nil {
		return nil, err
	}

	collectionURI := strings.Replace(APIInstallCert, "{MANAGER_ID}", *managerID, 1)
	_, collection, err := read[TruststoreCollection](ctx, c, collectionURI)
	if err != nil {
		return nil, err
	}

	ret := make([]TruststoreCert, 0, len(collection.Members))
	for _, member := range collection.Members {
		uri := normalizeRedfishURI(member.ODataID)
		if uri == "" {
			continue
		}
		_, certResource, err := read[TruststoreCertificate](ctx, c, uri)
		if err != nil {
			return nil, err
		}

		fingerprint, err := certificateFingerprintSHA256(certResource.CertificateString)
		if err != nil {
			return nil, fmt.Errorf("parse truststore certificate %q: %w", uri, err)
		}
		ret = append(ret, TruststoreCert{
			URI:         uri,
			Fingerprint: fingerprint,
		})
	}
	return ret, nil
}

// DeleteTruststoreCert deletes a truststore certificate at the given Redfish URI.
func (c *Client) DeleteTruststoreCert(certURI string) (*resty.Response, *ExtendedInfo, error) {
	resp, err := c.Client.R().Delete(normalizeRedfishURI(certURI))
	if err != nil {
		return nil, nil, err
	}
	if len(resp.Body()) == 0 {
		return resp, nil, nil
	}

	info := &ExtendedInfo{}
	if err := json.Unmarshal(resp.Body(), info); err != nil {
		return resp, nil, err
	}
	return resp, info, nil
}

// ReplaceCACert replaces the trusted CA certificate with the given caCert
func (c *Client) ReplaceCACert(ctx context.Context, caCert string) (*resty.Response, *ExtendedInfo, error) {
	managerID, err := getBMCManagerID(ctx, c)
	if err != nil {
		return nil, nil, err
	}

	caCertJSON := map[string]interface{}{
		"CertificateString": caCert,
		"CertificateType":   "PEM",
		"CertificateUri": map[string]interface{}{
			"@odata.id": fmt.Sprintf("/redfish/v1/Managers/%s/Truststore/Certificates/1", *managerID),
		},
	}
	return c.ReplaceCert(caCertJSON)
}

// ReplaceServerCert replaces the server certificate used by BMC APIs
func (c *Client) ReplaceServerCert(ctx context.Context, srvCert string) (*resty.Response, *ExtendedInfo, error) {
	managerID, err := getBMCManagerID(ctx, c)
	if err != nil {
		return nil, nil, err
	}

	srvCertJSON := map[string]interface{}{
		"CertificateString": srvCert,
		"CertificateType":   "PEM",
		"CertificateUri": map[string]interface{}{
			"@odata.id": fmt.Sprintf("/redfish/v1/Managers/%s/NetworkProtocol/HTTPS/Certificates/1", *managerID),
		},
	}
	return c.ReplaceCert(srvCertJSON)
}

// ServerCertInfo carries the certificate the BMC is currently serving for HTTPS.
type ServerCertInfo struct {
	// CertificateString is the PEM-encoded certificate the BMC serves on its HTTPS endpoint.
	CertificateString string `json:"CertificateString,omitempty"`
}

// GetServerCert fetches the certificate the BMC is actually serving on its HTTPS endpoint.
// It is used for cold-start backfill of the recorded expiry and to detect out-of-band
// changes to the BMC server certificate.
func (c *Client) GetServerCert(ctx context.Context) (*resty.Response, *ServerCertInfo, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	managerID, err := getBMCManagerID(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	return read[ServerCertInfo](ctx, c, strings.Replace(APIServerCert, managerIDPlaceholder, *managerID, 1))
}

// ReplaceCert replaces existing certificate. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/redfish+certificate+management#src-704886301_RedfishCertificateManagement-third
func (c *Client) ReplaceCert(body map[string]interface{}) (*resty.Response, *ExtendedInfo, error) {
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(body).
			Post(APIReplaceCert)
	})
}

type CSRInfo struct {
	CSRString string
}

// GenerateCSR generates a server CSR that can be signed by external CA. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/redfish+certificate+management#src-704886301_RedfishCertificateManagement-forth
func (c *Client) GenerateCSR(ctx context.Context, cn string) (*resty.Response, *CSRInfo, error) {
	managerID, err := getBMCManagerID(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	urlString, err := url.JoinPath("https://", cn, APIGenerateCSR)
	if err != nil {
		return nil, nil, err
	}
	CSRRequest := map[string]interface{}{
		"CommonName":         cn,
		"City":               "Santa Clara",
		"Country":            "US",
		"Organization":       "NVIDIA",
		"OrganizationalUnit": "NBU",
		"State":              "CA",
		"CertificateCollection": map[string]interface{}{
			"@odata.id": fmt.Sprintf("/redfish/v1/Managers/%s/NetworkProtocol/HTTPS/Certificates", *managerID),
		},
		"AlternativeNames": []string{
			fmt.Sprintf("IP: %s", cn),
			"DNS: localhost",
			"IP: 127.0.0.1",
		},
	}
	return do[CSRInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(CSRRequest).
			Post(urlString)
	})
}

// EnableMTLS activates mTLS on BMC
func (c *Client) EnableMTLS() (*resty.Response, *ExtendedInfo, error) {
	reqBody := `{"Oem": {"OpenBMC": {"AuthMethods": {"TLS": true}}}}`
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetBody(reqBody).
			Patch(APIEnableMTLS)
	})
}

// CheckBMCFirmware fetches BMC firmware version. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/cec+and+bmc+firmware+operations#src-704886294_CECandBMCFirmwareOperations-FetchingRunningBMCFirmwareVersion
func (c *Client) CheckBMCFirmware(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	bmcFwID := "BMC_Firmware"
	if c.IsBF4 {
		bmcFwID = "BlueField_FW_BMC_0"
	}

	url := strings.Replace(APICheckBMCFW, "{BMC_FW_ID}", bmcFwID, 1)

	return read[VersionInfo](ctx, c, url)
}

func (c *Client) CheckDpuBoardFW(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	return read[VersionInfo](ctx, c, APICheckDpuBoardFW)
}

func (c *Client) CheckBMCEROTFW(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	return read[VersionInfo](ctx, c, APICheckBMCEROTFW)
}

func (c *Client) GetSystem(ctx context.Context) (*resty.Response, *SystemInfo, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, nil, err
	}

	return read[SystemInfo](ctx, c, strings.Replace(APIGetSystem, "{SYSTEM_ID}", systemID, 1))
}

// CheckDPUNIC fetches DPU NIC version
func (c *Client) CheckDPUNIC(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	dpuNicID := "DPU_NIC"
	if c.IsBF4 {
		dpuNicID = "BlueField_FW_NIC_0"
	}

	url := strings.Replace(APICheckDPUNIC, "{DPU_NIC_ID}", dpuNicID, 1)

	return read[VersionInfo](ctx, c, url)
}

// CheckDPUOS fetches DPU OS version
func (c *Client) CheckDPUOS(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	return read[VersionInfo](ctx, c, APICheckDPUOS)
}

func (c *Client) CheckDPUUEFI(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	uefiID := "DPU_UEFI"
	if c.IsBF4 {
		uefiID = "BlueField_FW_CPU_0"
	}

	url := strings.Replace(APICheckDPUUEFI, "{DPU_UEFI_ID}", uefiID, 1)

	return read[VersionInfo](ctx, c, url)
}

func (c *Client) CheckDPUBSP(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	return read[VersionInfo](ctx, c, APICheckDPUBSP)
}

// UpdateBMCFirmware using HttpPushUri method. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/cec+and+bmc+firmware+operations#src-704886294_CECandBMCFirmwareOperations-UpdatingBMCFirmware
func (c *Client) UpdateBMCFirmware(fwFile *os.File) (*resty.Response, *TaskInfo, error) {
	return do[TaskInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetBody(fwFile).
			SetHeader("Content-Type", "application/octet-stream").
			Post(APIUpdateFW)
	})
}

// InstallBFB installs BFB to DPU via BMC. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/deploying+bluefield+software+using+bfb+from+bmc
func (c *Client) InstallBFB(imageURI string) (*resty.Response, *TaskInfo, error) {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	reqBody := map[string]interface{}{
		"TransferProtocol": "HTTPS",
		"ImageURI":         imageURI,
		"Targets":          []string{"redfish/v1/UpdateService/FirmwareInventory/DPU_OS"},
	}
	return do[TaskInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetHeaders(headers).
			SetBody(reqBody).
			Post(APIInstallBFB)
	})
}

func (c *Client) GetManagers(ctx context.Context) (*resty.Response, *Managers, error) {
	resp, managers, err := read[Managers](ctx, c, APIGetManagers)
	if err != nil {
		return resp, nil, fmt.Errorf("get managers from %q failed: %w", APIGetManagers, err)
	}
	return resp, managers, nil
}

func getBMCManagerID(ctx context.Context, c *Client) (*string, error) {
	_, managers, err := c.GetManagers(ctx)
	if err != nil {
		return nil, err
	}
	return findBMCManagerID(managers)
}

// findBMCManagerID selects the BMC member from the managers collection.
func findBMCManagerID(managers *Managers) (*string, error) {
	if managers == nil || len(managers.Members) == 0 {
		return nil, fmt.Errorf("no managers found")
	}
	var managerID string
	for _, manager := range managers.Members {
		if strings.Contains(strings.ToLower(manager.ODataID), "bmc") {
			managerID = manager.ODataID[strings.LastIndex(manager.ODataID, "/")+1:]
			break
		}
	}
	if managerID == "" {
		return nil, fmt.Errorf("no BMC manager found")
	}
	return &managerID, nil
}

func normalizeRedfishURI(uri string) string {
	if uri == "" {
		return uri
	}
	return strings.TrimPrefix(uri, "/")
}

func certificateFingerprintSHA256(certPEM string) (string, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return "", fmt.Errorf("no PEM block found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cert.Raw)
	return fmt.Sprintf("%x", sum), nil
}

// FactoryResetBMC resets BMC to factory defaults. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2504/factory+reset+bmc
func (c *Client) FactoryResetBMC(ctx context.Context) (*resty.Response, *ExtendedInfo, error) {
	managerID, err := getBMCManagerID(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	reqBody := `{"ResetToDefaultsType": "ResetAll"}`
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetBody(reqBody).
			Post(strings.Replace(APIFactoryResetBMC, managerIDPlaceholder, *managerID, 1))
	})
}

// ResetBMC resets BMC. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/cec+and+bmc+firmware+operations#src-704886294_CECandBMCFirmwareOperations-UpdatingBMC
func (c *Client) ResetBMC(ctx context.Context) (*resty.Response, *ExtendedInfo, error) {
	managerID, err := getBMCManagerID(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	reqBody := `{"ResetType": "GracefulRestart"}`
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetBody(reqBody).
			Post(strings.Replace(APIResetBMC, managerIDPlaceholder, *managerID, 1))
	})
}

// CheckTaskProgress fetches progress of the given task
func (c *Client) CheckTaskProgress(ctx context.Context, taskID string) (*resty.Response, *TaskProgress, error) {
	return read[TaskProgress](ctx, c, fmt.Sprintf("%s/%s", APICheckProgress, taskID))
}

// GetSELEntries fetches the BMC System Event Log entries collection. Used by
// the OS Installing failure paths to surface sensor-threshold events (e.g.
// 12V_ATX low) as operator hints. Best-effort: callers must tolerate errors
// and never propagate them. The context is propagated to the HTTP layer so
// callers can cap the call duration on unreachable BMCs.
func (c *Client) GetSELEntries(ctx context.Context) (*resty.Response, *SELEntries, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	url := strings.Replace(APIGetSELEntries, "{SYSTEM_ID}", systemID, 1)
	return read[SELEntries](ctx, c, url)
}

// DisableHostRshim disables host RShim. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/nic+subsystem+management#src-704886345_NICSubsystemManagement-DisablingHostRShim
func (c *Client) DisableHostRshim() (*resty.Response, *ExtendedInfo, error) {
	reqBody := `{"HostRshim":"Disabled"}`
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetBody(reqBody).
			Post(APIDisableHostRshim)
	})
}

// SetHostPrivilegeRestricted sets PrivilegeMode to Restricted via the HostPrivilegeConfig/Settings resource.
// Currently only BF4 is supported. BF3 uses a different path:
//
//	redfish/v1/Chassis/Card1/NetworkAdapters/NvidiaNetworkAdapter/Oem/Nvidia/HostPrivilegeConfig/Settings
func (c *Client) SetHostPrivilegeRestricted() (*resty.Response, *ExtendedInfo, error) {
	payload := map[string]interface{}{
		"PrivilegeMode": "Restricted",
	}
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(payload).
			Patch(APIHostPrivilegeConfigSettings)
	})
}

// EnableBMCRShim enables the RShim on BMC OS. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/rshim+over+usb#src-704886337_RShimOverUSB-EnablingRShimonBlueFieldBMC
func (c *Client) EnableBMCRShim() (*resty.Response, *ExtendedInfo, error) {
	reqBody := `{"BmcRShim": {"BmcRShimEnabled":true}}`
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetBody(reqBody).
			Patch(APIEnableBMCRshim)
	})
}

type BMCRShimOem struct {
	BmcRShim struct {
		BmcRShimEnabled *bool `json:"BmcRShimEnabled"`
	} `json:"BmcRShim"`
}

// GetBMCRShimEnabled GETs Managers/Bluefield_BMC/Oem/Nvidia and returns BmcRShimEnabled.
func (c *Client) GetBMCRShimEnabled(ctx context.Context) (bool, *resty.Response, error) {
	resp, oem, err := read[BMCRShimOem](ctx, c, APIEnableBMCRshim)
	if err != nil {
		return false, resp, err
	}
	if oem.BmcRShim.BmcRShimEnabled == nil {
		return false, resp, fmt.Errorf("BmcRShim.BmcRShimEnabled missing from Redfish response")
	}
	return *oem.BmcRShim.BmcRShimEnabled, resp, nil
}

// ChassisAssetTagUnavailable is the BMC sentinel when chassis AssetTag (PSID) is not set.
const ChassisAssetTagUnavailable = "N/A"

// ChassisInfo contains the part number information responded by RedFish API
type ChassisInfo struct {
	AssetTag     string                 `json:"AssetTag,omitempty"`
	Model        string                 `json:"Model"`
	PartNumber   string                 `json:"PartNumber"`
	SerialNumber string                 `json:"SerialNumber"`
	Oem          map[string]interface{} `json:"Oem"`
}

var blueFieldRegex = regexp.MustCompile(`bluefield[- ]?(\d+)`)

func (c *Client) GetPSID(ctx context.Context) (string, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	if !c.IsBF4 {
		_, versionInfo, err := c.CheckDpuBoardFW(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to check DPU board firmware: %w", err)
		}
		return versionInfo.Version, nil
	}

	_, systemInfo, err := c.GetSystem(ctx)
	if err != nil {
		return "", err
	}

	if systemInfo.AssetTag != nil {
		return *systemInfo.AssetTag, nil
	}

	// TODO: Remove this once initial FW would start from 0.8
	_, chassisInfo, err := c.GetChassis(ctx)
	if err != nil {
		return "", err
	}
	if chassisInfo.AssetTag != ChassisAssetTagUnavailable {
		return chassisInfo.AssetTag, nil
	}
	return "", fmt.Errorf("AssetTag is not available")
}

func (c *ChassisInfo) GetBlueFieldVersion() provisioningv1.DPUType {
	// Extract BlueField version number from model string
	matches := blueFieldRegex.FindStringSubmatch(strings.ToLower(c.Model))
	if len(matches) >= 2 {
		switch matches[1] {
		case "2":
			return provisioningv1.DPUTypeBlueField2
		case "3":
			return provisioningv1.DPUTypeBlueField3
		case "4":
			return provisioningv1.DPUTypeBlueField4
		default:
			return provisioningv1.DPUTypeUnknown
		}
	}
	if strings.HasPrefix(strings.ToUpper(c.Model), "B4") {
		return provisioningv1.DPUTypeBlueField4
	}
	return provisioningv1.DPUTypeUnknown
}

// GetChassis fetches part number of DPU
func (c *Client) GetChassis(ctx context.Context) (*resty.Response, *ChassisInfo, error) {
	chassisID := "Card1"
	if c.IsBF4 {
		chassisID = "BlueField_0"
	}

	return read[ChassisInfo](ctx, c, strings.Replace(APIGetChassis, "{CHASSIS_ID}", chassisID, 1))
}

func (c *Client) GetErotChassis(ctx context.Context) (*resty.Response, *ChassisInfo, error) {
	chassisID := "BlueField_ERoT_BMC_0"
	url := strings.Replace(APIGetChassis, "{CHASSIS_ID}", chassisID, 1)
	return read[ChassisInfo](ctx, c, url)
}

func (c *Client) GetSystems(ctx context.Context) (*resty.Response, *Systems, error) {
	return read[Systems](ctx, c, APIGetSystems)
}

func getSystemID(ctx context.Context, c *Client) (string, error) {
	_, systems, err := c.GetSystems(ctx)
	if err != nil {
		return "", err
	}

	return findSystemID(systems)
}

// findSystemID selects the BlueField member from the systems collection.
func findSystemID(systems *Systems) (string, error) {
	for _, system := range systems.Members {
		if strings.Contains(strings.ToLower(system.ODataID), "bluefield") {
			return system.ODataID[strings.LastIndex(system.ODataID, "/")+1:], nil
		}
	}
	return "", fmt.Errorf("no system found")
}

// GetProductDescription fetches product spec of DPU
func (c *Client) GetProductDescription(ctx context.Context) (*resty.Response, *ProductSpecInfo, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	return read[ProductSpecInfo](ctx, c, strings.Replace(APIProductDescription, "{SYSTEM_ID}", systemID, 1))
}

// GetBios returns a Bios information for current DPU
func (c *Client) GetBios(ctx context.Context) (*resty.Response, *Bios, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	return read[Bios](ctx, c, strings.Replace(APIGetBios, "{SYSTEM_ID}", systemID, 1))
}

type NetworkDeviceFunction struct {
	ID             string   `json:"Id"`
	Ethernet       Ethernet `json:"Ethernet"`
	NetDevFuncType string   `json:"NetDevFuncType"` // "Ethernet" or "Infiniband"
}

type Ethernet struct {
	MACAddress          string `json:"MACAddress"`
	PermanentMACAddress string `json:"PermanentMACAddress"`
	MTUSize             int    `json:"MTUSize"`
}

func (c *Client) GetNetworkDeviceFunction(ctx context.Context, pfID string) (*resty.Response, *NetworkDeviceFunction, error) {
	url := APIGetNetworkDeviceFunctions
	if c.IsBF4 {
		url = APIGetNetworkDeviceFunctionsBF4
	}

	url = strings.Replace(url, "{PF_ID}", pfID, 1)
	return read[NetworkDeviceFunction](ctx, c, url)
}

// SetDpuMode returns a Bios information for current DPU
func (c *Client) SetDpuMode(ctx context.Context, desiredMode provisioningv1.DpuModeType) (*resty.Response, error) {
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, err
	}
	var body []byte
	switch desiredMode {
	case provisioningv1.DpuMode:
		body = []byte(`{ "Attributes": {"InternalCPUModel": "Privileged" } }`)
		resp, err := c.Client.R().SetBody(body).Patch(strings.Replace(APISetBiosSettings, "{SYSTEM_ID}", systemID, 1))
		if err != nil {
			return resp, err
		}
		body = []byte(`{"Mode": "DpuMode"}`)
	default:
		return nil, fmt.Errorf("unsupported DPU mode: %s", desiredMode)
	}

	return c.Client.R().SetBody(body).Post(APISetMode)
}

// reqFunc is a function that sends a request
type reqFunc func() (*resty.Response, error)

// RespBody returns the response body as a string, guarding against a nil resp.
func RespBody(resp *resty.Response) string {
	if resp == nil {
		return ""
	}
	return resp.String()
}

// do sends a request and unmarshals the response body into the given type
func do[T any](req reqFunc) (*resty.Response, *T, error) {
	resp, err := req()
	if err != nil {
		return resp, nil, err
	}
	var t T
	if err := json.Unmarshal(resp.Body(), &t); err != nil {
		return resp, nil, fmt.Errorf("failed to decode redfish response: %w (%s)", err, responseDebugSummary(resp))
	}
	return resp, &t, nil
}

func responseDebugSummary(resp *resty.Response) string {
	if resp == nil {
		return "response=nil"
	}

	return fmt.Sprintf("status=%s body_len=%d", resp.Status(), len(resp.Body()))
}

// NewRawClient creates a client to check if the BMC is reachable
// /redfish/v1/ can be accessed without authentication
func NewRawClient(bmcAddress string) (*Client, error) {
	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}
	u, err := url.ParseRequestURI(bmcAddress)
	if err != nil {
		return nil, err
	}
	tlsCfg, err := newRedfishTLSConfig(nil, nil, true, "")
	if err != nil {
		return nil, err
	}
	return &Client{Client: resty.New().SetBaseURL(u.String()).SetTLSClientConfig(tlsCfg)}, nil
}

// GetRootService returns the root service of the BMC
func (c *Client) GetRootService(ctx context.Context) (*resty.Response, *RootServiceInfo, error) {
	return read[RootServiceInfo](ctx, c, APIRootService)
}

// BMCCredentialResult contains the resolved BMC credential information.
type BMCCredentialResult struct {
	Password   string
	SecretName string
}

// ResolveBMCCredential resolves the BMC password to use for a DPUDevice.
// If bmcCredentialSecretName is set, it reads the per-device secret.
// Otherwise it falls back to the shared bmc-shared-password secret.
func ResolveBMCCredential(ctx context.Context, namespace string, bmcCredentialSecretName *string, k8sClient client.Client) (*BMCCredentialResult, error) {
	secretName := BMCPasswordSecret
	if bmcCredentialSecretName != nil && *bmcCredentialSecretName != "" {
		secretName = *bmcCredentialSecretName
	}
	return readPasswordFromSecret(ctx, namespace, secretName, k8sClient)
}

func readPasswordFromSecret(ctx context.Context, namespace, secretName string, k8sClient client.Client) (*BMCCredentialResult, error) {
	nn := types.NamespacedName{Name: secretName, Namespace: namespace}
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, nn, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("credential secret %q not found: %w", secretName, err)
		}
		return nil, fmt.Errorf("failed to get credential secret %q: %w", secretName, err)
	}
	passwd := string(secret.Data[BMCSharedPasswordKey])
	if passwd == "" {
		return nil, fmt.Errorf("password key is empty or missing in credential secret %q", secretName)
	}
	return &BMCCredentialResult{Password: passwd, SecretName: secretName}, nil
}

// ErrBMCPasswordRejected is returned when the BMC answered an authentication attempt and turned it
// down. It is what separates a wrong password from a BMC that never answered at all, so that a
// caller does not report an unreachable BMC as a credential problem.
var ErrBMCPasswordRejected = errors.New("the default BMC password has been changed and the given password is wrong")

const unexpectedBMCStatusFmt = "unexpected BMC status: %s"

func unexpectedBMCStatus(resp *resty.Response) error {
	return fmt.Errorf(unexpectedBMCStatusFmt, resp.Status())
}

// PasswordChangeRequired reports whether the BMC accepted the credentials but is refusing further
// access until the account password is changed. BlueField BMCs enter this state after a factory
// reset while still holding the factory default password: /redfish/v1 answers, but Managers,
// UpdateService, and FirmwareInventory return 403 with MessageId PasswordChangeRequired.
func PasswordChangeRequired(resp *resty.Response) bool {
	if resp == nil || resp.StatusCode() != http.StatusForbidden {
		return false
	}
	for _, id := range requestError(resp, nil).MessageIDs {
		if strings.HasSuffix(id, ".PasswordChangeRequired") {
			return true
		}
	}
	return false
}

// VerifyBMCCredential tries to authenticate to the BMC with the given password,
// attempting BF3 (root) first, then falling back to BF4 (admin).
// It returns the authenticated client and the BMC username that succeeded.
//
// The probe is GET /redfish/v1/Managers: that is what InitPassword uses, and unlike
// FirmwareInventory it is the right signal after a factory reset. A 200 means the password is
// fully usable. A 403 PasswordChangeRequired also means the password is correct — the BMC is
// holding the factory default and demanding a change before any other Redfish access — so the
// credential check succeeds and the caller (password hardening) clears the requirement.
//
// A password the BMC rejects yields ErrBMCPasswordRejected; every other failure means the BMC gave
// no usable answer.
func VerifyBMCCredential(ctx context.Context, bmcAddress, password string) (*Client, string, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}

	for _, user := range []string{BF3BMCUser, BF4BMCUser} {
		c, err := NewBasicAuthClient(ctx, bmcAddress, user, password)
		if err != nil {
			return nil, "", err
		}
		resp, _, err := c.GetManagers(ctx)
		if err != nil && !HasHTTPStatus(err, http.StatusUnauthorized) && !HasHTTPStatus(err, http.StatusForbidden) {
			c.CloseIdleConnections()
			return nil, "", err
		}
		switch resp.StatusCode() {
		case http.StatusOK:
			return c, user, nil
		case http.StatusForbidden:
			if PasswordChangeRequired(resp) {
				return c, user, nil
			}
			c.CloseIdleConnections()
			return nil, "", requestError(resp, nil)
		case http.StatusUnauthorized:
			c.CloseIdleConnections()
			continue
		}
	}
	return nil, "", ErrBMCPasswordRejected
}

// InitPassword resolves the BMC password and authenticates to the BMC.
func InitPassword(ctx context.Context, bmcAddress string, namespace string, bmcCredentialSecretName *string, k8sClient client.Client) (*Client, error) {
	readCtx, cancel := ReadContext(ctx)
	defer cancel()
	cred, err := ResolveBMCCredential(readCtx, namespace, bmcCredentialSecretName, k8sClient)
	if err != nil {
		return nil, err
	}
	passwd := cred.Password

	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}

	user, err := bmcUserForAddress(readCtx, bmcAddress)
	if err != nil {
		return nil, err
	}

	client, err := NewBasicAuthClient(readCtx, bmcAddress, user, passwd)
	if err != nil {
		return nil, err
	}
	resp, _, err := client.GetManagers(readCtx)
	if err != nil && !HasHTTPStatus(err, http.StatusUnauthorized) &&
		(!HasHTTPStatus(err, http.StatusForbidden) || !PasswordChangeRequired(resp)) {
		client.CloseIdleConnections()
		return nil, err
	}
	result, err := completeInitPassword(ctx, client, resp, bmcAddress, user, passwd)
	if result != client {
		client.CloseIdleConnections()
	}
	return result, err
}

func bmcUserForAddress(ctx context.Context, bmcAddress string) (string, error) {
	rootClient, err := NewRawClient(bmcAddress)
	if err != nil {
		return "", err
	}
	defer rootClient.CloseIdleConnections()
	_, rootServiceInfo, err := rootClient.GetRootService(ctx)
	if err != nil {
		return "", err
	}
	if rootServiceInfo.IsBF4() {
		log.FromContext(ctx).Info("Assuming BF4 model, BMC user changed to admin")
		return BF4BMCUser, nil
	}
	return BF3BMCUser, nil
}

// completeInitPassword finishes InitPassword based on the Managers probe response: harden when the
// BMC still holds the factory default (401 or 403 PasswordChangeRequired), ensure the service
// account matches when the target password already works, or fail on unexpected status.
func completeInitPassword(ctx context.Context, client *Client, resp *resty.Response, bmcAddress, user, passwd string) (*Client, error) {
	switch resp.StatusCode() {
	case http.StatusUnauthorized:
		if err := changeDefaultPassword(ctx, bmcAddress, user, passwd); err != nil {
			return nil, err
		}
		return client, nil
	case http.StatusForbidden:
		return completeInitPasswordForbidden(ctx, resp, bmcAddress, user, passwd)
	case http.StatusOK:
		// The target password already authenticates, so the Redfish user needs no write. The
		// service account still might: this is the retry path for a partial change where the
		// Redfish PATCH landed and the service PATCH did not, and returning early here would
		// leave the ssh-only account on the factory default forever.
		if err := client.SetServiceAccountPassword(ctx, passwd); err != nil {
			return nil, err
		}
		return client, nil
	default:
		return nil, unexpectedBMCStatus(resp)
	}
}

func completeInitPasswordForbidden(ctx context.Context, resp *resty.Response, bmcAddress, user, passwd string) (*Client, error) {
	// Post-reset BMCs accept the factory default but refuse Managers until the password is
	// changed. That surfaces here when the credential Secret still holds 0penBmc, or when
	// changeDefaultPassword has not run yet and passwd happens to be the current default.
	if !PasswordChangeRequired(resp) {
		return nil, unexpectedBMCStatus(resp)
	}
	if passwd == BMCDefaultPassword {
		return nil, fmt.Errorf("BMC requires changing the factory default password before access is granted; set a non-default password in the credential Secret")
	}
	if err := changeDefaultPassword(ctx, bmcAddress, user, passwd); err != nil {
		return nil, err
	}
	return NewBasicAuthClient(ctx, bmcAddress, user, passwd)
}

// changeDefaultPassword moves a BMC that still holds the factory default password onto passwd,
// hardening every account DPF manages. A BMC that rejects the default password too is reported as
// holding an unknown password rather than as a connectivity problem.
func changeDefaultPassword(ctx context.Context, bmcAddress, user, passwd string) error {
	log.FromContext(ctx).Info("try to change password")
	defaultClient, err := NewBasicAuthClient(ctx, bmcAddress, user, BMCDefaultPassword)
	if err != nil {
		return err
	}
	defer defaultClient.CloseIdleConnections()
	resp, _, err := defaultClient.ChangeBMCPassword(ctx, passwd, user)
	if err != nil {
		return err
	}
	if resp.StatusCode() == http.StatusUnauthorized {
		return ErrBMCPasswordRejected
	}
	if !PasswordChangeAccepted(resp) {
		return AccountPasswordError(user, resp)
	}
	log.FromContext(ctx).Info("successfully changed password")
	return nil
}

// RotatePassword performs BMC password rotation from oldPassword to newPassword.
// It first tries to authenticate with newPassword (in case rotation already happened).
// If that fails, it authenticates with oldPassword and changes the BMC password to newPassword.
func RotatePassword(ctx context.Context, bmcAddress string, newPassword, oldPassword string) (*Client, error) {
	readCtx, cancel := ReadContext(ctx)
	defer cancel()
	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}

	// Crash-recovery: password might already be rotated.
	newClient, _, err := VerifyBMCCredential(readCtx, bmcAddress, newPassword)
	if err == nil {
		log.FromContext(ctx).Info("new password already active on BMC")
		// Re-apply to the service account: a previous pass may have changed the Redfish user and
		// then failed before the service account, and this branch is what that retry lands on.
		if err := newClient.SetServiceAccountPassword(ctx, newPassword); err != nil {
			newClient.CloseIdleConnections()
			return nil, err
		}
		return newClient, nil
	}
	if !errors.Is(err, ErrBMCPasswordRejected) {
		return nil, fmt.Errorf("BMC connectivity issue during password rotation: %w", err)
	}

	// Authenticate with old password to perform the rotation.
	oldClient, bmcUser, err := VerifyBMCCredential(readCtx, bmcAddress, oldPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with old password: %w", err)
	}
	defer oldClient.CloseIdleConnections()

	log.FromContext(ctx).Info("rotating BMC password", "user", bmcUser)
	resp, _, err := oldClient.ChangeBMCPassword(ctx, newPassword, bmcUser)
	if err != nil {
		return nil, fmt.Errorf("failed to change BMC password: %w", err)
	}
	if !PasswordChangeAccepted(resp) {
		return nil, fmt.Errorf("failed to change BMC password: %w", AccountPasswordError(bmcUser, resp))
	}

	rotatedClient, err := NewBasicAuthClient(ctx, bmcAddress, bmcUser, newPassword)
	if err != nil {
		return nil, err
	}
	log.FromContext(ctx).Info("BMC password rotation completed successfully")
	return rotatedClient, nil
}

// NewBasicAuthClient returns a Client using basic auth
func NewBasicAuthClient(ctx context.Context, bmcAddress, user, passwd string) (*Client, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}
	_, err := url.ParseRequestURI(bmcAddress)
	if err != nil {
		return nil, err
	}

	tlsCfg, err := newRedfishTLSConfig(nil, nil, true, "")
	if err != nil {
		return nil, err
	}
	c := resty.New().
		SetTLSClientConfig(tlsCfg).
		SetBaseURL(bmcAddress).
		SetBasicAuth(user, passwd)

	client := &Client{Client: c, IsBF4: false}

	resp, rootServiceInfo, err := client.GetRootService(ctx)
	// Root authentication rejection is not a constructor failure: the credential
	// workflow checks Managers and preserves the BF3/BF4 account fallback.
	if err != nil && !HasHTTPStatus(err, http.StatusUnauthorized) &&
		(!HasHTTPStatus(err, http.StatusForbidden) || !PasswordChangeRequired(resp)) {
		client.CloseIdleConnections()
		return nil, err
	}

	if rootServiceInfo != nil && rootServiceInfo.IsBF4() {
		client.IsBF4 = true
	}

	return client, nil
}

// tlsClientError wraps an error with the BMC address used to construct the client.
func tlsClientError(bmcAddress string, err error) error {
	return fmt.Errorf("failed to create TLS client for %s: %w", bmcAddress, err)
}

// NewTLSClient returns a Client using verified mTLS. The BMC server certificate is verified
// against the DPF CA (CA-pinned chain + IP-or-CN identity pinning); client-side auth is provided by
// the Redfish client key pair sourced via CertSource (Kubernetes API or mounted files).
func NewTLSClient(ctx context.Context, bmcAddress string, namespace string, k8sClient client.Client) (*Client, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}

	bmcURL, err := url.Parse(bmcAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse BMC address %q: %w", bmcAddress, err)
	}
	serverName := bmcURL.Hostname()

	rawClient, err := NewRawClient(bmcAddress)
	if err != nil {
		return nil, tlsClientError(bmcAddress, fmt.Errorf("failed to create raw client: %w", err))
	}
	defer rawClient.CloseIdleConnections()

	_, rootServiceInfo, err := rawClient.GetRootService(ctx)
	if err != nil {
		return nil, tlsClientError(bmcAddress, err)
	}
	if rootServiceInfo != nil && rootServiceInfo.IsBF4() {
		rawClient.IsBF4 = true
	}

	certSource := newCertSource(k8sClient, namespace)
	caCertBundle, err := certSource.CACert(ctx)
	if err != nil {
		return nil, tlsClientError(bmcAddress, err)
	}
	clientKeyPair, err := certSource.ClientKeyPair(ctx, rawClient.IsBF4)
	if err != nil {
		return nil, tlsClientError(bmcAddress, err)
	}
	if err := verifyClientKeyPairChainsToCA(clientKeyPair, caCertBundle); err != nil {
		return nil, tlsClientError(bmcAddress, err)
	}
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCertBundle) {
		return nil, fmt.Errorf("failed to load CA certs")
	}
	tlsCfg, err := newRedfishTLSConfig(certPool, []tls.Certificate{clientKeyPair}, false, serverName)
	if err != nil {
		return nil, tlsClientError(bmcAddress, err)
	}
	c := resty.New().SetBaseURL(bmcAddress).SetTLSClientConfig(tlsCfg)

	tlsClient := &Client{Client: c, IsBF4: rawClient.IsBF4}

	return tlsClient, nil
}

// GetSecureBoot queries current Secure Boot state from BMC
func (c *Client) GetSecureBoot(ctx context.Context) (*resty.Response, *SecureBootInfo, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	url := strings.Replace(APISecureBoot, "{SYSTEM_ID}", systemID, 1)
	return read[SecureBootInfo](ctx, c, url)
}

// EnableSecureBoot configures Secure Boot to enabled
func (c *Client) EnableSecureBoot(ctx context.Context) (*resty.Response, error) {
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, err
	}
	url := strings.Replace(APISecureBoot, "{SYSTEM_ID}", systemID, 1)

	payload := map[string]interface{}{
		"SecureBootEnable": true,
	}
	resp, err := c.Client.R().
		SetBody(payload).
		Patch(url)
	if err != nil {
		return resp, fmt.Errorf("failed to enable Secure Boot: %w", err)
	}
	// PATCH operations may return 200 OK or 204 No Content
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		return resp, fmt.Errorf("failed to enable Secure Boot: unexpected status code %d", resp.StatusCode())
	}
	return resp, nil
}

// DisableSecureBoot configures Secure Boot to disabled
func (c *Client) DisableSecureBoot(ctx context.Context) (*resty.Response, error) {
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, err
	}

	url := strings.Replace(APISecureBoot, "{SYSTEM_ID}", systemID, 1)

	payload := map[string]interface{}{
		"SecureBootEnable": false,
	}
	resp, err := c.Client.R().
		SetBody(payload).
		Patch(url)
	if err != nil {
		return resp, fmt.Errorf("failed to disable Secure Boot: %w", err)
	}
	// PATCH operations may return 200 OK or 204 No Content
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		return resp, fmt.Errorf("failed to disable Secure Boot: unexpected status code %d", resp.StatusCode())
	}
	return resp, nil
}

// ForceRestartDPUArm performs ForceRestart on DPU ARM (not host power cycle).
func (c *Client) ForceRestartDPUArm(ctx context.Context) (*resty.Response, error) {
	return c.resetDPUArm(ctx, "ForceRestart")
}

// ForceResetSOC posts Oem Nvidia SOC.ForceReset. Unlike ComputerSystem.Reset
// ForceRestart, this does not wait for host PERST, which is required for NIC
// firmware parameters to apply on hostless CMX.
func (c *Client) ForceResetSOC(ctx context.Context) (*resty.Response, error) {
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, err
	}
	url := strings.Replace(APISOCForceReset, "{SYSTEM_ID}", systemID, 1)
	resp, err := c.Client.R().Post(url)
	if err != nil {
		return resp, fmt.Errorf("failed to SOC.ForceReset DPU: %w", err)
	}
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent && resp.StatusCode() != http.StatusAccepted {
		return resp, fmt.Errorf("failed to SOC.ForceReset DPU: unexpected status code %d", resp.StatusCode())
	}
	return resp, nil
}

// GracefulRestartDPUArm performs GracefulRestart on the DPU ARM system.
func (c *Client) GracefulRestartDPUArm(ctx context.Context) (*resty.Response, error) {
	return c.resetDPUArm(ctx, "GracefulRestart")
}

func (c *Client) resetDPUArm(ctx context.Context, resetType string) (*resty.Response, error) {
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, err
	}
	url := strings.Replace(APIResetSystem, "{SYSTEM_ID}", systemID, 1)

	payload := ResetRequest{ResetType: resetType}
	resp, err := c.Client.R().
		SetBody(payload).
		Post(url)
	if err != nil {
		return resp, fmt.Errorf("failed to reset DPU ARM with %s: %w", resetType, err)
	}
	// POST reset operations may return 202 Accepted while the reset runs asynchronously.
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent && resp.StatusCode() != http.StatusAccepted {
		return resp, fmt.Errorf("failed to reset DPU ARM with %s: unexpected status code %d", resetType, resp.StatusCode())
	}
	return resp, nil
}

func (c *Client) InstallBluefieldArmImage(imageURI string) (*resty.Response, *TaskInfo, error) {
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	reqBody := map[string]interface{}{
		"TransferProtocol": "HTTPS",
		"ImageURI":         imageURI,
		"Targets":          []string{APICheckOSImage},
	}
	return do[TaskInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetHeaders(headers).
			SetBody(reqBody).
			Post(APIInstallBFB)
	})
}

func (c *Client) InstallBluefieldArmConfig(imageURI string) (*resty.Response, *TaskInfo, error) {
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	reqBody := map[string]interface{}{
		"TransferProtocol": "HTTPS",
		"ImageURI":         imageURI,
		"Targets":          []string{APICheckConfigImage},
	}
	return do[TaskInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetHeaders(headers).
			SetBody(reqBody).
			Post(APIInstallBFB)
	})
}

func (c *Client) SetBootTarget(target string, bootSourceOverride bool) (*resty.Response, error) {
	bootSourceOverrideEnabled := "Disabled"
	if bootSourceOverride {
		bootSourceOverrideEnabled = "Once"
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	reqBody := map[string]interface{}{
		"Boot": map[string]interface{}{
			"BootSourceOverrideTarget":     target,
			"UefiTargetBootSourceOverride": "None",
			"BootSourceOverrideMode":       "UEFI",
			"BootSourceOverrideEnabled":    bootSourceOverrideEnabled,
			"BootNext":                     "",
			"AutomaticRetryConfig":         "Disabled",
		},
	}

	systemID, err := legacyBootSystemID(c)
	if err != nil {
		return nil, err
	}
	bluefieldSettingsURL := strings.Replace(APIBluefieldSettings, "{SYSTEM_ID}", systemID, 1)
	resp, err := c.Client.R().SetHeaders(headers).SetBody(reqBody).Patch(bluefieldSettingsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to set boot target: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent && resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to set boot target: unexpected status code %d", resp.StatusCode())
	}
	return resp, nil
}

func (c *Client) GetSettings(ctx context.Context) (*resty.Response, *Settings, error) {
	ctx, cancel := ReadContext(ctx)
	defer cancel()
	systemID, err := getSystemID(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	url := strings.Replace(APIBluefieldSettings, "{SYSTEM_ID}", systemID, 1)
	return read[Settings](ctx, c, url)
}

func insertVirtualMedia(c *Client, reqBody map[string]interface{}, mediaID string) (*resty.Response, error) {
	managerID, err := legacyBootManagerID(c)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	ejectVirtualMediaURL := strings.Replace(APIEjectVirtualMedia, managerIDPlaceholder, *managerID, 1)
	ejectVirtualMediaURL = strings.Replace(ejectVirtualMediaURL, "{MEDIA_ID}", mediaID, 1)
	resp, err := c.Client.R().
		SetHeaders(headers).
		Post(ejectVirtualMediaURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to eject virtual media %s: %s", mediaID, resp.Status())
	}
	resp, virtualMedia, err := c.legacyBootVirtualMedia(mediaID)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to get virtual media %s: %s", mediaID, resp.Status())
	}
	if virtualMedia.Inserted {
		return nil, fmt.Errorf("failed to eject virtual media %s: virtual media is still inserted", mediaID)
	}

	virtualMediaURL := strings.Replace(APIInsertVirtualMedia, managerIDPlaceholder, *managerID, 1)
	virtualMediaURL = strings.Replace(virtualMediaURL, "{MEDIA_ID}", mediaID, 1)

	resp, err = c.Client.R().
		SetHeaders(headers).
		SetBody(reqBody).
		Post(virtualMediaURL)

	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to insert virtual media %s: %s", mediaID, resp.Status())
	}

	resp, virtualMedia, err = c.legacyBootVirtualMedia(mediaID)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to get virtual media %s: %s", mediaID, resp.Status())
	}

	if !virtualMedia.Inserted {
		return nil, fmt.Errorf("failed to insert virtual media %s: virtual media is not inserted", mediaID)
	}

	return resp, nil
}

func (c *Client) InsertVirtualMediaConfig() (*resty.Response, error) {

	reqBody := map[string]interface{}{
		"Image":          "file:///media/bf_arm_os/config/config.iso",
		"TransferMethod": "Stream",
	}

	return insertVirtualMedia(c, reqBody, "CONFIG")

}

func (c *Client) InsertVirtualMediaImage() (*resty.Response, error) {

	reqBody := map[string]interface{}{
		"Image":          "file:///media/bf_arm_os/image/image.iso",
		"TransferMethod": "Stream",
	}

	return insertVirtualMedia(c, reqBody, "IMAGE")
}

func (c *Client) chassisReset(data map[string]interface{}) (*resty.Response, error) {
	resp, err := c.Client.R().
		SetBody(data).
		Post(strings.Replace(APIChassisReset, "{CHASSIS_ID}", "BlueField_0", 1))
	if err != nil {
		return resp, fmt.Errorf("failed to reset chassis: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return resp, fmt.Errorf("failed to reset chassis: unexpected status code %d", resp.StatusCode())
	}
	return resp, nil
}

func (c *Client) ChassisReset() (*resty.Response, error) {
	data := map[string]interface{}{
		"ResetType": "ArmReset",
	}
	return c.chassisReset(data)
}

func (c *Client) ArmShutdown() (*resty.Response, error) {
	data := map[string]interface{}{
		"ResetType": "ArmShutdown",
	}
	return c.chassisReset(data)
}

func (c *Client) UpdateBluefieldFirmwareMultipart(fwFile *os.File, force bool) (*resty.Response, *TaskInfo, error) {
	updateParameters := make(map[string]interface{})
	if force {
		updateParameters["ForceUpdate"] = true
	}
	updateParametersJSON, err := json.Marshal(updateParameters)
	if err != nil {
		return nil, nil, err
	}
	return do[TaskInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetFileReader("UpdateFile", fwFile.Name(), fwFile).
			SetMultipartField("UpdateParameters", "", "application/json", strings.NewReader(string(updateParametersJSON))).
			Post(APIUpdateBluefieldFWMultipart)
	})
}

func (c *Client) ActivatePendingBundle() (*resty.Response, error) {
	reqBody := map[string]interface{}{
		"Targets": []Manager{
			{ODataID: "/" + APICheckPendingBundle},
		},
	}
	return c.Client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		Post(APIActivatePendingBundle)
}

// CheckOSImage returns the BlueField Arm OS image member of the BMC firmware inventory.
func (c *Client) CheckOSImage(ctx context.Context) (*VersionInfo, error) {
	return c.getFirmwareInventory(ctx, APICheckOSImage)
}

// CheckConfigImage returns the BlueField Arm OS config member of the BMC firmware inventory.
func (c *Client) CheckConfigImage(ctx context.Context) (*VersionInfo, error) {
	return c.getFirmwareInventory(ctx, APICheckConfigImage)
}

// CheckPendingBMCFirmware returns the BMC firmware version staged by a BF4 PLDM update.
func (c *Client) CheckPendingBMCFirmware(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	return read[VersionInfo](ctx, c, APICheckPendingBMCFW)
}

// CheckPendingBMCEROTFW returns the BMC ERoT firmware version staged by a BF4 PLDM update.
func (c *Client) CheckPendingBMCEROTFW(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	return read[VersionInfo](ctx, c, APICheckPendingBMCEROTFW)
}

// CheckPendingDPUUEFI returns the DPU UEFI version staged by a BF4 PLDM update.
func (c *Client) CheckPendingDPUUEFI(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	return read[VersionInfo](ctx, c, APICheckPendingDPUUEFI)
}

// CheckPendingDPUNIC returns the DPU NIC firmware version staged by a BF4 PLDM update.
func (c *Client) CheckPendingDPUNIC(ctx context.Context) (*resty.Response, *VersionInfo, error) {
	return read[VersionInfo](ctx, c, APICheckPendingDPUNIC)
}

// getFirmwareInventory reads a single firmware inventory member, preserving
// status and cause when the BMC has not published it or the request fails.
func (c *Client) getFirmwareInventory(ctx context.Context, uri string) (*VersionInfo, error) {
	_, info, err := read[VersionInfo](ctx, c, uri)
	if err != nil {
		return nil, fmt.Errorf("get %q: %w", uri, err)
	}
	return info, nil
}

// legacyBootSystemID keeps the original transport and lifetime for BF4 boot reads.
// The caller restarts installation on failure without durable command
// checkpoints, so a new read timeout could replay an already accepted mutation.
func legacyBootSystemID(c *Client) (string, error) {
	response, systems, err := do[Systems](func() (*resty.Response, error) { return c.Client.R().Get(APIGetSystems) })
	if err != nil {
		return "", err
	}
	if response.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", response.StatusCode())
	}
	return findSystemID(systems)
}

// legacyBootManagerID discovers the BMC manager without changing boot-mutation timeouts.
func legacyBootManagerID(c *Client) (*string, error) {
	_, managers, err := do[Managers](func() (*resty.Response, error) { return c.Client.R().Get(APIGetManagers) })
	if err != nil {
		return nil, err
	}
	return findBMCManagerID(managers)
}

// GetSettingsForBootMutation reads boot settings with the mutation client's original lifetime.
func (c *Client) GetSettingsForBootMutation() (*resty.Response, *Settings, error) {
	systemID, err := legacyBootSystemID(c)
	if err != nil {
		return nil, nil, err
	}
	url := strings.Replace(APIBluefieldSettings, "{SYSTEM_ID}", systemID, 1)
	return do[Settings](func() (*resty.Response, error) {
		return c.Client.R().Get(url)
	})
}

// legacyBootVirtualMedia reads virtual media without changing boot-mutation timeouts.
func (c *Client) legacyBootVirtualMedia(mediaID string) (*resty.Response, *VirtualMedia, error) {
	managerID, err := legacyBootManagerID(c)
	if err != nil {
		return nil, nil, err
	}
	url := strings.Replace(APIGetVirtualMedia, managerIDPlaceholder, *managerID, 1)
	url = strings.Replace(url, "{MEDIA_ID}", mediaID, 1)
	return do[VirtualMedia](func() (*resty.Response, error) {
		return c.Client.R().Get(url)
	})
}
