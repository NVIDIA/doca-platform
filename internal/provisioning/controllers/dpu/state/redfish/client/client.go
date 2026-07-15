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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

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
	APICheckDPUBSP                  = "redfish/v1/UpdateService/FirmwareInventory/DPU_BSP"
	APICheckDPUNIC                  = "redfish/v1/UpdateService/FirmwareInventory/{DPU_NIC_ID}"
	APICheckDPUOS                   = "redfish/v1/UpdateService/FirmwareInventory/DPU_OS"
	APICheckDPUUEFI                 = "redfish/v1/UpdateService/FirmwareInventory/{DPU_UEFI_ID}"
	APICheckPendingBundle           = "redfish/v1/UpdateService/FirmwareInventory/Pending_Bundle"
	APIInstallBFB                   = "redfish/v1/UpdateService/Actions/UpdateService.SimpleUpdate"
	APIInsertVirtualMedia           = "redfish/v1/Managers/{MANAGER_ID}/VirtualMedia/{MEDIA_ID}/Actions/VirtualMedia.InsertMedia"
	APIEjectVirtualMedia            = "redfish/v1/Managers/{MANAGER_ID}/VirtualMedia/{MEDIA_ID}/Actions/VirtualMedia.EjectMedia"
	APIUpdateFW                     = "redfish/v1/UpdateService"
	APICheckProgress                = "redfish/v1/TaskService/Tasks"
	APIGetManagers                  = "redfish/v1/Managers"
	APIGetSystems                   = "redfish/v1/Systems"
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
	// APIGetSELEntries is the Redfish System Event Log entries collection. The BMC
	// records sensor-threshold events (e.g., 12V_ATX low) here, which we surface
	// as best-effort hints when an install task fails.
	// Reference: https://docs.nvidia.com/networking/display/bfswtroubleshooting/bmc
	APIGetSELEntries = APISystemRoot + "/LogServices/SEL/Entries"

	// APIHostPrivilegeConfigSettings is the Settings URI for host privilege configuration (BF4).
	APIHostPrivilegeConfigSettings = APIRootService + "/Chassis/BlueField_0/NetworkAdapters/BlueField_NIC_0/Oem/Nvidia/HostPrivilegeConfig/Settings"

	// CASecret is created by the cert-manager Certificate deployed by DPF,
	CASecret = "dpf-provisioning-ca-secret"
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
	BF3BMCUser           = "root"
	BF4BMCUser           = "admin"
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
	IsBF4 bool
}

// ChangeBMCPassword changes BMC password. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/connecting+to+bmc+interfaces#src-704886267_ConnectingtoBMCInterfaces-ChangingDefaultPassword
func (c *Client) ChangeBMCPassword(newPassword string, user string) (*resty.Response, *ExtendedInfo, error) {
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(map[string]string{
				"Password": newPassword,
			}).
			Patch(strings.Replace(APIChangePasswd, "{USER}", user, 1))
	})
}

// InstallCert installs the given certificate, making the certificate trusted by BMC
func (c *Client) InstallCert(caCert string) (*resty.Response, *ExtendedInfo, error) {
	managerID, err := getBMCManagerID(c)
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

// ReplaceCACert replaces the trusted CA certificate with the given caCert
func (c *Client) ReplaceCACert(caCert string) (*resty.Response, *ExtendedInfo, error) {
	managerID, err := getBMCManagerID(c)
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
func (c *Client) ReplaceServerCert(srvCert string) (*resty.Response, *ExtendedInfo, error) {
	managerID, err := getBMCManagerID(c)
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
func (c *Client) GetServerCert() (*resty.Response, *ServerCertInfo, error) {
	managerID, err := getBMCManagerID(c)
	if err != nil {
		return nil, nil, err
	}
	return do[ServerCertInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(strings.Replace(APIServerCert, managerIDPlaceholder, *managerID, 1))
	})
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
func (c *Client) GenerateCSR(cn string) (*resty.Response, *CSRInfo, error) {
	managerID, err := getBMCManagerID(c)
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
func (c *Client) CheckBMCFirmware() (*resty.Response, *VersionInfo, error) {
	bmcFwID := "BMC_Firmware"
	if c.IsBF4 {
		bmcFwID = "BlueField_FW_BMC_0"
	}

	url := strings.Replace(APICheckBMCFW, "{BMC_FW_ID}", bmcFwID, 1)

	return do[VersionInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(url)
	})
}

func (c *Client) CheckBMCEROTFW() (*resty.Response, *VersionInfo, error) {
	return do[VersionInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APICheckBMCEROTFW)
	})
}

func (c *Client) GetSystem() (*resty.Response, *SystemInfo, error) {
	systemID, err := getSystemID(c)
	if err != nil {
		return nil, nil, err
	}

	return do[SystemInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(strings.Replace(APIGetSystem, "{SYSTEM_ID}", systemID, 1))
	})
}

