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
	"encoding/binary"
	"fmt"
	"net"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-provisioning-dpu-nvidia-com-v1alpha1-dpudiscovery,mutating=false,failurePolicy=fail,sideEffects=None,groups=provisioning.dpu.nvidia.com,resources=dpudiscoveries,verbs=create;update,versions=v1alpha1,name=vdpudiscovery.kb.io,admissionReviewVersions=v1

// DPUDiscoveryValidator validates DPUDiscovery objects (e.g. startIP <= endIP).
type DPUDiscoveryValidator struct{}

var _ webhook.CustomValidator = &DPUDiscoveryValidator{}

var dpudiscoverylog = logf.Log.WithName("dpudiscovery-resource")

// SetupWebhookWithManager registers the webhook with the manager.
func (v *DPUDiscoveryValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&provisioningv1.DPUDiscovery{}).
		WithValidator(v).
		Complete()
}

// ValidateCreate validates the DPUDiscovery object on creation.
func (v *DPUDiscoveryValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dpuDiscovery, ok := obj.(*provisioningv1.DPUDiscovery)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a DPUDiscovery but got a %T", obj))
	}
	dpudiscoverylog.V(4).Info("validate create", "name", dpuDiscovery.Name)
	return nil, validateIPRangeOrder(dpuDiscovery.Spec.IPRangeSpec.IPRange)
}

// ValidateUpdate validates the DPUDiscovery object on update.
func (v *DPUDiscoveryValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	dpuDiscovery, ok := newObj.(*provisioningv1.DPUDiscovery)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a DPUDiscovery but got a %T", newObj))
	}
	dpudiscoverylog.V(4).Info("validate update", "name", dpuDiscovery.Name)
	return nil, validateIPRangeOrder(dpuDiscovery.Spec.IPRangeSpec.IPRange)
}

// ValidateDelete validates the DPUDiscovery object on deletion (no-op).
func (v *DPUDiscoveryValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// validateIPRangeOrder ensures startIP is numerically less than or equal to endIP (IPv4).
func validateIPRangeOrder(ipRange provisioningv1.IPRange) error {
	startIP := net.ParseIP(ipRange.StartIP)
	endIP := net.ParseIP(ipRange.EndIP)
	if startIP == nil || endIP == nil {
		return fmt.Errorf("startIP and endIP must be valid IP addresses")
	}
	start4 := startIP.To4()
	end4 := endIP.To4()
	if start4 == nil || end4 == nil {
		return fmt.Errorf("only IPv4 addresses are supported for IP range")
	}
	start := binary.BigEndian.Uint32(start4)
	end := binary.BigEndian.Uint32(end4)
	if start > end {
		return fmt.Errorf("startIP must be less than or equal to endIP (got startIP %s, endIP %s)", ipRange.StartIP, ipRange.EndIP)
	}
	return nil
}
