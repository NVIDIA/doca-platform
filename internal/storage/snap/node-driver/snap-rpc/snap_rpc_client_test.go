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

package rpcclient

import (
	"fmt"
	"slices"
	"testing"
	"time"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
)

// MockClientForClientFunctions extends the existing mock client for client function operations
type MockClientForClientFunctions struct {
	requestID int
	timeout   time.Duration

	// Control flags for different failure scenarios
	// NVMe related failures
	shouldFailEmulationList       bool
	shouldFailSubsystemList       bool
	shouldFailNamespaceCreate     bool
	shouldFailSubsystemCreate     bool
	shouldFailSubsystemDestroy    bool
	shouldFailControllerCreate    bool
	shouldFailControllerAttach    bool
	shouldFailControllerResume    bool
	shouldFailControllerDetach    bool
	shouldFailControllerDestroy   bool
	shouldFailNamespaceDestroy    bool
	shouldFailNvmeFunctionCreate  bool
	shouldFailNvmeFunctionDestroy bool
	shouldFailControllerHotplug   bool
	shouldFailControllerHotunplug bool

	// VirtioFS related failures
	shouldFailTransportGet        bool
	shouldFailTransportCreate     bool
	shouldFailPossibleManagersGet bool
	shouldFailManagerGet          bool
	shouldFailManagerCreate       bool
	shouldFailTransportStart      bool
	shouldFailFunctionGet         bool
	shouldFailFunctionCreate      bool
	shouldFailDeviceGet           bool
	shouldFailDeviceCreate        bool
	shouldFailDeviceModify        bool
	shouldFailDeviceStart         bool
	shouldFailDeviceHotplug       bool
	shouldFailDeviceStop          bool
	shouldFailDeviceDestroy       bool
	shouldFailFunctionDestroy     bool
	shouldFailTransportStop       bool
	shouldFailManagerDestroy      bool
	shouldFailTransportDestroy    bool
	shouldFailDeviceHotunplug     bool
	deviceGetCallCount            int
	exposeDevicePattern           bool
	// Devices created through virtio_fs_device_create, reported by
	// virtio_fs_get_devices so create/start flows behave like SNAP.
	createdDevices []FSDevice

	// extraSubsystems are appended to the nvme_subsystem_list response, so a test
	// can start from a volume that already owns its own subsystem.
	extraSubsystems NvmeSubsystemListResponse
	// createdSubsystems and destroyedSubsystems record the NQNs the driver asked
	// SNAP to create and destroy, including attempts that failed.
	createdSubsystems   []string
	destroyedSubsystems []string
	// destroyedControllers and destroyedFunctions record what teardown reached,
	// so a test can assert that another volume's controller or hotplug function
	// was left alone.
	destroyedControllers []string
	destroyedFunctions   []string
}

func NewMockClientForClientFunctions() *MockClientForClientFunctions {
	return &MockClientForClientFunctions{
		requestID: 0,
		timeout:   60 * time.Second,
	}
}

func (m *MockClientForClientFunctions) Send(method string, params map[string]interface{}) (int, error) {
	m.requestID++
	return m.requestID, nil
}

func (m *MockClientForClientFunctions) Recv() (map[string]interface{}, error) {
	return map[string]interface{}{
		"result": map[string]interface{}{
			"status": "success",
		},
	}, nil
}

func (m *MockClientForClientFunctions) Close() error {
	return nil
}

