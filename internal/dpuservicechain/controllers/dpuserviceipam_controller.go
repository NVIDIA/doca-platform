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
	"errors"
	"fmt"
	"maps"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/pkg/utils/predicates"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// ParentDPUServiceIPAMNameLabel points to the name of the DPUServiceIPAM object that owns a resource in the DPU
	// cluster.
	ParentDPUServiceIPAMNameLabel = "dpu.nvidia.com/dpuserviceipam-name"
	// ParentDPUServiceIPAMNamespaceLabel points to the namespace of the DPUServiceIPAM object that owns a resource in
	// the DPU cluster.
	ParentDPUServiceIPAMNamespaceLabel = "dpu.nvidia.com/dpuserviceipam-namespace"
)

// DPUServiceIPAMReconciler reconciles a DPUServiceIPAM object
type DPUServiceIPAMReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	controller  controller.Controller
	RemoteCache *dpucluster.RemoteCache
}

var _ objectsInDPUClustersReconciler = &DPUServiceIPAMReconciler{}

const (
	dpuServiceIPAMControllerName = "dpuserviceipamcontroller"
)

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuserviceipams,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuserviceipams/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuserviceipams/finalizers,verbs=update
// +kubebuilder:rbac:groups=nv-ipam.nvidia.com,resources=ippools,verbs=get;list;watch;create;update;patch;delete

// Reconcile reconciles changes in a DPUServiceIPAM object
//
//nolint:dupl
func (r *DPUServiceIPAMReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")

	dpuServiceIPAM := &dpuservicev1.DPUServiceIPAM{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuServiceIPAM); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuServiceIPAM, r.Client)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")

		if err := updateSummary(ctx, r, r.Client, dpuservicev1.ConditionDPUIPAMObjectReady, dpuServiceIPAM); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
		if err := patcher.Patch(ctx, dpuServiceIPAM,
			patch.WithFieldOwner(dpuServiceIPAMControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.DPUServiceIPAMConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	conditions.EnsureConditions(dpuServiceIPAM, dpuservicev1.DPUServiceIPAMConditions)

	// Handle deletion reconciliation loop.
	if !dpuServiceIPAM.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuServiceIPAM)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuServiceIPAM, dpuservicev1.DPUServiceIPAMFinalizer) {
		log.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuServiceIPAM, dpuservicev1.DPUServiceIPAMFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuServiceIPAM)
}

