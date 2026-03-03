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

package reboot

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("Reboot", func() {
	Context("HandleReboot", func() {
		It("should reboot the host if DPU ARM has not been booted", func() {
			dpu := &provisioningv1.DPU{}
			bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
			Expect(err).NotTo(HaveOccurred())
			bootIDStr := strings.TrimSpace(string(bootID))
			optCtx := &operations.Context{
				LatestDPU:                dpu,
				UpdateStatusUntilSuccess: func(context.Context) {}, // no-op for unit test
			}
			reboot := &HandleReboot{
				skipBlock: true,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Expect(cmd).To(Equal(fmt.Sprintf("sleep %d && shutdown -h now", shutdownDelayInSeconds)))
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
			Expect(optCtx.Status.InitialBootID).NotTo(BeNil())
			Expect(*optCtx.Status.InitialBootID).To(Equal(bootIDStr))
		})

		It("should not reboot the host if DPU ARM has already been booted", func() {
			bootID, err := os.ReadFile(bootIDFile)
			Expect(err).NotTo(HaveOccurred())
			currentBootIDStr := strings.TrimSpace(string(bootID))
			aDifferentBootID := currentBootIDStr + "-other"

			dpu := &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					AgentStatus: &provisioningv1.AgentStatus{
						InitialBootID: ptr.To(aDifferentBootID),
					},
				},
			}

			optCtx := &operations.Context{LatestDPU: dpu}
			reboot := &HandleReboot{}
			Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
			Expect(optCtx.Status.InitialBootID).To(BeNil())
		})
	})

	Context("getRebootMethod", func() {
		// getRebootMethod is currently hardcoded to support only SystemLevelReset (legacy flow) and NoAction.
		// It must not return PowerCycle, SystemReboot, or FirmwareReset.
		It("returns only RebootMethodSystemLevelReset or RebootMethodNoAction", func() {
			currentBootID, err := os.ReadFile(bootIDFile)
			Expect(err).NotTo(HaveOccurred())
			currentBootIDStr := strings.TrimSpace(string(currentBootID))
			differentBootID := currentBootIDStr + "x"

			allowedMethods := map[provisioningv1.RebootMethodType]bool{
				provisioningv1.RebootMethodSystemLevelReset: true,
				provisioningv1.RebootMethodNoAction:         true,
			}

			h := &HandleReboot{}

			// When host has not been booted (no InitialBootID or same as current): expect SystemLevelReset.
			optCtxNotBooted := &operations.Context{LatestDPU: &provisioningv1.DPU{}}
			m, err := h.getRebootMethod(optCtxNotBooted)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(allowedMethods[*m]).To(BeTrue(), "getRebootMethod must only return SystemLevelReset or NoAction, got %s", *m)
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))

			// When host has already been booted (different InitialBootID): expect NoAction.
			optCtxBooted := &operations.Context{
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						AgentStatus: &provisioningv1.AgentStatus{
							InitialBootID: ptr.To(differentBootID),
						},
					},
				},
			}
			m, err = h.getRebootMethod(optCtxBooted)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(allowedMethods[*m]).To(BeTrue(), "getRebootMethod must only return SystemLevelReset or NoAction, got %s", *m)
			Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
		})

		// Sanity check (InitialBootID != currentRebootID → reboot already done) is for when the hasBeenBooted
		// logic above is replaced: it will error if the new logic wrongly returns non-NoAction after a reboot.
		// With current logic we return NoAction when stored != current (hasBeenBooted), so the check never fires.
		It("returns NoAction when stored InitialBootID != current boot ID (reboot already done)", func() {
			currentBootID, err := os.ReadFile(bootIDFile)
			Expect(err).NotTo(HaveOccurred())
			currentBootIDStr := strings.TrimSpace(string(currentBootID))
			storedBootIDDifferent := currentBootIDStr + "-old"
			optCtx := &operations.Context{
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						AgentStatus: &provisioningv1.AgentStatus{
							InitialBootID: ptr.To(storedBootIDDifferent),
						},
					},
				},
			}
			h := &HandleReboot{}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
		})
	})

	Context("execPowerCycle", func() {
		It("sets RebootMethod PowerCycle", func() {
			optCtx := &operations.Context{}
			h := &HandleReboot{}
			Expect(h.execPowerCycle(optCtx)).To(Succeed())
			Expect(optCtx.Status.RebootMethod).NotTo(BeNil())
			Expect(*optCtx.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
		})
	})

	Context("execSystemReboot", func() {
		It("sets RebootMethod SystemReboot", func() {
			optCtx := &operations.Context{}
			h := &HandleReboot{}
			Expect(h.execSystemReboot(optCtx)).To(Succeed())
			Expect(optCtx.Status.RebootMethod).NotTo(BeNil())
			Expect(*optCtx.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodSystemReboot))
		})
	})

	Context("execSystemLevelReset", func() {
		It("sets status and runs shutdown command", func() {
			optCtx := &operations.Context{
				UpdateStatusUntilSuccess: func(context.Context) {}, // no-op for unit test
			}
			var shutdownCmd string
			h := &HandleReboot{
				skipBlock: true,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					shutdownCmd = cmd
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			Expect(h.execSystemLevelReset(context.Background(), optCtx)).To(Succeed())
			Expect(optCtx.Status.RebootMethod).NotTo(BeNil())
			Expect(*optCtx.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
			// InitialBootID is set in Execute() for host-reboot methods, not in execSystemLevelReset.
			Expect(shutdownCmd).To(Equal(fmt.Sprintf("sleep %d && shutdown -h now", shutdownDelayInSeconds)))
		})
	})

	Context("execFirmwareReset", func() {
		It("sets status and runs mlxfwreset command", func() {
			dir, err := os.MkdirTemp("", "reboot-mst-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt4119_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())
			optCtx := &operations.Context{
				UpdateStatusUntilSuccess: func(context.Context) {}, // no-op for unit test
			}
			var fwResetCmd string
			h := &HandleReboot{
				skipBlock:      true,
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					fwResetCmd = cmd
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			Expect(h.execFirmwareReset(context.Background(), optCtx)).To(Succeed())
			Expect(optCtx.Status.RebootMethod).NotTo(BeNil())
			Expect(*optCtx.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodFirmwareReset))
			Expect(fwResetCmd).To(Equal(fmt.Sprintf("mlxfwreset -d %s -y reset", devicePath)))
		})

		It("fails when no MST devices found", func() {
			dir, err := os.MkdirTemp("", "reboot-mst-empty-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			optCtx := &operations.Context{}
			h := &HandleReboot{mstDevicesPath: dir}
			execErr := h.execFirmwareReset(context.Background(), optCtx)
			Expect(execErr).To(HaveOccurred())
			Expect(execErr.Error()).To(ContainSubstring("no MST devices found"))
		})
	})
})
