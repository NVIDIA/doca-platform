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
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"

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
	Options dnutil.DMSPodOptions
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

func (r *DPUNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := log.FromContext(ctx)
	log.Info("Reconcile")

	dpuNode := &provisioningv1.DPUNode{}
	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuNode, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := patcher.Patch(ctx, dpuNode,
			patch.WithFieldOwner(DPUNodeControllerName),
			patch.WithStatusObservedGeneration{},
		); err != nil {
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

		dmsPodName := cutil.GenerateDMSPodName(dpuNode)
		nn := types.NamespacedName{
			Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
			Name:      dmsPodName,
		}

		if err := r.Get(ctx, nn, pod); err != nil {
			if apierrors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		conditionMessage, err := r.isPodRunning(ctx, pod)
		if err != nil {
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionFalse, "ErrorOccurred", err.Error())
			return ctrl.Result{}, err
		}

		if conditionMessage != nil {
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionFalse, "NotReady", *conditionMessage)
			return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
		}
	}

	// Handle redfish reboot sync
	if result, err := r.handleRebootSync(ctx, dpuNode); err != nil || !result.IsZero() {
		return result, err
	}

	// Handle host agent upgrade
	if result, err := r.handleHostAgentUpgrade(ctx, dpuNode, nodeRef != nil); err != nil || !result.IsZero() {
		return result, err
	}

	// TODO: handle DPU modified

	// Update DPUNode status - DPUInstallInterface
	if r.DPUInstallInterface == nil {
		return ctrl.Result{}, errors.New("DPUInstallInterface is not set")
	}
	if dpuNode.Status.DPUInstallInterface == nil {
		dpuNode.Status.DPUInstallInterface = r.DPUInstallInterface
		return ctrl.Result{}, nil
	}

	if err := r.reconcileDPUDevices(ctx, dpuNode); err != nil {
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionInvalidDPUDetails, metav1.ConditionTrue, string(provisioningv1.DPUNodeConditionInvalidDPUDetails), err.Error())
		return ctrl.Result{}, err
	}

	// Check if the DMS server is ready
	if *dpuNode.Status.DPUInstallInterface == string(provisioningv1.DPUNodeInstallInterfaceGNOI) {
		if dpuNode.Spec.NodeDMSAddress == nil {
			msg := fmt.Sprintf("DPUNode %s NodeDMSAddress is not set", dpuNode.Name)
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionFalse, "NoNodeDMSAddress", msg)
			return ctrl.Result{}, errors.New(msg)
		}
		addr := dpuNode.Spec.NodeDMSAddress.String()
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			msg := fmt.Sprintf("the DMS server %s is not ready yet, err: %v", addr, err)
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionFalse, "DMSServerNotReady", msg)
			return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
		}

		defer func() {
			if err := conn.Close(); err != nil {
				log.Error(fmt.Errorf("failed to close connection of %s, err: %v", addr, err), "")
			}
		}()

	}

	// handle DPU modified
	if err := r.noneDPUInNodeEffectOrRebooting(ctx, dpuNode); err == nil {
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionTrue, "", "")
	} else {
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionFalse, "", err.Error())
	}

	// TODO: add health check for DMS pod
	return ctrl.Result{}, nil
}

// handleDeletionAndFinalizer handles deletion timestamp and finalizer setup
func (r *DPUNodeReconciler) handleDeletionAndFinalizer(ctx context.Context, dpuNode *provisioningv1.DPUNode) error {
	if !dpuNode.DeletionTimestamp.IsZero() {
		return removeFinalizer(ctx, r.Client, dpuNode)
	}
	if !controllerutil.ContainsFinalizer(dpuNode, provisioningv1.DPUNodeFinalizer) {
		controllerutil.AddFinalizer(dpuNode, provisioningv1.DPUNodeFinalizer)
		return nil
	}

	return nil
}

// handleRebootSync handles the host reboot sync for redfish interface
func (r *DPUNodeReconciler) handleRebootSync(ctx context.Context, dpuNode *provisioningv1.DPUNode) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if dpuNode.Spec.NodeRebootMethod.External != nil || dpuNode.Spec.NodeRebootMethod.Script != nil {
		dpuPhases := map[string]struct{}{}
		err := cutil.GetDPUPhases(ctx, r.Client, dpuNode, dpuPhases)
		log.Info(fmt.Sprintf("dpuNode: %s , dpuPhases: %v", dpuNode.Name, dpuPhases))
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
				log.Info("There are DPUs in provisioning phase, requeue 30s.")
				return ctrl.Result{RequeueAfter: cutil.RebootSyncInterval}, nil
			}
			// perform host reboot
			if result, err := r.rebootNode(ctx, dpuNode); err != nil || !result.IsZero() {
				return result, err
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *DPUNodeReconciler) noneDPUInNodeEffectOrRebooting(ctx context.Context, dpuNode *provisioningv1.DPUNode) error {
	// handle DPU modified
	// if DPU is in NodeEffect or Rebooting phase, and the DPUNode is ready, then set the DPUNode condition to false
	dpuList := &provisioningv1.DPUList{}
	if err := r.List(ctx, dpuList); err != nil {
		return err
	}

	for _, dpu := range dpuList.Items {
		if dpu.Spec.DPUNodeName == dpuNode.Name {
			if dpu.Status.Phase == provisioningv1.DPUNodeEffect || dpu.Status.Phase == provisioningv1.DPURebooting {
				return fmt.Errorf("DPU %s is in %s phase", dpu.Name, dpu.Status.Phase)
			}
		}
	}
	return nil
}

