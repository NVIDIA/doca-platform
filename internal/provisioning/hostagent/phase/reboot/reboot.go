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
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	condition = string(provisioningv1.DPUCondRebooted)
)

type RebootRequest struct {
	DPUName      string `json:"dpuName"`
	DPUNamespace string `json:"dpuNamespace"`
	UID          string `json:"uid"`
	RebootID     string `json:"rebootID"`
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

func (r *Handler) Handle(ctx context.Context, dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error) {
	logger := log.FromContext(ctx)
	finished, err := r.bootIDStore.IsRebootFinished(dpu)
	if err != nil {
		logger.Error(err, "Failed to check reboot progress")
		return dpu.Status, ctrl.Result{}, err
	}
	if finished {
		logger.Info("Reboot finished")
		_, interfaceInitializedCondition := cutil.GetDPUCondition(&dpu.Status, string(provisioningv1.DPUCondInterfaceInitialized))
		if interfaceInitializedCondition != nil && interfaceInitializedCondition.Message == string(provisioningv1.DPUCondMessageModeUpdate) {
			// set the condition to success for DPU mode transition case(NIC->DPU mode)
			hostutil.NewCondition(condition).Success(string(provisioningv1.DPUCondMessageRebootFinishedForModeUpdate)).Set(&dpu.Status.Conditions)
		} else {
			hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
		}
	}
	return dpu.Status, ctrl.Result{}, nil
}

func (r *Handler) Start() {
	go wait.PollUntilContextCancel(context.TODO(), 30*time.Second, true, func(ctx context.Context) (bool, error) { //nolint:errcheck
		failedDPUs := r.run()
		if len(failedDPUs) == 0 {
			return false, nil
		}
		for _, dpu := range failedDPUs {
			if err := r.Status().Update(ctx, dpu); err != nil {
				klog.Errorf("failed to update reboot failure condition for DPU %s: %v", dpu.Name, err)
			}
		}
		return false, nil
	})
}
