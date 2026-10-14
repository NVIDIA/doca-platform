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

package mock

import (
	"context"
	"fmt"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	BFCFGDir = "bfcfg"
)

func InitializeInterface(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	state.Phase = provisioningv1.DPUConfigFWParameters
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

	return *state, nil
}

func ConfigFWParameters(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	state.Phase = provisioningv1.DPUPrepareBFB
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondFWConfigured, "", ""))

	return *state, nil
}

func HostNetworkConfiguration(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	state.Phase = provisioningv1.DPUClusterConfig
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondHostNetworkReady, "", ""))

	return *state, nil
}

func PrepareBFB(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	bfCFGPath := filepath.Join(BFCFGDir, fmt.Sprintf("%s_%s_%s", dpu.Namespace, dpu.Name, dpu.UID))

	state.BFCFGFile = bfCFGPath
	state.Phase = provisioningv1.DPUOSInstalling
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondBFBPrepared, "", ""))

	return *state, nil
}

func Installing(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	node := &corev1.Node{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: "", Name: dpu.Spec.DPUNodeName}, node); err != nil {
		err = fmt.Errorf("DPUNode %s not found", dpu.Spec.DPUNodeName)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondOSInstalled.String(), err, "DPUNodeNotFound", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	annotations := node.GetAnnotations()
	if value, ok := annotations[reboot.RebootCmdKey]; !ok || value != reboot.Skip {
		err := fmt.Errorf("DPUNode should have '%s = %s'", reboot.RebootCmdKey, reboot.Skip)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondOSInstalled.String(), err, "DPUNodeInvalid", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}
	ctrlCtx.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))
	state.Phase = provisioningv1.DPUCheckingHostRebootNeed
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondOSInstalled, "", ""))

	return *state, nil
}

func RebootRequiredCheck(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	state.Phase = provisioningv1.DPURebooting
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondCheckedHostRebootNeed, "", ""))
	return *state, nil
}

func ClusterConfig(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	state.Phase = provisioningv1.DPUNodeEffectRemoval
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectRemoved, "", ""))

	return *state, nil
}

func NodeEffectRemoval(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}
	state.Phase = provisioningv1.DPUReady
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondReady, "", ""))
	return *state, nil
}

func Deleting(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	ctrlCtx.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))

	controllerutil.RemoveFinalizer(dpu, provisioningv1.DPUFinalizer)
	if err := ctrlCtx.Update(ctx, dpu); err != nil {
		err = fmt.Errorf("failed to update DPU: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDeleting.String(), err, "UpdateError", err.Error()))
		return *state, err
	}

	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondDeleting, "", ""))

	return *state, nil
}
