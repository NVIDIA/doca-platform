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

package controller

import (
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/features"
	"github.com/nvidia/doca-platform/internal/servicechainset/predicates"
	"github.com/nvidia/doca-platform/internal/utils"
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

	// nsiNodeLabel is stamped on every NodeServiceInterfaces object to record the node
	// it belongs to, enabling label-based lookup independent of the object name.
	nsiNodeLabel = dpuservicev1.SvcDpuGroupName + "/node"
	// nsiTypeLabel is stamped on every NodeServiceInterfaces object to record its NSI type shard.
	nsiTypeLabel = dpuservicev1.SvcDpuGroupName + "/nsi-type"

	// interfaceModeAnnotation is set once on the first reconcile to permanently commit
	// a ServiceInterfaceSet to either the legacy ServiceInterface path or the new
	// NodeServiceInterfaces path. Once written it is never changed.
	interfaceModeAnnotation = dpuservicev1.SvcDpuGroupName + "/interface-mode"
	interfaceModeLegacy     = "legacy"
	interfaceModeNSI        = "nsi"
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
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=nodeserviceinterfaces,verbs=get;list;watch;create;update;patch;delete
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
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(serviceInterfaceSet, r.Client)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")

		mode := serviceInterfaceSet.GetAnnotations()[interfaceModeAnnotation]
		var summaryErr error
		if mode == interfaceModeNSI {
			summaryErr = r.updateSummaryNSI(ctx, serviceInterfaceSet)
		} else {
			summaryErr = updateSummary(ctx, r, r.Client, dpuservicev1.ConditionServiceInterfacesReady, serviceInterfaceSet)
		}
		if summaryErr != nil {
			reterr = kerrors.NewAggregate([]error{reterr, summaryErr})
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
		mode := serviceInterfaceSet.Annotations[interfaceModeAnnotation]
		if mode == interfaceModeNSI {
			numChildren, err := r.reconcileDeleteNSI(ctx, serviceInterfaceSet)
			if err != nil {
				return ctrl.Result{}, err
			}
			if numChildren > 0 {
				conditions.AddFalse(
					serviceInterfaceSet,
					dpuservicev1.ConditionServiceInterfacesReconciled,
					conditions.ReasonAwaitingDeletion,
					conditions.ConditionMessage(fmt.Sprintf("%d NSI entries still awaiting resource release", numChildren)),
				)
				return ctrl.Result{RequeueAfter: defaultRequeueAfter}, nil
			}
			return ctrl.Result{}, nil
		}
		// Legacy path
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
	mode, err := r.ensureInterfaceMode(ctx, serviceInterfaceSet)
	if err != nil {
		conditions.AddFalse(
			serviceInterfaceSet,
			dpuservicev1.ConditionServiceInterfacesReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, err
	}

	var res ctrl.Result
	if mode == interfaceModeNSI {
		err = r.reconcileNSI(ctx, serviceInterfaceSet)
	} else {
		res, err = reconcileSet(ctx, serviceInterfaceSet, r.Client, serviceInterfaceSet.Spec.NodeSelector, r)
	}
	if err != nil {
		conditions.AddFalse(
			serviceInterfaceSet,
			dpuservicev1.ConditionServiceInterfacesReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err.Error())),
		)
		return ctrl.Result{}, err
	}
	conditions.AddTrue(serviceInterfaceSet, dpuservicev1.ConditionServiceInterfacesReconciled)
	return res, nil
}

