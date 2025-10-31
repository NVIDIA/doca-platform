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

package dpunodemaintenance

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	"github.com/fluxcd/pkg/runtime/patch"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// DPUNodeMaintenanceControllerName is the name of the DPUNodeMaintenance controller
	DPUNodeMaintenanceControllerName = "dpunodemaintenance"
)

// DPUNodeMaintenanceReconciler reconciles a DPUNodeMaintenance object
type DPUNodeMaintenanceReconciler struct {
	client.Client
	DPUInstallInterface *string
	// Options is the options for the DPUNodeMaintenance controller.
	Options DPUNodeMaintenanceOptions
	// Recorder is an event recorder that is used to record events that occur during the execution of the controller.
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodemaintenances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodemaintenances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodemaintenances/finalizers,verbs=update

func (r *DPUNodeMaintenanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling")
	dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
	if err := r.Get(ctx, req.NamespacedName, dpunodemaintenance); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpunodemaintenance, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		logger.Info("Patching")
		if err := patcher.Patch(ctx, dpunodemaintenance,
			patch.WithFieldOwner(DPUNodeMaintenanceControllerName),
			patch.WithStatusObservedGeneration{},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	r.ensureStatusDefaults(dpunodemaintenance)

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpunodemaintenance, provisioningv1.DPUNodeMaintenanceFinalizer) {
		logger.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpunodemaintenance, provisioningv1.DPUNodeMaintenanceFinalizer)
		return ctrl.Result{}, nil
	}

	// If Requestor is empty, remove node effect and delete the DPUNodeMaintenance object
	if len(dpunodemaintenance.Spec.Requestor) == 0 {
		if dpunodemaintenance.GetDeletionTimestamp().IsZero() {
			if err := r.Client.Delete(ctx, dpunodemaintenance); err != nil {
				return ctrl.Result{}, err
			}
			logger.Info(fmt.Sprintf("Deleted DPUNodeMaintenance %s object", dpunodemaintenance.Name))
			return ctrl.Result{}, nil
		}
		return r.reconcileDelete(ctx, dpunodemaintenance, provisioningv1.DPUNodeMaintenanceFinalizer)
	}

	return r.reconcile(ctx, dpunodemaintenance)
}

func (r *DPUNodeMaintenanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPUNodeMaintenance{}).
		Complete(r)
}

func (r *DPUNodeMaintenanceReconciler) ensureStatusDefaults(dpunodemaintenance *provisioningv1.DPUNodeMaintenance) {
	if dpunodemaintenance.Status.MultiDPUOperationsSyncWaitTime == nil {
		dpunodemaintenance.Status.MultiDPUOperationsSyncWaitTime = &metav1.Duration{Duration: r.Options.MultiDPUOperationsSyncWaitTime}
	}
	if dpunodemaintenance.Status.MaxUnavailableDPUNodes == nil {
		dpunodemaintenance.Status.MaxUnavailableDPUNodes = &r.Options.MaxUnavailableDPUNodes
	}
}

//nolint:unparam
func (r *DPUNodeMaintenanceReconciler) reconcileDelete(ctx context.Context, dpunodemaintenance *provisioningv1.DPUNodeMaintenance, finalizer string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling delete")
	if len(dpunodemaintenance.Spec.Requestor) > 0 {
		logger.Info(fmt.Sprintf("The requestor(%v) of DPUNodeMaintenance (%s/%s) is set, skipping delete",
			dpunodemaintenance.Spec.Requestor, dpunodemaintenance.Namespace, dpunodemaintenance.Name))
		return ctrl.Result{}, nil
	}

	if err := handleNodeEffectRemoval(ctx, r.Client, dpunodemaintenance); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(dpunodemaintenance, finalizer)
	return ctrl.Result{}, nil
}

