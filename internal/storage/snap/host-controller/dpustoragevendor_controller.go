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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	"github.com/fluxcd/pkg/runtime/patch"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// DPUStorageVendorReconciler reconciles a DPUStorageVendor object
type DPUStorageVendorReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	controller  controller.Controller
	RemoteCache *dpucluster.RemoteCache
	Options     Options
}

const (
	dpuStorageVendorControllerName = "dpustoragevendorcontroller"
)

// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragevendors,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragevendors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragevendors/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch

// Reconcile reconciles changes in a DPUStorageVendor.

//nolint:dupl
func (r *DPUStorageVendorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling")

	dpuCluster := &provisioningv1.DPUCluster{}
	targetDPUCluster := r.Options.DPUCluster
	err := r.Get(ctx, targetDPUCluster, dpuCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPU cluster %s: %w", targetDPUCluster.String(), err)
	}
	if err := r.watchStorageVendorsInDPUCluster(ctx, targetDPUCluster); err != nil {
		if errors.Is(err, dpucluster.ErrDPUClusterNotConnected) {
			reqLog.Info("cluster is not connected, requeue", "cluster", targetDPUCluster.String())
			return ctrl.Result{Requeue: true, RequeueAfter: requeueIntervalOnDpuClusterNotConnected}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to start watch for DPU cluster %s: %w", targetDPUCluster.String(), err)
	}

	dpuClusterClient, err := r.RemoteCache.GetClient(targetDPUCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get client for DPU cluster %s: %w", targetDPUCluster.String(), err)
	}

	dpuStorageVendor := &storagev1.DPUStorageVendor{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuStorageVendor); err != nil {
		if apierrors.IsNotFound(err) {
			// DPUStorageVendor is not found, but we need to ensure that the corresponding
			// StorageVendor in the DPU cluster is also removed to avoid orphaned resources.
			return cleanupOrphanedObject(ctx, dpuClusterClient,
				r.getStorageVendorNameFromDPUStorageVendorName(req.NamespacedName),
				&storagev1.StorageVendor{},
				"StorageVendor")
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
		return r.reconcileDelete(ctx, dpuClusterClient, dpuStorageVendor)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuStorageVendor, storagev1.DPUStorageVendorFinalizer) {
		reqLog.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuStorageVendor, storagev1.DPUStorageVendorFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuClusterClient, dpuStorageVendor)
}

func (r *DPUStorageVendorReconciler) finalizeConditions(dpuStorageVendor *storagev1.DPUStorageVendor, err error) {
	// in case of any error set ConditionDPUStorageVendorReconciled to false with error reason
	if err != nil {
		conditions.AddFalse(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled,
			conditions.ReasonError, conditions.ConditionMessage(err.Error()))
	}
	conditions.SetSummary(dpuStorageVendor)
}

// reconcile handles the main reconciliation loop

//nolint:unparam
func (r *DPUStorageVendorReconciler) reconcile(ctx context.Context, dpuClusterClient client.Client,
	dpuStorageVendor *storagev1.DPUStorageVendor) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)

	// Create/update the StorageVendor in the DPU cluster
	storageVendorNamespacedName := r.getStorageVendorNameFromDPUStorageVendorName(client.ObjectKeyFromObject(dpuStorageVendor))
	desiredStorageVendor := &storagev1.StorageVendor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      storageVendorNamespacedName.Name,
			Namespace: storageVendorNamespacedName.Namespace,
		},
		Spec: storagev1.StorageVendorSpec{
			StorageClassName: dpuStorageVendor.Spec.StorageClassName,
			PluginName:       dpuStorageVendor.Spec.PluginName,
		},
	}

	apiStorageVendor := &storagev1.StorageVendor{}
	err := dpuClusterClient.Get(ctx, storageVendorNamespacedName, apiStorageVendor)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to read StorageVendor from DPU cluster: %w", err)
	}

	if apierrors.IsNotFound(err) {
		reqLog.Info("StorageVendor not found in the DPU cluster, creating")
		apiStorageVendor = desiredStorageVendor
		if err := dpuClusterClient.Create(ctx, apiStorageVendor); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create StorageVendor in DPU cluster: %w", err)
		}
	} else {
		reqLog.Info("StorageVendor found in the DPU cluster")
		// Check if StorageVendor is being deleted in the DPU cluster.
		// This is an edge case that may occur due to manual intervention (e.g. manual deletion of the StorageVendor in the DPU cluster).
		// In this case we wait for deletion to complete and then recreate the StorageVendor.
		if !apiStorageVendor.ObjectMeta.DeletionTimestamp.IsZero() {
			reqLog.Info("StorageVendor in the DPU cluster is deleting")
			r.setAwaitingDeletion(dpuStorageVendor)
			return ctrl.Result{}, nil
		}
		// Check if it needs to be updated
		if !r.validateExistingStorageVendorSpec(desiredStorageVendor, apiStorageVendor) {
			reqLog.Info("StorageVendor in the DPU cluster has different parameters, updating")
			apiStorageVendor.Spec = desiredStorageVendor.Spec
			if err := dpuClusterClient.Update(ctx, apiStorageVendor); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update StorageVendor in DPU cluster: %w", err)
			}
		}
	}
	// always set ConditionDPUStorageVendorValid to true to be compatible with latest storage API
	conditions.AddTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorValid)
	conditions.AddTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled)
	return ctrl.Result{}, nil
}

