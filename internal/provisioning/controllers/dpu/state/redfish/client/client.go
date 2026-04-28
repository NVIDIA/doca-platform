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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	APIChangePasswd              = "redfish/v1/AccountService/Accounts/{USER}"
	APICheckBMCFW                = "redfish/v1/UpdateService/FirmwareInventory/BMC_Firmware"
	APICheckDPUBSP               = "redfish/v1/UpdateService/FirmwareInventory/DPU_BSP"
	APICheckDPUNIC               = "redfish/v1/UpdateService/FirmwareInventory/DPU_NIC"
	APICheckDPUOS                = "redfish/v1/UpdateService/FirmwareInventory/DPU_OS"
	APICheckDPUUEFI              = "redfish/v1/UpdateService/FirmwareInventory/DPU_UEFI"
	APIInstallBFB                = "redfish/v1/UpdateService/Actions/UpdateService.SimpleUpdate"
	APIUpdateFW                  = "redfish/v1/UpdateService"
	APICheckProgress             = "redfish/v1/TaskService/Tasks"
	APIGetManagers               = "redfish/v1/Managers"
	APIFactoryResetBMC           = "redfish/v1/Managers/{MANAGER_ID}/Actions/Manager.ResetToDefaults"
	APIResetBMC                  = "redfish/v1/Managers/{MANAGER_ID}/Actions/Manager.Reset"
	APIEnableBMCRshim            = "redfish/v1/Managers/Bluefield_BMC/Oem/Nvidia"
	APIGetSystem                 = "redfish/v1/Systems/Bluefield"
	APIDisableHostRshim          = "redfish/v1/Systems/Bluefield/Oem/Nvidia/Actions/HostRshim.Set"
	APIInstallCert               = "redfish/v1/Managers/Bluefield_BMC/Truststore/Certificates"
	APIReplaceCert               = "redfish/v1/CertificateService/Actions/CertificateService.ReplaceCertificate"
	APIGetBios                   = "redfish/v1/Systems/Bluefield/Bios"
	APISetBiosSettings           = "redfish/v1/Systems/Bluefield/Bios/Settings"
	APISetMode                   = "/redfish/v1/Systems/Bluefield/Oem/Nvidia/Actions/Mode.Set"
	APIGenerateCSR               = "redfish/v1/CertificateService/Actions/CertificateService.GenerateCSR"
	APIEnableMTLS                = "redfish/v1/AccountService"
	APIProductDescription        = "redfish/v1/Systems/Bluefield/Oem/Nvidia"
	APIGetChassis                = "redfish/v1/Chassis/{CHASSIS_ID}"
	APIGetNetworkDeviceFunctions = "redfish/v1/Chassis/Card1/NetworkAdapters/NvidiaNetworkAdapter/NetworkDeviceFunctions/{PF_ID}"
	APIRootService               = "redfish/v1"
	APISystemRoot                = APIRootService + "/Systems/Bluefield"
	APISecureBoot                = APISystemRoot + "/SecureBoot"
	APIResetSystem               = APISystemRoot + "/Actions/ComputerSystem.Reset"

	// CASecret is created by the cert-manager Certificate deployed by DPF,
	CASecret = "dpf-provisioning-ca-secret"
	// Issuer is a cert-manager Issuer deployed by DPF
	Issuer = "dpf-provisioning-issuer"
	// ClientCertSecret is created by the cert-manager Certificate deployed by DPF,
	ClientCertSecret = "dpf-provisioning-redfish-client-secret"
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

// MessageExtendedInfo contains the Message.ExtendedInfo responded by RedFish API
type MessageExtendedInfo struct {
	ODataType       string `json:"@odata.type,omitempty"`
	Message         string
	MessageArgs     json.RawMessage
	MessageID       string `json:"MessageId,omitempty"`
	MessageSeverity string
	Resolution      string
}

// ProductSpecInfo contains the product specification information responded by RedFish API
type ProductSpecInfo struct {
	Description string
	Mode        NicModeType
}

type SystemInfo struct {
	BootProgress BootProgress `json:"BootProgress,omitempty"`
}

type BootProgress struct {
	OemLastState string `json:"OemLastState,omitempty"`
}

// Client is a Redfish client
type Client struct {
	*resty.Client
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
	caCertJSON := map[string]interface{}{
		"CertificateString": caCert,
		"CertificateType":   "PEM",
	}
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(caCertJSON).
			Post(APIInstallCert)
	})
}

// ReplaceCACert replaces the trusted CA certificate with the given caCert
func (c *Client) ReplaceCACert(caCert string) (*resty.Response, *ExtendedInfo, error) {
	caCertJSON := map[string]interface{}{
		"CertificateString": caCert,
		"CertificateType":   "PEM",
		"CertificateUri": map[string]interface{}{
			"@odata.id": "/redfish/v1/Managers/Bluefield_BMC/Truststore/Certificates/1",
		},
	}
	return c.ReplaceCert(caCertJSON)
}

