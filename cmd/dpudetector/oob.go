//go:build linux

/*
Copyright 2024 NVIDIA

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

package main

import (
	"fmt"
	"net"

	"github.com/golang/glog"
	"github.com/vishvananda/netlink"
)

const (
	bridgeName   = "br-dpu"
	lowestMetric = 2
)

func isOOBBridgeConfigured() bool {
	if err := verifyIfBridgeExists(); err != nil {
		glog.Error(err)
		return false
	}

	if err := verifyDefaultRoute(); err != nil {
		glog.Error(err)
		return false
	}

	return true
}

func verifyIfBridgeExists() error {
	link, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("bridge %s does not exist", bridgeName)
	}
	if link.Type() != "bridge" {
		return fmt.Errorf("interface %s is not a bridge", bridgeName)
	}
	return nil
}

func verifyDefaultRoute() error {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	var foundRoute bool
	_, defaultRoute, _ := net.ParseCIDR("0.0.0.0/0")
	for _, route := range routes {
		if !route.Dst.Contains(defaultRoute.IP) || route.LinkIndex != linkIndex(bridgeName) {
			continue
		}

		if route.Priority <= lowestMetric {
			return fmt.Errorf("metric for default route for bridge %s is not higher than 2", bridgeName)
		}

		foundRoute = true
	}

	if !foundRoute {
		return fmt.Errorf("default route for bridge %s does not exist", bridgeName)
	}
	return nil
}

// Helper function to get link index by interface name
func linkIndex(iface string) int {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return -1
	}
	return link.Attrs().Index
}
