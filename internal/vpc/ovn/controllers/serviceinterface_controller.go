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

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs,verbs=get;list;watch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=isolationclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces,verbs=get;list;watch

const (
	serviceInterfaceControllerName = "vpcserviceinterfacecontroller"
	serviceInterfaceFinalizer      = "ovn.vpc.dpu.nvidia.com/serviceinterface-protection"
)

// ServiceInterfaceReconciler reconciles ServiceInterface objects in dpu clusters
type ServiceInterfaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceInterfaceReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(serviceInterfaceControllerName).
		For(&dpuservicev1.ServiceInterface{}).
		Complete(r)
}

// Reconcile reconciles a ServiceInterface object when virtualNetwork is set
func (r *ServiceInterfaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)

	log.Info("Reconciling")
	si := &dpuservicev1.ServiceInterface{}
	if err := r.Client.Get(ctx, req.NamespacedName, si); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(si, r.Client)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := patcher.Patch(ctx, si,
			patch.WithFieldOwner(serviceInterfaceControllerName),
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !si.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, si)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(si, serviceInterfaceFinalizer) {
		controllerutil.AddFinalizer(si, serviceInterfaceFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, si)
}

//nolint:unparam
func (r *ServiceInterfaceReconciler) reconcile(ctx context.Context, si *dpuservicev1.ServiceInterface) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("reconcile internal")
	// TODO: Add reconcile logic here.
	return ctrl.Result{}, nil
}

//nolint:unparam
func (r *ServiceInterfaceReconciler) reconcileDelete(ctx context.Context, si *dpuservicev1.ServiceInterface) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("reconcile delete")
	// TODO: Add reconcile logic here.

	// Remove finalizer if not set.
	if controllerutil.ContainsFinalizer(si, serviceInterfaceFinalizer) {
		controllerutil.RemoveFinalizer(si, serviceInterfaceFinalizer)
	}

	return ctrl.Result{}, nil
}
