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

package controllers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// CleanupGate is used to synchronize cleanup operations, enusring that Either Reconcilers are performing
// operations or cleanup controller is performing operations but not both.
// multiple Reconcilers can run in parallel, but only one cleanup can be performed at a time.
type CleanupGate interface {
	// LockForReconcile waits for the cleanup to be done, if no cleanup is in progress it returns immediately
	// returns a function that unlocks the gate. it MUST be called once reconcile is done.
	LockForReconcile() func()
	// LockForCleanup waits for the reconcile to be done, if no reconcile in progress it returns immediately
	// returns a function that unlocks the gate. it MUST be called once cleanup is done.
	LockForCleanup() func()
}

// NewCleanupGate creates a new CleanupGate
func NewCleanupGate() CleanupGate {
	return &cleanupGate{
		sync.RWMutex{},
	}
}

// cleanupGate implements CleanupGate
type cleanupGate struct {
	sync.RWMutex
}

// LockForReconcile waits for the cleanup to be done, if no cleanup is in progress it returns immediately
// returns a function that unlocks the gate. it MUST be called once reconcile is done.
func (c *cleanupGate) LockForReconcile() func() {
	c.RLock()
	return c.RUnlock
}

// LockForCleanup waits for the reconcile to be done, if no reconcile in progress it returns immediately
// returns a function that unlocks the gate. it MUST be called once cleanup is done.
func (c *cleanupGate) LockForCleanup() func() {
	c.Lock()
	return c.Unlock
}

// TopologyCleaner is a controller that periodically cleans up ovn resources based on the current state of the cluster
type TopologyCleaner struct {
	client.Client

	RemoteCache     *dpucluster.RemoteCache
	ReconcilePeriod time.Duration
	CleanupGate     CleanupGate

	// used for dependency injection
	topologyManagerFromIsolationClassFn func(ctx context.Context, isoCls *vpcv1.IsolationClass) (topology.Manager, error)
}

// SetupWithManager sets up the topology cleaner controller with the manager–
func (t *TopologyCleaner) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if t.topologyManagerFromIsolationClassFn == nil {
		t.topologyManagerFromIsolationClassFn = TopologyManagerFromIsolationClass
	}

	return mgr.Add(t)
}

// Start begins the periodic reconciliation process
func (t *TopologyCleaner) Start(ctx context.Context) error {
	log := ctrllog.FromContext(ctx).WithValues("controller", "topologycleaner")
	ctx = ctrllog.IntoContext(ctx, log)
	log.Info("Starting TopologyCleaner controller", "reconcilePeriod", t.ReconcilePeriod.String())

	// Run the reconcile function every topologyCleanerReconcilePeriod, until the context is done
	go wait.UntilWithContext(ctx, func(ctx context.Context) {
		// Define backoff parameters for retry
		backoff := wait.Backoff{
			Steps:    5,
			Duration: 10 * time.Second,
			Factor:   2.0,
			Jitter:   0.0,
		}

		// Attempt reconciliation with retry on failure
		err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
			if err := t.reconcile(ctx); err != nil {
				log.Error(err, "TopologyCleaner failed to reconcile, will retry with backoff")
				return false, nil // Return false to continue retrying
			}
			return true, nil // Return true to stop retrying
		})

		if err != nil {
			log.Error(err, "TopologyCleaner failed to reconcile. will retry after reconcile period",
				"reconcilePeriod", t.ReconcilePeriod.String())
		}
		log.Info("TopologyCleaner next reconcile", "reconcilePeriod", t.ReconcilePeriod.String())
	}, t.ReconcilePeriod)

	<-ctx.Done()
	klog.Info("Stopping TopologyCleaner controller")
	return nil
}

// reconcile performs the actual cleanup logic
func (t *TopologyCleaner) reconcile(ctx context.Context) error {
	defer t.CleanupGate.LockForCleanup()()

	log := ctrllog.FromContext(ctx)
	log.Info("Running topology cleanup reconciliation")

	// get DPUCluster clients
	dpuClusterClients, err := t.RemoteCache.ListClients()
	if err != nil {
		return fmt.Errorf("failed to get DPUCluster clients. %w", err)
	}

	// get all isolationclasses
	isolationClasses := &vpcv1.IsolationClassList{}
	if err := t.Client.List(ctx, isolationClasses); err != nil {
		return fmt.Errorf("failed to list IsolationClasses. %w", err)
	}

	var errs []error
	for _, isoCls := range isolationClasses.Items {
		if isoCls.Spec.Provisioner != OVNProvisionerName {
			// skip non ovn isolation classes
			continue
		}

		if err := t.cleanupTopologyForIsolationClass(ctx, &isoCls, dpuClusterClients); err != nil {
			errs = append(errs, fmt.Errorf("failed to cleanup topology for isolation class %s. %w", isoCls.Name, err))
		}
	}

	log.Info("Topology cleanup reconciliation completed")
	return kerrors.NewAggregate(errs)
}

