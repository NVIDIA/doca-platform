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
	_, interfaceInitializedCondition := cutil.GetDPUCondition(&dpu.Status, string(provisioningv1.DPUCondInterfaceInitialized))
	_, rebootCondition := cutil.GetDPUCondition(&dpu.Status, string(provisioningv1.DPUCondRebooted))
	// checking the reboot condition is nil to make sure the controller get the latest DPU object(removing rebooting condition for NIC->DPU mode transition case)
	// In any case, the DPU should not include rebooting conditions at this phase.
	if interfaceInitializedCondition != nil && interfaceInitializedCondition.Status == metav1.ConditionTrue &&
		rebootCondition == nil {
		// If the DPU is in NicMode, we need to set it to DpuMode and reboot（make DPU mode to be active） before the DPU provisioning process.
		if interfaceInitializedCondition.Message == string(provisioningv1.DPUCondMessageModeUpdate) {
			state.Phase = provisioningv1.DPURebooting
		} else {
			state.Phase = provisioningv1.DPUConfigFWParameters
		}
	}
	return *state, nil
}
