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
	"context"
	"fmt"
	"os"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"github.com/Masterminds/semver/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testPci0         = "0000:03:00.0"
	testPci1         = "0000:03:00.1"
	mftVersionLegacy = "mlxconfig, mft 4.35.0"
	// devlink JSON key; British spelling required by API
	devlinkKeyFlavour = "flavour" //nolint:misspell
)

const devlinkFlavourPlaceholder = "{{F}}" // placeholder replaced with devlinkKeyFlavour (devlink API key)

var devlinkPortShowTemplate = `{
	"port": {
		"pci/0000:03:00.0/262143": {"type": "eth", "netdev": "p0", "{{F}}": "physical", "port": 0, "splittable": false},
		"pci/0000:03:00.0/196608": {"type": "eth", "netdev": "pf0hpf", "{{F}}": "pcipf", "controller": 1, "pfnum": 0, "external": true, "splittable": false, "function": {"hw_addr": "a0:88:c2:f2:30:3c"}},
		"pci/0000:03:00.0/229408": {"type": "eth", "netdev": "en3f0pf0sf0", "{{F}}": "pcisf", "controller": 0, "pfnum": 0, "sfnum": 0, "splittable": false, "function": {"hw_addr": "02:e0:a9:05:72:9a", "state": "active", "opstate": "attached"}},
		"pci/0000:03:00.1/327679": {"type": "eth", "netdev": "p1", "{{F}}": "physical", "port": 1, "splittable": false},
		"pci/0000:03:00.1/262144": {"type": "eth", "netdev": "pf1hpf", "{{F}}": "pcipf", "controller": 1, "pfnum": 1, "external": true, "splittable": false, "function": {"hw_addr": "a0:88:c2:f2:30:3d"}},
		"pci/0000:03:00.1/294944": {"type": "eth", "netdev": "en3f1pf1sf0", "{{F}}": "pcisf", "controller": 0, "pfnum": 1, "sfnum": 0, "splittable": false, "function": {"hw_addr": "02:9c:ed:fa:d2:78", "state": "active", "opstate": "attached"}},
		"auxiliary/mlx5_core.sf.2/10813440": {"type": "eth", "netdev": "enp3s0f0s0", "{{F}}": "virtual", "splittable": false},
		"auxiliary/mlx5_core.sf.3/12910592": {"type": "eth", "netdev": "enp3s0f1s0", "{{F}}": "virtual", "splittable": false}
	}
}`

// devlinkPortShowRealistic is sample "devlink port show -j" output with physical (p0, p1), pcipf, pcisf, and virtual ports.
var devlinkPortShowRealistic = strings.ReplaceAll(devlinkPortShowTemplate, devlinkFlavourPlaceholder, devlinkKeyFlavour)

// Dummy getUplinkName for tests that call pciToNetdevMap or Execute (returns p0/p1).
var getUplinkNameForTest = func(pci string) (string, error) {
	switch pci {
	case testPci0:
		return "p0", nil
	case testPci1:
		return "p1", nil
	default:
		return "", fmt.Errorf("unknown pci %s", pci)
	}
}