// ensureInterfaceMode reads the interface-mode annotation, or determines and persists it on first
// reconcile. Precedence for un-annotated sets: existing ServiceInterface children → legacy;
// VPC set when NSIPathForVPC gate is disabled → legacy;
// SFC set when NSIPathForSFC gate is disabled → legacy; otherwise → NSI.
func (r *ServiceInterfaceSetReconciler) ensureInterfaceMode(ctx context.Context, set *dpuservicev1.ServiceInterfaceSet) (string, error) {
	if mode, ok := set.GetAnnotations()[interfaceModeAnnotation]; ok {
		return mode, nil
	}

	// check for pre-existing ServiceInterface children.
	existing, err := r.legacyChildMap(ctx, set)
	if err != nil {
		return "", fmt.Errorf("checking for existing ServiceInterface children: %w", err)
	}
	mode := interfaceModeNSI
	if len(existing) > 0 {
		mode = interfaceModeLegacy
	}

	isVPC := set.Spec.Template.Spec.GetVirtualNetworkName() != ""
	if isVPC && !features.Gates.Enabled(features.NSIPathForVPC) {
		mode = interfaceModeLegacy
	}
	if !isVPC && !features.Gates.Enabled(features.NSIPathForSFC) {
		mode = interfaceModeLegacy
	}

	if set.GetAnnotations() == nil {
		set.SetAnnotations(map[string]string{})
	}
	set.Annotations[interfaceModeAnnotation] = mode
	return mode, nil
}

func (r *ServiceInterfaceSetReconciler) getChildMap(ctx context.Context, set client.Object) (map[string]client.Object, error) {
	return r.legacyChildMap(ctx, set.(*dpuservicev1.ServiceInterfaceSet))
}

