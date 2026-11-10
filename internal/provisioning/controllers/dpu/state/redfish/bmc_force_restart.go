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

package redfish

import (
	"context"
	"fmt"
	"net/http"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	bmcRestartReasonRequested = "Requested"
	bmcRestartReasonFailed    = "FailedToRequestBMCReset"
	bmcRestartReasonCompleted = "Completed"
)

// BMCForceRestart force restarts the BMC after a BF4 PLDM bundle activation
// and moves the DPU to PrepareBFB once the BMC reports a new LastResetTime.
// This phase is a workaround for NVBug 6720772. Remove it once that bug is
// fixed.
func BMCForceRestart(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	if err := checkFirmwareUpdateTimeout(state, ctrlCtx.Options.FirmwareUpdateTimeout); err != nil {
		logger.Error(err, "Firmware update timeout exceeded while waiting for the BMC to restart")
		setBMCRestartCondition(state, metav1.ConditionFalse, "FirmwareUpdateTimeout", err.Error())
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		setBMCRestartCondition(state, metav1.ConditionFalse, "FailedToGetDPUDevice", err.Error())
		return *state, err
	}
	logger = logger.WithValues("bmc", dpuDevice.BMCAddress())
	ctx = log.IntoContext(ctx, logger)

	readCtx, cancelRead := rc.ReadContext(ctx)
	defer cancelRead()
	client, err := ctrlCtx.RedfishProvider().AcquireForDevice(readCtx, dpu.Namespace, dpuDevice)
	if err != nil {
		setBMCRestartCondition(state, metav1.ConditionFalse, "FailedToCreateClient", err.Error())
		return *state, err
	}

	_, manager, err := client.GetBmcManager(ctx)
	if err != nil {
		if state.BMCLastResetTimeBeforeRestart == nil {
			// The BMC is expected to be up before the restart; report the read failure.
			setBMCRestartCondition(state, metav1.ConditionFalse, "FailedToGetBMCManager", err.Error())
			return *state, fmt.Errorf("failed to read the BMC manager: %w", err)
		}
		logger.Info("BMC is not answering, waiting", "error", err.Error())
		return *state, nil
	}

	// A recorded LastResetTime means the restart request was accepted
	if state.BMCLastResetTimeBeforeRestart != nil {
		if manager.LastResetTime != *state.BMCLastResetTimeBeforeRestart {
			// The restarted BMC never answers on a connection opened before the restart. Close
			// the pooled ones, or the next write within the transport's idle timeout reuses one
			// and waits on it until the kernel gives up.
			client.CloseIdleConnections()
			logger.Info("BMC is back after the restart. Moving to Prepare BFB phase", "lastResetTime", manager.LastResetTime)
			setBMCRestartCondition(state, metav1.ConditionTrue, bmcRestartReasonCompleted, "BMC restarted after the bundle activation")
			state.Phase = provisioningv1.DPUPrepareBFB
			return *state, nil
		}
		logger.Info("BMC has not restarted yet, waiting", "lastResetTime", manager.LastResetTime)
		return *state, nil
	}

	resp, _, err := client.ForceRestartBMC(ctx)
	if err != nil {
		setBMCRestartCondition(state, metav1.ConditionFalse, bmcRestartReasonFailed, err.Error())
		return *state, fmt.Errorf("failed to request the BMC restart: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		err := fmt.Errorf("unexpected status code from Manager.Reset: %s", resp.Status())
		setBMCRestartCondition(state, metav1.ConditionFalse, bmcRestartReasonFailed, err.Error())
		return *state, err
	}

	logger.Info("BMC restart requested, waiting for the BMC to come back", "lastResetTime", manager.LastResetTime)
	state.BMCLastResetTimeBeforeRestart = ptr.To(manager.LastResetTime)
	setBMCRestartCondition(state, metav1.ConditionFalse, bmcRestartReasonRequested, "BMC restart requested, waiting for the BMC to come back")
	return *state, nil
}

func setBMCRestartCondition(state *provisioningv1.DPUStatus, status metav1.ConditionStatus, reason, message string) {
	cutil.SetDPUCondition(state, &metav1.Condition{
		Type:    provisioningv1.DPUCondBMCForceRestarted.String(),
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}
