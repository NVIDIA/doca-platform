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

package util

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
)

// PreInstallAgentReported is true when the in-band agent has reported liveness (preInstall.agentReported timestamp set).
func PreInstallAgentReported(dpu *provisioningv1.DPU) bool {
	if dpu == nil || dpu.Status.AgentStatus == nil || dpu.Status.AgentStatus.PreInstall == nil {
		return false
	}
	reported := dpu.Status.AgentStatus.PreInstall.AgentReported
	return reported != nil && !reported.IsZero()
}

// PreInstallNVConfigReported is true when agentStatus.preInstall.conditions contains NVConfigApplied (any status).
func PreInstallNVConfigReported(dpu *provisioningv1.DPU) bool {
	if dpu == nil || dpu.Status.AgentStatus == nil || dpu.Status.AgentStatus.PreInstall == nil {
		return false
	}
	return meta.FindStatusCondition(
		dpu.Status.AgentStatus.PreInstall.Conditions,
		provisioningv1.DPUAgentConditionNVConfigApplied,
	) != nil
}
