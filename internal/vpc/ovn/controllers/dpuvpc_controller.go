/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

//nolint:dupl
package controllers

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ipmanager"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology"

	"github.com/fluxcd/pkg/runtime/patch"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	dpfpredicates "github.com/nvidia/doca-platform/pkg/utils/predicates"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs/finalizers,verbs=update
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=isolationclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch

const (
	dpuVPCControllerName = "dpuvpccontroller"
	dpuVPCFinalizer      = "ovn.vpc.dpu.nvidia.com/vpc-protection"
	ovnProvisionerName   = "ovn.vpc.dpu.nvidia.com"
)

var (
	dpuVPCMergePatchOptions = []client.PatchOption{client.FieldOwner(dpuVPCControllerName)}
)

// DPUVPCReconciler reconciles a DPUVPC objects
type DPUVPCReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	IPManager   ipmanager.IPManager
	RemoteCache *dpucluster.RemoteCache
	CleanupGate CleanupGate

	selfController ctrlcontroller.Controller

	// used for dependency injection
	topologyManagerFromIsolationClassFn func(ctx context.Context, isoCls *vpcv1.IsolationClass) (topology.Manager, error)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUVPCReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if r.topologyManagerFromIsolationClassFn == nil {
		r.topologyManagerFromIsolationClassFn = TopologyManagerFromIsolationClass
	}

	c, err := ctrlbuilder.ControllerManagedBy(mgr).
		Named(dpuVPCControllerName).
		For(&vpcv1.DPUVPC{}).
		// Watch DPUVirtualNetwork and trigger reconcile for its VPC
		Watches(&vpcv1.DPUVirtualNetwork{}, handler.EnqueueRequestsFromMapFunc(r.requestsForDPUVirtualNetwork)).
		// Watch IsolationClass create events and trigger reconcile for all related VPCs
		Watches(
			&vpcv1.IsolationClass{},
			handler.EnqueueRequestsFromMapFunc(r.isolationClassToDPUVPCRequests),
			ctrlbuilder.WithPredicates(dpfpredicates.PredicateFuncsByEventTypes(event.CreateEvent{}))).
		Build(r)
	r.selfController = c
	return err
}

// Reconcile reconciles a DPUVPC object
func (r *DPUVPCReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	defer r.CleanupGate.LockForReconcile()()

	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling")

	// get dpuVPC object
	vpc := &vpcv1.DPUVPC{}
	if err := r.Client.Get(ctx, req.NamespacedName, vpc); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(vpc, r.Client)
	conditions.EnsureConditions(vpc, vpcv1.DPUVPCConditions)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		reqLog.Info("Patching")
		conditions.SetSummary(vpc)
		if err := patcher.Patch(ctx, vpc,
			patch.WithFieldOwner(dpuVPCControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(vpcv1.DPUVPCConditions)},
		); kerrors.FilterOut(err, apierrors.IsNotFound) != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
		r.IPManager.LogAllocationStats(reqLog.WithName("IPManager Stats"))
	}()

	// get dpucluster clients
	dpuClusterClients, err := r.RemoteCache.ListClients()
	if err != nil {
		reqLog.Error(err, "Failed to get DPU cluster clients")
		return ctrl.Result{}, err
	}

	// initialize ip manager if needed
	if !r.IPManager.Initialized() {
		if err := r.initializeIPManager(ctx, dpuClusterClients); err != nil {
			conditions.AddFalse(vpc,
				vpcv1.ConditionPreReqsReady,
				conditions.ReasonError,
				conditions.ConditionMessage(fmt.Sprintf("Error occurred: failed to initialize IP manager. %s", err)))
			return ctrl.Result{}, fmt.Errorf("failed to initialize IP manager. %w", err)
		}
	}

	// get isolationClass
	isoCls, err := IsolationClassForVPC(ctx, r.Client, vpc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// we will requeue when the isolation class is created
			reqLog.Info("Waiting for isolation class to be created", "isolationClass", vpc.Spec.IsolationClassName)
			conditions.AddFalse(vpc,
				vpcv1.ConditionPreReqsReady,
				conditions.ReasonPending,
				conditions.ConditionMessage("Waiting for isolation class to be created"))
			return ctrl.Result{}, nil
		}

		conditions.AddFalse(vpc,
			vpcv1.ConditionPreReqsReady,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: failed to get isolation class. %s", err)))
		return ctrl.Result{}, err
	}

	reqLog.Info("Isolation class for VPC", "isolationClass", isoCls.Name, "provisioner", isoCls.Spec.Provisioner)

	// get virtual networks for VPC
	vnsForVpc, err := VirtualNetworksForVPC(ctx, r.Client, vpc)
	if err != nil {
		conditions.AddFalse(vpc,
			vpcv1.ConditionDPUVPCReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: failed to list virtual networks for VPC. %s", err)))
		return ctrl.Result{}, fmt.Errorf("failed to list virtual networks for VPC %s: %w", client.ObjectKeyFromObject(vpc), err)
	}

	// Handle deletion reconciliation loop.
	if !vpc.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, isoCls, vpc, vnsForVpc, dpuClusterClients)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(vpc, dpuVPCFinalizer) {
		controllerutil.AddFinalizer(vpc, dpuVPCFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, isoCls, vpc, vnsForVpc, dpuClusterClients)
}

