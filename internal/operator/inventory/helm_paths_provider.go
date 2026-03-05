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

package inventory

import operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

const (
	// multiSplitChar is used to split multiple containers per component in the Helm paths.
	// TODO: refactor to not use this multiSplitChar workaround.
	multiSplitChar = ","

	// TODO: move these keys into the helmPaths map to have a single source of truth.
	cniBinDirPathKey                     = "cniBinDir"
	cniConfDirPathKey                    = "cniConfDir"
	openvSwitchRunDirPathKey             = "openvSwitchRunDir"
	openvSwitchBinDirPathKey             = "openvSwitchBinDir"
	openvSwitchSharedLibraryDirPathKey   = "openvSwitchSharedLibraryDir"
	openvSwitchSharedLibrary64DirPathKey = "openvSwitchSharedLibrary64Dir"
	skipCNIConfigInstallationKey         = "skipCNIConfigInstallation"
)

// helmPathsProvider defines an interface for accessing Helm chart paths configuration
type helmPathsProvider interface {
	getPath(componentName string) (map[string]containerPaths, bool)
}

// getPath returns the container paths for a given component name
func (p *defaultHelmPathsProvider) getPath(componentName string) (map[string]containerPaths, bool) {
	paths, exists := p.paths[componentName]
	return paths, exists
}

// defaultHelmPathsProvider implements helmPathsProvider with the default configuration
type defaultHelmPathsProvider struct {
	paths map[string]map[string]containerPaths
}

// containerPaths defines the paths for a specific container within a component
type containerPaths struct {
	Repository []string
	Tag        []string
	Resources  []string
}

// helmPaths creates a new default Helm paths provider
func helmPaths() helmPathsProvider {
	return &defaultHelmPathsProvider{
		paths: map[string]map[string]containerPaths{
			operatorv1.FlannelName: {
				operatorv1.FlannelContainerDaemonName.String(): {
					Repository: []string{"flannel", "image", "repository"},
					Tag:        []string{"flannel", "image", "tag"},
					Resources:  []string{"flannel", "resources"},
				},
				operatorv1.FlannelContainerCNIName.String(): {
					Repository: []string{"flannel", "image_cni", "repository"},
					Tag:        []string{"flannel", "image_cni", "tag"},
				},
			},
			operatorv1.NVIPAMName: {
				"": {
					Repository: []string{"nvIpam", "image", "repository"},
					Tag:        []string{"nvIpam", "image", "tag"},
				},
				operatorv1.NVIPAMContainerControllerName.String(): {
					Resources: []string{"nvIpam", "controller", "resources"},
				},
				operatorv1.NVIPAMContainerNodeName.String(): {
					Resources: []string{"nvIpam", "node", "resources"},
				},
			},
			operatorv1.ServiceSetControllerName: {
				"": {
					Repository: []string{"controllerManager", "manager", "image", "repository"},
					Tag:        []string{"controllerManager", "manager", "image", "tag"},
					Resources:  []string{"controllerManager", "manager", "resources"},
				},
			},
			operatorv1.SFCControllerName: {
				"": {
					Repository: []string{"controllerManager", "manager", "image", "repository"},
					Tag:        []string{"controllerManager", "manager", "image", "tag"},
					Resources:  []string{"controllerManager", "manager", "resources"},
				},
			},
			operatorv1.MultusName: {
				"": {
					Repository: []string{"kubeMultusDs", "image", "repository"},
					Tag:        []string{"kubeMultusDs", "image", "tag"},
					Resources:  []string{"kubeMultusDs", "kubeMultus", "resources"},
				},
			},
			operatorv1.SRIOVDevicePluginName: {
				"": {
					Repository: []string{"kubeSriovDevicePlugin", "kubeSriovdp", "image", "repository"},
					Tag:        []string{"kubeSriovDevicePlugin", "kubeSriovdp", "image", "tag"},
					Resources:  []string{"kubeSriovDevicePlugin", "kubeSriovdp", "resources"},
				},
			},
			operatorv1.OVSCNIName: {
				"": {
					Repository: []string{"arm64", "image", "repository"},
					Tag:        []string{"arm64", "image", "tag"},
					Resources:  []string{"arm64", "ovsCniMarker", "resources"},
				},
			},
		},
	}
}
