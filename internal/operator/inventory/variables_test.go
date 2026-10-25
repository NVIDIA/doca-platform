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

package inventory

import (
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
)

const testDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

func Test_imageWithDefaultTag(t *testing.T) {
	tests := []struct {
		name         string
		img          string
		defaultImage string
		want         string
	}{
		{
			name:         "inject tag from default",
			img:          "nvcr.io/nvstaging/doca/dpf-system",
			defaultImage: "nvcr.io/nvidia/doca/dpf-system:v30.0.0",
			want:         "nvcr.io/nvstaging/doca/dpf-system:v30.0.0",
		},
		{
			name:         "inject tag and digest from default",
			img:          "mirror.example.com/kata-deploy",
			defaultImage: "quay.io/kata-containers/kata-deploy:3.32.0@" + testDigest,
			want:         "mirror.example.com/kata-deploy:3.32.0@" + testDigest,
		},
		{
			name:         "inject digest from default",
			img:          "mirror.example.com/image",
			defaultImage: "example.com/image@" + testDigest,
			want:         "mirror.example.com/image@" + testDigest,
		},
		{
			name:         "inject tag from default with registry port",
			img:          "registry.example.com:5000/dpf-system",
			defaultImage: "default.example.com:5000/dpf-system:v1.0.0",
			want:         "registry.example.com:5000/dpf-system:v1.0.0",
		},
		{
			name:         "keep tag of image",
			img:          "registry.example.com:5000/dpf-system:v2.0.0",
			defaultImage: "example.com/dpf-system:v1.0.0",
			want:         "registry.example.com:5000/dpf-system:v2.0.0",
		},
		{
			name:         "keep digest of image",
			img:          "example.com/dpf-system@" + testDigest,
			defaultImage: "example.com/dpf-system:v1.0.0",
			want:         "example.com/dpf-system@" + testDigest,
		},
		{
			name:         "keep image without default",
			img:          "example.com/dpf-system",
			defaultImage: "",
			want:         "example.com/dpf-system",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(imageWithDefaultTag(tt.img, tt.defaultImage)).To(Equal(tt.want))
		})
	}
}

func TestVariablesFromDPFOperatorConfig_ImageTags(t *testing.T) {
	g := NewWithT(t)
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())
	defaultTag := imageTagAndDigest(defaults.DPFSystemImage)
	g.Expect(defaultTag).NotTo(BeEmpty())
	defaultDevicePluginTag := imageTagAndDigest(defaults.NodeSRIOVDevicePluginImage)
	g.Expect(defaultDevicePluginTag).NotTo(BeEmpty())

	config := &operatorv1.DPFOperatorConfig{
		Spec: operatorv1.DPFOperatorConfigSpec{
			KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
				Controller: &operatorv1.DefaultOverridesConfiguration{
					ImageComponentConfig: operatorv1.ImageComponentConfig{Image: ptr.To("nvcr.io/nvstaging/doca/dpf-system")},
				},
			},
			DPUServiceController: &operatorv1.DPUServiceControllerConfiguration{
				Image: ptr.To("nvcr.io/nvstaging/doca/dpf-system"), //nolint:staticcheck // Covers the deprecated single image field.
			},
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				Controller: &operatorv1.DefaultOverridesConfiguration{
					ImageComponentConfig: operatorv1.ImageComponentConfig{Image: ptr.To("nvcr.io/nvstaging/doca/dpf-system:v9.9.9")},
				},
			},
			Multus: &operatorv1.MultusConfiguration{
				CNI: &operatorv1.DefaultOverridesConfiguration{
					ImageComponentConfig: operatorv1.ImageComponentConfig{Image: ptr.To("mirror.example.com/multus-cni")},
				},
			},
			NodeSRIOVDevicePluginController: &operatorv1.NodeSRIOVDevicePluginControllerConfiguration{
				DevicePlugin: &operatorv1.NodeSRIOVDevicePluginSettings{
					Image:     ptr.To("mirror.example.com/sriov-network-device-plugin"),
					InitImage: ptr.To("nvcr.io/nvstaging/doca/dpf-system"),
				},
			},
		},
	}

	vars := VariablesFromDPFOperatorConfig(defaults, config, nil)

	g.Expect(vars.Images).To(HaveKeyWithValue(
		operatorv1.KamajiClusterManagerName.WithContainer(operatorv1.ControllerManagerContainer),
		"nvcr.io/nvstaging/doca/dpf-system"+defaultTag))
	g.Expect(vars.Images).To(HaveKeyWithValue(
		operatorv1.DPUServiceControllerName.WithContainer(operatorv1.ControllerManagerContainer),
		"nvcr.io/nvstaging/doca/dpf-system"+defaultTag))
	g.Expect(vars.Images).To(HaveKeyWithValue(
		operatorv1.ProvisioningControllerName.WithContainer(operatorv1.ControllerManagerContainer),
		"nvcr.io/nvstaging/doca/dpf-system:v9.9.9"))
	// Multus has no default image in the operator, the helm chart's default tag is used instead.
	g.Expect(vars.Images).To(HaveKeyWithValue(
		operatorv1.MultusName.WithContainer(operatorv1.MultusContainer),
		"mirror.example.com/multus-cni"))
	g.Expect(vars.NodeSRIOVDevicePluginController.DevicePluginImage).To(Equal("mirror.example.com/sriov-network-device-plugin" + defaultDevicePluginTag))
	g.Expect(vars.NodeSRIOVDevicePluginController.DevicePluginInitImage).To(Equal("nvcr.io/nvstaging/doca/dpf-system" + defaultTag))
}