func (r *DPUNodeReconciler) updateDPUCondition(ctx context.Context, dpus []*provisioningv1.DPU, condition *metav1.Condition) error {
	for _, dpu := range dpus {
		cutil.SetDPUCondition(&dpu.Status, condition)
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
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
	logger.Info("entry rebootNode")
	// TODO: handle the rebootCommand == reboot.Skip
	dpus, err := cutil.GetDPUsWithPhase(ctx, r.Client, dpuNode, provisioningv1.DPURebooting)
	if err != nil {
		return ctrl.Result{}, err
	}
	logger.Info(fmt.Sprintf("DPUs in rebooting phase for dpuNode: %s: %v", dpuNode.Name, dpus))
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

				r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionFalse, "", "")
				if err := r.updateDPUCondition(ctx, dpus, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", "")); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			} else if job.Status.Failed > 0 {
				r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionFalse, "", "")
				// Not remove the failed job for user debuging.
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

	podTemplateStr, ok := configMap.Data[PodTemplateConfigMapKey]
	if !ok {
		err := fmt.Errorf("%s not found in ConfigMap", PodTemplateConfigMapKey)
		logger.Error(err, fmt.Sprintf("ConfigMap is missing %s key", PodTemplateConfigMapKey))
		return err
	}

	var podTemplate corev1.PodTemplateSpec
	if err := json.Unmarshal([]byte(podTemplateStr), &podTemplate); err != nil {
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
	// Check whether the external reboot is already triggerred.
	condExists := false
	for _, dpu := range dpus {
		if _, existedCond := cutil.GetDPUCondition(&dpu.Status, c.Type); existedCond != nil {
			condExists = true
			break
		}
	}
	if condExists {
		if _, ok := dpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]; ok {
			return nil
		}

		for _, dpu := range dpus {
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
			if err := r.Status().Update(ctx, dpu); err != nil {
				if apierrors.IsConflict(err) {
					log.FromContext(ctx).Info("DPU update conflict, will retry", "dpu", dpu.Name, "error", err)
					return err
				}
				return err
			}
		}
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionFalse, "", "")
		return nil
	}

	// Update the DPUNode status condition to true
	r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionRebootInProgress, metav1.ConditionTrue, "", "")

	newDpuNode := &provisioningv1.DPUNode{}
	if dpuNode.Annotations == nil {
		newDpuNode.Annotations = make(map[string]string)
	}

	// Re-read the dpuNode to get the latest version before updating
	if err := r.Get(ctx, client.ObjectKey{Namespace: dpuNode.Namespace, Name: dpuNode.Name}, newDpuNode); err != nil {
		return err
	}
	newDpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation] = "true"
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return r.Update(ctx, newDpuNode)
	})
	if err != nil {
		logger.Error(err, fmt.Sprintf("failed to add annotation %s to DPUNode %s", provisioningv1.DPUNodeExternalRebootRequiredAnnotation, dpuNode.Name))
		return err
	}

	c.Status = metav1.ConditionFalse
	for _, dpu := range dpus {
		cutil.SetDPUCondition(&dpu.Status, c)
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.Status().Update(ctx, dpu)
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *DPUNodeReconciler) generateJobName(dpuNode *provisioningv1.DPUNode) string {
	return fmt.Sprintf("%s-script-job", dpuNode.Name)
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

func (r *DPUNodeReconciler) handleHostAgentUpgrade(ctx context.Context, dpuNode *provisioningv1.DPUNode, isKubernetes bool) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if isKubernetes {
		return ctrl.Result{}, nil
	}
	dpfOperatorConfig, err := dpfutils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		log.Error(fmt.Errorf("getting DPFOperatorConfig, err: %v", err), "")
		return ctrl.Result{}, err
	}
	if !dpfOperatorConfig.UpgradeInProgress() {
		return ctrl.Result{}, nil
	}

	dpuNodeUpgradeConditionExists, needHostAgentUpgradeValue := r.getDPUNodeUpgradeCondition(dpuNode)
	if !dpuNodeUpgradeConditionExists {
		// Update the DPUNode condition to true and wait for the user to upgrade DMS
		msg := "Need user to upgrade host agent during the dpf upgrade."
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionNeedHostAgentUpgrade, metav1.ConditionTrue, "", msg)
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
	} else if !needHostAgentUpgradeValue {
		// User has completed the DMS upgrade
		log.Info("Host agent upgrade is completed.")
		return ctrl.Result{}, nil
	} else {
		log.Info("Waiting for the user to upgrade host agent.")
		return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
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

func isTimeout(pod *corev1.Pod, timeoutDuration time.Duration) bool {
	return time.Since(pod.CreationTimestamp.Time) > timeoutDuration
}

func (r *DPUNodeReconciler) isPodRunning(ctx context.Context, pod *corev1.Pod) (*string, error) {
	// TODO: verifiy all returned conditions are OK.
	logger := log.FromContext(ctx)
	if !pod.DeletionTimestamp.IsZero() {
		message := fmt.Sprintf("DMS pod %s is in terminating state", pod.Name)
		return &message, nil
	}
	switch pod.Status.Phase {
	// TODO: fix the case when pod is in Pending state, check all containers and all initContainers and return proper message
	case corev1.PodPending:
		// Verify NFS server connection using the DMS container startup probe.
		if len(pod.Status.ContainerStatuses) == 0 || pod.Status.ContainerStatuses[0].State.Waiting != nil {
			for _, condition := range pod.Status.Conditions {
				if condition.Type != corev1.PodReadyToStartContainers || condition.Status != corev1.ConditionFalse {
					continue
				}
				message := fmt.Sprintf("the DMS server %s is not ready yet, wait for the NFS server to become available", pod.Name)
				return &message, nil
			}
		}
		message := fmt.Sprintf("the DMS server %s is not ready yet", pod.Name)
		return &message, nil

	case corev1.PodRunning:
		// a simple probe to check if the DMS server is ready
		logger.Info("DMS pod is running")
	case corev1.PodFailed:
		return nil, fmt.Errorf("DMS Pod Failed")

	default:
		if isTimeout(pod, r.Options.DMSPodTimeout) {
			return nil, fmt.Errorf("DMS Pod didn't run and timed out")
		}
	}
	return nil, nil
}

func (r *DPUNodeReconciler) reconcileDPUDevices(ctx context.Context, dpuNode *provisioningv1.DPUNode) error {
	if dpuNode.Status.DPUInstallInterface == nil {
		return fmt.Errorf("DPUInstallInterface is not provided")
	}
	dpuInstallInterface := *dpuNode.Status.DPUInstallInterface
	labels := map[string]string{
		cutil.DPUNodeNameLabel: dpuNode.Name,
	}
	for _, dpu := range dpuNode.Spec.DPUs {
		dpuDevice := &provisioningv1.DPUDevice{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: dpuNode.Namespace, Name: dpu.Name}, dpuDevice); err != nil {
			return err
		}
		switch dpuInstallInterface {
		case string(provisioningv1.DPUNodeInstallInterfaceGNOI):
			if dpuDevice.Status.PCIAddress == nil {
				return fmt.Errorf("DPUDevice %s does not have a PCI address", dpuDevice.Name)
			}
		case string(provisioningv1.DPUNodeInstallIntrefaceRedfish):
			if dpuDevice.Spec.BMCIP == nil {
				return fmt.Errorf("DPUDevice %s does not have a BMC IP address", dpuDevice.Name)
			}
		default:
			return fmt.Errorf("DPUInstallInterface %s is not supported", dpuInstallInterface)
		}
		if dpuDevice.Status.PCIAddress != nil {
			labels[cutil.DPUDevicePCIAddressLabel] = *dpuDevice.Status.PCIAddress
		}
		if dpuDevice.Spec.PSID != nil {
			labels[cutil.DPUDevicePSIDLabel] = *dpuDevice.Spec.PSID
		}
		if dpuDevice.Spec.OPN != nil {
			labels[cutil.DPUDeviceOPNLabel] = *dpuDevice.Spec.OPN
		}
		if dpuDevice.Spec.NumberOfPFs != nil {
			labels[cutil.DPUDeviceNumOfPFsLabel] = fmt.Sprintf("%d", *dpuDevice.Spec.NumberOfPFs)
		}
		if dpuDevice.Spec.PF0Name != nil {
			labels[cutil.DPUDevicePF0NameLabel] = *dpuDevice.Spec.PF0Name
		}
		if dpuDevice.Spec.BMCIP != nil {
			labels[cutil.DPUDeviceBMCIPLabel] = *dpuDevice.Spec.BMCIP
		}

		// add labels to DPUDevice CR
		patcher := patch.NewSerialPatcher(dpuDevice, r.Client)
		dpuDevice.Labels = cutil.CopyLabelsOrAnnotations(dpuDevice.Labels, labels)
		if err := patcher.Patch(ctx, dpuDevice); err != nil {
			return err
		}
	}
	return nil
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

// removeFinalizer removes the finalizer from the DPUNode object to ensure that it can be deleted
func removeFinalizer(ctx context.Context, k8sclient client.Client, dpuNode *provisioningv1.DPUNode) error {
	dpuList := &provisioningv1.DPUList{}
	if err := k8sclient.List(ctx, dpuList); err != nil {
		return err
	}

	dpuCountOfNode := 0
	for _, dpu := range dpuList.Items {
		if dpu.Spec.DPUNodeName == dpuNode.Name {
			dpuCountOfNode = dpuCountOfNode + 1
		}
	}
	if dpuCountOfNode == 0 {
		controllerutil.RemoveFinalizer(dpuNode, provisioningv1.DPUNodeFinalizer)
	}

	return nil
}