// reconcile handles the main reconciliation loop
//
//nolint:unparam
func (r *DPUServiceIPAMReconciler) reconcile(ctx context.Context, dpuServiceIPAM *dpuservicev1.DPUServiceIPAM) (ctrl.Result, error) {
	if err := reconcileObjectsInDPUClusters(ctx, r, r.Client, dpuServiceIPAM); err != nil {
		conditions.AddFalse(
			dpuServiceIPAM,
			dpuservicev1.ConditionDPUIPAMObjectReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, err
	}
	conditions.AddTrue(
		dpuServiceIPAM,
		dpuservicev1.ConditionDPUIPAMObjectReconciled,
	)

	// start watching the ServiceInterfaceSet in the DPU clusters
	if err := watchObjectsInDPUClusters(ctx, r.Client, r); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileDelete handles the delete reconciliation loop
//
//nolint:unparam
func (r *DPUServiceIPAMReconciler) reconcileDelete(ctx context.Context, dpuServiceIPAM *dpuservicev1.DPUServiceIPAM) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")

	if err := reconcileObjectDeletionInDPUClusters(ctx, r, r.Client, dpuServiceIPAM); err != nil {
		e := &shouldRequeueError{}
		if errors.As(err, &e) {
			log.Info(fmt.Sprintf("Requeueing because %s", err.Error()))
			conditions.AddFalse(
				dpuServiceIPAM,
				dpuservicev1.ConditionDPUIPAMObjectReconciled,
				conditions.ReasonAwaitingDeletion,
				conditions.ConditionMessage(err.Error()),
			)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, fmt.Errorf("error while reconciling deletion of objects in DPU clusters: %w", err)
	}

	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(dpuServiceIPAM, dpuservicev1.DPUServiceIPAMFinalizer)
	return ctrl.Result{}, nil
}

// getObjectsInDPUCluster is the method called by the reconcileObjectDeletionInDPUClusters function which deletes
// objects in the DPU cluster related to the given parentObject. The implementation should get the created objects
// in the DPU cluster.
func (r *DPUServiceIPAMReconciler) getObjectsInDPUCluster(ctx context.Context, c client.Client, dpuObject client.Object) ([]unstructured.Unstructured, error) {
	pools := []unstructured.Unstructured{}
	for _, poolListType := range []string{nvipamv1.IPPoolListKind, nvipamv1.CIDRPoolListKind} {
		p := &unstructured.UnstructuredList{}
		p.SetGroupVersionKind(nvipamv1.GroupVersion.WithKind(poolListType))
		if err := c.List(ctx, p, client.InNamespace(dpuObject.GetNamespace()), client.MatchingLabels{
			ParentDPUServiceIPAMNameLabel:      dpuObject.GetName(),
			ParentDPUServiceIPAMNamespaceLabel: dpuObject.GetNamespace(),
		}); err != nil {
			return nil, fmt.Errorf("error while listing %s as unstructured: %w", p.GetObjectKind().GroupVersionKind().String(), err)
		}

		pools = append(pools, p.Items...)
	}

	return pools, nil
}

// createOrUpdateChild is the method called by the reconcileObjectsInDPUClusters function which applies changes to the
// DPU clusters on DPUServiceIPAM object updates.
func (r *DPUServiceIPAMReconciler) createOrUpdateObjectsInDPUCluster(ctx context.Context, c client.Client, dpuObject client.Object) error {
	dpuServiceIPAM, ok := dpuObject.(*dpuservicev1.DPUServiceIPAM)
	if !ok {
		return errors.New("error converting input object to DPUServiceIPAM")
	}

	if dpuServiceIPAM.Spec.IPV4Subnet != nil {
		return reconcileIPPoolMode(ctx, c, dpuServiceIPAM)
	}

	return reconcileCIDRPoolMode(ctx, c, dpuServiceIPAM)
}

// deleteObjectsInDPUCluster is the method called by the reconcileObjectDeletionInDPUClusters function which deletes
// objects in the DPU cluster related to the deleted DPUServiceIPAM object.
func (r *DPUServiceIPAMReconciler) deleteObjectsInDPUCluster(ctx context.Context, c client.Client, dpuObject client.Object) error {
	dpuServiceIPAM, ok := dpuObject.(*dpuservicev1.DPUServiceIPAM)
	if !ok {
		return errors.New("error converting input object to DPUServiceIPAM")
	}

	for _, poolType := range []string{nvipamv1.IPPoolKind, nvipamv1.CIDRPoolKind} {
		if err := deleteDPUServiceOwnedPoolsOfType(ctx, c, dpuServiceIPAM, poolType); err != nil {
			return err
		}
	}

	return nil
}

// getUnreadyObjects is the method called by reconcileReadinessOfObjectsInDPUClusters function which returns whether
// objects in the DPU cluster are ready. The input to the function is a list of objects that exist in a particular
// cluster.
func (r *DPUServiceIPAMReconciler) getUnreadyObjects(objects []unstructured.Unstructured) ([]types.NamespacedName, error) {
	unreadyObjs := []types.NamespacedName{}
	for _, o := range objects {
		// Both IPPool and CIDRPool objects have the same status field. Unfortunately we don't have a condition ready
		// for those resources so we rely on the allocations struct to be populated to indicate that a resource is ready.
		allocations, exists, err := unstructured.NestedSlice(o.Object, "status", "allocations")
		if err != nil {
			return nil, err
		}
		if len(allocations) == 0 || !exists {
			unreadyObjs = append(unreadyObjs, types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()})
		}
	}
	return unreadyObjs, nil
}

// registerKindToWatcher registers the IPPool and CIDRPool kinds to the remote cache watcher
// so that the DPUServiceIPAM controller can watch for changes in the IPPool and CIDRPool objects
// in the DPU clusters. This is used to trigger reconciliation of the DPUServiceIPAM
// when an IPPool or CIDRPool is created, updated, or deleted in any of the DPU clusters.
func (r *DPUServiceIPAMReconciler) registerKindToWatcher(ctx context.Context, dpuCluster client.ObjectKey) error {
	if err := r.RemoteCache.Watch(ctx, dpuCluster, dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuserviceipam-watch-ippool",
		Watcher:      r.controller,
		Kind:         &nvipamv1.IPPool{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.ipPoolToDPUServiceChain),
		Predicates:   []predicate.TypedPredicate[client.Object]{predicates.TypedResourceIsChanged[client.Object]()},
	})); err != nil {
		return fmt.Errorf("error while watching IPPool in DPU cluster: %w", err)
	}
	if err := r.RemoteCache.Watch(ctx, dpuCluster, dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuserviceipam-watch-cidrpool",
		Watcher:      r.controller,
		Kind:         &nvipamv1.CIDRPool{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.cidrPoolToDPUServiceChain),
		Predicates:   []predicate.TypedPredicate[client.Object]{predicates.TypedResourceIsChanged[client.Object]()},
	})); err != nil {
		return fmt.Errorf("error while watching CIDRPool in DPU cluster: %w", err)
	}
	return nil
}

