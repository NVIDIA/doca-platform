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
	"k8s.io/client-go/util/retry"
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

	// DPUNodeRebootMethodEnvVar is the env var name injected into custom-script
	// reboot pods that holds the aggregated reboot method (highest-priority
	// non-Unknown RebootMethod across the DPUs associated with the DPUNode).
	// Possible values mirror the provisioningv1.RebootMethodType enum;
	// "Unknown" is used when no DPU has reported a RebootMethod yet.
	// Stop-gap for v26.4: lets custom reboot scripts branch on the required
	// host action (e.g. PowerCycle vs SystemReboot) without consuming the DPU API.
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

	// Handle redfish reboot sync
	if result, err := r.HandleRebootSync(ctx, dpuNode); err != nil || !result.IsZero() {
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
	if err := r.noneDPUInNodeEffectOrRebooting(ctx, dpuNode, dpunodemaintenanceExists); err == nil {
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

// HandleRebootSync handles the host reboot sync for redfish interface
func (r *DPUNodeReconciler) HandleRebootSync(ctx context.Context, dpuNode *provisioningv1.DPUNode) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if dpuNode.Spec.NodeRebootMethod.External != nil || dpuNode.Spec.NodeRebootMethod.Script != nil {
		dpuPhases := map[string]struct{}{}
		err := cutil.GetDPUPhases(ctx, r.Client, dpuNode, dpuPhases)
		log.Info(fmt.Sprintf("DPUNode: %s , DPUPhases: %v", dpuNode.Name, dpuPhases))
		if err != nil {
			return ctrl.Result{}, err
		}
		provisioningPhases := map[string]struct{}{
			string(provisioningv1.DPUPrepareBFB):          {},
			string(provisioningv1.DPUConfigFWParameters):  {},
			string(provisioningv1.DPUInitializeInterface): {},
			string(provisioningv1.DPUOSInstalling):        {},
			string(provisioningv1.DPUConfig):              {},
		}
		if cutil.ContainsDPUPhase(dpuPhases, provisioningv1.DPURebooting) {
			if len(dpuNode.Spec.DPUs) > 1 && cutil.ContainsDPUPhases(provisioningPhases, dpuPhases) {
				r.Recorder.Event(dpuNode, corev1.EventTypeNormal, "HostRebootCheck", "DPU in provisioning phase is found, wait 30 seconds and check again.")
				log.Info("There are DPUs in provisioning phase, requeue the request and reboot host later.")
				return ctrl.Result{RequeueAfter: cutil.RebootSyncInterval}, nil
			}
			// perform host reboot
			if result, err := r.rebootNode(ctx, dpuNode); err != nil || !result.IsZero() {
				if err != nil {
					r.Recorder.Event(dpuNode, corev1.EventTypeWarning, "HostRebootError", err.Error())
				}
				return result, err
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *DPUNodeReconciler) noneDPUInNodeEffectOrRebooting(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpunodemaintenanceExists bool) error {
	log := log.FromContext(ctx)

	// set DPUNode not ready when DPUNodeMaintenance exists and NodeEffect is in progress
	if dpunodemaintenanceExists {
		condition := meta.FindStatusCondition(dpuNode.Status.Conditions, provisioningv1.DPUNodeConditionNodeEffectInProgress.String())
		if condition != nil && (condition.Status == metav1.ConditionTrue) {
			return fmt.Errorf("node effect is in progress")
		}
	}

	// Check RebootInProgress condition
	rebootCondition := meta.FindStatusCondition(dpuNode.Status.Conditions, provisioningv1.DPUNodeConditionRebootInProgress.String())
	if rebootCondition != nil {
		if rebootCondition.Status == metav1.ConditionTrue {
			return fmt.Errorf("reboot is in progress")
		}

		dpus := &provisioningv1.DPUList{}
		if err := r.List(ctx, dpus, client.MatchingLabels{provisioningv1.DPUNodeNameLabel: dpuNode.Name}); err != nil {
			return err
		}
		if len(dpus.Items) == 0 {
			log.Info(fmt.Sprintf("DPUNode %s has no DPU objects, removing RebootInProgress condition", dpuNode.Name))
			meta.RemoveStatusCondition(&dpuNode.Status.Conditions, provisioningv1.DPUNodeConditionRebootInProgress.String())
		}
	}

	return nil
}

func (r *DPUNodeReconciler) updateDPUCondition(ctx context.Context, dpus []*provisioningv1.DPU, condition *metav1.Condition) error {
	for _, dpu := range dpus {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Re-read the DPU to get the latest version
			if err := r.Get(ctx, client.ObjectKeyFromObject(dpu), dpu); err != nil {
				return err
			}
			cutil.SetDPUCondition(&dpu.Status, condition)
			return r.Status().Update(ctx, dpu)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *DPUNodeReconciler) rebootNode(ctx context.Context, dpuNode *provisioningv1.DPUNode) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("DPUNode conditions", "DPUNode", dpuNode.Status.Conditions)
	dpus, err := cutil.GetDPUsWithPhase(ctx, r.Client, dpuNode, provisioningv1.DPURebooting)
	if err != nil {
		return ctrl.Result{}, err
	}
	dpuNames := make([]string, len(dpus))
	for i, dpu := range dpus {
		dpuNames[i] = dpu.Name
	}
	logger.Info(fmt.Sprintf("DPUs in rebooting phase for DPUNode: %s: %v", dpuNode.Name, dpuNames))

	if dpuNode.Spec.NodeRebootMethod.External != nil {
		logger.Info("waiting for manual power cycle or reboot")
		if err := r.proccessExternalReboot(ctx, dpuNode, dpus); err != nil {
			err = fmt.Errorf("failed to process external reboot: %w", err)
			if err := r.updateDPUCondition(ctx, dpus, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToProcessExternalReboot", err.Error())); err != nil {
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

// processScriptReboot manages the script-based reboot lifecycle, detecting and
// cleaning up stale jobs from previous provisioning cycles before creating new ones.
func (r *DPUNodeReconciler) processScriptReboot(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) (ctrl.Result, error) {
	condExists := false
	for _, dpu := range dpus {
		if _, existedCond := cutil.GetDPUCondition(&dpu.Status, string(provisioningv1.DPUCondRebooted)); existedCond != nil {
			if isScriptRelatedReason(existedCond.Reason) {
				condExists = true
				break
			}
		}
	}
	isFirstRun := !condExists
	return r.handleExistingScriptJob(ctx, dpuNode, dpus, isFirstRun)
}

func (r *DPUNodeReconciler) createAndTrackScriptJob(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) (ctrl.Result, error) {
	if err := r.createScriptJob(ctx, dpuNode, dpus); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create script job: %w", err)
	}
	r.Recorder.Event(dpuNode, corev1.EventTypeNormal, "ScriptRebootJobCreated",
		"Custom reboot script job created")
	waitErr := fmt.Errorf("waiting for script to reboot node")
	if err := r.updateDPUCondition(ctx, dpus, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), waitErr, cutil.ReasonRebootScriptWaiting, waitErr.Error())); err != nil {
		return ctrl.Result{}, err
	}
	r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue, "", "")
	return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
}

// handleExistingScriptJob fetches the script Job and routes based on its status.
// When isFirstRun is true (no script-related condition on any DPU), the job is
// stale from a previous provisioning cycle and is deleted regardless of status.
func (r *DPUNodeReconciler) handleExistingScriptJob(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU, isFirstRun bool) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	jobName := r.generateJobName(dpuNode)
	job := &batchv1.Job{}

	if err := r.Get(ctx, client.ObjectKey{Namespace: dpuNode.Namespace, Name: jobName}, job); err != nil {
		if !apierrors.IsNotFound(err) {
			err = fmt.Errorf("failed to fetch Job %s: %w", jobName, err)
			_ = r.updateDPUCondition(ctx, dpus, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, cutil.ReasonRebootScriptFailedToFetchJob, err.Error()))
			return ctrl.Result{}, err
		}
		return r.handleJobNotFound(ctx, dpuNode, dpus)
	}

	// Any job present when isFirstRun is true is stale from a previous
	// provisioning cycle (deterministic name + TTL). Delete it and requeue so a
	// fresh job can be created with the same name.
	if isFirstRun {
		logger.Info("Deleting stale script job from previous provisioning cycle",
			"job", jobName, "succeeded", job.Status.Succeeded, "failed", job.Status.Failed)
		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground)); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete stale job %s: %w", jobName, err)
		}
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
	}

	if isJobComplete(job) {
		return r.handleJobSucceeded(ctx, dpuNode, dpus)
	}

	if isJobFailed(job) {
		return r.handleJobFailed(ctx, dpuNode, dpus, job)
	}

	// Job is active or in backoff -- wait for it to complete.
	return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
}

