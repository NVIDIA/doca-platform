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

// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks/finalizers,verbs=update
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=isolationclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuserviceinterfaces,verbs=get;list;watch

const (
	dpuVirtualNetworkControllerName = "dpuvirtualnetworkcontroller"
	dpuVirtualNetworkFinalizer      = "ovn.vpc.dpu.nvidia.com/virtualnetwork-protection"
)

// DPUVirtualNetworkReconciler reconciles a DPUVirtualNetwork objects
type DPUVirtualNetworkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUVirtualNetworkReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(dpuVirtualNetworkControllerName).
		For(&vpcv1.DPUVirtualNetwork{}).
		Complete(r)
}

// Reconcile reconciles a DPUVirtualNetwork object
func (r *DPUVirtualNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)

	log.Info("Reconciling")
	dpuVnet := &vpcv1.DPUVirtualNetwork{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuVnet); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuVnet, r.Client)

	conditions.EnsureConditions(dpuVnet, vpcv1.DPUVirtualNetworkConditions)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := patcher.Patch(ctx, dpuVnet,
			patch.WithFieldOwner(dpuVirtualNetworkControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(vpcv1.DPUVirtualNetworkConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !dpuVnet.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuVnet)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuVnet, dpuVirtualNetworkFinalizer) {
		controllerutil.AddFinalizer(dpuVnet, dpuVirtualNetworkFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuVnet)
}

//nolint:unparam
func (r *DPUVirtualNetworkReconciler) reconcile(ctx context.Context, dpuVnet *vpcv1.DPUVirtualNetwork) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("reconcile internal")
	// TODO: Add reconcile logic here.
	conditions.AddTrue(dpuVnet, conditions.TypeReady)
	return ctrl.Result{}, nil
}

//nolint:unparam
func (r *DPUVirtualNetworkReconciler) reconcileDelete(ctx context.Context, dpuVnet *vpcv1.DPUVirtualNetwork) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("reconcile delete")
	// TODO: Add reconcile logic here.

	// Remove finalizer if not set.
	if controllerutil.ContainsFinalizer(dpuVnet, dpuVirtualNetworkFinalizer) {
		controllerutil.RemoveFinalizer(dpuVnet, dpuVirtualNetworkFinalizer)
	}

	return ctrl.Result{}, nil
}
