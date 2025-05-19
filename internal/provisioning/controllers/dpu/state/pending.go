/*
Copyright 2024 NVIDIA

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

package state

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Pending(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	// Check for the presence of the specified BFB
	bfb := &provisioningv1.BFB{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.BFB}, bfb); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBReady.String(), err, "BFBNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBReady.String(), err, "GetBFBError", err.Error()))
		return *state, err
	}

	// Check BFB is ready
	if bfb.Status.Phase != provisioningv1.BFBReady {
		err := fmt.Errorf("BFB is not ready")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBReady.String(), err, "BFBIsNotReady", err.Error()))
		return *state, err
	}
	logger.Info("BFB is ready")
	state.BFBFile = cutil.GenerateBFBFilePath(bfb.Status.FileName)
	state.Phase = provisioningv1.DPUNodeEffect
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondBFBReady, "", ""))

	return *state, nil
}
