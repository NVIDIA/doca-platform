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
	getPath(componentName operatorv1.ComponentName) (map[operatorv1.ContainerName]containerPaths, bool)
}

// getPath returns the container paths for a given component name
func (p *defaultHelmPathsProvider) getPath(componentName operatorv1.ComponentName) (map[operatorv1.ContainerName]containerPaths, bool) {
	paths, exists := p.paths[componentName]
	return paths, exists
}

// defaultHelmPathsProvider implements helmPathsProvider with the default configuration
type defaultHelmPathsProvider struct {
	paths map[operatorv1.ComponentName]map[operatorv1.ContainerName]containerPaths
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
		paths: map[operatorv1.ComponentName]map[operatorv1.ContainerName]containerPaths{
			operatorv1.FlannelName: {
				operatorv1.FlannelContainerDaemon: {
					Repository: []string{"flannel", "image", "repository"},
					Tag:        []string{"flannel", "image", "tag"},
					Resources:  []string{"flannel", "resources"},
				},
				operatorv1.FlannelContainerCNI: {
					Repository: []string{"flannel", "image_cni", "repository"},
					Tag:        []string{"flannel", "image_cni", "tag"},
				},
			},
			operatorv1.NVIPAMControllerName: {
				operatorv1.NVIPAMContainerController: {
					Repository: []string{"nvIpam", "image", "repository"},
					Tag:        []string{"nvIpam", "image", "tag"},
					Resources:  []string{"nvIpam", "controller", "resources"},
				},
				operatorv1.NVIPAMContainerNode: {
					Repository: []string{"nvIpam", "image", "repository"},
					Tag:        []string{"nvIpam", "image", "tag"},
					Resources:  []string{"nvIpam", "node", "resources"},
				},
			},
			operatorv1.ServiceSetControllerName: {
				operatorv1.ControllerManagerContainer: {
					Repository: []string{"controllerManager", "manager", "image", "repository"},
					Tag:        []string{"controllerManager", "manager", "image", "tag"},
					Resources:  []string{"controllerManager", "manager", "resources"},
				},
			},
			operatorv1.SFCControllerName: {
				operatorv1.ControllerManagerContainer: {
					Repository: []string{"controllerManager", "manager", "image", "repository"},
					Tag:        []string{"controllerManager", "manager", "image", "tag"},
					Resources:  []string{"controllerManager", "manager", "resources"},
				},
			},
			operatorv1.MultusName: {
				operatorv1.MultusContainer: {
					Repository: []string{"kubeMultusDs", "image", "repository"},
					Tag:        []string{"kubeMultusDs", "image", "tag"},
					Resources:  []string{"kubeMultusDs", "kubeMultus", "resources"},
				},
			},
			operatorv1.SRIOVDevicePluginName: {
				operatorv1.SRIOVDevicePluginContainer: {
					Repository: []string{"kubeSriovDevicePlugin", "kubeSriovdp", "image", "repository"},
					Tag:        []string{"kubeSriovDevicePlugin", "kubeSriovdp", "image", "tag"},
					Resources:  []string{"kubeSriovDevicePlugin", "kubeSriovdp", "resources"},
				},
			},
			operatorv1.OVSCNIName: {
				operatorv1.OVSCNI: {
					Repository: []string{"arm64", "image", "repository"},
					Tag:        []string{"arm64", "image", "tag"},
					Resources:  []string{"arm64", "ovsCniPlugin", "resources"},
				},
			},
			operatorv1.CNIInstallerName: {
				operatorv1.CNIInstallerContainer: {
					Repository: []string{"cniInstaller", "image", "repository"},
					Tag:        []string{"cniInstaller", "image", "tag"},
					Resources:  []string{"cniInstaller", "resources"},
				},
			},
			operatorv1.KubeStateMetricsName: {
				operatorv1.KubeStateMetricsContainer: {
					Repository: []string{"image", "repository"},
					Tag:        []string{"image", "tag"},
					Resources:  []string{"resources"},
				},
			},
			operatorv1.NodeProblemDetectorName: {
				operatorv1.NodeProblemDetectorContainer: {
					Repository: []string{"image", "repository"},
					Tag:        []string{"image", "tag"},
					Resources:  []string{"resources"},
				},
			},
			operatorv1.OpenTelemetryCollectorName: {
				operatorv1.OpenTelemetryCollectorContainer: {
					Repository: []string{"image", "repository"},
					Tag:        []string{"image", "tag"},
					Resources:  []string{"resources"},
				},
			},
		},
	}
}
