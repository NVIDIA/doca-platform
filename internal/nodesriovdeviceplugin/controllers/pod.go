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

import (
	"encoding/json"
	"fmt"

	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/common"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
)

const (
	// initContainerName is the name of the init container.
	initContainerName = "dpf-device-plugin-init"
	// mainContainerName is the name of the main device plugin container.
	mainContainerName = "sriov-device-plugin"
	// configVolumeName is the name of the volume for the device plugin config.
	configVolumeName = "config"
	// downwardAPIVolumeName is the name of the volume for the downward API.
	downwardAPIVolumeName = "podinfo"
	// devicePluginSocketVolumeName is the volume name for device plugin socket directory.
	devicePluginSocketVolumeName = "device-plugin-socket"
	// deviceInfoVolumeName is the volume name for CNI device info directory.
	deviceInfoVolumeName = "device-info"
	// sysVolumeName is the volume name for sysfs.
	sysVolumeName = "sys"

	// configMountPath is the path where the device plugin config is mounted.
	configMountPath = "/etc/pcidp"
	// downwardAPIMountPath is the path where the downward API is mounted.
	downwardAPIMountPath = "/etc/dpf/device-plugin"
	// devicePluginSocketMountPath is the path for device plugin sockets.
	devicePluginSocketMountPath = "/var/lib/kubelet/device-plugins"
	// deviceInfoMountPath is the path where the device plugin writes CNI device info.
	deviceInfoMountPath = "/var/run/k8s.cni.cncf.io/devinfo/dp"
	// sysMountPath is the path for sysfs.
	sysMountPath = "/sys"

	// inputFileName is the name of the input file in the downward API volume.
	inputFileName = "input.json"
)

// buildDesiredPod constructs the desired Pod spec for a given node.
func buildDesiredPod(
	nodeName string,
	namespace string,
	inputConfig common.NodeInputConfig,
	devicePluginConfig *DevicePluginConfig,
	ownerRefs []metav1.OwnerReference,
) (*corev1.Pod, error) {
	inputConfigJSON, err := json.Marshal(inputConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input config: %w", err)
	}

	podName := generatePodName(nodeName)
	hostPathDirectory := corev1.HostPathDirectory
	hostPathDirectoryOrCreate := corev1.HostPathDirectoryOrCreate

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				ManagedByLabelKey: ManagedByLabelValue,
			},
			Annotations: map[string]string{
				PodInputAnnotationKey: string(inputConfigJSON),
			},
			OwnerReferences: ownerRefs,
		},
		Spec: corev1.PodSpec{
			HostNetwork:       true,
			DNSPolicy:         corev1.DNSClusterFirstWithHostNet,
			PriorityClassName: "system-node-critical",
			InitContainers: []corev1.Container{
				{
					Name:    initContainerName,
					Image:   devicePluginConfig.InitImage,
					Command: []string{"/nodesriovdeviceplugin-init"},
					Args: []string{
						"--input-path=" + downwardAPIMountPath + "/" + inputFileName,
						"--output-path=" + configMountPath,
						"--default-resource-prefix=" + devicePluginConfig.DefaultResourcePrefix,
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      downwardAPIVolumeName,
							MountPath: downwardAPIMountPath,
							ReadOnly:  true,
						},
						{
							Name:      configVolumeName,
							MountPath: configMountPath,
						},
						{
							Name:      sysVolumeName,
							MountPath: sysMountPath,
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
						RunAsUser:  ptr.To(int64(0)),
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  mainContainerName,
					Image: devicePluginConfig.Image,
					Args: []string{
						"--config-file=/etc/pcidp/config.json",
						fmt.Sprintf("--resource-prefix=%s", devicePluginConfig.DefaultResourcePrefix),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      configVolumeName,
							MountPath: configMountPath,
							ReadOnly:  true,
						},
						{
							Name:      devicePluginSocketVolumeName,
							MountPath: devicePluginSocketMountPath,
						},
						{
							Name:      deviceInfoVolumeName,
							MountPath: deviceInfoMountPath,
						},
						{
							Name:      sysVolumeName,
							MountPath: sysMountPath,
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: configVolumeName,
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: downwardAPIVolumeName,
					VolumeSource: corev1.VolumeSource{
						DownwardAPI: &corev1.DownwardAPIVolumeSource{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									Path: inputFileName,
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: fmt.Sprintf(
											"metadata.annotations['%s']",
											PodInputAnnotationKey,
										),
									},
								},
							},
						},
					},
				},
				{
					Name: devicePluginSocketVolumeName,
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: devicePluginSocketMountPath,
							Type: &hostPathDirectory,
						},
					},
				},
				{
					Name: deviceInfoVolumeName,
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: deviceInfoMountPath,
							Type: &hostPathDirectoryOrCreate,
						},
					},
				},
				{
					Name: sysVolumeName,
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/sys",
							Type: &hostPathDirectory,
						},
					},
				},
			},
			ImagePullSecrets: getImagePullSecrets(devicePluginConfig),
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
				},
			},
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchFields: []corev1.NodeSelectorRequirement{
									{
										Key:      metav1.ObjectNameField,
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{nodeName},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Compute and add the pod spec hash as an annotation.
	pod.Annotations[PodObjectHashAnnotationKey] = computePodObjectHash(pod)

	return pod, nil
}

// getImagePullSecrets returns the image pull secrets from the device plugin config.
func getImagePullSecrets(config *DevicePluginConfig) []corev1.LocalObjectReference {
	if len(config.ImagePullSecrets) == 0 {
		return nil
	}
	secrets := make([]corev1.LocalObjectReference, len(config.ImagePullSecrets))
	for i, name := range config.ImagePullSecrets {
		secrets[i] = corev1.LocalObjectReference{Name: name}
	}
	return secrets
}

// generatePodName creates a deterministic pod name for the specified node.
// If the resulting name exceeds the DNS-1123 subdomain max length (253 chars), it generates a short hash-based name instead.
func generatePodName(nodeName string) string {
	namePrefix := "dpf-sriov-device-plugin-"
	name := namePrefix + nodeName
	if len(name) <= validation.DNS1123SubdomainMaxLength {
		return name
	}
	d := digest.FromObjects(name)
	return namePrefix + digest.Short(d, 10)
}

// isPodOutdated checks if the existing pod needs to be recreated.
func isPodOutdated(existing *corev1.Pod, desired *corev1.Pod) bool {
	// Check if the pod object hash has changed.
	existingHash := existing.Annotations[PodObjectHashAnnotationKey]
	desiredHash := desired.Annotations[PodObjectHashAnnotationKey]
	return existingHash != desiredHash
}

// isPodInTerminalState checks if the pod is in a terminal state (phase Failed or Succeeded).
func isPodInTerminalState(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
}

// computePodObjectHash computes a SHA256 hash of the pod object for change detection.
func computePodObjectHash(pod *corev1.Pod) string {
	d := digest.FromObjects(pod)
	return digest.Short(d, 10)
}
