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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	kmount "k8s.io/mount-utils"
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
		filepath.Join(tempDir, "test_fs_storage_vendor_dpu.sock"),
		filepath.Join(tempDir, "test_fs_storage_vendor_dpu_provider.sock"),
		filepath.Join(tempDir, "test_fs_storage_vendor_dpu_volumes"))
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
	require.Equal(t, pb.StoragePluginServiceCapability_RPC_TYPE_CREATE_DELETE_FS_DEVICE,
		resp.Capabilities[0].GetRpc().Type)
}

func TestGetSNAPProvider(t *testing.T) {
	server := &StoragePluginServer{}
	resp, err := server.GetSNAPProvider(context.Background(), &pb.GetSNAPProviderRequest{})
	require.NoError(t, err, "GetSNAPProvider should not return an error")
	require.Equal(t, defaultProviderName, resp.ProviderName)
}

func TestCreateDevice(t *testing.T) {
	tests := []struct {
		name        string
		volumeID    string
		fsdevs      FsdevGetFsdevsResponse
		createErr   error
		expectError bool
		expectCode  codes.Code
	}{
		{
			name:     "successful creation",
			volumeID: "test-vol",
			fsdevs: FsdevGetFsdevsResponse{
				Fsdevs: mockFsdevResponse.Fsdevs,
			},
			createErr:   nil,
			expectError: false,
		},
		{
			name:     "device already exists",
			volumeID: "fs_test",
			fsdevs: FsdevGetFsdevsResponse{
				Fsdevs: mockFsdevResponse.Fsdevs,
			},
			createErr:   nil,
			expectError: false,
		},
		{
			name:     "creation error",
			volumeID: "error-vol",
			fsdevs: FsdevGetFsdevsResponse{
				Fsdevs: mockFsdevResponse.Fsdevs,
			},
			createErr:   fmt.Errorf("failed to create filesystem"),
			expectError: true,
			expectCode:  codes.FailedPrecondition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for testing
			tempDir, err := os.MkdirTemp("", "test-storage-*")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer func() {
				if err := os.RemoveAll(tempDir); err != nil {
					t.Errorf("Failed to remove temp directory: %v", err)
				}
			}()

			client := &mockRPCClient{
				fsdevs:    tt.fsdevs,
				createErr: tt.createErr,
			}

			// Create fake mounter that always succeeds for mount commands
			fakeMounter := kmount.NewFakeMounter(nil)

			server := &StoragePluginServer{
				volumesPath:       filepath.Join(tempDir, "test_fs_storage_vendor_dpu_volumes"),
				snapRPCSocketPath: filepath.Join(tempDir, "test_fs_storage_vendor_dpu_provider.sock"),
				mounter:           fakeMounter,
				newRPCClientFunc: func(socketPath string) (RPCClient, error) {
					return client, nil
				},
			}

			req := &pb.CreateDeviceRequest{
				VolumeId: tt.volumeID,
				PublishContext: map[string]string{
					"nv-volumeName": "test-vol",
				},
				VolumeContext: map[string]string{
					"server": "test.example.com",
					"share":  "/export/test",
					"subdir": "subdir",
				},
			}

			resp, err := server.CreateDevice(context.Background(), req)
			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.expectCode, st.Code())
				return
			}

			require.NoError(t, err)
			assert.Equal(t, GetDeviceName(tt.volumeID), resp.DeviceName)
		})
	}
}

func TestDeleteDevice(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		fsdevs      FsdevGetFsdevsResponse
		deleteErr   error
		expectError bool
		expectCode  codes.Code
	}{
		{
			name:       "successful deletion",
			deviceName: "fs_test",
			fsdevs: FsdevGetFsdevsResponse{
				Fsdevs: mockFsdevResponse.Fsdevs,
			},
			deleteErr:   nil,
			expectError: false,
		},
		{
			name:       "device doesn't exist",
			deviceName: "fs_nonexistent",
			fsdevs: FsdevGetFsdevsResponse{
				Fsdevs: mockFsdevResponse.Fsdevs,
			},
			deleteErr:   nil,
			expectError: false,
		},
		{
			name:       "deletion error",
			deviceName: "fs_test",
			fsdevs: FsdevGetFsdevsResponse{
				Fsdevs: mockFsdevResponse.Fsdevs,
			},
			deleteErr:   fmt.Errorf("failed to delete filesystem"),
			expectError: true,
			expectCode:  codes.FailedPrecondition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for testing
			tempDir, err := os.MkdirTemp("", "test-storage-*")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer func() {
				if err := os.RemoveAll(tempDir); err != nil {
					t.Errorf("Failed to remove temp directory: %v", err)
				}
			}()

			client := &mockRPCClient{
				fsdevs:    tt.fsdevs,
				deleteErr: tt.deleteErr,
			}

			// Create fake mounter that always succeeds for umount commands
			fakeMounter := kmount.NewFakeMounter(nil)

			server := &StoragePluginServer{
				volumesPath:       filepath.Join(tempDir, "test_fs_storage_vendor_dpu_volumes"),
				snapRPCSocketPath: filepath.Join(tempDir, "test_fs_storage_vendor_dpu_provider.sock"),
				mounter:           fakeMounter,
				newRPCClientFunc: func(socketPath string) (RPCClient, error) {
					return client, nil
				},
			}

			req := &pb.DeleteDeviceRequest{
				DeviceName: tt.deviceName,
			}

			resp, err := server.DeleteDevice(context.Background(), req)
			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.expectCode, st.Code())
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, resp)
		})
	}
}

