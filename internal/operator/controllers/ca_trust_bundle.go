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
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/certmanager"
	"github.com/nvidia/doca-platform/pkg/conditions"

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
	certificatePEMBlockType  = "CERTIFICATE"
	// dpfOperatorComponentLabelValue marks the trust bundle as one DPF filled in, which is what
	// tells a bundle it may delete on teardown from one the operator provided.
	dpfOperatorComponentLabelValue = "dpf-operator"
	// Field names of the structured log values below, named once so the same object is not filed
	// under a different key depending on which path logged it.
	logKeyConfigMap = "configMap"
	logKeySecret    = "secret"
)

// caTrustBundlePendingError indicates the CA trust bundle cannot be reconciled yet because
// something it is assembled from is still missing: the provisioning CA Secret cert-manager issues
// asynchronously, or the bundle the operator provides in external issuer mode. It is not a fatal
// error: the caller should surface it on the relevant condition and requeue rather than failing the
// reconcile. It carries the reason to report, because a CA that is still being issued and a bundle
// that was never supplied need different things from whoever reads the condition.
type caTrustBundlePendingError struct {
	reason  conditions.ConditionReason
	message string
}

func (e *caTrustBundlePendingError) Error() string {
	return e.message
}

// reconcileCATrustBundle keeps the ConfigMap holding the CA certificate(s) that DPF components
// validate their peers against in sync with the certificate authority in effect.
//
// Who owns the content depends on what anchors the PKI. With the self-signed root DPF both fills
// and hashes the bundle; with an external issuer the trust material belongs to the operator, whose
// own anchor DPF cannot see, so DPF only hashes what it finds. Both modes keep bundle-hash written
// by DPF, since the convergence tracking on DPUDevice/DPU compares against it and a hash computed
// by hand would silently diverge from the one this package produces.
//
// It returns a *caTrustBundlePendingError when the bundle cannot be assembled yet, so the caller
// can report it and requeue instead of treating it as a fatal error.
func (r *DPFOperatorConfigReconciler) reconcileCATrustBundle(ctx context.Context, config *operatorv1.DPFOperatorConfig, anchor certmanager.IssuerReference) error {
	if anchor.IsExternalIssuer() {
		return r.reconcileOperatorOwnedCATrustBundle(ctx, config)
	}
	return r.reconcileSelfSignedCATrustBundle(ctx, config)
}

// reconcileOperatorOwnedCATrustBundle refreshes bundle-hash over a bundle DPF does not write.
//
// An external issuer anchors the PKI to an authority DPF has no access to, so it cannot derive the
// certificates peers have to trust, and writing ca.crt would mean clobbering material the operator
// curates, including the second anchor they add to keep peers validating across a rotation. The
// ConfigMap is therefore read-only to DPF apart from bundle-hash, and a missing one is reported
// rather than created: DPF has nothing to put in it.
func (r *DPFOperatorConfigReconciler) reconcileOperatorOwnedCATrustBundle(ctx context.Context, config *operatorv1.DPFOperatorConfig) error {
	log := ctrllog.FromContext(ctx)
	bundleName := config.GetCATrustBundleConfigMapName()

	existing := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: bundleName}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			return &caTrustBundlePendingError{
				reason: operatorv1.CATrustBundleReasonNotProvided,
				message: fmt.Sprintf("ConfigMap %q does not exist. An external issuer anchors the PKI, so the "+
					"certificates peers are validated against are provided by whoever owns that issuer rather "+
					"than by DPF.", bundleName),
			}
		}
		return fmt.Errorf("failed to get CA trust bundle ConfigMap %s/%s: %w", config.Namespace, bundleName, err)
	}

	// Hashing rejects a bundle with no certificate in it, which is the same missing prerequisite as an
	// absent ConfigMap as far as a peer trying to validate against it is concerned.
	bundleHash, err := computeBundleHash([]byte(existing.Data[operatorv1.CATrustBundleKey]))
	if err != nil {
		return &caTrustBundlePendingError{
			reason: operatorv1.CATrustBundleReasonNotProvided,
			// The reason why is carried through rather than summarized, since a key holding something
			// that is not a certificate and a key holding nothing are the same condition here but not
			// the same mistake to go and correct.
			message: fmt.Sprintf("ConfigMap %q holds no certificate DPF can read under %q: %v. An external "+
				"issuer anchors the PKI, so it has to carry the certificates peers are validated against "+
				"before DPF components can trust each other.", bundleName, operatorv1.CATrustBundleKey, err),
		}
	}

	// A bundle DPF filled under the self-signed root and the operator has taken over, the cluster
	// having been re-anchored to an authority of their own, still carries the component label from
	// when DPF wrote it. That label is what says a bundle is DPF's to delete on teardown, so it comes
	// off here, where DPF stops writing the content and starts only stamping the hash on it.
	owned := existing.Labels[operatorv1.DPFComponentLabelKey] == dpfOperatorComponentLabelValue
	if existing.Data[operatorv1.CATrustBundleHashKey] == bundleHash && !owned {
		return nil
	}

	// Optimistic lock as in the self-signed path, so an edit landing between the read above and this
	// write is retried rather than overwritten. The patch carries bundle-hash and the label it
	// releases, which is what keeps ca.crt and everything else in the ConfigMap the operator's.
	patch := client.MergeFromWithOptions(existing.DeepCopy(), client.MergeFromWithOptimisticLock{})
	if existing.Data == nil {
		existing.Data = map[string]string{}
	}
	existing.Data[operatorv1.CATrustBundleHashKey] = bundleHash
	delete(existing.Labels, operatorv1.DPFComponentLabelKey)
	if err := r.Client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("failed to patch CA trust bundle ConfigMap %s/%s: %w", config.Namespace, bundleName, err)
	}
	log.Info("Updated CA trust bundle hash", logKeyConfigMap, client.ObjectKey{Namespace: config.Namespace, Name: bundleName})

	return nil
}

