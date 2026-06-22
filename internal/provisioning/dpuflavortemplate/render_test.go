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
	)
})
