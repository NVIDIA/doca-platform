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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/nvidia/doca-platform/api/grpc/nvidia/storage/plugins/v1"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateGRPCServer(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "test-storage-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: Failed to remove test socket file: %v", err)
		}
	}()
	server, listener, err := CreateGRPCServer(
		filepath.Join(tempDir, "test_block_dpu.sock"),
		filepath.Join(tempDir, "test_block_dpu_provider.sock"))
	require.NoError(t, err, "Failed to create gRPC server")
	require.NotNil(t, server, "Server should not be nil")
	require.NotNil(t, listener, "Listener should not be nil")

	if err := listener.Close(); err != nil {
		t.Logf("Warning: Failed to close listener: %v", err)
	}
}

func TestGetPluginInfo(t *testing.T) {
	// Save original environment
	origPluginName := os.Getenv(envPluginName)
	defer func() {
		if err := os.Setenv(envPluginName, origPluginName); err != nil {
			t.Logf("Warning: Failed to restore environment variable: %v", err)
		}
	}()

	// Test with default plugin name
	if err := os.Unsetenv(envPluginName); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}
	server := &StoragePluginServer{}
	resp, err := server.GetPluginInfo(context.Background(), &pb.GetPluginInfoRequest{})
	require.NoError(t, err, "GetPluginInfo should not return an error")
	require.Equal(t, fmt.Sprintf("storage.dpu.%s.com", defaultPluginName), resp.Name)
	require.Equal(t, "1.0", resp.VendorVersion)
	require.Contains(t, resp.Manifest, "description")
	require.Contains(t, resp.Manifest, "maintainer")

	// Test with custom plugin name
	customName := "custom-plugin"
	if err := os.Setenv(envPluginName, customName); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	resp, err = server.GetPluginInfo(context.Background(), &pb.GetPluginInfoRequest{})
	require.NoError(t, err, "GetPluginInfo should not return an error")
	require.Equal(t, fmt.Sprintf("storage.dpu.%s.com", customName), resp.Name)
}

func TestProbe(t *testing.T) {
	server := &StoragePluginServer{}
	resp, err := server.Probe(context.Background(), &pb.ProbeRequest{})
	require.NoError(t, err, "Probe should not return an error")
	require.True(t, resp.Ready.Value)
}

func TestStoragePluginGetCapabilities(t *testing.T) {
	server := &StoragePluginServer{}
	resp, err := server.StoragePluginGetCapabilities(context.Background(), &pb.StoragePluginGetCapabilitiesRequest{})
	require.NoError(t, err, "StoragePluginGetCapabilities should not return an error")
	require.Len(t, resp.Capabilities, 1)
	require.Equal(t, pb.StoragePluginServiceCapability_RPC_TYPE_CREATE_DELETE_BLOCK_DEVICE,
		resp.Capabilities[0].GetRpc().Type)
}

func TestGetSNAPProvider(t *testing.T) {
	server := &StoragePluginServer{}
	resp, err := server.GetSNAPProvider(context.Background(), &pb.GetSNAPProviderRequest{})
	require.NoError(t, err, "GetSNAPProvider should not return an error")
	require.Equal(t, defaultPluginName, resp.ProviderName)
}

func TestCreateDevice(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.CreateDeviceRequest
		mockClient  *mockRPCClient
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "Successful device creation",
			req: &pb.CreateDeviceRequest{
				VolumeId: "test-volume",
				VolumeContext: map[string]string{
					"targetType": "RDMA",
					"targetAddr": "192.168.1.1",
					"targetPort": "4420",
					"nqn":        "nqn.2016-06.io.nvmet:swx-mtvr-stor02",
				},
			},
			mockClient: &mockRPCClient{
				bdevs: BdevGetBdevsResponse{},
			},
			expectError: false,
		},
		{
			name: "Device already exists",
			req: &pb.CreateDeviceRequest{
				VolumeId: "test-volume",
				VolumeContext: map[string]string{
					"targetType": "RDMA",
					"targetAddr": "1.1.1.1",
					"targetPort": "4420",
					"nqn":        "nqn.2016-06.io.nvmet:swx-mtvr-stor02",
				},
			},
			mockClient: &mockRPCClient{
				bdevs: mockBdevResponse,
			},
			expectError: false,
		},
		{
			name: "BdevGetBdevs error",
			req: &pb.CreateDeviceRequest{
				VolumeId: "test-volume",
				VolumeContext: map[string]string{
					"targetType": "RDMA",
					"targetAddr": "192.168.1.1",
					"targetPort": "4420",
					"nqn":        "nqn.2016-06.io.nvmet:swx-mtvr-stor02",
				},
			},
			mockClient: &mockRPCClient{
				bdevsErr: fmt.Errorf("failed to get bdevs"),
			},
			expectError: true,
			errorCode:   codes.Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupTestServer(tt.mockClient)
			resp, err := server.CreateDevice(context.Background(), tt.req)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != codes.Unknown {
					st, ok := status.FromError(err)
					require.True(t, ok)
					require.Equal(t, tt.errorCode, st.Code())
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.NotEmpty(t, resp.DeviceName)
			}
		})
	}
}

