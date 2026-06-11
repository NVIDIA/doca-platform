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

//nolint:dupl
package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/pkg/utils/predicates"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//nolint:dupl
var _ objectsInDPUClustersReconciler = &DPUServiceInterfaceReconciler{}

func (r *DPUServiceInterfaceReconciler) calculateDPUServiceObjectStateBasedOnStatus(_ []*dpucluster.Config, _ dpuservicev1.DPUServiceObject) (bool, error) {
	return false, nil
}

// DPUServiceInterfaceReconciler reconciles a DPUServiceInterface object
type DPUServiceInterfaceReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	controller  controller.Controller
	RemoteCache *dpucluster.RemoteCache
}

const (
	dpuServiceInterfaceControllerName = "dpuserviceinterfacecontroller"
)

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuserviceinterfaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuserviceinterfaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuserviceinterfaces/finalizers,verbs=update
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs,verbs=get;list;watch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=isolationclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kamaji.clastix.io,resources=tenantcontrolplanes,verbs=get;list;watch

// Reconcile reconciles changes in a DPUServiceInterface.
//
//nolint:dupl
func (r *DPUServiceInterfaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")

	dpuServiceInterface := &dpuservicev1.DPUServiceInterface{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuServiceInterface); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuServiceInterface, r.Client)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")

		if err := updateSummary(ctx, r, r.Client, dpuservicev1.ConditionServiceInterfaceSetReady, dpuServiceInterface); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
		if err := patcher.Patch(ctx, dpuServiceInterface,
			patch.WithFieldOwner(dpuServiceInterfaceControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.DPUServiceInterfaceConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	conditions.EnsureConditions(dpuServiceInterface, dpuservicev1.DPUServiceInterfaceConditions)

	// Handle deletion reconciliation loop.
	if !dpuServiceInterface.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuServiceInterface)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuServiceInterface, dpuservicev1.DPUServiceInterfaceFinalizer) {
		log.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuServiceInterface, dpuservicev1.DPUServiceInterfaceFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuServiceInterface)
}

// reconcile handles the main reconciliation loop
//
//nolint:unparam
func (r *DPUServiceInterfaceReconciler) reconcile(ctx context.Context, dpuServiceInterface *dpuservicev1.DPUServiceInterface) (ctrl.Result, error) {
	ready, err := r.reconcilePreReqs(ctx, dpuServiceInterface)
	if err != nil {
		message := fmt.Sprintf("Unable to reconcile serviceinterface prereqs: %s", err.Error())
		conditions.AddFalse(
			dpuServiceInterface,
			dpuservicev1.ConditionServiceInterfacePreReqsReady,
			conditions.ReasonError,
			conditions.ConditionMessage(message),
		)
		return ctrl.Result{}, err
	}
	if !ready {
		// the specific condition reason is already set inside the reconcilePreReqs function
		return ctrl.Result{}, nil
	}
	conditions.AddTrue(dpuServiceInterface, dpuservicev1.ConditionServiceInterfacePreReqsReady)

	if err := reconcileObjectsInDPUClusters(ctx, r, r.Client, dpuServiceInterface); err != nil {
		e := &longOperationError{}
		conditions.AddFalse(
			dpuServiceInterface,
			dpuservicev1.ConditionServiceInterfaceSetReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		// We enqueue without exponential backoff if the error is a longOperationError because this
		// error is returned when we know an operation that was triggered will take time to complete
		if errors.As(err, &e) {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}
	conditions.AddTrue(
		dpuServiceInterface,
		dpuservicev1.ConditionServiceInterfaceSetReconciled,
	)

	// start watching the ServiceInterfaceSet in the DPU clusters
	if err := watchObjectsInDPUClusters(ctx, r.Client, r); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// registerKindToWatcher registers the ServiceInterfaceSet kind to the remote cache watcher
// so that the DPUServiceInterface controller can watch for changes in the ServiceInterfaceSet objects
// in the DPU clusters. This is used to trigger reconciliation of the DPUServiceInterface
// when a ServiceInterfaceSet is created, updated, or deleted in any of the DPU clusters.
func (r *DPUServiceInterfaceReconciler) registerKindToWatcher(ctx context.Context, dpuCluster client.ObjectKey) error {
	return r.RemoteCache.Watch(ctx, dpuCluster, dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuserviceinterface-watch-serviceinterfaceset",
		Watcher:      r.controller,
		Kind:         &dpuservicev1.ServiceInterfaceSet{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.serviceInterfaceSetToDPUServiceInterface),
		Predicates:   []predicate.TypedPredicate[client.Object]{predicates.TypedResourceIsChanged[client.Object]()},
	}))
}

func (r *DPUServiceInterfaceReconciler) serviceInterfaceSetToDPUServiceInterface(ctx context.Context, o client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	set, ok := o.(*dpuservicev1.ServiceInterfaceSet)
	if !ok {
		log.Error(fmt.Errorf("expected a ServiceInterfaceSet, got %T", o), "failed to convert object")
		return nil
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: set.Namespace, Name: set.Name}}}
}

