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

package e2e

import (
	"os"

	"sigs.k8s.io/yaml"
)

// config represents the raw configuration data loaded from YAML files.
// This struct contains file paths and basic configuration values that are used
// to load and initialize the actual Kubernetes objects for testing.
//
// - Loaded from YAML config files (e.g., config-quick.yaml, config-provisioning.yaml)
// - Used by applyConfig() to populate systemTestInput with actual Kubernetes objects
// - Used by systemTestInput for object loading and initialization
//
// Fields are grouped by the suite that consumes them. Most fields are only
// consumed by specific suites and are therefore optional pointers: they are
// loaded when set and validated where the consuming suite loads them
// (applyConfig, applySDNConfig, applyVPCOVNConfig, applyWeaveConfig).
// validateRequiredConfigFields enforces the fields that are mandatory for the
// selected suites before any object is loaded.
type config struct {
	// DPFOperatorConfigPath, if set, is the DPFOperatorConfig manifest to use
	// instead of the one generated in Go. Upgrade path configs set this so each
	// phase uses the config shape of the release it installs or validates.
	DPFOperatorConfigPath *string `json:"dpfOperatorConfig,omitempty"`

	// Required by every suite (enforced by validateRequiredConfigFields).
	DPUClusterPaths          []string `json:"dpuClusters"`
	DPUDeploymentPath        string   `json:"dpuDeployment"`
	DPUServiceConfiguration  string   `json:"dpuServiceConfiguration"`
	DPUServiceTemplatePath   string   `json:"dpuServiceTemplate"`
	IPPoolDPUServiceIPAMPath string   `json:"ipPoolDPUServiceIPAM"`

	// Required by every suite except upgrade phases, which create only the
	// objects their phase steps declare (enforced by
	// validateRequiredConfigFields, loaded by applyConfig when set).
	CIDRPoolDPUServiceIPAMPath      *string `json:"cidrPoolDPUServiceIPAM,omitempty"`
	DPUServiceChainPath             *string `json:"dpuServiceChain,omitempty"`
	DPUServiceCredentialRequestPath *string `json:"dpuServiceCredentialRequest,omitempty"`
	DPUServiceInterfacePath         *string `json:"dpuServiceInterface,omitempty"`
	DPUServicePath                  *string `json:"dpuService,omitempty"`
	DPUSetPath                      *string `json:"dpuSet,omitempty"`

	// SDN suite only (loaded and validated by applySDNConfig).
	DPUServiceHBNPath *string `json:"dpuServiceHBN,omitempty"`
	DPUServiceNADPath *string `json:"dpuServiceNAD,omitempty"`

	// Shared by the SDN and VPC OVN suites (loaded and validated by both
	// applySDNConfig and applyVPCOVNConfig).
	DPUServiceChainTemplatePath     *string `json:"dpuServiceChainTemplate,omitempty"`
	DPUServiceInterfaceTemplatePath *string `json:"dpuServiceInterfaceTemplate,omitempty"`
	DPUServiceIPAMTemplatePath      *string `json:"dpuServiceIPAMTemplate,omitempty"`

	// VPC OVN suite only (loaded and validated by applyVPCOVNConfig).
	DPUServiceOVNCentralPath       *string `json:"dpuServiceOVNCentral,omitempty"`
	DPUServiceOVNControllerPath    *string `json:"dpuServiceOVNController,omitempty"`
	DPUServiceVPCOVNControllerPath *string `json:"dpuServiceVPCOVNController,omitempty"`
	DPUServiceVPCOVNNodePath       *string `json:"dpuServiceVPCOVNNode,omitempty"`

	// Shared by the VPC OVN and Weave suites (loaded and validated by both
	// applyVPCOVNConfig and applyWeaveConfig).
	DHCPDaemonSetPath *string `json:"dhcpDaemonSet,omitempty"`

	// OVN Kubernetes/HBN performance scenario (loaded by applyConfig when
	// set; config-provisioning-physical-perf.yaml is the only config that
	// sets them).
	DPUServiceConfigurationHBNPath *string  `json:"dpuServiceConfigurationHBN,omitempty"`
	DPUServiceConfigurationOVNPath *string  `json:"dpuServiceConfigurationOVN,omitempty"`
	DPUServiceInterfaceOVNPath     *string  `json:"dpuServiceInterfaceOVN,omitempty"`
	DPUServiceInterfacesHBNPaths   []string `json:"dpuServiceInterfacesHBN,omitempty"`
	DPUServiceTemplateHBNPath      *string  `json:"dpuServiceTemplateHBN,omitempty"`
	DPUServiceTemplateOVNPath      *string  `json:"dpuServiceTemplateOVN,omitempty"`
	OVNCredentialRequestPath       *string  `json:"ovnCredentialRequest,omitempty"`

	// Upgrade suite: the extra DPUServiceTemplate/DPUServiceConfiguration
	// revision the upgrade phases roll the DPUDeployment to (loaded by
	// applyConfig when set, consumed by createAdditionalDPUServiceDependencies).
	AdditionalDPUServiceConfigurationPath *string `json:"additionalDPUServiceConfiguration,omitempty"`
	AdditionalDPUServiceTemplatePath      *string `json:"additionalDPUServiceTemplate,omitempty"`

	// Provisioning objects and environment settings (loaded by applyConfig
	// when set; which configs set them depends on the environment, not the
	// selected suite).
	BFBPath                           *string  `json:"bfb,omitempty"`
	BlueFieldSoftwarePath             *string  `json:"blueFieldSoftware,omitempty"`
	DPUClusterPrerequisiteObjectPaths []string `json:"dpuClusterPrerequisiteObjectPath"`
	DPUDiscoveryPath                  *string  `json:"dpuDiscovery,omitempty"`
	DPUFlavorPath                     *string  `json:"dpuFlavor,omitempty"`               // flavor name is propagated into dpuSet and dpuDeployment
	NodeRebootConfigMap               string   `json:"nodeRebootConfigMap,omitempty"`     // required for ZeroTrust (validateFlags)
	NodeRebootConfigMapPath           string   `json:"nodeRebootConfigMapPath,omitempty"` // required for ZeroTrust (validateFlags)
	NumberOfDPUNodes                  int      `json:"numberOfDPUNodes"`
	NumberOfDPUsPerNode               int      `json:"numberOfDPUsPerNode,omitempty"`
	ProvisioningControllerPVCPath     *string  `json:"provisioningControllerPVC,omitempty"`
	UseExternalNodeReboot             bool     `json:"useExternalNodeReboot,omitempty"`
}

func readConfig(path string) (*config, error) {
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	conf := &config{}
	if err = yaml.UnmarshalStrict(configData, conf); err != nil {
		return nil, err
	}
	return conf, nil
}
