/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

package topology

//go:generate mockgen -copyright_file ../../hack/boilerplate.go.txt -package mock -destination mock/Manager.go . Manager

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ip"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/nbdb"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ovnlib"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology/iprange"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology/sets"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8sset "k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	routerPortType   = "router"
	routerPortOption = "router-port"

	// GRChassisKey is the key for the chassis option for the gateway routers.
	GRChassisKey = "chassis"
	// GRMACBindingAgeThresholdKey is the key for the mac_binding_age_threshold option for the gateway routers.
	GRMACBindingAgeThresholdKey = "mac_binding_age_threshold"
	// GRDynamicNeighRoutersKey is the key for the dynamic_neigh_routers option for the gateway routers.
	GRDynamicNeighRoutersKey = "dynamic_neigh_routers"
	// GRAlwaysLearnFromARPRequestKey is the key for the always_learn_from_arp_request option for the gateway routers.
	GRAlwaysLearnFromARPRequestKey = "always_learn_from_arp_request"

	// GRMACBindingAgeThreshold is the lifetime in seconds of each MAC binding
	// entry for the gateway routers. After this time, the entry is removed and
	// may be refreshed with a new ARP request.
	GRMACBindingAgeThreshold = "300"
)

type ServiceInterfacRef struct {
	ServiceInterface client.ObjectKey
	VirtualNetwork   client.ObjectKey
	VPC              client.ObjectKey
}

// Manager manages ovn logical topology
type Manager interface {
	// ApplyTopology applies the logical topology to OVN for the VPC virtual networks and nodes
	ApplyTopology(ctx context.Context, vpc *vpcv1.DPUVPC, networks []*vpcv1.DPUVirtualNetwork, nodes []*corev1.Node) error
	// RemoveTopology removes the logical topology from OVN for the VPC
	RemoveTopology(ctx context.Context, vpc *vpcv1.DPUVPC) error
	// PlugServiceInterface plugs a service interface into virtual network
	PlugServiceInterface(ctx context.Context, vpc *vpcv1.DPUVPC, vn *vpcv1.DPUVirtualNetwork, node *corev1.Node, si *dpuservicev1.ServiceInterface) error
	// UnplugServiceInterface unplugs a service interface from virtual network
	UnplugServiceInterface(ctx context.Context, vn *vpcv1.DPUVirtualNetwork, si *dpuservicev1.ServiceInterface) error
	// ListVPCs lists all VPCs in OVN and returns the associated DPUVPCs object keys
	ListVPCs(ctx context.Context) ([]client.ObjectKey, error)
	// ListServiceInterfaces lists all service interfaces in OVN and returns the associated ServiceInterfacRef
	ListServiceInterfaces(ctx context.Context) ([]ServiceInterfacRef, error)
}

// NewManager creates a new Manager instance
func NewManager(ovnclient ovnlib.OVNWrapper) Manager {
	return &manager{
		ovnclient: ovnclient,
	}
}

type manager struct {
	ovnclient ovnlib.OVNWrapper
}

// ApplyTopology applies the logical topology to OVN for the VPC, virtual networks and nodes
func (m *manager) ApplyTopology(ctx context.Context, vpc *vpcv1.DPUVPC, networks []*vpcv1.DPUVirtualNetwork, nodes []*corev1.Node) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Applying topology for VPC", "vpc", vpc.Name)

	// applyClustserSwitchAndRouter
	if err := m.applyClusterSwitchAndRouter(ctx, vpc); err != nil {
		return fmt.Errorf("failed to apply cluster switch and router: %w", err)
	}
	// applyNetworksSwitchAndRouter
	if err := m.applyNetworksSwitchAndRouter(ctx, vpc, networks); err != nil {
		return fmt.Errorf("failed to apply switch and router for virtual networks: %w", err)
	}

	// applyGatewaySwitchAndRouters (external connectivity) - ensure gateway router, switch and ports exist
	if err := m.applyGatewaySwitchAndRouters(ctx, vpc, nodes); err != nil {
		return fmt.Errorf("failed to apply gateway routers: %w", err)
	}

	// apply dhcp options for networks
	if err := m.applyDHCPOptionsForNetworks(ctx, vpc, networks); err != nil {
		return fmt.Errorf("failed to apply dhcp options for virtual networks: %w", err)
	}

	// applyRouterRoutes for all routers (vpc, network and gateways)
	if err := m.applyRouterRoutes(ctx, vpc, networks, nodes); err != nil {
		return fmt.Errorf("failed to apply routes for virtual networks: %w", err)
	}

	// applyRouterPolicies for all routers (vpc, network and gateways)
	if err := m.applyRouterPolicies(ctx, vpc, networks); err != nil {
		return fmt.Errorf("failed to apply policies for virtual networks: %w", err)
	}

	// applyGatewayRouterNATRules for all gateway routers
	if err := m.applyGatewayRouterNATRules(ctx, vpc, networks, nodes); err != nil {
		return fmt.Errorf("failed to apply gateway router NAT rules: %w", err)
	}

	return nil
}

// applyNetworksSwitchAndRouter ensures the network switch, router, related ports and DHCP options exist in OVN
// it will remove stale switches and routers from the topology as well
func (m *manager) applyNetworksSwitchAndRouter(ctx context.Context, vpc *vpcv1.DPUVPC, vns []*vpcv1.DPUVirtualNetwork) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Applying virtualnetwork switch and router for VPC", "vpc", vpc.Name)

	for _, vn := range vns {
		// network router
		routerName := common.VirtualNetworkRouterName(vn)
		routerPortMAC, routerPortNetworks, err := m.routerPortMACAndNetworksFromAnnotation(vn.Annotations)
		if err != nil {
			return fmt.Errorf("failed to get router port MAC and networks from VN %s annotation: %w", vn.Name, err)
		}
		err = m.applyLogicalRouter(ctx, routerName, NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn), nil)
		if err != nil {
			return fmt.Errorf("failed to apply logical router for VN %s: %w", vn.Name, err)
		}

		// connect network router to vpc switch
		if err = m.applyLogicalRouterToSwitchPorts(
			ctx, routerName, common.VPCSwitchName(vpc), routerPortMAC, routerPortNetworks.Networks(),
			NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn)); err != nil {
			return fmt.Errorf("failed to apply network router to vpc switch ports for VN %s: %w", vn.Name, err)
		}

		// network switch(s) and ports to network router
		if err := m.applyNetworkSwitch(ctx, vpc, vn); err != nil {
			return fmt.Errorf("failed to apply network switch(s) for VN %s: %w", vn.Name, err)
		}
	}

	// finally remove stale network switches and routers
	if err := m.removeStaleNetworksSwitchAndRouter(ctx, vpc, vns); err != nil {
		return fmt.Errorf("failed to remove stale virtualnetwork switches and routers for VPC %s: %w", vpc.Name, err)
	}

	return nil
}

// removeStaleNetworksSwitchAndRouter removes stale network logical switches and routers for the VPC
func (m *manager) removeStaleNetworksSwitchAndRouter(ctx context.Context, vpc *vpcv1.DPUVPC, vns []*vpcv1.DPUVirtualNetwork) error {
	// construct map of current owner refs for vpc and virtual networks
	currentOwnerRefs := k8sset.New[string]()

	for _, vn := range vns {
		vnOwnerRefVal := NewExternalIDs().WithOwnerRef(vn)[VPCOwnerRefKey]
		currentOwnerRefs.Insert(vnOwnerRefVal)
	}

	// remove stale network routers
	if err := m.removeStaleNetworksRouter(ctx, vpc, vpcv1.DPUVirtualNetworkKind, currentOwnerRefs); err != nil {
		return err
	}

	// remove stale network switches
	if err := m.removeStaleNetworksSwitch(ctx, vpc, vpcv1.DPUVirtualNetworkKind, currentOwnerRefs); err != nil {
		return err
	}

	// remove stale network switch ports from cluster switch
	if err := m.removeStaleClusterSwitchNetworkPorts(ctx, vpc, vpcv1.DPUVirtualNetworkKind, currentOwnerRefs); err != nil {
		return err
	}

	return nil
}