// reconcileSelfSignedCATrustBundle ensures a ConfigMap exists with the public CA certificate(s) used
// for DPU provisioning. It copies the tls.crt of the provisioning CA Secret into the bundle using an
// ensure-present, non-pruning merge so that additional certificates (e.g. during a dual-CA rotation)
// are preserved.
func (r *DPFOperatorConfigReconciler) reconcileSelfSignedCATrustBundle(ctx context.Context, config *operatorv1.DPFOperatorConfig) error {
	bundleName := config.GetCATrustBundleConfigMapName()

	caCert, err := r.readProvisioningCACertificate(ctx, config.Namespace)
	if err != nil {
		return err
	}

	// Read the existing bundle so the merge below preserves any certificates a user added. The read-modify-
	// write window between this Get and the write is closed by the optimistic lock on the patch, not by the
	// read itself.
	existing, found, err := r.getCATrustBundle(ctx, config.Namespace, bundleName)
	if err != nil {
		return err
	}

	var existingBundle []byte
	if found {
		existingBundle = []byte(existing.Data[operatorv1.CATrustBundleKey])
	}

	merged, err := mergeCABundle(existingBundle, caCert)
	if err != nil {
		return fmt.Errorf("failed to merge CA trust bundle: %w", err)
	}

	if !found {
		return r.createCATrustBundle(ctx, config.Namespace, bundleName, merged)
	}
	return r.updateCATrustBundle(ctx, existing, merged)
}

// readProvisioningCACertificate returns the certificate held by the provisioning CA Secret.
//
// cert-manager issues that Secret asynchronously, so neither a missing Secret nor one without
// tls.crt yet is a fatal error: both return a *caTrustBundlePendingError for the caller to surface
// and requeue on.
func (r *DPFOperatorConfigReconciler) readProvisioningCACertificate(ctx context.Context, namespace string) ([]byte, error) {
	log := ctrllog.FromContext(ctx)

	caSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ProvisioningCASecretName}, caSecret); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Provisioning CA secret not found yet, requeuing", logKeySecret, ProvisioningCASecretName)
			return nil, &caTrustBundlePendingError{
				reason:  conditions.ReasonPending,
				message: "Waiting for the provisioning CA secret to be issued by cert-manager",
			}
		}
		return nil, fmt.Errorf("failed to get provisioning CA secret %s/%s: %w", namespace, ProvisioningCASecretName, err)
	}

	caCert := caSecret.Data[corev1.TLSCertKey]
	if len(caCert) == 0 {
		log.Info("Provisioning CA secret has no certificate yet, requeuing", logKeySecret, ProvisioningCASecretName)
		return nil, &caTrustBundlePendingError{
			reason:  conditions.ReasonPending,
			message: "Waiting for the provisioning CA secret to contain a certificate",
		}
	}

	return caCert, nil
}

