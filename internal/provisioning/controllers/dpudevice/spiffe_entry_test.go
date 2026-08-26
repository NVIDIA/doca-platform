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
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	"github.com/nvidia/doca-platform/pkg/conditions"
	spirev1alpha1 "github.com/nvidia/doca-platform/third_party/forked/github.com/spiffe/spire-controller-manager/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	testTrustDomain = "cs.internal"
	testSerial      = "MT2440600YYW"
	testCSEName     = "dpu-agent-mt2440600yyw"
	testClassName   = "spire-mgmt-spire"
	testDeviceName  = "dev-1"
)

func spiffeScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	Expect(operatorv1.AddToScheme(scheme)).To(Succeed())
	Expect(spirev1alpha1.AddToScheme(scheme)).To(Succeed())
	return scheme
}

func spiffeConfig(enabled bool) *operatorv1.DPFOperatorConfig {
	cfg := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "dpfoperatorconfig", Namespace: "dpf-operator-system"},
	}
	if enabled {
		cfg.Spec.Security = &operatorv1.SecurityConfiguration{
			SPIFFE: &operatorv1.SPIFFEConfiguration{
				SPIREServerAddress:                "spire-server.spire-system.svc:8081",
				SPIRETrustDomain:                  testTrustDomain,
				DPUAgentSPIFFEIDTemplate:          "spiffe://{{ .TrustDomain }}/tenant/dummy-operator/service/dsx/dpu/{{ .SerialNumber }}/process/dpu-agent",
				DPUAgentExchangedSPIFFEIDTemplate: "spiffe://dummy-operator.example.test/dpu/{{ .SerialNumber }}/process/dpu-agent",
				KubeAPIAudience:                   "dpf",
				SPIREOIDCURL:                      "https://spire.example.com",
				SPIREControllerManagerClassName:   testClassName,
			},
		}
	}
	return cfg
}

func spiffeDPUDevice() *provisioningv1.DPUDevice {
	return &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{Name: testDeviceName, Namespace: "default"},
		Spec:       provisioningv1.DPUDeviceSpec{SerialNumber: testSerial},
	}
}

func dpuBoundTo(mode *provisioningv1.IdentityMode) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "default"},
		Spec:       provisioningv1.DPUSpec{DPUDeviceName: testDeviceName},
		Status:     provisioningv1.DPUStatus{IdentityMode: mode},
	}
}

func getCSE(ctx context.Context, c client.Client) (*spirev1alpha1.ClusterStaticEntry, error) {
	cse := &spirev1alpha1.ClusterStaticEntry{}
	err := c.Get(ctx, types.NamespacedName{Name: testCSEName}, cse)
	return cse, err
}

