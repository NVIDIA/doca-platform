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
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"path/filepath"

	pb "github.com/nvidia/doca-platform/api/grpc/nvidia/storage/plugins/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
)

// StoragePluginServer represents the gRPC server for filesystem storage operations
type StoragePluginServer struct {
	pb.UnimplementedStoragePluginServiceServer
	pb.UnimplementedIdentityServiceServer

	mounter mount.Interface
	// newRPCClientFunc is a function that creates a new RPCClient,
	// required to mock the RPCClient for testing
	newRPCClientFunc func(socketPath string) (RPCClient, error)
	// volumesPath is the path to the directory where the volumes are mounted
	volumesPath string
	// snapRPCSocketPath is the path to the SNAP RPC socket
	snapRPCSocketPath string
}

// Configuration constants and their default values
const (
	// default value for the plugin name
	defaultPluginName = "nvidia-nfs"
	// default value for the provider name
	defaultProviderName = "nvidia"

	// Environment variable names for configuration
	// contains name of the environment variable that can be used to configure plugin socket path
	envPluginRPCSocketPath = "PLUGIN_SOCKET_PATH"
	// contains name of the environment variable that can be used to configure snap socket path
	envSNAPRPCSocketPath = "SNAP_SOCKET_PATH"
	// contains name of the environment variable that can be used to configure plugin name
	envPluginName = "PLUGIN_NAME"
	// contains name of the environment variable that can be used to configure provider name
	envProviderName = "PROVIDER_NAME"
	// contains name of the environment variable that can be used to configure volume directory
	envVolumeDirectory = "VOLUME_DIRECTORY"

	// default template for the plugin socket path
	defaultPluginRPCSocketPathTmpl = "/var/lib/nvidia/storage/snap/plugins/%s/dpu.sock"
	// default template for the snap socket path
	defaultSNAPRPCSocketPathTmpl = "/var/lib/nvidia/storage/snap/providers/%s/snap.sock"
	// default template for the volume directory path
	defaultVolumeDirectoryTmpl = "/var/lib/nvidia/storage/snap/providers/%s/volumes/%s"
)

// parameters that are set by the NFS CSI driver https://github.com/kubernetes-csi/csi-driver-nfs
// some options that are not supported by the DPU plugin are not included here
const (
	paramServer       = "server"
	paramShare        = "share"
	paramSubDir       = "subdir"
	mountOptionsField = "mountoptions"
)

// GetPluginName returns the plugin name, configurable via environment variable
func GetPluginName() string {
	if name := os.Getenv(envPluginName); name != "" {
		return name
	}
	return defaultPluginName
}

// GetProviderName returns the provider name, configurable via environment variable
func GetProviderName() string {
	if name := os.Getenv(envProviderName); name != "" {
		return name
	}
	return defaultProviderName
}

// GetPluginRPCSocketPath returns the plugin RPC socket path, configurable via environment variable
func GetPluginRPCSocketPath() string {
	if path := os.Getenv(envPluginRPCSocketPath); path != "" {
		return path
	}
	return fmt.Sprintf(defaultPluginRPCSocketPathTmpl, GetPluginName())
}

// GetSNAPRPCSocketPath returns the SNAP RPC socket path, configurable via environment variable
func GetSNAPRPCSocketPath() string {
	if path := os.Getenv(envSNAPRPCSocketPath); path != "" {
		return path
	}
	return fmt.Sprintf(defaultSNAPRPCSocketPathTmpl, GetProviderName())
}

// GetVolumeDirectory returns the volume directory, configurable via environment variable
func GetVolumeDirectory() string {
	if path := os.Getenv(envVolumeDirectory); path != "" {
		return path
	}
	return fmt.Sprintf(defaultVolumeDirectoryTmpl, GetProviderName(), GetPluginName())
}

// GetDeviceName generates suitable device name from the volumeID.
// Current implementation of the Virtio FS in snap service has limit for FS device name length equal to 21 characters.
// Caused by the fact that rmem pool names are limited to 31 characters
// FUSE dispatcher creates rmem pools with pattern "fuse_disp_" + fsdev_name (21 characters left).
// Note: resulting fsdev_name is "dev_" + name passed as param to FS device creation (17 characters left).
func GetDeviceName(volumeID string) string {
	hash := fnv.New64a()
	hash.Write([]byte(volumeID))
	return hex.EncodeToString(hash.Sum([]byte{}))
}

