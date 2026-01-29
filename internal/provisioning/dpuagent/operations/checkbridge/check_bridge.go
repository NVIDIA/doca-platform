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
	"context"
	"fmt"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

// linkByNameFunc is the function signature for getting a link by name
type linkByNameFunc func(name string) (netlink.Link, error)

// addrListFunc is the function signature for listing addresses on a link
type addrListFunc func(link netlink.Link, family int) ([]netlink.Addr, error)

type CheckBridgeIP struct {
	// linkByName is a configurable function for getting a link by name.
	// Defaults to netlink.LinkByName if nil.
	linkByName linkByNameFunc
	// addrList is a configurable function for listing addresses on a link.
	// Defaults to netlink.AddrList if nil.
	addrList addrListFunc
}

// getLinkByName returns the configured linkByName function or the default
func (c *CheckBridgeIP) getLinkByName() linkByNameFunc {
	if c.linkByName != nil {
		return c.linkByName
	}
	return netlink.LinkByName
}

// getAddrList returns the configured addrList function or the default
func (c *CheckBridgeIP) getAddrList() addrListFunc {
	if c.addrList != nil {
		return c.addrList
	}
	return netlink.AddrList
}

func (c *CheckBridgeIP) Name() string {
	return "Check Bridge IP"
}

func (c *CheckBridgeIP) ConditionType() string {
	return "BridgeIPChecked"
}

func (c *CheckBridgeIP) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.ZeroTrustMode
}

func (c *CheckBridgeIP) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (c *CheckBridgeIP) Execute(execCtx context.Context, optCtx *operations.Context) error {
	klog.Info("Checking if br-comm-ch has an IP address")
	link, err := c.getLinkByName()("br-comm-ch")
	if err != nil {
		return fmt.Errorf("failed to get link by name: %w", err)
	}
	addrs, err := c.getAddrList()(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to get addresses: %w", err)
	}
	if len(addrs) > 0 {
		ips := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			ips = append(ips, addr.IP.String())
		}
		klog.Infof("br-comm-ch IP addresses: %s", strings.Join(ips, ", "))
		return nil
	}
	return fmt.Errorf("br-comm-ch does not have an IP address")
}