func (r *DPUNodeMaintenanceReconciler) reconcile(ctx context.Context, dpunodemaintenance *provisioningv1.DPUNodeMaintenance) (_ ctrl.Result, reterr error) {
	// If node effect is already applied, return
	if cutil.IsNodeEffectApplied(dpunodemaintenance) {
		conditions.AddTrue(dpunodemaintenance, provisioningv1.ConditionNodeEffectApplied)
		return ctrl.Result{}, nil
	}

	nodeEffect := dpunodemaintenance.Spec.NodeEffect
	if nodeEffect == nil {
		return ctrl.Result{}, fmt.Errorf("nodeEffect is nil for DPUNodeMaintenance %s/%s", dpunodemaintenance.Namespace, dpunodemaintenance.Name)
	}

	dpuNode := &provisioningv1.DPUNode{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: dpunodemaintenance.Namespace, Name: dpunodemaintenance.Spec.DPUNodeName}, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("DPU node %s not found during node effect removal", dpunodemaintenance.Spec.DPUNodeName)
		}
		return ctrl.Result{}, fmt.Errorf("failed to get DPU node %s during node effect removal: %w", dpunodemaintenance.Spec.DPUNodeName, err)
	}

	node := &corev1.Node{}
	if dpuNode.Status.KubeNodeRef != nil {
		if err := r.Get(ctx, types.NamespacedName{Namespace: "", Name: *dpuNode.Status.KubeNodeRef}, node); err != nil {
			if apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("node %s not found during node effect removal", *dpuNode.Status.KubeNodeRef)
			}
			return ctrl.Result{}, fmt.Errorf("failed to get node %s during node effect removal, err: %w", *dpuNode.Status.KubeNodeRef, err)
		}
	} else if nodeEffect.IsCustomLabel() || nodeEffect.IsTaint() || nodeEffect.IsDrain() {
		return ctrl.Result{}, fmt.Errorf("node effect %s is not supported for non k8s environment", nodeEffect.String())
	}

	if dpunodemaintenance.Status.NodeEffectSyncStartTime == nil {
		dpunodemaintenance.Status.NodeEffectSyncStartTime = &metav1.Time{Time: time.Now()}
	}

	// Check safety preconditions for applying node effect
	shouldProceed, result, err := r.checkMaxUnavailableAndSyncWaitTime(ctx, dpunodemaintenance, dpuNode)
	if !shouldProceed {
		return result, err
	}

	var nodeEffectError error
	var requeue bool
	switch nodeEffect.String() {
	case provisioningv1.NodeEffectDrain:
		requeue, nodeEffectError = r.reconcileDrain(ctx, dpunodemaintenance)
	case provisioningv1.NodeEffectTaint:
		nodeEffectError = r.reconcileTaint(ctx, dpunodemaintenance, node)
	case provisioningv1.NodeEffectCustomLabel:
		nodeEffectError = r.reconcileCustomLabel(ctx, dpunodemaintenance, node)
	case provisioningv1.NodeEffectCustomAction:
		requeue, nodeEffectError = r.reconcileCustomAction(ctx, dpunodemaintenance)
	case provisioningv1.NodeEffectHold:
		requeue, nodeEffectError = r.reconcileHold(ctx, dpunodemaintenance)
	}
	if requeue {
		conditions.AddFalse(
			dpunodemaintenance,
			provisioningv1.ConditionNodeEffectApplied,
			"NodeEffectIsProcessing",
			conditions.ConditionMessage(fmt.Sprintf("Node effect is being applied: %s", nodeEffectError.Error())),
		)
		r.Recorder.Eventf(dpunodemaintenance, corev1.EventTypeNormal, "NodeEffectIsProcessing", nodeEffectError.Error())
		if err := r.updateDPUNodeEffectInProgress(ctx, dpunodemaintenance.Namespace, dpunodemaintenance.Spec.DPUNodeName, metav1.ConditionTrue, "NodeEffectInProgress", ""); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	} else if nodeEffectError != nil {
		conditions.AddFalse(
			dpunodemaintenance,
			provisioningv1.ConditionNodeEffectApplied,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %s", nodeEffectError.Error())),
		)
		r.Recorder.Eventf(dpunodemaintenance, corev1.EventTypeWarning, "NodeEffectError", nodeEffectError.Error())
		return ctrl.Result{}, nodeEffectError
	}
	conditions.AddTrue(dpunodemaintenance, provisioningv1.ConditionNodeEffectApplied)
	r.Recorder.Eventf(dpunodemaintenance, corev1.EventTypeNormal, "NodeEffectApplied", "Node effect is applied")
	if err := r.updateDPUNodeEffectInProgress(ctx, dpunodemaintenance.Namespace, dpunodemaintenance.Spec.DPUNodeName, metav1.ConditionTrue, "NodeEffectInProgress", ""); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *DPUNodeMaintenanceReconciler) checkMaxUnavailableAndSyncWaitTime(ctx context.Context, dpunodemaintenance *provisioningv1.DPUNodeMaintenance, dpuNode *provisioningv1.DPUNode) (bool, ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// If force is false, check the multiDPUOperationsSyncWaitTime and maxUnavailableDPUNodes
	if dpunodemaintenance.Spec.NodeEffect.Force != nil && !*dpunodemaintenance.Spec.NodeEffect.Force {
		nodeNotReadyCount, err := r.countOtherNotReadyDPUNodes(ctx, dpunodemaintenance)
		if err != nil {
			return false, ctrl.Result{}, err
		}

		// If maxUnavailableDPUNodes is set, check if the number of not ready nodes is greater than maxUnavailableDPUNodes
		condition := conditions.Get(dpunodemaintenance, provisioningv1.ConditionNodeEffectApplied)
		logger.V(3).Info(fmt.Sprintf("condition: %+v", condition))
		if condition == nil || (condition.Status == metav1.ConditionFalse && condition.Reason != "NodeEffectIsProcessing") {
			if dpunodemaintenance.Status.MaxUnavailableDPUNodes != nil && nodeNotReadyCount >= *dpunodemaintenance.Status.MaxUnavailableDPUNodes {
				logger.V(3).Info(fmt.Sprintf("Number of not ready nodes would exceed maxUnavailableDPUNodes: %d >= %d", nodeNotReadyCount, *dpunodemaintenance.Status.MaxUnavailableDPUNodes))
				msg := fmt.Sprintf("Number of not ready nodes would exceed maxUnavailableDPUNodes: %d >= %d", nodeNotReadyCount, *dpunodemaintenance.Status.MaxUnavailableDPUNodes)
				conditions.AddFalse(
					dpunodemaintenance,
					provisioningv1.ConditionNodeEffectApplied,
					conditions.ReasonPending,
					conditions.ConditionMessage(msg),
				)
				r.Recorder.Eventf(dpunodemaintenance, corev1.EventTypeWarning, "MaxUnavailableExceeded", msg)
				return false, ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
		}
		logger.V(3).Info(fmt.Sprintf("nodeNotReadyCount: %d, maxUnavailableDPUNodes: %d", nodeNotReadyCount, *dpunodemaintenance.Status.MaxUnavailableDPUNodes))
		// If there are multiple DPUs on the node, check the multiDPUOperationsSyncWaitTime
		if len(dpuNode.Spec.DPUs) > 1 {
			if time.Since(dpunodemaintenance.Status.NodeEffectSyncStartTime.Time) < dpunodemaintenance.Status.MultiDPUOperationsSyncWaitTime.Duration {
				requeueAfterTime := dpunodemaintenance.Status.MultiDPUOperationsSyncWaitTime.Duration - time.Since(dpunodemaintenance.Status.NodeEffectSyncStartTime.Time)
				msg := "NodeEffect is waiting for MultiDPUOperationsSyncWaitTime to be reached"
				conditions.AddFalse(
					dpunodemaintenance,
					provisioningv1.ConditionNodeEffectApplied,
					"NodeEffectSyncInProgress",
					conditions.ConditionMessage(msg),
				)
				logger.V(3).Info(fmt.Sprintf("multiple DPU operations sync wait time: %s", requeueAfterTime))
				r.Recorder.Eventf(dpunodemaintenance, corev1.EventTypeNormal, "WaitingForOperationsSync", "Multiple DPU operations sync wait time")
				return false, ctrl.Result{RequeueAfter: requeueAfterTime}, nil
			}
		}
	}

	return true, ctrl.Result{}, nil
}

func (r *DPUNodeMaintenanceReconciler) countOtherNotReadyDPUNodes(ctx context.Context, dpunodemaintenance *provisioningv1.DPUNodeMaintenance) (int32, error) {
	logger := log.FromContext(ctx)
	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := r.List(ctx, dpuNodeList, client.InNamespace(dpunodemaintenance.Namespace)); err != nil {
		return 0, fmt.Errorf("failed to list DPU nodes: %w", err)
	}
	var nodeNotReadyCount int32 = 0
	for i := range dpuNodeList.Items {
		dn := &dpuNodeList.Items[i]
		if dpunodemaintenance.Spec.DPUNodeName == dn.Name {
			continue
		}
		// add dpuNodeInProgress to avoid the race condition
		dpuNodeInProgress := false
		condition := meta.FindStatusCondition(dn.Status.Conditions, provisioningv1.DPUNodeConditionNodeEffectInProgress.String())
		if condition != nil && (condition.Status == metav1.ConditionTrue) {
			dpuNodeInProgress = true
		}
		if !cutil.IsDPUNodeReady(dn) || dpuNodeInProgress {
			logger.V(3).Info(fmt.Sprintf("dpunode %s is not ready", dn.Name))
			nodeNotReadyCount++
		}
	}
	return nodeNotReadyCount, nil
}

func (r *DPUNodeMaintenanceReconciler) updateDPUNodeEffectInProgress(ctx context.Context, namespace, dpuNodeName string, status metav1.ConditionStatus, reason string, message string) error {
	logger := log.FromContext(ctx)
	dpuNode := &provisioningv1.DPUNode{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: dpuNodeName}, dpuNode); err != nil {
		return err
	}
	changed := false
	changed = meta.SetStatusCondition(&dpuNode.Status.Conditions, metav1.Condition{
		Type:    provisioningv1.DPUNodeConditionNodeEffectInProgress.String(),
		Status:  status,
		Reason:  reason,
		Message: message,
	})
	if changed {
		if err := r.Client.Status().Update(ctx, dpuNode); err != nil {
			logger.Error(err, "Failed to update DPUNodeNodeEffectInProgress condition")
			return err
		}
		logger.Info(fmt.Sprintf("DPUNodeNodeEffectInProgress condition updated for %s/%s", namespace, dpuNodeName))
	}
	return nil
}

