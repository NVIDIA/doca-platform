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
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"k8s.io/klog/v2"
)

// Error types for specific JSON-RPC errors
// Each error type is distinct for clear error handling with errors.As

// NoSpaceLeftError represents a "no space left" error from the JSON-RPC service
type NoSpaceLeftError struct {
	Cause error
}

func (e *NoSpaceLeftError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("no space left: %v", e.Cause)
	}
	return "no space left"
}

func (e *NoSpaceLeftError) Unwrap() error {
	return e.Cause
}

// NoSuchDeviceError represents a "no such device" error from the JSON-RPC service
type NoSuchDeviceError struct {
	DeviceName string
	Cause      error
}

func (e *NoSuchDeviceError) Error() string {
	if e.DeviceName != "" {
		if e.Cause != nil {
			return fmt.Sprintf("no such device '%s': %v", e.DeviceName, e.Cause)
		}
		return fmt.Sprintf("no such device '%s'", e.DeviceName)
	}
	if e.Cause != nil {
		return fmt.Sprintf("no such device: %v", e.Cause)
	}
	return "no such device"
}

func (e *NoSuchDeviceError) Unwrap() error {
	return e.Cause
}

// InvalidParametersError represents an "invalid parameters" error from the JSON-RPC service
type InvalidParametersError struct {
	Cause error
}

func (e *InvalidParametersError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("invalid parameters: %v", e.Cause)
	}
	return "invalid parameters"
}

func (e *InvalidParametersError) Unwrap() error {
	return e.Cause
}

// RPCClient defines the interface for RPC operations
type RPCClient interface {
	// FsdevGetFsdevs retrieves a list of all filesystem devices
	FsdevGetFsdevs() (FsdevGetFsdevsResponse, error)
	// FsdevNfsCreate creates an NFS filesystem device
	FsdevNfsCreate(name string, volumePath string, nfsServer string) error
	// FsdevNfsDelete deletes an NFS filesystem device
	FsdevNfsDelete(name string) error
	// Close closes the connection
	Close() error
}

// rpcClient handles JSON-RPC over a Unix domain socket
type rpcClient struct {
	conn  net.Conn
	rpcID int32
}

