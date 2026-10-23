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
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/certmanager"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// certificatePEMBlockType is the PEM block the certificates below are read from.
	certificatePEMBlockType = "CERTIFICATE"
	// logKeySecret names the Secret the migration logs about, so the same object is not filed under
	// a different key depending on which step logged it.
	logKeySecret = "secret"
)

// MigrationState is what the one-time migration of the legacy provisioning CA into the platform
// intermediate CA needs from the rest of the reconcile.
type MigrationState struct {
	// CACommonName is the subject the platform intermediate CA is requested with. On a migrated
	// cluster it is the subject of the CA whose keypair was carried over, which is what lets the
	// intermediate stand in for it; everywhere else it is empty, leaving the manifest's own.
	CACommonName string

	// LegacyAuthority is the certificate of the legacy provisioning CA, for as long as that CA
	// exists. It is empty on a fresh install and on a cluster already cleaned up.
	LegacyAuthority []byte

	// LegacyAuthorityRetired reports that the platform intermediate has adopted the legacy keypair
	// and been reissued under the anchor, so it validates everything the legacy certificate did and
	// that certificate can leave the trust bundle.
	LegacyAuthorityRetired bool

	// Blocked reports that a CA rotation in flight held the migration back, which the caller has
	// already been told on CertManagementReadyCondition. It travels to the chain check at the end of
	// the reconcile so that a chain which is perfectly ready, as it is throughout a rotation, does
	// not report itself over the more actionable state and leave the block invisible.
	Blocked bool

	// Pending reports that the migration is waiting on cert-manager to issue over a keypair it has
	// seeded. Nothing wakes the reconcile when that issuance lands: the Secret informer is scoped to
	// the Secret of the legacy CA and the Certificate of the platform CA is not watched at all, so
	// without a requeue every step of the migration waits out the resync period and an upgrade
	// spends tens of minutes publishing two authorities instead of seconds.
	Pending bool

	// LegacyChainRetained keeps the legacy provisioning CA chain in the desired inventory of the
	// provisioning component. The ApplySet prunes whatever that component stops emitting, which
	// would otherwise take the issuer, the CA Certificate and the self-signed issuer over it away
	// from a migration that still needs all three. It is set for as long as the CA Secret of that
	// chain exists, which the migration deletes last.
	LegacyChainRetained bool

	// LeavesHeldOnLegacyIssuer keeps the provisioning leaf certificates, and the ones the
	// provisioning controller mints at runtime, signed by the legacy provisioning issuer until the
	// platform intermediate CA carries the identity of the legacy CA. Seeding the key and requesting
	// the subject happen on separate reconciles, and cert-manager issues the platform CA under the
	// subject of the manifest in between. A leaf signed in that window names an issuer that exists
	// only for those seconds, which a BMC that never received it rejects with HTTP 401.
	LeavesHeldOnLegacyIssuer bool
}

// MigrateProvisioningCA carries the keypair of the legacy provisioning CA into the platform
// intermediate CA, and removes the legacy chain once it has.
//
// The leaves moved onto the platform issuer without inheriting the identity of the CA that used to
// sign them, which is why the trust bundle publishes two authorities: the certificates the BMC
// truststores hold, and the ones signed since. Collapsing that back into one cannot be done by
// pointing the new Certificate at the old Secret, because cert-manager neither moves a Secret nor
// adopts one it did not create: it would generate a fresh keypair and change the identity of the CA
// that signs the leaves, which is a fleet wide CA rotation. So the operator seeds the private key
// itself, before cert-manager is asked for the certificate over it.
//
// What preserves the identity is the private key together with the subject: a certificate with both
// unchanged validates every leaf the legacy CA signed, which is what makes removing the legacy
// certificate from the trust bundle safe rather than something the DPUs have to converge on first.
// The subject is therefore requested through CACommonName, and the key is seeded here.
//
// Every step is derived from the cluster rather than recorded, so an interrupted migration is
// resumed by the next reconcile and a completed one is recognized by the absence of what it
// consumed.
//
// An external issuer anchors the PKI to an authority that owns its own keys, and the trust material
// under it is curated by whoever owns that authority rather than published by DPF, so there is
// nothing for DPF to collapse and the legacy chain is left where it is. A subject already carried
// over is still requested under it, so the identity the leaves name survives the change of anchor.
func MigrateProvisioningCA(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig, anchor certmanager.IssuerReference) (MigrationState, error) {
	state, err := migrateProvisioningCA(ctx, c, config, anchor)
	if err != nil {
		setError(config, err)
	}
	return state, err
}

