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

package bfb

import (
	"context"
	"fmt"
	"reflect"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb/state"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// controller name that will be used when reporting events
	BFBControllerName = "bfb"
)

// BFBReconciler reconciles a BFB object
type BFBReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=bfbs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=bfbs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=bfbs/finalizers,verbs=update

func (r *BFBReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile")

	bfb := &provisioningv1.BFB{}
	if err := r.Get(ctx, req.NamespacedName, bfb); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get BFB %w", err)
	}

	patcher := patch.NewSerialPatcher(bfb, r.Client)
	defer func() {
		logger.Info("Patching")
		if err := patcher.Patch(ctx, bfb,
			patch.WithFieldOwner(BFBControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(provisioningv1.BFBConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Add finalizer if not set and BFB is not currently deleting.
	if !controllerutil.ContainsFinalizer(bfb, provisioningv1.BFBFinalizer) && bfb.DeletionTimestamp.IsZero() {
		controllerutil.AddFinalizer(bfb, provisioningv1.BFBFinalizer)
		return ctrl.Result{}, nil
	}

	currentState := state.GetBFBState(bfb, r.Recorder)
	nextState, err := currentState.Handle(ctx, r.Client)
	if err != nil {
		logger.Error(err, "BFB state handle error", "phase", bfb.Status.Phase)
	}

	if !reflect.DeepEqual(bfb.Status, nextState) {
		logger.Info("Update BFB status", "current phase", bfb.Status.Phase, "next phase", nextState.Phase)
		bfb.Status = nextState
		// Patcher will handle the status update in deferred call
		return ctrl.Result{}, nil
	} else if nextState.Phase != provisioningv1.BFBError {
		// requeue if bfb is not in error state
		// TODO: move the state checking in state machine
		logger.Info(fmt.Sprintf("Requeue in %s", cutil.RequeueInterval), "current phase", bfb.Status.Phase)
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BFBReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.BFB{}).
		Complete(r)
}
