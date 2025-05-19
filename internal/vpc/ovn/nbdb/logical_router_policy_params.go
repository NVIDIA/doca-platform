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
