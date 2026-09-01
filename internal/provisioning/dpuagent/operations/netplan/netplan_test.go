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

package netplan

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var _ = Describe("Netplan", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "netplan-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("configure network", func() {
		It("should skip if SkipNetworkConfig is true", func() {
			operation := &ConfigureNetwork{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipNetworkConfig: true,
				},
			})).To(BeTrue())
		})

		It("[ZeroTrustMode] should create files and apply netplan", func() {
			mockFile := filepath.Join(tempDir, "60-mlnx.yaml")
			Expect(os.MkdirAll(filepath.Dir(mockFile), 0755)).To(Succeed())
			Expect(os.WriteFile(mockFile, []byte(""), 0644)).To(Succeed())
			_, err := os.Stat(mockFile)
			Expect(err).NotTo(HaveOccurred())

			applied := false
			operation := &ConfigureNetwork{
				netplanRoot: tempDir,
				applyNetplanFunc: func() error {
					applied = true
					return nil
				},
			}
			Expect(operation.Execute(ctx, &operations.Context{
				Options: opts.Options{
					ZeroTrustMode: true,
				},
			})).To(Succeed())

			_, err = os.Stat(mockFile)
			Expect(os.IsNotExist(err)).To(BeTrue())

			tmfifoFile := filepath.Join(tempDir, "98-oob-tmfifo.yaml")
			_, err = os.Stat(tmfifoFile)
			Expect(err).NotTo(HaveOccurred())

			By("verifying tmfifo_net0 is configured with IPv6 link-local address")
			content, err := os.ReadFile(tmfifoFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(ContainSubstring("tmfifo_net0"))
			Expect(string(content)).To(ContainSubstring("fe80::2/64"))
			Expect(string(content)).To(ContainSubstring("oob_net0"))
			Expect(string(content)).To(ContainSubstring("dhcp6: false"))
			Expect(string(content)).To(ContainSubstring("accept-ra: false"))

			// 99-dpf-comm-ch.yaml is created only if ZeroTrustMode is false
			_, err = os.Stat(filepath.Join(tempDir, "99-dpf-comm-ch.yaml"))
			Expect(os.IsNotExist(err)).To(BeTrue())

			Expect(applied).To(BeTrue())

		})

		It("[TrustedHost] should create files and apply netplan", func() {
			mockFile := filepath.Join(tempDir, "60-mlnx.yaml")
			Expect(os.MkdirAll(filepath.Dir(mockFile), 0755)).To(Succeed())
			Expect(os.WriteFile(mockFile, []byte(""), 0644)).To(Succeed())
			_, err := os.Stat(mockFile)
			Expect(err).NotTo(HaveOccurred())

			applied := false
			operation := &ConfigureNetwork{
				netplanRoot: tempDir,
				applyNetplanFunc: func() error {
					applied = true
					return nil
				},
			}
			Expect(operation.Execute(ctx, &operations.Context{
				Options: opts.Options{
					ZeroTrustMode: false,
				},
			})).To(Succeed())

			_, err = os.Stat(mockFile)
			Expect(os.IsNotExist(err)).To(BeTrue())

			tmfifoFile := filepath.Join(tempDir, "98-oob-tmfifo.yaml")
			_, err = os.Stat(tmfifoFile)
			Expect(err).NotTo(HaveOccurred())

			By("verifying tmfifo_net0 is configured with IPv6 link-local address")
			content, err := os.ReadFile(tmfifoFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(ContainSubstring("tmfifo_net0"))
			Expect(string(content)).To(ContainSubstring("fe80::2/64"))

			_, err = os.Stat(filepath.Join(tempDir, "99-dpf-comm-ch.yaml"))
			Expect(err).NotTo(HaveOccurred())
			content, err = os.ReadFile(filepath.Join(tempDir, "99-dpf-comm-ch.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(ContainSubstring("pf0vf0"))

			Expect(applied).To(BeTrue())
		})

	})

	Context("check network", func() {
		It("should never be skipped", func() {
			operation := &CheckNetwork{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipNetworkConfig: true,
				},
			})).To(BeFalse())
		})

		It("should return error if API server is not reachable", func() {
			fakeCS := k8sfake.NewClientset()
			fakeCS.Discovery().(*fakediscovery.FakeDiscovery).PrependReactor("*", "*", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
				return true, nil, fmt.Errorf("connection refused")
			})
			operation := &CheckNetwork{}
			Expect(operation.Execute(ctx, &operations.Context{
				K8sClient: fakeCS,
			})).To(HaveOccurred())
		})

		It("should succeed when API server is reachable", func() {
			operation := &CheckNetwork{}
			Expect(operation.Execute(ctx, &operations.Context{
				K8sClient: k8sfake.NewClientset(),
			})).To(Succeed())
		})
	})
})
