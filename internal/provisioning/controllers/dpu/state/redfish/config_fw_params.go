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
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	waitingForBMCRShimReason  = "WaitingForBMCRShim"
	bmcRShimWaitTimeoutReason = "BMCRShimWaitTimeout"
	armPowerOnTimeoutReason   = "DPUArmPowerOnTimeout"
	lowPowerStandbyReason     = "BMCRShimUnavailableLowPowerStandby"

	armPowerOnWaitTimeout = 5 * time.Minute
	bmcRShimWaitTimeout   = 10 * time.Minute
)

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
		if err := cutil.EnsureDPUAgentRole(ctx, ctrlCtx.Client, dpu, device, ctrlCtx.Options.CATrustBundleConfigMapName); err != nil {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToEnsureAgentRBAC", err.Error()))
			return *state, err
		}
		logger.V(2).Info("reprovision: waiting for agent preInstall NVConfigApplied before Config FW Parameters (Redfish)")
		return *state, nil
	}

	readCtx, cancelRead := rc.ReadContext(ctx)
	defer cancelRead()
	client, err := ctrlCtx.RedfishProvider().AcquireForDevice(readCtx, dpu.Namespace, device)
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

	_, system, err := client.GetSystem(readCtx)
	if err != nil {
		err = fmt.Errorf("failed to get system: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToGetSystem", err.Error()))
		return *state, err
	}
	if isDPUArmPoweredOff(system) {
		// A powered-off Arm invalidates any earlier BMC RShim request.
		removeCondition(state, provisioningv1.DPUCondFWBMCRShimRequested)
		_, armCond := cutil.GetDPUCondition(state, provisioningv1.DPUCondFWArmRestarted.String())
		switch {
		case armCond == nil || armCond.Status != metav1.ConditionTrue:
			if _, err := client.ForceRestartDPUArm(ctx); err != nil {
				err = fmt.Errorf("failed to reset DPU Arm: %w", err)
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToResetDPUArm", err.Error()))
				return *state, err
			}
			logger.Info("successfully reset DPU Arm. Waiting for DPU Arm to become powered on...")
			cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondFWArmRestarted, "", ""))
		case time.Since(armCond.LastTransitionTime.Time) > armPowerOnWaitTimeout:
			// Not terminal: an operator chassis reset must let provisioning continue by itself.
			reason := armPowerOnTimeoutReason
			msg := fmt.Sprintf("DPU Arm did not power on within %s after ForceRestart (PowerState %q, Status.State %q)",
				armPowerOnWaitTimeout, system.PowerState, system.Status.State)
			// LowPowerStandby is a BF3-only state; BF4 returns before this phase's RShim logic.
			if isLowPowerStandby(system) {
				reason = lowPowerStandbyReason
				msg += "; BMC RShim cannot attach in LowPowerStandby. Collect a BMC dump, then run 'ipmitool chassis power reset' on the DPU BMC; provisioning resumes automatically"
			}
			markStalled(ctx, state, dpu, ctrlCtx, reason, msg)
			return *state, nil
		}
		// Keep the message empty/stable so LastTransitionTime stays anchored for the timeout.
		waitErr := fmt.Errorf("waiting for DPU Arm to power on after ForceRestart")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), waitErr, "WaitingForDPUArmPowerOn", ""))
		return *state, nil
	}

	// The marker gates the ownership-changing calls (once per request) and anchors the wait timer.
	_, requested := cutil.GetDPUCondition(state, provisioningv1.DPUCondFWBMCRShimRequested.String())
	if requested == nil {
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
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondFWBMCRShimRequested, "", ""))
		_, requested = cutil.GetDPUCondition(state, provisioningv1.DPUCondFWBMCRShimRequested.String())
	}

	enabled, _, err := client.GetBMCRShimEnabled(ctx)
	if err != nil {
		err = fmt.Errorf("failed to get BMC RShim status: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToGetBMCRShim", err.Error()))
		return *state, err
	}
	if !enabled {
		// BMC enable is asynchronous: PATCH success does not mean BmcRShimEnabled is true yet.
		if time.Since(requested.LastTransitionTime.Time) > bmcRShimWaitTimeout {
			markStalled(ctx, state, dpu, ctrlCtx, bmcRShimWaitTimeoutReason,
				fmt.Sprintf("BMC RShim not enabled %s after the request (PowerState %q, Status.State %q, OemLastState %q); see the Zero Trust RShim troubleshooting guide",
					bmcRShimWaitTimeout, system.PowerState, system.Status.State, system.BootProgress.OemLastState))
			return *state, nil
		}
		waitErr := fmt.Errorf("waiting for BMC RShim to become enabled")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), waitErr, waitingForBMCRShimReason, ""))
		return *state, nil
	}
	logger.Info("BMC RShim is enabled")
	// Reset the per-attempt markers so a reprovision starts clean (a stale ArmRestarted would skip the ForceRestart).
	for _, t := range []provisioningv1.DPUConditionType{provisioningv1.DPUCondFWBMCRShimStalled, provisioningv1.DPUCondFWBMCRShimRequested, provisioningv1.DPUCondFWArmRestarted} {
		removeCondition(state, t)
	}

	state.Phase = provisioningv1.DPUPrepareBFB
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondFWConfigured, "", ""))
	return *state, nil
}

// isLowPowerStandby matches the signature seen when the BMC cannot attach rshim (redmine 5261461/5263635).
// It is a diagnostic-only subset of isDPUArmPoweredOff (no behavior depends on it) and only runs for BF3: BF4 returns before this phase's rshim logic.
func isLowPowerStandby(system *rc.SystemInfo) bool {
	return system != nil && system.PowerState == "Paused" && system.Status.State == "StandbyOffline" &&
		system.BootProgress.OemLastState == "LowPowerStandby"
}

// removeCondition removes every condition of the given type from the DPU status.
func removeCondition(state *provisioningv1.DPUStatus, condType provisioningv1.DPUConditionType) {
	state.Conditions = slices.DeleteFunc(state.Conditions, func(c metav1.Condition) bool { return c.Type == condType.String() })
}

// markStalled flags a wait that exceeded its bound without leaving the phase, and emits a Warning event once per reason.
func markStalled(ctx context.Context, state *provisioningv1.DPUStatus, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, reason, msg string) {
	_, prev := cutil.GetDPUCondition(state, provisioningv1.DPUCondFWBMCRShimStalled.String())
	isNew := prev == nil || prev.Reason != reason
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondFWBMCRShimStalled, reason, msg))
	err := errors.New(msg)
	cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, reason, msg))
	if isNew {
		log.FromContext(ctx).Error(err, "Config FW Parameters stalled", "reason", reason)
		if ctrlCtx.Recorder != nil {
			ctrlCtx.Recorder.Event(dpu, corev1.EventTypeWarning, reason, msg)
		}
	}
}
