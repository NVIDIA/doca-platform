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

package operations

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/client"
)

// Operation is the interface for all operations.
// The same optCtx instance is passed to all operations, which can be used to pass data between operations.
// Since the optCtx is not thread-safe, do not execute operations in parallel.
type Operation interface {
	Name() string
	ConditionType() string
	Execute(execCtx context.Context, optCtx *Context) error
	ShouldSkip(optCtx *Context) bool
	ShouldUpdateStatusBeforeContinue(optCtx *Context) bool
}

// Context is the context for all operations.
// It is passed to all operations and can be used to pass data between operations.
// Since the Context is not thread-safe, do not access it from multiple goroutines.
type Context struct {
	Client    client.Client
	DPUFlavor provisioningv1.DPUFlavor
	Options   opts.Options
	LatestDPU *provisioningv1.DPU
	Status    provisioningv1.DPUInternalStatus
}