// getCATrustBundle reads the CA trust bundle ConfigMap, reporting an absent one through found rather
// than an error, because the caller creates it in that case.
func (r *DPFOperatorConfigReconciler) getCATrustBundle(ctx context.Context, namespace, name string) (*corev1.ConfigMap, bool, error) {
	existing := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get CA trust bundle ConfigMap %s/%s: %w", namespace, name, err)
	}
	return existing, true, nil
}

// createCATrustBundle creates the bundle ConfigMap holding merged.
func (r *DPFOperatorConfigReconciler) createCATrustBundle(ctx context.Context, namespace, name string, merged []byte) error {
	log := ctrllog.FromContext(ctx)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	if err := setCATrustBundleFields(cm, merged); err != nil {
		return fmt.Errorf("failed to set CA trust bundle fields: %w", err)
	}
	if err := r.Client.Create(ctx, cm); err != nil {
		return fmt.Errorf("failed to create CA trust bundle ConfigMap %s/%s: %w", namespace, name, err)
	}
	log.Info("Created CA trust bundle ConfigMap", logKeyConfigMap, client.ObjectKey{Namespace: namespace, Name: name})

	return nil
}

// updateCATrustBundle writes merged to a bundle ConfigMap that already exists, returning early when
// it holds it already.
//
// The merge patch is guarded by an optimistic lock, which rejects the write with a conflict if the
// ConfigMap changed since it was read, so a concurrent edit is retried instead of overwritten. The
// patch only touches the keys set here and leaves other data untouched.
func (r *DPFOperatorConfigReconciler) updateCATrustBundle(ctx context.Context, existing *corev1.ConfigMap, merged []byte) error {
	log := ctrllog.FromContext(ctx)

	expectedBundleHash, err := computeBundleHash(merged)
	if err != nil {
		return fmt.Errorf("failed to compute CA trust bundle hash: %w", err)
	}
	if existing.Data[operatorv1.CATrustBundleKey] == string(merged) &&
		existing.Data[operatorv1.CATrustBundleHashKey] == expectedBundleHash {
		return nil
	}

	patch := client.MergeFromWithOptions(existing.DeepCopy(), client.MergeFromWithOptimisticLock{})
	if err := setCATrustBundleFields(existing, merged); err != nil {
		return fmt.Errorf("failed to set CA trust bundle fields: %w", err)
	}
	if err := r.Client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("failed to patch CA trust bundle ConfigMap %s/%s: %w", existing.Namespace, existing.Name, err)
	}
	log.Info("Updated CA trust bundle ConfigMap", logKeyConfigMap, client.ObjectKey{Namespace: existing.Namespace, Name: existing.Name})

	return nil
}

// setCATrustBundleFields sets the component label and merged CA bundle fields on cm, without disturbing
// other labels or data keys. It is shared by the create and patch paths so both write identical fields.
func setCATrustBundleFields(cm *corev1.ConfigMap, merged []byte) error {
	bundleHash, err := computeBundleHash(merged)
	if err != nil {
		return err
	}
	if cm.Labels == nil {
		cm.Labels = map[string]string{}
	}
	cm.Labels[operatorv1.DPFComponentLabelKey] = dpfOperatorComponentLabelValue
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[operatorv1.CATrustBundleKey] = string(merged)
	cm.Data[operatorv1.CATrustBundleHashKey] = bundleHash
	return nil
}

// computeBundleHash returns a deterministic hash of the effective certificate set in the bundle.
// It de-duplicates by certificate bytes and ignores ordering differences by sorting certificate fingerprints.
func computeBundleHash(bundle []byte) (string, error) {
	rest := bundle
	seen := map[[sha256.Size]byte]struct{}{}
	fingerprints := make([]string, 0)

	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != certificatePEMBlockType {
			continue
		}
		fingerprint := sha256.Sum256(block.Bytes)
		if _, ok := seen[fingerprint]; ok {
			continue
		}
		seen[fingerprint] = struct{}{}
		fingerprints = append(fingerprints, hex.EncodeToString(fingerprint[:]))
	}

	if len(fingerprints) == 0 {
		return "", fmt.Errorf("CA trust bundle contains no valid certificate PEM blocks")
	}

	sort.Strings(fingerprints)
	hashBytes := sha256.Sum256([]byte(strings.Join(fingerprints, "\n")))
	return hex.EncodeToString(hashBytes[:]), nil
}