func (r *DPUServiceIPAMReconciler) ipPoolToDPUServiceChain(ctx context.Context, o client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	set, ok := o.(*nvipamv1.IPPool)
	if !ok {
		log.Error(fmt.Errorf("expected a IPPool, got %T", o), "failed to convert object")
		return nil
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: set.Namespace, Name: set.Name}}}
}

func (r *DPUServiceIPAMReconciler) cidrPoolToDPUServiceChain(ctx context.Context, o client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	set, ok := o.(*nvipamv1.CIDRPool)
	if !ok {
		log.Error(fmt.Errorf("expected a CIDRPool, got %T", o), "failed to convert object")
		return nil
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: set.Namespace, Name: set.Name}}}
}

// deleteDPUServiceOwnedPoolsOfType deletes all the objects owned by the given DPUServiceIPAM object
func deleteDPUServiceOwnedPoolsOfType(ctx context.Context, c client.Client, dpuServiceIPAM *dpuservicev1.DPUServiceIPAM, poolType string) error {
	p := &unstructured.Unstructured{}
	p.SetGroupVersionKind(nvipamv1.GroupVersion.WithKind(poolType))
	if err := c.DeleteAllOf(ctx, p, client.InNamespace(dpuServiceIPAM.Namespace), client.MatchingLabels{
		ParentDPUServiceIPAMNameLabel:      dpuServiceIPAM.Name,
		ParentDPUServiceIPAMNamespaceLabel: dpuServiceIPAM.Namespace,
	}); err != nil {
		return fmt.Errorf("error while removing all %s: %w", p.GetObjectKind().GroupVersionKind().String(), err)
	}

	return nil
}

// reconcileIPPoolMode reconciles NVIPAM IPPool object and removes any leftover CIDRPool
func reconcileIPPoolMode(ctx context.Context, c client.Client, dpuServiceIPAM *dpuservicev1.DPUServiceIPAM) error {
	pool := generateIPPool(dpuServiceIPAM)
	return reconcilePoolMode(ctx, c, dpuServiceIPAM, pool, nvipamv1.CIDRPoolKind)
}

// reconcileCIDRPoolMode reconciles NVIPAM CIDRPool object and removes any leftover IPPool
func reconcileCIDRPoolMode(ctx context.Context, c client.Client, dpuServiceIPAM *dpuservicev1.DPUServiceIPAM) error {
	pool := generateCIDRPool(dpuServiceIPAM)
	return reconcilePoolMode(ctx, c, dpuServiceIPAM, pool, nvipamv1.IPPoolKind)
}

// reconcilePoolMode is a generic helper that reconciles a pool object and removes leftover pools of a different type
func reconcilePoolMode(ctx context.Context, c client.Client, dpuServiceIPAM *dpuservicev1.DPUServiceIPAM, pool client.Object, leftoverPoolKind string) error {
	// Delete leftover pool with the default name that might exist from a previous reconcile and override the name on
	// the generated pool so that it can be patched with the new name.
	if overridenPoolName, ok := dpuServiceIPAM.Annotations[dpuservicev1.DPUServiceIPAMChildObjectNameOverrideAnnotationKey]; ok {
		// Cleanup only if the pool name is different than what the name the user asked for
		if pool.GetName() != overridenPoolName {
			if err := c.Delete(ctx, pool); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("error while deleting %s %s: %w", pool.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(pool), err)
			}
		}
		pool.SetName(overridenPoolName)
	}

	if err := c.Patch(ctx, pool, client.Apply, client.ForceOwnership, client.FieldOwner(dpuServiceIPAMControllerName)); err != nil {
		return fmt.Errorf("error while patching %s %s: %w", pool.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(pool), err)
	}

	// Delete any leftover pools of a different type in case the configuration has changed.
	if err := deleteDPUServiceOwnedPoolsOfType(ctx, c, dpuServiceIPAM, leftoverPoolKind); err != nil {
		return fmt.Errorf("error while removing potential leftover NVIPAM CRs: %w", err)
	}

	return nil
}