// CreateGRPCServer creates a new gRPC server and registers the StoragePluginServiceServer and IdentityServiceServer for it.
// Accepts optional parameters:
//   - socketPath: The Unix domain socket path where this plugin's gRPC server will listen for incoming requests.
//     If empty, defaults to GetPluginRPCSocketPath() which returns a path based on the plugin name.
//     The function will create the socket directory if it doesn't exist and remove any existing socket file.
//   - snapRPCSocketPath: The Unix domain socket path used to communicate with the SNAP service.
//     If empty, defaults to GetSNAPRPCSocketPath() which returns a path based on the provider name.
//     This path is stored in the StoragePluginServer and used for creating RPC clients to the SNAP service.
//   - volumeDirectory: The target directory for mounting volumes.
//     If empty, defaults to GetVolumeDirectory() which returns a path based on the provider and plugin names.
func CreateGRPCServer(socketPath, snapRPCSocketPath, volumeDirectory string) (*grpc.Server, net.Listener, error) {
	// Set defaults for empty values
	if socketPath == "" {
		socketPath = GetPluginRPCSocketPath()
	}
	if snapRPCSocketPath == "" {
		snapRPCSocketPath = GetSNAPRPCSocketPath()
	}
	if volumeDirectory == "" {
		volumeDirectory = GetVolumeDirectory()
	}

	// if the socket directory does not exist, create it
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create Plugin socket directory: %v", err)
	}
	// if volume directory does not exist, create it
	if err := os.MkdirAll(volumeDirectory, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create Volume directory: %v", err)
	}

	klog.Infof("Starting gRPC server initialization at %s, volume directory %s", socketPath, volumeDirectory)

	if _, err := os.Stat(socketPath); err == nil {
		klog.Infof("Socket file %s already exists. Removing it.", socketPath)
		if rmErr := os.Remove(socketPath); rmErr != nil {
			return nil, nil, fmt.Errorf("failed to remove existing socket file: %v", rmErr)
		}
	} else if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("error checking socket file: %v", err)
	}

	klog.Infof("Creating GRPC socket listener at %s", socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create listener: %v", err)
	}

	// Initialize the StoragePluginServer
	storageServer := &StoragePluginServer{
		volumesPath:       volumeDirectory,
		snapRPCSocketPath: snapRPCSocketPath,
		mounter:           mount.New(""),
		newRPCClientFunc: func(path string) (RPCClient, error) {
			klog.Infof("Creating new RPC client for path: %s", path)
			client, err := NewRPCClient(path)
			if err != nil {
				klog.Errorf("Failed to create RPC client: %v", err)
				return nil, err
			}
			return client, nil
		},
	}

	// Create a new gRPC server
	server := grpc.NewServer()
	pb.RegisterStoragePluginServiceServer(server, storageServer)
	pb.RegisterIdentityServiceServer(server, storageServer)

	klog.Infof("gRPC server is ready at %s (awaiting Serve call)", socketPath)
	return server, listener, nil
}

// GetPluginInfo RPC
func (s *StoragePluginServer) GetPluginInfo(ctx context.Context, req *pb.GetPluginInfoRequest) (*pb.GetPluginInfoResponse, error) {
	klog.Infof("Received GetPluginInfo request: %+v", req)
	pluginName := GetPluginName()
	resp := &pb.GetPluginInfoResponse{
		Name:          fmt.Sprintf("storage.dpu.%s.com", pluginName),
		VendorVersion: "1.0",
		Manifest: map[string]string{
			"description": fmt.Sprintf("%s SNAP Filesystem Storage Plugin", pluginName),
			"maintainer":  pluginName,
		},
	}
	klog.Infof("Responding with GetPluginInfo: %+v", resp)
	return resp, nil
}

// Probe RPC
func (s *StoragePluginServer) Probe(ctx context.Context, req *pb.ProbeRequest) (*pb.ProbeResponse, error) {
	klog.Infof("Received Probe request: %+v", req)
	resp := &pb.ProbeResponse{
		Ready: &wrapperspb.BoolValue{Value: true},
	}
	klog.Infof("Responding with Probe: %+v", resp)
	return resp, nil
}

// StoragePluginGetCapabilities RPC
func (s *StoragePluginServer) StoragePluginGetCapabilities(ctx context.Context, req *pb.StoragePluginGetCapabilitiesRequest) (*pb.StoragePluginGetCapabilitiesResponse, error) {
	klog.Infof("Received StoragePluginGetCapabilities request: %+v", req)
	capabilities := []*pb.StoragePluginServiceCapability{
		{
			Type: &pb.StoragePluginServiceCapability_Rpc{
				Rpc: &pb.StoragePluginServiceCapability_RPC{
					Type: pb.StoragePluginServiceCapability_RPC_TYPE_CREATE_DELETE_FS_DEVICE,
				},
			},
		},
	}
	resp := &pb.StoragePluginGetCapabilitiesResponse{
		Capabilities: capabilities,
	}
	klog.Infof("Responding with StoragePluginGetCapabilities: %+v", resp)
	return resp, nil
}

// GetSNAPProvider RPC
func (s *StoragePluginServer) GetSNAPProvider(ctx context.Context, req *pb.GetSNAPProviderRequest) (*pb.GetSNAPProviderResponse, error) {
	klog.Infof("Received GetSNAPProvider request: %+v", req)
	resp := &pb.GetSNAPProviderResponse{ProviderName: GetProviderName()}
	klog.Infof("Responding with GetSNAPProvider: %+v", resp)
	return resp, nil
}

