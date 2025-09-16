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

package dpu

import (
	"context"
	"fmt"
	"reflect"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/gnoi"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/mock"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type PhaseHandlerFunc func(context.Context, *provisioningv1.DPU, *util.ControllerContext) (provisioningv1.DPUStatus, error)

// DPUControllerName is used when reporting events
const DPUControllerName = "dpu"

// DPUReconciler reconciles a DPU object
type DPUReconciler struct {
	ctrlCtx              *util.ControllerContext
	handlers             map[provisioningv1.DPUPhase]PhaseHandlerFunc
	DPUInProvisioningMap *util.DPUInProvisioningMap
}

type DPUPhaseCategory int

const (
	PhaseBeforeProvisioning DPUPhaseCategory = iota
	PhaseInProvisioning
	PhaseAfterProvisioning
	PhaseUnknown
)

func GetDPUPhaseCategory(phase provisioningv1.DPUPhase) DPUPhaseCategory {
	switch {
	case cutil.IsDPUBeforeProvisioningPhase(phase):
		return PhaseBeforeProvisioning
	case cutil.IsDPUInProvisioningPhase(phase):
		return PhaseInProvisioning
	case cutil.IsDPUAfterProvisioningPhase(phase):
		return PhaseAfterProvisioning
	default:
		return PhaseUnknown
	}
}

func NewDPUReconciler(mgr manager.Manager, alloc allocator.Allocator, joinCommandGenerator util.NodeJoinCommandGenerator, hostUptimeChecker reboot.HostUptimeChecker, options util.DPUOptions, dpuMap *util.DPUInProvisioningMap) *DPUReconciler {
	ctrlCtx := &util.ControllerContext{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		Recorder:             mgr.GetEventRecorderFor(DPUControllerName),
		Options:              options,
		ClusterAllocator:     alloc,
		JoinCommandGenerator: joinCommandGenerator,
		HostUptimeChecker:    hostUptimeChecker,
		DPUInProvisioningMap: dpuMap,
	}
	handlers := map[provisioningv1.DPUPhase]PhaseHandlerFunc{
		"":                              state.Initializing,
		provisioningv1.DPUInitializing:  state.Initializing,
		provisioningv1.DPUPending:       state.Pending,
		provisioningv1.DPUNodeEffect:    state.NodeEffect,
		provisioningv1.DPURebooting:     state.Rebooting,
		provisioningv1.DPUClusterConfig: state.ClusterConfig,
		provisioningv1.DPUReady:         state.Ready,
		provisioningv1.DPUDeleting:      state.Deleting,
		provisioningv1.DPUError:         state.Error,
	}
	switch options.DPUInstallInterface {
	case string(provisioningv1.InstallViaGNOI):
		handlers[provisioningv1.DPUInitializeInterface] = gnoi.InitializeInterface
		handlers[provisioningv1.DPUConfigFWParameters] = gnoi.ConfigFWParameters
		handlers[provisioningv1.DPUHostNetworkConfiguration] = gnoi.SetupNetwork
		handlers[provisioningv1.DPUOSInstalling] = gnoi.Installing
		handlers[provisioningv1.DPUCheckingHostRebootNeed] = gnoi.RebootRequiredCheck
	case string(provisioningv1.InstallViaRedFish):
		handlers[provisioningv1.DPUInitializeInterface] = redfish.InitializeInterface
		handlers[provisioningv1.DPUConfigFWParameters] = redfish.ConfigFWParameters
		handlers[provisioningv1.DPUPrepareBFB] = redfish.PrepareBFB
		handlers[provisioningv1.DPUOSInstalling] = redfish.Installing
	case string(provisioningv1.InstallViaMock):
		handlers[provisioningv1.DPUInitializeInterface] = mock.InitializeInterface
		handlers[provisioningv1.DPUConfigFWParameters] = mock.ConfigFWParameters
		handlers[provisioningv1.DPUHostNetworkConfiguration] = mock.HostNetworkConfiguration
		handlers[provisioningv1.DPUPrepareBFB] = mock.PrepareBFB
		handlers[provisioningv1.DPUOSInstalling] = mock.Installing
		handlers[provisioningv1.DPUCheckingHostRebootNeed] = mock.RebootRequiredCheck
		handlers[provisioningv1.DPUClusterConfig] = mock.ClusterConfig
		handlers[provisioningv1.DPUDeleting] = mock.Deleting
	default:
		panic(fmt.Errorf("unsupported interface %q. Supported: %s,%s",
			options.DPUInstallInterface, provisioningv1.InstallViaGNOI, provisioningv1.InstallViaRedFish))
	}

	return &DPUReconciler{
		ctrlCtx:              ctrlCtx,
		handlers:             handlers,
		DPUInProvisioningMap: dpuMap,
	}
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuflavors,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods;pods/exec;nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;create;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list
// +kubebuilder:rbac:groups="",resources=events,verbs=patch;update;delete;create
// +kubebuilder:rbac:groups=maintenance.nvidia.com,resources=nodemaintenances;nodemaintenances/status,verbs=*
// +kubebuilder:rbac:groups="cert-manager.io",resources=*,verbs=*
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch;update

func (r *DPUReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile")

	dpu := &provisioningv1.DPU{}
	if err := r.ctrlCtx.Client.Get(ctx, req.NamespacedName, dpu); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get DPU %w", err)
	}

	// Add finalizer if not set and DPU is not currently deleting.
	if !controllerutil.ContainsFinalizer(dpu, provisioningv1.DPUFinalizer) && dpu.DeletionTimestamp.IsZero() {
		controllerutil.AddFinalizer(dpu, provisioningv1.DPUFinalizer)
		if err := r.ctrlCtx.Client.Update(ctx, dpu); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add DPU finalizer %w", err)
		}
		return ctrl.Result{}, nil
	}

	// This is to cache the DPUs that are created with the cluster field set in their manifests, such DPUs will not go through the Allocate() procedure in Initialization phase
	// PS: Users are able to create DPUs without DPUSets, which is not officially supported but also not forbidden. If the cluster field is empty, a DPUCluster will be allocated for it as usual.
	r.ctrlCtx.ClusterAllocator.SaveAssignedDPU(dpu)

	h := r.handlers[dpu.Status.Phase]
	if h == nil {
		// Unmatching states indicate that the DPU was provisioned using an old version of provisioning-controller.
		// TODO: delete the DPU and reprovision
		err := fmt.Errorf("unsupported phase %q", dpu.Status.Phase)
		logger.Error(err, err.Error())
		return ctrl.Result{}, err
	}

	// for zero-trust mode, the PCI address is not provided in the DPU object, so we do not need to build the context with the target PCI address
	if dpu.Spec.PCIAddress != nil {
		ctx = cutil.BuildContextWithTargetPCIAddress(ctx, *dpu.Spec.PCIAddress)
	}

	nextState, err := h(ctx, dpu, r.ctrlCtx)
	if err != nil {
		logger.Error(err, "State handle error")
	}
	if !reflect.DeepEqual(dpu.Status, nextState) {
		logger.Info("Update DPU status", "current phase", dpu.Status.Phase, "next phase", nextState.Phase)
		dpu.Status = nextState
		if err := r.ctrlCtx.Client.Status().Update(ctx, dpu); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update DPU %w", err)
		}
	} else if nextState.Phase != provisioningv1.DPUError {
		// TODO: move the state checking in state machine
		logger.Info(fmt.Sprintf("Requeue in %s", cutil.RequeueInterval), "current phase", dpu.Status.Phase)
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
	}

	// If we have an error we have to requeue the DPU and let controller-runtime handle the error.
	return ctrl.Result{}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPU{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.nonInitializedDPU)).
		// Watch DPUNode annotation changes for external reboot method
		Watches(&provisioningv1.DPUNode{}, handler.EnqueueRequestsFromMapFunc(r.dpuNodeToDPU), builder.WithPredicates(predicate.AnnotationChangedPredicate{})).
		Complete(r)
}

