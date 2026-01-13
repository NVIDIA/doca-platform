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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckBdevExistsByTrid(t *testing.T) {
	tests := []struct {
		name         string
		req          BdevNvmeAttachControllerRequest
		expectedBdev string
		expectError  bool
	}{
		{
			name: "Valid Trid - Should return existing Bdev",
			req: BdevNvmeAttachControllerRequest{
				Trtype:  "RDMA",
				Adrfam:  "IPv4",
				Traddr:  "1.1.1.1",
				Trsvcid: "4420",
				Subnqn:  "nqn.2016-06.io.nvmet:swx-mtvr-stor02",
			},
			expectedBdev: "nvme_pv-namen1",
			expectError:  false,
		},
		{
			name: "Non-existent Trid - Should return empty string",
			req: BdevNvmeAttachControllerRequest{
				Trtype:  "RDMA",
				Adrfam:  "IPv4",
				Traddr:  "192.168.10.99",
				Trsvcid: "4420",
				Subnqn:  "nqn.2016-06.io.nvmet:invalid",
			},
			expectedBdev: "",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bdevName, err := CheckBdevExistsByTrid(tt.req, mockBdevResponse)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedBdev, bdevName)
			}
		})
	}
}

func TestCheckBdevExistsByBdev(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		expectExist bool
	}{
		{
			name:        "Bdev Exists",
			deviceName:  "nvme_pv-namen1",
			expectExist: true,
		},
		{
			name:        "Bdev Does Not Exist",
			deviceName:  "non_existent_bdev",
			expectExist: false,
		},
		{
			name:        "Empty device name",
			deviceName:  "",
			expectExist: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exists, err := CheckBdevExistsByBdev(tt.deviceName, mockBdevResponse)
			require.NoError(t, err)
			require.Equal(t, tt.expectExist, exists)
		})
	}
}

func TestGetTridByBdev(t *testing.T) {
	tests := []struct {
		name         string
		bdevName     string
		expectedTrid NVMeTrid
		expectError  bool
	}{
		{
			name:     "Valid Bdev",
			bdevName: "nvme_pv-namen1",
			expectedTrid: NVMeTrid{
				TrType:  "RDMA",
				AdrFam:  "IPv4",
				TrAddr:  "1.1.1.1",
				TrSvcID: "4420",
				SubNQN:  "nqn.2016-06.io.nvmet:swx-mtvr-stor02",
			},
			expectError: false,
		},
		{
			name:         "Bdev Not Found",
			bdevName:     "non_existent_bdev",
			expectedTrid: NVMeTrid{},
			expectError:  true,
		},
		{
			name:         "Empty bdev name",
			bdevName:     "",
			expectedTrid: NVMeTrid{},
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trid, err := getTridByBdev(tt.bdevName, mockBdevResponse)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedTrid, trid)
			}
		})
	}
}

func TestGetControllerByTrid(t *testing.T) {
	tests := []struct {
		name         string
		targetTrid   NVMeTrid
		expectedName string
		expectError  bool
	}{
		{
			name: "Valid Trid - Should return controller name",
			targetTrid: NVMeTrid{
				TrType:  "RDMA",
				AdrFam:  "IPv4",
				TrAddr:  "1.1.1.1",
				TrSvcID: "4420",
				SubNQN:  "nqn.2016-06.io.nvmet:swx-mtvr-stor02",
			},
			expectedName: "nvme_pv-name",
			expectError:  false,
		},
		{
			name: "Invalid Trid - Should return error",
			targetTrid: NVMeTrid{
				TrType:  "RDMA",
				AdrFam:  "IPv4",
				TrAddr:  "192.168.10.99",
				TrSvcID: "4420",
				SubNQN:  "nqn.2016-06.io.nvmet:invalid",
			},
			expectedName: "",
			expectError:  true,
		},
		{
			name:         "Empty Trid - Should return error",
			targetTrid:   NVMeTrid{},
			expectedName: "",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controllerName, err := getControllerByTrid(tt.targetTrid, mockControllersResponse)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedName, controllerName)
			}
		})
	}
}

