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
//nolint:unparam
package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology"

	"github.com/fluxcd/pkg/runtime/patch"
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvpcs,verbs=get;list;watch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=dpuvirtualnetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=vpc.dpu.nvidia.com,resources=isolationclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces,verbs=get;list;watch

const (
	serviceInterfaceControllerName = "vpcserviceinterfacecontroller"
	serviceInterfaceFinalizer      = "ovn.vpc.dpu.nvidia.com/serviceinterface-protection"
)

var (
	relatedObjectsPendingRequeueTime = 10 * time.Second
)

// ServiceInterfaceReconciler reconciles ServiceInterface objects in dpu clusters
type ServiceInterfaceReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	RemoteCache *dpucluster.RemoteCache
	CleanupGate CleanupGate

	selfController ctrlcontroller.TypedController[RequestWithCluster]

	// used for dependency injection
	topologyManagerFromIsolationClassFn func(ctx context.Context, isoCls *vpcv1.IsolationClass) (topology.Manager, error)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceInterfaceReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if r.topologyManagerFromIsolationClassFn == nil {
		r.topologyManagerFromIsolationClassFn = TopologyManagerFromIsolationClass
	}
	c, err := ctrlbuilder.TypedControllerManagedBy[RequestWithCluster](mgr).
		Named(serviceInterfaceControllerName).
		// watch for DPUVirtualNetwork that are ready and enqueue all service interfaces that belong to this virtual network
		Watches(&vpcv1.DPUVirtualNetwork{}, handler.TypedEnqueueRequestsFromMapFunc(r.virtualNetworkToServiceInterfaceRequestWithClusters)).
		Build(r)
	r.selfController = c
	return err
}

// Reconcile reconciles a ServiceInterface object when virtualNetwork is set
func (r *ServiceInterfaceReconciler) Reconcile(ctx context.Context, req RequestWithCluster) (_ ctrl.Result, reterr error) {
	defer r.CleanupGate.LockForReconcile()()

	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling", "request", req.Request.NamespacedName.String(), "cluster", req.cluster.String())

	dpuclient, err := r.RemoteCache.GetClient(req.cluster)
	if err != nil {
		if errors.Is(err, dpucluster.ErrDPUClusterNotConnected) {
			// DPU cluster not connected. if/once connected, watches will be re-registered and reconcile will be triggered
			reqLog.Info("DPU cluster not connected. requeue if connected", "cluster", req.cluster.String())
			return ctrl.Result{}, nil
		}
		reqLog.Error(err, "failed to get dpu client", "cluster", req.cluster.String())
		return ctrl.Result{}, err
	}

	// get service interface
	si := &dpuservicev1.ServiceInterface{}
	if err := dpuclient.Get(ctx, req.NamespacedName, si); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(si, dpuclient)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		reqLog.Info("Patching")
		if err := patcher.Patch(ctx, si,
			patch.WithFieldOwner(serviceInterfaceControllerName),
		); kerrors.FilterOut(err, apierrors.IsNotFound) != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !si.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, si)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(si, serviceInterfaceFinalizer) {
		controllerutil.AddFinalizer(si, serviceInterfaceFinalizer)
		return ctrl.Result{}, nil
	}

	node := &corev1.Node{}
	if si.Spec.Node == nil || *si.Spec.Node == "" {
		reqLog.Info("node is not set for ServiceInterface. skipping reconcile")
		return ctrl.Result{}, nil
	}

	if err := dpuclient.Get(ctx, types.NamespacedName{Name: *si.Spec.Node}, node); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get node %s: %w", *si.Spec.Node, err)
	}

	return r.reconcile(ctx, si, node)
}

