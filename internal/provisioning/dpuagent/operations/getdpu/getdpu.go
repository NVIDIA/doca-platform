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

package getdpu

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
)

type GetLatestDPU struct {
}

func (g *GetLatestDPU) Name() string {
	return "Get Latest DPU"
}

func (g *GetLatestDPU) ConditionType() string {
	return "DPURetrieved"
}

func (g *GetLatestDPU) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (g *GetLatestDPU) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (g *GetLatestDPU) Execute(execCtx context.Context, optCtx *operations.Context) error {
	dpu := &provisioningv1.DPU{}
	if err := optCtx.Client.GetObject(execCtx, optCtx.Options.DPUNamespace, optCtx.Options.DPUName, dpu); err != nil {
		return err
	}
	optCtx.LatestDPU = dpu
	return nil
}
