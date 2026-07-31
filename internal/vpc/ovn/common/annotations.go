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

package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
)

const (
	// LRPAddressesAnnotationKey is the annotation where related logical router port IP addresses are stored for a given object
	LRPAddressesAnnotationKey = "ovn.vpc.dpu.nvidia.com/lrp-address"
	// LSPMACAddressAnnotationKey is the annotation where the MAC address for a logical switch port is stored
	LSPMACAddressAnnotationKey = "ovn.vpc.dpu.nvidia.com/lsp-mac-address"
	// LSPUnknownMACAnnotationKey is the annotation that indicates if the logical switch port has an unknown MAC address
	LSPUnknownMACAnnotationKey = "ovn.vpc.dpu.nvidia.com/unknown-mac"
	// LSPConnectedAnnotationKey is the annotation that indicates if the logical switch port is connected to the logical switch
	// this is used for serviceInterfaces to indicate if the interface is connected to ovn
	LSPConnectedAnnotationKey = "ovn.vpc.dpu.nvidia.com/lsp-connected"
	// OVNGatewayConfigAnnotationKey is the annotation where the OVN gateway configuration is stored
	OVNGatewayConfigAnnotationKey = "ovn.vpc.dpu.nvidia.com/gw-config"
	// OVNChassisIDAnnotationKey is the annotation where the OVN chassis ID is stored
	OVNChassisIDAnnotationKey = "ovn.vpc.dpu.nvidia.com/ovn-chassis-id"
	// OVNVtepIPAnnotationKey is the annotation where the OVN VTEP IP address is stored
	OVNVtepIPAnnotationKey = "ovn.vpc.dpu.nvidia.com/ovn-vtep-ip"
	// AnnotationValueTrue is the value of the annotation that indicates a true value
	AnnotationValueTrue = "true"
)

var (
	// ErrAnnotationNotFound is returned when an annotation is not found
	ErrAnnotationNotFound = errors.New("annotation not found")
)

// LRPAddress is the struct that holds the IP addresses for a logical router port.
// Both IPV4 and IPV6 fields should be in CIDR notation (e.g., "192.168.1.1/24" or "2001:db8::1/64")
type LRPAddress struct {
	IPV4 string `json:"ipv4,omitempty"`
	IPV6 string `json:"ipv6,omitempty"`
}

// IPConfiguration holds the IP configuration
type IPConfiguration struct {
	IPv4 string `json:"ipv4,omitempty"`
	IPv6 string `json:"ipv6,omitempty"`
}

// IPNetConfiguration holds the IP configuration in CIDR notation
type IPNetConfiguration struct {
	IPv4 string `json:"ipv4,omitempty"`
	IPv6 string `json:"ipv6,omitempty"`
}

// GatewayConfig holds the gateway configuration
type GatewayConfig struct {
	// IP is the gateway router port IP address and network provided as CIDR notation.
	IP IPNetConfiguration `json:"gateway-ip"`
	// MAC is the gateway router port MAC address.
	MAC string `json:"gateway-interface-mac"`
	// NextHop is the nexthop gateway IP address.
	NextHop IPConfiguration `json:"nexthop"`
}

// LRPAddressFromAnnotation extracts LRPAddress from annotations
func LRPAddressFromAnnotation(annotations map[string]string) (*LRPAddress, error) {
	if annotations == nil {
		return nil, nil
	}

	data := annotations[LRPAddressesAnnotationKey]
	if data == "" {
		return nil, nil
	}

	vpcAddress := LRPAddress{}
	err := json.Unmarshal([]byte(data), &vpcAddress)
	if err != nil {
		return nil, err
	}
	return &vpcAddress, nil
}

// LRPAddressToAnnotation sets LRPAddress to annotations
func LRPAddressToAnnotation(addresses LRPAddress, annotations map[string]string) error {
	if annotations == nil {
		return fmt.Errorf("annotations is nil")
	}

	j, err := json.Marshal(addresses)
	if err != nil {
		return err
	}
	annotations[LRPAddressesAnnotationKey] = string(j)
	return nil
}

// HasLRPAddressAnnotation returns true if annotations have LRPAddressesAnnotation
func HasLRPAddressAnnotation(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	_, ok := annotations[LRPAddressesAnnotationKey]
	return ok
}

