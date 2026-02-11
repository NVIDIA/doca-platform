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
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"k8s.io/klog/v2"
)

// hostCommand creates an exec.Cmd that runs a command in the host environment
// via nsenter. The hostagent container runs with hostPID: true, so
// nsenter --target 1 enters the host's namespaces where host binaries
// (e.g. nmstatectl) are installed. This avoids having to install
// host-specific tools in the Ubuntu-based container image.
//
// nsenter is used to launch processes inside the container in a way that makes
// said processes feel and behave as if they're running on the host directly
// rather than inside the container.
func hostCommand(command string, args ...string) *exec.Cmd {
	nsenterArgs := []string{
		"--target", "1",
		// Entering the cgroup namespace is needed on some Fedora/systemd-based
		// systems to ensure proper cgroup slice handling.
		"--cgroup",
		// The mount namespace is required to access the host's filesystem and binaries.
		"--mount",
		// The IPC namespace is needed for shared memory and D-Bus communication.
		"--ipc",
		// The PID namespace ensures the process sees the host's PID space.
		"--pid",
		"--",
		command,
	}
	nsenterArgs = append(nsenterArgs, args...)
	return exec.Command("nsenter", nsenterArgs...)
}

// nmstateClient abstracts the nmstate CLI (nmstatectl) for testability.
type nmstateClient interface {
	ApplyNetState(state string) (string, error)
}

// nmstatectlClient implements nmstateClient by running the host's nmstatectl
// binary via nsenter into the host's namespaces.
type nmstatectlClient struct{}

func newNmstateClient() nmstateClient {
	return &nmstatectlClient{}
}

func (c *nmstatectlClient) ApplyNetState(state string) (string, error) {
	cmd := hostCommand("nmstatectl", "apply")
	cmd.Stdin = strings.NewReader(state)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("nmstatectl apply failed: %w, output: %s", err, string(output))
	}
	klog.V(3).Infof("nmstatectl apply output: %s", string(output))
	return state, nil
}

// probeNmstate checks that the host has nmstatectl installed and functional
// by running nmstatectl show via nsenter into the host's namespaces.
func probeNmstate() error {
	cmd := hostCommand("nmstatectl", "show")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nmstatectl is not available on the host: %w, output: %s", err, string(output))
	}
	return nil
}

// getCurrentNmstate retrieves the current network state from the host via nmstatectl.
func getCurrentNmstate() (*nmstateNetState, error) {
	cmd := hostCommand("nmstatectl", "show", "--json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("nmstatectl show failed: %w, output: %s", err, string(output))
	}
	var state nmstateNetState
	if err := json.Unmarshal(output, &state); err != nil {
		return nil, fmt.Errorf("failed to parse nmstate output: %w", err)
	}
	return &state, nil
}

// isDHCPEnabled checks whether DHCP is currently enabled for the given interface
// by querying the host's network state via nmstatectl.
func isDHCPEnabled(currentState *nmstateNetState, interfaceName string) bool {
	for _, iface := range currentState.Interfaces {
		if iface.Name == interfaceName {
			return iface.IPv4 != nil && iface.IPv4.DHCP
		}
	}
	return false
}

// NmstateBackend implements Backend interface using nmstate (nmstatectl).
//
// nmstate operates declaratively: we describe the desired network state and
// nmstate reconciles it against the current state, applying only the necessary
// changes. Before applying, we compare the current state (MTU via netlink,
// DHCP via nmstatectl show) against the desired state to avoid unnecessary
// apply calls. When apply is needed, nmstate handles verification and
// automatic rollback on failure.
type NmstateBackend struct {
	nms               nmstateClient
	pendingInterfaces []nmstateInterface
}

// nmstate network state JSON types

type nmstateNetState struct {
	Interfaces []nmstateInterface `json:"interfaces,omitempty"`
}

type nmstateInterface struct {
	Name  string       `json:"name"`
	Type  string       `json:"type"`
	State string       `json:"state"`
	MTU   *int         `json:"mtu,omitempty"`
	IPv4  *nmstateIPv4 `json:"ipv4,omitempty"`
}

type nmstateIPv4 struct {
	Enabled bool `json:"enabled"`
	DHCP    bool `json:"dhcp"`
}

func NewNmstateBackend(client nmstateClient) Backend {
	return &NmstateBackend{nms: client}
}

func (n *NmstateBackend) Name() string {
	return string(BackendTypeNmstate)
}

func (n *NmstateBackend) IsAvailable() bool {
	return probeNmstate() == nil
}

func (n *NmstateBackend) Reset() {
	n.pendingInterfaces = nil
}

