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

package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	ProvisioningControllerName          ComponentName = "provisioning-controller"
	DPUServiceControllerName            ComponentName = "dpuservice-controller"
	ServiceSetControllerName            ComponentName = "servicechainset-controller"
	ServiceChainSetCRDsName             ComponentName = "servicechainset-rbac-and-crds"
	DPUDetectorName                     ComponentName = "dpudetector"
	FlannelName                         ComponentName = "flannel"
	MultusName                          ComponentName = "multus"
	SRIOVDevicePluginName               ComponentName = "sriov-device-plugin"
	OVSCNIName                          ComponentName = "ovs-cni"
	NVIPAMName                          ComponentName = "nvidia-k8s-ipam"
	SFCControllerName                   ComponentName = "sfc-controller"
	KamajiClusterManagerName            ComponentName = "kamaji-cluster-manager"
	StaticClusterManagerName            ComponentName = "static-cluster-manager"
	BFBRegistryName                     ComponentName = "bfb-registry"
	CNIInstallerName                    ComponentName = "cni-installer"
	NodeSRIOVDevicePluginControllerName ComponentName = "nodesriovdeviceplugin-controller"
	KubeStateMetricsName                ComponentName = "kube-state-metrics"
	KubeStateMetricsRBACName            ComponentName = "kube-state-metrics-rbac"
	NodeProblemDetectorName             ComponentName = "node-problem-detector"
	OpenTelemetryCollectorName          ComponentName = "opentelemetry-collector"
)

type ComponentName string

func (c ComponentName) String() string {
	return string(c)
}

func (c ComponentName) WithContainer(cName ContainerName) string {
	return fmt.Sprintf("%s,%s", c.String(), cName.String())
}

// Container names for the helm path provider in internal/operator/inventory/helm_paths_provider.go.
var (
	// ControllerManagerContainer is the default name of the scaffolded controller-runtime manager container.
	ControllerManagerContainer ContainerName = "manager"
	// DPUDetectorContainer is the default name of the DPU Detector container.
	DPUDetectorContainer ContainerName = "dpu-detector"
	// FlannelContainerDaemon is the name of the flannel daemon container.
	FlannelContainerDaemon ContainerName = "daemon"
	// FlannelContainerCNI is the name of the flannel CNI container.
	FlannelContainerCNI ContainerName = "cni"
	// NVIPAMContainerController is the name of the NVIPAM controller container.
	NVIPAMContainerController ContainerName = "controller"
	// NVIPAMContainerNode is the name of the NVIPAM node container.
	NVIPAMContainerNode ContainerName = "node"
	// MultusContainer is the default name of the scaffolded MultusContainer CNI container.
	MultusContainer ContainerName = "kube-multus"
	// SRIOVDevicePluginContainer is the default name of the scaffolded SR-IOV Device Plugin container.
	SRIOVDevicePluginContainer ContainerName = "kube-sriovdp"
	// OVSCNI is the default name of the scaffolded OVS CNI
	OVSCNI ContainerName = "ovs-cni-marker"
	// CNIInstallerContainer is the default name of the scaffolded CNI Installer container.
	CNIInstallerContainer ContainerName = "cni-installer"
	// KubeStateMetricsContainer is the default name of the kube-state-metrics container.
	KubeStateMetricsContainer ContainerName = "kube-state-metrics"
	// NodeProblemDetectorContainer is the default name of the node-problem-detector container.
	NodeProblemDetectorContainer ContainerName = "node-problem-detector"
	// OpenTelemetryCollectorContainer is the default name of the opentelemetry-collector container.
	OpenTelemetryCollectorContainer ContainerName = "opentelemetry-collector"
)

type ContainerName string

func (c ContainerName) String() string {
	return string(c)
}

func (c *DPFOperatorConfig) ComponentConfigs() []ComponentConfigurable {
	out := []ComponentConfigurable{}

	if c.Spec.ProvisioningController != nil {
		out = append(out, c.Spec.ProvisioningController)
	}
	if c.Spec.DPUServiceController != nil {
		out = append(out, c.Spec.DPUServiceController)
	}
	if c.Spec.DPUDetector != nil {
		out = append(out, c.Spec.DPUDetector)
	}
	if c.Spec.Flannel != nil {
		out = append(out, c.Spec.Flannel)
	}
	if c.Spec.Multus != nil {
		out = append(out, c.Spec.Multus)
	}
	if c.Spec.SRIOVDevicePlugin != nil {
		out = append(out, c.Spec.SRIOVDevicePlugin)
	}
	if c.Spec.NVIPAM != nil {
		out = append(out, c.Spec.NVIPAM)
	}
	if c.Spec.SFCController != nil {
		out = append(out, c.Spec.SFCController)
	}
	if c.Spec.OVSCNI != nil {
		out = append(out, c.Spec.OVSCNI)
	}
	if c.Spec.ServiceSetController != nil {
		out = append(out, c.Spec.ServiceSetController)
	}
	if c.Spec.KamajiClusterManager != nil {
		out = append(out, c.Spec.KamajiClusterManager)
	}
	if c.Spec.StaticClusterManager != nil {
		out = append(out, c.Spec.StaticClusterManager)
	}
	if c.Spec.CNIInstaller != nil {
		out = append(out, c.Spec.CNIInstaller)
	}
	if c.Spec.NodeSRIOVDevicePluginController != nil {
		out = append(out, c.Spec.NodeSRIOVDevicePluginController)
	}
	if c.Spec.Monitoring != nil && c.Spec.Monitoring.KubeStateMetrics != nil {
		out = append(out, c.Spec.Monitoring.KubeStateMetrics)
	}
	if c.Spec.Monitoring != nil && c.Spec.Monitoring.NodeProblemDetector != nil {
		out = append(out, c.Spec.Monitoring.NodeProblemDetector)
	}
	if c.Spec.Monitoring != nil && c.Spec.Monitoring.OpenTelemetryCollector != nil {
		out = append(out, c.Spec.Monitoring.OpenTelemetryCollector)
	}
	return out
}

