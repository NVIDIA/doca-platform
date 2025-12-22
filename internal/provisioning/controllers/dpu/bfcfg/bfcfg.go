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

package bfcfg

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	"github.com/Masterminds/sprig/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// The maximum size of the bf.cfg file is expanded to 128k since DOCA 2.8
	MaxBFSize     = 1024 * 128
	MaxTrustedSfs = 10
)

var (
	//go:embed bf.cfg.template
	defaultBFCFGTemplateData string
	// ConfigMapDataKey is the key in the configmap where the bfb.cfg.template is stored if overwritten.
	ConfigMapDataKey = "BF_CFG_TEMPLATE"
	defaultTemplate  = template.Must(template.New("").Funcs(sprig.FuncMap()).Parse(defaultBFCFGTemplateData))
)

func getTemplate(bfbCFGFilePath string) (*template.Template, error) {
	if bfbCFGFilePath == "" {
		return defaultTemplate, nil
	}
	data, err := os.ReadFile(bfbCFGFilePath)
	if err != nil {
		return nil, err
	}
	return template.New("").Funcs(sprig.FuncMap()).Parse(string(data))
}

type BFCFGData struct {
	KubeadmJoinCMD             string
	DPUHostName                string
	BFGCFGParams               []string
	UbuntuPassword             string
	NVConfigParams             []NVConfigEntry
	Sysctl                     []string
	ConfigFiles                []BFCFGWriteFile
	OVSRawScript               string
	KernelParameters           string
	ContainerdRegistryEndpoint string
	SFNum                      int
	TrustedSFs                 int
	// AdditionalReboot adds an extra reboot during the DPU provisioning. This is required in some environments.
	AdditionalReboot bool
	// OOBNetwork is a flag to indicate if the DPU accesses DPU cluster via the OOB interface.
	OOBNetwork       bool
	RedfishInterface bool
	ControlPlaneMTU  int
	// The interface name for all the PFs.
	PFs     []string
	DpuMode string
}

type NVConfigEntry struct {
	Device     string // Device identifier: "*" (wildcard for all), "p0"/"P0" (port 0), "p1"/"P1" (port 1)
	Parameters string // Space-joined parameters
}

type BFCFGWriteFile struct {
	Path        string
	IsAppend    bool
	Content     string
	Permissions string
}

func GenerateBFConfig(ctx context.Context, c client.Client, bfCFGTemplateFile string, dpu *provisioningv1.DPU, dpuNode *provisioningv1.DPUNode, dpuDevice *provisioningv1.DPUDevice, flavor *provisioningv1.DPUFlavor, joinCommand, installInterface string) ([]byte, error) {
	logger := log.FromContext(ctx)
	additionalReboot, err := shouldTriggerAdditionalReboot(ctx, dpuNode, dpu)
	if err != nil {
		return nil, err
	}

	dpfOperatorConfigList := operatorv1.DPFOperatorConfigList{}
	if err := c.List(ctx, &dpfOperatorConfigList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("list DPFOperatorConfigs: %w", err)
	}
	if len(dpfOperatorConfigList.Items) == 0 || len(dpfOperatorConfigList.Items) > 1 {
		return nil, fmt.Errorf("exactly one DPFOperatorConfig necessary")
	}
	if dpfOperatorConfigList.Items[0].Spec.Networking == nil {
		return nil, fmt.Errorf("DPFOperatorConfig networking section is missing")
	}
	if dpuDevice.Spec.NumberOfPFs == nil {
		return nil, fmt.Errorf("numberOfPFs is not set")
	}

	controlPlaneMTU := *dpfOperatorConfigList.Items[0].Spec.Networking.ControlPlaneMTU
	buf, err := Generate(flavor, cutil.GenerateNodeName(dpu), joinCommand, additionalReboot, bfCFGTemplateFile, installInterface, controlPlaneMTU, *dpuDevice.Spec.NumberOfPFs)
	if err != nil {
		return nil, err
	}
	if buf == nil {
		return nil, fmt.Errorf("failed bf.cfg creation due to buffer issue")
	}
	if len(buf) > MaxBFSize {
		return nil, fmt.Errorf("bf.cfg for %s size (%d) exceeds the maximum limit (%d)", dpu.Name, len(buf), MaxBFSize)
	}
	logger.V(3).Info(fmt.Sprintf("bf.cfg for %s has len: %d data: %s", dpu.Name, len(buf), string(buf)))

	return buf, nil
}

// shouldTriggerAdditionalReboot returns whether an additional reboot should be triggered after bfb-install
func shouldTriggerAdditionalReboot(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpu *provisioningv1.DPU) (bool, error) {
	logger := log.FromContext(ctx)

	cmd, _, err := reboot.GenerateCmd(dpuNode.Annotations, dpu.Annotations)
	if err != nil {
		logger.Error(err, "failed to generate ipmitool command")
		return false, err
	}
	dpuNodeLabels := dpuNode.GetLabels()
	if _, ok := dpuNodeLabels[cutil.DPUNodeAdditionalDPURebootLabel]; ok {
		return true, nil
	}
	if cmd == reboot.Skip {
		return true, nil
	}
	conds := append([]metav1.Condition(nil), dpu.Status.Conditions...) // shallow copy
	for _, cond := range conds {
		if cond.Type == string(provisioningv1.DPUCondFWConfigured) && cond.Message == string(provisioningv1.DPUCondMessageModeUpdate) {
			return true, nil
		}
	}
	return false, nil
}