// handleJobNotFound recreates the script job when it is not found. This covers
// first-run (no job yet), accidental user deletion, and TTL cleanup races.
// If a concurrent reconcile created the job between the NotFound check and the
// Create call, createScriptJob's defense-in-depth guard returns an "already
// exists" error, which triggers a harmless backoff retry.
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
	if err := r.updateDPUCondition(ctx, dpus, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", "")); err != nil {
		return ctrl.Result{}, err
	}
	r.Recorder.Event(dpuNode, corev1.EventTypeNormal, "ScriptRebootSucceeded",
		"Custom reboot script completed successfully")
	return ctrl.Result{}, nil
}

// handleJobFailed sets a RebootScriptFailed condition on DPUs with enriched pod failure details.
func (r *DPUNodeReconciler) handleJobFailed(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU, job *batchv1.Job) (ctrl.Result, error) {
	failureDetails := r.extractPodFailureDetails(ctx, job)
	r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionFalse, "", "")
	failErr := fmt.Errorf("custom reboot script failed: %s. To retry, delete the failed job and the controller will recreate it", failureDetails)
	if updateErr := r.updateDPUCondition(ctx, dpus, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), failErr, cutil.ReasonRebootScriptFailed, failErr.Error())); updateErr != nil {
		return ctrl.Result{}, updateErr
	}
	r.Recorder.Event(dpuNode, corev1.EventTypeWarning, "ScriptRebootFailed", failureDetails)
	return ctrl.Result{}, nil
}

