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
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("Reboot", func() {
	Context("CheckHostRebootRequired", func() {
		It("should reboot the host if DPU ARM has not been booted", func() {
			dpu := &provisioningv1.DPU{}
			bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
			Expect(err).NotTo(HaveOccurred())
			bootIDStr := strings.TrimSpace(string(bootID))
			optCtx := &operations.Context{
				LatestDPU: dpu,
			}
			reboot := &CheckHostRebootRequired{}
			Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
			Expect(optCtx.Status.HostRebootRequired).NotTo(BeNil())
			Expect(*optCtx.Status.HostRebootRequired).To(BeTrue())
			Expect(optCtx.Status.InitialBootID).NotTo(BeNil())
			Expect(*optCtx.Status.InitialBootID).To(Equal(bootIDStr))
		})

		It("should not reboot the host if DPU ARM has already been booted", func() {
			bootID, err := os.ReadFile(bootIDFile)
			Expect(err).NotTo(HaveOccurred())
			aDifferentBootID := string(bootID) + "1"

			dpu := &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					DPUInternalStatus: &provisioningv1.DPUInternalStatus{
						InitialBootID: ptr.To(aDifferentBootID),
					},
				},
			}

			optCtx := &operations.Context{
				LatestDPU: dpu,
				Status: provisioningv1.DPUInternalStatus{
					InitialBootID: ptr.To(aDifferentBootID),
				},
			}
			reboot := &CheckHostRebootRequired{}
			Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
			Expect(optCtx.Status.InitialBootID).NotTo(BeNil())
			Expect(*optCtx.Status.InitialBootID).To(Equal(aDifferentBootID))
		})
	})

	Context("ShutDownArm", func() {
		It("should shut down if DPU ARM has not been booted", func() {
			optCtx := &operations.Context{
				LatestDPU: &provisioningv1.DPU{},
			}
			reboot := &ShutDownArm{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Expect(cmd).To(Equal(fmt.Sprintf("sleep %d && shutdown -h now", shutdownDelayInSeconds)))
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
		})

		It("should not shut down if DPU ARM has already been booted", func() {
			bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
			Expect(err).NotTo(HaveOccurred())

			dpu := &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					DPUInternalStatus: &provisioningv1.DPUInternalStatus{
						InitialBootID: ptr.To(string(bootID)),
					},
				},
			}
			optCtx := &operations.Context{
				LatestDPU: dpu,
			}
			shutdownCalled := false
			reboot := &ShutDownArm{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					shutdownCalled = true
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
			Expect(shutdownCalled).To(BeFalse())
		})
	})
})
