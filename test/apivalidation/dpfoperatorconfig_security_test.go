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