func (r *DPUVPCReconciler) initializeIPManager(ctx context.Context, dpuClusterClients []client.Client) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("initializeIPManager")

	// get all VPCs that have the finalizer set
	vpcl := &vpcv1.DPUVPCList{}
	if err := r.Client.List(ctx, vpcl); err != nil {
		return fmt.Errorf("failed to list DPUVPCs: %w", err)
	}

	vpcs := []vpcv1.DPUVPC{}
	for _, vpc := range vpcl.Items {
		if !controllerutil.ContainsFinalizer(&vpc, dpuVPCFinalizer) {
			continue
		}
		vpcs = append(vpcs, vpc)
	}

	// get all virtual networks that have the finalizer set
	vnl := &vpcv1.DPUVirtualNetworkList{}
	if err := r.Client.List(ctx, vnl); err != nil {
		return fmt.Errorf("failed to list DPUVirtualNetworks: %w", err)
	}

	vns := []vpcv1.DPUVirtualNetwork{}
	for _, vn := range vnl.Items {
		if !controllerutil.ContainsFinalizer(&vn, dpuVirtualNetworkFinalizer) {
			continue
		}
		vns = append(vns, vn)
	}

	// get all DPU nodes
	dpuNodes := []corev1.Node{}
	for _, dpuClient := range dpuClusterClients {
		nl := &corev1.NodeList{}
		if err := dpuClient.List(ctx, nl); err != nil {
			return fmt.Errorf("failed to list nodes in dpucluster: %w", err)
		}
		dpuNodes = append(dpuNodes, nl.Items...)
	}

	return r.IPManager.Initialize(vpcs, vns, dpuNodes)
}

