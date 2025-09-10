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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/storage/names"
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

var defaultRequeueAfter = 30 * time.Second

var _ serviceSetReconciler = &ServiceChainSetReconciler{}

// ServiceChainSetReconciler reconciles a ServiceChainSet object
type ServiceChainSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	ServiceChainSetNameLabel      = dpuservicev1.SvcDpuGroupName + "/servicechainset-name"
	ServiceChainSetNamespaceLabel = dpuservicev1.SvcDpuGroupName + "/servicechainset-namespace"
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
		numChildren, err := reconcileDelete(ctx, serviceChainSet, r.Client, r, dpuservicev1.ServiceChainSetFinalizer)
		if err != nil {
			return ctrl.Result{}, err
		}
		if numChildren > 0 {
			conditions.AddFalse(
				serviceChainSet,
				dpuservicev1.ConditionServiceChainsReconciled,
				conditions.ReasonAwaitingDeletion,
				conditions.ConditionMessage(fmt.Sprintf("%d child `ServiceChain`s still exist in DPU cluster", numChildren)),
			)
			log.Info("child `ServiceChain`s still exist, requeueing", "children", numChildren)
			return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
		}
		return ctrl.Result{}, nil
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

func getServiceChainForNode(ctx context.Context, cl client.Client, set *dpuservicev1.ServiceChainSet, nodeName string) (*dpuservicev1.ServiceChain, error) { //nolint:dupl
	serviceChainList := &dpuservicev1.ServiceChainList{}
	if err := cl.List(ctx, serviceChainList,
		client.MatchingLabels(map[string]string{
			ServiceChainSetNameLabel:      set.GetName(),
			ServiceChainSetNamespaceLabel: set.GetNamespace(),
		}),
		client.InNamespace(set.GetNamespace()),
		client.MatchingFields{nodeFieldKey: nodeName},
	); err != nil {
		return nil, err
	}
	switch len(serviceChainList.Items) {
	case 0:
		return nil, nil
	case 1:
		return &serviceChainList.Items[0], nil
	default:
		return nil, fmt.Errorf("more than one ServiceChain found for ServiceChainSet %s and node: %s", set.GetName(), nodeName)
	}
}

func (r *ServiceChainSetReconciler) createOrUpdateChild(ctx context.Context, set client.Object, nodeName string) error {
	log := ctrllog.FromContext(ctx)

	serviceChainSet := set.(*dpuservicev1.ServiceChainSet)
	defaultLabels := map[string]string{
		ServiceChainSetNameLabel:      serviceChainSet.Name,
		ServiceChainSetNamespaceLabel: serviceChainSet.Namespace,
	}

	sc, err := getServiceChainForNode(ctx, r.Client, serviceChainSet, nodeName)
	if err != nil {
		return err
	}

	scName := names.SimpleNameGenerator.GenerateName(serviceChainSet.Name)
	scLabels := map[string]string{}
	scAnnotations := map[string]string{}
	if sc != nil {
		scName = sc.Name
		scLabels = sc.Labels
		scAnnotations = sc.Annotations
	}
	// merge labels and annotation from ServiceChain if it exists.
	finalLabels := mergeMaps(scLabels, defaultLabels, serviceChainSet.Spec.Template.ObjectMeta.Labels)
	finalAnnotations := mergeMaps(scAnnotations, serviceChainSet.Spec.Template.ObjectMeta.Annotations)

	switches := make([]dpuservicev1.Switch, len(serviceChainSet.Spec.Template.Spec.Switches))
	for i, serviceChainSwitch := range serviceChainSet.Spec.Template.Spec.Switches {
		switches[i] = *serviceChainSwitch.DeepCopy()
	}

	owner := metav1.NewControllerRef(serviceChainSet, dpuservicev1.GroupVersion.WithKind("ServiceChainSet"))
	serviceChain := &dpuservicev1.ServiceChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:            scName,
			Namespace:       serviceChainSet.Namespace,
			Labels:          finalLabels,
			Annotations:     finalAnnotations,
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
	serviceChainList := &unstructured.UnstructuredList{}
	serviceChainList.SetGroupVersionKind(dpuservicev1.ServiceChainGroupVersionKind)
	err := k8sClient.List(ctx, serviceChainList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(map[string]string{
			ServiceChainSetNameLabel:      dpuObject.GetName(),
			ServiceChainSetNamespaceLabel: dpuObject.GetNamespace(),
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("error while getting %s %s: %w", serviceChainList.GetObjectKind().GroupVersionKind().String(),
			types.NamespacedName{Name: dpuObject.GetName(), Namespace: dpuObject.GetNamespace()}.String(), err)
	}

	return serviceChainList.Items, nil
}

func (r *ServiceChainSetReconciler) getUnreadyObjects(objects []unstructured.Unstructured) ([]types.NamespacedName, error) {
	unreadyObjs := []types.NamespacedName{}
	for _, o := range objects {
		serviceChain := &dpuservicev1.ServiceChain{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &serviceChain)
		if err != nil {
			return nil, fmt.Errorf("convert unstructured to %T: %w", serviceChain, err)
		}
		if meta.IsStatusConditionTrue(serviceChain.GetConditions(), string(conditions.TypeReady)) {
			continue
		}
		unreadyObjs = append(unreadyObjs, types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()})
	}
	return unreadyObjs, nil
}

func (r *ServiceChainSetReconciler) setReadyStatus(serviceSet client.Object, numberApplied, numberReady int32) {
	obj := serviceSet.(*dpuservicev1.ServiceChainSet)
	// TODO add NumberReady state as soon as we have the state of a ServiceChain
	obj.Status.NumberApplied = numberApplied
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceChainSetReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&dpuservicev1.ServiceChain{},
		nodeFieldKey,
		func(o client.Object) []string {
			if sc, ok := o.(*dpuservicev1.ServiceChain); ok && sc.Spec.Node != nil {
				return []string{*sc.Spec.Node}
			}
			return nil
		},
	); err != nil {
		return err
	}

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
