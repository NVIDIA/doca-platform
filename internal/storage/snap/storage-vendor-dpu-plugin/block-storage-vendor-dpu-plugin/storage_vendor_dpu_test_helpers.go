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

package storagevendordpuplugin

// Mock responses and test data
var (
	// mockBdevResponse represents a mock response for BdevGetBdevs
	mockBdevResponse = BdevGetBdevsResponse{
		Bdevs: []Bdev{
			{
				Name: "nvme_pv-namen1",
				DriverSpecific: map[string]interface{}{
					"nvme": []interface{}{
						map[string]interface{}{
							"trid": map[string]interface{}{
								"trtype":  "RDMA",
								"adrfam":  "IPv4",
								"traddr":  "1.1.1.1",
								"trsvcid": "4420",
								"subnqn":  "nqn.2016-06.io.nvmet:swx-mtvr-stor02",
							},
						},
					},
				},
			},
		},
	}

	// mockControllersResponse represents a mock response for BdevNvmeGetControllers
	mockControllersResponse = BdevNvmeGetControllersResponse{
		Controllers: []NvmeController{
			{
				Name: "nvme_pv-name",
				Ctrlrs: []struct {
					Trid NVMeTrid `json:"trid"`
				}{
					{
						Trid: NVMeTrid{
							TrType:  "RDMA",
							AdrFam:  "IPv4",
							TrAddr:  "1.1.1.1",
							TrSvcID: "4420",
							SubNQN:  "nqn.2016-06.io.nvmet:swx-mtvr-stor02",
						},
					},
				},
			},
		},
	}
)

// Mock RPCClient for testing
type mockRPCClient struct {
	bdevs       BdevGetBdevsResponse
	controllers BdevNvmeGetControllersResponse
	bdevsErr    error
	ctrlErr     error
	attachErr   error
	detachErr   error
}

func (m *mockRPCClient) BdevGetBdevs() (BdevGetBdevsResponse, error) {
	return m.bdevs, m.bdevsErr
}

func (m *mockRPCClient) BdevNvmeGetControllers() (BdevNvmeGetControllersResponse, error) {
	return m.controllers, m.ctrlErr
}

func (m *mockRPCClient) BdevNvmeAttachController(req BdevNvmeAttachControllerRequest) (BdevNvmeAttachControllerResponse, error) {
	if m.attachErr != nil {
		return BdevNvmeAttachControllerResponse{}, m.attachErr
	}
	return BdevNvmeAttachControllerResponse{BdevName: "nvme_" + req.Name}, nil
}

func (m *mockRPCClient) BdevNvmeDetachController(req BdevNvmeDetachControllerRequest) error {
	return m.detachErr
}

// Close implements the Close method for the RPCClient interface
func (m *mockRPCClient) Close() error {
	// No-op for mock client
	return nil
}

// setupTestServer creates a StoragePluginServer with a mock RPC client for testing
func setupTestServer(mockClient *mockRPCClient) *StoragePluginServer {
	return &StoragePluginServer{
		newRPCClientFunc: func(socketPath string) (RPCClient, error) {
			return mockClient, nil
		},
	}
}
