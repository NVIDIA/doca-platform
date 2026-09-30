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
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
)

// Step performs one part of an upgrade rollout.
type Step[T any] func(ctx context.Context, input T, description string)

// DPUDeploymentAction describes how one DPUDeployment participates in a
// dependency rollout.
type DPUDeploymentAction struct {
	useCurrentDependencies bool
	reprovision            bool
	modifiers              []func(spec *dpuservicev1.DPUDeploymentSpec)
}

// WithCurrentDependencies updates a DPUDeployment to reference the current
// BFB, flavor, DPUServiceTemplate, and DPUServiceConfiguration.
func WithCurrentDependencies() DPUDeploymentAction {
	return DPUDeploymentAction{useCurrentDependencies: true}
}

// ReprovisionWithCurrentDependencies updates a DPUDeployment to reference the
// current dependencies and recreates its DPU.
func ReprovisionWithCurrentDependencies() DPUDeploymentAction {
	return DPUDeploymentAction{
		useCurrentDependencies: true,
		reprovision:            true,
	}
}

// ReprovisionWithExistingDependencies recreates a DPU while retaining its
// existing dependency references. Modifiers selectively update the installed
// spec before reprovisioning.
func ReprovisionWithExistingDependencies(
	modifiers ...func(spec *dpuservicev1.DPUDeploymentSpec),
) DPUDeploymentAction {
	return DPUDeploymentAction{
		reprovision: true,
		modifiers:   modifiers,
	}
}

// UsesCurrentDependencies reports whether the action updates dependency
// references to the current test manifests.
func (a DPUDeploymentAction) UsesCurrentDependencies() bool {
	return a.useCurrentDependencies
}

// KeepsExistingDependencies reports whether the action recreates the DPU while
// retaining its existing dependency references.
func (a DPUDeploymentAction) KeepsExistingDependencies() bool {
	return !a.useCurrentDependencies
}

// Reprovisions reports whether the action explicitly recreates the DPU.
func (a DPUDeploymentAction) Reprovisions() bool {
	return a.reprovision
}

// ApplyModifiers applies the action's selective updates in declaration order.
func (a DPUDeploymentAction) ApplyModifiers(spec *dpuservicev1.DPUDeploymentSpec) bool {
	for _, modify := range a.modifiers {
		modify(spec)
	}
	return len(a.modifiers) > 0
}

// Plan is the validated configuration for a dependency rollout.
type Plan struct {
	deploymentActions               map[int]DPUDeploymentAction
	skipDPUFlavorTemplateValidation bool
	expectedDPFVersion              func() string
}

// Option configures a dependency rollout plan.
type Option func(*Plan)

// ForDPUDeployment configures an action for a DPUDeployment by its index after
// the deployments have been sorted by name.
func ForDPUDeployment(index int, action DPUDeploymentAction) Option {
	return func(plan *Plan) {
		if index < 0 {
			panic(fmt.Sprintf("DPUDeployment rollout index must not be negative: %d", index))
		}
		if _, ok := plan.deploymentActions[index]; ok {
			panic(fmt.Sprintf("DPUDeployment rollout index %d is configured more than once", index))
		}
		plan.deploymentActions[index] = action
	}
}

// WithoutDPUFlavorTemplateValidation skips DPUFlavorTemplate dependency checks
// for releases that predate that resource.
func WithoutDPUFlavorTemplateValidation() Option {
	return func(plan *Plan) {
		plan.skipDPUFlavorTemplateValidation = true
	}
}

// ExpectDPFVersion requires replacement DPUs to report a DPFVersion containing
// the major.minor version returned by version. version is called when the
// plan is checked, not when the option is configured, so it can safely share
// a getter (e.g. a package variable populated later, during suite setup)
// with a phase's expectedDPFVersion.
func ExpectDPFVersion(version func() string) Option {
	return func(plan *Plan) {
		plan.expectedDPFVersion = version
	}
}

// NewPlan validates options and returns an immutable rollout plan.
func NewPlan(opts ...Option) Plan {
	plan := Plan{deploymentActions: map[int]DPUDeploymentAction{}}
	for _, opt := range opts {
		opt(&plan)
	}
	if len(plan.deploymentActions) == 0 {
		panic("dependency rollout must configure at least one DPUDeployment")
	}
	if plan.expectedDPFVersion == nil {
		panic("dependency rollout must configure ExpectDPFVersion")
	}
	return plan
}

// DeploymentActions returns a copy of the configured actions keyed by sorted
// DPUDeployment index.
func (p Plan) DeploymentActions() map[int]DPUDeploymentAction {
	actions := make(map[int]DPUDeploymentAction, len(p.deploymentActions))
	for index, action := range p.deploymentActions {
		actions[index] = action
	}
	return actions
}

// SkipDPUFlavorTemplateValidation reports whether flavor-template dependency
// validation should be skipped.
func (p Plan) SkipDPUFlavorTemplateValidation() bool {
	return p.skipDPUFlavorTemplateValidation
}

// ExpectedDPFVersion returns the major.minor version expected on replacement
// DPUs, as configured by the mandatory ExpectDPFVersion option.
func (p Plan) ExpectedDPFVersion() string {
	return p.expectedDPFVersion()
}
