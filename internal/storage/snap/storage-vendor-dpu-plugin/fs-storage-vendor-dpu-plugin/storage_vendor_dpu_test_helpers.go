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

import (
	"k8s.io/klog/v2"
)

// Mock responses and test data
var (
	// mockFsdevResponse represents a mock response for FsdevGetFsdevs
	mockFsdevResponse = FsdevGetFsdevsResponse{
		Fsdevs: []Fsdev{
			{
				Name:       "fs_test",
				ModuleName: "aio",
				ModuleSpecific: FsdevModuleSpecific{
					RootPath: "/etc/virtiofs/test",
				},
			},
		},
	}
)

// Mock RPCClient for testing filesystem operations
type mockRPCClient struct {
	// Filesystem related fields
	fsdevs    FsdevGetFsdevsResponse
	fsdevsErr error
	createErr error
	deleteErr error
}

// Filesystem related methods
func (m *mockRPCClient) FsdevGetFsdevs() (FsdevGetFsdevsResponse, error) {
	return m.fsdevs, m.fsdevsErr
}

func (m *mockRPCClient) FsdevAioCreate(name string, volumePath string) error {
	if m.createErr != nil {
		return m.createErr
	}
	klog.Infof("Mock: Created filesystem device %s", name)
	return nil
}

func (m *mockRPCClient) FsdevAioDelete(name string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	klog.Infof("Mock: Deleted filesystem device %s", name)
	return nil
}

// Close implements the Close method for the RPCClient interface
func (m *mockRPCClient) Close() error {
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
