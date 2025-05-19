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

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs,verbs=get;list;watch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=isolationclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch

const (
	dpuNodeControllerName = "vpcdpunodecontroller"
)

// DPUNodeReconciler reconciles Node objects in dpu clusters
type DPUNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUNodeReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(dpuNodeControllerName).
		For(&corev1.Node{}).
		Complete(r)
}

// Reconcile reconciles a Node object that belong to a VPC
func (r *DPUNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)

	log.Info("Reconciling")
	node := &corev1.Node{}
	if err := r.Client.Get(ctx, req.NamespacedName, node); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(node, r.Client)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := patcher.Patch(ctx, node,
			patch.WithFieldOwner(serviceInterfaceControllerName),
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	return r.reconcile(ctx, node)
}

//nolint:unparam
func (r *DPUNodeReconciler) reconcile(ctx context.Context, node *corev1.Node) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("reconcile internal")
	// TODO: Add reconcile logic here.
	return ctrl.Result{}, nil
}
