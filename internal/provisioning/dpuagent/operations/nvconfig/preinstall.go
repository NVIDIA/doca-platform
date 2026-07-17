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

package nvconfig

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PreInstallConfigureNVConfig applies mlxconfig best-effort during Config FW Parameters on reprovision.
// Results are written to agentStatus.preInstall.conditions (type NVConfigApplied), not agentStatus.conditions.
type PreInstallConfigureNVConfig struct {
	ConfigureNVConfig
}

func (n *PreInstallConfigureNVConfig) Name() string {
	return "Pre-Install NVConfig"
}

func (n *PreInstallConfigureNVConfig) ConditionType() string {
	return provisioningv1.DPUAgentConditionNVConfigApplied
}

// ShouldConfigureNVConfig reports whether best-effort pre-install NVConfig should run
// for the DPU in ctx.LatestDPU during the Config FW Parameters phase on reprovision.
func ShouldConfigureNVConfig(ctx *operations.Context) bool {
	if ctx.LatestDPU.Status.Phase != provisioningv1.DPUConfigFWParameters {
		return false
	}
	if ctx.LatestDPU.Status.AgentStatus == nil || ctx.LatestDPU.Status.AgentStatus.PreInstall == nil {
		return true
	}
	cond := meta.FindStatusCondition(
		ctx.LatestDPU.Status.AgentStatus.PreInstall.Conditions,
		provisioningv1.DPUAgentConditionNVConfigApplied,
	)
	return cond == nil
}

func (n *PreInstallConfigureNVConfig) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return true
}

func (n *PreInstallConfigureNVConfig) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if optCtx.LatestDPU == nil {
		return fmt.Errorf("latest DPU not retrieved")
	}
	klog.InfoS("pre-install NVConfig start",
		"dpu", klog.KObj(optCtx.LatestDPU),
		"phase", optCtx.LatestDPU.Status.Phase,
		"rebootMethodDiscovery", optCtx.RebootMethodDiscovery)
	flavorName := optCtx.LatestDPU.Spec.DPUFlavor
	if flavorName == "" {
		return fmt.Errorf("pre-install NVConfig: DPU spec.dpuFlavor is empty")
	}
	flavor := &provisioningv1.DPUFlavor{}
	if err := optCtx.Client.Get(execCtx, client.ObjectKey{Namespace: optCtx.LatestDPU.Namespace, Name: flavorName}, flavor); err != nil {
		return fmt.Errorf("pre-install NVConfig: get DPUFlavor %s/%s: %w", optCtx.LatestDPU.Namespace, flavorName, err)
	}
	klog.InfoS("pre-install NVConfig flavor loaded",
		"name", flavorName,
		"nvconfigProfiles", len(flavor.Spec.NVConfig))
	saved := optCtx.DPUFlavor
	optCtx.DPUFlavor = *flavor
	defer func() { optCtx.DPUFlavor = saved }()
	if err := n.ConfigureNVConfig.Execute(execCtx, optCtx); err != nil {
		return err
	}
	klog.InfoS("pre-install NVConfig done",
		"dpu", klog.KObj(optCtx.LatestDPU),
		"condMessage", optCtx.CondMessage)
	return nil
}
