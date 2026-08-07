/*
Copyright 2026 NVIDIA

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

package bluefieldsoftware

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/state"
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
	BlueFieldSoftwareControllerName = "bluefieldsoftware"
)

// BlueFieldSoftwareReconciler reconciles a BlueFieldSoftware object
type BlueFieldSoftwareReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=bluefieldsoftwares,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=bluefieldsoftwares/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=bluefieldsoftwares/finalizers,verbs=update

func (r *BlueFieldSoftwareReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile")

	bfs := &provisioningv1.BlueFieldSoftware{}
	if err := r.Get(ctx, req.NamespacedName, bfs); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get BlueFieldSoftware %w", err)
	}

	patcher := patch.NewSerialPatcher(bfs, r.Client)
	defer func() {
		logger.Info("Patching")
		if err := patcher.Patch(ctx, bfs,
			patch.WithFieldOwner(BlueFieldSoftwareControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(provisioningv1.BlueFieldSoftwareConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Add cleanup finalizer if not set and BlueFieldSoftware is not currently deleting.
	// Per-DPUSet protection finalizers (bfs-dpuset-*) are owned by the DPUSet controller.
	if !controllerutil.ContainsFinalizer(bfs, provisioningv1.BlueFieldSoftwareFinalizer) && bfs.DeletionTimestamp.IsZero() {
		controllerutil.AddFinalizer(bfs, provisioningv1.BlueFieldSoftwareFinalizer)
		return ctrl.Result{}, nil
	}

	currentState := state.GetBlueFieldSoftwareState(bfs, r.Recorder)
	if err := currentState.Handle(ctx, r.Client); err != nil {
		logger.Error(err, "BlueFieldSoftware state handle error", "phase", bfs.Status.Phase)
		// Return error immediately to trigger exponential backoff
		return ctrl.Result{}, err
	}

	// Status modified in-place by state handlers
	// Always requeue to monitor state, even in Error phase to avoid getting stuck
	logger.Info(fmt.Sprintf("Requeue in %s", cutil.RequeueInterval), "current phase", bfs.Status.Phase)
	return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BlueFieldSoftwareReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.BlueFieldSoftware{}).
		Complete(r)
}
