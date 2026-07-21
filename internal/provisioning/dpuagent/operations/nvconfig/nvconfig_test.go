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

package nvconfig

import (
	"bytes"
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	testPci0 = "0000:03:00.0"
	testPci1 = "0000:03:00.1"
)

// discoverPortsForTest returns a DiscoverPorts function that yields the two
// physical NIC ports used throughout the nvconfig tests.
func discoverPortsForTest() func(pciutil.PortScope) ([]pciutil.NICPort, error) {
	return func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
		return []pciutil.NICPort{
			{Netdev: "p0", PCIAddress: testPci0},
			{Netdev: "p1", PCIAddress: testPci1},
		}, nil
	}
}

// queryOutputForParams builds mlxconfig q output exposing all desired params except hidden names.
func queryOutputForParams(params string, hidden ...string) string {
	hiddenSet := make(map[string]struct{}, len(hidden))
	for _, name := range hidden {
		hiddenSet[strings.ToUpper(name)] = struct{}{}
	}
	entries := parseParamEntries(params)
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, ok := hiddenSet[entry.name]; ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s False(0)", entry.name))
	}
	return strings.Join(lines, "\n") + "\n"
}

func runBashWithQuery(recorded *[]string, queryOut string) func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
	return func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
		*recorded = append(*recorded, cmd)
		if strings.Contains(cmd, " q") {
			var stdout bytes.Buffer
			stdout.WriteString(queryOut)
			return stdout, bytes.Buffer{}, nil
		}
		return bytes.Buffer{}, bytes.Buffer{}, nil
	}
}

