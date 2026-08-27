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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
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
	// BdevGetBdevs retrieves a list of all block devices
	BdevGetBdevs() (BdevGetBdevsResponse, error)
	// BdevNvmeGetControllers retrieves the list of NVMe controllers
	BdevNvmeGetControllers() (BdevNvmeGetControllersResponse, error)
	// BdevNvmeAttachController attaches an NVMe controller with the given parameters
	BdevNvmeAttachController(BdevNvmeAttachControllerRequest) (BdevNvmeAttachControllerResponse, error)
	// BdevNvmeDetachController detaches an NVMe controller
	BdevNvmeDetachController(BdevNvmeDetachControllerRequest) error
	// Close closes the connection
	Close() error
}

// rpcClient handles JSON-RPC over a Unix domain socket.
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

// NewRPCClient is a variable function that creates an rpcClient that communicates over a Unix domain socket directly
// It can be overridden in tests to provide mock implementations
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

	// Write request line
	if _, err := c.conn.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("%s: write request failed: %w", method, err)
	}

	// Read response line
	reader := bufio.NewReader(c.conn)
	respLine, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("%s: read response failed: %w", method, err)
	}

	klog.Infof("Received response (method=%s, id=%d): %s", method, id, string(respLine))

	// Decode response
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
		// Create a proper error with specific error type
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

// Bdev represents a block device.
type Bdev struct {
	Name           string                 `json:"name"`
	DriverSpecific map[string]interface{} `json:"driver_specific"`
}

// BdevGetBdevsResponse contains a list of block devices.
type BdevGetBdevsResponse struct {
	Bdevs []Bdev `json:"bdevs"`
}

