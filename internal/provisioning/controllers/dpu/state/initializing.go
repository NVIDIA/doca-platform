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
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type dpuInitializingState struct {
	dpu   *provisioningv1.DPU
	alloc allocator.Allocator
}

func (st *dpuInitializingState) Handle(ctx context.Context, c client.Client, _ dutil.DPUOptions) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := st.dpu.Status.DeepCopy()
	if isDeleting(st.dpu) {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	node := &corev1.Node{}
	if err := c.Get(ctx, types.NamespacedName{
		Namespace: "",
		Name:      st.dpu.Spec.NodeName,
	}, node); err != nil {
		return *state, err
	}

	// Check if the DPU OOB bridge is configured. If not configured, set the condition and return.
	if _, ok := node.GetLabels()[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel]; !ok {
		logger.Info("DPU OOB bridge is not configured")
		err := fmt.Errorf("DPU OOB bridge is not configured")
		cond := cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "DPUOOBBridgeNotConfigured", err.Error())
		cutil.SetDPUCondition(state, cond)
		return *state, nil
	}

	if st.dpu.Spec.Cluster.Name == "" {
		rst, err := st.alloc.Allocate(ctx, st.dpu)
		if err != nil {
			logger.Error(err, "failed to allocate DPUCluster")
			cond := cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "DPUClusterNotReady", err.Error())
			cutil.SetDPUCondition(state, cond)
			return *state, nil
		}
		logger.V(2).Info("allocate cluster %s for DPU %s", rst, cutil.GetNamespacedName(st.dpu))
		return *state, nil
	}

	state.Phase = provisioningv1.DPUNodeEffect
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondInitialized, "", ""))
	return *state, nil
}
