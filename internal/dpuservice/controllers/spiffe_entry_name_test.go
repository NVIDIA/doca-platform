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

package controllers

import (
	"strings"
	"testing"
)

func TestDPUServiceClusterStaticEntryName(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		dpuServiceName string
		serial         string
		want           string
		wantErr        bool
	}{
		{name: "lowercases all segments", namespace: "svc-ns", dpuServiceName: "My-Service", serial: "MT2440600YYW", want: "dpu-service-svc-ns-my-service-mt2440600yyw-4853776d"},
		{name: "the same name in another namespace yields another entry", namespace: "other-ns", dpuServiceName: "My-Service", serial: "MT2440600YYW", want: "dpu-service-other-ns-my-service-mt2440600yyw-951eade1"},
		{name: "reject empty namespace", namespace: "", dpuServiceName: "svc", serial: "mt2440", wantErr: true},
		{name: "reject empty DPUService name", namespace: "svc-ns", dpuServiceName: "", serial: "mt2440", wantErr: true},
		{name: "reject a serial that is not DNS-1123", namespace: "svc-ns", dpuServiceName: "svc", serial: "mt_2440", wantErr: true},
		{name: "reject combined name over 253 chars", namespace: "svc-ns", dpuServiceName: strings.Repeat("a", 63), serial: strings.Repeat("b", 243), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dpuServiceClusterStaticEntryName(tt.namespace, tt.dpuServiceName, tt.serial)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDPUServiceEntryNameSeparatorIsUnambiguous covers the pair the readable part cannot tell
// apart, because "-" is legal inside a namespace and a DPUService name alike.
func TestDPUServiceEntryNameSeparatorIsUnambiguous(t *testing.T) {
	first, err := dpuServiceClusterStaticEntryName("tenant", "a-svc", "mt2440")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := dpuServiceClusterStaticEntryName("tenant-a", "svc", "mt2440")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first == second {
		t.Fatalf("(tenant, a-svc) and (tenant-a, svc) share the entry name %q", first)
	}
}

// TestDPUServiceEntryNameNeverCollidesWithDPUAgent guards the one namespace both entry families
// share: ClusterStaticEntry is cluster scoped, so a collision would have one overwrite the other.
func TestDPUServiceEntryNameNeverCollidesWithDPUAgent(t *testing.T) {
	serviceName, err := dpuServiceClusterStaticEntryName("svc-ns", "svc", "mt2440")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentName := "dpu-agent-mt2440"; serviceName == agentName {
		t.Fatalf("entry names collide: %q", serviceName)
	}
	if !strings.HasPrefix(serviceName, dpuServiceClusterStaticEntryPrefix) {
		t.Fatalf("%q does not carry the DPUService prefix", serviceName)
	}
}
