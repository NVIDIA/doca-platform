/*
Copyright 2025 NVIDIA

  Licensed under the Apache License, Version 2.0 (the License);
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

      http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an AS IS BASIS,
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

	"github.com/google/uuid"
)

// Global Mock Emulation Function List Response
var mockEmulationFunctionList = EmulationFunctionListResponse{
	{
		Hotplugged:    false,
		EmulationType: NVMeProtocol,
		PFIndex:       0,
		PCIBDF:        "26:00.2",
		VHCAID:        2,
		VUID:          "MT2328XZ17DFNVMES0D0F2",
		VFs: []VF{
			{
				Hotplugged:    false,
				EmulationType: NVMeProtocol,
				PFIndex:       0,
				VFIndex:       0,
				PCIBDF:        "26:0c.0",
				VHCAID:        98,
				VUID:          "MT2328XZ17DFNVMES0D0F2VF1",
				CtrlID:        "NVMeCtrl2",
			},
			{
				Hotplugged:    false,
				EmulationType: NVMeProtocol,
				PFIndex:       0,
				VFIndex:       1,
				PCIBDF:        "26:0c.1",
				VHCAID:        99,
				VUID:          "MT2328XZ17DFNVMES0D0F2VF2",
			},
			{
				Hotplugged:    false,
				EmulationType: NVMeProtocol,
				PFIndex:       0,
				VFIndex:       2,
				PCIBDF:        "26:0c.2",
				VHCAID:        100,
				VUID:          "MT2328XZ17DFNVMES0D0F2VF3",
			},
		},
	},
	{
		Hotplugged:    true,
		EmulationType: NVMeProtocol,
		PFIndex:       1,
		PCIBDF:        "26:00.3",
		VHCAID:        3,
		VUID:          "MT2323XZ09G2NVMES1D0F0",
		CtrlID:        "NVMeCtrl_26:00.3",
		VFs:           []VF{},
	},
}

// Global Mock NVMe Subsystem List Response
var mockNvmeSubsystemList = NvmeSubsystemListResponse{
	{
		NQN:  "nqn.2022-10.io.nvda.nvme:01",
		MN:   "BlueField NVMe SNAP Controller",
		SN:   "MNC12",
		MNAN: 1024,
		NN:   1024,
		Controllers: []interface{}{
			map[string]interface{}{
				"ctrl_id":    "NVMeCtrl2",
				"mdts":       7,
				"vhca_id":    98,
				"nqn":        "nqn.2022-10.io.nvda.nvme:01",
				"plugged":    true,
				"state":      "STARTED",
				"num_queues": 16,
				"max_queues": 32,
				"namespaces": []interface{}{
					map[string]interface{}{
						"nsid": 1,
						"bdev": "null1",
						"uuid": "263826ad-19a3-4feb-bc25-4bc81ee7748e",
					},
				},
			},
		},
		Namespaces: []Namespace{
			{
				NSID:                  1,
				Bdev:                  "null1",
				Ready:                 "No",
				NQN:                   "nqn.2022-10.io.nvda.nvme:01",
				UUID:                  "263826ad-19a3-4feb-bc25-4bc81ee7748e",
				MaxInflightsPerWeight: 65535,
				Controllers: []interface{}{
					map[string]interface{}{
						"ctrl_id": "NVMeCtrl2",
					},
				},
			},
		},
	},
	{
		NQN:  "nqn.2022-10.io.nvda.nvme:0",
		MN:   "BlueField NVMe SNAP Controller",
		SN:   "MNC12",
		MNAN: 1024,
		NN:   1024,
		Controllers: []interface{}{
			map[string]interface{}{
				"ctrl_id":    "NVMeCtrl1",
				"mdts":       7,
				"vhca_id":    2,
				"nqn":        "nqn.2022-10.io.nvda.nvme:0",
				"plugged":    true,
				"state":      "STARTED",
				"num_queues": 16,
				"max_queues": 32,
				"namespaces": []interface{}{
					map[string]interface{}{
						"nsid": 1,
						"bdev": "null0",
						"uuid": "263826ad-19a3-4feb-bc25-4bc81ee7749e",
					},
				},
			},
		},
		Namespaces: []Namespace{
			{
				NSID:                  1,
				Bdev:                  "null0",
				Ready:                 "No",
				NQN:                   "nqn.2022-10.io.nvda.nvme:0",
				UUID:                  "263826ad-19a3-4feb-bc25-4bc81ee7749e",
				MaxInflightsPerWeight: 65535,
				Controllers: []interface{}{
					map[string]interface{}{
						"ctrl_id": "NVMeCtrl1",
					},
				},
			},
		},
	},
}

// MockJSONRPCSnapClient is a mock implementation for testing
type MockJSONRPCSnapClient struct {
	requestID int
	timeout   time.Duration
}

// NewMockClient creates a new mock client for testing
func NewMockClient() JSONRPCClient {
	return &MockJSONRPCSnapClient{
		requestID: 0,
		timeout:   60 * time.Second,
	}
}

// Send implements the Send method for the mock client
func (m *MockJSONRPCSnapClient) Send(method string, params map[string]interface{}) (int, error) {
	m.requestID++
	return m.requestID, nil
}

// Recv implements the Recv method for the mock client
func (m *MockJSONRPCSnapClient) Recv() (map[string]interface{}, error) {
	return map[string]interface{}{
		"result": map[string]interface{}{
			"status": "success",
		},
	}, nil
}

// Call implements the Call method for the mock client
func (m *MockJSONRPCSnapClient) Call(method string, params map[string]interface{}) (interface{}, error) {
	switch method {
	case "nvme_namespace_create":
		return map[string]interface{}{
			"status":    "success",
			"bdev_name": params["bdev_name"],
			"nsid":      params["nsid"],
		}, nil
	case "nvme_controller_create":
		return map[string]interface{}{
			"status":  "success",
			"ctrl_id": fmt.Sprintf("NVMeCtrl_%s", params["pci_bdf"]),
		}, nil
	case "nvme_function_create":
		return map[string]interface{}{
			"vhca_id": 7,
			"vuid":    "MT2328XZ17DFNVMES1D0F0",
		}, nil
	case "nvme_controller_hotplug":
		return map[string]interface{}{
			"status": "success",
		}, nil
	case "nvme_controller_hotunplug":
		return map[string]interface{}{
			"status": "success",
		}, nil
	case "nvme_function_destroy":
		return map[string]interface{}{
			"status": "success",
		}, nil
	default:
		return nil, fmt.Errorf("unexpected method: %s", method)
	}
}

// Close implements the Close method for the mock client
func (m *MockJSONRPCSnapClient) Close() error {
	return nil
}

func TestNvmeNamespaceCreate(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		subsystems  NvmeSubsystemListResponse
		dpuStatus   snapstoragev1.VolumeAttachmentStatusDPU
		expectError bool
		expectNSID  int
		expectUUID  interface{} // can be bool or string
	}{
		{
			name:        "Create new namespace with generated UUID",
			deviceName:  "test-device",
			subsystems:  mockNvmeSubsystemList,
			dpuStatus:   snapstoragev1.VolumeAttachmentStatusDPU{},
			expectError: false,
			expectNSID:  2,    // Based on mock data where first namespace has NSID 1
			expectUUID:  true, // expect a valid UUID
		},
		{
			name:       "Create namespace with existing DPU status",
			deviceName: "test-device",
			subsystems: mockNvmeSubsystemList,
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				DeviceName: "existing-device",
				BdevAttrs: snapstoragev1.BdevAttrs{
					NVMeNsID: 5,
					NVMeUUID: "550e8400-e29b-41d4-a716-446655440000",
				},
			},
			expectError: false,
			expectNSID:  5,
			expectUUID:  "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:        "Empty subsystems list",
			deviceName:  "test-device",
			subsystems:  NvmeSubsystemListResponse{},
			dpuStatus:   snapstoragev1.VolumeAttachmentStatusDPU{},
			expectError: true,
			expectNSID:  0,
			expectUUID:  false,
		},
		{
			name:       "Empty namespaces list",
			deviceName: "test-device",
			subsystems: NvmeSubsystemListResponse{
				{
					NQN:        "nqn.2022-10.io.nvda.nvme:test",
					Namespaces: []Namespace{},
				},
			},
			dpuStatus:   snapstoragev1.VolumeAttachmentStatusDPU{},
			expectError: false,
			expectNSID:  1, // Should start with NSID 1 for empty namespace list
			expectUUID:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockClient()

			nsid, uuidStr, err := NvmeNamespaceCreate(client, tt.deviceName, tt.subsystems, tt.dpuStatus)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if nsid != tt.expectNSID {
				t.Errorf("Expected NSID %d, got %d", tt.expectNSID, nsid)
			}

			// Handle UUID validation based on the type of expectUUID
			switch expected := tt.expectUUID.(type) {
			case bool:
				if expected {
					if uuidStr == "" {
						t.Error("Expected non-empty UUID, got empty string")
					}
					// Verify UUID format
					_, err := uuid.Parse(uuidStr)
					if err != nil {
						t.Errorf("Invalid UUID format: %v", err)
					}
				} else {
					// Expect no UUID
					if uuidStr != "" {
						t.Errorf("Expected empty UUID, got %s", uuidStr)
					}
				}
			case string:
				// Check for a specific UUID value
				if uuidStr != expected {
					t.Errorf("Expected specific UUID %s, got %s", expected, uuidStr)
				}
			}
		})
	}
}

func TestNvmeControllerCreate(t *testing.T) {
	tests := []struct {
		name           string
		subsystems     NvmeSubsystemListResponse
		emulationFuncs EmulationFunctionListResponse
		dpuStatus      snapstoragev1.VolumeAttachmentStatusDPU
		parameters     map[string]string
		functionType   string
		expectError    bool
		expectCtrlID   string
		expectPciBDF   string
	}{
		{
			name:           "Create controller with available VF",
			subsystems:     mockNvmeSubsystemList,
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:     map[string]string{},
			functionType:   "vf",
			expectError:    false,
			expectCtrlID:   "NVMeCtrl_26:0c.1",
			expectPciBDF:   "26:0c.1",
		},
		{
			name:           "Create controller with PF",
			subsystems:     mockNvmeSubsystemList,
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:     map[string]string{},
			functionType:   "pf",
			expectError:    false,
			expectCtrlID:   "NVMeCtrl_26:00.2",
			expectPciBDF:   "26:00.2",
		},
		{
			name:           "Create controller with existing DPU status",
			subsystems:     mockNvmeSubsystemList,
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{PCIDeviceAddress: "26:0c.3"},
			functionType:   "vf",
			expectError:    false,
			expectCtrlID:   "NVMeCtrl_26:0c.3",
			expectPciBDF:   "26:0c.3",
		},
		{
			name:           "Create controller with VUID",
			subsystems:     mockNvmeSubsystemList,
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:     map[string]string{"vuid": "MT2323XZ09G2NVMES1D0F0"},
			functionType:   "vf",
			expectError:    false,
			expectCtrlID:   "NVMeCtrl_26:00.3",
			expectPciBDF:   "26:00.3",
		},
		{
			name:           "Empty subsystems list",
			subsystems:     NvmeSubsystemListResponse{},
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			functionType:   "vf",
			expectError:    true,
			expectCtrlID:   "",
			expectPciBDF:   "",
		},
		{
			name:       "No available VFs",
			subsystems: mockNvmeSubsystemList,
			emulationFuncs: EmulationFunctionListResponse{
				{
					Hotplugged:    false,
					EmulationType: NVMeProtocol,
					PFIndex:       0,
					PCIBDF:        "26:00.2",
					VHCAID:        2,
					VUID:          "MT2328XZ17DFNVMES0D0F2",
					VFs: []VF{
						{
							Hotplugged:    false,
							EmulationType: NVMeProtocol,
							PFIndex:       0,
							VFIndex:       0,
							PCIBDF:        "26:0c.0",
							VHCAID:        98,
							VUID:          "MT2328XZ17DFNVMES0D0F2VF1",
							CtrlID:        "NVMeCtrl2",
						},
					},
				},
			},
			dpuStatus:    snapstoragev1.VolumeAttachmentStatusDPU{},
			functionType: "vf",
			expectError:  true,
			expectCtrlID: "",
			expectPciBDF: "",
		},
		{
			name:           "No available PF",
			subsystems:     mockNvmeSubsystemList,
			emulationFuncs: EmulationFunctionListResponse{},
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			functionType:   "pf",
			expectError:    true,
			expectCtrlID:   "",
			expectPciBDF:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockClient()

			ctrlID, pciBDF, err := NvmeControllerCreate(client, tt.subsystems, tt.emulationFuncs, tt.dpuStatus, tt.parameters, tt.functionType)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if ctrlID != tt.expectCtrlID {
				t.Errorf("Expected controller ID %s, got %s", tt.expectCtrlID, ctrlID)
			}

			if pciBDF != tt.expectPciBDF {
				t.Errorf("Expected PCI BDF %s, got %s", tt.expectPciBDF, pciBDF)
			}
		})
	}
}

func TestGetNvmeControllerByPciAddr(t *testing.T) {
	tests := []struct {
		name               string
		pciAddr            string
		expectedController string
		expectError        bool
	}{
		{
			name:               "Valid PCI BDF - Should return NVMeCtrl2",
			pciAddr:            "26:0c.0",
			expectedController: "NVMeCtrl2",
			expectError:        false,
		},
		{
			name:               "Invalid PCI BDF - Should return error",
			pciAddr:            "26:0c.3",
			expectedController: "",
			expectError:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrlID := getNvmeControllerByPciAddr(tt.pciAddr, mockEmulationFunctionList)
			if ctrlID != tt.expectedController {
				t.Errorf("Expected controller '%s', got '%s'", tt.expectedController, ctrlID)
			}
		})
	}
}

func TestGetPciAddrByCtrlID(t *testing.T) {
	tests := []struct {
		name        string
		ctrlID      string
		expectedBDF string
		expectError bool
		hotplug     bool
	}{
		{
			name:        "Valid Controller ID - Should return PCI BDF 26:0c.0",
			ctrlID:      "NVMeCtrl2",
			expectedBDF: "26:0c.0",
			expectError: false,
			hotplug:     false,
		},
		{
			name:        "Invalid Controller ID - Should return error",
			ctrlID:      "NVMeCtrlX",
			expectedBDF: "",
			expectError: true,
			hotplug:     false,
		},
		{
			name:        "Hot-plugged PF - should return PF BDF",
			ctrlID:      "NVMeCtrl_26:00.3",
			expectedBDF: "26:00.3",
			expectError: false,
			hotplug:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bdf, err := getPciAddrByCtrlID(tt.ctrlID, mockEmulationFunctionList, tt.hotplug)

			if tt.expectError {
				if err == nil {
					t.Fatalf("Expected error but got nil for controller ID %s", tt.ctrlID)
				}
				expectedErrMsg := fmt.Sprintf("no PCI address found for NVMe controller ID %s", tt.ctrlID)
				if err.Error() != expectedErrMsg {
					t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if bdf != tt.expectedBDF {
					t.Errorf("Expected PCI BDF '%s', got '%s'", tt.expectedBDF, bdf)
				}
			}
		})
	}
}

func TestGetNamespaceByDeviceName(t *testing.T) {
	tests := []struct {
		name         string
		deviceName   string
		expectedNSID int
	}{
		{
			name:         "Valid Device Name - Should return NSID 1",
			deviceName:   "null1",
			expectedNSID: 1,
		},
		{
			name:         "Invalid Device Name - Should return -1",
			deviceName:   "non-existent-device",
			expectedNSID: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nsid := getNamespaceByDeviceName(tt.deviceName, mockNvmeSubsystemList)

			if nsid != tt.expectedNSID {
				t.Errorf("Test failed: Expected NSID %d, but got %d", tt.expectedNSID, nsid)
			}
		})
	}
}

func TestCheckNamespaceAttached(t *testing.T) {
	tests := []struct {
		name                    string
		nsid                    int
		ctrlID                  string
		expectedNamespaceExists bool
		expectedAttachedToCtrl  bool
	}{
		{
			name:                    "Valid NSID and attached controller",
			nsid:                    1,
			ctrlID:                  "NVMeCtrl2",
			expectedNamespaceExists: true,
			expectedAttachedToCtrl:  true,
		},
		{
			name:                    "Valid NSID but unattached controller",
			nsid:                    1,
			ctrlID:                  "NVMeCtrl_26:00.3",
			expectedNamespaceExists: true,
			expectedAttachedToCtrl:  false,
		},
		{
			name:                    "Invalid NSID with valid controller",
			nsid:                    999,
			ctrlID:                  "NVMeCtrl2",
			expectedNamespaceExists: false,
			expectedAttachedToCtrl:  false,
		},
		{
			name:                    "Invalid NSID with invalid controller",
			nsid:                    999,
			ctrlID:                  "NVMeCtrl_26:00.3",
			expectedNamespaceExists: false,
			expectedAttachedToCtrl:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespaceExists, attachedToCtrl := checkNamespaceAttached(tt.nsid, tt.ctrlID, mockNvmeSubsystemList)

			if namespaceExists != tt.expectedNamespaceExists {
				t.Errorf("Test failed: Expected namespaceExists %v, got %v for NSID %d and Controller %s",
					tt.expectedNamespaceExists, namespaceExists, tt.nsid, tt.ctrlID)
			}
			if attachedToCtrl != tt.expectedAttachedToCtrl {
				t.Errorf("Test failed: Expected attachedToCtrl %v, got %v for NSID %d and Controller %s",
					tt.expectedAttachedToCtrl, attachedToCtrl, tt.nsid, tt.ctrlID)
			}
		})
	}
}

func TestGetCtrlByDeviceName(t *testing.T) {
	tests := []struct {
		name           string
		deviceName     string
		expectedCtrlID string
	}{
		{
			name:           "Valid Device Name - Should return NVMeCtrl2",
			deviceName:     "null1",
			expectedCtrlID: "NVMeCtrl2",
		},
		{
			name:           "Invalid Device Name - Should return empty string",
			deviceName:     "non-existent-device",
			expectedCtrlID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrlID := getCtrlByDeviceName(tt.deviceName, mockNvmeSubsystemList)

			if ctrlID != tt.expectedCtrlID {
				t.Errorf("Test failed: Expected Controller ID %s, but got %s", tt.expectedCtrlID, ctrlID)
			}
		})
	}
}

func TestIsControllerAttachedToNamespace(t *testing.T) {
	tests := []struct {
		name           string
		ctrlID         string
		nsid           int
		subsystems     NvmeSubsystemListResponse
		expectedResult bool
	}{
		{
			name:           "Controller is attached to namespace",
			ctrlID:         "NVMeCtrl2",
			nsid:           1,
			subsystems:     mockNvmeSubsystemList,
			expectedResult: true,
		},
		{
			name:           "Controller is not attached to namespace",
			ctrlID:         "NVMeCtrl_26:00.3",
			nsid:           1,
			subsystems:     mockNvmeSubsystemList,
			expectedResult: false,
		},
		{
			name:           "Namespace does not exist",
			ctrlID:         "NVMeCtrl2",
			nsid:           999,
			subsystems:     mockNvmeSubsystemList,
			expectedResult: false,
		},
		{
			name:   "Empty controllers list",
			ctrlID: "NVMeCtrl2",
			nsid:   1,
			subsystems: NvmeSubsystemListResponse{
				{
					NQN: "nqn.2022-10.io.nvda.nvme:01",
					Namespaces: []Namespace{
						{
							NSID:        1,
							Bdev:        "null1",
							Controllers: []interface{}{},
						},
					},
				},
			},
			expectedResult: false,
		},
		{
			name:   "Multiple controllers, target controller is attached",
			ctrlID: "NVMeCtrl2",
			nsid:   1,
			subsystems: NvmeSubsystemListResponse{
				{
					NQN: "nqn.2022-10.io.nvda.nvme:01",
					Namespaces: []Namespace{
						{
							NSID: 1,
							Bdev: "null1",
							Controllers: []interface{}{
								map[string]interface{}{
									"ctrl_id": "NVMeCtrl1",
								},
								map[string]interface{}{
									"ctrl_id": "NVMeCtrl2",
								},
							},
						},
					},
				},
			},
			expectedResult: true,
		},
		{
			name:   "Controller with invalid type",
			ctrlID: "NVMeCtrl2",
			nsid:   1,
			subsystems: NvmeSubsystemListResponse{
				{
					NQN: "nqn.2022-10.io.nvda.nvme:01",
					Namespaces: []Namespace{
						{
							NSID: 1,
							Bdev: "null1",
							Controllers: []interface{}{
								"invalid-controller", // Not a map
							},
						},
					},
				},
			},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isControllerAttachedToNamespace(tt.ctrlID, tt.nsid, tt.subsystems)

			if result != tt.expectedResult {
				t.Errorf("Test failed: Expected %v, but got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestGetPCI(t *testing.T) {
	tests := []struct {
		name           string
		emFuncs        EmulationFunctionListResponse
		dpuStatus      snapstoragev1.VolumeAttachmentStatusDPU
		parameters     map[string]string
		functionType   string
		expectedPCIBDF string
		expectError    bool
	}{
		{
			name:           "Uses DPU status PCI address",
			emFuncs:        mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{PCIDeviceAddress: "26:0c.9"},
			parameters:     map[string]string{},
			functionType:   "vf",
			expectedPCIBDF: "26:0c.9",
			expectError:    false,
		},
		{
			name:           "Resolves by VUID (hotplugged PF)",
			emFuncs:        mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:     map[string]string{"vuid": "MT2323XZ09G2NVMES1D0F0"},
			functionType:   "vf",
			expectedPCIBDF: "26:00.3",
			expectError:    false,
		},
		{
			name:           "Selects PF when requested",
			emFuncs:        mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:     map[string]string{},
			functionType:   "pf",
			expectedPCIBDF: "26:00.2",
			expectError:    false,
		},
		{
			name:           "Selects first free VF",
			emFuncs:        mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:     map[string]string{},
			functionType:   "vf",
			expectedPCIBDF: "26:0c.1",
			expectError:    false,
		},
		{
			name: "Errors when no PF available",
			emFuncs: EmulationFunctionListResponse{
				{
					Hotplugged:    false,
					EmulationType: NVMeProtocol,
					PFIndex:       0,
					PCIBDF:        "26:aa.0",
					VUID:          "pf-used",
					CtrlID:        "NVMeCtrlUsed",
				},
			},
			dpuStatus:    snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:   map[string]string{},
			functionType: "pf",
			expectError:  true,
		},
		{
			name: "Errors when no free VF available",
			emFuncs: EmulationFunctionListResponse{
				{
					Hotplugged:    false,
					EmulationType: NVMeProtocol,
					PFIndex:       0,
					PCIBDF:        "26:00.2",
					VUID:          "pf",
					VFs: []VF{
						{EmulationType: NVMeProtocol, PCIBDF: "26:0c.0", CtrlID: "NVMeCtrlA"},
					},
				},
			},
			dpuStatus:    snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:   map[string]string{},
			functionType: "vf",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bdf, err := getPCI(tt.emFuncs, tt.dpuStatus, tt.parameters, tt.functionType)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bdf != tt.expectedPCIBDF {
				t.Errorf("expected %s, got %s", tt.expectedPCIBDF, bdf)
			}
		})
	}
}

func TestGetControllerParams(t *testing.T) {
	t.Run("suspended set when pciBDF provided", func(t *testing.T) {
		params := getControllerParams("nqn.x", "26:0c.1", map[string]string{
			"num_queues": "4",
		})

		if params["nqn"] != "nqn.x" {
			t.Errorf("expected nqn=nqn.x")
		}
		if params["pci_bdf"] != "26:0c.1" {
			t.Errorf("expected pci_bdf=26:0c.1")
		}
		if v, ok := params["suspended"].(bool); !ok || !v {
			t.Errorf("expected suspended=true")
		}
		if v, ok := params["num_queues"].(int); !ok || v != 4 {
			t.Errorf("expected num_queues=4 (int)")
		}
	})

	t.Run("suspended set when using VUID path", func(t *testing.T) {
		params := getControllerParams("nqn.z", "00:00.0", map[string]string{
			"vuid": "v-1",
		})

		if params["nqn"] != "nqn.z" {
			t.Errorf("expected nqn=nqn.z")
		}
		if params["vuid"] != "v-1" {
			t.Errorf("expected vuid=v-1")
		}
		if v, ok := params["suspended"].(bool); !ok || !v {
			t.Errorf("expected suspended=true")
		}
	})
}

func TestConvertStringMapToInterfaceMap(t *testing.T) {
	input := map[string]string{
		"a": "123",
		"b": "true",
		"c": "False",
		"d": "hello",
		"e": "001",
		"f": "notbool",
	}

	result := convertStringMapToInterfaceMap(input)

	if v, ok := result["a"].(int); !ok || v != 123 {
		t.Errorf("expected a=123 (int), got %#v", result["a"])
	}
	if v, ok := result["b"].(bool); !ok || v != true {
		t.Errorf("expected b=true (bool), got %#v", result["b"])
	}
	if v, ok := result["c"].(bool); !ok || v != false {
		t.Errorf("expected c=false (bool), got %#v", result["c"])
	}
	if v, ok := result["d"].(string); !ok || v != "hello" {
		t.Errorf("expected d=hello (string), got %#v", result["d"])
	}
	if v, ok := result["e"].(int); !ok || v != 1 {
		t.Errorf("expected e=1 (int), got %#v", result["e"])
	}
	if v, ok := result["f"].(string); !ok || v != "notbool" {
		t.Errorf("expected f=notbool (string), got %#v", result["f"])
	}
}

func TestGetPCIByVUID(t *testing.T) {
	// Success: hotplugged PF with matching VUID exists in mockEmulationFunctionList
	bdf, err := getPCIByVUID(mockEmulationFunctionList, "MT2323XZ09G2NVMES1D0F0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bdf != "26:00.3" {
		t.Errorf("expected 26:00.3, got %s", bdf)
	}

	// Not found: returns error
	_, err = getPCIByVUID(mockEmulationFunctionList, "unknown-vuid")
	if err == nil {
		t.Fatalf("expected error for unknown VUID, got nil")
	}
}

func TestGetHotplugVUIDByPCIAddress(t *testing.T) {
	// Success: hotplugged PF at 26:00.3 has VUID MT2323XZ09G2NVMES1D0F0
	vuid := getHotplugVUIDByPCIAddress("26:00.3", mockEmulationFunctionList)
	if vuid != "MT2323XZ09G2NVMES1D0F0" {
		t.Errorf("expected MT2323XZ09G2NVMES1D0F0, got %s", vuid)
	}

	// Non-hotplugged PF: should not match (26:00.2 is not hotplugged), returns empty
	vuid = getHotplugVUIDByPCIAddress("26:00.2", mockEmulationFunctionList)
	if vuid != "" {
		t.Errorf("expected empty string for non-hotplugged PCI address, got %s", vuid)
	}

	// Unknown PCI address: returns empty
	vuid = getHotplugVUIDByPCIAddress("99:99.9", mockEmulationFunctionList)
	if vuid != "" {
		t.Errorf("expected empty string for unknown PCI address, got %s", vuid)
	}
}

func TestGetPCIForStaticPF(t *testing.T) {
	// Success: first available PF without controller should be returned
	bdf, err := getPCIForStaticPF(mockEmulationFunctionList)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bdf != "26:00.2" {
		t.Errorf("expected 26:00.2, got %s", bdf)
	}

	// No available PF: expect error
	emFuncs := EmulationFunctionListResponse{
		{EmulationType: NVMeProtocol, Hotplugged: false, CtrlID: "in-use", PCIBDF: "00:11.1"},
		{EmulationType: NVMeProtocol, Hotplugged: true, CtrlID: "", PCIBDF: "00:22.2"},
	}
	_, err = getPCIForStaticPF(emFuncs)
	if err == nil {
		t.Fatalf("expected error when no available PF, got nil")
	}
}

func TestGetPCIForVF(t *testing.T) {
	// Success: first available VF without controller should be returned
	bdf, err := getPCIForVF(mockEmulationFunctionList)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bdf != "26:0c.1" {
		t.Errorf("expected 26:0c.1, got %s", bdf)
	}

	// No available VF: expect error
	emFuncs := EmulationFunctionListResponse{
		{
			EmulationType: NVMeProtocol,
			Hotplugged:    false,
			PCIBDF:        "26:00.2",
			VFs: []VF{
				{EmulationType: NVMeProtocol, PCIBDF: "26:0c.0", CtrlID: "used-1"},
				{EmulationType: NVMeProtocol, PCIBDF: "26:0c.1", CtrlID: "used-2"},
			},
		},
	}
	_, err = getPCIForVF(emFuncs)
	if err == nil {
		t.Fatalf("expected error when no available VF, got nil")
	}
}

func TestNvmeFunctionCreate(t *testing.T) {
	tests := []struct {
		name        string
		expectError bool
		expectVUID  string
	}{
		{
			name:        "Successfully create NVMe function",
			expectError: false,
			expectVUID:  "MT2328XZ17DFNVMES1D0F0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockClient()

			vuid, err := NvmeFunctionCreate(client)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if vuid != tt.expectVUID {
				t.Errorf("Expected VUID %s, got %s", tt.expectVUID, vuid)
			}
		})
	}
}

func TestNvmeControllerHotplug(t *testing.T) {
	tests := []struct {
		name        string
		ctrlID      string
		expectError bool
	}{
		{
			name:        "Successfully hotplug controller",
			ctrlID:      "NVMeCtrl1",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockClient()

			err := NvmeControllerHotplug(client, tt.ctrlID)

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

func TestNvmeControllerHotunplug(t *testing.T) {
	tests := []struct {
		name        string
		ctrlID      string
		expectError bool
	}{
		{
			name:        "Successfully hotunplug controller",
			ctrlID:      "NVMeCtrl1",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockClient()

			err := NvmeControllerHotunplug(client, tt.ctrlID)

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

func TestNvmeFunctionDestroy(t *testing.T) {
	tests := []struct {
		name        string
		vuid        string
		expectError bool
	}{
		{
			name:        "Successfully destroy NVMe function",
			vuid:        "MT2328XZ17DFNVMES1D0F0",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockClient()

			err := NvmeFunctionDestroy(client, tt.vuid)

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
