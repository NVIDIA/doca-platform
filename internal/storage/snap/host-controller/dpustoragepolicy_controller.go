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
	"fmt"
	"strings"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/indexers"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// DPUStoragePolicyReconciler reconciles a DPUStoragePolicy object
type DPUStoragePolicyReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Options Options
}

const (
	dpuStoragePolicyControllerName = "dpustoragepolicycontroller"
)

// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragepolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragepolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragepolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpustoragevendors,verbs=get;list;watch

// Reconcile reconciles changes in a DPUStoragePolicy.

//nolint:dupl
func (r *DPUStoragePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling")

	dpuStoragePolicy := &storagev1.DPUStoragePolicy{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuStoragePolicy); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, nothing to do
			reqLog.Info("not found, skipping")
			return ctrl.Result{}, nil
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
		return r.reconcileDelete(ctx, dpuStoragePolicy)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuStoragePolicy, storagev1.DPUStoragePolicyFinalizer) {
		reqLog.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuStoragePolicy, storagev1.DPUStoragePolicyFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuStoragePolicy)
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

// reconcile handles DPUStoragePolicy validation against referenced DPUStorageVendors.
//
//nolint:unparam
func (r *DPUStoragePolicyReconciler) reconcile(ctx context.Context, dpuStoragePolicy *storagev1.DPUStoragePolicy) (ctrl.Result, error) {
	isValid := true
	statusSummary := make([]string, 0, len(dpuStoragePolicy.Spec.DPUStorageVendors))
	for _, vendorName := range dpuStoragePolicy.Spec.DPUStorageVendors {
		vendor := &storagev1.DPUStorageVendor{}
		vendorNamespacedName := types.NamespacedName{Namespace: dpuStoragePolicy.Namespace, Name: vendorName}
		if err := r.Client.Get(ctx, vendorNamespacedName, vendor); err != nil {
			if apierrors.IsNotFound(err) {
				isValid = false
				statusSummary = append(statusSummary, fmt.Sprintf("* DPUStorageVendor %s: not found", vendorNamespacedName.String()))
				continue
			}
			return ctrl.Result{}, fmt.Errorf("failed to get DPUStorageVendor %s: %w", vendorNamespacedName.String(), err)
		}
		if !conditions.IsTrue(vendor, conditions.TypeReady) {
			isValid = false
			statusSummary = append(statusSummary, fmt.Sprintf("* DPUStorageVendor %s: vendor is invalid", vendorNamespacedName.String()))
		}
	}
	if isValid {
		conditions.AddTrue(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyValid)
	} else {
		conditions.AddFalse(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyValid,
			conditions.ReasonError, conditions.ConditionMessage(fmt.Sprintf("DPUStoragePolicy is not valid:\n%s", strings.Join(statusSummary, "\n"))))
	}
	conditions.AddTrue(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyReconciled)
	return ctrl.Result{}, nil
}

// reconcileDelete handles deletion of the DPUStoragePolicy by removing finalizer.
//
//nolint:unparam
func (r *DPUStoragePolicyReconciler) reconcileDelete(ctx context.Context, dpuStoragePolicy *storagev1.DPUStoragePolicy) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling delete")
	controllerutil.RemoveFinalizer(dpuStoragePolicy, storagev1.DPUStoragePolicyFinalizer)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.

//nolint:dupl
func (r *DPUStoragePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&storagev1.DPUStoragePolicy{}).
		// Watch DPUStorageVendor and enqueue only related DPUStoragePolicies
		Watches(&storagev1.DPUStorageVendor{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUStoragePoliciesByVendor)).
		Build(r)
	return err
}

// enqueueDPUStoragePoliciesByVendor enqueues DPUStoragePolicy objects when DPUStorageVendor changes
//
//nolint:dupl
func (r *DPUStoragePolicyReconciler) enqueueDPUStoragePoliciesByVendor(ctx context.Context, o client.Object) []reconcile.Request {
	vendor, ok := o.(*storagev1.DPUStorageVendor)
	if !ok {
		return nil
	}
	dpuStoragePolicyList := &storagev1.DPUStoragePolicyList{}
	if err := r.Client.List(ctx, dpuStoragePolicyList,
		client.InNamespace(r.Options.Namespace),
		client.MatchingFields{indexers.DPUStoragePolicySpecDPUStorageVendors: vendor.Name}); err != nil {
		return nil
	}
	result := make([]reconcile.Request, 0, len(dpuStoragePolicyList.Items))
	for _, m := range dpuStoragePolicyList.Items {
		result = append(result, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&m)})
	}
	return result
}