// removeStaleNetworksRouter removes stale network logical routers for the VPC.
// ownerKind is the Kind of owner for the router
// currentOwnerRefs is a map of current owner refs of kind ownerKind for the VPC and virtual networks.
//
//nolint:dupl
func (m *manager) removeStaleNetworksRouter(ctx context.Context, vpc *vpcv1.DPUVPC, ownerKind string, currentOwnerRefs k8sset.Set[string]) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Removing stale routers for VPC", "vpc", vpc.Name, "ownerKind", ownerKind)

	// get all logical routers for the VPC
	lrs, err := m.ovnclient.ListLogicalRouter(ctx, &nbdb.LogicalRouterListParams{ExternalIDs: NewExternalIDs().WithVPCRef(vpc)})
	if err != nil {
		return fmt.Errorf("failed to list logical routers for VPC %s: %w", vpc.Name, err)
	}

	// remove stale logical routers
	for _, lr := range lrs {
		if !strings.HasPrefix(lr.ExternalIDs[VPCOwnerRefKey], ownerKind) {
			continue
		}

		// if owner ref not in currentOwnerRefs, remove the router
		if !currentOwnerRefs.Has(lr.ExternalIDs[VPCOwnerRefKey]) {
			if err := m.ovnclient.DeleteLogicalRouter(ctx, &nbdb.LogicalRouterDeleteParams{UUID: lr.UUID}); err != nil {
				return fmt.Errorf("failed to delete logical router %s(%s): %w", lr.Name, lr.UUID, err)
			}
		}
	}

	return nil
}

// removeStaleNetworksSwitch removes stale network logical switches for the VPC
// ownerKind is the Kind of owner for the switch
// currentOwnerRefs is a map of current owner refs of kind ownerKind for the VPC and virtual networks.
//
//nolint:dupl
func (m *manager) removeStaleNetworksSwitch(ctx context.Context, vpc *vpcv1.DPUVPC, ownerKind string, currentOwnerRefs k8sset.Set[string]) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Removing stale switch for VPC", "vpc", vpc.Name, "ownerKind", ownerKind)

	// get all logical switches and routers and ports for the VPC
	lss, err := m.ovnclient.ListLogicalSwitch(ctx, &nbdb.LogicalSwitchListParams{ExternalIDs: NewExternalIDs().WithVPCRef(vpc)})
	if err != nil {
		return fmt.Errorf("failed to list logical switches for VPC %s: %w", vpc.Name, err)
	}

	// remove stale logical switches
	for _, ls := range lss {
		if !strings.HasPrefix(ls.ExternalIDs[VPCOwnerRefKey], ownerKind) {
			continue
		}
		// if owner ref not in currentOwnerRefs, remove the switch
		if !currentOwnerRefs.Has(ls.ExternalIDs[VPCOwnerRefKey]) {
			if err := m.ovnclient.DeleteLogicalSwitch(ctx, &nbdb.LogicalSwitchDeleteParams{UUID: ls.UUID}); err != nil {
				return fmt.Errorf("failed to delete logical switch %s(%s): %w", ls.Name, ls.UUID, err)
			}
		}
	}

	return nil
}

// removeStaleClusterRouterNetworkPorts removes stale switch ports from VPC switch
// ownerKind is the Kind of owner for the switch
// currentOwnerRefs is a map of current owner refs of kind ownerKind for the VPC and virtual networks.
func (m *manager) removeStaleClusterSwitchNetworkPorts(ctx context.Context, vpc *vpcv1.DPUVPC, ownerKind string, currentOwnerRefs k8sset.Set[string]) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Removing stale switch ports for VPC", "vpc", vpc.Name, "ownerKind", ownerKind)

	// remove stale logical switch ports that are associated with virtual network from cluster switch
	lsps, err := m.listLogicalSwitchPortsForSwitch(ctx, common.VPCSwitchName(vpc))
	if err != nil {
		return fmt.Errorf("failed to list logical switch ports for switch %s: %w", common.VPCSwitchName(vpc), err)
	}

	for _, lsp := range lsps {
		if !strings.HasPrefix(lsp.ExternalIDs[VPCOwnerRefKey], ownerKind) {
			continue
		}

		// if owner ref not in currentOwnerRefs, remove the port
		if !currentOwnerRefs.Has(lsp.ExternalIDs[VPCOwnerRefKey]) {
			if err := m.ovnclient.DeleteLogicalSwitchPort(ctx, &nbdb.LogicalSwitchGetParams{Name: common.VPCSwitchName(vpc)}, &nbdb.LogicalSwitchPortDeleteParams{UUID: lsp.UUID}); err != nil {
				return fmt.Errorf("failed to delete logical switch port from vpc switch %s(%s): %w", lsp.Name, lsp.UUID, err)
			}
		}
	}

	return nil
}

// listLogicalSwitchPortsForSwitch returns the logical switch ports for the given switch by name
func (m *manager) listLogicalSwitchPortsForSwitch(ctx context.Context, switchName string) ([]*nbdb.LogicalSwitchPort, error) {
	ls, err := m.ovnclient.GetLogicalSwitch(ctx, &nbdb.LogicalSwitchGetParams{Name: switchName})
	if err != nil {
		return nil, fmt.Errorf("failed to get logical switch %s: %w", switchName, err)
	}
	lsps := make([]*nbdb.LogicalSwitchPort, 0, len(ls.Ports))
	for _, portUUID := range ls.Ports {
		lsp, err := m.ovnclient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{UUID: portUUID})
		if err != nil {
			return nil, fmt.Errorf("failed to get logical switch port %s from switch %s: %w", portUUID, switchName, err)
		}
		lsps = append(lsps, lsp)
	}
	return lsps, nil
}

// applyGatewaySwitchAndRouters applies gateway switch and routers for the VPC and provided nodes
func (m *manager) applyGatewaySwitchAndRouters(ctx context.Context, vpc *vpcv1.DPUVPC, nodes []*corev1.Node) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Applying gateway routers for VPC", "vpc", vpc.Name)

	for _, node := range nodes {
		// apply gateway switch
		if err := m.applyLogicalSwitch(ctx, common.GatewaySwitchName(vpc, node), nil, nil, NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(node)); err != nil {
			return fmt.Errorf("failed to apply gateway switch for node %s: %w", node.Name, err)
		}

		// apply gateway router
		routerOpts := make(map[string]string)
		routerOpts[GRMACBindingAgeThresholdKey] = GRMACBindingAgeThreshold
		routerOpts[GRDynamicNeighRoutersKey] = "true"
		routerOpts[GRAlwaysLearnFromARPRequestKey] = "false"
		routerOpts[GRChassisKey] = node.Annotations[common.OVNChassisIDAnnotationKey]
		if routerOpts[GRChassisKey] == "" {
			return fmt.Errorf("failed to get chassis id for node %s, annotation %s is not set", node.Name, common.OVNChassisIDAnnotationKey)
		}

		if err := m.applyLogicalRouter(ctx, common.GatewayRouterName(vpc, node), NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(node), routerOpts); err != nil {
			return fmt.Errorf("failed to apply gateway router for node %s: %w", node.Name, err)
		}

		// gateway switch port to gateway router
		gatewayConfig, err := common.GatewayConfigFromAnnotation(node.Annotations)
		if err != nil {
			return fmt.Errorf("failed to get gateway config for node %s: %w", node.Name, err)
		}

		routerPortMACStr := gatewayConfig.MAC
		routerPortMAC, err := net.ParseMAC(routerPortMACStr)
		if err != nil {
			return fmt.Errorf("failed to parse gw router port mac %s for node %s: %w", routerPortMACStr, node.Name, err)
		}

		extPortNetworks := []*net.IPNet{}
		extIP4, extIPNet4, err := net.ParseCIDR(gatewayConfig.IP.IPv4)
		if err != nil {
			return fmt.Errorf("failed to parse gw router port external network %s for node %s: %w", gatewayConfig.IP.IPv4, node.Name, err)
		}
		extIPNet4.IP = extIP4
		extPortNetworks = append(extPortNetworks, extIPNet4)

		// TODO(adrianc): do the same for IPV6 network

		if err := m.applyLogicalRouterToSwitchPorts(
			ctx,
			common.GatewayRouterName(vpc, node),
			common.GatewaySwitchName(vpc, node),
			routerPortMAC,
			extPortNetworks,
			NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(node)); err != nil {
			return fmt.Errorf("failed to apply gateway router to switch ports for node %s: %w", node.Name, err)
		}

		// gateway router port to vpc switch
		routerPortMAC, routerPortNetworkCfg, err := m.routerPortMACAndNetworksFromAnnotation(node.Annotations)
		if err != nil {
			return fmt.Errorf("failed to get router port MAC and networks from node %s annotation: %w", node.Name, err)
		}

		if err := m.applyLogicalRouterToSwitchPorts(
			ctx,
			common.GatewayRouterName(vpc, node),
			common.VPCSwitchName(vpc),
			routerPortMAC,
			routerPortNetworkCfg.Networks(),
			NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(node)); err != nil {
			return fmt.Errorf("failed to apply gateway router to vpc switch ports for node %s: %w", node.Name, err)
		}

		// localnet switch port
		localNetPort := &nbdb.LogicalSwitchPort{
			Name:        common.GatewaySwitchLocalnetPortName(common.GatewaySwitchName(vpc, node)),
			ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(node),
			Type:        "localnet",
			Addresses:   []string{"unknown"},
			Options: map[string]string{
				"network_name": common.LocalnetNetworkName(node),
			},
		}

		if err := m.applyLogicalSwitchPort(
			ctx,
			common.GatewaySwitchName(vpc, node),
			localNetPort); err != nil {
			return fmt.Errorf("failed to apply localnet switch port for node %s: %w", node.Name, err)
		}

	}

	// remove stale gateway routers, switches and ports in vpc switch
	if err := m.removeStaleGatewaySwitchAndRouters(ctx, vpc, nodes); err != nil {
		return fmt.Errorf("failed to remove stale gateway switch and routers for VPC %s: %w", vpc.Name, err)
	}

	return nil
}

