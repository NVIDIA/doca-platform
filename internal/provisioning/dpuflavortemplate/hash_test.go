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
)

var _ = Describe("Hash", func() {
	It("is stable for the same inputs", func() {
		spec := provisioningv1.DPUFlavorTemplateSpec{
			Template:     "spec: {}",
			DPUResources: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")},
		}
		h1, err := Hash(spec)
		Expect(err).NotTo(HaveOccurred())
		h2, err := Hash(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(h1).To(Equal(h2))
		Expect(h1).To(HaveLen(hashPrefixLen))
	})

	It("changes when the template body changes", func() {
		base := provisioningv1.DPUFlavorTemplateSpec{Template: "spec: {}"}
		other := provisioningv1.DPUFlavorTemplateSpec{Template: "spec: {a: 1}"}
		h1, err := Hash(base)
		Expect(err).NotTo(HaveOccurred())
		h2, err := Hash(other)
		Expect(err).NotTo(HaveOccurred())
		Expect(h1).NotTo(Equal(h2))
	})

	It("changes when a structured resource field changes", func() {
		base := provisioningv1.DPUFlavorTemplateSpec{
			Template:     "spec: {}",
			DPUResources: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")},
		}
		other := provisioningv1.DPUFlavorTemplateSpec{
			Template:     "spec: {}",
			DPUResources: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8")},
		}
		h1, err := Hash(base)
		Expect(err).NotTo(HaveOccurred())
		h2, err := Hash(other)
		Expect(err).NotTo(HaveOccurred())
		Expect(h1).NotTo(Equal(h2))
	})
})

var _ = Describe("ValuesHash", func() {
	It("is stable and independent of key order", func() {
		h1, err := ValuesHash(rawValues(`{"mtu":9000,"hugepages":3072}`))
		Expect(err).NotTo(HaveOccurred())
		h2, err := ValuesHash(rawValues(`{"hugepages":3072,"mtu":9000}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(h1).To(Equal(h2))
		Expect(h1).To(HaveLen(hashPrefixLen))
	})

	It("treats nil and empty values as the same empty map", func() {
		h1, err := ValuesHash(nil)
		Expect(err).NotTo(HaveOccurred())
		h2, err := ValuesHash(rawValues(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(h1).To(Equal(h2))
	})

	It("changes when values change", func() {
		h1, err := ValuesHash(rawValues(`{"mtu":9000}`))
		Expect(err).NotTo(HaveOccurred())
		h2, err := ValuesHash(rawValues(`{"mtu":1500}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(h1).NotTo(Equal(h2))
	})

	It("returns an error for malformed values", func() {
		_, err := ValuesHash(rawValues(`{not json`))
		Expect(err).To(HaveOccurred())
	})
})
