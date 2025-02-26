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
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/Masterminds/sprig/v3"
)

var (
	//go:embed bf.cfg.template
	tmplData string
	tmpl     = template.Must(template.New("").Funcs(sprig.FuncMap()).Parse(tmplData))
)

type BFCFGData struct {
	KubeadmJoinCMD             string
	DPUHostName                string
	BFGCFGParams               []string
	UbuntuPassword             string
	NVConfigParams             string
	Sysctl                     []string
	ConfigFiles                []BFCFGWriteFile
	OVSRawScript               string
	KernelParameters           string
	ContainerdRegistryEndpoint string
	SFNum                      int
	// AdditionalReboot adds an extra reboot during the DPU provisioning. This is required in some environments.
	AdditionalReboot bool
}

type BFCFGWriteFile struct {
	Path        string
	IsAppend    bool
	Content     string
	Permissions string
}

func Generate(flavor *provisioningv1.DPUFlavor, dpuName, joinCmd string, additionalReboot bool) ([]byte, error) {
	config := &BFCFGData{
		KubeadmJoinCMD:   joinCmd,
		DPUHostName:      dpuName,
		AdditionalReboot: additionalReboot,
		KernelParameters: strings.TrimSpace(strings.Join(flavor.Spec.Grub.KernelParameters, " ")),
	}

	config.ContainerdRegistryEndpoint = flavor.Spec.ContainerdConfig.RegistryEndpoint

	config.BFGCFGParams, config.UbuntuPassword = bfcfgParams(flavor)
	// currently, there can be at most one nvconfig, and that nvconfig applies to all devices on a host
	if len(flavor.Spec.NVConfig) > 0 {
		config.NVConfigParams = strings.Join(flavor.Spec.NVConfig[0].Parameters, " ")
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

	if num, ok := getPFTotalSFFromFlavor(flavor); ok {
		config.SFNum = num
	}

	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, config); err != nil {
		return nil, fmt.Errorf("execute template failed, err: %v", err)
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