// Test for the custom error types and error handling improvements
func TestCustomErrorTypes(t *testing.T) {
	// Create instances of our custom error types
	originalErr := fmt.Errorf("original error")
	spaceErr := &NoSpaceLeftError{Cause: originalErr}
	deviceErr := &NoSuchDeviceError{DeviceName: "test-device", Cause: originalErr}
	paramsErr := &InvalidParametersError{Cause: originalErr}

	// Test error messages
	t.Run("Error messages are formatted correctly", func(t *testing.T) {
		require.Equal(t, "no space left: original error", spaceErr.Error())
		require.Equal(t, "no such device 'test-device': original error", deviceErr.Error())
		require.Equal(t, "invalid parameters: original error", paramsErr.Error())
	})

	// Test errors.As functionality
	t.Run("errors.As identifies NoSpaceLeftError", func(t *testing.T) {
		wrappedErr := fmt.Errorf("wrapped: %w", spaceErr)

		var noSpaceErr *NoSpaceLeftError
		require.True(t, errors.As(wrappedErr, &noSpaceErr))
		require.Equal(t, spaceErr, noSpaceErr)

		var noSuchDeviceErr *NoSuchDeviceError
		require.False(t, errors.As(wrappedErr, &noSuchDeviceErr))

		var invalidParamsErr *InvalidParametersError
		require.False(t, errors.As(wrappedErr, &invalidParamsErr))
	})

	t.Run("errors.As identifies NoSuchDeviceError", func(t *testing.T) {
		wrappedErr := fmt.Errorf("wrapped: %w", deviceErr)

		var noSpaceErr *NoSpaceLeftError
		require.False(t, errors.As(wrappedErr, &noSpaceErr))

		var noSuchDeviceErr *NoSuchDeviceError
		require.True(t, errors.As(wrappedErr, &noSuchDeviceErr))
		require.Equal(t, deviceErr, noSuchDeviceErr)
		require.Equal(t, "test-device", noSuchDeviceErr.DeviceName)

		var invalidParamsErr *InvalidParametersError
		require.False(t, errors.As(wrappedErr, &invalidParamsErr))
	})

	t.Run("errors.As identifies InvalidParametersError", func(t *testing.T) {
		wrappedErr := fmt.Errorf("wrapped: %w", paramsErr)

		var noSpaceErr *NoSpaceLeftError
		require.False(t, errors.As(wrappedErr, &noSpaceErr))

		var noSuchDeviceErr *NoSuchDeviceError
		require.False(t, errors.As(wrappedErr, &noSuchDeviceErr))

		var invalidParamsErr *InvalidParametersError
		require.True(t, errors.As(wrappedErr, &invalidParamsErr))
		require.Equal(t, paramsErr, invalidParamsErr)
	})

	// Test error unwrapping
	t.Run("Unwrap returns original error", func(t *testing.T) {
		require.Equal(t, originalErr, errors.Unwrap(spaceErr))
		require.Equal(t, originalErr, errors.Unwrap(deviceErr))
		require.Equal(t, originalErr, errors.Unwrap(paramsErr))
	})
}

// Test parseRPCError function
func TestParseRPCError(t *testing.T) {
	tests := []struct {
		name         string
		inputErr     error
		expectedType interface{}
		checkDevName bool
		expectedName string
	}{
		{
			name:         "No space left error",
			inputErr:     fmt.Errorf("json response error: no space left on device"),
			expectedType: &NoSpaceLeftError{},
			checkDevName: false,
		},
		{
			name:         "No such device error",
			inputErr:     fmt.Errorf("json response error: no such device"),
			expectedType: &NoSuchDeviceError{},
			checkDevName: false,
		},
		{
			name:         "No such device with name in quotes",
			inputErr:     fmt.Errorf("json response error: no such device 'nvme0'"),
			expectedType: &NoSuchDeviceError{},
			checkDevName: true,
			expectedName: "nvme0",
		},
		{
			name:         "No such device with name after colon",
			inputErr:     fmt.Errorf("json response error: no such device: nvme1"),
			expectedType: &NoSuchDeviceError{},
			checkDevName: true,
			expectedName: "nvme1",
		},
		{
			name:         "Invalid parameters error",
			inputErr:     fmt.Errorf("json response error: invalid parameters"),
			expectedType: &InvalidParametersError{},
			checkDevName: false,
		},
		{
			name:         "Nil error",
			inputErr:     nil,
			expectedType: nil,
			checkDevName: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedErr := parseRPCError(tt.inputErr)

			if tt.expectedType == nil {
				if tt.inputErr == nil {
					require.Nil(t, parsedErr)
				} else {
					// For generic errors, we expect the original error back
					require.Equal(t, tt.inputErr, parsedErr)
				}
				return
			}

			switch expected := tt.expectedType.(type) {
			case *NoSpaceLeftError:
				var spaceErr *NoSpaceLeftError
				require.True(t, errors.As(parsedErr, &spaceErr))
				require.Equal(t, tt.inputErr, errors.Unwrap(parsedErr))
			case *NoSuchDeviceError:
				var deviceErr *NoSuchDeviceError
				require.True(t, errors.As(parsedErr, &deviceErr))
				require.Equal(t, tt.inputErr, errors.Unwrap(parsedErr))
				if tt.checkDevName {
					require.Equal(t, tt.expectedName, deviceErr.DeviceName)
				}
			case *InvalidParametersError:
				var paramsErr *InvalidParametersError
				require.True(t, errors.As(parsedErr, &paramsErr))
				require.Equal(t, tt.inputErr, errors.Unwrap(parsedErr))
			default:
				t.Fatalf("unexpected type: %T", expected)
			}
		})
	}
}
