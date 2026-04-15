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
	"os"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Installing(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	// Check if DPU deletion is requested during OS installation
	if !dpu.DeletionTimestamp.IsZero() {
		logger.Info("DPU deletion requested while in Installing state, cannot delete DPU during OS installation")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, "CannotDeleteWhileInstalling", "Cannot delete DPU during OS installation. Wait for completion or timeout."))
	}

	// Check for installation timeout
	if err := checkInstallationTimeout(state, ctrlCtx.Options.OSInstallTimeout, logger); err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "InstallationTimeout", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	device := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, device); err != nil {
		return *state, err
	}

	client, err := rc.NewTLSClient(ctx, device.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		err = fmt.Errorf("failed to create TLS client: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}

	_, cond := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondBFBTransferred))
	if cond == nil || cond.Status != metav1.ConditionTrue {
		return submitAndMonitorBfbInstallTask(ctx, dpu, ctrlCtx, client)
	}

	resp, system, err := client.GetSystem()
	if err != nil || resp.StatusCode() != http.StatusOK {
		err = fmt.Errorf("failed to get system: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToGetSystem", err.Error()))
		return *state, err
	}

	if system.BootProgress.OemLastState != "OsIsRunning" {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, "OemLastState", system.BootProgress.OemLastState))
		return *state, nil
	}

	_, cond = cutil.GetDPUCondition(state, string(provisioningv1.DPUCondOSInstalled))
	if cond == nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, "BFBInstalled", "BFB installed, waiting for the DPU agent to start"))
		_, cond = cutil.GetDPUCondition(state, string(provisioningv1.DPUCondOSInstalled))
	}

	// wait until the DPU agent is started
	if dpu.Status.AgentStatus == nil || dpu.Status.AgentStatus.LastStartupTime == nil {
		if time.Since(cond.LastTransitionTime.Time) > 20*time.Minute {
			err := fmt.Errorf("DPU agent not started after 20 minutes")
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "DPUAgentNotStarted", err.Error()))
			// NOTE: This differs slightly from the HLD, which specifies retrying Installing after
			// 20 minutes. Since the current Installing phase has no retry mechanism, we fall back
			// to reporting an Error directly.
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
		logger.Info("DPU agent not started, waiting for it to start")
		return *state, nil
	}

	ctrlCtx.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))
	state.Phase = provisioningv1.DPUConfig
	logger.Info("installation finished")
	return *state, nil
}

