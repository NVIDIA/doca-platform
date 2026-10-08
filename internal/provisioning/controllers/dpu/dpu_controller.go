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
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/hostagent"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/mock"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"
	dpucluster "github.com/nvidia/doca-platform/pkg/dpucluster"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type PhaseHandlerFunc func(context.Context, *provisioningv1.DPU, *util.ControllerContext) (provisioningv1.DPUStatus, error)

const (
	// DPUControllerName is used when reporting events.
	DPUControllerName = "dpu"

	// ClusterNodeResyncPeriod is how often the DPUCluster Node cache relists.
	// Ten hours is controller-runtime's default cache sync period.
	ClusterNodeResyncPeriod = 10 * time.Hour

	// dpuNameField indexes DPU metadata.name so a Node event lists only DPUs with that name.
	dpuNameField = "dpu.metadata.name"
)

// DPUReconciler reconciles a DPU object
type DPUReconciler struct {
	ctrlCtx              *util.ControllerContext
	handlers             map[provisioningv1.DPUPhase]PhaseHandlerFunc
	DPUInProvisioningMap *util.DPUInProvisioningMap
	controller           controller.Controller
}

func NewDPUReconciler(mgr manager.Manager, alloc allocator.Allocator, joinCommandGenerator util.NodeJoinCommandGenerator, artifactGenerator util.DPUArtifactGenerator, options util.DPUOptions, dpuMap *util.DPUInProvisioningMap) *DPUReconciler {
	ctrlCtx := &util.ControllerContext{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		Recorder:             mgr.GetEventRecorderFor(DPUControllerName),
		Options:              options,
		ClusterAllocator:     alloc,
		JoinCommandGenerator: joinCommandGenerator,
		DPUArtifactGenerator: artifactGenerator,
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
		provisioningv1.DPUServiceReadiness:  state.ServiceReadiness,
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
		handlers[provisioningv1.DPUUpdateFirmware] = redfish.FirmwareUpdate
	case string(provisioningv1.InstallViaMock):
		handlers[provisioningv1.DPUInitializeInterface] = mock.InitializeInterface
		handlers[provisioningv1.DPUConfigFWParameters] = mock.ConfigFWParameters
		handlers[provisioningv1.DPUHostNetworkConfiguration] = mock.HostNetworkConfiguration
		handlers[provisioningv1.DPUPrepareBFB] = mock.PrepareBFB
		handlers[provisioningv1.DPUOSInstalling] = mock.Installing
		handlers[provisioningv1.DPUClusterConfig] = mock.ClusterConfig
		handlers[provisioningv1.DPUServiceReadiness] = mock.ServiceReadiness
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
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuflavors,verbs=get;list;watch;update;patch;delete
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
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
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

	// Claim the resources the DPU references while alive (DpuDevice finalizer, and in template
	// mode the generated DPUFlavor's ownerRef + finalizer).
	if dpu.DeletionTimestamp.IsZero() {
		if err := r.claimReferencedResources(ctx, dpu); err != nil {
			return ctrl.Result{}, err
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

	// Read the DPFOperatorConfig once per reconcile and hand it to the phase handler through a copy
	// of the controller context.
	// A failed read must not be mistaken for an absent config: the phase handlers gate provisioning
	// on it, so a nil config would silently skip the upgrade hold. Only an actually absent config
	// falls back to the controller options, so DPUs can still be reconciled while the
	// DPFOperatorConfig is gone.
	dpfOperatorConfig, cfgErr := dpfutils.GetDPFOperatorConfig(ctx, r.ctrlCtx.Client)
	if cfgErr != nil {
		if !errors.Is(cfgErr, dpfutils.ErrDPFOperatorConfigNotFound) {
			return ctrl.Result{}, fmt.Errorf("failed to read DPFOperatorConfig: %w", cfgErr)
		}
		logger.Info("No DPFOperatorConfig exists, falling back to the controller options")
	}
	ctrlCtx := *r.ctrlCtx
	ctrlCtx.DPFOperatorConfig = dpfOperatorConfig

	nextState, err := h(ctx, dpu, &ctrlCtx)
	if err != nil {
		logger.Error(err, "State handle error")
	}

	// Mirror the render-failed annotation (set by the DPUSet controller) into the
	// DPUFlavorRendered condition in any phase. This is phase-agnostic for the update case: a
	// post-Pending edit to the template/values that fails to re-render is recorded without
	// disrupting the running DPU, and this condition is its only surface. Status only; self-heals
	// when a later successful render clears the annotation.
	setDPUFlavorRenderedCondition(dpu, &nextState)

	// The DPU agent reports unrecoverable failures through the Error condition. Acting on it here
	// keeps phase transitions owned by this controller, independent of the phase the DPU is in.
	setErrorPhaseFromCondition(dpu, &nextState)

	deploymentMode := provisioningv1.DeploymentMode(r.ctrlCtx.Options.DeploymentMode)
	if dpfOperatorConfig != nil {
		deploymentMode = provisioningv1.DeploymentMode(dpfOperatorConfig.Spec.DeploymentMode)
	}
	nextState.DeploymentMode = deploymentMode

	// Capture the phase this reconcile handled. UpdateDPUStatus replaces dpu.Status.
	phase := dpu.Status.Phase
	if UpdateDPUStatus(dpu, nextState) {
		logger.Info("DPU phase changed", "from", dpu.Status.PreviousPhase, "to", dpu.Status.Phase)
	}
	if StayReadyWithoutRequeue(phase, nextState.Phase) {
		// A DPU that is already Ready and stays Ready is not polled on the provisioning
		// interval. DPU update and delete events enqueue it, as does a relevant Node
		// change from the DPUCluster cache. A handler error uses the same fixed interval
		// as the other phases. Returning the error would use controller-runtime backoff
		// and stretch the retry toward 16 minutes.
		if err != nil {
			logger.Info("DPU is Ready but the handler failed; requeueing", "error", err.Error(), "interval", cutil.RequeueInterval)
			return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
		}
		logger.V(1).Info("DPU is Ready; waiting for an event before reconciling again")
		return ctrl.Result{}, nil
	}
	if nextState.Phase != provisioningv1.DPUError {
		// TODO: move the state checking in state machine
		logger.Info(fmt.Sprintf("Requeue in %s", cutil.RequeueInterval), "current phase", dpu.Status.Phase)
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
	}

	// If we have an error we have to requeue the DPU and let controller-runtime handle the error.
	return ctrl.Result{}, err
}

// StayReadyWithoutRequeue reports that this reconcile handled a Ready DPU which is
// still Ready, so the interval requeue should be skipped. Leaving Ready, or entering
// it from another phase, still requeues.
func StayReadyWithoutRequeue(phase, next provisioningv1.DPUPhase) bool {
	return phase == provisioningv1.DPUReady && next == provisioningv1.DPUReady
}

// setDPUFlavorRenderedCondition mirrors the DPUFlavorTemplate render status into the
// DPUFlavorRendered condition for template-mode DPUs, in any phase. It is a no-op for
// non-template DPUs. When the DPUSet controller recorded a render-failure annotation it sets
// the condition False (with the failure reason/message); otherwise it sets it True. It is purely
// a status surface: it never changes the phase or touches the generated DPUFlavor, so an
// update-time failure stays non-disruptive, and the condition self-heals once the annotation is
// cleared after a successful render.
func setDPUFlavorRenderedCondition(dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus) {
	if dpu.Labels[cutil.DPUFlavorTemplateNameLabel] == "" {
		return
	}
	if reason, ok := dpu.Annotations[cutil.RenderFailedReasonAnnotation]; ok {
		message := dpu.Annotations[cutil.RenderFailedMessageAnnotation]
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUFlavorRendered.String(), errors.New(message), reason, message))
		return
	}
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondDPUFlavorRendered, "", ""))
}