func (r *DPUReconciler) nonInitializedDPU(ctx context.Context, obj client.Object) []reconcile.Request {
	var ret []reconcile.Request
	dc := obj.(*provisioningv1.DPUCluster)
	if dc.Status.Phase != provisioningv1.PhaseReady {
		return nil
	}
	dpuList := &provisioningv1.DPUList{}
	if err := r.ctrlCtx.Client.List(ctx, dpuList); err != nil {
		log.FromContext(ctx).Error(fmt.Errorf("failed to list DPUs, err: %v", err), "")
		return nil
	}
	for _, dpu := range dpuList.Items {
		if dpu.Spec.Cluster.Name == "" {
			ret = append(ret, reconcile.Request{NamespacedName: cutil.GetNamespacedName(&dpu)})
		}
	}
	return ret
}

func (r *DPUReconciler) dpuNodeToDPU(ctx context.Context, obj client.Object) []reconcile.Request {
	dpuNode := obj.(*provisioningv1.DPUNode)
	dpuList := provisioningv1.DPUList{}
	if err := r.ctrlCtx.Client.List(ctx, &dpuList,
		client.MatchingLabels{cutil.DPUNodeNameLabel: dpuNode.Name},
		client.InNamespace(dpuNode.Namespace)); err != nil {
		log.FromContext(ctx).Error(fmt.Errorf("failed to list DPUs, err: %v", err), "")
		return nil
	}
	ret := []reconcile.Request{}
	for _, dpu := range dpuList.Items {
		ret = append(ret, reconcile.Request{NamespacedName: cutil.GetNamespacedName(&dpu)})
	}
	return ret
}