// removeStaleGatewaySwitchAndRouters removes stale gateway routers, switches and ports toward vpc switch
func (m *manager) removeStaleGatewaySwitchAndRouters(ctx context.Context, vpc *vpcv1.DPUVPC, nodes []*corev1.Node) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Removing stale gateway routers, switches for VPC", "vpc", vpc.Name)

	// construct map of current owner refs for vpc and virtual networks
	currentOwnerRefs := k8sset.New[string]()
	for _, node := range nodes {
		nodeOwnerRefVal := NewExternalIDs().WithOwnerRef(node)[VPCOwnerRefKey]
		currentOwnerRefs.Insert(nodeOwnerRefVal)
	}

	nodeKind := "Node"
	// remove stale gateway routers
	if err := m.removeStaleNetworksRouter(ctx, vpc, nodeKind, currentOwnerRefs); err != nil {
		return fmt.Errorf("failed to remove stale gateway routers for VPC %s: %w", vpc.Name, err)
	}

	// remove stale gateway switches
	if err := m.removeStaleNetworksSwitch(ctx, vpc, nodeKind, currentOwnerRefs); err != nil {
		return fmt.Errorf("failed to remove stale gateway switches for VPC %s: %w", vpc.Name, err)
	}

	// remove stale network switch ports from cluster switch
	if err := m.removeStaleClusterSwitchNetworkPorts(ctx, vpc, nodeKind, currentOwnerRefs); err != nil {
		return fmt.Errorf("failed to remove stale network switch ports from cluster switch for VPC %s: %w", vpc.Name, err)
	}

	return nil
}

// applyDHCPOptionsForNetworks ensures the DHCP options exist in OVN for the virtual networks that have dhcp specified
func (m *manager) applyDHCPOptionsForNetworks(ctx context.Context, vpc *vpcv1.DPUVPC, vns []*vpcv1.DPUVirtualNetwork) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Applying DHCP options for virtualnetworks", "vpc", vpc.Name)

	for _, vn := range vns {
		// dhcp options for network
		if vn.Spec.BridgedNetwork.IPAM != nil &&
			vn.Spec.BridgedNetwork.IPAM.IPv4 != nil && vn.Spec.BridgedNetwork.IPAM.IPv4.DHCP {
			_, ipn, err := net.ParseCIDR(vn.Spec.BridgedNetwork.IPAM.IPv4.Subnet)
			if err != nil {
				return fmt.Errorf("failed to parse ipv4 virtual network %s subnet %s: %w",
					vn.Name, vn.Spec.BridgedNetwork.IPAM.IPv4.Subnet, err)
			}
			if err = m.applyLogicalDHCPOptions(ctx, ipn, NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn)); err != nil {
				return fmt.Errorf("failed to apply dhcp options for network %s: %w", vn.Name, err)
			}
		}
	}

	// finally remove stale dhcp options
	if err := m.removeStaleDHCPOptionsForNetworks(ctx, vpc, vns); err != nil {
		return fmt.Errorf("failed to remove stale dhcp options for VPC %s: %w", vpc.Name, err)
	}

	return nil
}

// removeStaleDHCPOptionsForNetworks removes stale DHCP options for the VPC
func (m *manager) removeStaleDHCPOptionsForNetworks(ctx context.Context, vpc *vpcv1.DPUVPC, vns []*vpcv1.DPUVirtualNetwork) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Removing stale DHCP options for VPC", "vpc", vpc.Name)

	// get all DHCP options for the VPC
	dhcpOpts, err := m.ovnclient.ListDhcpOptions(ctx, &nbdb.DHCPOptionsListParams{ExternalIDs: NewExternalIDs().WithVPCRef(vpc)})
	if err != nil {
		return fmt.Errorf("failed to list DHCP options for VPC %s: %w", vpc.Name, err)
	}

	// construct map of virtual network owner refs (possible owners of dhcp options)
	vnRefs := k8sset.New[string]()
	for _, vn := range vns {
		vnOwnerRefVal := NewExternalIDs().WithOwnerRef(vn)[VPCOwnerRefKey]
		vnRefs.Insert(vnOwnerRefVal)
	}

	// remove stale logical switches
	for _, dhcpOpt := range dhcpOpts {
		// skip non virtual network owner refs
		if !strings.HasPrefix(dhcpOpt.ExternalIDs[VPCOwnerRefKey], vpcv1.DPUVirtualNetworkKind) {
			continue
		}
		// if owner ref not in vnRefs, remove the dhcp option
		if !vnRefs.Has(dhcpOpt.ExternalIDs[VPCOwnerRefKey]) {
			if err := m.ovnclient.DeleteDhcpOptions(ctx, &nbdb.DHCPOptionsDeleteParams{UUID: dhcpOpt.UUID}); err != nil {
				return fmt.Errorf("failed to delete dhcp option %s(%s): %w", dhcpOpt.UUID, dhcpOpt.Cidr, err)
			}
		}
	}

	return nil
}

// applyNetworkSwitch creates network switch(s) based on the virtual network type and IPAM configuration
// then connects them to the network router. the first IP in the subnet is used as the network router port
func (m *manager) applyNetworkSwitch(ctx context.Context, vpc *vpcv1.DPUVPC, vn *vpcv1.DPUVirtualNetwork) error {
	if vn.Spec.Type != vpcv1.BridgedVirtualNetworkType {
		//TODO(adrianc): handle L3 topology here, i.e create switch for each dpu that belongs to the network
		return fmt.Errorf("unsupported virtual network type %s", vn.Spec.Type)
	}

	// applyNetworkSwitchBridged
	// NOTE(adrianc): either ipv4 dhcp or ipv6 prefix should be set in virtual network
	var (
		err                       error
		switchSubnet4             *net.IPNet
		networkRouterPortMAC      net.HardwareAddr
		networkRouterPortNetworks []*net.IPNet
		excludeIPs                []iprange.IPRange
	)

	if vn.Spec.BridgedNetwork.IPAM == nil {
		return fmt.Errorf("missing IPAM configuration for virtual network %s", vn.Name)
	}

	if vn.Spec.BridgedNetwork.IPAM.IPv4 != nil {
		_, ipn, err := net.ParseCIDR(vn.Spec.BridgedNetwork.IPAM.IPv4.Subnet)
		if err != nil {
			return fmt.Errorf("failed to parse ipv4 virtual network %s subnet %s: %w", vn.Name, vn.Spec.BridgedNetwork.IPAM.IPv4.Subnet, err)
		}
		firstIP := ip.NextIP(ipn.IP)
		if firstIP == nil {
			return fmt.Errorf("failed to get first IP for subnet %s", ipn)
		}
		ipn.IP = firstIP
		networkRouterPortNetworks = append(networkRouterPortNetworks, ipn)
		networkRouterPortMAC = common.IPtoMAC(ipn.IP)

		if vn.Spec.BridgedNetwork.IPAM.IPv4.DHCP {
			switchSubnet4 = ipn
		}
	}

	if switchSubnet4 != nil {
		// this means we got dhcp enabled, need to set exclude ips

		// exclude logical router port IPs in this subnet
		for _, n := range networkRouterPortNetworks {
			excludeIPs = append(excludeIPs, iprange.IPRangeFromIP(n.IP))
		}

		// exclude user provided IPs
		if vn.Spec.BridgedNetwork.IPAM.IPv4.ExcludeIPs != nil {
			ipr, err := iprange.IPRangeFromExcludeIPsSpec(vn.Spec.BridgedNetwork.IPAM.IPv4.ExcludeIPs, switchSubnet4)
			if err != nil {
				return fmt.Errorf("failed to parse exclude IPs for virtual network %s: %w", vn.Name, err)
			}
			excludeIPs = append(excludeIPs, ipr...)
		}
	}

	if err = m.applyLogicalSwitch(
		ctx,
		common.VirtualNetworkSwitchName(vn),
		switchSubnet4,
		excludeIPs,
		NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn)); err != nil {
		return fmt.Errorf("failed to apply logical switch for VN %s: %w", vn.Name, err)
	}

	if err = m.applyLogicalRouterToSwitchPorts(
		ctx,
		common.VirtualNetworkRouterName(vn),
		common.VirtualNetworkSwitchName(vn),
		networkRouterPortMAC,
		networkRouterPortNetworks,
		NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn)); err != nil {
		return fmt.Errorf("failed to apply network router to network switch ports for VN %s: %w", vn.Name, err)
	}

	return nil
}