// migrateProvisioningCA is the migration itself, split out so that every failure of it is reported
// on the condition by the one return above rather than by each path that produces one.
func migrateProvisioningCA(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig, anchor certmanager.IssuerReference) (MigrationState, error) {
	legacy, err := getSecret(ctx, c, config.Namespace, operatorv1.LegacyProvisioningCASecretName)
	if err != nil {
		return MigrationState{}, err
	}

	// The chain over that Secret is kept in the desired inventory for as long as the Secret itself
	// is there, and on every cluster, not only the ones being migrated: the ApplySet prunes whatever
	// the provisioning component stops emitting, so dropping it any earlier would take the issuer,
	// the CA Certificate and the self-signed issuer over it away from a migration still carrying
	// that keypair over, and out of a cluster anchored to an authority DPF does not own, where no
	// migration runs at all and the leaves the CA signed are nobody's to reissue.
	state := MigrationState{LegacyChainRetained: legacy != nil}

	// Nothing to migrate: a fresh install, one this already ran on, or one anchored to an external
	// issuer, which owns its own keys and leaves DPF nothing to collapse. The subject is read back
	// from the platform CA itself on every one of them, so a migrated identity outlives the Secret it
	// came from and the anchor it was carried over under: the external issuer re-signs the platform
	// CA over the same key, and requesting the subject of the manifest then would leave every
	// certificate it signed naming an issuer that no longer chains to the new anchor.
	if anchor.IsExternalIssuer() || legacy == nil {
		commonName, err := migratedCACommonName(ctx, c, config.Namespace)
		if err != nil {
			return state, err
		}
		state.CACommonName = commonName
		return state, nil
	}

	legacyPEM, legacyCert, err := caCertificate(legacy)
	if err != nil {
		return state, err
	}
	if legacyCert == nil {
		// A legacy Secret cert-manager has not filled in yet says nothing about what signs the
		// leaves, and seeding a key without the subject that goes with it would produce a CA that
		// cannot stand in for it.
		state.Pending = true
		state.LeavesHeldOnLegacyIssuer = true
		return state, nil
	}

	pinnedTo, err := provisioningCAPinnedTo(ctx, c, config)
	if err != nil {
		return state, err
	}
	if pinnedTo != "" {
		// A CA rotation has the legacy issuer pinned to a backup Secret while the real one is
		// renewed, so what is in it right now is not necessarily what signs the leaves. Reported and
		// left alone: the bundle keeps publishing both authorities, the chain stays retained above,
		// and the next reconcile after the rotation completes migrates.
		SetMigrationBlocked(config, pinnedTo)
		state.Blocked = true
		state.LegacyAuthority = legacyPEM
		state.LeavesHeldOnLegacyIssuer = true
		return state, nil
	}

	adopted, migrated, err := ensureLegacyKeyAdopted(ctx, c, config.Namespace, legacy, legacyCert)
	if err != nil {
		// The one failure here that no further reconcile can clear. Reported and carried past
		// rather than returned, because returning stops the reconcile before the system components
		// are applied: a PKI nobody but an administrator can resolve would hold back every other
		// component of the release, including the upgrade that might carry the fix. The subject
		// goes to the apply so that it keeps requesting the identity the peers hold rather than
		// reverting the platform CA to the one the manifest names, which is the identity they have.
		refused := &reseedRefusedError{}
		if errors.As(err, &refused) {
			SetMigrationRefused(config, refused.carriedIdentity)
			state.Blocked = true
			state.CACommonName = refused.carriedIdentity
			state.LegacyAuthority = legacyPEM
			return state, nil
		}
		return state, err
	}

	// The subject the platform CA is requested under follows the key rather than leading it. A
	// certificate under the legacy subject proves nothing on its own: over a key of cert-manager's
	// own it validates none of the leaves the legacy CA signed, and it is indistinguishable from the
	// one state this migration must never touch, a platform CA that has already taken the identity
	// over. Requested only once the key is verifiably the legacy one, that state cannot be reached
	// by accident, so re-seeding stays available to every reconcile that needs it and the refusal
	// below means what it says.
	if adopted {
		state.CACommonName = legacyCert.Subject.CommonName
	}
	state.LegacyAuthority = legacyPEM
	state.LegacyAuthorityRetired = migrated
	// The leaves move onto the platform issuer only once its CA validates everything the legacy one
	// did, so cert-manager reissues them under the subject they already name and the move is not a
	// change of trust the BMCs have to converge on.
	state.LeavesHeldOnLegacyIssuer = !migrated
	if !migrated {
		state.Pending = true
		return state, nil
	}

	// The legacy chain goes once the certificate that replaces it has reached the peers, which is
	// the bundle no longer carrying it. Reading that from the published bundle rather than assuming
	// the write above landed keeps a failed or conflicted bundle write from deleting the only copy
	// of the authority still in it.
	published, err := legacyAuthorityPublished(ctx, c, config, legacyPEM)
	if err != nil {
		return state, err
	}
	if published {
		state.Pending = true
		return state, nil
	}
	if err := deleteLegacyCA(ctx, c, config.Namespace); err != nil {
		return state, err
	}
	// Retained no longer, so the chain over the deleted Secret is left out of the apply this
	// reconcile goes on to do and the ApplySet prunes it. Keeping it for one reconcile more would
	// have cert-manager issue a fresh keypair into the Secret just deleted: a provisioning CA that
	// signed nothing, and a migration that starts over on an identity no peer ever had.
	state.LegacyChainRetained = false
	return state, nil
}

