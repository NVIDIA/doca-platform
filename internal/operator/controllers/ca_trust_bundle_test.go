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

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	t.Run("requeues when the CA secret is missing", func(t *testing.T) {
		g := NewWithT(t)
		ns := "ca-bundle-no-secret"
		createNamespace(g, ns)
		r := newReconciler()

		err := r.reconcileCATrustBundle(ctx, newConfig(ns))
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

		err := r.reconcileCATrustBundle(ctx, newConfig(ns))
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

		err := r.reconcileCATrustBundle(ctx, config)
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

		err := r.reconcileCATrustBundle(ctx, newConfig(ns))
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
			err := r.reconcileCATrustBundle(ctx, newConfig(ns))
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
		g.Expect(r.reconcileCATrustBundle(ctx, newConfig(ns))).To(Succeed())

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
		g.Expect(r.reconcileCATrustBundle(ctx, newConfig(ns))).To(Succeed())

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
		r := newReconciler()
		config := newConfig(ns)
		g.Expect(r.reconcileCATrustBundle(ctx, config)).To(Succeed())

		// Sanity check: the bundle exists before deletion.
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, &corev1.ConfigMap{})).To(Succeed())

		g.Expect(r.deleteCATrustBundle(ctx, config)).To(Succeed())

		err := testClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: operatorv1.DefaultCATrustBundleConfigMapName}, &corev1.ConfigMap{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())

		// Deleting again is a no-op.
		g.Expect(r.deleteCATrustBundle(ctx, config)).To(Succeed())
	})
}
