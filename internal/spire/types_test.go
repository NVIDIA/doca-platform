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

package spire

import (
	"strings"
	"testing"
)

func TestDPUAgentSpiffePath(t *testing.T) {
	tests := []struct {
		name    string
		serial  string
		want    string
		wantErr bool
	}{
		{name: "uppercase serial is lowercased", serial: "MT2440600YYW", want: "/dpu/mt2440600yyw/process/dpu-agent"},
		{name: "already lowercase", serial: "mt2440600yyw", want: "/dpu/mt2440600yyw/process/dpu-agent"},
		{name: "surrounding whitespace trimmed", serial: "  MT2440600YYW  ", want: "/dpu/mt2440600yyw/process/dpu-agent"},
		{name: "unreserved punctuation allowed", serial: "mt-24.40_0~1", want: "/dpu/mt-24.40_0~1/process/dpu-agent"},
		{name: "reject empty", serial: "", wantErr: true},
		{name: "reject whitespace only", serial: "   ", wantErr: true},
		{name: "reject colon and slash", serial: "MT:24/40", wantErr: true},
		{name: "reject space inside", serial: "mt 2440", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DPUAgentSpiffePath(tt.serial)
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

func TestSpireWorkloadID(t *testing.T) {
	tests := []struct {
		name    string
		td      string
		serial  string
		want    string
		wantErr bool
	}{
		{name: "valid", td: "cs.internal", serial: "MT2440600YYW", want: "spiffe://cs.internal/dpu/mt2440600yyw/process/dpu-agent"},
		{name: "reject invalid serial", td: "cs.internal", serial: "MT:24/40", wantErr: true},
		{name: "reject empty serial", td: "cs.internal", serial: "", wantErr: true},
		{name: "reject whitespace trust domain", td: " ", serial: "MT2440600YYW", wantErr: true},
		{name: "reject slash in trust domain", td: "cs.internal/extra", serial: "MT2440600YYW", wantErr: true},
		{name: "reject empty trust domain", td: "", serial: "MT2440600YYW", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SpireWorkloadID(tt.td, tt.serial)
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
		{name: "accept max serial length for prefixed name", serial: strings.Repeat("a", 243), want: "dpu-agent-" + strings.Repeat("a", 243)},
		{name: "reject serial one char over prefixed limit", serial: strings.Repeat("a", 244), wantErr: true},
		{name: "reject over-length raw serial", serial: strings.Repeat("a", 254), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DPUAgentClusterStaticEntryName(tt.serial)
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
