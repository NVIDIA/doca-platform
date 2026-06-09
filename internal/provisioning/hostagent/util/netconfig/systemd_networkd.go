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
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"k8s.io/klog/v2"
)

// SystemdNetworkdBackend implements Backend using systemd-networkd and netplan.
type SystemdNetworkdBackend struct{}

// NewSystemdNetworkdBackend creates a new systemd-networkd backend.
func NewSystemdNetworkdBackend() Backend {
	return &SystemdNetworkdBackend{}
}

func (s *SystemdNetworkdBackend) Name() string {
	return "systemd-networkd"
}

func (s *SystemdNetworkdBackend) ResetPendingChanges() {
	// No-op: systemd-networkd applies changes immediately via netplan, not batched.
}

func (s *SystemdNetworkdBackend) ConfigurePFInterfaces(pciAddress string, portConfigs []hostutil.PortConfig) (bool, error) {
	return hostutil.ConfigurePFs(pciAddress, portConfigs)
}

// ConfigureBridgeMTU configures the bridge MTU via netplan. The bridgeName
// parameter is ignored; netplan always targets the canonical br-dpu bridge.
func (s *SystemdNetworkdBackend) ConfigureBridgeMTU(bridgeName string, mtu int) (bool, error) {
	if bridgeName != hostutil.BridgeName {
		klog.Warningf("systemd-networkd backend: requested bridge %q differs from canonical %q; using %q",
			bridgeName, hostutil.BridgeName, hostutil.BridgeName)
	}
	return hostutil.ConfigureBridgeMTU(mtu)
}

func (s *SystemdNetworkdBackend) ApplyConfiguration() error {
	return hostutil.ApplyNetplan()
}

func (s *SystemdNetworkdBackend) IsDHCPConfigured(interfaceName string) (bool, error) {
	return hostutil.IsDHCPConfigured(interfaceName)
}

func (s *SystemdNetworkdBackend) EnsureVFsUnmanaged() error {
	return nil
}
