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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const factoryResetTestPassword = "dpfSecretPassword1"

var _ = Describe("reconcileBMCFactoryReset", func() {
	var ctx context.Context

	BeforeEach(func() { ctx = context.Background() })

	// setupFactoryResetTest returns a BMC that enforces authentication and a reconciler holding the
	// shared credential Secret, which is what the reset step authenticates with.
	setupFactoryResetTest := func(bmcPassword, secretPassword string) (*mock.RedfishMockServer, *DPUDeviceReconciler) {
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		mockServer, err := mock.CreateMockRedfishServer("BF-24.10", bmcPassword)
		Expect(err).NotTo(HaveOccurred())
		mockServer.SetEnforceBasicAuth(true)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&provisioningv1.DPUDevice{}).
			WithObjects(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: rfclient.BMCPasswordSecret, Namespace: testNamespace},
				Data:       map[string][]byte{rfclient.BMCSharedPasswordKey: []byte(secretPassword)},
			}).
			Build()

		return mockServer, &DPUDeviceReconciler{Client: fakeClient}
	}

	// newDevice also creates the DPUDevice on the API server, because the reset step records its
	// request time there itself rather than through the patch the reconcile defers to its end.
	newDevice := func(reconciler *DPUDeviceReconciler, mockServer *mock.RedfishMockServer, name string) *provisioningv1.DPUDevice {
		bmcIP := mockServer.GetIPAddress()
		bmcPort := uint32(mockServer.GetPort())
		dpuDevice := &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Generation: 1},
			Spec:       provisioningv1.DPUDeviceSpec{SerialNumber: mock.DpuSerialNumber},
		}
		Expect(reconciler.Client.Create(ctx, dpuDevice)).To(Succeed())
		dpuDevice.Status = provisioningv1.DPUDeviceStatus{BMCIP: &bmcIP, BMCPort: &bmcPort}
		return dpuDevice
	}

	// persistedRequestTime reads status.bmcFactoryResetRequestTime back from the API server.
	persistedRequestTime := func(reconciler *DPUDeviceReconciler, dpuDevice *provisioningv1.DPUDevice) *metav1.Time {
		stored := &provisioningv1.DPUDevice{}
		Expect(reconciler.Client.Get(ctx, client.ObjectKeyFromObject(dpuDevice), stored)).To(Succeed())
		return stored.Status.BMCFactoryResetRequestTime
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
			Expect(findCondition(dpuDevice, string(provisioningv1.ConditionDpuDeviceInitialized)).Reason).To(Equal("Pending"))
		})

		It("completes once the BMC answers with the factory default password", func() {
			mockServer, reconciler := setupFactoryResetTest(rfclient.BMCDefaultPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-completed")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeFalse())

			condition := resetCondition(dpuDevice)
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetCompleted))
		})

		It("keeps FactoryResetInProgress with an actionable message when the BMC rejects the factory default", func() {
			// After submit the BMC should still hold the Secret password until ResetToDefaults
			// actually lands. If it answers and rejects 0penBmc, the BMC is up but the reset has
			// not taken effect — not "still coming back".
			mockServer, reconciler := setupFactoryResetTest(factoryResetTestPassword, factoryResetTestPassword)
			defer mockServer.Stop()
			dpuDevice := newDevice(reconciler, mockServer, "reset-default-rejected")
			dpuDevice.Status.BMCFactoryResetRequestTime = ptr.To(metav1.NewTime(time.Now().Add(-5 * time.Minute)))

			stop, err := reconciler.reconcileBMCFactoryReset(ctx, dpuDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(stop).To(BeTrue())

			condition := resetCondition(dpuDevice)
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(provisioningv1.ReasonFactoryResetInProgress))
			Expect(condition.Message).To(ContainSubstring("still rejects the factory default password"))
			Expect(condition.Message).NotTo(ContainSubstring("waiting for the BMC to come back"))
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