func (r *DPUNodeMaintenanceReconciler) reconcileCustomLabel(ctx context.Context, dpunodemaintenance *provisioningv1.DPUNodeMaintenance, node *corev1.Node) error {
	logger := log.FromContext(ctx)
	logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"CustomLabel\" for node: %s", dpunodemaintenance.Spec.DPUNodeName))

	if err := cutil.AddLabelsToObject(ctx, r.Client, node, dpunodemaintenance.Spec.NodeEffect.CustomLabel); err != nil {
		return err
	}

	return nil
}

func (r *DPUNodeMaintenanceReconciler) reconcileCustomAction(ctx context.Context, dpunodemaintenance *provisioningv1.DPUNodeMaintenance) (bool, error) {
	logger := log.FromContext(ctx)
	logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"CustomAction\" for node: %s", dpunodemaintenance.Spec.DPUNodeName))

	jobName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpunodemaintenance.Spec.DPUNodeName, dpunodemaintenance.Spec.NodeEffect)
	if err != nil {
		return false, err
	}
	nn := types.NamespacedName{
		Namespace: dpunodemaintenance.Namespace,
		Name:      jobName,
	}
	job := &batchv1.Job{}
	if err := r.Client.Get(ctx, nn, job); err != nil {
		if apierrors.IsNotFound(err) {
			err := r.createCustomActionJob(ctx, dpunodemaintenance, jobName)
			if err != nil {
				logger.Error(err, "Failed to create custom action job")
				return false, err
			}
			logger.Info(fmt.Sprintf("Submited customAction batch job %s", jobName))
			return true, fmt.Errorf("job is being created")
		} else {
			logger.Error(err, "Failed to get job")
			return false, err
		}
	} else {
		jobSuccess, err := r.processJobConditions(job)
		if err != nil {
			err = fmt.Errorf("failed to process job conditions: %w", err)
			return false, err
		}

		if !jobSuccess {
			return true, fmt.Errorf("job is not finished")
		}
		return false, nil
	}
}

