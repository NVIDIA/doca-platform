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
	"path/filepath"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"

	types "k8s.io/apimachinery/pkg/types"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Installing(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	taskName := fmt.Sprintf("%s-%s", dpu.Name, dpu.UID)
	defer func(oldPhase provisioningv1.DPUPhase) {
		if oldPhase == state.Phase {
			return
		}
		dutil.OsInstallTaskMap.Delete(taskName)
	}(state.Phase)

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
	taskID, ok := dutil.OsInstallTaskMap.Load(taskName)
	if !ok {
		resp, taskInfo, err := client.InstallBFB(concatBFBAndBFCFGPath(ctrlCtx.Options.BFBRegistry, dpu.Status.BFBFile, dpu.Status.BFCFGFile))
		if err != nil {
			err = fmt.Errorf("failed to install BFB: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToInstall", err.Error()))
			state.Phase = provisioningv1.DPUError
			// when we transition to ERROR phase, we should return nil to make sure the next Reconcile is trigger by the UPDATE event.
			// If an error is returned, the next Reconcile may be triggered as a retry, leading to installing BFB again.
			// todo: other phases trasitioning to ERROR phase should also follow this pattern
			return *state, nil
		} else if resp.StatusCode() != http.StatusAccepted {
			err = fmt.Errorf("get status: %s", resp.Status())
			logger.Error(err, "Failed to install BFB", "status", resp.Status(), "body", resp.String())
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToInstall", err.Error()))
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
		dutil.OsInstallTaskMap.Store(taskName, taskInfo.ID)
		logger.Info(fmt.Sprintf("new install task: %+v", *taskInfo))
		return *state, nil
	}

	// check progress
	resp, prog, err := client.CheckTaskProgress(taskID.(string))
	if err != nil {
		err = fmt.Errorf("failed to check task progress: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToCheckProgress", err.Error()))
		return *state, err
	} else if resp.StatusCode() != http.StatusOK {
		err = fmt.Errorf("get status: %s is not OK", resp.Status())
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToCheckProgress", err.Error()))
		return *state, err
	}
	logger.Info(fmt.Sprintf("taskProgress: %+v", prog))
	if prog.TaskState == "Exception" {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToInstall", fmt.Sprintf("Task %s is in Exception state: %v", taskID.(string), prog.Messages)))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}
	if prog.PercentComplete < 100 {
		taskProgress := fmt.Sprintf("install task %d%% complete", prog.PercentComplete)
		logger.Info(taskProgress)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, "TaskProgress", taskProgress))
		return *state, nil
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

	_, cond := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondOSInstalled))
	if cond == nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, "", ""))
		_, cond = cutil.GetDPUCondition(state, string(provisioningv1.DPUCondOSInstalled))
	}

	// Wait 3 minutes to finish cloud-init
	if time.Since(cond.LastTransitionTime.Time) < 3*time.Minute {
		return *state, nil
	}

	patch := kclient.MergeFrom(dpu.DeepCopy())
	if dpu.Spec.Cluster.NodeLabels == nil {
		dpu.Spec.Cluster.NodeLabels = make(map[string]string)
	}
	dpu.Spec.Cluster.NodeLabels[cutil.HostNameDPULabelKey] = dpu.Spec.DPUNodeName
	// Set the DPU version in the node labels. This is necessary to be able to schedule Pods
	// on the DPU Node based on the DPF version.
	// The DPF version in the status should never be nil, but we check it here to avoid nil pointer dereference.
	if dpu.Status.DPFVersion != nil {
		dpu.Spec.Cluster.NodeLabels[release.DPFVersionLabelKey] = *dpu.Status.DPFVersion
	}
	if err := ctrlCtx.Client.Patch(ctx, dpu, patch); err != nil {
		err = fmt.Errorf("failed to patch DPU: %w", err)
		return *state, err
	}

	ctrlCtx.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))
	state.Phase = provisioningv1.DPURebooting
	logger.Info("installation finished")
	return *state, nil
}

// concatBFBAndBFCFGPath returns the bfb-registry path of concatenated bfbFile and bfcfgFile
// Given bfbFile is /bfb/file.bfb and bfcfgFile is /bfb/bfcfg/file.cfg,
// it returns /bfb/??file.bfb,bfcfg/file.cfg?/bfb-to-install
func concatBFBAndBFCFGPath(bfbRegistry string, bfbFile string, bfcfgFile string) string {
	return filepath.Join(bfbRegistry, cutil.BFBBaseDir, fmt.Sprintf("??%s,%s?", filepath.Base(bfbFile), bfcfgFile), "bfb-to-install")
}
