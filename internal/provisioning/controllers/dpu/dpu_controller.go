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
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/hostagent"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/mock"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
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

func NewDPUReconciler(mgr manager.Manager, alloc allocator.Allocator, joinCommandGenerator util.NodeJoinCommandGenerator, artifactGenerator util.DPUArtifactGenerator, hostUptimeChecker reboot.HostUptimeChecker, options util.DPUOptions, dpuMap *util.DPUInProvisioningMap) *DPUReconciler {
	ctrlCtx := &util.ControllerContext{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		Recorder:             mgr.GetEventRecorderFor(DPUControllerName),
		Options:              options,
		ClusterAllocator:     alloc,
		JoinCommandGenerator: joinCommandGenerator,
		DPUArtifactGenerator: artifactGenerator,
		HostUptimeChecker:    hostUptimeChecker,
		DPUInProvisioningMap: dpuMap,
	}
	handlers := map[provisioningv1.DPUPhase]PhaseHandlerFunc{
		"":                                  state.Initializing,
		provisioningv1.DPUInitializing:      state.Initializing,
		provisioningv1.DPUPending:           state.Pending,
		provisioningv1.DPUNodeEffect:        state.NodeEffect,
		provisioningv1.DPUPrepareBFB:        state.PrepareBFB,
		provisioningv1.DPUConfig:            state.DPUConfig,
		provisioningv1.DPUClusterConfig:     state.ClusterConfig,
		provisioningv1.DPUNodeEffectRemoval: state.NodeEffectRemoval,
		provisioningv1.DPUReady:             state.Ready,
		provisioningv1.DPUDeleting:          state.Deleting,
		provisioningv1.DPUError:             state.Error,
	}
	switch options.DPUInstallInterface {
	case string(provisioningv1.InstallViaGNOI), string(provisioningv1.InstallViaHostAgent):
		handlers[provisioningv1.DPUInitializeInterface] = hostagent.InitializeInterface
		handlers[provisioningv1.DPUConfigFWParameters] = hostagent.ConfigFWParameters
		handlers[provisioningv1.DPUHostNetworkConfiguration] = hostagent.SetupNetwork
		handlers[provisioningv1.DPUOSInstalling] = hostagent.Installing
		handlers[provisioningv1.DPURebooting] = hostagent.Rebooting
	case string(provisioningv1.InstallViaRedFish):
		handlers[provisioningv1.DPUInitializeInterface] = redfish.InitializeInterface
		handlers[provisioningv1.DPUConfigFWParameters] = redfish.ConfigFWParameters
		handlers[provisioningv1.DPUOSInstalling] = redfish.Installing
		handlers[provisioningv1.DPUPerformArmForceRestart] = redfish.PerformArmForceRestart
		handlers[provisioningv1.DPURebooting] = redfish.Rebooting
	case string(provisioningv1.InstallViaMock):
		handlers[provisioningv1.DPUInitializeInterface] = mock.InitializeInterface
		handlers[provisioningv1.DPUConfigFWParameters] = mock.ConfigFWParameters
		handlers[provisioningv1.DPUHostNetworkConfiguration] = mock.HostNetworkConfiguration
		handlers[provisioningv1.DPUPrepareBFB] = mock.PrepareBFB
		handlers[provisioningv1.DPUOSInstalling] = mock.Installing
		handlers[provisioningv1.DPUClusterConfig] = mock.ClusterConfig
		handlers[provisioningv1.DPUNodeEffectRemoval] = mock.NodeEffectRemoval
		handlers[provisioningv1.DPUDeleting] = mock.Deleting
		handlers[provisioningv1.DPURebooting] = mock.Rebooting
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
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods;nodes;services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;create;delete;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list
// +kubebuilder:rbac:groups="",resources=events,verbs=patch;update;delete;create
// +kubebuilder:rbac:groups=maintenance.nvidia.com,resources=nodemaintenances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=maintenance.nvidia.com,resources=nodemaintenances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificaterequests;certificates;issuers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodemaintenances,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;create;delete
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;delete

func (r *DPUReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile")

	if req.Name == bfbregistry.PodName {
		if err := r.reconcileBFBRegistry(ctx, req.NamespacedName.Namespace); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconcile bfb-registry: %w", err)
		}
		return ctrl.Result{}, nil
	}

	dpu := &provisioningv1.DPU{}
	if err := r.ctrlCtx.Client.Get(ctx, req.NamespacedName, dpu); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get DPU %w", err)
	}

	patcher := patch.NewSerialPatcher(dpu, r.ctrlCtx.Client)
	defer func() {
		logger.Info("Patching")
		if err := patcher.Patch(ctx, dpu,
			patch.WithFieldOwner(DPUControllerName),
			patch.WithStatusObservedGeneration{},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Add finalizer if not set and DPU is not currently deleting.
	if !controllerutil.ContainsFinalizer(dpu, provisioningv1.DPUFinalizer) && dpu.DeletionTimestamp.IsZero() {
		controllerutil.AddFinalizer(dpu, provisioningv1.DPUFinalizer)
		return ctrl.Result{}, nil
	}

	// Add DpuDevice finalizer to prevent DpuDevice deletion while DPU is using it
	if dpu.DeletionTimestamp.IsZero() {
		if err := r.addDpuDeviceFinalizer(ctx, dpu); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add DpuDevice finalizer %w", err)
		}
	}

	// This is to cache the DPUs that are created with the cluster field set in their manifests, such DPUs will not go through the Allocate() procedure in Initialization phase
	// PS: Users are able to create DPUs without DPUSets, which is not officially supported but also not forbidden. If the cluster field is empty, a DPUCluster will be allocated for it as usual.
	r.ctrlCtx.ClusterAllocator.SaveAssignedDPU(dpu)

	if err := r.UpdateDPUNodeMaintenanceRequestors(ctx, dpu, r.ctrlCtx.Client); err != nil {
		// Return error to trigger requeue with backoff
		return ctrl.Result{}, fmt.Errorf("failed to update DPUNodeMaintenanceRequestors: %w", err)
	}

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

	deploymentMode := provisioningv1.DeploymentMode(r.ctrlCtx.Options.DeploymentMode)
	dpfOperatorConfig, cfgErr := dpfutils.GetDPFOperatorConfig(ctx, r.ctrlCtx.Client)
	if cfgErr != nil {
		logger.Error(cfgErr, "failed to read DPFOperatorConfig, falling back to controller option deployment mode")
	} else {
		deploymentMode = provisioningv1.DeploymentMode(dpfOperatorConfig.Spec.DeploymentMode)
	}
	nextState.DeploymentMode = deploymentMode

	if UpdateDPUStatus(dpu, nextState) {
		logger.Info("DPU phase changed", "from", dpu.Status.PreviousPhase, "to", dpu.Status.Phase)
	}
	if nextState.Phase != provisioningv1.DPUError {
		// TODO: move the state checking in state machine
		logger.Info(fmt.Sprintf("Requeue in %s", cutil.RequeueInterval), "current phase", dpu.Status.Phase)
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
	}

	// If we have an error we have to requeue the DPU and let controller-runtime handle the error.
	return ctrl.Result{}, err
}

// UpdateDPUStatus updates only dpu.Status when next differs from the current status (DeepEqual).
// Returns false without mutating status when unchanged.
// Returns true only when Phase changes after applying next; still mutates status when other fields
// differ so the deferred patch persists condition-only updates.
func UpdateDPUStatus(dpu *provisioningv1.DPU, next provisioningv1.DPUStatus) bool {
	if reflect.DeepEqual(dpu.Status, next) {
		return false
	}
	before := dpu.Status
	phaseChanged := before.Phase != next.Phase
	if next.Phase != before.Phase && before.Phase != "" {
		next.PreviousPhase = before.Phase
	}
	dpu.Status = next
	return phaseChanged
}

// addDpuDeviceFinalizer adds the DpuDevice finalizer to prevent deletion while DPU is using it
func (r *DPUReconciler) addDpuDeviceFinalizer(ctx context.Context, dpu *provisioningv1.DPU) error {
	dpuDevice := &provisioningv1.DPUDevice{}
	if err := r.ctrlCtx.Client.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		if apierrors.IsNotFound(err) {
			// DpuDevice not found, this is expected in some cases
			return nil
		}
		return fmt.Errorf("failed to get DpuDevice %s: %w", dpu.Spec.DPUDeviceName, err)
	}

	if !controllerutil.ContainsFinalizer(dpuDevice, provisioningv1.DPUDeviceFinalizer) {
		controllerutil.AddFinalizer(dpuDevice, provisioningv1.DPUDeviceFinalizer)
		if err := r.ctrlCtx.Client.Update(ctx, dpuDevice); err != nil {
			return fmt.Errorf("failed to add DpuDevice finalizer: %w", err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPU{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.nonInitializedDPU)).
		// Watch DPUNode annotation changes for external reboot method
		Watches(&provisioningv1.DPUNode{}, handler.EnqueueRequestsFromMapFunc(r.dpuNodeToDPU), builder.WithPredicates(predicate.AnnotationChangedPredicate{})).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.bfbRegistryPodToRequest),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.isBFBRegistryPod))).
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
		client.MatchingLabels{provisioningv1.DPUNodeNameLabel: dpuNode.Name},
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

