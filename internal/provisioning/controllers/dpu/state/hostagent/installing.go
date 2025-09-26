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

package hostagent

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/phase/install"
	"github.com/nvidia/doca-platform/internal/release"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Installing(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	if !dpu.DeletionTimestamp.IsZero() {
		cond := meta.FindStatusCondition(dpu.Status.Conditions, string(provisioningv1.DPUCondOSInstalled))
		if cond != nil && (cond.Status == metav1.ConditionTrue || cond.Reason == install.InstallationTerminated) {
			logger.Info("Terminate installation and delete DPU", "status", cond.Status, "reason", cond.Reason)
			state.Phase = provisioningv1.DPUDeleting
		} else {
			logger.V(3).Info("DPU is deleting, but waiting for bfb-install to complete")
		}
		return *state, nil
	}

	_, condition := cutil.GetDPUCondition(&dpu.Status, string(provisioningv1.DPUCondOSInstalled))
	if condition == nil {
		return *state, nil
	} else if condition.Status != metav1.ConditionTrue {
		switch condition.Reason {
		case install.InstallationTerminated:
			logger.Info("Installation terminated, transition to Error", "status", condition.Status, "reason", condition.Reason)
			state.Phase = provisioningv1.DPUError
		default:
			logger.Info("Installation in progress", "status", condition.Status, "reason", condition.Reason)
		}
		return *state, nil
	}

	if dpu.Spec.Cluster.NodeLabels == nil {
		dpu.Spec.Cluster.NodeLabels = make(map[string]string)
	}

	// Add DOCA_BFB version to DPU NodeLabels as it is found in DPU ARM under /etc/mlnx-release. e.g.,
	// cat /etc/mlnx-release
	// bf-bundle-2.7.0-33_24.04_ubuntu-22.04_prod
	patch := client.MergeFrom(dpu.DeepCopy())
	dpu.Spec.Cluster.NodeLabels[cutil.HostNameDPULabelKey] = dpu.Spec.DPUNodeName
	// Set the DPU version in the node labels. This is necessary to be able to schedule Pods
	// on the DPU Node based on the DPF version.
	// The DPF version in the status should never be nil, but we check it here to avoid nil pointer dereference.
	if dpu.Status.DPFVersion != nil {
		dpu.Spec.Cluster.NodeLabels[release.DPFVersionLabelKey] = *dpu.Status.DPFVersion
	}
	if err := ctrlCtx.Client.Patch(ctx, dpu, patch); err != nil {
		return *state, fmt.Errorf("failed to patch DPU: %w", err)
	}

	state.Phase = provisioningv1.DPUCheckingHostRebootNeed
	return *state, nil
}