func (r *DPUNodeMaintenanceReconciler) processJobConditions(job *batchv1.Job) (bool, error) {
	timeout, _ := time.ParseDuration("10m")
	for _, condition := range job.Status.Conditions {
		switch {
		case condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue:
			return true, nil

		case condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue:
			return false, fmt.Errorf("job %s/%s failed", job.Namespace, job.Name)

		default:
			if job.Status.StartTime != nil {
				startTime := job.Status.StartTime.Time
				elapsedTime := time.Since(startTime)
				if elapsedTime > timeout {
					return false, fmt.Errorf("job %s/%s timed out", job.Namespace, job.Name)
				}
			}
		}
	}
	return false, nil
}

func (r *DPUNodeMaintenanceReconciler) createCustomActionJob(ctx context.Context, dpunodemaintenance *provisioningv1.DPUNodeMaintenance, jobName string) error {
	logger := log.FromContext(ctx)
	nodeEffect := dpunodemaintenance.Spec.NodeEffect
	configMap := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: dpunodemaintenance.Namespace, Name: *nodeEffect.CustomAction}, configMap); err != nil {
		logger.Error(err, "Failed to get config-map with a name %s", nodeEffect.CustomAction)
		return err
	}

	var podYaml string
	for k, v := range configMap.Data {
		if strings.Contains(k, "yaml") {
			podYaml = v
		}
	}

	if len(podYaml) == 0 {
		return fmt.Errorf("no YAML file definition in CustomAction")
	}

	pod := &corev1.Pod{}
	if err := yaml.Unmarshal([]byte(podYaml), pod); err != nil {
		logger.Error(err, "Failed to unmarshal YML file")
		return err
	}

	backoffLimit := int32(0)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: dpunodemaintenance.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				},
				Spec: pod.Spec,
			},
			BackoffLimit: &backoffLimit,
		},
	}

	err := r.Client.Create(ctx, job)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to create %s  job", jobName))
		return err
	}
	logger.V(3).Info(fmt.Sprintf("%s job created", jobName))
	return nil
}

