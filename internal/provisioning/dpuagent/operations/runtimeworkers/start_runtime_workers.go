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

package runtimeworkers

import (
	"context"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
)

// StartRuntimeWorkers launches prerequisites for the release gate without waiting
// for readiness. The workers report their own conditions and use the process context.
type StartRuntimeWorkers struct {
	StartNIC   func(context.Context)
	StartSPIRE func(context.Context)
}

// Name identifies worker startup in operation logs.
func (*StartRuntimeWorkers) Name() string { return "Start runtime workers" }

// ConditionType identifies the startup request condition, separate from worker readiness.
func (*StartRuntimeWorkers) ConditionType() string { return "RuntimeWorkersStarted" }

// ShouldSkip returns false because each worker decides whether startup applies.
func (*StartRuntimeWorkers) ShouldSkip(*operations.Context) bool { return false }

// ShouldUpdateStatusBeforeContinue leaves status publication to the normal reporting loop.
func (*StartRuntimeWorkers) ShouldUpdateStatusBeforeContinue(*operations.Context) bool { return false }

// Execute requests worker startup without waiting for readiness, which the workers report separately.
func (s *StartRuntimeWorkers) Execute(ctx context.Context, optCtx *operations.Context) error {
	s.StartNIC(ctx)
	s.StartSPIRE(ctx)
	optCtx.CondMessage = "Startup requested for applicable runtime workers; readiness is reported separately"
	return nil
}
