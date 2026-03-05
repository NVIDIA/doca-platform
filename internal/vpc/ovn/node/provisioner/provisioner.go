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

package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"

	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/kelseyhightower/envconfig"
	"github.com/nvidia/doca-platform/pkg/ipallocator"
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	"github.com/nvidia/doca-platform/pkg/utils/networkhelper"
	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// OVN configuration defaults
	defaultOVNEncapType          = "geneve"
	defaultOVNBridgeDatapathType = "netdev"
	defaultOVNVtepPortName       = "ovnvtep"
	defaultOVNExtBridgeName      = "br-ovn-ext"
	internalBridgeInterfaceType  = "internal"
	// vtepIPAllocationFilePath is the path to the file that contains the VTEP IP allocation done by the IP Allocator.
	// We should ensure that the IP Allocation request name is vtep to have this file created correctly.
	vtepIPAllocationFilePath    = "/tmp/ips/vtep"
	gatewayIPAllocationFilePath = "/tmp/ips/gateway"
	ovsSystemIDFilePath         = "/etc/openvswitch/system-id.conf"
)

// VPCOVNDPUProvisioner manages the VPC provisioning on DPU
type VPCOVNDPUProvisioner struct {
	ctx           context.Context
	networkHelper networkhelper.NetworkHelper
	ovsClient     ovsutils.API
	config        *Config
	k8sClient     client.Client
	patcher       *patch.SerialPatcher
	node          *corev1.Node
}

// Config holds the VPC provisioner configuration
type Config struct {
	OVNSBEndpoint string `envconfig:"OVN_SB_ENDPOINT" default:""`
	NodeName      string `envconfig:"NODE_NAME" default:""`
}

// FromEnv populates the Config from environment variables.
// Returns an error if required variables are not set.
func (config *Config) FromEnv() error {
	if err := envconfig.Process("", config); err != nil {
		return fmt.Errorf("failed to parse environment variables: %v", err)
	}
	if config.OVNSBEndpoint == "" {
		return fmt.Errorf("OVN_SB_ENDPOINT is not set")
	}
	if config.NodeName == "" {
		return fmt.Errorf("NODE_NAME is not set")
	}
	return nil
}

// NewVPCOVNDPUProvisioner creates a new VPC OVN DPU provisioner instance with the given context and network helper
func NewVPCOVNDPUProvisioner(ctx context.Context, config *Config, networkHelper networkhelper.NetworkHelper, k8sClient client.Client, ovsClient ovsutils.API) *VPCOVNDPUProvisioner {
	return &VPCOVNDPUProvisioner{
		ctx:           ctx,
		config:        config,
		networkHelper: networkHelper,
		k8sClient:     k8sClient,
		ovsClient:     ovsClient,
		node:          &corev1.Node{},
	}
}

// Provision initializes and Provisions the DPU for VPC OVN service.
// It sets up OVS configuration, bridges, and VTEP networking.
func (p *VPCOVNDPUProvisioner) Provision() (reterr error) {
	if err := p.setupBridges(); err != nil {
		return err
	}

	err := p.k8sClient.Get(p.ctx, client.ObjectKey{Name: p.config.NodeName}, p.node)
	if err != nil {
		return fmt.Errorf("failed to get node: %v", err)
	}
	p.patcher = patch.NewSerialPatcher(p.node, p.k8sClient)

	// Defer a patch call to always patch the object when Provision exits.
	defer func() {
		if err := p.patcher.Patch(p.ctx, p.node); err != nil {
			if reterr != nil {
				reterr = kerrors.NewAggregate([]error{reterr, fmt.Errorf("failed to patch node: %w", err)})
			} else {
				reterr = fmt.Errorf("failed to patch node: %w", err)
			}
		}
	}()

	if p.node.Annotations == nil {
		p.node.Annotations = make(map[string]string)
	}

	vtepIP, err := p.configureVTEP()
	if err != nil {
		return err
	}

	// system-id is the node name
	systemID := p.config.NodeName

	if err := p.configChassisID(systemID); err != nil {
		return err
	}

	if err := p.setOVSExternalIDs(vtepIP, systemID); err != nil {
		return err
	}

	if err := p.addGatewayConfig(); err != nil {
		return err
	}

	return nil
}

