/*
Copyright 2025 NVIDIA.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
