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
	"errors"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	nvconfigutil "github.com/nvidia/doca-platform/internal/provisioning/utils/nvconfig"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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

	// Template-mode create-time render failures: the generated DPUFlavor was never created, so
	// block in Error before the DPUFlavor lookup below reports a misleading "DPUFlavorNotFound".
	// Only the phase is driven here; the DPUFlavorRendered condition is surfaced uniformly for
	// any phase by the DPU controller. An update-time failure keeps the existing generated flavor
	// and is intentionally NOT handled here, so provisioning is not blocked.
	if dpu.Annotations[cutil.RenderFailedReasonAnnotation] == cutil.RenderFailedOnCreate {
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	// Hold the DPU in Pending while a DPF upgrade is in progress, so it does not start provisioning
	// against a half upgraded deployment. DPUs which already started hold the upgrade instead.
	if ctrlCtx.DPFOperatorConfig != nil && ctrlCtx.DPFOperatorConfig.UpgradeInProgress() {
		err := fmt.Errorf("DPF Operator upgrade from version %s is in progress", ptr.Deref(ctrlCtx.DPFOperatorConfig.Status.Version, ""))
		logger.Info("Waiting for the DPF Operator upgrade to complete before provisioning the DPU")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondPending.String(), err, cutil.ReasonDPFOperatorUpgradeInProgress, err.Error()))
		return *state, nil
	}

	if dpu.Status.DPUType == provisioningv1.DPUTypeBlueField4 {

		blueFieldSoftware := &provisioningv1.BlueFieldSoftware{}
		if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: ptr.Deref(dpu.Spec.BlueFieldSoftware, "")}, blueFieldSoftware); err != nil {
			if apierrors.IsNotFound(err) {
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBlueFieldSoftwareReady.String(), err, "BlueFieldSoftwareNotFound", err.Error()))
				return *state, err
			}
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBlueFieldSoftwareReady.String(), err, "GetBlueFieldSoftwareError", err.Error()))
			return *state, err
		}

		if blueFieldSoftware.Status.Phase != provisioningv1.BlueFieldSoftwareReady {
			err := fmt.Errorf("BlueFieldSoftware is not ready")
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBlueFieldSoftwareReady.String(), err, "BlueFieldSoftwareIsNotReady", err.Error()))
			return *state, err
		}

		logger.Info("BlueFieldSoftware is ready")
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondBlueFieldSoftwareReady, "", ""))
	} else {
		// Check for the presence of the specified BFB
		bfb := &provisioningv1.BFB{}
		if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: ptr.Deref(dpu.Spec.BFB, "")}, bfb); err != nil {
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
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondBFBReady, "", ""))
	}

	// Check for the presence of the specified DPUFlavor
	if dpu.Spec.DPUFlavor == "" {
		err := fmt.Errorf("DPUFlavor is not specified")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUFlavorExists.String(), err, "DPUFlavorNotSpecified", err.Error()))
		return *state, err
	}
	dpuFlavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUFlavor}, dpuFlavor); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUFlavorExists.String(), err, "DPUFlavorNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUFlavorExists.String(), err, "GetDPUFlavorError", err.Error()))
		return *state, err
	}
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondDPUFlavorExists, "", ""))

	if nvconfigutil.FlavorRequestsHostOSInitHold(dpuFlavor.Spec.NVConfig) && !ctrlCtx.Options.ZeroTrustProvisioningFlow() {
		err := fmt.Errorf("%s requires zero-trust mode: either deploy in zero-trust mode, or point the DPUSet at a "+
			"DPUFlavor without this parameter",
			nvconfigutil.DelayHostOSInitParam)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondPending.String(), err, "DPUFlavorRequiresZeroTrustMode", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	// Check if we can proceed with provisioning
	if err := ctrlCtx.DPUInProvisioningMap.CanProceed(dutil.DPUID(dpu.UID)); err != nil {
		var cpErr *dutil.CanProceedError
		reason := "CannotProceed"
		if errors.As(err, &cpErr) {
			reason = cpErr.Reason
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondPending.String(), err, reason, err.Error()))
		return *state, nil
	}

	// Clear the pending condition when proceeding successfully
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondPending, "", ""))

	state.Phase = provisioningv1.DPUNodeEffect
	return *state, nil
}
