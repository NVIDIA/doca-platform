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
type config struct {
	DPUFlavorPath                     *string  `json:"dpuFlavor,omitempty"`
	ProvisioningControllerPVCPath     *string  `json:"provisioningControllerPVC,omitempty"`
	BFBPath                           string   `json:"bfb"`
	DPUClusterPaths                   []string `json:"dpuClusters"`
	DPUSetPath                        string   `json:"dpuSet"`
	DPUDiscoveryPath                  *string  `json:"dpuDiscovery,omitempty"`
	DPUServiceInterfacePath           string   `json:"dpuServiceInterface"`
	DPUServiceChainPath               string   `json:"dpuServiceChain"`
	DPUDeploymentPath                 string   `json:"dpuDeployment"`
	DPUServiceChainTemplatePath       string   `json:"dpuServiceChainTemplate"`
	DPUServiceInterfaceTemplatePath   string   `json:"dpuServiceInterfaceTemplate"`
	DPUServiceCredentialRequestPath   string   `json:"dpuServiceCredentialRequest"`
	IPPoolDPUServiceIPAMPath          string   `json:"ipPoolDPUServiceIPAM"`
	CIDRPoolDPUServiceIPAMPath        string   `json:"cidrPoolDPUServiceIPAM"`
	DPUServiceIPAMTemplatePath        string   `json:"dpuServiceIPAMTemplate"`
	DPUServiceNADPath                 string   `json:"dpuServiceNAD"`
	DPUServicePath                    string   `json:"dpuService"`
	DPUServiceHBNPath                 string   `json:"dpuServiceHBN"`
	DPUServiceOVNCentralPath          string   `json:"dpuServiceOVNCentral"`
	DPUServiceOVNControllerPath       string   `json:"dpuServiceOVNController"`
	DPUServiceVPCOVNControllerPath    string   `json:"dpuServiceVPCOVNController"`
	DPUServiceVPCOVNNodePath          string   `json:"dpuServiceVPCOVNNode"`
	DHCPDaemonSetPath                 string   `json:"dhcpDaemonSet"`
	DPUServiceTemplatePath            string   `json:"dpuServiceTemplate"`
	DPUServiceInterfacesHBNPaths      []string `json:"dpuServiceInterfacesHBN,omitempty"`
	DPUServiceInterfaceOVNPath        *string  `json:"dpuServiceInterfaceOVN,omitempty"`
	DPUServiceTemplateOVNPath         *string  `json:"dpuServiceTemplateOVN,omitempty"`
	DPUServiceTemplateHBNPath         *string  `json:"dpuServiceTemplateHBN,omitempty"`
	DPUServiceConfiguration           string   `json:"dpuServiceConfiguration"`
	DPUServiceConfigurationOVNPath    *string  `json:"dpuServiceConfigurationOVN,omitempty"`
	DPUServiceConfigurationHBNPath    *string  `json:"dpuServiceConfigurationHBN,omitempty"`
	OVNCredentialRequestPath          *string  `json:"ovnCredentialRequest,omitempty"`
	DPUClusterPrerequisiteObjectPaths []string `json:"dpuClusterPrerequisiteObjectPath"`
	NumberOfDPUNodes                  int      `json:"numberOfDPUNodes"`
	NumberOfDPUsPerNode               int      `json:"numberOfDPUsPerNode,omitempty"`
	NodeRebootConfigMap               string   `json:"nodeRebootConfigMap,omitempty"`
	NodeRebootConfigMapPath           string   `json:"nodeRebootConfigMapPath,omitempty"`
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
