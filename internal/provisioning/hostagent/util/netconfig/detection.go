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

package netconfig

import (
	"fmt"
	"os/exec"
	"strings"
)

// DetectBackend detects and returns the appropriate network configuration backend
// Priority: nmstate > systemd-networkd
// Returns error if no backend is available
func DetectBackend() (Backend, error) {
	// Try nmstate first (used on RHEL/OpenShift)
	if err := probeNmstate(); err == nil {
		return NewNmstateBackend(newNmstateClient()), nil
	}

	// Fall back to systemd-networkd (used on Ubuntu)
	if HasSystemdNetworkd() {
		if err := EnsureSystemdNetworkdActive(); err == nil {
			return NewSystemdNetworkdBackend(), nil
		}
	}

	return nil, fmt.Errorf("no supported network configuration backend found (checked nmstate and systemd-networkd)")
}

// HasSystemdNetworkd checks if systemd-networkd is available on the system
func HasSystemdNetworkd() bool {
	cmd := exec.Command("systemctl", "status", "systemd-networkd")
	err := cmd.Run()
	return err == nil
}

// EnsureSystemdNetworkdActive validates that systemd-networkd is currently active and available
// Returns nil if systemd-networkd is active, error otherwise
func EnsureSystemdNetworkdActive() error {
	cmd := exec.Command("systemctl", "is-active", "systemd-networkd")
	output, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "active" {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to check systemd-networkd status: %w", err)
	}

	return fmt.Errorf("systemd-networkd is not active (status: %s)", strings.TrimSpace(string(output)))
}

// HasNetplan checks if netplan is available on the system
func HasNetplan() bool {
	_, err := exec.LookPath("netplan")
	return err == nil
}
