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

package dpuflavortemplate

import (
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
)

func rawValues(s string) *runtime.RawExtension {
	return &runtime.RawExtension{Raw: []byte(s)}
}

var _ = Describe("Render", func() {
	It("substitutes templated values into the rendered DPUFlavor", func() {
		spec := provisioningv1.DPUFlavorTemplateSpec{
			Template: `
spec:
  bfcfgParameters:
  - "MTU={{ .mtu }}"
  - "HUGEPAGES={{ .hugepages }}"
`,
		}
		flavor, err := Render(spec, rawValues(`{"mtu":9000,"hugepages":3072}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(flavor.Spec.BFCfgParameters).To(Equal([]string{"MTU=9000", "HUGEPAGES=3072"}))
	})

	It("renders a template that has no placeholders with nil values", func() {
		spec := provisioningv1.DPUFlavorTemplateSpec{
			Template: `
spec:
  bfcfgParameters:
  - "STATIC=1"
`,
		}
		flavor, err := Render(spec, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(flavor.Spec.BFCfgParameters).To(Equal([]string{"STATIC=1"}))
	})

	It("stamps the structured resource fields when the body omits them", func() {
		spec := provisioningv1.DPUFlavorTemplateSpec{
			Template:                `spec: {}`,
			DPUResources:            corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")},
			SystemReservedResources: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		}
		flavor, err := Render(spec, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(flavor.Spec.DPUResources.Cpu().String()).To(Equal("4"))
		Expect(flavor.Spec.SystemReservedResources.Cpu().String()).To(Equal("1"))
	})

	It("gives the structured resource fields precedence over the rendered body", func() {
		spec := provisioningv1.DPUFlavorTemplateSpec{
			Template: `
spec:
  dpuResources:
    cpu: "1"
  systemReservedResources:
    cpu: "1"
`,
			DPUResources:            corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8")},
			SystemReservedResources: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
		}
		flavor, err := Render(spec, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(flavor.Spec.DPUResources.Cpu().String()).To(Equal("8"))
		Expect(flavor.Spec.SystemReservedResources.Cpu().String()).To(Equal("2"))
	})

	It("leaves the body's resources untouched when the structured fields are unset", func() {
		spec := provisioningv1.DPUFlavorTemplateSpec{
			Template: `
spec:
  dpuResources:
    cpu: "3"
`,
		}
		flavor, err := Render(spec, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(flavor.Spec.DPUResources.Cpu().String()).To(Equal("3"))
	})

	DescribeTable("error cases",
		func(spec provisioningv1.DPUFlavorTemplateSpec, values *runtime.RawExtension, wantErr string) {
			_, err := Render(spec, values)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(wantErr))
		},
		Entry("references a key missing from the values",
			provisioningv1.DPUFlavorTemplateSpec{Template: `spec:
  bfcfgParameters: ["MTU={{ .mtu }}"]`},
			rawValues(`{}`),
			"failed to render template"),
		Entry("has invalid template syntax",
			provisioningv1.DPUFlavorTemplateSpec{Template: `spec: {{ .mtu `},
			rawValues(`{"mtu":9000}`),
			"failed to parse template"),
		Entry("renders content that is not a valid DPUFlavor",
			provisioningv1.DPUFlavorTemplateSpec{Template: "- a\n- b\n"},
			nil,
			"rendered template is not a valid DPUFlavor"),
		Entry("is given malformed values",
			provisioningv1.DPUFlavorTemplateSpec{Template: `spec: {}`},
			rawValues(`{not json`),
			"failed to decode DPUDevice.spec.values"),
		Entry("declares a nested template with define",
			provisioningv1.DPUFlavorTemplateSpec{Template: `{{ define "t" }}x{{ end }}spec: {}`},
			nil,
			"must not declare nested templates"),
		Entry("declares a nested template with block",
			provisioningv1.DPUFlavorTemplateSpec{Template: `{{ block "b" . }}spec: {}{{ end }}`},
			nil,
			"must not declare nested templates"),
		Entry("invokes the root template",
			provisioningv1.DPUFlavorTemplateSpec{Template: `spec: {}
{{ template "dpuflavortemplate" }}`},
			nil,
			"must not invoke templates"),
		Entry("invokes a template inside a nested action",
			provisioningv1.DPUFlavorTemplateSpec{Template: `spec: {}
{{ if .x }}{{ template "t" }}{{ end }}`},
			nil,
			"must not invoke templates"),
		Entry("invokes a template inside a range body",
			provisioningv1.DPUFlavorTemplateSpec{Template: `spec: {}
{{ range .a }}{{ template "t" }}{{ end }}`},
			nil,
			"must not invoke templates"),
		Entry("invokes a template inside a with body",
			provisioningv1.DPUFlavorTemplateSpec{Template: `spec: {}
{{ with .x }}{{ template "t" }}{{ end }}`},
			nil,
			"must not invoke templates"),
		Entry("exceeds the template body size limit",
			provisioningv1.DPUFlavorTemplateSpec{Template: strings.Repeat("#", maxTemplateBytes+1)},
			nil,
			"exceeding the"),
	)

	It("fails a render whose output exceeds the size limit", func() {
		// 512 elements, each emitting 4 KiB, amplify a small template body into 2 MiB
		// of output, exceeding maxRenderedBytes.
		spec := provisioningv1.DPUFlavorTemplateSpec{
			Template: `{{ range .a }}` + strings.Repeat("x", 4096) + `{{ end }}`,
		}
		values := `{"a":[` + strings.Repeat("0,", 511) + `0]}`
		_, err := Render(spec, rawValues(values))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("rendered output exceeds"))
	})
})

// telemetryLevelLabelsTemplate is the telemetry label-enrichment use case: it renders
// /opt/mellanox/doca/telemetry/config-dpu/level_labels.ini from per-device values so
// telemetry services can label their metrics.
const telemetryLevelLabelsTemplate = `spec:
  configFiles:
  - path: /opt/mellanox/doca/telemetry/config-dpu/level_labels.ini
    operation: override
    permissions: "0644"
    raw: |
      [global_labels]
      pod={{ .podId }}
      su={{ .suId }}
      rack={{ .rack }}

      [device_labels]
      {{- range .eastWestDevices }}
      {{- if eq .deviceType "physical" }}
      {{ .device }}=rail|{{ .rail }}|plane|{{ .plane }}|device_type|{{ .deviceType }}|traffic_direction|east-west|destination|{{ .destination }}|destination_port|{{ .destinationPort }}
      {{- else }}
      {{ .device }}=rail|{{ .rail }}|device_type|{{ .deviceType }}|traffic_direction|east-west
      {{- end }}
      {{- end }}

      [device_mapping]

      [data_types_mapping]
      ethtool_event=device_name|netif
      ppcc_eth=device_name|mst
      ifconfig_event=device_name|netif
      amber_event=device_name|mst
`

const telemetryLevelLabelsValues = `{
  "podId": 1,
  "suId": 0,
  "rack": 0,
  "eastWestDevices": [
    {"device": "0000:03:00.0", "deviceType": "physical", "rail": 0, "plane": 0, "destination": "leaf-p0-su00-r0", "destinationPort": "swp1s1"},
    {"device": "0000:03:00.1", "deviceType": "physical", "rail": 0, "plane": 1, "destination": "leaf-p1-su00-r0", "destinationPort": "swp1s1"},
    {"device": "0000:03:00.4", "deviceType": "virtual", "rail": 0}
  ]
}`

const telemetryLevelLabelsRendered = `[global_labels]
pod=1
su=0
rack=0

[device_labels]
0000:03:00.0=rail|0|plane|0|device_type|physical|traffic_direction|east-west|destination|leaf-p0-su00-r0|destination_port|swp1s1
0000:03:00.1=rail|0|plane|1|device_type|physical|traffic_direction|east-west|destination|leaf-p1-su00-r0|destination_port|swp1s1
0000:03:00.4=rail|0|device_type|virtual|traffic_direction|east-west

[device_mapping]

[data_types_mapping]
ethtool_event=device_name|netif
ppcc_eth=device_name|mst
ifconfig_event=device_name|netif
amber_event=device_name|mst
`

var _ = Describe("Render telemetry level labels", func() {
	It("renders the documented level_labels.ini config file from device values", func() {
		spec := provisioningv1.DPUFlavorTemplateSpec{Template: telemetryLevelLabelsTemplate}
		flavor, err := Render(spec, rawValues(telemetryLevelLabelsValues))
		Expect(err).NotTo(HaveOccurred())

		Expect(flavor.Spec.ConfigFiles).To(HaveLen(1))
		file := flavor.Spec.ConfigFiles[0]
		Expect(file.Path).To(Equal("/opt/mellanox/doca/telemetry/config-dpu/level_labels.ini"))
		Expect(file.Operation).To(Equal(provisioningv1.FileOverride))
		Expect(file.Permissions).To(Equal("0644"))
		Expect(file.Raw).NotTo(BeNil())
		Expect(*file.Raw).To(Equal(telemetryLevelLabelsRendered))
	})
})
