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
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	apiserverv1 "k8s.io/apiserver/pkg/apis/apiserver/v1"
)

const (
	// staticKeyNamePrefix prefixes generated AES-GCM key names in the rendered config.
	staticKeyNamePrefix = "key"

	staticKeyAnnotationPrefix = annotationPrefix + "static-key-"

	// staticKeyActiveSecretNameAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the source Secret name for the first/write staticKey, rebuilds DPUCluster status,
	// and is retained when rotation completes.
	staticKeyActiveSecretNameAnnotation = staticKeyAnnotationPrefix + "active-secret-name"
	// staticKeyActiveSecretKeyAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the source Secret data key for the first/write staticKey, rebuilds DPUCluster status,
	// and is retained when rotation completes.
	staticKeyActiveSecretKeyAnnotation = staticKeyAnnotationPrefix + "active-secret-key"
	// staticKeyActiveSecretNamespaceAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the source Secret namespace for the first/write staticKey, rebuilds DPUCluster status,
	// and is retained when rotation completes.
	staticKeyActiveSecretNamespaceAnnotation = staticKeyAnnotationPrefix + "active-secret-namespace"
	// staticKeyActiveSecretUIDAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the source Secret UID for the first/write staticKey, rebuilds DPUCluster status,
	// and is retained when rotation completes.
	staticKeyActiveSecretUIDAnnotation = staticKeyAnnotationPrefix + "active-secret-uid"
	// staticKeyActiveSecretRVAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the source Secret resourceVersion for the first/write staticKey, rebuilds DPUCluster
	// status, and is retained when rotation completes.
	staticKeyActiveSecretRVAnnotation = staticKeyAnnotationPrefix + "active-secret-resource-version"
	// staticKeyPhaseAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the persisted staticKey configuration phase for crash recovery and is always present.
	staticKeyPhaseAnnotation = staticKeyAnnotationPrefix + "rotation-state"
	// staticKeyRotationTargetSecretNameAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the in-flight target source Secret name for crash recovery and is cleared in Idle.
	staticKeyRotationTargetSecretNameAnnotation = staticKeyAnnotationPrefix + "rotation-target-secret-name"
	// staticKeyRotationTargetSecretKeyAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the in-flight target source Secret data key for crash recovery and is cleared in Idle.
	staticKeyRotationTargetSecretKeyAnnotation = staticKeyAnnotationPrefix + "rotation-target-secret-key"
	// staticKeyRotationTargetSecretNamespaceAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the in-flight target source Secret namespace for crash recovery and is cleared in Idle.
	staticKeyRotationTargetSecretNamespaceAnnotation = staticKeyAnnotationPrefix + "rotation-target-secret-namespace"
	// staticKeyRotationTargetSecretUIDAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the in-flight target source Secret UID for crash recovery and is cleared in Idle.
	staticKeyRotationTargetSecretUIDAnnotation = staticKeyAnnotationPrefix + "rotation-target-secret-uid"
	// staticKeyRotationTargetSecretRVAnnotation is maintained on the per-cluster encryption config Secret.
	// It records the in-flight target source Secret resourceVersion for crash recovery and is cleared in Idle.
	staticKeyRotationTargetSecretRVAnnotation = staticKeyAnnotationPrefix + "rotation-target-secret-resource-version"
)

// rotationIDLength is the digest length used in tenant StorageVersionMigration names.
const rotationIDLength = 32

// staticKeyConfig wraps a validated staticKey encryption config Secret.
type staticKeyConfig struct {
	baseConfig
	phase     Phase
	keys      []keyEntry
	activeRef provisioningv1.ObservedSecretKeyRef
	targetRef *provisioningv1.ObservedSecretKeyRef
	hash      string
}

// keyEntry is the internal ordered AES-GCM key representation.
type keyEntry struct {
	Name   string
	Secret string
}

