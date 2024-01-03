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

package networkhelper

import (
	"errors"
	"fmt"
	"math"
	"net"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"k8s.io/utils/ptr"
)

type networkHelper struct{}

// New creates a NetworkHelper
func newNetworkHelper() NetworkHelper {
	return &networkHelper{}
}

// SetLinkUp sets the administrative state of a link to "up"
func (n *networkHelper) SetLinkUp(link string) error {
	l, err := netlink.LinkByName(link)
	if err != nil {
		return fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	err = netlink.LinkSetUp(l)
	if err != nil {
		return fmt.Errorf("netlink.LinkSetUp() failed: %w", err)
	}
	return nil
}

// SetLinkDown sets the administrative state of a link to "down"
func (n *networkHelper) SetLinkDown(link string) error {
	l, err := netlink.LinkByName(link)
	if err != nil {
		return fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	err = netlink.LinkSetDown(l)
	if err != nil {
		return fmt.Errorf("netlink.LinkSetDown() failed: %w", err)
	}
	return nil
}

// RenameLink renames a link to the given name
func (n *networkHelper) RenameLink(link string, newName string) error {
	l, err := netlink.LinkByName(link)
	if err != nil {
		return fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	err = netlink.LinkSetName(l, newName)
	if err != nil {
		return fmt.Errorf("netlink.LinkSetName() failed: %w", err)
	}
	return nil
}

// SetLinkIPAddress sets the IP address of a link
func (n *networkHelper) SetLinkIPAddress(link string, ipNet *net.IPNet) error {
	if ipNet == nil {
		return errors.New("ipNet is empty, can't set link IP")
	}
	l, err := netlink.LinkByName(link)
	if err != nil {
		return fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	ip, err := netlink.ParseAddr(ipNet.String())
	if err != nil {
		return fmt.Errorf("netlink.ParseAddr() failed: %w", err)
	}
	err = netlink.AddrAdd(l, ip)
	if err != nil {
		return fmt.Errorf("netlink.AddrAdd() failed: %w", err)
	}
	return nil
}

// LinkIPAddressExists checks whether a link has the given IP.
func (n *networkHelper) LinkIPAddressExists(link string, ipNet *net.IPNet) (bool, error) {
	l, err := netlink.LinkByName(link)
	if err != nil {
		return false, fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	givenIP, err := netlink.ParseAddr(ipNet.String())
	if err != nil {
		return false, fmt.Errorf("netlink.ParseAddr() failed: %w", err)
	}
	ips, err := netlink.AddrList(l, netlink.FAMILY_V4)
	if err != nil {
		return false, fmt.Errorf("netlink.AddrList() failed: %w", err)
	}
	for _, ip := range ips {
		if givenIP.Equal(ip) {
			return true, nil
		}
	}
	return false, nil
}

// DeleteLinkIPAddress deletes the given IP of a link
func (n *networkHelper) DeleteLinkIPAddress(link string, ipNet *net.IPNet) error {
	if ipNet == nil {
		return errors.New("ipNet is empty, can't delete link IP")
	}
	l, err := netlink.LinkByName(link)
	if err != nil {
		return fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	ip, err := netlink.ParseAddr(ipNet.String())
	if err != nil {
		return fmt.Errorf("netlink.ParseAddr() failed: %w", err)
	}
	err = netlink.AddrDel(l, ip)
	if err != nil {
		return fmt.Errorf("netlink.AddrDel() failed: %w", err)
	}
	return nil
}

// DeleteNeighbor deletes a neighbor
func (n *networkHelper) DeleteNeighbor(ip net.IP, device string) error {
	if ip == nil {
		return errors.New("ip is empty, can't delete neighbor")
	}
	l, err := netlink.LinkByName(device)
	if err != nil {
		return fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	neigh := &netlink.Neigh{
		LinkIndex: l.Attrs().Index,
		IP:        ip,
	}
	err = netlink.NeighDel(neigh)
	if err != nil {
		return fmt.Errorf("netlink.NeighDel() failed: %w", err)
	}
	return nil
}

// NeighborExists checks whether an neighbor entry exists
func (n *networkHelper) NeighborExists(ip net.IP, device string) (bool, error) {
	if ip == nil {
		return false, errors.New("ip is empty, can't check whether neighbor exists")
	}
	l, err := netlink.LinkByName(device)
	if err != nil {
		return false, fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	neighbors, err := netlink.NeighList(l.Attrs().Index, netlink.FAMILY_V4)
	if err != nil {
		return false, fmt.Errorf("netlink.NeighList() failed: %w", err)
	}
	for _, neigh := range neighbors {
		if neigh.IP.String() == ip.String() {
			return true, nil
		}
	}
	return false, nil
}

// DeleteRoute deletes a route
func (n *networkHelper) DeleteRoute(network *net.IPNet, gateway net.IP, device string) error {
	if network == nil {
		return errors.New("network is empty, can't delete route")
	}
	l, err := netlink.LinkByName(device)
	if err != nil {
		return fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	r := &netlink.Route{
		Dst:       network,
		Gw:        gateway,
		LinkIndex: l.Attrs().Index,
	}
	err = netlink.RouteDel(r)
	if err != nil {
		return fmt.Errorf("netlink.RouteDel() failed: %w", err)
	}
	return nil
}

// RouteExists checks whether a route exists
func (n *networkHelper) RouteExists(network *net.IPNet, gateway net.IP, device string) (bool, error) {
	if network == nil {
		return false, errors.New("network is empty, can't check whether route exists")
	}
	l, err := netlink.LinkByName(device)
	if err != nil {
		return false, fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	routes, err := netlink.RouteList(l, netlink.FAMILY_V4)
	if err != nil {
		return false, fmt.Errorf("netlink.RouteList() failed: %w", err)
	}

	for _, r := range routes {
		if r.Dst.String() == network.String() && r.Gw.String() == gateway.String() {
			return true, nil
		}
	}
	return false, nil
}

// AddRoute adds a route
func (n *networkHelper) AddRoute(network *net.IPNet, gateway net.IP, device string, metric *int) error {
	if network == nil {
		return errors.New("network is empty, can't add route")
	}
	l, err := netlink.LinkByName(device)
	if err != nil {
		return fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	r := &netlink.Route{
		Dst:       network,
		Gw:        gateway,
		LinkIndex: l.Attrs().Index,
		Priority:  ptr.Deref[int](metric, 0),
	}
	err = netlink.RouteAdd(r)
	if err != nil {
		return fmt.Errorf("netlink.RouteAdd() failed: %w", err)
	}
	return nil
}

// AddDummyLink adds a dummy link
func (n *networkHelper) AddDummyLink(link string) error {
	l := netlink.Dummy{
		LinkAttrs: netlink.LinkAttrs{
			Name: link,
		},
	}
	err := netlink.LinkAdd(&l)
	if err != nil {
		return fmt.Errorf("netlink.LinkAdd() failed: %w", err)
	}
	return nil
}

// DummyLinkExists checks if a dummy link exists
func (n *networkHelper) DummyLinkExists(link string) (bool, error) {
	l, err := netlink.LinkByName(link)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return false, nil
		}
		return false, fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}

	d := netlink.Dummy{}
	if l.Type() != d.Type() {
		return false, fmt.Errorf("link %s exists but is not of type dummy: type=%s", link, l.Type())
	}

	return true, nil
}

// LinkExists checks whether a link exists
func (n *networkHelper) LinkExists(link string) (bool, error) {
	_, err := netlink.LinkByName(link)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return false, nil
		}
		return false, fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}

	return true, nil
}

// GetPFRepMacAddress returns the MAC address of the PF Representor provided as input. When you run this function in
// the DPU, it will give you the MAC address of the PF Representor on the host.
func (n *networkHelper) GetPFRepMACAddress(device string) (net.HardwareAddr, error) {
	ports, err := netlink.DevLinkGetAllPortList()
	if err != nil {
		return nil, fmt.Errorf("netlink.DevLinkGetAllPortList() failed: %w", err)
	}

	for _, p := range ports {
		//nolint:misspell
		if p.PortFlavour == nl.DEVLINK_PORT_FLAVOUR_PCI_PF && p.Fn != nil {
			return p.Fn.HwAddr, nil
		}
	}
	return nil, fmt.Errorf("MAC Address not found for PF Representor %s", device)
}

// GetLinkIPAddresses returns the IP addresses of a link
func (n *networkHelper) GetLinkIPAddresses(link string) ([]*net.IPNet, error) {
	l, err := netlink.LinkByName(link)
	if err != nil {
		return nil, fmt.Errorf("netlink.LinkByName() failed: %w", err)
	}
	ips, err := netlink.AddrList(l, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("netlink.AddrList() failed: %w", err)
	}
	addrs := make([]*net.IPNet, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, ip.IPNet)
	}
	return addrs, nil
}

// GetGateway returns the gateway for the given network with the lower metric
func (n *networkHelper) GetGateway(network *net.IPNet) (net.IP, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("netlink.RouteList() failed: %w", err)
	}

	var gateway net.IP
	// https://man7.org/linux/man-pages/man8/ip-route.8.html
	lowestPriority := math.MaxUint32 + 1
	for _, r := range routes {
		if r.Dst.String() == network.String() {
			if r.Priority < lowestPriority {
				lowestPriority = r.Priority
				gateway = r.Gw
			}
		}
	}

	if gateway == nil {
		return nil, errors.New("no gateway found")
	}

	return gateway, nil
}
