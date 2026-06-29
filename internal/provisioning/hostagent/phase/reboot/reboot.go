//go:build linux

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

package reboot

import (
	"bytes"
	"context"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type RebootRequest struct {
	DPUName      string `json:"dpuName"`
	DPUNamespace string `json:"dpuNamespace"`
	UID          string `json:"uid"`
	RebootID     string `json:"rebootID"`
	// Host reboot is currently requested from two provisioning paths:
	// DPUInitializeInterface and DPUConfig. The InitializeInterface-triggered reboot
	// happens earlier in the flow, so when DPUConfig later requests another reboot,
	// hostagent must recognize that the older request file belongs to the earlier
	// InitializeInterface reboot and is therefore stale.
	PreviousPhase provisioningv1.DPUPhase `json:"previousPhase,omitempty"`
	// DPUConfig may loop through DPUConfig -> DPURebooting -> DPUConfig multiple
	// times. RebootSequenceCount distinguishes those repeated DPUConfig-triggered
	// reboot requests so hostagent does not mistake a request file from a previous
	// DPUConfig reboot for evidence that the current reboot already happened.
	RebootSequenceCount *int32 `json:"rebootSequenceCount,omitempty"`
}

type Handler struct {
	client.Client
	bootIDStore                 BootIDStore
	getNodeNameFunc             func() string
	getDeviceBySerialNumberFunc func(string) (hostutil.Device, bool)
	getDPUNodeFunc              func(context.Context) (*provisioningv1.DPUNode, error)
	listDPUFunc                 func(context.Context) ([]provisioningv1.DPU, error)
	runPowerCycleCmdFunc        func(string) (bytes.Buffer, bytes.Buffer, error)
	runShutdownARMFunc          func(string) (bytes.Buffer, bytes.Buffer, error)
	runRebootHostFunc           func(string) (bytes.Buffer, bytes.Buffer, error)
	getRshimNameByPCIFunc       func(string) (string, error)
	isDPUOffFunc                func(string) (bool, string, error)
}

func NewHandler(client client.Client, nodeFunc func() string, snFunc func(string) (hostutil.Device, bool)) *Handler {
	h := &Handler{
		Client:                      client,
		bootIDStore:                 NewFileSystemStore(client, BootIDDir),
		getNodeNameFunc:             nodeFunc,
		getDeviceBySerialNumberFunc: snFunc,
	}
	h.Start()
	return h
}

// updateDPURebootStatus delegates to util.UpdateDPURebootStatus (shared with DPUNodeReconciler).
func (r *Handler) updateDPURebootStatus(ctx context.Context, dpu *provisioningv1.DPU, phase provisioningv1.RebootStatusPhase, reason string, message string) error {
	return cutil.UpdateDPURebootStatus(ctx, r.Client, dpu, phase, reason, message)
}

func (r *Handler) Handle(ctx context.Context, dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// if the label check fails, proceed with real hardware provisioning.
	skipHW, err := cutil.ShouldSkipHWProvisioning(ctx, r.Client, dpu)
	if err != nil {
		logger.V(3).Info("Failed to check skip-hw-provisioning label", "error", err)
	}
	if skipHW {
		logger.Info("skip-hw-provisioning label set - skipping reboot")
		if err := r.updateDPURebootStatus(ctx, dpu, provisioningv1.RebootStatusSucceeded,
			"ModeUpdateRebootCompleted", string(provisioningv1.DPUCondMessageRebootFinishedForModeUpdate)); err != nil {
			logger.Error(err, "Failed to update DPU reboot status")
		}
		return dpu.Status, ctrl.Result{}, nil
	}

	finished, err := r.bootIDStore.IsRebootFinished(dpu)
	if err != nil {
		logger.Error(err, "Failed to check reboot progress")
		return dpu.Status, ctrl.Result{}, err
	}
	if finished {
		logger.Info("Reboot finished")
		_, interfaceInitializedCondition := cutil.GetDPUCondition(&dpu.Status, string(provisioningv1.DPUCondInterfaceInitialized))
		var err error
		if interfaceInitializedCondition != nil && interfaceInitializedCondition.Message == string(provisioningv1.DPUCondMessageModeUpdate) {
			// RebootStatus.message DPUCondMessageRebootFinishedForModeUpdate: IsRebootFinished(store); controller mirrors to DPUCondRebooted.
			err = r.updateDPURebootStatus(ctx, dpu, provisioningv1.RebootStatusSucceeded,
				"ModeUpdateRebootCompleted", string(provisioningv1.DPUCondMessageRebootFinishedForModeUpdate))
		} else {
			err = r.updateDPURebootStatus(ctx, dpu, provisioningv1.RebootStatusSucceeded, "", "")
		}
		if err != nil {
			logger.Error(err, "Failed to update DPU reboot status")
			return dpu.Status, ctrl.Result{}, err
		}
	}
	return dpu.Status, ctrl.Result{}, nil
}

func (r *Handler) Start() {
	go wait.PollUntilContextCancel(context.TODO(), 30*time.Second, true, func(ctx context.Context) (bool, error) { //nolint:errcheck
		r.run()
		return false, nil
	})
}
