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
	"errors"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-provisioning-dpu-nvidia-com-v1alpha1-dpuset,mutating=true,failurePolicy=fail,sideEffects=None,groups=provisioning.dpu.nvidia.com,resources=dpusets,verbs=create;update,versions=v1alpha1,name=mdpuset.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-provisioning-dpu-nvidia-com-v1alpha1-dpuset,mutating=false,failurePolicy=fail,sideEffects=None,groups=provisioning.dpu.nvidia.com,resources=dpusets,verbs=create;update,versions=v1alpha1,name=vdpuset.kb.io,admissionReviewVersions=v1

// DPUSet implements the webhooks for the DPUSet type.
type DPUSet struct {
	// DeploymentMode is the cluster-wide DPF deployment mode (e.g. zero-trust vs host-trusted),
	// matching DPFOperatorConfig and provisioning controller flags. Required for Astra validation.
	DeploymentMode string
}

var _ webhook.CustomDefaulter = &DPUSet{}
var _ webhook.CustomValidator = &DPUSet{}

// log is for logging in this package.
var dpusetlog = logf.Log.WithName("dpuset-resource")

func (r *DPUSet) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&provisioningv1.DPUSet{}).
		WithDefaulter(r).
		WithValidator(r).
		Complete()
}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *DPUSet) Default(ctx context.Context, obj runtime.Object) error {
	dpuSet, ok := obj.(*provisioningv1.DPUSet)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPUSet got %s", obj.GetObjectKind().GroupVersionKind().String()))
	}
	dpusetlog.V(4).Info("default", "name", dpuSet.Name)

	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DPUSet) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dpuSet, ok := obj.(*provisioningv1.DPUSet)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPUSet got %s", obj.GetObjectKind().GroupVersionKind().String()))
	}

	dpusetlog.V(4).Info("validate create", "name", dpuSet.Name)
	errs := field.ErrorList{}
	newPath := field.NewPath("spec")
	if err := validateStrategy(dpuSet.Spec.Strategy); err != nil {
		errs = append(errs, field.Invalid(newPath.Child("strategy"), dpuSet.Spec.Strategy, err.Error()))

	}

	// Exactly-one-of dpuFlavor/dpuFlavorTemplate is enforced by the CRD XValidation rule.
	dpuTemplatePath := newPath.Child("dpuTemplate", "spec")
	if dpuSet.Spec.DPUTemplate.Spec.BFB != nil && dpuSet.Spec.DPUTemplate.Spec.BFB.Name == "" {
		errs = append(errs, field.Required(dpuTemplatePath.Child("bfb", "name"), "bfb.name must be non-empty"))
	}

	if err := reboot.ValidateHostPowerCycleRequire(dpuSet.Spec.DPUTemplate.Annotations); err != nil {
		errs = append(errs, field.Invalid(newPath.Child("dpu_template.annotations", reboot.HostPowerCycleRequireKey), dpuSet.Annotations[reboot.HostPowerCycleRequireKey], err.Error()))
	}

	if err := r.validateAstraEnabledInstallInterface(newPath, dpuSet.Spec); err != nil {
		errs = append(errs, err)
	}
	if len(errs) != 0 {
		return nil, apierrors.NewInvalid(schema.GroupKind{Group: "provisioning.dpu.nvidia.com", Kind: "DPUSet"},
			dpuSet.Name,
			errs)
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DPUSet) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	dpuSet, ok := newObj.(*provisioningv1.DPUSet)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPUSet got %s", newObj.GetObjectKind().GroupVersionKind().String()))
	}

	dpusetlog.V(4).Info("validate update", "name", dpuSet.Name)
	errs := field.ErrorList{}
	newPath := field.NewPath("spec")
	if err := validateStrategy(dpuSet.Spec.Strategy); err != nil {
		errs = append(errs, field.Invalid(newPath.Child("strategy"), dpuSet.Spec.Strategy, err.Error()))

	}

	// Exactly-one-of dpuFlavor/dpuFlavorTemplate is enforced by the CRD XValidation rule.
	dpuTemplatePath := newPath.Child("dpuTemplate", "spec")
	if dpuSet.Spec.DPUTemplate.Spec.BFB != nil && dpuSet.Spec.DPUTemplate.Spec.BFB.Name == "" {
		errs = append(errs, field.Required(dpuTemplatePath.Child("bfb", "name"), "bfb.name must be non-empty"))
	}

	if err := r.validateAstraEnabledInstallInterface(newPath, dpuSet.Spec); err != nil {
		errs = append(errs, err)
	}

	if len(errs) != 0 {
		return nil, apierrors.NewInvalid(schema.GroupKind{Group: "provisioning.dpu.nvidia.com", Kind: "DPUSet"},
			dpuSet.Name,
			errs)
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DPUSet) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (r *DPUSet) validateAstraEnabledInstallInterface(path *field.Path, spec provisioningv1.DPUSetSpec) *field.Error {
	if spec.DPUTemplate.Spec.AstraEnabled == nil || !*spec.DPUTemplate.Spec.AstraEnabled {
		return nil
	}
	if (dutil.DPUOptions{DeploymentMode: r.DeploymentMode}).ZeroTrustProvisioningFlow() {
		return nil
	}
	return field.Invalid(
		path.Child("dpuTemplate", "spec", "astraEnabled"),
		true,
		"astraEnabled=true requires zero-trust deployment mode",
	)
}

func validateStrategy(strategy provisioningv1.DPUSetStrategy) error {
	if strategy.Type == provisioningv1.RollingUpdateStrategyType {
		//nolint:staticcheck // SA1019: MaxUnavailable is deprecated but still supported
		if strategy.RollingUpdate == nil || strategy.RollingUpdate.MaxUnavailable == nil {
			return nil
		}
		//nolint:staticcheck // SA1019: MaxUnavailable is deprecated but still supported
		switch strategy.RollingUpdate.MaxUnavailable.Type {
		case intstr.String:
			//nolint:staticcheck // SA1019: MaxUnavailable is deprecated but still supported
			if scaledValue, err := intstr.GetScaledValueFromIntOrPercent(strategy.RollingUpdate.MaxUnavailable, 100, false); err != nil {
				return err
			} else {
				if scaledValue <= 0 || scaledValue > 100 {
					return errors.New("the value range of maxUnavailable must be greater than 0% and less than or equal to 100%")
				}
			}

		case intstr.Int:
			//nolint:staticcheck // SA1019: MaxUnavailable is deprecated but still supported
			if strategy.RollingUpdate.MaxUnavailable.IntVal <= 0 {
				return errors.New("the value range of maxUnavailable must be greater 0")
			}

		}
	}

	return nil
}