// SetMigrationBlocked reports a migration held back by a CA rotation in flight on
// CertManagementReadyCondition.
func SetMigrationBlocked(config *operatorv1.DPFOperatorConfig, pinnedTo string) {
	conditions.AddFalse(
		config,
		operatorv1.CertManagementReadyCondition,
		operatorv1.CertManagementReasonCAMigrationBlocked,
		conditions.ConditionMessage(fmt.Sprintf(
			"Issuer %q is pinned to Secret %q, which is how a CA rotation keeps signing with the previous "+
				"authority while the current one is renewed. The keypair of the provisioning CA is carried into "+
				"the platform intermediate CA once that rotation completes and the pin is removed, since what is "+
				"in %q during it is not necessarily what signs the certificates the peers hold.",
			operatorv1.LegacyProvisioningIssuerName, pinnedTo, operatorv1.LegacyProvisioningCASecretName)))
}

// SetMigrationRefused reports a migration that cannot go on without an administrator on
// CertManagementReadyCondition.
func SetMigrationRefused(config *operatorv1.DPFOperatorConfig, carriedIdentity string) {
	conditions.AddFalse(
		config,
		operatorv1.CertManagementReadyCondition,
		operatorv1.CertManagementReasonCAMigrationRefused,
		conditions.ConditionMessage(fmt.Sprintf(
			"The platform intermediate CA in Secret %q already stands in for %q, and Secret %q holds a different "+
				"keypair, which the certificates the peers hold were not signed with. Adopting it would retire the "+
				"certificate that proves the identity they were provisioned with, so it is left alone and the trust "+
				"bundle keeps publishing both authorities. Delete %q to resolve this: the CA Certificate and the "+
				"issuers over it are emitted only while that Secret exists, so they go with it. Putting the keypair "+
				"of %q back into it is not an alternative, because that Certificate is live again for as long as the "+
				"Secret is there and cert-manager reissues over a keypair of its own into it.",
			operatorv1.PlatformIntermediateCAName, carriedIdentity, operatorv1.LegacyProvisioningCASecretName,
			operatorv1.LegacyProvisioningCASecretName, carriedIdentity)))
}