//nolint:gocyclo
func (m *MockClientForClientFunctions) Call(method string, params map[string]interface{}) (interface{}, error) {
	switch method {
	case "emulation_function_list":
		if m.shouldFailEmulationList {
			return nil, fmt.Errorf("failed to get emulation functions")
		}
		return mockEmulationFunctionList, nil

	case "nvme_function_create":
		if m.shouldFailNvmeFunctionCreate {
			return nil, fmt.Errorf("failed to create NVMe function")
		}
		return map[string]interface{}{"vuid": "MT2323XZ09G2NVMES1D0F0", "status": "success"}, nil

	case "nvme_function_destroy":
		vuid, _ := params["vuid"].(string)
		m.destroyedFunctions = append(m.destroyedFunctions, vuid)
		if m.shouldFailNvmeFunctionDestroy {
			return nil, fmt.Errorf("failed to destroy NVMe function")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_controller_hotplug":
		if m.shouldFailControllerHotplug {
			return nil, fmt.Errorf("failed to hotplug controller")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_controller_hotunplug":
		if m.shouldFailControllerHotunplug {
			return nil, fmt.Errorf("failed to hotunplug controller")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_subsystem_list":
		if m.shouldFailSubsystemList {
			return nil, fmt.Errorf("failed to get subsystems")
		}
		return append(append(NvmeSubsystemListResponse{}, mockNvmeSubsystemList...), m.extraSubsystems...), nil

	case "nvme_subsystem_create":
		nqn, _ := params["nqn"].(string)
		m.createdSubsystems = append(m.createdSubsystems, nqn)
		if m.shouldFailSubsystemCreate {
			return nil, fmt.Errorf("failed to create subsystem")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_subsystem_destroy":
		nqn, _ := params["nqn"].(string)
		m.destroyedSubsystems = append(m.destroyedSubsystems, nqn)
		if m.shouldFailSubsystemDestroy {
			return nil, fmt.Errorf("failed to destroy subsystem")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_namespace_create":
		if m.shouldFailNamespaceCreate {
			return nil, fmt.Errorf("failed to create namespace")
		}
		return map[string]interface{}{
			"status": "success",
			"nsid":   params["nsid"],
		}, nil

	case "nvme_controller_create":
		if m.shouldFailControllerCreate {
			return nil, fmt.Errorf("failed to create controller")
		}
		return map[string]interface{}{
			"status":  "success",
			"ctrl_id": fmt.Sprintf("NVMeCtrl_%s", params["pci_bdf"]),
		}, nil

	case "nvme_controller_attach_ns":
		if m.shouldFailControllerAttach {
			return nil, fmt.Errorf("failed to attach namespace")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_controller_resume":
		if m.shouldFailControllerResume {
			return nil, fmt.Errorf("failed to resume controller")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_controller_detach_ns":
		if m.shouldFailControllerDetach {
			return nil, fmt.Errorf("failed to detach namespace")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_controller_destroy":
		ctrlID, _ := params["ctrl_id"].(string)
		m.destroyedControllers = append(m.destroyedControllers, ctrlID)
		if m.shouldFailControllerDestroy {
			return nil, fmt.Errorf("failed to destroy controller")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_namespace_destroy":
		if m.shouldFailNamespaceDestroy {
			return nil, fmt.Errorf("failed to destroy namespace")
		}
		return map[string]interface{}{"status": "success"}, nil

	// VirtioFS methods
	case "virtio_fs_get_transports":
		if m.shouldFailTransportGet {
			return nil, fmt.Errorf("failed to get transports")
		}
		return []Transport{{Name: "DOCA", State: "started"}}, nil

	case "virtio_fs_transport_create":
		if m.shouldFailTransportCreate {
			return nil, fmt.Errorf("failed to create transport")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_doca_get_possible_managers":
		if m.shouldFailPossibleManagersGet {
			return nil, fmt.Errorf("failed to get possible managers")
		}
		return []DOCAManager{{Name: "mlx5_0"}}, nil

	case "virtio_fs_doca_get_managers":
		if m.shouldFailManagerGet {
			return nil, fmt.Errorf("failed to get managers")
		}
		return []DOCAManager{{Name: "mlx5_0"}}, nil

	case "virtio_fs_doca_manager_create":
		if m.shouldFailManagerCreate {
			return nil, fmt.Errorf("failed to create manager")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_transport_start":
		if m.shouldFailTransportStart {
			return nil, fmt.Errorf("failed to start transport")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_doca_get_functions":
		if m.shouldFailFunctionGet {
			return nil, fmt.Errorf("failed to get functions")
		}
		return []DOCAFunctionList{
			{
				Manager: "mlx5_0",
				FunctionList: []DOCAFunction{
					{
						VUID:       "test-vuid-1",
						PCIAddress: "26:0c.0",
					},
					{
						VUID:       "test-vuid-2",
						PCIAddress: "26:0c.1",
					},
				},
			},
		}, nil

	case "virtio_fs_doca_function_create":
		if m.shouldFailFunctionCreate {
			return nil, fmt.Errorf("failed to create function")
		}
		return "test-vuid-1", nil

	case "virtio_fs_get_devices":
		if m.shouldFailDeviceGet {
			return nil, fmt.Errorf("failed to get devices")
		}

		m.deviceGetCallCount++

		if m.exposeDevicePattern { // For ExposeFSDevice
			switch m.deviceGetCallCount {
			case 1:
				return append([]FSDevice{
					{
						Name:             "dev_my-fast-volume2",
						TransportName:    "DOCA",
						State:            "running",
						Fsdev:            "my-fast-volume2",
						Tag:              "my-fast-volume2tag",
						QueueSize:        256,
						NumRequestQueues: 8,
					},
					{
						Name:             "dev_test-fs-device",
						TransportName:    "DOCA",
						State:            "running",
						Fsdev:            "dev_test-fs-device",
						Tag:              "dev_test-fs-devicetag",
						QueueSize:        256,
						NumRequestQueues: 8,
					},
				}, m.createdDevices...), nil
			default:
				return append([]FSDevice{
					{
						Name:             "dev_my-fast-volume2",
						TransportName:    "DOCA",
						State:            "running",
						Fsdev:            "my-fast-volume2",
						Tag:              "my-fast-volume2tag",
						QueueSize:        256,
						NumRequestQueues: 8,
					},
					{
						Name:             "dev_test-fs-device",
						TransportName:    "DOCA",
						State:            "running",
						Fsdev:            "dev_test-fs-device",
						Tag:              "dev_test-fs-devicetag",
						QueueSize:        256,
						NumRequestQueues: 8,
					},
					{
						Name:             "test-fs-device",
						TransportName:    "DOCA",
						State:            "running",
						Fsdev:            "test-fs-device",
						Tag:              "test-fs-devicetag",
						QueueSize:        256,
						NumRequestQueues: 8,
					},
				}, m.createdDevices...), nil
			}
		} else { // For DestroyFSDevice
			switch m.deviceGetCallCount {
			case 1:
				return []FSDevice{
					{
						Name:             "test-fs-device",
						TransportName:    "DOCA",
						State:            "running",
						Fsdev:            "test-fs-device",
						Tag:              "test-fs-devicetag",
						QueueSize:        256,
						NumRequestQueues: 8,
					},
					{
						Name:             "dev_test-fs-device",
						TransportName:    "DOCA",
						State:            "running",
						Fsdev:            "dev_test-fs-device",
						Tag:              "dev_test-fs-devicetag",
						QueueSize:        256,
						NumRequestQueues: 8,
					},
				}, nil
			case 2:
				return []FSDevice{}, nil
			default:
				return []FSDevice{}, nil
			}
		}

	case "virtio_fs_device_create":
		if m.shouldFailDeviceCreate {
			return nil, fmt.Errorf("failed to create device")
		}
		devName, _ := params["dev_name"].(string)
		fsdev, _ := params["fsdev"].(string)
		tag, _ := params["tag"].(string)
		m.createdDevices = append(m.createdDevices, FSDevice{
			Name:             devName,
			TransportName:    "DOCA",
			State:            "stopped",
			Fsdev:            fsdev,
			Tag:              tag,
			QueueSize:        256,
			NumRequestQueues: 8,
		})
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_doca_device_modify":
		if m.shouldFailDeviceModify {
			return nil, fmt.Errorf("failed to modify device")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_device_start":
		if m.shouldFailDeviceStart {
			return nil, fmt.Errorf("failed to start device")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_doca_device_hotplug":
		if m.shouldFailDeviceHotplug {
			return nil, fmt.Errorf("failed to hotplug device")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_device_stop":
		if m.shouldFailDeviceStop {
			return nil, fmt.Errorf("failed to stop device")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_device_destroy":
		if m.shouldFailDeviceDestroy {
			return nil, fmt.Errorf("failed to destroy device")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_doca_function_destroy":
		if m.shouldFailFunctionDestroy {
			return nil, fmt.Errorf("failed to destroy function")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_transport_stop":
		if m.shouldFailTransportStop {
			return nil, fmt.Errorf("failed to stop transport")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_doca_manager_destroy":
		if m.shouldFailManagerDestroy {
			return nil, fmt.Errorf("failed to destroy manager")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_transport_destroy":
		if m.shouldFailTransportDestroy {
			return nil, fmt.Errorf("failed to destroy transport")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "virtio_fs_doca_device_hotunplug":
		if m.shouldFailDeviceHotunplug {
			return nil, fmt.Errorf("failed to hotunplug device")
		}
		return map[string]interface{}{"status": "success"}, nil

	default:
		return nil, fmt.Errorf("unexpected method: %s", method)
	}
}

func TestExposeBlockDevice(t *testing.T) {
	tests := []struct {
		name                         string
		snapProvider                 string
		dpuStatus                    snapstoragev1.VolumeAttachmentStatusDPU
		spec                         snapstoragev1.VolumeAttachmentSpec
		parameters                   map[string]string
		shouldFailEmulationList      bool
		shouldFailSubsystemList      bool
		shouldFailNamespaceCreate    bool
		shouldFailSubsystemCreate    bool
		shouldFailControllerCreate   bool
		shouldFailControllerAttach   bool
		shouldFailControllerResume   bool
		shouldFailNvmeFunctionCreate bool
		expectError                  bool
		expectedNSID                 int
		expectedPCIBDF               string
		expectedUUID                 string
		expectedFuncVUID             string
		// expectCreatedSubsystem is the NQN the driver should have created, or
		// empty when it should have reused a subsystem already present.
		expectCreatedSubsystem string
	}{
		{
			name:         "Create new namespace and controller successfully",
			snapProvider: "test-provider",
			dpuStatus:    snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "new-device"},
			spec: snapstoragev1.VolumeAttachmentSpec{
				Parameters: map[string]string{},
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType: "vf",
				},
			},
			expectError:            false,
			expectedNSID:           volumeNSID,
			expectedPCIBDF:         "26:0c.1",
			expectedFuncVUID:       "MT2328XZ17DFNVMES0D0F2",
			expectCreatedSubsystem: SubsystemNQNForDevice("new-device"),
		},
		{
			// A namespace already in the shared subsystem, which is what an
			// attachment made before this change looks like, is reused where it is.
			name:         "Use existing namespace",
			snapProvider: "test-provider",
			dpuStatus:    snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "null1"},
			spec: snapstoragev1.VolumeAttachmentSpec{
				Parameters: map[string]string{},
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType: "vf",
				},
			},
			expectError:      false,
			expectedNSID:     1,
			expectedPCIBDF:   "26:0c.0",
			expectedUUID:     "263826ad-19a3-4feb-bc25-4bc81ee7748e",
			expectedFuncVUID: "MT2328XZ17DFNVMES0D0F2",
		},
		{
			name: "Use DPU status values",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				DeviceName:       "test-device",
				PCIDeviceAddress: "26:0c.2",
				BdevAttrs: snapstoragev1.BdevAttrs{
					NVMeNsID: 5,
					NVMeUUID: "550e8400-e29b-41d4-a716-446655440000",
				},
			},
			spec: snapstoragev1.VolumeAttachmentSpec{
				Parameters: map[string]string{},
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType: "vf",
				},
			},
			// A namespace recorded under the old shared-subsystem model keeps its
			// NSID and UUID even though it is recreated in its own subsystem.
			expectError:            false,
			expectedNSID:           5,
			expectedPCIBDF:         "26:0c.2",
			expectedUUID:           "550e8400-e29b-41d4-a716-446655440000",
			expectedFuncVUID:       "MT2328XZ17DFNVMES0D0F2",
			expectCreatedSubsystem: SubsystemNQNForDevice("test-device"),
		},
		{
			name:                    "Emulation function list failure",
			dpuStatus:               snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "test-device"},
			spec:                    snapstoragev1.VolumeAttachmentSpec{},
			shouldFailEmulationList: true,
			expectError:             true,
		},
		{
			name:                    "Subsystem list failure",
			dpuStatus:               snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "test-device"},
			spec:                    snapstoragev1.VolumeAttachmentSpec{},
			shouldFailSubsystemList: true,
			expectError:             true,
		},
		{
			name:                      "Namespace create failure",
			dpuStatus:                 snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "new-device"},
			spec:                      snapstoragev1.VolumeAttachmentSpec{},
			shouldFailNamespaceCreate: true,
			expectError:               true,
		},
		{
			name:                      "Subsystem create failure",
			dpuStatus:                 snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "new-device"},
			spec:                      snapstoragev1.VolumeAttachmentSpec{},
			shouldFailSubsystemCreate: true,
			expectError:               true,
		},
		{
			name:                       "Controller create failure",
			dpuStatus:                  snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "new-device"},
			spec:                       snapstoragev1.VolumeAttachmentSpec{},
			shouldFailControllerCreate: true,
			expectError:                true,
		},
		{
			name: "Use DPU status values with hotplug",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				DeviceName: "test-device",
			},
			spec: snapstoragev1.VolumeAttachmentSpec{
				Parameters: map[string]string{},
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType:    "vf",
					HotplugFunction: true,
				},
			},
			expectError:            false,
			expectedNSID:           volumeNSID,
			expectedPCIBDF:         "26:00.3",
			expectedFuncVUID:       "MT2323XZ09G2NVMES1D0F0",
			expectCreatedSubsystem: SubsystemNQNForDevice("test-device"),
		},
		{
			name: "Hotplug reuses persisted FuncVUID without creating function",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				DeviceName: "test-device",
				FuncVUID:   "MT2323XZ09G2NVMES1D0F0",
			},
			spec: snapstoragev1.VolumeAttachmentSpec{
				Parameters: map[string]string{},
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType:    "pf",
					HotplugFunction: true,
				},
			},
			shouldFailNvmeFunctionCreate: true, // proves create is not called
			expectError:                  false,
			expectedNSID:                 volumeNSID,
			expectedPCIBDF:               "26:00.3",
			expectedFuncVUID:             "MT2323XZ09G2NVMES1D0F0",
			expectCreatedSubsystem:       SubsystemNQNForDevice("test-device"),
		},
		{
			name: "Hotplug discovers existing controller VUID without creating function",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				DeviceName: "hotplug-device",
			},
			spec: snapstoragev1.VolumeAttachmentSpec{
				Parameters: map[string]string{},
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType:    "pf",
					HotplugFunction: true,
				},
			},
			shouldFailNvmeFunctionCreate: true, // proves create is not called
			expectError:                  false,
			expectedNSID:                 3,
			expectedPCIBDF:               "26:00.3",
			expectedUUID:                 "263826ad-19a3-4feb-bc25-4bc81ee7750e",
			expectedFuncVUID:             "MT2323XZ09G2NVMES1D0F0",
		},
		{
			name: "Hotplug rejects mismatched persisted FuncVUID and existing controller",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				DeviceName: "hotplug-device",
				FuncVUID:   "WRONG-VUID",
			},
			spec: snapstoragev1.VolumeAttachmentSpec{
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType:    "pf",
					HotplugFunction: true,
				},
			},
			expectError: true,
		},
		{
			name:      "NVMe function create failure with hotplug",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "test-device"},
			spec: snapstoragev1.VolumeAttachmentSpec{
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType:    "vf",
					HotplugFunction: true,
				},
			},
			shouldFailNvmeFunctionCreate: true,
			expectError:                  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpcClient := NewMockClientForClientFunctions()
			rpcClient.shouldFailEmulationList = tt.shouldFailEmulationList
			rpcClient.shouldFailSubsystemList = tt.shouldFailSubsystemList
			rpcClient.shouldFailNamespaceCreate = tt.shouldFailNamespaceCreate
			rpcClient.shouldFailSubsystemCreate = tt.shouldFailSubsystemCreate
			rpcClient.shouldFailControllerCreate = tt.shouldFailControllerCreate
			rpcClient.shouldFailControllerAttach = tt.shouldFailControllerAttach
			rpcClient.shouldFailControllerResume = tt.shouldFailControllerResume
			rpcClient.shouldFailNvmeFunctionCreate = tt.shouldFailNvmeFunctionCreate

			client := NewClient(rpcClient)

			nsid, pciBDF, uuid, funcVUID, err := client.ExposeBlockDevice(tt.dpuStatus, tt.spec, tt.parameters)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if nsid != tt.expectedNSID {
				t.Errorf("Expected NSID %d, got %d", tt.expectedNSID, nsid)
			}

			if pciBDF != tt.expectedPCIBDF {
				t.Errorf("Expected PCI BDF %s, got %s", tt.expectedPCIBDF, pciBDF)
			}

			if tt.expectedUUID != "" && uuid != tt.expectedUUID {
				t.Errorf("Expected UUID %s, got %s", tt.expectedUUID, uuid)
			}
			if funcVUID != tt.expectedFuncVUID {
				t.Errorf("Expected function UUID %s, got %s", tt.expectedFuncVUID, funcVUID)
			}

			var expectedSubsystems []string
			if tt.expectCreatedSubsystem != "" {
				expectedSubsystems = []string{tt.expectCreatedSubsystem}
			}
			if !slices.Equal(rpcClient.createdSubsystems, expectedSubsystems) {
				t.Errorf("Expected created subsystems %v, got %v", expectedSubsystems, rpcClient.createdSubsystems)
			}
		})
	}
}

