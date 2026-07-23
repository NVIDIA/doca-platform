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

package nvidia

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/clustermanager/kamaji/encryptionconfig"
	testutils "github.com/nvidia/doca-platform/test/utils"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	storagemigrationv1 "k8s.io/api/storagemigration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// verifiedReloadVerifier reports every config as reloaded.
type verifiedReloadVerifier struct{}

// VerifyReload always reports success.
func (v *verifiedReloadVerifier) VerifyReload(context.Context, *provisioningv1.DPUCluster, encryptionconfig.StaticKey) (bool, error) {
	return true, nil
}

// unverifiedReloadVerifier reports that reload is incomplete and optionally returns an error.
type unverifiedReloadVerifier struct {
	err error
}

// VerifyReload reports the configured unsuccessful verification result.
func (v *unverifiedReloadVerifier) VerifyReload(context.Context, *provisioningv1.DPUCluster, encryptionconfig.StaticKey) (bool, error) {
	return false, v.err
}

// staticTenantClientProvider returns a fixed tenant client.
type staticTenantClientProvider struct {
	client      client.Client
	deadline    time.Time
	hasDeadline bool
}

// Client returns the configured fake tenant client.
func (p *staticTenantClientProvider) Client(ctx context.Context, _ *provisioningv1.DPUCluster) (client.Client, error) {
	p.deadline, p.hasDeadline = ctx.Deadline()
	return p.client, nil
}

// failingTenantClientProvider returns a configured tenant client error.
type failingTenantClientProvider struct {
	err error
}

// Client returns the configured error.
func (p *failingTenantClientProvider) Client(context.Context, *provisioningv1.DPUCluster) (client.Client, error) {
	return nil, p.err
}

// noopRequeueScheduler ignores scheduled requests.
type noopRequeueScheduler struct{}

// Schedule discards the request.
func (*noopRequeueScheduler) Schedule(types.NamespacedName, time.Duration) {}

// recordingRequeueScheduler records scheduled requests.
type recordingRequeueScheduler struct {
	requests []scheduledRequeue
}

type scheduledRequeue struct {
	cluster types.NamespacedName
	after   time.Duration
}

// Schedule records the request.
func (s *recordingRequeueScheduler) Schedule(cluster types.NamespacedName, after time.Duration) {
	s.requests = append(s.requests, scheduledRequeue{cluster: cluster, after: after})
}

