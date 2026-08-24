/*
Copyright 2026 NVIDIA

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

package cloudinit

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	provisioningconstants "github.com/nvidia/doca-platform/internal/provisioning/constants"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	httputils "github.com/nvidia/doca-platform/internal/provisioning/utils/http"

	"github.com/Masterminds/sprig/v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

var (
	//go:embed dpf.cfg
	dpfCfgContent []byte

	//go:embed user-data
	userDataTemplate []byte
)

const (
	DPFCfgPath    = "/etc/cloud/cloud.cfg.d/dpf.cfg"
	DPFCfgPerms   = "0644"
	UserDataPath  = "/var/lib/cloud/seed/nocloud-net/user-data"
	UserDataPerms = "0600"
)

// File represents a cloud-init file with its target path, permissions, and rendered content.
type File struct {
	Path        string
	Permissions string
	Content     string
}

// Params holds the parameters for cloud-init file generation.
type Params struct {
	DPUHostName            string
	KubeadmSecretName      string
	KubeadmSecretNamespace string
	BootstrapKubeconfig    string
	// SpiffeMode enables SPIRE/spiffe-helper rendering and SPIFFE dpu-agent flags.
	SpiffeMode bool
	// SPIFFEKubeconfig is the tokenFile kubeconfig for SPIFFE-mode DPUs.
	SPIFFEKubeconfig string
	// SPIRETrustBundle, when non-empty, is the initial trust bundle emitted on a SPIFFE-mode DPU.
	// It is mutually exclusive with BootstrapKubeconfig (a DPU uses exactly one identity mode).
	SPIRETrustBundle       string
	SPIRETrustBundlePath   string
	SPIRETrustBundleFormat string
	SPIREServerHost        string
	SPIREServerPort        int
	SPIRETrustDomain       string
	KubeAPIAudience        string
	SpiffeTokenPath        string
	SpiffeAgentSocketPath  string
	SpiffePluginPath       string
	// SpiffeCertDir and SpiffeTokenFileName are SpiffeTokenPath split into the
	// spiffe-helper cert_dir and the JWT SVID file name (relative to cert_dir).
	SpiffeCertDir        string
	SpiffeTokenFileName  string
	SpiffeAgentSocketDir string
	ControlPlaneMTU      int
	DPUName              string
	DPUNamespace         string
	DPUUID               string
	DPUType              provisioningv1.DPUType
	DPUAgentRepoURL      string
	DPUFlavorYAML        string
	UbuntuPassword       string
	ConfigFiles          []WriteFile
	OVSRawScript         string
	OOBNetwork           bool
	RedfishInterface     bool
	BFBRegistryURL       string
	AstraEnabled         bool
	NICDeviceCount       int

	// SpiffeTokenExchangeEndpoint enables optional exchange before writing the token.
	SpiffeTokenExchangeEndpoint string
	// CATrustBundle holds one or more concatenated PEM certificates for the DPF CA. When set, it is
	// written to the DPU OS trust store via cloud-init and installed with update-ca-certificates so
	// the DPU OS (apt over HTTPS, etc.) validates the bfb-registry server certificate.
	CATrustBundle string
}

// ApplyFlavor populates the flavor-derived fields from the given DPUFlavor.
func (p *Params) ApplyFlavor(flavor *provisioningv1.DPUFlavor) error {
	flavorBytes, err := yaml.Marshal(flavor)
	if err != nil {
		return fmt.Errorf("marshaling DPUFlavor: %w", err)
	}
	p.DPUFlavorYAML = string(flavorBytes)
	p.UbuntuPassword = ExtractUbuntuPassword(flavor)
	p.OVSRawScript = flavor.Spec.OVS.RawConfigScript
	for _, f := range flavor.Spec.ConfigFiles {
		if f.Type != nil && *f.Type != provisioningv1.ConfigFileTypeCloudInit {
			continue
		}
		// Only inline content is written through cloud-init.
		// ConfigMap-backed content is resolved later by dpu-agent.
		if f.ContentFrom != nil {
			continue
		}
		rawContent := ""
		if f.Raw != nil {
			rawContent = *f.Raw
		}
		p.ConfigFiles = append(p.ConfigFiles, WriteFile{
			Path:        f.Path,
			Permissions: f.Permissions,
			IsAppend:    f.Operation == provisioningv1.FileAppend,
			Content:     rawContent,
		})
	}
	return nil
}

type WriteFile struct {
	Path        string
	IsAppend    bool
	Content     string
	Permissions string
}

// ResolveParams builds the Params needed to provision a DPU by reading the
// DPFOperatorConfig and the DPU/Secret objects, and applying flavor-derived
// fields. It also returns the DPFOperatorConfig for callers that need it
// (e.g. bf.cfg template resolution).
func ResolveParams(ctx context.Context, controllerCtx *util.ControllerContext, dpu *provisioningv1.DPU, flavor *provisioningv1.DPUFlavor) (Params, operatorv1.DPFOperatorConfig, error) {
	var configList operatorv1.DPFOperatorConfigList
	if err := controllerCtx.List(ctx, &configList); err != nil {
		return Params{}, operatorv1.DPFOperatorConfig{}, fmt.Errorf("list DPFOperatorConfigs: %w", err)
	}
	if len(configList.Items) == 0 || len(configList.Items) > 1 {
		return Params{}, operatorv1.DPFOperatorConfig{}, fmt.Errorf("exactly one DPFOperatorConfig necessary")
	}
	if configList.Items[0].Spec.Networking == nil {
		return Params{}, operatorv1.DPFOperatorConfig{}, fmt.Errorf("DPFOperatorConfig networking section is missing")
	}
	dpfOperatorConfig := configList.Items[0]
	controlPlaneMTU := *dpfOperatorConfig.Spec.Networking.ControlPlaneMTU

	isRedfish := controllerCtx.Options.DPUInstallInterface == string(provisioningv1.InstallViaRedFish)

	params := Params{
		DPUHostName:            cutil.GenerateNodeName(dpu),
		KubeadmSecretName:      cutil.KubeadmJoinSecretName(dpu.Name),
		KubeadmSecretNamespace: dpu.Namespace,
		RedfishInterface:       isRedfish,
		OOBNetwork:             isRedfish,
		ControlPlaneMTU:        controlPlaneMTU,
		DPUName:                dpu.Name,
		DPUNamespace:           dpu.Namespace,
		DPUUID:                 string(dpu.UID),
		DPUType:                dpu.Status.DPUType,
		NICDeviceCount:         provisioningconstants.DefaultNICDeviceCount,
	}
	if dpu.Spec.AstraEnabled != nil {
		params.AstraEnabled = *dpu.Spec.AstraEnabled
	}
	nicDeviceCount, err := resolveDPUDeviceNICDeviceCount(ctx, controllerCtx, dpu)
	if err != nil {
		return Params{}, operatorv1.DPFOperatorConfig{}, err
	}
	params.NICDeviceCount = nicDeviceCount

	if isRedfish {
		if err := resolveRedfishRepoURLs(ctx, controllerCtx, &params); err != nil {
			return Params{}, operatorv1.DPFOperatorConfig{}, err
		}
	} else {
		params.DPUAgentRepoURL = "http://[fe80::1%25tmfifo_net0]:11029/deb"
	}
	if err := params.ApplyFlavor(flavor); err != nil {
		return Params{}, operatorv1.DPFOperatorConfig{}, fmt.Errorf("applying flavor: %w", err)
	}

	caBundle, err := resolveCATrustBundle(ctx, controllerCtx, &dpfOperatorConfig)
	if err != nil {
		return Params{}, operatorv1.DPFOperatorConfig{}, err
	}
	params.CATrustBundle = caBundle

	return params, dpfOperatorConfig, nil
}

// resolveRedfishRepoURLs sets the dpu-agent apt repo and bfb-registry URLs on params for the
// zero-trusted (Redfish) install mode. bfb-registry is HTTPS-only; the DPU OS trusts the DPF CA
// (delivered via cloud-init), so the dpu-agent apt repo (dpf.list) and the NIC firmware base both
// resolve to https here. The host-trusted tmfifo repo (non-Redfish) stays HTTP (out of scope).
func resolveRedfishRepoURLs(ctx context.Context, controllerCtx *util.ControllerContext, params *Params) error {
	bfbRegistryAddr := controllerCtx.Options.BFBRegistryLoadBalancer
	if bfbRegistryAddr == "" {
		addr, err := cutil.GetBFBRegistryAddressWithPort(ctx, controllerCtx.Client, os.Getenv("POD_NAMESPACE"), controllerCtx.Options.BFBRegistry)
		if err != nil {
			return fmt.Errorf("bfb-registry address with port: %w", err)
		}
		bfbRegistryAddr = addr
	}
	base := httputils.EnsureHTTPSScheme(strings.TrimRight(bfbRegistryAddr, "/"))
	params.DPUAgentRepoURL = base + "/deb"
	params.BFBRegistryURL = base
	return nil
}

// resolveCATrustBundle reads the public DPF CA trust bundle the Operator maintains so it can be
// delivered to the DPU OS via cloud-init. It is best-effort: a missing ConfigMap or key yields an
// empty bundle (the cloud-init template then skips writing it) rather than failing provisioning.
func resolveCATrustBundle(ctx context.Context, controllerCtx *util.ControllerContext, dpfOperatorConfig *operatorv1.DPFOperatorConfig) (string, error) {
	name := dpfOperatorConfig.GetCATrustBundleConfigMapName()
	cm := &corev1.ConfigMap{}
	if err := controllerCtx.Get(ctx, types.NamespacedName{Namespace: dpfOperatorConfig.Namespace, Name: name}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("CA trust bundle ConfigMap not found; DPU OS will not receive a DPF CA certificate", "configMap", name, "namespace", dpfOperatorConfig.Namespace)
			return "", nil
		}
		return "", fmt.Errorf("getting CA trust bundle ConfigMap %s/%s: %w", dpfOperatorConfig.Namespace, name, err)
	}
	return strings.TrimSpace(cm.Data[operatorv1.CATrustBundleKey]), nil
}

// GenerateNetworkCfg returns the dpf.cfg file that disables cloud-init's
// default network configuration
func GenerateNetworkCfg() File {
	return File{
		Path:        DPFCfgPath,
		Permissions: DPFCfgPerms,
		Content:     string(dpfCfgContent),
	}
}

// GenerateUserData renders the cloud-init user-data file from the given Params.
// The caller must ensure ApplyFlavor has already been called (e.g. via
// ResolveParams).
func GenerateUserData(params Params) (File, error) {
	if params.NICDeviceCount <= 0 {
		params.NICDeviceCount = provisioningconstants.DefaultNICDeviceCount
	}
	tmpl, err := template.New("user-data").Funcs(sprig.FuncMap()).Parse(string(userDataTemplate))
	if err != nil {
		return File{}, fmt.Errorf("parsing user-data template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return File{}, fmt.Errorf("executing user-data template: %w", err)
	}

	return File{
		Path:        UserDataPath,
		Permissions: UserDataPerms,
		Content:     buf.String(),
	}, nil
}

// ExtractUbuntuPassword returns the ubuntu_PASSWORD value from the DPUFlavor's
// BFCfgParameters, single-quoted if not already.
//
// NOTE: BFCfgParameters is a BF3/bf.cfg concept. On BF3 this is the natural
// place for the password, but BF4 has no bf.cfg so sourcing it from here is
// a stopgap. Once a dedicated password field is added to the DPUFlavor spec
// for BF4, this function should read from there instead.
func ExtractUbuntuPassword(flavor *provisioningv1.DPUFlavor) string {
	for _, param := range flavor.Spec.BFCfgParameters {
		parts := strings.SplitN(param, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) != "ubuntu_PASSWORD" {
			continue
		}
		passwd := strings.TrimSpace(parts[1])
		if passwd != "" && !strings.HasPrefix(passwd, "'") {
			passwd = fmt.Sprintf("'%s'", passwd)
		}
		return passwd
	}
	return ""
}

func resolveDPUDeviceNICDeviceCount(ctx context.Context, controllerCtx *util.ControllerContext, dpu *provisioningv1.DPU) (int, error) {
	if strings.TrimSpace(dpu.Spec.DPUDeviceName) == "" {
		return provisioningconstants.DefaultNICDeviceCount, nil
	}

	dpuDevice := &provisioningv1.DPUDevice{}
	if err := controllerCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		return 0, fmt.Errorf("getting DPUDevice %s/%s for NIC device count: %w", dpu.Namespace, dpu.Spec.DPUDeviceName, err)
	}

	if dpuDevice.Spec.NICDeviceCount == nil {
		return provisioningconstants.DefaultNICDeviceCount, nil
	}
	nicDeviceCount := *dpuDevice.Spec.NICDeviceCount
	if nicDeviceCount < provisioningconstants.MinNICDeviceCount || nicDeviceCount > provisioningconstants.MaxNICDeviceCount {
		return 0, fmt.Errorf("invalid DPUDevice spec.nicDeviceCount %d on %s/%s, expected integer in range [%d, %d]",
			nicDeviceCount, dpuDevice.Namespace, dpuDevice.Name, provisioningconstants.MinNICDeviceCount, provisioningconstants.MaxNICDeviceCount)
	}
	return nicDeviceCount, nil
}
