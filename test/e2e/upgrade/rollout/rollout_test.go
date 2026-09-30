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

package rollout

import (
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

func TestNewPlanValidation(t *testing.T) {
	tests := []struct {
		name      string
		buildPlan func()
		wantPanic string
	}{
		{
			name: "requires a DPUDeployment",
			buildPlan: func() {
				NewPlan()
			},
			wantPanic: "dependency rollout must configure at least one DPUDeployment",
		},
		{
			name: "rejects a negative index",
			buildPlan: func() {
				NewPlan(ForDPUDeployment(-1, WithCurrentDependencies()))
			},
			wantPanic: "DPUDeployment rollout index must not be negative: -1",
		},
		{
			name: "rejects a duplicate index",
			buildPlan: func() {
				NewPlan(
					ForDPUDeployment(0, WithCurrentDependencies()),
					ForDPUDeployment(0, ReprovisionWithExistingDependencies()),
				)
			},
			wantPanic: "DPUDeployment rollout index 0 is configured more than once",
		},
		{
			name: "requires ExpectDPFVersion",
			buildPlan: func() {
				NewPlan(ForDPUDeployment(0, WithCurrentDependencies()))
			},
			wantPanic: "dependency rollout must configure ExpectDPFVersion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if recovered != tt.wantPanic {
					t.Fatalf("got panic %q, want %q", recovered, tt.wantPanic)
				}
			}()
			tt.buildPlan()
		})
	}
}

func TestPlan(t *testing.T) {
	plan := NewPlan(
		ForDPUDeployment(0, ReprovisionWithCurrentDependencies()),
		ForDPUDeployment(1, ReprovisionWithExistingDependencies(
			func(spec *dpuservicev1.DPUDeploymentSpec) {
				spec.DPUs.DPUSetStrategy.Type = provisioningv1.RollingUpdateStrategyType
			},
		)),
		WithoutDPUFlavorTemplateValidation(),
		ExpectDPFVersion(func() string { return "v26.4" }),
	)

	actions := plan.DeploymentActions()
	if !actions[0].UsesCurrentDependencies() {
		t.Fatal("DPUDeployment 0 should use current dependencies")
	}
	if !actions[0].Reprovisions() {
		t.Fatal("DPUDeployment 0 should be reprovisioned")
	}
	if !actions[1].KeepsExistingDependencies() {
		t.Fatal("DPUDeployment 1 should keep existing dependencies")
	}
	if !actions[1].Reprovisions() {
		t.Fatal("DPUDeployment 1 should be reprovisioned")
	}
	installed := &dpuservicev1.DPUDeploymentSpec{}
	actions[1].ApplyModifiers(installed)
	if installed.DPUs.DPUSetStrategy.Type != provisioningv1.RollingUpdateStrategyType {
		t.Fatalf("got DPUSet strategy %q, want %q",
			installed.DPUs.DPUSetStrategy.Type, provisioningv1.RollingUpdateStrategyType)
	}

	if !plan.SkipDPUFlavorTemplateValidation() {
		t.Fatal("DPUFlavorTemplate validation should be skipped")
	}
	if plan.ExpectedDPFVersion() != "v26.4" {
		t.Fatalf("got expected DPF version %q, want v26.4", plan.ExpectedDPFVersion())
	}

	delete(actions, 0)
	if len(plan.DeploymentActions()) != 2 {
		t.Fatal("mutating returned actions must not mutate the plan")
	}
}