// reconcile reconciles the DPUVPC object
//
//nolint:unparam
func (r *DPUVPCReconciler) reconcile(ctx context.Context,
	isoCls *vpcv1.IsolationClass,
	vpc *vpcv1.DPUVPC,
	vnsForVpc []*vpcv1.DPUVirtualNetwork,
	dpuClusterClients []client.Client,
) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile internal")

	// reconcile ips for vpc
	if err := r.reconcileIPsForVPC(ctx, vpc); err != nil {
		conditions.AddFalse(vpc,
			vpcv1.ConditionDPUVPCReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: failed to reconcile IPs for VPC. %s", err)))
		return ctrl.Result{}, fmt.Errorf("failed to reconcile IPs for VPC: %w", err)
	}

	if !r.dpuVirtualNetworksReady(vnsForVpc) {
		reqLog.Info("Waiting for virtual networks of VPC to be ready", "vpc", client.ObjectKeyFromObject(vpc).String())
		conditions.AddFalse(vpc,
			vpcv1.ConditionPreReqsReady,
			conditions.ReasonPending,
			conditions.ConditionMessage("Waiting for virtual networks of VPC to be ready"))
		// we will requeue when the virtual networks are updated
		return ctrl.Result{}, nil
	}

	// reconcile dpu nodes
	nodesForVpc, err := r.reconcileDPUNodes(ctx, vpc, dpuClusterClients)
	if err != nil {
		conditions.AddFalse(vpc,
			vpcv1.ConditionDPUNodesReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: failed to reconcile DPU nodes. %s", err)))
		return ctrl.Result{}, err
	}

	// remove deleted nodes from VPC IP allocation
	r.removeStaleVPCIPsForDeletedNodes(ctx, vpc, nodesForVpc)

	conditions.AddTrue(vpc, vpcv1.ConditionDPUNodesReconciled)

	// check node annotations pre-requisites are met
	for _, node := range nodesForVpc {
		if !r.nodePreReqsMet(node) {
			conditions.AddFalse(vpc,
				vpcv1.ConditionPreReqsReady,
				conditions.ReasonPending,
				conditions.ConditionMessage("Some nodes dont not have all needed annotations set by the vpc ovn node agent"))
			reqLog.Info("node does not have all needed annotations set by vpc ovn node agent", "node", node.Name)
			// we will requeue when the node is updated with the needed annotations
			return ctrl.Result{}, nil
		}
	}
	conditions.AddTrue(vpc, vpcv1.ConditionPreReqsReady)

	// reconcile ovn logical topology
	if err := r.reconcileOVNTopology(ctx, isoCls, vpc, vnsForVpc, nodesForVpc); err != nil {
		conditions.AddFalse(vpc,
			vpcv1.ConditionTopologyReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: failed to reconcile OVN logical topology. %s", err)))
		return ctrl.Result{}, fmt.Errorf("failed to reconcile OVN logical topology for VPC: %w", err)
	}
	conditions.AddTrue(vpc, vpcv1.ConditionTopologyReconciled)

	// update virtual networks in status
	reqLog.Info("Reconcile virtual networks in status")
	vnStatus := make([]vpcv1.VirtualNetworkStatus, 0, len(vnsForVpc))
	for _, vn := range vnsForVpc {
		vnStatus = append(vnStatus, vpcv1.VirtualNetworkStatus{Name: vn.Name})
	}
	// sort by name for a stable list in status
	slices.SortFunc(vnStatus, func(a, b vpcv1.VirtualNetworkStatus) int { return cmp.Compare(a.Name, b.Name) })
	vpc.Status.VirtualNetworks = vnStatus

	conditions.AddTrue(vpc, vpcv1.ConditionDPUVPCReconciled)
	return ctrl.Result{}, nil
}

func (r *DPUVPCReconciler) removeStaleVPCIPsForDeletedNodes(ctx context.Context, vpc *vpcv1.DPUVPC, nodesForVpc []*corev1.Node) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Remove stale VPC IPs for deleted nodes", "vpc", client.ObjectKeyFromObject(vpc).String())

	ipa := r.IPManager.GetNetworkIPAllocator(ipmanager.ObjToID(vpc), ipmanager.VPCClusterNetworkIPV4)
	if ipa == nil {
		return
	}

	currentAllocs := ipa.ListAllocationIDs()
	currentNodeAllocs := sets.New[string]()
	// Note: this is not perfect, but will get the job done.
	for _, alloc := range currentAllocs {
		if strings.HasPrefix(alloc, "v1.Node_") {
			currentNodeAllocs.Insert(alloc)
		}
	}

	expectedAllocs := sets.New[string]()
	for _, node := range nodesForVpc {
		expectedAllocs.Insert(ipmanager.ObjToID(node))
	}

	staleAllocs := currentNodeAllocs.Difference(expectedAllocs)
	for staleAlloc := range staleAllocs {
		reqLog.Info("Removing stale VPC IP allocation", "alloc", staleAlloc)
		ipa.Deallocate(staleAlloc)
	}
}

// reconcileOVNTopology reconciles the OVN logical topology for the VPC
func (r *DPUVPCReconciler) reconcileOVNTopology(
	ctx context.Context,
	isoCls *vpcv1.IsolationClass,
	vpc *vpcv1.DPUVPC,
	vnsForVpc []*vpcv1.DPUVirtualNetwork,
	nodesForVpc []*corev1.Node,
) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile OVN logical topology for VPC", "vpc", client.ObjectKeyFromObject(vpc).String())

	// create manager and apply topology
	tm, err := r.topologyManagerFromIsolationClassFn(ctx, isoCls)
	if err != nil {
		return fmt.Errorf("failed to get ovn topology manager. %w", err)
	}

	return tm.ApplyTopology(ctx, vpc, vnsForVpc, nodesForVpc)
}

