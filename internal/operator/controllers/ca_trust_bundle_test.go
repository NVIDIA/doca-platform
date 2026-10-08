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
	"crypto/sha256"
	"encoding/pem"
	"errors"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/certmanager"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// testCertPEM returns a PEM-encoded CERTIFICATE block. The bytes do not need to be a real DER
// certificate: mergeCABundle only parses PEM blocks and de-duplicates by their content.
func testCertPEM(seed string) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte(seed)})
}

func countCerts(b []byte) int {
	n := 0
	rest := b
	for {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		if blk.Type == "CERTIFICATE" {
			n++
		}
	}
	return n
}

func TestMergeCABundle(t *testing.T) {
	certA := testCertPEM("certificate-a")
	certB := testCertPEM("certificate-b")

	t.Run("empty existing returns the CA cert", func(t *testing.T) {
		g := NewWithT(t)
		out, err := mergeCABundle(nil, certA)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(out)).To(Equal(string(certA)))
	})

	t.Run("CA already present is not duplicated", func(t *testing.T) {
		g := NewWithT(t)
		out, err := mergeCABundle(certA, certA)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(countCerts(out)).To(Equal(1))
		g.Expect(string(out)).To(Equal(string(certA)))
	})

	t.Run("existing cert is preserved and CA appended (non-pruning)", func(t *testing.T) {
		g := NewWithT(t)
		out, err := mergeCABundle(certB, certA)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(countCerts(out)).To(Equal(2))
		// Existing first, then the CA.
		g.Expect(string(out)).To(Equal(string(certB) + string(certA)))
	})

	t.Run("existing certs kept when CA already among them", func(t *testing.T) {
		g := NewWithT(t)
		existing := append(append([]byte{}, certA...), certB...)
		out, err := mergeCABundle(existing, certA)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(countCerts(out)).To(Equal(2))
		g.Expect(string(out)).To(Equal(string(certA) + string(certB)))
	})

	t.Run("duplicate blocks in existing are de-duplicated", func(t *testing.T) {
		g := NewWithT(t)
		existing := append(append([]byte{}, certA...), certA...)
		out, err := mergeCABundle(existing, certA)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(countCerts(out)).To(Equal(1))
	})

	t.Run("non-certificate PEM blocks are ignored", func(t *testing.T) {
		g := NewWithT(t)
		key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not-a-cert")})
		out, err := mergeCABundle(key, certA)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(countCerts(out)).To(Equal(1))
		g.Expect(string(out)).To(Equal(string(certA)))
	})
}

func TestAppendCertBlocks(t *testing.T) {
	certA := testCertPEM("append-a")
	certB := testCertPEM("append-b")

	t.Run("writes only CERTIFICATE blocks and skips others", func(t *testing.T) {
		g := NewWithT(t)
		key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not-a-cert")})
		input := append(append([]byte{}, key...), certA...)

		var out bytes.Buffer
		seen := map[[sha256.Size]byte]bool{}
		g.Expect(appendCertBlocks(&out, seen, input)).To(Succeed())
		g.Expect(out.String()).To(Equal(string(certA)))
		g.Expect(countCerts(out.Bytes())).To(Equal(1))
	})

	t.Run("de-duplicates across calls using the shared seen map", func(t *testing.T) {
		g := NewWithT(t)
		var out bytes.Buffer
		seen := map[[sha256.Size]byte]bool{}
		// certA is written by the first call; the second call must not write it again but must append certB.
		g.Expect(appendCertBlocks(&out, seen, certA)).To(Succeed())
		g.Expect(appendCertBlocks(&out, seen, append(append([]byte{}, certA...), certB...))).To(Succeed())
		g.Expect(countCerts(out.Bytes())).To(Equal(2))
		g.Expect(out.String()).To(Equal(string(certA) + string(certB)))
	})

	t.Run("input without PEM certificate blocks produces no output", func(t *testing.T) {
		g := NewWithT(t)
		var out bytes.Buffer
		seen := map[[sha256.Size]byte]bool{}
		g.Expect(appendCertBlocks(&out, seen, []byte("not pem at all"))).To(Succeed())
		g.Expect(out.Len()).To(BeZero())
	})
}

