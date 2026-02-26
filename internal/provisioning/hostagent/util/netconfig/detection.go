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

package netconfig

import (
	"fmt"
	"os/exec"
	"strings"

	"k8s.io/klog/v2"
)

// nmManagesDevicesFunc checks whether NetworkManager is managing ethernet
// devices via the provided NMClient. Overridable in tests.
var nmManagesDevicesFunc = func(client NMClient) bool {
	manages, err := client.IsManagingEthernetDevices()
	if err != nil {
		klog.V(3).Infof("Failed to query NM managed devices: %v", err)
		return false
	}
	return manages
}

// commandRunner executes a command and returns its combined output.
// Overridable in tests.
var commandRunner = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// newNMClientFunc creates an NMClient. Overridable in tests.
var newNMClientFunc = func() (NMClient, error) {
	return NewDBusNMClient()
}

// DetectBackend probes the host (via D-Bus from the container) to determine
// which network management service is actively managing interfaces, and
// returns the appropriate Backend.
//
// Detection strategy:
//  1. Check if systemd-networkd is active (systemctl is-active, routed via D-Bus).
//  2. Check if NetworkManager is active and reachable on D-Bus.
//  3. If only one is active, use it.
//  4. If both are active, query NetworkManager via D-Bus to see if it is managing
//     any ethernet devices. If NM is managing devices, prefer NetworkManager;
//     otherwise prefer systemd-networkd.
//  5. If neither is active, return an error.
func DetectBackend() (Backend, error) {
	systemdActive := isSystemdNetworkdActive()
	nmClient, nmActive := probeNetworkManager()

	klog.Infof("Backend detection: systemd-networkd active=%v, NetworkManager active=%v", systemdActive, nmActive)

	switch {
	case systemdActive && !nmActive:
		klog.Infof("Selected backend: systemd-networkd (only active service)")
		return NewSystemdNetworkdBackend(), nil

	case !systemdActive && nmActive:
		klog.Infof("Selected backend: NetworkManager (only active service)")
		return NewNetworkManagerBackend(nmClient), nil

	case systemdActive && nmActive:
		if nmManagesDevicesFunc(nmClient) {
			klog.Infof("Selected backend: NetworkManager (both active, NM managing ethernet devices)")
			return NewNetworkManagerBackend(nmClient), nil
		}
		klog.Infof("Selected backend: systemd-networkd (both active, NM not managing ethernet devices)")
		return NewSystemdNetworkdBackend(), nil

	default:
		return nil, fmt.Errorf("no supported network backend found (checked systemd-networkd and NetworkManager)")
	}
}

// isSystemdNetworkdActive checks whether the host's systemd-networkd service is active.
// Inside the DMS container SYSTEMCTL_FORCE_BUS=1 routes systemctl over D-Bus to the host.
func isSystemdNetworkdActive() bool {
	output, err := commandRunner("systemctl", "is-active", "systemd-networkd")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "active"
}

// probeNetworkManager checks whether the host's NetworkManager service is
// active AND reachable via D-Bus. Returns the verified client on success
// so callers can reuse it without creating a second connection.
func probeNetworkManager() (NMClient, bool) {
	output, err := commandRunner("systemctl", "is-active", "NetworkManager")
	if err != nil {
		return nil, false
	}
	if strings.TrimSpace(string(output)) != "active" {
		return nil, false
	}

	client, err := newNMClientFunc()
	if err != nil {
		klog.V(3).Infof("NetworkManager service is active but D-Bus not accessible: %v", err)
		return nil, false
	}
	if _, err := client.GetVersion(); err != nil {
		klog.V(3).Infof("NetworkManager D-Bus reachable but GetVersion failed: %v", err)
		return nil, false
	}
	return client, true
}