func TestExposeFSDevice(t *testing.T) {
	tests := []struct {
		name                          string
		snapProvider                  string
		deviceName                    string
		dpuStatus                     snapstoragev1.VolumeAttachmentStatusDPU
		parameters                    map[string]string
		shouldFailTransportGet        bool
		shouldFailPossibleManagersGet bool
		shouldFailManagerGet          bool
		shouldFailFunctionGet         bool
		shouldFailDeviceGet           bool
		shouldFailDeviceCreate        bool
		shouldFailDeviceModify        bool
		shouldFailDeviceStart         bool
		shouldFailDeviceHotplug       bool
		expectError                   bool
		expectedTag                   string
		expectedPCIAddr               string
		expectedFuncVUID              string
	}{
		{
			name:         "Use existing PCI address",
			snapProvider: "test-provider",
			deviceName:   "test-fs-device",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				PCIDeviceAddress: "26:0c.1",
			},
			parameters:       map[string]string{},
			expectError:      false,
			expectedTag:      "test-fs-devicetag",
			expectedPCIAddr:  "26:0c.1",
			expectedFuncVUID: "test-vuid-2",
		},
		{
			name:         "Existing device reuses persisted FuncVUID without creating function",
			snapProvider: "test-provider",
			deviceName:   "test-fs-device",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				FuncVUID: "test-vuid-2",
			},
			parameters:       map[string]string{},
			expectError:      false,
			expectedTag:      "test-fs-devicetag",
			expectedPCIAddr:  "26:0c.1",
			expectedFuncVUID: "test-vuid-2",
		},
		{
			name:         "Persisted FuncVUID reused for new device without creating function",
			snapProvider: "test-provider",
			deviceName:   "new-test-fs-device",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				FuncVUID: "test-vuid-1",
			},
			parameters:       map[string]string{},
			expectError:      false,
			expectedTag:      "new-test-fs-devicetag",
			expectedPCIAddr:  "26:0c.0",
			expectedFuncVUID: "test-vuid-1",
		},
		{
			name:         "Existing device without identity refuses to create function",
			snapProvider: "test-provider",
			deviceName:   "test-fs-device",
			dpuStatus:    snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:   map[string]string{},
			expectError:  true,
		},
		{
			name:                   "Transport get failure",
			snapProvider:           "test-provider",
			deviceName:             "test-fs-device",
			dpuStatus:              snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:             map[string]string{},
			shouldFailTransportGet: true,
			expectError:            true,
		},
		{
			name:                          "Possible managers get failure",
			snapProvider:                  "test-provider",
			deviceName:                    "test-fs-device",
			dpuStatus:                     snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:                    map[string]string{},
			shouldFailPossibleManagersGet: true,
			expectError:                   true,
		},
		{
			name:                 "Manager get failure",
			snapProvider:         "test-provider",
			deviceName:           "test-fs-device",
			dpuStatus:            snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:           map[string]string{},
			shouldFailManagerGet: true,
			expectError:          true,
		},
		{
			name:                   "Device create failure",
			snapProvider:           "test-provider",
			deviceName:             "new-test-fs-device",
			dpuStatus:              snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:             map[string]string{},
			shouldFailDeviceCreate: true,
			expectError:            true,
		},
		{
			name:                   "Device modify failure",
			snapProvider:           "test-provider",
			deviceName:             "new-test-fs-device",
			dpuStatus:              snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:             map[string]string{},
			shouldFailDeviceModify: true,
			expectError:            true,
		},
		{
			name:                  "Device start failure",
			snapProvider:          "test-provider",
			deviceName:            "new-test-fs-device",
			dpuStatus:             snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:            map[string]string{},
			shouldFailDeviceStart: true,
			expectError:           true,
		},
		{
			name:                    "Device hotplug failure",
			snapProvider:            "test-provider",
			deviceName:              "new-test-fs-device",
			dpuStatus:               snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:              map[string]string{},
			shouldFailDeviceHotplug: true,
			expectError:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpcClient := NewMockClientForClientFunctions()

			// Configure for ExposeFSDevice pattern
			rpcClient.exposeDevicePattern = true
			// Configure failure scenarios
			rpcClient.shouldFailTransportGet = tt.shouldFailTransportGet
			rpcClient.shouldFailPossibleManagersGet = tt.shouldFailPossibleManagersGet
			rpcClient.shouldFailManagerGet = tt.shouldFailManagerGet
			rpcClient.shouldFailFunctionGet = tt.shouldFailFunctionGet
			rpcClient.shouldFailDeviceGet = tt.shouldFailDeviceGet
			rpcClient.shouldFailDeviceCreate = tt.shouldFailDeviceCreate
			rpcClient.shouldFailDeviceModify = tt.shouldFailDeviceModify
			rpcClient.shouldFailDeviceStart = tt.shouldFailDeviceStart
			rpcClient.shouldFailDeviceHotplug = tt.shouldFailDeviceHotplug

			client := NewClient(rpcClient)

			tag, pciAddr, funcVUID, err := client.ExposeFSDevice(tt.deviceName, tt.dpuStatus, tt.parameters)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tag != tt.expectedTag {
				t.Errorf("Expected tag %s, got %s", tt.expectedTag, tag)
			}

			if pciAddr != tt.expectedPCIAddr {
				t.Errorf("Expected PCI address %s, got %s", tt.expectedPCIAddr, pciAddr)
			}
			if funcVUID != tt.expectedFuncVUID {
				t.Errorf("Expected function UUID %s, got %s", tt.expectedFuncVUID, funcVUID)
			}
		})
	}
}

