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

	pb "github.com/nvidia/doca-platform/api/grpc/nvidia/storage/plugins/v1"
	real "github.com/nvidia/doca-platform/internal/storage/snap/storage-vendor-dpu-plugin/fs-storage-vendor-dpu-plugin"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/klog/v2"
)

type storagePluginServer struct {
	pb.UnimplementedStoragePluginServiceServer
	pb.UnimplementedIdentityServiceServer

	newRPCClientFunc  func(socketPath string) (real.RPCClient, error)
	volumesPath       string
	snapRPCSocketPath string
}

// Error message formats reused across the RPC handlers.
const (
	errFmtCreateRPCClient = "failed to create rpcClient: %v"
	errFmtGetFsdevs       = "failed to get fsdevs: %v"
)

// createGRPCServer creates the fake gRPC server. Same signature as the real one
// so the same main.go pattern works.
func createGRPCServer(socketPath, snapRPCSocketPath, volumeDirectory string) (*grpc.Server, net.Listener, error) {
	if socketPath == "" {
		socketPath = real.GetPluginRPCSocketPath()
	}
	if snapRPCSocketPath == "" {
		snapRPCSocketPath = real.GetSNAPRPCSocketPath()
	}
	if volumeDirectory == "" {
		volumeDirectory = real.GetVolumeDirectory()
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create plugin socket directory: %v", err)
	}
	if err := os.MkdirAll(volumeDirectory, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create volume directory: %v", err)
	}

	klog.Infof("Starting fake gRPC server at %s, volume directory %s", socketPath, volumeDirectory)

	if _, err := os.Stat(socketPath); err == nil {
		if rmErr := os.Remove(socketPath); rmErr != nil {
			return nil, nil, fmt.Errorf("failed to remove existing socket file: %v", rmErr)
		}
	} else if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("error checking socket file: %v", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create listener: %v", err)
	}

	srv := &storagePluginServer{
		volumesPath:       volumeDirectory,
		snapRPCSocketPath: snapRPCSocketPath,
		newRPCClientFunc: func(path string) (real.RPCClient, error) {
			return real.NewRPCClient(path)
		},
	}

	server := grpc.NewServer()
	pb.RegisterStoragePluginServiceServer(server, srv)
	pb.RegisterIdentityServiceServer(server, srv)

	klog.Infof("Fake gRPC server ready at %s", socketPath)
	return server, listener, nil
}

func (s *storagePluginServer) GetPluginInfo(_ context.Context, _ *pb.GetPluginInfoRequest) (*pb.GetPluginInfoResponse, error) {
	name := real.GetPluginName()
	return &pb.GetPluginInfoResponse{
		Name:          fmt.Sprintf("storage.dpu.%s.com", name),
		VendorVersion: "1.0-fake",
		Manifest: map[string]string{
			"description": fmt.Sprintf("%s SNAP Filesystem Storage Plugin (fake, local backing)", name),
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
					Type: pb.StoragePluginServiceCapability_RPC_TYPE_CREATE_DELETE_FS_DEVICE,
				},
			},
		}},
	}, nil
}

func (s *storagePluginServer) GetSNAPProvider(_ context.Context, _ *pb.GetSNAPProviderRequest) (*pb.GetSNAPProviderResponse, error) {
	return &pb.GetSNAPProviderResponse{ProviderName: real.GetProviderName()}, nil
}

// deviceVolumePath maps deviceName to its on-disk directory (s.volumesPath/<deviceName>), where the fake
// plugin keeps that device's data. deviceName must be a plain name: it errors on "", ".", ".." or any name
// containing "/". That keeps the path inside s.volumesPath, so CreateDevice's MkdirAll and DeleteDevice's
// RemoveAll can never act outside the volume root.
func (s *storagePluginServer) deviceVolumePath(deviceName string) (string, error) {
	if deviceName == "" || deviceName == "." || deviceName == ".." || deviceName != filepath.Base(deviceName) {
		return "", status.Errorf(codes.InvalidArgument, "invalid device name %q", deviceName)
	}
	return filepath.Join(s.volumesPath, deviceName), nil
}