// setupBridges creates the required OVS bridges with specified configuration
func (p *VPCOVNDPUProvisioner) setupBridges() error {
	if err := p.ovsClient.AddBridge(p.ctx, defaultOVNExtBridgeName, defaultOVNBridgeDatapathType, internalBridgeInterfaceType); err != nil {
		return fmt.Errorf("failed to create bridge %s: %v", defaultOVNExtBridgeName, err)
	}

	return nil
}

// configureVTEP sets up the VTEP for OVN networking.
// It first get the ip allocation from the IP allocator,
// then checks for existing VTEP IP configuration on node Annotation and on the VTEP link in the DPU.
// If they match, nothing more to do. Otherwise, it allocates the new IP from the IP allocator, configures the OVN link with this IP, and stores the IP in the node's annotations.
// Returns the configured VTEP IP network and any error if encountered.
func (p *VPCOVNDPUProvisioner) configureVTEP() (*net.IPNet, error) {
	// Add the VTEP port to the external bridge
	if err := p.ovsClient.AddPort(p.ctx, defaultOVNExtBridgeName, defaultOVNVtepPortName, "internal", nil); err != nil {
		return nil, fmt.Errorf("failed to add port %s to bridge %s: %v", defaultOVNVtepPortName, defaultOVNExtBridgeName, err)
	}

	// Extract the VTEP IP from the IP Allocator
	vtepIP, _, err := p.getIPAllocation(vtepIPAllocationFilePath)
	if err != nil {
		return nil, err
	}

	// Check if the VTEP IP is already configured on the link
	hasSameLinkIP, err := p.networkHelper.LinkIPAddressExists(defaultOVNVtepPortName, vtepIP)
	if err != nil {
		return nil, fmt.Errorf("error checking whether IP exists: %w", err)
	}

	// Check if the VTEP IP is already configured on the node annotation
	hasSameAnnotationIP, err := p.hasMatchingVTEPAnnotationIP(vtepIP)
	if err != nil {
		return nil, err
	}

	// If the VTEP IP is already configured on the link and the node annotation, nothing more to do.
	if hasSameLinkIP && hasSameAnnotationIP {
		return vtepIP, nil
	}

	if !hasSameLinkIP {
		// Set the VTEP IP on the bridge
		if err := p.setLinkIPAddress(defaultOVNVtepPortName, vtepIP); err != nil {
			return nil, fmt.Errorf("failed to set link ip address: %v", err)
		}
	}
	if err := p.networkHelper.SetLinkUp(defaultOVNVtepPortName); err != nil {
		return nil, fmt.Errorf("error while setting link %s up: %w", defaultOVNVtepPortName, err)
	}

	// vtepIP configuration, for now we only support IPv4
	vtepIPConfig := common.IPNetConfiguration{
		IPv4: vtepIP.String(),
	}

	if err := p.addIPNetToNodeAnnotation(vtepIPConfig, common.OVNVtepIPAnnotationKey); err != nil {
		return nil, fmt.Errorf("failed to add IP to node annotation: %v", err)
	}

	return vtepIP, nil
}

// setOVSExternalIDs updates OVS external IDs with VTEP and networking parameters
func (p *VPCOVNDPUProvisioner) setOVSExternalIDs(vtepIP *net.IPNet, systemID string) error {
	externalIDs := map[string]string{
		"ovn-remote":               p.config.OVNSBEndpoint,
		"ovn-encap-type":           defaultOVNEncapType,
		"ovn-bridge-datapath-type": defaultOVNBridgeDatapathType,
		"ovn-bridge-mappings":      p.bridgeMappingConfig(),
		"ovn-encap-ip":             vtepIP.IP.String(),
		"system-id":                systemID,
	}

	return p.ovsClient.SetOpenVSwitchExternalIDs(p.ctx, externalIDs)
}

