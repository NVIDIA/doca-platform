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
	"strings"

	"k8s.io/klog/v2"
)

// RshimInfo contains comprehensive information from querying a rshim device with DISPLAY_LEVEL 2.
type RshimInfo struct {
	// RshimName is the rshim device name (e.g., "rshim0", "rshim1")
	RshimName string
	// SecureBootEnabled indicates UEFI Secure Boot status (nil if not found in output)
	SecureBootEnabled *bool
	// RawOutput contains the full output from rshim for additional parsing if needed
	RawOutput string
}

// RshimQuerier provides methods for querying rshim devices with injectable bash execution for testing.
type RshimQuerier struct {
	// runBash is the function used to execute bash commands.
	// If nil, defaults to RunBash for production use.
	// In tests, inject a mock function to verify commands and return controlled output.
	runBash func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

// QueryByPCI finds the rshim device for a given PCI address and queries it with DISPLAY_LEVEL 2.
// This is the single comprehensive method for all rshim operations to avoid duplication.
//
// DISPLAY_LEVEL 2 returns all available information including:
//   - PCI address (for device matching)
//   - UEFI Secure Boot status
//   - Firmware version, BIOS version, etc.
//
// Args:
//   - pciAddress: PCI address of the DPU (e.g., "0000:4d:00")
//
// Returns:
//   - RshimInfo: Comprehensive information from the rshim device
//   - error: Any error encountered during discovery/query
//
// Usage examples:
//   - Secure Boot detection: Check info.SecureBootEnabled
//   - General rshim lookup: Use info.RshimName
func (r *RshimQuerier) QueryByPCI(pciAddress string) (*RshimInfo, error) {
	// Default to production RunBash if not injected (for tests)
	if r.runBash == nil {
		r.runBash = RunBash
	}
	// Bash command breakdown:
	// 1. List all /dev/rshim* devices
	// 2. For each rshim device:
	//    a. Set DISPLAY_LEVEL 2 to get detailed output (including Secure Boot)
	//    b. Read the output
	//    c. Search for the target PCI address
	//    d. If found, output the rshim name and full content
	cmd := fmt.Sprintf(
		"ls /dev | egrep 'rshim.*[0-9]+' | while read line ; do "+
			"echo 'DISPLAY_LEVEL 2' > /dev/$line/misc && "+
			"output=$(cat /dev/$line/misc) && "+
			"if echo \"$output\" | grep -q %s ; then "+
			"echo \"RSHIM:$line\" && echo \"$output\" ; "+
			"fi ; "+
			"done",
		pciAddress)

	stdout, stderr, err := r.runBash(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to query rshim devices for PCI address %s: stdout=%s, stderr=%s, error=%v",
			pciAddress, stdout.String(), stderr.String(), err)
	}

	output := strings.TrimSpace(stdout.String())
	if len(output) == 0 {
		return nil, fmt.Errorf("no rshim device found for PCI address %s", pciAddress)
	}

	var rshimName string
	var secureBootEnabled *bool

	for line := range strings.SplitSeq(output, "\n") {
		if name, found := strings.CutPrefix(line, "RSHIM:"); found {
			rshimName = name
			continue
		}

		// Extract UEFI Secure Boot status
		// Check explicitly for both enabled and disabled to be more robust.
		// If neither found, leave as nil (unknown state).
		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, "uefi secure boot") {
			if strings.Contains(lineLower, "(enabled)") {
				enabled := true
				secureBootEnabled = &enabled
			} else if strings.Contains(lineLower, "(disabled)") {
				enabled := false
				secureBootEnabled = &enabled
			}
			// If neither (enabled) nor (disabled) found, leave as nil
		}
	}

	if rshimName == "" {
		return nil, fmt.Errorf("failed to parse rshim device name from output for PCI %s", pciAddress)
	}

	info := &RshimInfo{
		RshimName:         rshimName,
		RawOutput:         output,
		SecureBootEnabled: secureBootEnabled,
	}

	klog.V(3).Infof("Found rshim %s for PCI %s (SecureBoot: %v)",
		rshimName, pciAddress, info.SecureBootEnabled)

	return info, nil
}

// QueryRshimByPCI is a convenience wrapper around RshimQuerier.QueryByPCI for backward compatibility.
// It uses the default RunBash implementation for bash execution.
//
// For testing with mocked bash commands, use RshimQuerier directly with an injected runBash function.
func QueryRshimByPCI(pciAddress string) (*RshimInfo, error) {
	return (&RshimQuerier{}).QueryByPCI(pciAddress)
}
