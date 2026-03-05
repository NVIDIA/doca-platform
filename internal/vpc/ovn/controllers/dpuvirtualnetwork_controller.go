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
	"context"
	"fmt"
	"time"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ipmanager"

	"github.com/fluxcd/pkg/runtime/patch"
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	dpfpredicates "github.com/nvidia/doca-platform/pkg/utils/predicates"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks/finalizers,verbs=update
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=isolationclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuserviceinterfaces,verbs=get;list;watch

const (
	dpuVirtualNetworkControllerName = "dpuvirtualnetworkcontroller"
	dpuVirtualNetworkFinalizer      = "ovn.vpc.dpu.nvidia.com/virtualnetwork-protection"
)

// DPUVirtualNetworkReconciler reconciles a DPUVirtualNetwork objects
type DPUVirtualNetworkReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	IPManager ipmanager.IPManager
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUVirtualNetworkReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&vpcv1.DPUVirtualNetwork{},
		"spec.vpcName",
		func(o client.Object) []string {
			vpc := o.(*vpcv1.DPUVirtualNetwork)
			return []string{vpc.Spec.VPCName}
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(dpuVirtualNetworkControllerName).
		For(&vpcv1.DPUVirtualNetwork{}).
		// Watch DPUVPC create and update events and trigger reconcile of all related virtual networks
		Watches(
			&vpcv1.DPUVPC{},
			handler.EnqueueRequestsFromMapFunc(r.dpuVPCToDPUVirtualNetworkRequests),
			builder.WithPredicates(dpfpredicates.PredicateFuncsByEventTypes(event.CreateEvent{}, event.DeleteEvent{}))).
		// Watch DPUServiceInterface delete events and trigger reconcile for the related DPUVirtualNetwork if its deleting
		// so that finalizer may be removed.
		Watches(
			&dpuservicev1.DPUServiceInterface{},
			handler.EnqueueRequestsFromMapFunc(r.dpuServiceInterfaceToDPUVirtualNetworkRequests),
			builder.WithPredicates(dpfpredicates.PredicateFuncsByEventTypes(event.DeleteEvent{}))).
		Complete(r)
}

// Reconcile reconciles a DPUVirtualNetwork object
func (r *DPUVirtualNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	reqLog := ctrllog.FromContext(ctx)

	reqLog.Info("Reconciling")
	dpuVnet := &vpcv1.DPUVirtualNetwork{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuVnet); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuVnet, r.Client)

	conditions.EnsureConditions(dpuVnet, vpcv1.DPUVirtualNetworkConditions)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		reqLog.Info("Patching")
		conditions.SetSummary(dpuVnet)
		if err := patcher.Patch(ctx, dpuVnet,
			patch.WithFieldOwner(dpuVirtualNetworkControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(vpcv1.DPUVirtualNetworkConditions)},
		); kerrors.FilterOut(err, apierrors.IsNotFound) != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	dpuVpc := &vpcv1.DPUVPC{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: dpuVnet.Namespace, Name: dpuVnet.Spec.VPCName}, dpuVpc); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early. a request will be enqueued when the VPC is created.
			conditions.AddFalse(dpuVnet, vpcv1.ConditionPreReqsReady, conditions.ReasonPending, "VPC not found")
			return ctrl.Result{}, nil
		}
		conditions.AddFalse(dpuVnet,
			vpcv1.ConditionPreReqsReady,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err)))
		return ctrl.Result{}, fmt.Errorf("failed to get VPC %s for virtual network %s: %w", dpuVnet.Spec.VPCName, client.ObjectKeyFromObject(dpuVnet).String(), err)

	}

	if !r.IPManager.Initialized() {
		reqLog.Info("IP manager not initialized")
		conditions.AddFalse(dpuVnet, vpcv1.ConditionPreReqsReady, conditions.ReasonPending, "IP manager not initialized")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	conditions.AddTrue(dpuVnet, vpcv1.ConditionPreReqsReady)

	// Handle deletion reconciliation loop.
	if !dpuVnet.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuVnet, dpuVpc)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuVnet, dpuVirtualNetworkFinalizer) {
		controllerutil.AddFinalizer(dpuVnet, dpuVirtualNetworkFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuVnet, dpuVpc)
}

//nolint:unparam
func (r *DPUVirtualNetworkReconciler) reconcile(ctx context.Context, dpuVnet *vpcv1.DPUVirtualNetwork, dpuVpc *vpcv1.DPUVPC) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile internal")

	// allocate vpc router IPs for virtual network
	if !common.HasLRPAddressAnnotation(dpuVnet.Annotations) {
		alloc := r.IPManager.GetNetworkIPAllocator(ipmanager.ObjToID(dpuVpc), ipmanager.VPCClusterNetworkIPV4)
		if alloc == nil {
			conditions.AddFalse(dpuVnet,
				vpcv1.ConditionDPUVirtualNetworkReconciled,
				conditions.ReasonError,
				"IP allocator not found for VPC")
			return ctrl.Result{}, fmt.Errorf("IP allocator not found for VPC %s", client.ObjectKeyFromObject(dpuVpc).String())
		}

		if dpuVnet.Annotations == nil {
			dpuVnet.Annotations = make(map[string]string)
		}
		// allocate router IP for virtual network
		ipa, err := alloc.Allocate(ipmanager.ObjToID(dpuVnet), nil)
		if err != nil {
			conditions.AddFalse(dpuVnet,
				vpcv1.ConditionDPUVirtualNetworkReconciled,
				conditions.ReasonError,
				conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err)))
			return ctrl.Result{}, fmt.Errorf("failed to allocate router IP for virtual network %s: %w", client.ObjectKeyFromObject(dpuVnet).String(), err)
		}
		// save allocated IP in annotation (object patched in main reconcile function)
		err = common.LRPAddressToAnnotation(common.LRPAddress{IPV4: ipa.Address.String()}, dpuVnet.Annotations)
		if err != nil {
			err = fmt.Errorf("failed to store allocated router IP for virtual network in annotation %s: %w",
				client.ObjectKeyFromObject(dpuVnet).String(), err)
			conditions.AddFalse(dpuVnet,
				vpcv1.ConditionDPUVirtualNetworkReconciled,
				conditions.ReasonError,
				conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", err)))
			return ctrl.Result{}, err
		}
	}

	conditions.AddTrue(dpuVnet, vpcv1.ConditionDPUVirtualNetworkReconciled)
	return ctrl.Result{}, nil
}

