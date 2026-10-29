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

// LogicalRouterStaticRouteGetParams either UUID or IPPrefix and Nexthop must be set.
// Use IPPrefix and Nexthop if it's unique across all entities, otherwise use UUID.
type LogicalRouterStaticRouteGetParams struct {
	UUID     string `ovsdb:"_uuid"`
	IPPrefix string `ovsdb:"ip_prefix"`
	Nexthop  string `ovsdb:"nexthop"`
}

// LogicalRouterStaticRouteDeleteParams either UUID or IPPrefix and Nexthop must be set.
// Use IPPrefix and Nexthop if it's unique across all entities, otherwise use UUID.
type LogicalRouterStaticRouteDeleteParams struct {
	UUID     string `ovsdb:"_uuid"`
	IPPrefix string `ovsdb:"ip_prefix"`
	Nexthop  string `ovsdb:"nexthop"`
}

type LogicalRouterStaticRouteListParams struct {
	UUID        string            `ovsdb:"_uuid"`
	IPPrefix    string            `ovsdb:"ip_prefix"`
	Nexthop     string            `ovsdb:"nexthop"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}