func (r *ServiceInterfaceSetReconciler) legacyChildMap(ctx context.Context, set *dpuservicev1.ServiceInterfaceSet) (map[string]client.Object, error) {
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

	siName := digest.GenerateName(serviceInterfaceSet.Name, nodeName)
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
		serviceInterface.Spec.Physical = serviceInterfaceSet.Spec.Template.Spec.Physical.DeepCopy()
	}
	if serviceInterfaceSet.Spec.Template.Spec.Vlan != nil {
		serviceInterface.Spec.Vlan = serviceInterfaceSet.Spec.Template.Spec.Vlan.DeepCopy()
		serviceInterface.Spec.Vlan.ParentInterfaceRef = serviceInterface.Spec.Vlan.ParentInterfaceRef + "-" + nodeName
	}
	if serviceInterfaceSet.Spec.Template.Spec.VF != nil {
		serviceInterface.Spec.VF = serviceInterfaceSet.Spec.Template.Spec.VF.DeepCopy()
		if serviceInterfaceSet.Spec.Template.Spec.VF.ParentInterfaceRef != nil {
			serviceInterface.Spec.VF.ParentInterfaceRef = ptr.To(*serviceInterfaceSet.Spec.Template.Spec.VF.ParentInterfaceRef + "-" + nodeName)
		}
	}
	if serviceInterfaceSet.Spec.Template.Spec.PF != nil {
		serviceInterface.Spec.PF = serviceInterfaceSet.Spec.Template.Spec.PF.DeepCopy()
	}
	if serviceInterfaceSet.Spec.Template.Spec.Service != nil {
		serviceInterface.Spec.Service = serviceInterfaceSet.Spec.Template.Spec.Service.DeepCopy()
	}
	if serviceInterfaceSet.Spec.Template.Spec.OVN != nil {
		serviceInterface.Spec.OVN = serviceInterfaceSet.Spec.Template.Spec.OVN.DeepCopy()
	}
	if serviceInterfaceSet.Spec.Template.Spec.Patch != nil {
		serviceInterface.Spec.Patch = serviceInterfaceSet.Spec.Template.Spec.Patch.DeepCopy()
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

// ownedNSIEntry holds an NSI object and the specific interface entry within it
// that is owned by a given ServiceInterfaceSet.
type ownedNSIEntry struct {
	NSI   *dpuservicev1.NodeServiceInterfaces
	Entry dpuservicev1.InterfaceEntry
}

// nsiFieldManager returns the SSA field manager name for a given ServiceInterfaceSet.
// It is unique per (namespace, name) pair and stable across restarts.
func nsiFieldManager(set *dpuservicev1.ServiceInterfaceSet) string {
	return "serviceinterfaceset_" + set.Namespace + "_" + set.Name
}

// nsiName returns the name for the NodeServiceInterfaces object for the given
// node and NSI type. Both inputs are included in the hash so that long node
// names that cause base truncation do not collapse different (node, type) pairs
// onto the same object name.
func nsiName(nodeName, nsiType string) string {
	return digest.GenerateName(nodeName+"-"+nsiType, nodeName, nsiType)
}

// interfaceEntryName returns the entry name for the given ServiceInterfaceSet namespace
// and name.
func interfaceEntryName(namespace, name string) string {
	return namespace + "_" + name
}

// nsiType returns the NSI type for the given set, defaulting to "sfc".
func nsiTypeForSet(set *dpuservicev1.ServiceInterfaceSet) string {
	// if the set has a virtual network, it is a VPC set
	if vn := set.Spec.Template.Spec.GetVirtualNetworkName(); vn != "" {
		// get IsolationClass name from the labels
		if provisioner, ok := set.GetLabels()[vpcv1.ProvisionerNameLabel]; ok {
			return fmt.Sprintf("%s-%s", dpuservicev1.NSITypeVPC, provisioner)
		}
		return dpuservicev1.NSITypeVPC
	}
	return dpuservicev1.NSITypeSFC
}

// reconcileNSI handles the NSI path of the main reconcile loop.
func (r *ServiceInterfaceSetReconciler) reconcileNSI(ctx context.Context, set *dpuservicev1.ServiceInterfaceSet) error {
	nsiType := nsiTypeForSet(set)

	nodeList, err := getNodeList(ctx, r.Client, set.Spec.NodeSelector)
	if err != nil {
		return fmt.Errorf("get node list: %w", err)
	}

	interfaceEntries, err := r.listNSIEntriesForServiceInterfaceSet(ctx, set)
	if err != nil {
		return fmt.Errorf("list interface entries for service interface set: %w", err)
	}
	// Build a stale map keyed by node name; entries are removed as we confirm they are still desired.
	staleByNode := make(map[string]ownedNSIEntry, len(interfaceEntries))
	for _, interfaceEntry := range interfaceEntries {
		staleByNode[interfaceEntry.NSI.Spec.Node] = interfaceEntry
	}

	for _, node := range nodeList.Items {
		delete(staleByNode, node.Name)
		if err := r.createOrUpdateEntryInNSI(ctx, set, node.Name, nsiType); err != nil {
			return fmt.Errorf("upsert NSI entry for node %s: %w", node.Name, err)
		}
	}

	for _, owned := range staleByNode {
		if !owned.Entry.Terminating {
			if err := r.markEntryTerminating(ctx, set, owned.NSI); err != nil {
				return fmt.Errorf("mark NSI entry terminating on node %s: %w", owned.NSI.Spec.Node, err)
			}
			continue
		}
		if !isEntryResourceReleased(owned.NSI, owned.Entry.Name) {
			continue
		}
		updatedNSI, err := r.removeEntryFromNSI(ctx, set, owned.NSI)
		if err != nil {
			return fmt.Errorf("remove NSI entry on node %s: %w", owned.NSI.Spec.Node, err)
		}
		if err := r.deleteNSIIfEmpty(ctx, updatedNSI); err != nil {
			return err
		}
	}

	return nil
}

// reconcileDeleteNSI handles the NSI path when the ServiceInterfaceSet is being deleted.
// Returns the count of entries still waiting for resource release.
func (r *ServiceInterfaceSetReconciler) reconcileDeleteNSI(ctx context.Context, set *dpuservicev1.ServiceInterfaceSet) (int, error) {
	ownedEntries, err := r.listNSIEntriesForServiceInterfaceSet(ctx, set)
	if err != nil {
		return 0, fmt.Errorf("list owned NSI entries: %w", err)
	}

	waiting := 0
	for _, owned := range ownedEntries {
		if !owned.Entry.Terminating {
			if err := r.markEntryTerminating(ctx, set, owned.NSI); err != nil {
				return 0, fmt.Errorf("mark entry terminating: %w", err)
			}
			waiting++
			continue
		}
		if !isEntryResourceReleased(owned.NSI, owned.Entry.Name) {
			waiting++
			continue
		}
		updatedNSI, err := r.removeEntryFromNSI(ctx, set, owned.NSI)
		if err != nil {
			return 0, fmt.Errorf("remove NSI entry: %w", err)
		}
		if err := r.deleteNSIIfEmpty(ctx, updatedNSI); err != nil {
			return 0, err
		}
	}

	if waiting == 0 {
		controllerutil.RemoveFinalizer(set, dpuservicev1.ServiceInterfaceSetFinalizer)
	}
	return waiting, nil
}

// createOrUpdateEntryInNSI creates or updates the NSI entry for this set on the given node.
// It uses server-side apply with a per-set field manager so concurrent sets can safely
// write to the same NSI object without clobbering each other's entries.
func (r *ServiceInterfaceSetReconciler) createOrUpdateEntryInNSI(ctx context.Context, set *dpuservicev1.ServiceInterfaceSet, nodeName, nType string) error {
	entryName := interfaceEntryName(set.Namespace, set.Name)
	entry := buildInterfaceEntry(set, entryName, nodeName)
	_, err := r.applyNSIEntry(ctx, set, nodeName, nType, &entry)
	return err
}

// markEntryTerminating sets Terminating=true for this set's entry in the given NSI via SSA.
func (r *ServiceInterfaceSetReconciler) markEntryTerminating(ctx context.Context, set *dpuservicev1.ServiceInterfaceSet, existingNSI *dpuservicev1.NodeServiceInterfaces) error {
	entry := buildInterfaceEntry(set, interfaceEntryName(set.Namespace, set.Name), existingNSI.Spec.Node)
	entry.Terminating = true
	_, err := r.applyNSIEntry(ctx, set, existingNSI.Spec.Node, existingNSI.Spec.Type, &entry)
	return err
}

// removeEntryFromNSI removes this set's entry from the NSI via SSA and returns
// the server-updated NSI so the caller can check whether it is now empty.
func (r *ServiceInterfaceSetReconciler) removeEntryFromNSI(ctx context.Context, set *dpuservicev1.ServiceInterfaceSet, existingNSI *dpuservicev1.NodeServiceInterfaces) (*dpuservicev1.NodeServiceInterfaces, error) {
	return r.applyNSIEntry(ctx, set, existingNSI.Spec.Node, existingNSI.Spec.Type, nil)
}

// applyNSIEntry performs a server-side apply on the NodeServiceInterfaces object for
// (nodeName, nType), setting this set's field manager to own the given entry.
// Passing nil removes the set's previously owned entry.
// It returns the server-updated NSI object.
func (r *ServiceInterfaceSetReconciler) applyNSIEntry(
	ctx context.Context,
	set *dpuservicev1.ServiceInterfaceSet,
	nodeName, nType string,
	entry *dpuservicev1.InterfaceEntry,
) (*dpuservicev1.NodeServiceInterfaces, error) {
	var interfaces []dpuservicev1.InterfaceEntry
	if entry != nil {
		interfaces = []dpuservicev1.InterfaceEntry{*entry}
	}
	nsi := &dpuservicev1.NodeServiceInterfaces{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsiName(nodeName, nType),
			Namespace: utils.NSIObjectsNamespace,
			Labels: map[string]string{
				nsiNodeLabel: nodeName,
				nsiTypeLabel: nType,
			},
		},
		Spec: dpuservicev1.NodeServiceInterfacesSpec{
			Node:       nodeName,
			Type:       nType,
			Interfaces: interfaces,
		},
	}
	nsi.SetManagedFields(nil)
	nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
	return nsi, r.Client.Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner(nsiFieldManager(set)))
}

