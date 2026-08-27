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
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	spirev1alpha1 "github.com/nvidia/doca-platform/third_party/forked/github.com/spiffe/spire-controller-manager/api/v1alpha1"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	testTrustDomain      = "cs.internal"
	testClassName        = "dpf"
	testServiceID        = "my-service"
	testClusterNamespace = "tenant"
)

func spiffeTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	g := NewWithT(t)
	s := runtime.NewScheme()
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())
	g.Expect(provisioningv1.AddToScheme(s)).To(Succeed())
	g.Expect(operatorv1.AddToScheme(s)).To(Succeed())
	g.Expect(spirev1alpha1.AddToScheme(s)).To(Succeed())
	return s
}

func spiffeTestDPUService(spiffeEnabled bool) *dpuservicev1.DPUService {
	dpuService := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "svc-ns"},
		Spec: dpuservicev1.DPUServiceSpec{
			ServiceID: ptr.To(testServiceID),
			Security:  &dpuservicev1.DPUServiceSecurity{Privileged: ptr.To(false)},
		},
	}
	if spiffeEnabled {
		dpuService.Spec.Security.SPIFFE = &dpuservicev1.DPUServiceSPIFFE{}
	}
	return dpuService
}

func spiffeTestOperatorConfig(spiffeEnabled bool) *operatorv1.DPFOperatorConfig {
	cfg := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "dpfoperatorconfig", Namespace: "dpf-operator-system"},
		Spec:       operatorv1.DPFOperatorConfigSpec{Security: &operatorv1.SecurityConfiguration{}},
	}
	if spiffeEnabled {
		cfg.Spec.Security.SPIFFE = &operatorv1.SPIFFEConfiguration{
			SPIRETrustDomain:                testTrustDomain,
			SPIREControllerManagerClassName: testClassName,
		}
	}
	return cfg
}

func spiffeTestDPUCluster() *dpucluster.Config {
	return &dpucluster.Config{Cluster: &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: testClusterNamespace},
	}}
}

func spiffeTestDPU(name, serial, clusterName string, mode provisioningv1.IdentityMode) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "dpf-operator-system"},
		Spec: provisioningv1.DPUSpec{
			SerialNumber: serial,
			Cluster:      provisioningv1.K8sCluster{Name: clusterName, Namespace: testClusterNamespace},
		},
		Status: provisioningv1.DPUStatus{IdentityMode: &mode},
	}
}

func TestDPUServiceSpiffeEnabled(t *testing.T) {
	g := NewWithT(t)

	g.Expect(dpuServiceSpiffeEnabled(&dpuservicev1.DPUService{})).To(BeFalse(), "no security block does not opt in")
	g.Expect(dpuServiceSpiffeEnabled(spiffeTestDPUService(false))).To(BeFalse(), "a security block without spiffe does not opt in")
	g.Expect(dpuServiceSpiffeEnabled(spiffeTestDPUService(true))).To(BeTrue(), "an empty spiffe block opts in, the field is presence-gated")
}

// TestDPUServiceSpiffeSelectors pins the selector shape. These selectors are the only thing
// stopping a DPUService SVID from being obtainable by an unrelated workload on the same DPU,
// so a change here is a security-relevant change.
func TestDPUServiceSpiffeSelectors(t *testing.T) {
	g := NewWithT(t)

	selectors := dpuServiceSpiffeSelectors(spiffeTestDPUService(true))

	g.Expect(selectors).To(ConsistOf(
		"k8s:ns:svc-ns",
		"k8s:pod-label:svc.dpu.nvidia.com/service:my-service",
	))
	// The DPU Agent entry created by the DPUDevice controller uses unix:uid:0 and shares a
	// parent SPIRE agent with this entry. Reusing that selector would let any uid-0 workload
	// on the DPU obtain the DPUService identity as well.
	g.Expect(selectors).NotTo(ContainElement("unix:uid:0"))
}

