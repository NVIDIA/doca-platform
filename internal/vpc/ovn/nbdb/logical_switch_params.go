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

// LogicalSwitchGetParams either Name or UUID must be set.
// Use Name if it's unique across all entities, otherwise use UUID.
type LogicalSwitchGetParams struct {
	UUID string `ovsdb:"_uuid"`
	Name string `ovsdb:"name"`
}

// LogicalSwitchDeleteParams either Name or UUID must be set.
// Use Name if it's unique across all entities, otherwise use UUID.
type LogicalSwitchDeleteParams struct {
	UUID string `ovsdb:"_uuid"`
	Name string `ovsdb:"name"`
}

type LogicalSwitchListParams struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	OtherConfig map[string]string `ovsdb:"other_config"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}