// deleteNSIIfEmpty deletes the NSI object if the server-updated state shows no remaining entries.
func (r *ServiceInterfaceSetReconciler) deleteNSIIfEmpty(ctx context.Context, nsi *dpuservicev1.NodeServiceInterfaces) error {
	if len(nsi.Spec.Interfaces) == 0 {
		return client.IgnoreNotFound(r.Delete(ctx, nsi))
	}
	return nil
}

// listNSIEntriesForServiceInterfaceSet lists all NSI entries in the DPF-owned namespace that are owned by
// this ServiceInterfaceSet (identified by entry.Name == interfaceEntryName(ns, name)).
func (r *ServiceInterfaceSetReconciler) listNSIEntriesForServiceInterfaceSet(ctx context.Context, set *dpuservicev1.ServiceInterfaceSet) ([]ownedNSIEntry, error) {
	nsiList := &dpuservicev1.NodeServiceInterfacesList{}
	if err := r.List(ctx, nsiList, client.InNamespace(utils.NSIObjectsNamespace)); err != nil {
		return nil, err
	}

	entryName := interfaceEntryName(set.Namespace, set.Name)
	var owned []ownedNSIEntry
	for i := range nsiList.Items {
		nsi := &nsiList.Items[i]
		for _, entry := range nsi.Spec.Interfaces {
			if entry.Name == entryName {
				owned = append(owned, ownedNSIEntry{NSI: nsi, Entry: entry})
				break
			}
		}
	}
	return owned, nil
}

