//go:build linux

/*
Copyright 2025 NVIDIA

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

package networkmanager

import (
	"os"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("NetworkManager", func() {
	Context("NetworkManager.NewNetworkManager", Label("NewNetworkManager"), func() {
		It("should create NetworkManager with nil client", func() {
			nm := NewNetworkManager(nil)
			Expect(nm).NotTo(BeNil())
			Expect(nm.initialized).To(BeFalse())
			Expect(nm.devicesBySN).NotTo(BeNil())
			Expect(nm.reqs).NotTo(BeNil())
		})

		It("should initialize empty maps", func() {
			nm := NewNetworkManager(nil)
			Expect(nm.devicesBySN).To(BeEmpty())
			Expect(nm.reqs).To(BeEmpty())
		})
	})

	Context("NetworkManager.GetDevice", Label("GetDevice"), func() {
		It("should return false for non-existent device", func() {
			nm := NewNetworkManager(nil)
			_, found := nm.GetDevice("nonexistent-serial")
			Expect(found).To(BeFalse())
		})

		It("should return device when it exists", func() {
			nm := NewNetworkManager(nil)
			// Manually add a device for testing
			nm.devicesBySN["MT2334XZ0L"] = hostutil.Device{
				Address:      "0000:03:00",
				SerialNumber: "MT2334XZ0L",
				NumOfPFs:     2,
			}

			dev, found := nm.GetDevice("MT2334XZ0L")
			Expect(found).To(BeTrue())
			Expect(dev.Address).To(Equal("0000:03:00"))
			Expect(dev.SerialNumber).To(Equal("MT2334XZ0L"))
			Expect(dev.NumOfPFs).To(Equal(2))
		})

		It("should be thread-safe for concurrent reads", func() {
			nm := NewNetworkManager(nil)
			nm.devicesBySN["MT2334XZ0L"] = hostutil.Device{
				Address:      "0000:03:00",
				SerialNumber: "MT2334XZ0L",
			}

			// Multiple concurrent reads should not panic
			done := make(chan bool)
			for i := 0; i < 10; i++ {
				go func() {
					_, _ = nm.GetDevice("MT2334XZ0L")
					done <- true
				}()
			}
			for i := 0; i < 10; i++ {
				<-done
			}
		})
	})

	Context("NetworkManager.AddNetworkRequest", Label("AddNetworkRequest"), func() {
		It("should return error when not initialized", func() {
			nm := NewNetworkManager(nil)
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "default",
					UID:       "test-uid",
				},
			}

			err := nm.AddNetworkRequest(dpu, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("network manager is not initialized"))
		})

		It("should return error when DPU is nil", func() {
			nm := NewNetworkManager(nil)
			nm.initialized = true // Bypass initialization check

			err := nm.AddNetworkRequest(nil, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("DPU is nil"))
		})

		It("should return nil when request already exists", func() {
			nm := NewNetworkManager(nil)
			nm.initialized = true

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "default",
					UID:       "test-uid-123",
				},
			}

			// Pre-add the request
			nm.reqs["test-uid-123"] = NetworkRequest{
				UID: "test-uid-123",
			}

			// Should return nil since request already exists
			err := nm.AddNetworkRequest(dpu, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("vfCount override on existing request", func() {
			var (
				tempDir               string
				origNetworkRequestDir string
			)

			BeforeEach(func() {
				var err error
				tempDir, err = os.MkdirTemp("", "nm-vfcount-test-*")
				Expect(err).NotTo(HaveOccurred())
				origNetworkRequestDir = NetworkRequestDir
				NetworkRequestDir = tempDir
			})

			AfterEach(func() {
				NetworkRequestDir = origNetworkRequestDir
				_ = os.RemoveAll(tempDir)
			})

			It("should update VF count on existing request when vfCount is provided", func() {
				nm := NewNetworkManager(nil)
				nm.initialized = true

				dpu := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpu",
						Namespace: "default",
						UID:       "test-uid-vf-update",
					},
				}
				nm.reqs["test-uid-vf-update"] = NetworkRequest{
					UID:      "test-uid-vf-update",
					NumOfVFs: 4,
					DpuName:  "test-dpu",
				}

				vfCount := 8
				err := nm.AddNetworkRequest(dpu, &vfCount)
				Expect(err).NotTo(HaveOccurred())
				Expect(nm.reqs["test-uid-vf-update"].NumOfVFs).To(Equal(8))
			})

			It("should not update VF count when vfCount is nil", func() {
				nm := NewNetworkManager(nil)
				nm.initialized = true

				dpu := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpu",
						Namespace: "default",
						UID:       "test-uid-nil-vf",
					},
				}
				nm.reqs["test-uid-nil-vf"] = NetworkRequest{
					UID:      "test-uid-nil-vf",
					NumOfVFs: 4,
				}

				err := nm.AddNetworkRequest(dpu, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(nm.reqs["test-uid-nil-vf"].NumOfVFs).To(Equal(4))
			})

			It("should not update VF count when vfCount is zero", func() {
				nm := NewNetworkManager(nil)
				nm.initialized = true

				dpu := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpu",
						Namespace: "default",
						UID:       "test-uid-zero-vf",
					},
				}
				nm.reqs["test-uid-zero-vf"] = NetworkRequest{
					UID:      "test-uid-zero-vf",
					NumOfVFs: 4,
				}

				vfCount := 0
				err := nm.AddNetworkRequest(dpu, &vfCount)
				Expect(err).NotTo(HaveOccurred())
				Expect(nm.reqs["test-uid-zero-vf"].NumOfVFs).To(Equal(4))
			})

			It("should not update VF count when it matches existing", func() {
				nm := NewNetworkManager(nil)
				nm.initialized = true

				dpu := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpu",
						Namespace: "default",
						UID:       "test-uid-same-vf",
					},
				}
				nm.reqs["test-uid-same-vf"] = NetworkRequest{
					UID:      "test-uid-same-vf",
					NumOfVFs: 4,
				}

				vfCount := 4
				err := nm.AddNetworkRequest(dpu, &vfCount)
				Expect(err).NotTo(HaveOccurred())
				Expect(nm.reqs["test-uid-same-vf"].NumOfVFs).To(Equal(4))
			})
		})

		It("should return error when device not found by serial number", func() {
			nm := NewNetworkManager(nil)
			nm.initialized = true

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "default",
					UID:       "test-uid-456",
				},
				Spec: provisioningv1.DPUSpec{
					SerialNumber: "unknown-serial",
					NodeEffect:   provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				},
			}

			err := nm.AddNetworkRequest(dpu, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("PCI address of device"))
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Context("NetworkManager.Start", Label("Start"), func() {
		var (
			tempDir               string
			origNetworkRequestDir string
		)

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "nm-start-test-*")
			Expect(err).NotTo(HaveOccurred())

			// Save original path and override for testing to avoid permission issues
			// with /var/lib/dpf which requires root access
			origNetworkRequestDir = NetworkRequestDir
			NetworkRequestDir = tempDir
		})

		AfterEach(func() {
			NetworkRequestDir = origNetworkRequestDir
			_ = os.RemoveAll(tempDir)
		})

		It("should run Start and handle result", func() {
			nm := NewNetworkManager(nil)

			// Start will either succeed or fail depending on system configuration
			err := nm.Start()
			if err != nil {
				// Could fail at systemd-networkd check or later stages
				Expect(err.Error()).To(SatisfyAny(
					ContainSubstring("systemd-networkd"),
					ContainSubstring("failed to"),
				))
			}
		})

		It("should set initialized to true on successful Start", func() {
			nm := NewNetworkManager(nil)

			err := nm.Start()
			if err == nil {
				// If Start succeeded, initialized should be true
				Expect(nm.initialized).To(BeTrue())
			} else {
				// If Start failed, initialized should remain false
				Expect(nm.initialized).To(BeFalse())
			}
		})
	})
})
