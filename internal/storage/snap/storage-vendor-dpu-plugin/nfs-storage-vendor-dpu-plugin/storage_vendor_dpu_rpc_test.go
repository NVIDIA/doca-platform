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
	"encoding/json"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockConn struct {
	readData  []byte
	writeData []byte
}

func TestParseRPCError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		expectType interface{}
	}{
		{
			name:       "no space left error",
			err:        fmt.Errorf("no space left on device"),
			expectType: &NoSpaceLeftError{},
		},
		{
			name:       "no such device error",
			err:        fmt.Errorf("no such device 'fs_test'"),
			expectType: &NoSuchDeviceError{},
		},
		{
			name:       "invalid parameters error",
			err:        fmt.Errorf("invalid parameters"),
			expectType: &InvalidParametersError{},
		},
		{
			name:       "unknown error",
			err:        fmt.Errorf("some other error"),
			expectType: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedErr := parseRPCError(tt.err)
			if tt.expectType == nil {
				assert.NotNil(t, parsedErr)
				return
			}
			assert.IsType(t, tt.expectType, parsedErr)
		})
	}
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if len(m.readData) == 0 {
		return 0, io.EOF
	}
	copy(b, m.readData)
	return len(m.readData), nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	m.writeData = make([]byte, len(b))
	copy(m.writeData, b)
	return len(b), nil
}

func (m *mockConn) Close() error {
	return nil
}

// Additional required methods for net.Conn interface
func (m *mockConn) LocalAddr() net.Addr {
	return &net.UnixAddr{Name: "mock", Net: "unix"}
}

func (m *mockConn) RemoteAddr() net.Addr {
	return &net.UnixAddr{Name: "mock", Net: "unix"}
}

func (m *mockConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func TestFsdevNfsCreate(t *testing.T) {
	tests := []struct {
		name             string
		deviceName       string
		volumePath       string
		nfsServer        string
		mockResp         string
		expectError      bool
		errorType        interface{}
		skipRequestCheck bool
	}{
		{
			name:        "successful creation",
			deviceName:  "test_fs1",
			volumePath:  "/export/test",
			nfsServer:   "1.1.1.1",
			mockResp:    `{"jsonrpc": "2.0", "id": 1, "result": "success"}`,
			expectError: false,
		},
		{
			name:             "empty device name",
			deviceName:       "",
			volumePath:       "/export/test",
			nfsServer:        "1.1.1.1",
			mockResp:         "",
			expectError:      true,
			skipRequestCheck: true,
		},
		{
			name:             "empty volume path",
			deviceName:       "test_fs1",
			volumePath:       "",
			nfsServer:        "1.1.1.1",
			mockResp:         "",
			expectError:      true,
			skipRequestCheck: true,
		},
		{
			name:             "empty nfs server",
			deviceName:       "test_fs1",
			volumePath:       "/export/test",
			nfsServer:        "",
			mockResp:         "",
			expectError:      true,
			skipRequestCheck: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConn := &mockConn{
				readData: []byte(tt.mockResp + "\n"),
			}

			client := &rpcClient{
				conn: mockConn,
			}

			createErr := client.FsdevNfsCreate(tt.deviceName, tt.volumePath, tt.nfsServer)

			if !tt.skipRequestCheck && len(mockConn.writeData) > 0 {
				var request map[string]interface{}
				unmarshalErr := json.Unmarshal(mockConn.writeData, &request)
				if !assert.NoError(t, unmarshalErr) {
					return
				}

				method, ok := request["method"].(string)
				if assert.True(t, ok, "method should be a string") {
					assert.Equal(t, "fsdev_nfs_create", method)
				}

				params, ok := request["params"].(map[string]interface{})
				if assert.True(t, ok, "params should be a map") {
					assert.Equal(t, tt.deviceName, params["name"])
					assert.Equal(t, tt.volumePath, params["exportname"])
					assert.Equal(t, tt.nfsServer, params["server"])
				}
			}

			if tt.expectError {
				assert.Error(t, createErr)
				if tt.errorType != nil {
					assert.IsType(t, tt.errorType, createErr)
				}
				return
			}
			assert.NoError(t, createErr)
		})
	}
}

