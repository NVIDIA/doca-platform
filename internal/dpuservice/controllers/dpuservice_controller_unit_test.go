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

package controllers

import (
	"context"
	"errors"
	"strconv"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/argocd"
	"github.com/nvidia/doca-platform/internal/features"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"

	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/component-base/featuregate"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func applicationPrereqsTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	g := NewWithT(t)
	s := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(s)).To(Succeed())
	g.Expect(admissionregistrationv1.AddToScheme(s)).To(Succeed())
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())
	g.Expect(provisioningv1.AddToScheme(s)).To(Succeed())
	g.Expect(argov1.AddToScheme(s)).To(Succeed())
	return s
}

func noopPrivilegedPodEnforcementValidator(context.Context, client.Client) error {
	return nil
}

func TestReconcileApplicationPrereqsEnsuresNamespaceForDeployInCluster(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DPUServiceReconciler{Client: c, Scheme: scheme}
	dpuService := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "svc-ns",
		},
		Spec: dpuservicev1.DPUServiceSpec{
			DeployInCluster: ptr.To(true),
		},
	}

	g.Expect(r.reconcileApplicationPrereqs(ctx, dpuService, nil, &operatorv1.DPFOperatorConfig{})).To(Succeed())

	ns := &corev1.Namespace{}
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "svc-ns"}, ns)).To(Succeed())
	g.Expect(ns.Labels).ToNot(HaveKey(NamespaceScopeLabelKey))
	g.Expect(ns.Labels).ToNot(HaveKey(DPUServicePrereqLabel))
}

func TestReconcileNamespace(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DPUServiceReconciler{Client: c, Scheme: scheme}

	g.Expect(r.ensureNamespace(ctx, c, "svc-ns")).To(Succeed())

	ns := &corev1.Namespace{}
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "svc-ns"}, ns)).To(Succeed())
	g.Expect(ns.Labels).To(HaveKeyWithValue(NamespaceScopeLabelKey, "svc-ns"))
	g.Expect(ns.Labels).To(HaveKeyWithValue(DPUServicePrereqLabel, "true"))
}

func TestReconcileNamespaceAddsLabelsToExistingNamespace(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "svc-ns",
			Labels: map[string]string{"existing": "true"},
		},
	}).Build()
	r := &DPUServiceReconciler{Client: c, Scheme: scheme}

	g.Expect(r.ensureNamespace(ctx, c, "svc-ns")).To(Succeed())

	ns := &corev1.Namespace{}
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "svc-ns"}, ns)).To(Succeed())
	g.Expect(ns.Labels).To(HaveKeyWithValue("existing", "true"))
	g.Expect(ns.Labels).To(HaveKeyWithValue(NamespaceScopeLabelKey, "svc-ns"))
	g.Expect(ns.Labels).To(HaveKeyWithValue(DPUServicePrereqLabel, "true"))
}

func TestEnsureNamespacesEnsuresLabelsInDPUCluster(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)
	hostClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	cluster := &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpu-cluster",
			Namespace: "dpu-cluster-ns",
		},
	}
	r := &DPUServiceReconciler{
		Client: hostClient,
		Scheme: scheme,
		RemoteCache: dpucluster.NewStaticClusterClientProvider(map[client.ObjectKey]client.Client{
			client.ObjectKeyFromObject(cluster): dpuClusterClient,
		}),
	}

	g.Expect(r.ensureNamespaces(ctx, []*dpucluster.Config{{Cluster: cluster}}, argocd.AppProjectNameDPU, "svc-ns")).To(Succeed())

	ns := &corev1.Namespace{}
	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: "svc-ns"}, ns)).To(Succeed())
	g.Expect(ns.Labels).To(HaveKeyWithValue(NamespaceScopeLabelKey, "svc-ns"))
	g.Expect(ns.Labels).To(HaveKeyWithValue(DPUServicePrereqLabel, "true"))
}

