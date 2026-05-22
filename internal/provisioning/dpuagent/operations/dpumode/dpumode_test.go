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

package dpumode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Ensure Mode", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "dpumode-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("setting DPU mode", func() {
		It("should never be skipped", func() {
			operation := &EnsureMode{}
			Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
		})

		It("should set BF3 DPU mode to zero-trust", func() {
			reg := fmt.Sprintf("mlxprivhost -d (%s|%s) r --disable_rshim --disable_tracer --disable_counter_rd --disable_port_owner",
				filepath.Join(tempDir, "dev/mst/dev1"), filepath.Join(tempDir, "dev/mst/dev2"))
			expectedCmd := regexp.MustCompile(reg)
			By(fmt.Sprintf("regex: %s", reg))
			operation := &EnsureMode{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					By(fmt.Sprintf("checking that the command is correct: %s", cmd))
					Expect(expectedCmd.MatchString(cmd)).To(BeTrue())
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			err := operation.Execute(context.Background(), &operations.Context{
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						DPUType:        provisioningv1.DPUTypeBlueField3,
						DeploymentMode: provisioningv1.DeploymentModeZeroTrust,
					},
				},
				DiscoverPorts: func() ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{MSTDevice: filepath.Join(tempDir, "dev/mst/dev1")},
						{MSTDevice: filepath.Join(tempDir, "dev/mst/dev2")},
					}, nil
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should set BF3 DPU mode to DPU", func() {
			reg := fmt.Sprintf("mlxprivhost -d (%s|%s) p",
				filepath.Join(tempDir, "dev/mst/dev1"), filepath.Join(tempDir, "dev/mst/dev2"))
			expectedCmd := regexp.MustCompile(reg)
			By(fmt.Sprintf("regex: %s", reg))
			operation := &EnsureMode{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					By(fmt.Sprintf("checking that the command is correct: %s", cmd))
					Expect(expectedCmd.MatchString(cmd)).To(BeTrue())
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			err := operation.Execute(context.Background(), &operations.Context{
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						DPUType:        provisioningv1.DPUTypeBlueField3,
						DeploymentMode: provisioningv1.DeploymentModeHostTrusted,
					},
				},
				DiscoverPorts: func() ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{MSTDevice: filepath.Join(tempDir, "dev/mst/dev1")},
						{MSTDevice: filepath.Join(tempDir, "dev/mst/dev2")},
					}, nil
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should skip mlxprivhost for BF4", func() {
			operation := &EnsureMode{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					if strings.Contains(cmd, "mlxprivhost") {
						Fail("mlxprivhost should not be called for BF4")
					}
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			err := operation.Execute(context.Background(), &operations.Context{
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						DPUType:        provisioningv1.DPUTypeBlueField4,
						DeploymentMode: provisioningv1.DeploymentModeZeroTrust,
					},
				},
				DiscoverPorts: func() ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{MSTDevice: filepath.Join(tempDir, "dev/mst/dev1")},
						{MSTDevice: filepath.Join(tempDir, "dev/mst/dev2")},
					}, nil
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