// reseedRefusedError is the state the migration cannot get itself out of: the platform CA stands in
// for the legacy one already, and the Secret of that legacy chain holds some other keypair.
type reseedRefusedError struct {
	namespace string
	// carriedIdentity is the subject the platform intermediate CA stands in for, which the apply has
	// to keep requesting for as long as this lasts.
	carriedIdentity string
}

func (e *reseedRefusedError) Error() string {
	return fmt.Sprintf(
		"refusing to re-seed the platform intermediate CA Secret %s/%s: it already carries the identity of %q, "+
			"and Secret %s/%s holds a different keypair, which the certificates the peers hold were not signed with",
		e.namespace, operatorv1.PlatformIntermediateCAName, e.carriedIdentity,
		e.namespace, operatorv1.LegacyProvisioningCASecretName)
}

// ensureLegacyKeyAdopted seeds the private key of the legacy CA into the Secret of the platform
// intermediate CA. It reports whether that key is in place, which is what lets the subject that
// goes with it be requested, and whether cert-manager has issued the certificate over it.
//
// Only the key is written, and the certificate it no longer matches is left alone. cert-manager
// answers a key that disagrees with its certificate by reissuing over the key, since the
// certificate is the half it treats as stale, and with the rotationPolicy the platform CA is
// requested with it keeps that key rather than generating one. Removing the certificate to ask for
// the same issuance asks for it with no key to keep, and cert-manager stages one of its own, taking
// with it the identity this migration exists to carry over.
//
// A Secret holding a different key is re-seeded, but only while the platform CA is still under the
// subject of the manifest. That is the cluster this FR's earlier releases anchored a platform CA on
// before this migration existed: it has an intermediate of its own, with an identity no peer was
// ever provisioned with, and collapsing the authorities means replacing it. The certificate it had
// stays in the trust bundle, which never prunes on its own, so the leaves signed under it keep
// validating until they reissue. Under a subject carried over, the same replacement would throw
// away the identity the peers hold, so it is refused instead.
func ensureLegacyKeyAdopted(ctx context.Context, c client.Client, namespace string, legacy *corev1.Secret,
	legacyCert *x509.Certificate) (bool, bool, error) {
	log := ctrllog.FromContext(ctx)
	legacyKey := legacy.Data[corev1.TLSPrivateKeyKey]
	if len(legacyKey) == 0 {
		return false, false, nil
	}

	platform, err := getSecret(ctx, c, namespace, operatorv1.PlatformIntermediateCAName)
	if err != nil {
		return false, false, err
	}

	if platform == nil {
		seed := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      operatorv1.PlatformIntermediateCAName,
				Namespace: namespace,
				Labels:    map[string]string{operatorv1.DPFComponentLabelKey: "dpf-operator"},
			},
			Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{
				corev1.TLSPrivateKeyKey: legacyKey,
				// A TLS Secret has to carry both keys, and an empty certificate is what asks
				// cert-manager to issue one.
				corev1.TLSCertKey: {},
			},
		}
		if err := c.Create(ctx, seed); err != nil {
			return false, false, fmt.Errorf("failed to seed the platform intermediate CA Secret %s/%s with the keypair of %s: %w",
				namespace, operatorv1.PlatformIntermediateCAName, operatorv1.LegacyProvisioningCASecretName, err)
		}
		log.Info("Carried the keypair of the provisioning CA into the platform intermediate CA",
			logKeySecret, operatorv1.PlatformIntermediateCAName)
		return false, false, nil
	}

	_, platformCert, err := caCertificate(platform)
	if err != nil {
		return false, false, err
	}

	if !sameKeypair(platform.Data[corev1.TLSPrivateKeyKey], legacyKey) {
		// Re-seeding replaces the identity of the platform CA, and is only ever safe while that
		// identity is the one the manifest asks for: an intermediate an earlier release of this FR
		// anchored under a subject of its own, which no peer was provisioned with. A platform CA
		// already standing in for a CA that was is a migration that has happened, so a legacy
		// Secret holding some other key is not an authority to adopt. Adopting it would discard
		// the identity the peers hold and retire the certificate that proves it.
		//
		// Nothing but a legacy Secret that is genuinely not the one carried over reaches this,
		// because the subject is requested only once the key behind it has been adopted: a
		// certificate cert-manager issued over a key of its own lands under the subject of the
		// manifest, which is re-seedable, and never under a carried one.
		if platformCert != nil && platformCert.Subject.CommonName != operatorv1.PlatformIntermediateCAName {
			return false, false, &reseedRefusedError{namespace: namespace, carriedIdentity: platformCert.Subject.CommonName}
		}

		patch := client.MergeFromWithOptions(platform.DeepCopy(), client.MergeFromWithOptimisticLock{})
		if platform.Data == nil {
			platform.Data = map[string][]byte{}
		}
		// Only the key is written, and the certificate that no longer matches it is left where it
		// is. That mismatch is what asks cert-manager to issue, and the one request it answers by
		// keeping the key it finds in the Secret: the certificate is what it treats as wrong, so
		// the key is what it preserves. Emptying the certificate instead asks for an issuance with
		// nothing to preserve a key for, and cert-manager stages one of its own, which costs the
		// migration the identity it exists to carry over.
		platform.Data[corev1.TLSPrivateKeyKey] = legacyKey
		if err := c.Patch(ctx, platform, patch); err != nil {
			return false, false, fmt.Errorf("failed to re-seed the platform intermediate CA Secret %s/%s with the keypair of %s: %w",
				namespace, operatorv1.PlatformIntermediateCAName, operatorv1.LegacyProvisioningCASecretName, err)
		}
		log.Info("Replaced the keypair of the platform intermediate CA with the one of the provisioning CA",
			logKeySecret, operatorv1.PlatformIntermediateCAName)
		return false, false, nil
	}

	// The key is in place, so the subject can be requested. It is the certificate over it that
	// decides whether the migration is complete.
	if platformCert == nil {
		return true, false, nil
	}
	return true, carriesLegacyIdentity(ctx, platformCert, legacyCert), nil
}

