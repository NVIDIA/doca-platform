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
	"context"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// WaitPreInstallAgentRegistrationOrProceed updates state while waiting in Initializing.
// When proceed is true, the caller may advance to Pending; when false, stay in Initializing.
func WaitPreInstallAgentRegistrationOrProceed(
	ctx context.Context,
	dpu *provisioningv1.DPU,
	state *provisioningv1.DPUStatus,
	timeout time.Duration,
) (*provisioningv1.DPUStatus, bool) {
	logger := log.FromContext(ctx)
	if PreInstallAgentReported(dpu) {
		logger.V(2).Info("dpu-agent reported liveness", "dpu", dpu.Name)
		return state, true
	}

	initCondType := provisioningv1.DPUCondInitialized.String()
	existing := meta.FindStatusCondition(state.Conditions, initCondType)
	if existing == nil {
		SetDPUCondition(state, NewCondition(initCondType, nil, initCondType, ""))
		return state, false
	}

	if time.Since(existing.LastTransitionTime.Time) >= timeout {
		logger.Info("dpu-agent registration timeout; proceeding without preInstall.agentReported",
			"dpu", dpu.Name, "timeout", timeout.String())
		return state, true
	}

	return state, false
}
