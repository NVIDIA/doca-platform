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

package dpucluster

import (
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

func TestIsDPUClusterReadinessAggregationIgnoredCondition(t *testing.T) {
	tests := []struct {
		name          string
		conditionType provisioningv1.ConditionType
		want          bool
	}{
		{
			name:          "Ready",
			conditionType: provisioningv1.ConditionReady,
			want:          true,
		},
		{
			name:          "rotation in progress",
			conditionType: provisioningv1.ConditionEtcdEncryptionRotationInProgress,
			want:          true,
		},
		{
			name:          "rotation blocked",
			conditionType: provisioningv1.ConditionEtcdEncryptionRotationBlocked,
			want:          true,
		},
		{
			name:          "Created",
			conditionType: provisioningv1.ConditionCreated,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDPUClusterReadinessAggregationIgnoredCondition(tt.conditionType); got != tt.want {
				t.Fatalf("isDPUClusterReadinessAggregationIgnoredCondition(%q) = %t, want %t",
					tt.conditionType, got, tt.want)
			}
		})
	}
}
