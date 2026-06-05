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
	"sigs.k8s.io/controller-runtime/pkg/client"
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

		DescribeTable("Validates exactly one of bfb or blueFieldSoftware in DPUTemplateSpec",
			func(templateSpec provisioningv1.DPUTemplateSpec, expectError bool) {
				dpuSet := getMinimalDPUSet(testNs.Name)
				dpuSet.Spec.DPUTemplate.Spec = templateSpec
				err := testClient.Create(ctx, dpuSet)
				if expectError {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("exactly one of bfb or blueFieldSoftware must be set"))
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
			},
			Entry("both bfb and blueFieldSoftware are specified",
				provisioningv1.DPUTemplateSpec{
					BFB:               &provisioningv1.BFBReference{Name: "somebfb"},
					BlueFieldSoftware: &provisioningv1.BlueFieldSoftwareReference{Name: "somebfs"},
					DPUFlavor:         "someflavor",
					NodeEffect:        provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, true),
			Entry("only bfb is specified",
				provisioningv1.DPUTemplateSpec{
					BFB:        &provisioningv1.BFBReference{Name: "somebfb"},
					DPUFlavor:  "someflavor",
					NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, false),
			Entry("only blueFieldSoftware is specified",
				provisioningv1.DPUTemplateSpec{
					BlueFieldSoftware: &provisioningv1.BlueFieldSoftwareReference{Name: "somebfs"},
					DPUFlavor:         "someflavor",
					NodeEffect:        provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, false),
			Entry("neither bfb nor blueFieldSoftware is specified",
				provisioningv1.DPUTemplateSpec{
					DPUFlavor:  "someflavor",
					NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, true),
		)
	})

	Context("When checking the DPUFlavor API validations", func() {
		Context("Package validation", func() {
			It("should default package version match policy to AtLeast", func() {
				obj := getMinimalDPUFlavor(testNs.Name)
				obj.Spec.Packages = []provisioningv1.PackageSpec{
					{
						Name: "doca-extra",
						Version: &provisioningv1.PackageVersionSpec{
							Value: "1.2.3",
						},
						RepoFileRef: "/etc/apt/sources.list.d/doca.list",
					},
				}
				Expect(testClient.Create(ctx, obj)).To(Succeed())
				Expect(obj.Spec.Packages[0].Version.MatchPolicy).To(Equal(provisioningv1.PackageVersionMatchAtLeast))
			})

			It("should accept package version settings with exact match policy", func() {
				obj := getMinimalDPUFlavor(testNs.Name)
				obj.Spec.Packages = []provisioningv1.PackageSpec{
					{
						Name: "doca-extra",
						Version: &provisioningv1.PackageVersionSpec{
							Value:       "1.2.3",
							MatchPolicy: provisioningv1.PackageVersionMatchExact,
						},
					},
				}
				Expect(testClient.Create(ctx, obj)).To(Succeed())
			})

			It("should reject package settings without a name", func() {
				obj := getMinimalDPUFlavor(testNs.Name)
				obj.Spec.Packages = []provisioningv1.PackageSpec{{}}
				err := testClient.Create(ctx, obj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Or(
					ContainSubstring("Required value"),
					ContainSubstring("name"),
				))
			})

			DescribeTable("should reject invalid package version settings",
				func(version *provisioningv1.PackageVersionSpec) {
					obj := getMinimalDPUFlavor(testNs.Name)
					obj.Spec.Packages = []provisioningv1.PackageSpec{
						{
							Name:    "doca-extra",
							Version: version,
						},
					}
					err := testClient.Create(ctx, obj)
					Expect(err).To(HaveOccurred())
				},
				Entry("missing version value", &provisioningv1.PackageVersionSpec{}),
				Entry("invalid match policy", &provisioningv1.PackageVersionSpec{
					Value:       "1.2.3",
					MatchPolicy: provisioningv1.PackageVersionMatchPolicy("Latest"),
				}),
			)

			It("should reject duplicate package names", func() {
				obj := getMinimalDPUFlavor(testNs.Name)
				obj.Spec.Packages = []provisioningv1.PackageSpec{
					{Name: "doca-extra", Version: &provisioningv1.PackageVersionSpec{Value: "1.0.0"}},
					{Name: "doca-extra", RepoFileRef: "/etc/apt/sources.list.d/doca.list"},
				}
				err := testClient.Create(ctx, obj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("package names must be unique"))
			})

			It("should accept multiple packages with distinct names", func() {
				obj := getMinimalDPUFlavor(testNs.Name)
				obj.Spec.Packages = []provisioningv1.PackageSpec{
					{Name: "doca-extra"},
					{Name: "doca-ofed"},
				}
				Expect(testClient.Create(ctx, obj)).To(Succeed())
			})
		})

		Context("SystemdService validation", func() {
			It("should reject duplicate systemd service names", func() {
				obj := getMinimalDPUFlavor(testNs.Name)
				obj.Spec.SystemdServices = []provisioningv1.SystemdServiceSpec{
					{Name: "doca-agent", Operation: provisioningv1.SystemdServiceStart},
					{Name: "doca-agent", Operation: provisioningv1.SystemdServiceEnable},
				}
				err := testClient.Create(ctx, obj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("systemd service names must be unique"))
			})

			It("should accept multiple systemd services with distinct names", func() {
				obj := getMinimalDPUFlavor(testNs.Name)
				obj.Spec.SystemdServices = []provisioningv1.SystemdServiceSpec{
					{Name: "doca-agent", Operation: provisioningv1.SystemdServiceStart},
					{Name: "containerd", Operation: provisioningv1.SystemdServiceEnableAndStart},
				}
				Expect(testClient.Create(ctx, obj)).To(Succeed())
			})
		})

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

	// Status.RebootMethod is the aggregated reboot-method marker.
	// These tests enforce the API contract: status subresource semantics
	// (no spec-side update), optional on Create, and enum-validated values.
	Context("When checking the DPUNode Status.RebootMethod field", func() {
		It("does not require Status.RebootMethod on Create (omitempty)", func() {
			node := getMinimalDPUNode(testNs.Name)
			Expect(testClient.Create(ctx, node)).To(Succeed())
			Expect(node.Status.RebootMethod).To(BeNil())
		})

		It("ignores Status.RebootMethod on a non-status Update (status subresource)", func() {
			node := getMinimalDPUNode(testNs.Name)
			Expect(testClient.Create(ctx, node)).To(Succeed())

			node.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodPowerCycle)
			Expect(testClient.Update(ctx, node)).To(Succeed())

			refetched := &provisioningv1.DPUNode{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(node), refetched)).To(Succeed())
			Expect(refetched.Status.RebootMethod).To(BeNil(), "non-status Update must not stamp Status.RebootMethod")
		})

		It("accepts the field via the status subresource", func() {
			node := getMinimalDPUNode(testNs.Name)
			Expect(testClient.Create(ctx, node)).To(Succeed())

			node.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodPowerCycle)
			Expect(testClient.Status().Update(ctx, node)).To(Succeed())

			refetched := &provisioningv1.DPUNode{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(node), refetched)).To(Succeed())
			Expect(refetched.Status.RebootMethod).NotTo(BeNil())
			Expect(*refetched.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
		})

		It("rejects Status.RebootMethod values outside the RebootMethodType enum", func() {
			node := getMinimalDPUNode(testNs.Name)
			Expect(testClient.Create(ctx, node)).To(Succeed())

			bogus := provisioningv1.RebootMethodType("BogusUserValue")
			node.Status.RebootMethod = &bogus
			err := testClient.Status().Update(ctx, node)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Or(
				ContainSubstring("supported values"),
				ContainSubstring("Unsupported value"),
			))
		})
	})

	Context("When checking the DPUDevice Status.PSID field", func() {
		It("does not require Status.PSID on Create (omitempty)", func() {
			device := getMinimalDPUDevice(testNs.Name)
			Expect(testClient.Create(ctx, device)).To(Succeed())
			Expect(device.Status.PSID).To(BeNil())
		})

		It("accepts a valid PSID via the status subresource", func() {
			device := getMinimalDPUDevice(testNs.Name)
			Expect(testClient.Create(ctx, device)).To(Succeed())

			device.Status.PSID = ptr.To("MT25066004C7")
			Expect(testClient.Status().Update(ctx, device)).To(Succeed())

			refetched := &provisioningv1.DPUDevice{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(device), refetched)).To(Succeed())
			Expect(refetched.Status.PSID).NotTo(BeNil())
			Expect(*refetched.Status.PSID).To(Equal("MT25066004C7"))
		})

		It("accepts Status.PSID values without a CRD pattern (vendor-agnostic)", func() {
			device := getMinimalDPUDevice(testNs.Name)
			Expect(testClient.Create(ctx, device)).To(Succeed())

			device.Status.PSID = ptr.To("N/A")
			Expect(testClient.Status().Update(ctx, device)).To(Succeed())

			refetched := &provisioningv1.DPUDevice{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(device), refetched)).To(Succeed())
			Expect(refetched.Status.PSID).NotTo(BeNil())
			Expect(*refetched.Status.PSID).To(Equal("N/A"))
		})
	})

	Context("When checking the DPUDevice PSID label", func() {
		const dpuDevicePSIDLabel = "provisioning.dpu.nvidia.com/dpudevice-psid"

		It("rejects label values that are not valid Kubernetes label values", func() {
			device := getMinimalDPUDevice(testNs.Name)
			Expect(testClient.Create(ctx, device)).To(Succeed())

			device.Labels = map[string]string{
				dpuDevicePSIDLabel: "N/A",
			}
			err := testClient.Update(ctx, device)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(And(
				ContainSubstring("metadata.labels"),
				ContainSubstring("N/A"),
			))
		})

		It("accepts a valid PSID label value", func() {
			device := getMinimalDPUDevice(testNs.Name)
			Expect(testClient.Create(ctx, device)).To(Succeed())

			device.Labels = map[string]string{
				dpuDevicePSIDLabel: "MT25066004C7",
			}
			Expect(testClient.Update(ctx, device)).To(Succeed())

			refetched := &provisioningv1.DPUDevice{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(device), refetched)).To(Succeed())
			Expect(refetched.Labels).To(HaveKeyWithValue(dpuDevicePSIDLabel, "MT25066004C7"))
		})
	})
})

func getMinimalDPUNode(namespace string) *provisioningv1.DPUNode {
	return &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dpunode-test-",
			Namespace:    namespace,
		},
	}
}

func getMinimalDPUDevice(namespace string) *provisioningv1.DPUDevice {
	return &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dpudevice-test-",
			Namespace:    namespace,
		},
		Spec: provisioningv1.DPUDeviceSpec{
			SerialNumber: "MT25066004C7",
		},
	}
}

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
					BFB: &provisioningv1.BFBReference{
						Name: "somebfb",
					},
					DPUFlavor:  "someflavor",
					NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				},
			},
		},
	}
}
