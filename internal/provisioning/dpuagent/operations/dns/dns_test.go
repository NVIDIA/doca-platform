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

package dns

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mockRunBashSuccess(cmd string) (bytes.Buffer, bytes.Buffer, error) {
	return bytes.Buffer{}, bytes.Buffer{}, nil
}

func mockRunBashFailure(cmd string) (bytes.Buffer, bytes.Buffer, error) {
	return bytes.Buffer{}, bytes.Buffer{}, errors.New("mock bash error")
}

var _ = Describe("DNS", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "dns-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("Configure DNS", func() {
		It("should skip if SkipDNSConfig is true", func() {
			operation := &ConfigureDNS{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipDNSConfig: true,
				},
			})).To(BeTrue())
		})

		It("should not skip if LatestDPU is nil", func() {
			operation := &ConfigureDNS{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
				LatestDPU: nil,
			})).To(BeFalse())
		})

		It("should not skip if DPUInternalStatus is nil", func() {
			operation := &ConfigureDNS{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
				LatestDPU: &provisioningv1.DPU{},
			})).To(BeFalse())
		})

		It("should skip if DNSConfigured condition is true", func() {
			operation := &ConfigureDNS{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						DPUInternalStatus: &provisioningv1.DPUInternalStatus{
							Conditions: []metav1.Condition{
								{
									Type:   CondDNSConfigured,
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			})).To(BeTrue())
		})

		It("should not skip if DNSConfigured condition is false", func() {
			operation := &ConfigureDNS{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						DPUInternalStatus: &provisioningv1.DPUInternalStatus{
							Conditions: []metav1.Condition{
								{
									Type:   CondDNSConfigured,
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
				},
			})).To(BeFalse())
		})

		It("should not skip if DNSConfigured condition does not exist", func() {
			operation := &ConfigureDNS{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						DPUInternalStatus: &provisioningv1.DPUInternalStatus{
							Conditions: []metav1.Condition{},
						},
					},
				},
			})).To(BeFalse())
		})

		It("should execute all DNS configuration steps", func() {
			// Create a fake /etc/resolv.conf in the temp directory
			resolvConfDir := filepath.Join(tempDir, "etc")
			Expect(os.MkdirAll(resolvConfDir, 0755)).To(Succeed())
			originalContent := "nameserver 8.8.8.8\n"
			Expect(os.WriteFile(filepath.Join(resolvConfDir, "resolv.conf"), []byte(originalContent), 0644)).To(Succeed())

			var receivedCmd string
			operation := &ConfigureDNS{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					receivedCmd = cmd
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
				LatestDPU: &provisioningv1.DPU{},
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking config files are created")
			expectedFiles := []struct {
				path    string
				content string
			}{
				{
					path:    filepath.Join(tempDir, resolvedConfPath),
					content: resolvedConfContent,
				},
				{
					path:    filepath.Join(tempDir, nmConfPath),
					content: nmConfContent,
				},
			}
			for _, file := range expectedFiles {
				By(fmt.Sprintf("checking file %s", file.path))
				content, err := os.ReadFile(file.path)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal(file.content))
			}

			By("checking resolv.conf backup exists with original content")
			bakContent, err := os.ReadFile(filepath.Join(tempDir, resolvConfBakPath))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(bakContent)).To(Equal(originalContent))

			By("checking resolv.conf is a symlink to systemd-resolved")
			resolvConf := filepath.Join(tempDir, resolvConfPath)
			linkTarget, err := os.Readlink(resolvConf)
			Expect(err).NotTo(HaveOccurred())
			Expect(linkTarget).To(Equal(resolvConfTarget))

			By("checking dnsmasq was masked")
			Expect(receivedCmd).To(Equal("systemctl mask dnsmasq"))
		})

		It("should fail if systemctl mask dnsmasq fails", func() {
			// Create a fake /etc/resolv.conf in the temp directory
			resolvConfDir := filepath.Join(tempDir, "etc")
			Expect(os.MkdirAll(resolvConfDir, 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(resolvConfDir, "resolv.conf"), []byte("nameserver 8.8.8.8\n"), 0644)).To(Succeed())

			operation := &ConfigureDNS{
				rootFS:  tempDir,
				runBash: mockRunBashFailure,
			}
			err := operation.Execute(ctx, &operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
				LatestDPU: &provisioningv1.DPU{},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to mask dnsmasq"))
		})

		It("should succeed if resolv.conf does not exist", func() {
			// Do not create /etc/resolv.conf - rename should be silently skipped
			operation := &ConfigureDNS{
				rootFS:  tempDir,
				runBash: mockRunBashSuccess,
			}
			err := operation.Execute(ctx, &operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
				LatestDPU: &provisioningv1.DPU{},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail if LatestDPU is nil", func() {
			operation := &ConfigureDNS{
				rootFS:  tempDir,
				runBash: mockRunBashSuccess,
			}
			err := operation.Execute(ctx, &operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
				LatestDPU: nil,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("latest DPU not retrieved"))
		})
	})
})