// setErrorPhaseFromCondition moves the DPU to the Error phase while the Error condition is True.
// The DPU agent reports unrecoverable failures by setting that condition, and never writes the
// phase itself, so this is where such a report is turned into a phase transition.
// A deleting DPU is left alone: the condition is never cleared, so forcing the Error phase here
// would take the DPU out of the Deleting phase and stall its deletion.
func setErrorPhaseFromCondition(dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus) {
	if !dpu.DeletionTimestamp.IsZero() {
		return
	}
	if _, condition := cutil.GetDPUCondition(state, provisioningv1.DPUCondError.String()); condition != nil && condition.Status == metav1.ConditionTrue {
		state.Phase = provisioningv1.DPUError
	}
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

// claimReferencedResources adds the finalizers/ownership the DPU holds on objects it references
// while it is alive: the DpuDevice finalizer, and (template mode) the ownerReference + protective
// finalizer on the generated DPUFlavor. Each step is idempotent and self-heals on later reconciles.
// The DPU is the single controller that both adds these (here) and releases them on deletion.
func (r *DPUReconciler) claimReferencedResources(ctx context.Context, dpu *provisioningv1.DPU) error {
	if err := r.addDpuDeviceFinalizer(ctx, dpu); err != nil {
		return fmt.Errorf("failed to add DpuDevice finalizer: %w", err)
	}
	if err := r.adoptGeneratedFlavor(ctx, dpu); err != nil {
		return fmt.Errorf("failed to adopt generated DPUFlavor: %w", err)
	}
	return nil
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

// adoptGeneratedFlavor makes the generated DPUFlavor of a template-mode DPU owned by that DPU by
// setting the controller ownerReference (for garbage collection) and the protective finalizer in a
// single patch. It is idempotent (the write is skipped once both are present) and runs on every
// reconcile, so a missed claim self-heals.
//
// It is guarded so it only ever touches a generated flavor: it returns early for non-template DPUs
// and for a flavor lacking the generated-by label. This matters because the DPU controller
// reconciles static-flavor DPUs too, whose dpu.Spec.DPUFlavor points at a shared, user-authored
// DPUFlavor that must never be adopted or finalized. A missing flavor (render-failed/blocked DPU,
// or an in-flight reprovision) is tolerated as a no-op.
func (r *DPUReconciler) adoptGeneratedFlavor(ctx context.Context, dpu *provisioningv1.DPU) error {
	if dpu.Labels[cutil.DPUFlavorTemplateNameLabel] == "" {
		return nil
	}
	flavor := &provisioningv1.DPUFlavor{}
	if err := r.ctrlCtx.Client.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Spec.DPUFlavor}, flavor); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get generated DPUFlavor %s: %w", dpu.Spec.DPUFlavor, err)
	}
	if flavor.Labels[cutil.GeneratedByLabel] != cutil.GeneratedByDPUFlavorTemplate {
		return nil
	}
	// Runs every reconcile; skip the write once this DPU owns the flavor and the finalizer is present.
	// Checking both (set together below) self-heals if either is dropped, and avoids a no-op
	// optimistic-lock patch that could conflict needlessly.
	if metav1.IsControlledBy(flavor, dpu) && controllerutil.ContainsFinalizer(flavor, cutil.GeneratedDPUFlavorFinalizer) {
		return nil
	}
	base := flavor.DeepCopy()
	// SetControllerReference derives the GVK from the scheme and preserves any non-controller owner
	// references, rather than overwriting the whole slice with a hard-coded ref.
	if err := controllerutil.SetControllerReference(dpu, flavor, r.ctrlCtx.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on generated DPUFlavor %s: %w", dpu.Spec.DPUFlavor, err)
	}
	controllerutil.AddFinalizer(flavor, cutil.GeneratedDPUFlavorFinalizer)
	if err := r.ctrlCtx.Client.Patch(ctx, flavor, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("failed to adopt generated DPUFlavor %s: %w", dpu.Spec.DPUFlavor, err)
	}
	return nil
}