// getIPAllocation reads and parses the IP allocation from the configuration file.
// Returns the IP network and the gateway IP or error if occurred.
func (p *VPCOVNDPUProvisioner) getIPAllocation(filePath string) (*net.IPNet, net.IP, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("error while reading file %s: %w", filePath, err)
	}

	results := []ipallocator.NVIPAMIPAllocatorResult{}
	if err := json.Unmarshal(content, &results); err != nil {
		return nil, nil, fmt.Errorf("error while unmarshalling IP Allocator results: %w", err)
	}

	if len(results) != 1 {
		return nil, nil, fmt.Errorf("expecting exactly 1 IP allocation, got %d", len(results))
	}
	result := results[0]

	ipNet, err := netlink.ParseIPNet(result.IP)
	if err != nil {
		return nil, nil, fmt.Errorf("error while parsing IP %s to net.IPNet: %w", result.IP, err)
	}

	gateway := net.ParseIP(result.Gateway)
	if gateway == nil {
		return nil, nil, fmt.Errorf("failed to parse gateway IP: %s", result.Gateway)
	}

	return ipNet, gateway, nil
}

// setLinkIPAddress configures an IP address on a network link if not already set.
// It removes any existing IPs before setting the new one.
func (p *VPCOVNDPUProvisioner) setLinkIPAddress(link string, ipNet *net.IPNet) error {
	// delete all the IP addresses from the link
	ips, err := p.networkHelper.GetLinkIPAddresses(link)
	if err != nil {
		return fmt.Errorf("error getting link IP addresses: %w", err)
	}
	for _, ip := range ips {
		if err = p.networkHelper.DeleteLinkIPAddress(link, ip); err != nil {
			return fmt.Errorf("error deleting link IP address: %w", err)
		}
	}
	// set the IP address to the link
	if err := p.networkHelper.SetLinkIPAddress(link, ipNet); err != nil {
		return fmt.Errorf("error setting IP address: %w", err)
	}
	return nil
}

// addGatewayConfig adds the gateway configuration to node annotation
func (p *VPCOVNDPUProvisioner) addGatewayConfig() error {
	// Extract the Gateway IP from the IP Allocator
	gatewayIP, nextHopIP, err := p.getIPAllocation(gatewayIPAllocationFilePath)
	if err != nil {
		return fmt.Errorf("failed to get gateway IP allocation: %v", err)
	}

	// Get interface to extract the MAC address of the gateway interface
	iface, err := p.ovsClient.GetIfaceWithName(p.ctx, defaultOVNExtBridgeName)
	if err != nil {
		return err
	}

	if iface.MACInUse == nil || *iface.MACInUse == "" {
		return fmt.Errorf("failed to get MAC address of the gateway interface: mac address in use is nil or empty")
	}

	gatewayConfig, err := p.getGatewayConfigFromNodeAnnotation()
	if err != nil && !errors.Is(err, common.ErrAnnotationNotFound) {
		return fmt.Errorf("failed to get gateway config: %v", err)
	}

	// If the gateway IP is already configured correctly, skip adding the annotation
	if gatewayConfig != nil &&
		gatewayConfig.IP.IPv4 == gatewayIP.String() &&
		gatewayConfig.NextHop.IPv4 == nextHopIP.String() &&
		gatewayConfig.MAC == *iface.MACInUse {
		return nil
	}

	newGatewayConfig := common.GatewayConfig{
		IP: common.IPNetConfiguration{
			IPv4: gatewayIP.String(),
		},
		MAC: *iface.MACInUse,
		NextHop: common.IPConfiguration{
			IPv4: nextHopIP.String(),
		},
	}

	if err := p.addGatewayConfigToNodeAnnotation(newGatewayConfig); err != nil {
		return fmt.Errorf("failed to add gateway config to node annotation: %v", err)
	}

	return nil
}

