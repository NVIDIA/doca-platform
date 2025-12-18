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
	"github.com/nvidia/doca-platform/pkg/utils/bashhelper"

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
	GetDevice func(string) (hostutil.Device, bool)
}

func NewHandler(client client.Client, getDevice func(string) (hostutil.Device, bool)) *Handler {
	return &Handler{
		Client:    client,
		GetDevice: getDevice,
	}
}

func (h *Handler) Handle(ctx context.Context, dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error) {
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
	stdout, stderr, err := bashhelper.Run(cmd)
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
