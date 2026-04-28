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
	"errors"
	"os"
	"sync"

	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

const (
	// defaultsFile is the file path where the DPF defaults are stored at runtime.
	// Loading defaults from a file instead of embedding improves Docker build cache
	// efficiency by preventing defaults changes from invalidating binary layers.
	defaultsFile = "/etc/dpf-defaults.yaml"
)

// defaultsContent is loaded lazily from the defaults file when first needed.
// This avoids warnings for binaries (like dpfctl) that import this package
// only for version info and never use defaults.
var (
	defaultsContent []byte
	defaultsOnce    sync.Once
)

func loadDefaults() {
	defaultsOnce.Do(func() {
		var err error
		defaultsContent, err = os.ReadFile(defaultsFile)
		if err != nil {
			klog.Warningf("Unable to load defaults file from path %q: %v", defaultsFile, err)
			defaultsContent = []byte{}
		}
	})
}

// Defaults structure contains the default artifacts that the operators should deploy
type Defaults struct {
	DMSImage                   string `yaml:"dmsImage"`
	DPFSystemImage             string `yaml:"dpfSystemImage"`
	CNIInstallerImage          string `yaml:"cniInstallerImage"`
	DPUNetworkingHelmChart     string `yaml:"dpuNetworkingHelmChart"`
	OVSCNIImage                string `yaml:"ovsCniImage"`
	BFBRegistryImage           string `yaml:"bfbRegistryImage"`
	KeepalivedImage            string `yaml:"keepalivedImage"`
	NodeSRIOVDevicePluginImage string `yaml:"nodeSRIOVDevicePluginImage"`
}

// Parse parses the defaults from the embedded generated YAML file
// TODO: Add more validations here as needed.
func (d *Defaults) Parse() error {
	loadDefaults()
	err := yaml.Unmarshal(defaultsContent, d)
	if err != nil {
		return err
	}
	if len(d.DMSImage) == 0 {
		return errors.New("dmsImage can't be empty")
	}
	if len(d.DPFSystemImage) == 0 {
		return errors.New("dpfSystemImage can't be empty")
	}
	if len(d.CNIInstallerImage) == 0 {
		return errors.New("cniInstallerImage can't be empty")
	}
	if len(d.DPUNetworkingHelmChart) == 0 {
		return errors.New("DPUNetworkingHelmChart can't be empty")
	}
	if len(d.OVSCNIImage) == 0 {
		return errors.New("ovsCniImage can't be empty")
	}
	if len(d.KeepalivedImage) == 0 {
		return errors.New("keepalivedImage can't be empty")
	}
	if len(d.NodeSRIOVDevicePluginImage) == 0 {
		return errors.New("nodeSRIOVDevicePluginImage can't be empty")
	}

	return nil
}

// NewDefaults creates a new defaults object
func NewDefaults() *Defaults {
	return &Defaults{}
}

// SetDefaultsContentForTesting allows injecting defaults content for testing purposes.
// This should only be used in tests.
func SetDefaultsContentForTesting(content []byte) {
	// Reset the Once so loadDefaults() won't overwrite the injected content,
	// and use a pre-fired Once to prevent file loading.
	defaultsOnce = sync.Once{}
	defaultsOnce.Do(func() {})
	defaultsContent = content
}