// applyClusterSwitchAndRouter ensures the VPC switch, router and related ports exist in OVN
func (m *manager) applyClusterSwitchAndRouter(ctx context.Context, vpc *vpcv1.DPUVPC) error {
	var err error

	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Applying cluster switch and router for VPC", "vpc", vpc.Name)

	vpcSwitchName := common.VPCSwitchName(vpc)
	vpcRouterName := common.VPCRouterName(vpc)

	// get router port MAC and networks
	vpcRouterPortMAC, vpcRouterPortNetworks, err := m.routerPortMACAndNetworksFromAnnotation(vpc.Annotations)
	if err != nil {
		return fmt.Errorf("failed to get router port MAC and networks from VPC %s annotation: %w", vpc.Name, err)
	}

	// ensure vpc switch exists or create it
	if err := m.applyLogicalSwitch(ctx, vpcSwitchName, nil, nil, NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vpc)); err != nil {
		return fmt.Errorf("failed to apply logical switch for VPC %s, %w", vpc.Name, err)
	}

	// ensure vpc router exists or create it
	if err := m.applyLogicalRouter(ctx, vpcRouterName, NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vpc), nil); err != nil {
		return fmt.Errorf("failed to apply logical router for VPC %s, %w", vpc.Name, err)
	}

	// ensure vpc router & switch ports exists or create it
	if err := m.applyLogicalRouterToSwitchPorts(
		ctx,
		vpcRouterName,
		vpcSwitchName,
		vpcRouterPortMAC,
		vpcRouterPortNetworks.Networks(),
		NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vpc)); err != nil {
		return fmt.Errorf("failed to apply router to switch ports for VPC %s, %w", vpc.Name, err)
	}

	return nil
}

// applyLogicalRouter creates a logical router in OVN if it does not exist
// externalIDs are set as external_ids for the logical router
// options are set as the router options.
func (m *manager) applyLogicalRouter(ctx context.Context, routerName string, externalIDs map[string]string, options map[string]string) error {
	router, err := m.ovnclient.GetLogicalRouter(ctx, &nbdb.LogicalRouterGetParams{Name: routerName})
	if err != nil {
		if ovnlib.GetOvnErrorCodeFromError(err) != ovnlib.ErrNotFound {
			return fmt.Errorf("failed to get logical router %s: %w", routerName, err)
		}
		// logical router not found, create it
		router, err = m.ovnclient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name:        routerName,
			ExternalIDs: externalIDs,
			Options:     options})
		if err != nil {
			return fmt.Errorf("failed to create logical router %s: %w", routerName, err)
		}
	}

	// update logical router options
	_, err = m.ovnclient.UpdateLogicalRouterOptions(ctx, &nbdb.LogicalRouterGetParams{UUID: router.UUID}, options)
	if err != nil {
		return fmt.Errorf("failed to update logical router options for %s: %w", routerName, err)
	}
	return nil
}

// applyLogicalSwitch creates a logical switch in OVN if it does not exist.
// if subnet is not empty, it will set the subnet for the logical switch.
// if excludeIPs is not empty, it will set the exclude_ips for the logical switch.
// excludeIPs must be in the provided subnet.
// externalIDs are set as external_ids for the logical switch
func (m *manager) applyLogicalSwitch(ctx context.Context, switchName string, subnet *net.IPNet, excludeIPs []iprange.IPRange, externalIDs map[string]string) error {
	if len(excludeIPs) > 0 && subnet == nil {
		return fmt.Errorf("excludeIPs is set but subnet is empty")
	}

	_, err := m.ovnclient.GetLogicalSwitch(ctx, &nbdb.LogicalSwitchGetParams{Name: switchName})
	if err != nil {
		if ovnlib.GetOvnErrorCodeFromError(err) != ovnlib.ErrNotFound {
			return fmt.Errorf("failed to get logical switch %s: %w", switchName, err)
		}
		// logical switch not found, create it

		// create other_config
		oc := map[string]string{}
		if subnet != nil {
			oc["subnet"] = subnet.String()
		}
		if len(excludeIPs) > 0 {
			oc["exclude_ips"] = iprange.IPRangesString(excludeIPs)
		}

		_, err = m.ovnclient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{
			Name:        switchName,
			OtherConfig: oc,
			ExternalIDs: externalIDs,
		})
		if err != nil {
			return fmt.Errorf("failed to create logical switch %s: %w", switchName, err)
		}
	}
	return nil
}

// applyLogicalRouterToSwitchPorts connects between logical switch and logical router by creating
// logical router and switch ports in OVN if they do not exist.
// The logical switch port is connected to the logical router, designating it as its peer.
// externalIDs are set as external_ids for the logical router and switch ports
func (m *manager) applyLogicalRouterToSwitchPorts(ctx context.Context, routerName, switchName string, routerPortMAC net.HardwareAddr, routerPortNetworks []*net.IPNet, externalIDs map[string]string) error {
	routerPortName := common.RouterToSwitchPortName(routerName, switchName)
	switchPortName := common.SwitchToRouterPortName(switchName, routerName)

	routerNetworks := make([]string, 0, len(routerPortNetworks))
	for _, ipn := range routerPortNetworks {
		routerNetworks = append(routerNetworks, ipn.String())
	}

	currentLRP, err := m.ovnclient.GetLogicalRouterPort(ctx, &nbdb.LogicalRouterPortGetParams{Name: routerPortName})
	if err != nil {
		if ovnlib.GetOvnErrorCodeFromError(err) != ovnlib.ErrNotFound {
			return fmt.Errorf("failed to get logical router port %s: %w", routerPortName, err)
		}

		// logical router port not found, create it
		_, err = m.ovnclient.CreateLogicalRouterPort(
			ctx, &nbdb.LogicalRouterGetParams{Name: routerName}, &nbdb.LogicalRouterPort{
				Name:        routerPortName,
				MAC:         routerPortMAC.String(),
				Networks:    routerNetworks,
				ExternalIDs: externalIDs,
			})
		if err != nil {
			return fmt.Errorf("failed to create logical router port %s: %w", routerPortName, err)
		}
	} else {
		// LRP exists, check if mac and networks are the same
		if currentLRP.MAC != routerPortMAC.String() || !slices.Equal(currentLRP.Networks, routerNetworks) {
			// re-create the port
			if err := m.ovnclient.DeleteLogicalRouterPort(
				ctx,
				&nbdb.LogicalRouterGetParams{Name: routerName},
				&nbdb.LogicalRouterPortDeleteParams{UUID: currentLRP.UUID},
			); err != nil {
				return fmt.Errorf("failed to delete logical router port %s: %w", routerPortName, err)
			}

			_, err = m.ovnclient.CreateLogicalRouterPort(
				ctx, &nbdb.LogicalRouterGetParams{Name: routerName}, &nbdb.LogicalRouterPort{
					Name:        routerPortName,
					MAC:         routerPortMAC.String(),
					Networks:    routerNetworks,
					ExternalIDs: externalIDs,
				})
			if err != nil {
				return fmt.Errorf("failed to create logical router port %s: %w", routerPortName, err)
			}
		}
	}

	// switch port
	_, err = m.ovnclient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{Name: switchPortName})
	if err != nil {
		if ovnlib.GetOvnErrorCodeFromError(err) != ovnlib.ErrNotFound {
			return fmt.Errorf("failed to get logical switch port %s: %w", switchPortName, err)
		}
		// logical switch port not found, create it
		_, err = m.ovnclient.CreateLogicalSwitchPort(
			ctx, &nbdb.LogicalSwitchGetParams{Name: switchName}, &nbdb.LogicalSwitchPort{
				Name:        switchPortName,
				Type:        routerPortType,                                      // set type as router port
				Addresses:   []string{routerPortType},                            // set addresses to router so that they are obtained from the router port
				Options:     map[string]string{routerPortOption: routerPortName}, // set options:router-port to point to its peer router port
				ExternalIDs: externalIDs,
			})
		if err != nil {
			return fmt.Errorf("failed to create logical switch port %s: %w", switchPortName, err)
		}
	}

	return nil
}

// applyLogicalDHCPOptions creates logical dhcp options in OVN if they do not exist
// first IP of the CIDR is assumed to be the router IP
func (m *manager) applyLogicalDHCPOptions(ctx context.Context, cidr *net.IPNet, externalIDs map[string]string) error {
	dopts, err := m.ovnclient.ListDhcpOptions(ctx, &nbdb.DHCPOptionsListParams{ExternalIDs: externalIDs})
	if err != nil {
		return fmt.Errorf("failed to list dhcp options. %w", err)
	}

	for _, dopt := range dopts {
		if dopt.Cidr == cidr.String() {
			// dhcp options already exist
			return nil
		}
	}

	// first ip of the cidr is assumed to be the router IP
	routerIP := ip.NextIP(cidr.IP)
	if routerIP == nil {
		return fmt.Errorf("failed to get first ip from CIDR %s", cidr)
	}

	// create dhcp option
	options := make(map[string]string)
	options["lease_time"] = "3600"
	options["router"] = routerIP.String()
	options["server_id"] = routerIP.String()
	options["server_mac"] = common.IPtoMAC(routerIP).String()

	_, err = m.ovnclient.CreateDhcpOptions(ctx, &nbdb.DHCPOptions{Cidr: cidr.String(), ExternalIDs: externalIDs, Options: options})
	if err != nil {
		return fmt.Errorf("failed to create dhcp options. %w", err)
	}

	return nil
}

