/*
Copyright 2026 NVIDIA

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

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	"github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/common"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-noderesources-dpu-nvidia-com-v1alpha1-nodesriovdevicepluginconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=noderesources.dpu.nvidia.com,resources=nodesriovdevicepluginconfigs,verbs=create;update,versions=v1alpha1,name=vnodesriovdevicepluginconfig.kb.io,admissionReviewVersions=v1

// NodeSRIOVDevicePluginConfigValidator implements a webhook for the
// NodeSRIOVDevicePluginConfig object.
type NodeSRIOVDevicePluginConfigValidator struct {
	// DefaultResourcePrefix is used when a resource entry omits `resourcePrefix`.
	DefaultResourcePrefix string
}

var _ webhook.CustomValidator = &NodeSRIOVDevicePluginConfigValidator{}

var log = logf.Log.WithName("nodesriovdevicepluginconfig-resource")

func (v *NodeSRIOVDevicePluginConfigValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&noderesourcesv1.NodeSRIOVDevicePluginConfig{}).
		WithValidator(v).
		Complete()
}

// validate is shared by create/update to keep behavior identical.
func (v *NodeSRIOVDevicePluginConfigValidator) validate(obj runtime.Object, operation string) (admission.Warnings, error) {
	cfg, ok := obj.(*noderesourcesv1.NodeSRIOVDevicePluginConfig)
	if !ok {
		return admission.Warnings{}, apierrors.NewBadRequest(fmt.Sprintf(
			"invalid object type expected NodeSRIOVDevicePluginConfig got %s",
			obj.GetObjectKind().GroupVersionKind().String(),
		))
	}

	log.V(1).Info("validate "+operation, "name", cfg.Name)

	errs := common.ValidateNodeSRIOVDevicePluginConfig(
		v.DefaultResourcePrefix,
		cfg,
	)
	if len(errs) != 0 {
		return nil, apierrors.NewInvalid(
			schema.GroupKind{
				Group: noderesourcesv1.GroupVersion.Group,
				Kind:  noderesourcesv1.NodeSRIOVDevicePluginConfigKind,
			},
			cfg.Name,
			errs,
		)
	}

	return nil, nil
}

// ValidateCreate validates resource name/prefix uniqueness and VF ranges.
func (v *NodeSRIOVDevicePluginConfigValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return v.validate(obj, "create")
}

// ValidateUpdate validates resource name/prefix uniqueness and VF ranges.
func (v *NodeSRIOVDevicePluginConfigValidator) ValidateUpdate(ctx context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	return v.validate(newObj, "update")
}

func (v *NodeSRIOVDevicePluginConfigValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// No validation needed for delete operations.
	return nil, nil
}