// createScriptJob creates the custom-reboot-script Job for dpuNode. The dpus
// argument MUST be the set of DPUs the controller has already filtered down to
// phase=Rebooting (see rebootNode -> processScriptReboot). The slice is the
// authoritative source for the reboot-method context propagated to the script
// pod, ensuring the Job is stamped with the methods of exactly the DPUs whose
// host reboot the script is being asked to perform.
func (r *DPUNodeReconciler) createScriptJob(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) error {
	logger := log.FromContext(ctx)
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

	// v26.4 stop-gap: surface the aggregated reboot method so custom scripts can
	// branch on PowerCycle vs SystemReboot vs SLR without consuming the DPU API.
	// dpus is already restricted to phase=Rebooting by the caller, so the
	// aggregation reflects exactly the host action the controller is asking the
	// script to perform - it does not include DPUs in DPUConfig or other
	// provisioning phases that may still be reporting a stale RebootMethod.
	aggregatedRebootMethod, perDPURebootMethods := aggregateDPURebootMethods(dpus)

	// Never stamp the script Job with an Unknown aggregate: the script must
	// always have an actionable signal. If no DPU has reported a RebootMethod
	// yet (or every DPU reports Unknown), default the aggregate to
	// SystemLevelReset - the safe middle ground that triggers a host-impacting
	// reboot without escalating to a hard PowerCycle. The per-DPU mapping
	// intentionally retains "<dpu>=Unknown" entries so scripts that need
	// per-DPU detail can still tell which DPUs have not reported yet.
	if aggregatedRebootMethod == provisioningv1.RebootMethodUnknown {
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

func (r *DPUNodeReconciler) proccessExternalReboot(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []*provisioningv1.DPU) error {
	logger := log.FromContext(ctx)
	c := cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForManualPowerCycleOrReboot", "")

	// Check if every rebooting DPU has DPUCondRebooted
	// First call of this function DPUCondRebooted should be nil and annotation should be set as Step 1.
	// Second call of this function DPUCondRebooted should be set in addition to annotation as Step 2.
	// Third call of this function should update DPUNodeConditionRebootInProgress condition as 'WaitingForExternalReboot'.
	// After the rebooting done and Annotation is removed,
	// DPUCondRebooted should be set to True and DPUNodeConditionRebootInProgress condition should be updated to 'Rebooted'.
	condExists := true
	for _, dpu := range dpus {
		if dpu.Status.Phase != provisioningv1.DPURebooting {
			continue
		}
		if _, existedCond := cutil.GetDPUCondition(&dpu.Status, c.Type); existedCond == nil {
			condExists = false
		}
	}
	if condExists {
		// Primary path for External Reboot Method to wait manual power cycle or reboot
		// Check if the external reboot required annotation is present
		// If present, update the DPUNode status condition to true and wait for user reboot
		if _, ok := dpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]; ok {
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue, "WaitForExternalReboot", "")
			logger.Info("Waiting for user reboot and remove the dpunode-external-reboot-required annotation")
			return nil
		}

		// Fallback path: annotation was removed after reboot while DPUCondRebooted still exists on DPUs.
		// Treat reboot as completed: set DPUCondRebooted True on rebooting DPUs and clear RebootInProgress on DPUNode.
		for _, dpu := range dpus {
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				// Re-read the DPU to get the latest version
				if err := r.Get(ctx, client.ObjectKeyFromObject(dpu), dpu); err != nil {
					return err
				}
				cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
				return r.Status().Update(ctx, dpu)
			})
			if err != nil {
				return err
			}
			logger.Info("Update DPU condition", "dpu", dpu.Name, "DPURebooted", "true")
		}
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
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue, "", "")
		logger.Info("Recorded dpunode-external-reboot-required", "DPUNode", dpuNode.Name)
		return nil
	}

	// condExists is false:
	// Step 2 — Annotation is already stored (this reconcile or a prior one).
	// Safe to initialize DPUCondRebooted to False on each DPU in DPURebooting phase
	// so the operator waits for the host reboot cycle.
	c.Status = metav1.ConditionFalse
	for _, dpu := range dpus {
		if dpu.Status.Phase != provisioningv1.DPURebooting {
			continue
		}
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Re-read the DPU to get the latest version
			if err := r.Get(ctx, client.ObjectKeyFromObject(dpu), dpu); err != nil {
				return err
			}
			cutil.SetDPUCondition(&dpu.Status, c)
			return r.Status().Update(ctx, dpu)
		})
		if err != nil {
			return err
		}
		logger.Info("Update DPU condition", "DPU", dpu.Name, "DPURebooted", "false")
	}

	return nil
}

