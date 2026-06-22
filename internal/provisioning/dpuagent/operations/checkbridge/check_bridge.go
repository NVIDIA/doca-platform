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

package checkbridge

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

// linkByNameFunc is the function signature for getting a link by name
type linkByNameFunc func(name string) (netlink.Link, error)

// addrListFunc is the function signature for listing addresses on a link
type addrListFunc func(link netlink.Link, family int) ([]netlink.Addr, error)

type CheckBridge struct {
	// linkByName is a configurable function for getting a link by name.
	// Defaults to netlink.LinkByName if nil.
	linkByName linkByNameFunc
	// addrList is a configurable function for listing addresses on a link.
	// Defaults to netlink.AddrList if nil.
	addrList addrListFunc
	runBash  func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

// getLinkByName returns the configured linkByName function or the default
func (c *CheckBridge) getLinkByName() linkByNameFunc {
	if c.linkByName != nil {
		return c.linkByName
	}
	return netlink.LinkByName
}

// getAddrList returns the configured addrList function or the default
func (c *CheckBridge) getAddrList() addrListFunc {
	if c.addrList != nil {
		return c.addrList
	}
	return netlink.AddrList
}

func (c *CheckBridge) Name() string {
	return "Check Bridge"
}

func (c *CheckBridge) ConditionType() string {
	return "BridgeChecked"
}

func (c *CheckBridge) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.ZeroTrustMode
}

func (c *CheckBridge) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (c *CheckBridge) Execute(execCtx context.Context, optCtx *operations.Context) error {
	shouldRunNetplanApplyForBridgeIP, err := c.checkBridgeIP()
	if err != nil {
		return fmt.Errorf("failed to check bridge IP: %w", err)
	}

	shouldRunNetplanApplyForBridgeMTU, err := c.checkBridgeMTU(optCtx)
	if err != nil {
		return fmt.Errorf("failed to check bridge MTU: %w", err)
	}

	shouldRunNetplanApply := shouldRunNetplanApplyForBridgeIP || shouldRunNetplanApplyForBridgeMTU

	if !shouldRunNetplanApply {
		klog.Info("No reconfiguration required")
		return nil
	}

	klog.Info("Running netplan apply to reconfigure")
	if c.runBash == nil {
		c.runBash = bash.Run
	}
	if _, stderr, err := c.runBash("netplan apply"); err != nil {
		klog.Warningf("netplan apply failed: %v, stderr: %s", err, stderr.String())
	}

	return fmt.Errorf("br-comm-ch required reconfiguration")
}

// checkBridgeIP checks if br-comm-ch has an IP address
func (c *CheckBridge) checkBridgeIP() (bool, error) {
	klog.Info("Checking if br-comm-ch has an IP address")

	link, err := c.getLinkByName()("br-comm-ch")
	if err != nil {
		return false, fmt.Errorf("failed to get link by name for br-comm-ch: %w", err)
	}
	addrs, err := c.getAddrList()(link, netlink.FAMILY_V4)
	if err != nil {
		return false, fmt.Errorf("failed to get addresses for br-comm-ch: %w", err)
	}

	if len(addrs) == 0 {
		klog.Info("br-comm-ch has no IP address, will run netplan apply to reconfigure")
		return true, nil
	}

	ips := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP.String())
	}
	klog.Infof("br-comm-ch IP addresses: %s", strings.Join(ips, ", "))

	return false, nil
}

// checkBridgeMTU checks if br-comm-ch and pf0vf0 have the expected MTU
func (c *CheckBridge) checkBridgeMTU(optCtx *operations.Context) (bool, error) {
	klog.Info("Checking if br-comm-ch and pf0vf0 have expected MTU")
	commChLink, err := c.getLinkByName()("br-comm-ch")
	if err != nil {
		return false, fmt.Errorf("failed to get link by name for br-comm-ch: %w", err)
	}

	if commChLink.Attrs().MTU != int(optCtx.Options.ControlPlaneMTU) {
		klog.Infof("br-comm-ch MTU mismatch (current=%d, desired=%d), will run netplan apply to reconfigure", commChLink.Attrs().MTU, optCtx.Options.ControlPlaneMTU)
		return true, nil
	}

	// Note: pf0vf0 is only valid for BF3.
	pf0vf0Link, err := c.getLinkByName()("pf0vf0")
	if err != nil {
		return false, fmt.Errorf("failed to get link by name for pf0vf0: %w", err)
	}

	if pf0vf0Link.Attrs().MTU != int(optCtx.Options.ControlPlaneMTU) {
		klog.Infof("pf0vf0 MTU mismatch (current=%d, desired=%d), will run netplan apply to reconfigure", pf0vf0Link.Attrs().MTU, optCtx.Options.ControlPlaneMTU)
		return true, nil
	}

	klog.Infof("matching MTU(%d) found for br-comm-ch and pf0vf0", optCtx.Options.ControlPlaneMTU)
	return false, nil
}
