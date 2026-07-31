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

// LogicalRouterPolicyGetParams either UUID or Match and Priority must be set.
// Use Match and Priority if it's unique across all entities, otherwise use UUID.
type LogicalRouterPolicyGetParams struct {
	UUID     string `ovsdb:"_uuid"`
	Match    string `ovsdb:"match"`
	Priority int    `ovsdb:"priority"`
}

// LogicalRouterPolicyDeleteParams either UUID or Match and Priority must be set.
// Use Match and Priority if it's unique across all entities, otherwise use UUID.
type LogicalRouterPolicyDeleteParams struct {
	UUID     string `ovsdb:"_uuid"`
	Match    string `ovsdb:"match"`
	Priority int    `ovsdb:"priority"`
}

type LogicalRouterPolicyListParams struct {
	UUID        string                    `ovsdb:"_uuid"`
	Action      LogicalRouterPolicyAction `ovsdb:"action"`
	Match       string                    `ovsdb:"match"`
	Priority    int                       `ovsdb:"priority"`
	ExternalIDs map[string]string         `ovsdb:"external_ids"`
}