var _ = Describe("SPIFFE ClusterStaticEntry reconcile", func() {
	var (
		ctx        context.Context
		reconciler *DPUDeviceReconciler
		recorder   *record.FakeRecorder
		dpuDevice  *provisioningv1.DPUDevice
	)

	build := func(objs ...client.Object) {
		recorder = record.NewFakeRecorder(20)
		fakeClient := fake.NewClientBuilder().
			WithScheme(spiffeScheme()).
			WithIndex(&provisioningv1.DPU{}, dpuByDPUDeviceNameField, indexDPUByDPUDeviceName).
			WithObjects(objs...).
			Build()
		reconciler = &DPUDeviceReconciler{Client: fakeClient, Recorder: recorder}
	}

	BeforeEach(func() {
		ctx = context.Background()
		dpuDevice = spiffeDPUDevice()
	})

	It("is a no-op when SPIFFE is disabled cluster-wide", func() {
		cfg := spiffeConfig(false)
		build(cfg, dpuBoundTo(ptr.To(provisioningv1.IdentityModeSpiffe)))

		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())
		Expect(conditions.Get(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady)).To(BeNil())
		Expect(dpuDevice.Finalizers).NotTo(ContainElement(provisioningv1.SPIFFEDeregistrationFinalizer))
		_, err := getCSE(ctx, reconciler.Client)
		Expect(err).To(HaveOccurred())
	})

	It("is a no-op when the bound DPU uses bootstrap-token identity", func() {
		cfg := spiffeConfig(true)
		build(cfg, dpuBoundTo(ptr.To(provisioningv1.IdentityModeBootstrapToken)))

		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())
		Expect(conditions.Get(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady)).To(BeNil())
		_, err := getCSE(ctx, reconciler.Client)
		Expect(err).To(HaveOccurred())
	})

	It("selects deterministically and warns when multiple SPIFFE DPUs bind one device", func() {
		spiffe := ptr.To(provisioningv1.IdentityModeSpiffe)
		newSpiffeDPU := func(name string) *provisioningv1.DPU {
			return &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec:       provisioningv1.DPUSpec{DPUDeviceName: testDeviceName},
				Status:     provisioningv1.DPUStatus{IdentityMode: spiffe},
			}
		}
		// Seed in non-sorted order to prove selection does not depend on list/insertion order.
		build(newSpiffeDPU("dpu-b"), newSpiffeDPU("dpu-a"))

		got, err := reconciler.findOwningSpiffeDPU(ctx, dpuDevice)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.Name).To(Equal("dpu-a"), "the lexicographically-first DPU must be selected deterministically")

		Expect(recorder.Events).To(Receive(ContainSubstring(events.EventSPIFFEDuplicateDPUReason)))
	})

	It("does not warn when exactly one SPIFFE DPU binds the device", func() {
		build(dpuBoundTo(ptr.To(provisioningv1.IdentityModeSpiffe)))

		got, err := reconciler.findOwningSpiffeDPU(ctx, dpuDevice)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(recorder.Events).NotTo(Receive(), "single match must not emit a duplicate warning")
	})

	It("persists the finalizer before the ClusterStaticEntry, then creates it and reports Pending", func() {
		cfg := spiffeConfig(true)
		build(cfg, dpuBoundTo(ptr.To(provisioningv1.IdentityModeSpiffe)), dpuDevice)

		// First pass: the finalizer is added but the CSE is NOT created yet -- the finalizer must be
		// durable before any external SPIRE identity exists, otherwise a delete could leak it.
		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())
		Expect(dpuDevice.Finalizers).To(ContainElement(provisioningv1.SPIFFEDeregistrationFinalizer))
		_, err := getCSE(ctx, reconciler.Client)
		Expect(err).To(HaveOccurred())
		Expect(conditions.Get(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady)).To(BeNil())

		// Persist the finalizer (the controller's deferred patcher does this in production) and confirm
		// it is durable by re-reading the stored DPUDevice from the API.
		Expect(reconciler.Client.Update(ctx, dpuDevice)).To(Succeed())
		stored := &provisioningv1.DPUDevice{}
		Expect(reconciler.Client.Get(ctx, types.NamespacedName{Name: testDeviceName, Namespace: "default"}, stored)).To(Succeed())
		Expect(stored.Finalizers).To(ContainElement(provisioningv1.SPIFFEDeregistrationFinalizer))

		// Second pass (finalizer now present): the CSE is created and status is mirrored as Pending.
		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())

		cse, err := getCSE(ctx, reconciler.Client)
		Expect(err).NotTo(HaveOccurred())
		spiffeID, parentID, className := cse.Spec.SPIFFEID, cse.Spec.ParentID, cse.Spec.ClassName
		Expect(spiffeID).To(Equal("spiffe://cs.internal/tenant/dummy-operator/service/dsx/dpu/mt2440600yyw/process/dpu-agent"))
		Expect(parentID).To(Equal("spiffe://cs.internal/spire/agent/dpu_hw/mt2440600yyw"))
		Expect(className).To(Equal(testClassName))
		Expect(cse.GetLabels()).To(HaveKeyWithValue(LabelDPUDeviceName, "dev-1"))

		cond := conditions.Get(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(conditions.ReasonPending)))
	})

	It("reports Error (no requeue) for an unrepresentable serial", func() {
		dpuDevice.Spec.SerialNumber = "MT/24:40" // not DNS-1123 / not in the serial charset
		cfg := spiffeConfig(true)
		build(cfg, dpuBoundTo(ptr.To(provisioningv1.IdentityModeSpiffe)))

		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())

		cond := conditions.Get(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady)
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(conditions.ReasonError)))
		// No CR is created for an invalid serial.
		_, err := getCSE(ctx, reconciler.Client)
		Expect(err).To(HaveOccurred())
	})

	It("reports Error (no requeue) for a serial over the identity length limit", func() {
		dpuDevice.Spec.SerialNumber = strings.Repeat("a", maxClusterStaticEntrySerialLen+1)
		cfg := spiffeConfig(true)
		build(cfg, dpuBoundTo(ptr.To(provisioningv1.IdentityModeSpiffe)))

		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())

		cond := conditions.Get(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady)
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(conditions.ReasonError)))
		Expect(cond.Message).To(ContainSubstring("too long for DPU Agent identity"))
	})

	It("returns an actionable error for invalid identity template configuration", func() {
		cfg := spiffeConfig(true)
		cfg.Spec.Security.SPIFFE.DPUAgentSPIFFEIDTemplate = "spiffe://{{ .TrustDomain }}/dpu/static"
		build(cfg, dpuBoundTo(ptr.To(provisioningv1.IdentityModeSpiffe)))

		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(
			MatchError(ContainSubstring("invalid DPU Agent identity template configuration")))

		cond := conditions.Get(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady)
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(conditions.ReasonError)))
		Expect(cond.Message).To(ContainSubstring("identity template configuration"))
		Expect(recorder.Events).To(Receive(ContainSubstring(events.EventSPIFFEEntryRegistrationFailedReason)))
		_, err := getCSE(ctx, reconciler.Client)
		Expect(err).To(HaveOccurred())
	})

	It("reclaims out-of-band spec edits and emits a drift event", func() {
		cfg := spiffeConfig(true)
		build(cfg, dpuBoundTo(ptr.To(provisioningv1.IdentityModeSpiffe)))
		// First pass only adds the finalizer; the second pass creates the CSE.
		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())
		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())
		// drain the create event
		Eventually(recorder.Events).Should(Receive())

		// External actor mutates the spec.
		cse, err := getCSE(ctx, reconciler.Client)
		Expect(err).NotTo(HaveOccurred())
		cse.Spec.SPIFFEID = "spiffe://evil/x"
		Expect(reconciler.Client.Update(ctx, cse)).To(Succeed())

		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())

		cse, err = getCSE(ctx, reconciler.Client)
		Expect(err).NotTo(HaveOccurred())
		spiffeID := cse.Spec.SPIFFEID
		Expect(spiffeID).To(Equal("spiffe://cs.internal/tenant/dummy-operator/service/dsx/dpu/mt2440600yyw/process/dpu-agent"))
		Expect(recorder.Events).To(Receive(ContainSubstring("Drift")))
	})

	It("updates the entry when the trust domain changes", func() {
		cfg := spiffeConfig(true)
		dpuDevice.Finalizers = []string{provisioningv1.SPIFFEDeregistrationFinalizer}
		build(cfg, dpuBoundTo(ptr.To(provisioningv1.IdentityModeSpiffe)))

		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())
		cfg.Spec.Security.SPIFFE.SPIRETrustDomain = "updated.internal"
		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, cfg)).To(Succeed())

		cse, err := getCSE(ctx, reconciler.Client)
		Expect(err).NotTo(HaveOccurred())
		spiffeID := cse.Spec.SPIFFEID
		Expect(spiffeID).To(Equal("spiffe://updated.internal/tenant/dummy-operator/service/dsx/dpu/mt2440600yyw/process/dpu-agent"))
	})

	It("reports an actionable error when entry creation is forbidden", func() {
		dpuDevice.Finalizers = []string{provisioningv1.SPIFFEDeregistrationFinalizer}
		recorder = record.NewFakeRecorder(20)
		fakeClient := fake.NewClientBuilder().
			WithScheme(spiffeScheme()).
			WithIndex(&provisioningv1.DPU{}, dpuByDPUDeviceNameField, indexDPUByDPUDeviceName).
			WithObjects(spiffeConfig(true), dpuBoundTo(ptr.To(provisioningv1.IdentityModeSpiffe))).
			WithInterceptorFuncs(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
					// Match on the Go type: a typed object does not carry a populated TypeMeta,
					// so its GroupVersionKind is empty here.
					if _, ok := obj.(*spirev1alpha1.ClusterStaticEntry); ok {
						return apierrors.NewForbidden(schema.GroupResource{Group: clusterStaticEntryGVK.Group, Resource: "clusterstaticentries"}, obj.GetName(), nil)
					}
					return nil
				},
			}).
			Build()
		reconciler = &DPUDeviceReconciler{Client: fakeClient, Recorder: recorder}

		Expect(reconciler.reconcileSPIFFEEntry(ctx, dpuDevice, spiffeConfig(true))).To(MatchError(ContainSubstring("failed to apply ClusterStaticEntry")))
		cond := conditions.Get(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady)
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(conditions.ReasonError)))
		Expect(recorder.Events).To(Receive(ContainSubstring(events.EventSPIFFEEntryRegistrationFailedReason)))
	})
})

