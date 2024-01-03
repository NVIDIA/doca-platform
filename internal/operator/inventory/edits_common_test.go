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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