// RemoveTopology removes the logical topology from OVN for the VPC
func (m *manager) RemoveTopology(ctx context.Context, vpc *vpcv1.DPUVPC) error {
	extIDsForVPC := NewExternalIDs().WithVPCRef(vpc)
	// list all logical switches and routers with the VPC external ID and delete them
	// associated ports, routes, policies are deleted automatically
	lss, err := m.ovnclient.ListLogicalSwitch(ctx, &nbdb.LogicalSwitchListParams{ExternalIDs: extIDsForVPC})
	if err != nil {
		return fmt.Errorf("failed to list logical switches for VPC %s: %w", vpc.Name, err)
	}
	for _, ls := range lss {
		if err := m.ovnclient.DeleteLogicalSwitch(ctx, &nbdb.LogicalSwitchDeleteParams{UUID: ls.UUID}); err != nil {
			return fmt.Errorf("failed to delete logical switch %s: %w", ls.UUID, err)
		}
	}

	lrs, err := m.ovnclient.ListLogicalRouter(ctx, &nbdb.LogicalRouterListParams{ExternalIDs: extIDsForVPC})
	if err != nil {
		return fmt.Errorf("failed to list logical routers for VPC %s: %w", vpc.Name, err)
	}
	for _, lr := range lrs {
		if err := m.ovnclient.DeleteLogicalRouter(ctx, &nbdb.LogicalRouterDeleteParams{UUID: lr.UUID}); err != nil {
			return fmt.Errorf("failed to delete logical router %s: %w", lr.UUID, err)
		}
	}

	// list all dhcp options for VPC and delete them
	dhcpOpts, err := m.ovnclient.ListDhcpOptions(ctx, &nbdb.DHCPOptionsListParams{ExternalIDs: extIDsForVPC})
	if err != nil {
		return fmt.Errorf("failed to list dhcp options for VPC %s: %w", vpc.Name, err)
	}
	for _, dhcpOpt := range dhcpOpts {
		if err := m.ovnclient.DeleteDhcpOptions(ctx, &nbdb.DHCPOptionsDeleteParams{UUID: dhcpOpt.UUID}); err != nil {
			return fmt.Errorf("failed to delete dhcp options %s: %w", dhcpOpt.UUID, err)
		}
	}

	return nil
}

// networkConfig is a struct that contains the IPV4 and IPV6 network configurations
type networkConfig struct {
	IPV4 *net.IPNet
	IPV6 *net.IPNet
}

// Networks returns a list of networks for the network config
func (n *networkConfig) Networks() []*net.IPNet {
	networks := make([]*net.IPNet, 0, 2)
	if n.IPV4 != nil {
		networks = append(networks, n.IPV4)
	}
	if n.IPV6 != nil {
		networks = append(networks, n.IPV6)
	}
	return networks
}

// routerPortMACAndNetworksFromAnnotation returns the router port MAC and networks from the provided annotations
func (m *manager) routerPortMACAndNetworksFromAnnotation(annotations map[string]string) (net.HardwareAddr, *networkConfig, error) {
	// get router port MAC and networks
	ipn4, ipn6, err := common.NetworksFromLRPAddressAnnotation(annotations)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get router port networks annotation: %w", err)
	}
	vpcRouterPortMAC := common.IPtoMAC(ipn4.IP)
	if vpcRouterPortMAC == nil {
		return nil, nil, fmt.Errorf("failed to derrive router port MAC from IP %s", ipn4.IP)
	}
	netCfg := &networkConfig{}
	netCfg.IPV4 = ipn4
	if ipn6 != nil {
		netCfg.IPV6 = ipn6
	}

	return vpcRouterPortMAC, netCfg, nil
}

// applyRouterRoutes applies static routes for VPC, virtual networks and gateway routers
func (m *manager) applyRouterRoutes(ctx context.Context, vpc *vpcv1.DPUVPC, vns []*vpcv1.DPUVirtualNetwork, nodes []*corev1.Node) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Applying router routes for VPC", "vpc", vpc.Name)

	// apply routes for vpc router
	var err error
	var desiredRouteSet sets.RouteSet

	if desiredRouteSet, err = m.getDesiredRoutesForVPCRouter(vpc, vns); err != nil {
		return fmt.Errorf("failed to get desired routes for vpc router: %w", err)
	}
	if err := m.applyRoutesForRouter(ctx, common.VPCRouterName(vpc), desiredRouteSet); err != nil {
		return fmt.Errorf("failed to apply routes for router %s: %w", common.VPCRouterName(vpc), err)
	}

	// apply routes for network routers
	for _, vn := range vns {
		desiredRouteSet, err = m.getDesiredRoutesForNetworkRouter(vpc, vn)
		if err != nil {
			return fmt.Errorf("failed to get desired routes for network router: %w", err)
		}

		if err := m.applyRoutesForRouter(ctx, common.VirtualNetworkRouterName(vn), desiredRouteSet); err != nil {
			return fmt.Errorf("failed to apply routes for router %s: %w", common.VirtualNetworkRouterName(vn), err)
		}
	}

	// apply routes for gateway routers
	for _, node := range nodes {
		desiredRouteSet, err = m.getDesiredRoutesForGatewayRouter(vpc, vns, node)
		if err != nil {
			return fmt.Errorf("failed to get desired routes for gateway router %s: %w", common.GatewayRouterName(vpc, node), err)
		}

		if err := m.applyRoutesForRouter(ctx, common.GatewayRouterName(vpc, node), desiredRouteSet); err != nil {
			return fmt.Errorf("failed to apply routes for gateway router %s: %w", common.GatewayRouterName(vpc, node), err)
		}
	}

	return nil
}

// applyRoutesForRouter applies desired routes to routerName. it removes any routes not in desired set.
func (m *manager) applyRoutesForRouter(ctx context.Context, routerName string, desired sets.RouteSet) error {
	// get routes for router
	router, err := m.ovnclient.GetLogicalRouter(ctx, &nbdb.LogicalRouterGetParams{Name: routerName})
	if err != nil {
		return fmt.Errorf("failed to get logical router %s: %w", routerName, err)
	}

	current := sets.NewRouteSet()
	for _, uid := range router.StaticRoutes {
		sr, err := m.ovnclient.GetLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterStaticRouteGetParams{UUID: uid})
		if err != nil {
			return fmt.Errorf("failed to get logical router static route %s: %w", uid, err)
		}
		current.Add(sr)
	}

	// calculate diff between desired and current routes
	routesToAdd := desired.Difference(current)
	routesToRemove := current.Difference(desired)

	// add new routes
	for _, r := range routesToAdd.List() {
		_, err := m.ovnclient.CreateLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterGetParams{Name: routerName}, r)
		if err != nil {
			return fmt.Errorf("failed to create logical router static route (%s,%s): %w", r.IPPrefix, r.Nexthop, err)
		}
	}

	// remove stale routes
	for _, r := range routesToRemove.List() {
		err := m.ovnclient.DeleteLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterGetParams{Name: routerName}, &nbdb.LogicalRouterStaticRouteDeleteParams{UUID: r.UUID})
		if err != nil {
			return fmt.Errorf("failed to delete logical router static route uuid %s: %w", r.UUID, err)
		}
	}

	return nil
}

// getDesiredRoutesForGatewayRouter returns the desired routes for the gateway router
func (m *manager) getDesiredRoutesForGatewayRouter(vpc *vpcv1.DPUVPC, vns []*vpcv1.DPUVirtualNetwork, node *corev1.Node) (sets.RouteSet, error) {
	rs := sets.NewRouteSet()

	_, networks, err := m.routerPortMACAndNetworksFromAnnotation(vpc.Annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to get router port MAC and networks from VPC %s annotation: %w", vpc.Name, err)
	}

	// per network routes to virtual network subnet via cluster router
	for _, vn := range vns {
		vnSubnet4 := vn.GetIPv4Subnet()
		if vnSubnet4 == "" {
			return nil, fmt.Errorf("missing IPAM configuration for virtual network %s", vn.Name)
		}

		// route traffic destined to virtual network subnet via the network's router port
		rs.Add(&nbdb.LogicalRouterStaticRoute{
			IPPrefix:    vnSubnet4,
			Nexthop:     networks.IPV4.IP.String(),
			ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn),
		})
	}

	gatewayConfig, err := common.GatewayConfigFromAnnotation(node.Annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to get gateway config for node %s: %w", node.Name, err)
	}

	extNextHopIP4 := net.ParseIP(gatewayConfig.NextHop.IPv4)
	if extNextHopIP4 == nil {
		return nil, fmt.Errorf("failed to parse gw router port nexthop %s for node %s", gatewayConfig.NextHop.IPv4, node.Name)
	}

	// TODO(adrianc): do the same for IPV6 networks

	// default route to nexthop router (egress to external)
	rs.Add(&nbdb.LogicalRouterStaticRoute{
		IPPrefix:    "0.0.0.0/0",
		Nexthop:     extNextHopIP4.String(),
		ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(node),
	})

	return rs, nil
}

