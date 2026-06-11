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
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

	g.Expect(r.reconcileApplicationPrereqs(ctx, dpuService, nil)).To(Succeed())

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
	r := &DPUServiceReconciler{Client: hostClient, Scheme: scheme}

	g.Expect(r.reconcilePrivilegedPodEnforcement(ctx, dpuClusterClient, dpuService, true)).To(Succeed())

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
	r := &DPUServiceReconciler{Client: hostClient, Scheme: scheme}

	g.Expect(r.reconcilePrivilegedPodEnforcement(ctx, dpuClusterClient, currentSvc, true)).To(Succeed())

	cm := &corev1.ConfigMap{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace}, cm)).To(Succeed())
	// svc-id was removed because shouldAllow=false; other-id must remain because
	// its owning DPUService still exists in a *different* namespace.
	g.Expect(cm.Data).To(Equal(map[string]string{"other-id": "other-ns/other"}))
}

// TestSweepOrphanedPrivilegedDPUServiceEntries verifies the sweep removes
// entries whose owning DPUService no longer exists.
func TestSweepOrphanedPrivilegedDPUServiceEntries(t *testing.T) {
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

	g.Expect(r.removeOrphanedPrivilegedDPUServiceEntries(ctx, dpuClusterClient)).To(Succeed())

	cm := &corev1.ConfigMap{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace}, cm)).To(Succeed())
	g.Expect(cm.Data).To(Equal(map[string]string{"svc-id": "svc-ns/svc"}))
}

// TestApplyPrivilegedPodPolicyVAPCreatesConfigMapBeforeBinding ensures the
// allowlist ConfigMap exists with the correct labels before the binding is
// applied so the binding's parameterNotFoundAction=Deny path is never hit
// during the bootstrap window.
func TestApplyPrivilegedPodPolicyVAPCreatesConfigMapBeforeBinding(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DPUServiceReconciler{Scheme: scheme}

	g.Expect(r.applyPrivilegedPodPolicyVAP(ctx, dpuClusterClient)).To(Succeed())

	cm := &corev1.ConfigMap{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace}, cm)).To(Succeed())
	g.Expect(cm.Labels).To(HaveKeyWithValue(DPUServicePrereqLabel, "true"))
	g.Expect(cm.Data).To(BeEmpty())
	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedVAPName}, &admissionregistrationv1.ValidatingAdmissionPolicy{})).To(Succeed())
	g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedVAPName}, &admissionregistrationv1.ValidatingAdmissionPolicyBinding{})).To(Succeed())
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

func TestReconcilePrivilegedPodEnforcementCleansUpWhenFeatureGateDisabled(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := applicationPrereqsTestScheme(t)
	featuregatetesting.SetFeatureGateDuringTest(t, features.Gates.(featuregate.MutableFeatureGate), features.PrivilegedPodEnforcement, false)

	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: privilegedAllowlistConfigMapNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name:      privilegedAllowlistConfigMapName,
			Namespace: privilegedAllowlistConfigMapNamespace,
			Labels:    map[string]string{DPUServicePrereqLabel: dpuServicePrereqLabelValue},
		}},
	).Build()
	r := &DPUServiceReconciler{Scheme: scheme}
	dpuService := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "svc-ns"},
		Status:     dpuservicev1.DPUServiceStatus{ServiceID: "svc-id"},
	}

	g.Expect(r.reconcilePrivilegedPodEnforcement(ctx, dpuClusterClient, dpuService, false)).To(Succeed())

	g.Expect(apierrors.IsNotFound(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedVAPName}, &admissionregistrationv1.ValidatingAdmissionPolicy{}))).To(BeTrue())
	g.Expect(apierrors.IsNotFound(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedVAPName}, &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}))).To(BeTrue())
	g.Expect(apierrors.IsNotFound(dpuClusterClient.Get(ctx, types.NamespacedName{Name: privilegedAllowlistConfigMapName, Namespace: privilegedAllowlistConfigMapNamespace}, &corev1.ConfigMap{}))).To(BeTrue())
}
