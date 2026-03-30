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
		logger.Info("waiting for custom script reboot")

		condExists := false
		c := cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForScriptToRebootNode", "")
		for _, dpu := range dpus {
			if _, existedCond := cutil.GetDPUCondition(&dpu.Status, c.Type); existedCond != nil {
				condExists = true
				break
			}
		}
		// Check whether the custom script reboot is already triggerred.
		if condExists {
			jobName := r.generateJobName(dpuNode)
			job := &batchv1.Job{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: dpuNode.Namespace, Name: jobName}, job); err != nil {
				if apierrors.IsNotFound(err) {
					// Job not found - check if ConfigMap has been modified to trigger auto-retry
					shouldRetry, retryErr := r.shouldRetryScriptJob(ctx, dpuNode)
					if retryErr != nil {
						logger.Error(retryErr, "Failed to check if script job should be retried")
					} else if shouldRetry {
						logger.Info("Job not found but ConfigMap has been modified, triggering retry")

						if err := r.clearDPURebootedConditions(ctx, dpus); err != nil {
							return ctrl.Result{}, err
						}

						r.Recorder.Event(dpuNode, corev1.EventTypeNormal, "ScriptRebootRetry",
							"ConfigMap modified after job deletion, retrying script reboot")

						// Requeue to create a new job
						return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
					}

					// No retry - set JobNotFound condition
					err = fmt.Errorf("job %s not found", jobName)
					if err := r.updateDPUCondition(ctx, dpus, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "JobNotFound", err.Error())); err != nil {
						return ctrl.Result{}, err
					}
				}
				err = fmt.Errorf("failed to fetch Job %s: %w", jobName, err)
				if err := r.updateDPUCondition(ctx, dpus, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToFetchJob", err.Error())); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, err
			}

			if job.Status.Succeeded > 0 {
				logger.Info("The custom reboot script succeeded.")
				// Clean up the ConfigMap version annotation and persist to cluster
				delete(dpuNode.Annotations, provisioningv1.DPUNodeScriptConfigMapVersionAnnotation)
				r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionFalse, "", "")
				if err := r.updateDPUCondition(ctx, dpus, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", "")); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			} else if job.Status.Failed > 0 {
				// Check if ConfigMap has been modified since the job was created
				shouldRetry, err := r.shouldRetryScriptJob(ctx, dpuNode)
				if err != nil {
					logger.Error(err, "Failed to check if script job should be retried")
					// Continue with normal failure handling if we can't check
				} else if shouldRetry {
					logger.Info("ConfigMap has been modified, automatically retrying script job")

					// Delete the failed job
					if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
						logger.Error(err, fmt.Sprintf("Unable to delete failed Job %s for retry", jobName))
						return ctrl.Result{}, err
					}

					if err := r.clearDPURebootedConditions(ctx, dpus); err != nil {
						return ctrl.Result{}, err
					}

					r.Recorder.Event(dpuNode, corev1.EventTypeNormal, "ScriptRebootRetry",
						"ConfigMap was modified, retrying script reboot job")
					logger.Info("Retry preparation complete, new job will be created on next reconciliation")
					return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
				}

				r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionFalse, "", "")
				// Not remove the failed job for user debugging.
				err = fmt.Errorf("the custom reboot script failed")
				if err := r.updateDPUCondition(ctx, dpus, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "RebootFailed", err.Error())); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
		}

		if err := r.createScriptJob(ctx, dpuNode); err != nil {
			err = fmt.Errorf("failed to create script job: %w", err)
			if err := r.updateDPUCondition(ctx, dpus, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToCreateScriptJob", err.Error())); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, err
		}
		err = fmt.Errorf("waiting for script to reboot node")
		if err := r.updateDPUCondition(ctx, dpus, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "WaitingForScriptToRebootNode", err.Error())); err != nil {
			return ctrl.Result{}, err
		}
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue, "", "")
		logger.Info("Update DPUNode condition RebootInProgress to true.")
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
	} else {
		panic("should not reach here")
	}

	return ctrl.Result{}, nil
}

