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

package controller

import (
	"context"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/ecpf"
	"github.com/nvidia/doca-platform/pkg/ovsutils"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	NodeServiceInterfacesController = "nodeserviceinterfacescontroller"
	nodeServiceInterfacesTypeSFC    = "sfc"
)

// NodeServiceInterfacesReconciler reconciles SFC-owned NodeServiceInterfaces objects.
type NodeServiceInterfacesReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	NodeName    string
	OVS         ovsutils.API
	ECPFManager ecpf.ECPFManager
}

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=nodeserviceinterfaces,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=nodeserviceinterfaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=nodeserviceinterfaces/finalizers,verbs=update

func (r *NodeServiceInterfacesReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	nsi := &dpuservicev1.NodeServiceInterfaces{}
	if err := r.Client.Get(ctx, req.NamespacedName, nsi); err != nil {
		if apierrors.IsNotFound(err) {
			return requeueDone()
		}
		return requeueError()
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeServiceInterfacesReconciler) SetupWithManager(mgr ctrl.Manager) error {
	nsiPredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
		nsi, ok := o.(*dpuservicev1.NodeServiceInterfaces)
		if !ok {
			return false
		}

		return nsi.Spec.Node == r.NodeName && nsi.Spec.Type == nodeServiceInterfacesTypeSFC
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&dpuservicev1.NodeServiceInterfaces{},
			builder.WithPredicates(nsiPredicate),
		).
		Complete(r)
}
