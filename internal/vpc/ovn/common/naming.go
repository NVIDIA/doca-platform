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
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// VPCSwitchName returns the name of the central logical switch of a VPC
func VPCSwitchName(vpc *vpcv1.DPUVPC) string {
	return fmt.Sprintf("%s_%sSwitch", vpc.Namespace, vpc.Name)
}

// VPCRouterName returns the name of the central logical router of a VPC
func VPCRouterName(vpc *vpcv1.DPUVPC) string {
	return fmt.Sprintf("%s_%sRouter", vpc.Namespace, vpc.Name)
}

// VirtualNetworkSwitchName returns the name of the logical switch for a virtual network
func VirtualNetworkSwitchName(vn *vpcv1.DPUVirtualNetwork) string {
	return fmt.Sprintf("%s_%s_%sSwitch", vn.Namespace, vn.Spec.VPCName, vn.Name)
}

// VirtualNetworkRouterName returns the name of the logical router for a virtual network
func VirtualNetworkRouterName(vn *vpcv1.DPUVirtualNetwork) string {
	return fmt.Sprintf("%s_%s_%sRouter", vn.Namespace, vn.Spec.VPCName, vn.Name)
}

// SwitchToRouterPortName returns the name of the logical port connecting a logical switch to a logical router
func SwitchToRouterPortName(switchName, routerName string) string {
	return fmt.Sprintf("%s_to_%s", switchName, routerName)
}

// RouterToSwitchPortName returns the name of the logical port connecting a logical router to a logical switch
func RouterToSwitchPortName(routerName, switchName string) string {
	return fmt.Sprintf("%s_to_%s", routerName, switchName)
}

// ServiceInterfacePortName returns the name of the logical port for a service interface
func ServiceInterfacePortName(si *dpuservicev1.ServiceInterface) string {
	return fmt.Sprintf("%s_%s", si.Namespace, si.Name)
}

// GatewayRouterName returns the name of the logical gateway router for a node
func GatewayRouterName(vpc *vpcv1.DPUVPC, node *corev1.Node) string {
	return fmt.Sprintf("%s_%s_%sGR", vpc.Namespace, vpc.Name, node.Name)
}

// GatewaySwitchName returns the name of the logical gateway switch for a node
func GatewaySwitchName(vpc *vpcv1.DPUVPC, node *corev1.Node) string {
	return fmt.Sprintf("%s_%s_%sGS", vpc.Namespace, vpc.Name, node.Name)
}

// GatewaySwitchLocalnetPortName returns the name of the logical port connecting the gateway switch to the localnet
func GatewaySwitchLocalnetPortName(switchName string) string {
	return fmt.Sprintf("%s_to_localnet", switchName)
}

// LocalnetNetworkName returns the physical network name used for the localnet port on a node
func LocalnetNetworkName(node *corev1.Node) string {
	return fmt.Sprintf("physnet-%s", node.Name)
}
