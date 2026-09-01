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

package underlaymtu

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/netplan"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	underlayMTU        = 9216
	ovsTimeout         = "5"
	defaultNetplanRoot = "/etc/netplan"
	pfMTUNetplanFile   = "97-pf-mtu.yaml"
)

// SetNetplanUnderlayMTU writes netplan MTU 9216 for N/S uplinks and host PF
// representors that OVS has not given an mtu_request. Interfaces whose OVS
// Interface.mtu_request is set keep that value from the DPUFlavor script in
// RunOVSScript.
//
// This operation must run after RunOVSScript so add-port / mtu_request have
// already been applied.
type SetNetplanUnderlayMTU struct {
	netplanRoot      string
	runBash          func(cmd string) (bytes.Buffer, bytes.Buffer, error)
	listPFRepsFunc   func() ([]string, error)
	applyNetplanFunc func() error
}

func (s *SetNetplanUnderlayMTU) Name() string {
	return "Set Netplan Underlay MTU"
}

func (s *SetNetplanUnderlayMTU) ConditionType() string {
	return "UnderlayNetplanMTUConfigured"
}

func (s *SetNetplanUnderlayMTU) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipNetworkConfig
}

func (s *SetNetplanUnderlayMTU) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (s *SetNetplanUnderlayMTU) Execute(execCtx context.Context, optCtx *operations.Context) error {
	devs, err := s.underlayDevs(optCtx)
	if err != nil {
		return err
	}
	run := s.runBash
	if run == nil {
		run = bash.Run
	}

	needNetplan := make([]string, 0, len(devs))
	for _, dev := range devs {
		if err := validateNetdev(dev); err != nil {
			return err
		}
		owned, err := ovsOwnsMTU(run, dev)
		if err != nil {
			return err
		}
		if owned {
			klog.Infof("Skipping netplan MTU for %s: OVS Interface.mtu_request is set", dev)
			continue
		}
		needNetplan = append(needNetplan, dev)
		klog.Infof("Setting netplan MTU for %s: OVS mtu_request not set", dev)
	}
	if len(needNetplan) == 0 {
		klog.Info("All underlay ports have OVS mtu_request; skipping netplan underlay MTU")
		return nil
	}
	if err := s.writeNetplan(needNetplan); err != nil {
		return err
	}
	apply := s.applyNetplanFunc
	if apply == nil {
		apply = runNetplanApply
	}
	if err := apply(); err != nil {
		return fmt.Errorf("failed to apply netplan underlay MTU: %w", err)
	}
	return nil
}

func (s *SetNetplanUnderlayMTU) writeNetplan(devs []string) error {
	root := s.netplanRoot
	if root == "" {
		root = defaultNetplanRoot
	}
	config := &netplan.Config{
		Network: netplan.Network{
			Version:  2,
			Renderer: "networkd",
		},
	}
	config.Network.Ethernets = make(map[string]netplan.Ethernet, len(devs))
	for _, dev := range devs {
		config.Network.Ethernets[dev] = netplan.Ethernet{
			MTU: ptr.To(int32(underlayMTU)),
		}
	}
	name := filepath.Join(root, pfMTUNetplanFile)
	if err := config.WriteToFile(name); err != nil {
		return fmt.Errorf("failed to write %s: %w", name, err)
	}
	klog.Infof("Wrote netplan underlay MTU %d for interfaces without OVS mtu_request: %s", underlayMTU, strings.Join(devs, ", "))
	return nil
}

func (s *SetNetplanUnderlayMTU) underlayDevs(optCtx *operations.Context) ([]string, error) {
	ports, err := optCtx.NSPorts()
	if err != nil {
		return nil, err
	}
	listPFReps := pciutil.DefaultPortDiscoverer.DiscoverNSPFRepresentors
	if s.listPFRepsFunc != nil {
		listPFReps = s.listPFRepsFunc
	}
	pfReps, err := listPFReps()
	if err != nil {
		return nil, err
	}
	devs := make([]string, 0, len(ports)+len(pfReps))
	for _, port := range ports {
		if strings.TrimSpace(port.Netdev) == "" {
			continue
		}
		devs = append(devs, port.Netdev)
	}
	devs = append(devs, pfReps...)
	return devs, nil
}

func ovsOwnsMTU(run func(cmd string) (bytes.Buffer, bytes.Buffer, error), dev string) (bool, error) {
	cmd := fmt.Sprintf("ovs-vsctl --timeout=%s get Interface %s mtu_request", ovsTimeout, dev)
	stdout, stderr, err := run(cmd)
	if err != nil {
		combined := stderr.String() + err.Error()
		// Missing Interface row means OVS has no MTU for this netdev.
		if strings.Contains(combined, "no row") {
			klog.Infof("get Interface %s mtu_request: no OVS Interface: %v, stderr: %s", dev, err, stderr.String())
			return false, nil
		}
		return false, fmt.Errorf("failed to get OVS mtu_request for %s: %w, stderr: %s", dev, err, stderr.String())
	}
	val := strings.TrimSpace(stdout.String())
	if val == "" || val == "[]" {
		klog.Infof("get Interface %s mtu_request: not set (%q)", dev, val)
		return false, nil
	}
	return true, nil
}

func validateNetdev(name string) error {
	if name == "" {
		return fmt.Errorf("empty netdev name")
	}
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
			continue
		}
		return fmt.Errorf("invalid netdev name %q", name)
	}
	return nil
}

func runNetplanApply() error {
	stdout, stderr, err := bash.Run("netplan apply")
	if err != nil {
		return fmt.Errorf("failed to apply netplan: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}
