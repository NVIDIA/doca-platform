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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-provisioning-dpu-nvidia-com-v1alpha1-bfb,mutating=true,failurePolicy=fail,sideEffects=None,groups=provisioning.dpu.nvidia.com,resources=bfbs,verbs=create;update,versions=v1alpha1,name=mbfb.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-provisioning-dpu-nvidia-com-v1alpha1-bfb,mutating=false,failurePolicy=fail,sideEffects=None,groups=provisioning.dpu.nvidia.com,resources=bfbs,verbs=create;update;delete,versions=v1alpha1,name=vbfb.kb.io,admissionReviewVersions=v1

// BFB implements a webhook for the BFB object.
type BFB struct{}

var _ webhook.CustomDefaulter = &BFB{}
var _ webhook.CustomValidator = &BFB{}

const (
	BFBFileNameExtension = ".bfb"
)

var (
	// log is for logging in this package.
	bfblog = logf.Log.WithName("bfb-resource")
	bfbMgr ctrl.Manager
)

func (r *BFB) SetupWebhookWithManager(mgr ctrl.Manager) error {
	bfbMgr = mgr
	return ctrl.NewWebhookManagedBy(mgr).
		For(&provisioningv1.BFB{}).
		WithDefaulter(r).
		WithValidator(r).
		Complete()
}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *BFB) Default(ctx context.Context, obj runtime.Object) error {
	bfb, ok := obj.(*provisioningv1.BFB)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected BFB got %s", obj.GetObjectKind().GroupVersionKind().String()))
	}

	bfblog.V(4).Info("default", "name", bfb.Name)
	if bfb.Spec.FileName == "" {
		bfb.Spec.FileName = fmt.Sprintf("%s-%s%s", bfb.Namespace, bfb.Name, BFBFileNameExtension)
	}
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *BFB) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *BFB) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *BFB) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	bfb, ok := obj.(*provisioningv1.BFB)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected BFB got %s", obj.GetObjectKind().GroupVersionKind().String()))
	}

	bfblog.V(4).Info("validate delete", "name", bfb.Name)

	dpusetList := &provisioningv1.DPUSetList{}
	if err := bfbMgr.GetClient().List(context.TODO(), dpusetList); err != nil {
		return nil, fmt.Errorf("list DPUSets failed, err: %v", err)
	}
	var ref []string
	for _, ds := range dpusetList.Items {
		if ds.Spec.DPUTemplate.Spec.BFB.Name == bfb.Name {
			ref = append(ref, ds.Name)
		}
	}
	if len(ref) > 0 {
		return nil, fmt.Errorf("being referred to by DPUSet(s) %s, you must delete the DPUSet(s) first", ref)
	}

	dpuList := &provisioningv1.DPUList{}
	if err := bfbMgr.GetClient().List(context.TODO(), dpuList); err != nil {
		return nil, fmt.Errorf("list DPUs failed, err: %v", err)
	}
	for _, dpu := range dpuList.Items {
		if dpu.Spec.BFB == dpu.Name {
			ref = append(ref, dpu.Name)
		}
	}
	if len(ref) > 0 {
		return nil, fmt.Errorf("being refferred to by DPU(s) %s, you must delete the DPU(s) first", ref)
	}
	return nil, nil
}