func TestDesiredSPIFFEEntries(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		serviceSpiffe   bool
		clusterSpiffe   bool
		deployInCluster bool
		serviceID       string
		clusterSelector *metav1.LabelSelector
		dpus            []*provisioningv1.DPU
		wantNames       []string
		wantSpiffeIDs   []string
		wantInvalid     []string
		clusterDeleting bool
	}{
		{
			name:          "no entries when the DPUService does not opt in",
			serviceSpiffe: false,
			clusterSpiffe: true,
			dpus:          []*provisioningv1.DPU{spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe)},
		},
		{
			name:          "no entries when the cluster does not opt in",
			serviceSpiffe: true,
			clusterSpiffe: false,
			dpus:          []*provisioningv1.DPU{spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe)},
		},
		{
			name:            "no entries for an in-cluster DPUService, which runs on no DPU",
			serviceSpiffe:   true,
			clusterSpiffe:   true,
			deployInCluster: true,
			dpus:            []*provisioningv1.DPU{spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe)},
		},
		{
			name:          "bootstrap-token DPUs are skipped, SPIFFE DPUs are registered",
			serviceSpiffe: true,
			clusterSpiffe: true,
			dpus: []*provisioningv1.DPU{
				spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe),
				spiffeTestDPU("dpu-b", "MT002", "cluster-a", provisioningv1.IdentityModeBootstrapToken),
			},
			wantNames:     []string{"dpu-service-svc-ns-svc-mt001-6d265f34"},
			wantSpiffeIDs: []string{"spiffe://cs.internal/dpu/mt001/service/svc-ns/my-service"},
		},
		{
			name:          "DPUs in non-selected clusters are skipped",
			serviceSpiffe: true,
			clusterSpiffe: true,
			dpus: []*provisioningv1.DPU{
				spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe),
				spiffeTestDPU("dpu-b", "MT002", "other-cluster", provisioningv1.IdentityModeSpiffe),
			},
			wantNames:     []string{"dpu-service-svc-ns-svc-mt001-6d265f34"},
			wantSpiffeIDs: []string{"spiffe://cs.internal/dpu/mt001/service/svc-ns/my-service"},
		},
		{
			name:          "entries are returned in a deterministic order",
			serviceSpiffe: true,
			clusterSpiffe: true,
			dpus: []*provisioningv1.DPU{
				spiffeTestDPU("dpu-z", "MT999", "cluster-a", provisioningv1.IdentityModeSpiffe),
				spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe),
			},
			wantNames: []string{"dpu-service-svc-ns-svc-mt001-6d265f34", "dpu-service-svc-ns-svc-mt999-4a7da031"},
			wantSpiffeIDs: []string{
				"spiffe://cs.internal/dpu/mt001/service/svc-ns/my-service",
				"spiffe://cs.internal/dpu/mt999/service/svc-ns/my-service",
			},
		},
		{
			name:            "a DPUCluster being deleted stops renewing identities for its DPUs",
			serviceSpiffe:   true,
			clusterSpiffe:   true,
			clusterDeleting: true,
			dpus:            []*provisioningv1.DPU{spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe)},
		},
		{
			// DPUDeployment generates <deployment>_<service>_<digest>. Underscores are legal in
			// a SPIFFE path segment, so these must register rather than be marked invalid.
			name:          "a DPUDeployment generated service ID is registered",
			serviceSpiffe: true,
			clusterSpiffe: true,
			serviceID:     "mydeployment_hbn_abc123",
			dpus:          []*provisioningv1.DPU{spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe)},
			wantNames:     []string{"dpu-service-svc-ns-svc-mt001-6d265f34"},
			wantSpiffeIDs: []string{"spiffe://cs.internal/dpu/mt001/service/svc-ns/mydeployment_hbn_abc123"},
		},
		{
			// One broken serial must not withhold identities from the healthy DPUs of the
			// same DPUService.
			name:          "a serial that cannot form a SPIFFE identity marks only that DPU invalid",
			serviceSpiffe: true,
			clusterSpiffe: true,
			dpus: []*provisioningv1.DPU{
				spiffeTestDPU("dpu-broken", "MT:00/1", "cluster-a", provisioningv1.IdentityModeSpiffe),
				spiffeTestDPU("dpu-ok", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe),
			},
			wantNames:     []string{"dpu-service-svc-ns-svc-mt001-6d265f34"},
			wantSpiffeIDs: []string{"spiffe://cs.internal/dpu/mt001/service/svc-ns/my-service"},
			wantInvalid:   []string{"dpu-broken"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := spiffeTestScheme(t)

			dpuService := spiffeTestDPUService(tt.serviceSpiffe)
			if tt.serviceID != "" {
				dpuService.Spec.ServiceID = ptr.To(tt.serviceID)
			}
			dpuService.Spec.DeployInCluster = ptr.To(tt.deployInCluster)
			dpuService.Spec.DPUClusterSelector = tt.clusterSelector

			objs := make([]client.Object, 0, len(tt.dpus))
			for _, dpu := range tt.dpus {
				objs = append(objs, dpu)
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&spirev1alpha1.ClusterStaticEntry{}).WithObjects(objs...).Build()
			r := &DPUServiceReconciler{Client: fakeClient, UncachedClient: fakeClient, Scheme: scheme}

			clusterConfig := spiffeTestDPUCluster()
			if tt.clusterDeleting {
				clusterConfig.Cluster.DeletionTimestamp = ptr.To(metav1.Now())
			}

			targets, invalid, err := r.desiredSPIFFEEntries(ctx, dpuService,
				[]*dpucluster.Config{clusterConfig}, spiffeTestOperatorConfig(tt.clusterSpiffe))
			g.Expect(err).NotTo(HaveOccurred(), "an unrepresentable serial is terminal for that DPU, not a reconcile failure")
			g.Expect(invalid).To(Equal(tt.wantInvalid))

			var gotNames, gotSpiffeIDs []string
			for _, target := range targets {
				gotNames = append(gotNames, target.name)
				gotSpiffeIDs = append(gotSpiffeIDs, target.spiffeID)
				// Every entry is parented to the SPIRE agent of its own DPU.
				g.Expect(target.parentID).To(HavePrefix("spiffe://cs.internal/spire/agent/dpu_hw/"))
			}
			g.Expect(gotNames).To(Equal(tt.wantNames))
			g.Expect(gotSpiffeIDs).To(Equal(tt.wantSpiffeIDs))
		})
	}
}