// sameKeypair reports whether two PEM encoded private keys are the same key.
//
// They are compared as parsed keys rather than as bytes, because one key has more than one
// encoding and cert-manager writes the key back into the Secret every time it issues, in whichever
// encoding the Certificate asks for. A key seeded here and written back in another is
// byte-different and the same key; reading it as a different one would re-seed a platform CA that
// needs nothing, or refuse a migration that has already happened.
//
// A key that cannot be read is the same as nothing, which leaves a Secret holding something other
// than a key to be re-seeded rather than compared against.
func sameKeypair(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	parsedA, err := parsePrivateKey(a)
	if err != nil {
		return false
	}
	parsedB, err := parsePrivateKey(b)
	if err != nil {
		return false
	}
	// The public halves are what the certificates over these keys carry, and comparing them is how
	// every key type in crypto says whether it is the same key.
	public, ok := parsedA.Public().(interface{ Equal(crypto.PublicKey) bool })
	if !ok {
		return false
	}
	return public.Equal(parsedB.Public())
}

// parsePrivateKey reads a private key in any of the encodings cert-manager writes: the PKCS#1 and
// SEC1 forms it defaults to, and PKCS#8 where the Certificate asks for it.
func parsePrivateKey(keyPEM []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("no PEM block holding a private key")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("unrecognized private key encoding: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("private key of type %T cannot sign", key)
	}
	return signer, nil
}

