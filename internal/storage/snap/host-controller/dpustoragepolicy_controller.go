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

// DPUStoragePolicyReconciler reconciles a DPUStoragePolicy object
type DPUStoragePolicyReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	controller  controller.Controller
	RemoteCache *dpucluster.RemoteCache
	Options     Options
}

const (
	dpuStoragePolicyControllerName = "dpustoragepolicycontroller"
)

// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragepolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragepolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragepolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch

// Reconcile reconciles changes in a DPUStoragePolicy.

//nolint:dupl
func (r *DPUStoragePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling")

	dpuCluster := &provisioningv1.DPUCluster{}
	targetDPUCluster := r.Options.DPUCluster
	err := r.Get(ctx, targetDPUCluster, dpuCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPU cluster %s: %w", targetDPUCluster.String(), err)
	}
	if err := r.watchStoragePoliciesInDPUCluster(ctx, targetDPUCluster); err != nil {
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

	dpuStoragePolicy := &storagev1.DPUStoragePolicy{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuStoragePolicy); err != nil {
		if apierrors.IsNotFound(err) {
			// DPUStoragePolicy is not found, but we need to ensure that the corresponding
			// StoragePolicy in the DPU cluster is also removed to avoid orphaned resources.
			return cleanupOrphanedObject(ctx, dpuClusterClient,
				r.getStoragePolicyNameFromDPUStoragePolicyName(req.NamespacedName),
				&storagev1.StoragePolicy{},
				"StoragePolicy")
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuStoragePolicy, r.Client)

	conditions.EnsureConditions(dpuStoragePolicy, storagev1.DPUStoragePolicyConditions)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		r.finalizeConditions(dpuStoragePolicy, reterr)
		reqLog.Info("Patching")
		if err := patcher.Patch(ctx, dpuStoragePolicy,
			patch.WithFieldOwner(dpuStoragePolicyControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(storagev1.DPUStoragePolicyConditions)},
		); kerrors.FilterOut(err, apierrors.IsNotFound) != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !dpuStoragePolicy.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuClusterClient, dpuStoragePolicy)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuStoragePolicy, storagev1.DPUStoragePolicyFinalizer) {
		reqLog.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuStoragePolicy, storagev1.DPUStoragePolicyFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuClusterClient, dpuStoragePolicy)
}

// finalizeConditions updates the conditions of the DPUStoragePolicy
func (r *DPUStoragePolicyReconciler) finalizeConditions(dpuStoragePolicy *storagev1.DPUStoragePolicy, err error) {
	// in case of any error set ConditionDPUStoragePolicyReconciled to false with error reason
	if err != nil {
		conditions.AddFalse(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyReconciled,
			conditions.ReasonError, conditions.ConditionMessage(err.Error()))
	}
	conditions.SetSummary(dpuStoragePolicy)
}

// reconcile reconciles the DPUStoragePolicy
//
//nolint:unparam
func (r *DPUStoragePolicyReconciler) reconcile(ctx context.Context, dpuClusterClient client.Client,
	dpuStoragePolicy *storagev1.DPUStoragePolicy) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)

	// Create/update the StoragePolicy in the DPU cluster
	storagePolicyNamespacedName := r.getStoragePolicyNameFromDPUStoragePolicyName(client.ObjectKeyFromObject(dpuStoragePolicy))
	desiredStoragePolicy := &storagev1.StoragePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      storagePolicyNamespacedName.Name,
			Namespace: storagePolicyNamespacedName.Namespace,
		},
		Spec: storagev1.StoragePolicySpec{
			StorageVendors:      dpuStoragePolicy.Spec.DPUStorageVendors,
			StorageParameters:   dpuStoragePolicy.Spec.Parameters,
			StorageSelectionAlg: ConvertDPUSelectionAlgorithmToStorageSelectionAlg(dpuStoragePolicy.Spec.SelectionAlgorithm),
		},
	}

	apiStoragePolicy := &storagev1.StoragePolicy{}
	err := dpuClusterClient.Get(ctx, storagePolicyNamespacedName, apiStoragePolicy)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to read StoragePolicy from DPU cluster: %w", err)
	}

	if apierrors.IsNotFound(err) {
		reqLog.Info("StoragePolicy not found in the DPU cluster, creating")
		apiStoragePolicy = desiredStoragePolicy
		if err := dpuClusterClient.Create(ctx, apiStoragePolicy); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create StoragePolicy in DPU cluster: %w", err)
		}
	} else {
		reqLog.Info("StoragePolicy found in the DPU cluster")
		// Check if StoragePolicy is being deleted in the DPU cluster.
		// This is an edge case that may occur due to manual intervention (e.g. manual deletion of the StoragePolicy in the DPU cluster).
		// In this case we wait for deletion to complete and then recreate the StoragePolicy.
		if !apiStoragePolicy.ObjectMeta.DeletionTimestamp.IsZero() {
			reqLog.Info("StoragePolicy in the DPU cluster is deleting")
			r.setAwaitingDeletion(dpuStoragePolicy)
			return ctrl.Result{}, nil
		}
		// StoragePolicy exists, check if it needs to be updated
		if !r.validateExistingStoragePolicySpec(desiredStoragePolicy, apiStoragePolicy) {
			reqLog.Info("StoragePolicy in the DPU cluster has different parameters, updating")
			apiStoragePolicy.Spec = desiredStoragePolicy.Spec
			if err := dpuClusterClient.Update(ctx, apiStoragePolicy); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update StoragePolicy in DPU cluster: %w", err)
			}
		}
	}
	if apiStoragePolicy.Status.State == storagev1.StorageVendorStateValid {
		conditions.AddTrue(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyValid)
	} else {
		conditions.AddFalse(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyValid,
			conditions.ReasonPending, conditions.ConditionMessage(apiStoragePolicy.Status.Message))
	}
	conditions.AddTrue(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyReconciled)
	return ctrl.Result{}, nil
}