// reconcileIPsForVPC allocates vpc router (central router) IPs for the VPC and its virtual networks
// returns error if occurred
func (r *DPUVPCReconciler) reconcileIPsForVPC(ctx context.Context, vpc *vpcv1.DPUVPC) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile IPs for VPC")

	// add IP allocator for VPC (if it doesnt exist) and the ipv4 cluster network
	r.IPManager.AddVPC(ipmanager.ObjToID(vpc))
	err := r.IPManager.AddNetwork(ipmanager.ObjToID(vpc), ipmanager.VPCClusterNetworkIPV4, ipmanager.VPCClusterCIDRIPV4)
	if err != nil {
		return fmt.Errorf("failed to add ipv4 cluster network to IP manager: %w", err)
	}

	alloc := r.IPManager.GetNetworkIPAllocator(ipmanager.ObjToID(vpc), ipmanager.VPCClusterNetworkIPV4)
	if alloc == nil {
		return fmt.Errorf("IP allocator for VPC(%s) not found", client.ObjectKeyFromObject(vpc).String())
	}

	// allocate vpc router IP for VPC
	if !common.HasLRPAddressAnnotation(vpc.Annotations) {
		if vpc.Annotations == nil {
			vpc.Annotations = make(map[string]string)
		}

		ipa, err := alloc.AllocateGateway(ipmanager.ObjToID(vpc))
		if err != nil {
			return fmt.Errorf("failed to allocate router IP for VPC: %w", err)
		}
		err = common.LRPAddressToAnnotation(common.LRPAddress{IPV4: ipa.Address.String()}, vpc.Annotations)
		if err != nil {
			return fmt.Errorf("failed to store allocated router IP for VPC in annotation: %w", err)
		}
		// NOTE(adrianc): vpc annotation is updated when Reconcile call returns
	}

	return nil
}

// dpuVirtualNetworksReady returns true if all specified DPUVirtualNetworks are ready
func (r *DPUVPCReconciler) dpuVirtualNetworksReady(vns []*vpcv1.DPUVirtualNetwork) bool {
	for _, vn := range vns {
		if !conditions.IsTrue(vn, conditions.TypeReady) {
			return false
		}
	}
	return true
}

// reconcileDelete reconciles the deletion of a DPUVPC object
//
//nolint:unparam
func (r *DPUVPCReconciler) reconcileDelete(
	ctx context.Context,
	isoCls *vpcv1.IsolationClass,
	vpc *vpcv1.DPUVPC,
	vnsForVpc []*vpcv1.DPUVirtualNetwork,
	dpuClusterClients []client.Client,
) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile delete")

	if len(vnsForVpc) > 0 {
		reqLog.Info("Virtual networks still present, waiting for DPUVirtualNetwork deletion")
		conditions.AddFalse(vpc,
			vpcv1.ConditionDPUVPCReconciled,
			conditions.ReasonAwaitingDeletion,
			conditions.ConditionMessage("Waiting for DPUVirtualNetwork deletion"))
		return ctrl.Result{}, nil
	}

	// teardown ovn logical topology
	if err := r.reconcileOVNTopologyOnDelete(ctx, isoCls, vpc); err != nil {
		conditions.AddFalse(vpc,
			vpcv1.ConditionTopologyReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: failed to teardown OVN logical topology. %s", err)))
		return ctrl.Result{}, fmt.Errorf("failed to teardown OVN logical topology. %w", err)
	}
	conditions.AddTrue(vpc, vpcv1.ConditionTopologyReconciled)

	// remove IP allocator for VPC
	r.IPManager.RemoveVPC(ipmanager.ObjToID(vpc))

	// remove labels and annotations from dpu nodes
	if err := r.reconcileDPUNodesOnDelete(ctx, vpc, dpuClusterClients); err != nil {
		conditions.AddFalse(vpc,
			vpcv1.ConditionDPUNodesReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: failed to reconcile DPU Node labels and annotations for VPC on delete. %s", err)))
		return ctrl.Result{}, fmt.Errorf("failed to reconcile DPU Node labels and annotations for VPC on delete. %w", err)
	}
	conditions.AddTrue(vpc, vpcv1.ConditionDPUNodesReconciled)

	// Remove finalizer if not set.
	if controllerutil.ContainsFinalizer(vpc, dpuVPCFinalizer) {
		controllerutil.RemoveFinalizer(vpc, dpuVPCFinalizer)
	}

	conditions.AddTrue(vpc, vpcv1.ConditionDPUVPCReconciled)
	return ctrl.Result{}, nil
}

// reconcileOVNTopologyOnDelete reconciles OVN toplogy on delete of the VPC. returns error if occurred
func (r *DPUVPCReconciler) reconcileOVNTopologyOnDelete(ctx context.Context, isoCls *vpcv1.IsolationClass, vpc *vpcv1.DPUVPC) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile OVN logical topology on delete for VPC", "vpc", client.ObjectKeyFromObject(vpc).String())

	// create manager and remove topology
	tm, err := r.topologyManagerFromIsolationClassFn(ctx, isoCls)
	if err != nil {
		return fmt.Errorf("failed to get ovn topology manager. %w", err)
	}

	reqLog.Info("Removing topology for VPC", "vpc", client.ObjectKeyFromObject(vpc).String())
	return tm.RemoveTopology(ctx, vpc)
}

