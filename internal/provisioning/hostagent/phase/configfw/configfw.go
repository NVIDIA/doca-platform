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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	condition = string(provisioningv1.DPUCondFWConfigured)
	// getModeCmd is the command to get the mode of the DPU. The command is copied from DMS source code
	getModeCmd = "mlxconfig -d $TargetPCIAddrFull q INTERNAL_CPU_MODEL | grep INTERNAL_CPU_MODEL.*0 | [ $(wc -l) -eq 1 ] && echo SEPARATE || echo $(mlxconfig -d $TargetPCIAddrFull q INTERNAL_CPU_OFFLOAD_ENGINE | grep INTERNAL_CPU_OFFLOAD_ENGINE.*0 | [ $(wc -l) -eq 1 ] && echo DPU || echo NIC)"
	// setModeCmd is the command to set the mode of the DPU. The command is copied from DMS source code
	setModeCmd = "bf3=$(lspci | grep $TargetPCIAddrFull.*BlueField-3 | [ $(wc -l) -eq 1 ] && echo true || echo false);sep=$([ '$0' == 'SEPARATE' ] && echo 0 || echo 1);sepb=$([ '$0' == 'SEPARATE' ] && echo true || echo false);nic=$([ '$0' == 'NIC' ] && echo 1 || echo 0);mlxconfig -d $TargetPCIAddrFull -y s $(echo INTERNAL_CPU_MODEL=$sep);$sepb || mlxconfig -d $TargetPCIAddrFull -y s $(echo INTERNAL_CPU_OFFLOAD_ENGINE=$nic);params=$(echo INTERNAL_CPU_MODEL=$sep $($sepb || echo INTERNAL_CPU_OFFLOAD_ENGINE=$nic) $($bf3 && echo EXP_ROM_UEFI_ARM_ENABLE=$sep || echo $($sepb || echo INTERNAL_CPU_IB_VPORT0=$nic INTERNAL_CPU_PAGE_SUPPLIER=$nic INTERNAL_CPU_ESWITCH_MANAGER=$nic)));mlxconfig -d $TargetPCIAddrFull -y s $params"
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
	dev, ok := h.GetDevice(dpu.Spec.SerialNumber)
	if !ok {
		return dpu.Status, ctrl.Result{}, fmt.Errorf("device not found")
	}
	flavor := &provisioningv1.DPUFlavor{}
	if err := h.Get(ctx, types.NamespacedName{Name: dpu.Spec.DPUFlavor, Namespace: dpu.Namespace}, flavor); err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToGetDPUFlavor").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	pciAddress := filepath.Base(hostutil.NewPCIHelper(dev.Address).PF(0).Path())
	mode, err := GetDPUMode(pciAddress)
	if err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToGetDPUMode").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	if strings.EqualFold(mode, "DPU") {
		hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, nil
	}
	if err := SetDPUMode(pciAddress); err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToSetDPUMode").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
	return dpu.Status, ctrl.Result{}, nil
}

func GetDPUMode(pciAddress string) (string, error) {
	cmd := strings.ReplaceAll(getModeCmd, "$TargetPCIAddrFull", pciAddress)
	stdout, stderr, err := hostutil.RunBash(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to get mode. cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func SetDPUMode(pciAddress string) error {
	cmd := strings.ReplaceAll(setModeCmd, "$0", "DPU")
	cmd = strings.ReplaceAll(cmd, "$TargetPCIAddrFull", pciAddress)
	_, stderr, err := hostutil.RunBash(cmd)
	if err != nil {
		return fmt.Errorf("failed to set mode. cmd: %s, err: %w, stderr: %s", cmd, err, stderr.String())
	}
	return nil
}
