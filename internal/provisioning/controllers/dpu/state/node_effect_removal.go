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

package state

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

func NodeEffectRemoval(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	if !dpu.Spec.NodeEffect.IsNoEffect() {
		dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
		if err != nil {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), err, "FailedGetDPUNodeMaintenance", err.Error()))
			return *state, err
		}

		dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
		key := types.NamespacedName{Namespace: dpu.Namespace, Name: dpunodemaintenanceName}
		if err := ctrlCtx.Client.Get(ctx, key, dpunodemaintenance); err != nil {
			if apierrors.IsNotFound(err) {
				cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectRemoved, "", ""))
				state.Phase = provisioningv1.DPUReady
				return *state, nil
			} else {
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), err, "FailedRemoveRequestor", err.Error()))
				return *state, err
			}
		} else {
			if err := RemoveRequestorFromDPUNodeMaintenance(ctx, dpu, ctrlCtx); err != nil {
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), err, "FailedRemoveRequestor", err.Error()))
				return *state, err
			}
		}
	} else {
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectRemoved, "", ""))
		state.Phase = provisioningv1.DPUReady
		return *state, nil
	}
	return *state, nil
}

func RemoveRequestorFromDPUNodeMaintenance(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) error {
	if !dpu.Spec.NodeEffect.IsNoEffect() {
		dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
		if err != nil {
			return err
		}

		dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
		key := types.NamespacedName{Namespace: dpu.Namespace, Name: dpunodemaintenanceName}
		return retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := ctrlCtx.Client.Get(ctx, key, dpunodemaintenance); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				// If DPUNodeMaintenance object doesn't exist, return nil
				return nil
			}
			// mutate obj.Spec.Requestor
			newRequestors := []string{}
			for _, r := range dpunodemaintenance.Spec.Requestor {
				if r != dpu.Name {
					newRequestors = append(newRequestors, r)
				}
			}
			if len(newRequestors) != len(dpunodemaintenance.Spec.Requestor) {
				dpunodemaintenance.Spec.Requestor = newRequestors
				return ctrlCtx.Client.Update(ctx, dpunodemaintenance)
			}
			return nil
		})
	}

	return nil
}
