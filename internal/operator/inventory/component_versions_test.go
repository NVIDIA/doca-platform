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

package inventory

import (
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	. "github.com/onsi/gomega"
)

func TestShouldSkipUpgradeCheck(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name               string
		componentName      operatorv1.ComponentName
		upgradeFromVersion string
		wantSkip           bool
		wantErr            bool
	}{
		{
			name:               "component not in registry should not skip",
			componentName:      operatorv1.MultusName,
			upgradeFromVersion: "v25.10.1",
			wantSkip:           false,
			wantErr:            false,
		},
		{
			name:               "kube-state-metrics introduced after v25.10.1 should skip",
			componentName:      operatorv1.KubeStateMetricsName,
			upgradeFromVersion: "v25.10.1",
			wantSkip:           true,
			wantErr:            false,
		},
		{
			name:               "kube-state-metrics upgrading from same version should not skip",
			componentName:      operatorv1.KubeStateMetricsName,
			upgradeFromVersion: "v26.4.0",
			wantSkip:           false,
			wantErr:            false,
		},
		{
			name:               "invalid version should return error",
			componentName:      operatorv1.KubeStateMetricsName,
			upgradeFromVersion: "invalid",
			wantSkip:           false,
			wantErr:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skip, err := ShouldSkipUpgradeCheck(tt.componentName, tt.upgradeFromVersion)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred(), "expected error but got none")
			} else {
				g.Expect(err).ToNot(HaveOccurred(), "unexpected error: %v", err)
				g.Expect(skip).To(Equal(tt.wantSkip), "skip mismatch")
			}
		})
	}
}
