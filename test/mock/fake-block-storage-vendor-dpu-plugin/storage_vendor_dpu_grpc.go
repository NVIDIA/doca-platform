/*
Copyright 2026 NVIDIA

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

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	pb "github.com/nvidia/doca-platform/api/grpc/nvidia/storage/plugins/v1"
	real "github.com/nvidia/doca-platform/internal/storage/snap/storage-vendor-dpu-plugin/block-storage-vendor-dpu-plugin"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/klog/v2"
)

const (
	envVolumeSizeBytes = "FAKE_BLOCK_VOLUME_SIZE_BYTES"
	defaultVolumeSize  = int64(1 << 30)
	aioBlockSize       = 512
)

type rpcClient interface {
	BdevGetBdevs() (real.BdevGetBdevsResponse, error)
	Call(method string, params interface{}) (interface{}, error)
	Close() error
}

type storagePluginServer struct {
	pb.UnimplementedStoragePluginServiceServer
	pb.UnimplementedIdentityServiceServer

	newRPCClientFunc  func(socketPath string) (rpcClient, error)
	volumesPath       string
	volumeSize        int64
	snapRPCSocketPath string
	mu                sync.Mutex
}

func configuredVolumeSize() (int64, error) {
	value := os.Getenv(envVolumeSizeBytes)
	if value == "" {
		return defaultVolumeSize, nil
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size <= 0 || size%aioBlockSize != 0 {
		return 0, fmt.Errorf("%s must be a positive multiple of %d", envVolumeSizeBytes, aioBlockSize)
	}
	return size, nil
}

func createGRPCServer() (*grpc.Server, net.Listener, error) {
	socketPath := real.GetPluginRPCSocketPath()
	snapRPCSocketPath := real.GetSNAPRPCSocketPath()
	volumeDirectory := filepath.Join(filepath.Dir(snapRPCSocketPath), "fake-block-volumes")
	volumeSize, err := configuredVolumeSize()
	if err != nil {
		return nil, nil, err
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create plugin socket directory: %v", err)
	}
	if err := os.MkdirAll(volumeDirectory, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create volume directory: %v", err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("failed to remove existing socket file: %v", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create listener: %v", err)
	}

	srv := &storagePluginServer{
		volumesPath:       volumeDirectory,
		volumeSize:        volumeSize,
		snapRPCSocketPath: snapRPCSocketPath,
		newRPCClientFunc: func(path string) (rpcClient, error) {
			return real.NewRPCClient(path)
		},
	}
	server := grpc.NewServer()
	pb.RegisterStoragePluginServiceServer(server, srv)
	pb.RegisterIdentityServiceServer(server, srv)
	return server, listener, nil
}

func (s *storagePluginServer) GetPluginInfo(_ context.Context, _ *pb.GetPluginInfoRequest) (*pb.GetPluginInfoResponse, error) {
	name := real.GetPluginName()
	return &pb.GetPluginInfoResponse{
		Name:          fmt.Sprintf("storage.dpu.%s.com", name),
		VendorVersion: "1.0-fake",
		Manifest: map[string]string{
			"description": fmt.Sprintf("%s SNAP block storage plugin (fake, local AIO backing)", name),
		},
	}, nil
}

func (s *storagePluginServer) Probe(_ context.Context, _ *pb.ProbeRequest) (*pb.ProbeResponse, error) {
	return &pb.ProbeResponse{Ready: &wrapperspb.BoolValue{Value: true}}, nil
}

func (s *storagePluginServer) StoragePluginGetCapabilities(_ context.Context, _ *pb.StoragePluginGetCapabilitiesRequest) (*pb.StoragePluginGetCapabilitiesResponse, error) {
	return &pb.StoragePluginGetCapabilitiesResponse{
		Capabilities: []*pb.StoragePluginServiceCapability{{
			Type: &pb.StoragePluginServiceCapability_Rpc{
				Rpc: &pb.StoragePluginServiceCapability_RPC{
					Type: pb.StoragePluginServiceCapability_RPC_TYPE_CREATE_DELETE_BLOCK_DEVICE,
				},
			},
		}},
	}, nil
}

func (s *storagePluginServer) GetSNAPProvider(_ context.Context, _ *pb.GetSNAPProviderRequest) (*pb.GetSNAPProviderResponse, error) {
	return &pb.GetSNAPProviderResponse{ProviderName: real.GetProviderName()}, nil
}

func (s *storagePluginServer) deviceName(volumeID string) (string, error) {
	if volumeID == "" || volumeID == "." || volumeID == ".." || volumeID != filepath.Base(volumeID) {
		return "", status.Errorf(codes.InvalidArgument, "invalid volume ID %q", volumeID)
	}
	return "aio_" + volumeID, nil
}

func (s *storagePluginServer) deviceVolumePath(deviceName string) (string, error) {
	if deviceName == "" || deviceName == "." || deviceName == ".." || deviceName != filepath.Base(deviceName) {
		return "", status.Errorf(codes.InvalidArgument, "invalid device name %q", deviceName)
	}
	return filepath.Join(s.volumesPath, deviceName+".img"), nil
}

func (s *storagePluginServer) CreateDevice(_ context.Context, req *pb.CreateDeviceRequest) (*pb.CreateDeviceResponse, error) {
	if req.GetVolumeMode() != "Block" {
		return nil, status.Errorf(codes.InvalidArgument, "volume mode must be Block")
	}
	deviceName, err := s.deviceName(req.GetVolumeId())
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.newRPCClientFunc(s.snapRPCSocketPath)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "failed to create RPC client: %v", err)
	}
	defer func() { _ = client.Close() }()

	bdevs, err := client.BdevGetBdevs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get bdevs: %v", err)
	}
	exists, err := real.CheckBdevExistsByBdev(deviceName, bdevs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check bdev: %v", err)
	}
	if exists {
		return &pb.CreateDeviceResponse{DeviceName: deviceName}, nil
	}

	volumePath, err := s.deviceVolumePath(deviceName)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(volumePath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to create backing file: %v", err)
	}
	if err := file.Truncate(s.volumeSize); err != nil {
		_ = file.Close()
		_ = os.Remove(volumePath)
		return nil, status.Errorf(codes.FailedPrecondition, "failed to size backing file: %v", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(volumePath)
		return nil, status.Errorf(codes.FailedPrecondition, "failed to close backing file: %v", err)
	}

	_, err = client.Call("bdev_aio_create", map[string]interface{}{
		"name":       deviceName,
		"filename":   volumePath,
		"block_size": aioBlockSize,
	})
	if err != nil {
		_ = os.Remove(volumePath)
		return nil, status.Errorf(codes.FailedPrecondition, "failed to create AIO bdev: %v", err)
	}

	klog.Infof("Created AIO bdev %s with local backing at %s", deviceName, volumePath)
	return &pb.CreateDeviceResponse{DeviceName: deviceName}, nil
}

func (s *storagePluginServer) DeleteDevice(_ context.Context, req *pb.DeleteDeviceRequest) (*pb.DeleteDeviceResponse, error) {
	deviceName := req.GetDeviceName()
	klog.Infof("Deleting AIO bdev %q", deviceName)
	// The node driver may detach before a device was created.
	if deviceName == "" {
		klog.Info("No device name provided, skipping deletion")
		return &pb.DeleteDeviceResponse{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	volumePath, err := s.deviceVolumePath(deviceName)
	if err != nil {
		return nil, err
	}
	client, err := s.newRPCClientFunc(s.snapRPCSocketPath)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "failed to create RPC client: %v", err)
	}
	defer func() { _ = client.Close() }()

	bdevs, err := client.BdevGetBdevs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get bdevs: %v", err)
	}
	exists, err := real.CheckBdevExistsByBdev(deviceName, bdevs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check bdev: %v", err)
	}
	if exists {
		if _, err := client.Call("bdev_aio_delete", map[string]interface{}{"name": deviceName}); err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to delete AIO bdev: %v", err)
		}
		klog.Infof("Deleted AIO bdev %q", deviceName)
	} else {
		klog.Infof("AIO bdev %q does not exist, skipping SNAP deletion", deviceName)
	}
	if err := os.Remove(volumePath); err != nil {
		if !os.IsNotExist(err) {
			return nil, status.Errorf(codes.Internal, "failed to remove backing file: %v", err)
		}
		klog.Infof("Backing file %s does not exist", volumePath)
	} else {
		klog.Infof("Removed backing file %s", volumePath)
	}
	klog.Infof("Completed deletion for AIO bdev %q", deviceName)
	return &pb.DeleteDeviceResponse{}, nil
}

func (s *storagePluginServer) GetDevice(_ context.Context, req *pb.GetDeviceRequest) (*pb.GetDeviceResponse, error) {
	client, err := s.newRPCClientFunc(s.snapRPCSocketPath)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "failed to create RPC client: %v", err)
	}
	defer func() { _ = client.Close() }()

	bdevs, err := client.BdevGetBdevs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get bdevs: %v", err)
	}
	exists, err := real.CheckBdevExistsByBdev(req.GetDeviceName(), bdevs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check bdev: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "device %s not found", req.GetDeviceName())
	}
	return &pb.GetDeviceResponse{VolumeMode: "Block"}, nil
}

func (s *storagePluginServer) ListDevices(_ context.Context, _ *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "ListDevices is not implemented")
}