// reconcileDelete handles deletion of the DPUStorageVendor

//nolint:unparam,dupl
func (r *DPUStorageVendorReconciler) reconcileDelete(ctx context.Context,
	dpuClusterClient client.Client, dpuStorageVendor *storagev1.DPUStorageVendor) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling delete")

	storageVendorNamespacedName := r.getStorageVendorNameFromDPUStorageVendorName(client.ObjectKeyFromObject(dpuStorageVendor))
	apiStorageVendor := &storagev1.StorageVendor{}
	err := dpuClusterClient.Get(ctx, storageVendorNamespacedName, apiStorageVendor)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to get StorageVendor from DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		reqLog.Info("StorageVendor in the DPU cluster not found, removing finalizer")
		controllerutil.RemoveFinalizer(dpuStorageVendor, storagev1.DPUStorageVendorFinalizer)
		return ctrl.Result{}, nil
	}

	if !apiStorageVendor.GetDeletionTimestamp().IsZero() {
		reqLog.Info("StorageVendor in the DPU cluster is already deleting")
		r.setAwaitingDeletion(dpuStorageVendor)
		return ctrl.Result{}, nil
	}

	reqLog.Info("Delete StorageVendor")
	err = dpuClusterClient.Delete(ctx, apiStorageVendor)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to delete StorageVendor from DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		reqLog.Info("StorageVendor in the DPU cluster not found, removing finalizer")
		controllerutil.RemoveFinalizer(dpuStorageVendor, storagev1.DPUStorageVendorFinalizer)
		return ctrl.Result{}, nil
	}

	reqLog.Info("StorageVendor in the DPU cluster is deleting")
	r.setAwaitingDeletion(dpuStorageVendor)
	return ctrl.Result{}, nil
}

// setAwaitingDeletion sets ConditionDPUStorageVendorReconciled to ReasonAwaitingDeletion
// with the message "StorageVendor is deleting".
func (r *DPUStorageVendorReconciler) setAwaitingDeletion(dpuStorageVendor *storagev1.DPUStorageVendor) {
	conditions.AddFalse(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled,
		conditions.ReasonAwaitingDeletion, conditions.ConditionMessage("StorageVendor is deleting"))
}

// getStorageVendorNameFromDPUStorageVendorName returns the namespaced name of the corresponding StorageVendor
func (r *DPUStorageVendorReconciler) getStorageVendorNameFromDPUStorageVendorName(dpuStorageVendorName types.NamespacedName) types.NamespacedName {
	return types.NamespacedName{Name: dpuStorageVendorName.Name, Namespace: r.Options.TargetNamespace}
}

// validateExistingStorageVendorSpec checks if the existing StorageVendor matches the desired spec
func (r *DPUStorageVendorReconciler) validateExistingStorageVendorSpec(
	desiredStorageVendor, actualStorageVendor *storagev1.StorageVendor) bool {
	return equality.Semantic.DeepEqual(desiredStorageVendor.Spec, actualStorageVendor.Spec)
}

// enqueueDPUStorageVendorByStorageVendor returns a MapFunc that maps StorageVendor objects to corresponding DPUStorageVendor reconcile requests
func (r *DPUStorageVendorReconciler) enqueueDPUStorageVendorByStorageVendor() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		sv, ok := o.(*storagev1.StorageVendor)
		if !ok {
			return nil
		}
		if sv.GetNamespace() != r.Options.TargetNamespace {
			// ignore the object if it is originated not from the target namespace
			return nil
		}
		return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: r.Options.Namespace, Name: sv.Name}}}
	}
}

// enqueueAllDPUStorageVendors returns a MapFunc that enqueues all DPUStorageVendor objects for reconciliation
// when a DPUCluster object changes, ensuring all vendors are re-evaluated against the cluster state
func (r *DPUStorageVendorReconciler) enqueueAllDPUStorageVendors() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		result := []reconcile.Request{}
		dpuStorageVendorList := &storagev1.DPUStorageVendorList{}
		if err := r.Client.List(ctx, dpuStorageVendorList, client.InNamespace(r.Options.Namespace)); err != nil {
			return nil
		}
		for _, m := range dpuStorageVendorList.Items {
			name := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
			result = append(result, reconcile.Request{NamespacedName: name})
		}
		return result
	}
}

// watchStorageVendorsInDPUCluster watches for StorageVendor changes in the DPU cluster
func (r *DPUStorageVendorReconciler) watchStorageVendorsInDPUCluster(ctx context.Context, dpuCluster client.ObjectKey) error {
	return r.RemoteCache.Watch(ctx, dpuCluster, dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpustoragevendor-watch-storagevendor",
		Watcher:      r.controller,
		Kind:         &storagev1.StorageVendor{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.enqueueDPUStorageVendorByStorageVendor()),
	}))
}

// SetupWithManager sets up the controller with the Manager.

//nolint:dupl
func (r *DPUStorageVendorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	r.controller, err = ctrl.NewControllerManagedBy(mgr).
		For(&storagev1.DPUStorageVendor{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllDPUStorageVendors())).
		Build(r)
	return err
}
