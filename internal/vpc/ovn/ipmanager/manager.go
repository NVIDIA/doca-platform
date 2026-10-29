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

package ipmanager

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ip"

	"github.com/go-logr/logr"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// VPCClusterNetworkIPV4 is the name of the IPv4 cluster network
	// It is used for the VPC cluster network in OVN
	VPCClusterNetworkIPV4 = "ClusterNetwork4"
	// VPCClusterCIDRIPV4 is the CIDR for the IPv4 cluster network
	VPCClusterCIDRIPV4 = "100.64.0.0/16"

	// VPCClusterNetworkIPV6 is the name of the IPv6 cluster network
	// It is used for the VPC cluster network in OVN
	VPCClusterNetworkIPV6 = "ClusterNetwork6"
	// VPCClusterCIDRIPV6 is the CIDR for the IPv6 cluster network
	VPCClusterCIDRIPV6 = "fe80:dead:beef::/64"
)

// IPManager is the interface for managing IP address allocations for VPCs
type IPManager interface {
	// AddVPC adds a new vpc by ID. if exists, its a no-op.
	AddVPC(vpcid string)
	// RemoveVPC removes vpc by ID. if doesnt exist, its a no-op.
	// Caller must ensure that there are no IPAllocator references to any network in use before calling this.
	RemoveVPC(vpcid string)
	// Lists VPCs by ID
	ListVPCs() []string
	// AddNetworkadds a new network with the given cidr to a vpc. if exists, its a no-op
	// the gateway for the network is assumed to be the first IP of the network cidr
	// if vpc does not exist, error is returned
	// if the network exists but with different cidr, error is returned
	AddNetwork(vpcid string, networkid string, cidr string) error
	// RemoveNetwork removes network by ID from the given VPC. if doesnt exist, its a no-op
	// caller must ensure that there are no IPAllocator references to the network in use before calling this.
	RemoveNetwork(vpcid string, networkid string)
	// Lists networks by ID in the given VPC
	ListNetworks(vpcid string) []string
	// GetNetworkIPAllocator returns the IPAllocator for the given network in the given VPC. if either network or VPC does not exist
	// nil is returned
	GetNetworkIPAllocator(vpcid string, networkid string) ip.IPAllocator
	// Initialize initializes the IPManager with the given DPUVPCs, DPUVirtualNetworks and nodes
	// if IPManager is already initialized, then its a no-op. returns error if occurred during initialization
	// this method resets and overwrites any existing state in the IPManager.
	Initialize(vpcs []vpcv1.DPUVPC, vns []vpcv1.DPUVirtualNetwork, nodes []corev1.Node) error
	// Initialized returns true if the IPManager is initialized and false otherwise.
	Initialized() bool
	// ResetInitialized sets IPManager initialized state to false.
	ResetInitialized()
	// LogAllocationStats logs the allocation stats for all IPAllocators
	LogAllocationStats(log logr.Logger)
}

// NewIPManager creates a new IPManager instance
func NewIPManager() IPManager {
	return &ipManagerImpl{
		mu:            sync.Mutex{},
		vpcallocators: make(vpcToNetwork),
		initialized:   false,
	}
}

// ObjToID returns a unique ID for the given object
func ObjToID(obj client.Object) string {
	// remove the pointer (*) from the type string
	typeStr := strings.TrimPrefix(fmt.Sprintf("%T", obj), "*")
	return fmt.Sprintf("%s_%s_%s", typeStr, obj.GetNamespace(), obj.GetName())
}

// networkIPAllocator wraps an IPAllocator with additional metadata
type networkIPAllocator struct {
	// cidr is the cidr used for the allocator
	cidr        string
	ipallocator ip.IPAllocator
}

// networkToIPAllocators maps network ID to IPAllocator
type networkToIPAllocators map[string]networkIPAllocator

// vpcToNetwork maps VPC ID to network ID to IPAllocator
type vpcToNetwork map[string]networkToIPAllocators

// ipManagerImpl implements IPManager interface
type ipManagerImpl struct {
	mu sync.Mutex

	// vpcallocators if a map of VPC ID to network ID to IPAllocator
	vpcallocators vpcToNetwork
	initialized   bool
}

// AddVPC adds a new vpc by ID. if exists, its a no-op.
func (i *ipManagerImpl) AddVPC(vpcid string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	_, ok := i.vpcallocators[vpcid]
	if !ok {
		i.vpcallocators[vpcid] = make(networkToIPAllocators)
	}
}

// RemoveVPC removes vpc by ID. if doesnt exist, its a no-op.
// Caller must ensure that there are no IPAllocator references to any network in use before calling this.
func (i *ipManagerImpl) RemoveVPC(vpcid string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	delete(i.vpcallocators, vpcid)
}

// Lists VPCs by ID
func (i *ipManagerImpl) ListVPCs() []string {
	i.mu.Lock()
	defer i.mu.Unlock()

	vpcids := make([]string, 0, len(i.vpcallocators))
	for vpcid := range i.vpcallocators {
		vpcids = append(vpcids, vpcid)
	}
	return vpcids
}