// CheckDPUNIC fetches DPU NIC version
func (c *Client) CheckDPUNIC() (*resty.Response, *VersionInfo, error) {
	dpuNicID := "DPU_NIC"
	if c.IsBF4 {
		dpuNicID = "BlueField_FW_NIC_0"
	}

	url := strings.Replace(APICheckDPUNIC, "{DPU_NIC_ID}", dpuNicID, 1)

	return do[VersionInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(url)
	})
}

// CheckDPUOS fetches DPU OS version
func (c *Client) CheckDPUOS() (*resty.Response, *VersionInfo, error) {
	return do[VersionInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APICheckDPUOS)
	})
}

func (c *Client) CheckDPUUEFI() (*resty.Response, *VersionInfo, error) {
	uefiID := "DPU_UEFI"
	if c.IsBF4 {
		uefiID = "BlueField_FW_CPU_0"
	}

	url := strings.Replace(APICheckDPUUEFI, "{DPU_UEFI_ID}", uefiID, 1)

	return do[VersionInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(url)
	})
}

func (c *Client) CheckDPUBSP() (*resty.Response, *VersionInfo, error) {
	return do[VersionInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APICheckDPUBSP)
	})
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
		"TransferProtocol": "HTTP",
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

func (c *Client) GetManagers() (*resty.Response, *Managers, error) {
	return do[Managers](func() (*resty.Response, error) {
		return c.Client.R().Get(APIGetManagers)
	})
}

func getBMCManagerID(c *Client) (*string, error) {
	_, managers, err := c.GetManagers()
	if err != nil {
		return nil, err
	}
	if managers == nil || len(managers.Members) == 0 {
		return nil, fmt.Errorf("no managers found")
	}
	var managerID string
	for _, manager := range managers.Members {
		if strings.Contains(strings.ToLower(manager.ODataID), "bmc") {
			managerID = strings.Split(manager.ODataID, "/")[len(strings.Split(manager.ODataID, "/"))-1]
			break
		}
	}
	if managerID == "" {
		return nil, fmt.Errorf("no BMC manager found")
	}
	return &managerID, nil
}

