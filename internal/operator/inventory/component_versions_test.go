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
			name:               "vault-kms introduced after v26.4.0 should skip",
			componentName:      operatorv1.VaultKMSName,
			upgradeFromVersion: "v26.4.0",
			wantSkip:           true,
			wantErr:            false,
		},
		{
			name:               "vault-kms upgrading from same version should not skip",
			componentName:      operatorv1.VaultKMSName,
			upgradeFromVersion: "v26.10.0",
			wantSkip:           false,
			wantErr:            false,
		},
		// Prerelease handling is about the upgrade source version. Without normalizing
		// v26.4.0-rc.1, semver would sort it before v26.4.0 and incorrectly skip
		// checks when upgrading from the prerelease to a later build.
		{
			name:               "kube-state-metrics upgrading from prerelease of introduced version should not skip",
			componentName:      operatorv1.KubeStateMetricsName,
			upgradeFromVersion: "v26.4.0-rc.1",
			wantSkip:           false,
			wantErr:            false,
		},
		{
			name:               "kube-state-metrics upgrading from prerelease before introduced version should skip",
			componentName:      operatorv1.KubeStateMetricsName,
			upgradeFromVersion: "v26.3.0-rc.1",
			wantSkip:           true,
			wantErr:            false,
		},
		{
			// TODO: Update the test for v26.8.0 once the first v26.8 beta ships with CoreDNS as a DPUService.
			name:               "coredns upgrading from v26.8.0-alpha.3 should skip",
			componentName:      operatorv1.CoreDNSName,
			upgradeFromVersion: "v26.8.0-alpha.3",
			wantSkip:           true,
			wantErr:            false,
		},
		{
			name:               "coredns upgrading from v26.8.0 GA should skip",
			componentName:      operatorv1.CoreDNSName,
			upgradeFromVersion: "v26.8.0",
			wantSkip:           true,
			wantErr:            false,
		},
		{
			name:               "coredns upgrading from v26.10.0 should not skip",
			componentName:      operatorv1.CoreDNSName,
			upgradeFromVersion: "v26.10.0",
			wantSkip:           false,
			wantErr:            false,
		},
		{
			name:               "kata-containers upgrading from v26.4.0 should skip",
			componentName:      operatorv1.KataContainersName,
			upgradeFromVersion: "v26.4.0",
			wantSkip:           true,
			wantErr:            false,
		},
		{
			name:               "kata-containers upgrading from v26.8.0 should not skip",
			componentName:      operatorv1.KataContainersName,
			upgradeFromVersion: "v26.8.0",
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
			g := NewWithT(t)

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