// deleteCATrustBundle deletes the CA trust bundle ConfigMap. The bundle is intentionally not owned by the
// DPFOperatorConfig, so it must be deleted explicitly rather than relying on Kubernetes owner-reference
// cleanup. It is a no-op when the ConfigMap does not exist (e.g. it was never created).
//
// A bundle DPF never filled is left behind: with an external issuer the certificates in it come from
// the operator, are trusted by peers DPF does not manage, and DPF could not recreate them if the
// config came back. Which of the two a bundle is comes from the component label, which DPF writes
// onto the ones it fills and releases the moment it stops filling one, so the answer is on the
// object itself. Teardown has no order to rely on, and resolving the anchor instead would put the
// answer behind a Certificate the chart owns and may already have taken with it.
func (r *DPFOperatorConfigReconciler) deleteCATrustBundle(ctx context.Context, config *operatorv1.DPFOperatorConfig) error {
	bundle, found, err := r.getCATrustBundle(ctx, config.Namespace, config.GetCATrustBundleConfigMapName())
	if err != nil || !found {
		return err
	}
	if bundle.Labels[operatorv1.DPFComponentLabelKey] != dpfOperatorComponentLabelValue {
		return nil
	}

	ctrllog.FromContext(ctx).Info("Deleting the CA trust bundle ConfigMap", logKeyConfigMap, client.ObjectKeyFromObject(bundle))
	return client.IgnoreNotFound(r.Client.Delete(ctx, bundle))
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
		if block.Type != certificatePEMBlockType {
			continue
		}
		fingerprint := sha256.Sum256(block.Bytes)
		if seen[fingerprint] {
			continue
		}
		seen[fingerprint] = true
		if err := pem.Encode(out, &pem.Block{Type: certificatePEMBlockType, Bytes: block.Bytes}); err != nil {
			return err
		}
	}
	return nil
}

// ProvisioningCASecretToDPFOperatorConfig enqueues a reconcile when the provisioning CA Secret changes
// so the CA trust bundle is kept in sync with the CA certificate (e.g. after a CA renewal/rotation).
func (r *DPFOperatorConfigReconciler) ProvisioningCASecretToDPFOperatorConfig(_ context.Context, o client.Object) []ctrl.Request {
	result := make([]ctrl.Request, 0, 1)
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

// CATrustBundleConfigMapToDPFOperatorConfig enqueues a reconcile when the CA trust bundle ConfigMap changes
// so bundle-hash is recomputed by the operator after bundle edits (e.g. prune phase, or an operator
// adding an anchor to a bundle of their own).
//
// The bundle is only at the default name until an external issuer is configured, so recognizing it by
// that name alone would stop the recompute exactly where it matters most: a bundle DPF does not write
// changes underneath it. The configured name comes from the config itself, read here rather than
// enqueueing every ConfigMap in the namespace, which would reconcile the whole system on unrelated
// edits.
func (r *DPFOperatorConfigReconciler) CATrustBundleConfigMapToDPFOperatorConfig(ctx context.Context, o client.Object) []ctrl.Request {
	result := make([]ctrl.Request, 0, 1)
	// Ignore this enqueue function if the singletonNamespaceName is not set. This is done to enable easier testing.
	if r.Settings.ConfigSingletonNamespaceName == nil {
		return result
	}
	if o.GetName() != operatorv1.DefaultCATrustBundleConfigMapName {
		config := &operatorv1.DPFOperatorConfig{}
		if err := r.Client.Get(ctx, *r.Settings.ConfigSingletonNamespaceName, config); err != nil {
			return result
		}
		if o.GetName() != config.GetCATrustBundleConfigMapName() {
			return result
		}
	}
	result = append(result, ctrl.Request{NamespacedName: *r.Settings.ConfigSingletonNamespaceName})
	return result
}