var _ = Describe("NVConfig Operation", func() {
	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "nvconfig-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		_ = os.RemoveAll(tempDir)
	})

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

		It("when NVConfig is empty should run reset on all devices (new flow)", func() {
			pci0, pci1 := testPci0, testPci1
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					recorded = append(recorded, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				getMlxconfigVersion: func() (string, error) { return "mlxconfig, mft 4.35.1", nil },
				getDevlinkPort:      func() (string, error) { return devlinkPortShowRealistic, nil },
				getUplinkName:       getUplinkNameForTest,
			}
			operationCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{NVConfig: []provisioningv1.NVConfig{}}},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{DPUInternalStatus: &provisioningv1.DPUInternalStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(ConsistOf(
				fmt.Sprintf("mlxconfig -d %s -y reset", pci0),
				fmt.Sprintf("mlxconfig -d %s -y reset", pci1),
			))
		})

		It("when NVConfig has single entry with device '*' should run set --with_default with same params on all netdevs (new flow)", func() {
			pci0, pci1 := testPci0, testPci1
			params := "PARAM1=VALUE1 PARAM2=VALUE2"
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					recorded = append(recorded, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				getMlxconfigVersion: func() (string, error) { return "mlxconfig, mft 4.35.1", nil },
				getDevlinkPort:      func() (string, error) { return devlinkPortShowRealistic, nil },
				getUplinkName:       getUplinkNameForTest,
			}
			operationCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						NVConfig: []provisioningv1.NVConfig{
							{Device: ptr.To("*"), Parameters: strings.Split(params, " ")},
						},
					},
				},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{DPUInternalStatus: &provisioningv1.DPUInternalStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(ConsistOf(
				fmt.Sprintf("mlxconfig -d %s -y --with_default set %s", pci0, params),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set %s", pci1, params),
			))
		})

		It("when NVConfig is empty and MFT is legacy should run reset only on all devices", func() {
			pci0, pci1 := testPci0, testPci1
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					recorded = append(recorded, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				getMlxconfigVersion: func() (string, error) { return mftVersionLegacy, nil },
				getDevlinkPort:      func() (string, error) { return devlinkPortShowRealistic, nil },
				getUplinkName:       getUplinkNameForTest,
			}
			operationCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{NVConfig: []provisioningv1.NVConfig{}}},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{DPUInternalStatus: &provisioningv1.DPUInternalStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(ConsistOf(
				fmt.Sprintf("mlxconfig -d %s -y reset", pci0),
				fmt.Sprintf("mlxconfig -d %s -y reset", pci1),
			))
		})

		It("when MFT version is below min uses legacy flow (set without --with_default)", func() {
			pci0, pci1 := testPci0, testPci1
			params := "PARAM1=VALUE1 PARAM2=VALUE2"
			var recorded []string
			operation := ConfigureNVConfig{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					recorded = append(recorded, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				getMlxconfigVersion: func() (string, error) { return mftVersionLegacy, nil },
				getDevlinkPort:      func() (string, error) { return devlinkPortShowRealistic, nil },
				getUplinkName:       getUplinkNameForTest,
			}
			operationCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						NVConfig: []provisioningv1.NVConfig{
							{Device: ptr.To("*"), Parameters: strings.Split(params, " ")},
						},
					},
				},
				LatestDPU: &provisioningv1.DPU{Status: provisioningv1.DPUStatus{DPUInternalStatus: &provisioningv1.DPUInternalStatus{Conditions: []metav1.Condition{}}}},
			}
			Expect(operation.Execute(ctx, operationCtx)).To(Succeed())
			Expect(recorded).To(ConsistOf(
				fmt.Sprintf("mlxconfig -d %s -y reset", pci0),
				fmt.Sprintf("mlxconfig -d %s -y set %s", pci0, params),
				fmt.Sprintf("mlxconfig -d %s -y reset", pci1),
				fmt.Sprintf("mlxconfig -d %s -y set %s", pci1, params),
			))
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
				DPUFlavor: dpuFlavor,
			}
			operationCtx.LatestDPU = &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					DPUInternalStatus: &provisioningv1.DPUInternalStatus{
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

		It("should succeed", func() {
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

			// MFT >= minMftVersion: set --with_default only (no reset). p0 uses PARAM5/PARAM6, p1 uses PARAM7/PARAM8.
			expectedCommands := []string{
				fmt.Sprintf("mlxconfig -d %s -y --with_default set PARAM5=VALUE5 PARAM6=VALUE6", pci0),
				fmt.Sprintf("mlxconfig -d %s -y --with_default set PARAM7=VALUE7 PARAM8=VALUE8", pci1),
			}
			var recorded []string
			runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				recorded = append(recorded, cmd)
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}
			operation := ConfigureNVConfig{
				runBash: runBash,
				getMlxconfigVersion: func() (string, error) {
					return "mlxconfig, mft 4.36.1, built on Feb 12 2026", nil
				},
				getDevlinkPort: func() (string, error) { return devlinkPortShowRealistic, nil },
				getUplinkName:  getUplinkNameForTest,
			}
			operationCtx := &operations.Context{
				DPUFlavor: dpuFlavor,
				Client: &mockClient{
					updateStatusFunc: func(execCtx context.Context, status provisioningv1.DPUInternalStatus) error {
						return nil
					},
					healthCheckFunc: func() error {
						return nil
					},
				},
			}
			operationCtx.LatestDPU = &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					DPUInternalStatus: &provisioningv1.DPUInternalStatus{
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
			Expect(recorded).To(ConsistOf(expectedCommands))
		})

		It("should succeed (legacy flow)", func() {
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

			// MFT < minMftVersion: reset then set (no --with_default). p0 uses PARAM5/PARAM6, p1 uses PARAM7/PARAM8.
			expectedCommands := []string{
				fmt.Sprintf("mlxconfig -d %s -y reset", pci0),
				fmt.Sprintf("mlxconfig -d %s -y set PARAM5=VALUE5 PARAM6=VALUE6", pci0),
				fmt.Sprintf("mlxconfig -d %s -y reset", pci1),
				fmt.Sprintf("mlxconfig -d %s -y set PARAM7=VALUE7 PARAM8=VALUE8", pci1),
			}
			var recorded []string
			runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				recorded = append(recorded, cmd)
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}
			operation := ConfigureNVConfig{
				runBash: runBash,
				getMlxconfigVersion: func() (string, error) {
					return mftVersionLegacy, nil
				},
				getDevlinkPort: func() (string, error) { return devlinkPortShowRealistic, nil },
				getUplinkName:  getUplinkNameForTest,
			}
			operationCtx := &operations.Context{
				DPUFlavor: dpuFlavor,
				Client: &mockClient{
					updateStatusFunc: func(execCtx context.Context, status provisioningv1.DPUInternalStatus) error {
						return nil
					},
					healthCheckFunc: func() error {
						return nil
					},
				},
			}
			operationCtx.LatestDPU = &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					DPUInternalStatus: &provisioningv1.DPUInternalStatus{
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
			Expect(recorded).To(ConsistOf(expectedCommands))
		})
	})

	Context("Mlxconfig Version", func() {
		It("should extract version from output", func() {
			testCases := []struct {
				output          string
				expectedVersion *semver.Version
			}{
				{
					output:          "mlxconfig, mft 4.30.1-8, built on Nov 28 2024",
					expectedVersion: semver.MustParse("4.30.1-8"),
				},
				{
					output:          "mlxconfig 4.29.0",
					expectedVersion: semver.MustParse("4.29.0"),
				},
			}
			for _, tc := range testCases {
				operation := ConfigureNVConfig{
					getMlxconfigVersion: func() (string, error) {
						return tc.output, nil
					},
				}
				version, err := operation.mlxconfigVersion()
				Expect(err).NotTo(HaveOccurred())
				Expect(version.Equal(tc.expectedVersion)).To(BeTrue(), "output %q => got %s, expected %s", tc.output, version.String(), tc.expectedVersion.String())
			}
		})

		It("should fail when version cannot be extracted", func() {
			operation := ConfigureNVConfig{
				getMlxconfigVersion: func() (string, error) {
					return "no version here", nil
				},
			}
			_, err := operation.mlxconfigVersion()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to extract version"))
		})

		It("should fail when version is in short form (e.g. 4.30)", func() {
			operation := ConfigureNVConfig{
				getMlxconfigVersion: func() (string, error) {
					return "mlxconfig 4.30", nil
				},
			}
			_, err := operation.mlxconfigVersion()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to extract version"))
		})
	})

	Context("pciToNetdevMap", func() {
		It("should parse devlink JSON and return PCI -> netdev map", func() {
			operation := ConfigureNVConfig{
				getDevlinkPort: func() (string, error) { return devlinkPortShowRealistic, nil },
				getUplinkName:  getUplinkNameForTest,
			}
			m, err := operation.pciToNetdevMap()
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(HaveLen(2))
			Expect(m[testPci0]).To(Equal("p0"))
			Expect(m[testPci1]).To(Equal("p1"))
		})

		It("should include all pci/ keys and skip already included PCI", func() {
			// All pci/... entries are included; second PCI is deduped if key repeats.
			devlinkJSON := strings.ReplaceAll(`{
				"port": {
					"pci/0000:03:00.0/0": {"type": "eth", "netdev": "p0", "{{F}}": "physical"},
					"pci/0000:03:00.1/1": {"type": "eth", "netdev": "", "{{F}}": "virtual"}
				}
			}`, devlinkFlavourPlaceholder, devlinkKeyFlavour)
			operation := ConfigureNVConfig{
				getDevlinkPort: func() (string, error) { return devlinkJSON, nil },
				getUplinkName:  getUplinkNameForTest,
			}
			m, err := operation.pciToNetdevMap()
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(HaveLen(2))
			Expect(m[testPci0]).To(Equal("p0"))
			Expect(m[testPci1]).To(Equal("p1"))
		})

		It("should fail when getDevlinkPort returns an error", func() {
			operation := ConfigureNVConfig{
				getDevlinkPort: func() (string, error) { return "", os.ErrNotExist },
				getUplinkName:  getUplinkNameForTest,
			}
			_, err := operation.pciToNetdevMap()
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(os.ErrNotExist))
		})

		It("should fail on invalid JSON", func() {
			operation := ConfigureNVConfig{
				getDevlinkPort: func() (string, error) { return "not json", nil },
				getUplinkName:  getUplinkNameForTest,
			}
			_, err := operation.pciToNetdevMap()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("devlink port show: parse JSON"))
		})

		It("should fail when port object is missing", func() {
			operation := ConfigureNVConfig{
				getDevlinkPort: func() (string, error) { return "{}", nil },
				getUplinkName:  getUplinkNameForTest,
			}
			_, err := operation.pciToNetdevMap()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing \"port\" object"))
		})

		It("should include each PCI once when devlink has multiple entries per PCI", func() {
			// Same PCI in two keys; skip already included and get one PCI.
			dupPCIJSON := strings.ReplaceAll(`{
				"port": {
					"pci/0000:03:00.0/0": {"type":"eth", "netdev":"p0", "{{F}}":"physical"},
					"pci/0000:03:00.0/1": {"type":"eth", "netdev":"p1", "{{F}}":"physical"}
				}
			}`, devlinkFlavourPlaceholder, devlinkKeyFlavour)
			operation := ConfigureNVConfig{
				getDevlinkPort: func() (string, error) { return dupPCIJSON, nil },
				getUplinkName:  getUplinkNameForTest,
			}
			m, err := operation.pciToNetdevMap()
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(HaveLen(1))
			Expect(m[testPci0]).To(Equal("p0"))
		})
	})
})

type mockClient struct {
	getObjectFunc    func(execCtx context.Context, namespace, name string, obj client.Object) error
	updateStatusFunc func(execCtx context.Context, status provisioningv1.DPUInternalStatus) error
	healthCheckFunc  func() error
}

func (m *mockClient) GetObject(execCtx context.Context, namespace, name string, obj client.Object) error {
	return m.getObjectFunc(execCtx, namespace, name, obj)
}

func (m *mockClient) UpdateStatus(execCtx context.Context, status provisioningv1.DPUInternalStatus) error {
	return m.updateStatusFunc(execCtx, status)
}

func (m *mockClient) HealthCheck() error {
	return m.healthCheckFunc()
}