func TestDestroyBlockDevice(t *testing.T) {
	// ownedDeviceName is a volume attached after the move to a subsystem per
	// volume, so its namespace sits at NSID 1 in a subsystem the driver owns.
	const ownedDeviceName = "owned-device"
	ownedNQN := SubsystemNQNForDevice(ownedDeviceName)
	ownedSubsystem := NvmeSubsystemListResponse{
		{
			NQN:  ownedNQN,
			MNAN: 1,
			Controllers: []interface{}{
				map[string]interface{}{"ctrl_id": "NVMeCtrl2"},
			},
			Namespaces: []Namespace{
				{
					NSID: volumeNSID,
					Bdev: ownedDeviceName,
					NQN:  ownedNQN,
					Controllers: []interface{}{
						map[string]interface{}{"ctrl_id": "NVMeCtrl2"},
					},
				},
			},
		},
	}

	// strandedDeviceName owns a subsystem whose namespace is still attached to
	// the controller at 26:0c.0, but its recorded PCI address points at the
	// hotplugged function 26:00.3, which SNAP has since given to another volume.
	const strandedDeviceName = "stranded-device"
	strandedNQN := SubsystemNQNForDevice(strandedDeviceName)
	strandedSubsystem := NvmeSubsystemListResponse{
		{
			NQN:  strandedNQN,
			MNAN: 1,
			Controllers: []interface{}{
				map[string]interface{}{"ctrl_id": "NVMeCtrl2"},
			},
			Namespaces: []Namespace{
				{
					NSID: volumeNSID,
					Bdev: strandedDeviceName,
					NQN:  strandedNQN,
					Controllers: []interface{}{
						map[string]interface{}{"ctrl_id": "NVMeCtrl2"},
					},
				},
			},
		},
	}

	tests := []struct {
		name                          string
		snapProvider                  string
		deviceName                    string
		nsid                          int
		pciAddr                       string
		extraSubsystems               NvmeSubsystemListResponse
		shouldFailEmulationList       bool
		shouldFailSubsystemList       bool
		shouldFailControllerDetach    bool
		shouldFailControllerDestroy   bool
		shouldFailNamespaceDestroy    bool
		shouldFailSubsystemDestroy    bool
		shouldFailNvmeFunctionDestroy bool
		expectError                   bool
		hotplug                       bool
		// expectDestroyedSubsystem is the NQN teardown should have removed, or
		// empty when no subsystem should be touched.
		expectDestroyedSubsystem string
		// expectDestroyedControllers and expectDestroyedFunctions are asserted
		// only when set, so the cases that predate stale-address handling stay
		// as they were.
		expectDestroyedControllers []string
		expectDestroyedFunctions   []string
	}{
		{
			// A legacy attachment lives in the shared subsystem, which hosts other
			// volumes and the admin-only PF controller, so it must survive detach.
			name:         "Destroy legacy device without destroying the shared subsystem",
			snapProvider: "test-provider",
			deviceName:   "null1",
			nsid:         1,
			pciAddr:      "26:0c.0",
			expectError:  false,
			hotplug:      false,
		},
		{
			name:                     "Destroy device and its own subsystem",
			snapProvider:             "test-provider",
			deviceName:               ownedDeviceName,
			nsid:                     volumeNSID,
			pciAddr:                  "26:0c.0",
			extraSubsystems:          ownedSubsystem,
			expectError:              false,
			hotplug:                  false,
			expectDestroyedSubsystem: ownedNQN,
		},
		{
			// A leaked subsystem is recoverable, an undeletable volume is not.
			name:                       "Subsystem destroy failure does not fail the detach",
			snapProvider:               "test-provider",
			deviceName:                 ownedDeviceName,
			nsid:                       volumeNSID,
			pciAddr:                    "26:0c.0",
			extraSubsystems:            ownedSubsystem,
			shouldFailSubsystemDestroy: true,
			expectError:                false,
			hotplug:                    false,
			expectDestroyedSubsystem:   ownedNQN,
		},
		{
			// The recorded address now hosts NVMeCtrl_26:00.3, which belongs to
			// hotplug-device. Tearing it down would detach that volume, so the
			// controller the namespace itself reports has to be used instead.
			name:                       "Stale PCI address leaves the other volume's controller alone",
			snapProvider:               "test-provider",
			deviceName:                 strandedDeviceName,
			nsid:                       volumeNSID,
			pciAddr:                    "26:00.3",
			extraSubsystems:            strandedSubsystem,
			expectError:                false,
			hotplug:                    false,
			expectDestroyedSubsystem:   strandedNQN,
			expectDestroyedControllers: []string{"NVMeCtrl2"},
		},
		{
			// Same stale address, but hotplug teardown would also have destroyed
			// the function under the other volume's controller.
			name:                       "Stale PCI address leaves the other volume's hotplug function alone",
			snapProvider:               "test-provider",
			deviceName:                 strandedDeviceName,
			nsid:                       volumeNSID,
			pciAddr:                    "26:00.3",
			extraSubsystems:            strandedSubsystem,
			expectError:                false,
			hotplug:                    true,
			expectDestroyedSubsystem:   strandedNQN,
			expectDestroyedControllers: []string{"NVMeCtrl2"},
			expectDestroyedFunctions:   []string{},
		},
		{
			name:         "Destroy non-existent device",
			snapProvider: "test-provider",
			deviceName:   "missing-device",
			nsid:         999,
			pciAddr:      "26:0c.9",
			expectError:  false,
			hotplug:      false,
		},
		{
			name:                    "Emulation list failure",
			snapProvider:            "test-provider",
			deviceName:              "null1",
			nsid:                    1,
			pciAddr:                 "26:0c.0",
			shouldFailEmulationList: true,
			expectError:             true,
			hotplug:                 false,
		},
		{
			name:                    "Subsystem list failure",
			snapProvider:            "test-provider",
			deviceName:              "null1",
			nsid:                    1,
			pciAddr:                 "26:0c.0",
			shouldFailSubsystemList: true,
			expectError:             true,
			hotplug:                 false,
		},
		{
			name:                       "Controller detach failure",
			snapProvider:               "test-provider",
			deviceName:                 "null1",
			nsid:                       1,
			pciAddr:                    "26:0c.0",
			shouldFailControllerDetach: true,
			expectError:                true,
			hotplug:                    false,
		},
		{
			name:                        "Controller destroy failure",
			snapProvider:                "test-provider",
			deviceName:                  "null1",
			nsid:                        1,
			pciAddr:                     "26:0c.0",
			shouldFailControllerDestroy: true,
			expectError:                 true,
			hotplug:                     false,
		},
		{
			name:                       "Namespace destroy failure",
			snapProvider:               "test-provider",
			deviceName:                 "null1",
			nsid:                       1,
			pciAddr:                    "26:0c.0",
			shouldFailNamespaceDestroy: true,
			expectError:                true,
			hotplug:                    false,
		},
		{
			name:                          "NVMe function destroy failure with hotplug",
			snapProvider:                  "test-provider",
			deviceName:                    "hotplug-device",
			nsid:                          3,
			pciAddr:                       "26:00.3",
			shouldFailNvmeFunctionDestroy: true,
			expectError:                   true,
			hotplug:                       true,
		},
		{
			name:                    "Emulation list failure with hotplug",
			snapProvider:            "test-provider",
			deviceName:              "hotplug-device",
			nsid:                    3,
			pciAddr:                 "26:00.3",
			shouldFailEmulationList: true,
			expectError:             true,
			hotplug:                 true,
		},
		{
			name:                    "Subsystem list failure with hotplug",
			snapProvider:            "test-provider",
			deviceName:              "hotplug-device",
			nsid:                    3,
			pciAddr:                 "26:00.3",
			shouldFailSubsystemList: true,
			expectError:             true,
			hotplug:                 true,
		},
		{
			name:                        "Controller destroy failure with hotplug",
			snapProvider:                "test-provider",
			deviceName:                  "hotplug-device",
			nsid:                        3,
			pciAddr:                     "26:00.3",
			shouldFailControllerDestroy: true,
			expectError:                 true,
			hotplug:                     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpcClient := NewMockClientForClientFunctions()
			rpcClient.extraSubsystems = tt.extraSubsystems
			rpcClient.shouldFailEmulationList = tt.shouldFailEmulationList
			rpcClient.shouldFailSubsystemList = tt.shouldFailSubsystemList
			rpcClient.shouldFailControllerDetach = tt.shouldFailControllerDetach
			rpcClient.shouldFailControllerDestroy = tt.shouldFailControllerDestroy
			rpcClient.shouldFailNamespaceDestroy = tt.shouldFailNamespaceDestroy
			rpcClient.shouldFailSubsystemDestroy = tt.shouldFailSubsystemDestroy
			rpcClient.shouldFailNvmeFunctionDestroy = tt.shouldFailNvmeFunctionDestroy

			client := NewClient(rpcClient)

			err := client.DestroyBlockDevice(tt.deviceName, tt.nsid, tt.pciAddr, tt.hotplug)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			var expectedSubsystems []string
			if tt.expectDestroyedSubsystem != "" {
				expectedSubsystems = []string{tt.expectDestroyedSubsystem}
			}
			if !slices.Equal(rpcClient.destroyedSubsystems, expectedSubsystems) {
				t.Errorf("Expected destroyed subsystems %v, got %v", expectedSubsystems, rpcClient.destroyedSubsystems)
			}

			if tt.expectDestroyedControllers != nil && !slices.Equal(rpcClient.destroyedControllers, tt.expectDestroyedControllers) {
				t.Errorf("Expected destroyed controllers %v, got %v", tt.expectDestroyedControllers, rpcClient.destroyedControllers)
			}
			if tt.expectDestroyedFunctions != nil && !slices.Equal(rpcClient.destroyedFunctions, tt.expectDestroyedFunctions) {
				t.Errorf("Expected destroyed functions %v, got %v", tt.expectDestroyedFunctions, rpcClient.destroyedFunctions)
			}
		})
	}
}