// NetworksFromLRPAddressAnnotation returns the IPs and Networks from the LRPAddress annotation
// first IPNet is ipv4 ip and network, second IPNet is ipv6 ip and network if present, nil otherwise.
// error is returned if the annotation is not present or the IPs are not valid.
func NetworksFromLRPAddressAnnotation(annotations map[string]string) (*net.IPNet, *net.IPNet, error) {
	lrpa, err := LRPAddressFromAnnotation(annotations)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get router port address from annotation. %w", err)
	}
	if lrpa == nil {
		return nil, nil, fmt.Errorf("router port address not present in annotation")
	}

	var ipn4 *net.IPNet
	if lrpa.IPV4 == "" {
		return nil, nil, fmt.Errorf("IPv4 address not present in annotation")
	}

	ip4, ipn4, err := net.ParseCIDR(lrpa.IPV4)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse IPv4 address from annotation. %w", err)
	}
	ipn4.IP = ip4

	var ipn6 *net.IPNet
	var ip6 net.IP
	if lrpa.IPV6 != "" {
		ip6, ipn6, err = net.ParseCIDR(lrpa.IPV6)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse IPv6 address from annotation. %w", err)
		}
		ipn6.IP = ip6
	}
	return ipn4, ipn6, nil
}

// LSPMACAddressFromAnnotation returns the MAC address for a logical switch port from the annotation
// if no error is returned and MAC address is nil it means that no mac address is requested to be set in OVN.
// in this case, unknown address should be used.
func LSPMACAddressFromAnnotation(annotations map[string]string) (*net.HardwareAddr, error) {
	if annotations == nil {
		return nil, fmt.Errorf("annotations is nil")
	}

	data, ok := annotations[LSPMACAddressAnnotationKey]
	if !ok {
		return nil, fmt.Errorf("MAC address annotation is missing")
	}
	if data == "" {
		return nil, fmt.Errorf("MAC address annotation is empty")
	}
	if data == "unknown" {
		// MAC address unknown, no MAC address requested to be set in OVN
		return nil, nil
	}

	mac, err := net.ParseMAC(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse MAC address from %s annotation. %w", LSPMACAddressAnnotationKey, err)
	}
	return &mac, nil
}

// GatewayConfigFromAnnotation extracts GatewayConfig from annotations
func GatewayConfigFromAnnotation(annotations map[string]string) (*GatewayConfig, error) {
	if annotations == nil {
		return nil, fmt.Errorf("annotations is nil")
	}

	data, ok := annotations[OVNGatewayConfigAnnotationKey]
	if !ok {
		return nil, fmt.Errorf("gateway config annotation is missing: %w", ErrAnnotationNotFound)
	}
	if data == "" {
		return nil, fmt.Errorf("gateway config annotation is empty: %w", ErrAnnotationNotFound)
	}

	gatewayConfig := GatewayConfig{}
	err := json.Unmarshal([]byte(data), &gatewayConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal gateway config: %w", err)
	}
	return &gatewayConfig, nil
}

// GatewayConfigToAnnotation sets GatewayConfig to annotations
func GatewayConfigToAnnotation(gatewayConfig GatewayConfig, annotations map[string]string) error {
	if annotations == nil {
		return fmt.Errorf("annotations is nil")
	}

	j, err := json.Marshal(gatewayConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal gateway config: %v", err)
	}
	annotations[OVNGatewayConfigAnnotationKey] = string(j)
	return nil
}

// IPNetConfigurationFromAnnotation extracts IPNetConfiguration from annotations
func IPNetConfigurationFromAnnotation(annotations map[string]string, annotationKey string) (*IPNetConfiguration, error) {
	if annotations == nil {
		return nil, fmt.Errorf("annotations is nil")
	}

	data, ok := annotations[annotationKey]
	if !ok {
		return nil, fmt.Errorf("IP configuration annotation is missing: %w", ErrAnnotationNotFound)
	}
	if data == "" {
		return nil, fmt.Errorf("IP configuration annotation is empty: %w", ErrAnnotationNotFound)
	}

	ipNetConfig := IPNetConfiguration{}
	err := json.Unmarshal([]byte(data), &ipNetConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal IP configuration: %w", err)
	}
	return &ipNetConfig, nil
}

// IPNetConfigurationToAnnotation sets IPNetConfiguration to annotations
func IPNetConfigurationToAnnotation(ipNetConfig IPNetConfiguration, annotations map[string]string, annotationKey string) error {
	if annotations == nil {
		return fmt.Errorf("annotations is nil")
	}

	j, err := json.Marshal(ipNetConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal IP configuration: %v", err)
	}
	annotations[annotationKey] = string(j)
	return nil
}