func submitAndMonitorBfbInstallTask(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, client *rc.Client) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	if dpu.Status.RedfishTaskID == nil {
		bfbRegistryAddr, err := getBFBRegistryAddress(ctx, ctrlCtx)
		if err != nil {
			err = fmt.Errorf("failed to get bfb-registry address: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToGetBFBRegistryAddress", err.Error()))
			return *state, err
		}
		logger.Info("submit BFB install task", "will use bfbRegistry", bfbRegistryAddr, "bfbFile", dpu.Status.BFBFile, "bfcfgFile", dpu.Status.BFCFGFile)
		resp, taskInfo, err := client.InstallBFB(concatBFBAndBFCFGPath(bfbRegistryAddr, dpu.Status.BFBFile, dpu.Status.BFCFGFile))
		if err != nil {
			err = fmt.Errorf("failed to install BFB: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToInstall", err.Error()))
			state.Phase = provisioningv1.DPUError
			// when we transition to ERROR phase, we should return nil to make sure the next Reconcile is trigger by the UPDATE event.
			// If an error is returned, the next Reconcile may be triggered as a retry, leading to installing BFB again.
			// todo: other phases trasitioning to ERROR phase should also follow this pattern
			return *state, nil
		} else if resp.StatusCode() == http.StatusBadRequest && strings.Contains(resp.String(), "Another update is in progress") {
			logger.Info("another update is in progress, waiting for it to finish", "dpuName", dpu.Name)
			return *state, nil
		} else if resp.StatusCode() != http.StatusAccepted {
			err = fmt.Errorf("get status: %s", resp.Status())
			logger.Error(err, "Failed to install BFB", "status", resp.Status(), "body", resp.String())
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToInstall", resp.String()))
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
		// Update the state with the task ID so it's reflected in the returned status
		state.RedfishTaskID = &taskInfo.ID

		logger.Info(fmt.Sprintf("new install task: %+v", *taskInfo))
		return *state, nil
	}

	// check progress
	resp, prog, err := client.CheckTaskProgress(*dpu.Status.RedfishTaskID)
	if err != nil {
		err = fmt.Errorf("failed to check task progress: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToCheckProgress", err.Error()))
		return *state, err
	} else if resp.StatusCode() != http.StatusOK {
		err = fmt.Errorf("get status: %s is not OK", resp.Status())
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToCheckProgress", err.Error()))
		return *state, err
	}
	if prog.TaskState == "Exception" {
		taskErr := fmt.Errorf("task %s is in Exception state: %v", *dpu.Status.RedfishTaskID, resp.String())
		logger.Error(taskErr, "Failed to install BFB")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), taskErr, "FailToInstall", fmt.Sprintf("Task %s is in Exception state: %v", *dpu.Status.RedfishTaskID, prog.Messages)))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	logger.Info(fmt.Sprintf("taskProgress: %+v", prog))
	if prog.PercentComplete < 100 {
		taskProgress := fmt.Sprintf("install task %d%% complete", prog.PercentComplete)
		cond := cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), nil, "TaskProgress", taskProgress)
		cond.Status = metav1.ConditionFalse
		cutil.SetDPUCondition(state, cond)
		return *state, nil
	}

	state.RedfishTaskID = nil
	cond := cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), nil, "", "")
	cond.Status = metav1.ConditionTrue
	cutil.SetDPUCondition(state, cond)
	return *state, nil
}

// getBFBRegistryAddress returns the full bfb-registry address
func getBFBRegistryAddress(ctx context.Context, ctrlCtx *dutil.ControllerContext) (string, error) {
	if ctrlCtx.Options.BFBRegistryLoadBalancer != "" {
		return ctrlCtx.Options.BFBRegistryLoadBalancer, nil
	}
	return cutil.GetBFBRegistryAddressWithPort(ctx, ctrlCtx.Client, os.Getenv("POD_NAMESPACE"), ctrlCtx.Options.BFBRegistry)
}

// concatBFBAndBFCFGPath returns the bfb-registry path of concatenated bfbFile and bfcfgFile
// Given bfbFile is /bfb/file.bfb and bfcfgFile is /bfb/bfcfg/file.cfg,
// it returns /bfb/??file.bfb,bfcfg/file.cfg?/bfb-to-install
func concatBFBAndBFCFGPath(bfbRegistry string, bfbFile string, bfcfgFile string) string {
	schemes := []string{"http://", "https://"}
	for _, prefix := range schemes {
		bfbRegistry = strings.TrimPrefix(bfbRegistry, prefix)
	}
	bfCfg := strings.TrimPrefix(bfcfgFile, "/"+cutil.BFBBaseDir+"/")
	return filepath.Join(bfbRegistry, cutil.BFBBaseDir, fmt.Sprintf("??%s,%s?", filepath.Base(bfbFile), bfCfg), "bfb-to-install")
}

// checkInstallationTimeout checks if the OS installation has exceeded the configured timeout.
// Returns an error if timeout is exceeded, nil otherwise.
func checkInstallationTimeout(state *provisioningv1.DPUStatus, timeout time.Duration, logger logr.Logger) error {
	if timeout <= 0 {
		return nil
	}

	_, bfbPreparedCond := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondBFBPrepared))
	if bfbPreparedCond == nil {
		return nil
	}

	elapsed := time.Since(bfbPreparedCond.LastTransitionTime.Time)
	if elapsed <= timeout {
		return nil
	}

	logger.Info("OS installation timeout exceeded", "elapsed", elapsed, "timeout", timeout)
	return fmt.Errorf("OS installation timeout exceeded: %v > %v", elapsed, timeout)
}