// FactoryResetBMC resets BMC to factory defaults. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2504/factory+reset+bmc
func (c *Client) FactoryResetBMC() (*resty.Response, *ExtendedInfo, error) {
	managerID, err := getBMCManagerID(c)
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
func (c *Client) ResetBMC() (*resty.Response, *ExtendedInfo, error) {
	managerID, err := getBMCManagerID(c)
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
func (c *Client) CheckTaskProgress(taskID string) (*resty.Response, *TaskProgress, error) {
	return do[TaskProgress](func() (*resty.Response, error) {
		return c.Client.R().Get(fmt.Sprintf("%s/%s", APICheckProgress, taskID))
	})
}

// GetSELEntries fetches the BMC System Event Log entries collection. Used by
// the OS Installing failure paths to surface sensor-threshold events (e.g.
// 12V_ATX low) as operator hints. Best-effort: callers must tolerate errors
// and never propagate them. The context is propagated to the HTTP layer so
// callers can cap the call duration on unreachable BMCs.
func (c *Client) GetSELEntries(ctx context.Context) (*resty.Response, *SELEntries, error) {
	systemID, err := getSystemID(c)
	if err != nil {
		return nil, nil, err
	}
	url := strings.Replace(APIGetSELEntries, "{SYSTEM_ID}", systemID, 1)
	return do[SELEntries](func() (*resty.Response, error) {
		return c.Client.R().SetContext(ctx).Get(url)
	})
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

// ChassisAssetTagUnavailable is the BMC sentinel when chassis AssetTag (PSID) is not set.
const ChassisAssetTagUnavailable = "N/A"

// ChassisInfo contains the part number information responded by RedFish API
type ChassisInfo struct {
	AssetTag     string                 `json:"AssetTag"`
	Model        string                 `json:"Model"`
	PartNumber   string                 `json:"PartNumber"`
	SerialNumber string                 `json:"SerialNumber"`
	Oem          map[string]interface{} `json:"Oem"`
}

var blueFieldRegex = regexp.MustCompile(`bluefield[- ]?(\d+)`)

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
func (c *Client) GetChassis() (*resty.Response, *ChassisInfo, error) {
	chassisID := "Card1"
	if c.IsBF4 {
		chassisID = "BlueField_0"
	}

	return do[ChassisInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(strings.Replace(APIGetChassis, "{CHASSIS_ID}", chassisID, 1))
	})
}

func (c *Client) GetErotChassis() (*resty.Response, *ChassisInfo, error) {
	chassisID := "BlueField_ERoT_BMC_0"
	url := strings.Replace(APIGetChassis, "{CHASSIS_ID}", chassisID, 1)
	return do[ChassisInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(url)
	})
}

func (c *Client) GetSystems() (*resty.Response, *Systems, error) {
	return do[Systems](func() (*resty.Response, error) {
		return c.Client.R().Get(APIGetSystems)
	})
}

func getSystemID(c *Client) (string, error) {
	response, systems, err := c.GetSystems()
	if err != nil {
		return "", err
	}
	if response.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", response.StatusCode())
	}

	for _, system := range systems.Members {
		if strings.Contains(strings.ToLower(system.ODataID), "bluefield") {
			return strings.Split(system.ODataID, "/")[len(strings.Split(system.ODataID, "/"))-1], nil
		}
	}
	return "", fmt.Errorf("no system found")
}

// GetProductDescription fetches product spec of DPU
func (c *Client) GetProductDescription() (*resty.Response, *ProductSpecInfo, error) {
	systemID, err := getSystemID(c)
	if err != nil {
		return nil, nil, err
	}
	return do[ProductSpecInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(strings.Replace(APIProductDescription, "{SYSTEM_ID}", systemID, 1))
	})
}