// carriesLegacyIdentity reports whether the certificate of the platform intermediate CA stands in
// for the legacy one, which is what makes removing that certificate from the trust bundle a no-op
// for the peers rather than a change of trust they have to converge on first.
//
// All three parts are required. The public key has to be the one the leaves were signed with, or
// their signatures do not verify. The subject has to be the one they name as their issuer, or there
// is no chain to verify those signatures along in the first place: a certificate issued under the
// subject of the manifest over the adopted key validates nothing the legacy CA signed. And it has
// to be signed by the anchor rather than by itself, or it is the legacy certificate copied into
// another Secret rather than one the platform chain can be rooted at.
//
// Checking the key alone would be enough if the subject were requested before the key is seeded,
// but it is not: the apply that requests it runs after this, so cert-manager can issue over the
// seeded key under the subject of the manifest first, and that certificate is signed by the anchor
// like the right one would be.
//
// The subject is compared as encoded rather than by common name, because that is how a verifier
// matches it against the issuer of a leaf.
func carriesLegacyIdentity(ctx context.Context, platformCert, legacyCert *x509.Certificate) bool {
	if !bytes.Equal(platformCert.RawSubjectPublicKeyInfo, legacyCert.RawSubjectPublicKeyInfo) {
		return false
	}
	if !bytes.Equal(platformCert.RawSubject, legacyCert.RawSubject) {
		// Logged rather than waited on silently: the next apply requests the subject and the next
		// reconcile sees it, so this is one reconcile of an upgrade, and anything longer is a
		// migration that has stalled with both authorities still published.
		ctrllog.FromContext(ctx).Info(
			"The platform intermediate CA has adopted the keypair of the provisioning CA but not its subject, so the provisioning CA stays published",
			"subject", platformCert.Subject.CommonName, "expectedSubject", legacyCert.Subject.CommonName)
		return false
	}
	return !bytes.Equal(platformCert.RawIssuer, platformCert.RawSubject)
}

// migratedCACommonName returns the subject of the platform intermediate CA when it is not the one
// the manifest requests, which is how a carried over identity is recognized after the Secret it was
// carried from is gone. It returns an empty name for a platform CA with a subject of its own, and
// for one not issued yet.
func migratedCACommonName(ctx context.Context, c client.Client, namespace string) (string, error) {
	platform, err := getSecret(ctx, c, namespace, operatorv1.PlatformIntermediateCAName)
	if err != nil || platform == nil {
		return "", err
	}
	_, cert, err := caCertificate(platform)
	if err != nil || cert == nil {
		return "", err
	}
	if cert.Subject.CommonName == operatorv1.PlatformIntermediateCAName {
		return "", nil
	}
	return cert.Subject.CommonName, nil
}

// provisioningCAPinnedTo returns the Secret the legacy provisioning Issuer signs with when that is
// not the CA Secret of its own chain, and an empty name otherwise.
//
// A pin is how the CA rotation flow keeps the previous authority signing while the current one is
// renewed, so it says the Secret this migration reads is not what signs the certificates the peers
// hold.
//
// Both the pin a rotation asks for and the one it already has count. The configured one is what the
// apply later in this reconcile writes onto the Issuer, so a rotation that has only just been
// started is in flight before its pin is on the cluster, and reading the Issuer alone would migrate
// in the window between the two. The Issuer is read as well so that a rotation is recognized
// however it was driven, and on a cluster where the field that drove it is already gone.
func provisioningCAPinnedTo(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) (string, error) {
	if config.Spec.Overrides != nil {
		//nolint:staticcheck // The field is deprecated, and still the only thing a rotation of the
		// chain being taken over is driven by.
		if v := config.Spec.Overrides.ProvisioningIssuerCASecretName; v != nil && *v != "" &&
			*v != operatorv1.LegacyProvisioningCASecretName {
			return *v, nil
		}
	}

	issuer := &unstructured.Unstructured{}
	issuer.SetGroupVersionKind(IssuerGVK)
	key := client.ObjectKey{Namespace: config.Namespace, Name: operatorv1.LegacyProvisioningIssuerName}
	if err := c.Get(ctx, key, issuer); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get Issuer %s: %w", key, err)
	}

	secretName, found, err := unstructured.NestedString(issuer.Object, "spec", "ca", "secretName")
	if err != nil {
		return "", fmt.Errorf("failed to read the CA Secret of Issuer %s: %w", key, err)
	}
	if !found || secretName == operatorv1.LegacyProvisioningCASecretName {
		return "", nil
	}
	return secretName, nil
}

