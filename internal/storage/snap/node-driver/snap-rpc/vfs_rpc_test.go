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
)

// Global Mock DOCA Function List Response
var mockDOCAFunctionList = []DOCAFunctionList{
	{
		Manager: "mlx5_0",
		FunctionList: []DOCAFunction{
			{
				HotPluggable: "false",
				PCIAddress:   "0000:07:00.2",
				VUID:         "MT2328XZ17DFVFSS0D0F2",
				FunctionType: "virtio_fs",
			},
			{
				HotPluggable: "true",
				PCIAddress:   "0000:03:00.0",
				VUID:         "MT2328XZ17DFVFSS0D0F0",
				FunctionType: "virtio_fs",
			},
			{
				HotPluggable: "true",
				PCIAddress:   "0000:03:00.1",
				VUID:         "MT2328XZ17DFVFSS0D0F1",
				FunctionType: "virtio_fs",
			},
		},
	},
}

// MockVFSClient is a mock implementation for testing
type MockVFSClient struct {
	requestID int
	timeout   time.Duration
}

// NewMockClient creates a new mock client for testing
func NewMockVFSClient() JSONRPCClient {
	return &MockVFSClient{
		requestID: 0,
		timeout:   60 * time.Second,
	}
}

// Send implements the Send method for the mock client
func (m *MockVFSClient) Send(method string, params map[string]interface{}) (int, error) {
	m.requestID++
	return m.requestID, nil
}

// Recv implements the Recv method for the mock client
func (m *MockVFSClient) Recv() (map[string]interface{}, error) {
	return map[string]interface{}{
		"result": map[string]interface{}{
			"status": "success",
		},
	}, nil
}

// Call implements the Call method for the mock client
func (m *MockVFSClient) Call(method string, params map[string]interface{}) (interface{}, error) {
	switch method {
	case "virtio_fs_doca_get_functions":
		return mockDOCAFunctionList, nil
	case "virtio_fs_doca_function_create":
		return "MT2328XZ17DFVFSS0D0F3", nil
	case "virtio_fs_transport_create",
		"virtio_fs_doca_manager_create",
		"virtio_fs_transport_start",
		"virtio_fs_device_create",
		"virtio_fs_doca_device_modify",
		"virtio_fs_device_start",
		"virtio_fs_device_stop",
		"virtio_fs_device_destroy",
		"virtio_fs_transport_stop",
		"virtio_fs_doca_manager_destroy",
		"virtio_fs_doca_function_destroy",
		"virtio_fs_transport_destroy",
		"virtio_fs_doca_device_hotplug",
		"virtio_fs_doca_device_hotunplug":
		return map[string]interface{}{"status": "success"}, nil
	case "virtio_fs_get_transports":
		return []Transport{
			{
				Name:  "DOCA",
				State: "started",
				Managers: []DOCAManager{
					{Name: "mlx5_0"},
				},
			},
		}, nil
	case "virtio_fs_doca_get_managers":
		return []DOCAManager{
			{Name: "mlx5_0"},
		}, nil
	case "virtio_fs_doca_get_possible_managers":
		return []DOCAManager{
			{Name: "mlx5_0"},
		}, nil
	case "virtio_fs_get_devices":
		return []FSDevice{
			{
				Name:             "dev_test-device",
				TransportName:    "DOCA",
				State:            "running",
				Fsdev:            "test-device",
				Tag:              "test-devicetag",
				QueueSize:        256,
				NumRequestQueues: 8,
			},
		}, nil

	default:
		return nil, fmt.Errorf("unexpected method: %s", method)
	}
}

// Close implements the Close method for the mock client
func (m *MockVFSClient) Close() error {
	return nil
}