func (m *manager) getDesiredRoutesForVPCRouter(vpc *vpcv1.DPUVPC, vns []*vpcv1.DPUVirtualNetwork) (sets.RouteSet, error) {
	rs := sets.NewRouteSet()

	// hack, dummy route for default to allow rerouting when needed (e.g when external is true)
	_, networks, err := m.routerPortMACAndNetworksFromAnnotation(vpc.Annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to get router port MAC and networks from VPC %s annotation: %w", vpc.Name, err)
	}

	//TODO(adrianc): exclude this IP from ip allocator so it does not get allocated (edgecase)
	dummy := ip.LastIP(networks.IPV4)
	rs.Add(&nbdb.LogicalRouterStaticRoute{
		IPPrefix:    "0.0.0.0/0",
		Nexthop:     dummy.String(),
		ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vpc),
	})

	//TODO(adrianc): do the same for ipv6 (when we add proper support for it)

	// per network routes:
	for _, vn := range vns {
		_, networks, err := m.routerPortMACAndNetworksFromAnnotation(vn.Annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to get router port MAC and networks from VN %s annotation: %w", vn.Name, err)
		}

		routerIPv4Net := networks.IPV4
		vnSubnet4 := vn.GetIPv4Subnet()
		if vnSubnet4 == "" {
			return nil, fmt.Errorf("missing IPAM configuration for virtual network %s", vn.Name)
		}

		//route network subnet traffic to network router port IPv4
		rs.Add(&nbdb.LogicalRouterStaticRoute{
			IPPrefix:    vnSubnet4,
			Nexthop:     routerIPv4Net.IP.String(),
			ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn),
		})
		// TODO(adrianc): ipv6 route
	}

	return rs, nil
}

// getDesiredRoutesForNetworkRouter returns the desired routes for the network router
func (m *manager) getDesiredRoutesForNetworkRouter(vpc *vpcv1.DPUVPC, vn *vpcv1.DPUVirtualNetwork) (sets.RouteSet, error) {
	desired := sets.NewRouteSet()

	// default route to VPC router port IP
	_, networks, err := m.routerPortMACAndNetworksFromAnnotation(vpc.Annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to get router port MAC and networks from VPC %s annotation: %w", vpc.Name, err)
	}

	desired.Add(&nbdb.LogicalRouterStaticRoute{
		IPPrefix:    "0.0.0.0/0",
		Nexthop:     networks.IPV4.IP.String(),
		ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn),
	})

	//TODO(adrianc): do the same for ipv6 (when we add proper support for it)
	return desired, nil
}

// applyRouterPolicies applies router policies for VPC and virtual networks
func (m *manager) applyRouterPolicies(ctx context.Context, vpc *vpcv1.DPUVPC, vns []*vpcv1.DPUVirtualNetwork) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Applying router policies for VPC", "vpc", vpc.Name)

	// apply policies for vpc router
	var err error
	var desiredPolicySet sets.PolicySet

	if desiredPolicySet, err = m.getDesiredPoliciesForVPCRouter(vpc, vns); err != nil {
		return fmt.Errorf("failed to get desired policies for vpc router: %w", err)
	}
	if err := m.applyPoliciesForRouter(ctx, common.VPCRouterName(vpc), desiredPolicySet, ignoreOwnerKindPolicyFilter(dpuservicev1.ServiceInterfaceKind)); err != nil {
		return fmt.Errorf("failed to apply policies for router %s: %w", common.VPCRouterName(vpc), err)
	}

	return nil
}

// PolicyFilter is a function that filters a given policy.
// return true if the policy should be kept regardless of its presence in desired set
type PolicyFilter func(p *nbdb.LogicalRouterPolicy) bool

// ignoreOwnerKindPolicyFilter returns a PolicyFilter that returns true if router policy owner ref is owned by the
// given kind
func ignoreOwnerKindPolicyFilter(ownerKind string) PolicyFilter {
	return func(p *nbdb.LogicalRouterPolicy) bool {
		return p.ExternalIDs != nil && strings.HasPrefix(p.ExternalIDs[VPCOwnerRefKey], ownerKind)
	}
}

// ignoreNonMatchingExternalIDsPolicyFilter returns a PolicyFilter that returns true if
// the provided ExternalIDs are not in the router policy externalIDs
func ignoreNonMatchingExternalIDsPolicyFilter(extIDs ExternalIDs) PolicyFilter {
	return func(p *nbdb.LogicalRouterPolicy) bool {
		if extIDs == nil {
			return false
		}

		if p.ExternalIDs == nil {
			return true
		}

		for k, v := range extIDs {
			if p.ExternalIDs[k] != v {
				return true
			}
		}
		return false
	}
}

// applyPoliciesForRouter applies desired policies to routerName. it removes any policies not in desired set.
// if filter is provided and returns true, a policy will be kept regardless of its presence in desired set.
func (m *manager) applyPoliciesForRouter(ctx context.Context, routerName string, desired sets.PolicySet, filter PolicyFilter) error {
	// get policies for router
	router, err := m.ovnclient.GetLogicalRouter(ctx, &nbdb.LogicalRouterGetParams{Name: routerName})
	if err != nil {
		return fmt.Errorf("failed to get logical router %s: %w", routerName, err)
	}

	current := sets.NewPolicySet()
	for _, uid := range router.Policies {
		sr, err := m.ovnclient.GetLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyGetParams{UUID: uid})
		if err != nil {
			return fmt.Errorf("failed to get logical router policy %s: %w", uid, err)
		}
		if filter != nil && filter(sr) {
			// skip this policy so it may be preserved
			continue
		}

		current.Add(sr)
	}

	// calculate diff between desired and current policies
	policiesToAdd := desired.Difference(current)
	policiesToRemove := current.Difference(desired)

	// add new policies
	for _, p := range policiesToAdd.List() {
		_, err := m.ovnclient.CreateLogicalRouterPolicy(ctx, &nbdb.LogicalRouterGetParams{Name: routerName}, p)
		if err != nil {
			return fmt.Errorf("failed to create logical router policy (match=%s ,action=%s): %w", p.Match, p.Action, err)
		}
	}

	// remove stale policies
	for _, p := range policiesToRemove.List() {
		err := m.ovnclient.DeleteLogicalRouterPolicy(ctx, &nbdb.LogicalRouterGetParams{Name: routerName}, &nbdb.LogicalRouterPolicyDeleteParams{UUID: p.UUID})
		if err != nil {
			return fmt.Errorf("failed to delete logical router polict uuid %s: %w", p.UUID, err)
		}
	}

	return nil
}

// getDesiredPoliciesForVPCRouter returns the desired policies for the VPC router
func (m *manager) getDesiredPoliciesForVPCRouter(vpc *vpcv1.DPUVPC, vns []*vpcv1.DPUVirtualNetwork) (sets.PolicySet, error) {
	desired := sets.NewPolicySet()

	// default drop policy at prio 10
	desired.Add(&nbdb.LogicalRouterPolicy{
		Priority:    10,
		Match:       "1", // matchall
		Action:      nbdb.LogicalRouterPolicyActionDrop,
		ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vpc),
	})

	// allow or drop traffic between virtual networks at prio 200
	action := nbdb.LogicalRouterPolicyActionDrop
	if vpc.Spec.InterNetworkAccess {
		action = nbdb.LogicalRouterPolicyActionAllow
	}

	for _, srcVn := range vns {
		srcSubnet4 := srcVn.GetIPv4Subnet()
		if srcSubnet4 == "" {
			return nil, fmt.Errorf("failed to get ipv4 subnet for virtual network %s", srcVn.Name)
		}

		for _, dstVn := range vns {
			if srcVn.Name == dstVn.Name {
				continue
			}
			// ipv4
			dstSubnet4 := dstVn.GetIPv4Subnet()
			if dstSubnet4 == "" {
				return nil, fmt.Errorf("failed to get ipv4 subnet for virtual network %s", dstVn.Name)
			}
			desired.Add(&nbdb.LogicalRouterPolicy{
				Priority:    200,
				Match:       fmt.Sprintf("ip4.src == %s && ip4.dst == %s", srcSubnet4, dstSubnet4),
				Action:      action,
				ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(srcVn),
			})
			// TODO(adrianc): add ipv6
		}
	}

	// allow traffic from external to virtual networks at prio 100 if external connectivity enabled
	for _, vn := range vns {
		if !vn.Spec.ExternallyRouted {
			continue
		}

		vnSubnet4 := vn.GetIPv4Subnet()
		if vnSubnet4 == "" {
			return nil, fmt.Errorf("failed to get ipv4 subnet for virtual network %s", vn.Name)
		}

		desired.Add(&nbdb.LogicalRouterPolicy{
			Priority:    100,
			Match:       fmt.Sprintf("ip4.dst == %s", vnSubnet4),
			Action:      nbdb.LogicalRouterPolicyActionAllow,
			ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn),
		})
	}
	return desired, nil
}

