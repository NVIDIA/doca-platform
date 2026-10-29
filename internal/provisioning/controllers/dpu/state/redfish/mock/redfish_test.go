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

package mock

import (
	"testing"

	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"

	"k8s.io/klog/v2"
)

func TestRedfishMockServer(t *testing.T) {
	// Create and start the mock server
	server, err := CreateMockRedfishServer("BF-24.10", "testpassword")
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer server.Stop()

	// Get a client connected to the mock server
	client, err := server.GetClient()
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Run all the individual tests
	t.Run("RootService", func(t *testing.T) { testRootService(t, client) })
	t.Run("ChassisInfo", func(t *testing.T) { testChassisInfo(t, client) })

	klog.Infof("All tests passed successfully")
}

func testRootService(t *testing.T, client *client.Client) {
	resp, err := client.GetRootService()
	if err != nil {
		t.Fatalf("Failed to get root service: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode())
	}
}

func testChassisInfo(t *testing.T, client *client.Client) {
	resp, chassisInfo, err := client.GetChassis()
	if err != nil {
		t.Fatalf("Failed to get chassis info: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode())
	}
	if chassisInfo.Model != "BlueField-3 B3220" {
		t.Errorf("Expected model BlueField-3 B3220, got %s", chassisInfo.Model)
	}
	if chassisInfo.PartNumber != "900-9D3B6-00CV-AA0" {
		t.Errorf("Expected part number 900-9D3B6-00CV-AA0, got %s", chassisInfo.PartNumber)
	}
}