// reconcilePreReqs sets a label with the provisioner for service interfaces that have virtualNetwork enabled.
// It returns a boolean indicating whether all required resources for querying the data are available,
// and an error for any critical failures.
// If false is returned, the function is expected to set the ConditionServiceInterfacePreReqsReady condition
// with the appropriate reason.
func (r *DPUServiceInterfaceReconciler) reconcilePreReqs(ctx context.Context, dpuServiceInterface *dpuservicev1.DPUServiceInterface) (bool, error) {
	virtualNetworkName := dpuServiceInterface.GetVirtualNetworkName()
	if virtualNetworkName == "" {
		// the interface does not belong to the virtual network, we don't need to do anything
		return true, nil
	}
	virtualNetNamespacedName := types.NamespacedName{
		Name:      virtualNetworkName,
		Namespace: dpuServiceInterface.GetNamespace()}

	virtualNet := &vpcv1.DPUVirtualNetwork{}
	if err := r.Client.Get(ctx, virtualNetNamespacedName, virtualNet); err != nil {
		if apierrors.IsNotFound(err) {
			conditions.AddFalse(dpuServiceInterface, dpuservicev1.ConditionServiceInterfacePreReqsReady, conditions.ReasonPending,
				conditions.ConditionMessage(
					fmt.Sprintf("waiting for creation of %s %s", vpcv1.DPUVirtualNetworkKind, virtualNetNamespacedName.String())))
			return false, nil
		}
		return false, fmt.Errorf("error while getting %s %s: %v", vpcv1.DPUVirtualNetworkKind, virtualNetNamespacedName.String(), err)
	}
	vpcNamespacedName := types.NamespacedName{
		Name:      virtualNet.Spec.VPCName,
		Namespace: dpuServiceInterface.GetNamespace(),
	}
	vpc := &vpcv1.DPUVPC{}
	if err := r.Client.Get(ctx, vpcNamespacedName, vpc); err != nil {
		if apierrors.IsNotFound(err) {
			conditions.AddFalse(dpuServiceInterface, dpuservicev1.ConditionServiceInterfacePreReqsReady, conditions.ReasonPending,
				conditions.ConditionMessage(
					fmt.Sprintf("waiting for creation of %s %s", vpcv1.DPUVPCKind, vpcNamespacedName.String())))
			return false, nil
		}
		return false, fmt.Errorf("error while getting %s %s: %v", vpcv1.DPUVPCKind, vpcNamespacedName.String(), err)
	}
	isolationClassName := types.NamespacedName{
		Name:      vpc.Spec.IsolationClassName,
		Namespace: dpuServiceInterface.GetNamespace(),
	}
	isolationClass := &vpcv1.IsolationClass{}
	if err := r.Client.Get(ctx, isolationClassName, isolationClass); err != nil {
		if apierrors.IsNotFound(err) {
			conditions.AddFalse(dpuServiceInterface, dpuservicev1.ConditionServiceInterfacePreReqsReady, conditions.ReasonPending,
				conditions.ConditionMessage(
					fmt.Sprintf("waiting for creation of %s %s", vpcv1.IsolationClassKind, isolationClassName.String())))
			return false, nil
		}
		return false, fmt.Errorf("error while getting %s %s: %v", vpcv1.IsolationClassKind, isolationClassName.String(), err)
	}
	if dpuServiceInterface.Spec.Template.Labels == nil {
		dpuServiceInterface.Spec.Template.Labels = map[string]string{}
	}
	dpuServiceInterface.Spec.Template.Labels[vpcv1.ProvisionerNameLabel] = isolationClass.Spec.Provisioner
	return true, nil
}

func (r *DPUServiceInterfaceReconciler) getObjectsInDPUCluster(ctx context.Context, k8sClient client.Client, dpuObject dpuservicev1.DPUServiceObject) ([]unstructured.Unstructured, error) {
	sis := &unstructured.Unstructured{}
	sis.SetGroupVersionKind(dpuservicev1.ServiceInterfaceSetGroupVersionKind)
	key := client.ObjectKey{Namespace: dpuObject.GetNamespace(), Name: dpuObject.GetName()}
	err := k8sClient.Get(ctx, key, sis)
	if err != nil {
		return nil, fmt.Errorf("error while getting %s %s: %w", sis.GetObjectKind().GroupVersionKind().String(), key.String(), err)
	}

	return []unstructured.Unstructured{*sis}, nil
}

func (r *DPUServiceInterfaceReconciler) createOrUpdateObjectsInDPUCluster(ctx context.Context, k8sClient client.Client, _ types.NamespacedName, dpuObject dpuservicev1.DPUServiceObject) error {
	dpuServiceInterface := dpuObject.(*dpuservicev1.DPUServiceInterface)
	sis := &dpuservicev1.ServiceInterfaceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dpuServiceInterface.Name,
			Namespace:   dpuServiceInterface.Namespace,
			Labels:      dpuServiceInterface.Spec.Template.Labels,
			Annotations: dpuServiceInterface.Spec.Template.Annotations,
		},
		Spec: *dpuServiceInterface.Spec.Template.Spec.DeepCopy(),
	}
	sis.ObjectMeta.ManagedFields = nil
	sis.SetGroupVersionKind(dpuservicev1.GroupVersion.WithKind("ServiceInterfaceSet"))
	return k8sClient.Patch(ctx, sis, client.Apply, client.ForceOwnership, client.FieldOwner(dpuServiceInterfaceControllerName))
}