func (r *DPUNodeReconciler) generateJobName(dpuNode *provisioningv1.DPUNode) string {
	return fmt.Sprintf("%s-script-job", dpuNode.Name)
}

// isScriptRelatedReason returns true for DPUCondRebooted condition reasons that
// indicate a script-reboot lifecycle managed by this controller. It is used to
// decide whether to route to handleExistingScriptJob (true) or attempt job
// creation (false).
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

// getRebootMethodPriority returns the aggregation priority of a RebootMethodType,
// where lower numbers represent more demanding reboots. The order intentionally
// mirrors the host-side host-agent merge order while extending it to the full
// RebootMethodType enum so that the most disruptive action (PowerCycle) wins
// when multiple DPUs report different methods.
func getRebootMethodPriority(m provisioningv1.RebootMethodType) int {
	switch m {
	case provisioningv1.RebootMethodPowerCycle:
		return 0
	case provisioningv1.RebootMethodSystemLevelReset:
		return 1
	case provisioningv1.RebootMethodSystemReboot:
		return 2
	case provisioningv1.RebootMethodFirmwareReset:
		return 3
	case provisioningv1.RebootMethodNoAction:
		return 4
	default:
		// Unknown or unrecognized: never wins against a known method.
		return 5
	}
}

// aggregateDPURebootMethods inspects AgentStatus.RebootMethod on every DPU in
// the supplied slice and returns:
//   - the aggregated reboot method: the highest-priority non-Unknown method
//     across the DPUs, or RebootMethodUnknown when every DPU is missing or
//     reports Unknown.
//   - a comma-separated <dpu-name>=<method> mapping for every DPU, sorted by
//     DPU name. DPUs whose RebootMethod has not been reported yet appear as
//     "<dpu-name>=Unknown".
//
// This is a v26.4 stop-gap helper used exclusively by createScriptJob to
// expose reboot intent to custom-script reboot pods. Callers MUST pass only
// the DPUs that have advanced to phase=Rebooting so the aggregation never
// incorporates DPUs whose RebootMethod is still being negotiated in earlier
// provisioning phases (e.g. DPUConfig). The function is pure and does not
// touch the API server. An empty/nil slice yields (RebootMethodUnknown, "").
func aggregateDPURebootMethods(dpus []*provisioningv1.DPU) (provisioningv1.RebootMethodType, string) {
	if len(dpus) == 0 {
		return provisioningv1.RebootMethodUnknown, ""
	}

	sorted := make([]*provisioningv1.DPU, len(dpus))
	copy(sorted, dpus)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	aggregated := provisioningv1.RebootMethodUnknown
	aggregatedPriority := getRebootMethodPriority(provisioningv1.RebootMethodUnknown)
	pairs := make([]string, 0, len(sorted))
	for _, d := range sorted {
		if d == nil {
			continue
		}
		method := provisioningv1.RebootMethodUnknown
		if d.Status.AgentStatus != nil && d.Status.AgentStatus.RebootMethod != nil {
			method = *d.Status.AgentStatus.RebootMethod
		}
		if p := getRebootMethodPriority(method); p < aggregatedPriority {
			aggregated = method
			aggregatedPriority = p
		}
		pairs = append(pairs, fmt.Sprintf("%s=%s", d.Name, method))
	}
	return aggregated, strings.Join(pairs, ",")
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
