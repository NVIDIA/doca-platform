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

package webhooks

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-provisioning-dpu-nvidia-com-v1alpha1-dpunode,mutating=false,failurePolicy=fail,sideEffects=None,groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=create;update,versions=v1alpha1,name=vdpunode.kb.io,admissionReviewVersions=v1

// DPUNode implements a webhook for the DPUNode object.
type DPUNode struct {
	Client client.Client
}

var _ webhook.CustomValidator = &DPUNode{}

// log is for logging in this package.
var dpunodelog = logf.Log.WithName("dpunode-resource")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *DPUNode) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&provisioningv1.DPUNode{}).
		WithValidator(r).
		Complete()
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DPUNode) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dpuNode, ok := obj.(*provisioningv1.DPUNode)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPUNode got %s", obj.GetObjectKind().GroupVersionKind().String()))
	}

	dpunodelog.V(4).Info("validate create", "name", dpuNode.Name)

	errs := field.ErrorList{}
	specPath := field.NewPath("spec")

	// Validate DPU name uniqueness
	if err := r.validateDPUNameUniqueness(ctx, dpuNode); err != nil {
		errs = append(errs, field.Duplicate(specPath.Child("dpus"), err.Error()))
	}

	if len(errs) != 0 {
		return nil, apierrors.NewInvalid(schema.GroupKind{Group: cutil.ProvisioningGroupName, Kind: "DPUNode"},
			dpuNode.Name,
			errs)
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DPUNode) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	dpuNode, ok := newObj.(*provisioningv1.DPUNode)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPUNode got %s", newObj.GetObjectKind().GroupVersionKind().String()))
	}

	dpunodelog.V(4).Info("validate update", "name", dpuNode.Name)

	errs := field.ErrorList{}
	specPath := field.NewPath("spec")

	// Validate DPU name uniqueness only if DPUs have changed
	oldDpuNode, ok := oldObj.(*provisioningv1.DPUNode)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid old object type expected DPUNode got %s", oldObj.GetObjectKind().GroupVersionKind().String()))
	}

	// Only check uniqueness if the DPUs list has changed
	if !r.dpuListsEqual(oldDpuNode.Spec.DPUs, dpuNode.Spec.DPUs) {
		if err := r.validateDPUNameUniqueness(ctx, dpuNode); err != nil {
			errs = append(errs, field.Duplicate(specPath.Child("dpus"), err.Error()))
		}
	}

	if len(errs) != 0 {
		return nil, apierrors.NewInvalid(schema.GroupKind{Group: cutil.ProvisioningGroupName, Kind: "DPUNode"},
			dpuNode.Name,
			errs)
	}

	if errs = r.validateUpdateNodeRebootMethod(ctx, oldDpuNode, dpuNode); len(errs) != 0 {
		return nil, apierrors.NewInvalid(schema.GroupKind{Group: cutil.ProvisioningGroupName, Kind: "DPUNode"},
			dpuNode.Name,
			errs)
	}

	return nil, nil
}

func (r *DPUNode) validateUpdateNodeRebootMethod(ctx context.Context, oldDpuNode *provisioningv1.DPUNode, dpuNode *provisioningv1.DPUNode) field.ErrorList {
	errs := field.ErrorList{}
	specPath := field.NewPath("spec")
	dpuRebootMethodPath := specPath.Child("nodeRebootMethod")
	if oldDpuNode.Spec.NodeRebootMethod == nil && dpuNode.Spec.NodeRebootMethod == nil {
		return nil
	}

	if oldDpuNode.Spec.NodeRebootMethod != nil && dpuNode.Spec.NodeRebootMethod != nil && *oldDpuNode.Spec.NodeRebootMethod == *dpuNode.Spec.NodeRebootMethod {
		return nil
	}

	// Build label selector to find DPUs with matching DPUDeviceName labels
	if len(dpuNode.Spec.DPUs) == 0 {
		return nil
	}
	dpuDeviceNames := []string{}
	for _, dpu := range dpuNode.Spec.DPUs {
		dpuDeviceNames = append(dpuDeviceNames, dpu.Name)
	}

	// Create a requirement that matches any of the DPU device names using In operator
	selector := labels.NewSelector()
	if len(dpuDeviceNames) > 0 {
		req, err := labels.NewRequirement(cutil.DPUDeviceNameLabel, selection.In, dpuDeviceNames)
		if err != nil {
			errs = append(errs, field.InternalError(dpuRebootMethodPath, err))
			return errs
		}
		selector = selector.Add(*req)
	}

	dpuList := &provisioningv1.DPUList{}
	if err := r.Client.List(ctx, dpuList,
		client.InNamespace(dpuNode.Namespace),
		client.MatchingLabelsSelector{Selector: selector}); err != nil {
		errs = append(errs, field.InternalError(dpuRebootMethodPath, err))
		return errs
	}

	for _, dpu := range dpuList.Items {
		if dpu.Status.Phase != provisioningv1.DPUReady {
			errs = append(errs, field.Invalid(dpuRebootMethodPath, dpuNode.Spec.NodeRebootMethod, fmt.Sprintf("Node Reboot Method is not allowed to be updated when DPU %s is not ready", dpu.Name)))
		}
	}

	if len(errs) != 0 {
		return errs
	}

	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DPUNode) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// No validation needed for delete operations
	return nil, nil
}

// validateDPUNameUniqueness checks if the DPU names are unique across all DPUNode resources
func (r *DPUNode) validateDPUNameUniqueness(ctx context.Context, dpuNode *provisioningv1.DPUNode) error {
	if len(dpuNode.Spec.DPUs) == 0 {
		return nil // No DPUs to validate
	}

	// List all DPUNode resources
	var dpuNodeList provisioningv1.DPUNodeList
	if err := r.Client.List(ctx, &dpuNodeList); err != nil {
		dpunodelog.Error(err, "failed to list DPUNode resources")
		return fmt.Errorf("failed to validate DPU name uniqueness: %w", err)
	}

	// Create a map to track DPU names and their owners
	dpuNameToOwner := make(map[string]string)

	// Check for duplicate DPU names
	for _, existingNode := range dpuNodeList.Items {
		// Skip the current node if this is an update operation
		if existingNode.Name == dpuNode.Name && existingNode.Namespace == dpuNode.Namespace {
			continue
		}

		// Check each DPU in the existing node
		for _, dpuRef := range existingNode.Spec.DPUs {
			dpuNameToOwner[dpuRef.Name] = fmt.Sprintf("%s/%s", existingNode.Namespace, existingNode.Name)
		}
	}

	// Check if any DPU names in the current node are already in use
	for _, dpuRef := range dpuNode.Spec.DPUs {
		if owner, exists := dpuNameToOwner[dpuRef.Name]; exists {
			return fmt.Errorf("DPU name %s is already in use by DPUNode %s", dpuRef.Name, owner)
		}
	}

	return nil
}

// dpuListsEqual compares two DPU reference lists for equality
func (r *DPUNode) dpuListsEqual(list1, list2 []provisioningv1.DPURef) bool {
	if len(list1) != len(list2) {
		return false
	}

	// Create maps to compare the lists
	map1 := make(map[string]bool)
	map2 := make(map[string]bool)

	for _, dpu := range list1 {
		map1[dpu.Name] = true
	}

	for _, dpu := range list2 {
		map2[dpu.Name] = true
	}

	// Compare the maps
	for name := range map1 {
		if !map2[name] {
			return false
		}
	}

	for name := range map2 {
		if !map1[name] {
			return false
		}
	}

	return true
}
