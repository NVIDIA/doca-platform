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
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-provisioning-dpu-nvidia-com-v1alpha1-bfb,mutating=false,failurePolicy=fail,sideEffects=None,groups=provisioning.dpu.nvidia.com,resources=bfbs,verbs=create;update,versions=v1alpha1,name=vbfb.kb.io,admissionReviewVersions=v1

// BFB implements a webhook for the BFB object.
type BFB struct{}

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
		WithValidator(r).
		Complete()
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *BFB) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	bfb, ok := obj.(*provisioningv1.BFB)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid object type expected BFB got %s", obj.GetObjectKind().GroupVersionKind().String()))
	}

	bfblog.V(4).Info("validate create", "name", bfb.Name)

	// Check uniqueness of spec.fileName
	if bfb.Spec.FileName != nil {
		if conflict, conflictName, err := isFileNameConflict(bfbMgr.GetClient(), *bfb.Spec.FileName, "", ""); err != nil {
			return admission.Warnings{}, apierrors.NewInternalError(fmt.Errorf("failed to check fileName uniqueness: %v", err))
		} else if conflict {
			return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("spec.fileName '%s' is already used by BFB '%s'", *bfb.Spec.FileName, conflictName))
		}
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *BFB) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	newBFB, ok := newObj.(*provisioningv1.BFB)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("invalid new object type expected BFB got %s", newObj.GetObjectKind().GroupVersionKind().String()))
	}

	bfblog.V(4).Info("validate update", "name", newBFB.Name)

	// Check uniqueness of spec.fileName (skip self)
	if newBFB.Spec.FileName != nil {
		if conflict, conflictName, err := isFileNameConflict(bfbMgr.GetClient(), *newBFB.Spec.FileName, newBFB.Namespace, newBFB.Name); err != nil {
			return admission.Warnings{}, apierrors.NewInternalError(fmt.Errorf("failed to check fileName uniqueness: %v", err))
		} else if conflict {
			return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf("spec.fileName '%s' is already used by BFB '%s'", *newBFB.Spec.FileName, conflictName))
		}
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *BFB) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// Deletion validation moved to controller for better observability via conditions
	return nil, nil
}

// isFileNameConflict returns true and the conflicting BFB's namespaced name if any BFB (except skipNamespace/skipName) uses fileName.
func isFileNameConflict(client client.Client, fileName, skipNamespace, skipName string) (conflict bool, conflictName string, err error) {
	var bfbList provisioningv1.BFBList
	if err := client.List(context.TODO(), &bfbList); err != nil {
		return false, "", fmt.Errorf("listing BFBs: %w", err)
	}
	for _, b := range bfbList.Items {
		if b.Spec.FileName == nil || *b.Spec.FileName != fileName {
			continue
		}
		if b.Namespace == skipNamespace && b.Name == skipName {
			continue
		}
		return true, fmt.Sprintf("%s/%s", b.Namespace, b.Name), nil
	}
	return false, "", nil
}
