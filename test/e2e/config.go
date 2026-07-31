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

type config struct {
	DPUFlavorPath                     *string  `json:"dpuFlavor,omitempty"`
	ProvisioningControllerPVCPath     *string  `json:"provisioningControllerPVC,omitempty"`
	BFBPath                           string   `json:"bfb"`
	DPUClusterPath                    string   `json:"dpuCluster"`
	DPUSetPath                        string   `json:"dpuSet"`
	DPUServiceInterfacePath           string   `json:"dpuServiceInterface"`
	DPUServiceChainPath               string   `json:"dpuServiceChain"`
	DPUDeploymentPath                 string   `json:"dpuDeployment"`
	DPUServiceChainTemplatePath       string   `json:"dpuServiceChainTemplate"`
	DPUServiceInterfaceTemplatePath   string   `json:"dpuServiceInterfaceTemplate"`
	DPUServiceCredentialRequestPath   string   `json:"dpuServiceCredentialRequest"`
	IPPoolDPUServiceIPAMPath          string   `json:"ipPoolDPUServiceIPAM"`
	CIDRPoolDPUServiceIPAMPath        string   `json:"cidrPoolDPUServiceIPAM"`
	DPUServiceIPAMTemplatePath        string   `json:"dpuServiceIPAMTemplate"`
	DPUServicePath                    string   `json:"dpuService"`
	DPUServiceHBNPath                 string   `json:"dpuServiceHBN"`
	DPUServiceOVNCentralPath          string   `json:"dpuServiceOVNCentral"`
	DPUServiceOVNControllerPath       string   `json:"dpuServiceOVNController"`
	DPUServiceVPCOVNControllerPath    string   `json:"dpuServiceVPCOVNController"`
	DPUServiceVPCOVNNodePath          string   `json:"dpuServiceVPCOVNNode"`
	DHCPDaemonSetPath                 string   `json:"dhcpDaemonSet"`
	DPUServiceTemplatePath            string   `json:"dpuServiceTemplate"`
	DPUServiceConfiguration           string   `json:"dpuServiceConfiguration"`
	DPUClusterPrerequisiteObjectPaths []string `json:"dpuClusterPrerequisiteObjectPath"`
	NumberOfDPUNodes                  int      `json:"numberOfDPUNodes"`
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
