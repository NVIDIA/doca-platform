/*
Copyright 2025 NVIDIA

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

package apivalidation_test

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Provisioning API Validation", func() {
	var testNs *corev1.Namespace

	BeforeEach(func() {
		testNs = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-api-validation-"}}
		Expect(testClient.Create(ctx, testNs)).To(Succeed())
		DeferCleanup(testClient.Delete, ctx, testNs)
	})

	Context("When checking the DPUSet API validations", func() {
		DescribeTable("Validates mutual exclusivity of deprecated and new selector fields", func(dpuSetSpec provisioningv1.DPUSetSpec, expectError bool) {
			dpuSet := getMinimalDPUSet(testNs.Name)
			dpuSet.Spec.DPUNodeSelector = dpuSetSpec.DPUNodeSelector
			//nolint:staticcheck // Testing backward compatibility with deprecated field
			dpuSet.Spec.DPUSelector = dpuSetSpec.DPUSelector
			dpuSet.Spec.DPUDeviceSelector = dpuSetSpec.DPUDeviceSelector
			err := testClient.Create(ctx, dpuSet)
			if expectError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
			Entry("both dpuSelector and dpuDeviceSelector are specified",
				provisioningv1.DPUSetSpec{
					DPUSelector: map[string]string{
						"dpukey1": "dpuvalue1",
					},
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dpukey2": "dpuvalue2",
						},
					},
				}, true),
			Entry("only dpuSelector is specified",
				provisioningv1.DPUSetSpec{
					DPUSelector: map[string]string{
						"dpukey1": "dpuvalue1",
					},
				}, false),
			Entry("only dpuDeviceSelector is specified",
				provisioningv1.DPUSetSpec{
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dpukey1": "dpuvalue1",
						},
					},
				}, false),
			Entry("neither dpuSelector nor dpuDeviceSelector is specified",
				provisioningv1.DPUSetSpec{}, false),
		)
	})
})

func getMinimalDPUSet(namespace string) *provisioningv1.DPUSet {
	return &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpuset",
			Namespace: namespace,
		},
		Spec: provisioningv1.DPUSetSpec{
			DPUTemplate: provisioningv1.DPUTemplate{
				Spec: provisioningv1.DPUTemplateSpec{
					BFB: provisioningv1.BFBReference{
						Name: "somebfb",
					},
					DPUFlavor: "someflavor",
				},
			},
		},
	}
}