// CreateDevice creates a local directory and registers it with SNAP via JSON-RPC.
// No NFS mount — the local directory IS the backing storage.
func (s *storagePluginServer) CreateDevice(ctx context.Context, req *pb.CreateDeviceRequest) (*pb.CreateDeviceResponse, error) {
	klog.Infof("CreateDevice (fake, local backing): %+v", req)

	client, err := s.newRPCClientFunc(s.snapRPCSocketPath)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, errFmtCreateRPCClient, err)
	}
	defer func() { _ = client.Close() }()

	fsdevs, err := client.FsdevGetFsdevs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, errFmtGetFsdevs, err)
	}

	deviceName := real.GetDeviceName(req.GetVolumeId())

	if real.CheckFsdevExists(deviceName, fsdevs.Fsdevs) {
		klog.Infof("Device already exists: %s", deviceName)
		return &pb.CreateDeviceResponse{DeviceName: deviceName}, nil
	}

	volumePath, err := s.deviceVolumePath(deviceName)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(volumePath, 0755); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to create volume directory %s: %v", volumePath, err)
	}

	if err := client.FsdevAioCreate(deviceName, volumePath); err != nil {
		if rmErr := os.RemoveAll(volumePath); rmErr != nil {
			klog.Errorf("Failed to remove volume directory %s after device creation failed: %v", volumePath, rmErr)
		}
		return nil, status.Errorf(codes.FailedPrecondition, "failed to create filesystem device: %v", err)
	}

	klog.Infof("Created device %s with local backing at %s", deviceName, volumePath)
	return &pb.CreateDeviceResponse{DeviceName: deviceName}, nil
}

// DeleteDevice removes the SNAP device and cleans up the local directory.
func (s *storagePluginServer) DeleteDevice(ctx context.Context, req *pb.DeleteDeviceRequest) (*pb.DeleteDeviceResponse, error) {
	klog.Infof("DeleteDevice (fake, local backing): %+v", req)

	client, err := s.newRPCClientFunc(s.snapRPCSocketPath)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, errFmtCreateRPCClient, err)
	}
	defer func() { _ = client.Close() }()

	fsdevs, err := client.FsdevGetFsdevs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, errFmtGetFsdevs, err)
	}

	if real.CheckFsdevExists(req.DeviceName, fsdevs.Fsdevs) {
		if err := client.FsdevAioDelete(req.DeviceName); err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to delete filesystem device %s: %v", req.DeviceName, err)
		}
	} else {
		klog.Infof("Device %s does not exist, skipping SNAP deletion", req.DeviceName)
	}

	volumePath, err := s.deviceVolumePath(req.DeviceName)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(volumePath); err != nil {
		klog.Errorf("Failed to remove volume directory %s: %v", volumePath, err)
	}

	klog.Infof("Deleted device %s", req.DeviceName)
	return &pb.DeleteDeviceResponse{}, nil
}

func (s *storagePluginServer) GetDevice(ctx context.Context, req *pb.GetDeviceRequest) (*pb.GetDeviceResponse, error) {
	klog.Infof("GetDevice (fake): %+v", req)

	client, err := s.newRPCClientFunc(s.snapRPCSocketPath)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, errFmtCreateRPCClient, err)
	}
	defer func() { _ = client.Close() }()

	fsdevs, err := client.FsdevGetFsdevs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, errFmtGetFsdevs, err)
	}

	for _, fsdev := range fsdevs.Fsdevs {
		if fsdev.Name == req.DeviceName {
			return &pb.GetDeviceResponse{
				VolumeMode: "Filesystem",
				VolumeContext: map[string]string{
					"volumePath": fsdev.ModuleSpecific.RootPath,
					"type":       fsdev.ModuleName,
				},
			}, nil
		}
	}

	return nil, status.Errorf(codes.NotFound, "device %s not found", req.DeviceName)
}

func (s *storagePluginServer) ListDevices(_ context.Context, _ *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "ListDevices is not implemented")
}
