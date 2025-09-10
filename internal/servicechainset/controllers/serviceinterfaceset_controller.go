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

const (
	ServiceInterfaceSetNameLabel      = dpuservicev1.SvcDpuGroupName + "/serviceinterfaceset-name"
	ServiceInterfaceSetNamespaceLabel = dpuservicev1.SvcDpuGroupName + "/serviceinterfaceset-namespace"
	ServiceInterfaceServiceIDLabel    = dpuservicev1.SvcDpuGroupName + "/service-id"
	serviceInterfaceSetControllerName = "service-interface-set-controller"
	nodeFieldKey                      = "spec.node"
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
		numChildren, err := reconcileDelete(ctx, serviceInterfaceSet, r.Client, r, dpuservicev1.ServiceInterfaceSetFinalizer)
		if err != nil {
			return ctrl.Result{}, err
		}
		if numChildren > 0 {
			conditions.AddFalse(
				serviceInterfaceSet,
				dpuservicev1.ConditionServiceInterfacesReconciled,
				conditions.ReasonAwaitingDeletion,
				conditions.ConditionMessage(fmt.Sprintf("%d child `ServiceInterface`s still exist in DPU cluster", numChildren)),
			)
			log.Info("child `ServiceInterface`s still exist, requeueing", "children", numChildren)
			return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
		}
		return ctrl.Result{}, nil
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

func getServiceInterfaceForNode(ctx context.Context, cl client.Client, set *dpuservicev1.ServiceInterfaceSet, nodeName string) (*dpuservicev1.ServiceInterface, error) { //nolint:dupl
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
	if err := cl.List(ctx, serviceInterfaceList,
		client.MatchingLabels(map[string]string{
			ServiceInterfaceSetNameLabel:      set.GetName(),
			ServiceInterfaceSetNamespaceLabel: set.GetNamespace(),
		}),
		client.MatchingFields{nodeFieldKey: nodeName},
	); err != nil {
		return nil, err
	}
	switch len(serviceInterfaceList.Items) {
	case 0:
		return nil, nil
	case 1:
		return &serviceInterfaceList.Items[0], nil
	default:
		return nil, fmt.Errorf("more than one ServiceInterface found for ServiceInterfaceSet %s and node: %s", set.GetName(), nodeName)
	}
}

func (r *ServiceInterfaceSetReconciler) createOrUpdateChild(ctx context.Context, set client.Object, nodeName string) error {
	log := ctrllog.FromContext(ctx)

	serviceInterfaceSet := set.(*dpuservicev1.ServiceInterfaceSet)
	defaultLabels := map[string]string{
		ServiceInterfaceSetNameLabel:      serviceInterfaceSet.Name,
		ServiceInterfaceSetNamespaceLabel: serviceInterfaceSet.Namespace,
	}
	if serviceInterfaceSet.Spec.Template.Spec.Service != nil {
		defaultLabels[ServiceInterfaceServiceIDLabel] = serviceInterfaceSet.Spec.Template.Spec.Service.ServiceID
	}

	si, err := getServiceInterfaceForNode(ctx, r.Client, serviceInterfaceSet, nodeName)
	if err != nil {
		return err
	}

	siName := names.SimpleNameGenerator.GenerateName(serviceInterfaceSet.Name)
	siLabels := map[string]string{}
	siAnnotations := map[string]string{}
	if si != nil {
		siName = si.Name
		siLabels = si.Labels
		siAnnotations = si.Annotations
	}
	// merge labels and annotation from ServiceInterface if exists
	finalLabels := mergeMaps(siLabels, defaultLabels, serviceInterfaceSet.Spec.Template.ObjectMeta.Labels)
	finalAnnotations := mergeMaps(siAnnotations, serviceInterfaceSet.Spec.Template.ObjectMeta.Annotations)

	owner := metav1.NewControllerRef(serviceInterfaceSet, dpuservicev1.GroupVersion.WithKind("ServiceInterfaceSet"))

	serviceInterface := &dpuservicev1.ServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:            siName,
			Namespace:       serviceInterfaceSet.Namespace,
			Labels:          finalLabels,
			Annotations:     finalAnnotations,
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
			VFID:           serviceInterfaceSet.Spec.Template.Spec.VF.VFID,
			PFID:           serviceInterfaceSet.Spec.Template.Spec.VF.PFID,
			VirtualNetwork: serviceInterfaceSet.Spec.Template.Spec.VF.VirtualNetwork,
		}
		if serviceInterfaceSet.Spec.Template.Spec.VF.ParentInterfaceRef != nil {
			serviceInterface.Spec.VF.ParentInterfaceRef = ptr.To(*serviceInterfaceSet.Spec.Template.Spec.VF.ParentInterfaceRef + "-" + nodeName)
		}
	}
	if serviceInterfaceSet.Spec.Template.Spec.PF != nil {
		serviceInterface.Spec.PF = &dpuservicev1.PF{
			ID:             serviceInterfaceSet.Spec.Template.Spec.PF.ID,
			VirtualNetwork: serviceInterfaceSet.Spec.Template.Spec.PF.VirtualNetwork,
		}
	}
	if serviceInterfaceSet.Spec.Template.Spec.Service != nil {
		serviceInterface.Spec.Service = &dpuservicev1.ServiceDef{
			ServiceID:      serviceInterfaceSet.Spec.Template.Spec.Service.ServiceID,
			Network:        serviceInterfaceSet.Spec.Template.Spec.Service.Network,
			InterfaceName:  serviceInterfaceSet.Spec.Template.Spec.Service.InterfaceName,
			VirtualNetwork: serviceInterfaceSet.Spec.Template.Spec.Service.VirtualNetwork,
		}
	}
	if serviceInterfaceSet.Spec.Template.Spec.OVN != nil {
		serviceInterface.Spec.OVN = &dpuservicev1.OVN{
			ExternalBridge: serviceInterfaceSet.Spec.Template.Spec.OVN.ExternalBridge,
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
	serviceInterfaceList := &unstructured.UnstructuredList{}
	serviceInterfaceList.SetGroupVersionKind(dpuservicev1.ServiceInterfaceGroupVersionKind)
	key := client.ObjectKey{Namespace: dpuObject.GetNamespace(), Name: dpuObject.GetName()}
	err := k8sClient.List(ctx, serviceInterfaceList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(map[string]string{
			ServiceInterfaceSetNameLabel:      dpuObject.GetName(),
			ServiceInterfaceSetNamespaceLabel: dpuObject.GetNamespace(),
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("error while getting %s %s: %w", serviceInterfaceList.GetObjectKind().GroupVersionKind().String(), key.String(), err)
	}

	return serviceInterfaceList.Items, nil
}

func (r *ServiceInterfaceSetReconciler) getUnreadyObjects(objects []unstructured.Unstructured) ([]types.NamespacedName, error) {
	unreadyObjs := []types.NamespacedName{}
	for _, o := range objects {
		serviceInterface := &dpuservicev1.ServiceInterface{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &serviceInterface)
		if err != nil {
			return nil, fmt.Errorf("convert unstructured to %T: %w", serviceInterface, err)
		}
		if meta.IsStatusConditionTrue(serviceInterface.GetConditions(), string(conditions.TypeReady)) {
			continue
		}
		unreadyObjs = append(unreadyObjs, types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()})
	}
	return unreadyObjs, nil
}

func (r *ServiceInterfaceSetReconciler) setReadyStatus(serviceSet client.Object, numberApplied, numberReady int32) {
	obj := serviceSet.(*dpuservicev1.ServiceInterfaceSet)
	// TODO add NumberReady state as soon as we have the state of a ServiceInterface
	obj.Status.NumberApplied = numberApplied
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceInterfaceSetReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&dpuservicev1.ServiceInterface{},
		nodeFieldKey,
		func(o client.Object) []string {
			if si, ok := o.(*dpuservicev1.ServiceInterface); ok && si.Spec.Node != nil {
				return []string{*si.Spec.Node}
			}
			return nil
		},
	); err != nil {
		return err
	}

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