// configChassisID adds the chassis ID to the node annotation
func (p *VPCOVNDPUProvisioner) configChassisID(systemID string) error {
	// Overwrite the file with the persistent system ID
	err := os.WriteFile(ovsSystemIDFilePath, []byte(fmt.Sprintf("%s\n", systemID)), 0644)
	if err != nil {
		return fmt.Errorf("failed to write to file %s: %v", ovsSystemIDFilePath, err)
	}

	p.node.Annotations[common.OVNChassisIDAnnotationKey] = systemID
	return nil
}

// bridgeMappingConfig returns the bridge mapping configuration for the node
func (p *VPCOVNDPUProvisioner) bridgeMappingConfig() string {
	return fmt.Sprintf("physnet-%s:%s", p.config.NodeName, defaultOVNExtBridgeName)
}

// addGatewayConfigToNodeAnnotation updates the node's annotation with the given gateway configuration.
// Returns an error if the node cannot be retrieved or updated.
func (p *VPCOVNDPUProvisioner) addGatewayConfigToNodeAnnotation(gatewayConfig common.GatewayConfig) error {
	if err := common.GatewayConfigToAnnotation(gatewayConfig, p.node.Annotations); err != nil {
		return fmt.Errorf("failed to add gateway config to node annotation: %v", err)
	}
	return nil
}

// getGatewayConfigFromNodeAnnotation retrieves the gateway configuration from the node's annotations.
// Returns the gateway configuration, or an nil GatewayConfig if the annotation is not found.
// Returns an error if the node cannot be retrieved.
func (p *VPCOVNDPUProvisioner) getGatewayConfigFromNodeAnnotation() (*common.GatewayConfig, error) {
	return common.GatewayConfigFromAnnotation(p.node.Annotations)
}

// addIPNetToNodeAnnotation updates the node's annotation with the given IPNetConfiguration.
// The IPNetConfiguration is stored under the given annotation key.
// Returns an error if the node cannot be retrieved or updated.
func (p *VPCOVNDPUProvisioner) addIPNetToNodeAnnotation(ipnetConfig common.IPNetConfiguration, ipAnnotationKey string) error {
	if err := common.IPNetConfigurationToAnnotation(ipnetConfig, p.node.Annotations, ipAnnotationKey); err != nil {
		return fmt.Errorf("failed to add IP to node annotation: %v", err)
	}
	return nil
}

// hasMatchingVTEPAnnotationIP checks if the existing IP annotation matches the provided VTEP IP
func (p *VPCOVNDPUProvisioner) hasMatchingVTEPAnnotationIP(vtepIP *net.IPNet) (bool, error) {
	existingIPAnnotation, err := p.checkExistingVTEPIPConfiguration()
	if err != nil {
		return false, fmt.Errorf("failed to check existing IP configuration: %v", err)
	}
	if existingIPAnnotation != nil {
		return existingIPAnnotation.String() == vtepIP.String(), nil
	}
	return false, nil
}

// checkExistingIPConfiguration checks if the bridge already has an IP configured from node annotations
// Returns the IP if the bridge is already configured with the annotated IP else nil
func (p *VPCOVNDPUProvisioner) checkExistingVTEPIPConfiguration() (*net.IPNet, error) {
	ipnetConfig, err := common.IPNetConfigurationFromAnnotation(p.node.Annotations, common.OVNVtepIPAnnotationKey)
	if err != nil {
		if errors.Is(err, common.ErrAnnotationNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get IP from node annotation: %v", err)
	}

	ipNet, err := netlink.ParseIPNet(ipnetConfig.IPv4)
	if err != nil {
		return nil, fmt.Errorf("failed to parse IP %s to net.IPNet: %w", ipnetConfig.IPv4, err)
	}

	return ipNet, nil
}