// TestReconcileSPIFFEEntriesCreatesEntryAndHoldsFinalizer walks the happy path: finalizer
// first, then the entry, with the condition tracking the rendering state.
func TestReconcileSPIFFEEntriesCreatesEntryAndHoldsFinalizer(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := spiffeTestScheme(t)

	dpuService := spiffeTestDPUService(true)
	dpu := spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&spirev1alpha1.ClusterStaticEntry{}).WithObjects(dpuService, dpu).Build()
	r := &DPUServiceReconciler{Client: fakeClient, UncachedClient: fakeClient, Scheme: scheme}

	conditions.EnsureConditions(dpuService, dpuservicev1.Conditions)
	clusterConfigs := []*dpucluster.Config{spiffeTestDPUCluster()}
	cfg := spiffeTestOperatorConfig(true)

	// First pass takes the finalizer before creating anything, so a crash cannot leak a
	// SPIRE registration that nothing is holding a finalizer for.
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())
	g.Expect(controllerutil.ContainsFinalizer(dpuService, dpuservicev1.DPUServiceSPIFFEDeregistrationFinalizer)).To(BeTrue())

	entries := &spirev1alpha1.ClusterStaticEntryList{}
	g.Expect(fakeClient.List(ctx, entries)).To(Succeed())
	g.Expect(entries.Items).To(BeEmpty(), "no entry is created before the finalizer is durable")

	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())
	g.Expect(fakeClient.List(ctx, entries)).To(Succeed())
	g.Expect(entries.Items).To(HaveLen(1))

	entry := entries.Items[0]
	g.Expect(entry.GetName()).To(Equal("dpu-service-svc-ns-svc-mt001-6d265f34"))
	g.Expect(entry.GetLabels()).To(HaveKeyWithValue(dpuservicev1.DPUServiceNameLabelKey, "svc"))
	g.Expect(entry.GetLabels()).To(HaveKeyWithValue(dpuservicev1.DPUServiceNamespaceLabelKey, "svc-ns"))

	g.Expect(entry.Spec.SPIFFEID).To(Equal("spiffe://cs.internal/dpu/mt001/service/svc-ns/my-service"))
	g.Expect(entry.Spec.ParentID).To(Equal("spiffe://cs.internal/spire/agent/dpu_hw/mt001"))
	g.Expect(entry.Spec.ClassName).To(Equal(testClassName))

	// Not yet rendered by spire-controller-manager, so the condition stays pending rather
	// than claiming an identity that cannot be issued.
	g.Expect(conditions.Get(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady).Status).To(Equal(metav1.ConditionFalse))

	entry.Status.Rendered = true
	g.Expect(fakeClient.Status().Update(ctx, &entry)).To(Succeed())
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())
	g.Expect(conditions.Get(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady).Status).To(Equal(metav1.ConditionTrue))
}