var _ = Describe("staticKey rotation controller", func() {
	const testRotationID = "0123456789abcdef0123456789abcdef"

	var testNS *corev1.Namespace

	BeforeEach(func() {
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-static-key-rotation-"}}
		Expect(k8sClient.Create(ctx, testNS)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, testNS)
	})

	staticKey := func(b byte) string {
		return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32))
	}

	newDPUCluster := func() *provisioningv1.DPUCluster {
		dc := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-cluster-",
				Namespace:    testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type:       string(provisioningv1.KamajiCluster),
				MaxNodes:   100,
				Kubeconfig: "kubeconfig",
			},
		}
		Expect(k8sClient.Create(ctx, dc)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, dc)
		dc.Status.Phase = provisioningv1.PhaseReady
		return dc
	}

	createOperatorConfig := func(ref operatorv1.SecretKeyRef) *operatorv1.DPFOperatorConfig {
		cfg := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: testNS.Name},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("pvc"),
				},
				KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
					EtcdEncryptionAtRest: &operatorv1.EtcdEncryptionAtRestConfiguration{
						Provider: operatorv1.EtcdEncryptionProviderStaticKey,
						StaticKey: &operatorv1.StaticKeyConfiguration{
							KeySecretRef: ref,
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cfg)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, cfg)
		return cfg
	}

	createSourceSecret := func(name, key string) *corev1.Secret {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNS.Name},
			Data:       map[string][]byte{"key": []byte(key)},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, secret)
		return secret
	}

	observedRef := func(name string) provisioningv1.ObservedSecretKeyRef {
		return provisioningv1.ObservedSecretKeyRef{
			Name:            name,
			Key:             "key",
			Namespace:       testNS.Name,
			UID:             "target-uid",
			ResourceVersion: "1",
		}
	}

	loadStaticKey := func(dc *provisioningv1.DPUCluster) encryptionconfig.StaticKey {
		cfg, err := encryptionconfig.NewStore(k8sClient, scheme.Scheme).Load(ctx,
			encryptionConfigSecretName(dc), dc)
		Expect(err).NotTo(HaveOccurred())
		staticKey, ok := cfg.(encryptionconfig.StaticKey)
		Expect(ok).To(BeTrue())
		return staticKey
	}

	createIdleConfig := func(dc *provisioningv1.DPUCluster, key string, ref provisioningv1.ObservedSecretKeyRef) encryptionconfig.StaticKey {
		cfg, err := encryptionconfig.NewStore(k8sClient, scheme.Scheme).InitializeStaticKey(ctx,
			encryptionConfigSecretName(dc), dc,
			map[string]string{provisioningv1.DPUClusterNameLabelKey: dc.Name},
			encryptionconfig.SourceKey{Key: []byte(key), Ref: ref})
		Expect(err).NotTo(HaveOccurred())
		return cfg
	}

	saveConfig := func(cfg encryptionconfig.StaticKey) encryptionconfig.StaticKey {
		saved, err := encryptionconfig.NewStore(k8sClient, scheme.Scheme).Save(ctx, cfg)
		Expect(err).NotTo(HaveOccurred())
		staticKey, ok := saved.(encryptionconfig.StaticKey)
		Expect(ok).To(BeTrue())
		return staticKey
	}

	createPreparedConfig := func(dc *provisioningv1.DPUCluster, oldKey, newKey string, activeRef, targetRef provisioningv1.ObservedSecretKeyRef) encryptionconfig.StaticKey {
		idle := createIdleConfig(dc, oldKey, activeRef)
		prepared, err := idle.TransitionToPrepared(encryptionconfig.SourceKey{Key: []byte(newKey), Ref: targetRef})
		Expect(err).NotTo(HaveOccurred())
		return saveConfig(prepared)
	}

	createPromotedConfig := func(dc *provisioningv1.DPUCluster, oldKey, newKey string, activeRef, targetRef provisioningv1.ObservedSecretKeyRef) encryptionconfig.StaticKey {
		prepared := createPreparedConfig(dc, oldKey, newKey, activeRef, targetRef)
		promoted, err := prepared.TransitionToPromoted()
		Expect(err).NotTo(HaveOccurred())
		return saveConfig(promoted)
	}

	createFinalizedConfig := func(dc *provisioningv1.DPUCluster, oldKey, newKey string, activeRef, targetRef provisioningv1.ObservedSecretKeyRef) encryptionconfig.StaticKey {
		promoted := createPromotedConfig(dc, oldKey, newKey, activeRef, targetRef)
		finalized, err := promoted.TransitionToFinalized()
		Expect(err).NotTo(HaveOccurred())
		return saveConfig(finalized)
	}

	Describe("NewHandler", func() {
		It("configures rotation dependencies and applies overrides", func() {
			scheduler := &noopRequeueScheduler{}
			verifier := &verifiedReloadVerifier{}
			recorder := record.NewFakeRecorder(1)

			got := NewHandler(k8sClient, scheme.Scheme, "keepalived:test",
				WithRequeueScheduler(scheduler),
				WithRequeueAfter(5*time.Second),
				WithReloadVerifier(verifier),
				WithEventRecorder(recorder))

			handler, ok := got.(*clusterHandler)
			Expect(ok).To(BeTrue())
			Expect(handler.requeueScheduler).To(BeIdenticalTo(scheduler))
			Expect(handler.requeueAfter).To(Equal(5 * time.Second))
			Expect(handler.reloadVerifier).To(BeIdenticalTo(verifier))
			Expect(handler.recorder).To(BeIdenticalTo(recorder))
			Expect(handler.tenantClient).NotTo(BeNil())
		})
	})

	Describe("ReconcileCluster", func() {
		It("returns staticKey rotation conditions with the cluster condition", func() {
			key := staticKey(0x01)
			dc := newDPUCluster()
			createOperatorConfig(operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"})
			sourceSecret := createSourceSecret("etcd-key", key)
			createIdleConfig(dc, key, observedSecretKeyRef(sourceSecret, "key"))
			tcp := &kamajiv1.TenantControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: dc.Name, Namespace: dc.Namespace},
				Spec: kamajiv1.TenantControlPlaneSpec{
					ControlPlane: kamajiv1.ControlPlane{
						Service: kamajiv1.ServiceSpec{ServiceType: kamajiv1.ServiceTypeNodePort},
					},
					NetworkProfile: kamajiv1.NetworkProfileSpec{Port: 30443},
				},
			}
			Expect(k8sClient.Create(ctx, tcp)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, tcp)
			tcp.Status.Kubernetes.Version.Status = ptr.To(kamajiv1.VersionReady)
			Expect(k8sClient.Status().Update(ctx, tcp)).To(Succeed())
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme, requeueScheduler: &noopRequeueScheduler{}}

			kubeconfig, conditions, err := handler.ReconcileCluster(ctx, dc)

			Expect(err).NotTo(HaveOccurred())
			Expect(kubeconfig).To(Equal(adminKubeconfigName(dc)))
			Expect(meta.FindStatusCondition(conditions, string(provisioningv1.ConditionCreated))).NotTo(BeNil())
			Expect(meta.FindStatusCondition(conditions, string(provisioningv1.ConditionEtcdEncryptionRotationInProgress))).NotTo(BeNil())
			Expect(meta.FindStatusCondition(conditions, string(provisioningv1.ConditionEtcdEncryptionRotationBlocked))).NotTo(BeNil())
			Expect(dc.Status.EtcdEncryptionAtRest.StaticKey.ActiveKeyRef).To(Equal(ptr.To(observedSecretKeyRef(sourceSecret, "key"))))
		})
	})

	Describe("staticKeyConditions", func() {
		type expectedCondition struct {
			status metav1.ConditionStatus
			reason string
		}

		DescribeTable("returns condition-specific statuses and reasons",
			func(activeCondition provisioningv1.ConditionType, reason string, want map[string]expectedCondition) {
				conditions := staticKeyConditions(activeCondition, reason, "message")
				Expect(conditions).To(HaveLen(2))
				for _, condition := range conditions {
					expected := want[condition.Type]
					Expect(condition.Status).To(Equal(expected.status), "condition %s", condition.Type)
					Expect(condition.Reason).To(Equal(expected.reason), "condition %s", condition.Type)
				}
			},
			Entry("idle", provisioningv1.ConditionType(""), staticKeyStateIdle, map[string]expectedCondition{
				string(provisioningv1.ConditionEtcdEncryptionRotationInProgress): {status: metav1.ConditionFalse, reason: staticKeyStateIdle},
				string(provisioningv1.ConditionEtcdEncryptionRotationBlocked):    {status: metav1.ConditionFalse, reason: staticKeyReasonNotBlocked},
			}),
			Entry("in progress", provisioningv1.ConditionEtcdEncryptionRotationInProgress, staticKeyStatePromoted, map[string]expectedCondition{
				string(provisioningv1.ConditionEtcdEncryptionRotationInProgress): {status: metav1.ConditionTrue, reason: staticKeyStatePromoted},
				string(provisioningv1.ConditionEtcdEncryptionRotationBlocked):    {status: metav1.ConditionFalse, reason: staticKeyReasonNotBlocked},
			}),
			Entry("blocked", provisioningv1.ConditionEtcdEncryptionRotationBlocked, "InvalidStaticKey", map[string]expectedCondition{
				string(provisioningv1.ConditionEtcdEncryptionRotationInProgress): {status: metav1.ConditionFalse, reason: staticKeyReasonBlocked},
				string(provisioningv1.ConditionEtcdEncryptionRotationBlocked):    {status: metav1.ConditionTrue, reason: "InvalidStaticKey"},
			}),
			Entry("disabled", provisioningv1.ConditionEtcdEncryptionRotationBlocked, staticKeyStateDisabled, map[string]expectedCondition{
				string(provisioningv1.ConditionEtcdEncryptionRotationInProgress): {status: metav1.ConditionFalse, reason: staticKeyReasonBlocked},
				string(provisioningv1.ConditionEtcdEncryptionRotationBlocked):    {status: metav1.ConditionTrue, reason: staticKeyStateDisabled},
			}),
		)
	})

	Describe("reconcileStaticKeyRotation", func() {
		DescribeTable("silently waits for the DPUCluster to become Ready",
			func(phase provisioningv1.ClusterPhase) {
				handler := &clusterHandler{}
				dc := &provisioningv1.DPUCluster{Status: provisioningv1.DPUClusterStatus{Phase: phase}}

				result, err := handler.reconcileStaticKeyRotation(context.Background(), dc)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeNil())
			},
			Entry("while Creating", provisioningv1.PhaseCreating),
			Entry("while NotReady", provisioningv1.PhaseNotReady),
		)

		It("returns transient encryption config load errors for controller retry", func() {
			dc := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: testNS.Name, UID: "cluster-uid"},
				Status:     provisioningv1.DPUClusterStatus{Phase: provisioningv1.PhaseReady},
			}
			loadErr := errors.New("api server unavailable")
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(gctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.Secret); ok && key == encryptionConfigSecretName(dc) {
							return loadErr
						}
						return c.Get(gctx, key, obj, opts...)
					},
				}).
				Build()
			handler := &clusterHandler{Client: fakeClient, Scheme: scheme.Scheme}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)

			Expect(result).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("load staticKey encryption config")))
			Expect(errors.Is(err, loadErr)).To(BeTrue())
		})

		It("blocks malformed encryption config validation errors", func() {
			dc := newDPUCluster()
			createIdleConfig(dc, staticKey(0x01), observedRef("active-key"))
			encryptionSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, encryptionConfigSecretName(dc), encryptionSecret)).To(Succeed())
			encryptionSecret.Data[encryptionconfig.ConfigFileName] = []byte("invalid")
			Expect(k8sClient.Update(ctx, encryptionSecret)).To(Succeed())
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)

			Expect(err).NotTo(HaveOccurred())
			blocked := meta.FindStatusCondition(result.conditions, string(provisioningv1.ConditionEtcdEncryptionRotationBlocked))
			Expect(blocked.Status).To(Equal(metav1.ConditionTrue))
			Expect(blocked.Reason).To(Equal("EncryptionConfigSecretMalformed"))
		})

		It("starts a new idle rotation when the desired source key differs", func() {
			oldKey := staticKey(0x01)
			newKey := staticKey(0x02)
			dc := newDPUCluster()
			createOperatorConfig(operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"})
			sourceSecret := createSourceSecret("etcd-key", newKey)
			activeRef := observedRef("active-key")
			createIdleConfig(dc, oldKey, activeRef)
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			current := loadStaticKey(dc)
			Expect(current.Phase()).To(Equal(encryptionconfig.PhasePrepared))
			Expect(current.ActiveKeyRef()).To(Equal(activeRef))
			Expect(dc.Status.EtcdEncryptionAtRest.StaticKey.ActiveKeyRef).To(Equal(&activeRef))
			_, err = current.TransitionToPromoted()
			Expect(err).NotTo(HaveOccurred())
			Expect(observedSecretKeyRef(sourceSecret, "key").Name).To(Equal("etcd-key"))
		})

		It("refreshes active metadata when the desired source key already matches", func() {
			key := staticKey(0x01)
			dc := newDPUCluster()
			createOperatorConfig(operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"})
			sourceSecret := createSourceSecret("etcd-key", key)
			createIdleConfig(dc, key, observedRef("stale-key"))
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			current := loadStaticKey(dc)
			currentRef := observedSecretKeyRef(sourceSecret, "key")
			Expect(current.Phase()).To(Equal(encryptionconfig.PhaseIdle))
			Expect(current.ActiveKeyRef()).To(Equal(currentRef))
			Expect(dc.Status.EtcdEncryptionAtRest.StaticKey.ActiveKeyRef).To(Equal(&currentRef))
		})

		It("advances Prepared from persisted metadata even when the source Secret changed", func() {
			oldKey := staticKey(0x01)
			persistedNewKey := staticKey(0x02)
			latestSourceKey := staticKey(0x03)
			dc := newDPUCluster()
			createOperatorConfig(operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"})
			createSourceSecret("etcd-key", latestSourceKey)
			targetRef := observedRef("etcd-key")
			createPreparedConfig(dc, oldKey, persistedNewKey, observedRef("active-key"), targetRef)
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme, reloadVerifier: &verifiedReloadVerifier{}}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			current := loadStaticKey(dc)
			Expect(current.Phase()).To(Equal(encryptionconfig.PhasePromoted))
			Expect(current.ActiveKeyRef()).To(Equal(targetRef))
			Expect(dc.Status.EtcdEncryptionAtRest.StaticKey.ActiveKeyRef).To(Equal(&targetRef))
		})

		It("keeps Promoted migrating without the source Secret", func() {
			dc := newDPUCluster()
			dc.Spec.Kubeconfig = ""
			createOperatorConfig(operatorv1.SecretKeyRef{Name: "missing-key", Key: "key"})
			createPromotedConfig(dc, staticKey(0x01), staticKey(0x02), observedRef("active-key"), observedRef("missing-key"))
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme, reloadVerifier: &verifiedReloadVerifier{}}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			inProgress := meta.FindStatusCondition(result.conditions, string(provisioningv1.ConditionEtcdEncryptionRotationInProgress))
			Expect(inProgress.Status).To(Equal(metav1.ConditionTrue))
			Expect(inProgress.Reason).To(Equal(staticKeyStateMigrating))
		})

		It("finalizes Promoted when migrations completed", func() {
			dc := newDPUCluster()
			createOperatorConfig(operatorv1.SecretKeyRef{Name: "missing-key", Key: "key"})
			promoted := createPromotedConfig(dc, staticKey(0x01), staticKey(0x02), observedRef("active-key"), observedRef("missing-key"))
			rotationID, err := promoted.RotationID()
			Expect(err).NotTo(HaveOccurred())
			tenantClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(
					successfulStorageVersionMigration("secrets", rotationID),
					successfulStorageVersionMigration("configmaps", rotationID),
				).
				Build()
			handler := &clusterHandler{
				Client:         k8sClient,
				Scheme:         scheme.Scheme,
				reloadVerifier: &verifiedReloadVerifier{},
				tenantClient:   &staticTenantClientProvider{client: tenantClient},
			}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			Expect(loadStaticKey(dc).Phase()).To(Equal(encryptionconfig.PhaseFinalized))
		})

		It("completes Finalized without the source Secret", func() {
			dc := newDPUCluster()
			targetRef := observedRef("missing-key")
			createOperatorConfig(operatorv1.SecretKeyRef{Name: "missing-key", Key: "key"})
			createFinalizedConfig(dc, staticKey(0x01), staticKey(0x02), observedRef("active-key"), targetRef)
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme, reloadVerifier: &verifiedReloadVerifier{}}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			current := loadStaticKey(dc)
			Expect(current.Phase()).To(Equal(encryptionconfig.PhaseIdle))
			Expect(current.ActiveKeyRef()).To(Equal(targetRef))
			Expect(dc.Status.EtcdEncryptionAtRest.StaticKey.ActiveKeyRef).To(Equal(&targetRef))
		})

		DescribeTable("waits when reload verification is unsuccessful",
			func(phase encryptionconfig.Phase, verifierErr error) {
				const requeueAfter = 5 * time.Second

				dc := newDPUCluster()
				createOperatorConfig(operatorv1.SecretKeyRef{Name: "target-key", Key: "key"})
				activeRef := observedRef("active-key")
				targetRef := observedRef("target-key")
				var current encryptionconfig.StaticKey
				switch phase {
				case encryptionconfig.PhasePrepared:
					current = createPreparedConfig(dc, staticKey(0x01), staticKey(0x02), activeRef, targetRef)
				case encryptionconfig.PhasePromoted:
					current = createPromotedConfig(dc, staticKey(0x01), staticKey(0x02), activeRef, targetRef)
				case encryptionconfig.PhaseFinalized:
					current = createFinalizedConfig(dc, staticKey(0x01), staticKey(0x02), activeRef, targetRef)
				}

				tenantClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
				if phase == encryptionconfig.PhasePromoted {
					rotationID, err := current.RotationID()
					Expect(err).NotTo(HaveOccurred())
					tenantClientBuilder = tenantClientBuilder.WithObjects(
						successfulStorageVersionMigration("secrets", rotationID),
						successfulStorageVersionMigration("configmaps", rotationID),
					)
				}
				scheduler := &recordingRequeueScheduler{}
				handler := &clusterHandler{
					Client:           k8sClient,
					Scheme:           scheme.Scheme,
					reloadVerifier:   &unverifiedReloadVerifier{err: verifierErr},
					tenantClient:     &staticTenantClientProvider{client: tenantClientBuilder.Build()},
					requeueScheduler: scheduler,
					requeueAfter:     requeueAfter,
				}

				result, err := handler.reconcileStaticKeyRotation(ctx, dc)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				Expect(loadStaticKey(dc).Phase()).To(Equal(phase))
				inProgress := meta.FindStatusCondition(result.conditions, string(provisioningv1.ConditionEtcdEncryptionRotationInProgress))
				Expect(inProgress).NotTo(BeNil())
				Expect(inProgress.Status).To(Equal(metav1.ConditionTrue))
				Expect(inProgress.Reason).To(Equal(string(phase)))
				Expect(inProgress.Message).To(ContainSubstring("waiting for all kube-apiserver instances"))
				if verifierErr != nil {
					Expect(inProgress.Message).To(ContainSubstring(verifierErr.Error()))
				}
				Expect(scheduler.requests).To(Equal([]scheduledRequeue{{
					cluster: types.NamespacedName{Namespace: dc.Namespace, Name: dc.Name},
					after:   requeueAfter,
				}}))
			},
			Entry("in Prepared when verification is incomplete", encryptionconfig.PhasePrepared, nil),
			Entry("in Prepared when verification fails", encryptionconfig.PhasePrepared, errors.New("verification failed")),
			Entry("in Promoted when verification is incomplete", encryptionconfig.PhasePromoted, nil),
			Entry("in Promoted when verification fails", encryptionconfig.PhasePromoted, errors.New("verification failed")),
			Entry("in Finalized when verification is incomplete", encryptionconfig.PhaseFinalized, nil),
			Entry("in Finalized when verification fails", encryptionconfig.PhaseFinalized, errors.New("verification failed")),
		)

		It("blocks idle reconciliation when the source Secret is unavailable", func() {
			dc := newDPUCluster()
			createOperatorConfig(operatorv1.SecretKeyRef{Name: "missing-key", Key: "key"})
			createIdleConfig(dc, staticKey(0x01), observedRef("active-key"))
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)
			Expect(err).NotTo(HaveOccurred())

			blocked := meta.FindStatusCondition(result.conditions, string(provisioningv1.ConditionEtcdEncryptionRotationBlocked))
			Expect(blocked.Status).To(Equal(metav1.ConditionTrue))
			Expect(blocked.Reason).To(Equal("StaticKeySourceUnavailable"))
		})

		It("blocks idle reconciliation when the source Secret key is unavailable", func() {
			dc := newDPUCluster()
			createOperatorConfig(operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"})
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "etcd-key", Namespace: testNS.Name},
				Data:       map[string][]byte{"other-key": []byte(staticKey(0x02))},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, secret)
			createIdleConfig(dc, staticKey(0x01), observedRef("active-key"))
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)
			Expect(err).NotTo(HaveOccurred())

			blocked := meta.FindStatusCondition(result.conditions, string(provisioningv1.ConditionEtcdEncryptionRotationBlocked))
			Expect(blocked.Status).To(Equal(metav1.ConditionTrue))
			Expect(blocked.Reason).To(Equal("StaticKeySourceUnavailable"))
		})

		It("returns transient source Secret read errors for controller retry", func() {
			cfg := &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: testNS.Name},
				Spec: operatorv1.DPFOperatorConfigSpec{
					KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
						EtcdEncryptionAtRest: &operatorv1.EtcdEncryptionAtRestConfiguration{
							Provider: operatorv1.EtcdEncryptionProviderStaticKey,
							StaticKey: &operatorv1.StaticKeyConfiguration{
								KeySecretRef: operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"},
							},
						},
					},
				},
			}
			readErr := errors.New("api server unavailable")
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(cfg).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(gctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.Secret); ok && key == (client.ObjectKey{Name: "etcd-key", Namespace: testNS.Name}) {
							return readErr
						}
						return c.Get(gctx, key, obj, opts...)
					},
				}).
				Build()
			handler := &clusterHandler{Client: fakeClient, Scheme: scheme.Scheme}

			desired, result, err := handler.staticKeyDesiredSource(ctx, &provisioningv1.DPUCluster{})

			Expect(desired).To(BeNil())
			Expect(result).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("read staticKey source Secret")))
			Expect(errors.Is(err, readErr)).To(BeTrue())
		})

		It("does not require the source Secret when automatic rotation is disabled", func() {
			dc := newDPUCluster()
			cfg := createOperatorConfig(operatorv1.SecretKeyRef{Name: "missing-key", Key: "key"})
			cfg.Spec.KamajiClusterManager.EtcdEncryptionAtRest.StaticKey.AutomaticRotationDisabled = ptr.To(true)
			Expect(k8sClient.Update(ctx, cfg)).To(Succeed())
			createIdleConfig(dc, staticKey(0x01), observedRef("active-key"))
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)
			Expect(err).NotTo(HaveOccurred())

			blocked := meta.FindStatusCondition(result.conditions, string(provisioningv1.ConditionEtcdEncryptionRotationBlocked))
			Expect(blocked.Status).To(Equal(metav1.ConditionTrue))
			Expect(blocked.Reason).To(Equal(staticKeyStateDisabled))
			Expect(blocked.Message).To(Equal("automatic staticKey rotation is disabled"))
			Expect(loadStaticKey(dc).Phase()).To(Equal(encryptionconfig.PhaseIdle))
		})

		It("disables automatic rotation when staticKey settings are absent", func() {
			dc := newDPUCluster()
			cfg := createOperatorConfig(operatorv1.SecretKeyRef{Name: "missing-key", Key: "key"})
			cfg.Spec.KamajiClusterManager.EtcdEncryptionAtRest = nil
			Expect(k8sClient.Update(ctx, cfg)).To(Succeed())
			createIdleConfig(dc, staticKey(0x01), observedRef("active-key"))
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)
			Expect(err).NotTo(HaveOccurred())

			blocked := meta.FindStatusCondition(result.conditions, string(provisioningv1.ConditionEtcdEncryptionRotationBlocked))
			Expect(blocked.Status).To(Equal(metav1.ConditionTrue))
			Expect(blocked.Reason).To(Equal(staticKeyStateDisabled))
			Expect(loadStaticKey(dc).Phase()).To(Equal(encryptionconfig.PhaseIdle))
		})

		It("does not publish staticKey conditions for a VaultKMS config", func() {
			dc := newDPUCluster()
			_, err := encryptionconfig.NewStore(k8sClient, scheme.Scheme).InitializeVaultKMS(ctx,
				encryptionConfigSecretName(dc), dc, nil)
			Expect(err).NotTo(HaveOccurred())
			handler := &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme}

			result, err := handler.reconcileStaticKeyRotation(ctx, dc)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.conditions).To(BeEmpty())
			Expect(dc.Status.EtcdEncryptionAtRest).To(BeNil())
		})
	})

	Describe("reconcileStaticKeyStorageMigration", func() {
		It("returns tenant client failures for controller retry", func() {
			handler := &clusterHandler{tenantClient: &failingTenantClientProvider{err: errors.New("tenant API unavailable")}}
			dc := &provisioningv1.DPUCluster{Spec: provisioningv1.DPUClusterSpec{Kubeconfig: "kubeconfig"}}

			_, _, _, err := handler.reconcileStaticKeyStorageMigration(context.Background(), dc, "rotation")

			Expect(err).To(MatchError("create tenant client: tenant API unavailable"))
		})

		It("bounds tenant API operations with a timeout", func() {
			tenantClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			provider := &staticTenantClientProvider{client: tenantClient}
			handler := &clusterHandler{tenantClient: provider}
			dc := &provisioningv1.DPUCluster{Spec: provisioningv1.DPUClusterSpec{Kubeconfig: "kubeconfig"}}
			start := time.Now()

			_, _, _, err := handler.reconcileStaticKeyStorageMigration(context.Background(), dc, testRotationID)

			Expect(err).NotTo(HaveOccurred())
			Expect(provider.hasDeadline).To(BeTrue())
			Expect(provider.deadline).To(BeTemporally("~", start.Add(staticKeyStorageMigrationTimeout), time.Second))
		})

		It("creates a missing migration and then observes it as pending", func() {
			tenantClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()

			done, blocked, message, err := reconcileStorageVersionMigration(context.Background(), tenantClient, "test-secrets-migration", "secrets")

			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeFalse())
			Expect(blocked).To(BeFalse())
			Expect(message).To(ContainSubstring("created StorageVersionMigration"))
			created := &storagemigrationv1.StorageVersionMigration{}
			Expect(tenantClient.Get(context.Background(), client.ObjectKey{Name: "test-secrets-migration"}, created)).To(Succeed())
			Expect(created.Spec.Resource).To(Equal(metav1.GroupResource{Resource: "secrets"}))
		})

		It("waits for a deleting migration before re-creating it", func() {
			now := metav1.Now()
			svm := successfulStorageVersionMigration("secrets", "rotation")
			svm.Finalizers = []string{"test-finalizer"}
			svm.DeletionTimestamp = &now
			tenantClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(svm).
				Build()

			done, blocked, message, err := reconcileStorageVersionMigration(
				context.Background(), tenantClient, svm.Name, "secrets")

			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeFalse())
			Expect(blocked).To(BeFalse())
			Expect(message).To(ContainSubstring("deletion"))
		})

		It("creates missing migrations for all encrypted resources in one reconciliation", func() {
			rotationID := testRotationID
			tenantClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			handler := &clusterHandler{tenantClient: &staticTenantClientProvider{client: tenantClient}}
			dc := &provisioningv1.DPUCluster{Spec: provisioningv1.DPUClusterSpec{Kubeconfig: "kubeconfig"}}

			migrated, blocked, message, err := handler.reconcileStaticKeyStorageMigration(context.Background(), dc, rotationID)

			Expect(err).NotTo(HaveOccurred())
			Expect(migrated).To(BeFalse())
			Expect(blocked).To(BeFalse())
			Expect(message).To(ContainSubstring("created StorageVersionMigration"))
			Expect(message).To(ContainSubstring(storageVersionMigrationName("secrets", rotationID)))
			Expect(message).To(ContainSubstring(storageVersionMigrationName("configmaps", rotationID)))
			for _, resource := range encryptionconfig.EncryptedResources() {
				migration := &storagemigrationv1.StorageVersionMigration{}
				Expect(tenantClient.Get(context.Background(), client.ObjectKey{
					Name: storageVersionMigrationName(resource, rotationID),
				}, migration)).To(Succeed())
				Expect(migration.Spec.Resource).To(Equal(metav1.GroupResource{Resource: resource}))
			}
		})

		It("deletes migration objects after all migrations complete", func() {
			rotationID := testRotationID
			tenantClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(
					successfulStorageVersionMigration("secrets", rotationID),
					successfulStorageVersionMigration("configmaps", rotationID),
				).
				Build()
			handler := &clusterHandler{tenantClient: &staticTenantClientProvider{client: tenantClient}}
			dc := &provisioningv1.DPUCluster{Spec: provisioningv1.DPUClusterSpec{Kubeconfig: "kubeconfig"}}

			migrated, blocked, _, err := handler.reconcileStaticKeyStorageMigration(context.Background(), dc, rotationID)

			Expect(err).NotTo(HaveOccurred())
			Expect(migrated).To(BeTrue())
			Expect(blocked).To(BeFalse())
			for _, resource := range encryptionconfig.EncryptedResources() {
				svm := &storagemigrationv1.StorageVersionMigration{}
				err := tenantClient.Get(context.Background(), client.ObjectKey{
					Name: storageVersionMigrationName(resource, rotationID),
				}, svm)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
		})

		It("keeps migration objects when a migration fails", func() {
			rotationID := testRotationID
			tenantClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(
					successfulStorageVersionMigration("secrets", rotationID),
					failedStorageVersionMigration("configmaps", rotationID),
				).
				Build()
			handler := &clusterHandler{tenantClient: &staticTenantClientProvider{client: tenantClient}}
			dc := &provisioningv1.DPUCluster{Spec: provisioningv1.DPUClusterSpec{Kubeconfig: "kubeconfig"}}

			migrated, blocked, message, err := handler.reconcileStaticKeyStorageMigration(context.Background(), dc, rotationID)

			Expect(err).NotTo(HaveOccurred())
			Expect(migrated).To(BeFalse())
			Expect(blocked).To(BeTrue())
			Expect(message).To(ContainSubstring("failed"))
			for _, resource := range encryptionconfig.EncryptedResources() {
				svm := &storagemigrationv1.StorageVersionMigration{}
				Expect(tenantClient.Get(context.Background(), client.ObjectKey{
					Name: storageVersionMigrationName(resource, rotationID),
				}, svm)).To(Succeed())
			}
		})

		It("does not block completion when cleanup fails", func() {
			rotationID := testRotationID
			baseTenantClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(
					successfulStorageVersionMigration("secrets", rotationID),
					successfulStorageVersionMigration("configmaps", rotationID),
				).
				Build()
			deleteErr := errors.New("tenant API unavailable")
			tenantClient := interceptor.NewClient(baseTenantClient, interceptor.Funcs{
				Delete: func(deleteCtx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if obj.GetName() == storageVersionMigrationName("configmaps", rotationID) {
						return deleteErr
					}
					return c.Delete(deleteCtx, obj, opts...)
				},
			})
			handler := &clusterHandler{tenantClient: &staticTenantClientProvider{client: tenantClient}}
			dc := &provisioningv1.DPUCluster{Spec: provisioningv1.DPUClusterSpec{Kubeconfig: "kubeconfig"}}

			migrated, blocked, message, err := handler.reconcileStaticKeyStorageMigration(context.Background(), dc, rotationID)

			Expect(err).NotTo(HaveOccurred())
			Expect(migrated).To(BeTrue())
			Expect(blocked).To(BeFalse())
			Expect(message).To(ContainSubstring("completed"))
			deleted := &storagemigrationv1.StorageVersionMigration{}
			err = baseTenantClient.Get(context.Background(), client.ObjectKey{
				Name: storageVersionMigrationName("secrets", rotationID),
			}, deleted)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			retained := &storagemigrationv1.StorageVersionMigration{}
			Expect(baseTenantClient.Get(context.Background(), client.ObjectKey{
				Name: storageVersionMigrationName("configmaps", rotationID),
			}, retained)).To(Succeed())
		})
	})

	Describe("secretToDPUClusters", func() {
		It("maps the configured staticKey source Secret to Kamaji clusters", func() {
			dc := newDPUCluster()
			createOperatorConfig(operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"})
			sourceSecret := createSourceSecret("etcd-key", staticKey(0x01))
			staticCluster := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "static-cluster-", Namespace: testNS.Name},
				Spec: provisioningv1.DPUClusterSpec{
					Type:       string(provisioningv1.StaticCluster),
					Kubeconfig: "kubeconfig",
					MaxNodes:   100,
				},
			}
			Expect(k8sClient.Create(ctx, staticCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, staticCluster)
			staticKeySecretWatchClient.Store(k8sClient)

			requests := secretToDPUClusters(ctx, sourceSecret)

			Expect(requests).To(ContainElement(reconcile.Request{
				NamespacedName: types.NamespacedName{Name: dc.Name, Namespace: dc.Namespace},
			}))
			Expect(requests).NotTo(ContainElement(reconcile.Request{
				NamespacedName: types.NamespacedName{Name: staticCluster.Name, Namespace: staticCluster.Namespace},
			}))
		})

		It("maps a per-cluster encryption config Secret directly to its DPUCluster", func() {
			dc := newDPUCluster()
			createIdleConfig(dc, staticKey(0x01), observedRef("active-key"))
			encryptionSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, encryptionConfigSecretName(dc), encryptionSecret)).To(Succeed())
			staticKeySecretWatchClient.Store(k8sClient)

			Expect(secretToDPUClusters(ctx, encryptionSecret)).To(Equal([]reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: dc.Name, Namespace: dc.Namespace}},
			}))
		})
	})

	Describe("publishStaticKeyBlocked", func() {
		It("emits events only on state changes", func() {
			recorder := record.NewFakeRecorder(2)
			handler := &clusterHandler{recorder: recorder}
			dc := &provisioningv1.DPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "test-ns"}}

			result, err := handler.publishStaticKeyBlocked(dc, "InvalidStaticKey", "key is invalid")
			Expect(err).NotTo(HaveOccurred())
			Expect(recorder.Events).To(Receive(ContainSubstring("Warning EtcdEncryptionRotationBlocked key is invalid")))

			dc.Status.Conditions = result.conditions
			_, err = handler.publishStaticKeyBlocked(dc, "InvalidStaticKey", "key is invalid")
			Expect(err).NotTo(HaveOccurred())
			Consistently(recorder.Events, 10*time.Millisecond).ShouldNot(Receive())
		})
	})

	Describe("storageVersionMigrationName", func() {
		It("uses the opaque rotation ID deterministically", func() {
			rotationID := testRotationID

			Expect(storageVersionMigrationName("secrets", rotationID)).To(Equal("dpf-ear-secrets-" + rotationID))
			Expect(storageVersionMigrationName("configmaps", rotationID)).To(Equal("dpf-ear-configmaps-" + rotationID))
		})
	})
})

// successfulStorageVersionMigration builds a completed migration object for tests.
func successfulStorageVersionMigration(resource, rotationID string) *storagemigrationv1.StorageVersionMigration {
	svm := storageVersionMigrationObject(storageVersionMigrationName(resource, rotationID), resource)
	svm.Status.Conditions = []metav1.Condition{
		{
			Type:   string(storagemigrationv1.MigrationSucceeded),
			Status: metav1.ConditionTrue,
		},
	}
	return svm
}

// failedStorageVersionMigration builds a failed migration object for tests.
func failedStorageVersionMigration(resource, rotationID string) *storagemigrationv1.StorageVersionMigration {
	svm := storageVersionMigrationObject(storageVersionMigrationName(resource, rotationID), resource)
	svm.Status.Conditions = []metav1.Condition{
		{
			Type:    string(storagemigrationv1.MigrationFailed),
			Status:  metav1.ConditionTrue,
			Message: "migration failed",
		},
	}
	return svm
}
