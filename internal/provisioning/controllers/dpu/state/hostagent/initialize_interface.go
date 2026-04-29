/*
Copyright 2025 NVIDIA

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

package hostagent

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dpustate "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func InitializeInterface(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}
	_, initCond := cutil.GetDPUCondition(&dpu.Status, string(provisioningv1.DPUCondInterfaceInitialized))
	_, rebootCond := cutil.GetDPUCondition(&dpu.Status, provisioningv1.DPUCondRebooted.String())

	if initCond == nil {
		return *state, nil
	}

	if initCond.Status == metav1.ConditionFalse && initCond.Reason == "TrustedHostModeNotSupported" {
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	if initCond.Status != metav1.ConditionTrue {
		return *state, nil
	}

	// Interface initialized. Reboot condition must be absent here (fresh state after NIC->DPU transition).
	// If host agent reported mode update, transition to Rebooting to activate DPU mode before continuing.
	if rebootCond == nil && initCond.Message == string(provisioningv1.DPUCondMessageModeUpdate) {
		state.Phase = provisioningv1.DPURebooting
		if err := dpustate.InitializeDPURebootStatus(ctx, dpu, state, ctrlCtx, provisioningv1.DPUInitializeInterface); err != nil {
			return *state, err
		}
		return *state, nil
	}

	state.Phase = provisioningv1.DPUConfigFWParameters
	return *state, nil
}
