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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-provisioning-dpu-nvidia-com-v1alpha1-dpudevice,mutating=false,failurePolicy=fail,sideEffects=None,groups=provisioning.dpu.nvidia.com,resources=dpudevices,verbs=create;update,versions=v1alpha1,name=vdpudevice.kb.io,admissionReviewVersions=v1

// DPUDevice implements a webhook for the DPUDevice object.
type DPUDevice struct {
	Client client.Client
}

var _ webhook.CustomValidator = &DPUDevice{}

// log is for logging in this package.
var dpudevicelog = logf.Log.WithName("dpudevice-resource")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *DPUDevice) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&provisioningv1.DPUDevice{}).
		WithValidator(r).
		Complete()
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DPUDevice) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dpuDevice, ok := obj.(*provisioningv1.DPUDevice)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPUDevice got %s", obj.GetObjectKind().GroupVersionKind().String()))
	}

	dpudevicelog.V(4).Info("validate create", "name", dpuDevice.Name)

	errs := field.ErrorList{}
	specPath := field.NewPath("spec")

	// Validate serial number uniqueness
	if err := r.validateSerialNumberUniqueness(ctx, dpuDevice); err != nil {
		errs = append(errs, field.Duplicate(specPath.Child("serialNumber"), dpuDevice.Spec.SerialNumber))
	}

	if len(errs) != 0 {
		return nil, apierrors.NewInvalid(schema.GroupKind{Group: "provisioning.dpu.nvidia.com", Kind: "DPUDevice"},
			dpuDevice.Name,
			errs)
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DPUDevice) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	dpuDevice, ok := newObj.(*provisioningv1.DPUDevice)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPUDevice got %s", newObj.GetObjectKind().GroupVersionKind().String()))
	}

	dpudevicelog.V(4).Info("validate update", "name", dpuDevice.Name)

	errs := field.ErrorList{}
	specPath := field.NewPath("spec")

	// Validate serial number uniqueness only if it has changed
	oldDpuDevice, ok := oldObj.(*provisioningv1.DPUDevice)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid old object type expected DPUDevice got %s", oldObj.GetObjectKind().GroupVersionKind().String()))
	}

	// Only check uniqueness if the serial number has changed
	if oldDpuDevice.Spec.SerialNumber != dpuDevice.Spec.SerialNumber {
		if err := r.validateSerialNumberUniqueness(ctx, dpuDevice); err != nil {
			errs = append(errs, field.Duplicate(specPath.Child("serialNumber"), dpuDevice.Spec.SerialNumber))
		}
	}

	if len(errs) != 0 {
		return nil, apierrors.NewInvalid(schema.GroupKind{Group: "provisioning.dpu.nvidia.com", Kind: "DPUDevice"},
			dpuDevice.Name,
			errs)
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DPUDevice) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// No validation needed for delete operations
	return nil, nil
}

// validateSerialNumberUniqueness checks if the serial number is unique across all DPUDevice resources
func (r *DPUDevice) validateSerialNumberUniqueness(ctx context.Context, dpuDevice *provisioningv1.DPUDevice) error {
	if dpuDevice.Spec.SerialNumber == "" {
		return nil // Empty serial numbers are handled by the required validation
	}

	// List all DPUDevice resources
	var dpuDeviceList provisioningv1.DPUDeviceList
	if err := r.Client.List(ctx, &dpuDeviceList); err != nil {
		dpudevicelog.Error(err, "failed to list DPUDevice resources")
		return fmt.Errorf("failed to validate serial number uniqueness: %w", err)
	}

	// Check for duplicate serial numbers
	for _, existingDevice := range dpuDeviceList.Items {
		// Skip the current device if this is an update operation
		if existingDevice.Name == dpuDevice.Name && existingDevice.Namespace == dpuDevice.Namespace {
			continue
		}

		if existingDevice.Spec.SerialNumber == dpuDevice.Spec.SerialNumber {
			return fmt.Errorf("serial number %s is already in use by DPUDevice %s/%s",
				dpuDevice.Spec.SerialNumber, existingDevice.Namespace, existingDevice.Name)
		}
	}

	return nil
}
