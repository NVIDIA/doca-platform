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

package webhooks

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("validateIPRangeOrder", func() {
	It("rejects when startIP > endIP", func() {
		err := validateIPRangeOrder(provisioningv1.IPRange{
			StartIP: "10.0.110.125",
			EndIP:   "10.0.110.120",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("startIP must be less than or equal to endIP"))
	})

	It("allows when startIP == endIP", func() {
		err := validateIPRangeOrder(provisioningv1.IPRange{
			StartIP: "10.0.110.120",
			EndIP:   "10.0.110.120",
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("allows when startIP < endIP", func() {
		err := validateIPRangeOrder(provisioningv1.IPRange{
			StartIP: "10.0.110.120",
			EndIP:   "10.0.110.125",
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects invalid IPs", func() {
		err := validateIPRangeOrder(provisioningv1.IPRange{
			StartIP: "not-an-ip",
			EndIP:   "10.0.110.125",
		})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("DPUDiscovery IP range validation", func() {
	ctx := context.Background()

	It("rejects create when startIP is greater than endIP", func() {
		obj := &provisioningv1.DPUDiscovery{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-range",
				Namespace: "default",
			},
			Spec: provisioningv1.DPUDiscoverySpec{
				IPRangeSpec: provisioningv1.IPRangeValidationSpec{
					IPRange: provisioningv1.IPRange{
						StartIP: "10.0.110.125",
						EndIP:   "10.0.110.120",
					},
				},
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("startIP must be less than or equal to endIP"))
	})

	It("allows create when startIP equals endIP", func() {
		obj := &provisioningv1.DPUDiscovery{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-single-ip",
				Namespace: "default",
			},
			Spec: provisioningv1.DPUDiscoverySpec{
				IPRangeSpec: provisioningv1.IPRangeValidationSpec{
					IPRange: provisioningv1.IPRange{
						StartIP: "10.0.110.120",
						EndIP:   "10.0.110.120",
					},
				},
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
	})

	It("allows create when startIP is less than endIP", func() {
		obj := &provisioningv1.DPUDiscovery{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-range",
				Namespace: "default",
			},
			Spec: provisioningv1.DPUDiscoverySpec{
				IPRangeSpec: provisioningv1.IPRangeValidationSpec{
					IPRange: provisioningv1.IPRange{
						StartIP: "10.0.110.120",
						EndIP:   "10.0.110.125",
					},
				},
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
	})
})