func TestReconcilePrivilegedPodEnforcementCreatesPolicyBindingAndConfigMapEntry(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)
	dpuService := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "svc-ns"},
		Spec: dpuservicev1.DPUServiceSpec{
			Security: &dpuservicev1.DPUServiceSecurity{Privileged: ptr.To(true)},
		},
		Status: dpuservicev1.DPUServiceStatus{ServiceID: "svc-id"},
	}
	hostClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpuService).Build()
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DPUServiceReconciler{
		Client:                            hostClient,
		Scheme:                            scheme,
		privilegedPodEnforcementValidator: noopPrivilegedPodEnforcementValidator,
	}

	g.Expect(r.reconcilePrivilegedPodEnforcement(ctx, dpuClusterClient, dpuService, &provisioningv1.DPUCluster{}, true)).To(Succeed())

	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedVAPName}, &admissionregistrationv1.ValidatingAdmissionPolicy{})).To(Succeed())
	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedVAPName}, &admissionregistrationv1.ValidatingAdmissionPolicyBinding{})).To(Succeed())

	cm := &corev1.ConfigMap{}
	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace}, cm)).To(Succeed())
	g.Expect(cm.Data).To(HaveKeyWithValue("svc-id", "svc-ns/svc"))
}

// TestReconcilePrivilegedPodEnforcementPreservesPeerEntries is the regression
// test for the namespace-scoped DPUService list bug: reconciling a DPUService
// in one namespace must not remove the allowlist entry of a DPUService in a
// different namespace.
func TestReconcilePrivilegedPodEnforcementPreservesPeerEntries(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)

	currentSvc := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "svc-ns"},
		Spec: dpuservicev1.DPUServiceSpec{
			Security: &dpuservicev1.DPUServiceSecurity{Privileged: ptr.To(false)},
		},
		Status: dpuservicev1.DPUServiceStatus{ServiceID: "svc-id"},
	}
	peerSvc := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "other-ns"},
		Spec: dpuservicev1.DPUServiceSpec{
			Security: &dpuservicev1.DPUServiceSecurity{Privileged: ptr.To(true)},
		},
		Status: dpuservicev1.DPUServiceStatus{ServiceID: "other-id"},
	}
	hostClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(currentSvc, peerSvc).Build()
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: privilegedAllowlistConfigMapNamespace}},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace},
			Data: map[string]string{
				"svc-id":   "svc-ns/svc",
				"other-id": "other-ns/other",
			},
		},
	).Build()
	r := &DPUServiceReconciler{
		Client:                            hostClient,
		Scheme:                            scheme,
		privilegedPodEnforcementValidator: noopPrivilegedPodEnforcementValidator,
	}

	g.Expect(r.reconcilePrivilegedPodEnforcement(ctx, dpuClusterClient, currentSvc, &provisioningv1.DPUCluster{}, true)).To(Succeed())

	cm := &corev1.ConfigMap{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace}, cm)).To(Succeed())
	// svc-id was removed because shouldAllow=false; other-id must remain because
	// its owning DPUService still exists in a *different* namespace.
	g.Expect(cm.Data).To(Equal(map[string]string{"other-id": "other-ns/other"}))
}

// TestReconcilePrivilegedAllowlistRemovesOrphans verifies the rebuild drops
// entries whose owning DPUService no longer exists.
func TestReconcilePrivilegedAllowlistRemovesOrphans(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)
	livingSvc := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "svc-ns"},
		Status:     dpuservicev1.DPUServiceStatus{ServiceID: "svc-id"},
	}
	hostClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(livingSvc).Build()
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: privilegedAllowlistConfigMapNamespace}},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace},
			Data: map[string]string{
				"svc-id":      "svc-ns/svc",
				"orphaned-id": "old-ns/gone",
			},
		},
	).Build()
	r := &DPUServiceReconciler{Client: hostClient, Scheme: scheme}

	g.Expect(r.reconcilePrivilegedAllowlist(ctx, dpuClusterClient, &provisioningv1.DPUCluster{})).To(Succeed())

	cm := &corev1.ConfigMap{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace}, cm)).To(Succeed())
	g.Expect(cm.Data).To(Equal(map[string]string{"svc-id": "svc-ns/svc"}))
}