func TestComputeBundleHash(t *testing.T) {
	certA := testCertPEM("generation-a")
	certB := testCertPEM("generation-b")

	t.Run("same effective set yields same generation regardless of order", func(t *testing.T) {
		g := NewWithT(t)
		bundleAB := append(append([]byte{}, certA...), certB...)
		bundleBA := append(append([]byte{}, certB...), certA...)

		genAB, err := computeBundleHash(bundleAB)
		g.Expect(err).NotTo(HaveOccurred())
		genBA, err := computeBundleHash(bundleBA)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(genAB).To(Equal(genBA))
	})

	t.Run("different effective set yields different generation", func(t *testing.T) {
		g := NewWithT(t)
		genA, err := computeBundleHash(certA)
		g.Expect(err).NotTo(HaveOccurred())
		genB, err := computeBundleHash(certB)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(genA).NotTo(Equal(genB))
	})

	t.Run("returns error when bundle has no certificate blocks", func(t *testing.T) {
		g := NewWithT(t)
		_, err := computeBundleHash([]byte("not a cert"))
		g.Expect(err).To(HaveOccurred())
	})
}

func TestGetCATrustBundleConfigMapName(t *testing.T) {
	g := NewWithT(t)
	config := &operatorv1.DPFOperatorConfig{}
	g.Expect(config.GetCATrustBundleConfigMapName()).To(Equal(operatorv1.DefaultCATrustBundleConfigMapName))
}

