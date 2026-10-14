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

// LogicalSwitchPortGetParams either Name or UUID must be set.
// Use Name if it's unique across all entities, otherwise use UUID.
type LogicalSwitchPortGetParams struct {
	UUID string `ovsdb:"_uuid"`
	Name string `ovsdb:"name"`
}

// LogicalSwitchPortDeleteParams either Name or UUID must be set.
// Use Name if it's unique across all entities, otherwise use UUID.
type LogicalSwitchPortDeleteParams struct {
	UUID string `ovsdb:"_uuid"`
	Name string `ovsdb:"name"`
}

type LogicalSwitchPortListParams struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Addresses   []string          `ovsdb:"addresses"`
	Options     map[string]string `ovsdb:"options"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}