// TestReconcilePrivilegedAllowlistRepopulatesPeersOnReEnable is the regression
// test for the breakglass re-enable race: whenever the allowlist is empty (e.g.
// a freshly created ConfigMap), a single DPUService reconcile must restore EVERY
// privileged peer that targets the cluster before the binding is flipped back to
// Deny — not just the triggering service's own entry — so peers' privileged
// workloads are never transiently denied during re-enable.
func TestReconcilePrivilegedAllowlistRepopulatesPeersOnReEnable(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)

	svcA := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "a-ns"},
		Spec:       dpuservicev1.DPUServiceSpec{Security: &dpuservicev1.DPUServiceSecurity{Privileged: ptr.To(true)}},
		Status:     dpuservicev1.DPUServiceStatus{ServiceID: "a-id"},
	}
	svcB := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "b-ns"},
		Spec:       dpuservicev1.DPUServiceSpec{Security: &dpuservicev1.DPUServiceSecurity{Privileged: ptr.To(true)}},
		Status:     dpuservicev1.DPUServiceStatus{ServiceID: "b-id"},
	}
	hostClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svcA, svcB).Build()
	// The allowlist ConfigMap exists but is empty (e.g. just created).
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: privilegedAllowlistConfigMapNamespace}},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace},
			Data:       map[string]string{},
		},
	).Build()
	r := &DPUServiceReconciler{Client: hostClient, Scheme: scheme}

	g.Expect(r.reconcilePrivilegedAllowlist(ctx, dpuClusterClient, &provisioningv1.DPUCluster{})).To(Succeed())

	cm := &corev1.ConfigMap{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace}, cm)).To(Succeed())
	// Both privileged peers are present after a single reconcile, not just one.
	g.Expect(cm.Data).To(Equal(map[string]string{"a-id": "a-ns/a", "b-id": "b-ns/b"}))
}

// TestProbeErrorClassification verifies the shared probe-error classifiers used
// by validatePrivilegedPodEnforcement and validateAllowlistedPrivilegedPodAdmission.
// The transient paramRef-not-synced signal and our VAP's own Invalid+message
// denial must be recognized consistently, and a denial from a different
// admission stage (e.g. PodSecurity Forbidden) must NOT be mistaken for either.
func TestProbeErrorClassification(t *testing.T) {
	g := NewWithT(t)

	paramRefNotSynced := errors.New("admission webhook denied the request: " + paramRefNotSyncedMarker)
	invalidDenied := &apierrors.StatusError{ErrStatus: metav1.Status{
		Reason:  metav1.StatusReasonInvalid,
		Message: `Pod "x" is invalid: ` + privilegedPodDeniedMessage,
	}}
	invalidOther := &apierrors.StatusError{ErrStatus: metav1.Status{
		Reason:  metav1.StatusReasonInvalid,
		Message: `Pod "x" is invalid: spec.containers: unrelated validation error`,
	}}
	forbidden := &apierrors.StatusError{ErrStatus: metav1.Status{
		Reason:  metav1.StatusReasonForbidden,
		Message: `pods "x" is forbidden: violates PodSecurity "restricted:latest"`,
	}}

	// isParamRefNotSyncedErr: only the transient informer-lag marker matches.
	g.Expect(isParamRefNotSyncedErr(nil)).To(BeFalse())
	g.Expect(isParamRefNotSyncedErr(paramRefNotSynced)).To(BeTrue())
	g.Expect(isParamRefNotSyncedErr(invalidDenied)).To(BeFalse())
	g.Expect(isParamRefNotSyncedErr(forbidden)).To(BeFalse())

	// isPrivilegedPodDeniedErr: only our VAP's Invalid+message denial matches; a
	// denial from another admission stage must not look like our VAP's decision.
	g.Expect(isPrivilegedPodDeniedErr(nil)).To(BeFalse())
	g.Expect(isPrivilegedPodDeniedErr(invalidDenied)).To(BeTrue())
	g.Expect(isPrivilegedPodDeniedErr(invalidOther)).To(BeFalse())
	g.Expect(isPrivilegedPodDeniedErr(forbidden)).To(BeFalse())
	g.Expect(isPrivilegedPodDeniedErr(paramRefNotSynced)).To(BeFalse())
}