func (r *DPUServiceInterfaceReconciler) reconcileDelete(ctx context.Context, dpuServiceInterface *dpuservicev1.DPUServiceInterface) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")
	if err := reconcileObjectDeletionInDPUClusters(ctx, r, r.Client, dpuServiceInterface); err != nil {
		e := &longOperationError{}
		if errors.As(err, &e) {
			log.Info(fmt.Sprintf("Requeueing because %s", err.Error()))
			conditions.AddFalse(
				dpuServiceInterface,
				dpuservicev1.ConditionServiceInterfaceSetReconciled,
				conditions.ReasonAwaitingDeletion,
				conditions.ConditionMessage(err.Error()),
			)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(dpuServiceInterface, dpuservicev1.DPUServiceInterfaceFinalizer)
	return ctrl.Result{}, nil
}

func (r *DPUServiceInterfaceReconciler) deleteObjectsInDPUCluster(ctx context.Context, k8sClient client.Client, dpuObject dpuservicev1.DPUServiceObject) error {
	dpuServiceInterface := dpuObject.(*dpuservicev1.DPUServiceInterface)
	sis := &dpuservicev1.ServiceInterfaceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpuServiceInterface.Name,
			Namespace: dpuServiceInterface.Namespace,
		},
	}
	return k8sClient.Delete(ctx, sis)
}

func (r *DPUServiceInterfaceReconciler) getUnreadyObjects(objects []unstructured.Unstructured) ([]types.NamespacedName, error) {
	unreadyObjs := []types.NamespacedName{}
	for _, o := range objects {
		serviceInterfaceSet := &dpuservicev1.ServiceInterfaceSet{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &serviceInterfaceSet)
		if err != nil {
			return nil, fmt.Errorf("convert unstructured to %T: %w", serviceInterfaceSet, err)
		}
		if meta.IsStatusConditionTrue(serviceInterfaceSet.GetConditions(), string(conditions.TypeReady)) {
			continue
		}
		unreadyObjs = append(unreadyObjs, types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()})
	}
	return unreadyObjs, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUServiceInterfaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.DPUServiceInterface{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.DPUClusterToDPUServiceInterface)).
		Watches(&vpcv1.DPUVirtualNetwork{}, handler.EnqueueRequestsFromMapFunc(r.VPCDependenciesToDPUServiceInterface),
			builder.WithPredicates(createOnlyPredicate{})).
		Watches(&vpcv1.DPUVPC{}, handler.EnqueueRequestsFromMapFunc(r.VPCDependenciesToDPUServiceInterface),
			builder.WithPredicates(createOnlyPredicate{})).
		Watches(&vpcv1.IsolationClass{}, handler.EnqueueRequestsFromMapFunc(r.VPCDependenciesToDPUServiceInterface),
			builder.WithPredicates(createOnlyPredicate{})).
		Build(r)

	if err != nil {
		return err
	}

	r.controller = c
	return nil
}

// DPUClusterToDPUServiceInterface ensures all DPUServiceInterfaces are updated each time there is an update to a DPUCluster.
func (r *DPUServiceInterfaceReconciler) DPUClusterToDPUServiceInterface(ctx context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	dpuServiceList := &dpuservicev1.DPUServiceInterfaceList{}
	if err := r.Client.List(ctx, dpuServiceList); err != nil {
		return nil
	}
	for _, m := range dpuServiceList.Items {
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

// VPCDependenciesToDPUServiceInterface ensures VPC-related DPUServiceInterfaces are reconciled when there is an update to VPC CRs
func (r *DPUServiceInterfaceReconciler) VPCDependenciesToDPUServiceInterface(ctx context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
	if err := r.Client.List(ctx, dpuServiceInterfaceList); err != nil {
		return nil
	}
	for _, m := range dpuServiceInterfaceList.Items {
		if m.GetVirtualNetworkName() == "" {
			continue
		}
		if m.Spec.Template.Labels[vpcv1.ProvisionerNameLabel] != "" {
			// the object already updated with the required VPC label (update is not supported)
			continue
		}
		result = append(result, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: m.Namespace, Name: m.Name}})
	}
	return result
}

// createOnlyPredicate returns true only for create calls
type createOnlyPredicate struct{}

// Create always returns true
func (createOnlyPredicate) Create(e event.CreateEvent) bool { return true }

// Update always returns false
func (createOnlyPredicate) Update(e event.UpdateEvent) bool { return false }

// Delete always returns false
func (createOnlyPredicate) Delete(e event.DeleteEvent) bool { return false }

// Generic always returns false
func (createOnlyPredicate) Generic(e event.GenericEvent) bool { return false }