// SetupIndexers registers the DPU name index used to map a DPU-cluster Node to its DPU.
func SetupIndexers(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &provisioningv1.DPU{}, dpuNameField, func(obj client.Object) []string {
		return []string{obj.GetName()}
	}); err != nil {
		return fmt.Errorf("failed to register indexer for DPU name: %w", err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUReconciler) SetupWithManager(mgr ctrl.Manager) error {
	maxConcurrentReconciles := r.ctrlCtx.Options.DPUMaxConcurrentReconciles
	if maxConcurrentReconciles < 1 {
		maxConcurrentReconciles = util.DefaultDPUMaxConcurrentReconciles
	}
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPU{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.nonInitializedDPU)).
		// Watch DPUNode annotation changes for external reboot method
		Watches(&provisioningv1.DPUNode{}, handler.EnqueueRequestsFromMapFunc(r.dpuNodeToDPU), builder.WithPredicates(predicate.AnnotationChangedPredicate{})).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.bfbRegistryPodToRequest),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.isBFBRegistryPod))).
		Watches(&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(r.bfbRegistryServiceToRequest),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.isBFBRegistryService))).
		WithOptions(controller.Options{MaxConcurrentReconciles: int(maxConcurrentReconciles)}).
		Build(r)
	if err != nil {
		return err
	}
	r.controller = c
	return nil
}

