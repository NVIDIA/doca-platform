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
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
					DPUFlavor:         ptr.To("someflavor"),
					NodeEffect:        provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, true),
			Entry("only bfb is specified",
				provisioningv1.DPUTemplateSpec{
					BFB:        &provisioningv1.BFBReference{Name: "somebfb"},
					DPUFlavor:  ptr.To("someflavor"),
					NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, false),
			Entry("only blueFieldSoftware is specified",
				provisioningv1.DPUTemplateSpec{
					BlueFieldSoftware: &provisioningv1.BlueFieldSoftwareReference{Name: "somebfs"},
					DPUFlavor:         ptr.To("someflavor"),
					NodeEffect:        provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, false),
			Entry("neither bfb nor blueFieldSoftware is specified",
				provisioningv1.DPUTemplateSpec{
					DPUFlavor:  ptr.To("someflavor"),
					NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, true),
		)

		DescribeTable("Validates exactly one of dpuFlavor or dpuFlavorTemplate in DPUTemplateSpec",
			func(templateSpec provisioningv1.DPUTemplateSpec, expectError bool) {
				dpuSet := getMinimalDPUSet(testNs.Name)
				dpuSet.Spec.DPUTemplate.Spec = templateSpec
				err := testClient.Create(ctx, dpuSet)
				if expectError {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("exactly one of dpuFlavor or dpuFlavorTemplate must be set"))
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
			},
			Entry("both dpuFlavor and dpuFlavorTemplate are specified",
				provisioningv1.DPUTemplateSpec{
					BFB:               &provisioningv1.BFBReference{Name: "somebfb"},
					DPUFlavor:         ptr.To("someflavor"),
					DPUFlavorTemplate: ptr.To("sometemplate"),
					NodeEffect:        provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, true),
			Entry("only dpuFlavor is specified",
				provisioningv1.DPUTemplateSpec{
					BFB:        &provisioningv1.BFBReference{Name: "somebfb"},
					DPUFlavor:  ptr.To("someflavor"),
					NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, false),
			Entry("only dpuFlavorTemplate is specified",
				provisioningv1.DPUTemplateSpec{
					BFB:               &provisioningv1.BFBReference{Name: "somebfb"},
					DPUFlavorTemplate: ptr.To("sometemplate"),
					NodeEffect:        provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, false),
			Entry("neither dpuFlavor nor dpuFlavorTemplate is specified",
				provisioningv1.DPUTemplateSpec{
					BFB:        &provisioningv1.BFBReference{Name: "somebfb"},
					NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, true),
		)
	})

	Context("When checking the DPU API validations", func() {
		DescribeTable("Validates exactly one of bfb or blueFieldSoftware in DPUSpec",
			func(dpuSpec provisioningv1.DPUSpec, expectError bool) {
				dpu := getMinimalDPU(testNs.Name)
				dpu.Spec = dpuSpec
				err := testClient.Create(ctx, dpu)
				if expectError {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("exactly one of bfb or blueFieldSoftware must be set"))
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
			},
			Entry("both bfb and blueFieldSoftware are specified",
				provisioningv1.DPUSpec{
					BFB:               ptr.To("somebfb"),
					BlueFieldSoftware: ptr.To("somebfs"),
					DPUDeviceName:     "some-device",
					DPUNodeName:       "some-node",
					SerialNumber:      "SN123",
					DPUFlavor:         "someflavor",
					NodeEffect:        provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, true),
			Entry("only bfb is specified",
				provisioningv1.DPUSpec{
					BFB:           ptr.To("somebfb"),
					DPUDeviceName: "some-device",
					DPUNodeName:   "some-node",
					SerialNumber:  "SN123",
					DPUFlavor:     "someflavor",
					NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, false),
			Entry("only blueFieldSoftware is specified",
				provisioningv1.DPUSpec{
					BlueFieldSoftware: ptr.To("somebfs"),
					DPUDeviceName:     "some-device",
					DPUNodeName:       "some-node",
					SerialNumber:      "SN123",
					DPUFlavor:         "someflavor",
					NodeEffect:        provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				}, false),
			Entry("neither bfb nor blueFieldSoftware is specified",
				provisioningv1.DPUSpec{
					DPUDeviceName: "some-device",
					DPUNodeName:   "some-node",
					SerialNumber:  "SN123",
					DPUFlavor:     "someflavor",
					NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
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

				It("should accept force on a wildcard entry", func() {
					obj := getMinimalDPUFlavor(testNs.Name)
					obj.Spec.NVConfig = []provisioningv1.NVConfig{
						{Parameters: []string{"ADVANCED_PCI_SETTINGS=1", "MAX_ACC_OUT_READ=44"}, Force: ptr.To(true)},
					}
					Expect(testClient.Create(ctx, obj)).To(Succeed())

					created := &provisioningv1.DPUFlavor{}
					Expect(testClient.Get(ctx, client.ObjectKeyFromObject(obj), created)).To(Succeed())
					Expect(created.Spec.NVConfig[0].Force).To(HaveValue(BeTrue()))
				})

				It("should accept force per port and keep the ports independent", func() {
					obj := getMinimalDPUFlavor(testNs.Name)
					obj.Spec.NVConfig = []provisioningv1.NVConfig{
						{Device: ptr.To("p0"), Parameters: []string{"ADVANCED_PCI_SETTINGS=1"}, Force: ptr.To(true)},
						{Device: ptr.To("p1"), Parameters: []string{"LINK_TYPE_P1=ETH"}},
					}
					Expect(testClient.Create(ctx, obj)).To(Succeed())

					created := &provisioningv1.DPUFlavor{}
					Expect(testClient.Get(ctx, client.ObjectKeyFromObject(obj), created)).To(Succeed())
					Expect(created.Spec.NVConfig[0].Force).To(HaveValue(BeTrue()))
					// Unset must stay unset rather than being defaulted to false by the API server.
					Expect(created.Spec.NVConfig[1].Force).To(BeNil())
				})

				It("should leave force unset when omitted", func() {
					obj := getMinimalDPUFlavor(testNs.Name)
					obj.Spec.NVConfig = []provisioningv1.NVConfig{
						{Parameters: []string{"SRIOV_EN=1"}},
					}
					Expect(testClient.Create(ctx, obj)).To(Succeed())

					created := &provisioningv1.DPUFlavor{}
					Expect(testClient.Get(ctx, client.ObjectKeyFromObject(obj), created)).To(Succeed())
					Expect(created.Spec.NVConfig[0].Force).To(BeNil())
				})

				It("should accept force under hostNetworkInterfaceConfigs even though it is inert there", func() {
					// NVConfig is shared with NetworkInterfaceConfig, so the schema carries force in
					// both places. Nothing reads it under hostNetworkInterfaceConfigs; this pins the
					// schema behavior so a future guard is a deliberate change, not a surprise.
					obj := getMinimalDPUFlavor(testNs.Name)
					obj.Spec.HostNetworkInterfaceConfigs = []provisioningv1.NetworkInterfaceConfig{
						{
							PortNumber: 0,
							NVConfig:   &provisioningv1.NVConfig{Parameters: []string{"SRIOV_EN=1"}, Force: ptr.To(true)},
						},
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

				It("should reject adding force to an existing flavor", func() {
					// DPUFlavorSpec is immutable, so force cannot be switched on in place: enabling
					// it on an existing deployment means creating a new flavor and repointing the
					// DPUSet at it.
					obj := getMinimalDPUFlavor(testNs.Name)
					obj.Name = "cel-force-immutable"
					obj.Spec.NVConfig = []provisioningv1.NVConfig{
						{Parameters: []string{"ADVANCED_PCI_SETTINGS=1"}},
					}
					Expect(testClient.Create(ctx, obj)).To(Succeed())

					created := &provisioningv1.DPUFlavor{}
					Expect(testClient.Get(ctx, client.ObjectKeyFromObject(obj), created)).To(Succeed())
					created.Spec.NVConfig[0].Force = ptr.To(true)
					err := testClient.Update(ctx, created)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("immutable"))
				})

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

		DescribeTable("Validates the ServiceReadiness gate enum",
			func(serviceReadiness *provisioningv1.ServiceReadiness, expectError bool) {
				obj := getMinimalDPUFlavor(testNs.Name)
				obj.Spec.ServiceReadiness = serviceReadiness
				err := testClient.Create(ctx, obj)
				if expectError {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Or(
						ContainSubstring("Unsupported value"),
						ContainSubstring("supported values"),
					))
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
			},
			Entry("DPUServiceCriticalPodsReady is accepted", &provisioningv1.ServiceReadiness{
				Gate: provisioningv1.GateDPUServiceCriticalPodsReady,
			}, false),
			Entry("OperationalReady is accepted", &provisioningv1.ServiceReadiness{
				Gate: provisioningv1.GateOperationalReady,
			}, false),
			// Both the block and the gate inside it are optional, so these mean "do not wait"
			// rather than being invalid. Gate is a non-pointer string with omitempty, so an empty
			// gate is serialized away and is indistinguishable from an absent one.
			Entry("an empty serviceReadiness block is accepted", &provisioningv1.ServiceReadiness{}, false),
			Entry("an absent serviceReadiness block is accepted", nil, false),
			Entry("a gate outside the enum is rejected", &provisioningv1.ServiceReadiness{
				Gate: provisioningv1.ServiceReadinessGate("NodeProblemsReady"),
			}, true),
		)
	})

	Context("When checking the DPUFlavorTemplate API validations", func() {
		It("should accept a DPUFlavorTemplate with a non-empty template", func() {
			Expect(testClient.Create(ctx, getMinimalDPUFlavorTemplate(testNs.Name))).To(Succeed())
		})

		It("should reject a DPUFlavorTemplate with an empty template", func() {
			obj := getMinimalDPUFlavorTemplate(testNs.Name)
			obj.Spec.Template = ""
			err := testClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Or(
				ContainSubstring("should be at least 1 chars long"),
				ContainSubstring("template"),
			))
		})

		It("should accept dpuResources and systemReservedResources and round-trip them", func() {
			obj := getMinimalDPUFlavorTemplate(testNs.Name)
			obj.Spec.DPUResources = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			}
			obj.Spec.SystemReservedResources = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			}
			Expect(testClient.Create(ctx, obj)).To(Succeed())

			refetched := &provisioningv1.DPUFlavorTemplate{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(obj), refetched)).To(Succeed())
			Expect(refetched.Spec.DPUResources).To(HaveKey(corev1.ResourceCPU))
			Expect(refetched.Spec.DPUResources).To(HaveKey(corev1.ResourceMemory))
			Expect(refetched.Spec.SystemReservedResources).To(HaveKey(corev1.ResourceMemory))
		})
	})

	Context("When checking the DPUDevice spec.values field", func() {
		It("does not require spec.values on Create (omitempty)", func() {
			device := getMinimalDPUDevice(testNs.Name)
			Expect(testClient.Create(ctx, device)).To(Succeed())
			Expect(device.Spec.Values).To(BeNil())
		})

		It("accepts an arbitrary JSON object in spec.values", func() {
			device := getMinimalDPUDevice(testNs.Name)
			device.Spec.Values = &runtime.RawExtension{Raw: []byte(`{"mtu":1500,"hugepages":3072}`)}
			Expect(testClient.Create(ctx, device)).To(Succeed())

			refetched := &provisioningv1.DPUDevice{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(device), refetched)).To(Succeed())
			Expect(refetched.Spec.Values).NotTo(BeNil())
			Expect(string(refetched.Spec.Values.Raw)).To(ContainSubstring("mtu"))
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

	// Status.IdentityMode is immutable once set and enum-validated: an immutable record of which
	// authentication mechanism the DPU Agent uses, set exactly once from the empty value.
	Context("When checking the DPU Status.IdentityMode field", func() {
		It("does not require IdentityMode on Create (omitempty)", func() {
			dpu := getMinimalDPU(testNs.Name)
			Expect(testClient.Create(ctx, dpu)).To(Succeed())
			Expect(dpu.Status.IdentityMode).To(BeNil())
		})

		It("accepts a stamp from unset to a valid value via the status subresource", func() {
			dpu := getMinimalDPU(testNs.Name)
			Expect(testClient.Create(ctx, dpu)).To(Succeed())

			dpu.Status.IdentityMode = ptr.To(provisioningv1.IdentityModeSpiffe)
			Expect(testClient.Status().Update(ctx, dpu)).To(Succeed())

			refetched := &provisioningv1.DPU{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), refetched)).To(Succeed())
			Expect(refetched.Status.IdentityMode).NotTo(BeNil())
			Expect(*refetched.Status.IdentityMode).To(Equal(provisioningv1.IdentityModeSpiffe))
		})

		It("rejects re-stamping a different value (immutable once set)", func() {
			dpu := getMinimalDPU(testNs.Name)
			Expect(testClient.Create(ctx, dpu)).To(Succeed())

			dpu.Status.IdentityMode = ptr.To(provisioningv1.IdentityModeSpiffe)
			Expect(testClient.Status().Update(ctx, dpu)).To(Succeed())

			refetched := &provisioningv1.DPU{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), refetched)).To(Succeed())
			refetched.Status.IdentityMode = ptr.To(provisioningv1.IdentityModeBootstrapToken)
			err := testClient.Status().Update(ctx, refetched)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("identityMode is immutable once set"))
		})

		It("rejects clearing identityMode after stamp (immutable once set)", func() {
			dpu := getMinimalDPU(testNs.Name)
			Expect(testClient.Create(ctx, dpu)).To(Succeed())

			dpu.Status.IdentityMode = ptr.To(provisioningv1.IdentityModeSpiffe)
			Expect(testClient.Status().Update(ctx, dpu)).To(Succeed())

			refetched := &provisioningv1.DPU{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), refetched)).To(Succeed())
			refetched.Status.IdentityMode = nil
			err := testClient.Status().Update(ctx, refetched)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("identityMode is immutable once set"))
		})

		It("rejects values outside the IdentityMode enum", func() {
			dpu := getMinimalDPU(testNs.Name)
			Expect(testClient.Create(ctx, dpu)).To(Succeed())

			dpu.Status.IdentityMode = ptr.To(provisioningv1.IdentityMode("bogus"))
			err := testClient.Status().Update(ctx, dpu)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Or(
				ContainSubstring("Unsupported value"),
				ContainSubstring("supported values"),
			))
		})
	})

	Context("When checking the DPU AgentStatus.Spiffe.LastProbeMessage field", func() {
		It("accepts a 256-char message via the status subresource", func() {
			dpu := getMinimalDPU(testNs.Name)
			Expect(testClient.Create(ctx, dpu)).To(Succeed())

			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				Spiffe: &provisioningv1.SpiffeStatus{
					LastProbeMessage: ptr.To(strings.Repeat("a", 256)),
				},
			}
			Expect(testClient.Status().Update(ctx, dpu)).To(Succeed())
		})

		It("rejects a message longer than 256 chars", func() {
			dpu := getMinimalDPU(testNs.Name)
			Expect(testClient.Create(ctx, dpu)).To(Succeed())

			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				Spiffe: &provisioningv1.SpiffeStatus{
					LastProbeMessage: ptr.To(strings.Repeat("a", 257)),
				},
			}
			err := testClient.Status().Update(ctx, dpu)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Or(
				ContainSubstring("maxLength"),
				ContainSubstring("Too long"),
				ContainSubstring("256"),
			))
		})
	})

	Context("When checking the DPUCluster API validations", func() {
		DescribeTable("Validates the DPUCluster name is a DNS-1035 label",
			func(name string, expectError bool) {
				cluster := &provisioningv1.DPUCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: testNs.Name,
					},
					Spec: provisioningv1.DPUClusterSpec{
						Type: string(provisioningv1.StaticCluster),
					},
				}
				err := testClient.Create(ctx, cluster)
				if expectError {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("DNS-1035"))
				} else {
					Expect(err).ToNot(HaveOccurred())
					DeferCleanup(testClient.Delete, ctx, cluster)
				}
			},
			Entry("name starting with a digit is rejected", "2604-hosted-hbn", true),
			Entry("name containing a dot is rejected", "hosted.cluster", true),
			Entry("valid DNS-1035 name is accepted", "hosted-2604-hbn", false),
		)

		// To simulate a cluster that already existed before the rule was introduced, we temporarily
		// remove the DNS-1035 validation from the CRD, create the non-compliant cluster, then restore
		// the rule before asserting that the pre-existing cluster can still be updated.
		It("allows updates to a pre-existing DPUCluster whose name is not a DNS-1035 label", func() {
			const crdName = "dpuclusters.provisioning.dpu.nvidia.com"
			const invalidName = "2604-existing-hbn"

			apiextScheme := runtime.NewScheme()
			Expect(apiextensionsv1.AddToScheme(apiextScheme)).To(Succeed())
			crdClient, err := client.New(cfg, client.Options{Scheme: apiextScheme})
			Expect(err).NotTo(HaveOccurred())

			var removedRule *apiextensionsv1.ValidationRule
			isDNS1035Rule := func(r apiextensionsv1.ValidationRule) bool {
				return strings.Contains(r.Rule, "matches(")
			}

			removeDNS1035Rule := func() {
				Eventually(func(g Gomega) {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					g.Expect(crdClient.Get(ctx, client.ObjectKey{Name: crdName}, crd)).To(Succeed())
					for i := range crd.Spec.Versions {
						schema := crd.Spec.Versions[i].Schema
						if schema == nil || schema.OpenAPIV3Schema == nil {
							continue
						}
						kept := make([]apiextensionsv1.ValidationRule, 0, len(schema.OpenAPIV3Schema.XValidations))
						for _, r := range schema.OpenAPIV3Schema.XValidations {
							if isDNS1035Rule(r) {
								rule := r
								removedRule = &rule
								continue
							}
							kept = append(kept, r)
						}
						schema.OpenAPIV3Schema.XValidations = kept
					}
					g.Expect(crdClient.Update(ctx, crd)).To(Succeed())
				}).Should(Succeed())
			}

			restoreDNS1035Rule := func() {
				if removedRule == nil {
					return
				}
				Eventually(func(g Gomega) {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					g.Expect(crdClient.Get(ctx, client.ObjectKey{Name: crdName}, crd)).To(Succeed())
					for i := range crd.Spec.Versions {
						schema := crd.Spec.Versions[i].Schema
						if schema == nil || schema.OpenAPIV3Schema == nil {
							continue
						}
						alreadyPresent := false
						for _, r := range schema.OpenAPIV3Schema.XValidations {
							if isDNS1035Rule(r) {
								alreadyPresent = true
								break
							}
						}
						if !alreadyPresent {
							schema.OpenAPIV3Schema.XValidations = append(schema.OpenAPIV3Schema.XValidations, *removedRule)
						}
					}
					g.Expect(crdClient.Update(ctx, crd)).To(Succeed())
				}).Should(Succeed())
			}

			// Ensure the rule is restored no matter how the test exits.
			DeferCleanup(restoreDNS1035Rule)

			By("relaxing the CRD so a non-compliant cluster can be created")
			removeDNS1035Rule()
			// CEL rule changes are compiled asynchronously, so poll until the create is accepted.
			Eventually(func(g Gomega) {
				g.Expect(testClient.Create(ctx, &provisioningv1.DPUCluster{
					ObjectMeta: metav1.ObjectMeta{Name: invalidName, Namespace: testNs.Name},
					Spec:       provisioningv1.DPUClusterSpec{Type: string(provisioningv1.StaticCluster)},
				})).To(Succeed())
			}).Should(Succeed())
			DeferCleanup(testClient.Delete, ctx, &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{Name: invalidName, Namespace: testNs.Name},
			})

			By("restoring the DNS-1035 rule and confirming it is active again")
			restoreDNS1035Rule()
			Eventually(func(g Gomega) {
				err := testClient.Create(ctx, &provisioningv1.DPUCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "2605-another-hbn", Namespace: testNs.Name},
					Spec:       provisioningv1.DPUClusterSpec{Type: string(provisioningv1.StaticCluster)},
				})
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring("DNS-1035"))
			}).Should(Succeed())

			By("verifying the pre-existing non-compliant cluster can still be updated")
			existing := &provisioningv1.DPUCluster{}
			Expect(testClient.Get(ctx, client.ObjectKey{Name: invalidName, Namespace: testNs.Name}, existing)).To(Succeed())
			existing.Labels = map[string]string{"example.com/updated": "true"}
			Expect(testClient.Update(ctx, existing)).To(Succeed())

			existing.Status.Phase = provisioningv1.PhaseReady
			Expect(testClient.Status().Update(ctx, existing)).To(Succeed())
		})
	})
})

func getMinimalDPU(namespace string) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dpu-test-",
			Namespace:    namespace,
		},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName:   "node-1",
			DPUDeviceName: "device-1",
			BFB:           ptr.To("somebfb"),
			SerialNumber:  "MT25066004C7",
			DPUFlavor:     "someflavor",
			NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
		},
	}
}

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

func getMinimalDPUFlavorTemplate(namespace string) *provisioningv1.DPUFlavorTemplate {
	return &provisioningv1.DPUFlavorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dpuflavortemplate-test-",
			Namespace:    namespace,
		},
		Spec: provisioningv1.DPUFlavorTemplateSpec{
			Template: "grub:\n  kernelParameters: []\n",
		},
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
					DPUFlavor:  ptr.To("someflavor"),
					NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				},
			},
		},
	}
}
