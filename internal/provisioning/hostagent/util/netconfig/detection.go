// Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package netconfig

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/godbus/dbus/v5"
)

// DetectBackend detects and returns the appropriate network configuration backend
// Priority: NetworkManager > systemd-networkd
// Returns error if no backend is available
func DetectBackend() (Backend, error) {
	// Try NetworkManager first (used on RHEL/OpenShift)
	if  HasNetworkManager() {
		if err := EnsureNetworkManagerActive(); err == nil {
			return NewNetworkManagerBackend(), nil
		}
	}

	// Fall back to systemd-networkd (used on Ubuntu)
	if HasSystemdNetworkd() {
		if err := EnsureSystemdNetworkdActive(); err == nil {
			return NewSystemdNetworkdBackend(), nil
		}
	}

	return nil, fmt.Errorf("no supported network configuration backend found (checked NetworkManager and systemd-networkd)")
}

// HasNetworkManager checks if NetworkManager is available on the system
func HasNetworkManager() bool {
	cmd := exec.Command("systemctl", "status", "NetworkManager")
	err := cmd.Run()
	return err == nil
}

// EnsureNetworkManagerActive validates that NetworkManager is currently active and available
// Returns nil if NetworkManager is active and accessible via D-Bus, error otherwise
func EnsureNetworkManagerActive() error {
	// First check if the service is active
	cmd := exec.Command("systemctl", "is-active", "NetworkManager")
	output, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(output)) != "active" {
		if err != nil {
			return fmt.Errorf("failed to check NetworkManager status: %w", err)
		}
		return fmt.Errorf("NetworkManager is not active (status: %s)", strings.TrimSpace(string(output)))
	}

	// Verify D-Bus connectivity to NetworkManager
	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system D-Bus: %w", err)
	}

	// Try to access NetworkManager on D-Bus
	obj := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")
	var version string
	err = obj.Call("org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.NetworkManager", "Version").Store(&version)
	if err != nil {
		return fmt.Errorf("NetworkManager not accessible via D-Bus: %w", err)
	}

	return nil
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
