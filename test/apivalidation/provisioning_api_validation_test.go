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
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
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

	Context("When checking the DPUFlavor API validations", func() {
		Context("NVConfig validation", func() {
			// ✅ Valid Configurations
			Context("Valid Configurations", func() {
				It("should accept multiple port devices (p0 + p1)", func() {
					obj := getMinimalDPUFlavor(testNs.Name)
					p0 := "p0"
					p1 := "P1" // Test case-insensitive - uppercase is valid
					obj.Spec.NVConfig = []provisioningv1.NVConfig{
						{Device: &p0, Parameters: []string{"LINK_TYPE_P1=ETH", "NUM_OF_VFS=8"}},
						{Device: &p1, Parameters: []string{"LINK_TYPE_P1=IB"}},
					}
					Expect(testClient.Create(ctx, obj)).To(Succeed())
				})

				It("should accept wildcard/unspecified device", func() {
					obj := getMinimalDPUFlavor(testNs.Name)
					obj.Spec.NVConfig = []provisioningv1.NVConfig{
						{Parameters: []string{"SRIOV_EN=1"}}, // Nil device = wildcard
					}
					Expect(testClient.Create(ctx, obj)).To(Succeed())
				})

				It("should accept empty parameter value", func() {
					obj := getMinimalDPUFlavor(testNs.Name)
					dev := "p0"
					obj.Spec.NVConfig = []provisioningv1.NVConfig{
						{Device: &dev, Parameters: []string{"FLAG="}}, // Empty value OK
					}
					Expect(testClient.Create(ctx, obj)).To(Succeed())
				})
			})

			// ❌ Invalid Configurations
			Context("Invalid Configurations", func() {
				DescribeTable("should reject invalid device enum values",
					func(name string, nvconfig []provisioningv1.NVConfig) {
						obj := getMinimalDPUFlavor(testNs.Name)
						obj.Name = name
						obj.Spec.NVConfig = nvconfig
						err := testClient.Create(ctx, obj)
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(Or(
							ContainSubstring("Unsupported value"),
							ContainSubstring("enum"),
						))
					},
					Entry("invalid PCI address (no longer supported)", "cel-invalid-pci", []provisioningv1.NVConfig{
						{Device: ptr.To("0000:b1:00.0"), Parameters: []string{"KEY=VAL"}},
					}),
					Entry("invalid MST path (no longer supported)", "cel-invalid-mst", []provisioningv1.NVConfig{
						{Device: ptr.To("/dev/mst/mt4129_pciconf0"), Parameters: []string{"KEY=VAL"}},
					}),
					Entry("invalid port identifier (p2)", "cel-invalid-port", []provisioningv1.NVConfig{
						{Device: ptr.To("p2"), Parameters: []string{"KEY=VAL"}},
					}),
				)

				It("should reject wildcard mixed with specific devices", func() {
					obj := getMinimalDPUFlavor(testNs.Name)
					obj.Name = "cel-wildcard-exclusivity"
					obj.Spec.NVConfig = []provisioningv1.NVConfig{
						{Device: ptr.To("*"), Parameters: []string{"GLOBAL=1"}},
						{Device: ptr.To("p0"), Parameters: []string{"SPECIFIC=1"}},
					}
					err := testClient.Create(ctx, obj)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("must be the only"))
				})

				DescribeTable("should reject duplicate devices",
					func(name string, nvconfig []provisioningv1.NVConfig) {
						obj := getMinimalDPUFlavor(testNs.Name)
						obj.Name = name
						obj.Spec.NVConfig = nvconfig
						err := testClient.Create(ctx, obj)
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring("must be unique"))
					},
					Entry("duplicate port p0 (case-insensitive)", "cel-dup-p0", []provisioningv1.NVConfig{
						{Device: ptr.To("p0"), Parameters: []string{"PARAM1=A"}},
						{Device: ptr.To("P0"), Parameters: []string{"PARAM2=B"}}, // Should be rejected (same as p0)
					}),
					Entry("duplicate wildcard (nil + explicit)", "cel-dup-wildcard", []provisioningv1.NVConfig{
						{Parameters: []string{"PARAM1=A"}},                      // nil = *
						{Device: ptr.To("*"), Parameters: []string{"PARAM2=B"}}, // explicit *
					}),
				)

				DescribeTable("should reject invalid parameter formats",
					func(name string, nvconfig []provisioningv1.NVConfig) {
						obj := getMinimalDPUFlavor(testNs.Name)
						obj.Name = name
						obj.Spec.NVConfig = nvconfig
						err := testClient.Create(ctx, obj)
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(Or(
							ContainSubstring("pattern"),
							ContainSubstring("should match"),
						))
					},
					Entry("missing equals", "cel-no-equals", []provisioningv1.NVConfig{
						{Device: ptr.To("p0"), Parameters: []string{"INVALID"}},
					}),
					Entry("empty key", "cel-empty-key", []provisioningv1.NVConfig{
						{Device: ptr.To("p0"), Parameters: []string{"=value"}},
					}),
					Entry("spaces in parameter", "cel-spaces", []provisioningv1.NVConfig{
						{Device: ptr.To("p1"), Parameters: []string{"KEY = VALUE"}},
					}),
				)

				// Note: MaxItems=3 constraint matches the maximum possible unique enum values
				// (*, p0, p1). With uniqueness validation, this is the theoretical maximum.

				It("should reject too many parameters (MaxItems=32)", func() {
					obj := getMinimalDPUFlavor(testNs.Name)
					obj.Name = "cel-maxitems-params"
					params := make([]string, 33)
					for i := 0; i < 33; i++ {
						params[i] = fmt.Sprintf("PARAM%d=value", i)
					}
					obj.Spec.NVConfig = []provisioningv1.NVConfig{
						{Device: ptr.To("p0"), Parameters: params},
					}
					err := testClient.Create(ctx, obj)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Or(
						ContainSubstring("maxItems"),
						ContainSubstring("Too many"), // Capital T!
					))
				})
			})
		})
	})
})

func getMinimalDPUFlavor(namespace string) *provisioningv1.DPUFlavor {
	return &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dpuflavor-test-",
			Namespace:    namespace,
		},
		Spec: provisioningv1.DPUFlavorSpec{},
	}
}

func getMinimalDPUSet(namespace string) *provisioningv1.DPUSet {
	return &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpuset",
			Namespace: namespace,
		},
		Spec: provisioningv1.DPUSetSpec{
			Strategy: provisioningv1.DPUSetStrategy{
				Type: provisioningv1.OnDeleteStrategyType,
			},
			DPUTemplate: provisioningv1.DPUTemplate{
				Spec: provisioningv1.DPUTemplateSpec{
					BFB: provisioningv1.BFBReference{
						Name: "somebfb",
					},
					DPUFlavor:  "someflavor",
					NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				},
			},
		},
	}
}