func TestFsdevNfsDelete(t *testing.T) {
	tests := []struct {
		name        string
		deviceName  string
		mockResp    string
		expectError bool
		errorType   interface{}
	}{
		{
			name:        "successful deletion",
			deviceName:  "test_fs1",
			mockResp:    `{"jsonrpc": "2.0", "id": 1, "result": "success"}`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConn := &mockConn{
				readData: []byte(tt.mockResp + "\n"),
			}

			client := &rpcClient{
				conn: mockConn,
			}

			deleteErr := client.FsdevNfsDelete(tt.deviceName)

			var request map[string]interface{}
			unmarshalErr := json.Unmarshal(mockConn.writeData, &request)
			assert.NoError(t, unmarshalErr)
			assert.Equal(t, "fsdev_nfs_delete", request["method"])
			params := request["params"].(map[string]interface{})
			assert.Equal(t, tt.deviceName, params["name"])

			if tt.expectError {
				assert.Error(t, deleteErr)
				if tt.errorType != nil {
					assert.IsType(t, tt.errorType, deleteErr)
				}
				return
			}
			assert.NoError(t, deleteErr)
		})
	}
}

func TestFsdevGetFsdevs(t *testing.T) {
	tests := []struct {
		name         string
		mockResp     string
		expectError  bool
		expectFsdevs FsdevGetFsdevsResponse
	}{
		{
			name:         "successful response with devices",
			mockResp:     `{"jsonrpc":"2.0","id":1,"result":[{"name":"fs_test","module_name":"nfs","module_specific":{"name":"fs_test","root_path":"/export/test"}}]}`,
			expectError:  false,
			expectFsdevs: mockFsdevResponse,
		},
		{
			name:        "empty device list",
			mockResp:    `{"jsonrpc":"2.0","id":1,"result":[]}`,
			expectError: false,
			expectFsdevs: FsdevGetFsdevsResponse{
				Fsdevs: []Fsdev{},
			},
		},
		{
			name:        "malformed JSON response",
			mockResp:    `{"jsonrpc":"2.0","id":1,"result":"invalid"}`,
			expectError: true,
		},
		{
			name:        "error response",
			mockResp:    `{"jsonrpc":"2.0","id":1,"error":{"code":-1,"message":"internal error"}}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConn := &mockConn{
				readData: []byte(tt.mockResp + "\n"),
			}

			client := &rpcClient{
				conn: mockConn,
			}

			result, err := client.FsdevGetFsdevs()

			var request map[string]interface{}
			unmarshalErr := json.Unmarshal(mockConn.writeData, &request)
			if assert.NoError(t, unmarshalErr) {
				assert.Equal(t, "fsdev_get_fsdevs", request["method"])
				assert.Equal(t, "2.0", request["jsonrpc"])
				assert.NotNil(t, request["id"])
			}

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, len(tt.expectFsdevs.Fsdevs), len(result.Fsdevs))

			if len(result.Fsdevs) > 0 {
				assert.Equal(t, tt.expectFsdevs.Fsdevs[0].Name, result.Fsdevs[0].Name)
				assert.Equal(t, tt.expectFsdevs.Fsdevs[0].ModuleName, result.Fsdevs[0].ModuleName)
				assert.Equal(t, tt.expectFsdevs.Fsdevs[0].ModuleSpecific.RootPath,
					result.Fsdevs[0].ModuleSpecific.RootPath)
			}
		})
	}
}

func TestCheckFsdevExists(t *testing.T) {
	fsdevs := mockFsdevResponse.Fsdevs

	tests := []struct {
		name         string
		deviceName   string
		expectExists bool
	}{
		{
			name:         "existing device",
			deviceName:   "fs_test",
			expectExists: true,
		},
		{
			name:         "non-existing device",
			deviceName:   "fs_nonexistent",
			expectExists: false,
		},
		{
			name:         "empty device name",
			deviceName:   "",
			expectExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exists := CheckFsdevExists(tt.deviceName, fsdevs)
			assert.Equal(t, tt.expectExists, exists)
		})
	}
}