// newStaticKeyConfig builds the initial Idle staticKey config Secret wrapper.
func newStaticKeyConfig(key types.NamespacedName, labels map[string]string, source SourceKey) (*staticKeyConfig, error) {
	base64Key, err := validateSourceKey(source)
	if err != nil {
		return nil, err
	}
	keyName := generateStaticKeyName(time.Now())
	rendered, err := renderStaticKeyEncryptionConfig([]keyEntry{{Name: keyName, Secret: base64Key}})
	if err != nil {
		return nil, err
	}
	secret := baseSecret(key, labels, operatorv1.EtcdEncryptionProviderStaticKey)
	secret.Data[ConfigFileName] = rendered
	secret.Annotations[staticKeyPhaseAnnotation] = string(PhaseIdle)
	setStaticKeyActiveRef(secret, source.Ref)
	return parseStaticKey(secret)
}

// parseStaticKey validates a Secret as a staticKey config and returns its wrapper.
func parseStaticKey(secret *corev1.Secret) (*staticKeyConfig, error) {
	cfg, raw, err := parseEncryptionConfiguration(secret)
	if err != nil {
		return nil, err
	}
	providers := cfg.Resources[0].Providers
	if providers[0].AESGCM == nil {
		return nil, validationErrorf("provider annotation %q declares staticKey but first provider is not aesgcm", ProviderAnnotation)
	}
	keys, err := parseStaticKeyEntries(providers[0].AESGCM.Keys)
	if err != nil {
		return nil, err
	}
	phase := Phase(secret.Annotations[staticKeyPhaseAnnotation])
	if phase == "" {
		return nil, validationErrorf("staticKey config is missing phase annotation %q", staticKeyPhaseAnnotation)
	}
	activeRef, err := staticKeyActiveRef(secret)
	if err != nil {
		return nil, err
	}
	targetRef, err := staticKeyTargetRef(secret)
	if err != nil {
		return nil, err
	}
	if err := validateStaticKeyShape(phase, keys, activeRef, targetRef); err != nil {
		return nil, err
	}
	return &staticKeyConfig{
		baseConfig: baseConfig{secret: secret.DeepCopy()},
		phase:      phase,
		keys:       keys,
		activeRef:  activeRef,
		targetRef:  targetRef,
		hash:       configHash(raw),
	}, nil
}

// Phase returns the persisted staticKey phase.
func (c *staticKeyConfig) Phase() Phase {
	return c.phase
}

// ConfigHash returns the kube-apiserver reload hash for the rendered config.
func (c *staticKeyConfig) ConfigHash() string {
	return c.hash
}

// ActiveKeyRef returns the observed source Secret for the first/write key.
func (c *staticKeyConfig) ActiveKeyRef() provisioningv1.ObservedSecretKeyRef {
	return c.activeRef
}

// TransitionToPrepared adds the desired key after the active key or refreshes Idle metadata.
func (c *staticKeyConfig) TransitionToPrepared(source SourceKey) (StaticKey, error) {
	if c.phase != PhaseIdle {
		return nil, transitionErrorf(c.phase, PhasePrepared, "transition requires Idle phase")
	}
	if len(c.keys) != 1 {
		return nil, transitionErrorf(c.phase, PhasePrepared, "expected one active key, got %d", len(c.keys))
	}
	desiredKey, err := validateSourceKey(source)
	if err != nil {
		return nil, err
	}
	if c.keys[0].Secret == desiredKey {
		return c.withState(PhaseIdle, c.keys, nil, source.Ref)
	}
	newKeyName := generateStaticKeyName(time.Now())
	if newKeyName == c.keys[0].Name {
		return nil, transitionErrorf(c.phase, PhasePrepared, "generated static key name collides with active key name %q", newKeyName)
	}
	return c.withState(PhasePrepared, []keyEntry{
		c.keys[0],
		{Name: newKeyName, Secret: desiredKey},
	}, &source.Ref, c.activeRef)
}

// TransitionToPromoted moves the target key to the first/write position.
func (c *staticKeyConfig) TransitionToPromoted() (StaticKey, error) {
	if c.phase != PhasePrepared {
		return nil, transitionErrorf(c.phase, PhasePromoted, "transition requires Prepared phase")
	}
	if len(c.keys) != 2 {
		return nil, transitionErrorf(c.phase, PhasePromoted, "expected two keys, got %d", len(c.keys))
	}
	if c.targetRef == nil {
		return nil, transitionErrorf(c.phase, PhasePromoted, "target source Secret metadata is missing")
	}
	return c.withState(PhasePromoted, []keyEntry{c.keys[1], c.keys[0]}, c.targetRef, *c.targetRef)
}

