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

package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/pem"
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// ProvisioningCASecretName is the cert-manager managed Secret holding the DPF provisioning CA
	// (certificate and private key). It is defined locally to avoid importing internal provisioning packages.
	// It is exported so the manager's cache can be scoped to only this Secret (see cmd/operator/main.go).
	ProvisioningCASecretName = "dpf-provisioning-ca-secret"
)

// caTrustBundlePendingError indicates the CA trust bundle cannot be reconciled yet because the
// provisioning CA Secret is still being issued asynchronously by cert-manager. It is not a fatal
// error: the caller should surface it on the relevant condition and requeue rather than failing the
// reconcile.
type caTrustBundlePendingError struct {
	reason string
}

func (e *caTrustBundlePendingError) Error() string {
	return e.reason
}

// reconcileCATrustBundle ensures a ConfigMap exists with the public CA certificate(s) used for DPU
// provisioning. It copies the tls.crt of the provisioning CA Secret into the bundle using an
// ensure-present, non-pruning merge so that additional certificates (e.g. during a dual-CA rotation)
// are preserved.
//
// It returns a *caTrustBundlePendingError when the CA Secret is not yet available so the caller can
// requeue instead of treating it as a fatal error.
func (r *DPFOperatorConfigReconciler) reconcileCATrustBundle(ctx context.Context, config *operatorv1.DPFOperatorConfig) error {
	log := ctrllog.FromContext(ctx)
	bundleName := config.GetCATrustBundleConfigMapName()

	// Read the provisioning CA Secret. cert-manager issues it asynchronously, so a missing Secret (or one
	// without tls.crt yet) is not a fatal error: signal the caller to requeue.
	caSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: ProvisioningCASecretName}, caSecret); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Provisioning CA secret not found yet, requeuing", "secret", ProvisioningCASecretName)
			return &caTrustBundlePendingError{reason: "Waiting for the provisioning CA secret to be issued by cert-manager"}
		}
		return fmt.Errorf("failed to get provisioning CA secret %s/%s: %w", config.Namespace, ProvisioningCASecretName, err)
	}
	caCert := caSecret.Data[corev1.TLSCertKey]
	if len(caCert) == 0 {
		log.Info("Provisioning CA secret has no certificate yet, requeuing", "secret", ProvisioningCASecretName)
		return &caTrustBundlePendingError{reason: "Waiting for the provisioning CA secret to contain a certificate"}
	}

	// Read the existing bundle so the merge below preserves any certificates a user added. The read-modify-
	// write window between this Get and the write is closed by the optimistic lock on the patch, not by the
	// read itself.
	existing := &corev1.ConfigMap{}
	found := true
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: bundleName}, existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get CA trust bundle ConfigMap %s/%s: %w", config.Namespace, bundleName, err)
		}
		found = false
	}

	var existingBundle []byte
	if found {
		existingBundle = []byte(existing.Data[operatorv1.CATrustBundleKey])
	}

	merged, err := mergeCABundle(existingBundle, caCert)
	if err != nil {
		return fmt.Errorf("failed to merge CA trust bundle: %w", err)
	}

	// Create the ConfigMap when it does not exist yet.
	if !found {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bundleName,
				Namespace: config.Namespace,
			},
		}
		setCATrustBundleFields(cm, merged)
		if err := r.Client.Create(ctx, cm); err != nil {
			return fmt.Errorf("failed to create CA trust bundle ConfigMap %s/%s: %w", config.Namespace, bundleName, err)
		}
		return nil
	}

	// Update the existing ConfigMap with a merge patch guarded by an optimistic lock. The lock rejects the
	// write with a conflict if the ConfigMap changed since the Get above, so a concurrent edit is retried
	// instead of overwritten. The merge patch only touches the keys we set and leaves other data untouched.
	patch := client.MergeFromWithOptions(existing.DeepCopy(), client.MergeFromWithOptimisticLock{})
	setCATrustBundleFields(existing, merged)
	if err := r.Client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("failed to patch CA trust bundle ConfigMap %s/%s: %w", config.Namespace, bundleName, err)
	}

	return nil
}

// setCATrustBundleFields sets the component label and the merged CA bundle on cm, without disturbing any
// other labels or data keys. It is shared by the create and patch paths so both write identical fields.
func setCATrustBundleFields(cm *corev1.ConfigMap, merged []byte) {
	if cm.Labels == nil {
		cm.Labels = map[string]string{}
	}
	cm.Labels[operatorv1.DPFComponentLabelKey] = "dpf-operator"
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[operatorv1.CATrustBundleKey] = string(merged)
}

// deleteCATrustBundle deletes the CA trust bundle ConfigMap. The bundle is intentionally not owned by the
// DPFOperatorConfig, so it must be deleted explicitly rather than relying on Kubernetes owner-reference
// cleanup. It is a no-op when the ConfigMap does not exist (e.g. it was never created).
func (r *DPFOperatorConfigReconciler) deleteCATrustBundle(ctx context.Context, config *operatorv1.DPFOperatorConfig) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.GetCATrustBundleConfigMapName(),
			Namespace: config.Namespace,
		},
	}
	return client.IgnoreNotFound(r.Client.Delete(ctx, cm))
}

// mergeCABundle returns a PEM bundle that contains all certificates from existing plus any certificate
// from caCert that is not already present. It never removes certificates already in existing (non-pruning)
// and de-duplicates by certificate content.
func mergeCABundle(existing, caCert []byte) ([]byte, error) {
	var out bytes.Buffer
	seen := map[[sha256.Size]byte]bool{}

	// Existing certificates first to keep them (and their order) stable, then the current CA if missing.
	if err := appendCertBlocks(&out, seen, existing); err != nil {
		return nil, err
	}
	if err := appendCertBlocks(&out, seen, caCert); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// appendCertBlocks decodes PEM certificate blocks from data and writes any not already
// recorded in seen to out, de-duplicating by certificate content.
func appendCertBlocks(out *bytes.Buffer, seen map[[sha256.Size]byte]bool, data []byte) error {
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		fingerprint := sha256.Sum256(block.Bytes)
		if seen[fingerprint] {
			continue
		}
		seen[fingerprint] = true
		if err := pem.Encode(out, &pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes}); err != nil {
			return err
		}
	}
	return nil
}

// ProvisioningCASecretToDPFOperatorConfig enqueues a reconcile when the provisioning CA Secret changes
// so the CA trust bundle is kept in sync with the CA certificate (e.g. after a CA renewal/rotation).
func (r *DPFOperatorConfigReconciler) ProvisioningCASecretToDPFOperatorConfig(_ context.Context, o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	// Ignore this enqueue function if the singletonNamespaceName is not set. This is done to enable easier testing.
	if r.Settings.ConfigSingletonNamespaceName == nil {
		return result
	}
	if o.GetName() != ProvisioningCASecretName {
		return result
	}
	result = append(result, ctrl.Request{NamespacedName: *r.Settings.ConfigSingletonNamespaceName})
	return result
}