//nolint:unparam
func (r *ServiceInterfaceReconciler) reconcile(ctx context.Context, si *dpuservicev1.ServiceInterface, node *corev1.Node) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("reconcile internal")

	// ensure mac address annotation is set
	mac, err := common.LSPMACAddressFromAnnotation(si.Annotations)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("mac address annotation(%s) is not set or is invalid: %w", common.LSPMACAddressAnnotationKey, err)
	}

	// get vpc, virtual network and isolation class
	isoCls, vpc, vn, found, err := r.getRelatedObjects(ctx, si)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !found {
		reqLog.Info("some of the related objects dont exist, requeue")
		return ctrl.Result{RequeueAfter: relatedObjectsPendingRequeueTime}, nil
	}

	if !conditions.IsTrue(vn, conditions.TypeReady) {
		// virtual network is not ready yet, an reconcile will be triggered once ready.
		reqLog.Info("virtual network is not ready yet. waiting for it to be ready")
		return ctrl.Result{}, nil
	}

	// get topology manager
	tm, err := r.topologyManagerFromIsolationClassFn(ctx, isoCls)
	if err != nil {
		return ctrl.Result{}, err
	}

	nodeInVPC, err := NodeInVPC(node, vpc)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to check if node %s is in vpc: %w", node.Name, err)
	}

	var connected bool
	defer func() {
		// set connected annotation on serviceinterface
		// NOTE(adrianc): we dont do this via conditions since this is implementation specific.
		if si.Annotations == nil {
			si.Annotations = make(map[string]string)
		}
		si.Annotations[common.LSPConnectedAnnotationKey] = fmt.Sprintf("%v", connected)
	}()

	// plug/unplug service interface if node where the service interface resides belongs to the vpc
	if !nodeInVPC {
		reqLog.Info("node of serviceInterface is not in vpc", "node", node.Name, "vpc", vpc.Name, "serviceInterface", client.ObjectKeyFromObject(si).String())
		// unplug interface from ovn
		reqLog.Info("unplugging interface from ovn",
			"interface", client.ObjectKeyFromObject(si).String(),
			"virtualNetwork", client.ObjectKeyFromObject(vn).String(),
			"vpc", client.ObjectKeyFromObject(vpc).String())

		if err := tm.UnplugServiceInterface(ctx, vn, si); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to unplug interface from ovn: %w", err)
		}
	} else {
		// plug interface into ovn
		macStr := "nil"
		if mac != nil {
			macStr = mac.String()
		}
		reqLog.Info("plugging interface into ovn",
			"interface", client.ObjectKeyFromObject(si).String(),
			"mac", macStr,
			"virtualNetwork", client.ObjectKeyFromObject(vn).String(),
			"vpc", client.ObjectKeyFromObject(vpc).String())

		if err := tm.PlugServiceInterface(ctx, vpc, vn, node, si); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to plug interface into ovn: %w", err)
		}
		connected = true
	}

	return ctrl.Result{}, nil
}

//nolint:unparam
func (r *ServiceInterfaceReconciler) reconcileDelete(ctx context.Context, si *dpuservicev1.ServiceInterface) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("reconcile delete")

	// get vpc, virtual network and isolation class
	isoCls, vpc, vn, found, err := r.getRelatedObjects(ctx, si)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !found {
		reqLog.Info("some of the related objects dont exist, requeue")
		return ctrl.Result{RequeueAfter: relatedObjectsPendingRequeueTime}, nil
	}

	// Note(adrianc): in contrast to the reconcile flow, we dont care here if the virtual network is ready or not
	// since we are deleting the service interface and we dont want to block the deletion.

	// get topology manager
	tm, err := r.topologyManagerFromIsolationClassFn(ctx, isoCls)
	if err != nil {
		return ctrl.Result{}, err
	}

	// unplug interface from ovn
	reqLog.Info("unplugging interface from ovn",
		"interface", client.ObjectKeyFromObject(si).String(),
		"virtualNetwork", client.ObjectKeyFromObject(vn).String(),
		"vpc", client.ObjectKeyFromObject(vpc).String())

	if err := tm.UnplugServiceInterface(ctx, vn, si); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to unplug interface from ovn: %w", err)
	}

	// Remove finalizer if not set.
	if controllerutil.ContainsFinalizer(si, serviceInterfaceFinalizer) {
		controllerutil.RemoveFinalizer(si, serviceInterfaceFinalizer)
	}

	return ctrl.Result{}, nil
}

