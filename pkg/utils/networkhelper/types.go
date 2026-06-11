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
	"net"

	"github.com/k8snetworkplumbingwg/sriovnet"
	"github.com/vishvananda/netlink"
)

// NetworkHelper is a helper that can be used to configure networking related settings on the system.

//go:generate mockgen -copyright_file ../../../hack/boilerplate.go.txt -destination mock/networkhelper.go -source types.go
type NetworkHelper interface {
	// SetLinkUp sets the administrative state of a link to "up"
	SetLinkUp(link string) error
	// SetLinkDown sets the administrative state of a link to "down"
	SetLinkDown(link string) error
	// RenameLink renames a link to the given name
	RenameLink(link string, newName string) error
	// SetLinkIPAddress sets the IP address of a link
	SetLinkIPAddress(link string, ipNet *net.IPNet) error
	// LinkIPAddressExists checks whether a link has the given IP.
	LinkIPAddressExists(link string, ipNet *net.IPNet) (bool, error)
	// DeleteLinkIPAddress deletes the given IP of a link
	DeleteLinkIPAddress(link string, ipNet *net.IPNet) error
	// DeleteNeighbor deletes a neighbor
	DeleteNeighbor(ip net.IP, device string) error
	// NeighborExists checks whether an neighbor entry exists
	NeighborExists(ip net.IP, device string) (bool, error)
	// AddRoute adds a route
	AddRoute(network *net.IPNet, gateway net.IP, device string, metric *int, table *int) error
	// DeleteRoute deletes a route
	DeleteRoute(network *net.IPNet, gateway net.IP, device string) error
	// RouteExists checks whether a route exists
	RouteExists(network *net.IPNet, gateway net.IP, device string, table *int) (bool, error)
	// RouteList returns IPv4 routes for device.
	// When table is non-nil, only routes in that routing table are returned.
	RouteList(device string, table *int) ([]netlink.Route, error)
	// AddDummyLink adds a dummy link
	AddDummyLink(link string) error
	// DummyLinkExists checks if a dummy link exists
	DummyLinkExists(link string) (bool, error)
	// LinkExists checks if a link exists
	LinkExists(link string) (bool, error)
	// GetHostPFMACAddressDPU returns the MAC address of the Host PF identified by pfID provided as input.
	// pfID is either "0" or "1". This function should only be called on the DPU.
	GetHostPFMACAddressDPU(pfID string) (net.HardwareAddr, error)
	// GetLinkIPAddresses returns the IP addresses of a link
	GetLinkIPAddresses(link string) ([]*net.IPNet, error)
	// GetGateway returns the gateway for the given network with the lower metric
	GetGateway(network *net.IPNet) (net.IP, error)
	// AddRule adds a rule in the routing policy database that controls the route selection algorithm
	AddRule(src *net.IPNet, table int, priority int) error
	// RuleExists checks whether a rule exists in the routing policy database that controls the route selection algorithm
	RuleExists(src *net.IPNet, table int, priority int) (bool, error)
	// GetPFRepresentorDPU returns PF representor on DPU for a host PF identified by its ID.
	GetPFRepresentorDPU(pfID string) (string, error)
	// GetVFRepresentorDPU returns VF representor on DPU for a host VF identified by pfID and vfIndex
	GetVFRepresentorDPU(pfID, vfIndex string) (string, error)
	// GetUplinkRepresentor gets a VF or PF PCI address (e.g '0000:03:00.4') and
	// returns the uplink representor netdev name for that VF or PF.
	GetUplinkRepresentor(pciAddress string) (string, error)
	// DevlinkPortList returns the list of devlink ports in the system
	DevlinkPortList() ([]*netlink.DevlinkPort, error)
	// GetVfRepresentorFromPortParams returns the VF representor netdev name for a given port parameters and VF index
	GetVfRepresentorFromPortParams(pp *sriovnet.RepresentorPortParams, vfIndex uint32) (string, error)
	// GetPfRepresentorFromPortParams returns the PF representor netdev name for a given port parameters
	GetPfRepresentorFromPortParams(pp *sriovnet.RepresentorPortParams) (string, error)
}
