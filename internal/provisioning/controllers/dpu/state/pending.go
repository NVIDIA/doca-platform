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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type dpuPendingState struct {
	dpu *provisioningv1.DPU
}

func (st *dpuPendingState) Handle(ctx context.Context, client client.Client, _ dutil.DPUOptions) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := st.dpu.Status.DeepCopy()
	if isDeleting(st.dpu) {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	nn := types.NamespacedName{
		Namespace: st.dpu.Namespace,
		Name:      st.dpu.Spec.BFB,
	}
	bfb := &provisioningv1.BFB{}
	if err := client.Get(ctx, nn, bfb); err != nil {
		logger.Error(err, fmt.Sprintf("get bfb %s failed", nn.String()))
		cond := cutil.DPUCondition(provisioningv1.DPUCondBFBReady, "GetBFBFailed", err.Error())
		cond.Status = metav1.ConditionFalse
		cutil.SetDPUCondition(state, cond)
		return *state, err
	}

	// checking whether bfb is ready
	if bfb.Status.Phase != provisioningv1.BFBReady {
		return *state, nil
	}

	state.Phase = provisioningv1.DPUDMSDeployment
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondBFBReady, "", ""))

	return *state, nil
}
