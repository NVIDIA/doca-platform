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

package controllers

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ovnlib"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NodeInVPC returns true if Node belongs to the VPC.
func NodeInVPC(node *corev1.Node, vpc *vpcv1.DPUVPC) (bool, error) {
	ls, err := metav1.LabelSelectorAsSelector(vpc.Spec.NodeSelector)
	if err != nil {
		return false, fmt.Errorf("failed to parse node selector for node %s: %w", node.Name, err)
	}
	return ls.Matches(labels.Set(node.Labels)), nil
}

// VPCForNode returns the VPC that the Node belongs to. if it belongs to more than one VPC, it fails.
// If the Node does not belong to any VPC, it returns nil.
func VPCForNode(node *corev1.Node, vpcs []vpcv1.DPUVPC) (*vpcv1.DPUVPC, error) {
	var matchingVPCs []*vpcv1.DPUVPC

	for _, vpc := range vpcs {
		inVPC, err := NodeInVPC(node, &vpc)
		if err != nil {
			return nil, err
		}
		if inVPC {
			matchingVPCs = append(matchingVPCs, &vpc)
		}
	}

	if len(matchingVPCs) == 0 {
		return nil, nil
	}
	if len(matchingVPCs) != 1 {
		vpcNames := make([]string, len(matchingVPCs))
		for _, vpc := range matchingVPCs {
			vpcNames = append(vpcNames, types.NamespacedName{Namespace: vpc.Namespace, Name: vpc.Name}.String())
		}
		return nil, fmt.Errorf("node %s belongs to %d VPCs: %s", node.Name, len(vpcNames), strings.Join(append(vpcNames[:2], "..."), ","))
	}

	return matchingVPCs[0], nil
}

// VirtualNetworksForVPC returns the VirtualNetworks that belong to the VPC.
// This function relies on fieldIdexer being registered for vn.Spec.VPCName in the provided cache backed client.
func VirtualNetworksForVPC(ctx context.Context, c client.Client, vpc *vpcv1.DPUVPC) ([]*vpcv1.DPUVirtualNetwork, error) {
	vnList := &vpcv1.DPUVirtualNetworkList{}
	listOpts := []client.ListOption{client.MatchingFields{"spec.vpcName": vpc.Name}, client.InNamespace(vpc.Namespace)}

	if err := c.List(ctx, vnList, listOpts...); err != nil {
		return nil, err
	}

	return ToPointerSlice(vnList.Items), nil
}

// VPCsForIsolationClass returns the VPCs that belong to the IsolationClass.
// This function relies on fieldIndexer being registered for vpc.Spec.IsolationClassName in the provided cache backed client.
func VPCsForIsolationClass(ctx context.Context, c client.Client, isoCls *vpcv1.IsolationClass) ([]*vpcv1.DPUVPC, error) {
	vpcList := &vpcv1.DPUVPCList{}
	listOpts := []client.ListOption{client.MatchingFields{"spec.isolationClassName": isoCls.Name}}

	if err := c.List(ctx, vpcList, listOpts...); err != nil {
		return nil, err
	}

	return ToPointerSlice(vpcList.Items), nil
}

// OVNClientFromIsolationClass returns OVNWrapper which is a wrapper interface over ovn client
// to interact with OVN NB database based on connection information from IsolationClass.
func OVNClientFromIsolationClass(ctx context.Context, isoCls *vpcv1.IsolationClass) (ovnlib.OVNWrapper, error) {
	// get ovn client
	endpoint := isoCls.Spec.Parameters["ovn-nb-endpoint"]
	if endpoint == "" {
		return nil, fmt.Errorf("ovn-nb-endpoint parameter not set in isolation class %s", isoCls.Name)
	}
	reconnectTimeSeconds := 5
	reconnectTimeStr := isoCls.Spec.Parameters["ovn-nb-reconnect-time"]
	if reconnectTimeStr != "" {
		var err error
		if reconnectTimeSeconds, err = strconv.Atoi(reconnectTimeStr); err != nil {
			return nil, fmt.Errorf("failed to parse ovn-nb-reconnect-time parameter: %w", err)
		}
	}

	ovnc, err := ovnlib.GetOvnNBClient(
		ctx,
		&ovnlib.Config{
			EndPoint:           endpoint,
			OVNNBReconnectTime: reconnectTimeSeconds,
		},
		nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get ovn client: %w", err)
	}

	return ovnc, nil
}

// TopologyManagerFromIsolationClass returns a new topology.Manager backed by ovn client based on the provided isolationClass
func TopologyManagerFromIsolationClass(ctx context.Context, isoCls *vpcv1.IsolationClass) (topology.Manager, error) {
	if isoCls.Spec.Provisioner != OVNProvisionerName {
		return nil, fmt.Errorf("isolation class provisioner is not %s. got %s", OVNProvisionerName, isoCls.Spec.Provisioner)
	}

	ovnc, err := OVNClientFromIsolationClass(ctx, isoCls)
	if err != nil {
		return nil, fmt.Errorf("failed to create ovn client: %w", err)
	}
	return topology.NewManager(ovnc), nil
}

// IsolationClassForVPC returns the IsolationClass for the VPC.
func IsolationClassForVPC(ctx context.Context, kclient client.Client, vpc *vpcv1.DPUVPC) (*vpcv1.IsolationClass, error) {
	// get isolationClass for VPC
	isoClass := &vpcv1.IsolationClass{}

	err := kclient.Get(ctx, client.ObjectKey{Name: vpc.Spec.IsolationClassName}, isoClass)
	if err != nil {
		return nil, err
	}

	// if isolation class provisioner name is not ovn, return early
	if isoClass.Spec.Provisioner != OVNProvisionerName {
		return nil, fmt.Errorf("isolation class provisioner is not %s. got %s", OVNProvisionerName, isoClass.Spec.Provisioner)
	}

	return isoClass, nil
}

// ToPointerSlice converts a slice of concrete type to a slice of pointers of the given type.
func ToPointerSlice[T any](in []T) []*T {
	out := make([]*T, len(in))
	for i := range in {
		out[i] = &in[i]
	}
	return out
}

// MergeNodeSlices merges two slices of nodes.
func MergeNodeSlices(s1 []corev1.Node, s2 []corev1.Node) []corev1.Node {
	out := make([]corev1.Node, 0, len(s1)+len(s2))
	out = append(out, s1...)
	out = append(out, s2...)
	slices.SortFunc(out, func(a, b corev1.Node) int {
		return cmp.Compare(a.Name, b.Name)
	})
	// Remove duplicates
	return slices.CompactFunc(out, func(i, j corev1.Node) bool {
		return client.ObjectKeyFromObject(&i) == client.ObjectKeyFromObject(&j)
	})
}
