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

package nbdb

// NatGetParams either UUID or LogicalIP and Type and optionally GatewayPort must be set.
// Use LogicalIP, ExternalIP and Type if it's unique across all entities, otherwise use UUID.
type NatGetParams struct {
	UUID        string  `ovsdb:"_uuid"`
	ExternalIP  string  `ovsdb:"external_ip"`
	LogicalIP   string  `ovsdb:"logical_ip"`
	Type        NATType `ovsdb:"type"`
	GatewayPort *string `ovsdb:"gateway_port"`
}

// NatDeleteParams either UUID or LogicalIP, Type and ExternalIP and optionally GatewayPort must be set.
// Use LogicalIP, ExternalIP and Type if it's unique across all entities, otherwise use UUID.
type NatDeleteParams struct {
	UUID        string  `ovsdb:"_uuid"`
	ExternalIP  string  `ovsdb:"external_ip"`
	LogicalIP   string  `ovsdb:"logical_ip"`
	Type        NATType `ovsdb:"type"`
	GatewayPort *string `ovsdb:"gateway_port"`
}

type NatListParams struct {
	UUID        string            `ovsdb:"_uuid"`
	ExternalIP  string            `ovsdb:"external_ip"`
	ExternalMAC *string           `ovsdb:"external_mac"`
	LogicalIP   string            `ovsdb:"logical_ip"`
	LogicalPort *string           `ovsdb:"logical_port"`
	Type        NATType           `ovsdb:"type"`
	GatewayPort *string           `ovsdb:"gateway_port"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}
