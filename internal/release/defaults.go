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

package release

import (
	_ "embed"
	"errors"

	"sigs.k8s.io/yaml"
)

//go:embed manifests/defaults.yaml
var defaultsContent []byte

// Defaults structure contains the default artifacts that the operators should deploy
type Defaults struct {
	DMSImage               string `yaml:"dmsImage"`
	HostNetworkSetupImage  string `yaml:"hostNetworkSetupImage"`
	DPFSystemImage         string `yaml:"dpfSystemImage"`
	DPFToolsImage          string `yaml:"dpfToolsImage"`
	DPUNetworkingHelmChart string `yaml:"dpuNetworkingHelmChart"`

	MultusImage  string `yaml:"multusImage"`
	SRIOVDPImage string `yaml:"sriovDPImage"`
	NVIPAMImage  string `yaml:"nvipamImage"`
	OVSCNIImage  string `yaml:"ovsCniImage"`

	// CustomOVNKubernetesDPUImage is the default custom OVN Kubernetes image that should be deployed to the DPU
	// enabled workers.
	CustomOVNKubernetesImage string `yaml:"customOVNKubernetesImage"`
}

// Parse parses the defaults from the embedded generated YAML file
// TODO: Add more validations here as needed.
func (d *Defaults) Parse() error {
	err := yaml.Unmarshal(defaultsContent, d)
	if err != nil {
		return err
	}
	if len(d.CustomOVNKubernetesImage) == 0 {
		return errors.New("customOVNKubernetesDPUImage can't be empty")
	}
	if len(d.DMSImage) == 0 {
		return errors.New("dmsImage can't be empty")
	}
	if len(d.HostNetworkSetupImage) == 0 {
		return errors.New("hostNetworkSetupImage can't be empty")
	}
	if len(d.DPFSystemImage) == 0 {
		return errors.New("dpfSystemImage can't be empty")
	}
	if len(d.DPFToolsImage) == 0 {
		return errors.New("dpfToolsImage can't be empty")
	}
	if len(d.DPUNetworkingHelmChart) == 0 {
		return errors.New("DPUNetworkingHelmChart can't be empty")
	}
	if len(d.MultusImage) == 0 {
		return errors.New("multusImage can't be empty")
	}
	if len(d.SRIOVDPImage) == 0 {
		return errors.New("sriovDPImage can't be empty")
	}
	if len(d.NVIPAMImage) == 0 {
		return errors.New("nvipamImage can't be empty")
	}
	if len(d.OVSCNIImage) == 0 {
		return errors.New("ovsCniImage can't be empty")
	}
	return nil
}

// NewDefaults creates a new defaults object
func NewDefaults() *Defaults {
	return &Defaults{}
}
