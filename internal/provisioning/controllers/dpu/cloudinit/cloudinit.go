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
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/Masterminds/sprig/v3"
	corev1 "k8s.io/api/core/v1"
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
	Kubeconfig             string
	IsRedfish              bool
	ControlPlaneMTU        int
	DPUName                string
	DPUNamespace           string
	DPUUID                 string
	DPUAgentRepoURL        string
	DPUFlavorYAML          string
	UbuntuPassword         string
	ConfigFiles            []writeFile
	OVSRawScript           string
	OOBNetwork             bool
	RedfishInterface       bool
}

// applyFlavor populates the flavor-derived fields from the given DPUFlavor.
func (p *Params) applyFlavor(flavor *provisioningv1.DPUFlavor) error {
	flavorBytes, err := yaml.Marshal(flavor)
	if err != nil {
		return fmt.Errorf("marshaling DPUFlavor: %w", err)
	}
	p.DPUFlavorYAML = string(flavorBytes)
	p.UbuntuPassword = ExtractUbuntuPassword(flavor)
	p.OVSRawScript = flavor.Spec.OVS.RawConfigScript
	p.OOBNetwork = p.IsRedfish
	p.RedfishInterface = p.IsRedfish
	for _, f := range flavor.Spec.ConfigFiles {
		p.ConfigFiles = append(p.ConfigFiles, writeFile{
			Path:        f.Path,
			Permissions: f.Permissions,
			IsAppend:    f.Operation == provisioningv1.FileAppend,
			Content:     f.Raw,
		})
	}
	return nil
}

type writeFile struct {
	Path        string
	IsAppend    bool
	Content     string
	Permissions string
}

// GenerateFiles generates the cloud-init files needed to provision a DPU.
// It returns two files:
//   - networkCfg (dpf.cfg): disables cloud-init's default network config
//     (ref: https://docs.cloud-init.io/en/latest/reference/network-config.html).
//   - userData: cloud-init user-data executed during boot to configure the DPU
//     (ref: https://docs.cloud-init.io/en/latest/explanation/boot.html).
func GenerateFiles(ctx context.Context, controllerCtx *util.ControllerContext, dpu *provisioningv1.DPU, kubeadmSecret *corev1.Secret, flavor *provisioningv1.DPUFlavor) (networkCfg File, userData File, err error) {
	params, _, err := ResolveParams(ctx, controllerCtx, dpu, kubeadmSecret)
	if err != nil {
		return File{}, File{}, err
	}
	networkCfg = GenerateNetworkCfg()
	userData, err = GenerateUserData(flavor, params)
	if err != nil {
		return File{}, File{}, err
	}
	return networkCfg, userData, nil
}

// ResolveParams builds the Params needed to provision a DPU by reading the
// DPFOperatorConfig and the DPU/Secret objects. It also returns the
// DPFOperatorConfig for callers that need it (e.g. bf.cfg template resolution).
func ResolveParams(ctx context.Context, controllerCtx *util.ControllerContext, dpu *provisioningv1.DPU, kubeadmSecret *corev1.Secret) (Params, operatorv1.DPFOperatorConfig, error) {
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

	var dpuAgentRepoURL string
	var kubeconfig string
	if isRedfish {
		if dpfOperatorConfig.Spec.Overrides == nil || dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerVIP == nil || dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerPort == nil {
			return Params{}, operatorv1.DPFOperatorConfig{}, fmt.Errorf("KubernetesAPIServerVIP and KubernetesAPIServerPort must be set in DPFOperatorConfig for zero-trust mode")
		}
		apiServerAddress := fmt.Sprintf("https://%s:%d", *dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerVIP, *dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerPort)
		// TODO: each DPU agent should use its own kubeconfig instead of sharing one.
		kubeconfigData, err := cutil.GenerateKubeconfig(ctx, controllerCtx.Client, apiServerAddress, dpu.Namespace)
		if err != nil {
			return Params{}, operatorv1.DPFOperatorConfig{}, fmt.Errorf("generating dpu-agent kubeconfig: %w", err)
		}
		kubeconfig = string(kubeconfigData)
		bfbRegistryAddr, err := cutil.GetBFBRegistryAddressWithPort(ctx, controllerCtx.Client, os.Getenv("POD_NAMESPACE"), controllerCtx.Options.BFBRegistry)
		if err != nil {
			return Params{}, operatorv1.DPFOperatorConfig{}, fmt.Errorf("bfb-registry address with port: %w", err)
		}
		dpuAgentRepoURL = strings.TrimRight(bfbRegistryAddr, "/") + "/deb"
	} else {
		dpuAgentRepoURL = "http://[fe80::1%25tmfifo_net0]:11029/deb"
	}

	params := Params{
		DPUHostName:            cutil.GenerateNodeName(dpu),
		KubeadmSecretName:      kubeadmSecret.Name,
		KubeadmSecretNamespace: kubeadmSecret.Namespace,
		Kubeconfig:             kubeconfig,
		IsRedfish:              isRedfish,
		ControlPlaneMTU:        controlPlaneMTU,
		DPUName:                dpu.Name,
		DPUNamespace:           dpu.Namespace,
		DPUUID:                 string(dpu.UID),
		DPUAgentRepoURL:        dpuAgentRepoURL,
	}
	return params, dpfOperatorConfig, nil
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

// GenerateUserData renders the cloud-init user-data file using the provided
// DPUFlavor and Params.
func GenerateUserData(flavor *provisioningv1.DPUFlavor, params Params) (File, error) {
	if err := params.applyFlavor(flavor); err != nil {
		return File{}, err
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