// reconcileDPUNodesOnDelete reconciles the DPU Node labels and annotations based on VPC membership when VPC object is being deleted. returns error if occurred.
func (r *DPUVPCReconciler) reconcileDPUNodesOnDelete(ctx context.Context, vpc *vpcv1.DPUVPC, dpuClusterClients []client.Client) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile DPU Nodes for VPC on delete")

	for _, dpuClient := range dpuClusterClients {
		nl := &corev1.NodeList{}
		if err := dpuClient.List(ctx, nl, client.MatchingLabels{common.OVNVPCNodeLabelKey: common.ObjectToLabelValue(vpc)}); err != nil {
			return fmt.Errorf("failed to list nodes in dpucluster. %w", err)
		}

		var errs []error
		for _, node := range nl.Items {
			err := r.reconcileSingleDPUNodeOnDelete(ctx, dpuClient, &node)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to reconcile dpu node for node %s: %w", node.Name, err))
			}
		}

		if len(errs) > 0 {
			return kerrors.NewAggregate(errs)
		}
	}
	return nil
}

// reconcileSingleDPUNodeOnDelete reconciles the labels on a single DPU node based on the VPC membership when VPC object is being deleted.
// returns error if occurred.
func (r *DPUVPCReconciler) reconcileSingleDPUNodeOnDelete(ctx context.Context, dpuClient client.Client, node *corev1.Node) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile DPU Node for VPC on delete", "node", node.Name)

	origNode := node.DeepCopy()
	delete(node.Labels, common.OVNVPCNodeLabelKey)
	delete(node.Annotations, common.LRPAddressesAnnotationKey)

	if err := dpuClient.Patch(ctx, node, client.MergeFrom(origNode), dpuVPCMergePatchOptions...); err != nil {
		return fmt.Errorf("failed to remove VPC label and annotation from node %s. %w", node.Name, err)
	}
	return nil
}

// reconcileDPUNodes reconciles DPU nodes based on the VPC membership. returns nodes of the VPC and error if occurred.
func (r *DPUVPCReconciler) reconcileDPUNodes(ctx context.Context, vpc *vpcv1.DPUVPC, dpuClusterClients []client.Client) ([]*corev1.Node, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile DPU Nodes for VPC")

	var nodesInVPC []*corev1.Node
	for _, dpuClient := range dpuClusterClients {
		// list nodes according to VPC node selector
		nodesMatchingSelector := &corev1.NodeList{}
		s, err := metav1.LabelSelectorAsSelector(vpc.Spec.NodeSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to convert VPC %s node selector to Selector. %w", client.ObjectKeyFromObject(vpc), err)
		}
		if err := dpuClient.List(ctx, nodesMatchingSelector, client.MatchingLabelsSelector{Selector: s}); err != nil {
			return nil, fmt.Errorf("failed to list nodes in dpucluster. %w", err)
		}

		// list nodes accodring to vpc label
		nodesWithVPCLabel := &corev1.NodeList{}
		if err := dpuClient.List(ctx, nodesWithVPCLabel, client.MatchingLabels{common.OVNVPCNodeLabelKey: common.ObjectToLabelValue(vpc)}); err != nil {
			return nil, fmt.Errorf("failed to list nodes in dpucluster. %w", err)
		}

		// merge and reconcile each node, aggregate errors to make as much progress as possible.
		var errs []error
		nodes := MergeNodeSlices(nodesMatchingSelector.Items, nodesWithVPCLabel.Items)
		for _, node := range nodes {
			inVPC, err := r.reconcileSingleDPUNode(ctx, vpc, dpuClient, &node)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to reconcile dpu node labels for node %s: %w", node.Name, err))
			}

			if inVPC {
				nodesInVPC = append(nodesInVPC, &node)
			}
		}

		if len(errs) > 0 {
			return nil, kerrors.NewAggregate(errs)
		}
	}
	return nodesInVPC, nil
}