// WatchDPUClusterNodes watches Nodes in one DPUCluster through the process-wide remote cache.
// Node events enqueue the Ready DPU on this controller. A dropped connection enqueues every
// Ready DPU in that cluster so state.Ready records that the Node could not be read.
func (r *DPUReconciler) WatchDPUClusterNodes(_ context.Context, _ client.Client, cluster client.ObjectKey) (dpucluster.Watcher, error) {
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:    "dpu-watch-cluster-nodes",
		Watcher: r.controller,
		Kind:    &corev1.Node{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return readyDPURequests(ctx, r.ctrlCtx.Client, cluster, obj.GetName())
		}),
		DisconnectHandler: func(ctx context.Context, dropped client.ObjectKey) []reconcile.Request {
			return readyDPUsOnDisconnect(ctx, r.ctrlCtx.Client, dropped)
		},
		Predicates: []predicate.Predicate{clusterNodePredicate()},
	}), nil
}

// clusterNodePredicate passes a Node add or delete through. An update is passed
// through when it changes a field state.Ready records.
func clusterNodePredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return NodeChangeAffectsDPU(nodeFromObject(e.ObjectOld), nodeFromObject(e.ObjectNew))
		},
	}
}

// readyDPUsOnDisconnect lists Ready DPUs whose spec.cluster is the DPUCluster that dropped.
// The list is served from the management-cluster cache. The remote cache client is already gone.
// A failure is logged and no DPUs are enqueued.
func readyDPUsOnDisconnect(ctx context.Context, c client.Client, cluster types.NamespacedName) []reconcile.Request {
	list := &provisioningv1.DPUList{}
	if err := c.List(ctx, list); err != nil {
		log.FromContext(ctx).Error(err, "failed to list Ready DPUs for a disconnected DPU cluster", "cluster", cluster)
		return nil
	}
	var requests []reconcile.Request
	for _, dpu := range ReadyDPUsForCluster(list.Items, cluster) {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(dpu)})
	}
	return requests
}

// readyDPURequests lists DPUs named like the Node and returns the Ready one in cluster.
// The list is served from the management-cluster cache. A failure is logged and the Node event is dropped.
func readyDPURequests(ctx context.Context, c client.Client, cluster types.NamespacedName, nodeName string) []reconcile.Request {
	if nodeName == "" {
		return nil
	}
	list := &provisioningv1.DPUList{}
	if err := c.List(ctx, list, client.MatchingFields{dpuNameField: nodeName}); err != nil {
		log.FromContext(ctx).Error(err, "failed to list DPUs for a DPU-cluster Node", "node", nodeName)
		return nil
	}
	var requests []reconcile.Request
	for _, dpu := range DPUsForClusterNode(list.Items, cluster, nodeName) {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(dpu)})
	}
	return requests
}

// NodeChangeAffectsDPU reports whether a remote Node event can change what
// state.Ready records: the Ready condition, addresses, or the last-applied
// label and annotation metadata. A resync delivers the same resourceVersion
// and is always accepted so a dropped watch is repaired.
func NodeChangeAffectsDPU(oldNode, newNode *corev1.Node) bool {
	if oldNode == nil || newNode == nil {
		return true
	}
	if oldNode.ResourceVersion == newNode.ResourceVersion {
		return true
	}
	if nodeReadyStatus(oldNode) != nodeReadyStatus(newNode) {
		log.Log.V(4).Info("DPU-cluster Node ready status changed", "node", newNode.Name, "from", nodeReadyStatus(oldNode), "to", nodeReadyStatus(newNode))
		return true
	}
	if !slices.Equal(oldNode.Status.Addresses, newNode.Status.Addresses) {
		log.Log.V(4).Info("DPU-cluster Node addresses changed", "node", newNode.Name, "from", oldNode.Status.Addresses, "to", newNode.Status.Addresses)
		return true
	}
	if annotation(oldNode, cutil.LastAppliedLabelsOnDPUKey) != annotation(newNode, cutil.LastAppliedLabelsOnDPUKey) {
		log.Log.V(4).Info("DPU-cluster Node last-applied labels changed", "node", newNode.Name)
		return true
	}
	if annotation(oldNode, cutil.LastAppliedAnnotationsOnDPUKey) != annotation(newNode, cutil.LastAppliedAnnotationsOnDPUKey) {
		log.Log.V(4).Info("DPU-cluster Node last-applied annotations changed", "node", newNode.Name)
		return true
	}
	return false
}

