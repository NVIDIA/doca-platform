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

package state

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	errorOccurredReason string = "ErrorOccured"
)

func NodeEffect(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	nodeEffect := dpu.Spec.NodeEffect

	if nodeEffect.IsNoEffect() {
		logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"NoEffect\" for node: %s", dpu.Spec.DPUNodeName))
		state.Phase = provisioningv1.DPUInitializeInterface
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, "", ""))
		return *state, nil
	}

	// Check for the presence of the specified Node
	dpuNode := &provisioningv1.DPUNode{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUNodeName}, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "DPUNodeNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "GetDPUNodeError", err.Error()))
		return *state, err
	}

	node := &corev1.Node{}
	if dpuNode.Status.KubeNodeRef != nil {
		// Check for the presence of the specified Node
		if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: "", Name: dpu.Spec.DPUNodeName}, node); err != nil {
			if apierrors.IsNotFound(err) {
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "NodeNotFound", err.Error()))
				return *state, err
			}
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "GetNodeError", err.Error()))
			return *state, err
		}
	} else if nodeEffect.IsCustomLabel() || nodeEffect.IsTaint() || nodeEffect.IsDrain() {
		err := fmt.Errorf("node effect (%s) is not supported for non k8s environment", nodeEffect.String())
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "InvalidNodeEffect", err.Error()))
		return *state, err
	}

	if nodeEffect.IsCustomLabel() {
		logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"CustomLabel\" for node: %s", dpu.Spec.DPUNodeName))
		if err := cutil.AddLabelsToObject(ctx, ctrlCtx.Client, dpuNode, nodeEffect.CustomLabel); err != nil {
			err = fmt.Errorf("failed to add labels to object: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "AddLabelsToObjectError", err.Error()))
			return *state, err
		}
	} else if nodeEffect.IsTaint() {
		return handleTaintNodeEffect(ctx, dpu, ctrlCtx, node)
	} else if nodeEffect.IsDrain() {
		return handleDrainNodeEffect(ctx, dpu, ctrlCtx)
	} else if nodeEffect.IsCustomAction() {
		return handleCustomAction(ctx, dpu, ctrlCtx)
	} else if nodeEffect.IsHold() {
		return handleHoldEffect(ctx, dpu, ctrlCtx)
	}

	state.Phase = provisioningv1.DPUInitializeInterface
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, "", ""))

	return *state, nil
}

func handleTaintNodeEffect(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, node *corev1.Node) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	logger := log.FromContext(ctx)
	logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"Taint\" for node: %s", node.Name))
	nodeEffect := dpu.Spec.NodeEffect
	taintExist := false
	for _, t := range node.Spec.Taints {
		if t.Key == nodeEffect.Taint.Key {
			taintExist = true
			break
		}
	}
	if !taintExist {
		node.Spec.Taints = append(node.Spec.Taints, *nodeEffect.Taint)
		if err := ctrlCtx.Client.Update(ctx, node); err != nil {
			return *state, err
		}
	}

	// TODO: add Toleration to the DMS Pod if it exists based on the taint

	state.Phase = provisioningv1.DPUInitializeInterface
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, "", ""))
	return *state, nil
}

func handleDrainNodeEffect(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	logger := log.FromContext(ctx)
	logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"Drain\" for node: %s", dpu.Spec.DPUNodeName))
	maintenanceNN := types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      dpu.Spec.DPUNodeName,
	}
	maintenance := &maintenancev1alpha1.NodeMaintenance{}
	if err := ctrlCtx.Client.Get(ctx, maintenanceNN, maintenance); err != nil {
		if apierrors.IsNotFound(err) {
			// Create node maintenance CR
			owner := metav1.NewControllerRef(dpu, provisioningv1.DPUGroupVersionKind)
			logger.V(3).Info(fmt.Sprintf("Createing NodeMaintenance (%s)", maintenanceNN))
			if err = createNodeMaintenance(ctx, ctrlCtx.Client, owner, dpu.Spec.DPUNodeName, dpu.Namespace); err != nil {
				setDPUCondNodeEffectReadyToFalse(state, errorOccurredReason, err.Error())
				return *state, err
			}
			return *state, nil
		} else {
			setDPUCondNodeEffectReadyToFalse(state, errorOccurredReason, err.Error())
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
	} else {
		if err := addAdditionalRequestor(ctx, ctrlCtx.Client, maintenance); err != nil {
			setDPUCondNodeEffectReadyToFalse(state, errorOccurredReason, err.Error())
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
		// check NM status
		if done := checkNodeMaintenanceProgress(maintenance); done {
			logger.V(3).Info(fmt.Sprintf("NodeMaintenance (%s/%s) succeeded", maintenance.Namespace, maintenance.Name))
		} else {
			setDPUCondNodeEffectReadyToFalse(state, "NodeMaintenanceIsNotReady", "NodeMaintenance is not ready")
			logger.V(3).Info(fmt.Sprintf("NodeMaintenance (%s/%s) is processing", maintenance.Namespace, maintenance.Name))
			return *state, fmt.Errorf("NodeMaintenance is in progress")
		}
	}
	state.Phase = provisioningv1.DPUInitializeInterface
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, "", ""))
	return *state, nil
}