// reconcileDelete handles deletion of the DPUStoragePolicy
//
//nolint:dupl,unparam
func (r *DPUStoragePolicyReconciler) reconcileDelete(ctx context.Context,
	dpuClusterClient client.Client, dpuStoragePolicy *storagev1.DPUStoragePolicy) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling delete")

	storagePolicyNamespacedName := r.getStoragePolicyNameFromDPUStoragePolicyName(client.ObjectKeyFromObject(dpuStoragePolicy))
	apiStoragePolicy := &storagev1.StoragePolicy{}
	err := dpuClusterClient.Get(ctx, storagePolicyNamespacedName, apiStoragePolicy)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to get StoragePolicy from DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		reqLog.Info("StoragePolicy in the DPU cluster not found, removing finalizer")
		controllerutil.RemoveFinalizer(dpuStoragePolicy, storagev1.DPUStoragePolicyFinalizer)
		return ctrl.Result{}, nil
	}

	if !apiStoragePolicy.GetDeletionTimestamp().IsZero() {
		reqLog.Info("StoragePolicy in the DPU cluster is already deleting")
		r.setAwaitingDeletion(dpuStoragePolicy)
		return ctrl.Result{}, nil
	}

	reqLog.Info("Delete StoragePolicy")
	err = dpuClusterClient.Delete(ctx, apiStoragePolicy)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to delete StoragePolicy from DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		reqLog.Info("StoragePolicy in the DPU cluster not found, removing finalizer")
		controllerutil.RemoveFinalizer(dpuStoragePolicy, storagev1.DPUStoragePolicyFinalizer)
		return ctrl.Result{}, nil
	}

	reqLog.Info("StoragePolicy in the DPU cluster is deleting")
	r.setAwaitingDeletion(dpuStoragePolicy)
	return ctrl.Result{}, nil
}

// getStoragePolicyNameFromDPUStoragePolicyName returns the namespaced name of the corresponding StoragePolicy
func (r *DPUStoragePolicyReconciler) getStoragePolicyNameFromDPUStoragePolicyName(dpuStoragePolicyName types.NamespacedName) types.NamespacedName {
	return types.NamespacedName{Name: dpuStoragePolicyName.Name, Namespace: r.Options.TargetNamespace}
}

// validateExistingStoragePolicySpec checks if the existing StoragePolicy matches the desired spec
func (r *DPUStoragePolicyReconciler) validateExistingStoragePolicySpec(
	desiredStoragePolicy, actualStoragePolicy *storagev1.StoragePolicy) bool {
	return equality.Semantic.DeepEqual(desiredStoragePolicy.Spec, actualStoragePolicy.Spec)
}

// setAwaitingDeletion sets ConditionDPUStoragePolicyReconciled to ReasonAwaitingDeletion
// with the message "StoragePolicy is deleting".
func (r *DPUStoragePolicyReconciler) setAwaitingDeletion(dpuStoragePolicy *storagev1.DPUStoragePolicy) {
	conditions.AddFalse(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyReconciled,
		conditions.ReasonAwaitingDeletion, conditions.ConditionMessage("StoragePolicy is deleting"))
}

// enqueueAllDPUStoragePolicies returns a MapFunc that enqueues all DPUStoragePolicy objects for reconciliation
func (r *DPUStoragePolicyReconciler) enqueueAllDPUStoragePolicies() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		result := []reconcile.Request{}
		dpuStoragePolicyList := &storagev1.DPUStoragePolicyList{}
		if err := r.Client.List(ctx, dpuStoragePolicyList, client.InNamespace(r.Options.Namespace)); err != nil {
			return nil
		}
		for _, m := range dpuStoragePolicyList.Items {
			name := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
			result = append(result, reconcile.Request{NamespacedName: name})
		}
		return result
	}
}

// enqueueDPUStoragePolicyByStoragePolicy returns a MapFunc that maps StoragePolicy objects to corresponding DPUStoragePolicy reconcile requests
func (r *DPUStoragePolicyReconciler) enqueueDPUStoragePolicyByStoragePolicy() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		sp, ok := o.(*storagev1.StoragePolicy)
		if !ok {
			return nil
		}
		if sp.GetNamespace() != r.Options.TargetNamespace {
			// ignore the object if it doesn't originate from the target namespace
			return nil
		}
		return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: r.Options.Namespace, Name: sp.Name}}}
	}
}

// watchStoragePoliciesInDPUCluster watches for StoragePolicy changes in the DPU cluster
func (r *DPUStoragePolicyReconciler) watchStoragePoliciesInDPUCluster(ctx context.Context, dpuCluster client.ObjectKey) error {
	return r.RemoteCache.Watch(ctx, dpuCluster, dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpustoragepolicy-watch-storagepolicy",
		Watcher:      r.controller,
		Kind:         &storagev1.StoragePolicy{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.enqueueDPUStoragePolicyByStoragePolicy()),
	}))
}

// SetupWithManager sets up the controller with the Manager.

//nolint:dupl
func (r *DPUStoragePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	r.controller, err = ctrl.NewControllerManagedBy(mgr).
		For(&storagev1.DPUStoragePolicy{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllDPUStoragePolicies())).
		Build(r)
	return err
}
