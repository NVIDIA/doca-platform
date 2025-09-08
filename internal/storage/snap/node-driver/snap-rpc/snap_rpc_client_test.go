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
	shouldFailEmulationList          bool
	shouldFailSubsystemList          bool
	shouldFailNamespaceCreate        bool
	shouldFailControllerCreate       bool
	shouldFailControllerAttach       bool
	shouldFailControllerResume       bool
	shouldFailControllerDetach       bool
	shouldFailControllerDestroy      bool
	shouldFailNamespaceDestroy       bool
	shouldFailEmulationAttach        bool
	shouldFailEmulationDetach        bool
	shouldFailEmulationDetachPrepare bool

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

	case "nvme_emulation_device_attach":
		if m.shouldFailEmulationAttach {
			return nil, fmt.Errorf("failed to attach NVMe emulation device")
		}
		return "MT2323XZ09G2NVMES1D0F0", nil

	case "nvme_emulation_device_detach_prepare":
		if m.shouldFailEmulationDetachPrepare {
			return nil, fmt.Errorf("failed to prepare NVMe emulation device detach")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_emulation_device_detach":
		if m.shouldFailEmulationDetach {
			return nil, fmt.Errorf("failed to detach NVMe emulation device")
		}
		return map[string]interface{}{"status": "success"}, nil

	case "nvme_subsystem_list":
		if m.shouldFailSubsystemList {
			return nil, fmt.Errorf("failed to get subsystems")
		}
		return mockNvmeSubsystemList, nil

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
				return []FSDevice{
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
				}, nil
			default:
				return []FSDevice{
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
				}, nil
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
		name                       string
		snapProvider               string
		dpuStatus                  snapstoragev1.VolumeAttachmentStatusDPU
		spec                       snapstoragev1.VolumeAttachmentSpec
		shouldFailEmulationList    bool
		shouldFailSubsystemList    bool
		shouldFailNamespaceCreate  bool
		shouldFailControllerCreate bool
		shouldFailControllerAttach bool
		shouldFailControllerResume bool
		shouldFailEmulationAttach  bool
		shouldFailEmulationDetach  bool
		expectError                bool
		expectedNSID               int
		expectedPCIBDF             string
		expectedUUID               string
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
			expectError:    false,
			expectedNSID:   2,
			expectedPCIBDF: "26:0c.1",
		},
		{
			name:         "Use existing namespace",
			snapProvider: "test-provider",
			dpuStatus:    snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "null1"},
			spec: snapstoragev1.VolumeAttachmentSpec{
				Parameters: map[string]string{},
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType: "vf",
				},
			},
			expectError:    false,
			expectedNSID:   1,
			expectedPCIBDF: "26:0c.0",
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
			expectError:    false,
			expectedNSID:   5,
			expectedPCIBDF: "26:0c.2",
			expectedUUID:   "550e8400-e29b-41d4-a716-446655440000",
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
			name:                       "Controller create failure",
			dpuStatus:                  snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "new-device"},
			spec:                       snapstoragev1.VolumeAttachmentSpec{},
			shouldFailControllerCreate: true,
			expectError:                true,
		},
		{
			name: "Use DPU status values with hotplug",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				DeviceName:       "test-device",
				PCIDeviceAddress: "26:00.3",
			},
			spec: snapstoragev1.VolumeAttachmentSpec{
				Parameters: map[string]string{},
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType:    "vf",
					HotplugFunction: true,
				},
			},
			expectError:    false,
			expectedNSID:   2,
			expectedPCIBDF: "26:00.3",
		},
		{
			name:      "Emulation device attach failure with hotplug",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{DeviceName: "test-device"},
			spec: snapstoragev1.VolumeAttachmentSpec{
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType:    "vf",
					HotplugFunction: true,
				},
			},
			shouldFailEmulationAttach: true,
			expectError:               true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpcClient := NewMockClientForClientFunctions()
			rpcClient.shouldFailEmulationList = tt.shouldFailEmulationList
			rpcClient.shouldFailSubsystemList = tt.shouldFailSubsystemList
			rpcClient.shouldFailNamespaceCreate = tt.shouldFailNamespaceCreate
			rpcClient.shouldFailControllerCreate = tt.shouldFailControllerCreate
			rpcClient.shouldFailControllerAttach = tt.shouldFailControllerAttach
			rpcClient.shouldFailControllerResume = tt.shouldFailControllerResume
			rpcClient.shouldFailEmulationAttach = tt.shouldFailEmulationAttach
			rpcClient.shouldFailEmulationDetach = tt.shouldFailEmulationDetach

			client := NewClient(rpcClient)

			nsid, pciBDF, uuid, err := client.ExposeBlockDevice(tt.dpuStatus, tt.spec)

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
	}{
		{
			name:         "Use existing PCI address",
			snapProvider: "test-provider",
			deviceName:   "test-fs-device",
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				PCIDeviceAddress: "26:0c.1",
			},
			parameters:      map[string]string{},
			expectError:     false,
			expectedTag:     "test-fs-devicetag",
			expectedPCIAddr: "26:0c.1",
		},
		{
			name:         "Device already exists (skip creation)",
			snapProvider: "test-provider",
			deviceName:   "test-fs-device",
			dpuStatus:    snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:   map[string]string{},
			expectError:  false,
			expectedTag:  "test-fs-devicetag",
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

			tag, pciAddr, err := client.ExposeFSDevice(tt.deviceName, tt.dpuStatus, tt.parameters)

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
		})
	}
}