// generateIPPool generates an IPPool object for the given dpuServiceIPAM
func generateIPPool(dpuServiceIPAM *dpuservicev1.DPUServiceIPAM) *nvipamv1.IPPool {
	routes := make([]nvipamv1.Route, 0, len(dpuServiceIPAM.Spec.IPV4Subnet.Routes))
	for _, route := range dpuServiceIPAM.Spec.IPV4Subnet.Routes {
		routes = append(routes, nvipamv1.Route{Dst: route.Dst})
	}

	pool := &nvipamv1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dpuServiceIPAM.Name,
			Namespace:   dpuServiceIPAM.Namespace,
			Labels:      getPoolLabels(dpuServiceIPAM),
			Annotations: dpuServiceIPAM.Spec.Annotations,
		},
		Spec: nvipamv1.IPPoolSpec{
			Subnet:           dpuServiceIPAM.Spec.IPV4Subnet.Subnet,
			PerNodeBlockSize: dpuServiceIPAM.Spec.IPV4Subnet.PerNodeIPCount,
			Gateway:          dpuServiceIPAM.Spec.IPV4Subnet.Gateway,
			NodeSelector:     dpuServiceIPAM.Spec.NodeSelector,
			DefaultGateway:   dpuServiceIPAM.Spec.IPV4Subnet.DefaultGateway,
			Routes:           routes,
		},
	}
	pool.ObjectMeta.ManagedFields = nil
	pool.SetGroupVersionKind(nvipamv1.GroupVersion.WithKind(nvipamv1.IPPoolKind))
	return pool
}

// generateCIDRPool generates a CIDRPool object for the given dpuServiceIPAM
func generateCIDRPool(dpuServiceIPAM *dpuservicev1.DPUServiceIPAM) *nvipamv1.CIDRPool {
	exclusions := make([]nvipamv1.ExcludeRange, 0, len(dpuServiceIPAM.Spec.IPV4Network.Exclusions))
	for _, ip := range dpuServiceIPAM.Spec.IPV4Network.Exclusions {
		exclusions = append(exclusions, nvipamv1.ExcludeRange{StartIP: ip, EndIP: ip})
	}

	allocations := make([]nvipamv1.CIDRPoolStaticAllocation, 0, len(dpuServiceIPAM.Spec.IPV4Network.Allocations))
	for node, prefix := range dpuServiceIPAM.Spec.IPV4Network.Allocations {
		allocations = append(allocations, nvipamv1.CIDRPoolStaticAllocation{NodeName: node, Prefix: prefix})
	}

	routes := make([]nvipamv1.Route, 0, len(dpuServiceIPAM.Spec.IPV4Network.Routes))
	for _, route := range dpuServiceIPAM.Spec.IPV4Network.Routes {
		routes = append(routes, nvipamv1.Route{Dst: route.Dst})
	}

	pool := &nvipamv1.CIDRPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dpuServiceIPAM.Name,
			Namespace:   dpuServiceIPAM.Namespace,
			Labels:      getPoolLabels(dpuServiceIPAM),
			Annotations: dpuServiceIPAM.Spec.Annotations,
		},
		Spec: nvipamv1.CIDRPoolSpec{
			CIDR:                 dpuServiceIPAM.Spec.IPV4Network.Network,
			GatewayIndex:         dpuServiceIPAM.Spec.IPV4Network.GatewayIndex,
			PerNodeNetworkPrefix: dpuServiceIPAM.Spec.IPV4Network.PrefixSize,
			NodeSelector:         dpuServiceIPAM.Spec.NodeSelector,
			Exclusions:           exclusions,
			StaticAllocations:    allocations,
			DefaultGateway:       dpuServiceIPAM.Spec.IPV4Network.DefaultGateway,
			Routes:               routes,
		},
	}
	pool.ObjectMeta.ManagedFields = nil
	pool.SetGroupVersionKind(nvipamv1.GroupVersion.WithKind(nvipamv1.CIDRPoolKind))
	return pool
}

func getPoolLabels(dpuServiceIPAM *dpuservicev1.DPUServiceIPAM) map[string]string {
	commonLabels := map[string]string{
		ParentDPUServiceIPAMNameLabel:      dpuServiceIPAM.Name,
		ParentDPUServiceIPAMNamespaceLabel: dpuServiceIPAM.Namespace,
	}
	poolLabels := map[string]string{}
	maps.Copy(poolLabels, dpuServiceIPAM.Spec.Labels)
	maps.Copy(poolLabels, commonLabels)

	return poolLabels
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUServiceIPAMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.DPUServiceIPAM{}).
		Build(r)

	if err != nil {
		return err
	}

	r.controller = c
	return nil
}
