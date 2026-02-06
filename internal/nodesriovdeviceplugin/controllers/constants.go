/*
Copyright 2026 NVIDIA

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

package controllers

import "time"

const (
	// NodeResourcesPrefix is the prefix used by controller for all node resources labels and annotations.
	NodeResourcesPrefix = "noderesources.dpu.nvidia.com/"

	// DPUDevicePluginConfigAnnotationKey is the annotation key on DPU objects that references
	// the name of the NodeSRIOVDevicePluginConfig CR to use for that DPU.
	DPUDevicePluginConfigAnnotationKey = NodeResourcesPrefix + "nodesriovdevicepluginconfig"

	// PodInputAnnotationKey is the annotation key on managed pods that holds the JSON input config
	// containing the per-node configuration (map of DPU serial -> config).
	PodInputAnnotationKey = NodeResourcesPrefix + "sriov-device-plugin-input"

	// PodObjectHashAnnotationKey is the annotation key on managed pods that holds the hash of
	// the desired pod object for change detection.
	PodObjectHashAnnotationKey = NodeResourcesPrefix + "pod-object-hash"

	// ManagedByLabelKey is the label key used to identify pods managed by this controller.
	ManagedByLabelKey = NodeResourcesPrefix + "managed-by"

	// ManagedByLabelValue is the label value for pods managed by this controller.
	ManagedByLabelValue = "nodesriovdeviceplugin-controller"

	// nodeControllerName is the name of the node controller.
	nodeControllerName = "nodesriovdeviceplugin-node-controller"

	// DefaultResourcePrefix is the default resource prefix for device plugin resources.
	DefaultResourcePrefix = "nvidia.com"

	// FailedPodBackoffBaseDelay is the base delay for failed pod backoff.
	FailedPodBackoffBaseDelay = 1 * time.Second

	// FailedPodBackoffMaxDelay is the maximum delay for failed pod backoff.
	FailedPodBackoffMaxDelay = 15 * time.Minute

	// BackoffGCInterval is the interval for backoff garbage collection.
	BackoffGCInterval = 1 * time.Minute
)

// DevicePluginConfig holds the configuration for device plugin pods.
// It is passed via command-line flags from the DPFOperatorConfig.
type DevicePluginConfig struct {
	// Image is the container image for the SRIOV device plugin.
	Image string
	// InitImage is the container image for the init container.
	InitImage string
	// DefaultResourcePrefix is the default resource prefix for device plugin resources.
	DefaultResourcePrefix string
	// ImagePullSecrets is the list of image pull secrets for the device plugin pod.
	ImagePullSecrets []string
}