func (r *DPUReconciler) isBFBRegistryPod(obj client.Object) bool {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return false
	}
	return pod.Labels[bfbregistry.LabelDPUComponent] == bfbregistry.LabelValue
}

func (r *DPUReconciler) bfbRegistryPodToRequest(ctx context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil
	}
	log.FromContext(ctx).Info("Mapping bfb-registry Pod to reconcile request", "pod", pod.Name, "namespace", pod.Namespace)
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: bfbregistry.PodName}},
	}
}

// reconcileBFBRegistry ensures the bfb-registry pod and service exist in the given namespace.
func (r *DPUReconciler) reconcileBFBRegistry(ctx context.Context, namespace string) error {
	logger := log.FromContext(ctx)
	podName := os.Getenv("POD_NAME")
	nodeName := os.Getenv("NODE_NAME")
	registryImage := os.Getenv("BFB_REGISTRY_IMAGE")
	if podName == "" || nodeName == "" || registryImage == "" {
		logger.V(4).Info("bfb-registry reconcile skipping: required env not set (POD_NAME, NODE_NAME, BFB_REGISTRY_IMAGE)")
		return nil
	}
	if err := bfbregistry.EnsureBFBRegistry(ctx, bfbregistry.EnsureBFBRegistryDeps{
		Client:           r.ctrlCtx.Client,
		BFBPVC:           r.ctrlCtx.Options.BFBPVC,
		ImagePullSecrets: r.ctrlCtx.Options.ImagePullSecrets,
	}, namespace, podName, nodeName, registryImage); err != nil {
		return fmt.Errorf("ensure bfb-registry: %w", err)
	}
	return nil
}

