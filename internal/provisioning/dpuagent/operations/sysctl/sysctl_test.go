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
	"fmt"
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

		It("should append to /etc/sysctl.conf if mandatory params are not matching", func() {
			By(fmt.Sprintf("tempDir: %s", tempDir))
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
			expectedContent := `kernel.keys.root_maxbytes=25000000
net.ipv4.ip_forward=1
net.bridge.bridge-nf-call-iptables=1
net.bridge.bridge-nf-call-ip6tables=1
`
			Expect(string(content)).To(Equal(expectedContent))

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
			})
			Expect(os.MkdirAll(filepath.Join(operation.etcDirectory, "sysctl.d"), 0755)).To(Succeed())

			By("execute operation")
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verify that applyParams is NOT called since no update is needed")
			Expect(applied).To(BeFalse())

			By("verify that sysctl.conf is NOT created since no update is needed")
			_, err = os.Stat(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(os.IsNotExist(err)).To(BeTrue())
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
			expectedContent := `net.ipv4.ip_forward=1
net.bridge.bridge-nf-call-iptables=1
net.bridge.bridge-nf-call-ip6tables=1
`
			Expect(string(content)).To(Equal(expectedContent))

			By("verify that user params are written to /etc/sysctl.d/99-dpf.conf")
			content, err = os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.d", "99-dpf.conf"))
			Expect(err).NotTo(HaveOccurred())
			expectedContent = `kernel.panic=10
kernel.panic_on_oops=1
`
			Expect(string(content)).To(Equal(expectedContent))

			By("verify that applyParams is called")
			Expect(applied).To(BeTrue())
		})
	})

	Context("appendMandatoryParamsToConf", func() {
		It("should skip params that already exist in sysctl.conf with exact match", func() {
			operation := &SetParams{
				etcDirectory: filepath.Join(tempDir, "etc"),
			}
			Expect(os.MkdirAll(operation.etcDirectory, 0755)).To(Succeed())

			By("create sysctl.conf with existing params")
			existingContent := `net.ipv4.ip_forward=1
net.bridge.bridge-nf-call-iptables=1
`
			Expect(os.WriteFile(filepath.Join(operation.etcDirectory, "sysctl.conf"), []byte(existingContent), 0644)).To(Succeed())

			By("append params where some already exist")
			err := operation.appendMandatoryParamsToConf([]string{
				"net.ipv4.ip_forward=1",                 // already exists, should skip
				"net.bridge.bridge-nf-call-iptables=1",  // already exists, should skip
				"net.bridge.bridge-nf-call-ip6tables=1", // new, should append
			})
			Expect(err).NotTo(HaveOccurred())

			By("verify that only new params are appended")
			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(err).NotTo(HaveOccurred())
			expectedContent := `net.ipv4.ip_forward=1
net.bridge.bridge-nf-call-iptables=1
net.bridge.bridge-nf-call-ip6tables=1
`
			Expect(string(content)).To(Equal(expectedContent))
		})

		It("should append params with same key but different value", func() {
			operation := &SetParams{
				etcDirectory: filepath.Join(tempDir, "etc"),
			}
			Expect(os.MkdirAll(operation.etcDirectory, 0755)).To(Succeed())

			By("create sysctl.conf with params having different values")
			existingContent := `net.ipv4.ip_forward=0
`
			Expect(os.WriteFile(filepath.Join(operation.etcDirectory, "sysctl.conf"), []byte(existingContent), 0644)).To(Succeed())

			By("append params with new values")
			err := operation.appendMandatoryParamsToConf([]string{
				"net.ipv4.ip_forward=1", // same key, different value, should append
			})
			Expect(err).NotTo(HaveOccurred())

			By("verify that new value is appended (sysctl uses the last value)")
			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(err).NotTo(HaveOccurred())
			expectedContent := `net.ipv4.ip_forward=0
net.ipv4.ip_forward=1
`
			Expect(string(content)).To(Equal(expectedContent))
		})

		It("should create sysctl.conf if it does not exist", func() {
			operation := &SetParams{
				etcDirectory: filepath.Join(tempDir, "etc"),
			}
			Expect(os.MkdirAll(operation.etcDirectory, 0755)).To(Succeed())

			By("verify sysctl.conf does not exist")
			_, err := os.Stat(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(os.IsNotExist(err)).To(BeTrue())

			By("append params to non-existent sysctl.conf")
			err = operation.appendMandatoryParamsToConf([]string{
				"net.ipv4.ip_forward=1",
				"net.bridge.bridge-nf-call-iptables=1",
			})
			Expect(err).NotTo(HaveOccurred())

			By("verify that sysctl.conf is created with all params")
			content, err := os.ReadFile(filepath.Join(operation.etcDirectory, "sysctl.conf"))
			Expect(err).NotTo(HaveOccurred())
			expectedContent := `net.ipv4.ip_forward=1
net.bridge.bridge-nf-call-iptables=1
`
			Expect(string(content)).To(Equal(expectedContent))
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
			By(fmt.Sprintf("tempDir: %s", tempDir))
			operation := &CheckParams{
				kernelParametersDirectory: filepath.Join(tempDir, "proc", "sys"),
			}
			By("create mock kernel parameters")
			createMockKernelParameters(operation.kernelParametersDirectory, []string{
				"net.ipv4.ip_forward=0",
				"net.bridge.bridge-nf-call-iptables=0",
				"net.bridge.bridge-nf-call-ip6tables=1",
			})
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("sysctl parameters mismatch. Current: net.ipv4.ip_forward=0, net.bridge.bridge-nf-call-iptables=0"))
		})

		It("should return error if user params are not matching", func() {
			By(fmt.Sprintf("tempDir: %s", tempDir))
			operation := &CheckParams{
				kernelParametersDirectory: filepath.Join(tempDir, "proc", "sys"),
			}
			createMockKernelParameters(operation.kernelParametersDirectory, []string{
				"kernel.panic=0",
				"kernel.panic_on_oops=0",
			})
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
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: *flavor,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("sysctl parameters mismatch. Current: kernel.panic=0, kernel.panic_on_oops=0"))
		})
	})
})