// RotationID returns an opaque, deterministic ID for the promoted key transition.
func (c *staticKeyConfig) RotationID() (string, error) {
	if c.phase != PhasePromoted {
		return "", transitionErrorf(c.phase, PhasePromoted, "rotation ID is only available in Promoted phase")
	}
	if len(c.keys) != 2 {
		return "", validationErrorf("expected two promoted staticKey entries, got %d", len(c.keys))
	}
	return digest.Short(digest.FromObjects(c.keys[1].Name, c.keys[0].Name), rotationIDLength), nil
}

// TransitionToFinalized drops the old key after migration completes.
func (c *staticKeyConfig) TransitionToFinalized() (StaticKey, error) {
	if c.phase != PhasePromoted {
		return nil, transitionErrorf(c.phase, PhaseFinalized, "transition requires Promoted phase")
	}
	if len(c.keys) != 2 {
		return nil, transitionErrorf(c.phase, PhaseFinalized, "expected two keys, got %d", len(c.keys))
	}
	if c.targetRef == nil {
		return nil, transitionErrorf(c.phase, PhaseFinalized, "target source Secret metadata is missing")
	}
	return c.withState(PhaseFinalized, []keyEntry{c.keys[0]}, c.targetRef, *c.targetRef)
}

// TransitionToIdle clears transient target metadata after final reload.
func (c *staticKeyConfig) TransitionToIdle() (StaticKey, error) {
	if c.phase != PhaseFinalized {
		return nil, transitionErrorf(c.phase, PhaseIdle, "transition requires Finalized phase")
	}
	if len(c.keys) != 1 {
		return nil, transitionErrorf(c.phase, PhaseIdle, "expected one key, got %d", len(c.keys))
	}
	return c.withState(PhaseIdle, c.keys, nil, c.activeRef)
}

// withState renders a new Secret shape and reparses it to enforce invariants.
func (c *staticKeyConfig) withState(phase Phase, keys []keyEntry, targetRef *provisioningv1.ObservedSecretKeyRef, activeRef provisioningv1.ObservedSecretKeyRef) (*staticKeyConfig, error) {
	rendered, err := renderStaticKeyEncryptionConfig(keys)
	if err != nil {
		return nil, err
	}
	secret := c.secret.DeepCopy()
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[ConfigFileName] = rendered
	secret.Annotations[ProviderAnnotation] = string(operatorv1.EtcdEncryptionProviderStaticKey)
	secret.Annotations[staticKeyPhaseAnnotation] = string(phase)
	setStaticKeyActiveRef(secret, activeRef)
	if targetRef == nil {
		clearStaticKeyTargetRef(secret)
	} else {
		setStaticKeyTargetRef(secret, *targetRef)
	}
	return parseStaticKey(secret)
}

// renderStaticKeyEncryptionConfig renders the AES-GCM provider with ordered keys.
func renderStaticKeyEncryptionConfig(keys []keyEntry) ([]byte, error) {
	apiserverKeys := make([]apiserverv1.Key, 0, len(keys))
	for _, key := range keys {
		apiserverKeys = append(apiserverKeys, apiserverv1.Key{Name: key.Name, Secret: key.Secret})
	}
	return renderEncryptionConfiguration(apiserverv1.ProviderConfiguration{
		AESGCM: &apiserverv1.AESConfiguration{
			Keys: apiserverKeys,
		},
	})
}