func (r *DPUNodeReconciler) createScriptJob(ctx context.Context, dpuNode *provisioningv1.DPUNode) error {
	logger := log.FromContext(ctx)
	job := &batchv1.Job{}
	jobName := r.generateJobName(dpuNode)
	// Checking job existed or not, if yes, delete it.
	if err := r.Get(ctx, client.ObjectKey{Namespace: dpuNode.Namespace, Name: jobName}, job); err == nil {
		if err := client.IgnoreNotFound(r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground))); err != nil {
			logger.Error(err, fmt.Sprintf("Unable to delete Job %s, err: %v", jobName, err))
			return err
		}
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

	// Store the ConfigMap ResourceVersion for change detection
	if dpuNode.Annotations == nil {
		dpuNode.Annotations = make(map[string]string)
	}
	dpuNode.Annotations[provisioningv1.DPUNodeScriptConfigMapVersionAnnotation] = configMap.ResourceVersion
	podTemplateStr, ok := configMap.Data[PodTemplateConfigMapKey]
	if !ok {
		err := fmt.Errorf("%s not found in ConfigMap", PodTemplateConfigMapKey)
		logger.Error(err, fmt.Sprintf("ConfigMap is missing %s key", PodTemplateConfigMapKey))
		return err
	}

	var podTemplate corev1.PodTemplateSpec
	if err := yaml.Unmarshal([]byte(podTemplateStr), &podTemplate); err != nil {
		logger.Error(err, fmt.Sprintf("Unable to unmarshal pod template from ConfigMap %s for DPUNode %s", dpuNode.Spec.NodeRebootMethod.Script.Name, dpuNode.Name))
		return err
	}

	// Add more information to Job's Pod for rebooting script, e.g. labels, annotations, etc.

	// Add DPUNODE_NAME to pod template containers env
	for i := range podTemplate.Spec.Containers {
		podTemplate.Spec.Containers[i].Env = r.ensureEnv(podTemplate.Spec.Containers[i].Env, DPUNodeNameEnvVar, dpuNode.Name)
	}

	// Add DPUNODE_NAME to pod template init containers env
	for i := range podTemplate.Spec.InitContainers {
		podTemplate.Spec.InitContainers[i].Env = r.ensureEnv(podTemplate.Spec.InitContainers[i].Env, DPUNodeNameEnvVar, dpuNode.Name)
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

	var backoffLimit int32 = 0
	var ttlSecondsAfterFinished int32 = 60
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
		if _, ok := dpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]; ok {
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue, "WaitForExternalReboot", "")
			logger.Info("Waiting for user reboot and remove the dpunode-external-reboot-required annotation")
			return nil
		}

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

	// Update the DPUNode status condition to true
	r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue, "", "")
	if dpuNode.Annotations == nil {
		dpuNode.Annotations = make(map[string]string)
	}
	dpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation] = "true"

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

// shouldRetryScriptJob checks if the ConfigMap has been modified since the job was created
func (r *DPUNodeReconciler) shouldRetryScriptJob(ctx context.Context, dpuNode *provisioningv1.DPUNode) (bool, error) {
	if dpuNode.Spec.NodeRebootMethod == nil || dpuNode.Spec.NodeRebootMethod.Script == nil {
		return false, nil
	}

	// Get the stored ConfigMap version
	storedVersion, ok := dpuNode.Annotations[provisioningv1.DPUNodeScriptConfigMapVersionAnnotation]
	if !ok {
		// No stored version means we can't compare - don't retry automatically
		return false, nil
	}

	// Fetch the current ConfigMap
	configMap := &corev1.ConfigMap{}
	configMapNamespacedName := types.NamespacedName{
		Namespace: dpuNode.Namespace,
		Name:      dpuNode.Spec.NodeRebootMethod.Script.Name,
	}
	if err := r.Get(ctx, configMapNamespacedName, configMap); err != nil {
		return false, err
	}

	// Compare ResourceVersions
	return configMap.ResourceVersion != storedVersion, nil
}

// clearDPURebootedConditions removes the Rebooted condition from all DPUs to allow retry.
func (r *DPUNodeReconciler) clearDPURebootedConditions(ctx context.Context, dpus []*provisioningv1.DPU) error {
	for _, dpu := range dpus {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := r.Get(ctx, client.ObjectKeyFromObject(dpu), dpu); err != nil {
				return err
			}
			// Remove the Rebooted condition to reset the state
			conditions := []metav1.Condition{}
			for _, cond := range dpu.Status.Conditions {
				if cond.Type != provisioningv1.DPUCondRebooted.String() {
					conditions = append(conditions, cond)
				}
			}
			dpu.Status.Conditions = conditions
			return r.Status().Update(ctx, dpu)
		})
		if err != nil {
			return err
		}
	}
	return nil
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
		// Watch ConfigMaps for script reboot method changes
		// Only watch ConfigMaps that have the pod-template key (used for script reboot)
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.configMapToDPUNodeReq),
			builder.WithPredicates(r.scriptRebootConfigMapPredicate())).
		Complete(r)
}

// scriptRebootConfigMapPredicate returns a predicate that filters ConfigMaps
// to only those that contain the pod-template key used for script reboot.
// This improves performance by avoiding processing unrelated ConfigMaps.
func (r *DPUNodeReconciler) scriptRebootConfigMapPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		cm, ok := obj.(*corev1.ConfigMap)
		if !ok {
			return false
		}
		// Only process ConfigMaps that have the pod-template key
		_, hasPodTemplate := cm.Data[PodTemplateConfigMapKey]
		return hasPodTemplate
	})
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

// configMapToDPUNodeReq maps ConfigMap changes to DPUNodes that use them for script reboot
func (r *DPUNodeReconciler) configMapToDPUNodeReq(ctx context.Context, resource client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)
	configMap := resource.(*corev1.ConfigMap)
	requests := make([]reconcile.Request, 0)

	// List all DPUNodes in the same namespace
	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := r.List(ctx, dpuNodeList, client.InNamespace(configMap.Namespace)); err != nil {
		logger.Error(err, "Failed to list DPUNodes for ConfigMap watch",
			"configMap", configMap.Name, "namespace", configMap.Namespace)
		return nil
	}

	// Find DPUNodes that reference this ConfigMap for script reboot
	for _, dpuNode := range dpuNodeList.Items {
		if dpuNode.Spec.NodeRebootMethod != nil &&
			dpuNode.Spec.NodeRebootMethod.Script != nil &&
			dpuNode.Spec.NodeRebootMethod.Script.Name == configMap.Name {
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
