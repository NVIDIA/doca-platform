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

package options

import (
	"testing"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	opts := New()
	assert.Equal(t, config.DefaultPluginName, opts.Name)
	assert.Equal(t, string(config.EmulationModeNVMe), opts.EmulationMode)
	assert.Equal(t, config.DefaultBindNetwork+"://"+config.DefaultBindAddress, opts.BindAddress)
	assert.Equal(t, config.DefaultSnapDeviceID, opts.SnapControllerDeviceID)
	assert.Equal(t, config.DefaultVirtiofsFSTypeName, opts.VirtiofsFSTypeName)
	assert.True(t, opts.NVMeLoadDriver)
	assert.True(t, opts.NVMeCreateVFs)
	assert.True(t, opts.VirtiofsLoadDriver)
	assert.NotNil(t, opts.LoggingOptions)
}

func TestAddFlags(t *testing.T) {
	opts := New()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.AddFlags(fs)
	// Check that some key flags were added
	assert.NotNil(t, fs.Lookup("name"))
	assert.NotNil(t, fs.Lookup("mode"))
	assert.NotNil(t, fs.Lookup("bind-address"))
	assert.NotNil(t, fs.Lookup("namespace"))
	assert.NotNil(t, fs.Lookup("node-id"))
	assert.Nil(t, fs.Lookup("node-root-fs"))
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name      string
		optName   string
		wantError bool
	}{
		{"valid name", "csi.snap.nvidia.com", false},
		{"empty name", "", true},
		{"invalid DNS name", "INVALID_NAME", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := New()
			opts.Name = tt.optName
			err := opts.validateName()
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateEmulationMode(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		wantError bool
	}{
		{"nvme mode", config.EmulationModeNVMe, false},
		{"virtiofs mode", config.EmulationModeVirtiofs, false},
		{"empty mode", "", true},
		{"invalid mode", "invalid", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := New()
			opts.EmulationMode = tt.mode
			err := opts.validateEmulationMode()
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidatePluginMode(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		wantError bool
	}{
		{"controller mode", config.PluginModeController, false},
		{"node mode", config.PluginModeNode, false},
		{"invalid mode", "invalid", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := New()
			opts.PluginMode = tt.mode

			err := opts.validatePluginMode()
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNodeFlags(t *testing.T) {
	tests := []struct {
		name                   string
		nodeID                 string
		maxVolumesPerNode      int64
		snapControllerDeviceID string
		virtiofsFSTypeName     string
		wantError              bool
	}{
		{
			name:                   "valid node flags",
			nodeID:                 "node1",
			maxVolumesPerNode:      10,
			snapControllerDeviceID: "6001",
			virtiofsFSTypeName:     "virtiofs",
			wantError:              false,
		},
		{
			name:                   "empty node id",
			nodeID:                 "",
			maxVolumesPerNode:      10,
			snapControllerDeviceID: "6001",
			virtiofsFSTypeName:     "virtiofs",
			wantError:              true,
		},
		{
			name:                   "negative max volumes",
			nodeID:                 "node1",
			maxVolumesPerNode:      -1,
			snapControllerDeviceID: "6001",
			virtiofsFSTypeName:     "virtiofs",
			wantError:              true,
		},
		{
			name:                   "empty snap controller device id",
			nodeID:                 "node1",
			maxVolumesPerNode:      10,
			snapControllerDeviceID: "",
			virtiofsFSTypeName:     "virtiofs",
			wantError:              true,
		},
		{
			name:                   "empty virtiofs driver name",
			nodeID:                 "node1",
			maxVolumesPerNode:      10,
			snapControllerDeviceID: "6001",
			virtiofsFSTypeName:     "",
			wantError:              true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := New()
			opts.NodeID = tt.nodeID
			opts.MaxVolumesPerNode = tt.maxVolumesPerNode
			opts.SnapControllerDeviceID = tt.snapControllerDeviceID
			opts.VirtiofsFSTypeName = tt.virtiofsFSTypeName
			err := opts.validateNodeFlags()
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateControllerFlags(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		wantError bool
	}{
		{"valid namespace", "test-namespace", false},
		{"empty namespace", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := New()
			opts.Namespace = tt.namespace
			err := opts.validateControllerFlags()
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseBindAddress(t *testing.T) {
	tests := []struct {
		name      string
		addr      string
		wantNet   string
		wantAddr  string
		wantError bool
	}{
		{
			name:      "valid tcp address",
			addr:      "tcp://127.0.0.1:9090",
			wantNet:   "tcp",
			wantAddr:  "127.0.0.1:9090",
			wantError: false,
		},
		{
			name:      "valid unix socket",
			addr:      "unix:///var/lib/csi.sock",
			wantNet:   "unix",
			wantAddr:  "/var/lib/csi.sock",
			wantError: false,
		},
		{
			name:      "unix socket with host",
			addr:      "unix://localhost/var/lib/csi.sock",
			wantNet:   "unix",
			wantAddr:  "localhost/var/lib/csi.sock",
			wantError: false,
		},
		{
			name:      "invalid scheme",
			addr:      "http://127.0.0.1:8080",
			wantNet:   "",
			wantAddr:  "",
			wantError: true,
		},
		{
			name:      "invalid url",
			addr:      "not-a-url",
			wantNet:   "",
			wantAddr:  "",
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			net, addr, err := ParseBindAddress(tt.addr)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantNet, net)
				assert.Equal(t, tt.wantAddr, addr)
			}
		})
	}
}

func TestValidateBindAddress(t *testing.T) {
	tests := []struct {
		name      string
		addr      string
		wantError bool
	}{
		{"valid tcp address", "tcp://127.0.0.1:9090", false},
		{"valid unix socket", "unix:///var/lib/csi.sock", false},
		{"invalid address", "invalid-address", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := New()
			opts.BindAddress = tt.addr
			err := opts.validateBindAddress()
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
