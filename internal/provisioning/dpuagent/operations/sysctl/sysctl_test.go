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

package sysctl

import (
	"os"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Sysctl Operation", func() {
	var tempDir string

	var createMockKernelParameters = func(kernelParametersDirectory string, params []string) {
		for _, param := range params {
			parts := strings.SplitN(param, "=", 2)
			Expect(parts).To(HaveLen(2))
			key := parts[0]
			value := parts[1]
			filename := strings.ReplaceAll(key, ".", "/")
			path := filepath.Join(kernelParametersDirectory, filename)
			Expect(os.MkdirAll(filepath.Dir(path), 0755)).To(Succeed())
			Expect(os.WriteFile(path, []byte(value), 0644)).To(Succeed())
		}
	}

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "sysctl-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("Execute", func() {
		It("should be skipped if SkipSysctl is true", func() {
			operation := &SetParams{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipSysctl: true,
				},
			})).To(BeTrue())
		})

		It("should return error when flavor params conflict with mandatory params", func() {
			applied := false
			operation := &SetParams{
				etcDirectory:              filepath.Join(tempDir, "etc"),
				kernelParametersDirectory: filepath.Join(tempDir, "proc", "sys"),
				applyParams:               func() error { applied = true; return nil },
			}

			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Sysctl: provisioningv1.DPUFLavorSysctl{
							Parameters: []string{
								"kernel.panic=0",
							},
						},
					},
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("flavor sysctl parameters conflict with mandatory parameters"))
			Expect(err.Error()).To(ContainSubstring("kernel.panic"))
			Expect(err.Error()).To(ContainSubstring("flavor requires 0"))
			Expect(err.Error()).To(ContainSubstring("mandatory requires 10"))
			Expect(applied).To(BeFalse())

			_, err = os.Stat(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(os.IsNotExist(err)).To(BeTrue())
			_, err = os.Stat(filepath.Join(operation.etcDirectory, "sysctl.d", "99-dpf.conf"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should append to /etc/sysctl.conf if mandatory params are not matching", func() {
			applied := false
			operation := &SetParams{
				etcDirectory:              filepath.Join(tempDir, "etc"),
				kernelParametersDirectory: filepath.Join(tempDir, "proc", "sys"),
				applyParams:               func() error { applied = true; return nil },
			}
			By("create mock kernel parameters")
			createMockKernelParameters(operation.kernelParametersDirectory, []string{
				"net.ipv4.ip_forward=0",
				"net.bridge.bridge-nf-call-iptables=0",
				"net.bridge.bridge-nf-call-ip6tables=0",
				"kernel.panic_on_oops=1",
				"kernel.panic=10",
				"vm.overcommit_memory=1",
			})
			Expect(os.MkdirAll(filepath.Join(operation.etcDirectory, "sysctl.d"), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(operation.etcDirectory, "sysctl.conf"), []byte("kernel.keys.root_maxbytes=25000000\n"), 0644)).To(Succeed())

			By("execute operation")
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verify that mandatory params are written to /etc/sysctl.conf")
			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Split(strings.TrimSpace(string(content)), "\n")).To(ConsistOf(
				"kernel.keys.root_maxbytes=25000000",
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic_on_oops=1",
				"kernel.panic=10",
				"vm.overcommit_memory=1",
			))

			By("verify that applyParams is called")
			Expect(applied).To(BeTrue())
		})

		It("should skip update when all sysctl parameters are already correctly set", func() {
			applied := false
			operation := &SetParams{
				etcDirectory:              filepath.Join(tempDir, "etc"),
				kernelParametersDirectory: filepath.Join(tempDir, "proc", "sys"),
				applyParams:               func() error { applied = true; return nil },
			}
			By("create mock kernel parameters with correct values")
			createMockKernelParameters(operation.kernelParametersDirectory, []string{
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic_on_oops=1",
				"kernel.panic=10",
				"vm.overcommit_memory=1",
			})
			Expect(os.MkdirAll(filepath.Join(operation.etcDirectory, "sysctl.d"), 0755)).To(Succeed())
			existingContent := `net.ipv4.ip_forward=1
net.bridge.bridge-nf-call-iptables=1
net.bridge.bridge-nf-call-ip6tables=1
kernel.panic_on_oops=1
kernel.panic=10
vm.overcommit_memory=1
`
			Expect(os.WriteFile(filepath.Join(operation.etcDirectory, "sysctl.conf"), []byte(existingContent), 0644)).To(Succeed())

			By("execute operation")
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verify that applyParams is NOT called since no update is needed")
			Expect(applied).To(BeFalse())

			By("verify that sysctl.conf is unchanged since no update is needed")
			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(existingContent))
		})

		It("should write user params to /etc/sysctl.d/99-dpf.conf", func() {
			applied := false
			operation := &SetParams{
				etcDirectory:              filepath.Join(tempDir, "etc"),
				kernelParametersDirectory: filepath.Join(tempDir, "proc", "sys"),
				applyParams:               func() error { applied = true; return nil },
			}
			By("create mock kernel parameters")
			createMockKernelParameters(operation.kernelParametersDirectory, []string{
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic=0",
				"kernel.panic_on_oops=0",
				"vm.overcommit_memory=1",
			})
			Expect(os.MkdirAll(filepath.Join(operation.etcDirectory, "sysctl.d"), 0755)).To(Succeed())

			flavor := &provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					Sysctl: provisioningv1.DPUFLavorSysctl{
						Parameters: []string{
							"kernel.panic=10",
							"kernel.panic_on_oops=1",
						},
					},
				},
			}
			By("execute operation")
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: *flavor,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verify that mandatory params are written to /etc/sysctl.conf")
			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Split(strings.TrimSpace(string(content)), "\n")).To(ConsistOf(
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic_on_oops=1",
				"kernel.panic=10",
				"vm.overcommit_memory=1",
			))

			By("verify that user params are written to /etc/sysctl.d/99-dpf.conf")
			content, err = os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.d", "99-dpf.conf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Split(strings.TrimSpace(string(content)), "\n")).To(ConsistOf(
				"kernel.panic=10",
				"kernel.panic_on_oops=1",
			))

			By("verify that applyParams is called")
			Expect(applied).To(BeTrue())
		})

		It("should apply when runtime drifts even if managed files already match", func() {
			applied := false
			operation := &SetParams{
				etcDirectory:              filepath.Join(tempDir, "etc"),
				kernelParametersDirectory: filepath.Join(tempDir, "proc", "sys"),
				applyParams:               func() error { applied = true; return nil },
			}
			Expect(os.MkdirAll(filepath.Join(operation.etcDirectory, "sysctl.d"), 0755)).To(Succeed())

			existingContent := `net.ipv4.ip_forward=1
net.bridge.bridge-nf-call-iptables=1
net.bridge.bridge-nf-call-ip6tables=1
kernel.panic_on_oops=1
kernel.panic=10
vm.overcommit_memory=1
`
			Expect(os.WriteFile(filepath.Join(operation.etcDirectory, "sysctl.conf"), []byte(existingContent), 0644)).To(Succeed())

			createMockKernelParameters(operation.kernelParametersDirectory, []string{
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic_on_oops=1",
				"kernel.panic=0",
				"vm.overcommit_memory=1",
			})

			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(applied).To(BeTrue())

			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(existingContent))
		})
	})

	Context("appendMandatoryParamsToConf", func() {
		It("should return false when the effective existing values already match", func() {
			operation := &SetParams{
				etcDirectory: filepath.Join(tempDir, "etc"),
			}
			Expect(os.MkdirAll(operation.etcDirectory, 0755)).To(Succeed())

			By("create sysctl.conf where a later duplicate already matches the mandatory value")
			existingContent := `net.ipv4.ip_forward=0
net.ipv4.ip_forward=1
net.bridge.bridge-nf-call-iptables=1
net.bridge.bridge-nf-call-ip6tables=1
kernel.panic_on_oops=1
kernel.panic=10
vm.overcommit_memory=1
`
			Expect(os.WriteFile(filepath.Join(operation.etcDirectory, "sysctl.conf"), []byte(existingContent), 0644)).To(Succeed())

			By("append mandatory params")
			modified, err := operation.appendMandatoryParamsToConf()
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeFalse())

			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(existingContent))
		})

		It("should return true when mandatory params need to be appended", func() {
			operation := &SetParams{
				etcDirectory: filepath.Join(tempDir, "etc"),
			}
			Expect(os.MkdirAll(operation.etcDirectory, 0755)).To(Succeed())

			By("create sysctl.conf with params having different values")
			existingContent := `net.ipv4.ip_forward=0
`
			Expect(os.WriteFile(filepath.Join(operation.etcDirectory, "sysctl.conf"), []byte(existingContent), 0644)).To(Succeed())

			By("append mandatory params")
			modified, err := operation.appendMandatoryParamsToConf()
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeTrue())

			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Split(strings.TrimSpace(string(content)), "\n")).To(ConsistOf(
				"net.ipv4.ip_forward=0",
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic_on_oops=1",
				"kernel.panic=10",
				"vm.overcommit_memory=1",
			))
		})

		It("should create sysctl.conf and return true if it does not exist", func() {
			operation := &SetParams{
				etcDirectory: filepath.Join(tempDir, "etc"),
			}
			Expect(os.MkdirAll(operation.etcDirectory, 0755)).To(Succeed())

			By("verify sysctl.conf does not exist")
			_, err := os.Stat(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(os.IsNotExist(err)).To(BeTrue())

			By("append mandatory params to non-existent sysctl.conf")
			modified, err := operation.appendMandatoryParamsToConf()
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeTrue())

			By("verify that sysctl.conf is created with all params")
			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Split(strings.TrimSpace(string(content)), "\n")).To(ConsistOf(
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic_on_oops=1",
				"kernel.panic=10",
				"vm.overcommit_memory=1",
			))
		})
	})

	Context("writeUserParams", func() {
		It("should return false and not create a file when user params are empty", func() {
			operation := &SetParams{
				etcDirectory: filepath.Join(tempDir, "etc"),
			}
			Expect(os.MkdirAll(filepath.Join(operation.etcDirectory, "sysctl.d"), 0755)).To(Succeed())

			modified, err := operation.writeUserParams(map[string]string{})
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeFalse())

			_, err = os.Stat(filepath.Join(operation.etcDirectory, "sysctl.d", "99-dpf.conf"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should return false when user params file content already matches exactly", func() {
			operation := &SetParams{
				etcDirectory: filepath.Join(tempDir, "etc"),
			}
			Expect(os.MkdirAll(filepath.Join(operation.etcDirectory, "sysctl.d"), 0755)).To(Succeed())

			expectedContent := `kernel.panic=10
kernel.panic_on_oops=1
`
			Expect(os.WriteFile(filepath.Join(operation.etcDirectory, "sysctl.d", "99-dpf.conf"), []byte(expectedContent), 0644)).To(Succeed())

			modified, err := operation.writeUserParams(map[string]string{
				"kernel.panic":         "10",
				"kernel.panic_on_oops": "1",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeFalse())

			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.d", "99-dpf.conf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(expectedContent))
		})

		It("should return true and rewrite file when user params content differs", func() {
			operation := &SetParams{
				etcDirectory: filepath.Join(tempDir, "etc"),
			}
			Expect(os.MkdirAll(filepath.Join(operation.etcDirectory, "sysctl.d"), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(operation.etcDirectory, "sysctl.d", "99-dpf.conf"), []byte("kernel.panic=0\n"), 0644)).To(Succeed())

			modified, err := operation.writeUserParams(map[string]string{
				"kernel.panic":         "10",
				"kernel.panic_on_oops": "1",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(modified).To(BeTrue())

			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.d", "99-dpf.conf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(`kernel.panic=10
kernel.panic_on_oops=1
`))
		})
	})

	Context("CheckParamsOperation", func() {
		It("should never be skipped", func() {
			operation := &CheckParams{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipSysctl: true,
				},
			})).To(BeFalse())
		})

		It("should return error if mandatory params are not matching", func() {
			operation := &CheckParams{
				kernelParametersDirectory: filepath.Join(tempDir, "proc", "sys"),
			}
			By("create mock kernel parameters")
			createMockKernelParameters(operation.kernelParametersDirectory, []string{
				"net.ipv4.ip_forward=0",
				"net.bridge.bridge-nf-call-iptables=0",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic_on_oops=1",
				"kernel.panic=10",
				"vm.overcommit_memory=1",
			})
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Mandatory sysctl parameters mismatch."))
			Expect(err.Error()).To(ContainSubstring("net.ipv4.ip_forward: expected 1, current 0"))
			Expect(err.Error()).To(ContainSubstring("net.bridge.bridge-nf-call-iptables: expected 1, current 0"))
		})

		It("should return error if user params are not matching", func() {
			operation := &CheckParams{
				kernelParametersDirectory: filepath.Join(tempDir, "proc", "sys"),
			}
			createMockKernelParameters(operation.kernelParametersDirectory, []string{
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic_on_oops=1",
				"kernel.panic=10",
				"vm.overcommit_memory=1",
				"net.core.somaxconn=1024",
			})
			flavor := &provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					Sysctl: provisioningv1.DPUFLavorSysctl{
						Parameters: []string{
							"net.core.somaxconn=2048",
						},
					},
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: *flavor,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("DPUFlavor sysctl parameters mismatch."))
			Expect(err.Error()).To(ContainSubstring("net.core.somaxconn: expected 2048, current 1024"))
		})

		It("should return conflict error before checking runtime values", func() {
			operation := &CheckParams{
				kernelParametersDirectory: filepath.Join(tempDir, "proc", "sys"),
			}

			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						Sysctl: provisioningv1.DPUFLavorSysctl{
							Parameters: []string{
								"kernel.panic=0",
							},
						},
					},
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("flavor sysctl parameters conflict with mandatory parameters"))
			Expect(err.Error()).To(ContainSubstring("kernel.panic"))
		})
	})

	Context("Helpers", func() {
		It("should return effective params using the last value for duplicate keys", func() {
			effectiveParams, err := getEffectiveSysctlParams([]string{
				"net.ipv4.ip_forward=0",
				"vm.overcommit_memory=1",
				" net.ipv4.ip_forward = 1 ",
				"net.core.somaxconn=1024",
				"vm.overcommit_memory=2",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(effectiveParams).To(Equal(map[string]string{
				"net.ipv4.ip_forward":  "1",
				"net.core.somaxconn":   "1024",
				"vm.overcommit_memory": "2",
			}))
		})

		It("should return error for conflicting flavor params after applying same-file override semantics", func() {
			effectiveFlavorParams, err := getEffectiveSysctlParams([]string{
				"vm.overcommit_memory=1",
				"kernel.panic=5",
				"kernel.panic=0",
				"net.ipv4.ip_forward=1",
			})
			Expect(err).NotTo(HaveOccurred())
			err = validateNoConflicts(effectiveFlavorParams)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kernel.panic"))
			Expect(err.Error()).To(ContainSubstring("flavor requires 0"))
			Expect(err.Error()).To(ContainSubstring("mandatory requires 10"))
		})

		It("should return nil when flavor overrides itself to the mandatory value", func() {
			effectiveFlavorParams, err := getEffectiveSysctlParams([]string{
				"kernel.panic=0",
				"kernel.panic=10",
			})
			Expect(err).NotTo(HaveOccurred())
			err = validateNoConflicts(effectiveFlavorParams)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return no mismatches when runtime params match expected params", func() {
			kernelParametersDirectory := filepath.Join(tempDir, "proc", "sys")
			createMockKernelParameters(kernelParametersDirectory, []string{
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic_on_oops=1",
				"kernel.panic=10",
				"vm.overcommit_memory=1",
				"net.core.somaxconn=2048",
			})

			effectiveFlavorParams, err := getEffectiveSysctlParams([]string{
				"net.core.somaxconn=1024",
				"net.core.somaxconn=2048",
			})
			Expect(err).NotTo(HaveOccurred())

			expectedRuntimeParams := map[string]string{
				"net.ipv4.ip_forward":                 "1",
				"net.bridge.bridge-nf-call-iptables":  "1",
				"net.bridge.bridge-nf-call-ip6tables": "1",
				"kernel.panic_on_oops":                "1",
				"kernel.panic":                        "10",
				"vm.overcommit_memory":                "1",
			}
			for key, value := range effectiveFlavorParams {
				expectedRuntimeParams[key] = value
			}

			mismatches, err := isRuntimeSysctlParamsMatching(kernelParametersDirectory, expectedRuntimeParams)
			Expect(err).NotTo(HaveOccurred())
			Expect(mismatches).To(BeEmpty())
		})

		It("should return all runtime mismatches", func() {
			kernelParametersDirectory := filepath.Join(tempDir, "proc", "sys")
			createMockKernelParameters(kernelParametersDirectory, []string{
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
				"net.bridge.bridge-nf-call-ip6tables=1",
				"kernel.panic_on_oops=1",
				"kernel.panic=0",
				"vm.overcommit_memory=1",
				"net.core.somaxconn=1024",
			})

			effectiveFlavorParams, err := getEffectiveSysctlParams([]string{
				"net.core.somaxconn=2048",
			})
			Expect(err).NotTo(HaveOccurred())

			expectedRuntimeParams := map[string]string{
				"net.ipv4.ip_forward":                 "1",
				"net.bridge.bridge-nf-call-iptables":  "1",
				"net.bridge.bridge-nf-call-ip6tables": "1",
				"kernel.panic_on_oops":                "1",
				"kernel.panic":                        "10",
				"vm.overcommit_memory":                "1",
			}
			for key, value := range effectiveFlavorParams {
				expectedRuntimeParams[key] = value
			}

			mismatches, err := isRuntimeSysctlParamsMatching(kernelParametersDirectory, expectedRuntimeParams)
			Expect(err).NotTo(HaveOccurred())
			Expect(mismatches).To(ConsistOf(
				MismatchParam{
					Key:           "kernel.panic",
					ExpectedValue: "10",
					CurrentValue:  "0",
				},
				MismatchParam{
					Key:           "net.core.somaxconn",
					ExpectedValue: "2048",
					CurrentValue:  "1024",
				},
			))
		})
	})
})
