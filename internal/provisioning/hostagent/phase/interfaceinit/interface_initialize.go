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

package interfaceinit

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	condition = string(provisioningv1.DPUCondInterfaceInitialized)
)

type Handler struct {
	client.Client
	GetDevice  func(string) (hostutil.Device, bool)
	QueryRshim func(pciAddress string) (*hostutil.RshimInfo, error)
	GetDPUMode func(ctx context.Context, pciAddress string) (provisioningv1.DpuModeType, error)
}

func NewHandler(client client.Client, getDevice func(string) (hostutil.Device, bool)) *Handler {
	return &Handler{
		Client:     client,
		GetDevice:  getDevice,
		QueryRshim: hostutil.QueryRshimByPCI,
		GetDPUMode: hostutil.GetDPUMode,
	}
}

func (h *Handler) Handle(ctx context.Context, dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error) {
	logger := log.FromContext(ctx)
	pciAddressForQueryMode := filepath.Base(hostutil.NewPCIHelper(*dpu.Spec.PCIAddress).PF(0).Path())
	mode, err := h.GetDPUMode(ctx, pciAddressForQueryMode)
	if err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToGetDPUMode").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	dpu.Status.DPUMode = mode
	if mode == provisioningv1.DpuMode {
		logger.Info(fmt.Sprintf("DPU %s successfully set to DpuMode", dpu.Name))
	} else {
		logger.Info(fmt.Sprintf("DPU %s is in NicMode. Setting DPU mode to DpuMode", dpu.Name))
		if err := hostutil.SetDPUMode(pciAddressForQueryMode); err != nil {
			hostutil.NewCondition(condition).Failure(err, "FailedToSetDPUMode").Set(&dpu.Status.Conditions)
			return dpu.Status, ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("DPU %s is in NicMode. Set DPU mode to DpuMode, requires host power cycle", dpu.Name))
		hostutil.NewCondition(condition).Success(string(provisioningv1.DPUCondMessageModeUpdate)).Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, nil
	}

	// Validate Secure Boot state if requested (Trusted Host cannot configure, only validate).
	if dpu.Spec.SecureBoot != nil {
		mismatch, err := h.validateSecureBoot(ctx, dpu)
		if err != nil {
			return dpu.Status, ctrl.Result{}, err
		}
		if mismatch {
			return dpu.Status, ctrl.Result{}, nil
		}
	}

	capacityResult, err := h.canSatisfy(ctx, dpu)
	if err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToCheckCapacity").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	switch capacityResult {
	case dutil.CapacityUnknown:
		// send a warning in the condition message, but continue the flow
		hostutil.NewCondition(condition).
			Success("WARNING: unable to check DPU CPU/Memory capacity, the DPUFlavor may be unfit for the DPU").
			Set(&dpu.Status.Conditions)
	case dutil.CapacityInsufficient:
		err := fmt.Errorf("not enough resources for the given DPUFlavor")
		hostutil.NewCondition(condition).Failure(err, "InsufficientResources").Set(&dpu.Status.Conditions)
	default:
		hostutil.NewCondition(condition).Success("").Set(&dpu.Status.Conditions)
	}
	return dpu.Status, ctrl.Result{}, nil
}

// validateSecureBoot validates that the DPU's current Secure Boot state matches the desired spec.
// Trusted Host mode cannot configure Secure Boot - only validate.
// On mismatch, sets the TrustedHostModeNotSupported condition
// Returns (true, nil) on mismatch, (false, nil) on match, or (false, err) on retryable failure.
func (h *Handler) validateSecureBoot(ctx context.Context, dpu *provisioningv1.DPU) (bool, error) {
	logger := log.FromContext(ctx)

	dev, ok := h.GetDevice(dpu.Spec.SerialNumber)
	if !ok {
		err := fmt.Errorf("device not found for serial number %s", dpu.Spec.SerialNumber)
		hostutil.NewCondition(condition).Failure(err, "DeviceNotFound").Set(&dpu.Status.Conditions)
		return false, err
	}

	rshimInfo, err := h.QueryRshim(dev.Address)
	if err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToQueryRshim").Set(&dpu.Status.Conditions)
		return false, err
	}

	if rshimInfo.SecureBootEnabled == nil {
		err := fmt.Errorf("secure boot status not available from rshim for PCI %s", dev.Address)
		hostutil.NewCondition(condition).Failure(err, "SecureBootStatusNotDetected").Set(&dpu.Status.Conditions)
		return false, err
	}

	desiredEnabled := *dpu.Spec.SecureBoot
	currentEnabled := *rshimInfo.SecureBootEnabled

	if desiredEnabled != currentEnabled {
		err := fmt.Errorf("secure boot configuration mismatch (desired=%v, current=%v): "+
			"configuration is not supported in Trusted Host mode, "+
			"please configure Secure Boot manually via DPU's UEFI menu or use Zero Trust mode", desiredEnabled, currentEnabled)
		logger.Error(err, "Secure Boot configuration not supported in Trusted Host mode",
			"dpu", dpu.Name, "desired", desiredEnabled, "current", currentEnabled)
		hostutil.NewCondition(condition).Failure(err, "TrustedHostModeNotSupported").Set(&dpu.Status.Conditions)
		return true, nil // Mismatch
	}

	dpu.Status.SecureBoot = &provisioningv1.SecureBootStatus{Enabled: &currentEnabled}
	return false, nil
}

func (h *Handler) canSatisfy(ctx context.Context, dpu *provisioningv1.DPU) (dutil.CapacityResult, error) {
	logger := log.FromContext(ctx)
	flavor := &provisioningv1.DPUFlavor{}
	if err := h.Get(ctx, types.NamespacedName{Name: dpu.Spec.DPUFlavor, Namespace: dpu.Namespace}, flavor); err != nil {
		return dutil.CapacityUnknown, err
	}
	dev, ok := h.GetDevice(dpu.Spec.SerialNumber)
	if !ok {
		return dutil.CapacityUnknown, fmt.Errorf("device not found")
	}
	cmd := fmt.Sprintf("flint -d %s query full", filepath.Base(hostutil.NewPCIHelper(dev.Address).PF(0).Path()))
	stdout, stderr, err := hostutil.RunBash(cmd)
	if err != nil {
		return dutil.CapacityUnknown, fmt.Errorf("failed to query DPU, cmd: %s, err: %v, stdout: %s, stderr: %s", cmd, err, stdout.String(), stderr.String())
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		var bfSpecs *dutil.BlueFieldSpecs
		switch kv[0] {
		case "Part Number":
			bfSpecs = dutil.LookUpPartNumber(strings.TrimSpace(kv[1]))
		case "Description":
			bfSpecs = dutil.ParseDescription(strings.TrimSpace(kv[1]))
		default:
			continue
		}
		if bfSpecs == nil {
			continue
		}
		logger.Info("retrieved DPU specs via flint", "bfSpecs", bfSpecs)
		if result := bfSpecs.CanSatisfy(flavor.Spec.DPUResources); result != dutil.CapacityUnknown {
			return result, nil
		}
	}
	logger.Info("WARNING: failed to retrieve DPU specs via flint", "flint output", stdout.String())
	return dutil.CapacityUnknown, nil
}
