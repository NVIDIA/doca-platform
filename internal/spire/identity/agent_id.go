/*
Copyright 2026 NVIDIA

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

package identity

import "fmt"

// PluginName is the SPIRE NodeAttestor plugin name ("dpu_hw").
const PluginName = "dpu_hw"

// MakeAgentID formats the SPIRE agent ID for a normalized serial:
//
//	spiffe://<trustDomain>/spire/agent/<PluginName>/<normalizedSerial>
//
// normalizedSerial must already be normalized via NormalizeSerial.
func MakeAgentID(trustDomain, normalizedSerial string) string {
	return fmt.Sprintf("spiffe://%s/spire/agent/%s/%s", trustDomain, PluginName, normalizedSerial)
}
