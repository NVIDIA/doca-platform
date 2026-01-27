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

package configfw

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/fluxcd/pkg/runtime/patch"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	condition = string(provisioningv1.DPUCondFWConfigured)
)

type Handler struct {
	client.Client
	GetDevice func(string) (hostutil.Device, bool)
}

func NewHandler(client client.Client, getDevice func(string) (hostutil.Device, bool)) *Handler {
	return &Handler{
		Client:    client,
		GetDevice: getDevice,
	}
}

func (h *Handler) Handle(ctx context.Context, dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error) {
	logger := log.FromContext(ctx)
	dev, ok := h.GetDevice(dpu.Spec.SerialNumber)
	if !ok {
		return dpu.Status, ctrl.Result{}, fmt.Errorf("device not found")
	}
	flavor := &provisioningv1.DPUFlavor{}
	if err := h.Get(ctx, types.NamespacedName{Name: dpu.Spec.DPUFlavor, Namespace: dpu.Namespace}, flavor); err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToGetDPUFlavor").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}

	if len(flavor.Spec.DpuMode) == 0 {
		flavor.Spec.DpuMode = provisioningv1.DpuMode
	}

	if flavor.Spec.DpuMode != provisioningv1.DpuMode {
		err := fmt.Errorf("requested mode %s is not supported by hostagent. Supported mode: %s", flavor.Spec.DpuMode, provisioningv1.DpuMode)
		hostutil.NewCondition(condition).Failure(err, "UnsupportedDPUMode").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}

	pciAddress := filepath.Base(hostutil.NewPCIHelper(dev.Address).PF(0).Path())

	valid, err := h.validateFWVersion(ctx, dpu, pciAddress)
	if err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToGetFWReleaseDate").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	if !valid {
		err = fmt.Errorf("NIC FW release version which is lower than April 2024 is not supported")
		hostutil.NewCondition(condition).Failure(err, "UnsupportedFWVersion").Set(&dpu.Status.Conditions)
		dpu.Status.Phase = provisioningv1.DPUError
		return dpu.Status, ctrl.Result{}, err
	}

	mode, err := hostutil.GetDPUMode(ctx, pciAddress)
	if err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToGetDPUMode").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	if mode == provisioningv1.DpuMode {
		hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
	} else {
		logger.Info("Setting DPU mode to DPU", "current mode", mode, "pciAddress", pciAddress)
		if err := hostutil.SetDPUMode(pciAddress); err != nil {
			hostutil.NewCondition(condition).Failure(err, "FailedToSetDPUMode").Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{}, err
		}
		// DPUCondReasonModeUpdate is the reason for updating the DPU mode in hostagent interface
		// which will be used to select the way of host rebooting in rebooting phase.
		hostutil.NewCondition(condition).Success(string(provisioningv1.DPUCondMessageModeUpdate)).Set(&dpu.Status.Conditions)
		logger.Info("Successfully set DPU mode", "PCI Address", pciAddress)
	}
	return dpu.Status, ctrl.Result{}, nil
}

func (h *Handler) validateFWVersion(ctx context.Context, dpu *provisioningv1.DPU, pciAddress string) (bool, error) {
	logger := log.FromContext(ctx)
	command := fmt.Sprintf("flint -d %s.0 q full | grep 'FW Release Date' | awk -F ': *' '{print $2}'", pciAddress)
	stdout, stderr, err := hostutil.RunBash(command)
	if err != nil {
		logger.Error(err, "Failed to run cmd", "cmd", command, "stdout", stdout.String(), "stderr", stderr.String())
		return false, fmt.Errorf("failed to run cmd: %s, err: %w, stdout: %s, stderr: %s", command, err, stdout.String(), stderr.String())
	}

	layout := "2.1.2006"
	fwReleaseDate, err := time.Parse(layout, strings.TrimSpace(stdout.String()))
	logger.Info("FW release date", "fwReleaseDate", fwReleaseDate)
	if err != nil {
		logger.Error(err, "Failed to parse FW release date", "fwReleaseDate", stdout.String())
		return false, fmt.Errorf("failed to parse FW release date: %w", err)
	}
	year, month := fwReleaseDate.Year(), fwReleaseDate.Month()
	if year > 2024 || (year == 2024 && month > 4) {
		return true, nil
	}

	if year == 2024 && month == 4 {
		// add "host-power-cycle-required" annotation on dpu object
		patcher := patch.NewSerialPatcher(dpu, h.Client)
		if dpu.Annotations == nil {
			dpu.Annotations = make(map[string]string)
		}
		dpu.Annotations[cutil.HostPowerCycleRequireKey] = "true"
		if err := patcher.Patch(ctx, dpu); err != nil {
			logger.Error(err, "Failed to add host-power-cycle-required annotation on dpu object", "dpu", dpu.Name)
			return false, fmt.Errorf("failed to add host-power-cycle-required annotation on dpu object: %w", err)
		}
		return true, nil
	}
	return false, nil
}