//nolint:unparam
func (r *DPUVirtualNetworkReconciler) reconcileDelete(ctx context.Context, dpuVnet *vpcv1.DPUVirtualNetwork, dpuVpc *vpcv1.DPUVPC) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconcile delete")

	// ensure no dpuserviceinterfaces are referencing this virtual network (which means that no serviceinterfaces are connected to this virtual network)
	// we look for DPUServiceInterfaces and not ServiceInterfaces because the latter is created as a result of the former
	// and the former has substantially less objects than the latter.
	dpuSIL := &dpuservicev1.DPUServiceInterfaceList{}
	if err := r.Client.List(ctx, dpuSIL, client.InNamespace(dpuVnet.Namespace)); err != nil {
		conditions.AddFalse(dpuVnet,
			vpcv1.ConditionDPUVirtualNetworkReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: failed to list DPUServiceInterfaces, %s", err)))
		return ctrl.Result{}, fmt.Errorf("failed to list DPUServiceInterfaces: %w", err)
	}
	for _, dpuServiceIfc := range dpuSIL.Items {
		if dpuServiceIfc.GetVirtualNetworkName() == dpuVnet.Name {
			reqLog.Info("DPUVirtualNetwork is in use by DPUServiceInterface", "dpuServiceInterface", client.ObjectKeyFromObject(&dpuServiceIfc).String())
			// virtual network is in use, a requeue will occur when DPUServiceInterface is deleted.
			conditions.AddFalse(dpuVnet,
				vpcv1.ConditionDPUVirtualNetworkReconciled,
				conditions.ReasonAwaitingDeletion,
				"DPUVirtualNetwork is in use by DPUServiceInterface")
			return ctrl.Result{}, nil
		}
	}

	// remove ip allocation for the virtual network
	alloc := r.IPManager.GetNetworkIPAllocator(ipmanager.ObjToID(dpuVpc), ipmanager.VPCClusterNetworkIPV4)
	if alloc == nil {
		reqLog.Info("IP allocator not found for VPC. skipping deallocation", "vpc", client.ObjectKeyFromObject(dpuVpc).String())
	} else {
		// deallocate IP from cluster network
		alloc.Deallocate(ipmanager.ObjToID(dpuVnet))
		delete(dpuVnet.Annotations, common.LRPAddressesAnnotationKey)
	}

	// Remove finalizer if not set.
	if controllerutil.ContainsFinalizer(dpuVnet, dpuVirtualNetworkFinalizer) {
		controllerutil.RemoveFinalizer(dpuVnet, dpuVirtualNetworkFinalizer)
	}

	conditions.AddTrue(dpuVnet, vpcv1.ConditionDPUVirtualNetworkReconciled)
	return ctrl.Result{}, nil
}

//nolint:prealloc
func (r *DPUVirtualNetworkReconciler) dpuVPCToDPUVirtualNetworkRequests(ctx context.Context, o client.Object) []reconcile.Request {
	var requests []reconcile.Request
	log := ctrllog.FromContext(ctx)
	dpuVpc, ok := o.(*vpcv1.DPUVPC)
	if !ok {
		log.Info("dpuVPCToDPUVirtualNetworkRequests: failed to cast object to DPUVPC")
		return requests
	}

	vns, err := VirtualNetworksForVPC(ctx, r.Client, dpuVpc)
	if err != nil {
		log.Info("dpuVPCToDPUVirtualNetworkRequests: failed to list DPUVirtualNetworks")
		return requests
	}

	for _, vn := range vns {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vn)})
	}
	return requests
}

func (r *DPUVirtualNetworkReconciler) dpuServiceInterfaceToDPUVirtualNetworkRequests(ctx context.Context, o client.Object) []reconcile.Request {
	var requests []reconcile.Request
	log := ctrllog.FromContext(ctx)
	dpuSI, ok := o.(*dpuservicev1.DPUServiceInterface)
	if !ok {
		log.Info("In dpuServiceInterfaceToDPUVirtualNetworkRequests: failed to cast object to DPUServiceInterface")
		return requests
	}

	if vn := dpuSI.GetVirtualNetworkName(); vn != "" {
		// get the DPUVirtualNetwork object
		vnObj := &vpcv1.DPUVirtualNetwork{}
		err := r.Client.Get(ctx, client.ObjectKey{Namespace: dpuSI.Namespace, Name: vn}, vnObj)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				log.Error(err, "In dpuServiceInterfaceToDPUVirtualNetworkRequests: failed to get DPUVirtualNetwork object", "virtualNetwork", vn)
			}
			return requests
		}
		if !vnObj.ObjectMeta.DeletionTimestamp.IsZero() {
			// object is deleting, we want to enqueue reconcile to attempt to remove finalizer
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(vnObj)})
		}
	}

	return requests
}