// isEntryResourceReleased returns true when the entry's ResourceReleased condition is True
// and was observed at the current generation of the NSI object.
func isEntryResourceReleased(nsi *dpuservicev1.NodeServiceInterfaces, entryName string) bool {
	entry := nsi.GetEntryStatus(entryName)
	if entry == nil {
		return false
	}
	return conditions.IsTrue(entry, dpuservicev1.ResourceReleased)
}

// updateSummaryNSI computes ServiceInterfaceSet readiness from per-entry NSI status conditions.
func (r *ServiceInterfaceSetReconciler) updateSummaryNSI(ctx context.Context, set *dpuservicev1.ServiceInterfaceSet) error {
	defer conditions.SetSummary(set)

	interfaceEntries, err := r.listNSIEntriesForServiceInterfaceSet(ctx, set)
	if err != nil {
		conditions.AddFalse(
			set,
			dpuservicev1.ConditionServiceInterfacesReady,
			conditions.ReasonPending,
			conditions.ConditionMessage(fmt.Sprintf("Error getting NSI entries: %s", err)),
		)
		return err
	}

	set.Status.NumberApplied = int32(len(interfaceEntries))

	var unreadyNames []string
	for _, interfaceEntry := range interfaceEntries {
		if interfaceEntry.Entry.Terminating {
			continue
		}
		entryStatus := interfaceEntry.NSI.GetEntryStatus(interfaceEntry.Entry.Name)
		entryReady := entryStatus != nil && conditions.IsTrue(entryStatus, conditions.TypeReady)
		if !entryReady {
			unreadyNames = append(unreadyNames, fmt.Sprintf("%s/%s", interfaceEntry.NSI.Name, interfaceEntry.Entry.Name))
		}
	}

	if len(unreadyNames) > 0 {
		conditions.AddFalse(
			set,
			dpuservicev1.ConditionServiceInterfacesReady,
			conditions.ReasonPending,
			conditions.ConditionMessage(conditions.ReadyConditionMessage("NSI entries not ready", unreadyNames)),
		)
		return nil
	}
	conditions.AddTrue(set, dpuservicev1.ConditionServiceInterfacesReady)
	return nil
}

