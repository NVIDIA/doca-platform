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

package main

import (
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

func TestResolveBFBRegistry(t *testing.T) {
	const nodeIPEnv = "NODE_IP"

	tests := []struct {
		name         string
		lbAddress    string
		installIface string
		nodeIP       string
		setNodeIP    bool
		want         string
		wantErr      bool
	}{
		{
			name:         "redfish with NODE_IP returns https host without port",
			installIface: string(provisioningv1.InstallViaRedFish),
			nodeIP:       "10.0.0.5",
			setNodeIP:    true,
			want:         "https://10.0.0.5",
		},
		{
			name:         "redfish with empty NODE_IP returns error",
			installIface: string(provisioningv1.InstallViaRedFish),
			nodeIP:       "",
			setNodeIP:    true,
			wantErr:      true,
		},
		{
			name:         "host-agent without load balancer uses https in-cluster default",
			installIface: string(provisioningv1.InstallViaHostAgent),
			want:         "https://" + "bfb-registry:8443",
		},
		{
			name:         "load balancer without scheme gets https",
			lbAddress:    "bfb.example.com",
			installIface: string(provisioningv1.InstallViaRedFish),
			want:         "https://bfb.example.com",
		},
		{
			name:         "load balancer with explicit http scheme is preserved",
			lbAddress:    "http://bfb.example.com:30082",
			installIface: string(provisioningv1.InstallViaHostAgent),
			want:         "http://bfb.example.com:30082",
		},
		{
			name:         "load balancer with explicit https scheme is preserved",
			lbAddress:    "https://bfb.example.com:30443",
			installIface: string(provisioningv1.InstallViaRedFish),
			want:         "https://bfb.example.com:30443",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setNodeIP {
				t.Setenv(nodeIPEnv, tc.nodeIP)
			}

			flags := &cliFlags{
				bfbRegistryLoadBalancerAddress: tc.lbAddress,
				dpuInstallInterface:            tc.installIface,
			}

			got, err := resolveBFBRegistry(flags)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveBFBRegistry() expected an error, got address %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveBFBRegistry() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("resolveBFBRegistry() = %q, want %q", got, tc.want)
			}
		})
	}
}