// parseStaticKeyEntries validates and normalizes AES-GCM keys.
func parseStaticKeyEntries(keys []apiserverv1.Key) ([]keyEntry, error) {
	if len(keys) == 0 {
		return nil, validationErrorf("expected at least one aesgcm key")
	}
	seen := map[string]struct{}{}
	entries := make([]keyEntry, 0, len(keys))
	for _, key := range keys {
		if key.Name == "" {
			return nil, validationErrorf("aesgcm key name is empty")
		}
		if _, ok := seen[key.Name]; ok {
			return nil, validationErrorf("duplicate aesgcm key name %q", key.Name)
		}
		seen[key.Name] = struct{}{}
		base64Key, err := ValidateBase64StaticKeyText([]byte(key.Secret))
		if err != nil {
			return nil, validationErrorf("invalid aesgcm key %q: %v", key.Name, err)
		}
		entries = append(entries, keyEntry{Name: key.Name, Secret: base64Key})
	}
	return entries, nil
}

// validateStaticKeyShape enforces phase-specific key and metadata invariants.
func validateStaticKeyShape(phase Phase, keys []keyEntry, activeRef provisioningv1.ObservedSecretKeyRef, targetRef *provisioningv1.ObservedSecretKeyRef) error {
	switch phase {
	case PhaseIdle:
		if len(keys) != 1 {
			return validationErrorf("expected one idle staticKey entry, got %d", len(keys))
		}
		if targetRef != nil {
			return validationErrorf("Idle staticKey config must not contain target source Secret metadata")
		}
	case PhasePrepared:
		if len(keys) != 2 {
			return validationErrorf("expected two Prepared staticKey entries, got %d", len(keys))
		}
		if targetRef == nil {
			return validationErrorf("Prepared staticKey config is missing target source Secret metadata")
		}
	case PhasePromoted:
		if len(keys) != 2 {
			return validationErrorf("expected two Promoted staticKey entries, got %d", len(keys))
		}
		if targetRef == nil {
			return validationErrorf("Promoted staticKey config is missing target source Secret metadata")
		}
		if activeRef != *targetRef {
			return validationErrorf("Promoted staticKey active source Secret metadata must match target metadata")
		}
	case PhaseFinalized:
		if len(keys) != 1 {
			return validationErrorf("expected one Finalized staticKey entry, got %d", len(keys))
		}
		if targetRef == nil {
			return validationErrorf("Finalized staticKey config is missing target source Secret metadata")
		}
		if activeRef != *targetRef {
			return validationErrorf("Finalized staticKey active source Secret metadata must match target metadata")
		}
	default:
		return validationErrorf("unsupported staticKey phase annotation %q", phase)
	}
	return nil
}

// validateSourceKey validates source metadata and normalizes key material.
func validateSourceKey(source SourceKey) (string, error) {
	base64Key, err := ValidateBase64StaticKeyText(source.Key)
	if err != nil {
		return "", err
	}
	if err := validateObservedRef(source.Ref); err != nil {
		return "", fmt.Errorf("source Secret metadata is incomplete: %w", err)
	}
	return base64Key, nil
}

// setStaticKeyActiveRef stores active source metadata on the Secret.
func setStaticKeyActiveRef(secret *corev1.Secret, ref provisioningv1.ObservedSecretKeyRef) {
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations[staticKeyActiveSecretNameAnnotation] = ref.Name
	secret.Annotations[staticKeyActiveSecretKeyAnnotation] = ref.Key
	secret.Annotations[staticKeyActiveSecretNamespaceAnnotation] = ref.Namespace
	secret.Annotations[staticKeyActiveSecretUIDAnnotation] = ref.UID
	secret.Annotations[staticKeyActiveSecretRVAnnotation] = ref.ResourceVersion
}

// staticKeyActiveRef reads complete active source metadata from the Secret.
func staticKeyActiveRef(secret *corev1.Secret) (provisioningv1.ObservedSecretKeyRef, error) {
	ref := provisioningv1.ObservedSecretKeyRef{
		Name:            secret.Annotations[staticKeyActiveSecretNameAnnotation],
		Key:             secret.Annotations[staticKeyActiveSecretKeyAnnotation],
		Namespace:       secret.Annotations[staticKeyActiveSecretNamespaceAnnotation],
		UID:             secret.Annotations[staticKeyActiveSecretUIDAnnotation],
		ResourceVersion: secret.Annotations[staticKeyActiveSecretRVAnnotation],
	}
	if err := validateObservedRef(ref); err != nil {
		return provisioningv1.ObservedSecretKeyRef{}, fmt.Errorf("staticKey config is missing active source Secret metadata: %w", err)
	}
	return ref, nil
}

