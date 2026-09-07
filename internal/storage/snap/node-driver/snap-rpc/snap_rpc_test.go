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
	"strings"
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
			map[string]interface{}{
				"ctrl_id":    "NVMeCtrl_26:00.3",
				"mdts":       7,
				"vhca_id":    3,
				"nqn":        "nqn.2022-10.io.nvda.nvme:0",
				"plugged":    true,
				"state":      "STARTED",
				"num_queues": 16,
				"max_queues": 32,
				"namespaces": []interface{}{
					map[string]interface{}{
						"nsid": 3,
						"bdev": "hotplug-device",
						"uuid": "263826ad-19a3-4feb-bc25-4bc81ee7750e",
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
			{
				NSID:                  3,
				Bdev:                  "hotplug-device",
				Ready:                 "Yes",
				NQN:                   "nqn.2022-10.io.nvda.nvme:0",
				UUID:                  "263826ad-19a3-4feb-bc25-4bc81ee7750e",
				MaxInflightsPerWeight: 65535,
				Controllers: []interface{}{
					map[string]interface{}{
						"ctrl_id": "NVMeCtrl_26:00.3",
					},
				},
			},
		},
	},
}

// mockInitNQN is the subsystem declared in snapRpcInitConf. Attachments made
// before the driver moved to a subsystem per volume still live in it, and it is
// never destroyed on detach.
const mockInitNQN = "nqn.2022-10.io.nvda.nvme:0"

// mockSecondaryNQN is a second pre-existing subsystem. Together with mockInitNQN
// it gives mockNvmeSubsystemList two namespaces at NSID 1, which is what the
// NQN-scoped lookups have to tell apart.
const mockSecondaryNQN = "nqn.2022-10.io.nvda.nvme:01"

// testVolumeNQN is the subsystem a volume named "test-device" would own.
var testVolumeNQN = SubsystemNQNForDevice("test-device")

// methodSubsystemCreate is the RPC TestNvmeSubsystemCreate asserts on repeatedly.
const methodSubsystemCreate = "nvme_subsystem_create"

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
	case "nvme_subsystem_create":
		return map[string]interface{}{
			"status": "success",
			"nqn":    params["nqn"],
		}, nil
	case "nvme_subsystem_destroy":
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

// recordingRPCClient captures the parameters every RPC was called with so tests
// can assert on what was sent, not only on what came back.
type recordingRPCClient struct {
	JSONRPCClient
	calls []recordedCall
	// failMethod, when set, makes that one method fail with failErr.
	failMethod string
	failErr    error
	// listResponse is what nvme_subsystem_list returns, so a test can decide
	// what SNAP reports after a call has failed. listErr, when set, fails the
	// listing instead, which failMethod cannot express alongside another
	// failing method.
	listResponse NvmeSubsystemListResponse
	listErr      error
}

type recordedCall struct {
	method string
	params map[string]interface{}
}

func newRecordingClient() *recordingRPCClient {
	return &recordingRPCClient{JSONRPCClient: NewMockClient()}
}

func (r *recordingRPCClient) Call(method string, params map[string]interface{}) (interface{}, error) {
	r.calls = append(r.calls, recordedCall{method: method, params: params})
	if method == r.failMethod {
		return nil, r.failErr
	}
	if method == "nvme_subsystem_list" {
		if r.listErr != nil {
			return nil, r.listErr
		}
		return r.listResponse, nil
	}
	return r.JSONRPCClient.Call(method, params)
}

// paramsFor returns the parameters of the first call to method.
func (r *recordingRPCClient) paramsFor(method string) (map[string]interface{}, bool) {
	for _, call := range r.calls {
		if call.method == method {
			return call.params, true
		}
	}
	return nil, false
}

func (r *recordingRPCClient) callCount(method string) int {
	count := 0
	for _, call := range r.calls {
		if call.method == method {
			count++
		}
	}
	return count
}

