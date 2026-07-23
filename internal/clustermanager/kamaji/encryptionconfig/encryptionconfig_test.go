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
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/kmsplugin/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	v1 "k8s.io/apiserver/pkg/apis/apiserver/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/yaml"
)

// TestEncryptionConfig runs the encryptionconfig Ginkgo suite.
func TestEncryptionConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Encryption Config Suite")
}

var _ = Describe("encryptionconfig", func() {
	var (
		ctx        context.Context
		testScheme *runtime.Scheme
		owner      *provisioningv1.DPUCluster
		key        types.NamespacedName
	)

	staticKey := func(b byte) string {
		return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32))
	}

	ref := func(name string) provisioningv1.ObservedSecretKeyRef {
		return provisioningv1.ObservedSecretKeyRef{
			Name:            name,
			Key:             "key",
			Namespace:       "test-ns",
			UID:             "uid-" + name,
			ResourceVersion: "1",
		}
	}

	kmsEndpoint := func() string {
		return "unix://" + config.DefaultSocketFile
	}

	newStore := func(objects ...client.Object) *Store {
		return NewStore(fake.NewClientBuilder().WithScheme(testScheme).WithObjects(objects...).Build(), testScheme)
	}

	BeforeEach(func() {
		ctx = context.Background()
		testScheme = scheme.Scheme
		Expect(provisioningv1.AddToScheme(testScheme)).To(Succeed())
		owner = &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "test-ns", UID: "cluster-uid"},
		}
		key = types.NamespacedName{Name: "encryption-config", Namespace: "test-ns"}
	})

	Describe("Store", func() {
		It("initializes and reloads staticKey configs", func() {
			store := newStore()
			source := SourceKey{Key: []byte(staticKey(0x01)), Ref: ref("etcd-key")}

			config, err := store.InitializeStaticKey(ctx, key, owner, map[string]string{"cluster": "cluster"}, source)
			Expect(err).NotTo(HaveOccurred())

			Expect(config.Provider()).To(Equal(operatorv1.EtcdEncryptionProviderStaticKey))
			Expect(config.Phase()).To(Equal(PhaseIdle))
			Expect(config.ActiveKeyRef()).To(Equal(source.Ref))

			loaded, err := store.Load(ctx, key, owner)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.Provider()).To(Equal(operatorv1.EtcdEncryptionProviderStaticKey))
		})

		It("initializes and reloads VaultKMS configs", func() {
			store := newStore()

			config, err := store.InitializeVaultKMS(ctx, key, owner, nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(config.Provider()).To(Equal(operatorv1.EtcdEncryptionProviderVaultKMS))
			loaded, err := store.Load(ctx, key, owner)
			Expect(err).NotTo(HaveOccurred())
			_, ok := loaded.(VaultKMS)
			Expect(ok).To(BeTrue())
		})

		It("skips updates when the persisted config is unchanged", func() {
			updateCalls := 0
			baseClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
			testClient := interceptor.NewClient(baseClient, interceptor.Funcs{
				Update: func(updateCtx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					updateCalls++
					return c.Update(updateCtx, obj, opts...)
				},
			})
			store := NewStore(testClient, testScheme)
			source := SourceKey{Key: []byte(staticKey(0x01)), Ref: ref("etcd-key")}
			_, err := store.InitializeStaticKey(ctx, key, owner, nil, source)
			Expect(err).NotTo(HaveOccurred())
			config, err := store.Load(ctx, key, owner)
			Expect(err).NotTo(HaveOccurred())

			saved, err := store.Save(ctx, config)

			Expect(err).NotTo(HaveOccurred())
			Expect(saved.Provider()).To(Equal(operatorv1.EtcdEncryptionProviderStaticKey))
			Expect(updateCalls).To(BeZero())
		})

		It("rejects Secrets owned by another DPUCluster", func() {
			store := newStore()
			_, err := store.InitializeVaultKMS(ctx, key, owner, nil)
			Expect(err).NotTo(HaveOccurred())
			other := &provisioningv1.DPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "test-ns", UID: "other-uid"}}

			_, err = store.Load(ctx, key, other)

			var ownershipErr *OwnershipError
			Expect(errors.As(err, &ownershipErr)).To(BeTrue())
		})

		It("returns a conflict when saving a stale wrapper", func() {
			store := newStore()
			source := SourceKey{Key: []byte(staticKey(0x01)), Ref: ref("etcd-key")}
			config, err := store.InitializeStaticKey(ctx, key, owner, nil, source)
			Expect(err).NotTo(HaveOccurred())
			secret := &corev1.Secret{}
			Expect(store.client.Get(ctx, key, secret)).To(Succeed())
			secret.Labels = map[string]string{"external": "change"}
			Expect(store.client.Update(ctx, secret)).To(Succeed())

			_, err = store.Save(ctx, config)

			Expect(err).To(MatchError(ContainSubstring("update encryption config secret")))
		})
	})

	Describe("provider dispatch", func() {
		It("trusts the provider annotation and rejects mismatched content", func() {
			rendered, err := renderStaticKeyEncryptionConfig([]keyEntry{{Name: "key-a", Secret: staticKey(0x01)}})
			Expect(err).NotTo(HaveOccurred())
			secret := baseSecret(key, nil, operatorv1.EtcdEncryptionProviderVaultKMS)
			secret.Data[ConfigFileName] = rendered

			_, err = parse(secret)

			Expect(err).To(MatchError(ContainSubstring("declares vaultKMS but first provider is not kms")))
		})

		It("rejects staticKey annotations on VaultKMS content", func() {
			rendered, err := renderVaultKMSEncryptionConfig()
			Expect(err).NotTo(HaveOccurred())
			secret := baseSecret(key, nil, operatorv1.EtcdEncryptionProviderStaticKey)
			secret.Data[ConfigFileName] = rendered

			_, err = parse(secret)

			Expect(err).To(MatchError(ContainSubstring("declares staticKey but first provider is not aesgcm")))
		})

		It("requires the provider annotation", func() {
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}

			_, err := parse(secret)

			Expect(err).To(MatchError(ContainSubstring("missing provider annotation")))
		})

		DescribeTable("rejects malformed canonical config shape",
			func(mutate func(*v1.EncryptionConfiguration), expected string) {
				cfg := &v1.EncryptionConfiguration{
					TypeMeta: metav1.TypeMeta{APIVersion: v1.SchemeGroupVersion.String(), Kind: "EncryptionConfiguration"},
					Resources: []v1.ResourceConfiguration{{
						Resources: []string{"secrets", "configmaps"},
						Providers: []v1.ProviderConfiguration{
							{KMS: &v1.KMSConfiguration{APIVersion: "v2", Name: vaultKMSProviderName, Endpoint: kmsEndpoint()}},
							{Identity: &v1.IdentityConfiguration{}},
						},
					}},
				}
				mutate(cfg)
				raw, err := yaml.Marshal(cfg)
				Expect(err).NotTo(HaveOccurred())
				secret := baseSecret(key, nil, operatorv1.EtcdEncryptionProviderVaultKMS)
				secret.Data[ConfigFileName] = raw

				_, err = parse(secret)

				Expect(err).To(MatchError(ContainSubstring(expected)))
			},
			Entry("wrong apiVersion", func(cfg *v1.EncryptionConfiguration) { cfg.APIVersion = "v1" }, "unsupported apiVersion"),
			Entry("wrong kind", func(cfg *v1.EncryptionConfiguration) { cfg.Kind = "Config" }, "unsupported kind"),
			Entry("wrong resources", func(cfg *v1.EncryptionConfiguration) { cfg.Resources[0].Resources = []string{"secrets"} }, "expected encrypted resources"),
			Entry("missing identity fallback", func(cfg *v1.EncryptionConfiguration) { cfg.Resources[0].Providers = cfg.Resources[0].Providers[:1] }, "expected exactly two providers"),
			Entry("wrong fallback provider", func(cfg *v1.EncryptionConfiguration) {
				cfg.Resources[0].Providers[1] = v1.ProviderConfiguration{AESGCM: &v1.AESConfiguration{}}
			}, "expected identity fallback"),
			Entry("multiple primary provider kinds", func(cfg *v1.EncryptionConfiguration) {
				cfg.Resources[0].Providers[0].AESGCM = &v1.AESConfiguration{}
			}, "expected primary provider to configure exactly one provider kind"),
			Entry("multiple fallback provider kinds", func(cfg *v1.EncryptionConfiguration) {
				cfg.Resources[0].Providers[1].KMS = &v1.KMSConfiguration{}
			}, "expected identity fallback provider"),
		)
	})

	Describe("staticKey transitions", func() {
		renderedStaticKeys := func(config StaticKey) []v1.Key {
			secret, err := secretForConfig(config)
			Expect(err).NotTo(HaveOccurred())
			rendered := &v1.EncryptionConfiguration{}
			Expect(yaml.Unmarshal(secret.Data[ConfigFileName], rendered)).To(Succeed())
			Expect(rendered.Resources).To(HaveLen(1))
			Expect(rendered.Resources[0].Providers).NotTo(BeEmpty())
			Expect(rendered.Resources[0].Providers[0].AESGCM).NotTo(BeNil())
			return rendered.Resources[0].Providers[0].AESGCM.Keys
		}

		It("moves through every persisted phase with the expected rendered key order", func() {
			store := newStore()
			oldKey := staticKey(0x01)
			newKey := staticKey(0x02)
			idle, err := store.InitializeStaticKey(ctx, key, owner, nil, SourceKey{Key: []byte(oldKey), Ref: ref("active")})
			Expect(err).NotTo(HaveOccurred())
			idleKeys := renderedStaticKeys(idle)
			Expect(idleKeys).To(HaveLen(1))
			Expect(idleKeys[0].Secret).To(Equal(oldKey))

			prepared, err := idle.TransitionToPrepared(SourceKey{Key: []byte(newKey), Ref: ref("target")})
			Expect(err).NotTo(HaveOccurred())
			Expect(prepared.Phase()).To(Equal(PhasePrepared))
			preparedKeys := renderedStaticKeys(prepared)
			Expect(preparedKeys).To(HaveLen(2))
			Expect(preparedKeys[0]).To(Equal(idleKeys[0]))
			Expect(preparedKeys[1].Secret).To(Equal(newKey))

			savedPrepared, err := store.Save(ctx, prepared)
			Expect(err).NotTo(HaveOccurred())
			promoted, err := savedPrepared.(StaticKey).TransitionToPromoted()
			Expect(err).NotTo(HaveOccurred())
			Expect(promoted.Phase()).To(Equal(PhasePromoted))
			Expect(renderedStaticKeys(promoted)).To(Equal([]v1.Key{preparedKeys[1], preparedKeys[0]}))
			rotationID, err := promoted.RotationID()
			Expect(err).NotTo(HaveOccurred())
			Expect(rotationID).To(MatchRegexp(`^[0-9a-f]{32}$`))

			finalized, err := promoted.TransitionToFinalized()
			Expect(err).NotTo(HaveOccurred())
			Expect(finalized.Phase()).To(Equal(PhaseFinalized))
			Expect(renderedStaticKeys(finalized)).To(Equal([]v1.Key{preparedKeys[1]}))

			idleAgain, err := finalized.TransitionToIdle()
			Expect(err).NotTo(HaveOccurred())
			Expect(idleAgain.Phase()).To(Equal(PhaseIdle))
			Expect(idleAgain.ActiveKeyRef()).To(Equal(ref("target")))
			Expect(renderedStaticKeys(idleAgain)).To(Equal([]v1.Key{preparedKeys[1]}))
			_, err = idleAgain.RotationID()
			Expect(err).To(HaveOccurred())
		})

		It("rejects illegal transitions", func() {
			store := newStore()
			idle, err := store.InitializeStaticKey(ctx, key, owner, nil, SourceKey{Key: []byte(staticKey(0x01)), Ref: ref("active")})
			Expect(err).NotTo(HaveOccurred())

			_, err = idle.TransitionToPromoted()

			var transitionErr *TransitionError
			Expect(errors.As(err, &transitionErr)).To(BeTrue())
		})

		It("refreshes active metadata when the desired key is already active", func() {
			store := newStore()
			idle, err := store.InitializeStaticKey(ctx, key, owner, nil, SourceKey{Key: []byte(staticKey(0x01)), Ref: ref("active")})
			Expect(err).NotTo(HaveOccurred())
			refreshedRef := ref("refreshed")

			refreshed, err := idle.TransitionToPrepared(SourceKey{Key: []byte(staticKey(0x01)), Ref: refreshedRef})

			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.Phase()).To(Equal(PhaseIdle))
			Expect(refreshed.ActiveKeyRef()).To(Equal(refreshedRef))
		})

		It("rejects source keys with incomplete metadata", func() {
			store := newStore()
			_, err := store.InitializeStaticKey(ctx, key, owner, nil, SourceKey{Key: []byte(staticKey(0x01)), Ref: provisioningv1.ObservedSecretKeyRef{}})

			Expect(err).To(MatchError(ContainSubstring("source Secret metadata is incomplete")))
		})

		It("rejects source keys with invalid key material during transition", func() {
			store := newStore()
			idle, err := store.InitializeStaticKey(ctx, key, owner, nil, SourceKey{Key: []byte(staticKey(0x01)), Ref: ref("active")})
			Expect(err).NotTo(HaveOccurred())

			_, err = idle.TransitionToPrepared(SourceKey{Key: []byte("not-base64"), Ref: ref("target")})

			Expect(err).To(MatchError(ContainSubstring("key must be base64-encoded AES key text")))
		})
	})

	Describe("validation", func() {
		It("validates base64 encoded AES key text", func() {
			got, err := ValidateBase64StaticKeyText([]byte("  " + staticKey(0x01) + "\n"))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(staticKey(0x01)))

			_, err = ValidateBase64StaticKeyText([]byte("not-base64"))
			Expect(err).To(MatchError(ContainSubstring("key must be base64-encoded AES key text")))
		})

		It("rejects Idle configs with target metadata", func() {
			secret := baseSecret(key, nil, operatorv1.EtcdEncryptionProviderStaticKey)
			rendered, err := renderStaticKeyEncryptionConfig([]keyEntry{{Name: "key-a", Secret: staticKey(0x01)}})
			Expect(err).NotTo(HaveOccurred())
			secret.Data[ConfigFileName] = rendered
			secret.Annotations[staticKeyPhaseAnnotation] = string(PhaseIdle)
			setStaticKeyActiveRef(secret, ref("active"))
			setStaticKeyTargetRef(secret, ref("target"))

			_, err = parseStaticKey(secret)

			Expect(err).To(MatchError(ContainSubstring("Idle staticKey config must not contain target")))
		})

		It("rejects VaultKMS configs with staticKey metadata", func() {
			secret := baseSecret(key, nil, operatorv1.EtcdEncryptionProviderVaultKMS)
			rendered, err := renderVaultKMSEncryptionConfig()
			Expect(err).NotTo(HaveOccurred())
			secret.Data[ConfigFileName] = rendered
			secret.Annotations[staticKeyPhaseAnnotation] = string(PhaseIdle)

			_, err = parseVaultKMS(secret)

			Expect(err).To(MatchError(ContainSubstring("must not contain staticKey metadata")))
		})

		It("rejects duplicate key names", func() {
			secret := baseSecret(key, nil, operatorv1.EtcdEncryptionProviderStaticKey)
			rendered, err := renderEncryptionConfiguration(v1.ProviderConfiguration{
				AESGCM: &v1.AESConfiguration{
					Keys: []v1.Key{
						{Name: "key-a", Secret: staticKey(0x01)},
						{Name: "key-a", Secret: staticKey(0x02)},
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			secret.Data[ConfigFileName] = rendered
			secret.Annotations[staticKeyPhaseAnnotation] = string(PhasePrepared)
			setStaticKeyActiveRef(secret, ref("active"))
			setStaticKeyTargetRef(secret, ref("target"))

			_, err = parseStaticKey(secret)

			Expect(err).To(MatchError(ContainSubstring("duplicate aesgcm key name")))
		})

		DescribeTable("rejects staticKey phase shape mismatches",
			func(phase Phase, keys []keyEntry, activeRef provisioningv1.ObservedSecretKeyRef, targetRef *provisioningv1.ObservedSecretKeyRef, expected string) {
				secret := baseSecret(key, nil, operatorv1.EtcdEncryptionProviderStaticKey)
				rendered, err := renderStaticKeyEncryptionConfig(keys)
				Expect(err).NotTo(HaveOccurred())
				secret.Data[ConfigFileName] = rendered
				secret.Annotations[staticKeyPhaseAnnotation] = string(phase)
				setStaticKeyActiveRef(secret, activeRef)
				if targetRef != nil {
					setStaticKeyTargetRef(secret, *targetRef)
				}

				_, err = parseStaticKey(secret)

				Expect(err).To(MatchError(ContainSubstring(expected)))
			},
			Entry("prepared requires target metadata", PhasePrepared,
				[]keyEntry{{Name: "key-a", Secret: staticKey(0x01)}, {Name: "key-b", Secret: staticKey(0x02)}},
				ref("active"), nil, "Prepared staticKey config is missing target"),
			Entry("promoted active must match target", PhasePromoted,
				[]keyEntry{{Name: "key-b", Secret: staticKey(0x02)}, {Name: "key-a", Secret: staticKey(0x01)}},
				ref("active"), ptr.To(ref("target")), "Promoted staticKey active source Secret metadata must match target"),
			Entry("finalized requires one key", PhaseFinalized,
				[]keyEntry{{Name: "key-b", Secret: staticKey(0x02)}, {Name: "key-a", Secret: staticKey(0x01)}},
				ref("target"), ptr.To(ref("target")), "expected one Finalized staticKey entry"),
		)

		DescribeTable("rejects malformed VaultKMS provider fields",
			func(kms *v1.KMSConfiguration, expected string) {
				raw, err := renderEncryptionConfiguration(v1.ProviderConfiguration{KMS: kms})
				Expect(err).NotTo(HaveOccurred())
				secret := baseSecret(key, nil, operatorv1.EtcdEncryptionProviderVaultKMS)
				secret.Data[ConfigFileName] = raw

				_, err = parseVaultKMS(secret)

				Expect(err).To(MatchError(ContainSubstring(expected)))
			},
			Entry("api version", &v1.KMSConfiguration{APIVersion: "v1", Name: vaultKMSProviderName, Endpoint: kmsEndpoint()}, "apiVersion v2"),
			Entry("provider name", &v1.KMSConfiguration{APIVersion: "v2", Name: "other", Endpoint: kmsEndpoint()}, "provider name"),
			Entry("endpoint", &v1.KMSConfiguration{APIVersion: "v2", Name: vaultKMSProviderName, Endpoint: "unix:///other.sock"}, "provider endpoint"),
		)
	})
})