// reconcileHold - create wait-for-external-nodeeffect annotation on DPUNodeMaintenance, set it to true and wait for it to change to false.
func (r *DPUNodeMaintenanceReconciler) reconcileHold(ctx context.Context, dpunodemaintenance *provisioningv1.DPUNodeMaintenance) (bool, error) {
	logger := log.FromContext(ctx)
	logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"Hold\" for node: %s", dpunodemaintenance.Spec.DPUNodeName))

	if dpunodemaintenance.Annotations == nil {
		dpunodemaintenance.Annotations = make(map[string]string)
	}
	val, exists := dpunodemaintenance.Annotations[cutil.HoldNodeEffectKey]
	if !exists {
		dpunodemaintenance.Annotations[cutil.HoldNodeEffectKey] = "true"
		return true, fmt.Errorf("DPUNodeMaintenance is in waiting for external node effect")
	}
	waitingForExternalNodeEffect, err := strconv.ParseBool(val)
	if err != nil {
		logger.Error(err, "Failed to parse waiting for external node effect annotation")
		return false, err
	}
	if waitingForExternalNodeEffect {
		return true, fmt.Errorf("DPUNodeMaintenance is in waiting for external node effect")
	}

	return false, nil
}

func (r *DPUNodeMaintenanceReconciler) reconcileTaint(ctx context.Context, dpunodemaintenance *provisioningv1.DPUNodeMaintenance, node *corev1.Node) error {
	logger := log.FromContext(ctx)
	logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"Taint\" for node: %s", dpunodemaintenance.Spec.DPUNodeName))
	taintExist := false
	for _, t := range node.Spec.Taints {
		if t.Key == dpunodemaintenance.Spec.NodeEffect.Taint.Key {
			taintExist = true
			break
		}
	}
	if !taintExist {
		node.Spec.Taints = append(node.Spec.Taints, *dpunodemaintenance.Spec.NodeEffect.Taint)
		if err := r.Client.Update(ctx, node); err != nil {
			return err
		}
	}
	return nil
}

