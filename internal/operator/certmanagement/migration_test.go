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

package certmanagement

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/certmanager"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// The anchors the migration branches on. It runs under the self-signed root, where DPF owns the
// keypair it is carrying over, and not under an issuer that owns its own.
var (
	selfSignedAnchor = certmanager.IssuerReference{
		Name:  operatorv1.GlobalRootIssuerName,
		Kind:  operatorv1.CertManagerIssuerKind,
		Group: operatorv1.CertManagerGroup,
	}
	externalAnchor = certmanager.IssuerReference{
		Name:  "openbao-issuer",
		Kind:  operatorv1.CertManagerClusterIssuerKind,
		Group: operatorv1.CertManagerGroup,
	}
)

// caKeyPair is a certificate authority as a Secret carries it: the private key, and the certificate
// over it.
type caKeyPair struct {
	key     *ecdsa.PrivateKey
	keyPEM  []byte
	certPEM []byte
}

// newSelfSignedCA builds a CA that signed its own certificate, which is what the provisioning CA is
// and what the operator seeds from.
func newSelfSignedCA(t *testing.T, commonName string) caKeyPair {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return caKeyPair{key: key, keyPEM: encodeKey(t, key), certPEM: signCA(t, commonName, &key.PublicKey, key, commonName)}
}

// reissue returns the certificate cert-manager would put in the Secret once it has issued over an
// adopted key: the same subject and the same key, signed by the authority anchoring the PKI rather
// than by itself.
func reissue(t *testing.T, ca caKeyPair, commonName string) []byte {
	t.Helper()
	root, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return signCA(t, commonName, &ca.key.PublicKey, root, "dpf-global-root-ca")
}

func signCA(t *testing.T, commonName string, pub *ecdsa.PublicKey, signer *ecdsa.PrivateKey, issuerCommonName string) []byte {
	t.Helper()
	g := NewWithT(t)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	g.Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		Issuer:                pkix.Name{CommonName: issuerCommonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	parent := template
	if issuerCommonName != commonName {
		parent = &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: issuerCommonName}}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, pub, signer)
	g.Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: certificatePEMBlockType, Bytes: der})
}

func encodeKey(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

// encodeKeyPKCS8 is the same key in the other encoding cert-manager writes, which is what a Secret
// holds after it has issued over a key seeded in the SEC1 one above.
func encodeKeyPKCS8(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func caSecret(name string, keyPEM, certPEM []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: name},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey: keyPEM,
			corev1.TLSCertKey:       certPEM,
		},
	}
}

// certManagerObject returns an empty object of the given cert-manager kind in the namespace of the
// config under test, addressed by name.
func certManagerObject(gvk schema.GroupVersionKind, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetNamespace(testNamespace)
	obj.SetName(name)
	return obj
}

// legacyChain returns the objects the provisioning component stops emitting once the migration has
// deleted the CA Secret, so a test can assert the migration leaves them to the ApplySet prune.
func legacyChain() []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		certManagerObject(CertificateGVK, operatorv1.LegacyProvisioningCACertificateName),
		certManagerObject(IssuerGVK, operatorv1.LegacyProvisioningIssuerName),
		certManagerObject(IssuerGVK, operatorv1.LegacyProvisioningSelfSignedIssuerName),
	}
}

func newMigrationConfig() *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "dpfoperatorconfig"},
	}
}