// ComponentConfigurable defines the shared config for all components deployed by the DPF Operator.
// +kubebuilder:object:generate=false
type ComponentConfigurable interface {
	Name() string
	Disabled() bool
}

// BaseComponentConfig provides common configuration fields that can be embedded
// by all component configurations to reduce code duplication.
type BaseComponentConfig struct {
	// Disable ensures the component is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`
}

// Disabled returns whether the component is disabled
func (b *BaseComponentConfig) Disabled() bool {
	return b.Disable != nil && *b.Disable
}

// BaseControllerConfig provides common configuration fields that can be embedded
// by all controller configurations to reduce code duplication.
type BaseControllerConfig struct {
	// Replicas is the number of replicas for the controller deployment.
	// This is used for High Availability. Leader election is enabled by default.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
}

// HelmComponentConfigurable is the shared config for helm components.
//
// +kubebuilder:object:generate=false
type HelmComponentConfigurable interface {
	GetHelmChart() *string
}

// HelmChart is a reference to a helm chart.
// +kubebuilder:validation:Pattern=`^(oci://|https://).+$`
// +optional
type HelmChart *string

type HelmComponentConfig struct {
	// HelmChart overrides the helm chart used by the ServiceSet controller.
	// The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid
	// OCI registry or a web-based repository.
	// +optional
	HelmChart HelmChart `json:"helmChart,omitempty"`
}

func (b *HelmComponentConfig) GetHelmChart() *string {
	return b.HelmChart
}

// Image is a reference to a container image.
// +kubebuilder:validation:Pattern= `^((?:(?:(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])(?:\.(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))*|\[(?:[a-fA-F0-9:]+)\])(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*)*)(?::([\w][\w.-]{0,127}))?(?:@([A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]{32,}))?$`
type Image *string // Validation is the same as the implementation at https://github.com/containers/image/blob/93fa49b0f1fb78470512e0484012ca7ad3c5c804/docker/reference/regexp.go

// DeprecatedImageComponentConfigurable is the shared config for helm components.
//
// Deprecated: new components should use the ImageComponentConfigurable instead.
//
// +kubebuilder:object:generate=false
type DeprecatedImageComponentConfigurable interface {
	// GetImage returns a string with one or more images to be configured for the components.
	// This is a comma-delimited string if the component has more than one image to be configured.
	GetImage() *string
}

// ImageComponentConfigurable defines configuration for overriding the images of a component’s containers.
// Specified at the pod container level.
// +kubebuilder:object:generate=false
type ImageComponentConfigurable interface {
	// GetImages returns a map of container names to their images.
	GetImages() map[ContainerName]*string
}

// ImageComponentConfig provides common configuration fields that can be embedded
// by all component configurations to reduce code duplication.
type ImageComponentConfig struct {
	// +optional
	Image Image `json:"image,omitempty"`
}

// GetImage returns the image override for the component
func (b *ImageComponentConfig) GetImage() *string {
	return b.Image
}

// ResourcesComponentConfigurable defines configuration for overriding the resources of a component’s containers.
// Specified at the pod container level.
// +kubebuilder:object:generate=false
type ResourcesComponentConfigurable interface {
	// GetResources returns a map of container names to their resource requirements.
	GetResources() map[ContainerName]*corev1.ResourceRequirements
}

// ResourceComponentConfig defines the resource requirements for a container.
type ResourceComponentConfig struct {
	// Resources defines the memory and CPU resource requests and limits for the component.
	// This field is optional, and if not set, the component will use the default resource.
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

type ResourceRequirements struct {
	// Requests defines the resource requests for the component.
	Requests *Resources `json:"requests,omitempty"`

	// Limits defines the resource limits for the component.
	Limits *Resources `json:"limits,omitempty"`
}

type Resources struct {
	// CPU is the amount of CPU requested by the component.
	// +optional
	CPU *resource.Quantity `json:"cpu,omitempty"`

	// Memory is the amount of Memory requested by the component.
	// +optional
	Memory *resource.Quantity `json:"memory,omitempty"`
}

// GetResource converts the ResourceComponentConfig to a Kubernetes ResourceRequirements.
// Returns nil if no resource configuration is specified.
// Only CPU and Memory resources are supported.
func (r *ResourceComponentConfig) GetResource() *corev1.ResourceRequirements {
	if r.Resources == nil {
		return nil
	}

	resources := &corev1.ResourceRequirements{}

	// Handle requests
	if requests := r.buildResourceList(r.Resources.Requests); len(requests) > 0 {
		resources.Requests = requests
	}

	// Handle limits
	if limits := r.buildResourceList(r.Resources.Limits); len(limits) > 0 {
		resources.Limits = limits
	}

	// Return nil if no resources were specified
	if len(resources.Requests) == 0 && len(resources.Limits) == 0 {
		return nil
	}

	return resources
}

// buildResourceList converts Resources to a corev1.ResourceList.
// Only CPU and Memory resources are included.
func (r *ResourceComponentConfig) buildResourceList(resources *Resources) corev1.ResourceList {
	if resources == nil {
		return nil
	}

	resourceList := make(corev1.ResourceList)

	if resources.CPU != nil {
		resourceList[corev1.ResourceCPU] = *resources.CPU
	}

	if resources.Memory != nil {
		resourceList[corev1.ResourceMemory] = *resources.Memory
	}

	return resourceList
}