func (r *DPUNodeMaintenanceReconciler) reconcileDrain(ctx context.Context, dpunodemaintenance *provisioningv1.DPUNodeMaintenance) (bool, error) {
	logger := log.FromContext(ctx)
	logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"Drain\" for node: %s", dpunodemaintenance.Spec.DPUNodeName))
	maintenanceNN := types.NamespacedName{
		Namespace: dpunodemaintenance.Namespace,
		Name:      dpunodemaintenance.Spec.DPUNodeName,
	}
	maintenance := &maintenancev1alpha1.NodeMaintenance{}
	if err := r.Client.Get(ctx, maintenanceNN, maintenance); err != nil {
		if apierrors.IsNotFound(err) {
			// Create node maintenance object
			owner := metav1.NewControllerRef(dpunodemaintenance, provisioningv1.DPUNodeMaintenanceGroupVersionKind)
			logger.V(3).Info(fmt.Sprintf("Creating NodeMaintenance (%s)", maintenanceNN))
			if err = createNodeMaintenance(ctx, r.Client, owner, dpunodemaintenance.Spec.DPUNodeName, dpunodemaintenance.Namespace); err != nil {
				return false, err
			}
			logger.V(3).Info(fmt.Sprintf("NodeMaintenance (%s) is being created", maintenanceNN))
			return true, fmt.Errorf("NodeMaintenance is created")
		}
		return false, err
	} else {
		// check node maintenance object status
		if done := checkNodeMaintenanceProgress(maintenance); done {
			logger.V(3).Info(fmt.Sprintf("NodeMaintenance (%s/%s) succeeded", maintenance.Namespace, maintenance.Name))
			return false, nil
		}
		logger.V(3).Info(fmt.Sprintf("NodeMaintenance (%s/%s) is processing", maintenance.Namespace, maintenance.Name))
		return true, fmt.Errorf("NodeMaintenance is in progress")
	}
}

func createNodeMaintenance(ctx context.Context, k8sClient client.Client, owner *metav1.OwnerReference, nodeName string, namespace string) error {
	logger := log.FromContext(ctx)

	nodeMaintenance := &maintenancev1alpha1.NodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name:            nodeName,
			Namespace:       namespace,
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: maintenancev1alpha1.NodeMaintenanceSpec{
			RequestorID: cutil.ProvisioningGroupName,
			NodeName:    nodeName,
			DrainSpec: &maintenancev1alpha1.DrainSpec{
				Force:          true,
				DeleteEmptyDir: true,
				PodSelector:    fmt.Sprintf("%s!=%s", cutil.ProvisioningComponentLabelKey, "hostagent"), //skip DMS pod
			},
		},
	}
	if err := k8sClient.Create(ctx, nodeMaintenance); err != nil {
		return err
	}
	logger.V(3).Info("Successfully created NodeMaintenance CR", "node", nodeName, "NodeMaintanence", nodeMaintenance)
	return nil
}

func checkNodeMaintenanceProgress(maintenance *maintenancev1alpha1.NodeMaintenance) bool {
	if condition := meta.FindStatusCondition(maintenance.Status.Conditions, maintenancev1alpha1.ConditionTypeReady); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func handleNodeEffectRemoval(ctx context.Context, k8sClient client.Client, dpunodemaintenance *provisioningv1.DPUNodeMaintenance) error {
	nodeEffect := dpunodemaintenance.Spec.NodeEffect
	if nodeEffect == nil {
		return fmt.Errorf("nodeEffect is nil for DPUNodeMaintenance %s/%s", dpunodemaintenance.Namespace, dpunodemaintenance.Name)
	}

	dpuNode := &provisioningv1.DPUNode{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: dpunodemaintenance.Namespace, Name: dpunodemaintenance.Spec.DPUNodeName}, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("DPU node %s not found during node effect removal", dpunodemaintenance.Spec.DPUNodeName)
		}
		return fmt.Errorf("failed to get DPU node %s during node effect removal: %w", dpunodemaintenance.Spec.DPUNodeName, err)
	}

	node := &corev1.Node{}
	if dpuNode.Status.KubeNodeRef != nil {
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "", Name: *dpuNode.Status.KubeNodeRef}, node); err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("node %s not found during node effect removal", *dpuNode.Status.KubeNodeRef)
			}
			return fmt.Errorf("failed to get node %s during node effect removal, err: %w", *dpuNode.Status.KubeNodeRef, err)
		}
	} else if nodeEffect.IsCustomLabel() || nodeEffect.IsTaint() || nodeEffect.IsDrain() {
		return fmt.Errorf("node effect %s is not supported for non k8s environment", nodeEffect.String())
	}

	switch nodeEffect.String() {
	case provisioningv1.NodeEffectNoEffect:
		return nil
	case provisioningv1.NodeEffectDrain:
		return removeNodeEffectDrain(ctx, k8sClient, node.Name, dpunodemaintenance.Namespace)
	case provisioningv1.NodeEffectTaint:
		return removeNodeEffectTaint(ctx, k8sClient, node, nodeEffect)
	case provisioningv1.NodeEffectCustomLabel:
		return removeNodeEffectCustomLabel(ctx, k8sClient, node, nodeEffect)
	case provisioningv1.NodeEffectCustomAction:
		return removeNodeEffectCustomAction(ctx, k8sClient, dpunodemaintenance.Name, dpunodemaintenance.Namespace)
	case provisioningv1.NodeEffectHold:
		// no need to remove hold effect
		return nil
	default:
		return fmt.Errorf("unknown node effect: %s", nodeEffect.String())
	}
}