// cleanupTopologyForIsolationClass cleans up ovn resources for an isolation class
func (t *TopologyCleaner) cleanupTopologyForIsolationClass(ctx context.Context, isoCls *vpcv1.IsolationClass, dpuClusterClients []client.Client) error {
	tm, err := t.topologyManagerFromIsolationClassFn(ctx, isoCls)
	if err != nil {
		return fmt.Errorf("failed to get topology manager for isolation class. %w", err)
	}

	var errs []error
	// cleanup stale VPCs
	if err := t.cleanupStaleVPCs(ctx, tm, isoCls); err != nil {
		errs = append(errs, fmt.Errorf("failed to cleanup stale VPCs. %w", err))
	}

	// cleanup stale service interfaces
	if err := t.cleanupStaleServiceInterfaces(ctx, tm, dpuClusterClients); err != nil {
		errs = append(errs, fmt.Errorf("failed to cleanup stale service interfaces for isolation class %s. %w", isoCls.Name, err))
	}

	return kerrors.NewAggregate(errs)
}

// cleanupStaleVPCs cleans up stale VPCs from ovn
func (t *TopologyCleaner) cleanupStaleVPCs(ctx context.Context, tm topology.Manager, isoCls *vpcv1.IsolationClass) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Cleaning up stale VPCs")

	ovnVPCRefs, err := tm.ListVPCs(ctx)
	if err != nil {
		return fmt.Errorf("topology manager failed to list VPCs. %w", err)
	}
	ovnVPCSet := sets.New(ovnVPCRefs...)

	vpcs, err := VPCsForIsolationClass(ctx, t.Client, isoCls)
	if err != nil {
		return fmt.Errorf("failed to get VPCs for isolation class. %w", err)
	}

	currentVPCSet := sets.New[client.ObjectKey]()
	for _, vpc := range vpcs {
		currentVPCSet.Insert(client.ObjectKeyFromObject(vpc))
	}

	staleVPCs := ovnVPCSet.Difference(currentVPCSet)

	// delete stale VPCs
	var errs []error
	for v := range staleVPCs {
		reqLog.Info("Deleting stale VPC", "vpc", v)

		// construct partial object to be used for deletion
		vpc := &vpcv1.DPUVPC{}
		vpc.SetNamespace(v.Namespace)
		vpc.SetName(v.Name)

		if err := tm.RemoveTopology(ctx, vpc); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove topology for stale VPC %s. %w", v, err))
		}
	}

	return kerrors.NewAggregate(errs)
}

// cleanupStaleServiceInterfaces cleans up stale service interfaces from ovn
func (t *TopologyCleaner) cleanupStaleServiceInterfaces(ctx context.Context, tm topology.Manager, dpuClusterClients []client.Client) error {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Cleaning up stale service interfaces")

	// get service interface refs from ovn
	ovnSIRefs, err := tm.ListServiceInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("topology manager failed to list service interfaces. %w", err)
	}

	ovnSISet := sets.New[client.ObjectKey]()
	siRefsBySi := make(map[client.ObjectKey]topology.ServiceInterfacRef)
	for _, siRef := range ovnSIRefs {
		ovnSISet.Insert(siRef.ServiceInterface)
		siRefsBySi[siRef.ServiceInterface] = siRef
	}

	// get service interfaces from all dpuclusters
	var serviceInterfaces []*dpuservicev1.ServiceInterface
	for _, dpuClusterClient := range dpuClusterClients {
		siList := &dpuservicev1.ServiceInterfaceList{}
		err := dpuClusterClient.List(ctx, siList)
		if err != nil {
			return fmt.Errorf("failed to list service interfaces. %w", err)
		}

		for _, si := range siList.Items {
			if si.Spec.Node == nil || !si.HasVirtualNetwork() {
				continue
			}
			serviceInterfaces = append(serviceInterfaces, &si)
		}
	}

	currentSISet := sets.New[client.ObjectKey]()
	for _, si := range serviceInterfaces {
		currentSISet.Insert(client.ObjectKeyFromObject(si))
	}

	// compare the two sets and delete the stale ones
	staleSIs := ovnSISet.Difference(currentSISet)

	var errs []error
	for s := range staleSIs {
		reqLog.Info("Deleting stale service interface", "serviceInterface", s)

		sir := siRefsBySi[s]

		// construct partial objects to be used for unplugging
		vn := &vpcv1.DPUVirtualNetwork{}
		vn.SetNamespace(sir.VirtualNetwork.Namespace)
		vn.SetName(sir.VirtualNetwork.Name)
		vn.Spec.VPCName = sir.VPC.Name

		si := &dpuservicev1.ServiceInterface{}
		si.SetNamespace(sir.ServiceInterface.Namespace)
		si.SetName(sir.ServiceInterface.Name)

		// unplug the service interface
		if err := tm.UnplugServiceInterface(ctx, vn, si); err != nil {
			errs = append(errs, fmt.Errorf("failed to unplug service interface %s. %w", s, err))
		}
	}

	return kerrors.NewAggregate(errs)
}
