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

import "testing"

func TestMakeAgentID(t *testing.T) {
	tests := []struct {
		name             string
		trustDomain      string
		normalizedSerial string
		want             string
	}{
		{
			name:             "builds canonical agent id",
			trustDomain:      "example.org",
			normalizedSerial: "mt2152x00abc",
			want:             "spiffe://example.org/spire/agent/dpu_hw/mt2152x00abc",
		},
		{
			name:             "serial with hyphen and underscore",
			trustDomain:      "dpf.nvidia.com",
			normalizedSerial: "mt-2152_x00",
			want:             "spiffe://dpf.nvidia.com/spire/agent/dpu_hw/mt-2152_x00",
		},
		{
			name:             "empty trust domain is not validated",
			trustDomain:      "",
			normalizedSerial: "mt2152x00abc",
			want:             "spiffe:///spire/agent/dpu_hw/mt2152x00abc",
		},
		{
			name:             "serial with slash is formatted verbatim",
			trustDomain:      "example.org",
			normalizedSerial: "abc/def",
			want:             "spiffe://example.org/spire/agent/dpu_hw/abc/def",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MakeAgentID(tc.trustDomain, tc.normalizedSerial)
			if got != tc.want {
				t.Fatalf("MakeAgentID(%q, %q) = %q, want %q", tc.trustDomain, tc.normalizedSerial, got, tc.want)
			}
		})
	}
}

func TestPluginNameIsStable(t *testing.T) {
	if PluginName != "dpu_hw" {
		t.Fatalf("PluginName = %q, want %q", PluginName, "dpu_hw")
	}
}