// TestReconcileSPIFFEEntriesReadyWithoutOptIn asserts the condition does not hold back the
// Ready summary of the overwhelming majority of DPUServices, which never use SPIFFE.
func TestReconcileSPIFFEEntriesReadyWithoutOptIn(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := spiffeTestScheme(t)

	dpuService := spiffeTestDPUService(false)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&spirev1alpha1.ClusterStaticEntry{}).WithObjects(dpuService).Build()
	r := &DPUServiceReconciler{Client: fakeClient, UncachedClient: fakeClient, Scheme: scheme}
	conditions.EnsureConditions(dpuService, dpuservicev1.Conditions)

	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService,
		[]*dpucluster.Config{spiffeTestDPUCluster()}, spiffeTestOperatorConfig(true))).To(Succeed())

	g.Expect(conditions.Get(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady).Status).To(Equal(metav1.ConditionTrue))
	g.Expect(controllerutil.ContainsFinalizer(dpuService, dpuservicev1.DPUServiceSPIFFEDeregistrationFinalizer)).To(BeFalse())
}

// TestReconcileSPIFFEEntriesRemovesStaleEntries covers opting back out: the entries are
// deleted and the finalizer released, so a DPUService is not pinned forever.
func TestReconcileSPIFFEEntriesRemovesStaleEntries(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := spiffeTestScheme(t)

	dpuService := spiffeTestDPUService(true)
	dpu := spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&spirev1alpha1.ClusterStaticEntry{}).WithObjects(dpuService, dpu).Build()
	r := &DPUServiceReconciler{Client: fakeClient, UncachedClient: fakeClient, Scheme: scheme}
	conditions.EnsureConditions(dpuService, dpuservicev1.Conditions)

	clusterConfigs := []*dpucluster.Config{spiffeTestDPUCluster()}
	cfg := spiffeTestOperatorConfig(true)
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())

	entries := &spirev1alpha1.ClusterStaticEntryList{}
	g.Expect(fakeClient.List(ctx, entries)).To(Succeed())
	g.Expect(entries.Items).To(HaveLen(1))

	// Opt the DPUService back out; the entry must be garbage collected by the controller,
	// since a cluster-scoped ClusterStaticEntry cannot carry an ownerReference to it.
	dpuService.Spec.Security.SPIFFE = nil
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())

	g.Expect(fakeClient.List(ctx, entries)).To(Succeed())
	g.Expect(entries.Items).To(BeEmpty())
	g.Expect(controllerutil.ContainsFinalizer(dpuService, dpuservicev1.DPUServiceSPIFFEDeregistrationFinalizer)).To(BeFalse())
	g.Expect(conditions.Get(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady).Status).To(Equal(metav1.ConditionTrue))
}

// TestReconcileDeleteSPIFFEEntries covers the deletion path releasing the finalizer.
func TestReconcileDeleteSPIFFEEntries(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := spiffeTestScheme(t)

	dpuService := spiffeTestDPUService(true)
	dpu := spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&spirev1alpha1.ClusterStaticEntry{}).WithObjects(dpuService, dpu).Build()
	r := &DPUServiceReconciler{Client: fakeClient, UncachedClient: fakeClient, Scheme: scheme}
	conditions.EnsureConditions(dpuService, dpuservicev1.Conditions)

	clusterConfigs := []*dpucluster.Config{spiffeTestDPUCluster()}
	cfg := spiffeTestOperatorConfig(true)
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())

	done, err := r.reconcileDeleteSPIFFEEntries(ctx, dpuService)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(done).To(BeTrue())
	g.Expect(controllerutil.ContainsFinalizer(dpuService, dpuservicev1.DPUServiceSPIFFEDeregistrationFinalizer)).To(BeFalse())

	entries := &spirev1alpha1.ClusterStaticEntryList{}
	g.Expect(fakeClient.List(ctx, entries)).To(Succeed())
	g.Expect(entries.Items).To(BeEmpty())
}

