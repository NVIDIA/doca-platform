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
	"regexp"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

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

	mode, err := GetDPUMode(ctx, pciAddress)
	if err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToGetDPUMode").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	if strings.EqualFold(mode, "DPU") {
		hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
	} else {
		logger.Info("Setting DPU mode to DPU", "current mode", mode, "pciAddress", pciAddress)
		if err := SetDPUMode(pciAddress); err != nil {
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

func GetDPUMode(ctx context.Context, pciAddress string) (string, error) {
	logger := log.FromContext(ctx)
	cmd := fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure --address 127.0.0.1:9339 --target %s get --path /nvidia/mode/config/mode", pciAddress)
	if stdout, stderr, err := hostutil.RunBash(cmd); err != nil {
		return "", fmt.Errorf("failed to run cmd: %s, err: %w, stdout: %s, stderr: %s", cmd, err, stdout.String(), stderr.String())
	} else {
		logger.Info(fmt.Sprintf("Get mode of %s output: %s", pciAddress, stdout.String()))
		// dmsc outputs the mode in a pretty weird format:
		//[
		//	{
		//	  "source": "127.0.0.1:9339",
		//	  "timestamp": 1761796906478936518,
		//	  "time": "2025-10-30T04:01:46.478936518Z",
		//	  "target": "c9:00.0",
		//	  "updates": [
		//		{
		//		  "Path": "nvidia/mode/config/mode",
		//		  "values": {
		//			"nvidia/mode/config/mode": "DPU"
		//		  }
		//		}
		//	  ]
		//	}
		//]

		pattern := `"nvidia/mode/config/mode"\s*:\s*"([^"]+)"`
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(stdout.String())
		if len(matches) > 1 {
			return matches[1], nil
		}
		return "", fmt.Errorf("failed to parse DPU mode from: %s", stdout.String())
	}
}

func SetDPUMode(pciAddress string) error {
	// DMS will use the PCI address without the "0000:" prefix to determine if the device is BlueField3.
	pciAddress = strings.TrimPrefix(pciAddress, "0000:")
	cmd := fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure --address 127.0.0.1:9339 --target %s set --update /nvidia/mode/config/mode:::string:::DPU", pciAddress)
	if stdout, stderr, err := hostutil.RunBash(cmd); err != nil {
		return fmt.Errorf("failed to run cmd: %s, err: %w, stdout: %s, stderr: %s", cmd, err, stdout.String(), stderr.String())
	}
	return nil
}