// ReplaceServerCert replaces the server certificate used by BMC APIs
func (c *Client) ReplaceServerCert(srvCert string) (*resty.Response, *ExtendedInfo, error) {
	srvCertJSON := map[string]interface{}{
		"CertificateString": srvCert,
		"CertificateType":   "PEM",
		"CertificateUri": map[string]interface{}{
			"@odata.id": "/redfish/v1/Managers/Bluefield_BMC/NetworkProtocol/HTTPS/Certificates/1",
		},
	}
	return c.ReplaceCert(srvCertJSON)
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
			"@odata.id": "/redfish/v1/Managers/Bluefield_BMC/NetworkProtocol/HTTPS/Certificates",
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
	return do[VersionInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APICheckBMCFW)
	})
}

func (c *Client) GetSystem() (*resty.Response, *SystemInfo, error) {
	return do[SystemInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APIGetSystem)
	})
}

// CheckDPUNIC fetches DPU NIC version
func (c *Client) CheckDPUNIC() (*resty.Response, *VersionInfo, error) {
	return do[VersionInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APICheckDPUNIC)
	})
}

// CheckDPUOS fetches DPU OS version
func (c *Client) CheckDPUOS() (*resty.Response, *VersionInfo, error) {
	return do[VersionInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APICheckDPUOS)
	})
}

func (c *Client) CheckDPUUEFI() (*resty.Response, *VersionInfo, error) {
	return do[VersionInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APICheckDPUUEFI)
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
			Post(strings.Replace(APIFactoryResetBMC, "{MANAGER_ID}", *managerID, 1))
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
			Post(strings.Replace(APIResetBMC, "{MANAGER_ID}", *managerID, 1))
	})
}

// CheckTaskProgress fetches progress of the given task
func (c *Client) CheckTaskProgress(taskID string) (*resty.Response, *TaskProgress, error) {
	return do[TaskProgress](func() (*resty.Response, error) {
		return c.Client.R().Get(fmt.Sprintf("%s/%s", APICheckProgress, taskID))
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

// ChassisInfo contains the part number information responded by RedFish API
type ChassisInfo struct {
	Model        string `json:"Model"`
	PartNumber   string `json:"PartNumber"`
	SerialNumber string `json:"SerialNumber"`
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
	} else {
		return provisioningv1.DPUTypeUnknown
	}
}

// GetChassis fetches part number of DPU
func (c *Client) GetChassis() (*resty.Response, *ChassisInfo, error) {
	return do[ChassisInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(strings.Replace(APIGetChassis, "{CHASSIS_ID}", "Card1", 1))
	})
}

// GetProductDescription fetches product spec of DPU
func (c *Client) GetProductDescription() (*resty.Response, *ProductSpecInfo, error) {
	return do[ProductSpecInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APIProductDescription)
	})
}

// GetBios returns a Bios information for current DPU
func (c *Client) GetBios() (*resty.Response, *Bios, error) {
	return do[Bios](func() (*resty.Response, error) {
		return c.Client.R().Get(APIGetBios)
	})
}

type NetworkDeviceFunction struct {
	ID             string   `json:"Id"`
	Ethernet       Ethernet `json:"Ethernet"`
	NetDevFuncType string   `json:"NetDevFuncType"` // "Ethernet" or "Infiniband"
}

type Ethernet struct {
	MACAddress string `json:"MACAddress"`
	MTUSize    int    `json:"MTUSize"`
}

func (c *Client) GetNetworkDeviceFunction(pfID string) (*resty.Response, *NetworkDeviceFunction, error) {
	return do[NetworkDeviceFunction](func() (*resty.Response, error) {
		return c.Client.R().Get(strings.Replace(APIGetNetworkDeviceFunctions, "{PF_ID}", pfID, 1))
	})
}

// SetDpuMode returns a Bios information for current DPU
func (c *Client) SetDpuMode(desiredMode provisioningv1.DpuModeType) (*resty.Response, error) {
	var body []byte
	switch desiredMode {
	case provisioningv1.DpuMode:
		body = []byte(`{ "Attributes": {"InternalCPUModel": "Privileged" } }`)
		resp, err := c.Client.R().SetBody(body).Patch(APISetBiosSettings)
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
	return &Client{Client: resty.New().SetBaseURL(u.String()).SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})}, nil
}

// GetRootService returns the root service of the BMC
func (c *Client) GetRootService() (*resty.Response, error) {
	return c.Client.R().Get(APIRootService)
}

