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

package util

import (
	"bytes"
	"fmt"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPCIAddress = "c9:00.0"
	stateModePath  = "nvidia/mode/state/mode"
	configModePath = "nvidia/mode/config/mode"
)

func dmsModeFixture(path, mode string) string {
	return fmt.Sprintf(`[
  {
    "source": "127.0.0.1:9339",
    "timestamp": 1761796906478936518,
    "time": "2025-10-30T04:01:46.478936518Z",
    "target": "c9:00.0",
    "updates": [
      {
        "Path": %q,
        "values": {
          %q: %q
        }
      }
    ]
  }
]`, path, path, mode)
}

func runBashWithOutput(output string) func(string) (bytes.Buffer, bytes.Buffer, error) {
	return func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
		var stdout bytes.Buffer
		stdout.WriteString(output)
		return stdout, bytes.Buffer{}, nil
	}
}

// TestGetDPUMode_CommandVerification verifies the dmsc get command targets state mode only.
func TestGetDPUMode_CommandVerification(t *testing.T) {
	var capturedCmd string
	runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
		capturedCmd = cmd
		return runBashWithOutput(dmsModeFixture(stateModePath, "DPU"))(cmd)
	}

	mode, err := getDPUMode(testPCIAddress, runBash)
	require.NoError(t, err)
	assert.Equal(t, provisioningv1.DpuMode, mode)
	assert.Contains(t, capturedCmd, "--path /nvidia/mode/state/mode")
	assert.NotContains(t, capturedCmd, "/nvidia/mode/config/mode")
	assert.Contains(t, capturedCmd, testPCIAddress)
}

// TestGetDPUMode_Parsing tests parsing verified DMS state mode responses.
func TestGetDPUMode_Parsing(t *testing.T) {
	tests := []struct {
		name         string
		rawMode      string
		expectedMode provisioningv1.DpuModeType
	}{
		{name: "DPU mode uppercase", rawMode: "DPU", expectedMode: provisioningv1.DpuMode},
		{name: "DPU mode lowercase", rawMode: "dpu", expectedMode: provisioningv1.DpuMode},
		{name: "NIC mode uppercase", rawMode: "NIC", expectedMode: provisioningv1.NicMode},
		{name: "NIC mode mixed case", rawMode: "Nic", expectedMode: provisioningv1.NicMode},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := getDPUMode(testPCIAddress, runBashWithOutput(dmsModeFixture(stateModePath, tt.rawMode)))
			require.NoError(t, err)
			assert.Equal(t, tt.expectedMode, mode)
		})
	}
}

// TestGetDPUMode_ErrorCases tests strict rejection and error handling.
func TestGetDPUMode_ErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		mockOutput    string
		expectedError string
		errorValue    string
	}{
		{
			name:          "config-only output rejected",
			mockOutput:    dmsModeFixture(configModePath, "DPU"),
			expectedError: "failed to parse DPU mode",
		},
		{
			name:          "missing state mode field",
			mockOutput:    `[{"updates":[{"Path":"nvidia/mode/state/mode","values":{}}]}]`,
			expectedError: "failed to parse DPU mode",
		},
		{
			name:          "malformed output",
			mockOutput:    "not json",
			expectedError: "failed to parse DPU mode",
		},
		{
			name:          "zero-trust mode rejected",
			mockOutput:    dmsModeFixture(stateModePath, "zero-trust"),
			expectedError: "unsupported DPU mode",
			errorValue:    "zero-trust",
		},
		{
			name:          "unknown mode rejected",
			mockOutput:    dmsModeFixture(stateModePath, "hypervisor"),
			expectedError: "unsupported DPU mode",
			errorValue:    "hypervisor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := getDPUMode(testPCIAddress, runBashWithOutput(tt.mockOutput))
			assert.Error(t, err)
			assert.Empty(t, mode)
			assert.Contains(t, err.Error(), tt.expectedError)
			if tt.errorValue != "" {
				assert.Contains(t, err.Error(), tt.errorValue)
			}
		})
	}
}

// TestGetDPUMode_CommandFailureContext verifies command errors include cmd, stdout, stderr, and cause.
func TestGetDPUMode_CommandFailureContext(t *testing.T) {
	cause := fmt.Errorf("exit status 1")
	runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
		var stdout, stderr bytes.Buffer
		stdout.WriteString("partial output")
		stderr.WriteString("connection refused")
		return stdout, stderr, cause
	}

	mode, err := getDPUMode(testPCIAddress, runBash)
	require.Error(t, err)
	assert.Empty(t, mode)
	assert.Contains(t, err.Error(), "failed to run cmd:")
	assert.Contains(t, err.Error(), testPCIAddress)
	assert.Contains(t, err.Error(), "--path /nvidia/mode/state/mode")
	assert.Contains(t, err.Error(), "partial output")
	assert.Contains(t, err.Error(), "connection refused")
	assert.ErrorIs(t, err, cause)
}
