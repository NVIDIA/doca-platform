/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

//nolint:dupl
package controllers

import (
	"context"
	"fmt"

	"github.com/fluxcd/pkg/runtime/patch"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	dpfperdicates "github.com/nvidia/doca-platform/pkg/utils/predicates"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs,verbs=get;list;watch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=isolationclasses,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=isolationclasses/finalizers,verbs=update

const (
	isolationClassControllerName = "isolationclasscontroller"
	isolationClassFinalizer      = "ovn.vpc.dpu.nvidia.com/isolationclass-protection"

	// OVNProvisionerName is the name of the OVN provisioner
	OVNProvisionerName = "ovn.vpc.dpu.nvidia.com"
)

// IsolationClassReconciler reconciles IsolationClass objects
type IsolationClassReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *IsolationClassReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&vpcv1.DPUVPC{},
		"spec.isolationClassName",
		func(o client.Object) []string {
			vpc := o.(*vpcv1.DPUVPC)
			return []string{vpc.Spec.IsolationClassName}
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(isolationClassControllerName).
		// enqueue only for IsolationClasses with OVN provisioner
		For(&vpcv1.IsolationClass{}, builder.WithPredicates(
			predicate.NewPredicateFuncs(func(o client.Object) bool {
				isoCls := o.(*vpcv1.IsolationClass)
				return isoCls.Spec.Provisioner == OVNProvisionerName
			}))).
		// enqueue for delete events of DPUVPCs if the referenced IsolationClass is being deleted
		Watches(
			&vpcv1.DPUVPC{},
			handler.EnqueueRequestsFromMapFunc(r.dpuVPCToIsolationClassRequests),
			builder.WithPredicates(dpfperdicates.PredicateFuncsByEventTypes(event.DeleteEvent{}))).
		Complete(r)
}

// Reconcile reconciles an IsolationClass object
func (r *IsolationClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)

	log.Info("Reconciling")
	isoCls := &vpcv1.IsolationClass{}
	if err := r.Client.Get(ctx, req.NamespacedName, isoCls); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(isoCls, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := patcher.Patch(ctx, isoCls,
			patch.WithFieldOwner(isolationClassControllerName),
		); kerrors.FilterOut(err, apierrors.IsNotFound) != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !isoCls.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, isoCls)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(isoCls, isolationClassFinalizer) {
		controllerutil.AddFinalizer(isoCls, isolationClassFinalizer)
	}

	return ctrl.Result{}, nil
}

//nolint:unparam
func (r *IsolationClassReconciler) reconcileDelete(ctx context.Context, isoCls *vpcv1.IsolationClass) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile delete")

	vpcsForIsoCls, err := VPCsForIsolationClass(ctx, r.Client, isoCls)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list VPCs for IsolationClass %s. %w", isoCls.Name, err)
	}

	if len(vpcsForIsoCls) > 0 {
		// we have a watch on DPUVPCs delete events to trigger reconcile
		reqLog.Info("There are VPCs referencing IsolationClass", "NumOfVPCs", len(vpcsForIsoCls), "IsolationClass", isoCls.Name)
		return ctrl.Result{}, nil
	}

	// Remove finalizer if not set.
	if controllerutil.ContainsFinalizer(isoCls, isolationClassFinalizer) {
		controllerutil.RemoveFinalizer(isoCls, isolationClassFinalizer)
	}

	return ctrl.Result{}, nil
}

func (r *IsolationClassReconciler) dpuVPCToIsolationClassRequests(ctx context.Context, o client.Object) []reconcile.Request {
	vpc, ok := o.(*vpcv1.DPUVPC)
	if !ok {
		return []reconcile.Request{}
	}
	// only enqueue if:
	//  1. we have an IsolationClass of type OVN
	//  2. its deletion timestamp is set
	isoCls, err := IsolationClassForVPC(ctx, r.Client, vpc)
	if err != nil {
		return []reconcile.Request{}
	}
	if isoCls.ObjectMeta.DeletionTimestamp.IsZero() {
		return []reconcile.Request{}
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name: vpc.Spec.IsolationClassName,
			},
		},
	}
}
