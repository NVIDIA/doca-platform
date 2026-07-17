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
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/features"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/ecpf"
	"github.com/nvidia/doca-platform/pkg/ovsutils"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const NodeServiceInterfacesController = "nodeserviceinterfacescontroller"

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

func setFalseNodeServiceInterfacesReconciledCondition(err error, nsi *dpuservicev1.NodeServiceInterfaces) {
	conditions.AddFalse(
		nsi,
		dpuservicev1.NodeServiceInterfacesReconciled,
		conditions.ReasonError,
		conditions.ConditionMessage(fmt.Sprintf("Error occurred: %v", err)),
	)
}

func setTrueNodeServiceInterfacesReconciledCondition(nsi *dpuservicev1.NodeServiceInterfaces) {
	conditions.AddTrue(
		nsi,
		dpuservicev1.NodeServiceInterfacesReconciled,
	)
}

// ensureEntryStatus guarantees GetEntryStatus(name) won't return nil.
func ensureEntryStatus(nsi *dpuservicev1.NodeServiceInterfaces, name string) {
	for i := range nsi.Status.InterfaceStatuses {
		if nsi.Status.InterfaceStatuses[i].Name == name {
			return
		}
	}
	nsi.Status.InterfaceStatuses = append(nsi.Status.InterfaceStatuses, dpuservicev1.InterfaceEntryStatus{Name: name})
}

// pruneOrphanedEntryStatuses drops status entries whose spec entry was already removed.
func pruneOrphanedEntryStatuses(nsi *dpuservicev1.NodeServiceInterfaces) {
	live := make(map[string]bool, len(nsi.Spec.Interfaces))
	for _, entry := range nsi.Spec.Interfaces {
		live[entry.Name] = true
	}

	kept := nsi.Status.InterfaceStatuses[:0]
	for _, status := range nsi.Status.InterfaceStatuses {
		if live[status.Name] {
			kept = append(kept, status)
		}
	}
	nsi.Status.InterfaceStatuses = kept
}

// reconcileDelete cleans up every spec entry from OVS regardless of its terminating flag, since the object is disappearing before the controller managing it can drain it.
func (r *NodeServiceInterfacesReconciler) reconcileDelete(ctx context.Context, nsi *dpuservicev1.NodeServiceInterfaces) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")

	var errs []error
	for i := range nsi.Spec.Interfaces {
		entry := &nsi.Spec.Interfaces[i]
		if err := DeleteInterfacesFromOvs(ctx, r.OVS, r.ECPFManager, entry, entry.Name); err != nil {
			log.Error(err, "failed to delete interface from OVS", "entry", entry.Name)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		err := kerrors.NewAggregate(errs)
		setFalseNodeServiceInterfacesReconciledCondition(err, nsi)
		return requeueError()
	}

	controllerutil.RemoveFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)
	return requeueDone()
}

// reconcileEntry reconciles a single interface entry in OVS and updates its status condition.
func (r *NodeServiceInterfacesReconciler) reconcileEntry(ctx context.Context, nsi *dpuservicev1.NodeServiceInterfaces, entry *dpuservicev1.InterfaceEntry) error {
	log := ctrllog.FromContext(ctx)
	entryStatus := nsi.GetEntryStatus(entry.Name)

	if entry.Terminating {
		if conditions.IsTrue(entryStatus, dpuservicev1.ResourceReleased) {
			return nil
		}
		if err := DeleteInterfacesFromOvs(ctx, r.OVS, r.ECPFManager, entry, entry.Name); err != nil {
			log.Error(err, "failed to release interface", "entry", entry.Name)
			return err
		}
		conditions.AddTrue(entryStatus, dpuservicev1.ResourceReleased)
		return nil
	}

	if err := AddInterfacesToOvs(ctx, r.OVS, r.ECPFManager, entry, entry.Name); err != nil {
		log.Error(err, "failed to reconcile interface", "entry", entry.Name)
		conditions.AddFalse(entryStatus, conditions.TypeReady, conditions.ReasonError, conditions.ConditionMessage(fmt.Sprintf("Error occurred: %v", err)))
		return err
	}
	conditions.AddTrue(entryStatus, conditions.TypeReady)
	return nil
}

// Reconcile moves the current state of the cluster closer to the desired state.
func (r *NodeServiceInterfacesReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.V(4).Info("reconciling")
	if req.Namespace != utils.NSIObjectsNamespace {
		return requeueDone()
	}
	nsi := &dpuservicev1.NodeServiceInterfaces{}

	if err := r.Client.Get(ctx, req.NamespacedName, nsi); err != nil {
		if apierrors.IsNotFound(err) {
			return requeueDone()
		}
		log.Error(err, "failed to get NodeServiceInterfaces")
		return requeueError()
	}

	patcher := patch.NewSerialPatcher(nsi, r.Client)
	conditions.EnsureConditions(nsi, dpuservicev1.NodeServiceInterfacesConditions)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.V(4).Info("Patching")

		conditions.SetSummary(nsi)
		if err := patcher.Patch(ctx, nsi,
			patch.WithFieldOwner(NodeServiceInterfacesController),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.NodeServiceInterfacesConditions)},
		); err != nil && !isOnlyNotFoundErr(err) {
			// Removing the last finalizer above can race the object's deletion, so a NotFound here is expected, not a failure.
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	if !nsi.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, nsi)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer) {
		controllerutil.AddFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)
		return ctrl.Result{}, nil
	}

	pruneOrphanedEntryStatuses(nsi)

	var errs []error
	for i := range nsi.Spec.Interfaces {
		entry := &nsi.Spec.Interfaces[i]
		ensureEntryStatus(nsi, entry.Name)
		if err := r.reconcileEntry(ctx, nsi, entry); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		err := kerrors.NewAggregate(errs)
		setFalseNodeServiceInterfacesReconciledCondition(err, nsi)
		return requeueError()
	}

	setTrueNodeServiceInterfacesReconciledCondition(nsi)
	return requeueSuccess()
}

// SetupWithManager sets up the controller with the Manager.
// The controller is only registered when the NSIPathForSFC feature gate is enabled.
func (r *NodeServiceInterfacesReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Register the spec.node index unconditionally: the stale remover filters NSI objects
	// by node even when the feature gate (and this controller) are disabled.
	if err := utils.SetupNSINodeIndexer(context.Background(), mgr); err != nil {
		return err
	}

	if !features.Gates.Enabled(features.NSIPathForSFC) {
		return nil
	}

	nsiPredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
		nsi, ok := o.(*dpuservicev1.NodeServiceInterfaces)
		if !ok {
			return false
		}

		return isSFCNodeShard(nsi, r.NodeName)
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&dpuservicev1.NodeServiceInterfaces{},
			builder.WithPredicates(nsiPredicate),
		).
		Complete(r)
}