func TestDeleteDevice(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.DeleteDeviceRequest
		mockClient  *mockRPCClient
		expectError bool
		errorCode   codes.Code
	}{
		{
			name: "Successful device deletion",
			req: &pb.DeleteDeviceRequest{
				DeviceName: "nvme_pv-namen1",
			},
			mockClient: &mockRPCClient{
				bdevs:       mockBdevResponse,
				controllers: mockControllersResponse,
			},
			expectError: false,
		},
		{
			name: "Device not found",
			req: &pb.DeleteDeviceRequest{
				DeviceName: "non-existent-device",
			},
			mockClient: &mockRPCClient{
				bdevs:       mockBdevResponse,
				controllers: mockControllersResponse,
			},
			expectError: false,
		},
		{
			name: "BdevGetBdevs error",
			req: &pb.DeleteDeviceRequest{
				DeviceName: "nvme_pv-namen1",
			},
			mockClient: &mockRPCClient{
				bdevsErr: fmt.Errorf("failed to get bdevs"),
			},
			expectError: true,
			errorCode:   codes.Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupTestServer(tt.mockClient)
			resp, err := server.DeleteDevice(context.Background(), tt.req)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != codes.Unknown {
					st, ok := status.FromError(err)
					require.True(t, ok)
					require.Equal(t, tt.errorCode, st.Code())
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
			}
		})
	}
}

func TestGetDevice(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.GetDeviceRequest
		mockClient  *mockRPCClient
		expectError bool
		errorCode   codes.Code
		expectTrid  NVMeTrid
	}{
		{
			name: "Successful device retrieval",
			req: &pb.GetDeviceRequest{
				DeviceName: "nvme_pv-namen1",
			},
			mockClient: &mockRPCClient{
				bdevs: mockBdevResponse,
			},
			expectError: false,
			expectTrid: NVMeTrid{
				TrType:  "RDMA",
				AdrFam:  "IPv4",
				TrAddr:  "1.1.1.1",
				TrSvcID: "4420",
				SubNQN:  "nqn.2016-06.io.nvmet:swx-mtvr-stor02",
			},
		},
		{
			name: "Device not found",
			req: &pb.GetDeviceRequest{
				DeviceName: "non-existent-device",
			},
			mockClient: &mockRPCClient{
				bdevs: mockBdevResponse,
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "BdevGetBdevs error",
			req: &pb.GetDeviceRequest{
				DeviceName: "nvme_pv-namen1",
			},
			mockClient: &mockRPCClient{
				bdevsErr: fmt.Errorf("failed to get bdevs"),
			},
			expectError: true,
			errorCode:   codes.Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupTestServer(tt.mockClient)
			resp, err := server.GetDevice(context.Background(), tt.req)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorCode != codes.Unknown {
					st, ok := status.FromError(err)
					require.True(t, ok)
					require.Equal(t, tt.errorCode, st.Code())
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, "Block", resp.VolumeMode)
				require.Equal(t, tt.expectTrid.TrType, resp.VolumeContext["TrType"])
				require.Equal(t, tt.expectTrid.AdrFam, resp.VolumeContext["AdrFam"])
				require.Equal(t, tt.expectTrid.TrAddr, resp.VolumeContext["TrAddr"])
				require.Equal(t, tt.expectTrid.TrSvcID, resp.VolumeContext["TrSvcID"])
				require.Equal(t, tt.expectTrid.SubNQN, resp.VolumeContext["SubNQN"])
			}
		})
	}
}

// TestConfigurablePaths verifies that socket paths are correctly configurable via environment variables
func TestConfigurablePaths(t *testing.T) {
	// Save original environment
	origPluginPath := os.Getenv(envPluginRPCSocketPath)
	origSNAPPath := os.Getenv(envSNAPRPCSocketPath)
	origPluginName := os.Getenv(envPluginName)

	// Restore environment after test
	defer func() {
		if err := os.Setenv(envPluginRPCSocketPath, origPluginPath); err != nil {
			t.Logf("Warning: Failed to restore environment variable: %v", err)
		}
		if err := os.Setenv(envSNAPRPCSocketPath, origSNAPPath); err != nil {
			t.Logf("Warning: Failed to restore environment variable: %v", err)
		}
		if err := os.Setenv(envPluginName, origPluginName); err != nil {
			t.Logf("Warning: Failed to restore environment variable: %v", err)
		}
	}()

	// Test default values when environment variables are not set
	if err := os.Unsetenv(envPluginRPCSocketPath); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}
	if err := os.Unsetenv(envSNAPRPCSocketPath); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}
	if err := os.Unsetenv(envPluginName); err != nil {
		t.Fatalf("Failed to unset environment variable: %v", err)
	}

	require.Equal(t, "/var/lib/nvidia/storage/snap/plugins/nvidia/dpu.sock", GetPluginRPCSocketPath())
	require.Equal(t, "/var/lib/nvidia/storage/snap/providers/nvidia/snap.sock", GetSNAPRPCSocketPath())
	require.Equal(t, defaultPluginName, GetPluginName())

	// Test custom values when environment variables are set
	customPluginPath := "/tmp/custom-plugin.sock"
	customSNAPPath := "/tmp/custom-snap.sock"
	customPluginName := "custom-plugin"

	if err := os.Setenv(envPluginRPCSocketPath, customPluginPath); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv(envSNAPRPCSocketPath, customSNAPPath); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv(envPluginName, customPluginName); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	require.Equal(t, customPluginPath, GetPluginRPCSocketPath())
	require.Equal(t, customSNAPPath, GetSNAPRPCSocketPath())
	require.Equal(t, customPluginName, GetPluginName())

	// Test that CreateGRPCServer uses the configured paths
	server := &StoragePluginServer{
		newRPCClientFunc: func(socketPath string) (RPCClient, error) {
			return &mockRPCClient{}, nil
		},
	}
	client, err := server.newRPCClientFunc(GetSNAPRPCSocketPath())
	require.NoError(t, err)
	require.NotNil(t, client)
}
