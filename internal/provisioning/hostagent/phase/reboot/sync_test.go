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
	"k8s.io/apimachinery/pkg/types"
)

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

		var mockPersistDPUBootIDFunc = func(store map[types.NamespacedName]bool) func(*provisioningv1.DPU, bool) error {
			return func(dpu *provisioningv1.DPU, skip bool) error {
				store[types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Name}] = skip
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
				},
				Status: provisioningv1.DPUStatus{
					Phase: phase,
				},
			}
		}

		It("do nothing if reboot method is external", func() {
			dpuNode := dpuNodeWithExternalRebootMethod()
			rebootingDPU := rebootingDPU("rebooting-dpu")
			nonRebootingDPU := nonRebootingDPU(provisioningv1.DPUInitializeInterface)
			dpus := []provisioningv1.DPU{
				rebootingDPU,
				nonRebootingDPU,
			}
			store := map[types.NamespacedName]bool{}
			handler := &Handler{
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
			rebootingDPU := rebootingDPU("rebooting-dpu")
			nonRebootingDPU := nonRebootingDPU(provisioningv1.DPUConfigFWParameters)
			dpus := []provisioningv1.DPU{
				rebootingDPU,
				nonRebootingDPU,
			}
			store := map[types.NamespacedName]bool{}
			handler := &Handler{
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

		It("should skip rebooting if the power cycle command on DPUNode is set to skip", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			dpuNode.Annotations = map[string]string{
				reboot.PowercycleCmdKey: reboot.Skip,
			}
			rebootingDPU := rebootingDPU("rebooting-dpu")
			nonRebootingDPU := nonRebootingDPU(provisioningv1.DPUPrepareBFB)
			dpus := []provisioningv1.DPU{
				rebootingDPU,
				nonRebootingDPU,
			}
			store := map[types.NamespacedName]bool{}
			handler := &Handler{
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
			Expect(store).To(HaveLen(1))
			Expect(store).To(HaveKey(types.NamespacedName{Namespace: rebootingDPU.Namespace, Name: rebootingDPU.Name}))
			Expect(store).NotTo(HaveKey(types.NamespacedName{Namespace: nonRebootingDPU.Namespace, Name: nonRebootingDPU.Name}))
			Expect(store[types.NamespacedName{Namespace: rebootingDPU.Namespace, Name: rebootingDPU.Name}]).To(BeTrue())
		})

		It("should skip rebooting if the reboot command on DPUNode is set to skip", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			dpuNode.Annotations = map[string]string{
				reboot.RebootCmdKey: reboot.Skip,
			}
			rebootingDPU := rebootingDPU("rebooting-dpu")
			nonRebootingDPU := nonRebootingDPU(provisioningv1.DPUOSInstalling)
			dpus := []provisioningv1.DPU{
				rebootingDPU,
				nonRebootingDPU,
			}
			store := map[types.NamespacedName]bool{}
			handler := &Handler{
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
			Expect(store).To(HaveLen(1))
			Expect(store).To(HaveKey(types.NamespacedName{Namespace: rebootingDPU.Namespace, Name: rebootingDPU.Name}))
			Expect(store).NotTo(HaveKey(types.NamespacedName{Namespace: nonRebootingDPU.Namespace, Name: nonRebootingDPU.Name}))
			Expect(store[types.NamespacedName{Namespace: rebootingDPU.Namespace, Name: rebootingDPU.Name}]).To(BeTrue())
		})

		It("should block if any DPU is being provisioned (not reaching DPURebooting phase)", func() {
			dpuNode := dpuNodeWithHostAgentRebootMethod()
			rebootingDPU := rebootingDPU("rebooting-dpu")
			nonRebootingDPU := nonRebootingDPU(provisioningv1.DPUCheckingHostRebootNeed)
			dpus := []provisioningv1.DPU{
				rebootingDPU,
				nonRebootingDPU,
			}
			store := map[types.NamespacedName]bool{}
			handler := &Handler{
				listDPUFunc:    mockListDPUFunc(dpus),
				getDPUNodeFunc: mockGetDPUNodeFunc(dpuNode),
				bootIDStore: &mockBootIDStore{
					persistBootIDFunc: mockPersistDPUBootIDFunc(store),
					isRebootFinishedFunc: func(dpu *provisioningv1.DPU) (bool, error) {
						return false, nil
					},
				},
			}
			failures := handler.run()
			Expect(failures).To(HaveLen(1))
			Expect(failures[0].Name).To(Equal(rebootingDPU.Name))
			Expect(failures[0].Status.Phase).To(Equal(provisioningv1.DPURebooting))
			Expect(failures[0].Status.Conditions).To(HaveLen(1))
			Expect(failures[0].Status.Conditions[0].Type).To(Equal(string(provisioningv1.DPUCondRebooted)))
			Expect(failures[0].Status.Conditions[0].Status).To(Equal(metav1.ConditionFalse))
			Expect(failures[0].Status.Conditions[0].Reason).To(Equal(failReason))
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
			failures := handler.run()
			Expect(failures).To(HaveLen(3))
			for _, dpu := range failures {
				Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPURebooting))
				Expect(dpu.Status.Conditions).To(HaveLen(1))
				Expect(dpu.Status.Conditions[0].Type).To(Equal(string(provisioningv1.DPUCondRebooted)))
				Expect(dpu.Status.Conditions[0].Status).To(Equal(metav1.ConditionFalse))
				Expect(dpu.Status.Conditions[0].Reason).To(Equal(failReason))
			}
			Expect(powerCycleCnt).To(Equal(0))
			Expect(shutDownARMCount).To(Equal(3))
			Expect(rebootHostCount).To(Equal(0))
		})
	})
})

type mockBootIDStore struct {
	persistBootIDFunc    func(*provisioningv1.DPU, bool) error
	isRebootFinishedFunc func(*provisioningv1.DPU) (bool, error)
}

func (s *mockBootIDStore) PersistBootID(dpu *provisioningv1.DPU, skip bool) error {
	return s.persistBootIDFunc(dpu, skip)
}

func (s *mockBootIDStore) IsRebootFinished(dpu *provisioningv1.DPU) (bool, error) {
	return s.isRebootFinishedFunc(dpu)
}
