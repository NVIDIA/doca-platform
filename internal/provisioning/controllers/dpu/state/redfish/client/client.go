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
	"net"
	"net/http"
	"net/url"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/go-resty/resty/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	APIChangePasswd       = "redfish/v1/AccountService/Accounts/root"
	APICheckBMCFW         = "redfish/v1/UpdateService/FirmwareInventory/BMC_Firmware"
	APICheckDPUNIC        = "redfish/v1/UpdateService/FirmwareInventory/DPU_NIC"
	APICheckDPUOS         = "redfish/v1/UpdateService/FirmwareInventory/DPU_OS"
	APIInstallBFB         = "redfish/v1/UpdateService/Actions/UpdateService.SimpleUpdate"
	APICheckProgress      = "redfish/v1/TaskService/Tasks"
	APIEnableBMCRshim     = "redfish/v1/Managers/Bluefield_BMC/Oem/Nvidia"
	APIDisableHostRshim   = "redfish/v1/Systems/Bluefield/Oem/Nvidia/Actions/HostRshim.Set"
	APIInstallCert        = "redfish/v1/Managers/Bluefield_BMC/Truststore/Certificates"
	APIReplaceCert        = "redfish/v1/CertificateService/Actions/CertificateService.ReplaceCertificate"
	APIGetBios            = "redfish/v1/Systems/Bluefield/Bios"
	APISetBiosSettings    = "redfish/v1/Systems/Bluefield/Bios/Settings"
	APISetMode            = "/redfish/v1/Systems/Bluefield/Oem/Nvidia/Actions/Mode.Set"
	APIGenerateCSR        = "redfish/v1/CertificateService/Actions/CertificateService.GenerateCSR"
	APIEnableMTLS         = "redfish/v1/AccountService"
	APIProductDescription = "redfish/v1/Systems/Bluefield/Oem/Nvidia"
	APIPartNumber         = "redfish/v1/Chassis/Card1"

	// CASecret is created by the cert-manager Certificate deployed by DPF,
	CASecret = "dpf-provisioning-ca-secret"
	// Issuer is a cert-manager Issuer deployed by DPF
	Issuer = "dpf-provisioning-issuer"
	// ClientCertSecret is created by the cert-manager Certificate deployed by DPF,
	ClientCertSecret = "dpf-provisioning-redfish-client-secret"
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
}

// Client is a Redfish client
type Client struct {
	*resty.Client
}

// ChangeBMCPassword changes BMC password. For more information, refer to
// https://docs.nvidia.com/networking/display/bluefieldbmcv2410/connecting+to+bmc+interfaces#src-704886267_ConnectingtoBMCInterfaces-ChangingDefaultPassword
func (c *Client) ChangeBMCPassword(newPassword string) (*resty.Response, *ExtendedInfo, error) {
	return do[ExtendedInfo](func() (*resty.Response, error) {
		return c.Client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(map[string]string{
				"Password": newPassword,
			}).
			Patch(APIChangePasswd)
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

// PartNumberInfo contains the part number information responded by RedFish API
type PartNumberInfo struct {
	PartNumber string
}

// GetPartNumber fetches part number of DPU
func (c *Client) GetPartNumber() (*resty.Response, *PartNumberInfo, error) {
	return do[PartNumberInfo](func() (*resty.Response, error) {
		return c.Client.R().Get(APIPartNumber)
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
	case provisioningv1.ZeroTrustMode:
		body = []byte(`{ "Attributes": {"InternalCPUModel": "Restricted" } }`)
		resp, err := c.Client.R().SetBody(body).Patch(APISetBiosSettings)
		if err != nil {
			return resp, err
		}
		body = []byte(`{"Mode": "DpuMode"}`)
	case provisioningv1.NicMode:
		body = []byte(`{"Mode": "NicMode"}`)
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

// NewBasicAuthClient returns a Client using basic auth
func NewBasicAuthClient(bmcIP, user, passwd string) (*Client, error) {
	if net.ParseIP(bmcIP) == nil {
		return nil, fmt.Errorf("invalid IP: %s", bmcIP)
	}
	c := resty.New().
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}).
		SetBaseURL(fmt.Sprintf("https://%s", bmcIP)).
		SetBasicAuth(user, passwd)
	return &Client{Client: c}, nil
}

// NewTLSClient returns a Client using mTLS
func NewTLSClient(ctx context.Context, dpu *provisioningv1.DPU, k8sClient client.Client) (*Client, error) {
	bmcIP := net.ParseIP(dpu.Spec.BMCIP)
	if bmcIP == nil {
		return nil, fmt.Errorf("invalid IP: %s", dpu.Spec.BMCIP)
	}
	caSecret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: CASecret, Namespace: dpu.Namespace}, caSecret); err != nil {
		return nil, err
	}
	caCert, ok := caSecret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("no CA crt in CA secret")
	}
	clientSecret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: ClientCertSecret, Namespace: dpu.Namespace}, clientSecret); err != nil {
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
	c := resty.New().SetBaseURL(fmt.Sprintf("https://%s", bmcIP)).SetTLSClientConfig(tlsCfg)

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