func TestDestroyBlockDevice(t *testing.T) {
	tests := []struct {
		name                        string
		snapProvider                string
		nsid                        int
		pciAddr                     string
		shouldFailEmulationList     bool
		shouldFailSubsystemList     bool
		shouldFailControllerDetach  bool
		shouldFailControllerDestroy bool
		shouldFailNamespaceDestroy  bool
		shouldFailEmulationDetach   bool
		expectError                 bool
		hotplug                     bool
	}{
		{
			name:         "Destroy block device successfully",
			snapProvider: "test-provider",
			nsid:         1,
			pciAddr:      "26:0c.0",
			expectError:  false,
			hotplug:      false,
		},
		{
			name:         "Destroy non-existent device",
			snapProvider: "test-provider",
			nsid:         999,
			pciAddr:      "26:0c.9",
			expectError:  false,
			hotplug:      false,
		},
		{
			name:                    "Emulation list failure",
			snapProvider:            "test-provider",
			nsid:                    1,
			pciAddr:                 "26:0c.0",
			shouldFailEmulationList: true,
			expectError:             true,
			hotplug:                 false,
		},
		{
			name:                    "Subsystem list failure",
			snapProvider:            "test-provider",
			nsid:                    1,
			pciAddr:                 "26:0c.0",
			shouldFailSubsystemList: true,
			expectError:             true,
			hotplug:                 false,
		},
		{
			name:                       "Controller detach failure",
			snapProvider:               "test-provider",
			nsid:                       1,
			pciAddr:                    "26:0c.0",
			shouldFailControllerDetach: true,
			expectError:                true,
			hotplug:                    false,
		},
		{
			name:                        "Controller destroy failure",
			snapProvider:                "test-provider",
			nsid:                        1,
			pciAddr:                     "26:0c.0",
			shouldFailControllerDestroy: true,
			expectError:                 true,
			hotplug:                     false,
		},
		{
			name:                       "Namespace destroy failure",
			snapProvider:               "test-provider",
			nsid:                       1,
			pciAddr:                    "26:0c.0",
			shouldFailNamespaceDestroy: true,
			expectError:                true,
			hotplug:                    false,
		},
		{
			name:                      "Emulation detach failure with hotplug",
			snapProvider:              "test-provider",
			nsid:                      1,
			pciAddr:                   "26:00.3",
			shouldFailEmulationDetach: true,
			expectError:               true,
			hotplug:                   true,
		},
		{
			name:                    "Emulation list failure with hotplug",
			snapProvider:            "test-provider",
			nsid:                    1,
			pciAddr:                 "26:00.3",
			shouldFailEmulationList: true,
			expectError:             true,
			hotplug:                 true,
		},
		{
			name:                    "Subsystem list failure with hotplug",
			snapProvider:            "test-provider",
			nsid:                    1,
			pciAddr:                 "26:00.3",
			shouldFailSubsystemList: true,
			expectError:             true,
			hotplug:                 true,
		},
		{
			name:                       "Controller detach failure with hotplug",
			snapProvider:               "test-provider",
			nsid:                       1,
			pciAddr:                    "26:00.3",
			shouldFailControllerDetach: true,
			expectError:                true,
			hotplug:                    true,
		},
		{
			name:                        "Controller destroy failure with hotplug",
			snapProvider:                "test-provider",
			nsid:                        1,
			pciAddr:                     "26:00.3",
			shouldFailControllerDestroy: true,
			expectError:                 true,
			hotplug:                     true,
		},
		{
			name:                       "Namespace destroy failure with hotplug",
			snapProvider:               "test-provider",
			nsid:                       1,
			pciAddr:                    "26:00.3",
			shouldFailNamespaceDestroy: true,
			expectError:                true,
			hotplug:                    true,
		},
		{
			name:                      "Emulation device detach failure with hotplug",
			snapProvider:              "test-provider",
			nsid:                      1,
			pciAddr:                   "26:00.3",
			shouldFailEmulationDetach: true,
			expectError:               true,
			hotplug:                   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpcClient := NewMockClientForClientFunctions()
			rpcClient.shouldFailEmulationList = tt.shouldFailEmulationList
			rpcClient.shouldFailSubsystemList = tt.shouldFailSubsystemList
			rpcClient.shouldFailControllerDetach = tt.shouldFailControllerDetach
			rpcClient.shouldFailControllerDestroy = tt.shouldFailControllerDestroy
			rpcClient.shouldFailNamespaceDestroy = tt.shouldFailNamespaceDestroy
			rpcClient.shouldFailEmulationDetach = tt.shouldFailEmulationDetach

			client := NewClient(rpcClient)

			err := client.DestroyBlockDevice(tt.nsid, tt.pciAddr, tt.hotplug)

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