func Generate(flavor *provisioningv1.DPUFlavor, dpuName, joinCmd string, additionalReboot bool, bfbCFGFilepath string, installInterface string, controlPlaneMTU int, numberOfPFs int) ([]byte, error) {
	config := &BFCFGData{
		KubeadmJoinCMD:   joinCmd,
		DPUHostName:      dpuName,
		AdditionalReboot: additionalReboot,
		KernelParameters: strings.TrimSpace(strings.Join(flavor.Spec.Grub.KernelParameters, " ")),
		ControlPlaneMTU:  controlPlaneMTU,
		RedfishInterface: installInterface == string(provisioningv1.InstallViaRedFish),
		DpuMode:          string(flavor.Spec.DpuMode),
	}

	config.ContainerdRegistryEndpoint = flavor.Spec.ContainerdConfig.RegistryEndpoint

	config.BFGCFGParams, config.UbuntuPassword = bfcfgParams(flavor)

	// Process all nvconfig entries for device-specific configurations
	config.NVConfigParams = make([]NVConfigEntry, 0, len(flavor.Spec.NVConfig))
	for _, nvcfg := range flavor.Spec.NVConfig {
		device := "*"
		if nvcfg.Device != nil {
			device = strings.TrimSpace(*nvcfg.Device)
		}
		config.NVConfigParams = append(config.NVConfigParams, NVConfigEntry{
			Device:     device,
			Parameters: strings.Join(nvcfg.Parameters, " "),
		})
	}

	config.Sysctl = flavor.Spec.Sysctl.Parameters
	for _, f := range flavor.Spec.ConfigFiles {
		config.ConfigFiles = append(config.ConfigFiles, BFCFGWriteFile{
			Path:        f.Path,
			Permissions: f.Permissions,
			IsAppend:    f.Operation == provisioningv1.FileAppend,
			Content:     f.Raw,
		})
	}
	config.OVSRawScript = flavor.Spec.OVS.RawConfigScript
	if installInterface == string(provisioningv1.InstallViaRedFish) {
		config.OOBNetwork = true
	}

	if num, ok := getPFTotalSFFromFlavor(flavor); ok {
		config.SFNum = num
	}

	if num, ok := getTrustedSFFromFlavor(flavor); ok {
		config.TrustedSFs = num
	}

	config.PFs = []string{}
	for i := 0; i < numberOfPFs; i++ {
		config.PFs = append(config.PFs, fmt.Sprintf("p%d", i))
		config.PFs = append(config.PFs, fmt.Sprintf("pf%dhpf", i))
	}

	bfbCFGTemplate, err := getTemplate(bfbCFGFilepath)
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(nil)
	if err := bfbCFGTemplate.Execute(buf, config); err != nil {
		return nil, fmt.Errorf("execute bfbCFGTemplate failed, err: %v", err)
	}
	return buf.Bytes(), nil
}

func bfcfgParams(flavor *provisioningv1.DPUFlavor) ([]string, string) {
	var ret []string
	var passwd string
	for _, param := range flavor.Spec.BFCfgParameters {
		info := strings.Split(param, "=")
		if len(info) != 2 {
			ret = append(ret, param)
			continue
		}
		key := strings.TrimSpace(info[0])
		if key != "ubuntu_PASSWORD" {
			ret = append(ret, param)
			continue
		}
		passwd = strings.TrimSpace(info[1])
	}
	if passwd == "" {
		return ret, passwd
	}
	if !strings.HasPrefix(passwd, "'") {
		passwd = fmt.Sprintf("'%s'", passwd)
	}
	return ret, passwd
}

func getPFTotalSFFromFlavor(flavor *provisioningv1.DPUFlavor) (int, bool) {
	regex := regexp.MustCompile(`^PF_TOTAL_SF=([0-9]+)`)
	for _, nvconfig := range flavor.Spec.NVConfig {
		for _, parmeter := range nvconfig.Parameters {
			matches := regex.FindStringSubmatch(parmeter)
			if len(matches) == 2 {
				if num, err := strconv.Atoi(matches[1]); err == nil {
					return num, true
				}
			}
		}
	}
	return 0, false
}

func getTrustedSFFromFlavor(flavor *provisioningv1.DPUFlavor) (int, bool) {
	if flavor.Annotations != nil {
		trustedSFCountFromAnnotation, found := flavor.Annotations[cutil.TrustedSFCount]
		if found {
			trustedSFCount, err := strconv.Atoi(trustedSFCountFromAnnotation)
			if err == nil && trustedSFCount > 0 && trustedSFCount <= MaxTrustedSfs {
				return trustedSFCount, true
			}

		}
	}
	return 0, false
}