func TestReconcileCATrustBundle(t *testing.T) {
	newReconciler := func() *DPFOperatorConfigReconciler {
		return &DPFOperatorConfigReconciler{
			Client:   testClient,
			Scheme:   scheme.Scheme,
			Settings: &DPFOperatorConfigReconcilerSettings{},
		}
	}

	createNamespace := func(g *WithT, name string) {
		g.Expect(testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})).To(Succeed())
	}

	newConfig := func(ns string) *operatorv1.DPFOperatorConfig {
		return &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpfoperatorconfig",
				Namespace: ns,
			},
		}
	}

	// The two anchors that decide who owns the bundle. The reconcile loop reads the anchor from the
	// webhook intermediate CA and passes it down, so the tests hand it over the same way.
	selfSignedAnchor := certmanager.IssuerReference{
		Name:  operatorv1.GlobalRootIssuerName,
		Kind:  operatorv1.CertManagerIssuerKind,
		Group: operatorv1.CertManagerGroup,
	}
	externalAnchor := certmanager.IssuerReference{
		Name:  "openbao-issuer",
		Kind:  operatorv1.CertManagerClusterIssuerKind,
		Group: operatorv1.CertManagerGroup,
	}

	t.Run("requeues when the CA secret is missing", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-no-secret"
		createNamespace(g, ns)
		r := newReconciler()

		err := r.reconcileCATrustBundle(ctx, newConfig(ns), selfSignedAnchor)
		g.Expect(err).To(HaveOccurred())
		pendingErr := &caTrustBundlePendingError{}
		g.Expect(errors.As(err, &pendingErr)).To(BeTrue())

		cm := &corev1.ConfigMap{}
		err = testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	t.Run("requeues when the CA secret has no certificate", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-empty-secret"
		createNamespace(g, ns)
		g.Expect(testClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: ProvisioningCASecretName},
			Data:       map[string][]byte{corev1.TLSCertKey: {}},
		})).To(Succeed())
		r := newReconciler()

		err := r.reconcileCATrustBundle(ctx, newConfig(ns), selfSignedAnchor)
		g.Expect(err).To(HaveOccurred())
		pendingErr := &caTrustBundlePendingError{}
		g.Expect(errors.As(err, &pendingErr)).To(BeTrue())

		cm := &corev1.ConfigMap{}
		err = testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	t.Run("creates the bundle from the CA secret", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-create"
		createNamespace(g, ns)
		caCert := testCertPEM("ca-create")
		g.Expect(testClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: ProvisioningCASecretName},
			Data:       map[string][]byte{corev1.TLSCertKey: caCert},
		})).To(Succeed())
		config := newConfig(ns)
		r := newReconciler()

		err := r.reconcileCATrustBundle(ctx, config, selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())

		cm := &corev1.ConfigMap{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
		g.Expect(cm.Data[operatorv1.CATrustBundleKey]).To(Equal(string(caCert)))
		g.Expect(cm.Data[operatorv1.CATrustBundleHashKey]).NotTo(BeEmpty())
		g.Expect(cm.Labels).To(HaveKeyWithValue(operatorv1.DPFComponentLabelKey, "dpf-operator"))
		// The bundle is intentionally not owned by the DPFOperatorConfig; it is deleted explicitly.
		g.Expect(cm.OwnerReferences).To(BeEmpty())
	})

	t.Run("merges the CA into an existing bundle without pruning other entries", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-merge"
		createNamespace(g, ns)
		caCert := testCertPEM("ca-merge")
		otherCert := testCertPEM("other-ca")
		g.Expect(testClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: ProvisioningCASecretName},
			Data:       map[string][]byte{corev1.TLSCertKey: caCert},
		})).To(Succeed())
		// Pre-existing bundle with another CA and an unrelated key that must be preserved.
		g.Expect(testClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName},
			Data: map[string]string{
				operatorv1.CATrustBundleKey: string(otherCert),
				"user-key":                  "keep-me",
			},
		})).To(Succeed())
		r := newReconciler()

		err := r.reconcileCATrustBundle(ctx, newConfig(ns), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())

		cm := &corev1.ConfigMap{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
		bundle := []byte(cm.Data[operatorv1.CATrustBundleKey])
		g.Expect(countCerts(bundle)).To(Equal(2))
		g.Expect(string(bundle)).To(ContainSubstring(string(otherCert)))
		g.Expect(string(bundle)).To(ContainSubstring(string(caCert)))
		g.Expect(cm.Data[operatorv1.CATrustBundleHashKey]).NotTo(BeEmpty())
		// The unrelated key set by another field manager must not be pruned by the Operator's apply.
		g.Expect(cm.Data).To(HaveKeyWithValue("user-key", "keep-me"))
	})

	t.Run("is idempotent across repeated reconciles", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-idempotent"
		createNamespace(g, ns)
		caCert := testCertPEM("ca-idempotent")
		g.Expect(testClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: ProvisioningCASecretName},
			Data:       map[string][]byte{corev1.TLSCertKey: caCert},
		})).To(Succeed())
		r := newReconciler()

		for i := 0; i < 3; i++ {
			err := r.reconcileCATrustBundle(ctx, newConfig(ns), selfSignedAnchor)
			g.Expect(err).NotTo(HaveOccurred())
		}

		cm := &corev1.ConfigMap{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
		g.Expect(countCerts([]byte(cm.Data[operatorv1.CATrustBundleKey]))).To(Equal(1))
	})

	t.Run("backfills bundle-hash on pre-existing ConfigMap with unchanged bundle", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-backfill-hash"
		createNamespace(g, ns)
		caCert := testCertPEM("ca-backfill")
		g.Expect(testClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: ProvisioningCASecretName},
			Data:       map[string][]byte{corev1.TLSCertKey: caCert},
		})).To(Succeed())

		// Simulate an older-operator ConfigMap: identical bundle content but missing bundle-hash key.
		g.Expect(testClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName},
			Data: map[string]string{
				operatorv1.CATrustBundleKey: string(caCert),
			},
		})).To(Succeed())

		r := newReconciler()
		g.Expect(r.reconcileCATrustBundle(ctx, newConfig(ns), selfSignedAnchor)).To(Succeed())

		cm := &corev1.ConfigMap{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
		g.Expect(cm.Data[operatorv1.CATrustBundleKey]).To(Equal(string(caCert)))
		g.Expect(cm.Data[operatorv1.CATrustBundleHashKey]).NotTo(BeEmpty())
	})

	t.Run("recomputes a stale non-empty bundle-hash after pruning", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-recompute-stale-hash"
		createNamespace(g, ns)
		oldCert := testCertPEM("ca-old")
		newCert := testCertPEM("ca-new")
		g.Expect(testClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: ProvisioningCASecretName},
			Data:       map[string][]byte{corev1.TLSCertKey: newCert},
		})).To(Succeed())

		staleHash, err := computeBundleHash(append(append([]byte{}, oldCert...), newCert...))
		g.Expect(err).NotTo(HaveOccurred())
		expectedHash, err := computeBundleHash(newCert)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(staleHash).NotTo(Equal(expectedHash))

		g.Expect(testClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName},
			Data: map[string]string{
				operatorv1.CATrustBundleKey:     string(newCert),
				operatorv1.CATrustBundleHashKey: staleHash,
			},
		})).To(Succeed())

		r := newReconciler()
		g.Expect(r.reconcileCATrustBundle(ctx, newConfig(ns), selfSignedAnchor)).To(Succeed())

		cm := &corev1.ConfigMap{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
		g.Expect(cm.Data[operatorv1.CATrustBundleKey]).To(Equal(string(newCert)))
		g.Expect(cm.Data[operatorv1.CATrustBundleHashKey]).To(Equal(expectedHash))
	})

	t.Run("deleteCATrustBundle deletes the bundle ConfigMap", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-delete"
		createNamespace(g, ns)
		caCert := testCertPEM("ca-delete")
		g.Expect(testClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: ProvisioningCASecretName},
			Data:       map[string][]byte{corev1.TLSCertKey: caCert},
		})).To(Succeed())
		// deleteCATrustBundle reads the anchor from the cluster rather than taking it as an argument,
		// so the webhook intermediate CA the chart owns has to stand in for it here.
		createWebhookIntermediateCA(g, ns)
		r := newReconciler()
		config := newConfig(ns)
		g.Expect(r.reconcileCATrustBundle(ctx, config, selfSignedAnchor)).To(Succeed())

		// Sanity check: the bundle exists before deletion.
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, &corev1.ConfigMap{})).To(Succeed())

		g.Expect(r.deleteCATrustBundle(ctx, config)).To(Succeed())

		err := testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, &corev1.ConfigMap{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())

		// Deleting again is a no-op.
		g.Expect(r.deleteCATrustBundle(ctx, config)).To(Succeed())
	})

	// newExternalConfig names the bundle the operator provides. What hands its content over is the
	// anchor rather than anything on the config, so these are paired with externalAnchor at the call
	// sites. An empty bundleName leaves the name at the default.
	newExternalConfig := func(ns, bundleName string) *operatorv1.DPFOperatorConfig {
		config := newConfig(ns)
		config.Spec.Security = &operatorv1.SecurityConfiguration{
			CertManagement: &operatorv1.CertManagementConfiguration{
				TrustBundleConfigMapName: bundleName,
			},
		}
		return config
	}

	t.Run("reports a missing operator-provided bundle rather than creating one", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-external-missing"
		createNamespace(g, ns)
		r := newReconciler()

		err := r.reconcileCATrustBundle(ctx, newExternalConfig(ns, ""), externalAnchor)
		pendingErr := &caTrustBundlePendingError{}
		g.Expect(errors.As(err, &pendingErr)).To(BeTrue())
		g.Expect(pendingErr.reason).To(Equal(operatorv1.CATrustBundleReasonNotProvided))

		// Creating an empty bundle would be worse than reporting: peers would trust nothing while the
		// ConfigMap they read looks present.
		err = testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, &corev1.ConfigMap{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	t.Run("reports an operator-provided bundle that carries no certificate", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-external-empty"
		createNamespace(g, ns)
		g.Expect(testClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName},
			Data:       map[string]string{"unrelated": "value"},
		})).To(Succeed())
		r := newReconciler()

		err := r.reconcileCATrustBundle(ctx, newExternalConfig(ns, ""), externalAnchor)
		pendingErr := &caTrustBundlePendingError{}
		g.Expect(errors.As(err, &pendingErr)).To(BeTrue())
		g.Expect(pendingErr.reason).To(Equal(operatorv1.CATrustBundleReasonNotProvided))

		cm := &corev1.ConfigMap{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
		g.Expect(cm.Data).NotTo(HaveKey(operatorv1.CATrustBundleHashKey))
	})

	t.Run("hashes the operator-provided bundle without merging the provisioning CA into it", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-external-hash-only"
		createNamespace(g, ns)
		operatorCert := testCertPEM("enterprise-root")
		// Present, and deliberately not what the bundle holds: an external anchor makes this CA
		// irrelevant to what peers trust, so merging it in would add a certificate the operator
		// never chose to trust.
		g.Expect(testClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: ProvisioningCASecretName},
			Data:       map[string][]byte{corev1.TLSCertKey: testCertPEM("provisioning-ca")},
		})).To(Succeed())
		g.Expect(testClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName},
			Data:       map[string]string{operatorv1.CATrustBundleKey: string(operatorCert)},
		})).To(Succeed())
		expectedHash, err := computeBundleHash(operatorCert)
		g.Expect(err).NotTo(HaveOccurred())

		r := newReconciler()
		config := newExternalConfig(ns, "")
		g.Expect(r.reconcileCATrustBundle(ctx, config, externalAnchor)).To(Succeed())

		cm := &corev1.ConfigMap{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
		g.Expect(cm.Data[operatorv1.CATrustBundleKey]).To(Equal(string(operatorCert)))
		g.Expect(cm.Data[operatorv1.CATrustBundleHashKey]).To(Equal(expectedHash))
		// DPF did not create this ConfigMap, so it does not claim it as one of its own components.
		g.Expect(cm.Labels).NotTo(HaveKey(operatorv1.DPFComponentLabelKey))

		// Repeating the reconcile must not start writing content either.
		g.Expect(r.reconcileCATrustBundle(ctx, config, externalAnchor)).To(Succeed())
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
		g.Expect(countCerts([]byte(cm.Data[operatorv1.CATrustBundleKey]))).To(Equal(1))
	})

	t.Run("hashes the bundle named by trustBundleConfigMapName", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-external-named"
		createNamespace(g, ns)
		operatorCert := testCertPEM("named-enterprise-root")
		g.Expect(testClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "enterprise-trust"},
			Data:       map[string]string{operatorv1.CATrustBundleKey: string(operatorCert)},
		})).To(Succeed())
		expectedHash, err := computeBundleHash(operatorCert)
		g.Expect(err).NotTo(HaveOccurred())

		r := newReconciler()
		g.Expect(r.reconcileCATrustBundle(ctx, newExternalConfig(ns, "enterprise-trust"), externalAnchor)).To(Succeed())

		cm := &corev1.ConfigMap{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "enterprise-trust"}, cm)).To(Succeed())
		g.Expect(cm.Data[operatorv1.CATrustBundleHashKey]).To(Equal(expectedHash))

		// The default name is not a fallback the operator has to clean up after.
		err = testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, &corev1.ConfigMap{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	t.Run("deleteCATrustBundle keeps an operator-provided bundle", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-external-delete"
		createNamespace(g, ns)
		g.Expect(testClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "enterprise-trust"},
			Data:       map[string]string{operatorv1.CATrustBundleKey: string(testCertPEM("keep-me-root"))},
		})).To(Succeed())
		// The anchor is stood up as the cluster would have it, though what keeps this bundle is the
		// label DPF never wrote onto it.
		webhookCA := newCACertificate(operatorv1.WebhookIntermediateCAName, externalAnchor, ns)
		g.Expect(testClient.Create(ctx, webhookCA)).To(Succeed())
		r := newReconciler()

		g.Expect(r.deleteCATrustBundle(ctx, newExternalConfig(ns, "enterprise-trust"))).To(Succeed())

		// The certificates in it are trusted by peers DPF does not manage and it could not put them
		// back, so uninstalling DPF must not take them with it.
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "enterprise-trust"}, &corev1.ConfigMap{})).To(Succeed())
	})

	// Teardown has no order to rely on: the chart that owns the Certificate the anchor is read from
	// can go first, and cert-manager with it. What DPF wrote is still DPF's to remove then, and what
	// it did not write is still not.
	t.Run("deleteCATrustBundle deletes the bundle DPF wrote once the anchor is gone", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-delete-no-anchor"
		createNamespace(g, ns)
		g.Expect(testClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: ProvisioningCASecretName},
			Data:       map[string][]byte{corev1.TLSCertKey: testCertPEM("ca-delete-no-anchor")},
		})).To(Succeed())
		createWebhookIntermediateCA(g, ns)
		r := newReconciler()
		config := newConfig(ns)
		g.Expect(r.reconcileCATrustBundle(ctx, config, selfSignedAnchor)).To(Succeed())

		webhookCA := newCACertificate(operatorv1.WebhookIntermediateCAName, selfSignedCAIssuerRef(), ns)
		g.Expect(testClient.Delete(ctx, webhookCA)).To(Succeed())

		g.Expect(r.deleteCATrustBundle(ctx, config)).To(Succeed())

		err := testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, &corev1.ConfigMap{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	// Re-anchoring a cluster to an authority of the operator's hands them a bundle DPF filled, and
	// from that point on DPF only stamps the hash on it. The label has to come off with the
	// ownership, or teardown would read it as DPF's and take the operator's certificates with it.
	t.Run("the operator taking over a bundle DPF wrote releases it", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-handover"
		createNamespace(g, ns)
		g.Expect(testClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: ProvisioningCASecretName},
			Data:       map[string][]byte{corev1.TLSCertKey: testCertPEM("ca-handover")},
		})).To(Succeed())
		r := newReconciler()
		config := newConfig(ns)
		g.Expect(r.reconcileCATrustBundle(ctx, config, selfSignedAnchor)).To(Succeed())

		cm := &corev1.ConfigMap{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
		g.Expect(cm.Labels).To(HaveKeyWithValue(operatorv1.DPFComponentLabelKey, "dpf-operator"))

		g.Expect(r.reconcileCATrustBundle(ctx, config, externalAnchor)).To(Succeed())

		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
		g.Expect(cm.Labels).NotTo(HaveKey(operatorv1.DPFComponentLabelKey))

		// Which is what keeps teardown off it, with or without an anchor left to read.
		g.Expect(r.deleteCATrustBundle(ctx, config)).To(Succeed())
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, cm)).To(Succeed())
	})

	t.Run("deleteCATrustBundle keeps an operator-provided bundle once the anchor is gone", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-external-delete-no-anchor"
		createNamespace(g, ns)
		g.Expect(testClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "enterprise-trust"},
			Data:       map[string]string{operatorv1.CATrustBundleKey: string(testCertPEM("keep-me-root-no-anchor"))},
		})).To(Succeed())
		r := newReconciler()

		g.Expect(r.deleteCATrustBundle(ctx, newExternalConfig(ns, "enterprise-trust"))).To(Succeed())

		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "enterprise-trust"}, &corev1.ConfigMap{})).To(Succeed())
	})
}

