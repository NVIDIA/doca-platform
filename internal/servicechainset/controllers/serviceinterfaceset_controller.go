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

const (
	ServiceInterfaceSetNameLabel      = dpuservicev1.SvcDpuGroupName + "/serviceinterfaceset-name"
	ServiceInterfaceSetNamespaceLabel = dpuservicev1.SvcDpuGroupName + "/serviceinterfaceset-namespace"
	ServiceInterfaceNodeNameLabel     = dpuservicev1.SvcDpuGroupName + "/nodeName"
	ServiceInterfaceServiceIDLabel    = dpuservicev1.SvcDpuGroupName + "/service-id"
	serviceInterfaceSetControllerName = "service-interface-set-controller"
)

var _ serviceSetReconciler = &ServiceInterfaceSetReconciler{}

// ServiceInterfaceSetReconciler reconciles a ServiceInterfaceSet object
type ServiceInterfaceSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfacesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfacesets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfacesets/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
//nolint:dupl
func (r *ServiceInterfaceSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")
	serviceInterfaceSet := &dpuservicev1.ServiceInterfaceSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, serviceInterfaceSet); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(serviceInterfaceSet, r.Client)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")

		if err := updateSummary(ctx, r, r.Client, dpuservicev1.ConditionServiceInterfacesReady, serviceInterfaceSet); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
		if err := patcher.Patch(ctx, serviceInterfaceSet,
			patch.WithFieldOwner(serviceInterfaceSetControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.ServiceInterfaceSetConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	conditions.EnsureConditions(serviceInterfaceSet, dpuservicev1.ServiceInterfaceSetConditions)

	if !serviceInterfaceSet.ObjectMeta.DeletionTimestamp.IsZero() {
		return reconcileDelete(ctx, serviceInterfaceSet, r.Client, r, dpuservicev1.ServiceInterfaceSetFinalizer)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(serviceInterfaceSet, dpuservicev1.ServiceInterfaceSetFinalizer) {
		log.Info("Adding finalizer")
		controllerutil.AddFinalizer(serviceInterfaceSet, dpuservicev1.ServiceInterfaceSetFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, serviceInterfaceSet)
}

func (r *ServiceInterfaceSetReconciler) reconcile(ctx context.Context, serviceInterfaceSet *dpuservicev1.ServiceInterfaceSet) (ctrl.Result, error) {
	res, err := reconcileSet(ctx, serviceInterfaceSet, r.Client, serviceInterfaceSet.Spec.NodeSelector, r)
	if err != nil {
		conditions.AddFalse(
			serviceInterfaceSet,
			dpuservicev1.ConditionServiceInterfacesReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, err
	}
	conditions.AddTrue(
		serviceInterfaceSet,
		dpuservicev1.ConditionServiceInterfacesReconciled,
	)

	return res, nil
}

func (r *ServiceInterfaceSetReconciler) getChildMap(ctx context.Context, set client.Object) (map[string]client.Object, error) {
	serviceInterfaceMap := make(map[string]client.Object)
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
	if err := r.List(ctx, serviceInterfaceList,
		client.MatchingLabels{
			ServiceInterfaceSetNameLabel:      set.GetName(),
			ServiceInterfaceSetNamespaceLabel: set.GetNamespace(),
		},
		client.InNamespace(set.GetNamespace()),
	); err != nil {
		return serviceInterfaceMap, err
	}
	for _, serviceInterface := range serviceInterfaceList.Items {
		serviceInterfaceMap[*serviceInterface.Spec.Node] = &serviceInterface
	}
	return serviceInterfaceMap, nil
}

func (r *ServiceInterfaceSetReconciler) createOrUpdateChild(ctx context.Context, set client.Object, nodeName string) error {
	log := ctrllog.FromContext(ctx)

	serviceInterfaceSet := set.(*dpuservicev1.ServiceInterfaceSet)
	labels := map[string]string{
		ServiceInterfaceSetNameLabel:      serviceInterfaceSet.Name,
		ServiceInterfaceSetNamespaceLabel: serviceInterfaceSet.Namespace,
		ServiceInterfaceNodeNameLabel:     nodeName,
	}
	if serviceInterfaceSet.Spec.Template.Spec.Service != nil {
		labels[ServiceInterfaceServiceIDLabel] = serviceInterfaceSet.Spec.Template.Spec.Service.ServiceID
	}
	maps.Copy(labels, serviceInterfaceSet.Spec.Template.ObjectMeta.Labels)

	owner := metav1.NewControllerRef(serviceInterfaceSet, dpuservicev1.GroupVersion.WithKind("ServiceInterfaceSet"))

	serviceInterface := &dpuservicev1.ServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-%s", serviceInterfaceSet.Name, nodeName),
			Namespace:       serviceInterfaceSet.Namespace,
			Labels:          labels,
			Annotations:     serviceInterfaceSet.Annotations,
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: dpuservicev1.ServiceInterfaceSpec{
			Node:          ptr.To(nodeName),
			InterfaceType: serviceInterfaceSet.Spec.Template.Spec.InterfaceType,
		},
	}

	if serviceInterfaceSet.Spec.Template.Spec.Physical != nil {
		serviceInterface.Spec.Physical = &dpuservicev1.Physical{
			InterfaceName: serviceInterfaceSet.Spec.Template.Spec.Physical.InterfaceName,
		}
	}
	if serviceInterfaceSet.Spec.Template.Spec.Vlan != nil {
		serviceInterface.Spec.Vlan = &dpuservicev1.VLAN{
			VlanID:             serviceInterfaceSet.Spec.Template.Spec.Vlan.VlanID,
			ParentInterfaceRef: serviceInterfaceSet.Spec.Template.Spec.Vlan.ParentInterfaceRef + "-" + nodeName,
		}
	}
	if serviceInterfaceSet.Spec.Template.Spec.VF != nil {
		serviceInterface.Spec.VF = &dpuservicev1.VF{
			VFID:               serviceInterfaceSet.Spec.Template.Spec.VF.VFID,
			PFID:               serviceInterfaceSet.Spec.Template.Spec.VF.PFID,
			ParentInterfaceRef: serviceInterfaceSet.Spec.Template.Spec.VF.ParentInterfaceRef + "-" + nodeName,
		}
	}
	if serviceInterfaceSet.Spec.Template.Spec.PF != nil {
		serviceInterface.Spec.PF = &dpuservicev1.PF{
			ID: serviceInterfaceSet.Spec.Template.Spec.PF.ID,
		}
	}
	if serviceInterfaceSet.Spec.Template.Spec.Service != nil {
		serviceInterface.Spec.Service = &dpuservicev1.ServiceDef{
			ServiceID:     serviceInterfaceSet.Spec.Template.Spec.Service.ServiceID,
			Network:       serviceInterfaceSet.Spec.Template.Spec.Service.Network,
			InterfaceName: serviceInterfaceSet.Spec.Template.Spec.Service.InterfaceName,
		}
	}
	serviceInterface.SetManagedFields(nil)
	serviceInterface.SetGroupVersionKind(dpuservicev1.GroupVersion.WithKind("ServiceInterface"))
	if err := r.Client.Patch(ctx, serviceInterface, client.Apply, client.ForceOwnership, client.FieldOwner(serviceInterfaceSetControllerName)); err != nil {
		return err
	}

	log.Info("ServiceInterface is updated/created", "ServiceInterface", serviceInterface)
	return nil
}

func (r *ServiceInterfaceSetReconciler) getObjectsInDPUCluster(ctx context.Context, k8sClient client.Client, dpuObject client.Object) ([]unstructured.Unstructured, error) {
	serviceInterfaceSet := &unstructured.Unstructured{}
	serviceInterfaceSet.SetGroupVersionKind(dpuservicev1.ServiceInterfaceSetGroupVersionKind)
	key := client.ObjectKey{Namespace: dpuObject.GetNamespace(), Name: dpuObject.GetName()}
	err := k8sClient.Get(ctx, key, serviceInterfaceSet)
	if err != nil {
		return nil, fmt.Errorf("error while getting %s %s: %w", serviceInterfaceSet.GetObjectKind().GroupVersionKind().String(), key.String(), err)
	}

	return []unstructured.Unstructured{*serviceInterfaceSet}, nil
}

func (r *ServiceInterfaceSetReconciler) getUnreadyObjects(objects []unstructured.Unstructured) ([]types.NamespacedName, error) {
	unreadyObjs := []types.NamespacedName{}
	for _, o := range objects {
		// TODO: Convert to ServiceInterface when we implement status for this controller
		_, _, err := unstructured.NestedSlice(o.Object, "status", "conditions")
		if err != nil {
			return nil, err
		}
		// TODO: Check on condition ready when we implement status for this controller
	}
	return unreadyObjs, nil
}

func (r *ServiceInterfaceSetReconciler) setReadyStatus(serviceSet client.Object, numberApplied, numberReady int32) {
	obj := serviceSet.(*dpuservicev1.ServiceInterfaceSet)
	// TODO add NumberReady state as soon as we have the state of a ServiceInterface
	obj.Status.NumberApplied = numberApplied
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceInterfaceSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.ServiceInterfaceSet{}).
		Owns(&dpuservicev1.ServiceInterface{}).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToServiceInterfaceSetReq),
			builder.WithPredicates(predicate.LabelChangedPredicate{})).
		Complete(r)
}

func (r *ServiceInterfaceSetReconciler) nodeToServiceInterfaceSetReq(ctx context.Context, resource client.Object) []reconcile.Request {
	serviceInterfaceSetList := &dpuservicev1.ServiceInterfaceSetList{}
	if err := r.List(ctx, serviceInterfaceSetList); err != nil {
		return nil
	}

	requests := []reconcile.Request{}
	for _, item := range serviceInterfaceSetList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			}})
	}
	return requests
}
