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

package reboot

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
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

func fakeClientWithDPUs(dpus []provisioningv1.DPU) client.Client {
	scheme := runtime.NewScheme()
	if err := provisioningv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	b := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&provisioningv1.DPU{})
	for i := range dpus {
		d := dpus[i].DeepCopy()
		b = b.WithObjects(d)
	}
	return b.Build()
}

var _ = Describe("Reboot Sync", func() {
	Context("When the DPU is being rebooted", func() {

		var mockGetDeviceBySerialNumberFunc = func() func(serialNumber string) (hostutil.Device, bool) {
			return func(serialNumber string) (hostutil.Device, bool) {
				return hostutil.Device{
					Address: fmt.Sprintf("pci-address-%s", serialNumber),
				}, true
			}
		}

		var mockListDPUFunc = func(dpus []provisioningv1.DPU) func(context.Context) ([]provisioningv1.DPU, error) {
			return func(ctx context.Context) ([]provisioningv1.DPU, error) {
				return dpus, nil
			}
		}

		var mockGetDPUNodeFunc = func(dpuNode *provisioningv1.DPUNode) func(context.Context) (*provisioningv1.DPUNode, error) {
			return func(ctx context.Context) (*provisioningv1.DPUNode, error) {
				return dpuNode, nil
			}
		}

		var mockPersistDPUBootIDFunc = func(store map[types.NamespacedName]bool) func(*provisioningv1.DPU) error {
			return func(dpu *provisioningv1.DPU) error {
				store[types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Name}] = true
				return nil
			}
		}

		var mockGetRshimNameByPCIFunc = func() func(string) (string, error) {
			return func(PCIAddress string) (string, error) {
				return fmt.Sprintf("rshim-%s", PCIAddress), nil
			}
		}

		var dpuNodeWithHostAgentRebootMethod = func() *provisioningv1.DPUNode {
			return &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-node",
					Namespace: hostutil.DPFNamespace,
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						HostAgent: &provisioningv1.HostAgent{},
					},
				},
			}
		}

		var dpuNodeWithExternalRebootMethod = func() *provisioningv1.DPUNode {
			return &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-node",
					Namespace: hostutil.DPFNamespace,
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						External: &provisioningv1.External{},
					},
				},
			}
		}

		var dpuNodeWithScriptRebootMethod = func() *provisioningv1.DPUNode {
			return &provisioningv1.DPUNode{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-node",
					Namespace: hostutil.DPFNamespace,
				},
				Spec: provisioningv1.DPUNodeSpec{
					NodeRebootMethod: &provisioningv1.NodeRebootMethod{
						Script: &provisioningv1.Script{
							Name: "test-script",
						},
					},
				},
			}
		}

		var rebootingDPU = func(name string) provisioningv1.DPU {
			return provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: hostutil.DPFNamespace,
					UID:       types.UID(fmt.Sprintf("uid-%s", name)),
				},
				Spec: provisioningv1.DPUSpec{
					SerialNumber: fmt.Sprintf("serial-number-%s", name),
					NodeEffect:   provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				},
				Status: provisioningv1.DPUStatus{
					Phase: provisioningv1.DPURebooting,
				},
			}
		}

		var nonRebootingDPU = func(phase provisioningv1.DPUPhase) provisioningv1.DPU {
			name := fmt.Sprintf("non-rebooting-dpu-%s", phase)
			return provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: hostutil.DPFNamespace,
					UID:       types.UID(fmt.Sprintf("uid-%s", name)),
				},
				Spec: provisioningv1.DPUSpec{
					SerialNumber: fmt.Sprintf("serial-number-%s", name),
					NodeEffect:   provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				},
				Status: provisioningv1.DPUStatus{
					Phase: phase,
				},
			}
		}

		It("do nothing if reboot method is external", func() {
			dpuNode := dpuNodeWithExternalRebootMethod()
			rbDPU := rebootingDPU("rebooting-dpu")
			nonRebootingDPU := nonRebootingDPU(provisioningv1.DPUInitializeInterface)
			dpus := []provisioningv1.DPU{
				rbDPU,
				nonRebootingDPU,
			}
			store := map[types.NamespacedName]bool{}
			handler := &Handler{
				Client:         fakeClientWithDPUs(dpus),
				listDPUFunc:    mockListDPUFunc(dpus),
				getDPUNodeFunc: mockGetDPUNodeFunc(dpuNode),
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return false, nil
					},
				},
			}
			result := handler.run()
			Expect(result).To(BeEmpty())
			Expect(store).To(BeEmpty())
		})

		It("do nothing if reboot method is script", func() {
			dpuNode := dpuNodeWithScriptRebootMethod()
			rbDPU := rebootingDPU("rebooting-dpu")
			nonRebootingDPU := nonRebootingDPU(provisioningv1.DPUConfigFWParameters)
			dpus := []provisioningv1.DPU{
				rbDPU,
				nonRebootingDPU,
			}
			store := map[types.NamespacedName]bool{}
			handler := &Handler{
				Client:         fakeClientWithDPUs(dpus),
				listDPUFunc:    mockListDPUFunc(dpus),
				getDPUNodeFunc: mockGetDPUNodeFunc(dpuNode),
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return false, nil
					},
				},
			}
			result := handler.run()
			Expect(result).To(BeEmpty())
			Expect(store).To(BeEmpty())
		})

		It("should block if any DPU is being provisioned (not reaching DPURebooting phase)", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			rbDPU := rebootingDPU("rebooting-dpu")
			nonRebootingDPU := nonRebootingDPU(provisioningv1.DPUOSInstalling)
			dpus := []provisioningv1.DPU{
				rbDPU,
				nonRebootingDPU,
			}
			store := map[types.NamespacedName]bool{}
			handler := &Handler{
				Client:         fakeClientWithDPUs(dpus),
				listDPUFunc:    mockListDPUFunc(dpus),
				getDPUNodeFunc: mockGetDPUNodeFunc(dpuNode),
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return false, nil
					},
				},
			}
			handler.run()
			updated := &provisioningv1.DPU{}
			Expect(handler.Get(context.Background(), types.NamespacedName{Namespace: rbDPU.Namespace, Name: rbDPU.Name}, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(provisioningv1.DPURebooting))
			Expect(updated.Status.RebootStatus).NotTo(BeNil())
			Expect(updated.Status.RebootStatus.Phase).To(Equal(provisioningv1.RebootStatusFailed))
			Expect(updated.Status.RebootStatus.Reason).To(Equal(failReason))
		})

		It("should run power cycle if any DPU requires it", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			dpuRequiresPowerCycle := rebootingDPU("rebooting-dpu-power-cycle")
			dpuRequiresPowerCycle.Annotations = map[string]string{
				reboot.HostPowerCycleRequireKey: "true",
			}
			dpuRequiresSLR := rebootingDPU("rebooting-dpu-slr")
			dpus := []provisioningv1.DPU{
				dpuRequiresPowerCycle,
				dpuRequiresSLR,
			}
			store := map[types.NamespacedName]bool{}
			powerCycleCnt := 0
			SLRCount := 0
			handler := &Handler{
				Client:                      fakeClientWithDPUs(dpus),
				getDeviceBySerialNumberFunc: mockGetDeviceBySerialNumberFunc(),
				getRshimNameByPCIFunc:       mockGetRshimNameByPCIFunc(),
				listDPUFunc:                 mockListDPUFunc(dpus),
				getDPUNodeFunc:              mockGetDPUNodeFunc(dpuNode),
				runPowerCycleCmdFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					powerCycleCnt++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				runShutdownARMFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					SLRCount++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return false, nil
					},
				},
			}
			failures := handler.run()
			Expect(failures).To(BeEmpty())
			Expect(powerCycleCnt).To(Equal(1))
			Expect(SLRCount).To(Equal(0))
		})

		It("should run ipmitool power cycle (default command)", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			dpuRequiresPowerCycle := rebootingDPU("rebooting-dpu-power-cycle")
			dpuRequiresPowerCycle.Annotations = map[string]string{
				reboot.HostPowerCycleRequireKey: "true",
			}
			dpuRequiresSLR := rebootingDPU("rebooting-dpu-slr")
			dpus := []provisioningv1.DPU{
				dpuRequiresPowerCycle,
				dpuRequiresSLR,
			}
			store := map[types.NamespacedName]bool{}
			powerCycleCnt := 0
			SLRCount := 0
			handler := &Handler{
				Client:                      fakeClientWithDPUs(dpus),
				getDeviceBySerialNumberFunc: mockGetDeviceBySerialNumberFunc(),
				getRshimNameByPCIFunc:       mockGetRshimNameByPCIFunc(),
				listDPUFunc:                 mockListDPUFunc(dpus),
				getDPUNodeFunc:              mockGetDPUNodeFunc(dpuNode),
				runPowerCycleCmdFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					powerCycleCnt++
					Expect(cmd).To(Equal("ipmitool chassis power cycle"))
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				runShutdownARMFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					SLRCount++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return false, nil
					},
				},
			}
			failures := handler.run()
			Expect(failures).To(BeEmpty())
			Expect(powerCycleCnt).To(Equal(1))
			Expect(SLRCount).To(Equal(0))
		})

		It("should run ipmitool power cycle", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			dpuRequiresPowerCycle := rebootingDPU("rebooting-dpu-power-cycle")
			dpuRequiresPowerCycle.Annotations = map[string]string{
				reboot.HostPowerCycleRequireKey: "true",
				reboot.PowercycleCmdKey:         reboot.Cycle,
			}
			dpuRequiresSLR := rebootingDPU("rebooting-dpu-slr")
			dpus := []provisioningv1.DPU{
				dpuRequiresPowerCycle,
				dpuRequiresSLR,
			}
			store := map[types.NamespacedName]bool{}
			powerCycleCnt := 0
			SLRCount := 0
			handler := &Handler{
				Client:                      fakeClientWithDPUs(dpus),
				getDeviceBySerialNumberFunc: mockGetDeviceBySerialNumberFunc(),
				getRshimNameByPCIFunc:       mockGetRshimNameByPCIFunc(),
				listDPUFunc:                 mockListDPUFunc(dpus),
				getDPUNodeFunc:              mockGetDPUNodeFunc(dpuNode),
				runPowerCycleCmdFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					powerCycleCnt++
					Expect(cmd).To(Equal("ipmitool chassis power cycle"))
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				runShutdownARMFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					SLRCount++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return false, nil
					},
				},
			}
			failures := handler.run()
			Expect(failures).To(BeEmpty())
			Expect(powerCycleCnt).To(Equal(1))
			Expect(SLRCount).To(Equal(0))
		})

		It("should run ipmitool power reset", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			dpuNode.Annotations = map[string]string{
				reboot.PowercycleCmdKey: reboot.Reset,
			}
			dpuRequiresPowerCycle := rebootingDPU("rebooting-dpu-power-cycle")
			dpuRequiresPowerCycle.Annotations = map[string]string{
				reboot.HostPowerCycleRequireKey: "true",
			}
			dpuRequiresSLR := rebootingDPU("rebooting-dpu-slr")
			dpus := []provisioningv1.DPU{
				dpuRequiresPowerCycle,
				dpuRequiresSLR,
			}
			store := map[types.NamespacedName]bool{}
			powerCycleCnt := 0
			SLRCount := 0
			handler := &Handler{
				Client:                      fakeClientWithDPUs(dpus),
				getDeviceBySerialNumberFunc: mockGetDeviceBySerialNumberFunc(),
				getRshimNameByPCIFunc:       mockGetRshimNameByPCIFunc(),
				listDPUFunc:                 mockListDPUFunc(dpus),
				getDPUNodeFunc:              mockGetDPUNodeFunc(dpuNode),
				runPowerCycleCmdFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					powerCycleCnt++
					Expect(cmd).To(Equal("ipmitool chassis power reset"))
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				runShutdownARMFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					SLRCount++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return false, nil
					},
				},
			}
			failures := handler.run()
			Expect(failures).To(BeEmpty())
			Expect(powerCycleCnt).To(Equal(1))
			Expect(SLRCount).To(Equal(0))
		})

		It("should run SLR if no DPU requires power cycle", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			dpuRequiresSLR1 := rebootingDPU("slr-1")
			dpuRequiresSLR2 := rebootingDPU("slr-2")
			dpus := []provisioningv1.DPU{
				dpuRequiresSLR1,
				dpuRequiresSLR2,
			}
			store := map[types.NamespacedName]bool{}
			powerCycleCnt := 0
			shutDownARMCountMutex := sync.Mutex{}
			shutDownARMCount := 0
			rebootHostCount := 0
			dpuOffCheckRecordMutex := sync.Mutex{}
			dpuOffCheckRecord := make(map[string]struct{})
			handler := &Handler{
				Client:                      fakeClientWithDPUs(dpus),
				getDeviceBySerialNumberFunc: mockGetDeviceBySerialNumberFunc(),
				getRshimNameByPCIFunc:       mockGetRshimNameByPCIFunc(),
				listDPUFunc:                 mockListDPUFunc(dpus),
				getDPUNodeFunc:              mockGetDPUNodeFunc(dpuNode),
				runPowerCycleCmdFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					powerCycleCnt++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				runShutdownARMFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					shutDownARMCountMutex.Lock()
					defer shutDownARMCountMutex.Unlock()
					shutDownARMCount++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				runRebootHostFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					rebootHostCount++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				isDPUOffFunc: func(rshim string) (bool, string, error) {
					dpuOffCheckRecordMutex.Lock()
					defer dpuOffCheckRecordMutex.Unlock()
					_, ok := dpuOffCheckRecord[rshim]
					if !ok {
						dpuOffCheckRecord[rshim] = struct{}{}
					}
					// returns false for the first time and true for the subsequent times
					return ok, "", nil
				},
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return false, nil
					},
				},
			}
			failures := handler.run()
			Expect(failures).To(BeEmpty())
			Expect(powerCycleCnt).To(Equal(0))
			Expect(shutDownARMCount).To(Equal(2))
			Expect(rebootHostCount).To(Equal(1))
		})

		It("should skip if reboot is finished", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			dpuRequiresSLR1 := rebootingDPU("slr-1")
			dpuRequiresSLR2 := rebootingDPU("slr-2")
			dpus := []provisioningv1.DPU{
				dpuRequiresSLR1,
				dpuRequiresSLR2,
			}
			store := map[types.NamespacedName]bool{}
			powerCycleCnt := 0
			shutDownARMCountMutex := sync.Mutex{}
			shutDownARMCount := 0
			rebootHostCount := 0
			dpuOffCheckRecordMutex := sync.Mutex{}
			dpuOffCheckRecord := make(map[string]struct{})
			handler := &Handler{
				Client:                      fakeClientWithDPUs(dpus),
				getDeviceBySerialNumberFunc: mockGetDeviceBySerialNumberFunc(),
				getRshimNameByPCIFunc:       mockGetRshimNameByPCIFunc(),
				listDPUFunc:                 mockListDPUFunc(dpus),
				getDPUNodeFunc:              mockGetDPUNodeFunc(dpuNode),
				runPowerCycleCmdFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					powerCycleCnt++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				runShutdownARMFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					shutDownARMCountMutex.Lock()
					defer shutDownARMCountMutex.Unlock()
					shutDownARMCount++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				runRebootHostFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					rebootHostCount++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				isDPUOffFunc: func(rshim string) (bool, string, error) {
					dpuOffCheckRecordMutex.Lock()
					defer dpuOffCheckRecordMutex.Unlock()
					_, ok := dpuOffCheckRecord[rshim]
					if !ok {
						dpuOffCheckRecord[rshim] = struct{}{}
					}
					// returns false for the first time and true for the subsequent times
					return ok, "", nil
				},
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return true, nil
					},
				},
			}
			failures := handler.run()
			Expect(failures).To(BeEmpty())
			Expect(powerCycleCnt).To(Equal(0))
			Expect(shutDownARMCount).To(Equal(0))
			Expect(rebootHostCount).To(Equal(0))
		})

		It("all DPUs should fail together if any DPU fails to reboot", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			dpuRequiresSLR1 := rebootingDPU("slr-1")
			dpuRequiresSLR2 := rebootingDPU("slr-2")
			dpuRequiresSLR3 := rebootingDPU("slr-3")
			dpus := []provisioningv1.DPU{
				dpuRequiresSLR1,
				dpuRequiresSLR2,
				dpuRequiresSLR3,
			}
			store := map[types.NamespacedName]bool{}
			powerCycleCnt := 0
			shutDownARMCountMutex := sync.Mutex{}
			shutDownARMCount := 0
			rebootHostCount := 0
			dpuOffCheckRecordMutex := sync.Mutex{}
			dpuOffCheckRecord := make(map[string]int)
			handler := &Handler{
				Client:                      fakeClientWithDPUs(dpus),
				getDeviceBySerialNumberFunc: mockGetDeviceBySerialNumberFunc(),
				getRshimNameByPCIFunc:       mockGetRshimNameByPCIFunc(),
				listDPUFunc:                 mockListDPUFunc(dpus),
				getDPUNodeFunc:              mockGetDPUNodeFunc(dpuNode),
				runPowerCycleCmdFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					powerCycleCnt++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				runShutdownARMFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					shutDownARMCountMutex.Lock()
					defer shutDownARMCountMutex.Unlock()
					shutDownARMCount++
					// mimic the behavior that mlxfwreset always returns an error
					return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("f-E- Maximum retries reached. Unable to retrieve ecos from mrsi register..")
				},
				runRebootHostFunc: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					rebootHostCount++
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
				isDPUOffFunc: func(rshim string) (bool, string, error) {
					dpuOffCheckRecordMutex.Lock()
					defer dpuOffCheckRecordMutex.Unlock()
					_, ok := dpuOffCheckRecord[rshim]
					if !ok {
						dpuOffCheckRecord[rshim] = len(dpuOffCheckRecord)
						// return false (DPU is not off) for the first check on each DPU
						return false, "", nil
					}

					// one out of three DPUs will fail to check if it is off
					if dpuOffCheckRecord[rshim] == 1 {
						return false, "", fmt.Errorf("failed to check if DPU is off")
					}
					return true, "", nil
				},
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return false, nil
					},
				},
			}
			handler.run()
			for _, d := range dpus {
				got := &provisioningv1.DPU{}
				Expect(handler.Get(context.Background(), types.NamespacedName{Namespace: d.Namespace, Name: d.Name}, got)).To(Succeed())
				Expect(got.Status.Phase).To(Equal(provisioningv1.DPURebooting))
				Expect(got.Status.RebootStatus).NotTo(BeNil())
				Expect(got.Status.RebootStatus.Phase).To(Equal(provisioningv1.RebootStatusFailed))
				Expect(got.Status.RebootStatus.Reason).To(Equal(failReason))
			}
			Expect(powerCycleCnt).To(Equal(0))
			Expect(shutDownARMCount).To(Equal(3))
			Expect(rebootHostCount).To(Equal(0))
		})
	})
})

