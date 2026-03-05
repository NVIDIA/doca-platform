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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

const (
	dpuclusterControllerName = "vpcdpuclustercontroller"
)

var (
	dpuclusterRequeueTime = 5 * time.Minute
)

// DPUClusterReconciler reconciles DPUCluster objects by registering watches in remote cache
type DPUClusterReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	RemoteCache      *dpucluster.RemoteCache
	WatchRegisterers []WatchRegisterer
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(dpuclusterControllerName).
		For(&provisioningv1.DPUCluster{}).
		Complete(r)
}

// Reconcile reconciles DPUCluster objects by registering watches in remote cache
func (r *DPUClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)

	log.Info("Reconciling")
	cluster := &provisioningv1.DPUCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			// NOTE(adrianc): RemoteCache will remove the accessor for this cluster together with any active watches.
			// so nothing to do here.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("dpucluster is deleting, skipping watch registration.", "cluster", cluster.Name)
		return ctrl.Result{}, nil
	}

	// register watches in remote cache
	for _, wr := range r.WatchRegisterers {
		if err := wr.RegisterWatchesForCluster(ctx, r.RemoteCache, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// requeue the request to ensure watches are registered in case remote cache removed the accessor for
	// the cluster and re-added it in case of health check failure.
	return ctrl.Result{RequeueAfter: dpuclusterRequeueTime}, nil
}
