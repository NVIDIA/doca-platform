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

				optCtxNotBooted := &operations.Context{LatestDPU: &provisioningv1.DPU{}, RebootMethodDiscovery: false}
				m, err := h.getRebootMethod(optCtxNotBooted)
				Expect(err).NotTo(HaveOccurred())
				Expect(m).NotTo(BeNil())
				Expect(allowedMethods[*m]).To(BeTrue(), "getRebootMethod got %s", *m)
				Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))

				optCtxBooted := &operations.Context{
					RebootMethodDiscovery: false,
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

			optCtx := &operations.Context{RebootMethodDiscovery: true}
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

			optCtx := &operations.Context{RebootMethodDiscovery: true}
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

			optCtx := &operations.Context{RebootMethodDiscovery: true}
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

			optCtx := &operations.Context{RebootMethodDiscovery: true}
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

		It("getRebootMethod returns error when no MST devices are found", func() {
			dir, err := os.MkdirTemp("", "reboot-mst-empty-dq-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			optCtx := &operations.Context{RebootMethodDiscovery: true}
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

			optCtx := &operations.Context{RebootMethodDiscovery: true}
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
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
			cond := meta.FindStatusCondition(optCtx.Status.Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodSystemLevelReset)))
			Expect(cond.Message).To(Equal(mlxfwresetFullJSON))
		})

		It("getRebootMethod returns error when mlxfwreset JSON is invalid", func() {
			dir, err := os.MkdirTemp("", "reboot-mst-dq-json-")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(dir) }()
			devicePath := filepath.Join(dir, "mt4125_pciconf0")
			Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())

			optCtx := &operations.Context{RebootMethodDiscovery: true}
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

	// exec* tests call HandleReboot helpers directly; they are not tied to RebootMethodDiscovery or Execute().
	Describe("HandleReboot exec helpers", func() {
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
			It("sets status and runs mlxfwreset reset for each MST device", func() {
				dir, err := os.MkdirTemp("", "reboot-mst-")
				Expect(err).NotTo(HaveOccurred())
				defer func() { _ = os.RemoveAll(dir) }()
				devicePath := filepath.Join(dir, "mt4119_pciconf0")
				Expect(os.WriteFile(devicePath, nil, 0600)).To(Succeed())
				optCtx := &operations.Context{
					UpdateStatusUntilSuccess: func(context.Context) {}, // no-op for unit test
				}
				var fwResetCmds []string
				h := &HandleReboot{
					skipBlock:      true,
					mstDevicesPath: dir,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						fwResetCmds = append(fwResetCmds, cmd)
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.execFirmwareReset(context.Background(), optCtx)).To(Succeed())
				Expect(optCtx.Status.RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodFirmwareReset))
				Expect(fwResetCmds).To(Equal([]string{fmt.Sprintf("mlxfwreset -d %s -y reset", devicePath)}))
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
				var cmds []string
				h := &HandleReboot{
					skipBlock:      true,
					mstDevicesPath: dir,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						cmds = append(cmds, cmd)
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.execFirmwareReset(context.Background(), optCtx)).To(Succeed())
				Expect(cmds).To(HaveLen(2))
				Expect(cmds).To(Equal([]string{
					fmt.Sprintf("mlxfwreset -d %s -y reset", paths[0]),
					fmt.Sprintf("mlxfwreset -d %s -y reset", paths[1]),
				}))
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