func TestGetDevice(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		fsdevs      FsdevGetFsdevsResponse
		expectError bool
		expectCode  codes.Code
	}{
		{
			name:       "device exists",
			deviceName: "fs_test",
			fsdevs: FsdevGetFsdevsResponse{
				Fsdevs: mockFsdevResponse.Fsdevs,
			},
			expectError: false,
		},
		{
			name:       "device doesn't exist",
			deviceName: "fs_nonexistent",
			fsdevs: FsdevGetFsdevsResponse{
				Fsdevs: mockFsdevResponse.Fsdevs,
			},
			expectError: true,
			expectCode:  codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockRPCClient{
				fsdevs: tt.fsdevs,
			}
			server := setupTestServer(client)

			req := &pb.GetDeviceRequest{
				DeviceName: tt.deviceName,
			}

			resp, err := server.GetDevice(context.Background(), req)
			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.expectCode, st.Code())
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "Filesystem", resp.VolumeMode)
			assert.Contains(t, resp.VolumeContext, "volumePath")
			assert.Contains(t, resp.VolumeContext, "type")
		})
	}
}

func TestGetDeviceName(t *testing.T) {
	tests := []struct {
		name     string
		volumeID string
	}{
		{
			name:     "short volume ID",
			volumeID: "vol1",
		},
		{
			name:     "long volume ID",
			volumeID: "very-long-volume-id-with-many-characters-to-test-hashing",
		},
		{
			name:     "volume ID with special characters",
			volumeID: "vol-123_test@domain.com",
		},
		{
			name:     "empty volume ID",
			volumeID: "",
		},
		{
			name:     "numeric volume ID",
			volumeID: "123456789",
		},
	}
	// Test that results are less than 17 characters
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetDeviceName(tt.volumeID)
			assert.NotEmpty(t, result, "Result should not be empty")
			assert.Less(t, len(result), 17, "Result should be less than 17 characters, got %d characters: %s", len(result), result)
		})
	}

	// Test that same values produce same results
	t.Run("same input produces same output", func(t *testing.T) {
		volumeID := "test-volume-123"
		result1 := GetDeviceName(volumeID)
		result2 := GetDeviceName(volumeID)
		assert.Equal(t, result1, result2, "Same input should produce same output")
	})

	// Test that different values produce different results
	t.Run("different inputs produce different outputs", func(t *testing.T) {
		volumeID1 := "test-volume-1"
		volumeID2 := "test-volume-2"
		result1 := GetDeviceName(volumeID1)
		result2 := GetDeviceName(volumeID2)
		assert.NotEqual(t, result1, result2, "Different inputs should produce different outputs")
	})

	// Test consistency across multiple calls with various inputs
	t.Run("consistency test", func(t *testing.T) {
		testInputs := []string{
			"vol1", "vol2", "vol3",
			"pvc-123-abc-def",
			"very-long-volume-identifier-with-uuid-like-structure",
		}

		results := make(map[string]string)
		for _, input := range testInputs {
			result := GetDeviceName(input)
			// Verify consistency
			assert.Equal(t, result, GetDeviceName(input), "Multiple calls with same input should be consistent")
			// Store result to check uniqueness
			results[input] = result
		}

		// Verify all results are unique
		seenResults := make(map[string]bool)
		for input, result := range results {
			assert.False(t, seenResults[result], "Result %s for input %s should be unique", result, input)
			seenResults[result] = true
		}
	})
}
