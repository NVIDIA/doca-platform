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

package hostcontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/indexers"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	utilsPredicates "github.com/nvidia/doca-platform/pkg/utils/predicates"

	"github.com/fluxcd/pkg/runtime/patch"
	corestoragev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// DPUStorageVendorReconciler reconciles a DPUStorageVendor object
type DPUStorageVendorReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	controller  controller.Controller
	RemoteCache *dpucluster.RemoteCache
	Options     Options

	dpuClusterHelper dpuclusterhelper.DPUClusterHelper
}

const (
	dpuStorageVendorControllerName = "dpustoragevendorcontroller"
)

// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragevendors,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragevendors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragevendors/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch

// Reconcile manages DPUStorageVendor lifecycle by validating storage components in DPU clusters.

//nolint:dupl
func (r *DPUStorageVendorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling")

	dpuStorageVendor := &storagev1.DPUStorageVendor{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuStorageVendor); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("not found, skipping")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuStorageVendor, r.Client)
	conditions.EnsureConditions(dpuStorageVendor, storagev1.DPUStorageVendorConditions)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		r.finalizeConditions(dpuStorageVendor, reterr)
		reqLog.Info("Patching")
		if err := patcher.Patch(ctx, dpuStorageVendor,
			patch.WithFieldOwner(dpuStorageVendorControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(storagev1.DPUStorageVendorConditions)},
		); kerrors.FilterOut(err, apierrors.IsNotFound) != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !dpuStorageVendor.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuStorageVendor)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuStorageVendor, storagev1.DPUStorageVendorFinalizer) {
		reqLog.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuStorageVendor, storagev1.DPUStorageVendorFinalizer)
		return ctrl.Result{}, nil
	}

	targetDPUClusterList, err := r.dpuClusterHelper.GetTargetDPUClusters(ctx, nil)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(targetDPUClusterList) == 0 {
		reqLog.Info("No target DPUClusters found, waiting for ready DPUClusters")
		conditions.AddFalse(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled,
			conditions.ReasonPending, "No ready DPUCluster found")
		return ctrl.Result{}, nil
	}
	dpuClusterClients, err := r.dpuClusterHelper.GetDPUClusterClients(ctx, targetDPUClusterList)
	if err != nil {
		if errors.Is(err, dpuclusterhelper.ErrDPUClusterClientNotAvailable) {
			reqLog.Info("client for DPUCluster is not available, requeue")
			return ctrl.Result{RequeueAfter: requeueIntervalOnDpuClusterNotConnected}, nil
		}
		reqLog.Error(err, "Failed to get DPUCluster clients")
		return ctrl.Result{}, err
	}

	return r.reconcile(ctx, dpuStorageVendor, dpuClusterClients)
}

func (r *DPUStorageVendorReconciler) finalizeConditions(dpuStorageVendor *storagev1.DPUStorageVendor, err error) {
	// in case of any error set ConditionDPUStorageVendorReconciled to false with error reason
	if err != nil {
		conditions.AddFalse(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled,
			conditions.ReasonError, conditions.ConditionMessage(err.Error()))
	}
	conditions.SetSummary(dpuStorageVendor)
}

// reconcile validates storage vendor components across DPU clusters

//nolint:unparam
func (r *DPUStorageVendorReconciler) reconcile(ctx context.Context, dpuStorageVendor *storagev1.DPUStorageVendor, dpuClusterClients []dpuclusterhelper.ClientForDPUCluster) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	clustersWithValidVendor := []storagev1.ObjectReference{}
	conditionSummary := make([]string, 0, len(dpuClusterClients))
	for _, dpuClusterClient := range dpuClusterClients {
		vendorCheckResult, err := r.checkVendorInDPUCluster(ctx, dpuClusterClient.Client, dpuStorageVendor)
		if err != nil {
			return ctrl.Result{}, err
		}
		dpuClusterNamespaceName := client.ObjectKeyFromObject(dpuClusterClient.DPUCluster)
		if vendorCheckResult.Valid {
			reqLog.Info("Vendor is valid in the DPU cluster", "dpuCluster", dpuClusterNamespaceName.String())
			clustersWithValidVendor = append(clustersWithValidVendor, storagev1.ObjectReference{
				Namespace: dpuClusterClient.DPUCluster.Namespace,
				Name:      dpuClusterClient.DPUCluster.Name,
			})
		} else {
			reqLog.Info("Vendor is invalid in the DPU cluster", "dpuCluster", dpuClusterNamespaceName.String(), "reason", vendorCheckResult.Reason)
			conditionSummary = append(conditionSummary, fmt.Sprintf("* DPUCluster %s: %s", dpuClusterNamespaceName.String(), vendorCheckResult.Reason))
		}
	}
	if len(clustersWithValidVendor) > 0 {
		dpuStorageVendor.Status.DPUClusters = clustersWithValidVendor
		conditions.AddTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorValid)
	} else {
		dpuStorageVendor.Status.DPUClusters = nil
		conditions.AddFalse(dpuStorageVendor, storagev1.ConditionDPUStorageVendorValid,
			conditions.ReasonError, conditions.ConditionMessage(fmt.Sprintf("DPUStorageVendor is not valid:\n%s", strings.Join(conditionSummary, "\n"))))
	}
	conditions.AddTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled)
	return ctrl.Result{}, nil
}

