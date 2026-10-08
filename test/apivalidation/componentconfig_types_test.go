// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package apivalidation_test

import (
	"reflect"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func TestCoreDNSConfigurationOverrides(t *testing.T) {
	cpu := resource.MustParse("100m")
	wantResources := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: cpu},
	}
	config := &operatorv1.CoreDNSConfiguration{
		Deployment: &operatorv1.CoreDNSDeployment{
			ImageComponentConfig: operatorv1.ImageComponentConfig{Image: ptr.To("example.com/coredns:v1")},
			ResourceComponentConfig: operatorv1.ResourceComponentConfig{
				Resources: &operatorv1.ResourceRequirements{Requests: &operatorv1.Resources{CPU: &cpu}},
			},
		},
	}

	if got, want := config.Name(), operatorv1.CoreDNSName.String(); got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := config.GetImages(), map[operatorv1.ContainerName]*string{operatorv1.CoreDNSContainer: ptr.To("example.com/coredns:v1")}; !reflect.DeepEqual(got, want) {
		t.Errorf("GetImages() = %#v, want %#v", got, want)
	}
	if got, want := config.GetResources(), map[operatorv1.ContainerName]*corev1.ResourceRequirements{operatorv1.CoreDNSContainer: wantResources}; !reflect.DeepEqual(got, want) {
		t.Errorf("GetResources() = %#v, want %#v", got, want)
	}
}

func TestSRIOVDevicePluginConfigurationOverrides(t *testing.T) {
	cpu := resource.MustParse("100m")
	wantResources := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: cpu},
	}
	config := &operatorv1.SRIOVDevicePluginConfiguration{
		DevicePlugin: &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{Image: ptr.To("example.com/sriov-device-plugin:v1")},
		},
		ConfigInit: &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{Image: ptr.To("example.com/dpf-system:v1")},
			ResourceComponentConfig: operatorv1.ResourceComponentConfig{
				Resources: &operatorv1.ResourceRequirements{Requests: &operatorv1.Resources{CPU: &cpu}},
			},
		},
	}

	if got, want := config.GetImages(), map[operatorv1.ContainerName]*string{
		operatorv1.SRIOVDevicePluginContainer:           ptr.To("example.com/sriov-device-plugin:v1"),
		operatorv1.SRIOVDevicePluginConfigInitContainer: ptr.To("example.com/dpf-system:v1"),
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("GetImages() = %#v, want %#v", got, want)
	}
	if got, want := config.GetResources(), map[operatorv1.ContainerName]*corev1.ResourceRequirements{
		operatorv1.SRIOVDevicePluginContainer:           nil,
		operatorv1.SRIOVDevicePluginConfigInitContainer: wantResources,
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("GetResources() = %#v, want %#v", got, want)
	}
}

// TestServiceDaemonSetMaxUnavailableValue checks the fallback and explicit rollout limit.
func TestServiceDaemonSetMaxUnavailableValue(t *testing.T) {
	cases := map[string]struct {
		config *operatorv1.DPUServiceControllerConfiguration
		want   int32
	}{
		"nil config": {
			config: nil,
			want:   operatorv1.DefaultServiceDaemonSetMaxUnavailableFallback,
		},
		"unset field": {
			config: &operatorv1.DPUServiceControllerConfiguration{},
			want:   operatorv1.DefaultServiceDaemonSetMaxUnavailableFallback,
		},
		"explicit value": {
			config: &operatorv1.DPUServiceControllerConfiguration{
				DefaultServiceDaemonSetMaxUnavailable: 5,
			},
			want: 5,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.config.ServiceDaemonSetMaxUnavailableValue(); got != tc.want {
				t.Errorf("ServiceDaemonSetMaxUnavailableValue() = %d, want %d", got, tc.want)
			}
		})
	}
}