// CreateDevice RPC
func (s *StoragePluginServer) CreateDevice(ctx context.Context, req *pb.CreateDeviceRequest) (*pb.CreateDeviceResponse, error) {
	klog.Infof("Received CreateDevice request: %+v", req)

	serverIP := req.GetVolumeContext()[paramServer]
	if serverIP == "" {
		return nil, status.Errorf(codes.InvalidArgument, "parameter %s must be set in volumeContext", paramServer)
	}

	exportName := req.GetVolumeContext()[paramShare]
	if exportName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "parameter %s must be set in volumeContext", paramShare)
	}

	subDir := req.GetVolumeContext()[paramSubDir]
	if subDir != "" {
		exportName = filepath.Join(exportName, subDir)
	}

	client, err := s.newRPCClientFunc(s.snapRPCSocketPath)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create rpcClient: %v", err)
		klog.Error(errMsg)
		return nil, status.Errorf(codes.Unavailable, "%s", errMsg)
	}
	defer func() {
		if err := client.Close(); err != nil {
			klog.Errorf("Failed to close client: %v", err)
		}
	}()

	fsdevs, err := client.FsdevGetFsdevs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get fsdevs: %v", err)
	}

	// Check if filesystem device already exists
	deviceName := GetDeviceName(req.GetVolumeId())

	if CheckFsdevExists(deviceName, fsdevs.Fsdevs) {
		klog.Infof("Filesystem device already exists: %s", deviceName)
		resp := &pb.CreateDeviceResponse{DeviceName: deviceName}
		klog.Infof("Responding with CreateDevice: %+v", resp)
		return resp, nil
	}

	err = client.FsdevNfsCreate(deviceName, exportName, serverIP)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create filesystem device: %v", err)
		klog.Error(errMsg)
		return nil, status.Errorf(codes.FailedPrecondition, "%s", errMsg)
	}

	resp := &pb.CreateDeviceResponse{DeviceName: deviceName}
	klog.Infof("Responding with CreateDevice: %+v", resp)
	return resp, nil
}

// DeleteDevice RPC
func (s *StoragePluginServer) DeleteDevice(ctx context.Context, req *pb.DeleteDeviceRequest) (*pb.DeleteDeviceResponse, error) {
	klog.Infof("Received DeleteDevice request: %+v", req)

	client, err := s.newRPCClientFunc(s.snapRPCSocketPath)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create rpcClient: %v", err)
		klog.Error(errMsg)
		return nil, status.Errorf(codes.Unavailable, "%s", errMsg)
	}
	defer func() {
		if err := client.Close(); err != nil {
			klog.Errorf("Failed to close client: %v", err)
		}
	}()

	fsdevs, err := client.FsdevGetFsdevs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get fsdevs: %v", err)
	}
	if CheckFsdevExists(req.DeviceName, fsdevs.Fsdevs) {
		// Delete the filesystem device
		err = client.FsdevNfsDelete(req.DeviceName)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to delete filesystem device %s: %v", req.DeviceName, err)
			klog.Error(errMsg)
			return nil, status.Errorf(codes.FailedPrecondition, "%s", errMsg)
		}
	} else {
		klog.Infof("Filesystem device %s does not exist. Skipping deletion.", req.DeviceName)
	}

	resp := &pb.DeleteDeviceResponse{}
	klog.Infof("Successfully deleted filesystem device: %s", req.DeviceName)
	return resp, nil
}

// GetDevice RPC
func (s *StoragePluginServer) GetDevice(ctx context.Context, req *pb.GetDeviceRequest) (*pb.GetDeviceResponse, error) {
	klog.Infof("Received GetDevice request: %+v", req)

	client, err := s.newRPCClientFunc(s.snapRPCSocketPath)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create rpcClient: %v", err)
		klog.Error(errMsg)
		return nil, status.Errorf(codes.Unavailable, "%s", errMsg)
	}
	defer func() {
		if err := client.Close(); err != nil {
			klog.Errorf("Failed to close client: %v", err)
		}
	}()

	fsdevs, err := client.FsdevGetFsdevs()
	if err != nil {
		errMsg := fmt.Sprintf("Failed to get fsdevs: %v", err)
		klog.Error(errMsg)
		return nil, status.Errorf(codes.Internal, "%s", errMsg)
	}

	var targetFsdev *Fsdev
	for _, fsdev := range fsdevs.Fsdevs {
		if fsdev.Name == req.DeviceName {
			targetFsdev = &fsdev
			break
		}
	}

	if targetFsdev == nil {
		klog.Infof("Filesystem device %s does not exist.", req.DeviceName)
		return nil, status.Errorf(codes.NotFound, "Device %s not found", req.DeviceName)
	}

	response := &pb.GetDeviceResponse{
		VolumeMode: "Filesystem",
		VolumeContext: map[string]string{
			"volumePath": targetFsdev.ModuleSpecific.RootPath,
			"type":       targetFsdev.ModuleName,
		},
	}

	klog.Infof("Returning GetDevice response: %+v", response)
	return response, nil
}

// ListDevices RPC
func (s *StoragePluginServer) ListDevices(ctx context.Context, req *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "ListDevices is not implemented")
}
