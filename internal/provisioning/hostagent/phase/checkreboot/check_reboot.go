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

package checkreboot

import (
	"context"
	"fmt"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	condition = string(provisioningv1.DPUCondCheckedHostRebootNeed)
)

type Handler struct {
	sync.Mutex
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
	dev, ok := h.GetDevice(dpu.Spec.SerialNumber)
	if !ok {
		err := fmt.Errorf("failed to get device by serial number: %s", dpu.Spec.SerialNumber)
		hostutil.NewCondition(condition).Failure(err, "FailedToGetDevice").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	needReboot, err := needReboot(dev.Address)
	if err != nil {
		message := fmt.Sprintf("Warning: failed to check if reboot is needed, attempt reboot. err: %s", err.Error())
		hostutil.NewCondition(condition).Success(message).Set(&dpu.Status.Conditions)
		dpu.Status.RequiredReset = ptr.To(true)
	} else if !needReboot {
		hostutil.NewCondition(condition).Success("No reboot needed").Set(&dpu.Status.Conditions)
		dpu.Status.RequiredReset = ptr.To(false)
	} else {
		hostutil.NewCondition(condition).Success("Reboot needed").Set(&dpu.Status.Conditions)
		dpu.Status.RequiredReset = ptr.To(true)
	}
	return dpu.Status, ctrl.Result{}, nil
}

func needReboot(pciAddress string) (bool, error) {
	command := fmt.Sprintf("mlxreg -d %s.0 --get --reg_name MFRL|grep pci_rescan_required|grep -o '0x[0-9a-fA-F]\\+'", pciAddress)
	stdout, stderr, err := hostutil.RunBash(command)
	if err != nil {
		return false, fmt.Errorf("cmd: %s, stderr: %s", command, stderr.String())
	}
	if stdout.String() == "1" {
		return true, nil
	}
	command = fmt.Sprintf("flint -d %s.0 q |grep 'FW Version'|awk -F ': *' '{print $2}'", pciAddress)
	stdout, stderr, err = hostutil.RunBash(command)
	if err != nil {
		return false, fmt.Errorf("cmd: %s, stderr: %s", command, stderr.String())
	}
	curVer := stdout.String()

	command = fmt.Sprintf("flint -d %s.0 q |grep 'FW Version(Running)'|awk -F ': *' '{print $2}'", pciAddress)
	stdout, stderr, err = hostutil.RunBash(command)
	if err != nil {
		return false, fmt.Errorf("cmd: %s, stderr: %s", command, stderr.String())
	}
	runningVer := stdout.String()
	if curVer == runningVer {
		return false, nil
	}

	resetLevel3 := false
	command = fmt.Sprintf("mlxfwreset -d %s.0 q|grep '3: Driver restart and PCI reset'|grep -oE 'Supported|Not supported|Not Supported'", pciAddress)
	stdout, stderr, err = hostutil.RunBash(command)
	if err != nil {
		return false, fmt.Errorf("cmd: %s, stderr: %s", command, stderr.String())
	}
	resetLevel3 = stdout.String() == "Supported"
	if resetLevel3 {
		return true, nil
	}

	syncLevel1 := false
	command = fmt.Sprintf("mlxfwreset -d %s.0 q|grep '1: Driver is the owner'|grep -oE 'Supported|Not supported|Not Supported'", pciAddress)
	stdout, stderr, err = hostutil.RunBash(command)
	if err != nil {
		return false, fmt.Errorf("cmd: %s, stderr: %s", command, stderr.String())
	}
	syncLevel1 = stdout.String() == "Supported"
	if syncLevel1 {
		return true, nil
	}
	canReset := (resetLevel3 && syncLevel1)
	if !canReset {
		return true, nil
	}
	command = fmt.Sprintf("mlxfwreset -d %s.0 -y -l 3 --sync 1 r", pciAddress)
	stdout, stderr, err = hostutil.RunBash(command)
	if err != nil {
		return false, fmt.Errorf("cmd: %s, stderr: %s", command, stderr.String())
	}
	return false, nil
}