// ReadyDPUsForCluster returns stub Ready DPUs whose spec.cluster points at cluster.
// Only name and namespace are set. Other phases already requeue on their own.
func ReadyDPUsForCluster(dpus []provisioningv1.DPU, cluster types.NamespacedName) []*provisioningv1.DPU {
	var matches []*provisioningv1.DPU
	for i := range dpus {
		dpu := &dpus[i]
		if dpu.Status.Phase != provisioningv1.DPUReady {
			continue
		}
		if dpu.Spec.Cluster.Name != cluster.Name || dpu.Spec.Cluster.Namespace != cluster.Namespace {
			continue
		}
		stub := &provisioningv1.DPU{}
		stub.Name = dpu.Name
		stub.Namespace = dpu.Namespace
		matches = append(matches, stub)
	}
	return matches
}

// DPUsForClusterNode returns stub Ready DPUs whose name is the remote Node name
// and whose spec.cluster points at cluster. Only name and namespace are set.
// Other phases already requeue on their own and do not need this watch.
func DPUsForClusterNode(dpus []provisioningv1.DPU, cluster types.NamespacedName, nodeName string) []*provisioningv1.DPU {
	var matches []*provisioningv1.DPU
	for i := range dpus {
		dpu := &dpus[i]
		if dpu.Status.Phase != provisioningv1.DPUReady {
			continue
		}
		if dpu.Name != nodeName {
			continue
		}
		if dpu.Spec.Cluster.Name != cluster.Name || dpu.Spec.Cluster.Namespace != cluster.Namespace {
			continue
		}
		stub := &provisioningv1.DPU{}
		stub.Name = dpu.Name
		stub.Namespace = dpu.Namespace
		matches = append(matches, stub)
	}
	return matches
}

// nodeFromObject returns the Node carried by a controller-runtime event.
func nodeFromObject(obj client.Object) *corev1.Node {
	node, _ := obj.(*corev1.Node)
	return node
}

// nodeReadyStatus returns the NodeReady condition status, or empty when the condition is absent.
func nodeReadyStatus(node *corev1.Node) corev1.ConditionStatus {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status
		}
	}
	return ""
}

// annotation returns one annotation value, or empty when the Node has no annotations.
func annotation(node *corev1.Node, key string) string {
	if node.Annotations == nil {
		return ""
	}
	return node.Annotations[key]
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

func (r *DPUReconciler) isBFBRegistryService(obj client.Object) bool {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return false
	}
	return svc.Name == bfbregistry.PodName
}

func (r *DPUReconciler) bfbRegistryServiceToRequest(ctx context.Context, obj client.Object) []reconcile.Request {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return nil
	}
	log.FromContext(ctx).Info("Mapping bfb-registry Service to reconcile request", "service", svc.Name, "namespace", svc.Namespace)
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Namespace: svc.Namespace, Name: bfbregistry.PodName}},
	}
}

// reconcileBFBRegistry ensures the bfb-registry pod and service exist in the given namespace.
func (r *DPUReconciler) reconcileBFBRegistry(ctx context.Context, namespace string) error {
	logger := log.FromContext(ctx)
	podName := os.Getenv("POD_NAME")
	nodeName := os.Getenv("NODE_NAME")
	nodeIP := os.Getenv("NODE_IP")
	registryImage := os.Getenv("BFB_REGISTRY_IMAGE")
	if podName == "" || nodeName == "" || registryImage == "" {
		logger.V(4).Info("bfb-registry reconcile skipping: required env not set (POD_NAME, NODE_NAME, BFB_REGISTRY_IMAGE)")
		return nil
	}
	if err := bfbregistry.EnsureBFBRegistry(ctx, bfbregistry.EnsureBFBRegistryDeps{
		Client:                 r.ctrlCtx.Client,
		BFBPVC:                 r.ctrlCtx.Options.BFBPVC,
		ImagePullSecrets:       r.ctrlCtx.Options.ImagePullSecrets,
		KubernetesAPIServerVIP: r.ctrlCtx.Options.KubernetesAPIServerVIP,
	}, namespace, podName, nodeName, nodeIP, registryImage); err != nil {
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
