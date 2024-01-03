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
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-provisioning-dpu-nvidia-com-v1alpha1-dpuflavor,mutating=false,failurePolicy=fail,sideEffects=None,groups=provisioning.dpu.nvidia.com,resources=dpuflavors,verbs=create;update;delete,versions=v1alpha1,name=vdpuflavor.kb.io,admissionReviewVersions=v1

// DPUFlavor implements a webhook for the DPUFlavor object.
type DPUFlavor struct{}

var _ webhook.CustomValidator = &DPUFlavor{}

// log is for logging in this package.
var (
	dpuflavorlog = logf.Log.WithName("dpuflavor-resource")
	manager      ctrl.Manager
)

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *DPUFlavor) SetupWebhookWithManager(mgr ctrl.Manager) error {
	manager = mgr
	return ctrl.NewWebhookManagedBy(mgr).
		For(&provisioningv1.DPUFlavor{}).
		WithValidator(r).
		Complete()
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DPUFlavor) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dpuFlavor, ok := obj.(*provisioningv1.DPUFlavor)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPUFlavor got %s", obj.GetObjectKind().GroupVersionKind().String()))
	}

	dpuflavorlog.V(4).Info("validate create", "name", dpuFlavor.Name)

	if err := validateResources(dpuFlavor); err != nil {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("resources are misconfigured: %s", err.Error()))
	}

	return nil, validateNVConfig(dpuFlavor)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DPUFlavor) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	dpuFlavor, ok := newObj.(*provisioningv1.DPUFlavor)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPUFlavor got %s", newObj.GetObjectKind().GroupVersionKind().String()))
	}
	dpuflavorlog.V(4).Info("validate update", "name", dpuFlavor.Name)
	// This is a no-op as this type is immutable. The immutability validation is done inside the CRD definition.
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DPUFlavor) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dpuFlavor, ok := obj.(*provisioningv1.DPUFlavor)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected DPUFlavor got %s", obj.GetObjectKind().GroupVersionKind().String()))
	}

	dpuflavorlog.V(4).Info("validate delete", "name", dpuFlavor.Name)
	dpuSetList := &provisioningv1.DPUSetList{}
	if err := manager.GetClient().List(ctx, dpuSetList, &client.ListOptions{Namespace: dpuFlavor.Namespace}); err != nil {
		return nil, fmt.Errorf("list DPUSets failed, err: %v", err)
	}
	var ref []string
	for _, ds := range dpuSetList.Items {
		if ds.Spec.DPUTemplate.Spec.DPUFlavor == dpuFlavor.Name {
			ref = append(ref, ds.Name)
		}
	}
	if len(ref) > 0 {
		return nil, fmt.Errorf("DPUFlavor is being referred to by DPUSet(s) %s, you must delete the DPUSet(s) first", ref)
	}

	dpuList := &provisioningv1.DPUList{}
	if err := manager.GetClient().List(ctx, dpuList, &client.ListOptions{Namespace: dpuFlavor.Namespace}); err != nil {
		return nil, fmt.Errorf("list DPUs failed, err: %v", err)
	}
	for _, dpu := range dpuList.Items {
		if dpu.Spec.DPUFlavor == dpuFlavor.Name {
			ref = append(ref, dpu.Name)
		}
	}
	if len(ref) > 0 {
		return nil, fmt.Errorf("DPUFlavor is being refferred to by DPU(s) %s, you must delete the DPU(s) first", ref)
	}
	return nil, nil
}

func validateNVConfig(flavor *provisioningv1.DPUFlavor) error {
	if len(flavor.Spec.NVConfig) == 0 {
		return nil
	} else if len(flavor.Spec.NVConfig) > 1 {
		return fmt.Errorf("there must be at most one element in nvconfig, and the element applies to all devices on a host")
	}
	nvcfg := flavor.Spec.NVConfig[0]
	// TODO: support regex device?
	if nvcfg.Device != nil && strings.TrimSpace(*nvcfg.Device) != "*" {
		return fmt.Errorf("nvconfig[*].device must be \"*\"")
	}
	return nil
}

// validateResources validates the resource related fields
func validateResources(flavor *provisioningv1.DPUFlavor) error {
	if flavor.Spec.SystemReservedResources != nil && flavor.Spec.DPUResources == nil {
		return errors.New("spec.systemReservedResources must not be specified if spec.dpuResources are not specified")
	}

	_, err := dpfutils.GetAllocatableResources(flavor.Spec.DPUResources, flavor.Spec.SystemReservedResources)
	if err != nil {
		e := &dpfutils.ResourcesExceedError{}
		if errors.As(err, &e) {
			return fmt.Errorf("reserved resource specified in spec.systemReservedResources exceed the ones defined in spec.dpuResources: Additional resources needed in spec.dpuResources: %v", e.AdditionalResourcesRequired)
		}
		return err
	}
	return nil
}