// reconcileSingleDPUNode reconciles a single DPU node based on the VPC membership
// adds vpc label membership and allocates IP address in VPC cluster network if node is part of the VPC
// returns error if occurred
func (r *DPUVPCReconciler) reconcileSingleDPUNode(ctx context.Context, vpc *vpcv1.DPUVPC, dpucluster client.Client, node *corev1.Node) (bool, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile single DPU Node for VPC", "node", node.Name)

	// ensure node belongs to a single VPC
	vpcl := &vpcv1.DPUVPCList{}
	err := r.Client.List(ctx, vpcl)
	if err != nil {
		return false, fmt.Errorf("failed to list VPCs: %w", err)
	}
	vpcOfNode, err := VPCForNode(node, vpcl.Items)
	if err != nil {
		// node belongs to more than one vpc
		return false, fmt.Errorf("failed to get VPC for node. %w", err)
	}

	inVPC := vpcOfNode != nil && vpcOfNode.UID == vpc.UID
	vpcNsLabelValue := common.ObjectToLabelValue(vpc)

	alloc := r.IPManager.GetNetworkIPAllocator(ipmanager.ObjToID(vpc), ipmanager.VPCClusterNetworkIPV4)
	if alloc == nil {
		return false, fmt.Errorf("IP allocator for VPC(%s) not found", client.ObjectKeyFromObject(vpc).String())
	}

	origNode := node.DeepCopy()
	if !inVPC {
		// no match, if node has vpc label pointing to the provided vpc name, remove it
		if node.Labels[common.OVNVPCNodeLabelKey] == vpcNsLabelValue {
			// remove VPC IP allocation for node
			alloc.Deallocate(ipmanager.ObjToID(node))
			// remove lrp annotation for node
			delete(node.Annotations, common.LRPAddressesAnnotationKey)
			// remove vpc label from node
			delete(node.Labels, common.OVNVPCNodeLabelKey)

			reqLog.Info("Removing VPC label and annotation from node", "node", node.Name)
			if err := dpucluster.Patch(ctx, node, client.MergeFrom(origNode), dpuVPCMergePatchOptions...); err != nil {
				return false, fmt.Errorf("failed to remove node label. failed to patch node %s. %w", node.Name, err)
			}
		}
		return inVPC, nil
	}

	// allocate VPC IP for node and store in node annotation
	// node may change VPC, thus we need to allocate IP even if annotation is present
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	ipa, err := alloc.Allocate(ipmanager.ObjToID(node), nil)
	if err != nil {
		return false, fmt.Errorf("failed to allocate VPC IP for node %s. %w", node.Name, err)
	}

	if err := common.LRPAddressToAnnotation(common.LRPAddress{IPV4: ipa.Address.String()}, node.Annotations); err != nil {
		return false, fmt.Errorf("failed to store allocated VPC IP for node %s in annotation. %w", node.Name, err)
	}

	// set vpc label on node
	if node.Labels[common.OVNVPCNodeLabelKey] != vpcNsLabelValue {
		reqLog.Info("Add VPC label to node", "node", node.Name)

		if node.Labels == nil {
			node.Labels = make(map[string]string)
		}

		node.Labels[common.OVNVPCNodeLabelKey] = vpcNsLabelValue
	}

	if err := dpucluster.Patch(ctx, node, client.MergeFrom(origNode), dpuVPCMergePatchOptions...); err != nil {
		return false, fmt.Errorf("failed to patch node %s with annotation. %w", node.Name, err)
	}

	return inVPC, nil
}

// nodePreReqsMet returns true if the node has all needed annotations set by the vpc ovn node agent
func (r *DPUVPCReconciler) nodePreReqsMet(node *corev1.Node) bool {
	if node.Annotations[common.OVNVtepIPAnnotationKey] == "" {
		return false
	}
	if node.Annotations[common.OVNGatewayConfigAnnotationKey] == "" {
		return false
	}
	if node.Annotations[common.OVNChassisIDAnnotationKey] == "" {
		return false
	}

	return true
}

// requestsForDPUVirtualNetwork returns requests for the DPUVPC that is referenced in the DPUVirtualNetwork
func (r *DPUVPCReconciler) requestsForDPUVirtualNetwork(_ context.Context, o client.Object) []ctrl.Request {
	vn, ok := o.(*vpcv1.DPUVirtualNetwork)
	if !ok {
		return nil
	}

	return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: vn.Spec.VPCName, Namespace: vn.Namespace}}}
}