// AddNetwork adds a new network with the given cidr to a vpc. if exists, its a no-op
// the gateway for the network is assumed to be the first IP of the network cidr
// if vpc does not exist, error is returned
func (i *ipManagerImpl) AddNetwork(vpcid string, networkid string, cidr string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.addNetwork(vpcid, networkid, cidr)
}

// addNetwork is used internally to add network to vpc by id
func (i *ipManagerImpl) addNetwork(vpcid string, networkid string, cidr string) error {
	vpcalloc, ok := i.vpcallocators[vpcid]
	if !ok {
		return fmt.Errorf("vpc %s does not exist", vpcid)
	}

	netalloc, ok := vpcalloc[networkid]
	if ok {
		if netalloc.cidr != cidr {
			return fmt.Errorf("network %s already exists with different cidr %s", networkid, netalloc.cidr)
		}
		return nil
	}

	_, ipn, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("failed to add network, error parsing cidr. %w", err)
	}

	rs := ip.RangeSet{
		ip.Range{Subnet: *ipn, Gateway: ip.NextIP(ipn.IP)},
	}
	if err := rs.Canonicalize(); err != nil {
		return fmt.Errorf("failed to add network, error canonicalizing range set. %w", err)
	}
	vpcalloc[networkid] = networkIPAllocator{cidr: cidr, ipallocator: ip.NewIPAllocator(&rs, nil)}

	return nil
}

// RemoveNetwork removes network by ID from the given VPC. if doesnt exist, its a no-op
// caller must ensure that there are no IPAllocator references to the network in use before calling this.
func (i *ipManagerImpl) RemoveNetwork(vpcid string, networkid string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	vpcalloc, ok := i.vpcallocators[vpcid]
	if !ok {
		return
	}
	delete(vpcalloc, networkid)
}

// Lists networks by ID in the given VPC
func (i *ipManagerImpl) ListNetworks(vpcid string) []string {
	i.mu.Lock()
	defer i.mu.Unlock()

	vpcalloc, ok := i.vpcallocators[vpcid]
	if !ok {
		return nil
	}

	networkids := make([]string, 0, len(vpcalloc))
	for networkid := range vpcalloc {
		networkids = append(networkids, networkid)
	}
	return networkids
}

// GetNetworkIPAllocator returns the IPAllocator for the given network in the given VPC. if either network or VPC does not exist
// nil is returned
func (i *ipManagerImpl) GetNetworkIPAllocator(vpcid string, networkid string) ip.IPAllocator {
	i.mu.Lock()
	defer i.mu.Unlock()

	vpcalloc, ok := i.vpcallocators[vpcid]
	if !ok {
		return nil
	}

	return vpcalloc[networkid].ipallocator
}

// Initialize initializes the IPManager with the given DPUVPCs, DPUVirtualNetworks and nodes
// if IPManager is already initialized, then its a no-op. returns error if occurred during initialization
// this method resets and overwrites any existing state in the IPManager.
//
//nolint:unparam
func (i *ipManagerImpl) Initialize(vpcs []vpcv1.DPUVPC, vns []vpcv1.DPUVirtualNetwork, nodes []corev1.Node) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.initialized {
		// in case of parallel initialization, check if already initialized
		return nil
	}

	i.vpcallocators = make(vpcToNetwork)

	if err := i.initializeFromVPCs(vpcs); err != nil {
		return fmt.Errorf("failed to initialize IPManager. failed to initialize from VPCs. %w", err)
	}

	if err := i.initializeFromDPUVirtualNetworks(vns); err != nil {
		return fmt.Errorf("failed to initialize IPManager. failed to initialize from DPUVirtualNetworks. %w", err)
	}

	if err := i.initializeFromNodes(vpcs, nodes); err != nil {
		return fmt.Errorf("failed to initialize IPManager. failed to initialize from nodes. %w", err)
	}

	i.initialized = true
	return nil
}

// initializeFromVPCs initializes the IPManager with the given DPUVPCs
func (i *ipManagerImpl) initializeFromVPCs(vpcs []vpcv1.DPUVPC) error {
	// create vpc entries for each vpc (we assume caller already filtered vpcs without finalizers set)
	for _, vpc := range vpcs {
		i.vpcallocators[ObjToID(&vpc)] = make(networkToIPAllocators)
		// add cluster network
		if err := i.addNetwork(ObjToID(&vpc), VPCClusterNetworkIPV4, VPCClusterCIDRIPV4); err != nil {
			return fmt.Errorf("failed to add cluster network for DPUVPC(%s). %w",
				client.ObjectKeyFromObject(&vpc).String(), err)
		}

		if !common.HasLRPAddressAnnotation(vpc.Annotations) {
			// nothing to do
			continue
		}

		ipn4, _, err := common.NetworksFromLRPAddressAnnotation(vpc.Annotations)
		if err != nil {
			return fmt.Errorf("failed to parse LRP address from DPUVPC(%s) annotations. %w",
				client.ObjectKeyFromObject(&vpc).String(), err)
		}

		// add LRP ipv4 address to cluster network
		if _, err := i.vpcallocators[ObjToID(&vpc)][VPCClusterNetworkIPV4].ipallocator.Allocate(
			ObjToID(&vpc), ipn4.IP); err != nil {
			return fmt.Errorf("failed to add LRP address to cluster network. %w", err)
		}
		// TODO(adrianc): do the same for IPV6 network
	}
	return nil
}

