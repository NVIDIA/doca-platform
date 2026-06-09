/*
Copyright 2025 NVIDIA

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

package dpunode

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"

	"github.com/fluxcd/pkg/runtime/patch"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
)

const (
	// controller name that will be used when
	DPUNodeControllerName              = "dpunode"
	PodTemplateConfigMapKey     string = "pod-template"
	PodInfoVolumeName           string = "dpf-pod-info"
	PodInfoMountPath            string = "/etc/dpf-pod-info"
	PodInfoLabelsPath           string = "labels"
	PodInfoAnnotationsPath      string = "annotations"
	PodInfoLabelsFieldPath      string = "metadata.labels"
	PodInfoAnnotationsFieldPath string = "metadata.annotations"
	DPUNodeNameEnvVar           string = "DPUNODE_NAME"

	// DPUNodeRebootMethodEnvVar holds the aggregated host reboot method
	// (highest-priority winner across DPUs in DPURebooting phase), injected
	// into custom-script reboot pods. Values mirror provisioningv1.RebootMethodType,
	// with SystemLevelReset substituted when no DPU has reported yet (or all
	// report Unknown) so the script always has an actionable signal. Mirrors
	// DPUNode.Status.RebootMethod.
	DPUNodeRebootMethodEnvVar string = "DPUNODE_REBOOT_METHOD"
	// DPUNodeRebootMethodsPerDPUEnvVar is the env var name injected into
	// custom-script reboot pods that holds a comma-separated <dpu-name>=<method>
	// mapping for every DPU associated with the DPUNode, sorted by DPU name.
	// DPUs that have not reported a RebootMethod yet appear as "<dpu-name>=Unknown".
	DPUNodeRebootMethodsPerDPUEnvVar string = "DPUNODE_REBOOT_METHODS_PER_DPU"
	// DPUNodeRebootMethodAnnotation mirrors DPUNodeRebootMethodEnvVar as a
	// pod-template annotation, exposed inside the pod through the existing
	// dpf-pod-info downward-API mount at /etc/dpf-pod-info/annotations.
	DPUNodeRebootMethodAnnotation string = cutil.DPUProvisioningPrefix + "reboot-method-aggregated"
	// DPUNodeRebootMethodsPerDPUAnnotation mirrors
	// DPUNodeRebootMethodsPerDPUEnvVar as a pod-template annotation.
	DPUNodeRebootMethodsPerDPUAnnotation string = cutil.DPUProvisioningPrefix + "reboot-methods-per-dpu"
	reasonExternalRebootWaitManual       string = "WaitingForManualPowerCycleOrReboot"
)

// DPUNodeReconciler reconciles a DPUNode object
type DPUNodeReconciler struct {
	client.Client
	DPUInstallInterface *string
	// Options are the Options used to configure the DMS Pods created by the controller.
	Options dnutil.HostAgentPodOptions
	// Recorder is an event recorder that is used to record events that occur during the execution of the controller.
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;create;delete;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

func (r *DPUNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := log.FromContext(ctx)
	log.Info("Reconcile")

	dpuNode := &provisioningv1.DPUNode{}
	if err := r.Get(ctx, req.NamespacedName, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("Reconcile", "DPUNode", dpuNode.Status.Conditions)
	patcher := patch.NewSerialPatcher(dpuNode, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		err := patcher.Patch(ctx, dpuNode,
			patch.WithFieldOwner(DPUNodeControllerName),
			patch.WithStatusObservedGeneration{},
		)
		// Ignore NotFound errors (including nested in aggregates) as the object may have been deleted after finalizer removal
		if err != nil && !containsNotFoundError(err) {
			log.Error(err, "Failed to patch DPUNode")
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion and finalizer setup
	if err := r.handleDeletionAndFinalizer(ctx, dpuNode); err != nil {
		return ctrl.Result{}, err
	}

	// TODO: once KubeNodeRef is moved from Status to Spec, change this to check Spec.KubeNodeRef
	var nodeRef *metav1.OwnerReference
	for i := range dpuNode.ObjectMeta.OwnerReferences {
		if dpuNode.ObjectMeta.OwnerReferences[i].Kind == "Node" {
			nodeRef = &dpuNode.ObjectMeta.OwnerReferences[i]
			break
		}
	}

	if nodeRef != nil {
		node := &corev1.Node{}
		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: "", Name: nodeRef.Name}, node); err != nil {
			if apierrors.IsNotFound(err) {
				if err := r.Client.Delete(ctx, dpuNode); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		// update DPUNode status - KubeNodeRef
		dpuNode.Status.KubeNodeRef = &node.Name

		// Copy labels and annotations from node to dpuNode
		dpuNode.Labels = cutil.CopyLabelsOrAnnotations(dpuNode.Labels, node.Labels)
		dpuNode.Annotations = cutil.CopyLabelsOrAnnotations(dpuNode.Annotations, node.Annotations)

	}

	// Compute the shared DPU phase snapshot once. Both reboot handling
	// (HandleRebootSync) and readiness (noneDPUInNodeEffectOrRebooting)
	// need a Spec-derived "is anyone rebooting?" signal, and computing it
	// here avoids duplicate Spec.DPUs walks downstream.
	dpuPhases := map[string]struct{}{}
	if err := cutil.GetDPUPhases(ctx, r.Client, dpuNode, dpuPhases); err != nil {
		return ctrl.Result{}, err
	}

	// Drive External/Script reboot paths and publish Status.RebootMethod.
	if result, err := r.HandleRebootSync(ctx, dpuNode, dpuPhases); err != nil || !result.IsZero() {
		return result, err
	}

	// Update DPUNode status - DPUInstallInterface
	if r.DPUInstallInterface == nil {
		return ctrl.Result{}, errors.New("DPUInstallInterface is not set")
	}
	if dpuNode.Status.DPUInstallInterface == nil {
		dpuNode.Status.DPUInstallInterface = r.DPUInstallInterface
		return ctrl.Result{}, nil
	}

	// Handle host agent upgrade
	if result := r.handleHostAgentUpgrade(ctx, dpuNode, nodeRef != nil); !result.IsZero() {
		return result, nil
	}

	// Delete the NodeEffectInProgress condition if there is no DPUNodeMaintenance for this DPUNode
	dpunodemaintenanceList := &provisioningv1.DPUNodeMaintenanceList{}
	dpunodemaintenanceExists := true
	if err := r.List(ctx, dpunodemaintenanceList, client.MatchingLabels{provisioningv1.DPUNodeNameLabel: dpuNode.Name}); err != nil {
		return ctrl.Result{}, err
	}
	if len(dpunodemaintenanceList.Items) == 0 {
		dpunodemaintenanceExists = false
		log.Info(fmt.Sprintf("DPUNode %s is ready because there is no DPUNodeMaintenance for this DPUNode", dpuNode.Name))
		meta.RemoveStatusCondition(&dpuNode.Status.Conditions, provisioningv1.DPUNodeConditionNodeEffectInProgress.String())
	}

	// Update DPUNode ready condition
	hasRebootingDPU := cutil.ContainsDPUPhase(dpuPhases, provisioningv1.DPURebooting)
	if err := r.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, dpunodemaintenanceExists, hasRebootingDPU); err == nil {
		log.Info(fmt.Sprintf("DPUNode %s is ready because there is no DPU in NodeEffect or Rebooting phase", dpuNode.Name))
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionTrue, "", "")
	} else {
		log.Info(fmt.Sprintf("DPUNode %s is not ready because there is a DPU in NodeEffect or Rebooting phase: %v", dpuNode.Name, err))
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionFalse, "", err.Error())
	}

	// TODO: add health check for DMS pod
	return ctrl.Result{}, nil
}

// handleDeletionAndFinalizer handles deletion timestamp and finalizer setup
func (r *DPUNodeReconciler) handleDeletionAndFinalizer(ctx context.Context, dpuNode *provisioningv1.DPUNode) error {
	if !dpuNode.DeletionTimestamp.IsZero() {
		dpus := &provisioningv1.DPUList{}
		if err := r.List(ctx, dpus, client.MatchingLabels{provisioningv1.DPUNodeNameLabel: dpuNode.Name}); err != nil {
			return err
		}
		for _, dpu := range dpus.Items {
			if err := r.Delete(ctx, &dpu); err != nil {
				return err
			}
		}
		dpuDevices := &provisioningv1.DPUDeviceList{}
		if err := r.List(ctx, dpuDevices, client.MatchingLabels{provisioningv1.DPUNodeNameLabel: dpuNode.Name}); err != nil {
			return err
		}
		if len(dpuDevices.Items) == 0 {
			return r.removeFinalizer(ctx, dpuNode)
		}
		for _, dpuDevice := range dpuDevices.Items {
			if err := r.Delete(ctx, &dpuDevice); err != nil {
				return err
			}
		}
	}
	if !controllerutil.ContainsFinalizer(dpuNode, provisioningv1.DPUNodeFinalizer) {
		controllerutil.AddFinalizer(dpuNode, provisioningv1.DPUNodeFinalizer)
		return nil
	}

	return nil
}

// HandleRebootSync drives the External and Script reboot paths and
// publishes Status.RebootMethod when at least one DPU is in DPURebooting.
// dpuPhases is the phase snapshot from Reconcile, reused to avoid a
// duplicate Spec.DPUs walk.
func (r *DPUNodeReconciler) HandleRebootSync(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpuPhases map[string]struct{}) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info(fmt.Sprintf("DPUNode: %s , DPUPhases: %v", dpuNode.Name, dpuPhases))

	var inScope []*provisioningv1.DPU
	if cutil.ContainsDPUPhase(dpuPhases, provisioningv1.DPURebooting) {
		var err error
		inScope, err = r.aggregateAndPublishRebootMethod(ctx, dpuNode)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if dpuNode.Spec.NodeRebootMethod.External == nil && dpuNode.Spec.NodeRebootMethod.Script == nil {
		return ctrl.Result{}, nil
	}

	provisioningPhases := map[string]struct{}{
		string(provisioningv1.DPUPrepareBFB):          {},
		string(provisioningv1.DPUConfigFWParameters):  {},
		string(provisioningv1.DPUInitializeInterface): {},
		string(provisioningv1.DPUOSInstalling):        {},
		string(provisioningv1.DPUConfig):              {},
	}
	if !cutil.ContainsDPUPhase(dpuPhases, provisioningv1.DPURebooting) {
		return ctrl.Result{}, nil
	}
	if len(dpuNode.Spec.DPUs) > 1 && cutil.ContainsDPUPhases(provisioningPhases, dpuPhases) {
		r.Recorder.Event(dpuNode, corev1.EventTypeNormal, "HostRebootCheck", "DPU in provisioning phase is found, wait 30 seconds and check again.")
		log.Info("There are DPUs in provisioning phase, requeue the request and reboot host later.")
		return ctrl.Result{RequeueAfter: cutil.RebootSyncInterval}, nil
	}
	// Guard against a brief disagreement between dpuPhases (label-list)
	// and inScope (Spec.DPUs Get) under concurrent updates: without it
	// External would stamp RebootInProgress=False without touching any
	// DPU, and Script would create a Job with no DPUs. Requeue.
	if len(inScope) == 0 {
		log.Info("DPURebooting phase observed but inScope snapshot is empty; requeueing for a fresh snapshot",
			"dpuPhases", dpuPhases, "specDPUs", dpuNode.Spec.DPUs)
		return ctrl.Result{RequeueAfter: cutil.RebootSyncInterval}, nil
	}
	if result, err := r.rebootNode(ctx, dpuNode, inScope); err != nil || !result.IsZero() {
		if err != nil {
			r.Recorder.Event(dpuNode, corev1.EventTypeWarning, "HostRebootError", err.Error())
		}
		return result, err
	}
	return ctrl.Result{}, nil
}

// noneDPUInNodeEffectOrRebooting decides whether the DPUNode is ready.
// hasRebootingDPU is a Spec-derived boolean (true iff any child DPU is in
// DPURebooting phase), computed by the caller from the dpuPhases snapshot.
func (r *DPUNodeReconciler) noneDPUInNodeEffectOrRebooting(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpunodemaintenanceExists bool, hasRebootingDPU bool) error {
	log := log.FromContext(ctx)

	// set DPUNode not ready when DPUNodeMaintenance exists and NodeEffect is in progress
	if dpunodemaintenanceExists {
		condition := meta.FindStatusCondition(dpuNode.Status.Conditions, provisioningv1.DPUNodeConditionNodeEffectInProgress.String())
		if condition != nil && (condition.Status == metav1.ConditionTrue) {
			return fmt.Errorf("node effect is in progress")
		}
	}

	rebootCondition := meta.FindStatusCondition(dpuNode.Status.Conditions, provisioningv1.DPUNodeConditionRebootInProgress.String())

	// Stale RebootInProgress=True with no DPU in DPURebooting: either the
	// reboot finished and the marker was not cleared, or the child DPUs
	// disappeared mid-reboot. Remove only RebootInProgress so the DPUNode
	// does not stay NotReady forever; leave Status.RebootMethod intact --
	// it must survive the rebooting -> idle transition and only the
	// no-DPUs branch below is allowed to clear it. Uses hasRebootingDPU
	// (Spec-derived) rather than a label List so it is immune to label
	// drift and cache races.
	if rebootCondition != nil && rebootCondition.Status == metav1.ConditionTrue && !hasRebootingDPU {
		log.Info(fmt.Sprintf("DPUNode %s has stale RebootInProgress=True with no DPU in DPURebooting, removing condition (Status.RebootMethod preserved)", dpuNode.Name))
		meta.RemoveStatusCondition(&dpuNode.Status.Conditions, provisioningv1.DPUNodeConditionRebootInProgress.String())
		return nil
	}

	// Active reboot with DPUs still in DPURebooting short-circuits readiness.
	if rebootCondition != nil && rebootCondition.Status == metav1.ConditionTrue {
		return fmt.Errorf("reboot is in progress")
	}

	// Sweep leftover reboot-method markers when the DPUNode has no DPUs at all.
	// The HostAgent path never sets RebootInProgress, so the gate is OR'd with
	// Status.RebootMethod (also covers an orphan field from external mutation
	// or a partial PATCH). Steady-state DPUNodes with no marker skip the List
	// entirely.
	hasRebootInProgress := rebootCondition != nil
	hasRebootMethodField := dpuNode.Status.RebootMethod != nil
	if hasRebootInProgress || hasRebootMethodField {
		dpus := &provisioningv1.DPUList{}
		if err := r.List(ctx, dpus, client.MatchingLabels{provisioningv1.DPUNodeNameLabel: dpuNode.Name}); err != nil {
			return err
		}
		if len(dpus.Items) == 0 {
			log.Info(fmt.Sprintf("DPUNode %s has no DPU objects, clearing reboot markers", dpuNode.Name))
			meta.RemoveStatusCondition(&dpuNode.Status.Conditions, provisioningv1.DPUNodeConditionRebootInProgress.String())
			dpuNode.Status.RebootMethod = nil
		}
	}

	return nil
}

func (r *DPUNodeReconciler) updateDPURebootStatus(ctx context.Context, dpus []*provisioningv1.DPU, phase provisioningv1.RebootStatusPhase, reason string, message string) error {
	for _, dpu := range dpus {
		if err := cutil.UpdateDPURebootStatus(ctx, r.Client, dpu, phase, reason, message); err != nil {
			return err
		}
	}
	return nil
}

func (r *DPUNodeReconciler) rebootNode(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("DPUNode conditions", "DPUNode", dpuNode.Status.Conditions)
	dpuNames := make([]string, len(dpus))
	for i, dpu := range dpus {
		dpuNames[i] = dpu.Name
	}
	logger.Info(fmt.Sprintf("DPUs in rebooting phase for DPUNode: %s: %v", dpuNode.Name, dpuNames))

	if dpuNode.Spec.NodeRebootMethod.External != nil {
		logger.Info("waiting for manual power cycle or reboot")
		if err := r.processExternalReboot(ctx, dpuNode, dpus); err != nil {
			err = fmt.Errorf("failed to process external reboot: %w", err)
			if err := r.updateDPURebootStatus(ctx, dpus, provisioningv1.RebootStatusFailed, "FailedToProcessExternalReboot", err.Error()); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, err
		}
	} else if dpuNode.Spec.NodeRebootMethod.Script != nil {
		return r.processScriptReboot(ctx, dpuNode, dpus)
	} else {
		panic("should not reach here")
	}

	return ctrl.Result{}, nil
}

// processScriptReboot applies cycle-level policy then delegates Job routing to
// reconcileScriptJob. Once every in-scope DPU has Succeeded the cycle is done and
// it returns without touching the Job: deleting or recreating it would re-reboot
// the host and regress RebootStatus. Callers pass the non-empty inScope snapshot
// from HandleRebootSync.
func (r *DPUNodeReconciler) processScriptReboot(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) (ctrl.Result, error) {
	if allRebootingDPUsSucceeded(dpus) {
		return ctrl.Result{}, nil
	}
	return r.reconcileScriptJob(ctx, dpuNode, dpus)
}

func (r *DPUNodeReconciler) createAndTrackScriptJob(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) (ctrl.Result, error) {
	if err := r.createScriptJob(ctx, dpuNode, dpus); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create script job: %w", err)
	}
	r.Recorder.Event(dpuNode, corev1.EventTypeNormal, "ScriptRebootJobCreated",
		"Custom reboot script job created")
	waitErr := fmt.Errorf("waiting for script to reboot node")
	if err := r.updateDPURebootStatus(ctx, dpus, provisioningv1.RebootStatusPending,
		cutil.ReasonRebootScriptWaiting, waitErr.Error()); err != nil {
		return ctrl.Result{}, err
	}
	r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue, "", "")
	return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
}

// reconcileScriptJob fetches the script Job and routes by RebootStatus lifecycle
// and Job terminality. A terminal Job is deleted as stale only when no DPU has an
// in-flight script lifecycle; an active Job with idle RebootStatus is the current
// cycle's Job whose status write has not yet reached the cache, so it is waited
// on rather than killed mid-reboot.
//
// Precondition: only called when allRebootingDPUsSucceeded is false, else a
// completed current-cycle Job would be misclassified as stale and deleted.
func (r *DPUNodeReconciler) reconcileScriptJob(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	jobName := r.generateJobName(dpuNode)
	job := &batchv1.Job{}

	if err := r.Get(ctx, client.ObjectKey{Namespace: dpuNode.Namespace, Name: jobName}, job); err != nil {
		if !apierrors.IsNotFound(err) {
			err = fmt.Errorf("failed to fetch Job %s: %w", jobName, err)
			if err2 := r.updateDPURebootStatus(ctx, dpus, provisioningv1.RebootStatusFailed, cutil.ReasonRebootScriptFailedToFetchJob, err.Error()); err2 != nil {
				return ctrl.Result{}, err2
			}
			return ctrl.Result{}, err
		}
		return r.handleJobNotFound(ctx, dpuNode, dpus)
	}

	lifecycleActive := scriptRebootLifecycleActiveOnAny(dpus)

	switch {
	case !lifecycleActive && isJobTerminal(job):
		logger.Info("Deleting stale script job from previous provisioning cycle",
			"job", jobName, "succeeded", job.Status.Succeeded, "failed", job.Status.Failed)
		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground)); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete stale job %s: %w", jobName, err)
		}
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
	case isJobComplete(job):
		return r.handleJobSucceeded(ctx, dpuNode, dpus)
	case isJobFailed(job):
		return r.handleJobFailed(ctx, dpuNode, dpus, job)
	default:
		// Job is active or in backoff -- wait for it to complete.
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
	}
}

// handleJobNotFound (re)creates the script job when absent: new cycle, user
// deletion, or TTL cleanup. A concurrent create loses the race to
// createScriptJob's guard, which returns an "already exists" error and retries.
func (r *DPUNodeReconciler) handleJobNotFound(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	jobName := r.generateJobName(dpuNode)
	logger.Info("Script job not found, creating", "job", jobName)
	r.Recorder.Event(dpuNode, corev1.EventTypeWarning, "ScriptRebootJobCreating",
		fmt.Sprintf("Script reboot job %s not found, creating", jobName))
	return r.createAndTrackScriptJob(ctx, dpuNode, dpus)
}

func (r *DPUNodeReconciler) handleJobSucceeded(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) (ctrl.Result, error) {
	r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionFalse, "", "")
	if err := r.updateDPURebootStatus(ctx, dpus, provisioningv1.RebootStatusSucceeded, "", ""); err != nil {
		return ctrl.Result{}, err
	}
	r.Recorder.Event(dpuNode, corev1.EventTypeNormal, "ScriptRebootSucceeded",
		"Custom reboot script completed successfully")
	return ctrl.Result{}, nil
}

// handleJobFailed persists RebootStatus failed; the DPU reconciler maps it to DPUCondRebooted (see reconcileHostRebootPhase).
func (r *DPUNodeReconciler) handleJobFailed(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU, job *batchv1.Job) (ctrl.Result, error) {
	failureDetails := r.extractPodFailureDetails(ctx, job)
	r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionFalse, "", "")
	failErr := fmt.Errorf("custom reboot script failed: %s. To retry, delete the failed job and the controller will recreate it", failureDetails)
	if err := r.updateDPURebootStatus(ctx, dpus, provisioningv1.RebootStatusFailed, cutil.ReasonRebootScriptFailed, failErr.Error()); err != nil {
		return ctrl.Result{}, err
	}
	r.Recorder.Event(dpuNode, corev1.EventTypeWarning, "ScriptRebootFailed", failureDetails)
	return ctrl.Result{}, nil
}

// createScriptJob creates the custom-reboot-script Job for dpuNode. The
// per-reconcile aggregation is recomputed locally from dpus so the job's
// env vars and annotations track the current cycle rather than the
// persisted DPUNode.Status.RebootMethod, which may still carry a previous
// cycle's winner.
func (r *DPUNodeReconciler) createScriptJob(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) error {
	logger := log.FromContext(ctx)
	agg := aggregateDPURebootMethods(dpus)
	job := &batchv1.Job{}
	jobName := r.generateJobName(dpuNode)
	if err := r.Get(ctx, client.ObjectKey{Namespace: dpuNode.Namespace, Name: jobName}, job); err == nil {
		return fmt.Errorf("refusing to create script job: job %s already exists (active=%d, succeeded=%d, failed=%d)",
			jobName, job.Status.Active, job.Status.Succeeded, job.Status.Failed)
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check for existing job %s: %w", jobName, err)
	}

	configMap := &corev1.ConfigMap{}
	configMapNamespacedName := types.NamespacedName{
		Namespace: dpuNode.Namespace,
		Name:      dpuNode.Spec.NodeRebootMethod.Script.Name,
	}
	if err := r.Get(ctx, configMapNamespacedName, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Error(err, fmt.Sprintf("ConfigMap %s not found for DPUNode %s", dpuNode.Spec.NodeRebootMethod.Script.Name, dpuNode.Name))
			return err
		}
		logger.Error(err, fmt.Sprintf("Unable to fetch ConfigMap %s for DPUNode %s", dpuNode.Spec.NodeRebootMethod.Script.Name, dpuNode.Name))
		return err
	}

	podTemplateStr, ok := configMap.Data[PodTemplateConfigMapKey]
	if !ok {
		err := fmt.Errorf("%s not found in ConfigMap", PodTemplateConfigMapKey)
		logger.Error(err, fmt.Sprintf("ConfigMap is missing %s key", PodTemplateConfigMapKey))
		return err
	}

	var podTemplate corev1.PodTemplateSpec
	if err := yaml.Unmarshal([]byte(podTemplateStr), &podTemplate); err != nil {
		return fmt.Errorf("unable to unmarshal pod template from ConfigMap %s for DPUNode %s: %w (the %q key must contain a PodTemplateSpec in YAML or JSON, not a full Pod manifest)",
			dpuNode.Spec.NodeRebootMethod.Script.Name, dpuNode.Name, err, PodTemplateConfigMapKey)
	}

	// Add more information to Job's Pod for rebooting script, e.g. labels, annotations, etc.

	// Surface agg (per-reconcile snapshot) so custom scripts can branch on
	// PowerCycle vs SystemReboot vs SLR without consuming the DPU API.
	// Stays consistent with DPUNode.Status.RebootMethod.
	aggregatedRebootMethod := agg.Method
	perDPURebootMethods := agg.PerDPU

	// Never stamp the Job with Unknown: the script must always have an
	// actionable signal. When no DPU has reported (or all report Unknown),
	// default to SystemLevelReset -- the safe middle ground. PerDPU keeps
	// "<dpu>=Unknown" entries so scripts can still tell who hasn't reported.
	if !agg.HasReporting || aggregatedRebootMethod == provisioningv1.RebootMethodUnknown {
		aggregatedRebootMethod = provisioningv1.RebootMethodSystemLevelReset
	}

	// Add DPUNODE_NAME to pod template containers env
	for i := range podTemplate.Spec.Containers {
		podTemplate.Spec.Containers[i].Env = r.ensureEnv(podTemplate.Spec.Containers[i].Env, DPUNodeNameEnvVar, dpuNode.Name)
		podTemplate.Spec.Containers[i].Env = r.ensureEnv(podTemplate.Spec.Containers[i].Env, DPUNodeRebootMethodEnvVar, string(aggregatedRebootMethod))
		podTemplate.Spec.Containers[i].Env = r.ensureEnv(podTemplate.Spec.Containers[i].Env, DPUNodeRebootMethodsPerDPUEnvVar, perDPURebootMethods)
	}

	// Add DPUNODE_NAME to pod template init containers env
	for i := range podTemplate.Spec.InitContainers {
		podTemplate.Spec.InitContainers[i].Env = r.ensureEnv(podTemplate.Spec.InitContainers[i].Env, DPUNodeNameEnvVar, dpuNode.Name)
		podTemplate.Spec.InitContainers[i].Env = r.ensureEnv(podTemplate.Spec.InitContainers[i].Env, DPUNodeRebootMethodEnvVar, string(aggregatedRebootMethod))
		podTemplate.Spec.InitContainers[i].Env = r.ensureEnv(podTemplate.Spec.InitContainers[i].Env, DPUNodeRebootMethodsPerDPUEnvVar, perDPURebootMethods)
	}

	// Add dpuNode annotations to pod template annotations
	if podTemplate.Annotations == nil {
		podTemplate.Annotations = make(map[string]string)
	}

	for k, v := range dpuNode.Annotations {
		if _, ok := podTemplate.Annotations[k]; ok {
			continue
		}
		podTemplate.Annotations[k] = v
	}

	// Stamp the aggregated reboot method as pod-template annotations so they
	// flow through the existing dpf-pod-info downward-API mount. These are
	// authoritative: any user-provided values for these specific keys are
	// overwritten because the DPUNode controller is the single source of truth.
	podTemplate.Annotations[DPUNodeRebootMethodAnnotation] = string(aggregatedRebootMethod)
	podTemplate.Annotations[DPUNodeRebootMethodsPerDPUAnnotation] = perDPURebootMethods

	// Add dpuNode labels to pod template labels
	if podTemplate.Labels == nil {
		podTemplate.Labels = make(map[string]string)
	}

	for k, v := range dpuNode.Labels {
		if _, ok := podTemplate.Labels[k]; ok {
			continue
		}
		podTemplate.Labels[k] = v
	}

	volumeExists := false
	for _, v := range podTemplate.Spec.Volumes {
		if v.Name == PodInfoVolumeName {
			volumeExists = true
			break
		}
	}
	if !volumeExists {
		// Add podInfo volume to pod template
		podTemplate.Spec.Volumes = append(podTemplate.Spec.Volumes, corev1.Volume{
			Name: PodInfoVolumeName,
			VolumeSource: corev1.VolumeSource{
				DownwardAPI: &corev1.DownwardAPIVolumeSource{
					Items: []corev1.DownwardAPIVolumeFile{
						{
							Path: PodInfoLabelsPath,
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: PodInfoLabelsFieldPath,
							},
						},
						{
							Path: PodInfoAnnotationsPath,
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: PodInfoAnnotationsFieldPath,
							},
						},
					},
				},
			},
		})

		// Add DPUNODE_NAME to pod template containers env
		for i := range podTemplate.Spec.Containers {
			podTemplate.Spec.Containers[i].VolumeMounts = r.ensureMount(podTemplate.Spec.Containers[i].VolumeMounts, PodInfoVolumeName, PodInfoMountPath)
		}

		// Add DPUNODE_NAME to pod template init containers env
		for i := range podTemplate.Spec.InitContainers {
			podTemplate.Spec.InitContainers[i].VolumeMounts = r.ensureMount(podTemplate.Spec.InitContainers[i].VolumeMounts, PodInfoVolumeName, PodInfoMountPath)
		}
	}

	podTemplate.Spec.Tolerations = ensureControlPlaneToleration(podTemplate.Spec.Tolerations)

	var backoffLimit int32 = 3
	// Allow enough time for the controller to observe job completion
	// before Kubernetes TTL controller auto-deletes the Job.
	var ttlSecondsAfterFinished int32 = 3600
	job = &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: dpuNode.Namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttlSecondsAfterFinished,
			Template:                podTemplate,
			BackoffLimit:            &backoffLimit,
		},
	}

	if err := r.Create(ctx, job); err != nil {
		logger.Error(err, fmt.Sprintf("Unable to create Job for DPUNode %s", dpuNode.Name))
		return err
	}

	return nil
}

// processExternalReboot drives the External reboot two-step state machine.
// agg is threaded from Reconcile so the WaitForExternalReboot Message can
// name the required reboot method and winner DPU without re-aggregating.
func (r *DPUNodeReconciler) processExternalReboot(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) error {
	logger := log.FromContext(ctx)
	agg := aggregateDPURebootMethods(dpus)

	// Every rebooting DPU must show external reboot progress via RebootStatus only: succeeded,
	// or explicit manual-wait pending (WaitingForManualPowerCycleOrReboot). Failed or unknown
	// Pending reasons require Step 1/2 again.
	// Step 1: record annotation only. Step 2: write RebootStatus Pending per DPU. Then wait for annotation removal / completion.
	condExists := true
	for _, dpu := range dpus {
		if dpu.Status.Phase != provisioningv1.DPURebooting {
			continue
		}
		rs := dpu.Status.RebootStatus
		if rs == nil {
			condExists = false
			continue
		}
		switch rs.Phase {
		case provisioningv1.RebootStatusSucceeded:
			continue
		case provisioningv1.RebootStatusPending:
			if rs.Reason == reasonExternalRebootWaitManual {
				continue
			}
		}
		condExists = false
	}
	if condExists {
		// Primary path for External Reboot Method to wait manual power cycle or reboot
		// Check if the external reboot required annotation is present
		// If present, update the DPUNode status condition to true and wait for user reboot
		if _, ok := dpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]; ok {
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue, "WaitForExternalReboot", buildExternalRebootWaitMessage(agg))
			logger.Info("Waiting for user reboot and remove the dpunode-external-reboot-required annotation")
			return nil
		}

		// Fallback path: annotation was removed after reboot while DPUCondRebooted still exists on DPUs.
		// Treat reboot as completed: RebootStatus succeeded; DPU reconciler derives DPUCondRebooted True from RebootStatus.
		if err := r.updateDPURebootStatus(ctx, dpus, provisioningv1.RebootStatusSucceeded, "", ""); err != nil {
			return err
		}
		logger.Info("Updated DPU RebootStatus after external reboot", "dpus", len(dpus))
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionFalse, "", "")
		return nil
	}

	// condExists is false:
	// Step 1 — First time only (no dpunode-external-reboot-required on DPUNode): record that the host must
	// reboot before we touch DPU status.
	// Mutate dpuNode in memory; Reconcile's deferred Patch persists metadata + status at exit.
	// It guarantees DPUCondRebooted is not set on DPUs until the annotation is set.
	if _, ok := dpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]; !ok {
		if dpuNode.Annotations == nil {
			dpuNode.Annotations = make(map[string]string)
		}
		dpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation] = "true"
		// Stamp the same enriched Reason/Message as the steady-state branch
		// so operators see the method and winner DPU immediately, not one
		// reconcile later when condExists flips to true.
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue,
			"WaitForExternalReboot", buildExternalRebootWaitMessage(agg))
		logger.Info("Recorded dpunode-external-reboot-required", "DPUNode", dpuNode.Name)
		return nil
	}

	// condExists is false:
	// Step 2 — Annotation is already stored (this reconcile or a prior one).
	// Per DPU: set RebootStatus pending with wait reason/message; DPU reconciler maps Pending -> DPUCondRebooted False (reconcileHostRebootPhase).
	for _, dpu := range dpus {
		if dpu.Status.Phase != provisioningv1.DPURebooting {
			continue
		}
		dpuSlice := []*provisioningv1.DPU{dpu}
		if err := r.updateDPURebootStatus(ctx, dpuSlice, provisioningv1.RebootStatusPending,
			reasonExternalRebootWaitManual, "waiting for manual power cycle or reboot"); err != nil {
			return err
		}
		logger.Info("Updated DPU RebootStatus pending for external reboot wait", "DPU", dpu.Name)
	}

	// Refresh the RebootInProgress Message so it tracks the current-cycle
	// agg (winner DPU / required method). Without this the steady-state
	// branch refreshes only on the next reconcile, leaving Step 2's reconcile
	// with a one-cycle-stale Message when agg changes between Step 1 and
	// Step 2.
	r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue,
		"WaitForExternalReboot", buildExternalRebootWaitMessage(agg))

	return nil
}

func (r *DPUNodeReconciler) generateJobName(dpuNode *provisioningv1.DPUNode) string {
	return fmt.Sprintf("%s-script-job", dpuNode.Name)
}

// scriptRebootLifecycleActive is true when RebootStatus reflects an in-flight script reboot
// (waiting for the job, failed to fetch the job, or script execution failed). The DPU
// controller mirrors this onto DPUCondRebooted.
func scriptRebootLifecycleActive(rs *provisioningv1.RebootStatus) bool {
	if rs == nil {
		return false
	}
	switch rs.Phase {
	case provisioningv1.RebootStatusPending:
		return rs.Reason == cutil.ReasonRebootScriptWaiting
	case provisioningv1.RebootStatusFailed:
		return isScriptRelatedReason(rs.Reason)
	default:
		return false
	}
}

// scriptRebootLifecycleActiveOnAny is true when any DPU has an in-flight script
// reboot. When false, an existing Job belongs to a previous cycle.
func scriptRebootLifecycleActiveOnAny(dpus []*provisioningv1.DPU) bool {
	for _, dpu := range dpus {
		if dpu != nil && scriptRebootLifecycleActive(dpu.Status.RebootStatus) {
			return true
		}
	}
	return false
}

// allRebootingDPUsSucceeded reports whether every in-scope DPU has Succeeded its
// script reboot (one host reboot serves all DPUs on the node). Returns false for
// an empty slice and on the first DPU that is nil, has nil RebootStatus, or is
// not yet Succeeded.
func allRebootingDPUsSucceeded(dpus []*provisioningv1.DPU) bool {
	if len(dpus) == 0 {
		return false
	}
	for _, dpu := range dpus {
		if dpu == nil {
			return false
		}
		rs := dpu.Status.RebootStatus
		if rs == nil || rs.Phase != provisioningv1.RebootStatusSucceeded {
			return false
		}
	}
	return true
}

// isScriptRelatedReason returns true for RebootStatus.reason values for script-reboot paths.
func isScriptRelatedReason(reason string) bool {
	switch reason {
	case cutil.ReasonRebootScriptWaiting,
		cutil.ReasonRebootScriptFailedToFetchJob, cutil.ReasonRebootScriptFailed:
		return true
	default:
		return false
	}
}

func isJobComplete(job *batchv1.Job) bool {
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func isJobFailed(job *batchv1.Job) bool {
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// isJobTerminal is true when the Job has reached a terminal state (Complete or Failed).
func isJobTerminal(job *batchv1.Job) bool {
	return isJobComplete(job) || isJobFailed(job)
}

func (r *DPUNodeReconciler) extractPodFailureDetails(ctx context.Context, job *batchv1.Job) string {
	logger := log.FromContext(ctx)
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods,
		client.InNamespace(job.Namespace),
		client.MatchingLabels{"job-name": job.Name},
	); err != nil {
		logger.V(1).Info("Unable to list pods for failure details, falling back to job status", "error", err)
		return extractJobFailureDetails(job)
	}
	if len(pods.Items) == 0 {
		return extractJobFailureDetails(job)
	}

	for i := len(pods.Items) - 1; i >= 0; i-- {
		for _, cs := range pods.Items[i].Status.InitContainerStatuses {
			if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
				return fmt.Sprintf("init container %q exited with code %d (reason: %s)",
					cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
			}
		}
		for _, cs := range pods.Items[i].Status.ContainerStatuses {
			if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
				return fmt.Sprintf("container %q exited with code %d (reason: %s)",
					cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
			}
		}
	}
	return extractJobFailureDetails(job)
}

func extractJobFailureDetails(job *batchv1.Job) string {
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return fmt.Sprintf("job failed (reason: %s): %s", cond.Reason, cond.Message)
		}
	}
	return "job failed with unknown reason"
}

func ensureControlPlaneToleration(tolerations []corev1.Toleration) []corev1.Toleration {
	const key = "node-role.kubernetes.io/control-plane"
	for _, t := range tolerations {
		if t.Key == key && t.Effect == corev1.TaintEffectNoSchedule {
			return tolerations
		}
	}
	return append(tolerations, corev1.Toleration{
		Key:      key,
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	})
}

func (r *DPUNodeReconciler) ensureEnv(envs []corev1.EnvVar, name, value string) []corev1.EnvVar {
	for _, e := range envs {
		if e.Name == name {
			return envs
		}
	}
	return append(envs, corev1.EnvVar{Name: name, Value: value})
}

func (r *DPUNodeReconciler) ensureMount(mnts []corev1.VolumeMount, name, path string) []corev1.VolumeMount {
	for _, m := range mnts {
		if m.Name == name {
			return mnts
		}
	}
	return append(mnts, corev1.VolumeMount{Name: name, MountPath: path})
}

// aggregateAndPublishRebootMethod recomputes the aggregated host-level
// reboot method from the DPUs in phase=DPURebooting and stamps
// Status.RebootMethod (the priority winner) when at least one DPU has
// reported. The field is left untouched when no DPU has reported, so it
// survives the rebooting -> idle transition; it is cleared by
// noneDPUInNodeEffectOrRebooting when the DPUNode loses all its DPUs.
//
// Returns the in-scope DPU snapshot so the caller can iterate it for
// per-DPU updates without re-listing.
func (r *DPUNodeReconciler) aggregateAndPublishRebootMethod(ctx context.Context, dpuNode *provisioningv1.DPUNode) ([]*provisioningv1.DPU, error) {
	dpus, err := cutil.GetDPUsWithPhase(ctx, r.Client, dpuNode, provisioningv1.DPURebooting)
	if err != nil {
		return nil, err
	}

	agg := aggregateDPURebootMethods(dpus)
	if !agg.HasReporting {
		return dpus, nil
	}

	dpuNode.Status.RebootMethod = ptr.To(agg.Method)
	return dpus, nil
}

// buildExternalRebootWaitMessage stamps the WaitForExternalReboot Message on
// the External path's RebootInProgress condition. Returns a "<pending agent
// report>" placeholder when no DPU has reported (defensive: production gates
// entry into DPURebooting on a non-nil method).
func buildExternalRebootWaitMessage(agg rebootMethodAggregation) string {
	if !agg.HasReporting {
		return "required reboot method: <pending agent report>"
	}
	return fmt.Sprintf("required reboot method: %s (driven by DPU %s)", agg.Method, agg.winnerOrNone())
}

// rebootMethodAggregation is the host-reboot decision for a slice of DPUs.
// Passed by value so downstream consumers do not need to parse the
// condition Message to recover the winner DPU.
type rebootMethodAggregation struct {
	// Method is the priority winner across the input slice, or RebootMethodUnknown
	// when HasReporting is false.
	Method provisioningv1.RebootMethodType
	// Winner is the name of the DPU that drove Method, or "" when HasReporting is false.
	Winner string
	// PerDPU is a comma-separated <name>=<method> mapping for the input slice,
	// sorted by DPU name. Empty for an empty slice.
	PerDPU string
	// HasReporting is true iff at least one DPU in the input slice has a
	// non-nil RebootStatus.Method. Distinguishes "no DPU has reported"
	// (HasReporting=false) from "all DPUs reported Unknown" (HasReporting=true,
	// Method=Unknown).
	HasReporting bool
}

// winnerOrNone returns Winner, substituting "<none>" for the empty-string
// (no-reporting) case so condition Messages always carry a placeholder.
func (a rebootMethodAggregation) winnerOrNone() string {
	if a.Winner == "" {
		return "<none>"
	}
	return a.Winner
}

// aggregateDPURebootMethods reads RebootStatus.Method on every DPU in dpus
// and returns the host-level priority winner, the DPU that drove it, the
// per-DPU mapping, and whether any DPU has reported. Callers should pass
// only DPUs in phase=DPURebooting.
//
// RebootStatus.Method is the canonical source: InitializeDPURebootStatus
// is the only writer, so reading it covers both the DPUConfig and the
// DPUInitializeInterface mode-transition paths uniformly.
//
// Pure: does not touch the API server. An empty/nil slice yields a
// zero-value rebootMethodAggregation.
func aggregateDPURebootMethods(dpus []*provisioningv1.DPU) rebootMethodAggregation {
	if len(dpus) == 0 {
		return rebootMethodAggregation{Method: provisioningv1.RebootMethodUnknown}
	}

	sorted := make([]*provisioningv1.DPU, 0, len(dpus))
	for _, d := range dpus {
		if d != nil {
			sorted = append(sorted, d)
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	agg := rebootMethodAggregation{Method: provisioningv1.RebootMethodUnknown}
	pairs := make([]string, 0, len(sorted))
	for _, d := range sorted {
		reported := d.Status.RebootStatus != nil && d.Status.RebootStatus.Method != nil
		method := provisioningv1.RebootMethodUnknown
		if reported {
			method = *d.Status.RebootStatus.Method
		}
		switch {
		case !agg.HasReporting && reported:
			// First reporting DPU seeds the winner regardless of priority,
			// so an all-Unknown reporting set still has a well-defined
			// Winner == lex-smallest reporting DPU.
			agg.HasReporting = true
			agg.Method = method
			agg.Winner = d.Name
		case cutil.GetRebootMethodPriority(method) < cutil.GetRebootMethodPriority(agg.Method):
			agg.Method = method
			agg.Winner = d.Name
		}
		pairs = append(pairs, fmt.Sprintf("%s=%s", d.Name, method))
	}
	agg.PerDPU = strings.Join(pairs, ",")
	return agg
}

func (r *DPUNodeReconciler) handleHostAgentUpgrade(ctx context.Context, dpuNode *provisioningv1.DPUNode, isKubernetes bool) ctrl.Result {
	log := log.FromContext(ctx)

	if *dpuNode.Status.DPUInstallInterface == string(provisioningv1.DPUNodeInstallIntrefaceRedfish) {
		return ctrl.Result{}
	}
	if isKubernetes {
		return ctrl.Result{}
	}
	if version, ok := dpuNode.Labels[release.DPFVersionLabelKey]; ok && version == release.DPFVersion() {
		return ctrl.Result{}
	}

	dpuNodeUpgradeConditionExists, needHostAgentUpgradeValue := r.getDPUNodeUpgradeCondition(dpuNode)
	if !dpuNodeUpgradeConditionExists {
		// Update the DPUNode condition to true and wait for the user to upgrade DMS
		msg := "Need user to upgrade host agent during the dpf upgrade."
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionNeedHostAgentUpgrade, metav1.ConditionTrue, "", msg)
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}
	} else if !needHostAgentUpgradeValue {
		// User has completed the DMS upgrade
		log.Info("Host agent upgrade is completed.")
		if dpuNode.Labels == nil {
			dpuNode.Labels = make(map[string]string)
		}
		dpuNode.Labels[release.DPFVersionLabelKey] = release.DPFVersion()
		return ctrl.Result{}
	} else {
		log.Info("Waiting for the user to upgrade host agent.")
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}
	}
}

func (r *DPUNodeReconciler) updateDPUNodeStatusConditions(dpuNode *provisioningv1.DPUNode, condType provisioningv1.DPUNodeConditionType, status metav1.ConditionStatus, reason string, message string) {
	cond := &metav1.Condition{
		Type:    condType.String(),
		Status:  status,
		Message: message,
	}
	if reason != "" {
		cond.Reason = reason
	} else {
		cond.Reason = condType.String()
	}
	meta.SetStatusCondition(&dpuNode.Status.Conditions, *cond)
}

func (r *DPUNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPUNode{}).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToDPUNodeReq),
			builder.WithPredicates(predicate.LabelChangedPredicate{})).
		Watches(&provisioningv1.DPU{},
			handler.EnqueueRequestsFromMapFunc(r.dpuToDPUNodeReq)).
		Watches(&provisioningv1.DPUDevice{},
			handler.EnqueueRequestsFromMapFunc(r.dpuDeviceToDPUNodeReq)).
		Watches(&operatorv1.DPFOperatorConfig{},
			handler.EnqueueRequestsFromMapFunc(r.dpfOperatorConfigToDPUNodeReq)).
		Complete(r)
}

func (r *DPUNodeReconciler) dpfOperatorConfigToDPUNodeReq(ctx context.Context, resource client.Object) []reconcile.Request {
	requests := make([]reconcile.Request, 0)
	dpuNodeList := &provisioningv1.DPUNodeList{}
	dpfOperatorConfig, ok := resource.(*operatorv1.DPFOperatorConfig)
	if !ok {
		return nil
	}
	if !dpfOperatorConfig.UpgradeInProgress() {
		return nil
	}

	if err := r.List(ctx, dpuNodeList); err == nil {
		for _, item := range dpuNodeList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      item.GetName(),
					Namespace: item.GetNamespace(),
				}})
		}
	}
	return requests
}

func (r *DPUNodeReconciler) nodeToDPUNodeReq(ctx context.Context, resource client.Object) []reconcile.Request {
	// Get the node that changed
	node := resource.(*corev1.Node)
	requests := make([]reconcile.Request, 0)

	// List all DPUNodes
	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := r.List(ctx, dpuNodeList); err != nil {
		return nil
	}

	// Find DPUNodes that reference this node and add requests for them
	for _, dpuNode := range dpuNodeList.Items {
		if dpuNode.Status.KubeNodeRef != nil && *dpuNode.Status.KubeNodeRef == node.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      dpuNode.GetName(),
					Namespace: dpuNode.GetNamespace(),
				},
			})
		}
	}

	return requests
}

func (r *DPUNodeReconciler) dpuToDPUNodeReq(ctx context.Context, resource client.Object) []reconcile.Request {
	// Logic for handling changes to DPU objects
	dpu := resource.(*provisioningv1.DPU)
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name:      dpu.Spec.DPUNodeName,
			Namespace: dpu.Namespace,
		},
	}}
}

// dpuDeviceToDPUNodeReq maps DPUDevice changes to the owning DPUNode.
// This ensures the DPUNode controller is notified when DPUDevices are deleted,
// so that the DPUNode finalizer can be removed promptly instead of waiting
// for the periodic cache resync.
func (r *DPUNodeReconciler) dpuDeviceToDPUNodeReq(_ context.Context, resource client.Object) []reconcile.Request {
	dpuDevice := resource.(*provisioningv1.DPUDevice)
	dpuNodeName, ok := dpuDevice.Labels[provisioningv1.DPUNodeNameLabel]
	if !ok || dpuNodeName == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name:      dpuNodeName,
			Namespace: dpuDevice.Namespace,
		},
	}}
}

func (r *DPUNodeReconciler) getDPUNodeUpgradeCondition(dpuNode *provisioningv1.DPUNode) (bool, bool) {
	upgradeConditionExists, needDMSUpgrade := false, false
	for _, condition := range dpuNode.Status.Conditions {
		if condition.Type == provisioningv1.DPUNodeConditionNeedHostAgentUpgrade.String() {
			upgradeConditionExists = true
			needDMSUpgrade = condition.Status == metav1.ConditionTrue
			break
		}
	}
	return upgradeConditionExists, needDMSUpgrade
}

// containsNotFoundError recursively checks if an error or aggregate error contains a NotFound error
func containsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsNotFound(err) {
		return true
	}
	// Check if it's an aggregate error
	if agg, ok := err.(kerrors.Aggregate); ok {
		for _, e := range agg.Errors() {
			if containsNotFoundError(e) {
				return true
			}
		}
	}
	return false
}

// removeFinalizer removes the finalizer from the DPUNode object to ensure that it can be deleted
func (r *DPUNodeReconciler) removeFinalizer(ctx context.Context, dpuNode *provisioningv1.DPUNode) error {
	dpuList := &provisioningv1.DPUList{}
	if err := r.Client.List(ctx, dpuList); err != nil {
		return err
	}

	dpuCountOfNode := 0
	for _, dpu := range dpuList.Items {
		if dpu.Spec.DPUNodeName == dpuNode.Name {
			dpuCountOfNode += 1
		}
	}
	if dpuCountOfNode == 0 {
		controllerutil.RemoveFinalizer(dpuNode, provisioningv1.DPUNodeFinalizer)
	}

	return nil
}
