/*
COPYRIGHT 2024 NVIDIA

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
	"maps"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ serviceSetReconciler = &ServiceChainSetReconciler{}

// ServiceChainSetReconciler reconciles a ServiceChainSet object
type ServiceChainSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	ServiceChainSetNameLabel      = dpuservicev1.SvcDpuGroupName + "/servicechainset-name"
	ServiceChainSetNamespaceLabel = dpuservicev1.SvcDpuGroupName + "/servicechainset-namespace"
	ServiceChainNodeNameLabel     = dpuservicev1.SvcDpuGroupName + "/nodeName"
	serviceChainSetControllerName = "service-chain-set-controller"
)

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechainsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechainsets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechainsets/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

//nolint:dupl
func (r *ServiceChainSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")

	serviceChainSet := &dpuservicev1.ServiceChainSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, serviceChainSet); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	patcher := patch.NewSerialPatcher(serviceChainSet, r.Client)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")

		if err := updateSummary(ctx, r, r.Client, dpuservicev1.ConditionServiceChainsReady, serviceChainSet); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
		if err := patcher.Patch(ctx, serviceChainSet,
			patch.WithFieldOwner(serviceChainSetControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.ServiceChainSetConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	conditions.EnsureConditions(serviceChainSet, dpuservicev1.ServiceChainSetConditions)

	if !serviceChainSet.ObjectMeta.DeletionTimestamp.IsZero() {
		return reconcileDelete(ctx, serviceChainSet, r.Client, r, dpuservicev1.ServiceChainSetFinalizer)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(serviceChainSet, dpuservicev1.ServiceChainSetFinalizer) {
		log.Info("Adding finalizer")
		controllerutil.AddFinalizer(serviceChainSet, dpuservicev1.ServiceChainSetFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, serviceChainSet)
}

func (r *ServiceChainSetReconciler) reconcile(ctx context.Context, serviceChainSet *dpuservicev1.ServiceChainSet) (ctrl.Result, error) {
	res, err := reconcileSet(ctx, serviceChainSet, r.Client, serviceChainSet.Spec.NodeSelector, r)
	if err != nil {
		conditions.AddFalse(
			serviceChainSet,
			dpuservicev1.ConditionServiceChainsReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, err
	}
	conditions.AddTrue(
		serviceChainSet,
		dpuservicev1.ConditionServiceChainsReconciled,
	)

	return res, nil
}

func (r *ServiceChainSetReconciler) getChildMap(ctx context.Context, set client.Object) (map[string]client.Object, error) {
	serviceChainMap := make(map[string]client.Object)
	serviceChainList := &dpuservicev1.ServiceChainList{}
	if err := r.List(ctx, serviceChainList,
		client.MatchingLabels{
			ServiceChainSetNameLabel:      set.GetName(),
			ServiceChainSetNamespaceLabel: set.GetNamespace(),
		},
		client.InNamespace(set.GetNamespace()),
	); err != nil {
		return serviceChainMap, err
	}
	for _, serviceChain := range serviceChainList.Items {
		serviceChainMap[*serviceChain.Spec.Node] = &serviceChain
	}
	return serviceChainMap, nil
}

func (r *ServiceChainSetReconciler) createOrUpdateChild(ctx context.Context, set client.Object, nodeName string) error {
	log := ctrllog.FromContext(ctx)

	serviceChainSet := set.(*dpuservicev1.ServiceChainSet)
	labels := map[string]string{
		ServiceChainSetNameLabel:      serviceChainSet.Name,
		ServiceChainSetNamespaceLabel: serviceChainSet.Namespace,
		ServiceChainNodeNameLabel:     nodeName,
	}
	maps.Copy(labels, serviceChainSet.Spec.Template.ObjectMeta.Labels)

	switches := make([]dpuservicev1.Switch, len(serviceChainSet.Spec.Template.Spec.Switches))
	for i, serviceChainSwitch := range serviceChainSet.Spec.Template.Spec.Switches {
		switches[i] = *serviceChainSwitch.DeepCopy()
	}

	owner := metav1.NewControllerRef(serviceChainSet, dpuservicev1.GroupVersion.WithKind("ServiceChainSet"))
	serviceChain := &dpuservicev1.ServiceChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-%s", serviceChainSet.Name, nodeName),
			Namespace:       serviceChainSet.Namespace,
			Labels:          labels,
			Annotations:     serviceChainSet.Annotations,
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: dpuservicev1.ServiceChainSpec{
			Node:     ptr.To(nodeName),
			Switches: switches,
		},
	}
	serviceChain.SetManagedFields(nil)
	serviceChain.SetGroupVersionKind(dpuservicev1.GroupVersion.WithKind("ServiceChain"))
	if err := r.Client.Patch(ctx, serviceChain, client.Apply, client.ForceOwnership, client.FieldOwner(serviceChainSetControllerName)); err != nil {
		return err
	}

	log.Info("ServiceChain is created", "ServiceChain", serviceChain)
	return nil
}

func (r *ServiceChainSetReconciler) getObjectsInDPUCluster(ctx context.Context, k8sClient client.Client, dpuObject client.Object) ([]unstructured.Unstructured, error) {
	serviceChainSet := &unstructured.Unstructured{}
	serviceChainSet.SetGroupVersionKind(dpuservicev1.ServiceChainSetGroupVersionKind)
	key := client.ObjectKey{Namespace: dpuObject.GetNamespace(), Name: dpuObject.GetName()}
	err := k8sClient.Get(ctx, key, serviceChainSet)
	if err != nil {
		return nil, fmt.Errorf("error while getting %s %s: %w", serviceChainSet.GetObjectKind().GroupVersionKind().String(), key.String(), err)
	}

	return []unstructured.Unstructured{*serviceChainSet}, nil
}

func (r *ServiceChainSetReconciler) getUnreadyObjects(objects []unstructured.Unstructured) ([]types.NamespacedName, error) {
	unreadyObjs := []types.NamespacedName{}
	for _, o := range objects {
		// TODO: Convert to ServiceChain when we implement status for this controller
		_, _, err := unstructured.NestedSlice(o.Object, "status", "conditions")
		if err != nil {
			return nil, err
		}
		// TODO: Check on condition ready when we implement status for this controller
	}
	return unreadyObjs, nil
}

func (r *ServiceChainSetReconciler) setReadyStatus(serviceSet client.Object, numberApplied, numberReady int32) {
	obj := serviceSet.(*dpuservicev1.ServiceChainSet)
	// TODO add NumberReady state as soon as we have the state of a ServiceChain
	obj.Status.NumberApplied = numberApplied
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceChainSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.ServiceChainSet{}).
		Owns(&dpuservicev1.ServiceChain{}).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToServiceChainSetReq),
			builder.WithPredicates(predicate.LabelChangedPredicate{})).
		Complete(r)
}

func (r *ServiceChainSetReconciler) nodeToServiceChainSetReq(ctx context.Context, resource client.Object) []reconcile.Request {
	serviceChainSetList := &dpuservicev1.ServiceChainSetList{}
	if err := r.List(ctx, serviceChainSetList); err != nil {
		return nil
	}

	requests := []reconcile.Request{}
	for _, item := range serviceChainSetList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		})
	}
	return requests
}
