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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"github.com/Masterminds/semver/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("Reboot", func() {
	Describe("RebootMethodDiscovery false (boot-ID based)", func() {
		Context("HandleReboot", func() {
			It("should reboot the host if DPU ARM has not been booted", func() {
				dpu := &provisioningv1.DPU{}
				bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
				Expect(err).NotTo(HaveOccurred())
				bootIDStr := strings.TrimSpace(string(bootID))
				optCtx := &operations.Context{
					LatestDPU:                dpu,
					RebootMethodDiscovery:    false,
					CurrentBootID:            bootIDStr,
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
				Expect(optCtx.Status.RebootSequenceCount).NotTo(BeNil())
				Expect(*optCtx.Status.RebootSequenceCount).To(Equal(int32(1)))
				cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
				Expect(cond).To(BeNil(), "legacy boot-ID path omits RebootMethodDiscovery condition")
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

				optCtx := &operations.Context{
					LatestDPU:             dpu,
					RebootMethodDiscovery: false,
					CurrentBootID:         currentBootIDStr,
				}
				reboot := &HandleReboot{}
				Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(optCtx.Status.InitialBootID).To(BeNil())
				Expect(optCtx.Status.RebootSequenceCount).NotTo(BeNil())
				Expect(*optCtx.Status.RebootSequenceCount).To(Equal(int32(0)))
				cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
				Expect(cond).To(BeNil(), "legacy boot-ID path omits RebootMethodDiscovery condition")
			})

			It("returns error when rebootSequenceCount limit is reached", func() {
				dpu := &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						AgentStatus: &provisioningv1.AgentStatus{
							RebootSequenceCount: ptr.To(int32(5)),
						},
					},
				}
				optCtx := &operations.Context{
					LatestDPU:             dpu,
					RebootMethodDiscovery: false,
					CurrentBootID:         "boot-id",
				}
				reboot := &HandleReboot{skipBlock: true}
				err := reboot.Execute(context.Background(), optCtx)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("rebootSequenceCount limit exceeded"))
			})

			It("increments RebootSequenceCount on each non-NoAction reboot", func() {
				dpu := &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						AgentStatus: &provisioningv1.AgentStatus{
							RebootSequenceCount: ptr.To(int32(2)),
						},
					},
				}
				optCtx := &operations.Context{
					LatestDPU:                dpu,
					RebootMethodDiscovery:    false,
					CurrentBootID:            "boot-id",
					UpdateStatusUntilSuccess: func(context.Context) {},
				}
				reboot := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(optCtx.Status.RebootSequenceCount).NotTo(BeNil())
				Expect(*optCtx.Status.RebootSequenceCount).To(Equal(int32(3)))
			})
		})

		Context("DPUWarmReboot when GrubConfigChanged", func() {
			It("should trigger DPUWarmReboot via boot-ID path when GrubConfigChanged is true", func() {
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

				var rebootCmd string
				statusPushed := false
				optCtx := &operations.Context{
					LatestDPU:                dpu,
					RebootMethodDiscovery:    false,
					CurrentBootID:            currentBootIDStr,
					GrubConfigChanged:        true,
					UpdateStatusUntilSuccess: func(context.Context) { statusPushed = true },
				}
				h := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						rebootCmd = cmd
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(rebootCmd).To(Equal(fmt.Sprintf("sleep %d && reboot", shutdownDelayInSeconds)))
				Expect(statusPushed).To(BeTrue())
				Expect(optCtx.Status.RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodDPUWarmReboot))
			})

			It("should trigger DPUWarmReboot via device-query path when GrubConfigChanged is true", func() {
				dir, err := os.MkdirTemp("", "reboot-grub-changed-")
				Expect(err).NotTo(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()
				devicePath := filepath.Join(dir, "mt4125_pciconf0")
				Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

				var rebootCmd string
				statusPushed := false
				optCtx := &operations.Context{
					RebootMethodDiscovery:    true,
					CurrentBootID:            "boot-id",
					GrubConfigChanged:        true,
					UpdateStatusUntilSuccess: func(context.Context) { statusPushed = true },
				}
				h := &HandleReboot{
					skipBlock:      true,
					mstDevicesPath: dir,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						if strings.Contains(cmd, "mlxfwreset") {
							var b bytes.Buffer
							_, _ = b.WriteString(`{"reset_needed":false}`)
							return b, bytes.Buffer{}, nil
						}
						rebootCmd = cmd
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(rebootCmd).To(Equal(fmt.Sprintf("sleep %d && reboot", shutdownDelayInSeconds)))
				Expect(statusPushed).To(BeTrue(), "status should be pushed before reboot")
				Expect(optCtx.Status.RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodDPUWarmReboot))
			})

		})

		Context("getRebootMethod", func() {
			It("returns RebootMethodSystemLevelReset or RebootMethodNoAction", func() {
				currentBootID, err := os.ReadFile(bootIDFile)
				Expect(err).NotTo(HaveOccurred())
				currentBootIDStr := strings.TrimSpace(string(currentBootID))
				differentBootID := currentBootIDStr + "x"

				allowedMethods := map[provisioningv1.RebootMethodType]bool{
					provisioningv1.RebootMethodSystemLevelReset: true,
					provisioningv1.RebootMethodNoAction:         true,
				}

				h := &HandleReboot{}

				optCtxNotBooted := &operations.Context{LatestDPU: &provisioningv1.DPU{}, RebootMethodDiscovery: false, CurrentBootID: currentBootIDStr}
				m, err := h.getRebootMethod(optCtxNotBooted)
				Expect(err).NotTo(HaveOccurred())
				Expect(m).NotTo(BeNil())
				Expect(allowedMethods[*m]).To(BeTrue(), "getRebootMethod got %s", *m)
				Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))

				optCtxBooted := &operations.Context{
					RebootMethodDiscovery: false,
					CurrentBootID:         currentBootIDStr,
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
				Expect(allowedMethods[*m]).To(BeTrue(), "getRebootMethod got %s", *m)
				Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
			})
		})
	})

	Describe("RebootMethodDiscovery true (Device Query based)", func() {
		It("getRebootMethod returns SystemLevelReset when reset_needed is true", func() {
			dir, err := os.MkdirTemp("", "reboot-mst-dq-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt4125_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			var ran string
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					ran = cmd
					var b bytes.Buffer
					_, _ = b.WriteString(`{"reset_needed":true}`)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ran).To(Equal(fmt.Sprintf("mlxfwreset -d %s s --json", devicePath)))
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
		})

		It("getRebootMethod returns SystemLevelReset when at least one device has reset_needed true", func() {
			dir, err := os.MkdirTemp("", "reboot-mst-dq-multi-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devA := filepath.Join(dir, "mt_a")
			devB := filepath.Join(dir, "mt_b")
			Expect(os.WriteFile(devA, nil, 0600)).To(Succeed())
			Expect(os.WriteFile(devB, nil, 0600)).To(Succeed())

			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					switch {
					case strings.Contains(cmd, "mt_a"):
						_, _ = b.WriteString(`{"reset_needed":false}`)
					case strings.Contains(cmd, "mt_b"):
						_, _ = b.WriteString(`{"reset_needed":true}`)
					default:
						Fail("unexpected cmd: " + cmd)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
		})

		It("getRebootMethod returns NoAction when reset_needed is false", func() {
			dir, err := os.MkdirTemp("", "reboot-mst-dq2-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt4125_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Expect(cmd).To(Equal(fmt.Sprintf("mlxfwreset -d %s s --json", devicePath)))
					var b bytes.Buffer
					_, _ = b.WriteString(`{"reset_needed":false}`)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodNoAction)))
			Expect(cond.Message).To(BeEmpty())
		})

		It("getRebootMethod returns NoAction when reset_needed is omitted from mlxfwreset JSON", func() {
			dir, err := os.MkdirTemp("", "reboot-mst-dq-omit-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt4125_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Expect(cmd).To(Equal(fmt.Sprintf("mlxfwreset -d %s s --json", devicePath)))
					var b bytes.Buffer
					// Omitted reset_needed unmarshals to *bool nil; must not be treated as reset required.
					_, _ = b.WriteString(`{"fw_version":"1.2.3"}`)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
		})

		Context("removeForeverPending workaround", func() {
			It("stores current pending NVConfig entries in agent status", func() {
				dir, err := os.MkdirTemp("", "reboot-mst-dq-pending-status-")
				Expect(err).NotTo(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()
				devicePath := filepath.Join(dir, "mt4125_pciconf0")
				Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())
				currentBootID, err := getCurrentRebootID()
				Expect(err).NotTo(HaveOccurred())

				mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "PARAM_A", "current": "0", "next_boot": "1"},
    {"name": "PARAM_B", "current": "x", "next_boot": "y"}
  ],
  "reasons": ["Pending NVCONFIG parameter change"]
}
`)
				optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: currentBootID}
				h := &HandleReboot{
					mstDevicesPath: dir,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}
				_, err = h.getRebootMethod(optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(optCtx.Status.LastObservedPendingNVConfig).To(Equal(&provisioningv1.PendingNVConfigState{
					BootID: currentBootID,
					Devices: []provisioningv1.PendingNVConfigDevice{
						{
							Device: devicePath,
							Entries: []provisioningv1.PendingNVConfigEntry{
								{Name: "PARAM_A", Current: "0", NextBoot: "1"},
								{Name: "PARAM_B", Current: "x", NextBoot: "y"},
							},
						},
					},
				}))
			})

			It("stores pending NVConfig state when reset_needed is false", func() {
				dir, err := os.MkdirTemp("", "reboot-mst-dq-pending-status-false-")
				Expect(err).NotTo(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()
				devicePath := filepath.Join(dir, "mt4125_pciconf0")
				Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

				mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": false,
  "pending_nvconfig_parameters": "N/A (No pending NVCONFIG parameters)",
  "reasons": []
}
`)
				optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
				h := &HandleReboot{
					mstDevicesPath: dir,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}
				_, err = h.getRebootMethod(optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(optCtx.Status.LastObservedPendingNVConfig).To(Equal(&provisioningv1.PendingNVConfigState{
					BootID: "boot-id",
					Devices: []provisioningv1.PendingNVConfigDevice{
						{
							Device:  devicePath,
							Entries: []provisioningv1.PendingNVConfigEntry{},
						},
					},
				}))
			})

			It("ignores repeated pending NVConfig entries after boot change when they are the only reason", func() {
				dir, err := os.MkdirTemp("", "reboot-mst-dq-ignore-stuck-")
				Expect(err).NotTo(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()
				devicePath := filepath.Join(dir, "mt4125_pciconf0")
				Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())
				currentBootID, err := getCurrentRebootID()
				Expect(err).NotTo(HaveOccurred())
				cmd := fmt.Sprintf("mlxfwreset -d %s reset --level 3", devicePath)

				mlxfwresetJSON := fmt.Sprintf(`{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "PARAM_A", "default": "2", "current": "0", "next_boot": "1"}
  ],
  "command_required": %q,
  "reasons": ["Pending NVCONFIG parameter change"]
}`, cmd)
				optCtx := &operations.Context{
					RebootMethodDiscovery: true,
					CurrentBootID:         currentBootID,
					LatestDPU: &provisioningv1.DPU{
						Status: provisioningv1.DPUStatus{
							AgentStatus: &provisioningv1.AgentStatus{
								LastObservedPendingNVConfig: &provisioningv1.PendingNVConfigState{
									BootID: "previous-boot",
									Devices: []provisioningv1.PendingNVConfigDevice{
										{
											Device: devicePath,
											Entries: []provisioningv1.PendingNVConfigEntry{
												{Name: "PARAM_A", Default: "2", Current: "0", NextBoot: "1"},
											},
										},
									},
								},
							},
						},
					},
				}
				h := &HandleReboot{
					mstDevicesPath: dir,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}
				m, err := h.getRebootMethod(optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
				Expect(optCtx.CondMessage).To(Equal(fmt.Sprintf(
					"device=%s pending NVCONFIG params did not take effect after reboot: [PARAM_A(default=2,current=0,next=1)]; reset ignored because no other reset reasons remain.",
					devicePath,
				)))
				Expect(optCtx.Status.LastObservedPendingNVConfig).To(Equal(&provisioningv1.PendingNVConfigState{
					BootID: currentBootID,
					Devices: []provisioningv1.PendingNVConfigDevice{
						{
							Device: devicePath,
							Entries: []provisioningv1.PendingNVConfigEntry{
								{Name: "PARAM_A", Default: "2", Current: "0", NextBoot: "1"},
							},
						},
					},
				}))
			})

			It("filters stuck pending NVConfig entries per entry before selecting reboot method", func() {
				dir, err := os.MkdirTemp("", "reboot-mst-dq-filter-stuck-")
				Expect(err).NotTo(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()
				devicePath := filepath.Join(dir, "mt4125_pciconf0")
				Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())
				cmd := fmt.Sprintf("mlxfwreset -d %s reset --level 3", devicePath)

				mlxfwresetJSON := fmt.Sprintf(`{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "INTERNAL_CPU_MODEL", "default": "model-default", "current": "x", "next_boot": "y"},
    {"name": "ROCE_CONTROL", "default": "ROCE_ENABLE(2)", "current": "DEVICE_DEFAULT(0)", "next_boot": "ROCE_DISABLE(1)"}
  ],
  "command_required": %q,
  "reasons": ["Pending NVCONFIG parameter change"]
}`, cmd)
				optCtx := &operations.Context{
					RebootMethodDiscovery: true,
					CurrentBootID:         "current-boot-id",
					LatestDPU: &provisioningv1.DPU{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								cutil.AgentAnnotationAllowFirmwareResetReboot: "true",
							},
						},
						Status: provisioningv1.DPUStatus{
							AgentStatus: &provisioningv1.AgentStatus{
								LastObservedPendingNVConfig: &provisioningv1.PendingNVConfigState{
									BootID: "previous-boot",
									Devices: []provisioningv1.PendingNVConfigDevice{
										{
											Device: devicePath,
											Entries: []provisioningv1.PendingNVConfigEntry{
												{Name: "INTERNAL_CPU_MODEL", Default: "model-default", Current: "x", NextBoot: "y"},
											},
										},
									},
								},
							},
						},
					},
				}
				h := &HandleReboot{
					mstDevicesPath: dir,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}
				m, err := h.getRebootMethod(optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(*m).To(Equal(provisioningv1.RebootMethodFirmwareReset))
				Expect(optCtx.CondMessage).To(Equal(fmt.Sprintf(
					"device=%s pending NVCONFIG params did not take effect after reboot: [INTERNAL_CPU_MODEL(default=model-default,current=x,next=y)]; reset still required because other reset reasons remain.",
					devicePath,
				)))
			})

			It("does not ignore reset when pending NVCONFIG is not the only reason", func() {
				dir, err := os.MkdirTemp("", "reboot-mst-dq-other-reasons-")
				Expect(err).NotTo(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()
				devicePath := filepath.Join(dir, "mt4125_pciconf0")
				Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

				mlxfwresetJSON := `{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "PARAM_A", "default": "2", "current": "0", "next_boot": "1"}
  ],
  "command_required": "Reboot external host is required",
  "reasons": ["Pending NVCONFIG parameter change", "another reason"]
}`
				optCtx := &operations.Context{
					RebootMethodDiscovery: true,
					CurrentBootID:         "current-boot-id",
					LatestDPU: &provisioningv1.DPU{
						Status: provisioningv1.DPUStatus{
							AgentStatus: &provisioningv1.AgentStatus{
								LastObservedPendingNVConfig: &provisioningv1.PendingNVConfigState{
									BootID: "previous-boot",
									Devices: []provisioningv1.PendingNVConfigDevice{
										{
											Device: devicePath,
											Entries: []provisioningv1.PendingNVConfigEntry{
												{Name: "PARAM_A", Default: "2", Current: "0", NextBoot: "1"},
											},
										},
									},
								},
							},
						},
					},
				}
				h := &HandleReboot{
					mstDevicesPath: dir,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}

				m, err := h.getRebootMethod(optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
				Expect(optCtx.CondMessage).To(Equal(fmt.Sprintf(
					"device=%s pending NVCONFIG params did not take effect after reboot: [PARAM_A(default=2,current=0,next=1)]; reset still required because other reset reasons remain.",
					devicePath,
				)))
				Expect(optCtx.Status.LastObservedPendingNVConfig).To(Equal(&provisioningv1.PendingNVConfigState{
					BootID: "current-boot-id",
					Devices: []provisioningv1.PendingNVConfigDevice{
						{
							Device: devicePath,
							Entries: []provisioningv1.PendingNVConfigEntry{
								{Name: "PARAM_A", Default: "2", Current: "0", NextBoot: "1"},
							},
						},
					},
				}))
			})

			It("ignores repeated pending NVConfig entries on the same boot", func() {
				dir, err := os.MkdirTemp("", "reboot-mst-dq-same-boot-")
				Expect(err).NotTo(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()
				devicePath := filepath.Join(dir, "mt4125_pciconf0")
				Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())
				currentBootID, err := getCurrentRebootID()
				Expect(err).NotTo(HaveOccurred())
				cmd := fmt.Sprintf("mlxfwreset -d %s reset --level 3", devicePath)

				mlxfwresetJSON := fmt.Sprintf(`{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "INTERNAL_CPU_MODEL", "current": "x", "next_boot": "y"}
  ],
  "command_required": %q,
  "reasons": ["Pending NVCONFIG parameter change"]
}`, cmd)
				optCtx := &operations.Context{
					RebootMethodDiscovery: true,
					CurrentBootID:         currentBootID,
					LatestDPU: &provisioningv1.DPU{
						Status: provisioningv1.DPUStatus{
							AgentStatus: &provisioningv1.AgentStatus{
								LastObservedPendingNVConfig: &provisioningv1.PendingNVConfigState{
									BootID: currentBootID,
									Devices: []provisioningv1.PendingNVConfigDevice{
										{
											Device: devicePath,
											Entries: []provisioningv1.PendingNVConfigEntry{
												{Name: "INTERNAL_CPU_MODEL", Current: "x", NextBoot: "y"},
											},
										},
									},
								},
							},
						},
					},
				}
				h := &HandleReboot{
					mstDevicesPath: dir,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}
				m, err := h.getRebootMethod(optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
			})
		})

		It("getRebootMethod returns error when no MST devices are found", func() {
			dir, err := os.MkdirTemp("", "reboot-mst-empty-dq-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{mstDevicesPath: dir}
			_, err = h.getRebootMethod(optCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no MST devices found"))
		})

		It("HandleReboot sets RebootMethodDiscovery True when device-query path", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-cond-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt4125_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true
}
`)

			optCtx := &operations.Context{
				RebootMethodDiscovery:    true,
				CurrentBootID:            "boot-id",
				UpdateStatusUntilSuccess: func(context.Context) {},
			}
			h := &HandleReboot{
				skipBlock:      true,
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					if strings.Contains(cmd, "mlxfwreset -d") && strings.Contains(cmd, "s --json") {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					}
					Expect(cmd).To(Equal(fmt.Sprintf("sleep %d && shutdown -h now", shutdownDelayInSeconds)))
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			Expect(h.Execute(context.Background(), optCtx)).To(Succeed())
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodSystemLevelReset)))
			Expect(cond.Message).To(Equal(mlxfwresetJSON))
		})

		It("RebootMethodDiscovery condition uses Reason=SystemLevelReset and Message=mlxfwreset JSON when reset_needed", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-reasons-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt4125_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "reasons": [
    "Pending FW update",
    "Pending NVCONFIG parameter change"
  ]
}
`)

			optCtx := &operations.Context{
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
			}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodSystemLevelReset)))
			Expect(cond.Message).To(Equal(mlxfwresetJSON))
		})

		It("stores full mlxfwreset JSON in condition Message (device, FW versions, nvconfig, reasons)", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-full-json-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt4125_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			// Representative `mlxfwreset -d <mst> s --json` output (embedded fixture, same style as dpuflavor webhook YAML tests).
			mlxfwresetFullJSON := strings.TrimSpace(`
{
  "device": "/dev/mst/mt4125_pciconf0",
  "pending_fw_version": "22.49.0264",
  "running_fw_version": "22.33.1048",
  "pending_nvconfig_parameters": [
    {
      "name": "ROCE_CONTROL",
      "default": "ROCE_ENABLE(2)",
      "current": "DEVICE_DEFAULT(0)",
      "next_boot": "ROCE_DISABLE(1)"
    }
  ],
  "reset_needed": true,
  "description_action": "Driver restart and PCI reset (Level 3)",
  "command_required": "mlxfwreset -d /dev/mst/mt4125_pciconf0 reset --level 3 --type 0 --sync 0 --method 0",
  "reasons": [
    "Pending FW update",
    "Pending NVCONFIG parameter change"
  ]
}
`)

			optCtx := &operations.Context{
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				LatestDPU: &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							cutil.AgentAnnotationAllowFirmwareResetReboot: "true",
						},
					},
				},
			}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Expect(cmd).To(Equal(fmt.Sprintf("mlxfwreset -d %s s --json", devicePath)))
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetFullJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodFirmwareReset))
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodFirmwareReset)))
			Expect(cond.Message).To(Equal(mlxfwresetFullJSON))
		})

		It("getRebootMethod maps firmware-style command_required to SystemLevelReset when allow-firmware-reset-reboot annotation is absent", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-full-json-no-dev-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt4125_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			mlxfwresetFullJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "command_required": "mlxfwreset -d /dev/mst/mt4125_pciconf0 reset --level 3 --type 0 --sync 0 --method 0"
}
`)
			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetFullJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
		})

		It("getRebootMethod returns PowerCycle when pending_nvconfig includes a power-cycle-only parameter", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-pwr-cycle-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt41692_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "INTERNAL_CPU_MODEL", "current": "x", "next_boot": "y"}
  ],
  "command_required": "mlxfwreset -d /dev/mst/mt41692_pciconf0 reset --level 3"
}
`)
			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
		})

		It("getRebootMethod returns PowerCycle when command_required contains power cycle (case-insensitive substring)", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-pwr-cycle-cmd-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt41692_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": "N/A (No pending NVCONFIG parameters)",
  "command_required": "Host POWER CYCLE is required before applying configuration"
}
`)
			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
		})

		It("getRebootMethod returns SystemLevelReset when command_required is external host reboot message", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-ext-host-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt41692_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": "N/A (No pending NVCONFIG parameters)",
  "command_required": "Reboot external host is required"
}
`)
			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodSystemLevelReset)))
		})

		It("getRebootMethod matches external host message with trim and case-insensitive command_required", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-ext-host-case-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt41692_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			mlxfwresetJSON := `{"reset_needed":true,"command_required":"  reboot EXTERNAL host is required  "}`
			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
		})

		It("merges multiple MST devices: PowerCycle wins over FirmwareReset", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-merge-pc-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devA := filepath.Join(dir, "mt_a")
			devB := filepath.Join(dir, "mt_b")
			Expect(os.WriteFile(devA, nil, 0600)).To(Succeed())
			Expect(os.WriteFile(devB, nil, 0600)).To(Succeed())

			jsonFR := `{"reset_needed":true,"command_required":"mlxfwreset -d /dev/mst/mt_a reset --level 3"}`
			jsonPC := `{"reset_needed":true,"pending_nvconfig_parameters":[{"name":"INTERNAL_CPU_MODEL"}]}`
			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					switch {
					case strings.Contains(cmd, "mt_a"):
						_, _ = b.WriteString(jsonFR)
					case strings.Contains(cmd, "mt_b"):
						_, _ = b.WriteString(jsonPC)
					default:
						Fail("unexpected cmd: " + cmd)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			Expect(h.perDeviceFirmwareResetCmds).To(BeNil())
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond.Message).To(Equal(jsonFR + "\n---\n" + jsonPC))
		})

		It("merges multiple MST devices: SystemLevelReset wins over FirmwareReset", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-merge-slr-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devA := filepath.Join(dir, "mt_a")
			devB := filepath.Join(dir, "mt_b")
			Expect(os.WriteFile(devA, nil, 0600)).To(Succeed())
			Expect(os.WriteFile(devB, nil, 0600)).To(Succeed())

			jsonFR := `{"reset_needed":true,"command_required":"mlxfwreset -d /dev/mst/mt_a reset --level 3"}`
			jsonSLR := `{"reset_needed":true,"command_required":"Reboot external host is required"}`
			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					switch {
					case strings.Contains(cmd, "mt_a"):
						_, _ = b.WriteString(jsonFR)
					case strings.Contains(cmd, "mt_b"):
						_, _ = b.WriteString(jsonSLR)
					default:
						Fail("unexpected cmd: " + cmd)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
			Expect(h.perDeviceFirmwareResetCmds).To(BeNil())
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond.Message).To(Equal(jsonFR + "\n---\n" + jsonSLR))
		})

		It("joins mlxfwreset JSON from each reset-needed device in discovery condition Message", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-join-msg-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devA := filepath.Join(dir, "mt_a")
			devB := filepath.Join(dir, "mt_b")
			Expect(os.WriteFile(devA, nil, 0600)).To(Succeed())
			Expect(os.WriteFile(devB, nil, 0600)).To(Succeed())
			jsonA := `{"reset_needed":true,"tag":"a"}`
			jsonB := `{"reset_needed":true,"tag":"b"}`
			optCtx := &operations.Context{RebootMethodDiscovery: true}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					if strings.Contains(cmd, "mt_a") {
						_, _ = b.WriteString(jsonA)
					} else {
						_, _ = b.WriteString(jsonB)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			_, err = h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond.Message).To(Equal(jsonA + "\n---\n" + jsonB))
		})

		It("populates per-device firmware reset commands when merged method is FirmwareReset", func() {
			dir, err := os.MkdirTemp("", "reboot-dq-fw-plan-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devA := filepath.Join(dir, "mt_a")
			devB := filepath.Join(dir, "mt_b")
			Expect(os.WriteFile(devA, nil, 0600)).To(Succeed())
			Expect(os.WriteFile(devB, nil, 0600)).To(Succeed())
			cmdA := "mlxfwreset -d /dev/mst/mt_a reset --level 3 --type 0"
			cmdB := "mlxfwreset -d /dev/mst/mt_b reset --level 1"
			jsonA := `{"reset_needed":true,"command_required":"` + cmdA + `"}`
			jsonB := `{"reset_needed":true,"command_required":"` + cmdB + `"}`
			optCtx := &operations.Context{
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				LatestDPU: &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							cutil.AgentAnnotationAllowFirmwareResetReboot: "true",
						},
					},
				},
			}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					if strings.Contains(cmd, "mt_a") {
						_, _ = b.WriteString(jsonA)
					} else {
						_, _ = b.WriteString(jsonB)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := h.getRebootMethod(optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodFirmwareReset))
			Expect(h.perDeviceFirmwareResetCmds).To(Equal([]firmwareResetPerDevice{
				{DevicePath: devA, Cmd: cmdA},
				{DevicePath: devB, Cmd: cmdB},
			}))
		})

		It("getRebootMethod returns error when mlxfwreset JSON is invalid", func() {
			dir, err := os.MkdirTemp("", "reboot-mst-dq-json-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt4125_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			optCtx := &operations.Context{RebootMethodDiscovery: true, CurrentBootID: "boot-id"}
			h := &HandleReboot{
				mstDevicesPath: dir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(`not json`)
					return b, bytes.Buffer{}, nil
				},
			}
			_, err = h.getRebootMethod(optCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parse JSON:"))
		})
	})

	Describe("MST reboot method helpers", func() {
		DescribeTable("rebootMethodMergePriority",
			func(m provisioningv1.RebootMethodType, want int) {
				Expect(rebootMethodMergePriority(m)).To(Equal(want))
			},
			Entry("PowerCycle", provisioningv1.RebootMethodPowerCycle, 0),
			Entry("SystemLevelReset", provisioningv1.RebootMethodSystemLevelReset, 1),
			Entry("FirmwareReset", provisioningv1.RebootMethodFirmwareReset, 2),
			Entry("NoAction", provisioningv1.RebootMethodNoAction, 3),
			Entry("unhandled type falls through to default bucket (same priority as NoAction)",
				provisioningv1.RebootMethodType("NotAnMSTMergeMethod"), 3),
		)

		DescribeTable("rebootMethodTakesPrecedenceOver",
			func(a, b provisioningv1.RebootMethodType, want bool) {
				Expect(rebootMethodTakesPrecedenceOver(a, b)).To(Equal(want))
			},
			Entry("PowerCycle beats FirmwareReset",
				provisioningv1.RebootMethodPowerCycle, provisioningv1.RebootMethodFirmwareReset, true),
			Entry("FirmwareReset does not beat PowerCycle",
				provisioningv1.RebootMethodFirmwareReset, provisioningv1.RebootMethodPowerCycle, false),
			Entry("PowerCycle beats SystemLevelReset",
				provisioningv1.RebootMethodPowerCycle, provisioningv1.RebootMethodSystemLevelReset, true),
			Entry("SystemLevelReset beats FirmwareReset",
				provisioningv1.RebootMethodSystemLevelReset, provisioningv1.RebootMethodFirmwareReset, true),
			Entry("FirmwareReset does not beat SystemLevelReset",
				provisioningv1.RebootMethodFirmwareReset, provisioningv1.RebootMethodSystemLevelReset, false),
			Entry("same method — no replacement",
				provisioningv1.RebootMethodFirmwareReset, provisioningv1.RebootMethodFirmwareReset, false),
			Entry("FirmwareReset beats NoAction",
				provisioningv1.RebootMethodFirmwareReset, provisioningv1.RebootMethodNoAction, true),
		)

		It("rebootMethodFromMlxfwresetStatus follows PowerCycle > SystemLevelReset > FirmwareReset > SystemLevelReset fallback", func() {
			dev := "/dev/mst/x"
			h := &HandleReboot{}

			var outPC mlxfwresetStatusJSON
			Expect(json.Unmarshal([]byte(`{"reset_needed":true,"pending_nvconfig_parameters":[{"name":"INTERNAL_CPU_MODEL"}],"command_required":"mlxfwreset -d x reset"}`), &outPC)).To(Succeed())
			Expect(rebootMethodFromMlxfwresetStatus(h, dev, &outPC)).To(Equal(provisioningv1.RebootMethodPowerCycle))

			var outPCCmd mlxfwresetStatusJSON
			Expect(json.Unmarshal([]byte(`{"reset_needed":true,"command_required":"Please perform a power cycle on the host"}`), &outPCCmd)).To(Succeed())
			Expect(rebootMethodFromMlxfwresetStatus(h, dev, &outPCCmd)).To(Equal(provisioningv1.RebootMethodPowerCycle))

			var outSLR mlxfwresetStatusJSON
			Expect(json.Unmarshal([]byte(`{"reset_needed":true,"command_required":"Reboot external host is required"}`), &outSLR)).To(Succeed())
			Expect(rebootMethodFromMlxfwresetStatus(h, dev, &outSLR)).To(Equal(provisioningv1.RebootMethodSystemLevelReset))

			var outFR mlxfwresetStatusJSON
			Expect(json.Unmarshal([]byte(`{"reset_needed":true,"command_required":"mlxfwreset -d x reset --level 3"}`), &outFR)).To(Succeed())
			Expect(rebootMethodFromMlxfwresetStatus(h, dev, &outFR)).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
			h.allowFirmwareReset = true
			Expect(rebootMethodFromMlxfwresetStatus(h, dev, &outFR)).To(Equal(provisioningv1.RebootMethodFirmwareReset))

			var outFallback mlxfwresetStatusJSON
			Expect(json.Unmarshal([]byte(`{"reset_needed":true}`), &outFallback)).To(Succeed())
			Expect(rebootMethodFromMlxfwresetStatus(nil, dev, &outFallback)).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
		})
	})

	// exec* tests call HandleReboot helpers directly; they are not tied to RebootMethodDiscovery or Execute().
	Describe("HandleReboot exec helpers", func() {
		Context("execPowerCycle", func() {
			It("sets RebootMethod PowerCycle and updates status before blocking", func() {
				statusPushed := false
				optCtx := &operations.Context{
					UpdateStatusUntilSuccess: func(context.Context) { statusPushed = true },
				}
				h := &HandleReboot{skipBlock: true}
				Expect(h.execPowerCycle(context.Background(), optCtx)).To(Succeed())
				Expect(statusPushed).To(BeTrue())
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
			It("sets status and runs mlxfwreset reset for each MST device", func() {
				dir, err := os.MkdirTemp("", "reboot-mst-")
				Expect(err).NotTo(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()
				devicePath := filepath.Join(dir, "mt4119_pciconf0")
				Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())
				cmd := fmt.Sprintf("mlxfwreset -d %s -y reset", devicePath)
				optCtx := &operations.Context{
					UpdateStatusUntilSuccess: func(context.Context) {}, // no-op for unit test
				}
				var fwResetCmds []string
				h := &HandleReboot{
					skipBlock: true,
					perDeviceFirmwareResetCmds: []firmwareResetPerDevice{
						{DevicePath: devicePath, Cmd: cmd},
					},
					runBash: func(c string) (bytes.Buffer, bytes.Buffer, error) {
						fwResetCmds = append(fwResetCmds, c)
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.execFirmwareReset(context.Background(), optCtx)).To(Succeed())
				Expect(optCtx.Status.RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodFirmwareReset))
				Expect(fwResetCmds).To(Equal([]string{cmd}))
			})

			It("runs discovered per-device firmware reset commands without listing MST devices", func() {
				optCtx := &operations.Context{
					UpdateStatusUntilSuccess: func(context.Context) {},
				}
				var ran []string
				h := &HandleReboot{
					skipBlock: true,
					perDeviceFirmwareResetCmds: []firmwareResetPerDevice{
						{DevicePath: "/dev/mst/a", Cmd: "custom-a"},
						{DevicePath: "/dev/mst/b", Cmd: "custom-b"},
					},
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						ran = append(ran, cmd)
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.execFirmwareReset(context.Background(), optCtx)).To(Succeed())
				Expect(ran).To(Equal([]string{"custom-a", "custom-b"}))
				Expect(h.perDeviceFirmwareResetCmds).To(BeNil())
			})

			It("runs mlxfwreset reset for every device under mst path", func() {
				dir, err := os.MkdirTemp("", "reboot-mst-multi-")
				Expect(err).NotTo(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()
				d1 := filepath.Join(dir, "mt_dev1")
				d2 := filepath.Join(dir, "mt_dev2")
				Expect(os.WriteFile(d1, nil, 0600)).To(Succeed())
				Expect(os.WriteFile(d2, nil, 0600)).To(Succeed())
				paths := []string{d1, d2}
				sort.Strings(paths)
				optCtx := &operations.Context{
					UpdateStatusUntilSuccess: func(context.Context) {},
				}
				var ran []string
				h := &HandleReboot{
					skipBlock: true,
					perDeviceFirmwareResetCmds: []firmwareResetPerDevice{
						{DevicePath: paths[0], Cmd: fmt.Sprintf("mlxfwreset -d %s -y reset", paths[0])},
						{DevicePath: paths[1], Cmd: fmt.Sprintf("mlxfwreset -d %s -y reset", paths[1])},
					},
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						ran = append(ran, cmd)
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.execFirmwareReset(context.Background(), optCtx)).To(Succeed())
				Expect(ran).To(HaveLen(2))
				Expect(ran).To(Equal([]string{
					fmt.Sprintf("mlxfwreset -d %s -y reset", paths[0]),
					fmt.Sprintf("mlxfwreset -d %s -y reset", paths[1]),
				}))
			})

			It("fails when per-device firmware reset commands are empty", func() {
				optCtx := &operations.Context{}
				h := &HandleReboot{skipBlock: true}
				execErr := h.execFirmwareReset(context.Background(), optCtx)
				Expect(execErr).To(HaveOccurred())
				Expect(execErr.Error()).To(ContainSubstring("per-device firmware reset commands are empty"))
			})
		})
	})
})

var _ = Describe("MFT tool version parsing and resolveRebootMethodDiscovery", func() {
	It("parseMlxfwresetVersionOutput skips prefix before mft then parses like mlxconfig", func() {
		out := "mlxfwreset 1.0.0, mft 4.36.0-93, Git SHA Hash: 2565c9618"
		version, err := parseMlxfwresetVersionOutput(out)
		Expect(err).NotTo(HaveOccurred())
		Expect(version.Equal(semver.MustParse("4.36.0-93"))).To(BeTrue())
	})

	It("parseMlxfwresetVersionOutput fails when version cannot be extracted", func() {
		_, err := parseMlxfwresetVersionOutput("no version here")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to extract mlxfwreset MFT version"))
	})

	It("parseMlxconfigVersionOutput extracts version like former nvconfig mlxconfigVersion", func() {
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
			{
				output:          "mlxconfig, mft 4.36.0-86. Git SHA Hash: e44fa1501",
				expectedVersion: semver.MustParse("4.36.0-86"),
			},
		}
		for _, tc := range testCases {
			version, err := parseMlxconfigVersionOutput(tc.output)
			Expect(err).NotTo(HaveOccurred())
			Expect(version.Equal(tc.expectedVersion)).To(BeTrue(), "output %q => got %s, expected %s", tc.output, version.String(), tc.expectedVersion.String())
		}
	})

	It("parseMlxconfigVersionOutput fails when version cannot be extracted", func() {
		_, err := parseMlxconfigVersionOutput("no version here")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to extract version from mlxconfig output"))
	})

	It("parseMlxconfigVersionOutput fails for short form (e.g. 4.30)", func() {
		_, err := parseMlxconfigVersionOutput("mlxconfig 4.30")
		Expect(err).To(HaveOccurred())
	})

	It("resolveRebootMethodDiscovery returns true when both tools meet minimum", func() {
		run := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			var b bytes.Buffer
			switch {
			case strings.HasPrefix(cmd, "mlxfwreset"):
				b.WriteString("mlxfwreset 1.0.0, mft 4.36.0-95, Git SHA Hash: abc\n")
			case strings.HasPrefix(cmd, "mlxconfig"):
				b.WriteString("mlxconfig, mft 4.36.0-95. Git SHA Hash: abc\n")
			}
			return b, bytes.Buffer{}, nil
		}
		Expect(resolveRebootMethodDiscovery(run)).To(BeTrue())
	})

	It("resolveRebootMethodDiscovery returns false when mlxfwreset is below minimum", func() {
		run := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			var b bytes.Buffer
			if strings.HasPrefix(cmd, "mlxfwreset") {
				b.WriteString("mlxfwreset 1.0.0, mft 4.36.0-86, Git SHA Hash: x\n")
				return b, bytes.Buffer{}, nil
			}
			b.WriteString("mlxconfig, mft 4.36.0-95\n")
			return b, bytes.Buffer{}, nil
		}
		Expect(resolveRebootMethodDiscovery(run)).To(BeFalse())
	})

	It("resolveRebootMethodDiscovery returns false when mlxconfig is below minimum", func() {
		run := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			var b bytes.Buffer
			if strings.HasPrefix(cmd, "mlxfwreset") {
				b.WriteString("mlxfwreset 1.0.0, mft 4.36.0-95, Git SHA Hash: x\n")
				return b, bytes.Buffer{}, nil
			}
			b.WriteString("mlxconfig, mft 4.36.0-86\n")
			return b, bytes.Buffer{}, nil
		}
		Expect(resolveRebootMethodDiscovery(run)).To(BeFalse())
	})

	It("semver prerelease ordering for min version 4.36.0-95", func() {
		minVer := semver.MustParse(MinRebootDiscoveryMFTVersion)
		v94 := semver.MustParse("4.36.0-94")
		v95 := semver.MustParse("4.36.0-95")
		Expect(mftVersionMeetsMinimum(v94, minVer)).To(BeFalse())
		Expect(mftVersionMeetsMinimum(v95, minVer)).To(BeTrue())
	})
})