// vendorCheckResult is a struct that contains the result of a vendor check.
type vendorCheckResult struct {
	// Valid is true if the vendor is present in the DPU cluster.
	Valid bool
	// Reason is a non-empty string if the vendor is not present in the DPU cluster.
	Reason string
}

// checkVendorInDPUCluster validates that StorageClass and CSIDriver exist in the DPU cluster
func (r *DPUStorageVendorReconciler) checkVendorInDPUCluster(ctx context.Context, dpuClusterClient client.Client,
	dpuStorageVendor *storagev1.DPUStorageVendor) (vendorCheckResult, error) {
	reqLog := ctrllog.FromContext(ctx)
	storageClass := &corestoragev1.StorageClass{}
	err := dpuClusterClient.Get(ctx, client.ObjectKey{Name: dpuStorageVendor.Spec.StorageClassName}, storageClass)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return vendorCheckResult{Valid: false, Reason: fmt.Sprintf("StorageClass %s not found", dpuStorageVendor.Spec.StorageClassName)}, nil
		}
		reqLog.Error(err, "failed to get StorageClass")
		return vendorCheckResult{Valid: false, Reason: ""}, err
	}
	csiDriver := &corestoragev1.CSIDriver{}
	err = dpuClusterClient.Get(ctx, client.ObjectKey{Name: storageClass.Provisioner}, csiDriver)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return vendorCheckResult{Valid: false, Reason: fmt.Sprintf("CSIDriver %s not found", storageClass.Provisioner)}, nil
		}
		reqLog.Error(err, "failed to get CSIDriver")
		return vendorCheckResult{Valid: false, Reason: ""}, err
	}
	return vendorCheckResult{Valid: true, Reason: ""}, nil
}

// reconcileDelete handles deletion of the DPUStorageVendor

//nolint:unparam,dupl
func (r *DPUStorageVendorReconciler) reconcileDelete(ctx context.Context, dpuStorageVendor *storagev1.DPUStorageVendor) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling delete")
	controllerutil.RemoveFinalizer(dpuStorageVendor, storagev1.DPUStorageVendorFinalizer)
	return ctrl.Result{}, nil
}

// enqueueByStorageClassInDPUCluster enqueues DPUStorageVendor objects when StorageClass changes in DPU cluster
//
//nolint:dupl
func (r *DPUStorageVendorReconciler) enqueueByStorageClassInDPUCluster(ctx context.Context, o client.Object) []reconcile.Request {
	storageClass, ok := o.(*corestoragev1.StorageClass)
	if !ok {
		return nil
	}
	dpuStorageVendorList := &storagev1.DPUStorageVendorList{}
	if err := r.Client.List(ctx, dpuStorageVendorList, client.InNamespace(r.Options.Namespace), client.MatchingFields{indexers.DPUStorageVendorSpecStorageClassName: storageClass.Name}); err != nil {
		return nil
	}
	result := make([]reconcile.Request, 0, len(dpuStorageVendorList.Items))
	for _, m := range dpuStorageVendorList.Items {
		result = append(result, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&m)})
	}
	return result
}

// enqueueAll enqueues all DPUStorageVendor objects for reconciliation
//
//nolint:dupl
func (r *DPUStorageVendorReconciler) enqueueAll(ctx context.Context, _ client.Object) []reconcile.Request {
	dpuStorageVendorList := &storagev1.DPUStorageVendorList{}
	if err := r.Client.List(ctx, dpuStorageVendorList, client.InNamespace(r.Options.Namespace)); err != nil {
		return nil
	}
	result := make([]reconcile.Request, 0, len(dpuStorageVendorList.Items))
	for _, m := range dpuStorageVendorList.Items {
		result = append(result, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&m)})
	}
	return result
}

// WatchDPUClusterStorageClass registers a watch for StorageClass in the DPU cluster
func (r *DPUStorageVendorReconciler) WatchDPUClusterStorageClass(_ context.Context, _ client.Client, _ client.ObjectKey) (dpucluster.Watcher, error) {
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpustoragevendor-watch-storageclass",
		Watcher:      r.controller,
		Kind:         &corestoragev1.StorageClass{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.enqueueByStorageClassInDPUCluster),
	}), nil
}

// WatchDPUClusterCSIDriver registers a watch for CSIDriver objects in the DPU cluster
func (r *DPUStorageVendorReconciler) WatchDPUClusterCSIDriver(_ context.Context, _ client.Client, _ client.ObjectKey) (dpucluster.Watcher, error) {
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpustoragevendor-watch-csidriver",
		Watcher:      r.controller,
		Kind:         &corestoragev1.CSIDriver{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.enqueueAll),
		Predicates:   []predicate.TypedPredicate[client.Object]{utilsPredicates.PredicateFuncsByEventTypes(event.CreateEvent{}, event.DeleteEvent{})},
	}), nil
}

// SetupWithManager sets up the controller with the Manager.

//nolint:dupl
func (r *DPUStorageVendorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	r.dpuClusterHelper = dpuclusterhelper.New(mgr.GetClient(), r.RemoteCache)
	r.controller, err = ctrl.NewControllerManagedBy(mgr).
		For(&storagev1.DPUStorageVendor{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAll)).
		Build(r)
	return err
}