// TestReconcilePrivilegedAllowlistSeedsConfigMapBeforeBinding ensures the
// allowlist ConfigMap (and its namespace) exists with the correct labels before
// the binding is applied. reconcilePrivilegedAllowlist seeds it, so the binding's
// parameterNotFoundAction=Deny path is never hit during the bootstrap window.
func TestReconcilePrivilegedAllowlistSeedsConfigMapBeforeBinding(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DPUServiceReconciler{
		// No DPUServices in the management cluster, so the seeded allowlist is empty.
		Client:                            fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:                            scheme,
		privilegedPodEnforcementValidator: noopPrivilegedPodEnforcementValidator,
	}

	// reconcilePrivilegedAllowlist ensures the namespace + ConfigMap exist; the
	// binding is applied only afterwards.
	g.Expect(r.reconcilePrivilegedAllowlist(ctx, dpuClusterClient, &provisioningv1.DPUCluster{})).To(Succeed())
	g.Expect(r.applyPrivilegedPodPolicyVAP(ctx, dpuClusterClient, admissionregistrationv1.Deny)).To(Succeed())

	cm := &corev1.ConfigMap{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace}, cm)).To(Succeed())
	g.Expect(cm.Labels).To(HaveKeyWithValue(DPUServicePrereqLabel, "true"))
	g.Expect(cm.Data).To(BeEmpty())
	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedVAPName}, &admissionregistrationv1.ValidatingAdmissionPolicy{})).To(Succeed())
	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedVAPName}, binding)).To(Succeed())
	g.Expect(binding.Spec.ValidationActions).To(ConsistOf(admissionregistrationv1.Deny))
}

// stubClusterClientProvider returns a configurable error from GetClient and
// is only used by tests that verify the remote-cache fallback paths in
// getDPUClusterClient.
type stubClusterClientProvider struct {
	err    error
	client client.Client
}

func (s *stubClusterClientProvider) GetClient(_ client.ObjectKey) (client.Client, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.client != nil {
		return s.client, nil
	}
	return nil, dpucluster.ErrDPUClusterNoConnectionAvailable
}

func (s *stubClusterClientProvider) ListClients() ([]client.Client, error) {
	if s.client != nil {
		return []client.Client{s.client}, nil
	}
	return nil, nil
}

func TestGetDPUClusterClientFallsBackOnBothNotReadySentinels(t *testing.T) {
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)
	cluster := &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu-cluster", Namespace: "dpu-cluster-ns"},
	}

	cases := []struct {
		name     string
		err      error
		fallback bool
	}{
		{"no connection available", dpucluster.ErrDPUClusterNoConnectionAvailable, true},
		{"not connected", dpucluster.ErrDPUClusterNotConnected, true},
		{"unrelated error", context.DeadlineExceeded, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			r := &DPUServiceReconciler{
				Client:      fake.NewClientBuilder().WithScheme(scheme).Build(),
				Scheme:      scheme,
				RemoteCache: &stubClusterClientProvider{err: tc.err},
			}
			_, err := r.getDPUClusterClient(ctx, cluster)
			// The direct-client builder needs a kamaji secret that we do not
			// seed, so the fallback path returns its own error — we only
			// assert whether the sentinel was treated as "fall back" (the
			// error must be different) or propagated as-is.
			g.Expect(err).To(HaveOccurred())
			if tc.fallback {
				g.Expect(err).ToNot(MatchError(tc.err), "sentinel should have triggered fallback, not be propagated")
			} else {
				g.Expect(err).To(MatchError(tc.err))
			}
		})
	}
}