func removeNodeEffectTaint(ctx context.Context, k8sClient client.Client, node *corev1.Node, nodeEffect *provisioningv1.NodeEffect) error {
	originalNode := node.DeepCopy()
	taintFound := false
	for i, taint := range node.Spec.Taints {
		if taint.Key == nodeEffect.Taint.Key {
			node.Spec.Taints = append(node.Spec.Taints[:i], node.Spec.Taints[i+1:]...)
			taintFound = true
			break
		}
	}
	if taintFound {
		patch := client.StrategicMergeFrom(originalNode)
		if err := k8sClient.Patch(ctx, node, patch); err != nil {
			return fmt.Errorf("failed to patch node %s after removing the Taint: %+v, err: %v", node.Name, nodeEffect.Taint, err)
		}
	}
	return nil
}

func removeNodeEffectDrain(ctx context.Context, k8sClient client.Client, nodeName string, namespace string) error {
	maintenance := &maintenancev1alpha1.NodeMaintenance{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: nodeName}, maintenance); err != nil {
		if apierrors.IsNotFound(err) {
			// node maintenance CR has been deleted
			return nil
		} else {
			return fmt.Errorf("failed to get NodeMaintenance object, err: %v", err)
		}
	}

	originalMaintenance := maintenance.DeepCopy()
	maintenance.Spec.AdditionalRequestors = []string{}
	patch := client.MergeFrom(originalMaintenance)
	if err := k8sClient.Patch(ctx, maintenance, patch); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to patch NodeMaintenance object after removing Spec.AdditionalRequestors, err: %v", err)
	}

	// delete node maintenance CR
	if err := cutil.DeleteObjects(ctx, k8sClient, maintenance); err != nil {
		return fmt.Errorf("failed to delete NodeMaintenance object, err: %v", err)
	}

	return nil
}

// TODO: Handling situations where multiple DPUNodeMaintenance CRs have overlapping labels
func removeNodeEffectCustomLabel(ctx context.Context, k8sClient client.Client, node *corev1.Node, nodeEffect *provisioningv1.NodeEffect) error {
	originalNode := node.DeepCopy()
	for k := range nodeEffect.CustomLabel {
		delete(node.Labels, k)
	}

	patch := client.StrategicMergeFrom(originalNode)
	if err := k8sClient.Patch(ctx, node, patch); err != nil {
		return fmt.Errorf("failed to patch node %s ,err: %v", node.Name, err)
	}

	return nil
}

func removeNodeEffectCustomAction(ctx context.Context, k8sClient client.Client, customJobName, namespace string) error {
	nn := types.NamespacedName{
		Namespace: namespace,
		Name:      customJobName,
	}
	job := &batchv1.Job{}
	if err := k8sClient.Get(ctx, nn, job); err == nil {
		if err := k8sClient.Delete(ctx, job); err != nil {
			return fmt.Errorf("failed to delete custom action job %s: %w", customJobName, err)
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get custom action job %s: %w", customJobName, err)
	}
	return nil
}
