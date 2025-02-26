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

package webhooks

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-provisioning-dpu-nvidia-com-v1alpha1-dpu,mutating=false,failurePolicy=fail,sideEffects=None,groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=create;update,versions=v1alpha1,name=vdpu.kb.io,admissionReviewVersions=v1

// DPU implements the webhook for the DPU object.
type DPU struct{}

var _ webhook.CustomValidator = &DPU{}

// log is for logging in this package.
var dpulog = logf.Log.WithName("dpu-resource")

func (r *DPU) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&provisioningv1.DPU{}).
		WithValidator(r).
		Complete()
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DPU) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dpu, ok := obj.(*provisioningv1.DPU)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPU got %s", obj.GetObjectKind().GroupVersionKind().String()))
	}
	dpulog.V(4).Info("validate create", "name", dpu.Name)
	errs := field.ErrorList{}
	newPath := field.NewPath("spec")

	if err := ValidateNodeEffect(*dpu.Spec.NodeEffect); err != nil {
		errs = append(errs, field.Invalid(newPath.Child(".node_effect"), dpu.Spec.NodeEffect, err.Error()))
	}
	if err := reboot.ValidateHostPowerCycleRequire(dpu.Annotations); err != nil {
		errs = append(errs, field.Invalid(newPath.Child(".annotations", reboot.HostPowerCycleRequireKey), dpu.Annotations[reboot.HostPowerCycleRequireKey], err.Error()))
	}

	if len(errs) != 0 {
		return nil, apierrors.NewInvalid(schema.GroupKind{Group: "provisioning.dpu.nvidia.com", Kind: "DPUSet"},
			dpu.Name,
			errs)
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DPU) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	dpu, ok := newObj.(*provisioningv1.DPU)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPU got %s", newObj.GetObjectKind().GroupVersionKind().String()))
	}

	dpulog.V(4).Info("validate update", "name", dpu.Name)

	errs := field.ErrorList{}
	newPath := field.NewPath("spec")

	if err := ValidateNodeEffect(*dpu.Spec.NodeEffect); err != nil {
		errs = append(errs, field.Invalid(newPath.Child(".node_effect"), dpu.Spec.NodeEffect, err.Error()))
	}
	if len(errs) != 0 {
		return nil, apierrors.NewInvalid(schema.GroupKind{Group: "provisioning.dpu.nvidia.com", Kind: "DPUSet"},
			dpu.Name,
			errs)
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DPU) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