func (r *DPUReconciler) UpdateDPUNodeMaintenanceRequestors(ctx context.Context, dpu *provisioningv1.DPU, client client.Client) error {
	logger := log.FromContext(ctx)
	dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
	if err != nil {
		return err
	}
	dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
	key := types.NamespacedName{Namespace: dpu.Namespace, Name: dpunodemaintenanceName}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := client.Get(ctx, key, dpunodemaintenance); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
			// If DPUNodeMaintenance object doesn't exist, return nil
			return nil
		}

		lastAppliedAdditionalRequestorsOnDPUKey := cutil.GenerateLastAppliedAdditionalRequestorsOnDPUAnnotationKey(dpu.Name)

		lastAppliedRequestorsStr, ok := dpunodemaintenance.Annotations[lastAppliedAdditionalRequestorsOnDPUKey]
		if !ok {
			// wait for node effect action to be completed
			return nil
		}
		// the lastAppliedRequestors is the service additional requestors
		var lastAppliedRequestors []string
		if err := json.Unmarshal([]byte(lastAppliedRequestorsStr), &lastAppliedRequestors); err != nil {
			return fmt.Errorf("failed to unmarshal last applied node maintenance additional requestors on DPU: %w", err)
		}

		expectedRequestors := dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors
		sort.Strings(expectedRequestors)
		sort.Strings(lastAppliedRequestors)
		// if lastAppliedRequestors is equal to expectedRequestors, return nil
		if slices.Equal(lastAppliedRequestors, expectedRequestors) {
			return nil
		}

		// update the requestors for dpunodemaintenance CR
		dpuRequestors := findOutDPURequestors(lastAppliedRequestors, dpunodemaintenance.Spec.Requestor)
		logger.V(4).Info(fmt.Sprintf("DPU requestors: %v", dpuRequestors))

		// update the LastAppliedNodeMaintenanceAdditionalRequestorsOnDPUKey annotation
		jsonStr, err := json.Marshal(expectedRequestors)
		if err != nil {
			return fmt.Errorf("failed to marshal expected requestors: %w", err)
		}
		dpunodemaintenance.Annotations[lastAppliedAdditionalRequestorsOnDPUKey] = string(jsonStr)

		// update the Requestor field
		expectedRequestors = append(expectedRequestors, dpuRequestors...)
		dpunodemaintenance.Spec.Requestor = expectedRequestors

		logger.Info(fmt.Sprintf("Updating NodeMaintenanceAdditionalRequestors: %v for DPUNodeMaintenance (%s/%s)", expectedRequestors, dpunodemaintenance.Namespace, dpunodemaintenance.Name))
		return client.Update(ctx, dpunodemaintenance)
	})
}

// lastAppliedRequestors is only used to store the service additional requestors for this DPU
// the requestors in the currentRequestors but not in the lastAppliedRequestors are the DPU requestors and the service requestors from other DPUs
func findOutDPURequestors(lastAppliedRequestors []string, currentRequestors []string) []string {
	var dpuRequestors []string
	for _, req := range currentRequestors {
		if !slices.Contains(lastAppliedRequestors, req) {
			dpuRequestors = append(dpuRequestors, req)
		}
	}
	return dpuRequestors
}