// GetBios returns a Bios information for current DPU
func (c *Client) GetBios() (*resty.Response, *Bios, error) {
	systemID, err := getSystemID(c)
	if err != nil {
		return nil, nil, err
	}
	return do[Bios](func() (*resty.Response, error) {
		return c.Client.R().Get(strings.Replace(APIGetBios, "{SYSTEM_ID}", systemID, 1))
	})
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

func (c *Client) GetNetworkDeviceFunction(pfID string) (*resty.Response, *NetworkDeviceFunction, error) {
	url := APIGetNetworkDeviceFunctions
	if c.IsBF4 {
		url = APIGetNetworkDeviceFunctionsBF4
	}

	url = strings.Replace(url, "{PF_ID}", pfID, 1)
	return do[NetworkDeviceFunction](func() (*resty.Response, error) {
		return c.Client.R().Get(url)
	})
}

// SetDpuMode returns a Bios information for current DPU
func (c *Client) SetDpuMode(desiredMode provisioningv1.DpuModeType) (*resty.Response, error) {
	systemID, err := getSystemID(c)
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

// do sends a request and unmarshals the response body into the given type
func do[T any](req reqFunc) (*resty.Response, *T, error) {
	resp, err := req()
	if err != nil {
		return nil, nil, err
	}
	var t T
	if err := json.Unmarshal(resp.Body(), &t); err != nil {
		return resp, nil, err
	}
	return resp, &t, nil
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
func (c *Client) GetRootService() (*resty.Response, *RootServiceInfo, error) {
	return do[RootServiceInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APIRootService)
	})
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

// VerifyBMCCredential tries to authenticate to the BMC with the given password,
// attempting BF3 (root) first, then falling back to BF4 (admin).
// It returns the authenticated client and the BMC username that succeeded.
func VerifyBMCCredential(bmcAddress, password string) (*Client, string, error) {
	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}

	for _, user := range []string{BF3BMCUser, BF4BMCUser} {
		c, err := NewBasicAuthClient(bmcAddress, user, password)
		if err != nil {
			return nil, "", err
		}
		resp, _, err := c.CheckBMCFirmware()
		if err != nil {
			return nil, "", err
		}
		switch resp.StatusCode() {
		case http.StatusOK:
			return c, user, nil
		case http.StatusUnauthorized:
			continue
		default:
			return nil, "", fmt.Errorf("unexpected BMC status: %s", resp.Status())
		}
	}
	return nil, "", fmt.Errorf("the default BMC password has been changed and the given password is wrong")
}

// InitPassword resolves the BMC password and authenticates to the BMC.
func InitPassword(ctx context.Context, bmcAddress string, namespace string, bmcCredentialSecretName *string, k8sClient client.Client) (*Client, error) {
	cred, err := ResolveBMCCredential(ctx, namespace, bmcCredentialSecretName, k8sClient)
	if err != nil {
		return nil, err
	}
	passwd := cred.Password

	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}

	rootClient, err := NewRawClient(bmcAddress)
	if err != nil {
		return nil, err
	}
	_, rootServiceInfo, err := rootClient.GetRootService()
	if err != nil {
		return nil, err
	}
	var user = BF3BMCUser
	if rootServiceInfo.IsBF4() {
		user = BF4BMCUser
		log.FromContext(ctx).Info("Assuming BF4 model, BMC user changed to admin")
	}

	// check if the default password has been changed as requested by the DOCA BMC manual
	client, err := NewBasicAuthClient(bmcAddress, user, passwd)
	if err != nil {
		return nil, err
	}

	resp, _, err := client.GetManagers()
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized:
		log.FromContext(ctx).Info("try to change password")
		defaultClient, err := NewBasicAuthClient(bmcAddress, user, BMCDefaultPassword)
		if err != nil {
			return nil, err
		}
		resp, _, err = defaultClient.ChangeBMCPassword(passwd, user)
		if err != nil {
			return nil, err
		} else if resp.StatusCode() == http.StatusUnauthorized {
			return nil, fmt.Errorf("the default BMC password has been changed and the given password is wrong")
		} else if resp.StatusCode() != http.StatusOK {
			return nil, fmt.Errorf("unexpected BMC status: %s", resp.Status())
		}
		log.FromContext(ctx).Info("successfully changed password")
		return client, nil
	case http.StatusOK:
		return client, nil
	default:
		return nil, fmt.Errorf("unexpected BMC status: %s", resp.Status())
	}
}

// RotatePassword performs BMC password rotation from oldPassword to newPassword.
// It first tries to authenticate with newPassword (in case rotation already happened).
// If that fails, it authenticates with oldPassword and changes the BMC password to newPassword.
func RotatePassword(ctx context.Context, bmcAddress string, newPassword, oldPassword string) (*Client, error) {
	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}

	// Crash-recovery: password might already be rotated.
	newClient, _, err := VerifyBMCCredential(bmcAddress, newPassword)
	if err == nil {
		log.FromContext(ctx).Info("new password already active on BMC")
		return newClient, nil
	}
	if !strings.Contains(err.Error(), "password is wrong") && !strings.Contains(err.Error(), "unexpected BMC status") {
		return nil, fmt.Errorf("BMC connectivity issue during password rotation: %w", err)
	}

	// Authenticate with old password to perform the rotation.
	oldClient, bmcUser, err := VerifyBMCCredential(bmcAddress, oldPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with old password: %w", err)
	}

	log.FromContext(ctx).Info("rotating BMC password", "user", bmcUser)
	resp, _, err := oldClient.ChangeBMCPassword(newPassword, bmcUser)
	if err != nil {
		return nil, fmt.Errorf("failed to change BMC password: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to change BMC password: status %s", resp.Status())
	}

	rotatedClient, err := NewBasicAuthClient(bmcAddress, bmcUser, newPassword)
	if err != nil {
		return nil, err
	}
	log.FromContext(ctx).Info("BMC password rotation completed successfully")
	return rotatedClient, nil
}