// initializeFromDPUVirtualNetworks initializes the IPManager with the given DPUVirtualNetworks
func (i *ipManagerImpl) initializeFromDPUVirtualNetworks(vns []vpcv1.DPUVirtualNetwork) error {
	// allocate ip addresses in cluster cidr for each dpuVirtualNetwork if we have annotations
	for _, vn := range vns {
		if !common.HasLRPAddressAnnotation(vn.Annotations) {
			// nothing to do
			continue
		}

		ipn4, _, err := common.NetworksFromLRPAddressAnnotation(vn.Annotations)
		if err != nil {
			return fmt.Errorf("failed to parse LRP address from DPUVirtualNetwork(%s) annotations. %w",
				client.ObjectKeyFromObject(&vn).String(), err)
		}

		// add LRP ipv4 address to cluster network
		// construct partial vpc obj
		vpc := &vpcv1.DPUVPC{}
		vpc.SetNamespace(vn.Namespace)
		vpc.SetName(vn.Spec.VPCName)

		vpcToNetworks := i.vpcallocators[ObjToID(vpc)]
		if vpcToNetworks == nil {
			return fmt.Errorf("VPC(%s) network not found for DPUVirtualNetwork(%s)",
				vn.Spec.VPCName, client.ObjectKeyFromObject(&vn).String())
		}
		if _, err := vpcToNetworks[VPCClusterNetworkIPV4].ipallocator.Allocate(
			ObjToID(&vn), ipn4.IP); err != nil {
			return fmt.Errorf("failed to add LRP address to cluster network. %w", err)
		}
		// TODO(adrianc): do the same for IPV6 network
	}
	return nil
}

// initializeFromNodes initializes the IPManager with the given nodes
func (i *ipManagerImpl) initializeFromNodes(vpcs []vpcv1.DPUVPC, nodes []corev1.Node) error {
	for _, node := range nodes {
		vpcStr := node.Labels[common.OVNVPCNodeLabelKey]
		if vpcStr == "" {
			continue
		}

		vpc, err := i.vpcFromLabelValue(vpcStr, vpcs)
		if err != nil {
			return fmt.Errorf("failed to initialize IPManager. %w", err)
		}

		if vpc == nil || !common.HasLRPAddressAnnotation(node.Annotations) {
			// vpc not found, or no LRP address annotation, nothing to do
			continue
		}

		ipn4, _, err := common.NetworksFromLRPAddressAnnotation(node.Annotations)
		if err != nil {
			return fmt.Errorf("failed to parse LRP address from node(%s) annotations. %w", node.Name, err)
		}

		// add LRP ipv4 address to cluster network
		vpcToNetworks := i.vpcallocators[ObjToID(vpc)]
		if vpcToNetworks == nil {
			return fmt.Errorf("failed to initialize IPManager. VPC(%s) networks not found for node(%s)", client.ObjectKeyFromObject(vpc), node.Name)
		}

		if _, err := vpcToNetworks[VPCClusterNetworkIPV4].ipallocator.Allocate(
			ObjToID(&node), ipn4.IP); err != nil {
			return fmt.Errorf("failed to initialize IPManager. failed to add LRP address to cluster network. %w", err)
		}
		// TODO(adrianc): do the same for IPV6 network
	}
	return nil
}

// vpcFromLabelValue returns the VPC object for the given label value. if not found in the provided vpcs list, nil is returned
func (i *ipManagerImpl) vpcFromLabelValue(vpcStr string, vpcs []vpcv1.DPUVPC) (*vpcv1.DPUVPC, error) {
	vpcObjKey, err := common.ObjectKeyFromLabelValue(vpcStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse vpc object key from label value(%s=%s). %w",
			common.OVNVPCNodeLabelKey, vpcStr, err)
	}

	for i := range vpcs {
		if client.ObjectKeyFromObject(&vpcs[i]) == vpcObjKey {
			return &vpcs[i], nil
		}
	}
	// vpc not found
	return nil, nil
}

// Initialized returns true if the IPManager is initialized and false otherwise.
func (i *ipManagerImpl) Initialized() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.initialized
}

// ResetInitialized sets IPManager initialized state to false.
func (i *ipManagerImpl) ResetInitialized() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.initialized = false
}

// LogAllocationStats logs the allocation stats for all IPAllocators
func (i *ipManagerImpl) LogAllocationStats(log logr.Logger) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if !i.initialized {
		log.Info("allocation stats: IPManager not initialized")
		return
	}

	if len(i.vpcallocators) == 0 {
		log.Info("allocation stats: no allocations")
	}

	for vpcid, networks := range i.vpcallocators {
		for networkid, allocator := range networks {
			log.Info("allocation stats", "vpc", vpcid, "network", networkid, "allocations", allocator.ipallocator.ListAllocationIDs())
		}
	}
}