// TestReconcileSPIFFEEntriesToleratesMissingCRD asserts a cluster opting into SPIFFE without
// spire-controller-manager installed is not wedged in a permanent error loop.
func TestReconcileSPIFFEEntriesToleratesMissingCRD(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	// The type stays registered in the scheme (the controller binary registers it
	// unconditionally); the CRD being absent surfaces from the RESTMapper as a
	// NoKindMatchError on every ClusterStaticEntry request.
	scheme := spiffeTestScheme(t)
	noKindMatch := func() error {
		return &meta.NoKindMatchError{
			GroupKind:        clusterStaticEntryGK,
			SearchedVersions: []string{spirev1alpha1.SchemeGroupVersion.Version},
		}
	}

	dpuService := spiffeTestDPUService(true)
	dpu := spiffeTestDPU("dpu-a", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&spirev1alpha1.ClusterStaticEntry{}).WithObjects(dpuService, dpu).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*spirev1alpha1.ClusterStaticEntry); ok {
					return noKindMatch()
				}
				return c.Get(ctx, key, obj, opts...)
			},
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*spirev1alpha1.ClusterStaticEntryList); ok {
					return noKindMatch()
				}
				return c.List(ctx, list, opts...)
			},
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*spirev1alpha1.ClusterStaticEntry); ok {
					return noKindMatch()
				}
				return c.Create(ctx, obj, opts...)
			},
		}).Build()
	r := &DPUServiceReconciler{Client: fakeClient, UncachedClient: fakeClient, Scheme: scheme}
	conditions.EnsureConditions(dpuService, dpuservicev1.Conditions)

	clusterConfigs := []*dpucluster.Config{spiffeTestDPUCluster()}
	cfg := spiffeTestOperatorConfig(true)
	// Finalizer pass, then the pass that would create the entry.
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())

	condition := conditions.Get(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady)
	g.Expect(condition.Status).To(Equal(metav1.ConditionFalse), "an entry that cannot be rendered must not report ready")
	// Pending keeps spiffeEntriesProgressing re-reconciling, so installing SPIRE resolves this.
	g.Expect(condition.Reason).To(Equal(string(conditions.ReasonPending)))
	g.Expect(condition.Message).To(ContainSubstring("CRD is not installed"),
		"a missing CRD must not be reported as spire-controller-manager failing to render")
}

// TestReconcileSPIFFEEntriesInvalidDPUIsTerminal asserts an unrepresentable serial is a
// terminal Failure, not a Pending that implies it self-resolves, and that it does not stop
// the healthy DPUs of the same DPUService.
func TestReconcileSPIFFEEntriesInvalidDPUIsTerminal(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := spiffeTestScheme(t)

	dpuService := spiffeTestDPUService(true)
	broken := spiffeTestDPU("dpu-broken", "MT:00/1", "cluster-a", provisioningv1.IdentityModeSpiffe)
	healthy := spiffeTestDPU("dpu-ok", "MT001", "cluster-a", provisioningv1.IdentityModeSpiffe)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&spirev1alpha1.ClusterStaticEntry{}).WithObjects(dpuService, broken, healthy).Build()
	r := &DPUServiceReconciler{Client: fakeClient, UncachedClient: fakeClient, Scheme: scheme}
	conditions.EnsureConditions(dpuService, dpuservicev1.Conditions)

	clusterConfigs := []*dpucluster.Config{spiffeTestDPUCluster()}
	cfg := spiffeTestOperatorConfig(true)
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())
	g.Expect(r.reconcileSPIFFEEntries(ctx, dpuService, clusterConfigs, cfg)).To(Succeed())

	condition := conditions.Get(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady)
	g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(condition.Reason).To(Equal(string(conditions.ReasonFailure)))
	g.Expect(condition.Message).To(ContainSubstring("dpu-broken"))

	entries := &spirev1alpha1.ClusterStaticEntryList{}
	g.Expect(fakeClient.List(ctx, entries)).To(Succeed())
	g.Expect(entries.Items).To(HaveLen(1))
	g.Expect(entries.Items[0].GetName()).To(Equal("dpu-service-svc-ns-svc-mt001-6d265f34"))
}