func (r *DPUVPCReconciler) isolationClassToDPUVPCRequests(c context.Context, o client.Object) []ctrl.Request {
	reqLog := ctrllog.FromContext(c)

	ic, ok := o.(*vpcv1.IsolationClass)
	if !ok {
		reqLog.Error(nil, "failed to convert object to IsolationClass")
		return nil
	}

	vpcList := &vpcv1.DPUVPCList{}
	if err := r.Client.List(c, vpcList); err != nil {
		reqLog.Error(err, "failed to list VPCs")
		return nil
	}

	var requests []ctrl.Request
	for _, vpc := range vpcList.Items {
		if vpc.Spec.IsolationClassName == ic.Name {
			requests = append(requests, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&vpc)})
		}
	}
	return requests
}

// RegisterWatchesForCluster registers watches for DPUVPC controller in a given cluster
// this is called by the dpucluster controller when a change in dpucluster is detected (create/update)
func (r *DPUVPCReconciler) RegisterWatchesForCluster(ctx context.Context, rc *dpucluster.RemoteCache, cluster *provisioningv1.DPUCluster) error {
	if err := r.watchNodes(ctx, rc, cluster); err != nil {
		return err
	}
	return nil
}

// watchNodes registers watches for nodes in the (dpu)cluster
func (r *DPUVPCReconciler) watchNodes(ctx context.Context, rc *dpucluster.RemoteCache, cluster *provisioningv1.DPUCluster) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Registering watch for Nodes", "controller", dpuVPCControllerName, "cluster", cluster.Name)

	// Watch expects unique name
	watcherName := fmt.Sprintf("%s-%s", dpuVPCControllerName, "node")
	return rc.Watch(ctx, client.ObjectKeyFromObject(cluster), dpucluster.NewWatcher(
		dpucluster.TypedWatcherOptions[client.Object, ctrl.Request]{
			Name:         watcherName,
			Kind:         &corev1.Node{},
			EventHandler: &nodeEventHandler{client: r.Client},
			Predicates:   []predicate.Predicate{predicate.Or(predicate.LabelChangedPredicate{}, predicate.AnnotationChangedPredicate{})},
			Watcher:      r.selfController,
		}))
}

// nodeEventHandler is a handler for node events
type nodeEventHandler struct {
	client client.Client
}

func (h *nodeEventHandler) handleNodeEventHelper(ctx context.Context, old *corev1.Node, new *corev1.Node, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqLog := ctrllog.FromContext(ctx)

	currentVPC, err := h.getCurrentVPCForNode(ctx, new)
	if err != nil {
		reqLog.Error(err, "Failed to get current VPC for node", "name", new.Name)
		return
	}

	requests, err := h.requestsIfNodeChangedVPC(new, currentVPC)
	if err != nil {
		reqLog.Error(err, "Failed to get requests for node", "name", new.Name)
		return
	}

	if old != nil {
		requests = append(requests, h.requestsIfNodeAgentGatewayAnnotationsChanged(old, new, currentVPC)...)
	}

	h.enqueue(requests, q)

}

func (h *nodeEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqLog := ctrllog.FromContext(ctx)

	node, ok := e.Object.(*corev1.Node)
	if !ok {
		reqLog.Error(fmt.Errorf("event expected a Node but got a %T", e.Object), "Failed to convert object")
		return
	}

	h.handleNodeEventHelper(ctx, nil, node, q)
}

func (h *nodeEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqLog := ctrllog.FromContext(ctx)

	nodeOld, ok := e.ObjectOld.(*corev1.Node)
	if !ok {
		reqLog.Error(fmt.Errorf("event expected a Node but got a %T", e.ObjectOld), "Failed to convert object")
		return
	}

	nodeNew, ok := e.ObjectNew.(*corev1.Node)
	if !ok {
		reqLog.Error(fmt.Errorf("event expected a Node but got a %T", e.ObjectNew), "Failed to convert object")
		return
	}

	h.handleNodeEventHelper(ctx, nodeOld, nodeNew, q)
}

func (h *nodeEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqLog := ctrllog.FromContext(ctx)

	node, ok := e.Object.(*corev1.Node)
	if !ok {
		reqLog.Error(fmt.Errorf("event expected a Node but got a %T", e.Object), "Failed to convert object")
		return
	}

	currentVPC, err := h.getCurrentVPCForNode(ctx, node)
	if err != nil {
		reqLog.Error(err, "Failed to get current VPC for node", "name", node.Name)
		return
	}

	if currentVPC != nil {
		h.enqueue(
			[]ctrl.Request{{NamespacedName: client.ObjectKeyFromObject(currentVPC)}},
			q,
		)
	}
}