var _ = Describe("NVConfig Operation", func() {
	Context("Set NVConfig", func() {
		It("should not skip when NVConfig is empty (Execute will run reset-only)", func() {
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{},
				},
			}
			operation := ConfigureNVConfig{runBash: func(string) (bytes.Buffer, bytes.Buffer, error) { return bytes.Buffer{}, bytes.Buffer{}, nil }}
			Expect(operation.ShouldSkip(&operations.Context{DPUFlavor: dpuFlavor})).To(BeFalse())
		})

		It("when NVConfig is empty and RebootMethodDiscovery is true should run no-op --with_default set on all devices", func() {
			pci0, pci1 := testPci0, testPci1
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					recorded = append(recorded, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			operationCtx := &operations.Context{
				DiscoverPorts:         discoverPortsForTest(),
				DPUFlavor:             provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{NVConfig: []provisioningv1.NVConfig{}}},
				RebootMethodDiscovery: true,
				LatestDPU:             &provisioningv1.DPU{Status: provisioningv1.DPUStatus{AgentStatus: &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(ConsistOf(
				fmt.Sprintf("mlxconfig -d %s -y --with_default set BOOT_DBG_LOG=0", pci0),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set BOOT_DBG_LOG=0", pci1),
			))
		})

		It("when NVConfig has single entry with device '*' and RebootMethodDiscovery is true should query then set visible params", func() {
			pci0, pci1 := testPci0, testPci1
			params := "PARAM1=VALUE1 PARAM2=VALUE2"
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: runBashWithQuery(&recorded, queryOutputForParams(params)),
			}
			operationCtx := &operations.Context{
				DiscoverPorts:         discoverPortsForTest(),
				RebootMethodDiscovery: true,
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						NVConfig: []provisioningv1.NVConfig{
							{Device: ptr.To("*"), Parameters: strings.Split(params, " ")},
						},
					},
				},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{AgentStatus: &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(ConsistOf(
				fmt.Sprintf("mlxconfig -d %s q", pci0),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set %s", pci0, params),
				fmt.Sprintf("mlxconfig -d %s q", pci1),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set %s", pci1, params),
			))
		})

		It("when NVConfig is empty and RebootMethodDiscovery is false should run mlxconfig reset on all devices", func() {
			pci0, pci1 := testPci0, testPci1
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					recorded = append(recorded, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			operationCtx := &operations.Context{
				DiscoverPorts:         discoverPortsForTest(),
				RebootMethodDiscovery: false,
				DPUFlavor:             provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{NVConfig: []provisioningv1.NVConfig{}}},
				LatestDPU:             &provisioningv1.DPU{Status: provisioningv1.DPUStatus{AgentStatus: &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(ConsistOf(
				fmt.Sprintf("mlxconfig -d %s -y reset", pci0),
				fmt.Sprintf("mlxconfig -d %s -y reset", pci1),
			))
		})

		It("when RebootMethodDiscovery is false should reset then set full flavor params", func() {
			pci0, pci1 := testPci0, testPci1
			params := "PARAM1=VALUE1 PARAM2=VALUE2"
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					recorded = append(recorded, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			operationCtx := &operations.Context{
				DiscoverPorts:         discoverPortsForTest(),
				RebootMethodDiscovery: false,
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						NVConfig: []provisioningv1.NVConfig{
							{Device: ptr.To("*"), Parameters: strings.Split(params, " ")},
						},
					},
				},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{AgentStatus: &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(Equal([]string{
				fmt.Sprintf("mlxconfig -d %s -y reset", pci0),
				fmt.Sprintf("mlxconfig -d %s -y reset", pci1),
				fmt.Sprintf("mlxconfig -d %s -y set %s", pci0, params),
				fmt.Sprintf("mlxconfig -d %s -y set %s", pci1, params),
			}))
		})

		It("should skip if NVConfig is already configured", func() {
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PARAM1=VALUE1", "PARAM2=VALUE2"},
						},
					},
				},
			}
			runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				Fail("should not run any bash commands")
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}
			operation := ConfigureNVConfig{
				runBash: runBash,
			}
			operationCtx := &operations.Context{
				DiscoverPorts: discoverPortsForTest(),
				DPUFlavor:     dpuFlavor,
			}
			operationCtx.LatestDPU = &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					AgentStatus: &provisioningv1.AgentStatus{
						Conditions: []metav1.Condition{
							{
								Type:   CondNVConfigApplied,
								Status: metav1.ConditionTrue,
								Reason: CondNVConfigApplied,
							},
						},
					},
				},
			}
			Expect(operation.ShouldSkip(operationCtx)).To(BeTrue())
		})

		It("should not skip when NVConfig is already configured if RebootMethodDiscovery is true", func() {
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PARAM1=VALUE1", "PARAM2=VALUE2"},
						},
					},
				},
			}
			operation := ConfigureNVConfig{}
			operationCtx := &operations.Context{
				DiscoverPorts:         discoverPortsForTest(),
				DPUFlavor:             dpuFlavor,
				RebootMethodDiscovery: true,
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						AgentStatus: &provisioningv1.AgentStatus{
							Conditions: []metav1.Condition{
								{
									Type:   CondNVConfigApplied,
									Status: metav1.ConditionTrue,
									Reason: CondNVConfigApplied,
								},
							},
						},
					},
				},
			}
			Expect(operation.ShouldSkip(operationCtx)).To(BeFalse())
		})

		It("PreInstall should skip when pre-install NVConfigApplied is True and RebootMethodDiscovery is false", func() {
			reportedAt := metav1.Now()
			operationCtx := &operations.Context{
				RebootMethodDiscovery: false,
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						Phase: provisioningv1.DPUConfigFWParameters,
						AgentStatus: &provisioningv1.AgentStatus{
							PreInstall: &provisioningv1.AgentPreInstallStatus{
								AgentReported: &reportedAt,
								Conditions: []metav1.Condition{
									{
										Type:   provisioningv1.DPUAgentConditionNVConfigApplied,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			}
			Expect(ShouldConfigureNVConfig(operationCtx)).To(BeFalse())
		})

		It("PreInstall should skip when pre-install NVConfigApplied is True and RebootMethodDiscovery is true", func() {
			reportedAt := metav1.Now()
			operationCtx := &operations.Context{
				RebootMethodDiscovery: true,
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						Phase: provisioningv1.DPUConfigFWParameters,
						AgentStatus: &provisioningv1.AgentStatus{
							PreInstall: &provisioningv1.AgentPreInstallStatus{
								AgentReported: &reportedAt,
								Conditions: []metav1.Condition{
									{
										Type:   provisioningv1.DPUAgentConditionNVConfigApplied,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			}
			Expect(ShouldConfigureNVConfig(operationCtx)).To(BeFalse())
		})

		It("PreInstall should skip when pre-install NVConfigApplied is False", func() {
			reportedAt := metav1.Now()
			operationCtx := &operations.Context{
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						Phase: provisioningv1.DPUConfigFWParameters,
						AgentStatus: &provisioningv1.AgentStatus{
							PreInstall: &provisioningv1.AgentPreInstallStatus{
								AgentReported: &reportedAt,
								Conditions: []metav1.Condition{
									{
										Type:   provisioningv1.DPUAgentConditionNVConfigApplied,
										Status: metav1.ConditionFalse,
									},
								},
							},
						},
					},
				},
			}
			Expect(ShouldConfigureNVConfig(operationCtx)).To(BeFalse())
		})

		It("PreInstall should execute when AgentStatus is missing and in-memory reported is absent", func() {
			operationCtx := &operations.Context{
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						Phase: provisioningv1.DPUConfigFWParameters,
					},
				},
			}
			Expect(ShouldConfigureNVConfig(operationCtx)).To(BeTrue())
		})

		It("PreInstall should execute when AgentStatus is missing but in-memory reported exists", func() {
			reportedAt := metav1.Now()
			operationCtx := &operations.Context{
				Status: provisioningv1.AgentStatus{
					PreInstall: &provisioningv1.AgentPreInstallStatus{
						AgentReported: &reportedAt,
					},
				},
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						Phase: provisioningv1.DPUConfigFWParameters,
					},
				},
			}
			Expect(ShouldConfigureNVConfig(operationCtx)).To(BeTrue())
		})

		It("should succeed with RebootMethodDiscovery true (--with_default per port)", func() {
			pci0, pci1 := testPci0, testPci1

			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Device:     ptr.To("*"),
							Parameters: []string{"PARAM1=VALUE1", "PARAM2=VALUE2"},
						},
						{
							Parameters: []string{"PARAM3=VALUE3", "PARAM4=VALUE4"},
						},
						{
							Device:     ptr.To("p0"),
							Parameters: []string{"PARAM5=VALUE5", "PARAM6=VALUE6"},
						},
						{
							Device:     ptr.To("p1"),
							Parameters: []string{"PARAM7=VALUE7", "PARAM8=VALUE8"},
						},
					},
				},
			}

			var recorded []string
			runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				recorded = append(recorded, cmd)
				if strings.Contains(cmd, " q") {
					var stdout bytes.Buffer
					switch {
					case strings.Contains(cmd, pci0):
						stdout.WriteString(queryOutputForParams("PARAM5=VALUE5 PARAM6=VALUE6"))
					case strings.Contains(cmd, pci1):
						stdout.WriteString(queryOutputForParams("PARAM7=VALUE7 PARAM8=VALUE8"))
					}
					return stdout, bytes.Buffer{}, nil
				}
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}
			operation := ConfigureNVConfig{
				runBash: runBash,
			}
			operationCtx := &operations.Context{
				DiscoverPorts:         discoverPortsForTest(),
				RebootMethodDiscovery: true,
				DPUFlavor:             dpuFlavor,
			}
			operationCtx.LatestDPU = &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					AgentStatus: &provisioningv1.AgentStatus{
						Conditions: []metav1.Condition{
							{
								Type:   CondNVConfigApplied,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			}

			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(ConsistOf(
				fmt.Sprintf("mlxconfig -d %s q", pci0),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set PARAM5=VALUE5 PARAM6=VALUE6", pci0),
				fmt.Sprintf("mlxconfig -d %s q", pci1),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set PARAM7=VALUE7 PARAM8=VALUE8", pci1),
			))
		})

		It("should succeed with RebootMethodDiscovery false (reset then set per port)", func() {
			pci0, pci1 := testPci0, testPci1

			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Device:     ptr.To("p0"),
							Parameters: []string{"PARAM5=VALUE5", "PARAM6=VALUE6"},
						},
						{
							Device:     ptr.To("p1"),
							Parameters: []string{"PARAM7=VALUE7", "PARAM8=VALUE8"},
						},
					},
				},
			}

			var recorded []string
			runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				recorded = append(recorded, cmd)
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}
			operation := ConfigureNVConfig{
				runBash: runBash,
			}
			operationCtx := &operations.Context{
				DiscoverPorts:         discoverPortsForTest(),
				RebootMethodDiscovery: false,
				DPUFlavor:             dpuFlavor,
			}
			operationCtx.LatestDPU = &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					AgentStatus: &provisioningv1.AgentStatus{
						Conditions: []metav1.Condition{
							{
								Type:   CondNVConfigApplied,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			}

			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(Equal([]string{
				fmt.Sprintf("mlxconfig -d %s -y reset", pci0),
				fmt.Sprintf("mlxconfig -d %s -y reset", pci1),
				fmt.Sprintf("mlxconfig -d %s -y set PARAM5=VALUE5 PARAM6=VALUE6", pci0),
				fmt.Sprintf("mlxconfig -d %s -y set PARAM7=VALUE7 PARAM8=VALUE8", pci1),
			}))
		})

		It("should defer params not exposed in mlxconfig q until after reboot", func() {
			pci0, pci1 := testPci0, testPci1
			params := "ADVANCED_PCI_SETTINGS=1 MAX_ACC_OUT_READ=44"
			queryOut := queryOutputForParams(params, "MAX_ACC_OUT_READ")
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: runBashWithQuery(&recorded, queryOut),
			}
			operationCtx := &operations.Context{
				DiscoverPorts:         discoverPortsForTest(),
				RebootMethodDiscovery: true,
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						NVConfig: []provisioningv1.NVConfig{
							{Device: ptr.To("*"), Parameters: strings.Split(params, " ")},
						},
					},
				},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{AgentStatus: &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(ConsistOf(
				fmt.Sprintf("mlxconfig -d %s q", pci0),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set ADVANCED_PCI_SETTINGS=1", pci0),
				fmt.Sprintf("mlxconfig -d %s q", pci1),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set ADVANCED_PCI_SETTINGS=1", pci1),
			))
		})

		It("should apply full list when all params are exposed in mlxconfig q", func() {
			pci0 := testPci0
			params := "ADVANCED_PCI_SETTINGS=1 MAX_ACC_OUT_READ=44"
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: runBashWithQuery(&recorded, queryOutputForParams(params)),
			}
			operationCtx := &operations.Context{
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{Netdev: "p0", PCIAddress: testPci0},
					}, nil
				},
				RebootMethodDiscovery: true,
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						NVConfig: []provisioningv1.NVConfig{
							{Device: ptr.To("*"), Parameters: strings.Split(params, " ")},
						},
					},
				},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{AgentStatus: &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(Equal([]string{
				fmt.Sprintf("mlxconfig -d %s q", pci0),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set %s", pci0, params),
			}))
		})

		It("should set visible params even when mlxconfig q already shows them", func() {
			pci0 := testPci0
			params := "ADVANCED_PCI_SETTINGS=1"
			queryOut := "ADVANCED_PCI_SETTINGS True(1)\n"
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: runBashWithQuery(&recorded, queryOut),
			}
			operationCtx := &operations.Context{
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPci0}}, nil
				},
				RebootMethodDiscovery: true,
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						NVConfig: []provisioningv1.NVConfig{
							{Device: ptr.To("*"), Parameters: strings.Split(params, " ")},
						},
					},
				},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{AgentStatus: &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(Equal([]string{
				fmt.Sprintf("mlxconfig -d %s q", pci0),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set %s", pci0, params),
			}))
		})

		It("should run no-op set when all flavor params are deferred by mlxconfig q filtering", func() {
			pci0 := testPci0
			params := "MAX_ACC_OUT_READ=44"
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: runBashWithQuery(&recorded, ""),
			}
			operationCtx := &operations.Context{
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPci0}}, nil
				},
				RebootMethodDiscovery: true,
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						NVConfig: []provisioningv1.NVConfig{
							{Device: ptr.To("*"), Parameters: strings.Split(params, " ")},
						},
					},
				},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{AgentStatus: &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(Equal([]string{
				fmt.Sprintf("mlxconfig -d %s q", pci0),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set BOOT_DBG_LOG=0", pci0),
			}))
		})

	})

	Context("mlxconfig q parsing and filtering", func() {
		It("parses mlxconfig q parameter names", func() {
			out := parseMlxconfigQuery("        ADVANCED_PCI_SETTINGS                           False(0)\nSRIOV_EN True(1)\n")
			Expect(out).To(HaveKey("ADVANCED_PCI_SETTINGS"))
			Expect(out).To(HaveKey("SRIOV_EN"))
			Expect(out).To(HaveLen(2))
		})

		It("filterParamsForSet defers hidden params and records CondMessage", func() {
			operation := ConfigureNVConfig{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var stdout bytes.Buffer
					stdout.WriteString("ADVANCED_PCI_SETTINGS False(0)\n")
					return stdout, bytes.Buffer{}, nil
				},
			}
			optCtx := &operations.Context{}
			resolved, err := operation.filterParamsForSet(optCtx, testPci0, "ADVANCED_PCI_SETTINGS=1 IBM_CAPI_EN=1")
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved).To(Equal("ADVANCED_PCI_SETTINGS=1"))
			Expect(optCtx.CondMessage).To(Equal(fmt.Sprintf(
				"device=%s deferred NVConfig params (not exposed by mlxconfig q on this pass): [IBM_CAPI_EN=1]",
				testPci0,
			)))
			Expect(optCtx.DeferredNVConfigParams).To(Equal([]operations.DeferredNVConfigParam{
				{Device: testPci0, Params: "IBM_CAPI_EN=1"},
			}))
		})

		It("sets CondMessage for deferred params only", func() {
			pci0 := testPci0
			params := "ADVANCED_PCI_SETTINGS=1 IBM_CAPI_EN=1"
			queryOut := "ADVANCED_PCI_SETTINGS False(0)\n"
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: runBashWithQuery(&recorded, queryOut),
			}
			operationCtx := &operations.Context{
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPci0}}, nil
				},
				RebootMethodDiscovery: true,
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						NVConfig: []provisioningv1.NVConfig{
							{Device: ptr.To("*"), Parameters: strings.Split(params, " ")},
						},
					},
				},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{AgentStatus: &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(operationCtx.CondMessage).To(Equal(fmt.Sprintf(
				"device=%s deferred NVConfig params (not exposed by mlxconfig q on this pass): [IBM_CAPI_EN=1]",
				pci0,
			)))
			Expect(operationCtx.DeferredNVConfigParams).To(Equal([]operations.DeferredNVConfigParam{
				{Device: pci0, Params: "IBM_CAPI_EN=1"},
			}))
		})

		It("sets CondMessage when all flavor params are deferred", func() {
			pci0 := testPci0
			params := "MAX_ACC_OUT_READ=44"
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: runBashWithQuery(&recorded, ""),
			}
			operationCtx := &operations.Context{
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPci0}}, nil
				},
				RebootMethodDiscovery: true,
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						NVConfig: []provisioningv1.NVConfig{
							{Device: ptr.To("*"), Parameters: strings.Split(params, " ")},
						},
					},
				},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{AgentStatus: &provisioningv1.AgentStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(operationCtx.CondMessage).To(Equal(fmt.Sprintf(
				"device=%s deferred NVConfig params (not exposed by mlxconfig q on this pass): [MAX_ACC_OUT_READ=44]",
				pci0,
			)))
		})
	})

	Context("pciToNetdevMap", func() {
		It("should return PCI -> netdev map from discovered ports", func() {
			operation := ConfigureNVConfig{}
			operationCtx := &operations.Context{
				DiscoverPorts: discoverPortsForTest(),
			}
			m, err := operation.pciToNetdevMap(operationCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(HaveLen(2))
			Expect(m[testPci0]).To(Equal("p0"))
			Expect(m[testPci1]).To(Equal("p1"))
		})

		It("should fail when DiscoverPorts returns an error", func() {
			operation := ConfigureNVConfig{}
			operationCtx := &operations.Context{
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return nil, fmt.Errorf("discovery failed")
				},
			}
			_, err := operation.pciToNetdevMap(operationCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("discovery failed"))
		})

		It("should return empty map when no ports discovered", func() {
			operation := ConfigureNVConfig{}
			operationCtx := &operations.Context{
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{}, nil
				},
			}
			m, err := operation.pciToNetdevMap(operationCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(BeEmpty())
		})

		It("should return single-port map when only one port is discovered", func() {
			operation := ConfigureNVConfig{}
			operationCtx := &operations.Context{
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{Netdev: "p0", PCIAddress: testPci0},
					}, nil
				},
			}
			m, err := operation.pciToNetdevMap(operationCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(HaveLen(1))
			Expect(m[testPci0]).To(Equal("p0"))
			Expect(m).NotTo(HaveKey(testPci1))
		})
	})
})