// getRelatedObjects gets the related objects for the service interface
// it returns the isolation class, vpc and virtual network
// if any of the objects are not found, it returns false for found
// if error occurs (other than not found error), it returns the error
func (r *ServiceInterfaceReconciler) getRelatedObjects(ctx context.Context, si *dpuservicev1.ServiceInterface) (
	isoCls *vpcv1.IsolationClass,
	vpc *vpcv1.DPUVPC,
	vn *vpcv1.DPUVirtualNetwork,
	found bool,
	err error) {
	// get virtual network
	virtualNetworkName := si.GetVirtualNetworkName()
	vn = &vpcv1.DPUVirtualNetwork{}
	vn.Name = virtualNetworkName
	vn.Namespace = si.Namespace

	if err = r.Client.Get(ctx, client.ObjectKeyFromObject(vn), vn); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, nil, false, nil
		}
		err = fmt.Errorf("failed to get virtual network %s: %w", client.ObjectKeyFromObject(vn).String(), err)
		return nil, nil, nil, false, err
	}

	// get VPC
	vpc = &vpcv1.DPUVPC{}
	vpc.Name = vn.Spec.VPCName
	vpc.Namespace = vn.Namespace

	if err = r.Client.Get(ctx, client.ObjectKeyFromObject(vpc), vpc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, nil, false, nil
		}
		err = fmt.Errorf("failed to get vpc %s: %w", client.ObjectKeyFromObject(vpc).String(), err)
		return nil, nil, nil, false, err
	}

	// get isolation class for vpc
	isoCls, err = IsolationClassForVPC(ctx, r.Client, vpc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, nil, false, nil
		}
		err = fmt.Errorf("failed to get isolation class for vpc %s: %w", client.ObjectKeyFromObject(vpc).String(), err)
		return nil, nil, nil, false, err
	}
	found = true

	return isoCls, vpc, vn, found, nil
}

// RegisterWatchesForCluster registers watches for the ServiceInterface controller in a given cluster
// this is called by the dpucluster controller when a change in dpucluster is detected (create/update)
func (r *ServiceInterfaceReconciler) RegisterWatchesForCluster(ctx context.Context, rc *dpucluster.RemoteCache, cluster *provisioningv1.DPUCluster) error {
	if err := r.watchServiceInterfaces(ctx, rc, cluster); err != nil {
		return err
	}
	if err := r.watchNodes(ctx, rc, cluster); err != nil {
		return err
	}
	return nil
}

// watchServiceInterfaces registers watches for service interfaces in the (dpu)cluster
func (r *ServiceInterfaceReconciler) watchServiceInterfaces(ctx context.Context, rc *dpucluster.RemoteCache, cluster *provisioningv1.DPUCluster) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("registering watch for ServiceInterfaces", "controller", serviceInterfaceControllerName, "cluster", cluster.Name)

	// Watch expects unique name
	watcherName := fmt.Sprintf("%s-%s", serviceInterfaceControllerName, "si")
	return rc.Watch(ctx, client.ObjectKeyFromObject(cluster), dpucluster.NewWatcher(
		dpucluster.TypedWatcherOptions[client.Object, RequestWithCluster]{
			Name: watcherName,
			Kind: &dpuservicev1.ServiceInterface{},
			EventHandler: handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []RequestWithCluster {
				reqLog := ctrllog.FromContext(ctx)

				si, ok := o.(*dpuservicev1.ServiceInterface)
				if !ok {
					reqLog.Error(fmt.Errorf("expected a ServiceInterface but got a %T", o), "Failed to convert object")
					return nil
				}
				return []RequestWithCluster{
					{
						Request: ctrl.Request{
							NamespacedName: types.NamespacedName{
								Namespace: si.Namespace,
								Name:      si.Name,
							},
						},
						cluster: client.ObjectKeyFromObject(cluster),
					},
				}
			}),
			Predicates: []predicate.Predicate{predicate.NewPredicateFuncs(func(o client.Object) bool {
				si, ok := o.(*dpuservicev1.ServiceInterface)
				if !ok {
					return false
				}
				return si.HasVirtualNetwork()
			})},
			Watcher: r.selfController,
		}))
}

