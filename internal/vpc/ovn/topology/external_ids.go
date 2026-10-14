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

import (
	"fmt"
	"strings"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// VPCOwnerRefKey is the key for storing the owning k8s object reference in external IDs
	VPCOwnerRefKey = "vpc-owner-ref"
	// VPCRefKey is the key for storing the VPC object reference in external IDs
	VPCRefKey = "vpc-ref"
	// VirtualNetworkRefKey is the key for storing the virtual network object reference in external IDs
	VirtualNetworkRefKey = "vpc-vnet-ref"
)

// ExternalIDs is a type that holds External IDs for OVN objects
type ExternalIDs map[string]string

// NewExternalIDs creates a new ExternalIDs
func NewExternalIDs() ExternalIDs {
	return make(map[string]string)
}

// WithOwnerRef adds owner reference to external IDs
func (e ExternalIDs) WithOwnerRef(o client.Object) ExternalIDs {
	e[VPCOwnerRefKey] = fmt.Sprintf("%s/%s/%s", o.GetObjectKind().GroupVersionKind().Kind, o.GetNamespace(), o.GetName())
	return e
}

// WithVPCRef adds reference for VPC object to external IDs
func (e ExternalIDs) WithVPCRef(vpc *vpcv1.DPUVPC) ExternalIDs {
	e[VPCRefKey] = fmt.Sprintf("%s/%s", vpc.GetNamespace(), vpc.GetName())
	return e
}

// WithVirtualNetworkRef adds reference for VirtualNetwork object to external IDs
func (e ExternalIDs) WithVirtualNetworkRef(vn *vpcv1.DPUVirtualNetwork) ExternalIDs {
	e[VirtualNetworkRefKey] = fmt.Sprintf("%s/%s", vn.GetNamespace(), vn.GetName())
	return e
}

// ObjectKeyFromRef converts a VPC/VirtualNetwork reference to a client.ObjectKey
func ObjectKeyFromRef(ref string) (client.ObjectKey, error) {
	parts := strings.Split(ref, "/")
	if len(parts) != 2 {
		return client.ObjectKey{}, fmt.Errorf("invalid vpc-* reference: %s", ref)
	}
	return client.ObjectKey{Namespace: parts[0], Name: parts[1]}, nil
}

// ObjectKeyFromOwnerRef converts an owner reference to a client.ObjectKey
func ObjectKeyFromOwnerRef(ref string) (client.ObjectKey, error) {
	parts := strings.Split(ref, "/")
	if len(parts) != 3 {
		return client.ObjectKey{}, fmt.Errorf("invalid owner reference: %s", ref)
	}
	return client.ObjectKey{Namespace: parts[1], Name: parts[2]}, nil
}
