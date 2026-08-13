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
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	opts "github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Ensure Mode", func() {
	Context("setting DPU mode", func() {
		It("should not be skipped when SkipHWProvisioning is false", func() {
			operation := &EnsureMode{}
			Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
		})

		It("should be skipped when SkipDPUMode is true", func() {
			operation := &EnsureMode{}
			Expect(operation.ShouldSkip(&operations.Context{Options: opts.Options{SkipDPUMode: true}})).To(BeTrue())
		})

		It("should set BF3 DPU mode to zero-trust once per ASIC on PF0", func() {
			var commands []string
			operation := &EnsureMode{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
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
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{PCIAddress: "0000:03:00.0"},
						{PCIAddress: "0000:03:00.1"},
					}, nil
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(commands).To(Equal([]string{
				"mlxprivhost -d 0000:03:00.0 r --disable_rshim --disable_tracer --disable_counter_rd --disable_port_owner",
			}))
		})

		It("should set BF3 DPU mode to DPU once per ASIC on PF0", func() {
			var commands []string
			operation := &EnsureMode{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
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
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{PCIAddress: "0000:03:00.0"},
						{PCIAddress: "0000:03:00.1"},
					}, nil
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(commands).To(Equal([]string{
				"mlxprivhost -d 0000:03:00.0 p",
			}))
		})

		It("should apply mlxprivhost once per ASIC when multiple devices are present", func() {
			var commands []string
			operation := &EnsureMode{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
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
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{PCIAddress: "0000:03:00.1"},
						{PCIAddress: "0000:03:00.0"},
						{PCIAddress: "0002:01:00.1"},
						{PCIAddress: "0002:01:00.0"},
					}, nil
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(commands).To(Equal([]string{
				"mlxprivhost -d 0000:03:00.0 r --disable_rshim --disable_tracer --disable_counter_rd --disable_port_owner",
				"mlxprivhost -d 0002:01:00.0 r --disable_rshim --disable_tracer --disable_counter_rd --disable_port_owner",
			}))
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
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{PCIAddress: "0000:03:00.0"},
						{PCIAddress: "0000:03:00.1"},
					}, nil
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
