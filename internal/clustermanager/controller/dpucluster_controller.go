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

package controller

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	watches []*watchItem
)

type watchItem struct {
	object  client.Object
	handler handler.EventHandler
	opts    []builder.WatchesOption
}

func RegisterWatch(object client.Object, eventHandler handler.EventHandler, opts ...builder.WatchesOption) {
	watches = append(watches, &watchItem{object, eventHandler, opts})
}

type ClusterHandler interface {
	ReconcileCluster(context.Context, *provisioningv1.DPUCluster) (string, []metav1.Condition, error)
	CleanUpCluster(context.Context, *provisioningv1.DPUCluster) (bool, error)
	Type() string
}

// DPUClusterReconciler reconciles a DPUCluster object
type DPUClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	ClusterHandler

	rvCacheLock sync.RWMutex
	rvCache     map[types.NamespacedName]int64
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DPUCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *DPUClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	dc := &provisioningv1.DPUCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, dc); err != nil {
		if apierrors.IsNotFound(err) {
			r.deleteCachedRV(ctx, req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	// avoid reconciling old objects
	curRV, err := strconv.ParseInt(dc.GetResourceVersion(), 10, 64)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to parse resource version, rv: %s, err: %v", dc.GetResourceVersion(), err)
	}
	cachedRV := r.getCachedRV(dc)
	if curRV < cachedRV {
		logger.Info(fmt.Sprintf("skip reconciling, curRV: %d, latestRV: %d", curRV, cachedRV))
		return ctrl.Result{}, nil
	}
	defer r.updateCachedRV(ctx, dc)

	if dc.Spec.Type != r.ClusterHandler.Type() {
		logger.Info(fmt.Sprintf("skip non-match type %s, expect %s", dc.Spec.Type, r.ClusterHandler.Type()))
		return ctrl.Result{}, nil
	}

	if !dc.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(dc, provisioningv1.FinalizerCleanUp) {
			return ctrl.Result{}, nil
		}
		done, err := r.ClusterHandler.CleanUpCluster(ctx, dc)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to clean up, err: %v", err)
		} else if !done {
			return ctrl.Result{}, nil
		}
		controllerutil.RemoveFinalizer(dc, provisioningv1.FinalizerCleanUp)
		if err := r.Client.Update(ctx, dc); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove finalizer, err: %v", err)
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(dc, provisioningv1.FinalizerCleanUp) {
		logger.V(3).Info(fmt.Sprintf("add finalizer %s", provisioningv1.FinalizerCleanUp))
		controllerutil.AddFinalizer(dc, provisioningv1.FinalizerCleanUp)
		if err := r.Client.Update(ctx, dc); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer, err: %v", err)
		}
	}

	switch dc.Status.Phase {
	case provisioningv1.PhaseCreating, provisioningv1.PhaseReady, provisioningv1.PhaseNotReady:
		var errList []error
		kubeconfig, conds, recErr := r.ClusterHandler.ReconcileCluster(ctx, dc)
		if recErr != nil {
			// For a better debugability, we will update the conditions even if the ReconcileCluster() returns an error
			logger.Error(fmt.Errorf("failed to reconcile cluster, err: %v", recErr), "")
			errList = append(errList, recErr)
		}
		// update kubeconfig first, because the Created condition indicates that the cluster has been created
		if kubeconfig != "" && dc.Spec.Kubeconfig == "" {
			logger.V(3).Info(fmt.Sprintf("set kubeconfig as %s", kubeconfig))
			dc.Spec.Kubeconfig = kubeconfig
			if err := r.Client.Update(ctx, dc); err != nil {
				errList = append(errList, fmt.Errorf("failed to set kubeconfig, err: %v", err))
				return ctrl.Result{}, kerrors.NewAggregate(errList)
			}
		}

		changed := false
		for _, c := range conds {
			if meta.SetStatusCondition(&dc.Status.Conditions, c) {
				changed = true
			}
		}
		if changed {
			logger.V(3).Info("update status condition(s)")
			if err := r.Client.Status().Update(ctx, dc); err != nil {
				errList = append(errList, fmt.Errorf("failed to update status conditions, err: %v", err))
				return ctrl.Result{}, kerrors.NewAggregate(errList)
			}
		}
		return ctrl.Result{}, kerrors.NewAggregate(errList)
	case "", provisioningv1.PhasePending, provisioningv1.PhaseFailed:
		logger.Info(fmt.Sprintf("skip %s cluster", dc.Status.Phase))
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, fmt.Errorf("unsupported phase %q", dc.Status.Phase)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.rvCacheLock.Lock()
	defer r.rvCacheLock.Unlock()
	if r.rvCache == nil {
		r.rvCache = make(map[types.NamespacedName]int64)
	}
	b := ctrl.NewControllerManagedBy(mgr).For(&provisioningv1.DPUCluster{})
	for _, w := range watches {
		b.Watches(w.object, w.handler, w.opts...)
	}
	return b.Complete(r)
}

func (r *DPUClusterReconciler) getCachedRV(dc *provisioningv1.DPUCluster) int64 {
	r.rvCacheLock.RLock()
	defer r.rvCacheLock.RUnlock()
	return r.rvCache[cutil.GetNamespacedName(dc)]
}

func (r *DPUClusterReconciler) updateCachedRV(ctx context.Context, dc *provisioningv1.DPUCluster) {
	r.rvCacheLock.Lock()
	defer r.rvCacheLock.Unlock()
	curRV, _ := strconv.ParseInt(dc.GetResourceVersion(), 10, 64)
	if curRV == 0 {
		return
	}
	cached, ok := r.rvCache[cutil.GetNamespacedName(dc)]
	if !ok || (cached < curRV) {
		r.rvCache[cutil.GetNamespacedName(dc)] = curRV
		log.FromContext(ctx).V(3).Info(fmt.Sprintf("update cached RV to %d", curRV))
	}
}

func (r *DPUClusterReconciler) deleteCachedRV(ctx context.Context, nn types.NamespacedName) {
	r.rvCacheLock.Lock()
	defer r.rvCacheLock.Unlock()
	if cached, ok := r.rvCache[nn]; ok {
		delete(r.rvCache, nn)
		log.FromContext(ctx).V(3).Info(fmt.Sprintf("delete cached RV %d", cached))
	}
}
