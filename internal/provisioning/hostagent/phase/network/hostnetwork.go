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

package network

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	condition = string(provisioningv1.DPUCondHostNetworkReady)
)

type Handler struct {
	AddNetworkRequest func(dpu *provisioningv1.DPU) error
}

func NewHandler(addNetworkRequest func(dpu *provisioningv1.DPU) error) *Handler {
	return &Handler{
		AddNetworkRequest: addNetworkRequest,
	}
}

func (h *Handler) Handle(ctx context.Context, dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error) {
	if err := h.AddNetworkRequest(dpu); err != nil {
		hostutil.NewCondition(condition).Failure(err, "FailedToSetupHostNetwork").Set(&dpu.Status.Conditions)
		return dpu.Status, ctrl.Result{}, err
	}
	return dpu.Status, ctrl.Result{}, nil
}