var _ = Describe("Handler updateDPURebootStatus", func() {
	It("preserves RebootStatus.Method from existing status", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		rm := provisioningv1.RebootMethodFirmwareReset
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-host", Namespace: hostutil.DPFNamespace},
			Status: provisioningv1.DPUStatus{
				RebootStatus: &provisioningv1.RebootStatus{
					Phase:  provisioningv1.RebootStatusPending,
					Method: &rm,
				},
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&provisioningv1.DPU{}).
			Build()
		Expect(cl.Create(ctx, dpu)).To(Succeed())
		Expect(cl.Status().Update(ctx, dpu)).To(Succeed())

		h := &Handler{Client: cl}
		err := h.updateDPURebootStatus(ctx, dpu, provisioningv1.RebootStatusFailed, "FailedToReboot", "boom")
		Expect(err).NotTo(HaveOccurred())

		updated := &provisioningv1.DPU{}
		Expect(cl.Get(ctx, types.NamespacedName{Namespace: hostutil.DPFNamespace, Name: "dpu-host"}, updated)).To(Succeed())
		Expect(updated.Status.RebootStatus).NotTo(BeNil())
		Expect(updated.Status.RebootStatus.Method).NotTo(BeNil())
		Expect(*updated.Status.RebootStatus.Method).To(Equal(provisioningv1.RebootMethodFirmwareReset))
		Expect(updated.Status.RebootStatus.Phase).To(Equal(provisioningv1.RebootStatusFailed))
		Expect(updated.Status.RebootStatus.Reason).To(Equal("FailedToReboot"))
	})
})

type mockBootIDStore struct {
	persistBootIDFunc    func(*provisioningv1.DPU) error
	isRebootFinishedFunc func(*provisioningv1.DPU) (bool, error)
}

func (s *mockBootIDStore) PersistBootID(dpu *provisioningv1.DPU) error {
	return s.persistBootIDFunc(dpu)
}

func (s *mockBootIDStore) IsRebootFinished(dpu *provisioningv1.DPU) (bool, error) {
	return s.isRebootFinishedFunc(dpu)
}