func (h *nodeEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqLog := ctrllog.FromContext(ctx)

	node, ok := e.Object.(*corev1.Node)
	if !ok {
		reqLog.Error(fmt.Errorf("event expected a Node but got a %T", e.Object), "Failed to convert object")
		return
	}

	h.handleNodeEventHelper(ctx, nil, node, q)
}

// enqueue enqueues requests into q after deduplication
func (h *nodeEventHandler) enqueue(requests []ctrl.Request, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqs := make(map[ctrl.Request]struct{})

	// deduplicate requests
	for _, req := range requests {
		reqs[req] = struct{}{}
	}

	for req := range reqs {
		q.Add(req)
	}
}

// requestsIfNodeChangedVPC returns requests for the VPC that the node belongs to if the node has changed VPC.
func (h *nodeEventHandler) requestsIfNodeChangedVPC(node *corev1.Node, currentVPC *vpcv1.DPUVPC) ([]ctrl.Request, error) {
	// There are 5 options here:
	// 1. node is not part of a VPC, and does not have VPC label - no requests
	// 2. node is not part of a VPC and has vpc label - request for the VPC in the label (which will remove the label)
	// 3. node is part of a VPC and does not have vpc label - request for the VPC
	// 4. node is part of a VPC and has vpc label of different VPC - request for the VPC in the label (which will remove the label)
	// 5. node is part of a VPC and has the vpc label of that VPC - no requests

	vpcFromLabel := node.Labels[common.OVNVPCNodeLabelKey]
	var vpcFromLabelObjKey client.ObjectKey

	if vpcFromLabel != "" {
		var err error
		vpcFromLabelObjKey, err = common.ObjectKeyFromLabelValue(node.Labels[common.OVNVPCNodeLabelKey])
		if err != nil {
			return nil, fmt.Errorf("failed to get object key from label %s=%s", common.OVNVPCNodeLabelKey, node.Labels[common.OVNVPCNodeLabelKey])
		}
	}

	switch {
	case currentVPC == nil && vpcFromLabel == "":
		return nil, nil
	case currentVPC == nil && vpcFromLabel != "":
		return []ctrl.Request{{NamespacedName: vpcFromLabelObjKey}}, nil
	case currentVPC != nil && vpcFromLabel == "":
		return []ctrl.Request{{NamespacedName: client.ObjectKeyFromObject(currentVPC)}}, nil
	case currentVPC != nil && vpcFromLabel != common.ObjectToLabelValue(currentVPC):
		return []ctrl.Request{{NamespacedName: vpcFromLabelObjKey}}, nil
	}

	// at this point: currentVPC != nil && ObjectToLabelValue(vpc) == vpcFromLabel
	return nil, nil
}

// requestsIfNodeAgentGatewayAnnotationsChanged returns requests for the VPC that the node belongs to if the node agent gateway annotations have changed
func (h *nodeEventHandler) requestsIfNodeAgentGatewayAnnotationsChanged(old *corev1.Node, new *corev1.Node, currentVPC *vpcv1.DPUVPC) []ctrl.Request {
	if old.Annotations[common.OVNChassisIDAnnotationKey] == new.Annotations[common.OVNChassisIDAnnotationKey] &&
		old.Annotations[common.OVNGatewayConfigAnnotationKey] == new.Annotations[common.OVNGatewayConfigAnnotationKey] {
		return nil
	}

	if currentVPC == nil {
		return nil
	}

	return []ctrl.Request{{NamespacedName: client.ObjectKeyFromObject(currentVPC)}}
}

// getCurrentVPCForNode returns the VPC for a node, or nil if the node is not part of a VPC
func (h *nodeEventHandler) getCurrentVPCForNode(ctx context.Context, node *corev1.Node) (*vpcv1.DPUVPC, error) {
	// get VPC for node
	vpcs := vpcv1.DPUVPCList{}
	if err := h.client.List(ctx, &vpcs); err != nil {
		return nil, fmt.Errorf("failed to list VPCs: %w", err)
	}

	vpc, err := VPCForNode(node, vpcs.Items)
	if err != nil {
		return nil, fmt.Errorf("failed to get VPC for node %s", node.Name)
	}

	return vpc, nil
}
