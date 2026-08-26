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

package dpudevice

import (
	"strings"
	"testing"
)

func TestDPUAgentClusterStaticEntryName(t *testing.T) {
	tests := []struct {
		name    string
		serial  string
		want    string
		wantErr bool
	}{
		{name: "uppercase serial is lowercased", serial: "MT2440600YYW", want: "dpu-agent-mt2440600yyw"},
		{name: "all-digit serial is a valid DNS-1123 subdomain", serial: "12345", want: "dpu-agent-12345"},
		{name: "reject underscore", serial: "MT_2440", wantErr: true},
		{name: "reject leading hyphen", serial: "-mt2440", wantErr: true},
		{name: "reject trailing hyphen", serial: "mt2440-", wantErr: true},
		{name: "reject colon", serial: "mt:2440", wantErr: true},
		{name: "reject empty", serial: "", wantErr: true},
		{name: "accept max identity serial length", serial: strings.Repeat("a", maxClusterStaticEntrySerialLen), want: "dpu-agent-" + strings.Repeat("a", maxClusterStaticEntrySerialLen)},
		{name: "reject serial one char over identity limit", serial: strings.Repeat("a", maxClusterStaticEntrySerialLen+1), wantErr: true},
		{name: "reject over-length raw serial", serial: strings.Repeat("a", 254), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dpuAgentClusterStaticEntryName(tt.serial)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for serial %q, got %q", tt.serial, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for serial %q: %v", tt.serial, err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