// watchNodes registers watches for service nodes in the (dpu)cluster
func (r *ServiceInterfaceReconciler) watchNodes(ctx context.Context, rc *dpucluster.RemoteCache, cluster *provisioningv1.DPUCluster) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("registering watch for Nodes", "controller", serviceInterfaceControllerName, "cluster", cluster.Name)

	// Watch expects unique name
	watcherName := fmt.Sprintf("%s-%s", serviceInterfaceControllerName, "node")
	return rc.Watch(ctx, client.ObjectKeyFromObject(cluster), dpucluster.NewWatcher(
		dpucluster.TypedWatcherOptions[client.Object, RequestWithCluster]{
			Name: watcherName,
			Kind: &corev1.Node{},
			EventHandler: handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []RequestWithCluster {
				reqLog := ctrllog.FromContext(ctx)

				node, ok := o.(*corev1.Node)
				if !ok {
					reqLog.Error(fmt.Errorf("expected a Node but got a %T", o), "Failed to convert object")
					return nil
				}

				// get service interfaces for node
				c, err := rc.GetClient(client.ObjectKeyFromObject(cluster))
				if err != nil {
					reqLog.Error(err, "failed to get client for cluster", "name", client.ObjectKeyFromObject(cluster).String())
					return nil
				}

				siList := &dpuservicev1.ServiceInterfaceList{}
				if err := c.List(ctx, siList); err != nil {
					reqLog.Error(err, "failed to list service interfaces for node", "node", node.Name)
					return nil
				}

				// filter service interfaces for node and enqueue them
				var requests []RequestWithCluster
				for _, si := range siList.Items {
					if si.Spec.Node == nil || *si.Spec.Node != node.Name {
						continue
					}
					if !si.HasVirtualNetwork() {
						continue
					}
					if si.ObjectMeta.DeletionTimestamp != nil {
						continue
					}
					requests = append(requests, RequestWithCluster{
						Request: ctrl.Request{
							NamespacedName: types.NamespacedName{
								Namespace: si.Namespace,
								Name:      si.Name,
							},
						},
						cluster: client.ObjectKeyFromObject(cluster),
					})
				}

				return requests
			}),
			Predicates: []predicate.Predicate{predicate.LabelChangedPredicate{}},
			Watcher:    r.selfController,
		}))
}

// virtualNetworkToServiceInterfaceRequestWithClusters is a handler to enqueue all service interfaces that belong to a virtual network
func (r *ServiceInterfaceReconciler) virtualNetworkToServiceInterfaceRequestWithClusters(ctx context.Context, o client.Object) []RequestWithCluster {
	log := ctrllog.FromContext(ctx)
	var requests []RequestWithCluster
	vn, ok := o.(*vpcv1.DPUVirtualNetwork)
	if !ok {
		log.Error(nil, "unable to convert object to DPUVirtualNetwork")
		return requests
	}

	if vn.GetDeletionTimestamp() != nil {
		return requests
	}

	// check Ready condition true
	if !conditions.IsTrue(vn, conditions.TypeReady) {
		return requests
	}

	// vn is ready, enqueue all service interfaces that belong to this virtual network
	dpuclusters := provisioningv1.DPUClusterList{}
	if err := r.List(ctx, &dpuclusters); err != nil {
		log.Error(err, "Failed to list dpuclusters")
		return requests
	}

	for _, dc := range dpuclusters.Items {
		c, err := r.RemoteCache.GetClient(client.ObjectKeyFromObject(&dc))
		if err != nil {
			log.Error(err, "failed to get dpu client from remote cache. skipping enqueue of serviceinterfaces for cluster", "cluster", client.ObjectKeyFromObject(&dc).String())
			continue
		}
		siList := &dpuservicev1.ServiceInterfaceList{}
		if err := c.List(ctx, siList, client.InNamespace(vn.Namespace)); err != nil {
			log.Error(err, "Failed to list service interfaces")
			return requests
		}

		for _, si := range siList.Items {
			if si.GetVirtualNetworkName() != vn.Name {
				continue
			}

			// ignore service interface that have connected annotation set (means that virtual network already existed and it was processed by the reconciler)
			if si.Annotations != nil {
				if _, ok := si.Annotations[common.LSPConnectedAnnotationKey]; ok {
					continue
				}
			}

			requests = append(requests, RequestWithCluster{
				Request: ctrl.Request{
					NamespacedName: client.ObjectKeyFromObject(&si),
				},
				cluster: client.ObjectKeyFromObject(&dc),
			})
		}
	}
	return requests
}
