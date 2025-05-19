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

//nolint:dupl
package controllers

import (
	"context"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs/finalizers,verbs=update
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=isolationclasses,verbs=get;list;watch

const (
	dpuVPCControllerName = "dpuvpccontroller"
	dpuVPCFinalizer      = "ovn.vpc.dpu.nvidia.com/vpc-protection"
)

// DPUVPCReconciler reconciles a DPUVPC objects
type DPUVPCReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUVPCReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(dpuVPCControllerName).
		For(&vpcv1.DPUVPC{}).
		Complete(r)
}

// Reconcile reconciles a DPUVPC object
func (r *DPUVPCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)

	log.Info("Reconciling")
	dpuVpc := &vpcv1.DPUVPC{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuVpc); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuVpc, r.Client)

	conditions.EnsureConditions(dpuVpc, vpcv1.DPUVPCConditions)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := patcher.Patch(ctx, dpuVpc,
			patch.WithFieldOwner(dpuVPCControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(vpcv1.DPUVPCConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !dpuVpc.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuVpc)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuVpc, dpuVPCFinalizer) {
		controllerutil.AddFinalizer(dpuVpc, dpuVPCFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuVpc)
}

//nolint:unparam
func (r *DPUVPCReconciler) reconcile(ctx context.Context, dpuVpc *vpcv1.DPUVPC) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("reconcile internal")
	// TODO: Add reconcile logic here.
	conditions.AddTrue(dpuVpc, conditions.TypeReady)
	return ctrl.Result{}, nil
}

//nolint:unparam
func (r *DPUVPCReconciler) reconcileDelete(ctx context.Context, dpuVpc *vpcv1.DPUVPC) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("reconcile delete")
	// TODO: Add reconcile logic here.

	// Remove finalizer if not set.
	if controllerutil.ContainsFinalizer(dpuVpc, dpuVPCFinalizer) {
		controllerutil.RemoveFinalizer(dpuVpc, dpuVPCFinalizer)
	}

	return ctrl.Result{}, nil
}
