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
	"reflect"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Ready(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	node, err := cutil.GetNodeFromDPUCluster(ctx, ctrlCtx.Client, dpu)
	if err != nil {
		updateFalseDPUCondReady(state, "GetNodeFromDPUClusterError", err.Error())
		return *state, err
	}

	if !reflect.DeepEqual(state.Addresses, node.Status.Addresses) {
		state.Addresses = make([]corev1.NodeAddress, len(node.Status.Addresses))
		copy(state.Addresses, node.Status.Addresses)
	}

	if !cutil.IsNodeReady(node) {
		err = fmt.Errorf("DPU's Node %s is not Ready", node.Name)
		updateFalseDPUCondReady(state, "NodeNotReady", err.Error())
		return *state, err
	}

	// Periodic / post-provisioning E/W NIC runtime config can fail after the DPU
	// already reached Ready. Stay in Ready phase but clear DPUCondReady until the
	// agent reports EWNICConfigured=True again.
	if ready, reason, message := ewnicRuntimeConfigReady(dpu); !ready {
		logger.Info("E/W NIC runtime configuration is not ready", "dpu", dpu.Name, "namespace", dpu.Namespace, "reason", reason)
		updateFalseDPUCondReady(state, reason, message)
		return *state, nil
	}

	cond := cutil.DPUCondition(provisioningv1.DPUCondReady, "DPUReady", "")
	cutil.SetDPUCondition(state, cond)

	ann := dpu.Spec.Cluster.NodeAnnotations
	if ann == nil {
		ann = map[string]string{}
	}
	needUpdateLabels, err := cutil.NeedUpdateLabelsOnNodeInDPUCluster(node, dpu.Spec.Cluster.NodeLabels)
	if err != nil {
		return *state, err
	}
	needUpdateAnn, err := cutil.NeedUpdateAnnotationsOnNodeInDPUCluster(node, ann)
	if err != nil {
		return *state, err
	}
	if needUpdateLabels || needUpdateAnn {
		// Check if applyOnLabelChange is enabled
		if dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange != nil && *dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange {
			// Set status field to trigger node effect
			state.PostProvisioningNodeEffect = ptr.To(true)
			// Transition to nodeEffect state instead of DPUClusterConfig
			state.Phase = provisioningv1.DPUNodeEffect
			updateFalseDPUCondReady(state, "RerunNodeEffect", "rerun node effect")
			logger.V(3).Info(fmt.Sprintf("node %s needs to update cluster node metadata, triggering node effect", node.Name))
			return *state, nil
		}
		state.Phase = provisioningv1.DPUClusterConfig
		updateFalseDPUCondReady(state, "RerunClusterConfig", "rerun cluster config")
		logger.V(3).Info(fmt.Sprintf("node %s needs to update cluster node labels or annotations", node.Name))
		return *state, nil
	}
	return *state, nil
}

func updateFalseDPUCondReady(status *provisioningv1.DPUStatus, reason string, message string) {
	cond := cutil.DPUCondition(provisioningv1.DPUCondReady, reason, message)
	cond.Status = metav1.ConditionFalse
	cutil.SetDPUCondition(status, cond)
}