func TestNvmeNamespaceCreate(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		nqn         string
		dpuStatus   snapstoragev1.VolumeAttachmentStatusDPU
		expectError bool
		expectNSID  int
		expectUUID  interface{} // can be bool or string
	}{
		{
			name:        "Create new namespace with generated UUID",
			deviceName:  "test-device",
			nqn:         SubsystemNQNForDevice("test-device"),
			dpuStatus:   snapstoragev1.VolumeAttachmentStatusDPU{},
			expectError: false,
			expectNSID:  volumeNSID, // the volume owns the subsystem, so the ID is fixed
			expectUUID:  true,       // expect a valid UUID
		},
		{
			name:       "Create namespace with existing DPU status",
			deviceName: "test-device",
			nqn:        SubsystemNQNForDevice("test-device"),
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
			name:        "Missing subsystem NQN",
			deviceName:  "test-device",
			nqn:         "",
			dpuStatus:   snapstoragev1.VolumeAttachmentStatusDPU{},
			expectError: true,
			expectNSID:  0,
			expectUUID:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMockClient()

			nsid, uuidStr, err := NvmeNamespaceCreate(client, tt.deviceName, tt.nqn, tt.dpuStatus)

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

// TestNvmeNamespaceCreateTargetsItsOwnSubsystem is the regression test for
// https://redmine.mellanox.com/issues/5227493: a new namespace no longer derives
// its ID from whatever else SNAP happens to be hosting, so an unordered
// nvme_namespace_list after a UM restart cannot produce a colliding NSID.
func TestNvmeNamespaceCreateTargetsItsOwnSubsystem(t *testing.T) {
	for _, deviceName := range []string{"volume-a", "volume-b", "volume-c"} {
		client := newRecordingClient()
		nqn := SubsystemNQNForDevice(deviceName)

		nsid, _, err := NvmeNamespaceCreate(client, deviceName, nqn, snapstoragev1.VolumeAttachmentStatusDPU{})
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", deviceName, err)
		}
		if nsid != volumeNSID {
			t.Errorf("expected NSID %d for %s, got %d", volumeNSID, deviceName, nsid)
		}

		params, ok := client.paramsFor("nvme_namespace_create")
		if !ok {
			t.Fatalf("nvme_namespace_create was not called for %s", deviceName)
		}
		if params["nqn"] != nqn {
			t.Errorf("expected namespace created in %s, got %v", nqn, params["nqn"])
		}
		if params["bdev_name"] != deviceName {
			t.Errorf("expected bdev_name %s, got %v", deviceName, params["bdev_name"])
		}
	}
}

func TestSubsystemNQNForDevice(t *testing.T) {
	t.Run("is stable for the same device", func(t *testing.T) {
		if first, second := SubsystemNQNForDevice("dev-1"), SubsystemNQNForDevice("dev-1"); first != second {
			t.Errorf("expected a stable NQN, got %q then %q", first, second)
		}
	})

	t.Run("differs between devices", func(t *testing.T) {
		if SubsystemNQNForDevice("dev-1") == SubsystemNQNForDevice("dev-2") {
			t.Error("expected different devices to get different NQNs")
		}
	})

	t.Run("carries the owned prefix", func(t *testing.T) {
		nqn := SubsystemNQNForDevice("dev-1")
		if !isDriverOwnedSubsystem(nqn) {
			t.Errorf("expected %q to be recognized as driver-owned", nqn)
		}
	})

	t.Run("sanitizes unsafe characters", func(t *testing.T) {
		nqn := SubsystemNQNForDevice("vol/with spaces:and#junk")
		for _, unsafe := range []string{"/", " ", "#"} {
			if strings.Contains(strings.TrimPrefix(nqn, subsystemNQNPrefix), unsafe) {
				t.Errorf("expected %q to be stripped from %q", unsafe, nqn)
			}
		}
	})

	t.Run("stays within the NQN limit and remains unique when truncated", func(t *testing.T) {
		long := strings.Repeat("a", 512)
		first := SubsystemNQNForDevice(long + "-one")
		second := SubsystemNQNForDevice(long + "-two")

		for _, nqn := range []string{first, second} {
			if len(nqn) > maxNQNLength {
				t.Errorf("expected NQN within %d bytes, got %d", maxNQNLength, len(nqn))
			}
		}
		if first == second {
			t.Error("expected truncated NQNs to stay unique via the digest suffix")
		}
	})
}

func TestIsDriverOwnedSubsystem(t *testing.T) {
	if isDriverOwnedSubsystem("nqn.2022-10.io.nvda.nvme:0") {
		t.Error("the subsystem from snapRpcInitConf must not be treated as driver-owned")
	}
	if !isDriverOwnedSubsystem(SubsystemNQNForDevice("dev-1")) {
		t.Error("expected a derived NQN to be treated as driver-owned")
	}
}

func TestSubsystemExists(t *testing.T) {
	if !subsystemExists(mockNvmeSubsystemList, "nqn.2022-10.io.nvda.nvme:0") {
		t.Error("expected the init subsystem to be found")
	}
	if subsystemExists(mockNvmeSubsystemList, "nqn.2022-10.io.nvda.nvme:missing") {
		t.Error("expected an unknown NQN not to be found")
	}
}

func TestNvmeSubsystemCreate(t *testing.T) {
	t.Run("creates a subsystem from the NQN alone", func(t *testing.T) {
		client := newRecordingClient()
		nqn := SubsystemNQNForDevice("dev-1")

		if err := NvmeSubsystemCreate(client, nqn, mockNvmeSubsystemList); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		params, ok := client.paramsFor(methodSubsystemCreate)
		if !ok {
			t.Fatal("nvme_subsystem_create was not called")
		}
		if params["nqn"] != nqn {
			t.Errorf("expected nqn %s, got %v", nqn, params["nqn"])
		}
		// nn in particular must stay at SNAP's default, so a legacy namespace can
		// be recreated at an NSID above 1.
		if len(params) != 1 {
			t.Errorf("expected only nqn to be sent, got %v", params)
		}
	})

	t.Run("is a no-op when the subsystem is already listed", func(t *testing.T) {
		client := newRecordingClient()

		if err := NvmeSubsystemCreate(client, "nqn.2022-10.io.nvda.nvme:0", mockNvmeSubsystemList); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count := client.callCount(methodSubsystemCreate); count != 0 {
			t.Errorf("expected no RPC for an existing subsystem, got %d", count)
		}
	})

	// A create that fails is tolerated on the strength of what SNAP reports
	// afterwards, never on how it worded the failure. These four cases pin both
	// halves of that: the wording varies and the outcome follows the listing.
	failureTests := []struct {
		name         string
		failErr      error
		listResponse NvmeSubsystemListResponse
		expectError  bool
	}{
		{
			name:    "tolerates an already-exists error from a stale listing",
			failErr: fmt.Errorf("RPC error: subsystem already exists"),
			listResponse: NvmeSubsystemListResponse{
				{NQN: SubsystemNQNForDevice("dev-1")},
			},
			expectError: false,
		},
		{
			// SNAP wording the collision differently must not turn a subsystem
			// that is really there into a failed attach.
			name:    "tolerates a collision reported without the word exists",
			failErr: fmt.Errorf("RPC error: duplicate NQN"),
			listResponse: NvmeSubsystemListResponse{
				{NQN: SubsystemNQNForDevice("dev-1")},
			},
			expectError: false,
		},
		{
			// The mirror image: an error that merely mentions existence must not
			// be mistaken for the subsystem being there.
			name:         "surfaces a does-not-exist error",
			failErr:      fmt.Errorf("RPC error: bdev does not exist"),
			listResponse: mockNvmeSubsystemList,
			expectError:  true,
		},
		{
			name:         "surfaces other errors",
			failErr:      fmt.Errorf("RPC error: out of resources"),
			listResponse: mockNvmeSubsystemList,
			expectError:  true,
		},
	}

	for _, tt := range failureTests {
		t.Run(tt.name, func(t *testing.T) {
			client := newRecordingClient()
			client.failMethod = methodSubsystemCreate
			client.failErr = tt.failErr
			client.listResponse = tt.listResponse

			err := NvmeSubsystemCreate(client, SubsystemNQNForDevice("dev-1"), nil)

			if tt.expectError && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("expected the failure to be tolerated, got %v", err)
			}
		})
	}

	t.Run("surfaces the create error when the listing cannot be refetched", func(t *testing.T) {
		client := newRecordingClient()
		client.failMethod = methodSubsystemCreate
		client.failErr = fmt.Errorf("RPC error: subsystem already exists")
		client.listErr = fmt.Errorf("RPC error: connection reset")

		// Without a listing to confirm it, even an already-exists wording has to
		// fail and let the next reconcile decide.
		if err := NvmeSubsystemCreate(client, SubsystemNQNForDevice("dev-1"), nil); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("requires an NQN", func(t *testing.T) {
		if err := NvmeSubsystemCreate(newRecordingClient(), "", nil); err == nil {
			t.Fatal("expected an error for an empty NQN, got nil")
		}
	})
}

func TestNvmeSubsystemDestroy(t *testing.T) {
	t.Run("destroys without forcing", func(t *testing.T) {
		client := newRecordingClient()
		nqn := SubsystemNQNForDevice("dev-1")

		if err := NvmeSubsystemDestroy(client, nqn); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		params, ok := client.paramsFor("nvme_subsystem_destroy")
		if !ok {
			t.Fatal("nvme_subsystem_destroy was not called")
		}
		if params["nqn"] != nqn {
			t.Errorf("expected nqn %s, got %v", nqn, params["nqn"])
		}
		if _, forced := params["force"]; forced {
			t.Error("expected force not to be set, so a non-empty subsystem surfaces as an error")
		}
	})

	t.Run("requires an NQN", func(t *testing.T) {
		if err := NvmeSubsystemDestroy(newRecordingClient(), ""); err == nil {
			t.Fatal("expected an error for an empty NQN, got nil")
		}
	})
}

func TestNvmeControllerCreate(t *testing.T) {
	tests := []struct {
		name           string
		nqn            string
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
			nqn:            testVolumeNQN,
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
			nqn:            testVolumeNQN,
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
			nqn:            testVolumeNQN,
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{PCIDeviceAddress: "26:0c.3"},
			functionType:   "vf",
			expectError:    false,
			expectCtrlID:   "NVMeCtrl_26:0c.3",
			expectPciBDF:   "26:0c.3",
		},
		{
			name:           "Create controller with VUID",
			nqn:            testVolumeNQN,
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			parameters:     map[string]string{"vuid": "MT2323XZ09G2NVMES1D0F0"},
			functionType:   "vf",
			expectError:    false,
			expectCtrlID:   "NVMeCtrl_26:00.3",
			expectPciBDF:   "26:00.3",
		},
		{
			name:           "Missing subsystem NQN",
			nqn:            "",
			emulationFuncs: mockEmulationFunctionList,
			dpuStatus:      snapstoragev1.VolumeAttachmentStatusDPU{},
			functionType:   "vf",
			expectError:    true,
			expectCtrlID:   "",
			expectPciBDF:   "",
		},
		{
			name: "No available VFs",
			nqn:  testVolumeNQN,
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
			nqn:            testVolumeNQN,
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

			ctrlID, pciBDF, err := NvmeControllerCreate(client, tt.nqn, tt.emulationFuncs, tt.dpuStatus, tt.parameters, tt.functionType)

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
		expectedNQN  string
		expectedNSID int
		expectedUUID string
	}{
		{
			name:         "Valid Device Name - Should return NSID 1",
			deviceName:   "null1",
			expectedNQN:  mockSecondaryNQN,
			expectedNSID: 1,
			expectedUUID: "263826ad-19a3-4feb-bc25-4bc81ee7748e",
		},
		{
			// Two namespaces share NSID 1 across subsystems, so the owning NQN is
			// the only thing that tells them apart.
			name:         "Device in another subsystem at the same NSID",
			deviceName:   "null0",
			expectedNQN:  mockInitNQN,
			expectedNSID: 1,
			expectedUUID: "263826ad-19a3-4feb-bc25-4bc81ee7749e",
		},
		{
			name:         "Invalid Device Name - Should return -1",
			deviceName:   "non-existent-device",
			expectedNQN:  "",
			expectedNSID: -1,
			expectedUUID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nqn, nsid, nsUUID := getNamespaceByDeviceName(tt.deviceName, mockNvmeSubsystemList)

			if nqn != tt.expectedNQN {
				t.Errorf("Test failed: Expected NQN %q, but got %q", tt.expectedNQN, nqn)
			}

			if nsid != tt.expectedNSID {
				t.Errorf("Test failed: Expected NSID %d, but got %d", tt.expectedNSID, nsid)
			}

			if nsUUID != tt.expectedUUID {
				t.Errorf("Test failed: Expected UUID %q, but got %q", tt.expectedUUID, nsUUID)
			}
		})
	}
}

func TestCheckNamespaceAttached(t *testing.T) {
	tests := []struct {
		name                    string
		nqn                     string
		nsid                    int
		ctrlID                  string
		expectedNamespaceExists bool
		expectedAttachedToCtrl  bool
	}{
		{
			name:                    "Valid NSID and attached controller",
			nqn:                     mockSecondaryNQN,
			nsid:                    1,
			ctrlID:                  "NVMeCtrl2",
			expectedNamespaceExists: true,
			expectedAttachedToCtrl:  true,
		},
		{
			name:                    "Valid NSID but unattached controller",
			nqn:                     mockSecondaryNQN,
			nsid:                    1,
			ctrlID:                  "NVMeCtrl_26:00.3",
			expectedNamespaceExists: true,
			expectedAttachedToCtrl:  false,
		},
		{
			// NSID 1 exists in both subsystems attached to different controllers.
			// Scoping by NQN is what stops a detach from tearing down the wrong one.
			name:                    "Same NSID in another subsystem resolves to that subsystem's controller",
			nqn:                     mockInitNQN,
			nsid:                    1,
			ctrlID:                  "NVMeCtrl1",
			expectedNamespaceExists: true,
			expectedAttachedToCtrl:  true,
		},
		{
			name:                    "Controller from the other subsystem is not matched",
			nqn:                     mockInitNQN,
			nsid:                    1,
			ctrlID:                  "NVMeCtrl2",
			expectedNamespaceExists: true,
			expectedAttachedToCtrl:  false,
		},
		{
			name:                    "Unknown NQN with a live NSID",
			nqn:                     testVolumeNQN,
			nsid:                    1,
			ctrlID:                  "NVMeCtrl2",
			expectedNamespaceExists: false,
			expectedAttachedToCtrl:  false,
		},
		{
			name:                    "Invalid NSID with valid controller",
			nqn:                     mockSecondaryNQN,
			nsid:                    999,
			ctrlID:                  "NVMeCtrl2",
			expectedNamespaceExists: false,
			expectedAttachedToCtrl:  false,
		},
		{
			name:                    "Invalid NSID with invalid controller",
			nqn:                     mockSecondaryNQN,
			nsid:                    999,
			ctrlID:                  "NVMeCtrl_26:00.3",
			expectedNamespaceExists: false,
			expectedAttachedToCtrl:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespaceExists, attachedToCtrl := checkNamespaceAttached(tt.nqn, tt.nsid, tt.ctrlID, mockNvmeSubsystemList)

			if namespaceExists != tt.expectedNamespaceExists {
				t.Errorf("Test failed: Expected namespaceExists %v, got %v for NQN %s, NSID %d and Controller %s",
					tt.expectedNamespaceExists, namespaceExists, tt.nqn, tt.nsid, tt.ctrlID)
			}
			if attachedToCtrl != tt.expectedAttachedToCtrl {
				t.Errorf("Test failed: Expected attachedToCtrl %v, got %v for NQN %s, NSID %d and Controller %s",
					tt.expectedAttachedToCtrl, attachedToCtrl, tt.nqn, tt.nsid, tt.ctrlID)
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
		nqn            string
		nsid           int
		subsystems     NvmeSubsystemListResponse
		expectedResult bool
	}{
		{
			name:           "Controller is attached to namespace",
			ctrlID:         "NVMeCtrl2",
			nqn:            mockSecondaryNQN,
			nsid:           1,
			subsystems:     mockNvmeSubsystemList,
			expectedResult: true,
		},
		{
			name:           "Controller is not attached to namespace",
			ctrlID:         "NVMeCtrl_26:00.3",
			nqn:            mockSecondaryNQN,
			nsid:           1,
			subsystems:     mockNvmeSubsystemList,
			expectedResult: false,
		},
		{
			// Without the NQN scope this would match NSID 1 in the other
			// subsystem and wrongly report the controller as already attached.
			name:           "Controller attached to the same NSID in another subsystem",
			ctrlID:         "NVMeCtrl1",
			nqn:            mockSecondaryNQN,
			nsid:           1,
			subsystems:     mockNvmeSubsystemList,
			expectedResult: false,
		},
		{
			name:           "Namespace does not exist",
			ctrlID:         "NVMeCtrl2",
			nqn:            mockSecondaryNQN,
			nsid:           999,
			subsystems:     mockNvmeSubsystemList,
			expectedResult: false,
		},
		{
			name:           "Subsystem does not exist",
			ctrlID:         "NVMeCtrl2",
			nqn:            testVolumeNQN,
			nsid:           1,
			subsystems:     mockNvmeSubsystemList,
			expectedResult: false,
		},
		{
			name:   "Empty controllers list",
			ctrlID: "NVMeCtrl2",
			nqn:    mockSecondaryNQN,
			nsid:   1,
			subsystems: NvmeSubsystemListResponse{
				{
					NQN: mockSecondaryNQN,
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
			nqn:    mockSecondaryNQN,
			nsid:   1,
			subsystems: NvmeSubsystemListResponse{
				{
					NQN: mockSecondaryNQN,
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
			nqn:    mockSecondaryNQN,
			nsid:   1,
			subsystems: NvmeSubsystemListResponse{
				{
					NQN: mockSecondaryNQN,
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
			result := isControllerAttachedToNamespace(tt.ctrlID, tt.nqn, tt.nsid, tt.subsystems)

			if result != tt.expectedResult {
				t.Errorf("Test failed: Expected %v, but got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestControllerSubsystemNQN(t *testing.T) {
	tests := []struct {
		name        string
		ctrlID      string
		subsystems  NvmeSubsystemListResponse
		expectedNQN string
	}{
		{
			name:        "Controller found in its subsystem",
			ctrlID:      "NVMeCtrl2",
			subsystems:  mockNvmeSubsystemList,
			expectedNQN: mockSecondaryNQN,
		},
		{
			name:        "Second controller of the same subsystem",
			ctrlID:      "NVMeCtrl_26:00.3",
			subsystems:  mockNvmeSubsystemList,
			expectedNQN: mockInitNQN,
		},
		{
			// The state an attach that failed before nvme_controller_attach_ns
			// leaves behind: the controller is listed on the subsystem with no
			// namespace attached to it.
			name:   "Controller listed with no namespace attached",
			ctrlID: "NVMeCtrl2",
			subsystems: NvmeSubsystemListResponse{
				{
					NQN: testVolumeNQN,
					Controllers: []interface{}{
						map[string]interface{}{"ctrl_id": "NVMeCtrl2"},
					},
				},
			},
			expectedNQN: testVolumeNQN,
		},
		{
			name:        "Controller not listed anywhere",
			ctrlID:      "NVMeCtrl99",
			subsystems:  mockNvmeSubsystemList,
			expectedNQN: "",
		},
		{
			// A listing that reports no subsystem-level controllers has to read
			// as unknown, so teardown keeps trusting the recorded address.
			name:   "Subsystem reports no controllers",
			ctrlID: "NVMeCtrl2",
			subsystems: NvmeSubsystemListResponse{
				{NQN: testVolumeNQN},
			},
			expectedNQN: "",
		},
		{
			name:   "Controller with invalid type",
			ctrlID: "NVMeCtrl2",
			subsystems: NvmeSubsystemListResponse{
				{
					NQN:         testVolumeNQN,
					Controllers: []interface{}{"invalid-controller"},
				},
			},
			expectedNQN: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := controllerSubsystemNQN(tt.ctrlID, tt.subsystems)

			if result != tt.expectedNQN {
				t.Errorf("Test failed: Expected %q, but got %q", tt.expectedNQN, result)
			}
		})
	}
}

func TestResolveOwnedController(t *testing.T) {
	tests := []struct {
		name       string
		deviceName string
		nqn        string
		pciAddr    string
		// subsystems defaults to mockNvmeSubsystemList when nil.
		subsystems          NvmeSubsystemListResponse
		expectedCtrlID      string
		expectedCtrlPCIAddr string
	}{
		{
			name:                "Recorded address holds this volume's controller",
			deviceName:          "null1",
			nqn:                 mockSecondaryNQN,
			pciAddr:             "26:0c.0",
			expectedCtrlID:      "NVMeCtrl2",
			expectedCtrlPCIAddr: "26:0c.0",
		},
		{
			// SNAP handed 26:00.3 to hotplug-device after the address was
			// recorded for null1, so the controller there must not come back as
			// null1's and the namespace's own controller is used instead.
			name:                "Recorded address was reused by another volume",
			deviceName:          "null1",
			nqn:                 mockSecondaryNQN,
			pciAddr:             "26:00.3",
			expectedCtrlID:      "NVMeCtrl2",
			expectedCtrlPCIAddr: "26:0c.0",
		},
		{
			// Nothing is left to identify the volume by, so teardown has no
			// controller to work with and must not fall back to the address.
			name:                "Recorded address was reused and the namespace is gone",
			deviceName:          "missing-device",
			nqn:                 testVolumeNQN,
			pciAddr:             "26:00.3",
			expectedCtrlID:      "",
			expectedCtrlPCIAddr: "",
		},
		{
			// A function with no controller cannot be another volume's, so it
			// stays addressable for hotplug function cleanup.
			name:                "Recorded address holds no controller",
			deviceName:          "missing-device",
			nqn:                 testVolumeNQN,
			pciAddr:             "26:0c.1",
			expectedCtrlID:      "",
			expectedCtrlPCIAddr: "26:0c.1",
		},
		{
			// Without subsystem-level controllers the owner cannot be
			// established, so teardown has to behave as it did before rather
			// than treat the controller as foreign and leak it.
			name:       "Unreported owner leaves the recorded address in use",
			deviceName: "null1",
			nqn:        testVolumeNQN,
			pciAddr:    "26:0c.0",
			subsystems: NvmeSubsystemListResponse{
				{
					NQN: mockSecondaryNQN,
					Namespaces: []Namespace{
						{NSID: 1, Bdev: "null1", NQN: mockSecondaryNQN},
					},
				},
			},
			expectedCtrlID:      "NVMeCtrl2",
			expectedCtrlPCIAddr: "26:0c.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subsystems := tt.subsystems
			if subsystems == nil {
				subsystems = mockNvmeSubsystemList
			}

			ctrlID, ctrlPCIAddr := resolveOwnedController(tt.deviceName, tt.nqn, tt.pciAddr,
				subsystems, mockEmulationFunctionList)

			if ctrlID != tt.expectedCtrlID {
				t.Errorf("Expected controller ID %q, got %q", tt.expectedCtrlID, ctrlID)
			}
			if ctrlPCIAddr != tt.expectedCtrlPCIAddr {
				t.Errorf("Expected controller PCI address %q, got %q", tt.expectedCtrlPCIAddr, ctrlPCIAddr)
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

func TestGetFunctionVUIDByPCIAddress(t *testing.T) {
	tests := []struct {
		name         string
		pciAddress   string
		expectedVUID string
		expectError  bool
	}{
		{
			name:         "static PF returns its own VUID",
			pciAddress:   "26:00.2",
			expectedVUID: "MT2328XZ17DFNVMES0D0F2",
		},
		{
			name:         "hotplugged PF returns its own VUID",
			pciAddress:   "26:00.3",
			expectedVUID: "MT2323XZ09G2NVMES1D0F0",
		},
		{
			name:         "VF returns its parent PF VUID",
			pciAddress:   "26:0c.1",
			expectedVUID: "MT2328XZ17DFNVMES0D0F2",
		},
		{
			name:        "unknown PCI address returns error",
			pciAddress:  "99:99.9",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vuid, err := getFunctionVUIDByPCIAddress(tt.pciAddress, mockEmulationFunctionList)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if vuid != tt.expectedVUID {
				t.Errorf("expected %s, got %s", tt.expectedVUID, vuid)
			}
		})
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
