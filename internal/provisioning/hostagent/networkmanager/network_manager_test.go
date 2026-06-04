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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

			err := nm.AddNetworkRequest(dpu)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("network manager is not initialized"))
		})

		It("should return error when DPU is nil", func() {
			nm := NewNetworkManager(nil)
			nm.initialized = true // Bypass initialization check

			err := nm.AddNetworkRequest(nil)
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
			err := nm.AddNetworkRequest(dpu)
			Expect(err).NotTo(HaveOccurred())
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

			err := nm.AddNetworkRequest(dpu)
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

	Context("NetworkManager.lookupDevice", Label("lookupDevice"), func() {
		It("should return device when initialized, device exists, and no existing request", func() {
			nm := NewNetworkManager(nil)
			nm.initialized = true
			nm.devicesBySN["MT2334XZ0L"] = hostutil.Device{
				Address:      "0000:03:00",
				SerialNumber: "MT2334XZ0L",
				NumOfPFs:     2,
			}

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{UID: "uid-1"},
				Spec:       provisioningv1.DPUSpec{SerialNumber: "MT2334XZ0L", NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}}},
			}

			dev, found, err := nm.lookupDevice(dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(dev.Address).To(Equal("0000:03:00"))
		})

		It("should return found=false when request already exists for the UID", func() {
			nm := NewNetworkManager(nil)
			nm.initialized = true
			nm.devicesBySN["MT2334XZ0L"] = hostutil.Device{Address: "0000:03:00", SerialNumber: "MT2334XZ0L"}
			nm.reqs["uid-1"] = NetworkRequest{UID: "uid-1"}

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{UID: "uid-1"},
				Spec:       provisioningv1.DPUSpec{SerialNumber: "MT2334XZ0L", NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}}},
			}

			_, found, err := nm.lookupDevice(dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())
		})

		It("should return error when not initialized", func() {
			nm := NewNetworkManager(nil)

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{UID: "uid-1"},
				Spec:       provisioningv1.DPUSpec{SerialNumber: "MT2334XZ0L", NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}}},
			}

			_, _, err := nm.lookupDevice(dpu)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not initialized"))
		})

		It("should return error when device serial number not in map", func() {
			nm := NewNetworkManager(nil)
			nm.initialized = true

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{UID: "uid-1"},
				Spec:       provisioningv1.DPUSpec{SerialNumber: "UNKNOWN", NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}}},
			}

			_, _, err := nm.lookupDevice(dpu)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Context("NetworkManager.addRequest and removeRequest", Label("addRequest", "removeRequest"), func() {
		It("should insert a request into the map", func() {
			nm := NewNetworkManager(nil)
			nr := &NetworkRequest{UID: "uid-add-1", DpuName: "dpu-1"}

			nm.addRequest(nr)

			Expect(nm.reqs).To(HaveKey("uid-add-1"))
			Expect(nm.reqs["uid-add-1"].DpuName).To(Equal("dpu-1"))
		})

		It("should remove a request from the map", func() {
			nm := NewNetworkManager(nil)
			nm.reqs["uid-rm-1"] = NetworkRequest{UID: "uid-rm-1"}

			nm.removeRequest("uid-rm-1")

			Expect(nm.reqs).NotTo(HaveKey("uid-rm-1"))
		})

		It("should handle removing a non-existent key", func() {
			nm := NewNetworkManager(nil)
			Expect(func() { nm.removeRequest("nonexistent") }).NotTo(Panic())
		})

		It("should be safe under concurrent access", func() {
			nm := NewNetworkManager(nil)
			var wg sync.WaitGroup

			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					uid := fmt.Sprintf("uid-%d", i)
					nm.addRequest(&NetworkRequest{UID: uid})
				}(i)
			}
			wg.Wait()
			Expect(nm.reqs).To(HaveLen(50))

			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					nm.removeRequest(fmt.Sprintf("uid-%d", i))
				}(i)
			}
			wg.Wait()
			Expect(nm.reqs).To(BeEmpty())
		})
	})

	Context("NetworkManager.processNetworkRequest", Label("processNetworkRequest"), func() {
		var (
			testScheme            *runtime.Scheme
			tempDir               string
			origNetworkRequestDir string
		)

		BeforeEach(func() {
			testScheme = runtime.NewScheme()
			Expect(provisioningv1.AddToScheme(testScheme)).To(Succeed())
			Expect(operatorv1.AddToScheme(testScheme)).To(Succeed())

			var err error
			tempDir, err = os.MkdirTemp("", "process-test-*")
			Expect(err).NotTo(HaveOccurred())
			origNetworkRequestDir = NetworkRequestDir
			NetworkRequestDir = tempDir
		})

		AfterEach(func() {
			NetworkRequestDir = origNetworkRequestDir
			_ = os.RemoveAll(tempDir)
		})

		It("should clean up when DPU is not found", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
			nm := NewNetworkManager(fakeClient)

			nr := NetworkRequest{
				UID:          "uid-gone",
				DpuName:      "nonexistent",
				DPUNamespace: "default",
				VFName:       "nonexistent-vf",
			}
			nm.reqs[nr.UID] = nr

			data, err := json.Marshal(&nr)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(tempDir, nr.UID), data, 0644)).To(Succeed())

			err = nm.processNetworkRequest(nr)
			Expect(err).NotTo(HaveOccurred())
			Expect(nm.reqs).NotTo(HaveKey("uid-gone"))
			Expect(filepath.Join(tempDir, nr.UID)).NotTo(BeAnExistingFile())
		})

		It("should clean up when UID does not match", func() {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "default",
					UID:       "uid-actual",
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(dpu).Build()
			nm := NewNetworkManager(fakeClient)

			nr := NetworkRequest{
				UID:          "uid-stale",
				DpuName:      "test-dpu",
				DPUNamespace: "default",
				VFName:       "nonexistent-vf",
			}
			nm.reqs[nr.UID] = nr

			err := nm.processNetworkRequest(nr)
			Expect(err).NotTo(HaveOccurred())
			Expect(nm.reqs).NotTo(HaveKey("uid-stale"))
		})
	})

	Context("Concurrency: AddNetworkRequest not blocked by run()", Label("concurrency"), func() {
		var (
			testScheme            *runtime.Scheme
			tempDir               string
			origNetworkRequestDir string
		)

		BeforeEach(func() {
			testScheme = runtime.NewScheme()
			Expect(provisioningv1.AddToScheme(testScheme)).To(Succeed())
			Expect(operatorv1.AddToScheme(testScheme)).To(Succeed())

			var err error
			tempDir, err = os.MkdirTemp("", "concurrency-test-*")
			Expect(err).NotTo(HaveOccurred())
			origNetworkRequestDir = NetworkRequestDir
			NetworkRequestDir = tempDir
		})

		AfterEach(func() {
			NetworkRequestDir = origNetworkRequestDir
			_ = os.RemoveAll(tempDir)
		})

		It("should not block AddNetworkRequest while run() is processing", func() {
			existingDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slow-dpu",
					Namespace: "default",
					UID:       "uid-slow",
				},
			}
			newDPU := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fast-dpu",
					Namespace: "default",
					UID:       "uid-fast",
				},
				Spec: provisioningv1.DPUSpec{
					SerialNumber: "SN-FAST",
					DPUFlavor:    "test-flavor",
					NodeEffect:   provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				},
			}
			flavor := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{Name: "test-flavor", Namespace: "default"},
			}
			mtu := 1500
			dpfConfig := &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "default"},
				Spec: operatorv1.DPFOperatorConfigSpec{
					Networking: &operatorv1.Networking{
						ControlPlaneMTU: &mtu,
					},
				},
			}

			slowClient := &slowGetClient{
				Client:  fake.NewClientBuilder().WithScheme(testScheme).WithObjects(existingDPU, newDPU, flavor, dpfConfig).Build(),
				delay:   500 * time.Millisecond,
				slowKey: types.NamespacedName{Name: "slow-dpu", Namespace: "default"},
			}

			nm := NewNetworkManager(slowClient)
			nm.initialized = true
			nm.devicesBySN["SN-FAST"] = hostutil.Device{Address: "0000:04:00", SerialNumber: "SN-FAST"}

			nm.reqs["uid-slow"] = NetworkRequest{
				UID:          "uid-slow",
				DpuName:      "slow-dpu",
				DPUNamespace: "default",
			}

			go nm.run()

			done := make(chan error, 1)
			go func() {
				done <- nm.AddNetworkRequest(newDPU)
			}()

			select {
			case err := <-done:
				Expect(err).NotTo(HaveOccurred())
				Expect(nm.reqs).To(HaveKey("uid-fast"))
			case <-time.After(200 * time.Millisecond):
				Fail("AddNetworkRequest was blocked by run() — lock contention detected")
			}
		})
	})
})

// slowGetClient wraps a real client and injects a delay on Get calls for a
// specific object, simulating a slow API server for concurrency tests.
type slowGetClient struct {
	client.Client
	delay   time.Duration
	slowKey types.NamespacedName
}

func (s *slowGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if key == s.slowKey {
		time.Sleep(s.delay)
	}
	return s.Client.Get(ctx, key, obj, opts...)
}
