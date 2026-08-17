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

	if dpu.Status.DPUType == provisioningv1.DPUTypeBlueField4 {
		if err := ensureBlueFieldSoftwareReady(ctx, dpu, ctrlCtx, state); err != nil {
			return *state, err
		}
	} else if done, err := ensureBFBReady(ctx, dpu, ctrlCtx, state); done || err != nil {
		return *state, err
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

// ensureBlueFieldSoftwareReady verifies BlueFieldSoftware exists and is Ready for BF4 DPUs.
func ensureBlueFieldSoftwareReady(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, state *provisioningv1.DPUStatus) error {
	blueFieldSoftware := &provisioningv1.BlueFieldSoftware{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: ptr.Deref(dpu.Spec.BlueFieldSoftware, "")}, blueFieldSoftware); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBlueFieldSoftwareReady.String(), err, "BlueFieldSoftwareNotFound", err.Error()))
			return err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBlueFieldSoftwareReady.String(), err, "GetBlueFieldSoftwareError", err.Error()))
		return err
	}

	if blueFieldSoftware.Status.Phase != provisioningv1.BlueFieldSoftwareReady {
		err := fmt.Errorf("BlueFieldSoftware is not ready")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBlueFieldSoftwareReady.String(), err, "BlueFieldSoftwareIsNotReady", err.Error()))
		return err
	}

	log.FromContext(ctx).Info("BlueFieldSoftware is ready")
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondBlueFieldSoftwareReady, "", ""))
	return nil
}

// ensureBFBReady verifies the BFB exists, is Ready, and is compatible with the card.
// done is true when Pending should return the updated state immediately without retry
// (currently the incompatible-card Error path).
func ensureBFBReady(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, state *provisioningv1.DPUStatus) (bool, error) {
	bfb := &provisioningv1.BFB{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: ptr.Deref(dpu.Spec.BFB, "")}, bfb); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBReady.String(), err, "BFBNotFound", err.Error()))
			return false, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBReady.String(), err, "GetBFBError", err.Error()))
		return false, err
	}

	if bfb.Status.Phase != provisioningv1.BFBReady {
		err := fmt.Errorf("BFB is not ready")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBReady.String(), err, "BFBIsNotReady", err.Error()))
		return false, err
	}
	if err := incompatibleBFBCardType(ctx, dpu, ctrlCtx, bfb); err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondBFBReady.String(), err, "BFBIncompatibleWithCard", err.Error()))
		state.Phase = provisioningv1.DPUError
		return true, nil
	}

	log.FromContext(ctx).Info("BFB is ready")
	state.BFBFile = cutil.GenerateBFBFilePath(bfb.Status.FileName)
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondBFBReady, "", ""))
	return false, nil
}

// incompatibleBFBCardType returns an error if the BFB is signed for a different card
// type than the DPU's card. Without this check the mismatch only surfaces as an
// installation timeout, long after the BFB has been pushed to the DPU.
//
// Both sides have to be classifiable to report a mismatch. An unrecognized OPN or BFB
// release name is treated as compatible, so a naming scheme we do not know about
// never blocks provisioning. The comparison is directional; see below.
func incompatibleBFBCardType(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, bfb *provisioningv1.BFB) error {
	// The file name has to come from the download URL. Status.FileName defaults to the
	// BFB object name, which carries no signing suffix and would read as unsigned.
	bfbCardType := dutil.CardTypeFromBFBFileName(dutil.BFBFileNameFromURL(bfb.Spec.URL))
	if bfbCardType == dutil.CardTypeUnknown || dpu.Spec.DPUDeviceName == "" {
		return nil
	}

	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		logger := log.FromContext(ctx)
		if apierrors.IsNotFound(err) {
			logger.Info("Skipping BFB card type check, DPUDevice is not discovered yet")
		} else {
			// Not fatal, but the check is silently bypassed, so make it visible.
			logger.Error(err, "Skipping BFB card type check, failed to get DPUDevice")
		}
		return nil
	}

	// Compatibility is directional, not equality: a QP card has no key fused and
	// nothing verifying the image, so any BFB boots on it. Only PK and DK cards
	// enforce a signing key and therefore require a matching BFB.
	opn := ptr.Deref(dpuDevice.Status.OPN, "")
	cardType := dutil.CardTypeFromOPN(opn)
	if cardType == dutil.CardTypeUnknown || cardType == dutil.CardTypeQP || cardType == bfbCardType {
		return nil
	}

	return fmt.Errorf("BFB %s is signed for %s cards, but DPUDevice %s with OPN %s is a %s card",
		bfb.Name, bfbCardType, dpuDevice.Name, opn, cardType)
}
