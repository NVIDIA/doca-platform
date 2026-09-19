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
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	if err := optCtx.Client.Get(execCtx, client.ObjectKey{Namespace: optCtx.Options.DPUNamespace, Name: optCtx.Options.DPUName}, dpu); err != nil {
		return err
	}
	if string(dpu.UID) != optCtx.Options.DPUUID {
		return fmt.Errorf("stale DPU object: expected UID %s but got %s", optCtx.Options.DPUUID, dpu.UID)
	}
	optCtx.LatestDPU = dpu

	flavorName := dpu.Spec.DPUFlavor
	if flavorName == "" {
		return fmt.Errorf("DPU spec.dpuFlavor is empty")
	}
	flavor := &provisioningv1.DPUFlavor{}
	if err := optCtx.Client.Get(execCtx, client.ObjectKey{Namespace: dpu.Namespace, Name: flavorName}, flavor); err != nil {
		return fmt.Errorf("get DPUFlavor %s/%s: %w", dpu.Namespace, flavorName, err)
	}
	optCtx.DPUFlavor = *flavor
	klog.InfoS("loaded DPUFlavor from API", "dpu", klog.KObj(dpu), "flavor", flavorName)
	return nil
}
