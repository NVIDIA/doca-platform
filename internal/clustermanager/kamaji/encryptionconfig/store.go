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
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// readBackPollInterval controls how often Store checks for patched Secret cache updates.
	readBackPollInterval = 100 * time.Millisecond
	// readBackPollTimeout bounds Store cache read-back waits.
	readBackPollTimeout = 10 * time.Second
)

// Store owns API reads and writes for per-cluster encryption config Secrets.
type Store struct {
	client client.Client
	scheme *runtime.Scheme
}

// NewStore creates a Store backed by the given Kubernetes client and scheme.
func NewStore(client client.Client, scheme *runtime.Scheme) *Store {
	return &Store{client: client, scheme: scheme}
}

// Load reads, ownership-checks, and validates an existing encryption config Secret.
func (s *Store) Load(ctx context.Context, key types.NamespacedName, owner *provisioningv1.DPUCluster) (Config, error) {
	secret := &corev1.Secret{}
	if err := s.client.Get(ctx, key, secret); err != nil {
		return nil, err
	}
	if !hasOwnerReference(secret, owner) {
		return nil, &OwnershipError{
			Secret: client.ObjectKeyFromObject(secret),
			Owner:  client.ObjectKeyFromObject(owner),
		}
	}
	return parse(secret)
}

// InitializeStaticKey creates the initial staticKey config Secret.
func (s *Store) InitializeStaticKey(ctx context.Context, key types.NamespacedName, owner *provisioningv1.DPUCluster, labels map[string]string, source SourceKey) (StaticKey, error) {
	cfg, err := newStaticKeyConfig(key, labels, source)
	if err != nil {
		return nil, err
	}
	if err := s.create(ctx, cfg.secret, owner); err != nil {
		return nil, err
	}
	return cfg, nil
}

// InitializeVaultKMS creates the initial VaultKMS config Secret.
func (s *Store) InitializeVaultKMS(ctx context.Context, key types.NamespacedName, owner *provisioningv1.DPUCluster, labels map[string]string) (VaultKMS, error) {
	cfg, err := newVaultKMSConfig(key, labels)
	if err != nil {
		return nil, err
	}
	if err := s.create(ctx, cfg.secret, owner); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save persists a validated wrapper transition and reloads it from the API cache.
func (s *Store) Save(ctx context.Context, cfg Config) (Config, error) {
	desired, err := secretForConfig(cfg)
	if err != nil {
		return nil, err
	}
	current := &corev1.Secret{}
	if err := s.client.Get(ctx, client.ObjectKeyFromObject(desired), current); err != nil {
		return nil, fmt.Errorf("get encryption config secret before update: %w", err)
	}
	if current.UID == desired.UID &&
		current.ResourceVersion == desired.ResourceVersion &&
		secretMatches(current, desired) {
		return parse(current)
	}
	if err := s.client.Update(ctx, desired); err != nil {
		return nil, fmt.Errorf("update encryption config secret: %w", err)
	}
	readBack, err := s.waitForReadBack(ctx, client.ObjectKeyFromObject(desired), desired)
	if err != nil {
		return nil, err
	}
	return parse(readBack)
}

// create writes a new config Secret with the DPUCluster owner reference.
func (s *Store) create(ctx context.Context, secret *corev1.Secret, owner *provisioningv1.DPUCluster) error {
	if err := controllerutil.SetOwnerReference(owner, secret, s.scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on encryption config secret: %w", err)
	}
	if err := s.client.Create(ctx, secret); err != nil {
		return fmt.Errorf("failed to create encryption config secret: %w", err)
	}
	return nil
}

// waitForReadBack waits until the client cache observes the expected Secret data and annotations.
func (s *Store) waitForReadBack(ctx context.Context, key client.ObjectKey, expected *corev1.Secret) (*corev1.Secret, error) {
	var readBack *corev1.Secret
	if err := wait.PollUntilContextTimeout(ctx, readBackPollInterval, readBackPollTimeout, true, func(ctx context.Context) (bool, error) {
		current := &corev1.Secret{}
		if err := s.client.Get(ctx, key, current); err != nil {
			return false, err
		}
		if !secretMatches(current, expected) {
			return false, nil
		}
		readBack = current
		return true, nil
	}); err != nil {
		return nil, fmt.Errorf("wait for encryption config secret cache to observe patch: %w", err)
	}
	return readBack, nil
}

// secretForConfig returns a copy of the Secret owned by a concrete wrapper.
func secretForConfig(cfg Config) (*corev1.Secret, error) {
	switch typed := cfg.(type) {
	case *staticKeyConfig:
		return typed.secret.DeepCopy(), nil
	case *vaultKMSConfig:
		return typed.secret.DeepCopy(), nil
	default:
		return nil, validationErrorf("unsupported encryption config implementation %T", cfg)
	}
}

// secretMatches compares only the persisted fields managed by the wrapper.
func secretMatches(secret, expected *corev1.Secret) bool {
	return apiequality.Semantic.DeepEqual(secret.Data, expected.Data) &&
		apiequality.Semantic.DeepEqual(secret.Annotations, expected.Annotations)
}

// hasOwnerReference reports whether obj is owned by the expected DPUCluster.
func hasOwnerReference(obj metav1.Object, owner *provisioningv1.DPUCluster) bool {
	if obj == nil || owner == nil {
		return false
	}
	return slices.ContainsFunc(obj.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
		return ref.UID == owner.GetUID()
	})
}

// baseSecret creates the common Secret skeleton for provider initialization.
func baseSecret(key types.NamespacedName, labels map[string]string, provider operatorv1.EtcdEncryptionAtRestProvider) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        key.Name,
			Namespace:   key.Namespace,
			Labels:      maps.Clone(labels),
			Annotations: map[string]string{ProviderAnnotation: string(provider)},
		},
		Data: map[string][]byte{},
	}
}
