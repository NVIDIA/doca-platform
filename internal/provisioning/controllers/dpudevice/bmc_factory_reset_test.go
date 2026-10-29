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

package dpudevice

import (
	"context"
	"errors"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const factoryResetTestPassword = "dpfSecretPassword1"

var _ = Describe("reconcileBMCFactoryReset", func() {
	var ctx context.Context

	BeforeEach(func() { ctx = context.Background() })

	// setupFactoryResetTest returns a BMC that enforces authentication and a reconciler holding the
	// shared credential Secret, which is what the reset step authenticates with.
	setupFactoryResetTest := func(bmcPassword, secretPassword string, funcs ...interceptor.Funcs) (*mock.RedfishMockServer, *DPUDeviceReconciler) {
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		mockServer, err := mock.CreateMockRedfishServer("BF-24.10", bmcPassword)
		Expect(err).NotTo(HaveOccurred())
		mockServer.SetEnforceBasicAuth(true)

		builder := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&provisioningv1.DPUDevice{}).
			WithObjects(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rfclient.BMCPasswordSecret,
					Namespace: testNamespace,
					UID:       types.UID("factory-reset-secret-uid"),
				},
				Data: map[string][]byte{rfclient.BMCSharedPasswordKey: []byte(secretPassword)},
			})
		if len(funcs) > 0 {
			builder = builder.WithInterceptorFuncs(funcs[0])
		}

		fakeClient := builder.Build()
		return mockServer, &DPUDeviceReconciler{Client: fakeClient, RedfishClients: rfclient.NewProvider(fakeClient)}
	}

	// newDevice also creates the DPUDevice on the API server, because the reset step records its
	// request time there itself rather than through the patch the reconcile defers to its end.
	newDevice := func(reconciler *DPUDeviceReconciler, mockServer *mock.RedfishMockServer, name string) *provisioningv1.DPUDevice {
		bmcIP := mockServer.GetIPAddress()
		bmcPort := uint32(mockServer.GetPort())
		dpuDevice := &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  testNamespace,
				Generation: 1,
				// The fake client does not assign a UID. The rejection annotation is keyed by it.
				UID: types.UID(name),
			},
			Spec: provisioningv1.DPUDeviceSpec{SerialNumber: mock.DpuSerialNumber},
		}
		Expect(reconciler.Client.Create(ctx, dpuDevice)).To(Succeed())
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status = provisioningv1.DPUDeviceStatus{BMCIP: &bmcIP, BMCPort: &bmcPort}
		Expect(reconciler.Client.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())
		return dpuDevice
	}

	// persistedRequestTime reads status.bmcFactoryResetRequestTime back from the API server.
	persistedRequestTime := func(reconciler *DPUDeviceReconciler, dpuDevice *provisioningv1.DPUDevice) *metav1.Time {
		stored := &provisioningv1.DPUDevice{}
		Expect(reconciler.Client.Get(ctx, client.ObjectKeyFromObject(dpuDevice), stored)).To(Succeed())
		return stored.Status.BMCFactoryResetRequestTime
	}

	updateSecretPassword := func(reconciler *DPUDeviceReconciler, password string) {
		secret := &corev1.Secret{}
		Expect(reconciler.Client.Get(ctx, types.NamespacedName{
			Name:      rfclient.BMCPasswordSecret,
			Namespace: testNamespace,
		}, secret)).To(Succeed())
		patch := client.MergeFrom(secret.DeepCopy())
		secret.Data[rfclient.BMCSharedPasswordKey] = []byte(password)
		Expect(reconciler.Client.Patch(ctx, secret, patch)).To(Succeed())
	}

	It("keeps the rejection annotation key within the annotation name limit", func() {
		const deviceUID = types.UID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
		key := rejectedPasswordAnnotationKey(deviceUID)
		name := key[len(provisioningv1.DPUProvisioningPrefix):]
		Expect(len(name)).To(BeNumerically("<=", 63))
		Expect(passwordDigest("a")).NotTo(Equal(passwordDigest("b")))
		Expect(rejectedPasswordAnnotationKey("other-device")).NotTo(Equal(key))
	})

	rejectedPasswordAnnotation := func(reconciler *DPUDeviceReconciler, dpuDevice *provisioningv1.DPUDevice) string {
		secret := &corev1.Secret{}
		Expect(reconciler.Client.Get(ctx, types.NamespacedName{
			Name:      rfclient.BMCPasswordSecret,
			Namespace: testNamespace,
		}, secret)).To(Succeed())
		return secret.Annotations[rejectedPasswordAnnotationKey(dpuDevice.UID)]
	}

	requestFactoryResetRetry := func(reconciler *DPUDeviceReconciler, dpuDevice *provisioningv1.DPUDevice) {
		secret := &corev1.Secret{}
		Expect(reconciler.Client.Get(ctx, types.NamespacedName{
			Name:      rfclient.BMCPasswordSecret,
			Namespace: testNamespace,
		}, secret)).To(Succeed())
		patch := client.MergeFrom(secret.DeepCopy())
		if secret.Annotations == nil {
			secret.Annotations = map[string]string{}
		}
		secret.Annotations[rejectedPasswordAnnotationKey(dpuDevice.UID)] = provisioningv1.BMCFactoryResetRetryAnnotationValue
		Expect(reconciler.Client.Patch(ctx, secret, patch)).To(Succeed())
	}

	// expectSameInstant compares two timestamps by the instant they name. A round trip through the
	// API server drops sub-second precision and renders the zone offset, so the value read back is
	// only ever structurally equal to the one in hand by coincidence of the runner's time zone.
	expectSameInstant := func(actual, expected *metav1.Time) {
		Expect(actual).NotTo(BeNil())
		Expect(actual.Time).To(BeTemporally("==", expected.Rfc3339Copy().Time))
	}

	resetCondition := func(dpuDevice *provisioningv1.DPUDevice) *metav1.Condition {
		return findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady))
	}

	DescribeTable("cleans up rejection annotations before deleting a failed device",
		func(perDeviceSecret, missingSecret, retryCleanup bool) {
			failCleanup := false
			mockServer, reconciler := setupFactoryResetTest("unknownPassword", factoryResetTestPassword, interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*corev1.Secret); ok && failCleanup {
						return errors.New("cannot remove rejection annotation")
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			})
			defer mockServer.Stop()
			device := newDevice(reconciler, mockServer, "reset-delete-failed")
			secretName := rfclient.BMCPasswordSecret
			if perDeviceSecret {
				secretName = "per-device-password"
				status := device.Status
				patch := client.MergeFrom(device.DeepCopy())
				device.Spec.BMCCredentialSecretName = ptr.To(secretName)
				Expect(reconciler.Client.Patch(ctx, device, patch)).To(Succeed())
				device.Status = status
				Expect(reconciler.Client.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: testNamespace},
					Data:       map[string][]byte{rfclient.BMCSharedPasswordKey: []byte(factoryResetTestPassword)},
				})).To(Succeed())
			}
			statusPatch := client.MergeFrom(device.DeepCopy())
			device.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))
			Expect(reconciler.Client.Status().Patch(ctx, device, statusPatch)).To(Succeed())
			_, err := reconciler.reconcileBMCFactoryReset(ctx, device)
			Expect(err).NotTo(HaveOccurred())
			Expect(resetCondition(device).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(reconciler.Client.Get(ctx, client.ObjectKeyFromObject(device), device)).To(Succeed())
			Expect(device.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))
			Expect(device.Status.BMCFactoryResetRequestTime).NotTo(BeNil())

			secret := &corev1.Secret{}
			secretKey := client.ObjectKey{Namespace: testNamespace, Name: secretName}
			Expect(reconciler.Client.Get(ctx, secretKey, secret)).To(Succeed())
			key := rejectedPasswordAnnotationKey(device.UID)
			Expect(secret.Annotations).To(HaveKeyWithValue(key, passwordDigest(factoryResetTestPassword)))
			otherKey := rejectedPasswordAnnotationKey("another-device")
			secretPatch := client.MergeFrom(secret.DeepCopy())
			secret.Annotations[otherKey] = passwordDigest("anotherPassword")
			secret.Annotations["example.com/keep"] = "keep"
			Expect(reconciler.Client.Patch(ctx, secret, secretPatch)).To(Succeed())
			if missingSecret {
				Expect(reconciler.Client.Delete(ctx, secret)).To(Succeed())
			}
			Expect(reconciler.Client.Delete(ctx, device)).To(Succeed())
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(device)}
			if retryCleanup {
				failCleanup = true
				_, err = reconciler.Reconcile(ctx, request)
				Expect(err).To(MatchError(ContainSubstring("cannot remove rejection annotation")))
				Expect(reconciler.Client.Get(ctx, request.NamespacedName, device)).To(Succeed())
				Expect(device.Finalizers).To(ContainElement(provisioningv1.BMCCredentialFinalizer))
				Expect(reconciler.Client.Get(ctx, secretKey, secret)).To(Succeed())
				Expect(secret.Annotations).To(HaveKey(key))
				failCleanup = false
			}
			_, err = reconciler.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(apierrors.IsNotFound(reconciler.Client.Get(ctx, request.NamespacedName, device))).To(BeTrue())
			if !missingSecret {
				Expect(reconciler.Client.Get(ctx, secretKey, secret)).To(Succeed())
				Expect(secret.Annotations).To(Equal(map[string]string{
					otherKey:           passwordDigest("anotherPassword"),
					"example.com/keep": "keep",
				}))
				Expect(string(secret.Data[rfclient.BMCSharedPasswordKey])).To(Equal(factoryResetTestPassword))
			}
		},
		Entry("shared Secret", false, false, false),
		Entry("per-device Secret", true, false, false),
		Entry("Secret already deleted", false, true, false),
		Entry("transient cleanup failure", false, false, true),
	)

	It("does not probe if cleanup protection cannot be persisted", func() {
		mockServer, reconciler := setupFactoryResetTest("unknownPassword", factoryResetTestPassword, interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if _, ok := obj.(*provisioningv1.DPUDevice); ok {
					return errors.New("cannot add credential finalizer")
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		})
		defer mockServer.Stop()
		device := newDevice(reconciler, mockServer, "reset-finalizer-write-failed")
		device.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))
		stop, err := reconciler.reconcileBMCFactoryReset(ctx, device)
		Expect(err).To(MatchError(ContainSubstring("cannot add credential finalizer")))
		Expect(stop).To(BeTrue())
		Expect(mockServer.DefaultPasswordAttempts()).To(BeZero())
		Expect(mockServer.CredentialPasswordAttempts()).To(BeZero())
		Expect(rejectedPasswordAnnotation(reconciler, device)).To(BeEmpty())
	})

	Context("submitting the reset", func() {
		It("submits over the Secret password without writing a password first", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-secret-password")

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			Expect(mockServer.GetAccountPatches()).To(BeEmpty())
			Expect(mockServer.GetFactoryResetRequests()).To(Equal(1))
			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).NotTo(BeNil())
			// A request time that only ever lived in memory would let the returning BMC be reset
			// a second time, so it reaches the API server without waiting for the deferred patch.
			expectSameInstant(persistedRequestTime(reconciler, dpuDevice), dpuDevice.Status.BMCFactoryResetRequestTime)
			Expect(resetCondition(dpuDevice).Status).To(Equal(metav1.ConditionFalse))
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetInProgress))
		})

		It("sets the Secret password on the Redfish user when the BMC is on the factory default", func() {
			mockServer, reconciler := setupFactoryResetTest(rfclient.BMCDefaultPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-default-password")

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			Expect(mockServer.GetAccountPatches()).To(Equal([]string{rfclient.BF3BMCUser}))
			Expect(mockServer.GetFactoryResetRequests()).To(Equal(1))
			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).NotTo(BeNil())
		})

		It("recovers from a crash between the password write and the submission", func() {
			// A crash after the password write leaves the BMC on the Secret password, which is
			// exactly what the next pass tries first.
			mockServer, reconciler := setupFactoryResetTest(rfclient.BMCDefaultPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			mockServer.SetPassword(factoryResetTestPassword)
			dpuDevice := newDevice(reconciler, mockServer, "reset-crash-recovery")

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockServer.GetFactoryResetRequests()).To(Equal(1))
			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).NotTo(BeNil())
		})

		It("reports an actionable failure when the BMC holds an unknown password", func() {
			mockServer, reconciler := setupFactoryResetTest("someoneElsesPassword", factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-unknown-password")

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).To(BeNil())
			Expect(persistedRequestTime(reconciler, dpuDevice)).To(BeNil())
			Expect(dpuDevice.Status.BMCCredentialSecretName).To(BeNil())
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionBMCCredentialsReady))).To(BeNil())

			condition := resetCondition(dpuDevice)
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(condition.Message).To(ContainSubstring("neither the password in the credential Secret nor the factory default"))

			// Nothing is terminal: once the Secret is corrected, the next pass submits.
			mockServer.SetPassword(factoryResetTestPassword)
			_, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockServer.GetFactoryResetRequests()).To(Equal(1))
		})

		It("points at the Secret when it holds the factory default and PasswordChangeRequired blocks ResetToDefaults", func() {
			// VerifyBMCCredential treats PasswordChangeRequired as success so await can probe, but
			// ResetToDefaults is blocked in that state. A Secret that still holds 0penBmc has
			// nothing better to write, so without this guard the submit path would loop on 403.
			mockServer, reconciler := setupFactoryResetTest(rfclient.BMCDefaultPassword, rfclient.BMCDefaultPassword)
			defer mockServer.Stop()
			mockServer.SetPasswordChangeRequired(true)
			dpuDevice := newDevice(reconciler, mockServer, "reset-default-pcr")

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).To(BeNil())
			condition := resetCondition(dpuDevice)
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(condition.Message).To(ContainSubstring("set a non-default password in the credential Secret"))
			Expect(condition.Message).NotTo(ContainSubstring("403"))
		})

		It("still submits when the Secret holds the factory default but Managers answers 200 OK", func() {
			// Rejecting every factory-default Secret would break this path: the BMC is fully
			// usable, so ResetToDefaults can proceed without a password write first.
			mockServer, reconciler := setupFactoryResetTest(rfclient.BMCDefaultPassword, rfclient.BMCDefaultPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-default-ok")

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			Expect(mockServer.GetFactoryResetRequests()).To(Equal(1))
			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).NotTo(BeNil())
		})

		It("reports a BMC that is not answering as unreachable, not as a bad credential", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-unreachable")
			// A BMC that has not finished booting is the common case at this point in
			// initialization, and nothing about it warrants pointing the operator at the Secret.
			mockServer.Stop()

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).To(BeNil())
			condition := resetCondition(dpuDevice)
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(condition.Message).To(ContainSubstring("is not answering"))
			Expect(condition.Message).NotTo(ContainSubstring("correct the Secret"))
		})

		It("retries a submission the BMC rejects, leaving the request time unset", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			mockServer.SetFactoryResetResponse(500, `{"error":{"code":"Base.1.18.1.InternalError","message":"Reset is not available right now."}}`)
			dpuDevice := newDevice(reconciler, mockServer, "reset-rejected")

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).To(BeNil())

			condition := resetCondition(dpuDevice)
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(condition.Message).To(ContainSubstring("Reset is not available right now"))

			mockServer.SetFactoryResetResponse(0, "")
			_, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockServer.GetFactoryResetRequests()).To(Equal(1))
		})

		It("submits the reset exactly once across repeated passes", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-once")

			for i := 0; i < 5; i++ {
				stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
				Expect(err).NotTo(HaveOccurred())
				Expect(stop).To(BeTrue())
				Expect(reconciler.Client.Get(ctx, client.ObjectKeyFromObject(dpuDevice), dpuDevice)).To(Succeed())
			}
			Expect(mockServer.GetFactoryResetRequests()).To(Equal(1))
		})
	})

	Context("waiting for the BMC to come back", func() {
		It("drives a BMC through submission, reboot and return in one sequence", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			mockServer.SetFactoryResetOfflineDuration(2 * time.Second)
			dpuDevice := newDevice(reconciler, mockServer, "reset-full-cycle")

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			// The reset returned both managed accounts to the factory default, which is the state
			// password hardening picks up from.
			Expect(mockServer.GetPassword()).To(Equal(rfclient.BMCDefaultPassword))
			Expect(mockServer.GetServiceAccountPassword()).To(Equal(rfclient.BMCDefaultPassword))

			Expect(reconciler.Client.Get(ctx, client.ObjectKeyFromObject(dpuDevice), dpuDevice)).To(Succeed())
			// Rewind the request time past the settle delay rather than waiting it out. A real BMC
			// is rebooting for far longer than the delay; the mock compresses that to two seconds.
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetInProgress))

			Eventually(func() string {
				stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
				Expect(err).NotTo(HaveOccurred())
				if !stop {
					return resetCondition(dpuDevice).Reason
				}
				return ""
			}, 10*time.Second, 500*time.Millisecond).Should(Equal(provisioningv1.ReasonFactoryResetCompleted))

			Expect(mockServer.GetFactoryResetRequests()).To(Equal(1))
		})

		It("does not probe before the settle delay elapses", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-settle-delay")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.Now())

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			// The BMC is still answering on the pre-reset password, and probing it now would read that
			// as a completed reset. Nothing is concluded before the delay elapses, but the reset
			// condition is still (re)stamped as in-progress: a request time can reach the API server
			// without the condition that was set alongside it (e.g. a crash between the two patches),
			// and this keeps status honest in that window instead of leaving it blank.
			condition := resetCondition(dpuDevice)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetInProgress))
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceInitialized)).Reason).To(Equal(string(conditions.ReasonPending)))
		})

		It("completes once the BMC answers with the factory default password", func() {
			mockServer, reconciler := setupFactoryResetTest(rfclient.BMCDefaultPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-completed")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))
			recorder := &retireRecorder{ClientProvider: reconciler.RedfishClients}
			reconciler.RedfishClients = recorder

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())
			// The reset wiped the BMC's trust state, so clients cached for it are dropped.
			Expect(recorder.retired).To(ConsistOf(dpuDevice.BMCAddress()))

			condition := resetCondition(dpuDevice)
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted))
		})

		It("detects a delayed reset without retrying the rejected factory default", func() {
			// The BMC still holds the Secret password when the fixed settle delay expires. Keep
			// checking that accepted credential until the delayed reset changes it, then verify
			// the factory default and let password hardening proceed.
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-default-rejected")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			condition := resetCondition(dpuDevice)
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetAwaitingCompletion))
			Expect(condition.Message).To(ContainSubstring("credential Secret password still works"))
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceInitialized)).Reason).To(Equal(string(conditions.ReasonPending)))
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
			rejectedDefaultAttempts := mockServer.DefaultPasswordAttempts()
			Expect(rejectedDefaultAttempts).NotTo(BeZero())

			// Another pass uses the known-good Secret password and remains non-terminal.
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetAwaitingCompletion))
			Expect(mockServer.DefaultPasswordAttempts()).To(Equal(rejectedDefaultAttempts))

			// Simulate the accepted ResetToDefaults completing after both checks.
			mockServer.SetPassword(rfclient.BMCDefaultPassword)
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted))
		})

		It("probes the factory default when the Secret is changed to it before that password is rejected", func() {
			// AwaitingCompletion means the previous Secret password still worked, so this pass
			// has not confirmed a factory-default rejection. Replacing the Secret with 0penBmc
			// must probe that password; failing immediately would miss a reset that has completed.
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-secret-changed-to-default")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetAwaitingCompletion))

			updateSecretPassword(reconciler, rfclient.BMCDefaultPassword)
			mockServer.SetPassword(rfclient.BMCDefaultPassword)
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted))
		})

		It("fails without a second factory-default login when the Secret holds the rejected default", func() {
			mockServer, reconciler := setupFactoryResetTest("someoneElsesPassword", rfclient.BMCDefaultPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-secret-is-rejected-default")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(resetCondition(dpuDevice).Message).To(ContainSubstring("holds that same password"))
			Expect(mockServer.DefaultPasswordAttempts()).To(Equal(int64(1)))
			Expect(mockServer.CredentialPasswordAttempts()).To(BeZero())
		})

		It("does not resend a rejected Secret password after the factory-default probe times out", func() {
			// The delayed reset rejects the Secret password, then the factory-default probe times
			// out. The next pass must retry only that probe: sending the Secret password again can
			// lock the BMC account.
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-secret-rejected-timeout")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetAwaitingCompletion))

			mockServer.SetPassword("someoneElsesPassword")
			mockServer.SetFactoryDefaultProbeUnavailable(true)
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCredentialRejected))
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceInitialized)).Reason).To(Equal(string(conditions.ReasonPending)))
			rejectedSecretAttempts := mockServer.CredentialPasswordAttempts()

			// Another timeout keeps the rejection and does not send the Secret password.
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCredentialRejected))
			Expect(mockServer.CredentialPasswordAttempts()).To(Equal(rejectedSecretAttempts))

			// The factory default is then rejected. The already-rejected Secret password stays unsent.
			mockServer.SetFactoryDefaultProbeUnavailable(false)
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceInitialized)).Reason).To(Equal(string(conditions.ReasonError)))
			Expect(mockServer.CredentialPasswordAttempts()).To(Equal(rejectedSecretAttempts))
		})

		It("completes after a preserved Secret rejection once the factory default answers", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-secret-rejected-then-default")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			mockServer.SetPassword("someoneElsesPassword")
			mockServer.SetFactoryDefaultProbeUnavailable(true)
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCredentialRejected))
			rejectedSecretAttempts := mockServer.CredentialPasswordAttempts()

			mockServer.SetFactoryDefaultProbeUnavailable(false)
			mockServer.SetPassword(rfclient.BMCDefaultPassword)
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted))
			Expect(mockServer.CredentialPasswordAttempts()).To(Equal(rejectedSecretAttempts))
		})

		It("stops when the factory default and the Secret password are both rejected", func() {
			// Both credentials were rejected, so another pass must not send either one again.
			mockServer, reconciler := setupFactoryResetTest("someoneElsesPassword", factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-both-passwords-rejected")
			statusPatch := client.MergeFrom(dpuDevice.DeepCopy())
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceInitialized)).Reason).To(Equal(string(conditions.ReasonError)))
			Expect(rejectedPasswordAnnotation(reconciler, dpuDevice)).To(Equal(passwordDigest(factoryResetTestPassword)))
			// Reconcile's deferred patch owns condition persistence.
			Expect(reconciler.Client.Status().Patch(ctx, dpuDevice, statusPatch)).To(Succeed())
			stored := &provisioningv1.DPUDevice{}
			Expect(reconciler.Client.Get(ctx, client.ObjectKeyFromObject(dpuDevice), stored)).To(Succeed())
			Expect(findCondition(stored, string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady)).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			attempts := mockServer.DefaultPasswordAttempts()
			secretAttempts := mockServer.CredentialPasswordAttempts()

			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(mockServer.DefaultPasswordAttempts()).To(Equal(attempts))
			Expect(mockServer.CredentialPasswordAttempts()).To(Equal(secretAttempts))
		})

		DescribeTable("does not retry rejected credentials when their annotation cannot be saved",
			func(secretPassword string, delayed bool, previousMarker string) {
				failWrite := false
				mockServer, reconciler := setupFactoryResetTest("someoneElsesPassword", secretPassword, interceptor.Funcs{
					Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						if secret, ok := obj.(*corev1.Secret); ok && failWrite && len(secret.Annotations) > 0 {
							return errors.New("cannot save rejection annotation")
						}
						return c.Patch(ctx, obj, patch, opts...)
					},
				})
				defer mockServer.Stop()
				device := newDevice(reconciler, mockServer, "reset-marker-write-failed")
				statusPatch := client.MergeFrom(device.DeepCopy())
				device.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))
				if previousMarker != "" {
					secret := &corev1.Secret{}
					Expect(reconciler.Client.Get(ctx, client.ObjectKey{
						Namespace: testNamespace,
						Name:      rfclient.BMCPasswordSecret,
					}, secret)).To(Succeed())
					patch := client.MergeFrom(secret.DeepCopy())
					secret.Annotations = map[string]string{rejectedPasswordAnnotationKey(device.UID): previousMarker}
					Expect(reconciler.Client.Patch(ctx, secret, patch)).To(Succeed())
					setBMCFactoryResetCondition(device, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetFailed, "previous rejection")
				}
				if delayed {
					setBMCFactoryResetCondition(device, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetAwaitingCompletion, "waiting for reset")
				}

				failWrite = true
				_, err := reconciler.reconcileBMCFactoryReset(ctx, device)
				Expect(err).To(MatchError(ContainSubstring("cannot save rejection annotation")))
				Expect(resetCondition(device).Reason).To(Equal(provisioningv1.ReasonFactoryResetCredentialRecordFailed))
				secretAttempts := mockServer.CredentialPasswordAttempts()
				defaultAttempts := mockServer.DefaultPasswordAttempts()
				for i := 0; i < 3; i++ {
					Expect(reconciler.Client.Status().Patch(ctx, device, statusPatch)).To(Succeed())
					stored := &provisioningv1.DPUDevice{}
					Expect(reconciler.Client.Get(ctx, client.ObjectKeyFromObject(device), stored)).To(Succeed())
					device = stored
					statusPatch = client.MergeFrom(device.DeepCopy())
					_, err = reconciler.reconcileBMCFactoryReset(ctx, device)
					Expect(err).NotTo(HaveOccurred())
				}
				Expect(mockServer.CredentialPasswordAttempts()).To(Equal(secretAttempts))
				Expect(mockServer.DefaultPasswordAttempts()).To(Equal(defaultAttempts))
				Expect(resetCondition(device).Reason).To(Equal(provisioningv1.ReasonFactoryResetCredentialRecordFailed))

				failWrite = false
				requestFactoryResetRetry(reconciler, device)
				mockServer.SetPassword(rfclient.BMCDefaultPassword)
				stop, err := reconciler.reconcileBMCFactoryReset(ctx, device)
				Expect(err).NotTo(HaveOccurred())
				Expect(stop).To(BeFalse())
				Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
			},
			Entry("both credentials rejected", factoryResetTestPassword, false, ""),
			Entry("Secret rejected during a delayed reset", factoryResetTestPassword, true, ""),
			Entry("Secret is the rejected factory default", rfclient.BMCDefaultPassword, false, ""),
			Entry("explicit retry also rejected", factoryResetTestPassword, false, provisioningv1.BMCFactoryResetRetryAnnotationValue),
			Entry("changed password also rejected", factoryResetTestPassword, false, passwordDigest("previouslyRejected")),
		)

		It("does not authenticate when an explicit retry cannot be consumed", func() {
			mockServer, reconciler := setupFactoryResetTest("someoneElsesPassword", factoryResetTestPassword, interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if secret, ok := obj.(*corev1.Secret); ok && len(secret.Annotations) == 0 {
						return errors.New("cannot consume retry")
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			})
			defer mockServer.Stop()
			device := newDevice(reconciler, mockServer, "reset-retry-write-failed")
			device.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))
			setBMCFactoryResetCondition(device, metav1.ConditionFalse, provisioningv1.ReasonFactoryResetFailed, "previous rejection")
			requestFactoryResetRetry(reconciler, device)

			for i := 0; i < 2; i++ {
				stop, err := reconciler.reconcileBMCFactoryReset(ctx, device)
				Expect(err).To(MatchError(ContainSubstring("cannot consume retry")))
				Expect(stop).To(BeTrue())
			}
			Expect(mockServer.DefaultPasswordAttempts()).To(BeZero())
			Expect(mockServer.CredentialPasswordAttempts()).To(BeZero())
		})

		It("keeps the annotation of the password the BMC rejected when the Secret changes before failure is recorded", func() {
			// An edit after the rejected probe must not become the stored annotation. The next
			// pass would then see a match and stop, leaving the corrected password untested.
			const corrected = "correctedSecretPassword"
			secretReads := 0
			replaceAfterRead := interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					secret, ok := obj.(*corev1.Secret)
					if !ok || secret.Name != rfclient.BMCPasswordSecret {
						return nil
					}
					secretReads++
					if secretReads != 1 {
						return nil
					}
					updated := secret.DeepCopy()
					updated.Data[rfclient.BMCSharedPasswordKey] = []byte(corrected)
					return c.Patch(ctx, updated, client.MergeFrom(secret))
				},
			}
			mockServer, reconciler := setupFactoryResetTest("someoneElsesPassword", factoryResetTestPassword, replaceAfterRead)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-secret-changed-during-failure")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(rejectedPasswordAnnotation(reconciler, dpuDevice)).To(Equal(passwordDigest(factoryResetTestPassword)))
			rejectedSecretAttempts := mockServer.CredentialPasswordAttempts()

			mockServer.SetPassword(corrected)
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetAwaitingCompletion))
			Expect(mockServer.CredentialPasswordAttempts()).To(BeNumerically(">", rejectedSecretAttempts))
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
		})

		It("retries completion probes after FactoryResetFailed when the Secret password changes", func() {
			mockServer, reconciler := setupFactoryResetTest("someoneElsesPassword", factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-retry-on-secret-change")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			rejectedDefault := mockServer.DefaultPasswordAttempts()
			rejectedSecret := mockServer.CredentialPasswordAttempts()

			// A metadata-only Secret write changes resourceVersion but not the password.
			secret := &corev1.Secret{}
			Expect(reconciler.Client.Get(ctx, types.NamespacedName{
				Name:      rfclient.BMCPasswordSecret,
				Namespace: testNamespace,
			}, secret)).To(Succeed())
			patch := client.MergeFrom(secret.DeepCopy())
			secret.Labels = map[string]string{"tick": "1"}
			Expect(reconciler.Client.Patch(ctx, secret, patch)).To(Succeed())
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			Expect(mockServer.DefaultPasswordAttempts()).To(Equal(rejectedDefault))
			Expect(mockServer.CredentialPasswordAttempts()).To(Equal(rejectedSecret))

			updateSecretPassword(reconciler, "correctedSecretPassword")
			mockServer.SetPassword(rfclient.BMCDefaultPassword)
			stop, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted))
			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).NotTo(BeNil())
			Expect(rejectedPasswordAnnotation(reconciler, dpuDevice)).To(BeEmpty())
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
			Expect(mockServer.CredentialPasswordAttempts()).To(Equal(rejectedSecret))
		})

		It("uses a corrected Secret password after FactoryResetFailed without resubmitting the reset", func() {
			mockServer, reconciler := setupFactoryResetTest("someoneElsesPassword", factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-retry-corrected-secret")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))

			updateSecretPassword(reconciler, "someoneElsesPassword")
			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetAwaitingCompletion))
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
		})

		It("retries the factory default after FactoryResetFailed when the annotation requests a retry", func() {
			mockServer, reconciler := setupFactoryResetTest("someoneElsesPassword", factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-retry-cleared-annotation")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetFailed))
			rejectedSecret := mockServer.CredentialPasswordAttempts()

			mockServer.SetPassword(rfclient.BMCDefaultPassword)
			requestFactoryResetRetry(reconciler, dpuDevice)
			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted))
			Expect(mockServer.CredentialPasswordAttempts()).To(Equal(rejectedSecret))
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
		})

		It("does not send a rejected Secret password restored between the change check and the probe", func() {
			// FactoryResetCredentialRejected reads the Secret to decide the password changed, then
			// the probe reads it again before sending. A rollback between those reads puts the
			// rejected password back; sending it is another failed login.
			const corrected = "correctedSecretPassword"
			restoreRejected := false
			secretReads := 0
			rollbackAfterCheck := interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					if !restoreRejected {
						return nil
					}
					secret, ok := obj.(*corev1.Secret)
					if !ok || secret.Name != rfclient.BMCPasswordSecret {
						return nil
					}
					secretReads++
					if secretReads != 1 {
						return nil
					}
					restored := secret.DeepCopy()
					restored.Data[rfclient.BMCSharedPasswordKey] = []byte(factoryResetTestPassword)
					return c.Patch(ctx, restored, client.MergeFrom(secret))
				},
			}
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword, rollbackAfterCheck)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-secret-rolled-back-before-send")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetAwaitingCompletion))

			mockServer.SetPassword("someoneElsesPassword")
			mockServer.SetFactoryDefaultProbeUnavailable(true)
			_, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCredentialRejected))
			rejected := rejectedPasswordAnnotation(reconciler, dpuDevice)
			Expect(rejected).To(Equal(passwordDigest(factoryResetTestPassword)))
			rejectedSecret := mockServer.CredentialPasswordAttempts()

			updateSecretPassword(reconciler, corrected)
			restoreRejected = true
			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCredentialRejected))
			Expect(mockServer.CredentialPasswordAttempts()).To(Equal(rejectedSecret))
			Expect(rejectedPasswordAnnotation(reconciler, dpuDevice)).To(Equal(rejected))
			Expect(secretReads).To(BeNumerically(">=", 2))
		})

		It("retries the Secret password from FactoryResetCredentialRejected when it changes", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-secret-changed-after-reject")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetAwaitingCompletion))

			mockServer.SetPassword("someoneElsesPassword")
			mockServer.SetFactoryDefaultProbeUnavailable(true)
			_, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCredentialRejected))
			Expect(rejectedPasswordAnnotation(reconciler, dpuDevice)).To(Equal(passwordDigest(factoryResetTestPassword)))
			rejectedSecret := mockServer.CredentialPasswordAttempts()

			_, err = reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockServer.CredentialPasswordAttempts()).To(Equal(rejectedSecret))

			updateSecretPassword(reconciler, "someoneElsesPassword")
			mockServer.SetFactoryDefaultProbeUnavailable(false)
			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetAwaitingCompletion))
			Expect(mockServer.CredentialPasswordAttempts()).To(BeNumerically(">", rejectedSecret))
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
		})

		It("completes when the factory default is active but PasswordChangeRequired blocks Managers", func() {
			// Real BlueField BMCs after ResetToDefaults accept 0penBmc on /redfish/v1 but return
			// 403 PasswordChangeRequired on Managers until the password is hardened. That must
			// count as the reset having taken effect, or await hangs forever.
			mockServer, reconciler := setupFactoryResetTest(rfclient.BMCDefaultPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			mockServer.SetPasswordChangeRequired(true)
			dpuDevice := newDevice(reconciler, mockServer, "reset-password-change-required")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())

			condition := resetCondition(dpuDevice)
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted))
		})

		It("deletes the stale server CertificateRequest on completion", func() {
			mockServer, reconciler := setupFactoryResetTest(rfclient.BMCDefaultPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-stale-cr")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			cr := &unstructured.Unstructured{}
			cr.SetGroupVersionKind(crGVK())
			cr.SetName(cutil.GenerateBMCServerCertRequestName(dpuDevice.Name))
			cr.SetNamespace(dpuDevice.Namespace)
			Expect(reconciler.Client.Create(ctx, cr)).To(Succeed())

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			fetched := &unstructured.Unstructured{}
			fetched.SetGroupVersionKind(crGVK())
			err = reconciler.Client.Get(ctx, types.NamespacedName{
				Name: cutil.GenerateBMCServerCertRequestName(dpuDevice.Name), Namespace: dpuDevice.Namespace,
			}, fetched)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("waits indefinitely and never resubmits when the BMC does not come back", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-never-back")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-30 * time.Minute)))
			// The BMC is unreachable: stop the server so every probe fails.
			mockServer.Stop()

			for i := 0; i < 3; i++ {
				stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
				Expect(err).NotTo(HaveOccurred())
				Expect(stop).To(BeTrue())
			}

			condition := resetCondition(dpuDevice)
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetInProgress))
			Expect(condition.Message).To(ContainSubstring("elapsed since the request"))
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
		})

		It("measures the settle delay from the persisted request time after a restart", func() {
			mockServer, reconciler := setupFactoryResetTest(rfclient.BMCDefaultPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			// A fresh reconciler with only the persisted status is what a restarted controller sees.
			dpuDevice := newDevice(reconciler, mockServer, "reset-restart")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))
			dpuDevice.Status.Conditions = []metav1.Condition{{
				Type:               string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady),
				Status:             metav1.ConditionFalse,
				Reason:             provisioningv1.ReasonFactoryResetInProgress,
				LastTransitionTime: metav1.Now(),
			}}

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
			Expect(resetCondition(dpuDevice).Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted))
		})
	})

	Context("policy", func() {
		It("skips the reset entirely when the policy is Never", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-policy-never")
			dpuDevice.Spec.BMCFactoryResetPolicy = provisioningv1.BMCFactoryResetPolicyNever

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())

			// Not even an authentication attempt: the policy is read before any BMC call.
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
			Expect(mockServer.GetAccountPatches()).To(BeEmpty())
			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).To(BeNil())

			condition := resetCondition(dpuDevice)
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetSkipped))
		})

		It("treats an unset policy as OnInitialization", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-policy-unset")
			Expect(dpuDevice.Spec.BMCFactoryResetPolicy).To(BeEmpty())

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockServer.GetFactoryResetRequests()).To(Equal(1))
		})

		It("never re-enters a finished reset, including after the policy is flipped", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-finished")
			dpuDevice.Spec.BMCFactoryResetPolicy = provisioningv1.BMCFactoryResetPolicyNever

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())

			dpuDevice.Spec.BMCFactoryResetPolicy = provisioningv1.BMCFactoryResetPolicyOnInitialization
			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
		})

		It("refreshes a stale generation in place, keeping the completion time", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-stale-generation")
			dpuDevice.Spec.BMCFactoryResetPolicy = provisioningv1.BMCFactoryResetPolicyNever

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			completedAt := resetCondition(dpuDevice).LastTransitionTime

			// An edit while the device is still initializing bumps the generation, which would make
			// conditions.IsTrue read the finished reset as unfinished.
			dpuDevice.Generation = 2
			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())
			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())

			condition := resetCondition(dpuDevice)
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.ObservedGeneration).To(Equal(int64(2)))
			Expect(condition.LastTransitionTime).To(Equal(completedAt))
		})
	})

	Context("upgrade safety", func() {
		DescribeTable("backfills devices DPF already manages",
			func(mutate func(*provisioningv1.DPUDevice)) {
				mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
				defer mockServer.Stop()
				dpuDevice := newDevice(reconciler, mockServer, "reset-backfill")
				mutate(dpuDevice)

				stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
				Expect(err).NotTo(HaveOccurred())
				Expect(stop).To(BeFalse())

				Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
				Expect(dpuDevice.Status.BMCFactoryResetRequestTime).To(BeNil())
				condition := resetCondition(dpuDevice)
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetSkipped))
				Expect(condition.Message).To(ContainSubstring("predates"))
			},
			Entry("status.bmcCredentialSecretName is set", func(d *provisioningv1.DPUDevice) {
				d.Status.BMCCredentialSecretName = ptr.To(rfclient.BMCPasswordSecret)
			}),
			Entry("Discovered is True", func(d *provisioningv1.DPUDevice) {
				d.Status.Conditions = []metav1.Condition{{
					Type:               string(provisioningv1.ConditionDpuDeviceDiscovered),
					Status:             metav1.ConditionTrue,
					Reason:             "Success",
					LastTransitionTime: metav1.Now(),
				}}
			}),
			Entry("status.dpuType is set", func(d *provisioningv1.DPUDevice) {
				d.Status.DPUType = provisioningv1.DPUTypeBlueField3
			}),
		)

		It("does not skip a freshly created device", func() {
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-fresh-device")
			// A brand new DPUDevice reports DPUType Unknown from the CRD default and carries none
			// of the other markers.
			dpuDevice.Status.DPUType = provisioningv1.DPUTypeUnknown

			_, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockServer.GetFactoryResetRequests()).To(Equal(1))
		})
	})
})

