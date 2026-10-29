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

package inventory

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestImageForDeploymentContainerEdit(t *testing.T) {
	initialImageName := "initial-image"
	containerName := "manager"
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  containerName,
							Image: initialImageName,
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name          string
		containerName string
		imageInput    string
		wantImage     string
	}{
		{
			name:          "name should be replaced",
			containerName: containerName,
			imageInput:    "image-one",
			wantImage:     "image-one",
		},
		{
			name:          "name not replaced when the container name is wrong",
			containerName: "not-the-manager",
			imageInput:    "image-one",
			wantImage:     initialImageName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			edit := ImageForDeploymentContainerEdit(tt.containerName, tt.imageInput)
			if err := edit(deployment); err != nil {
				t.Errorf("%s", err)
			}
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == tt.containerName {
					if container.Image != tt.wantImage {
						t.Errorf("got %q, want %q", container.Image, tt.wantImage)
					}
				}
			}
		})
	}
}

func TestResourcesEditForDeployment(t *testing.T) {
	containerName := "manager"
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}

	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: containerName,
						},
					},
				},
			},
		},
	}

	edit := ResourcesEditForDeployment(containerName, resources)
	err := edit(deployment)
	if err != nil {
		t.Errorf("ResourcesEditForDeployment failed: %v", err)
	}

	container := deployment.Spec.Template.Spec.Containers[0]
	if !reflect.DeepEqual(container.Resources, resources) {
		t.Errorf("got resources %v, want %v", container.Resources, resources)
	}
}

func TestResourcesEditForDeployment_ContainerNotFound(t *testing.T) {
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("100m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("100m"),
		},
	}

	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "manager",
						},
					},
				},
			},
		},
	}

	edit := ResourcesEditForDeployment("non-existent-container", resources)
	err := edit(deployment)
	if err == nil {
		t.Error("expected error when container not found, got nil")
	}
}

func TestResourcesEditForDaemonSet(t *testing.T) {
	containerName := "dpu-detector"
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}

	daemonset := &appsv1.DaemonSet{
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: containerName,
						},
					},
				},
			},
		},
	}

	edit := ResourcesEditForDaemonSet(containerName, resources)
	err := edit(daemonset)
	if err != nil {
		t.Errorf("ResourcesEditForDaemonSet failed: %v", err)
	}

	container := daemonset.Spec.Template.Spec.Containers[0]
	if !reflect.DeepEqual(container.Resources, resources) {
		t.Errorf("got resources %v, want %v", container.Resources, resources)
	}
}

func TestResourcesEditForDaemonSet_ContainerNotFound(t *testing.T) {
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("25m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("25m"),
		},
	}

	daemonset := &appsv1.DaemonSet{
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "dpu-detector",
						},
					},
				},
			},
		},
	}

	edit := ResourcesEditForDaemonSet("non-existent-container", resources)
	err := edit(daemonset)
	if err == nil {
		t.Error("expected error when container not found, got nil")
	}
}
