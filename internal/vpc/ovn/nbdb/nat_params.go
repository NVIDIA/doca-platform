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

// NatGetParams either UUID or LogicalIP and Type and optionally GatewayPort must be set.
// Use LogicalIP and Type if it's unique across all entities, otherwise use UUID.
type NatGetParams struct {
	UUID        string  `ovsdb:"_uuid"`
	LogicalIP   string  `ovsdb:"logical_ip"`
	Type        NATType `ovsdb:"type"`
	GatewayPort *string `ovsdb:"gateway_port"`
}

// NatDeleteParams either UUID or LogicalIP and Type and optionally GatewayPort must be set.
// Use LogicalIP and Type if it's unique across all entities, otherwise use UUID.
type NatDeleteParams struct {
	UUID        string  `ovsdb:"_uuid"`
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