func TestVariablesFromDPFOperatorConfig_NVIPAMImages(t *testing.T) {
	controllerKey := operatorv1.NVIPAMControllerName.WithContainer(operatorv1.NVIPAMContainerController)
	nodeKey := operatorv1.NVIPAMControllerName.WithContainer(operatorv1.NVIPAMContainerNode)
	defaults := &release.Defaults{}
	NewWithT(t).Expect(defaults.Parse()).To(Succeed())

	t.Run("the deprecated image sets both containers", func(t *testing.T) {
		g := NewWithT(t)
		config := &operatorv1.DPFOperatorConfig{Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{},
			//nolint:staticcheck // Intentionally using deprecated field for backward compatibility
			NVIPAM: &operatorv1.NVIPAMConfiguration{Image: ptr.To("mirror.example.com/nvidia-k8s-ipam")},
		}}
		vars := VariablesFromDPFOperatorConfig(defaults, config, nil)
		g.Expect(vars.Images).To(HaveKeyWithValue(controllerKey, "mirror.example.com/nvidia-k8s-ipam"))
		g.Expect(vars.Images).To(HaveKeyWithValue(nodeKey, "mirror.example.com/nvidia-k8s-ipam"))
	})

	t.Run("each container gets its own chart value", func(t *testing.T) {
		g := NewWithT(t)
		svc := &dpuservicev1.DPUService{}
		for key, img := range map[string]string{
			controllerKey: "ghcr.io/mellanox/nvidia-k8s-ipam:v0.4.0",
			nodeKey:       "mirror.example.com/nvidia-k8s-ipam",
		} {
			edits, err := imageEditsForComponent(key, img)
			g.Expect(err).NotTo(HaveOccurred())
			for _, edit := range edits {
				g.Expect(edit(svc)).To(Succeed())
			}
		}
		g.Expect(svc.Spec.HelmChart.Values.Object).To(Equal(&unstructured.Unstructured{Object: map[string]any{
			operatorv1.NVIPAMControllerName.String(): map[string]any{"nvIpam": map[string]any{
				"controller": map[string]any{"image": map[string]any{"repository": "ghcr.io/mellanox/nvidia-k8s-ipam", "tag": "v0.4.0"}},
				"node":       map[string]any{"image": map[string]any{"repository": "mirror.example.com/nvidia-k8s-ipam"}},
			}},
		}}))
	})
}

// TestVariablesFromDPFOperatorConfig_Tracing checks that the tracing settings of the DPFOperatorConfig
// reach the Variables.
func TestVariablesFromDPFOperatorConfig_Tracing(t *testing.T) {
	defaults := &release.Defaults{}
	NewWithT(t).Expect(defaults.Parse()).To(Succeed())

	configWithTracing := func(tracing *operatorv1.OpenTelemetryCollectorTracingConfiguration) *operatorv1.DPFOperatorConfig {
		return &operatorv1.DPFOperatorConfig{Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{},
			Monitoring:             &operatorv1.MonitoringConfiguration{OpenTelemetryCollector: &operatorv1.OpenTelemetryCollectorConfiguration{Tracing: tracing}},
		}}
	}
	endpoint := operatorv1.OpenTelemetryCollectorExportConfiguration{Endpoint: "http://collector:4318"}

	t.Run("an unset setting keeps the controllers' defaults", func(t *testing.T) {
		g := NewWithT(t)
		vars := VariablesFromDPFOperatorConfig(defaults, configWithTracing(&operatorv1.OpenTelemetryCollectorTracingConfiguration{
			OpenTelemetryCollectorExportConfiguration: endpoint,
		}), nil)

		g.Expect(vars.OpenTelemetryCollector.Tracing.Endpoint).To(Equal("http://collector:4318"))
		g.Expect(vars.OpenTelemetryCollector.Tracing.RecordIdleReconciles).To(BeFalse())
		g.Expect(vars.OpenTelemetryCollector.Tracing.RecordUnchangedReconciles).To(BeFalse())
		g.Expect(vars.OpenTelemetryCollector.Tracing.SamplingPercent).To(BeNil())
		g.Expect(vars.OpenTelemetryCollector.Tracing.RolloutTimeout).To(BeNil())
	})

	t.Run("the settings are passed on", func(t *testing.T) {
		g := NewWithT(t)
		vars := VariablesFromDPFOperatorConfig(defaults, configWithTracing(&operatorv1.OpenTelemetryCollectorTracingConfiguration{
			OpenTelemetryCollectorExportConfiguration: endpoint,
			RecordIdleReconciles:                      ptr.To(true),
			RecordUnchangedReconciles:                 ptr.To(true),
			SamplingPercent:                           ptr.To[int32](25),
			RolloutTimeout:                            &metav1.Duration{Duration: 30 * time.Minute},
		}), nil)

		g.Expect(vars.OpenTelemetryCollector.Tracing.RecordIdleReconciles).To(BeTrue())
		g.Expect(vars.OpenTelemetryCollector.Tracing.RecordUnchangedReconciles).To(BeTrue())
		g.Expect(vars.OpenTelemetryCollector.Tracing.SamplingPercent).To(HaveValue(Equal(int32(25))))
		g.Expect(vars.OpenTelemetryCollector.Tracing.RolloutTimeout).To(Equal(ptr.To(30 * time.Minute)))
	})
}

func Test_imageEditsForComponent_WithoutTag(t *testing.T) {
	g := NewWithT(t)
	// Only the repository is set so that the helm chart's default tag is used.
	edits, err := imageEditsForComponent(operatorv1.MultusName.WithContainer(operatorv1.MultusContainer), "mirror.example.com/multus-cni")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(edits).To(HaveLen(1))

	edits, err = imageEditsForComponent(operatorv1.MultusName.WithContainer(operatorv1.MultusContainer), "mirror.example.com/multus-cni:v1.0.0")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(edits).To(HaveLen(2))
}