// NewBasicAuthClient returns a Client using basic auth
func NewBasicAuthClient(bmcAddress, user, passwd string) (*Client, error) {
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

	_, rootServiceInfo, err := client.GetRootService()
	if err != nil {
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

	_, rootServiceInfo, err := rawClient.GetRootService()
	if err != nil {
		return nil, tlsClientError(bmcAddress, err)
	}
	if rootServiceInfo != nil && rootServiceInfo.IsBF4() {
		rawClient.IsBF4 = true
	}

	certSource := newCertSource(k8sClient, namespace)
	caCert, err := certSource.CACert(ctx)
	if err != nil {
		return nil, tlsClientError(bmcAddress, err)
	}
	clientKeyPair, err := certSource.ClientKeyPair(ctx, rawClient.IsBF4)
	if err != nil {
		return nil, tlsClientError(bmcAddress, err)
	}
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, tlsClientError(bmcAddress, fmt.Errorf("failed to load CA certs"))
	}
	tlsCfg, err := newRedfishTLSConfig(certPool, []tls.Certificate{clientKeyPair}, false, serverName)
	if err != nil {
		return nil, tlsClientError(bmcAddress, err)
	}
	c := resty.New().SetBaseURL(bmcAddress).SetTLSClientConfig(tlsCfg)

	tlsClient := &Client{Client: c, IsBF4: rawClient.IsBF4}

	// verify if the tls client is working
	// TODO: It is not necessary to perform verification every time when a tls client is created.
	// We currently want to eliminate other issues with the mlt client, so we temporarily put it in this function.
	// We will optimize it after redfish is stable.
	resp, _, err := tlsClient.GetManagers()
	if err != nil {
		return nil, tlsClientError(bmcAddress, fmt.Errorf("verify mtls client failed, err: %w", err))
	}

	if resp != nil && resp.StatusCode() != http.StatusOK {
		return nil, tlsClientError(bmcAddress, fmt.Errorf("redfish call getManagers failed, status code: %s", resp.Status()))
	}

	return tlsClient, nil
}

// GetSecureBoot queries current Secure Boot state from BMC
func (c *Client) GetSecureBoot() (*resty.Response, *SecureBootInfo, error) {
	systemID, err := getSystemID(c)
	if err != nil {
		return nil, nil, err
	}
	url := strings.Replace(APISecureBoot, "{SYSTEM_ID}", systemID, 1)
	return do[SecureBootInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(url)
	})
}

// EnableSecureBoot configures Secure Boot to enabled
func (c *Client) EnableSecureBoot() (*resty.Response, error) {
	systemID, err := getSystemID(c)
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
func (c *Client) DisableSecureBoot() (*resty.Response, error) {
	systemID, err := getSystemID(c)
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
func (c *Client) ForceRestartDPUArm() (*resty.Response, error) {
	return c.resetDPUArm("ForceRestart")
}

// GracefulRestartDPUArm performs GracefulRestart on the DPU ARM system.
func (c *Client) GracefulRestartDPUArm() (*resty.Response, error) {
	return c.resetDPUArm("GracefulRestart")
}

func (c *Client) resetDPUArm(resetType string) (*resty.Response, error) {
	systemID, err := getSystemID(c)
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
		"TransferProtocol": "HTTP",
		"ImageURI":         imageURI,
		"Targets":          []string{"redfish/v1/UpdateService/FirmwareInventory/BlueField_OS_Image_CPU_0"},
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
		"TransferProtocol": "HTTP",
		"ImageURI":         imageURI,
		"Targets":          []string{"redfish/v1/UpdateService/FirmwareInventory/BlueField_OS_Config_CPU_0"},
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

	systemID, err := getSystemID(c)
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

func (c *Client) GetSettings() (*resty.Response, *Settings, error) {
	systemID, err := getSystemID(c)
	if err != nil {
		return nil, nil, err
	}
	url := strings.Replace(APIBluefieldSettings, "{SYSTEM_ID}", systemID, 1)
	return do[Settings](func() (*resty.Response, error) {
		return c.Client.R().Get(url)
	})
}

func insertVirtualMedia(c *Client, reqBody map[string]interface{}, mediaID string) (*resty.Response, error) {
	managerID, err := getBMCManagerID(c)
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
		return nil, fmt.Errorf("failed to eject virtual media config: %s", resp.Status())
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
		return nil, fmt.Errorf("failed to insert virtual media config: %s", resp.Status())
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

func (c *Client) ChassisReset() (*resty.Response, error) {
	data := map[string]interface{}{
		"ResetType": "ArmReset",
	}
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