// setStaticKeyTargetRef stores in-flight target source metadata on the Secret.
func setStaticKeyTargetRef(secret *corev1.Secret, ref provisioningv1.ObservedSecretKeyRef) {
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations[staticKeyRotationTargetSecretNameAnnotation] = ref.Name
	secret.Annotations[staticKeyRotationTargetSecretKeyAnnotation] = ref.Key
	secret.Annotations[staticKeyRotationTargetSecretNamespaceAnnotation] = ref.Namespace
	secret.Annotations[staticKeyRotationTargetSecretUIDAnnotation] = ref.UID
	secret.Annotations[staticKeyRotationTargetSecretRVAnnotation] = ref.ResourceVersion
}

// staticKeyTargetRef reads optional in-flight target metadata from the Secret.
func staticKeyTargetRef(secret *corev1.Secret) (*provisioningv1.ObservedSecretKeyRef, error) {
	ref := provisioningv1.ObservedSecretKeyRef{
		Name:            secret.Annotations[staticKeyRotationTargetSecretNameAnnotation],
		Key:             secret.Annotations[staticKeyRotationTargetSecretKeyAnnotation],
		Namespace:       secret.Annotations[staticKeyRotationTargetSecretNamespaceAnnotation],
		UID:             secret.Annotations[staticKeyRotationTargetSecretUIDAnnotation],
		ResourceVersion: secret.Annotations[staticKeyRotationTargetSecretRVAnnotation],
	}
	if ref.Name == "" && ref.Key == "" && ref.Namespace == "" && ref.UID == "" && ref.ResourceVersion == "" {
		return nil, nil
	}
	if err := validateObservedRef(ref); err != nil {
		return nil, fmt.Errorf("staticKey config has incomplete target source Secret metadata: %w", err)
	}
	return &ref, nil
}

// clearStaticKeyTargetRef removes in-flight target metadata from the Secret.
func clearStaticKeyTargetRef(secret *corev1.Secret) {
	delete(secret.Annotations, staticKeyRotationTargetSecretNameAnnotation)
	delete(secret.Annotations, staticKeyRotationTargetSecretKeyAnnotation)
	delete(secret.Annotations, staticKeyRotationTargetSecretNamespaceAnnotation)
	delete(secret.Annotations, staticKeyRotationTargetSecretUIDAnnotation)
	delete(secret.Annotations, staticKeyRotationTargetSecretRVAnnotation)
}

// validateObservedRef requires every observed Secret identity field.
func validateObservedRef(ref provisioningv1.ObservedSecretKeyRef) error {
	switch {
	case ref.Name == "":
		return fmt.Errorf("name is empty")
	case ref.Key == "":
		return fmt.Errorf("key is empty")
	case ref.Namespace == "":
		return fmt.Errorf("namespace is empty")
	case ref.UID == "":
		return fmt.Errorf("uid is empty")
	case ref.ResourceVersion == "":
		return fmt.Errorf("resourceVersion is empty")
	default:
		return nil
	}
}

// ValidateBase64StaticKeyText treats the Secret value as base64 key text, decodes it only to
// validate that it is a well-formed AES key whose length is 16, 24, or 32 bytes, and returns the
// normalized base64 string to render into the EncryptionConfiguration.
func ValidateBase64StaticKeyText(value []byte) (string, error) {
	base64Key := strings.TrimSpace(string(value))
	if base64Key == "" {
		return "", fmt.Errorf("key is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return "", fmt.Errorf("key must be base64-encoded AES key text")
	}
	switch len(decoded) {
	case 16, 24, 32:
	default:
		return "", fmt.Errorf("decoded key length must be 16, 24, or 32 bytes, got %d", len(decoded))
	}
	return base64Key, nil
}

// generateStaticKeyName returns an opaque timestamped key name.
func generateStaticKeyName(now time.Time) string {
	return fmt.Sprintf("%s-%s-%d", staticKeyNamePrefix, utilrand.String(8), now.UTC().UnixMilli())
}