func TestMigrateProvisioningCA(t *testing.T) {
	ctx := context.Background()
	legacyCN := operatorv1.LegacyProvisioningCACertificateName

	t.Run("carries the keypair of the provisioning CA into the platform CA", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM)).
			Build()

		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())

		// The subject follows the key rather than leading it: until cert-manager has issued over
		// what was just seeded, the platform CA stays under the subject of the manifest, so a
		// certificate issued over a key of cert-manager's own cannot land under a carried subject.
		g.Expect(state.CACommonName).To(BeEmpty())
		g.Expect(state.LegacyAuthority).To(Equal(legacy.certPEM))
		g.Expect(state.LegacyAuthorityRetired).To(BeFalse())
		// Nothing on the cluster wakes the reconcile when cert-manager issues over what was just
		// seeded, so the caller has to come back on its own or the next step waits out the resync.
		g.Expect(state.Pending).To(BeTrue())
		// cert-manager is about to issue the platform CA under the subject of the manifest, which no
		// leaf may be signed under.
		g.Expect(state.LeavesHeldOnLegacyIssuer).To(BeTrue())

		seeded := &corev1.Secret{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.PlatformIntermediateCAName}, seeded)).To(Succeed())
		g.Expect(seeded.Data[corev1.TLSPrivateKeyKey]).To(Equal(legacy.keyPEM))
		// Only the key. The certificate is left for cert-manager to issue under the anchor, rather
		// than publishing the self-signed one as the platform chain in the meantime.
		g.Expect(seeded.Data[corev1.TLSCertKey]).To(BeEmpty())
	})

	t.Run("replaces a platform CA that carries an identity of its own", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		// A cluster anchored by a release that had the platform CA but not this migration: its
		// intermediate holds a keypair no peer was ever provisioned with.
		ownIdentity := newSelfSignedCA(t, operatorv1.PlatformIntermediateCAName)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(
				caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM),
				caSecret(operatorv1.PlatformIntermediateCAName, ownIdentity.keyPEM, ownIdentity.certPEM),
			).Build()

		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.CACommonName).To(BeEmpty())
		g.Expect(state.LegacyAuthorityRetired).To(BeFalse())

		reseeded := &corev1.Secret{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.PlatformIntermediateCAName}, reseeded)).To(Succeed())
		g.Expect(reseeded.Data[corev1.TLSPrivateKeyKey]).To(Equal(legacy.keyPEM))
		// The certificate that no longer matches the key is left where it is. It is that mismatch
		// cert-manager answers by reissuing over the key it finds; asked with nothing to keep a key
		// for, it stages one of its own and the identity being carried over is lost.
		g.Expect(reseeded.Data[corev1.TLSCertKey]).To(Equal(ownIdentity.certPEM))
	})

	t.Run("reports the authority retired once it has been reissued under the anchor", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		// The key adopted, the subject preserved, and a certificate signed by the anchor over both:
		// the platform CA now validates everything the legacy certificate did.
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(
				caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM),
				caSecret(operatorv1.PlatformIntermediateCAName, legacy.keyPEM, reissue(t, legacy, legacyCN)),
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: operatorv1.DefaultCATrustBundleConfigMapName},
					Data:       map[string]string{operatorv1.CATrustBundleKey: string(legacy.certPEM)},
				},
			).WithObjects(toObjects(legacyChain())...).Build()

		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.LegacyAuthorityRetired).To(BeTrue())
		g.Expect(state.CACommonName).To(Equal(legacyCN))
		g.Expect(state.LeavesHeldOnLegacyIssuer).To(BeFalse())

		// The bundle still carries the legacy certificate, so the chain it came from stays until the
		// prune has reached the peers.
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.LegacyProvisioningCASecretName},
			&corev1.Secret{})).To(Succeed())
	})

	t.Run("removes the legacy keypair and stops retaining the chain once the bundle no longer carries it", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		platformCert := reissue(t, legacy, legacyCN)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(
				caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM),
				caSecret(operatorv1.PlatformIntermediateCAName, legacy.keyPEM, platformCert),
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: operatorv1.DefaultCATrustBundleConfigMapName},
					Data:       map[string]string{operatorv1.CATrustBundleKey: string(platformCert)},
				},
			).WithObjects(toObjects(legacyChain())...).Build()

		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())

		err = c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.LegacyProvisioningCASecretName}, &corev1.Secret{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())

		// The Certificate goes with the Secret. Left behind, cert-manager would reissue a fresh
		// keypair into the Secret just deleted before the prune reached it, and the next reconcile
		// would re-seed the platform CA with an authority that signed nothing.
		certificate := certManagerObject(CertificateGVK, operatorv1.LegacyProvisioningCACertificateName)
		err = c.Get(ctx, client.ObjectKeyFromObject(certificate), certificate)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(),
			"the Certificate has to go with the Secret, or cert-manager puts the Secret back")

		// The issuers are left to the ApplySet prune, which happens by the component no longer
		// emitting them in the apply this same reconcile goes on to do. Neither can bring the
		// keypair back, so neither has to be raced for.
		g.Expect(state.LegacyChainRetained).To(BeFalse())
		for _, obj := range []*unstructured.Unstructured{
			certManagerObject(IssuerGVK, operatorv1.LegacyProvisioningIssuerName),
			certManagerObject(IssuerGVK, operatorv1.LegacyProvisioningSelfSignedIssuerName),
		} {
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed(),
				"%s %s is the prune's to remove, not the migration's", obj.GetKind(), obj.GetName())
		}
	})

	t.Run("refuses to re-seed a platform CA that already carries a migrated identity", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		// The platform CA already stands in for the legacy one, and a Secret of that chain has
		// turned up holding some other keypair. That is the state cert-manager leaves behind when
		// it reissues into a Secret whose Certificate outlived it. Adopting the stray would retire
		// the certificate proving the identity every peer was provisioned with, in favor of one
		// that signed nothing.
		stray := newSelfSignedCA(t, legacyCN)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(
				caSecret(operatorv1.LegacyProvisioningCASecretName, stray.keyPEM, stray.certPEM),
				caSecret(operatorv1.PlatformIntermediateCAName, legacy.keyPEM, reissue(t, legacy, legacyCN)),
			).WithObjects(toObjects(legacyChain())...).Build()

		config := newMigrationConfig()

		// Reported rather than returned. Only an administrator can resolve this, and a reconcile
		// that stops here never reaches the apply, so a PKI nobody can fix from the cluster would
		// hold back every other component of the release, including the upgrade carrying the fix.
		state, err := MigrateProvisioningCA(ctx, c, config, selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.Blocked).To(BeTrue())
		g.Expect(state.LegacyChainRetained).To(BeTrue())
		// The platform CA already carries the identity of the legacy one, so the leaves have nothing
		// to be held from.
		g.Expect(state.LeavesHeldOnLegacyIssuer).To(BeFalse())
		// The apply goes on to request this subject, which is the identity the peers hold. Left
		// empty it would ask for the one in the manifest and discard what is being protected here.
		g.Expect(state.CACommonName).To(Equal(legacyCN))

		condition := conditions.Get(config, operatorv1.CertManagementReadyCondition)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Reason).To(Equal(string(operatorv1.CertManagementReasonCAMigrationRefused)))

		// The identity the peers hold is still the one in the platform CA.
		platform := &corev1.Secret{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.PlatformIntermediateCAName},
			platform)).To(Succeed())
		g.Expect(platform.Data[corev1.TLSPrivateKeyKey]).To(Equal(legacy.keyPEM))
	})

	t.Run("requests the carried subject only once the key behind it is adopted", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		own := newSelfSignedCA(t, operatorv1.PlatformIntermediateCAName)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(
				caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM),
				caSecret(operatorv1.PlatformIntermediateCAName, own.keyPEM, own.certPEM),
			).WithObjects(toObjects(legacyChain())...).Build()

		// The reconcile that seeds asks for nothing new, so the certificate cert-manager issues
		// next lands under the subject of the manifest whichever key it used.
		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.CACommonName).To(BeEmpty())
		g.Expect(state.Pending).To(BeTrue())
		g.Expect(state.LeavesHeldOnLegacyIssuer).To(BeTrue())

		// Having kept the seeded key, cert-manager reissues under the manifest subject. Only now is
		// the carried one requested, and the issuance it provokes is over a key already adopted.
		platform := &corev1.Secret{}
		key := client.ObjectKey{Namespace: testNamespace, Name: operatorv1.PlatformIntermediateCAName}
		g.Expect(c.Get(ctx, key, platform)).To(Succeed())
		platform.Data[corev1.TLSCertKey] = reissue(t, legacy, operatorv1.PlatformIntermediateCAName)
		g.Expect(c.Update(ctx, platform)).To(Succeed())

		state, err = MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.CACommonName).To(Equal(legacyCN))
		g.Expect(state.LegacyAuthorityRetired).To(BeFalse())
		// This is the certificate a leaf moved onto the platform issuer now would name as its issuer,
		// and it lasts only until cert-manager issues under the subject just requested.
		g.Expect(state.LeavesHeldOnLegacyIssuer).To(BeTrue())

		carried := client.MergeFrom(platform.DeepCopy())
		platform.Data[corev1.TLSCertKey] = reissue(t, legacy, legacyCN)
		g.Expect(c.Patch(ctx, platform, carried)).To(Succeed())

		state, err = MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.LegacyAuthorityRetired).To(BeTrue())
		g.Expect(state.LeavesHeldOnLegacyIssuer).To(BeFalse())
	})

	t.Run("re-seeds a platform CA whose seeded key cert-manager replaced", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		// cert-manager issued over a key of its own instead of the one seeded. Because the carried
		// subject is requested only after a key is adopted, that certificate is under the subject
		// of the manifest, which says plainly that nothing has been carried over yet: the seeding
		// is repeated rather than refused, and the migration gets another go at it.
		clobbered := newSelfSignedCA(t, operatorv1.PlatformIntermediateCAName)
		config := newMigrationConfig()
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(
				caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM),
				caSecret(operatorv1.PlatformIntermediateCAName, clobbered.keyPEM,
					reissue(t, clobbered, operatorv1.PlatformIntermediateCAName)),
			).WithObjects(toObjects(legacyChain())...).Build()

		state, err := MigrateProvisioningCA(ctx, c, config, selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.Blocked).To(BeFalse())
		g.Expect(conditions.Get(config, operatorv1.CertManagementReadyCondition)).To(BeNil())

		reseeded := &corev1.Secret{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.PlatformIntermediateCAName},
			reseeded)).To(Succeed())
		g.Expect(reseeded.Data[corev1.TLSPrivateKeyKey]).To(Equal(legacy.keyPEM))
	})

	t.Run("recognizes an adopted key cert-manager wrote back in another encoding", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		// cert-manager writes the key into the Secret every time it issues, in whichever encoding
		// the Certificate asks for, so the key seeded here comes back as a different sequence of
		// bytes for the same key. Read as a different key it is a migration that has already
		// happened being refused, and a platform CA re-seeded with what it already holds.
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(
				caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM),
				caSecret(operatorv1.PlatformIntermediateCAName, encodeKeyPKCS8(t, legacy.key),
					reissue(t, legacy, legacyCN)),
			).WithObjects(toObjects(legacyChain())...).Build()
		config := newMigrationConfig()

		state, err := MigrateProvisioningCA(ctx, c, config, selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.LegacyAuthorityRetired).To(BeTrue())
		g.Expect(state.Blocked).To(BeFalse())
		g.Expect(conditions.Get(config, operatorv1.CertManagementReadyCondition)).To(BeNil())
	})

	t.Run("does not retire an authority whose subject the platform CA has not adopted", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		// cert-manager issued over the seeded key before the apply that requests the subject landed,
		// so this certificate is signed by the anchor but names the subject of the manifest. It
		// validates nothing the legacy CA signed, and retiring on it would prune the only authority
		// that does, then delete the keypair it came from.
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(
				caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM),
				caSecret(operatorv1.PlatformIntermediateCAName, legacy.keyPEM,
					reissue(t, legacy, operatorv1.PlatformIntermediateCAName)),
			).WithObjects(toObjects(legacyChain())...).Build()

		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.LegacyAuthorityRetired).To(BeFalse())
		g.Expect(state.LegacyAuthority).To(Equal(legacy.certPEM))
		g.Expect(state.LegacyChainRetained).To(BeTrue())
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.LegacyProvisioningCASecretName},
			&corev1.Secret{})).To(Succeed())
	})

	t.Run("retains the legacy chain while the migration has not completed", func(t *testing.T) {
		for name, anchor := range map[string]certmanager.IssuerReference{
			// Under an external anchor no migration runs, and the leaves the legacy CA signed are
			// nobody's to reissue, so the chain has to stay.
			"external issuer": externalAnchor,
			"self-signed":     selfSignedAnchor,
		} {
			t.Run(name, func(t *testing.T) {
				g := NewWithT(t)
				legacy := newSelfSignedCA(t, legacyCN)
				c := fake.NewClientBuilder().WithScheme(testScheme(t)).
					WithObjects(caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM)).
					WithObjects(toObjects(legacyChain())...).Build()

				state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), anchor)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(state.LegacyChainRetained).To(BeTrue())
			})
		}
	})

	t.Run("does not retain a legacy chain on a cluster that never had one", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()

		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state).To(Equal(MigrationState{}))
	})

	t.Run("refuses to migrate while a CA rotation has the legacy issuer pinned", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		pinned := certManagerObject(IssuerGVK, operatorv1.LegacyProvisioningIssuerName)
		g.Expect(unstructured.SetNestedField(pinned.Object, "dpf-ca-secret-backup", "spec", "ca", "secretName")).To(Succeed())
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM), pinned).
			Build()
		config := newMigrationConfig()

		state, err := MigrateProvisioningCA(ctx, c, config, selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())

		// Nothing seeded: what is in the legacy Secret during a rotation is not necessarily what
		// signs the certificates the peers hold.
		g.Expect(state.CACommonName).To(BeEmpty())
		err = c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.PlatformIntermediateCAName}, &corev1.Secret{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		// The bundle keeps publishing both authorities in the meantime.
		g.Expect(state.LegacyAuthority).To(Equal(legacy.certPEM))
		// Carried to the chain check at the end of the reconcile, which would otherwise report the
		// chain a rotation leaves perfectly ready over the block reported here.
		g.Expect(state.Blocked).To(BeTrue())
		g.Expect(state.LeavesHeldOnLegacyIssuer).To(BeTrue())

		condition := conditions.Get(config, operatorv1.CertManagementReadyCondition)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Reason).To(Equal(string(operatorv1.CertManagementReasonCAMigrationBlocked)))
	})

	t.Run("refuses to migrate while a CA rotation has only configured the pin", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM)).
			Build()
		// A rotation that has just been started. The apply that puts the pin it asks for onto the
		// Issuer runs after this, so the Issuer still signs with the Secret of its own chain, and a
		// migration reading only the Issuer would carry the keypair over in that window.
		config := newMigrationConfig()
		config.Spec.Overrides = &operatorv1.Overrides{
			//nolint:staticcheck // The field the rotation is driven by.
			ProvisioningIssuerCASecretName: ptr.To("dpf-ca-secret-backup"),
		}

		state, err := MigrateProvisioningCA(ctx, c, config, selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(state.CACommonName).To(BeEmpty())
		g.Expect(state.Blocked).To(BeTrue())
		err = c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.PlatformIntermediateCAName}, &corev1.Secret{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())

		condition := conditions.Get(config, operatorv1.CertManagementReadyCondition)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Reason).To(Equal(string(operatorv1.CertManagementReasonCAMigrationBlocked)))
	})

	t.Run("migrates when the configured pin is the Secret of the chain itself", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM)).
			Build()
		// Naming the Secret the Issuer already signs with is not a rotation, so it is not something
		// to hold the migration back on.
		config := newMigrationConfig()
		config.Spec.Overrides = &operatorv1.Overrides{
			//nolint:staticcheck // The field the rotation is driven by.
			ProvisioningIssuerCASecretName: ptr.To(operatorv1.LegacyProvisioningCASecretName),
		}

		state, err := MigrateProvisioningCA(ctx, c, config, selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.Blocked).To(BeFalse())
		g.Expect(conditions.Get(config, operatorv1.CertManagementReadyCondition)).To(BeNil())
		g.Expect(state.LegacyAuthority).To(Equal(legacy.certPEM))

		seeded := &corev1.Secret{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.PlatformIntermediateCAName},
			seeded)).To(Succeed())
		g.Expect(seeded.Data[corev1.TLSPrivateKeyKey]).To(Equal(legacy.keyPEM))
	})

	t.Run("reports the carried over subject after the legacy Secret is gone", func(t *testing.T) {
		g := NewWithT(t)
		migrated := newSelfSignedCA(t, legacyCN)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(caSecret(operatorv1.PlatformIntermediateCAName, migrated.keyPEM, reissue(t, migrated, legacyCN))).
			Build()

		// The identity has to outlive the Secret it was carried from, or the next apply would request
		// the platform CA under the subject of the manifest and invalidate every certificate under it.
		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.CACommonName).To(Equal(legacyCN))
		g.Expect(state.LegacyAuthority).To(BeEmpty())
	})

	t.Run("does nothing on a cluster that never had a provisioning CA", func(t *testing.T) {
		g := NewWithT(t)
		own := newSelfSignedCA(t, operatorv1.PlatformIntermediateCAName)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(caSecret(operatorv1.PlatformIntermediateCAName, own.keyPEM, own.certPEM)).
			Build()

		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state).To(Equal(MigrationState{}))
	})

	t.Run("leaves the legacy chain alone when an external issuer anchors the PKI", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM)).
			WithObjects(toObjects(legacyChain())...).Build()

		// DPF owns no keypair under an external issuer, and the trust material below it is curated by
		// whoever owns that authority, so there is nothing to carry over and nothing to collapse.
		// Leaving it alone means retaining it: the chain is emitted by the provisioning component,
		// and anything that component stops emitting the ApplySet prunes.
		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), externalAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state).To(Equal(MigrationState{LegacyChainRetained: true}))
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.LegacyProvisioningCASecretName},
			&corev1.Secret{})).To(Succeed())
	})

	t.Run("keeps the carried over subject when an external issuer anchors the PKI", func(t *testing.T) {
		g := NewWithT(t)
		migrated := newSelfSignedCA(t, legacyCN)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(caSecret(operatorv1.PlatformIntermediateCAName, migrated.keyPEM, reissue(t, migrated, legacyCN))).
			Build()

		// The external issuer re-signs the platform CA over the same key. Under the subject of the
		// manifest the leaves it signed before the switch would name an issuer that no longer chains
		// to the new anchor, and stop validating as soon as the previous root leaves the bundle.
		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), externalAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state).To(Equal(MigrationState{CACommonName: legacyCN}))
	})

	t.Run("keeps the carried over subject when an external issuer anchors the PKI mid-migration", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(
				caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM),
				caSecret(operatorv1.PlatformIntermediateCAName, legacy.keyPEM, reissue(t, legacy, legacyCN)),
			).WithObjects(toObjects(legacyChain())...).Build()

		state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), externalAnchor)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state).To(Equal(MigrationState{
			CACommonName:        legacyCN,
			LegacyChainRetained: true,
		}))
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: testNamespace,
			Name:      operatorv1.LegacyProvisioningCASecretName,
		}, &corev1.Secret{})).To(Succeed())
	})

	t.Run("is idempotent while cert-manager has not issued over the adopted key", func(t *testing.T) {
		g := NewWithT(t)
		legacy := newSelfSignedCA(t, legacyCN)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).
			WithObjects(caSecret(operatorv1.LegacyProvisioningCASecretName, legacy.keyPEM, legacy.certPEM)).
			Build()

		for i := 0; i < 3; i++ {
			state, err := MigrateProvisioningCA(ctx, c, newMigrationConfig(), selfSignedAnchor)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Pending).To(BeTrue())
			g.Expect(state.LegacyAuthorityRetired).To(BeFalse())
			if i == 0 {
				// The reconcile that seeds asks for nothing new, so a certificate cert-manager
				// issues over a key other than the seeded one lands under the manifest subject.
				g.Expect(state.CACommonName).To(BeEmpty())
				continue
			}
			// The key in the Secret is the legacy one from here on, so whatever cert-manager issues
			// is over it and the subject that goes with it can be requested.
			g.Expect(state.CACommonName).To(Equal(legacyCN))
		}

		seeded := &corev1.Secret{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: operatorv1.PlatformIntermediateCAName}, seeded)).To(Succeed())
		g.Expect(seeded.Data[corev1.TLSPrivateKeyKey]).To(Equal(legacy.keyPEM))
	})
}

// toObjects widens the legacy chain for the fake client builder.
func toObjects(objs []*unstructured.Unstructured) []client.Object {
	out := make([]client.Object, 0, len(objs))
	for _, obj := range objs {
		out = append(out, obj)
	}
	return out
}