// Close closes the connection of the rpcClient
func (c *rpcClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// NewRPCClient creates a new rpcClient that communicates over a Unix domain socket
var NewRPCClient = func(socketPath string) (*rpcClient, error) {
	c, err := net.DialTimeout("unix", socketPath, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to unix socket %s: %w", socketPath, err)
	}
	return &rpcClient{conn: c}, nil
}

// Call executes a JSON-RPC call with given method and params
func (c *rpcClient) Call(method string, params interface{}) (interface{}, error) {
	type rpcRequest struct {
		Ver    string      `json:"jsonrpc"`
		ID     int32       `json:"id"`
		Method string      `json:"method"`
		Params interface{} `json:"params,omitempty"`
	}

	id := atomic.AddInt32(&c.rpcID, 1)
	req := rpcRequest{
		Ver:    "2.0",
		ID:     id,
		Method: method,
		Params: params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}

	klog.Infof("Sending RPC (method=%s, id=%d): %s", method, id, string(data))

	if _, err := c.conn.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("%s: write request failed: %w", method, err)
	}

	reader := bufio.NewReader(c.conn)
	respLine, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("%s: read response failed: %w", method, err)
	}

	klog.Infof("Received response (method=%s, id=%d): %s", method, id, string(respLine))

	response := struct {
		ID    int32 `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result json.RawMessage `json:"result"`
	}{}

	if err := json.Unmarshal(respLine, &response); err != nil {
		return nil, fmt.Errorf("%s: decode response failed: %w", method, err)
	}

	if response.ID != id {
		return nil, fmt.Errorf("%s: response ID mismatch (got %d, expected %d)", method, response.ID, id)
	}

	if response.Error.Code != 0 {
		err := fmt.Errorf("%s: json response error: %s", method, response.Error.Message)
		return nil, parseRPCError(err)
	}

	return &response.Result, nil
}

// parseRPCError attempts to parse the error message and return a specific error type
func parseRPCError(err error) error {
	if err == nil {
		return nil
	}

	errMsg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(errMsg, "no space left"):
		return &NoSpaceLeftError{Cause: err}
	case strings.Contains(errMsg, "no such device"):
		// Try to extract device name if present in the error message
		var deviceName string
		parts := strings.Split(errMsg, "no such device")
		if len(parts) > 1 && len(parts[1]) > 0 {
			// Try to extract the device name if in quotes or after a colon
			if idx := strings.Index(parts[1], "'"); idx >= 0 {
				end := strings.Index(parts[1][idx+1:], "'")
				if end >= 0 {
					deviceName = parts[1][idx+1 : idx+1+end]
				}
			} else if idx := strings.Index(parts[1], ":"); idx >= 0 && len(parts[1]) > idx+1 {
				deviceName = strings.TrimSpace(parts[1][idx+1:])
			}
		}
		return &NoSuchDeviceError{DeviceName: deviceName, Cause: err}
	case strings.Contains(errMsg, "invalid parameters"):
		return &InvalidParametersError{Cause: err}
	default:
		return fmt.Errorf("unknown error: %w", err)
	}
}

// FsdevNfsCreate creates an NFS filesystem with the given parameters
func (c *rpcClient) FsdevNfsCreate(name string, volumePath string, nfsServer string) error {
	if name == "" || volumePath == "" || nfsServer == "" {
		return fmt.Errorf("name, volumePath and nfsServer are required")
	}

	createRequest := map[string]interface{}{
		"name":       name,
		"server":     nfsServer,
		"exportname": volumePath,
	}

	klog.Infof("Creating NFS filesystem with request: %+v", createRequest)
	_, err := c.Call("fsdev_nfs_create", createRequest)
	if err != nil {
		return fmt.Errorf("failed to create NFS filesystem: %w", err)
	}
	klog.Infof("Successfully created NFS filesystem: %s", name)
	return nil
}

// FsdevNfsDelete deletes an NFS filesystem
func (c *rpcClient) FsdevNfsDelete(name string) error {
	params := map[string]interface{}{
		"name": name,
	}

	klog.Infof("Deleting NFS filesystem: %+v", name)
	_, err := c.Call("fsdev_nfs_delete", params)
	if err != nil {
		return fmt.Errorf("failed to delete NFS filesystem: %w", err)
	}
	klog.Infof("Successfully deleted NFS filesystem: %s", name)
	return nil
}

// FsdevModuleSpecific represents the module-specific configuration for a filesystem device
type FsdevModuleSpecific struct {
	Name     string `json:"name"`
	RootPath string `json:"root_path"`
}

// Fsdev represents a filesystem device
type Fsdev struct {
	Name           string              `json:"name"`
	ModuleName     string              `json:"module_name"`
	ModuleSpecific FsdevModuleSpecific `json:"module_specific"`
}

// FsdevGetFsdevsResponse contains a list of filesystem devices.
type FsdevGetFsdevsResponse struct {
	Fsdevs []Fsdev `json:"fsdevs"`
}

// FsdevGetFsdevs retrieves a list of all filesystem devices (fsdevs)
func (c *rpcClient) FsdevGetFsdevs() (FsdevGetFsdevsResponse, error) {
	klog.Info("Calling FsdevGetFsdevs RPC")
	result, err := c.Call("fsdev_get_fsdevs", nil)
	if err != nil {
		return FsdevGetFsdevsResponse{}, fmt.Errorf("failed to get fsdevs: %w", err)
	}

	raw, ok := result.(*json.RawMessage)
	if !ok {
		return FsdevGetFsdevsResponse{}, fmt.Errorf("unexpected result type")
	}

	var fsdevs []Fsdev
	if err := json.Unmarshal(*raw, &fsdevs); err != nil {
		klog.Errorf("Failed to unmarshal response: %v", err)
		return FsdevGetFsdevsResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return FsdevGetFsdevsResponse{Fsdevs: fsdevs}, nil
}

// CheckFsdevExists checks if a filesystem device exists
func CheckFsdevExists(name string, fsdevs []Fsdev) bool {
	for _, fsdev := range fsdevs {
		if fsdev.Name == name {
			klog.V(4).Infof("Found fsdev: %s", name)
			return true
		}
	}
	klog.V(4).Infof("Fsdev not found: %s", name)
	return false
}