// ConfigurePFInterfaces declares the desired state for physical function network interfaces.
// Returns (needsApply, error) where needsApply is true only when the current state
// differs from the desired state.
func (n *NmstateBackend) ConfigurePFInterfaces(pciAddress string, portConfigs []PortConfig) (bool, error) {
	pciHelper := util.NewPCIHelper(pciAddress)
	needsApply := false

	// Retrieve current network state once for DHCP checks.
	var currentState *nmstateNetState
	for _, pc := range portConfigs {
		if pc.DHCP != nil {
			var err error
			currentState, err = getCurrentNmstate()
			if err != nil {
				return false, fmt.Errorf("failed to retrieve current network state: %w", err)
			}
			break
		}
	}

	for _, portConfig := range portConfigs {
		if portConfig.MTU == nil && portConfig.DHCP == nil {
			continue
		}

		pf := pciHelper.PF(int(portConfig.PortNumber))
		interfaceName, err := pf.InterfaceName()
		if err != nil {
			return false, fmt.Errorf("failed to get PF%d interface name: %w", portConfig.PortNumber, err)
		}

		iface := nmstateInterface{
			Name:  interfaceName,
			Type:  "ethernet",
			State: "up",
		}

		if portConfig.MTU != nil {
			mtu := int(*portConfig.MTU)
			iface.MTU = &mtu
			currentMTU, err := util.GetCurrentMTU(interfaceName)
			if err != nil {
				return false, fmt.Errorf("failed to get current MTU for %s: %w", interfaceName, err)
			}
			if currentMTU != int(*portConfig.MTU) {
				klog.Infof("%s MTU mismatch (current=%d, desired=%d)", interfaceName, currentMTU, *portConfig.MTU)
				needsApply = true
			}
		}

		if portConfig.DHCP != nil {
			iface.IPv4 = &nmstateIPv4{
				Enabled: true,
				DHCP:    *portConfig.DHCP,
			}
			currentDHCP := isDHCPEnabled(currentState, interfaceName)
			if currentDHCP != *portConfig.DHCP {
				klog.Infof("%s DHCP mismatch (current=%v, desired=%v)", interfaceName, currentDHCP, *portConfig.DHCP)
				needsApply = true
			}
		}

		n.pendingInterfaces = append(n.pendingInterfaces, iface)
	}

	return needsApply, nil
}

// ConfigureBridgeMTU declares the desired MTU for a bridge and its member interfaces.
// Returns (needsApply, error) where needsApply is true only when the current MTU
// of the bridge or any member differs from the desired value.
func (n *NmstateBackend) ConfigureBridgeMTU(bridgeName string, mtu int) (bool, error) {
	needsApply := false

	// Check bridge MTU
	currentBridgeMTU, err := util.GetCurrentMTU(bridgeName)
	if err != nil {
		return false, fmt.Errorf("failed to get current bridge MTU: %w", err)
	}
	if currentBridgeMTU != mtu {
		klog.Infof("Bridge %s MTU mismatch (current=%d, desired=%d)", bridgeName, currentBridgeMTU, mtu)
		needsApply = true
	}

	memberNames, err := util.GetBridgeMembers(bridgeName)
	if err != nil {
		return false, fmt.Errorf("failed to get bridge members for %s: %w", bridgeName, err)
	}

	// Check member MTUs
	for _, memberName := range memberNames {
		currentMTU, err := util.GetCurrentMTU(memberName)
		if err != nil {
			return false, fmt.Errorf("failed to get current MTU for bridge member %s: %w", memberName, err)
		}
		if currentMTU != mtu {
			klog.Infof("Bridge member %s MTU mismatch (current=%d, desired=%d)", memberName, currentMTU, mtu)
			needsApply = true
		}
	}

	// Always declare desired state so that if apply is needed, the full state is sent.
	// We intentionally omit the bridge port config — nmstate's partial editing
	// preserves existing port membership when the bridge subtree is not specified.
	n.pendingInterfaces = append(n.pendingInterfaces, nmstateInterface{
		Name:  bridgeName,
		Type:  "linux-bridge",
		State: "up",
		MTU:   &mtu,
	})

	for _, memberName := range memberNames {
		n.pendingInterfaces = append(n.pendingInterfaces, nmstateInterface{
			Name:  memberName,
			Type:  "ethernet",
			State: "up",
			MTU:   &mtu,
		})
	}

	return needsApply, nil
}

// ApplyConfiguration applies all pending desired state via a single nmstate transaction.
// nmstate verifies the result matches the desired state and rolls back on failure.
func (n *NmstateBackend) ApplyConfiguration() error {
	if len(n.pendingInterfaces) == 0 {
		klog.V(3).Info("No pending nmstate changes to apply")
		return nil
	}

	state := nmstateNetState{
		Interfaces: n.pendingInterfaces,
	}

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal nmstate desired state: %w", err)
	}

	klog.Infof("Applying nmstate desired state: %s", string(stateJSON))

	if _, err := n.nms.ApplyNetState(string(stateJSON)); err != nil {
		return fmt.Errorf("failed to apply nmstate network state: %w", err)
	}

	n.pendingInterfaces = nil

	return nil
}
