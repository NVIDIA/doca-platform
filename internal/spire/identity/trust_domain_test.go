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

func TestValidateTrustDomain(t *testing.T) {
	tests := []struct {
		name    string
		td      string
		want    string
		wantErr bool
	}{
		{name: "valid domain", td: "example.org", want: "example.org"},
		{name: "trims whitespace", td: "  example.org  ", want: "example.org"},
		{name: "empty rejected", td: "", wantErr: true},
		{name: "whitespace-only rejected", td: "   ", wantErr: true},
		{name: "slash rejected", td: "example.org/foo", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateTrustDomain(tc.td)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateTrustDomain(%q) = %q, want error", tc.td, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateTrustDomain(%q) unexpected error: %v", tc.td, err)
			}
			if got != tc.want {
				t.Fatalf("ValidateTrustDomain(%q) = %q, want %q", tc.td, got, tc.want)
			}
		})
	}
}
