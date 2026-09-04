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

package redfish

import (
	"context"
	"fmt"
	"net/http"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const waitingForBMCRShimReason = "WaitingForBMCRShim"

func ConfigFWParameters(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	device := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, device); err != nil {
		cutil.SetDPUCondition(state,
			cutil.NewCondition(
				provisioningv1.DPUCondFWConfigured.String(),
				err,
				"FailedToGetDPUDevice",
				err.Error()))

		return *state, err
	}

	if device.Labels[provisioningv1.DPUDeviceLabelSkipHWProvisioning] == "true" {
		logger.Info("skip-hw-provisioning label set - skipping firmware configuration")
		state.Phase = provisioningv1.DPUPrepareBFB
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondFWConfigured, "", ""))
		return *state, nil
	}

	if cutil.PreInstallAgentReported(dpu) && !cutil.PreInstallNVConfigReported(dpu) {
		// This phase waits on the agent with no timeout, so its RBAC has to be re-ensured on
		// every reconcile: a Role/RoleBinding garbage-collected with the previous DPUSet
		// generation would otherwise leave the agent 403-denied and the DPU stuck here forever.
		if err := cutil.EnsureDPUAgentRole(ctx, ctrlCtx.Client, dpu, device); err != nil {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToEnsureAgentRBAC", err.Error()))
			return *state, err
		}
		logger.V(2).Info("reprovision: waiting for agent preInstall NVConfigApplied before Config FW Parameters (Redfish)")
		return *state, nil
	}

	client, err := rc.NewTLSClient(ctx, device.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUConfigFWParameters), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}

	if dpu.Status.DPUType == provisioningv1.DPUTypeBlueField4 {
		resp, _, err := client.SetHostPrivilegeRestricted()
		if err != nil {
			err = fmt.Errorf("failed to set host privilege to restricted: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToSetHostPrivilege", err.Error()))
			return *state, err
		} else if resp.StatusCode() != http.StatusOK {
			err = fmt.Errorf("status code: %d is not OK", resp.StatusCode())
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToSetHostPrivilege", err.Error()))
			return *state, err
		}
		logger.Info("successfully set host privilege to restricted via BMC")

		state.Phase = provisioningv1.DPUUpdateFirmware
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondFWConfigured, "", ""))
		return *state, nil
	}

	flavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUFlavor}, flavor); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "DPUFlavorNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToGetDpuFlavor", err.Error()))
		return *state, err
	}

	if !isWaitingForBMCRShim(state) {
		// Note: this does NOT terminate running rshim on host
		resp, _, err := client.DisableHostRshim()
		if err != nil {
			err = fmt.Errorf("failed to disable host Rshim: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToDisableHostRshim", err.Error()))
			return *state, err
		} else if resp.StatusCode() != http.StatusOK {
			err = fmt.Errorf("status code: %d is not OK", resp.StatusCode())
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToDisableHostRshim", err.Error()))
			return *state, err
		}
		logger.Info("successfully disabled host RShim")

		resp, _, err = client.EnableBMCRShim()
		if err != nil {
			err = fmt.Errorf("failed to enable BMC Rshim: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToEnableBMCRshim", err.Error()))
			return *state, err
		} else if resp.StatusCode() != http.StatusOK {
			err = fmt.Errorf("status code: %d is not OK", resp.StatusCode())
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToEnableBMCRshim", err.Error()))
			return *state, err
		}
	}

	enabled, _, err := client.GetBMCRShimEnabled()
	if err != nil {
		err = fmt.Errorf("failed to get BMC RShim status: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToGetBMCRShim", err.Error()))
		return *state, err
	}
	if !enabled {
		// BMC enable is asynchronous: PATCH success does not mean BmcRShimEnabled is true yet.
		waitErr := fmt.Errorf("waiting for BMC RShim to become enabled")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), waitErr, waitingForBMCRShimReason, ""))
		return *state, nil
	}
	logger.Info("BMC RShim is enabled")

	state.Phase = provisioningv1.DPUPrepareBFB
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondFWConfigured, "", ""))
	return *state, nil
}

func isWaitingForBMCRShim(state *provisioningv1.DPUStatus) bool {
	_, cond := cutil.GetDPUCondition(state, provisioningv1.DPUCondFWConfigured.String())
	return cond != nil && cond.Reason == waitingForBMCRShimReason
}