var _ = Describe("mirrorSpiffeEntryStatus", func() {
	var dpuDevice *provisioningv1.DPUDevice

	BeforeEach(func() { dpuDevice = spiffeDPUDevice() })

	cseWithStatus := func(fields map[string]bool) *spirev1alpha1.ClusterStaticEntry {
		cse := &spirev1alpha1.ClusterStaticEntry{}
		cse.SetName(testCSEName)
		cse.Status = spirev1alpha1.ClusterStaticEntryStatus{
			Set:      fields["set"],
			Rendered: fields["rendered"],
			Masked:   fields["masked"],
		}
		return cse
	}

	expectCondition := func(status metav1.ConditionStatus, reason conditions.ConditionReason, wantMsgSubstr ...string) {
		cond := conditions.Get(dpuDevice, provisioningv1.ConditionSPIFFEEntryReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(status))
		Expect(cond.Reason).To(Equal(string(reason)))
		for _, substr := range wantMsgSubstr {
			Expect(cond.Message).To(ContainSubstring(substr))
		}
	}

	It("maps Set && Rendered && !Masked to Ready=True", func() {
		masked := (&DPUDeviceReconciler{}).mirrorSpiffeEntryStatus(dpuDevice, cseWithStatus(map[string]bool{"set": true, "rendered": true}))
		Expect(masked).To(BeFalse())
		expectCondition(metav1.ConditionTrue, conditions.ReasonSuccess)
	})

	// Regression canary: `set` is not consulted once `rendered=true` (see mirrorSpiffeEntryStatus's
	// `renderedFound && rendered` case). If that condition is ever tightened to `set && rendered`,
	// this is the test that must start failing.
	It("maps Rendered=true and Set=false to Ready=True", func() {
		masked := (&DPUDeviceReconciler{}).mirrorSpiffeEntryStatus(dpuDevice, cseWithStatus(map[string]bool{"set": false, "rendered": true}))
		Expect(masked).To(BeFalse())
		expectCondition(metav1.ConditionTrue, conditions.ReasonSuccess)
	})

	It("maps an absent status (not yet observed) to False/Pending", func() {
		masked := (&DPUDeviceReconciler{}).mirrorSpiffeEntryStatus(dpuDevice, cseWithStatus(nil))
		Expect(masked).To(BeFalse())
		expectCondition(metav1.ConditionFalse, conditions.ReasonPending, "Awaiting spire-controller-manager")
	})

	It("maps Set && !Rendered to False/Pending", func() {
		masked := (&DPUDeviceReconciler{}).mirrorSpiffeEntryStatus(dpuDevice, cseWithStatus(map[string]bool{"set": true, "rendered": false}))
		Expect(masked).To(BeFalse())
		expectCondition(metav1.ConditionFalse, conditions.ReasonPending, "set; rendering pending")
	})

	It("maps Masked to False/Error and reports masked", func() {
		masked := (&DPUDeviceReconciler{}).mirrorSpiffeEntryStatus(dpuDevice, cseWithStatus(map[string]bool{"set": true, "rendered": true, "masked": true}))
		Expect(masked).To(BeTrue())
		expectCondition(metav1.ConditionFalse, conditions.ReasonError, "masked by another entry")
	})
})

