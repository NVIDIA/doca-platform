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

package apivalidation_test

import (
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	"k8s.io/utils/ptr"
)

// TestPrivilegedPodEnforcementEnabled verifies the breakglass default: privileged
// pod enforcement is on unless the field is explicitly set to false. The
// dpuservice controller reads this directly to decide between Deny and Audit.
func TestPrivilegedPodEnforcementEnabled(t *testing.T) {
	tests := []struct {
		name string
		sec  *operatorv1.SecurityConfiguration
		want bool
	}{
		{name: "nil SecurityConfiguration defaults to enabled", sec: nil, want: true},
		{name: "empty PrivilegedPodEnforcement defaults to enabled", sec: &operatorv1.SecurityConfiguration{}, want: true},
		{name: "PrivilegedPodEnforcement explicitly enabled", sec: &operatorv1.SecurityConfiguration{PrivilegedPodEnforcement: ptr.To(true)}, want: true},
		{name: "PrivilegedPodEnforcement explicitly disabled", sec: &operatorv1.SecurityConfiguration{PrivilegedPodEnforcement: ptr.To(false)}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sec.PrivilegedPodEnforcementEnabled(); got != tt.want {
				t.Errorf("PrivilegedPodEnforcementEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// certManagementConfig builds a DPFOperatorConfig carrying the given certificate management
// configuration, so the tests below exercise the helpers through the optional Security group they
// have to walk.
func certManagementConfig(certManagement *operatorv1.CertManagementConfiguration) *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		Spec: operatorv1.DPFOperatorConfigSpec{
			Security: &operatorv1.SecurityConfiguration{CertManagement: certManagement},
		},
	}
}

// TestGetCATrustBundleConfigMapName verifies the resolved trust bundle name. It holds in both CA
// modes, which differ only in who fills the ConfigMap, so nothing here depends on the anchor.
func TestGetCATrustBundleConfigMapName(t *testing.T) {
	tests := []struct {
		name   string
		config *operatorv1.DPFOperatorConfig
		want   string
	}{
		{
			name:   "nil Security uses the default name",
			config: &operatorv1.DPFOperatorConfig{},
			want:   operatorv1.DefaultCATrustBundleConfigMapName,
		},
		{
			name:   "nil CertManagement uses the default name",
			config: certManagementConfig(nil),
			want:   operatorv1.DefaultCATrustBundleConfigMapName,
		},
		{
			name:   "an unset name uses the default name",
			config: certManagementConfig(&operatorv1.CertManagementConfiguration{}),
			want:   operatorv1.DefaultCATrustBundleConfigMapName,
		},
		{
			name: "an empty name uses the default name",
			config: certManagementConfig(&operatorv1.CertManagementConfiguration{
				TrustBundleConfigMapName: "",
			}),
			want: operatorv1.DefaultCATrustBundleConfigMapName,
		},
		{
			name: "a configured name is honored",
			config: certManagementConfig(&operatorv1.CertManagementConfiguration{
				TrustBundleConfigMapName: "enterprise-ca-bundle",
			}),
			want: "enterprise-ca-bundle",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetCATrustBundleConfigMapName(); got != tt.want {
				t.Errorf("GetCATrustBundleConfigMapName() = %v, want %v", got, tt.want)
			}
		})
	}
}
