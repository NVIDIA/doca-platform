/*
COPYRIGHT 2025 NVIDIA

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
	"encoding/json"
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
func NewMockClient() *MockJSONRPCSnapClient {
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
	if method == "nvme_namespace_create" {
		return map[string]interface{}{
			"status":    "success",
			"bdev_name": params["bdev_name"],
			"nsid":      params["nsid"],
		}, nil
	} else if method == "nvme_controller_create" {
		// Mock response for controller creation
		ctrlID := fmt.Sprintf("NVMeCtrl_%s", params["pci_bdf"])
		return map[string]interface{}{
			"status":  "success",
			"ctrl_id": ctrlID,
		}, nil
	}
	return nil, fmt.Errorf("unexpected method: %s", method)
}

// Close implements the Close method for the mock client
func (m *MockJSONRPCSnapClient) Close() error {
	return nil
}

// testNvmeNamespaceCreate is a test-specific version of NvmeNamespaceCreate that accepts mock client
func testNvmeNamespaceCreate(client *MockJSONRPCSnapClient, crdDeviceName string, subsystems NvmeSubsystemListResponse,
	dpuStatus snapstoragev1.VolumeAttachmentStatusDPU) (int, string, error) {

	if len(subsystems) == 0 {
		fmt.Println("no subsystems found")
		return 0, "", fmt.Errorf("no subsystems found")
	}

	targetSubsystem := &subsystems[0]
	var nsid int
	var uuidStr string
	if dpuStatus.DeviceName != "" {
		nsid = int(dpuStatus.BdevAttrs.NVMeNsID)
		uuidStr = dpuStatus.BdevAttrs.NVMeUUID
	} else {
		if len(targetSubsystem.Namespaces) == 0 {
			nsid = 1
		} else {
			nsid = targetSubsystem.Namespaces[0].NSID + 1
		}
		uuidStr = uuid.Must(uuid.NewRandom()).String()
	}

	params := map[string]interface{}{
		"bdev_type": "spdk",
		"nqn":       targetSubsystem.NQN,
		"nsid":      nsid,
		"uuid":      uuidStr,
		"bdev_name": crdDeviceName,
	}

	result, err := client.Call("nvme_namespace_create", params)

	if err != nil {
		return 0, "", fmt.Errorf("RPC call failed: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println("Namespace created successfully:", string(resultBytes))

	return nsid, uuidStr, nil
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

			nsid, uuidStr, err := testNvmeNamespaceCreate(client, tt.deviceName, tt.subsystems, tt.dpuStatus)

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

// testNvmeControllerCreate is a test-specific version of NvmeControllerCreate that accepts mock client
func testNvmeControllerCreate(client *MockJSONRPCSnapClient, subsystems NvmeSubsystemListResponse,
	emulationFunctions EmulationFunctionListResponse, dpuStatus snapstoragev1.VolumeAttachmentStatusDPU) (string, string, error) {

	if len(subsystems) == 0 {
		fmt.Println("no subsystems found")
		return "", "", fmt.Errorf("no subsystems found")
	}
	targetSubsystem := &subsystems[0]

	var currEmFunc EmulationFunction
	for _, emFunc := range emulationFunctions {
		if emFunc.EmulationType == NVMeProtocol {
			currEmFunc = emFunc
			break
		}
	}

	pciBDF := ""
	if dpuStatus.PCIDeviceAddress != "" {
		pciBDF = dpuStatus.PCIDeviceAddress
	} else {
		for _, vf := range currEmFunc.VFs {
			if vf.CtrlID == "" {
				pciBDF = vf.PCIBDF
				break
			}
		}
	}

	if pciBDF == "" {
		fmt.Println("no pci bdf found")
		return "", "", fmt.Errorf("no pci bdf found")
	}

	params := map[string]interface{}{
		"nqn":     targetSubsystem.NQN,
		"pci_bdf": pciBDF,
	}

	result, err := client.Call("nvme_controller_create", params)
	if err != nil {
		return "", "", fmt.Errorf("failed to create controller: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return "", "", fmt.Errorf("unexpected result type")
	}

	ctrlID, ok := resultMap["ctrl_id"].(string)
	if !ok {
		return "", "", fmt.Errorf("ctrl_id not found or not a string in response")
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println("Controller created successfully:", string(resultBytes))

	return ctrlID, pciBDF, nil
}

func TestNvmeControllerCreate(t *testing.T) {
	tests := []struct {
		name           string
		subsystems     NvmeSubsystemListResponse
		emulationFuncs EmulationFunctionListResponse
		dpuStatus      snapstoragev1.VolumeAttachmentStatusDPU
		expectError    bool
		expectCtrlID   string
		expectPciBDF   string
	}{
		{
			name:           "Create controller with available VF",
			subsystems:     mockNvmeSubsystemList,
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			expectError:    false,
			expectCtrlID:   "NVMeCtrl_26:0c.1", // Based on mock data, first available VF
			expectPciBDF:   "26:0c.1",
		},
		{
			name:           "Create controller with existing DPU status",
			subsystems:     mockNvmeSubsystemList,
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus: snapstoragev1.VolumeAttachmentStatusDPU{
				PCIDeviceAddress: "26:0c.3",
			},
			expectError:  false,
			expectCtrlID: "NVMeCtrl_26:0c.3", // Should use the PCI address from DPU status
			expectPciBDF: "26:0c.3",
		},
		{
			name:           "Empty subsystems list",
			subsystems:     NvmeSubsystemListResponse{},
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
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
			expectError:  true,
			expectCtrlID: "",
			expectPciBDF: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockClient()

			ctrlID, pciBDF, err := testNvmeControllerCreate(client, tt.subsystems, tt.emulationFuncs, tt.dpuStatus)

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
			ctrlID, err := getNvmeControllerByPciAddr(tt.pciAddr, mockEmulationFunctionList)

			if tt.expectError {
				if err == nil {
					t.Fatalf("Expected error but got nil for PCI BDF %s", tt.pciAddr)
				}
				expectedErrMsg := fmt.Sprintf("no NVMe controller found for PCI BDF %s", tt.pciAddr)
				if err.Error() != expectedErrMsg {
					t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if ctrlID != tt.expectedController {
					t.Errorf("Expected controller '%s', got '%s'", tt.expectedController, ctrlID)
				}
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
	}{
		{
			name:        "Valid Controller ID - Should return PCI BDF 26:0c.0",
			ctrlID:      "NVMeCtrl2",
			expectedBDF: "26:0c.0",
			expectError: false,
		},
		{
			name:        "Invalid Controller ID - Should return error",
			ctrlID:      "NVMeCtrlX",
			expectedBDF: "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bdf, err := getPciAddrByCtrlID(tt.ctrlID, mockEmulationFunctionList)

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
		name     string
		nsid     int
		ctrlID   string
		expected bool
	}{
		{
			name:     "Valid NSID and attached controller",
			nsid:     1,
			ctrlID:   "NVMeCtrl2",
			expected: true,
		},
		{
			name:     "Valid NSID but unattached controller",
			nsid:     1,
			ctrlID:   "NVMeCtrl3",
			expected: false,
		},
		{
			name:     "Invalid NSID with valid controller",
			nsid:     999,
			ctrlID:   "NVMeCtrl2",
			expected: false,
		},
		{
			name:     "Invalid NSID with invalid controller",
			nsid:     999,
			ctrlID:   "NVMeCtrl3",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exists := checkNamespaceAttached(tt.nsid, tt.ctrlID, mockNvmeSubsystemList)

			if exists != tt.expected {
				t.Errorf("Test failed: Expected %v, but got %v for NSID %d and Controller %s",
					tt.expected, exists, tt.nsid, tt.ctrlID)
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
			ctrlID:         "NVMeCtrl3",
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
