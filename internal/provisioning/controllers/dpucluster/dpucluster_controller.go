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

package dpucluster

import (
	"context"
	"fmt"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// DPUClusterControllerName is the controllers name that will be used when reporting events
const DPUClusterControllerName = "dpucluster"

// DPUClusterReconciler reconciles a DPUCluster object
type DPUClusterReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	adminClients sync.Map
	Allocator    allocator.Allocator
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters/finalizers,verbs=update

func (r *DPUClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile")

	dc := &provisioningv1.DPUCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, dc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get DPUCluster %w", err)
	}

	if !dc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, dc)
	}

	if !controllerutil.ContainsFinalizer(dc, provisioningv1.FinalizerInternalCleanUp) {
		logger.Info(fmt.Sprintf("add finalizer %s", provisioningv1.FinalizerInternalCleanUp))
		controllerutil.AddFinalizer(dc, provisioningv1.FinalizerInternalCleanUp)
		if err := r.Client.Update(ctx, dc); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer %w", err)
		}
	}
	r.Allocator.SaveCluster(dc)
	dc.Status.NodesCount = r.Allocator.GetDPUsCount(dc)
	logger.V(3).Info(fmt.Sprintf("NodesCount: %d", dc.Status.NodesCount))

	if dc.Status.Phase == provisioningv1.PhasePending {
		dc.Status.Phase = provisioningv1.PhaseCreating
		if err := r.Client.Status().Update(ctx, dc); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to set Creating phase %w", err)
		}
		logger.Info("Start creating cluster")
		return ctrl.Result{}, nil
	}

	allTrue, isCreated := true, false
	for _, cond := range dc.Status.Conditions {
		if cond.Type == string(provisioningv1.ConditionReady) {
			continue
		}
		allTrue = allTrue && (cond.Status == metav1.ConditionTrue)
		if cond.Type == string(provisioningv1.ConditionCreated) {
			isCreated = cond.Status == metav1.ConditionTrue
		}
	}
	allTrue = allTrue && isCreated

	var errList []error
	if allTrue {
		var healthCheckErr error
		var healthCheckMsg string

		adminClient, err := r.getOrCreateClient(ctx, dc)
		if err != nil {
			healthCheckErr = err
			healthCheckMsg = "failed to get admin client"
		} else if serverVersion, err := adminClient.ServerVersion(); err != nil {
			healthCheckErr = err
			healthCheckMsg = "health check failed"
		} else {
			dc.Status.Version = serverVersion.GitVersion
		}

		if healthCheckErr != nil {
			cond := cutil.NewCondition(string(provisioningv1.ConditionReady), healthCheckErr, "HealthCheckFailed", "")
			meta.SetStatusCondition(&dc.Status.Conditions, *cond)
			dc.Status.Phase = provisioningv1.PhaseNotReady
			errList = append(errList, fmt.Errorf("%s, err: %v", healthCheckMsg, healthCheckErr))
		} else {
			logger.Info("Cluster is Ready")
			cond := cutil.NewCondition(string(provisioningv1.ConditionReady), nil, "HealthCheckPassed", "")
			meta.SetStatusCondition(&dc.Status.Conditions, *cond)
			dc.Status.Phase = provisioningv1.PhaseReady
		}
	} else {
		switch dc.Status.Phase {
		case provisioningv1.PhaseReady, provisioningv1.PhaseNotReady:
			logger.Info("Cluster is not Ready")
			dc.Status.Phase = provisioningv1.PhaseNotReady
		default: // no-op
		}
	}
	if err := r.Client.Status().Update(ctx, dc); err != nil {
		errList = append(errList, fmt.Errorf("failed to update status, err: %v", err))
		return ctrl.Result{}, kerrors.NewAggregate(errList)
	}
	return ctrl.Result{}, kerrors.NewAggregate(errList)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPUCluster{}).
		Watches(&provisioningv1.DPU{},
			handler.EnqueueRequestsFromMapFunc(r.dpuToDPUClusterReq)).
		Complete(r)
}

func (r *DPUClusterReconciler) dpuToDPUClusterReq(ctx context.Context, obj client.Object) []reconcile.Request {
	dpu := obj.(*provisioningv1.DPU)
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name:      dpu.Spec.Cluster.Name,
			Namespace: dpu.Spec.Cluster.Namespace,
		},
	}}
}

func (r *DPUClusterReconciler) reconcileDelete(ctx context.Context, dc *provisioningv1.DPUCluster) error {

	dpuList := &provisioningv1.DPUList{}
	if err := r.Client.List(ctx, dpuList); err != nil {
		return fmt.Errorf("failed to list DPUs %w", err)
	}

	dpuCountOfCluster := 0
	for _, dpu := range dpuList.Items {
		if dpu.Spec.Cluster.Name == dc.Name && dpu.Spec.Cluster.Namespace == dc.Namespace {
			dpuCountOfCluster += 1
		}
	}

	if dpuCountOfCluster != 0 {
		return fmt.Errorf("there are still DPUs in the cluster")
	}

	r.Allocator.RemoveCluster(dc)
	r.adminClients.Delete(dc.UID)
	controllerutil.RemoveFinalizer(dc, provisioningv1.FinalizerInternalCleanUp)

	if err := r.Client.Update(ctx, dc); err != nil {
		return fmt.Errorf("failed to update DPUCluster %w", err)
	}

	// Waiting for existing DPUs to trigger finalizer removal again.
	return nil
}

func (r *DPUClusterReconciler) getOrCreateClient(ctx context.Context, dc *provisioningv1.DPUCluster) (*kubernetes.Clientset, error) {
	if dc == nil {
		return nil, fmt.Errorf("dc is nil")
	}
	if value, ok := r.adminClients.Load(dc.UID); ok {
		return value.(*kubernetes.Clientset), nil
	}

	clientSet, _, err := cutil.GetClientset(ctx, r.Client, dc)
	if err != nil {
		return nil, err
	}
	r.adminClients.Store(dc.UID, clientSet)
	return clientSet, nil
}