// legacyAuthorityPublished reports whether the trust bundle still carries the legacy certificate.
func legacyAuthorityPublished(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig, legacyPEM []byte) (bool, error) {
	bundle := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: config.Namespace, Name: config.GetCATrustBundleConfigMapName()}
	if err := c.Get(ctx, key, bundle); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get ConfigMap %s: %w", key, err)
	}
	return bytes.Contains([]byte(bundle.Data[operatorv1.CATrustBundleKey]), legacyPEM), nil
}

// deleteLegacyCA removes the Certificate that issues the keypair of the provisioning CA and the
// Secret holding it, which the platform intermediate CA now carries.
//
// The Certificate goes first, and the order is the point of doing this here at all. cert-manager
// reissues into a Secret its Certificate no longer has, generating a fresh keypair, so deleting the
// Secret while that Certificate is still on the cluster hands the next reconcile a provisioning CA
// that signed nothing and that no peer was ever provisioned with. That reconcile cannot tell it
// apart from the real one: it re-seeds the platform CA with it, retires the certificate that proves
// the identity the peers actually hold, and arrives at the same point one reconcile later.
//
// Deleting it here does not race the apply the caller goes on to do. The chain is out of the
// desired inventory by then, so the ApplySet has nothing to put back and its own prune of this
// object is a no-op. The issuer the leaves referenced and the self-signed issuer over it are left
// to that prune, since neither can bring the keypair back.
//
// Both are deleted explicitly rather than left to cert-manager, which only owns the Secrets of
// Certificates it still has, and the keypair lives on in the platform intermediate CA regardless.
func deleteLegacyCA(ctx context.Context, c client.Client, namespace string) error {
	certificate := &unstructured.Unstructured{}
	certificate.SetGroupVersionKind(CertificateGVK)
	certificate.SetNamespace(namespace)
	certificate.SetName(operatorv1.LegacyProvisioningCACertificateName)
	if err := c.Delete(ctx, certificate); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete Certificate %s/%s: %w",
			namespace, operatorv1.LegacyProvisioningCACertificateName, err)
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: namespace,
		Name:      operatorv1.LegacyProvisioningCASecretName,
	}}
	if err := c.Delete(ctx, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete Secret %s/%s: %w", namespace, operatorv1.LegacyProvisioningCASecretName, err)
	}
	ctrllog.FromContext(ctx).Info("Removed the keypair of the provisioning CA the platform intermediate CA took over from",
		logKeySecret, operatorv1.LegacyProvisioningCASecretName)
	return nil
}

// getSecret reads a Secret, reporting an absent one as a nil Secret rather than an error, since
// every caller here has something to do with one that is not there.
func getSecret(ctx context.Context, c client.Client, namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: namespace, Name: name}
	if err := c.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get Secret %s: %w", key, err)
	}
	return secret, nil
}

// caCertificate returns the certificate of a CA Secret, as the PEM block it is published in and
// parsed. A Secret cert-manager has not issued into yet yields no certificate rather than an error.
func caCertificate(secret *corev1.Secret) ([]byte, *x509.Certificate, error) {
	rest := secret.Data[corev1.TLSCertKey]
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, nil, nil
		}
		if block.Type != certificatePEMBlockType {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse the certificate in Secret %s/%s: %w",
				secret.Namespace, secret.Name, err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: certificatePEMBlockType, Bytes: block.Bytes}), cert, nil
	}
}
