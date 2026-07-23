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

package encryptionconfig

import (
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
)

const (
	annotationPrefix = "kamaji-cluster-manager.dpu.nvidia.com/"

	// ConfigFileName is the key holding the rendered EncryptionConfiguration in the per-cluster Secret.
	ConfigFileName = "encryption-config.yaml"
	// ProviderAnnotation records the encryption provider committed to the per-cluster Secret.
	ProviderAnnotation = annotationPrefix + "etcd-encryption-provider"
)

// Phase is the persisted staticKey configuration phase.
type Phase string

const (
	// PhaseIdle means no staticKey rotation is in progress.
	PhaseIdle Phase = "Idle"
	// PhasePrepared means the config contains active then target keys.
	PhasePrepared Phase = "Prepared"
	// PhasePromoted means the config contains target then old active keys.
	PhasePromoted Phase = "Promoted"
	// PhaseFinalized means the config contains only the target key before final reload.
	PhaseFinalized Phase = "Finalized"
)

// Config is the provider-neutral API for a validated encryption config Secret.
type Config interface {
	Provider() operatorv1.EtcdEncryptionAtRestProvider
	NamespacedName() types.NamespacedName
}

// StaticKey exposes staticKey-specific operations while hiding Secret details.
type StaticKey interface {
	Config
	Phase() Phase
	ConfigHash() string
	ActiveKeyRef() provisioningv1.ObservedSecretKeyRef
	TransitionToPrepared(SourceKey) (StaticKey, error)
	TransitionToPromoted() (StaticKey, error)
	RotationID() (string, error)
	TransitionToFinalized() (StaticKey, error)
	TransitionToIdle() (StaticKey, error)
}

// VaultKMS is a sealed marker interface for a validated VaultKMS encryption config Secret.
type VaultKMS interface {
	Config
	isVaultKMS()
}

// SourceKey is the source Secret key bytes and observed identity used for staticKey.
type SourceKey struct {
	Key []byte
	Ref provisioningv1.ObservedSecretKeyRef
}

// OwnershipError reports a per-cluster Secret that exists but is not owned by the expected DPUCluster.
type OwnershipError struct {
	Secret types.NamespacedName
	Owner  types.NamespacedName
}

// Error returns the ownership mismatch message.
func (e *OwnershipError) Error() string {
	return fmt.Sprintf("encryption config secret %s is not owned by DPUCluster %s", e.Secret, e.Owner)
}

// ValidationError reports malformed Secret content.
type ValidationError struct {
	Message string
}

// Error returns the validation message.
func (e *ValidationError) Error() string {
	return e.Message
}

// TransitionError reports an invalid staticKey phase transition.
type TransitionError struct {
	From    Phase
	To      Phase
	Message string
}

// Error returns the invalid transition message.
func (e *TransitionError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("cannot transition staticKey config from %s to %s: %s", e.From, e.To, e.Message)
	}
	return fmt.Sprintf("cannot transition staticKey config from %s to %s", e.From, e.To)
}

// validationErrorf formats a Secret validation error.
func validationErrorf(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// transitionErrorf formats an invalid phase transition error.
func transitionErrorf(from, to Phase, format string, args ...any) error {
	return &TransitionError{From: from, To: to, Message: fmt.Sprintf(format, args...)}
}