// applyGatewayRouterNATRules applies NAT rules for gateway routers
func (m *manager) applyGatewayRouterNATRules(ctx context.Context, vpc *vpcv1.DPUVPC, vns []*vpcv1.DPUVirtualNetwork, nodes []*corev1.Node) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Applying NAT rules for gateway routers", "vpc", vpc.Name)

	vnsWithMasquerade := virtualNetworksWithMasquerade(vns)

	for _, node := range nodes {
		// Apply NAT rules for each gateway router
		gatewayConfig, err := common.GatewayConfigFromAnnotation(node.Annotations)
		if err != nil {
			return fmt.Errorf("failed to get gateway config for node %s: %w", node.Name, err)
		}

		extIP4, _, err := net.ParseCIDR(gatewayConfig.IP.IPv4)
		if err != nil {
			return fmt.Errorf("failed to parse gw router port external network %s for node %s: %w", gatewayConfig.IP.IPv4, node.Name, err)
		}

		expected := sets.NewNATSet()
		for _, vn := range vnsWithMasquerade {
			expected.Add(&nbdb.NAT{
				ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn),
				Type:        nbdb.NATTypeSNAT,
				LogicalIP:   vn.GetIPv4Subnet(),
				ExternalIP:  extIP4.String(),
			})
		}

		if err := m.applyNATForRouter(ctx, common.GatewayRouterName(vpc, node), expected); err != nil {
			return fmt.Errorf("failed to apply NAT rules for router %s: %w", common.GatewayRouterName(vpc, node), err)
		}
	}

	return nil
}

// getNATEntriesForRouter returns NAT entries for the given router
func (m *manager) getNATEntriesForRouter(ctx context.Context, routerName string) ([]*nbdb.NAT, error) {
	// get LogicalRouter NAT uuids
	router, err := m.ovnclient.GetLogicalRouter(ctx, &nbdb.LogicalRouterGetParams{Name: routerName})
	if err != nil {
		return nil, fmt.Errorf("failed to get logical router %s: %w", routerName, err)
	}

	natEntriesForRouter := make([]*nbdb.NAT, 0, len(router.Nat))
	for _, natUUID := range router.Nat {
		nat, err := m.ovnclient.GetLogicalRouterNat(ctx, &nbdb.NatGetParams{UUID: natUUID})
		if err != nil {
			return nil, fmt.Errorf("failed to get logical router %s NAT %s: %w", routerName, natUUID, err)
		}
		natEntriesForRouter = append(natEntriesForRouter, nat)
	}

	return natEntriesForRouter, nil
}

// applyNATForRouter applies a NAT rules to a logical router.
// if NAT rules do not exist, they will be created.
// if NAT rules exists that are not in expected set they will be removed.
func (m *manager) applyNATForRouter(ctx context.Context, routerName string, expected sets.NATSet) error {
	current := sets.NewNATSet()
	entries, err := m.getNATEntriesForRouter(ctx, routerName)
	if err != nil {
		return fmt.Errorf("failed to get NAT entries for router %s: %w", routerName, err)
	}
	current.Add(entries...)

	toAdd := expected.Difference(current)
	toRemove := current.Difference(expected)

	// add NAT rules
	for _, nat := range toAdd.List() {
		_, err = m.ovnclient.CreateLogicalRouterNat(ctx, &nbdb.LogicalRouterGetParams{Name: routerName}, nat)
		if err != nil {
			return fmt.Errorf("failed to create logical router NAT. type=%s, externalIP=%s, logicalIP=%s: %w",
				nat.Type, nat.ExternalIP, nat.LogicalIP, err)
		}
	}

	// remove stale NAT rules
	for _, nat := range toRemove.List() {
		err := m.ovnclient.DeleteLogicalRouterNat(ctx, &nbdb.LogicalRouterGetParams{Name: routerName}, &nbdb.NatDeleteParams{UUID: nat.UUID})
		if err != nil {
			return fmt.Errorf("failed to delete logical router NAT(%s): %w", nat.UUID, err)
		}
	}

	return nil
}

// PlugServiceInterface plugs a service interface into virtual network
func (m *manager) PlugServiceInterface(ctx context.Context, vpc *vpcv1.DPUVPC, vn *vpcv1.DPUVirtualNetwork, node *corev1.Node, si *dpuservicev1.ServiceInterface) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Plugging service interface into virtual network", "vpc", vpc.Name, "vn", vn.Name, "si", si.Name)

	mac, err := common.LSPMACAddressFromAnnotation(si.Annotations)
	if err != nil {
		return fmt.Errorf("failed to get MAC address for service interface %s: %w", si.Name, err)
	}

	// add dhcp4 options if enabled
	dhcp4Enabled := vn.Spec.Type == vpcv1.BridgedVirtualNetworkType &&
		vn.Spec.BridgedNetwork != nil &&
		vn.Spec.BridgedNetwork.IPAM != nil &&
		vn.Spec.BridgedNetwork.IPAM.IPv4 != nil &&
		vn.Spec.BridgedNetwork.IPAM.IPv4.DHCP

	port := &nbdb.LogicalSwitchPort{
		Name:        common.ServiceInterfacePortName(si),
		ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithVirtualNetworkRef(vn).WithOwnerRef(si),
	}

	// set port.Address field:
	// 	1. no mac address provided use unknown address
	//	2. if dhcp is enabled use dynamic address
	switch {
	case mac == nil:
		port.Addresses = []string{"unknown"}
	case !dhcp4Enabled:
		port.Addresses = []string{mac.String()}
	default:
		port.Addresses = []string{fmt.Sprintf("%s dynamic", mac.String())}
	}

	// we dont set dhcp options if mac is nil ("unknown")
	if dhcp4Enabled && mac != nil {
		// get dhcp options for network
		dhcpOpts, err := m.ovnclient.ListDhcpOptions(ctx, &nbdb.DHCPOptionsListParams{
			ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithOwnerRef(vn),
		})
		if err != nil {
			return fmt.Errorf("failed to get dhcp options for virtual network %s: %w", vn.Name, err)
		}
		if len(dhcpOpts) == 0 {
			return fmt.Errorf("no dhcp options found for virtual network %s", vn.Name)
		}
		if len(dhcpOpts) > 1 {
			return fmt.Errorf("multiple dhcp options found for virtual network %s", vn.Name)
		}

		// set dhcp options on lsp (TODO(adrianc): need to handle ipv6)
		port.Dhcpv4Options = &dhcpOpts[0].UUID
	}

	// applyLSP
	if err := m.applyLogicalSwitchPort(ctx, common.VirtualNetworkSwitchName(vn), port); err != nil {
		return fmt.Errorf("failed to apply logical switch port %s: %w", port.Name, err)
	}

	if !dhcp4Enabled || mac == nil {
		return nil
	}

	// interface needs to get a dynamic address from OVN.

	// wait for dynamic address to be populated
	var curPort *nbdb.LogicalSwitchPort
	err = wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		var innerErr error

		curPort, innerErr = m.ovnclient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{Name: port.Name})
		if innerErr != nil {
			return false, fmt.Errorf("failed to get logical switch port %s: %w", port.Name, innerErr)
		}

		if curPort.DynamicAddresses == nil {
			return false, nil
		}

		reqLog.Info("LSP dynamic address", "address", *curPort.DynamicAddresses)
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed while waiting for dynamic address to be populated: %w", err)
	}

	// get assigned IP address from dynamic addresses
	dynamicAddresses := strings.Split(*curPort.DynamicAddresses, " ")
	if len(dynamicAddresses) < 2 {
		return fmt.Errorf("no dynamic addresses found for service interface %s. dynamicAddresses=%s", si.Name, *curPort.DynamicAddresses)
	}

	// first entry is the MAC, the second is IP
	assignedIP := dynamicAddresses[1]

	if err := m.applyRouterPolicyForServiceInterface(ctx, vpc, vn, node, si, assignedIP); err != nil {
		return fmt.Errorf("failed to apply router policy for service interface %s: %w", client.ObjectKeyFromObject(si), err)
	}

	return nil
}