// buildInterfaceEntry constructs an InterfaceEntry from a ServiceInterfaceSet template.
// The entry carries the same labels a legacy ServiceInterface would carry.
func buildInterfaceEntry(set *dpuservicev1.ServiceInterfaceSet, name, nodeName string) dpuservicev1.InterfaceEntry {
	tmpl := set.Spec.Template

	entryLabels := mergeMaps(tmpl.ObjectMeta.Labels, map[string]string{
		ServiceInterfaceSetNameLabel:      set.Name,
		ServiceInterfaceSetNamespaceLabel: set.Namespace,
	})
	if tmpl.Spec.Service != nil {
		entryLabels[ServiceInterfaceServiceIDLabel] = tmpl.Spec.Service.ServiceID
	}

	entry := dpuservicev1.InterfaceEntry{
		Name:          name,
		Labels:        entryLabels,
		Annotations:   tmpl.ObjectMeta.Annotations,
		InterfaceType: tmpl.Spec.InterfaceType,
	}

	switch tmpl.Spec.InterfaceType {
	case dpuservicev1.InterfaceTypePhysical:
		entry.Physical = &dpuservicev1.Physical{InterfaceName: tmpl.Spec.Physical.InterfaceName}
	case dpuservicev1.InterfaceTypeVLAN:
		entry.Vlan = &dpuservicev1.VLAN{
			VlanID:             tmpl.Spec.Vlan.VlanID,
			ParentInterfaceRef: tmpl.Spec.Vlan.ParentInterfaceRef + "-" + nodeName,
		}
	case dpuservicev1.InterfaceTypeVF:
		entry.VF = &dpuservicev1.VF{
			VFID:           tmpl.Spec.VF.VFID,
			PFID:           tmpl.Spec.VF.PFID,
			VirtualNetwork: tmpl.Spec.VF.VirtualNetwork,
		}
		if tmpl.Spec.VF.ParentInterfaceRef != nil {
			entry.VF.ParentInterfaceRef = ptr.To(*tmpl.Spec.VF.ParentInterfaceRef + "-" + nodeName)
		}
	case dpuservicev1.InterfaceTypePF:
		entry.PF = &dpuservicev1.PF{
			ID:             tmpl.Spec.PF.ID,
			VirtualNetwork: tmpl.Spec.PF.VirtualNetwork,
		}
	case dpuservicev1.InterfaceTypeService:
		entry.Service = &dpuservicev1.ServiceDef{
			ServiceID:      tmpl.Spec.Service.ServiceID,
			Network:        tmpl.Spec.Service.Network,
			InterfaceName:  tmpl.Spec.Service.InterfaceName,
			VirtualNetwork: tmpl.Spec.Service.VirtualNetwork,
		}
	case dpuservicev1.InterfaceTypeOVN:
		entry.OVN = &dpuservicev1.OVN{ExternalBridge: tmpl.Spec.OVN.ExternalBridge}
	case dpuservicev1.InterfaceTypePatch:
		entry.Patch = &dpuservicev1.PatchDef{
			PeerBridge:      tmpl.Spec.Patch.PeerBridge,
			PeerPatchName:   tmpl.Spec.Patch.PeerPatchName,
			PeerExternalIDs: tmpl.Spec.Patch.PeerExternalIDs,
		}
	}
	return entry
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
		Watches(&dpuservicev1.NodeServiceInterfaces{},
			handler.EnqueueRequestsFromMapFunc(r.nsiToServiceInterfaceSetReq),
			builder.WithPredicates(predicates.NSIPredicate{})).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToServiceInterfaceSetReq),
			builder.WithPredicates(predicate.LabelChangedPredicate{})).
		Complete(r)
}

// nsiToServiceInterfaceSetReq maps a NodeServiceInterfaces change to reconcile requests for
// all ServiceInterfaceSets that own entries in it, decoded from entry names.
func (r *ServiceInterfaceSetReconciler) nsiToServiceInterfaceSetReq(ctx context.Context, obj client.Object) []reconcile.Request {
	nsi, ok := obj.(*dpuservicev1.NodeServiceInterfaces)
	if !ok {
		return nil
	}

	seen := make(map[types.NamespacedName]bool)
	requests := make([]reconcile.Request, 0, len(nsi.Spec.Interfaces))
	for _, entry := range nsi.Spec.Interfaces {
		ns, name := entry.GetNamespacedName()
		if ns == "" || name == "" {
			continue
		}
		nn := types.NamespacedName{Namespace: ns, Name: name}
		if seen[nn] {
			continue
		}
		seen[nn] = true
		requests = append(requests, reconcile.Request{NamespacedName: nn})
	}
	return requests
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