func TestVirtioFSTransportCreate(t *testing.T) {
	tests := []struct {
		name        string
		transports  []Transport
		expectError bool
	}{
		{
			name:        "Create new transport",
			transports:  []Transport{},
			expectError: false,
		},
		{
			name: "Transport already exists",
			transports: []Transport{
				{Name: "DOCA"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()
			err := VirtioFSTransportCreate(client, tt.transports)
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSTransportCreate() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSDOCAManagerCreate(t *testing.T) {
	tests := []struct {
		name        string
		managerName string
		managers    []DOCAManager
		expectError bool
	}{
		{
			name:        "Create new manager",
			managerName: "mlx5_0",
			managers:    []DOCAManager{},
			expectError: false,
		},
		{
			name:        "Manager already exists",
			managerName: "mlx5_0",
			managers: []DOCAManager{
				{Name: "mlx5_0"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()
			err := VirtioFSDOCAManagerCreate(client, tt.managerName, tt.managers)
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSDOCAManagerCreate() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSTransportStart(t *testing.T) {
	tests := []struct {
		name        string
		transports  []Transport
		expectError bool
	}{
		{
			name: "Start transport",
			transports: []Transport{
				{Name: "DOCA", State: "stopped"},
			},
			expectError: false,
		},
		{
			name: "Transport already started",
			transports: []Transport{
				{Name: "DOCA", State: "started"},
			},
			expectError: false,
		},
		{
			name:        "Transport doesn't exist",
			transports:  []Transport{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()
			err := VirtioFSTransportStart(client, tt.transports)
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSTransportStart() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSDOCAGetFunctions(t *testing.T) {
	tests := []struct {
		name        string
		expectError bool
	}{
		{
			name:        "Successfully get DOCA functions",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()

			functions, err := VirtioFSDOCAGetFunctions(client)

			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSDOCAGetFunctions() error = %v, expectError %v", err, tt.expectError)
				return
			}

			// Compare with mockDOCAFunctionList
			if len(functions) != len(mockDOCAFunctionList) {
				t.Errorf("Got %d functions, want %d", len(functions), len(mockDOCAFunctionList))
				return
			}

			// Deep comparison of the returned data with mockDOCAFunctionList
			for i, funcList := range functions {
				if funcList.Manager != mockDOCAFunctionList[i].Manager {
					t.Errorf("Manager = %v, want %v", funcList.Manager, mockDOCAFunctionList[i].Manager)
				}

				if len(funcList.FunctionList) != len(mockDOCAFunctionList[i].FunctionList) {
					t.Errorf("FunctionList length = %v, want %v",
						len(funcList.FunctionList), len(mockDOCAFunctionList[i].FunctionList))
					continue
				}

				for j, fn := range funcList.FunctionList {
					mockFn := mockDOCAFunctionList[i].FunctionList[j]
					if fn.HotPluggable != mockFn.HotPluggable ||
						fn.PCIAddress != mockFn.PCIAddress ||
						fn.VUID != mockFn.VUID ||
						fn.FunctionType != mockFn.FunctionType {
						t.Errorf("Function[%d] = %+v, want %+v", j, fn, mockFn)
					}
				}
			}
		})
	}
}

func TestVirtioFSDOCAFunctionCreate(t *testing.T) {
	tests := []struct {
		name        string
		expectError bool
		expectVUID  string
	}{
		{
			name:        "Successful function creation",
			expectError: false,
			expectVUID:  "MT2328XZ17DFVFSS0D0F3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()

			vuid, err := VirtioFSDOCAFunctionCreate(client, "mlx5_0")

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

func TestVirtioFSDOCAFunctionDestroy(t *testing.T) {
	tests := []struct {
		name        string
		vuid        string
		managerName string
		expectError bool
	}{
		{
			name:        "Successful function destruction",
			vuid:        "MT2328XZ17DFVFSS0D0F0",
			managerName: "mlx5_0",
			expectError: false,
		},
		{
			name:        "Empty VUID",
			vuid:        "",
			managerName: "mlx5_0",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()

			err := VirtioFSDOCAFunctionDestroy(client, tt.vuid, tt.managerName)

			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSDOCAFunctionDestroy() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSDeviceCreate(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		params      map[string]string
		expectError bool
	}{
		{
			name:        "Create device with default parameters",
			deviceName:  "test-dev",
			params:      map[string]string{},
			expectError: false,
		},
		{
			name:       "Create device with custom num_request_queues",
			deviceName: "test-dev2",
			params: map[string]string{
				"num_request_queues": "16",
			},
			expectError: false,
		},
		{
			name:       "Create device with invalid num_request_queues",
			deviceName: "test-dev3",
			params: map[string]string{
				"num_request_queues": "invalid",
			},
			expectError: false, // Should use default value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()

			err := VirtioFSDeviceCreate(client, tt.deviceName, tt.params)

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

func TestVirtioFSDOCADeviceModify(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		vuid        string
		managerName string
		expectError bool
	}{
		{
			name:        "Successful device modification",
			deviceName:  "test-dev",
			vuid:        "MT2328XZ17DFVFSS0D0F3",
			managerName: "mlx5_0",
			expectError: false,
		},
		{
			name:        "Invalid device name",
			deviceName:  "",
			vuid:        "MT2328XZ17DFVFSS0D0F3",
			managerName: "mlx5_0",
			expectError: true,
		},
		{
			name:        "Invalid VUID",
			deviceName:  "test-dev",
			vuid:        "",
			managerName: "mlx5_0",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()

			err := VirtioFSDOCADeviceModify(client, tt.managerName, tt.deviceName, tt.vuid)

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

func TestVirtioFSDeviceStart(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		devices     []FSDevice
		expectError bool
	}{
		{
			name:       "Start device",
			deviceName: "test-device",
			devices: []FSDevice{
				{Name: "dev_test-device", State: "stopped"},
			},
			expectError: false,
		},
		{
			name:       "Device already started",
			deviceName: "test-device",
			devices: []FSDevice{
				{Name: "dev_test-device", State: "running"},
			},
			expectError: false,
		},
		{
			name:        "Device doesn't exist",
			deviceName:  "nonexistent",
			devices:     []FSDevice{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()
			err := VirtioFSDeviceStart(client, tt.deviceName, tt.devices)
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSDeviceStart() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSDeviceStop(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		expectError bool
	}{
		{
			name:        "Stop device",
			deviceName:  "test-device",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()
			err := VirtioFSDeviceStop(client, tt.deviceName)
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSDeviceStop() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSDeviceDestroy(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		expectError bool
	}{
		{
			name:        "Destroy device",
			deviceName:  "test-device",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()
			err := VirtioFSDeviceDestroy(client, tt.deviceName)
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSDeviceDestroy() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSTransportStop(t *testing.T) {
	tests := []struct {
		name        string
		expectError bool
	}{
		{
			name:        "Successfully stop transport",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()

			err := VirtioFSTransportStop(client)

			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSTransportStop() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSDOCAManagerDestroy(t *testing.T) {
	tests := []struct {
		name        string
		managerName string
		expectError bool
	}{
		{
			name:        "Successfully destroy DOCA manager",
			managerName: "mlx5_0",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()

			err := VirtioFSDOCAManagerDestroy(client, tt.managerName)

			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSDOCAManagerDestroy() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSTransportDestroy(t *testing.T) {
	tests := []struct {
		name        string
		expectError bool
	}{
		{
			name:        "Successfully destroy transport",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()

			err := VirtioFSTransportDestroy(client)

			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSTransportDestroy() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSDOCADeviceHotplug(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		expectError bool
	}{
		{
			name:        "Hotplug device",
			deviceName:  "test-device",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()
			err := VirtioFSDOCADeviceHotplug(client, tt.deviceName)
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSDOCADeviceHotplug() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSDOCADeviceHotunplug(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		expectError bool
	}{
		{
			name:        "Hotunplug device",
			deviceName:  "test-device",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()
			err := VirtioFSDOCADeviceHotunplug(client, tt.deviceName)
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSDOCADeviceHotunplug() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestVirtioFSGetTransports(t *testing.T) {
	tests := []struct {
		name            string
		expectError     bool
		expectLen       int
		expectTransport string
	}{
		{
			name:            "Get transports",
			expectError:     false,
			expectLen:       1,
			expectTransport: "DOCA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()
			transports, err := VirtioFSGetTransports(client)
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSGetTransports() error = %v, expectError %v", err, tt.expectError)
			}
			if len(transports) != tt.expectLen {
				t.Errorf("VirtioFSGetTransports() got %v transports, want %v", len(transports), tt.expectLen)
			}
			if len(transports) > 0 && transports[0].Name != tt.expectTransport {
				t.Errorf("VirtioFSGetTransports() got transport %v, want %v", transports[0].Name, tt.expectTransport)
			}
		})
	}
}

func TestVirtioFSDOCAGetManagers(t *testing.T) {
	tests := []struct {
		name          string
		expectError   bool
		expectLen     int
		expectManager string
	}{
		{
			name:          "Successfully get DOCA managers",
			expectError:   false,
			expectLen:     1,
			expectManager: "mlx5_0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()

			managers, err := VirtioFSDOCAGetManagers(client)

			// Check error expectation
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSDOCAGetManagers() error = %v, expectError %v", err, tt.expectError)
				return
			}

			// Check the number of managers returned
			if len(managers) != tt.expectLen {
				t.Errorf("Got %d managers, want %d", len(managers), tt.expectLen)
				return
			}

			// Check the manager name
			if managers[0].Name != tt.expectManager {
				t.Errorf("Manager name = %v, want %v", managers[0].Name, tt.expectManager)
			}

			// Compare with the mock response from Call method
			expectedManagers := []DOCAManager{
				{Name: "mlx5_0"},
			}

			for i, manager := range managers {
				if manager.Name != expectedManagers[i].Name {
					t.Errorf("Manager[%d] = %+v, want %+v", i, manager, expectedManagers[i])
				}
			}
		})
	}
}

func TestVirtioFSGetPossibleManagers(t *testing.T) {
	client := NewMockVFSClient()
	managers, err := VirtioFSGetPossibleManagers(client)
	if err != nil {
		t.Errorf("VirtioFSGetPossibleManagers() error = %v, expectError false", err)
		return
	}
	if len(managers) != 1 {
		t.Errorf("Got %d managers, want 1", len(managers))
		return
	}
	if managers[0].Name != "mlx5_0" {
		t.Errorf("Manager name = %v, want mlx5_0", managers[0].Name)
		return
	}
}

func TestVirtioFSGetDevices(t *testing.T) {
	tests := []struct {
		name        string
		expectError bool
		expectLen   int
	}{
		{
			name:        "Get devices",
			expectError: false,
			expectLen:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockVFSClient()
			devices, err := VirtioFSGetDevices(client)
			if (err != nil) != tt.expectError {
				t.Errorf("VirtioFSGetDevices() error = %v, expectError %v", err, tt.expectError)
			}
			if len(devices) != tt.expectLen {
				t.Errorf("VirtioFSGetDevices() got %v devices, want %v", len(devices), tt.expectLen)
			}
		})
	}
}

func TestVirtioFSDeviceExists(t *testing.T) {
	devices := []FSDevice{
		{Name: "dev_test-device"},
	}

	tests := []struct {
		name       string
		deviceName string
		want       bool
	}{
		{
			name:       "Device exists",
			deviceName: "test-device",
			want:       true,
		},
		{
			name:       "Device doesn't exist",
			deviceName: "nonexistent",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VirtioFSDeviceExists(devices, tt.deviceName); got != tt.want {
				t.Errorf("VirtioFSDeviceExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPCIAddressByVUID(t *testing.T) {
	tests := []struct {
		name        string
		vuid        string
		expectPCI   string
		expectError bool
	}{
		{
			name:        "Valid VUID",
			vuid:        "MT2328XZ17DFVFSS0D0F0",
			expectPCI:   "0000:03:00.0",
			expectError: false,
		},
		{
			name:        "Invalid VUID",
			vuid:        "invalid-vuid",
			expectPCI:   "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pciAddr, err := GetPCIAddressByVUID(mockDOCAFunctionList, tt.vuid)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if pciAddr != tt.expectPCI {
					t.Errorf("Expected PCI address %s, got %s", tt.expectPCI, pciAddr)
				}
			}
		})
	}
}

func TestGetVUIDByPCIAddress(t *testing.T) {
	tests := []struct {
		name        string
		pciAddr     string
		expectVUID  string
		expectError bool
	}{
		{
			name:        "Valid PCI Address",
			pciAddr:     "0000:03:00.0",
			expectVUID:  "MT2328XZ17DFVFSS0D0F0",
			expectError: false,
		},
		{
			name:        "Invalid PCI Address",
			pciAddr:     "invalid-pci",
			expectVUID:  "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vuid, err := GetVUIDByPCIAddress(mockDOCAFunctionList, tt.pciAddr)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if vuid != tt.expectVUID {
					t.Errorf("Expected VUID %s, got %s", tt.expectVUID, vuid)
				}
			}
		})
	}
}
