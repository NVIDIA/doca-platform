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
	"context"
	"fmt"
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Netplan", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "netplan-test-*")
		Expect(err).NotTo(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(tempDir, "bus/pci/devices/0000:00:00.0"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tempDir, "bus/pci/devices/0000:00:00.0/device"), []byte("0xa2dc\n"), 0644)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(tempDir, "bus/pci/devices/0000:00:00.0/net"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tempDir, "bus/pci/devices/0000:00:00.0/net/eth0"), []byte(""), 0644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tempDir, "bus/pci/devices/0000:00:00.0/vpd"), []byte(""), 0644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tempDir, "bus/pci/devices/0000:00:00.0/mtu"), []byte("9000"), 0644)).To(Succeed())
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
				sysFSRoot:   tempDir,
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
				Client: &mockClient{},
			})).To(Succeed())

			_, err = os.Stat(mockFile)
			Expect(os.IsNotExist(err)).To(BeTrue())

			_, err = os.Stat(filepath.Join(tempDir, "98-oob-tmfifo.yaml"))
			Expect(err).NotTo(HaveOccurred())

			// 99-dpf-comm-ch.yaml is created only if ZeroTrustMode is false
			_, err = os.Stat(filepath.Join(tempDir, "99-dpf-comm-ch.yaml"))
			Expect(os.IsNotExist(err)).To(BeTrue())

			_, err = os.Stat(filepath.Join(tempDir, "97-pf-mtu.yaml"))
			Expect(err).NotTo(HaveOccurred())

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
				sysFSRoot:   tempDir,
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
				Client: &mockClient{},
			})).To(Succeed())

			_, err = os.Stat(mockFile)
			Expect(os.IsNotExist(err)).To(BeTrue())

			_, err = os.Stat(filepath.Join(tempDir, "98-oob-tmfifo.yaml"))
			Expect(err).NotTo(HaveOccurred())

			_, err = os.Stat(filepath.Join(tempDir, "99-dpf-comm-ch.yaml"))
			Expect(err).NotTo(HaveOccurred())

			_, err = os.Stat(filepath.Join(tempDir, "97-pf-mtu.yaml"))
			Expect(err).NotTo(HaveOccurred())

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

		It("should return error if network is not healthy", func() {
			operation := &CheckNetwork{}
			Expect(operation.Execute(ctx, &operations.Context{
				Options: opts.Options{
					SkipNetworkConfig: true,
				},
				Client: &mockClient{shouldFail: true},
			})).To(HaveOccurred())
		})

		It("should succeed if network is healthy", func() {
			operation := &CheckNetwork{}
			Expect(operation.Execute(ctx, &operations.Context{
				Options: opts.Options{
					SkipNetworkConfig: true,
				},
				Client: &mockClient{shouldFail: false},
			})).To(Succeed())
		})
	})
})

type mockClient struct {
	shouldFail bool
}

func (c *mockClient) HealthCheck() error {
	if c.shouldFail {
		return fmt.Errorf("network is not healthy")
	}
	return nil
}

func (c *mockClient) UpdateStatus(ctx context.Context, status provisioningv1.DPUInternalStatus) error {
	if c.shouldFail {
		return fmt.Errorf("failed to update status")
	}
	return nil
}

func (c *mockClient) GetObject(ctx context.Context, namespace string, name string, obj client.Object) error {
	if c.shouldFail {
		return fmt.Errorf("failed to get object")
	}
	return nil
}