var _ = Describe("SPIFFE ClusterStaticEntry deletion", func() {
	var ctx context.Context

	BeforeEach(func() { ctx = context.Background() })

	noKindMatch := func() error {
		return &meta.NoKindMatchError{
			GroupKind:        clusterStaticEntryGVK.GroupKind(),
			SearchedVersions: []string{clusterStaticEntryGVK.Version},
		}
	}

	It("deletes the CR and reports done in a single pass when it has no finalizer", func() {
		cse := &spirev1alpha1.ClusterStaticEntry{}
		cse.SetName(testCSEName)
		fakeClient := fake.NewClientBuilder().WithScheme(spiffeScheme()).WithObjects(cse).Build()
		r := &DPUDeviceReconciler{Client: fakeClient, Recorder: record.NewFakeRecorder(10)}
		dpuDevice := spiffeDPUDevice()

		// The CR carries no finalizer, so the delete takes effect immediately and the re-read
		// confirms it is gone -- the DPUDevice finalizer can be released without an extra requeue.
		done, err := r.deleteSPIFFEEntry(ctx, dpuDevice)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())

		_, err = getCSE(ctx, r.Client)
		Expect(err).To(HaveOccurred())
	})

	It("reports not-done while the CR lingers behind an external finalizer", func() {
		cse := &spirev1alpha1.ClusterStaticEntry{}
		cse.SetName(testCSEName)
		cse.SetFinalizers([]string{"example.com/keep"})
		fakeClient := fake.NewClientBuilder().WithScheme(spiffeScheme()).WithObjects(cse).Build()
		r := &DPUDeviceReconciler{Client: fakeClient, Recorder: record.NewFakeRecorder(10)}
		dpuDevice := spiffeDPUDevice()

		// The delete is issued but the finalizer keeps the CR around (deletionTimestamp set), so
		// the caller must requeue until GC removes it.
		done, err := r.deleteSPIFFEEntry(ctx, dpuDevice)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeFalse())
	})

	It("reports done when no CR ever existed", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(spiffeScheme()).Build()
		r := &DPUDeviceReconciler{Client: fakeClient}

		done, err := r.deleteSPIFFEEntry(ctx, spiffeDPUDevice())
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())
	})

	It("reports done for an unrepresentable serial (no CR could have been created)", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(spiffeScheme()).Build()
		r := &DPUDeviceReconciler{Client: fakeClient}
		dpuDevice := spiffeDPUDevice()
		dpuDevice.Spec.SerialNumber = "MT/24:40"

		done, err := r.deleteSPIFFEEntry(ctx, dpuDevice)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())
	})

	It("reports done when the ClusterStaticEntry CRD is uninstalled, so deletion is not blocked", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(spiffeScheme()).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return noKindMatch()
				},
			}).Build()
		r := &DPUDeviceReconciler{Client: fakeClient}

		done, err := r.deleteSPIFFEEntry(ctx, spiffeDPUDevice())
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())
	})

	It("reports done when the ClusterStaticEntry CRD is uninstalled during delete", func() {
		cse := &spirev1alpha1.ClusterStaticEntry{}
		cse.SetName(testCSEName)
		fakeClient := fake.NewClientBuilder().WithScheme(spiffeScheme()).WithObjects(cse).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return noKindMatch()
				},
			}).Build()
		r := &DPUDeviceReconciler{Client: fakeClient}

		done, err := r.deleteSPIFFEEntry(ctx, spiffeDPUDevice())
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())
	})

	It("reports done when the ClusterStaticEntry CRD is uninstalled while confirming deletion", func() {
		cse := &spirev1alpha1.ClusterStaticEntry{}
		cse.SetName(testCSEName)
		firstGetDone := false
		confirmationGetReached := false
		fakeClient := fake.NewClientBuilder().WithScheme(spiffeScheme()).WithObjects(cse).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if firstGetDone {
						confirmationGetReached = true
						return noKindMatch()
					}
					firstGetDone = true
					return c.Get(ctx, key, obj, opts...)
				},
			}).Build()
		r := &DPUDeviceReconciler{Client: fakeClient}

		done, err := r.deleteSPIFFEEntry(ctx, spiffeDPUDevice())
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())
		Expect(confirmationGetReached).To(BeTrue())
	})
})