func TestDestroyFSDevice(t *testing.T) {
	tests := []struct {
		name                       string
		snapProvider               string
		deviceName                 string
		pciAddr                    string
		mockDevicesSecondCall      []FSDevice
		shouldFailDeviceGet        bool
		shouldFailDeviceStop       bool
		shouldFailDeviceDestroy    bool
		shouldFailFunctionGet      bool
		shouldFailFunctionDestroy  bool
		shouldFailTransportStop    bool
		shouldFailManagerDestroy   bool
		shouldFailTransportDestroy bool
		expectError                bool
		expectCleanup              bool
	}{
		{
			name:         "Destroy device with full cleanup (no remaining devices)",
			snapProvider: "test-provider",
			deviceName:   "test-fs-device",
			pciAddr:      "26:0c.0",
			expectError:  false,
		},
		{
			name:         "Destroy device with remaining devices (skip cleanup)",
			snapProvider: "test-provider",
			deviceName:   "test-fs-device",
			pciAddr:      "26:0c.0",
			mockDevicesSecondCall: []FSDevice{
				{
					Name:             "dev_other-device",
					TransportName:    "DOCA",
					State:            "running",
					Fsdev:            "other-device",
					Tag:              "other-devicetag",
					QueueSize:        256,
					NumRequestQueues: 8,
				},
			},
			expectError:   false,
			expectCleanup: false,
		},
		{
			name:                "Device get failure",
			snapProvider:        "test-provider",
			deviceName:          "test-device",
			pciAddr:             "26:0c.0",
			shouldFailDeviceGet: true,
			expectError:         true,
		},
		{
			name:                 "Device stop failure",
			snapProvider:         "test-provider",
			deviceName:           "test-fs-device",
			pciAddr:              "26:0c.0",
			shouldFailDeviceStop: true,
			expectError:          true,
		},
		{
			name:                    "Device destroy failure",
			snapProvider:            "test-provider",
			deviceName:              "test-fs-device",
			pciAddr:                 "26:0c.0",
			shouldFailDeviceDestroy: true,
			expectError:             true,
		},
		{
			name:                  "Function get failure",
			snapProvider:          "test-provider",
			deviceName:            "test-fs-device",
			pciAddr:               "26:0c.0",
			shouldFailFunctionGet: true,
			expectError:           true,
		},
		{
			name:                      "Function destroy failure",
			snapProvider:              "test-provider",
			deviceName:                "test-fs-device",
			pciAddr:                   "26:0c.0",
			shouldFailFunctionDestroy: true,
			expectError:               true,
		},
		{
			name:                    "Transport stop failure",
			snapProvider:            "test-provider",
			deviceName:              "test-fs-device",
			pciAddr:                 "26:0c.0",
			shouldFailTransportStop: true,
			expectError:             true,
		},
		{
			name:                     "Manager destroy failure",
			snapProvider:             "test-provider",
			deviceName:               "test-fs-device",
			pciAddr:                  "26:0c.0",
			shouldFailManagerDestroy: true,
			expectError:              true,
		},
		{
			name:                       "Transport destroy failure",
			snapProvider:               "test-provider",
			deviceName:                 "test-fs-device",
			pciAddr:                    "26:0c.0",
			shouldFailTransportDestroy: true,
			expectError:                true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpcClient := NewMockClientForClientFunctions()

			// Configure for DestroyFSDevice pattern
			rpcClient.exposeDevicePattern = false
			// Configure failure scenarios
			rpcClient.shouldFailDeviceGet = tt.shouldFailDeviceGet
			rpcClient.shouldFailDeviceStop = tt.shouldFailDeviceStop
			rpcClient.shouldFailDeviceDestroy = tt.shouldFailDeviceDestroy
			rpcClient.shouldFailFunctionGet = tt.shouldFailFunctionGet
			rpcClient.shouldFailFunctionDestroy = tt.shouldFailFunctionDestroy
			rpcClient.shouldFailTransportStop = tt.shouldFailTransportStop
			rpcClient.shouldFailManagerDestroy = tt.shouldFailManagerDestroy
			rpcClient.shouldFailTransportDestroy = tt.shouldFailTransportDestroy

			client := NewClient(rpcClient)

			err := client.DestroyFSDevice(tt.deviceName, tt.pciAddr)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

/*
 * CMX key/value path.
 *
 * ExposeMemosDevice and DestroyMemosDevice build and tear down the NVMe front
 * end of a MEMOS volume. The volume itself is the nvcache plugin's, and arrives
 * here as dpuStatus.DeviceName, so none of these tests create one.
 */

// testMemosParameters is the NVMe front-end configuration for one CMX attachment.
func testMemosParameters() map[string]string {
	return map[string]string{
		ParamMemosSubsystemNQN: "nqn.2026-01.io.spdk:cmx0",
		ParamMemosNvmeManagers: "mlx5_0",
	}
}

// testMemosHotplugMock is a SNAP service with a hotplug-capable PCI switch.
func testMemosHotplugMock() *MockMemosClient {
	return &MockMemosClient{
		switches: []PCISwitch{{
			Name:     "mlx5_0",
			Types:    []string{PCIPortTypeNVMe},
			MaxPorts: 31,
		}},
	}
}

// testMemosStaticMock is a SNAP service with one free static emulation function
// and no hotplug slots, which is how a DPU without hotplug firmware presents.
func testMemosStaticMock() *MockMemosClient {
	return &MockMemosClient{
		switches: []PCISwitch{{Name: "mlx5_0", Types: []string{PCIPortTypeNVMe}}},
		nvmeFunctions: []DocaNvmeEmulationFunction{{
			VUID:   "MT2333XZ0NVMES1D0F0",
			Kind:   FunctionKindStatic,
			BDF:    testMemosPCIBDF,
			EmuMgr: "mlx5_0",
		}},
	}
}

// testMemosDPUStatus is an attachment whose plugin has already created the volume.
func testMemosDPUStatus() snapstoragev1.VolumeAttachmentStatusDPU {
	return snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "memos_vol_1"}
}

// testMemosHotplugSpec requests a hotplug function rather than a static one.
func testMemosHotplugSpec() snapstoragev1.VolumeAttachmentSpec {
	spec := snapstoragev1.VolumeAttachmentSpec{}
	spec.FunctionTypeConfig.HotplugFunction = true
	return spec
}

func TestExposeMemosDevice(t *testing.T) {
	mock := testMemosHotplugMock()
	c := &client{rpcClient: mock}

	nsid, pciBDF, nguid, funcVUID, err := c.ExposeMemosDevice(
		testMemosDPUStatus(), testMemosHotplugSpec(), testMemosParameters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if nsid != 1 {
		t.Errorf("nsid = %d, want 1", nsid)
	}
	if pciBDF != testMemosPCIBDF {
		t.Errorf("pciBDF = %q, want %q", pciBDF, testMemosPCIBDF)
	}
	if len(nguid) != 32 {
		t.Errorf("host NGUID %q is %d characters, want 32", nguid, len(nguid))
	}
	if funcVUID == "" {
		t.Errorf("expected a function VUID")
	}

	// The NVMe module has to be configured before the first subsystem, the
	// namespace and port before the controller, and the controller is hotplugged
	// last: that is what publishes the namespace to the host.
	assertCallOrder(t, mock.methodsCalled(), []string{
		"doca_nvme_set_config",
		"doca_nvme_subsystem_create",
		"doca_nvme_subsystem_ns_create",
		"pci_switch_port_create",
		"doca_nvme_subsystem_controller_create",
		"doca_nvme_subsystem_controller_hotplug",
	})

	// The namespace is a KV namespace over the MEMOS volume: that pairing is what
	// makes the volume reachable by host KV clients.
	nsParams := mock.paramsFor("doca_nvme_subsystem_ns_create")
	if nsParams["csi"] != CSIKV {
		t.Errorf("csi = %v, want %v", nsParams["csi"], CSIKV)
	}
	if nsParams["backend_name"] != "memos_vol_1" {
		t.Errorf("backend_name = %v, want memos_vol_1", nsParams["backend_name"])
	}
	if nsParams["nguid"] != nguid {
		t.Errorf("namespace nguid = %v, want the returned %v", nsParams["nguid"], nguid)
	}

	// The subsystem is dedicated to this volume; its handle is derived from the
	// volume name but prefixed so it does not collide with the MEMOS volume.
	if got := mock.paramsFor("doca_nvme_subsystem_create")["subsystem_name"]; got != "nvme_subsys_memos_vol_1" {
		t.Errorf("subsystem_name = %v, want nvme_subsys_memos_vol_1", got)
	}
	// The NQN embeds the volume so each volume gets a distinct subsystem NQN.
	wantNQN := testMemosParameters()[ParamMemosSubsystemNQN] + ":memos_vol_1"
	if got := mock.paramsFor("doca_nvme_subsystem_create")["nqn"]; got != wantNQN {
		t.Errorf("subsystem nqn = %v, want %q", got, wantNQN)
	}

	// The controller binds to the switch port that was just created.
	if got := mock.paramsFor("doca_nvme_subsystem_controller_create")["vuid"]; got != funcVUID {
		t.Errorf("controller vuid = %v, want %v", got, funcVUID)
	}
	if got := mock.paramsFor("pci_switch_port_create")["type"]; got != PCIPortTypeNVMe {
		t.Errorf("port type = %v, want %v", got, PCIPortTypeNVMe)
	}
	hotplugParams := mock.paramsFor("doca_nvme_subsystem_controller_hotplug")
	if hotplugParams["subsystem_name"] != "nvme_subsys_memos_vol_1" {
		t.Errorf("hotplug subsystem_name = %v, want the volume's subsystem nvme_subsys_memos_vol_1", hotplugParams["subsystem_name"])
	}
	if hotplugParams["cntl_id"] != 0 {
		t.Errorf("hotplug cntl_id = %v, want 0", hotplugParams["cntl_id"])
	}
	if mock.countOf("pci_switch_port_set_power") != 0 {
		t.Errorf("pci_switch_port_set_power should not run; hotplug is what publishes the controller")
	}
}

func TestExposeMemosDeviceClaimsAStaticFunction(t *testing.T) {
	mock := testMemosStaticMock()
	c := &client{rpcClient: mock}

	// A static function already exists on the DPU, so it is claimed rather than
	// created, and it has no power to switch on.
	_, pciBDF, _, funcVUID, err := c.ExposeMemosDevice(
		testMemosDPUStatus(), snapstoragev1.VolumeAttachmentSpec{}, testMemosParameters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pciBDF != testMemosPCIBDF || funcVUID != "MT2333XZ0NVMES1D0F0" {
		t.Errorf("got (%q, %q), want the static function", pciBDF, funcVUID)
	}
	for _, method := range []string{
		"pci_switch_port_create",
		"pci_switch_port_set_power",
		"doca_nvme_subsystem_controller_hotplug",
	} {
		if mock.countOf(method) != 0 {
			t.Errorf("%s should not run for a static function", method)
		}
	}
}

func TestExposeMemosDeviceIsIdempotent(t *testing.T) {
	mock := testMemosHotplugMock()
	c := &client{rpcClient: mock}

	nsid, pciBDF, nguid, funcVUID, err := c.ExposeMemosDevice(
		testMemosDPUStatus(), testMemosHotplugSpec(), testMemosParameters())
	if err != nil {
		t.Fatalf("unexpected error on the first call: %v", err)
	}

	// A retry sees what the first call recorded on the CR.
	status := testMemosDPUStatus()
	status.FuncVUID = funcVUID
	status.PCIDeviceAddress = pciBDF
	status.BdevAttrs.NVMeNsID = int64(nsid)
	status.BdevAttrs.NVMeUUID = nguid

	nsid2, pciBDF2, nguid2, funcVUID2, err := c.ExposeMemosDevice(
		status, testMemosHotplugSpec(), testMemosParameters())
	if err != nil {
		t.Fatalf("unexpected error on the second call: %v", err)
	}

	if nsid2 != nsid || pciBDF2 != pciBDF || nguid2 != nguid || funcVUID2 != funcVUID {
		t.Errorf("retry changed the result: (%d,%s,%s,%s) then (%d,%s,%s,%s)",
			nsid, pciBDF, nguid, funcVUID, nsid2, pciBDF2, nguid2, funcVUID2)
	}

	// Creating a second PCI function or namespace on retry would leak a PCIe
	// function and a host-visible namespace.
	for _, method := range []string{
		"doca_nvme_subsystem_create",
		"doca_nvme_subsystem_ns_create",
		"pci_switch_port_create",
		"doca_nvme_subsystem_controller_create",
		"doca_nvme_subsystem_controller_hotplug",
	} {
		if count := mock.countOf(method); count != 1 {
			t.Errorf("%s called %d times across two calls, want 1", method, count)
		}
	}

	// An existing subsystem means the module is already configured.
	if count := mock.countOf("doca_nvme_set_config"); count != 1 {
		t.Errorf("doca_nvme_set_config called %d times, want 1", count)
	}
}

// TestExposeMemosDeviceReusesExistingNamespaceSubsystem covers a retry after the
// configured subsystem name changed (for example an upgrade that renamed the
// shared subsystem). The namespace still lives in its original subsystem, so the
// controller must be created there - not in the newly configured subsystem -
// otherwise it would not expose the namespace. No new subsystem or namespace may
// be created on this path either.
func TestExposeMemosDeviceReusesExistingNamespaceSubsystem(t *testing.T) {
	const oldSubsystem = "nqn.2026-01.io.spdk:cmx-old"

	mock := &MockMemosClient{
		switches: []PCISwitch{{Name: "mlx5_0", Types: []string{PCIPortTypeNVMe}}},
		nvmeSubsystems: []DocaNvmeSubsystem{{
			SubsystemName: oldSubsystem,
			NQN:           oldSubsystem,
		}},
		nvmeNamespaces: []DocaNvmeNamespace{{
			NSID:          5,
			CSI:           CSIKV,
			Backend:       "memos_vol_1",
			SubsystemName: oldSubsystem,
		}},
		nvmeFunctions: []DocaNvmeEmulationFunction{{
			VUID:   "MT2333XZ0NVMES1D0F0",
			Kind:   FunctionKindStatic,
			BDF:    testMemosPCIBDF,
			EmuMgr: "mlx5_0",
		}},
	}
	c := &client{rpcClient: mock}

	// The parameters now configure a different subsystem than the one the
	// namespace was created in.
	params := map[string]string{
		ParamMemosSubsystemNQN: "nqn.2026-01.io.spdk:cmx-new",
		ParamMemosNvmeManagers: "mlx5_0",
	}

	nsid, pciBDF, _, funcVUID, err := c.ExposeMemosDevice(
		testMemosDPUStatus(), snapstoragev1.VolumeAttachmentSpec{}, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nsid != 5 {
		t.Errorf("nsid = %d, want the existing namespace's 5", nsid)
	}
	if pciBDF != testMemosPCIBDF || funcVUID != "MT2333XZ0NVMES1D0F0" {
		t.Errorf("got (%q, %q), want the static function", pciBDF, funcVUID)
	}

	// Neither a new subsystem nor a new namespace may be created on this path.
	if mock.countOf("doca_nvme_subsystem_create") != 0 {
		t.Errorf("a new subsystem was created; the existing namespace's subsystem must be reused")
	}
	if mock.countOf("doca_nvme_subsystem_ns_create") != 0 {
		t.Errorf("a new namespace was created; the existing one must be reused")
	}

	// The controller must land in the namespace's subsystem so it exposes it.
	if len(mock.nvmeControllers) != 1 {
		t.Fatalf("expected exactly one controller, got %d", len(mock.nvmeControllers))
	}
	if got := mock.nvmeControllers[0].SubsystemName; got != oldSubsystem {
		t.Errorf("controller subsystem = %q, want the namespace's %q", got, oldSubsystem)
	}
	if got := mock.paramsFor("doca_nvme_subsystem_controller_create")["subsystem_name"]; got != oldSubsystem {
		t.Errorf("controller_create subsystem_name = %v, want %q", got, oldSubsystem)
	}
}

func TestExposeMemosDeviceDefaultsToEverySupportedManager(t *testing.T) {
	mock := testMemosHotplugMock()
	mock.nvmeManagers = []DocaNvmeManager{{ManagerName: "mlx5_0"}, {ManagerName: "mlx5_1"}}

	parameters := testMemosParameters()
	delete(parameters, ParamMemosNvmeManagers)
	parameters[ParamMemosNvmeNetworkDevs] = "mlx5_2"

	_, _, _, _, err := (&client{rpcClient: mock}).ExposeMemosDevice(
		testMemosDPUStatus(), testMemosHotplugSpec(), parameters)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	params := mock.paramsFor("doca_nvme_set_config")
	if params["managers_dev_list"] != "mlx5_0,mlx5_1" {
		t.Errorf("managers_dev_list = %v, want mlx5_0,mlx5_1", params["managers_dev_list"])
	}
	// The network list is where the CMX traffic goes, so it is passed through.
	if params["network_dev_list"] != "mlx5_2" {
		t.Errorf("network_dev_list = %v, want mlx5_2", params["network_dev_list"])
	}
}

func TestExposeMemosDeviceRequiresASubsystemNQN(t *testing.T) {
	mock := testMemosHotplugMock()
	parameters := testMemosParameters()
	delete(parameters, ParamMemosSubsystemNQN)

	_, _, _, _, err := (&client{rpcClient: mock}).ExposeMemosDevice(
		testMemosDPUStatus(), testMemosHotplugSpec(), parameters)
	if err == nil {
		t.Fatalf("expected an error when %s is missing", ParamMemosSubsystemNQN)
	}
	if len(mock.methodsCalled()) != 0 {
		t.Errorf("an incomplete configuration must not issue RPCs, got %v", mock.methodsCalled())
	}
}

func TestExposeMemosDeviceRequiresTheVolume(t *testing.T) {
	// The volume is the namespace backend, so without it there is nothing to
	// expose: the plugin has not run yet.
	mock := testMemosHotplugMock()

	_, _, _, _, err := (&client{rpcClient: mock}).ExposeMemosDevice(
		snapstoragev1.VolumeAttachmentStatusDPU{}, testMemosHotplugSpec(), testMemosParameters())
	if err == nil {
		t.Fatalf("expected an error when the attachment has no device name")
	}
	if len(mock.methodsCalled()) != 0 {
		t.Errorf("a missing volume must not issue RPCs, got %v", mock.methodsCalled())
	}
}

func TestExposeMemosDeviceRejectsVFFunctionType(t *testing.T) {
	// The MEMOS path has no VF selection, so a vf request must be rejected
	// rather than silently exposing a PF and diverging from the spec.
	mock := testMemosHotplugMock()
	spec := testMemosHotplugSpec()
	spec.FunctionTypeConfig.FunctionType = snapstoragev1.FunctionTypeVF

	_, _, _, _, err := (&client{rpcClient: mock}).ExposeMemosDevice(
		testMemosDPUStatus(), spec, testMemosParameters())
	if err == nil {
		t.Fatalf("expected an error when the function type is vf")
	}
	if len(mock.methodsCalled()) != 0 {
		t.Errorf("a rejected function type must not issue RPCs, got %v", mock.methodsCalled())
	}
}

func TestExposeMemosDeviceRequiresNVMeCapableSwitch(t *testing.T) {
	mock := testMemosHotplugMock()
	mock.switches = []PCISwitch{{Name: "mlx5_0", Types: []string{"virtio_fs"}}}

	_, _, _, _, err := (&client{rpcClient: mock}).ExposeMemosDevice(
		testMemosDPUStatus(), testMemosHotplugSpec(), testMemosParameters())
	if err == nil {
		t.Fatalf("expected an error when no switch supports NVMe ports")
	}
	if mock.countOf("pci_switch_port_create") != 0 {
		t.Errorf("no port should be created on a switch that does not support the type")
	}
}

func TestDestroyMemosDevice(t *testing.T) {
	mock := testMemosHotplugMock()
	c := &client{rpcClient: mock}

	nsid, pciBDF, _, funcVUID, err := c.ExposeMemosDevice(
		testMemosDPUStatus(), testMemosHotplugSpec(), testMemosParameters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mock.calls, mock.params = nil, nil

	if err := c.DestroyMemosDevice(funcVUID, nsid, pciBDF, true, "memos_vol_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Hotunplug retracts the namespace from the host before anything backing it
	// is torn down, and the port is only destroyed once nothing is bound to it.
	assertCallOrder(t, mock.methodsCalled(), []string{
		"doca_nvme_subsystem_controller_hotunplug",
		"doca_nvme_subsystem_controller_destroy",
		"doca_nvme_subsystem_ns_destroy",
		"doca_nvme_subsystem_destroy",
		"pci_switch_port_destroy",
	})

	unplugParams := mock.paramsFor("doca_nvme_subsystem_controller_hotunplug")
	if unplugParams["cntl_id"] != 0 {
		t.Errorf("hotunplug cntl_id = %v, want 0", unplugParams["cntl_id"])
	}
	if mock.countOf("pci_switch_port_set_power") != 0 {
		t.Errorf("pci_switch_port_set_power should not run; hotunplug is what retracts the controller")
	}

	// Nothing may be left behind on the service.
	if len(mock.nvmeNamespaces) != 0 || len(mock.nvmeControllers) != 0 || len(mock.nvmeSubsystems) != 0 {
		t.Errorf("NVMe objects survived teardown: ns=%v ctrl=%v subsys=%v",
			mock.nvmeNamespaces, mock.nvmeControllers, mock.nvmeSubsystems)
	}
	if len(mock.switches[0].Ports) != 0 {
		t.Errorf("PCI switch port survived teardown: %v", mock.switches[0].Ports)
	}
}

// TestDestroyMemosDeviceRecoversAfterFailedTeardown reproduces a teardown that
// fails after the hotunplug has already cleared the switch port's BDF. The retry
// must still resolve every resource from the persisted funcVUID and finish the
// cleanup; anchoring on the now-stale BDF would silently orphan the controller,
// namespace, subsystem and port and still report success.
func TestDestroyMemosDeviceRecoversAfterFailedTeardown(t *testing.T) {
	mock := testMemosHotplugMock()
	c := &client{rpcClient: mock}

	nsid, pciBDF, _, funcVUID, err := c.ExposeMemosDevice(
		testMemosDPUStatus(), testMemosHotplugSpec(), testMemosParameters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if funcVUID == "" {
		t.Fatalf("attach did not record a funcVUID")
	}

	// The first teardown fails at controller destroy, which runs after the
	// hotunplug that clears the port's BDF.
	mock.failMethod = "doca_nvme_subsystem_controller_destroy"
	if err := c.DestroyMemosDevice(funcVUID, nsid, pciBDF, true, "memos_vol_1"); err == nil {
		t.Fatalf("expected the injected controller-destroy failure")
	}

	// The hotunplug cleared the BDF, so the port can no longer be found by it -
	// only the persisted funcVUID still identifies the resources.
	if _, _, found := findPCISwitchPortByBDF(mock.switches, pciBDF); found {
		t.Fatalf("precondition: hotunplug should have cleared the port BDF")
	}

	mock.failMethod = ""
	mock.calls, mock.params = nil, nil

	if err := c.DestroyMemosDevice(funcVUID, nsid, pciBDF, true, "memos_vol_1"); err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}

	assertCallOrder(t, mock.methodsCalled(), []string{
		"doca_nvme_subsystem_controller_destroy",
		"doca_nvme_subsystem_ns_destroy",
		"doca_nvme_subsystem_destroy",
		"pci_switch_port_destroy",
	})

	// The retry must leave nothing behind.
	if len(mock.nvmeNamespaces) != 0 || len(mock.nvmeControllers) != 0 || len(mock.nvmeSubsystems) != 0 {
		t.Errorf("NVMe objects survived teardown: ns=%v ctrl=%v subsys=%v",
			mock.nvmeNamespaces, mock.nvmeControllers, mock.nvmeSubsystems)
	}
	if len(mock.switches[0].Ports) != 0 {
		t.Errorf("PCI switch port survived teardown: %v", mock.switches[0].Ports)
	}
}

// TestDestroyMemosDeviceReleasesSubsystemAfterControllerDestroyed reproduces a
// teardown that fails only after the controller has already been destroyed, so
// the retry can no longer learn the subsystem name from a live controller. The
// volume owns a subsystem whose handle is the volume name, so the retry recovers
// it from deviceName and must still release the namespace and subsystem instead
// of leaking them and reporting success.
func TestDestroyMemosDeviceReleasesSubsystemAfterControllerDestroyed(t *testing.T) {
	mock := testMemosHotplugMock()
	c := &client{rpcClient: mock}

	nsid, pciBDF, _, funcVUID, err := c.ExposeMemosDevice(
		testMemosDPUStatus(), testMemosHotplugSpec(), testMemosParameters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The first teardown fails at namespace destroy, which runs after the
	// controller has already been destroyed.
	mock.failMethod = "doca_nvme_subsystem_ns_destroy"
	if err := c.DestroyMemosDevice(funcVUID, nsid, pciBDF, true, "memos_vol_1"); err == nil {
		t.Fatalf("expected the injected namespace-destroy failure")
	}
	if len(mock.nvmeControllers) != 0 {
		t.Fatalf("precondition: the controller should already be destroyed, got %v", mock.nvmeControllers)
	}
	if len(mock.nvmeNamespaces) != 1 || len(mock.nvmeSubsystems) != 1 {
		t.Fatalf("precondition: namespace and subsystem should still exist, ns=%v subsys=%v",
			mock.nvmeNamespaces, mock.nvmeSubsystems)
	}

	mock.failMethod = ""
	mock.calls, mock.params = nil, nil

	if err := c.DestroyMemosDevice(funcVUID, nsid, pciBDF, true, "memos_vol_1"); err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}

	// The retry has no controller to name the subsystem, so it must fall back to
	// the volume name to find and release the namespace and subsystem.
	if mock.countOf("doca_nvme_subsystem_ns_destroy") != 1 {
		t.Errorf("expected the namespace to be destroyed on retry, calls=%v", mock.methodsCalled())
	}
	if mock.countOf("doca_nvme_subsystem_destroy") != 1 {
		t.Errorf("expected the subsystem to be destroyed on retry, calls=%v", mock.methodsCalled())
	}
	if len(mock.nvmeNamespaces) != 0 || len(mock.nvmeControllers) != 0 || len(mock.nvmeSubsystems) != 0 {
		t.Errorf("NVMe objects survived teardown: ns=%v ctrl=%v subsys=%v",
			mock.nvmeNamespaces, mock.nvmeControllers, mock.nvmeSubsystems)
	}
	if len(mock.switches[0].Ports) != 0 {
		t.Errorf("PCI switch port survived teardown: %v", mock.switches[0].Ports)
	}
}

func TestDestroyMemosDeviceKeepsAStaticFunction(t *testing.T) {
	mock := testMemosStaticMock()
	c := &client{rpcClient: mock}

	nsid, pciBDF, _, funcVUID, err := c.ExposeMemosDevice(
		testMemosDPUStatus(), snapstoragev1.VolumeAttachmentSpec{}, testMemosParameters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mock.calls, mock.params = nil, nil

	if err := c.DestroyMemosDevice(funcVUID, nsid, pciBDF, false, "memos_vol_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A static function is permanent: it is neither hotunplugged nor destroyed.
	for _, method := range []string{
		"pci_switch_port_set_power",
		"pci_switch_port_destroy",
		"doca_nvme_subsystem_controller_hotunplug",
	} {
		if mock.countOf(method) != 0 {
			t.Errorf("%s should not run for a static function", method)
		}
	}
	if len(mock.nvmeControllers) != 0 || len(mock.nvmeNamespaces) != 0 {
		t.Errorf("NVMe objects survived teardown: ctrl=%v ns=%v", mock.nvmeControllers, mock.nvmeNamespaces)
	}
}

// TestExposeMemosDeviceIsolatesVolumesInDistinctSubsystems attaches two volumes
// and proves each lands in its own subsystem holding exactly one namespace and
// one controller. That is what keeps a PCI function seeing only its own volume's
// namespace: nothing associates a namespace with a controller other than sharing
// a subsystem, so distinct subsystems are what isolate the attachments.
func TestExposeMemosDeviceIsolatesVolumesInDistinctSubsystems(t *testing.T) {
	mock := testMemosHotplugMock()
	c := &client{rpcClient: mock}

	if _, _, _, _, err := c.ExposeMemosDevice(
		testMemosDPUStatus(), testMemosHotplugSpec(), testMemosParameters()); err != nil {
		t.Fatalf("unexpected error attaching the first volume: %v", err)
	}

	second := testMemosDPUStatus()
	second.DeviceName = "memos_vol_2"
	if _, _, _, _, err := c.ExposeMemosDevice(second, testMemosHotplugSpec(), testMemosParameters()); err != nil {
		t.Fatalf("unexpected error attaching the second volume: %v", err)
	}

	// Two volumes must produce two subsystems, each with its own namespace and
	// controller - never one shared subsystem holding both namespaces.
	if len(mock.nvmeSubsystems) != 2 {
		t.Fatalf("expected one subsystem per volume, got %v", mock.nvmeSubsystems)
	}
	perSubsystem := map[string]struct{ namespaces, controllers int }{}
	for _, ns := range mock.nvmeNamespaces {
		entry := perSubsystem[ns.SubsystemName]
		entry.namespaces++
		perSubsystem[ns.SubsystemName] = entry
	}
	for _, ctrl := range mock.nvmeControllers {
		entry := perSubsystem[ctrl.SubsystemName]
		entry.controllers++
		perSubsystem[ctrl.SubsystemName] = entry
	}
	for _, volume := range []string{"nvme_subsys_memos_vol_1", "nvme_subsys_memos_vol_2"} {
		entry, ok := perSubsystem[volume]
		if !ok {
			t.Errorf("volume %q has no dedicated subsystem, got %v", volume, perSubsystem)
			continue
		}
		if entry.namespaces != 1 || entry.controllers != 1 {
			t.Errorf("subsystem %q holds %d namespaces and %d controllers, want 1 and 1",
				volume, entry.namespaces, entry.controllers)
		}
	}
}

// TestDestroyMemosDeviceLeavesOtherVolumesIntact tears one volume down and proves
// the other volume's dedicated subsystem, namespace and controller are untouched.
func TestDestroyMemosDeviceLeavesOtherVolumesIntact(t *testing.T) {
	mock := testMemosHotplugMock()
	c := &client{rpcClient: mock}

	nsid, pciBDF, _, funcVUID, err := c.ExposeMemosDevice(
		testMemosDPUStatus(), testMemosHotplugSpec(), testMemosParameters())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second := testMemosDPUStatus()
	second.DeviceName = "memos_vol_2"
	if _, _, _, _, err := c.ExposeMemosDevice(second, testMemosHotplugSpec(), testMemosParameters()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mock.calls, mock.params = nil, nil

	if err := c.DestroyMemosDevice(funcVUID, nsid, pciBDF, true, "memos_vol_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The detached volume owns its subsystem, so that subsystem is destroyed
	// along with its single namespace.
	if mock.countOf("doca_nvme_subsystem_destroy") != 1 {
		t.Errorf("the detached volume's dedicated subsystem should be destroyed")
	}
	if mock.countOf("doca_nvme_subsystem_ns_destroy") != 1 {
		t.Errorf("expected the requested namespace to be destroyed")
	}

	// The other volume is in a different subsystem, so it is left fully intact.
	if len(mock.nvmeNamespaces) != 1 || mock.nvmeNamespaces[0].Backend != "memos_vol_2" {
		t.Errorf("the surviving attachment's namespace was disturbed: %v", mock.nvmeNamespaces)
	}
	if len(mock.nvmeSubsystems) != 1 || mock.nvmeSubsystems[0].SubsystemName != "nvme_subsys_memos_vol_2" {
		t.Errorf("the surviving attachment's subsystem was disturbed: %v", mock.nvmeSubsystems)
	}
	if len(mock.nvmeControllers) != 1 || mock.nvmeControllers[0].SubsystemName != "nvme_subsys_memos_vol_2" {
		t.Errorf("the surviving attachment's controller was disturbed: %v", mock.nvmeControllers)
	}
}

func TestGetMemosFuncVUID(t *testing.T) {
	mock := testMemosStaticMock()
	c := &client{rpcClient: mock}

	vuid, err := c.GetMemosFuncVUID(testMemosPCIBDF)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vuid != "MT2333XZ0NVMES1D0F0" {
		t.Errorf("vuid = %q, want the static function at %s", vuid, testMemosPCIBDF)
	}
	if mock.countOf("doca_nvme_get_emulation_functions") < 1 {
		t.Errorf("expected doca_nvme_get_emulation_functions")
	}

	if _, err := c.GetMemosFuncVUID("5F:00.0"); err == nil {
		t.Errorf("expected an error for a BDF that is not on the function list")
	}
}

func TestDestroyMemosDeviceToleratesMissingObjects(t *testing.T) {
	mock := testMemosHotplugMock()

	// Detaching something already torn down has to converge: the controller
	// retries detachment until it succeeds. An empty funcVUID exercises the
	// legacy BDF-only fallback for attachments that never recorded one.
	if err := (&client{rpcClient: mock}).DestroyMemosDevice("", 1, testMemosPCIBDF, true, "memos_vol_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, method := range []string{
		"doca_nvme_subsystem_controller_hotunplug",
		"doca_nvme_subsystem_controller_destroy",
		"doca_nvme_subsystem_ns_destroy",
		"pci_switch_port_destroy",
	} {
		if mock.countOf(method) != 0 {
			t.Errorf("%s should not run when the object is already gone", method)
		}
	}
}
