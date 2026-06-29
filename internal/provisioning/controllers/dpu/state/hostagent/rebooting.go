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

package hostagent

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Rebooting(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	state, dpuNode, done, err := dutil.StartRebooting(ctx, dpu, ctrlCtx)
	if done || err != nil {
		return *state, err
	}

	if dpu.Status.Hostless {
		err := fmt.Errorf("hostless DPU %s requires the Redfish reboot handler", dpu.Name)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "HostlessRequiresRedfish", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	skipHW, err := cutil.ShouldSkipHWProvisioning(ctx, ctrlCtx.Client, dpu)
	if err != nil {
		logger.V(3).Info("Failed to check skip-hw-provisioning label, assuming real hardware", "error", err)
	}
	if skipHW {
		logger.Info("skip-hw-provisioning label set - skipping power cycle")
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
		state.Phase = provisioningv1.DPUHostNetworkConfiguration
		return *state, nil
	}

	switch {
	case dpuNode.Spec.NodeRebootMethod == nil:
		err := fmt.Errorf("DPUNode %s has no node reboot method", dpuNode.Name)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "NodeRebootMethodNotProvided", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	case dpuNode.Spec.NodeRebootMethod.GNOI != nil || dpuNode.Spec.NodeRebootMethod.HostAgent != nil: //nolint:staticcheck // GNOI is deprecated but still honored for compatibility.
		return dutil.CompleteRebooting(ctx, dpu, state, false), nil
	case dpuNode.Spec.NodeRebootMethod.External != nil || dpuNode.Spec.NodeRebootMethod.Script != nil:
		return dutil.CompleteRebooting(ctx, dpu, state, ctrlCtx.Options.ZeroTrustProvisioningFlow()), nil
	case dpuNode.Spec.NodeRebootMethod.None != nil:
		err := fmt.Errorf("DPUNode %s uses nodeRebootMethod none, but DPU %s is not marked hostless", dpuNode.Name, dpu.Name)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "HostlessStatusNotSet", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	default:
		err := fmt.Errorf("DPUNode %s has an unsupported node reboot method", dpuNode.Name)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "UnsupportedNodeRebootMethod", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}
}
