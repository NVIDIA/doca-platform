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

package controllers

import (
	"context"
	"fmt"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	"github.com/fluxcd/pkg/runtime/patch"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	storagePolicyControllerName = "storagePolicyController"
)

// StoragePolicy reconciles a StoragePolicy object
type StoragePolicy struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=storagepolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=storagepolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=storagevendors,verbs=get;list;watch;update;patch

func (r *StoragePolicy) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")

	storagePolicy := &snapstoragev1.StoragePolicy{}
	if err := r.Get(ctx, req.NamespacedName, storagePolicy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	patcher := patch.NewSerialPatcher(storagePolicy, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")

		if err := r.updateSummary(ctx, storagePolicy); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}

		if err := patcher.Patch(ctx, storagePolicy,
			patch.WithFieldOwner(storagePolicyControllerName),
			patch.WithStatusObservedGeneration{},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StoragePolicy) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&snapstoragev1.StoragePolicy{}).
		Watches(&snapstoragev1.StorageVendor{},
			handler.EnqueueRequestsFromMapFunc(r.storageVendorToReq)).
		Watches(&storagev1.StorageClass{},
			handler.EnqueueRequestsFromMapFunc(r.storageClassToReq)).
		Complete(r)
}

func (r *StoragePolicy) storageClassToReq(ctx context.Context, resource client.Object) []reconcile.Request {
	storagePolicyList := &snapstoragev1.StoragePolicyList{}
	if err := r.List(ctx, storagePolicyList); err != nil {
		return nil
	}
	requests := []reconcile.Request{}
	for _, item := range storagePolicyList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			}})
	}
	return requests
}

func (r *StoragePolicy) storageVendorToReq(ctx context.Context, resource client.Object) []reconcile.Request {
	storagePolicyList := &snapstoragev1.StoragePolicyList{}
	if err := r.List(ctx, storagePolicyList); err != nil {
		return nil
	}
	requests := []reconcile.Request{}
	for _, item := range storagePolicyList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			}})
	}
	return requests
}

func (r *StoragePolicy) updateSummary(ctx context.Context, storagePolicy *snapstoragev1.StoragePolicy) error {
	// a storage policy is valid if all provided storage vendors (storageVendors)
	// have a StorageVendor object with a valid storage class object
	if len(storagePolicy.Spec.StorageVendors) == 0 {
		storagePolicy.Status.State = snapstoragev1.StorageVendorStateInvalid
		storagePolicy.Status.Message = "storage vendor is empty"
		return nil
	}

	for _, storageVendor := range storagePolicy.Spec.StorageVendors {
		// check if the storage vendor object exist
		storageVendorName := types.NamespacedName{
			Name:      storageVendor,
			Namespace: storagePolicy.GetNamespace(),
		}
		storageVendor := &snapstoragev1.StorageVendor{}
		if err := r.Get(ctx, storageVendorName, storageVendor); err != nil {
			if apierrors.IsNotFound(err) {
				storagePolicy.Status.State = snapstoragev1.StorageVendorStateInvalid
				storagePolicy.Status.Message = fmt.Sprintf("storage vendor %s object does exist", storageVendorName.String())
				return nil
			}
			return err
		}

		// check if the storage class object exist
		storageClassName := types.NamespacedName{
			Name: storageVendor.Spec.StorageClassName,
		}
		storageClass := &storagev1.StorageClass{}
		if err := r.Get(ctx, storageClassName, storageClass); err != nil {
			if apierrors.IsNotFound(err) {
				storagePolicy.Status.State = snapstoragev1.StorageVendorStateInvalid
				storagePolicy.Status.Message = fmt.Sprintf("storage class %s object does exist", storageClassName.String())
				return nil
			}
			return err
		}
	}
	storagePolicy.Status.State = snapstoragev1.StorageVendorStateValid
	storagePolicy.Status.Message = ""
	return nil
}
