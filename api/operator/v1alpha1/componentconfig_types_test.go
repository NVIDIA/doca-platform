// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func TestCoreDNSConfigurationOverrides(t *testing.T) {
	cpu := resource.MustParse("100m")
	wantResources := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: cpu},
	}
	config := &CoreDNSConfiguration{
		Deployment: &CoreDNSDeployment{
			ImageComponentConfig: ImageComponentConfig{Image: ptr.To("example.com/coredns:v1")},
			ResourceComponentConfig: ResourceComponentConfig{
				Resources: &ResourceRequirements{Requests: &Resources{CPU: &cpu}},
			},
		},
	}

	if got, want := config.Name(), CoreDNSName.String(); got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := config.GetImages(), map[ContainerName]*string{CoreDNSContainer: ptr.To("example.com/coredns:v1")}; !reflect.DeepEqual(got, want) {
		t.Errorf("GetImages() = %#v, want %#v", got, want)
	}
	if got, want := config.GetResources(), map[ContainerName]*corev1.ResourceRequirements{CoreDNSContainer: wantResources}; !reflect.DeepEqual(got, want) {
		t.Errorf("GetResources() = %#v, want %#v", got, want)
	}
}