// TestReconcilePrivilegedPodEnforcementSwitchesToAuditWhenDisabled
// verifies the breakglass behavior: when enforcement is disabled (enforce=false,
// read from DPFOperatorConfig spec.security.privilegedPodEnforcement) the VAP,
// its binding, and the allowlist ConfigMap are all kept, the binding is switched
// to Audit (so nothing is denied), and the allowlist is kept maintained as in
// the enforcing case (rebuilt from the live DPUServices) so Audit-mode
// violations only flag pods that would otherwise be denied. The objects are
// never deleted, working around the Kubernetes paramRef informer bug
// (https://github.com/kubernetes/kubernetes/issues/133827).
func TestReconcilePrivilegedPodEnforcementSwitchesToAuditWhenDisabled(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)

	// Pre-seed an existing VAP and binding (in the enforcing Deny mode) with
	// distinct UIDs. The UIDs let us prove the objects are patched in place
	// rather than deleted and recreated: a delete+recreate would lose the UID,
	// which is exactly the cycle that triggers the Kubernetes paramRef informer
	// bug we are working around.
	const (
		vapUID     = types.UID("seeded-vap-uid")
		bindingUID = types.UID("seeded-binding-uid")
	)
	seededVAP := privilegedPodsVAP.DeepCopy()
	seededVAP.UID = vapUID
	seededBinding := privilegedPodsVAPBinding.DeepCopy()
	seededBinding.UID = bindingUID
	seededBinding.Spec.ValidationActions = []admissionregistrationv1.ValidationAction{admissionregistrationv1.Deny}

	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: privilegedAllowlistConfigMapNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name:      privilegedAllowlistConfigMapName,
			Namespace: privilegedAllowlistConfigMapNamespace,
			Labels:    map[string]string{DPUServicePrereqLabel: dpuServicePrereqLabelValue},
		},
			// A stale entry must be dropped by the authoritative rebuild even
			// while enforcement is disabled.
			Data: map[string]string{"stale-id": "svc-ns/stale"},
		},
		seededVAP,
		seededBinding,
	).Build()
	dpuService := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "svc-ns"},
		Status:     dpuservicev1.DPUServiceStatus{ServiceID: "svc-id"},
	}
	// The allowlist is rebuilt from the live DPUServices, so the service must be
	// reachable through the management-cluster client.
	hostClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpuService).Build()
	r := &DPUServiceReconciler{Client: hostClient, Scheme: scheme}

	g.Expect(r.reconcilePrivilegedPodEnforcement(ctx, dpuClusterClient, dpuService, &provisioningv1.DPUCluster{}, false)).To(Succeed())

	// The VAP is kept in place (same UID, not recreated).
	vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedVAPName}, vap)).To(Succeed())
	g.Expect(vap.UID).To(Equal(vapUID), "VAP must be patched in place, not deleted and recreated")

	// The binding is kept in place (same UID) and switched to Audit.
	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedVAPName}, binding)).To(Succeed())
	g.Expect(binding.UID).To(Equal(bindingUID), "binding must be patched in place, not deleted and recreated")
	g.Expect(binding.Spec.ValidationActions).To(ConsistOf(admissionregistrationv1.Audit))

	// The ConfigMap is kept and maintained as normal: the rebuild drops the
	// stale orphan entry and keeps the live privileged service's entry, so the
	// Audit log only flags pods that enforcement would have denied.
	cm := &corev1.ConfigMap{}
	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace}, cm)).To(Succeed())
	g.Expect(cm.Labels).To(HaveKeyWithValue(DPUServicePrereqLabel, dpuServicePrereqLabelValue))
	g.Expect(cm.Data).To(Equal(map[string]string{"svc-id": "svc-ns/svc"}))
}

// TestPrivilegedPodEnforcementChangedPredicate verifies the DPFOperatorConfig
// watch only enqueues DPUServices when the resolved enforcement state actually
// changes, so unrelated config edits do not re-reconcile every DPUService.
func TestPrivilegedPodEnforcementChangedPredicate(t *testing.T) {
	g := NewWithT(t)
	p := privilegedPodEnforcementChangedPredicate()

	configWith := func(enforce *bool) *operatorv1.DPFOperatorConfig {
		return &operatorv1.DPFOperatorConfig{
			Spec: operatorv1.DPFOperatorConfigSpec{
				Security: &operatorv1.SecurityConfiguration{PrivilegedPodEnforcement: enforce},
			},
		}
	}

	// Create/Delete/Generic never enqueue: DPUServices reconcile their own
	// lifecycle and pick up state via resync.
	g.Expect(p.Create(event.CreateEvent{Object: configWith(ptr.To(false))})).To(BeFalse())
	g.Expect(p.Delete(event.DeleteEvent{Object: configWith(ptr.To(false))})).To(BeFalse())
	g.Expect(p.Generic(event.GenericEvent{Object: configWith(ptr.To(false))})).To(BeFalse())

	// Enforcement flips true -> false: enqueue.
	g.Expect(p.Update(event.UpdateEvent{
		ObjectOld: configWith(ptr.To(true)),
		ObjectNew: configWith(ptr.To(false)),
	})).To(BeTrue())

	// Unset (defaults true) -> explicit false: enqueue.
	g.Expect(p.Update(event.UpdateEvent{
		ObjectOld: configWith(nil),
		ObjectNew: configWith(ptr.To(false)),
	})).To(BeTrue())

	// Enforcement unchanged (both default to enabled), only an unrelated spec
	// change: do not enqueue.
	oldCfg := configWith(nil)
	newCfg := configWith(ptr.To(true))
	newCfg.Spec.Networking = &operatorv1.Networking{ControlPlaneMTU: ptr.To(1500)}
	g.Expect(p.Update(event.UpdateEvent{ObjectOld: oldCfg, ObjectNew: newCfg})).To(BeFalse())
}

