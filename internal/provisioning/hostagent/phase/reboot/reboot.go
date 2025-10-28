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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	BootIDDir         = "/var/lib/dpf/hostagent/boot_id"
	SystemdBootIDFile = "/proc/sys/kernel/random/boot_id"
	condition         = string(provisioningv1.DPUCondRebooted)
)

type RebootRequest struct {
	DPUName      string `json:"dpuName"`
	DPUNamespace string `json:"dpuNamespace"`
	UID          string `json:"uid"`
	RebootID     string `json:"rebootID"`
}

type Handler struct {
	client.Client
	needReboot                  map[types.UID]*provisioningv1.DPU
	getNodeNameFunc             func() string
	getDeviceBySerialNumberFunc func(string) (hostutil.Device, bool)
	getDPUNodeFunc              func(context.Context) (*provisioningv1.DPUNode, error)
	listDPUFunc                 func(context.Context) ([]provisioningv1.DPU, error)
	persistBootIDFunc           func(*provisioningv1.DPU, bool) error
	runPowerCycleCmdFunc        func(string) (bytes.Buffer, bytes.Buffer, error)
	runShutdownARMFunc          func(string) (bytes.Buffer, bytes.Buffer, error)
	runRebootHostFunc           func(string) (bytes.Buffer, bytes.Buffer, error)
	getRshimNameByPCIFunc       func(string) (string, error)
	isDPUOffFunc                func(string) (bool, string, error)
	isRebootFinishedFunc        func(*provisioningv1.DPU) (bool, error)
}

func NewHandler(client client.Client, nodeFunc func() string, snFunc func(string) (hostutil.Device, bool)) *Handler {
	h := &Handler{
		needReboot:                  make(map[types.UID]*provisioningv1.DPU),
		Client:                      client,
		getNodeNameFunc:             nodeFunc,
		getDeviceBySerialNumberFunc: snFunc,
	}
	h.Start()
	return h
}

func (r *Handler) Handle(ctx context.Context, dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error) {
	logger := log.FromContext(ctx)
	finished, err := r.rebootFinished(dpu)
	if err != nil {
		logger.Error(err, "Failed to check reboot progress")
		return dpu.Status, ctrl.Result{}, err
	}
	if finished {
		hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
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
				klog.Errorf("failed to update DPU: %v", err)
			}
		}
		return false, nil
	})
}

func (r *Handler) rebootFinished(dpu *provisioningv1.DPU) (bool, error) {
	f := r.isRebootFinishedFunc
	if f == nil {
		f = rebootFinished
	}
	return f(dpu)
}

func rebootFinished(dpu *provisioningv1.DPU) (bool, error) {
	dpuBootID, err := readDPUBootID(dpu)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		return false, nil
	}
	currentBootID, err := os.ReadFile(SystemdBootIDFile)
	if err != nil {
		return false, fmt.Errorf("failed to read current boot ID, err: %v", err)
	}
	return dpuBootID != string(currentBootID), nil
}

func rebootRequestFileName(dpu *provisioningv1.DPU) string {
	return filepath.Join(BootIDDir, string(dpu.UID))
}

func readDPUBootID(dpu *provisioningv1.DPU) (string, error) {
	data, err := os.ReadFile(rebootRequestFileName(dpu))
	if err != nil {
		return "", err
	}
	request := &RebootRequest{}
	err = json.Unmarshal(data, request)
	if err != nil {
		return "", err
	}
	return request.RebootID, nil
}

func writeDPUBootIDFile(dpu *provisioningv1.DPU, skip bool) error {
	bootID := ""
	if skip {
		bootID = "skip"
	} else {
		systemBootID, err := os.ReadFile(SystemdBootIDFile)
		if err != nil {
			return err
		}
		bootID = string(systemBootID)
	}
	request := &RebootRequest{
		DPUName:      dpu.Name,
		DPUNamespace: dpu.Namespace,
		UID:          string(dpu.UID),
		RebootID:     bootID,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return err
	}
	return hostutil.AtomicWrite(rebootRequestFileName(dpu), requestBytes, 0644)
}
