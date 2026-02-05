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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

// TestRshimQuerier_QueryByPCI_CommandVerification verifies the exact bash command being constructed.
func TestRshimQuerier_QueryByPCI_CommandVerification(t *testing.T) {
	pciAddress := "0000:4d:00"
	expectedCmdParts := []string{
		"ls /dev | egrep 'rshim.*[0-9]+'",
		"DISPLAY_LEVEL 2",
		"/dev/$line/misc",
		pciAddress,
		"RSHIM:$line",
	}

	querier := &RshimQuerier{
		runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			for _, part := range expectedCmdParts {
				assert.Contains(t, cmd, part, "Command should contain: %s", part)
			}
			var stdout bytes.Buffer
			stdout.WriteString("RSHIM:rshim0\nUEFI Secure Boot (enabled)\n")
			return stdout, bytes.Buffer{}, nil
		},
	}

	info, err := querier.QueryByPCI(pciAddress)
	require.NoError(t, err)
	assert.NotNil(t, info)
}

// TestRshimQuerier_QueryByPCI_Parsing tests parsing various rshim outputs with different Secure Boot states.
func TestRshimQuerier_QueryByPCI_Parsing(t *testing.T) {
	tests := []struct {
		name               string
		mockOutput         string
		expectedRshim      string
		expectedSecureBoot *bool
	}{
		{
			name: "Secure Boot enabled",
			mockOutput: `RSHIM:rshim0
DISPLAY_LEVEL   2
DEV_NAME        pcie-0000:4d:00.2
INFO[UEFI]: UEFI Secure Boot (enabled)`,
			expectedRshim:      "rshim0",
			expectedSecureBoot: ptr.To(true),
		},
		{
			name: "Secure Boot disabled",
			mockOutput: `RSHIM:rshim1
DISPLAY_LEVEL   2
DEV_NAME        pcie-0000:b1:00.2
INFO[UEFI]: UEFI Secure Boot (disabled)`,
			expectedRshim:      "rshim1",
			expectedSecureBoot: ptr.To(false),
		},
		{
			name: "No Secure Boot info (older firmware)",
			mockOutput: `RSHIM:rshim2
DISPLAY_LEVEL   2
DEV_NAME        pcie-0000:c1:00.2
INFO[BIOS]: Version 1.0.0`,
			expectedRshim:      "rshim2",
			expectedSecureBoot: nil,
		},
		{
			name: "Malformed Secure Boot line",
			mockOutput: `RSHIM:rshim3
UEFI Secure Boot (unknown)`,
			expectedRshim:      "rshim3",
			expectedSecureBoot: nil,
		},
		{
			name: "Case insensitive - uppercase ENABLED",
			mockOutput: `RSHIM:rshim4
UEFI SECURE BOOT (ENABLED)`,
			expectedRshim:      "rshim4",
			expectedSecureBoot: ptr.To(true),
		},
		{
			name: "Case insensitive - mixed case Disabled",
			mockOutput: `RSHIM:rshim5
Uefi Secure Boot (Disabled)`,
			expectedRshim:      "rshim5",
			expectedSecureBoot: ptr.To(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &RshimQuerier{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var stdout bytes.Buffer
					stdout.WriteString(tt.mockOutput)
					return stdout, bytes.Buffer{}, nil
				},
			}

			info, err := querier.QueryByPCI("0000:4d:00")

			require.NoError(t, err)
			require.NotNil(t, info)
			assert.Equal(t, tt.expectedRshim, info.RshimName)

			if tt.expectedSecureBoot == nil {
				assert.Nil(t, info.SecureBootEnabled)
			} else {
				require.NotNil(t, info.SecureBootEnabled)
				assert.Equal(t, *tt.expectedSecureBoot, *info.SecureBootEnabled)
			}
		})
	}
}

// TestRshimQuerier_QueryByPCI_ErrorCases tests error handling scenarios.
func TestRshimQuerier_QueryByPCI_ErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		runBash       func(cmd string) (bytes.Buffer, bytes.Buffer, error)
		expectedError string
	}{
		{
			name: "No rshim device found",
			runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				return bytes.Buffer{}, bytes.Buffer{}, nil
			},
			expectedError: "no rshim device found",
		},
		{
			name: "Bash execution failure",
			runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				var stderr bytes.Buffer
				stderr.WriteString("permission denied")
				return bytes.Buffer{}, stderr, fmt.Errorf("exit status 1")
			},
			expectedError: "failed to query rshim devices",
		},
		{
			name: "Missing RSHIM: prefix",
			runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				var stdout bytes.Buffer
				stdout.WriteString("DISPLAY_LEVEL 2\nDEV_NAME pcie-0000:4d:00.2")
				return stdout, bytes.Buffer{}, nil
			},
			expectedError: "failed to parse rshim device name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &RshimQuerier{runBash: tt.runBash}
			info, err := querier.QueryByPCI("0000:4d:00")

			assert.Error(t, err)
			assert.Nil(t, info)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

// TestRshimQuerier_QueryByPCI_DefaultRunBash verifies nil runBash defaults to real RunBash.
func TestRshimQuerier_QueryByPCI_DefaultRunBash(t *testing.T) {
	querier := &RshimQuerier{}
	info, err := querier.QueryByPCI("0000:ff:ff")

	// Should fail without real rshim devices
	assert.Error(t, err)
	assert.Nil(t, info)
}

// TestQueryRshimByPCI_BackwardCompatibility tests the wrapper function maintains compatibility.
func TestQueryRshimByPCI_BackwardCompatibility(t *testing.T) {
	info, err := QueryRshimByPCI("0000:ff:ff")

	assert.Error(t, err)
	assert.Nil(t, info)
}