// TestOvnEncapIPFromNode verifies parsing of the OVN-Kubernetes node-encap-ips annotation.
func TestOvnEncapIPFromNode(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name          string
		annotations   map[string]string
		expectedIP    string
		hasAnnotation bool
	}{
		{
			name:          "returns first valid IPv4 IP from JSON array",
			annotations:   map[string]string{ovnNodeEncapIPsAnnotation: `["10.0.120.1"]`},
			expectedIP:    "10.0.120.1",
			hasAnnotation: true,
		},
		{
			name:          "returns first valid IPv4 IP when multiple are present",
			annotations:   map[string]string{ovnNodeEncapIPsAnnotation: `["10.0.120.1","10.0.120.2"]`},
			expectedIP:    "10.0.120.1",
			hasAnnotation: true,
		},
		{
			name:          "returns first valid IPv4 IP when IPv6 is present",
			annotations:   map[string]string{ovnNodeEncapIPsAnnotation: `["fa00:fb00::1","10.0.120.1"]`},
			expectedIP:    "10.0.120.1",
			hasAnnotation: true,
		},
		{
			name:          "returns empty when annotation absent",
			annotations:   nil,
			expectedIP:    "",
			hasAnnotation: false,
		},
		{
			name:          "returns empty when annotation is empty string",
			annotations:   map[string]string{ovnNodeEncapIPsAnnotation: ""},
			expectedIP:    "",
			hasAnnotation: true,
		},
		{
			name:          "returns empty when annotation is invalid JSON",
			annotations:   map[string]string{ovnNodeEncapIPsAnnotation: "not-json"},
			expectedIP:    "",
			hasAnnotation: true,
		},
		{
			name:          "returns empty when no valid IP in array",
			annotations:   map[string]string{ovnNodeEncapIPsAnnotation: `["not-an-ip"]`},
			expectedIP:    "",
			hasAnnotation: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "host-node-1", Annotations: tc.annotations},
			}
			ip, hasAnnotation := ovnEncapIPFromNode(ctx, node)
			g.Expect(ip).To(Equal(tc.expectedIP))
			g.Expect(hasAnnotation).To(Equal(tc.hasAnnotation))
		})
	}
}

// TestNodeEndpointAddress verifies endpoint address selection via main-cluster OVN encap IPs
// and fallback to the DPU-cluster NodeInternalIP.
func TestNodeEndpointAddress(t *testing.T) {
	ctx := context.Background()
	internalIP := corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "192.168.1.10"}

	dpuClusterNode := func(name string) *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{internalIP}},
		}
	}

	hostNodeWithOVN := func(hostNodeName, annotation string) *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: hostNodeName,
				Annotations: map[string]string{
					ovnNodeEncapIPsAnnotation: annotation,
				},
			},
		}
	}

	t.Run("with ConfigPortsOverHighSpeed enabled", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.Gates.(featuregate.MutableFeatureGate), features.ConfigPortsOverHighSpeed, true)

		t.Run("prefers OVN encap IP from host cluster Node", func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(nodeEndpointAddress(ctx, dpuClusterNode("dpu-cluster-node-1"), hostNodeWithOVN("host-node-1", `["10.0.120.1"]`))).To(Equal("10.0.120.1"))
		})

		t.Run("falls back to DPU-cluster NodeInternalIP when nil host cluster node", func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(nodeEndpointAddress(ctx, dpuClusterNode("dpu-cluster-node-1"), nil)).To(Equal("192.168.1.10"))
		})

		t.Run("falls back to DPU-cluster NodeInternalIP when OVN annotation is absent", func(t *testing.T) {
			g := NewWithT(t)
			hostNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "host-node-1"}}
			g.Expect(nodeEndpointAddress(ctx, dpuClusterNode("dpu-cluster-node-1"), hostNode)).To(Equal("192.168.1.10"))
		})

		t.Run("returns empty when host Node has invalid OVN encap annotation", func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(nodeEndpointAddress(ctx, dpuClusterNode("dpu-cluster-node-1"), hostNodeWithOVN("host-node-1", "not-json"))).To(Equal(""))
		})

		t.Run("returns empty when host Node has empty OVN encap annotation", func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(nodeEndpointAddress(ctx, dpuClusterNode("dpu-cluster-node-1"), hostNodeWithOVN("host-node-1", ""))).To(Equal(""))
		})
	})

	t.Run("with ConfigPortsOverHighSpeed disabled", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.Gates.(featuregate.MutableFeatureGate), features.ConfigPortsOverHighSpeed, false)

		t.Run("uses DPU-cluster NodeInternalIP even when host Node has OVN encap IP", func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(nodeEndpointAddress(ctx, dpuClusterNode("dpu-cluster-node-1"), hostNodeWithOVN("host-node-1", `["10.0.120.1"]`))).To(Equal("192.168.1.10"))
		})
	})
}

func requestsForHostNodeTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := applicationPrereqsTestScheme(t)
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithIndex(&provisioningv1.DPUNode{}, dpuNodeKubeNodeRefField, func(obj client.Object) []string {
			dpuNode, ok := obj.(*provisioningv1.DPUNode)
			if !ok || dpuNode.Status.KubeNodeRef == nil {
				return nil
			}
			return []string{*dpuNode.Status.KubeNodeRef}
		}).
		WithIndex(&provisioningv1.DPU{}, dpuNodeNameField, func(obj client.Object) []string {
			dpu, ok := obj.(*provisioningv1.DPU)
			if !ok {
				return nil
			}
			return []string{dpu.Spec.DPUNodeName}
		}).
		WithIndex(&dpuservicev1.DPUService{}, dpuServiceConfigPortsField, func(obj client.Object) []string {
			dpuService, ok := obj.(*dpuservicev1.DPUService)
			if !ok {
				return nil
			}
			return []string{strconv.FormatBool(dpuService.Spec.ConfigPorts != nil)}
		}).
		Build()
}

func TestRequestsForHostNode(t *testing.T) {
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)

	const (
		hostNodeName     = "host-node-encap"
		dpuNodeNamespace = "dpu-ns"
		dpuNodeName      = "dpu-node-encap"
		dpuName          = "dpu-1"
		clusterNamespace = "cluster-ns"
		clusterName      = "test-cluster"
		serviceNamespace = "svc-ns"
		serviceName      = "encap-service"
	)

	hostNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: hostNodeName,
			Annotations: map[string]string{
				ovnNodeEncapIPsAnnotation: `["10.0.120.1"]`,
			},
		},
	}
	dpuNode := &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpuNodeName,
			Namespace: dpuNodeNamespace,
		},
		Status: provisioningv1.DPUNodeStatus{
			KubeNodeRef: ptr.To(hostNodeName),
		},
	}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpuName,
			Namespace: dpuNodeNamespace,
		},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName: dpuNodeName,
			Cluster: provisioningv1.K8sCluster{
				Namespace: clusterNamespace,
				Name:      clusterName,
			},
		},
	}
	dpuClusterNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: dpuName},
	}
	dpuService := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: serviceNamespace,
		},
		Spec: dpuservicev1.DPUServiceSpec{
			ConfigPorts: &dpuservicev1.ConfigPorts{
				ServiceType: corev1.ServiceTypeNodePort,
				Ports: []dpuservicev1.ConfigPort{{
					Name:     "port1",
					Protocol: corev1.ProtocolTCP,
					Port:     80,
				}},
			},
		},
	}
	clusterKey := client.ObjectKey{Namespace: clusterNamespace, Name: clusterName}
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpuClusterNode).Build()

	newReconciler := func(objs ...client.Object) *DPUServiceReconciler {
		t.Helper()
		return &DPUServiceReconciler{
			Client: requestsForHostNodeTestClient(t, objs...),
			Scheme: scheme,
			RemoteCache: dpucluster.NewStaticClusterClientProvider(map[client.ObjectKey]client.Client{
				clusterKey: dpuClusterClient,
			}),
		}
	}

	t.Run("enqueues DPUService with config ports when host Node is linked via KubeNodeRef", func(t *testing.T) {
		g := NewWithT(t)
		r := newReconciler(hostNode, dpuNode, dpu, dpuService)

		g.Expect(r.requestsForHostNode(ctx, hostNode)).To(ContainElement(ctrl.Request{
			NamespacedName: types.NamespacedName{Namespace: serviceNamespace, Name: serviceName},
		}))
	})

	t.Run("does not enqueue when host Node has no linked DPUNode", func(t *testing.T) {
		g := NewWithT(t)
		r := newReconciler(hostNode, dpuService)

		g.Expect(r.requestsForHostNode(ctx, hostNode)).To(BeEmpty())
	})

	t.Run("does not enqueue when DPUService does not match NodeSelector", func(t *testing.T) {
		g := NewWithT(t)
		mismatchedService := dpuService.DeepCopy()
		mismatchedService.Spec.ServiceDaemonSet = &dpuservicev1.ServiceDaemonSetValues{
			NodeSelector: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key:      "region",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"us-west"},
					}},
				}},
			},
		}
		dpuClusterNodeWithLabel := dpuClusterNode.DeepCopy()
		dpuClusterNodeWithLabel.Labels = map[string]string{"region": "us-east"}
		dpuClusterClientWithNode := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpuClusterNodeWithLabel).Build()

		r := &DPUServiceReconciler{
			Client: requestsForHostNodeTestClient(t, hostNode, dpuNode, dpu, mismatchedService),
			Scheme: scheme,
			RemoteCache: dpucluster.NewStaticClusterClientProvider(map[client.ObjectKey]client.Client{
				clusterKey: dpuClusterClientWithNode,
			}),
		}

		g.Expect(r.requestsForHostNode(ctx, hostNode)).To(BeEmpty())
	})
}