// InitPassword reads a password from the user-created Secret and set it to all DPUs
func InitPassword(ctx context.Context, bmcAddress string, namespace string, k8sClient client.Client) (*Client, error) {
	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}
	// get BMC password from the secret created by user
	nn := types.NamespacedName{Name: BMCPasswordSecret, Namespace: namespace}
	passwdSecret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, nn, passwdSecret); err != nil {
		return nil, err
	}
	passwd := string(passwdSecret.Data[BMCSharedPasswordKey])
	if passwd == "" {
		return nil, fmt.Errorf("password not specified in secret %s", nn.String())
	}

	// check if the default password has been changed as requested by the DOCA BMC manual
	client, err := NewBasicAuthClient(bmcAddress, BF3BMCUser, passwd)
	if err != nil {
		return nil, err
	}
	resp, _, err := client.CheckBMCFirmware()
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized:
		log.FromContext(ctx).Info("try to change password")
		defaultClient, err := NewBasicAuthClient(bmcAddress, BF3BMCUser, BMCDefaultPassword)
		if err != nil {
			return nil, err
		}
		resp, _, err = defaultClient.ChangeBMCPassword(passwd, BF3BMCUser)
		if err != nil {
			return nil, err
		} else if resp.StatusCode() == http.StatusUnauthorized {
			defaultClient, err = NewBasicAuthClient(bmcAddress, BF4BMCUser, BMCDefaultPassword)
			if err != nil {
				return nil, err
			}
			resp, _, err = defaultClient.ChangeBMCPassword(passwd, BF4BMCUser)
			if err != nil {
				return nil, err
			}
			if resp.StatusCode() == http.StatusOK {
				client, err = NewBasicAuthClient(bmcAddress, BF4BMCUser, passwd)
				if err != nil {
					return nil, err
				}
				return client, nil
			}
			return nil, fmt.Errorf("the default BMC password has been changed and the given password is wrong")
		} else if resp.StatusCode() != http.StatusOK {
			return nil, fmt.Errorf("expected status %q, received status %q", http.StatusOK, resp.Status())
		}
		log.FromContext(ctx).Info("successfully changed password")
		return client, nil
	case http.StatusOK:
		return client, nil
	default:
		return nil, fmt.Errorf("unexpected status: %q", resp.Status())
	}
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

	c := resty.New().
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}).
		SetBaseURL(bmcAddress).
		SetBasicAuth(user, passwd)
	return &Client{Client: c}, nil
}

// NewTLSClient returns a Client using mTLS
func NewTLSClient(ctx context.Context, bmcAddress string, namespace string, k8sClient client.Client) (*Client, error) {
	if !strings.HasPrefix(bmcAddress, httpsPrefix) {
		bmcAddress = httpsPrefix + bmcAddress
	}
	caSecret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: CASecret, Namespace: namespace}, caSecret); err != nil {
		return nil, err
	}
	caCert, ok := caSecret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("no CA crt in CA secret")
	}
	clientSecret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: ClientCertSecret, Namespace: namespace}, clientSecret); err != nil {
		return nil, err
	}
	clientCert, ok := clientSecret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("no client crt in client secret")
	}
	clientKey, ok := clientSecret.Data["tls.key"]
	if !ok {
		return nil, fmt.Errorf("no client key in client secret")
	}
	clientKeyPair, err := tls.X509KeyPair(clientCert, clientKey)
	if err != nil {
		return nil, err
	}
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to load CA certs")
	}
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true,
		RootCAs:            certPool,
		Certificates:       []tls.Certificate{clientKeyPair},
	}

	c := resty.New().SetBaseURL(bmcAddress).SetTLSClientConfig(tlsCfg)

	tlsClient := &Client{Client: c}

	// verify if the tls client is working
	// TODO: It is not necessary to perform verification every time when a tls client is created.
	// We currently want to eliminate other issues with the mlt client, so we temporarily put it in this function.
	// We will optimize it after redfish is stable.
	resp, _, err := tlsClient.GetProductDescription()
	if err != nil {
		err = fmt.Errorf("verify mtls client failed, err: %v", err)
		return nil, err
	}

	if resp != nil && resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("redfish call getProductDescription failed, status code: %s", resp.Status())
	}

	return tlsClient, nil
}

// GetSecureBoot queries current Secure Boot state from BMC
func (c *Client) GetSecureBoot() (*resty.Response, *SecureBootInfo, error) {
	return do[SecureBootInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APISecureBoot)
	})
}

// EnableSecureBoot configures Secure Boot to enabled
func (c *Client) EnableSecureBoot() (*resty.Response, error) {
	payload := map[string]interface{}{
		"SecureBootEnable": true,
	}
	resp, err := c.Client.R().
		SetBody(payload).
		Patch(APISecureBoot)
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
	payload := map[string]interface{}{
		"SecureBootEnable": false,
	}
	resp, err := c.Client.R().
		SetBody(payload).
		Patch(APISecureBoot)
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
	payload := ResetRequest{ResetType: "ForceRestart"}
	resp, err := c.Client.R().
		SetBody(payload).
		Post(APIResetSystem)
	if err != nil {
		return resp, fmt.Errorf("failed to force restart DPU ARM: %w", err)
	}
	// POST operations may return 200 OK or 204 No Content
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		return resp, fmt.Errorf("failed to force restart DPU ARM: unexpected status code %d", resp.StatusCode())
	}
	return resp, nil
}
