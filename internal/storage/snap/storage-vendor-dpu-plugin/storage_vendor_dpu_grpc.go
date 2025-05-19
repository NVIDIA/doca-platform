/*
Copyright 2024 NVIDIA

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
	"net"
	"os"

	pb "github.com/nvidia/doca-platform/api/grpc/nvidia/storage/plugins/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/klog/v2"
)

// StoragePluginServer represents the gRPC server for storage operations
type StoragePluginServer struct {
	pb.UnimplementedStoragePluginServiceServer
	pb.UnimplementedIdentityServiceServer
	client RPCClient // Interface for RPC operations
}

// Configuration constants and their default values
const (
	pluginName = "nvidia"

	// Default socket paths
	defaultPluginRPCSocketPath = "/var/lib/nvidia/storage/snap/plugins/nvidia/dpu.sock"
	defaultSNAPRPCSocketPath   = "/var/lib/nvidia/storage/snap/providers/nvidia/snap.sock"

	// Environment variable names for configuration
	envPluginRPCSocketPath = "PLUGIN_SOCKET_PATH"
	envSNAPRPCSocketPath   = "SNAP_SOCKET_PATH"
	envPluginName          = "PLUGIN_NAME"
)

// GetPluginName returns the plugin name, configurable via environment variable
func GetPluginName() string {
	if name := os.Getenv(envPluginName); name != "" {
		return name
	}
	return pluginName
}

// GetPluginRPCSocketPath returns the plugin RPC socket path, configurable via environment variable
func GetPluginRPCSocketPath() string {
	if path := os.Getenv(envPluginRPCSocketPath); path != "" {
		return path
	}
	return defaultPluginRPCSocketPath
}

// GetSNAPRPCSocketPath returns the SNAP RPC socket path, configurable via environment variable
func GetSNAPRPCSocketPath() string {
	if path := os.Getenv(envSNAPRPCSocketPath); path != "" {
		return path
	}
	return defaultSNAPRPCSocketPath
}

// CreateGRPCServer creates a new gRPC server with the specified socket path
func CreateGRPCServer(socketPath string) (*grpc.Server, net.Listener, error) {
	// If socketPath is empty, use the configured default
	if socketPath == "" {
		socketPath = GetPluginRPCSocketPath()
	}

	klog.Infof("Starting gRPC server initialization at %s", socketPath)

	// If the socket file already exists, remove it
	if _, err := os.Stat(socketPath); err == nil {
		klog.Infof("Socket file %s already exists. Removing it.", socketPath)
		if rmErr := os.Remove(socketPath); rmErr != nil {
			return nil, nil, fmt.Errorf("failed to remove existing socket file: %v", rmErr)
		}
	} else if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("error checking socket file: %v", err)
	}

	// Create a Unix domain socket listener
	klog.Infof("Creating GRPC socket listener at %s", socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create listener: %v", err)
	}

	// Initialize the StoragePluginServer
	storageServer := &StoragePluginServer{}

	// Create a new gRPC server
	server := grpc.NewServer()
	pb.RegisterStoragePluginServiceServer(server, storageServer)
	pb.RegisterIdentityServiceServer(server, storageServer)

	klog.Infof("gRPC server is ready at %s (awaiting Serve call)", socketPath)

	return server, listener, nil
}

// getRPCClient creates a new RPC client. For testing, it returns the mock client if set
func (s *StoragePluginServer) getRPCClient() error {
	// For tests, if the client is already a mockRPCClient, just return
	if _, ok := s.client.(*mockRPCClient); ok {
		return nil
	}

	// Close the old client connection if it exists
	if s.client != nil {
		if rpcClient, ok := s.client.(*rpcClient); ok {
			klog.Info("Closing existing RPC client connection")
			if err := rpcClient.Close(); err != nil {
				klog.Errorf("Error closing RPC client connection: %v", err)
			}
		}
	}

	// Create a new client for each RPC call
	client, err := NewRPCClient(GetSNAPRPCSocketPath())
	if err != nil {
		return err
	}

	// Save the new client in s.client
	s.client = client
	return nil
}

// GetPluginInfo RPC
func (s *StoragePluginServer) GetPluginInfo(ctx context.Context, req *pb.GetPluginInfoRequest) (*pb.GetPluginInfoResponse, error) {
	klog.Infof("Received GetPluginInfo request: %+v", req)
	pluginName := GetPluginName()
	resp := &pb.GetPluginInfoResponse{
		Name:          fmt.Sprintf("storage.dpu.%s.com", pluginName),
		VendorVersion: "1.0",
		Manifest: map[string]string{
			"description": fmt.Sprintf("%s SNAP Storage Plugin", pluginName),
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
					Type: pb.StoragePluginServiceCapability_RPC_TYPE_CREATE_DELETE_BLOCK_DEVICE,
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
	resp := &pb.GetSNAPProviderResponse{ProviderName: GetPluginName()}
	klog.Infof("Responding with GetSNAPProvider: %+v", resp)
	return resp, nil
}

// CreateDevice RPC
func (s *StoragePluginServer) CreateDevice(ctx context.Context, req *pb.CreateDeviceRequest) (*pb.CreateDeviceResponse, error) {
	klog.Infof("Received CreateDevice request: %+v", req)

	err := s.getRPCClient()
	if err != nil {
		errMsg := fmt.Sprintf("failed to create rpcClient: %v", err)
		klog.Error(errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}

	bdevs, err := s.client.BdevGetBdevs()
	if err != nil {
		return nil, fmt.Errorf("failed to get bdevs: %v", err)
	}

	attachRequest := BdevNvmeAttachControllerRequest{
		Trtype:  req.VolumeContext["targetType"],
		Traddr:  req.VolumeContext["targetAddr"],
		Adrfam:  "ipv4",
		Trsvcid: req.VolumeContext["targetPort"],
		Subnqn:  req.VolumeContext["nqn"],
	}

	klog.Infof("Checking if NVMe device already exists: %+v", attachRequest)
	deviceName, err := CheckBdevExistsByTrid(attachRequest, bdevs)
	if err != nil {
		errMsg := fmt.Sprintf("failed to check NVMe existence: %v", err)
		klog.Error(errMsg)
		return nil, status.Errorf(codes.FailedPrecondition, "%s", errMsg)
	}

	if deviceName != "" {
		klog.Infof("Device already exists: %s", deviceName)
		resp := &pb.CreateDeviceResponse{DeviceName: deviceName}
		klog.Infof("Responding with CreateDevice: %+v", resp)
		return resp, nil
	}

	attachRequest.Name = "nvme_" + req.GetVolumeId()
	klog.Infof("Attaching NVMe controller: %+v", attachRequest)
	respAttach, err := s.client.BdevNvmeAttachController(attachRequest)
	if err != nil {
		errMsg := fmt.Sprintf("failed to attach NVMe controller: %v", err)
		klog.Error(errMsg)
		return nil, status.Errorf(codes.FailedPrecondition, "%s", errMsg)
	}

	resp := &pb.CreateDeviceResponse{DeviceName: respAttach.BdevName}
	klog.Infof("Responding with CreateDevice: %+v", resp)
	return resp, nil
}

func (s *StoragePluginServer) DeleteDevice(ctx context.Context, req *pb.DeleteDeviceRequest) (*pb.DeleteDeviceResponse, error) {
	klog.Infof("Received DeleteDevice request: %+v", req)

	err := s.getRPCClient()
	if err != nil {
		errMsg := fmt.Sprintf("failed to create rpcClient: %v", err)
		klog.Error(errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}

	bdevs, err := s.client.BdevGetBdevs()
	if err != nil {
		return nil, fmt.Errorf("failed to get bdevs: %v", err)
	}

	controllers, err := s.client.BdevNvmeGetControllers()
	if err != nil {
		return nil, fmt.Errorf("failed to get NVMe controllers: %v", err)
	}

	// Check if the Bdev exists before proceeding
	bdevExists, err := CheckBdevExistsByBdev(req.DeviceName, bdevs)
	if err != nil {
		errMsg := fmt.Sprintf("Error checking bdev existence: %v", err)
		klog.Error(errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}

	if !bdevExists {
		klog.Infof("Bdev %s does not exist. Skipping deletion.", req.DeviceName)
		return &pb.DeleteDeviceResponse{}, nil
	}

	// Extract the trid for the given Bdev name
	targetTrid, err := getTridByBdev(req.DeviceName, bdevs)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to extract trid for bdev %s: %v", req.DeviceName, err)
		klog.Error(errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}
	klog.Infof("Found trid: %+v", targetTrid)

	// Find the controller name that matches the trid
	controllerName, err := getControllerByTrid(targetTrid, controllers)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to find controller for trid %+v: %v", targetTrid, err)
		klog.Error(errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}

	klog.Infof("Found controller name: %s", controllerName)

	// Detach NVMe controller using the correct controller name
	detachRequest := BdevNvmeDetachControllerRequest{
		Name: controllerName,
	}

	klog.Infof("Detaching NVMe controller: %+v", detachRequest)
	err = s.client.BdevNvmeDetachController(detachRequest)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to detach NVMe controller %s: %v", controllerName, err)
		klog.Error(errMsg)
		return nil, status.Errorf(codes.FailedPrecondition, "%s", errMsg)
	}

	resp := &pb.DeleteDeviceResponse{}
	klog.Infof("Successfully deleted device: %+v", controllerName)
	return resp, nil
}

func (s *StoragePluginServer) GetDevice(ctx context.Context, req *pb.GetDeviceRequest) (*pb.GetDeviceResponse, error) {
	klog.Infof("Received GetDevice request: %+v", req)

	err := s.getRPCClient()
	if err != nil {
		errMsg := fmt.Sprintf("failed to create rpcClient: %v", err)
		klog.Error(errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}

	// Retrieve all Bdevs
	bdevs, err := s.client.BdevGetBdevs()
	if err != nil {
		errMsg := fmt.Sprintf("Failed to get Bdevs: %v", err)
		klog.Error(errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}

	// Check if the requested device exists
	bdevExists, err := CheckBdevExistsByBdev(req.DeviceName, bdevs)
	if err != nil {
		errMsg := fmt.Sprintf("Error checking Bdev existence: %v", err)
		klog.Error(errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}

	if !bdevExists {
		klog.Infof("Bdev %s does not exist.", req.DeviceName)
		return nil, status.Errorf(codes.NotFound, "Device %s not found", req.DeviceName)
	}

	// Get trid for the requested device
	trid, err := getTridByBdev(req.DeviceName, bdevs)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to extract trid for Bdev %s: %v", req.DeviceName, err)
		klog.Error(errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}
	klog.Infof("Found trid: %+v", trid)

	// Prepare response with VolumeMode set to "Block" and VolumeContext set to trid
	response := &pb.GetDeviceResponse{
		VolumeMode: "Block",
		VolumeContext: map[string]string{
			"TrType":  trid.TrType,
			"AdrFam":  trid.AdrFam,
			"TrAddr":  trid.TrAddr,
			"TrSvcID": trid.TrSvcID,
			"SubNQN":  trid.SubNQN,
		},
	}

	klog.Infof("Returning GetDevice response: %+v", response)
	return response, nil
}

func (s *StoragePluginServer) ListDevices(ctx context.Context, req *pb.ListDevicesRequest) (*pb.ListDevicesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "ListDevices is not implemented")
}