func TestNodeOVNEncapIPPredicate(t *testing.T) {
	predicate := nodeOVNEncapIPPredicate()
	ovnEncapIPValue := `["10.0.120.1"]`
	ovnEncapIPValueModified := `["10.0.120.2"]`

	getTestNode := func() *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "host-node",
				Annotations: map[string]string{},
			},
		}
	}

	t.Run("returns true when OVN encap IPs annotation changes", func(t *testing.T) {
		g := NewWithT(t)
		oldNode := getTestNode()
		oldNode.Annotations[ovnNodeEncapIPsAnnotation] = ovnEncapIPValue

		newNode := oldNode.DeepCopy()
		newNode.Annotations[ovnNodeEncapIPsAnnotation] = ovnEncapIPValueModified

		g.Expect(predicate.UpdateFunc(event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode})).To(BeTrue())
	})

	t.Run("returns false when unrelated node annotation change", func(t *testing.T) {
		g := NewWithT(t)
		oldNode := getTestNode()
		oldNode.Annotations[ovnNodeEncapIPsAnnotation] = ovnEncapIPValue

		newNode := oldNode.DeepCopy()
		newNode.Annotations["key"] = "new"

		g.Expect(predicate.UpdateFunc(event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode})).To(BeFalse())
	})

	t.Run("returns true on create when OVN encap IPs annotation is present", func(t *testing.T) {
		g := NewWithT(t)
		node := getTestNode()
		node.Annotations[ovnNodeEncapIPsAnnotation] = ovnEncapIPValue

		g.Expect(predicate.CreateFunc(event.CreateEvent{Object: node})).To(BeTrue())
	})

	t.Run("returns false on create when OVN encap IPs annotation is not present", func(t *testing.T) {
		g := NewWithT(t)
		node := getTestNode()

		g.Expect(predicate.CreateFunc(event.CreateEvent{Object: node})).To(BeFalse())
	})

	t.Run("returns true on delete when annotation was present", func(t *testing.T) {
		g := NewWithT(t)
		node := getTestNode()
		node.Annotations[ovnNodeEncapIPsAnnotation] = ovnEncapIPValue

		g.Expect(predicate.DeleteFunc(event.DeleteEvent{Object: node})).To(BeTrue())
	})

	t.Run("returns false on delete when annotation was not present", func(t *testing.T) {
		g := NewWithT(t)
		node := getTestNode()

		g.Expect(predicate.DeleteFunc(event.DeleteEvent{Object: node})).To(BeFalse())
	})

	t.Run("returns false for generic events", func(t *testing.T) {
		g := NewWithT(t)
		node := getTestNode()
		g.Expect(predicate.GenericFunc(event.GenericEvent{Object: node})).To(BeFalse())
	})
}