// applyRouterPolicyForServiceInterface applies a router policy in the VPC router for a service interface
func (m *manager) applyRouterPolicyForServiceInterface(
	ctx context.Context,
	vpc *vpcv1.DPUVPC,
	vn *vpcv1.DPUVirtualNetwork,
	node *corev1.Node,
	si *dpuservicev1.ServiceInterface, dynamicAddress string) error {
	if !vn.Spec.ExternallyRouted {
		// if virtual network is not externally routed, we dont need to apply a router policy
		return nil
	}
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Applying router policy for service interface", "vpc", vpc.Name, "vn", vn.Name, "node", node.Name, "si", si.Name, "dynamicAddress", dynamicAddress)
	desired := sets.NewPolicySet()

	_, gwRouterPortNetworks, err := m.routerPortMACAndNetworksFromAnnotation(node.Annotations)
	if err != nil {
		return fmt.Errorf("failed to get gateway router networks from node %s annotation: %w", node.Name, err)
	}
	nextHop := gwRouterPortNetworks.IPV4.IP.String()

	// route policy for service interface IP to node gateway router port IP
	desired.Add(&nbdb.LogicalRouterPolicy{
		Priority:    50,
		Match:       fmt.Sprintf("ip4.src == %s/32", dynamicAddress),
		Action:      nbdb.LogicalRouterPolicyActionReroute,
		Nexthop:     &nextHop,
		ExternalIDs: NewExternalIDs().WithVPCRef(vpc).WithVirtualNetworkRef(vn).WithOwnerRef(si),
	})

	extIDs := NewExternalIDs().WithVPCRef(vpc).WithVirtualNetworkRef(vn).WithOwnerRef(si)
	if err := m.applyPoliciesForRouter(ctx, common.VPCRouterName(vpc), desired, ignoreNonMatchingExternalIDsPolicyFilter(extIDs)); err != nil {
		return fmt.Errorf("failed to apply router policy in VPC router for service interface %s: %w", client.ObjectKeyFromObject(si), err)
	}

	return nil

}

// UnplugServiceInterface unplugs a service interface from virtual network
func (m *manager) UnplugServiceInterface(ctx context.Context, vn *vpcv1.DPUVirtualNetwork, si *dpuservicev1.ServiceInterface) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Unplugging service interface from virtual network", "vpc", vn.Spec.VPCName, "vn", vn.Name, "si", si.Name)

	err := m.ovnclient.DeleteLogicalSwitchPort(ctx,
		&nbdb.LogicalSwitchGetParams{Name: common.VirtualNetworkSwitchName(vn)},
		&nbdb.LogicalSwitchPortDeleteParams{Name: common.ServiceInterfacePortName(si)})
	if err != nil && ovnlib.GetOvnErrorCodeFromError(err) != ovnlib.ErrNotFound {
		return fmt.Errorf("failed to delete logical switch port %s: %w", common.ServiceInterfacePortName(si), err)
	}

	if err := m.removeRouterPolicyForServiceInterface(ctx, vn, si); err != nil {
		return fmt.Errorf("failed to remove router policy for service interface %s: %w", client.ObjectKeyFromObject(si), err)
	}

	return nil
}

// removeRouterPolicyForServiceInterface removes the router policies from the VPC router for a service interface
func (m *manager) removeRouterPolicyForServiceInterface(ctx context.Context, vn *vpcv1.DPUVirtualNetwork, si *dpuservicev1.ServiceInterface) error {
	desired := sets.NewPolicySet()

	// construct partial VPC object
	vpc := &vpcv1.DPUVPC{}
	vpc.SetNamespace(vn.Namespace)
	vpc.SetName(vn.Spec.VPCName)

	extIDs := NewExternalIDs().WithVPCRef(vpc).WithVirtualNetworkRef(vn).WithOwnerRef(si)
	if err := m.applyPoliciesForRouter(ctx, common.VPCRouterName(vpc), desired, ignoreNonMatchingExternalIDsPolicyFilter(extIDs)); err != nil {
		return fmt.Errorf("failed to apply router policy in VPC router for service interface %s: %w", client.ObjectKeyFromObject(si), err)
	}

	return nil
}

// applyLogicalSwitchPort creates a logical switch port if it does not exist
func (m *manager) applyLogicalSwitchPort(ctx context.Context, switchName string, port *nbdb.LogicalSwitchPort) error {
	curPort, err := m.ovnclient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{Name: port.Name, UUID: port.UUID})
	if err != nil {
		code := ovnlib.GetOvnErrorCodeFromError(err)
		if code != ovnlib.ErrNotFound {
			return fmt.Errorf("failed to get logical switch port %s: %w", port.Name, err)
		}
		// port does not exist, create it
		_, err = m.ovnclient.CreateLogicalSwitchPort(ctx, &nbdb.LogicalSwitchGetParams{Name: switchName}, port)
		if err != nil {
			return fmt.Errorf("failed to create logical switch port %s: %w", port.Name, err)
		}
		return nil
	}

	// if exists, check that adresses are the same
	if !slices.Equal(curPort.Addresses, port.Addresses) {
		// recreate port with new addresses
		err = m.ovnclient.DeleteLogicalSwitchPort(ctx, &nbdb.LogicalSwitchGetParams{Name: switchName}, &nbdb.LogicalSwitchPortDeleteParams{Name: port.Name})
		if err != nil {
			return fmt.Errorf("failed to delete logical switch port %s: %w", port.Name, err)
		}
		_, err = m.ovnclient.CreateLogicalSwitchPort(ctx, &nbdb.LogicalSwitchGetParams{Name: switchName}, port)
		if err != nil {
			return fmt.Errorf("failed to create logical switch port %s: %w", port.Name, err)
		}
	}

	return nil
}

// ListVPCs lists all VPCs in OVN
func (m *manager) ListVPCs(ctx context.Context) ([]client.ObjectKey, error) {
	// list all logical switches
	lsList, err := m.ovnclient.ListLogicalSwitch(ctx, &nbdb.LogicalSwitchListParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to list logical switches: %w", err)
	}

	// store vpc namespaced names in a set
	vpcRefs := k8sset.New[client.ObjectKey]()
	for _, ls := range lsList {
		ref := ls.ExternalIDs[VPCRefKey]
		if ref == "" {
			continue
		}

		objKey, err := ObjectKeyFromRef(ref)
		if err != nil {
			return nil, fmt.Errorf("failed to get object key from VPC reference %s: %w", ref, err)
		}
		vpcRefs.Insert(objKey)
	}

	return vpcRefs.UnsortedList(), nil
}

// ListServiceInterfaces lists all service interfaces in OVN
func (m *manager) ListServiceInterfaces(ctx context.Context) ([]ServiceInterfacRef, error) {
	// list all logical switches
	lspList, err := m.ovnclient.ListLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortListParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to list logical switches: %w", err)
	}

	// store service interface namespaced names in a set
	siRefs := k8sset.New[ServiceInterfacRef]()
	for _, lsp := range lspList {
		ownerRef := lsp.ExternalIDs[VPCOwnerRefKey]
		if !strings.HasPrefix(ownerRef, dpuservicev1.ServiceInterfaceKind) {
			continue
		}

		siKey, err := ObjectKeyFromOwnerRef(ownerRef)
		if err != nil {
			return nil, fmt.Errorf("failed to get object key from VPC owner reference %s: %w", siKey, err)
		}

		vpcKey, err := ObjectKeyFromRef(lsp.ExternalIDs[VPCRefKey])
		if err != nil {
			return nil, fmt.Errorf("failed to get object key from VPC reference %s: %w", lsp.ExternalIDs[VPCRefKey], err)
		}

		vnKey, err := ObjectKeyFromRef(lsp.ExternalIDs[VirtualNetworkRefKey])
		if err != nil {
			return nil, fmt.Errorf("failed to get object key from virtual network reference %s: %w", lsp.ExternalIDs[VirtualNetworkRefKey], err)
		}

		siRefs.Insert(ServiceInterfacRef{
			ServiceInterface: siKey,
			VirtualNetwork:   vnKey,
			VPC:              vpcKey,
		})
	}

	return siRefs.UnsortedList(), nil
}

// virtualNetworksWithMasquerade returns the virtual networks with masquerade enabled
func virtualNetworksWithMasquerade(vns []*vpcv1.DPUVirtualNetwork) []*vpcv1.DPUVirtualNetwork {
	var vnsWithMasquerade []*vpcv1.DPUVirtualNetwork

	for _, vn := range vns {
		if vn.Spec.ExternallyRouted && vn.Spec.Masquerade != nil && *vn.Spec.Masquerade {
			vnsWithMasquerade = append(vnsWithMasquerade, vn)
		}
	}

	return vnsWithMasquerade
}