func TestCATrustBundleConfigMapToDPFOperatorConfig(t *testing.T) {
	ns := "ca-bundle-enqueue"
	configKey := types.NamespacedName{Namespace: ns, Name: "dpfoperatorconfig"}

	// A fake client rather than the suite's: the mapping only reads the configured bundle name, so
	// the config does not have to be a complete one the API server would accept.
	config := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: configKey.Name},
		Spec: operatorv1.DPFOperatorConfigSpec{
			Security: &operatorv1.SecurityConfiguration{
				CertManagement: &operatorv1.CertManagementConfiguration{
					TrustBundleConfigMapName: "enterprise-trust",
				},
			},
		},
	}

	newReconciler := func() *DPFOperatorConfigReconciler {
		return &DPFOperatorConfigReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(config).Build(),
			Scheme:   scheme.Scheme,
			Settings: &DPFOperatorConfigReconcilerSettings{ConfigSingletonNamespaceName: &configKey},
		}
	}

	configMap := func(name string) *corev1.ConfigMap {
		return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	}

	t.Run("enqueues for the default bundle name", func(t *testing.T) {
		g := NewWithT(t)
		requests := newReconciler().CATrustBundleConfigMapToDPFOperatorConfig(ctx, configMap(operatorv1.DefaultCATrustBundleConfigMapName))
		g.Expect(requests).To(ConsistOf(ctrl.Request{NamespacedName: configKey}))
	})

	// The bundle DPF does not write is the one whose edits have to reach it, and it is exactly the
	// one that can be named something else.
	t.Run("enqueues for the name configured on the config", func(t *testing.T) {
		g := NewWithT(t)
		requests := newReconciler().CATrustBundleConfigMapToDPFOperatorConfig(ctx, configMap("enterprise-trust"))
		g.Expect(requests).To(ConsistOf(ctrl.Request{NamespacedName: configKey}))
	})

	t.Run("ignores an unrelated ConfigMap in the same namespace", func(t *testing.T) {
		g := NewWithT(t)
		requests := newReconciler().CATrustBundleConfigMapToDPFOperatorConfig(ctx, configMap("kube-root-ca.crt"))
		g.Expect(requests).To(BeEmpty())
	})

	t.Run("ignores everything when no singleton config is configured", func(t *testing.T) {
		g := NewWithT(t)
		r := newReconciler()
		r.Settings = &DPFOperatorConfigReconcilerSettings{}
		requests := r.CATrustBundleConfigMapToDPFOperatorConfig(ctx, configMap(operatorv1.DefaultCATrustBundleConfigMapName))
		g.Expect(requests).To(BeEmpty())
	})
}