var _ = Describe("mapClusterStaticEntryToDPUDevice", func() {
	It("maps via the stamped labels", func() {
		cse := &spirev1alpha1.ClusterStaticEntry{}
		cse.SetName(testCSEName)
		cse.SetLabels(map[string]string{LabelDPUDeviceName: "dev-1", LabelDPUDeviceNamespace: "default"})

		reqs := mapClusterStaticEntryToDPUDevice(context.Background(), cse)
		Expect(reqs).To(HaveLen(1))
		Expect(reqs[0].Name).To(Equal("dev-1"))
		Expect(reqs[0].Namespace).To(Equal("default"))
	})

	It("returns nothing when labels are absent", func() {
		cse := &spirev1alpha1.ClusterStaticEntry{}
		cse.SetName(testCSEName)
		Expect(mapClusterStaticEntryToDPUDevice(context.Background(), cse)).To(BeEmpty())
	})
})

var _ = Describe("dpuSpiffeIdentityPredicate", func() {
	var spiffeMode *provisioningv1.IdentityMode

	BeforeEach(func() {
		spiffeMode = ptr.To(provisioningv1.IdentityModeSpiffe)
	})

	It("accepts a defensive cache-start create for a bound SPIFFE DPU", func() {
		p := dpuSpiffeIdentityPredicate()
		Expect(p.Create(event.CreateEvent{Object: dpuBoundTo(spiffeMode)})).To(BeTrue())
	})

	It("filters create events for bootstrap-token, nil, and unexpected objects", func() {
		p := dpuSpiffeIdentityPredicate()

		Expect(p.Create(event.CreateEvent{Object: dpuBoundTo(ptr.To(provisioningv1.IdentityModeBootstrapToken))})).To(BeFalse())
		Expect(p.Create(event.CreateEvent{})).To(BeFalse())
		Expect(p.Create(event.CreateEvent{Object: &corev1.ConfigMap{}})).To(BeFalse())
	})

	It("accepts the primary update path from non-SPIFFE to SPIFFE", func() {
		p := dpuSpiffeIdentityPredicate()

		Expect(p.Update(event.UpdateEvent{
			ObjectOld: dpuBoundTo(nil),
			ObjectNew: dpuBoundTo(spiffeMode),
		})).To(BeTrue())
		Expect(p.Update(event.UpdateEvent{
			ObjectOld: dpuBoundTo(ptr.To(provisioningv1.IdentityModeBootstrapToken)),
			ObjectNew: dpuBoundTo(spiffeMode),
		})).To(BeTrue())
	})

	It("filters unrelated SPIFFE updates and unsafe update inputs", func() {
		p := dpuSpiffeIdentityPredicate()

		Expect(p.Update(event.UpdateEvent{
			ObjectOld: dpuBoundTo(spiffeMode),
			ObjectNew: dpuBoundTo(spiffeMode),
		})).To(BeFalse())
		Expect(p.Update(event.UpdateEvent{
			ObjectOld: &corev1.ConfigMap{},
			ObjectNew: dpuBoundTo(spiffeMode),
		})).To(BeFalse())
		Expect(p.Update(event.UpdateEvent{
			ObjectOld: dpuBoundTo(nil),
			ObjectNew: &corev1.ConfigMap{},
		})).To(BeFalse())
		Expect(p.Update(event.UpdateEvent{})).To(BeFalse())
	})

	It("filters delete and generic events", func() {
		p := dpuSpiffeIdentityPredicate()
		Expect(p.Delete(event.DeleteEvent{Object: dpuBoundTo(spiffeMode)})).To(BeFalse())
		Expect(p.Generic(event.GenericEvent{Object: dpuBoundTo(spiffeMode)})).To(BeFalse())
	})
})

var _ = Describe("mapDPUToDPUDevice", func() {
	It("maps a bound DPU to its DPUDevice", func() {
		reqs := mapDPUToDPUDevice(context.Background(), dpuBoundTo(ptr.To(provisioningv1.IdentityModeSpiffe)))
		Expect(reqs).To(HaveLen(1))
		Expect(reqs[0].Name).To(Equal(testDeviceName))
		Expect(reqs[0].Namespace).To(Equal("default"))
	})

	It("returns nothing for nil and unexpected objects", func() {
		Expect(mapDPUToDPUDevice(context.Background(), nil)).To(BeEmpty())
		Expect(mapDPUToDPUDevice(context.Background(), &corev1.ConfigMap{})).To(BeEmpty())
	})
})