// BdevGetBdevs retrieves a list of all block devices (bdevs)
func (c *rpcClient) BdevGetBdevs() (BdevGetBdevsResponse, error) {
	klog.Info("Calling BdevGetBdevs RPC")
	result, err := c.Call("bdev_get_bdevs", nil)
	if err != nil {
		return BdevGetBdevsResponse{}, err
	}

	raw, ok := result.(*json.RawMessage)
	if !ok {
		return BdevGetBdevsResponse{}, fmt.Errorf("unexpected result type")
	}

	// Unmarshal directly into a slice of Bdev
	var bdevs []Bdev
	if err := json.Unmarshal(*raw, &bdevs); err != nil {
		klog.Errorf("Failed to unmarshal response: %v", err)
		return BdevGetBdevsResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return BdevGetBdevsResponse{Bdevs: bdevs}, nil
}

type NVMeTrid struct {
	TrType  string `json:"trtype"`
	AdrFam  string `json:"adrfam"`
	TrAddr  string `json:"traddr"`
	TrSvcID string `json:"trsvcid"`
	SubNQN  string `json:"subnqn"`
}

type NvmeController struct {
	Name   string `json:"name"`
	Ctrlrs []struct {
		Trid NVMeTrid `json:"trid"`
	} `json:"ctrlrs"`
}

type BdevNvmeGetControllersResponse struct {
	Controllers []NvmeController `json:"controllers"`
}

// BdevNvmeGetControllers retrieves the list of NVMe controllers
func (c *rpcClient) BdevNvmeGetControllers() (BdevNvmeGetControllersResponse, error) {
	klog.Info("Calling BdevNvmeGetControllers RPC")

	result, err := c.Call("bdev_nvme_get_controllers", nil)
	if err != nil {
		return BdevNvmeGetControllersResponse{}, err
	}

	raw, ok := result.(*json.RawMessage)
	if !ok {
		return BdevNvmeGetControllersResponse{}, fmt.Errorf("unexpected result type")
	}

	var controllers []NvmeController
	if err := json.Unmarshal(*raw, &controllers); err != nil {
		klog.Errorf("Failed to unmarshal response: %v", err)
		return BdevNvmeGetControllersResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return BdevNvmeGetControllersResponse{Controllers: controllers}, nil
}

type BdevNvmeAttachControllerRequest struct {
	Name    string `json:"name"`
	Trtype  string `json:"trtype"`
	Traddr  string `json:"traddr"`
	Adrfam  string `json:"adrfam,omitempty"`
	Trsvcid string `json:"trsvcid,omitempty"`
	Subnqn  string `json:"subnqn,omitempty"`
}

type BdevNvmeAttachControllerResponse struct {
	BdevName string `json:"bdev_name"`
}

// equalTransportAddress normalizes IP addresses because equivalent IPv6 addresses
// can have different string representations. Non-IP addresses use the existing comparison.
func equalTransportAddress(first, second string) bool {
	firstAddr, firstErr := netip.ParseAddr(first)
	secondAddr, secondErr := netip.ParseAddr(second)
	if firstErr == nil && secondErr == nil {
		return firstAddr == secondAddr
	}
	return strings.EqualFold(first, second)
}

// CheckBdevExistsByTrid checks if an NVMe controller with the given parameters already exists.
func CheckBdevExistsByTrid(req BdevNvmeAttachControllerRequest, bdevResponse BdevGetBdevsResponse) (string, error) {
	klog.Infof("Checking if NVMe controller exists: %+v", req)

	for _, bdev := range bdevResponse.Bdevs {
		nvmeData, ok := bdev.DriverSpecific["nvme"]
		if !ok {
			continue
		}

		nvmeList, ok := nvmeData.([]interface{})
		if !ok {
			continue
		}

		for _, nvme := range nvmeList {
			nvmeMap, ok := nvme.(map[string]interface{})
			if !ok {
				continue
			}

			trid, ok := nvmeMap["trid"].(map[string]interface{})
			if !ok {
				continue
			}

			if strings.EqualFold(fmt.Sprint(trid["trtype"]), req.Trtype) &&
				strings.EqualFold(fmt.Sprint(trid["adrfam"]), req.Adrfam) &&
				equalTransportAddress(fmt.Sprint(trid["traddr"]), req.Traddr) &&
				strings.EqualFold(fmt.Sprint(trid["trsvcid"]), req.Trsvcid) &&
				strings.EqualFold(fmt.Sprint(trid["subnqn"]), req.Subnqn) {
				klog.Infof("Found existing NVMe controller: %s", bdev.Name)
				return bdev.Name, nil
			}
		}
	}

	klog.Info("No matching NVMe controller found.")
	return "", nil
}

// CheckBdevExistsByBdev verifies if a block device (bdev) with the given device name exists.
func CheckBdevExistsByBdev(deviceName string, bdevResponse BdevGetBdevsResponse) (bool, error) {
	for _, bdev := range bdevResponse.Bdevs {
		if bdev.Name == deviceName {
			klog.Infof("Bdev found: %s", deviceName)
			return true, nil
		}
	}

	klog.Infof("Bdev not found: %s", deviceName)
	return false, nil
}

func (c *rpcClient) BdevNvmeAttachController(req BdevNvmeAttachControllerRequest) (BdevNvmeAttachControllerResponse, error) {
	klog.Infof("Attaching NVMe controller with request: %+v", req)
	result, err := c.Call("bdev_nvme_attach_controller", req)
	if err != nil {
		// Check for specific error types
		var noSpaceErr *NoSpaceLeftError
		if errors.As(err, &noSpaceErr) {
			klog.Errorf("No space left when attaching NVMe controller: %v", noSpaceErr)
			return BdevNvmeAttachControllerResponse{}, noSpaceErr
		}

		var noSuchDeviceErr *NoSuchDeviceError
		if errors.As(err, &noSuchDeviceErr) {
			klog.Errorf("Device not found when attaching NVMe controller: %v", noSuchDeviceErr)
			return BdevNvmeAttachControllerResponse{}, fmt.Errorf("failed to attach NVMe controller (device not found): %w", noSuchDeviceErr)
		}

		var invalidParamsErr *InvalidParametersError
		if errors.As(err, &invalidParamsErr) {
			klog.Errorf("Invalid parameters when attaching NVMe controller: %v", invalidParamsErr)
			return BdevNvmeAttachControllerResponse{}, fmt.Errorf("failed to attach NVMe controller (invalid parameters): %w", invalidParamsErr)
		}

		// Generic error handling
		return BdevNvmeAttachControllerResponse{}, fmt.Errorf("failed to attach NVMe controller: %w", err)
	}

	raw, ok := result.(*json.RawMessage)
	if !ok {
		return BdevNvmeAttachControllerResponse{}, fmt.Errorf("unexpected result type")
	}

	// Handle response type variants
	var v interface{}
	if err := json.Unmarshal(*raw, &v); err != nil {
		return BdevNvmeAttachControllerResponse{}, fmt.Errorf("unmarshal result: %w", err)
	}

	var response BdevNvmeAttachControllerResponse
	switch val := v.(type) {
	case string:
		response.BdevName = val
	case []interface{}:
		if len(val) > 0 {
			name, ok := val[0].(string)
			if !ok {
				return BdevNvmeAttachControllerResponse{}, fmt.Errorf("unexpected item type in array")
			}
			response.BdevName = name
		} else {
			return BdevNvmeAttachControllerResponse{}, fmt.Errorf("response array is empty")
		}
	default:
		return BdevNvmeAttachControllerResponse{}, fmt.Errorf("unexpected response type: %T", v)
	}

	klog.Infof("NVMe controller attached, BdevName: %s", response.BdevName)
	return response, nil
}

type BdevNvmeDetachControllerRequest struct {
	Name string `json:"name"`
}

// getTridByBdev extracts the NVMeTrid from a given bdev name
func getTridByBdev(bdevName string, bdevResponse BdevGetBdevsResponse) (NVMeTrid, error) {
	for _, bdev := range bdevResponse.Bdevs {
		if bdev.Name != bdevName {
			continue
		}

		nvmeInterfaces, ok := bdev.DriverSpecific["nvme"].([]interface{})
		if !ok || len(nvmeInterfaces) == 0 {
			continue
		}

		nvmeMap, ok := nvmeInterfaces[0].(map[string]interface{})
		if !ok {
			continue
		}

		tridData, exists := nvmeMap["trid"]
		if !exists {
			continue
		}

		tridBytes, err := json.Marshal(tridData)
		if err != nil {
			klog.Errorf("Failed to marshal trid: %v", err)
			return NVMeTrid{}, fmt.Errorf("failed to marshal trid: %v", err)
		}

		var targetTrid NVMeTrid
		err = json.Unmarshal(tridBytes, &targetTrid)
		if err != nil {
			klog.Errorf("Failed to unmarshal trid into NVMeTrid: %v", err)
			return NVMeTrid{}, fmt.Errorf("failed to unmarshal trid into NVMeTrid: %v", err)
		}

		return targetTrid, nil
	}

	return NVMeTrid{}, fmt.Errorf("could not find trid for bdev name: %s", bdevName)
}

// getControllerByTrid extracts the NVMeTrid from a given bdev name
func getControllerByTrid(targetTrid NVMeTrid, controllersResponse BdevNvmeGetControllersResponse) (string, error) {
	for _, controller := range controllersResponse.Controllers {
		for _, ctrlr := range controller.Ctrlrs {
			if ctrlr.Trid == targetTrid {
				return controller.Name, nil
			}
		}
	}

	return "", fmt.Errorf("no matching controller found for trid: %+v", targetTrid)
}

func (c *rpcClient) BdevNvmeDetachController(req BdevNvmeDetachControllerRequest) error {
	klog.Infof("Detaching NVMe controller: %+v", req)

	_, err := c.Call("bdev_nvme_detach_controller", req)
	if err != nil {
		var noSuchDeviceErr *NoSuchDeviceError
		if errors.As(err, &noSuchDeviceErr) {
			// Set the device name in the error if it's not already set
			if noSuchDeviceErr.DeviceName == "" {
				noSuchDeviceErr.DeviceName = req.Name
			}
			klog.Warningf("NVMe controller %s not found: %v", req.Name, noSuchDeviceErr)
			return noSuchDeviceErr
		}

		// Check for other specific error types if needed
		var noSpaceErr *NoSpaceLeftError
		if errors.As(err, &noSpaceErr) {
			klog.Errorf("No space left when detaching NVMe controller %s: %v", req.Name, noSpaceErr)
			return fmt.Errorf("failed to detach NVMe controller %s (no space left): %w", req.Name, noSpaceErr)
		}

		var invalidParamsErr *InvalidParametersError
		if errors.As(err, &invalidParamsErr) {
			klog.Errorf("Invalid parameters when detaching NVMe controller %s: %v", req.Name, invalidParamsErr)
			return fmt.Errorf("failed to detach NVMe controller %s (invalid parameters): %w", req.Name, invalidParamsErr)
		}

		// Generic error handling
		klog.Errorf("Error detaching NVMe controller %s: %v", req.Name, err)
		return fmt.Errorf("failed to detach NVMe controller %s: %w", req.Name, err)
	}

	klog.Infof("Successfully detached NVMe controller: %s", req.Name)
	return nil
}