// handleHoldEffect - create wait-for-external-nodeeffect label, set it to true and wait for it to change to false.
func handleHoldEffect(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	logger := log.FromContext(ctx)
	if dpu.Annotations == nil {
		dpu.Annotations = make(map[string]string)
	}
	val, exists := dpu.Annotations[cutil.HoldNodeEffectKey]
	if !exists {
		dpu.Annotations[cutil.HoldNodeEffectKey] = "true"
		err := ctrlCtx.Client.Update(ctx, dpu)
		if err != nil {
			logger.Error(err, "Failed to update dpu annotation")
			return *state, err
		}
		setDPUCondNodeEffectReadyToFalse(state, "ExternalNodeEffect", "DPU is in wait-for-external-nodeeffect node effect")
		return *state, fmt.Errorf("DPU is in wait-for-external-nodeeffect node effect")
	}
	exists, err := strconv.ParseBool(val)
	if err != nil {
		logger.Error(err, "Failed to parse wait-for-external-nodeeffect annotation")
		setDPUCondNodeEffectReadyToFalse(state, "ExternalNodeEffect", "Failed to parse wait-for-external-nodeeffect annotation")
		return *state, err
	}
	if exists {
		setDPUCondNodeEffectReadyToFalse(state, "ExternalNodeEffect", "DPU is in wait-for-external-nodeeffect node effect")
		return *state, fmt.Errorf("DPU is in wait-for-external-nodeeffect node effect")
	}

	state.Phase = provisioningv1.DPUInitializeInterface
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, "", ""))
	return *state, nil
}

func GetCustomActionJobName(nodeEffect *provisioningv1.NodeEffect, dpu *provisioningv1.DPU) string {
	return strings.ToLower(fmt.Sprintf("custom-action-%.15s-%.15s-%.15x", *nodeEffect.CustomAction, dpu.Name, dpu.CreationTimestamp.Time.UnixMilli()))
}

// handleCustomAction submit batch job and track it's progress
func handleCustomAction(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	nodeEffect := dpu.Spec.NodeEffect
	state := dpu.Status.DeepCopy()
	logger := log.FromContext(ctx)
	jobName := GetCustomActionJobName(nodeEffect, dpu)
	nn := types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      jobName,
	}
	job := &batchv1.Job{}
	if err := ctrlCtx.Client.Get(ctx, nn, job); err != nil {
		if apierrors.IsNotFound(err) {
			err := createCustomActionJob(ctx, dpu, ctrlCtx, jobName)
			if err != nil {
				logger.Error(err, "Failed to create custom action job")
				setDPUCondNodeEffectReadyToFalse(state, "FailedToCreateJob", "Failed to create custom action job")
				return *state, err
			}
			logger.Info("Submited customAction batch job %s", jobName)
			setDPUCondNodeEffectReadyToFalse(state, "JobIsStarted", "Custom job is started")
			return *state, fmt.Errorf("custom job is started")
		} else {
			logger.Error(err, "Failed to get job")
			setDPUCondNodeEffectReadyToFalse(state, "FailedToGetJob", "Failed to get custom action job")
			return *state, err
		}
	} else {
		jobSuccess, err := ProcessJobConditions(job)
		if err != nil {
			err = fmt.Errorf("failed to process job conditions: %w", err)
			setDPUCondNodeEffectReadyToFalse(state, "FailedToProcessJobConditions", err.Error())
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}

		if !jobSuccess {
			setDPUCondNodeEffectReadyToFalse(state, "JobIsNotFinished", "Custom job is not finished")
			return *state, fmt.Errorf("custom job is not finished")
		}

	}
	state.Phase = provisioningv1.DPUInitializeInterface
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, "", ""))
	return *state, nil
}

func ProcessJobConditions(job *batchv1.Job) (bool, error) {
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

func createCustomActionJob(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, jobName string) error {
	logger := log.FromContext(ctx)
	nodeEffect := dpu.Spec.NodeEffect
	configMap := &corev1.ConfigMap{}
	if err := ctrlCtx.Client.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: *nodeEffect.CustomAction}, configMap); err != nil {
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
			Namespace: dpu.Namespace,
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

	err := ctrlCtx.Client.Create(ctx, job)
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to create %s  job", jobName))
		return err
	}
	logger.V(3).Info(fmt.Sprintf("%s job created", jobName))
	return nil
}

// add ProvisioningGroupName to AdditionalRequestors
func addAdditionalRequestor(ctx context.Context, k8sClient client.Client, maintenance *maintenancev1alpha1.NodeMaintenance) error {
	for _, requestor := range maintenance.Spec.AdditionalRequestors {
		if requestor == cutil.ProvisioningGroupName {
			// ProvisioningGroupName already exist in AdditionalRequestors
			return nil
		}
	}

	originalMaintenance := maintenance.DeepCopy()
	maintenance.Spec.AdditionalRequestors = append(maintenance.Spec.AdditionalRequestors, cutil.ProvisioningGroupName)
	patch := client.MergeFrom(originalMaintenance)
	if err := k8sClient.Patch(ctx, maintenance, patch); err != nil {
		return fmt.Errorf("failed to patch node maintenance %s, err: %v", originalMaintenance.Name, err)
	}
	return nil
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
			RequestorID: cutil.NodeMaintenanceRequestorID,
			NodeName:    nodeName,
			DrainSpec: &maintenancev1alpha1.DrainSpec{
				Force:          true,
				DeleteEmptyDir: true,
				PodSelector:    fmt.Sprintf("%s!=%s", cutil.ProvisioningComponentLabelKey, "dms"), //skip DMS pod
			},
			AdditionalRequestors: []string{cutil.ProvisioningGroupName},
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

func setDPUCondNodeEffectReadyToFalse(state *provisioningv1.DPUStatus, reason, message string) {
	cond := cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, "", "")
	cond.Status = metav1.ConditionFalse
	cond.Reason = reason
	cond.Message = message
	cutil.SetDPUCondition(state, cond)
}