var _ = Describe("BMC factory reset with a non-Redfish install interface", func() {
	const deviceName = "reset-host-agent"

	// reconcileUnderHostAgent runs a full reconcile with installViaHostAgent, which returns before
	// initializeDPUDevice and so never reaches reconcileBMCFactoryReset.
	reconcileUnderHostAgent := func(dpuDevice *provisioningv1.DPUDevice) {
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		Expect(operatorv1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(
				dpuDevice,
				&operatorv1.DPFOperatorConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpfoperatorconfig",
						Namespace: testNamespace,
					},
					Spec: operatorv1.DPFOperatorConfigSpec{
						ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
							InstallInterface: &operatorv1.ProvisioningInstallInterface{
								InstallViaHostAgent: &operatorv1.InstallViaHostAgent{},
							},
						},
					},
				},
				&provisioningv1.DPUNode{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "reset-host-agent-node",
						Namespace: testNamespace,
					},
					Spec: provisioningv1.DPUNodeSpec{
						DPUs: []provisioningv1.DPURef{
							{Name: deviceName},
						},
					},
				},
			).
			WithStatusSubresource(dpuDevice).
			Build()

		_, err := (&DPUDeviceReconciler{Client: fakeClient}).reconcile(context.Background(), dpuDevice)
		Expect(err).NotTo(HaveOccurred())
	}

	newDevice := func() *provisioningv1.DPUDevice {
		return &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{
				Name:       deviceName,
				Namespace:  testNamespace,
				Generation: 1,
			},
			Spec: provisioningv1.DPUDeviceSpec{
				SerialNumber:          mock.DpuSerialNumber,
				BMCFactoryResetPolicy: provisioningv1.BMCFactoryResetPolicyOnInitialization,
			},
		}
	}

	It("reports the reset as skipped even though the policy is OnInitialization", func() {
		dpuDevice := newDevice()

		reconcileUnderHostAgent(dpuDevice)

		Expect(dpuDevice.Status.BMCFactoryResetRequestTime).To(BeNil())
		condition := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady))
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetSkipped))
		Expect(condition.Message).To(ContainSubstring("install interface is not Redfish"))
	})

	It("keeps a reset that already finished", func() {
		dpuDevice := newDevice()
		dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-time.Hour)))
		dpuDevice.Status.Conditions = []metav1.Condition{{
			Type:               string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady),
			Status:             metav1.ConditionTrue,
			Reason:             provisioningv1.ReasonFactoryResetCompleted,
			Message:            "BMC was reset to factory defaults and is reachable again",
			ObservedGeneration: 1,
			LastTransitionTime: metav1.Now(),
		}}

		reconcileUnderHostAgent(dpuDevice)

		condition := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady))
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted))
	})

	It("keeps a reset that is still in progress", func() {
		dpuDevice := newDevice()
		requestTime := metav1.NewTime(time.Now().Add(-time.Minute))
		dpuDevice.Status.BMCFactoryResetRequestTime = &requestTime
		dpuDevice.Status.Conditions = []metav1.Condition{{
			Type:               string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady),
			Status:             metav1.ConditionFalse,
			Reason:             provisioningv1.ReasonFactoryResetInProgress,
			Message:            "ResetToDefaults accepted by the BMC; waiting for it to come back",
			ObservedGeneration: 1,
			LastTransitionTime: metav1.Now(),
		}}

		reconcileUnderHostAgent(dpuDevice)

		// A True skipped condition here would stop a later Redfish pass from ever checking whether
		// the submitted reset landed.
		Expect(dpuDevice.Status.BMCFactoryResetRequestTime).NotTo(BeNil())
		Expect(dpuDevice.Status.BMCFactoryResetRequestTime.Time).To(BeTemporally("==", requestTime.Rfc3339Copy().Time))
		condition := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady))
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetInProgress))
	})

	// A device onboarded through another install interface is already provisioned by DPF, so
	// switching it to Redfish must not wipe its BMC. The Discovered=True that the non-Redfish path
	// records is what the upgrade-safety marker reads, independently of BMCFactoryResetReady.
	DescribeTable("does not reset the BMC after the install interface switches to Redfish",
		func(dropSkippedCondition bool, expectedMessage string) {
			dpuDevice := newDevice()
			reconcileUnderHostAgent(dpuDevice)
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceDiscovered)).Status).To(Equal(metav1.ConditionTrue))
			if dropSkippedCondition {
				conditionsList := dpuDevice.GetConditions()
				meta.RemoveStatusCondition(&conditionsList, string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady))
				dpuDevice.SetConditions(conditionsList)
			}

			mockServer, err := mock.CreateMockRedfishServer("BF-24.10", factoryResetTestPassword)
			Expect(err).NotTo(HaveOccurred())
			defer mockServer.Stop()
			mockServer.SetEnforceBasicAuth(true)
			bmcIP := mockServer.GetIPAddress()
			bmcPort := uint32(mockServer.GetPort())
			dpuDevice.Status.BMCIP = &bmcIP
			dpuDevice.Status.BMCPort = &bmcPort

			scheme := runtime.NewScheme()
			Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			// The credential Secret lets the Redfish pass submit a reset, so only the skip logic
			// stands between this device and a wiped BMC.
			redfishReconciler := &DPUDeviceReconciler{Client: fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&provisioningv1.DPUDevice{}).
				WithObjects(&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rfclient.BMCPasswordSecret,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{
						rfclient.BMCSharedPasswordKey: []byte(factoryResetTestPassword),
					},
				}).
				Build()}

			stop, err := redfishReconciler.reconcileBMCFactoryReset(context.Background(), dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())

			Expect(mockServer.GetFactoryResetRequests()).To(BeZero())
			Expect(dpuDevice.Status.BMCFactoryResetRequestTime).To(BeNil())
			condition := findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady))
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetSkipped))
			Expect(condition.Message).To(ContainSubstring(expectedMessage))
		},
		Entry("with the skipped condition the non-Redfish path recorded", false, "install interface is not Redfish"),
		Entry("without it, as before BMCFactoryResetReady was set off Redfish", true, "Discovered is True"),
	)
})
