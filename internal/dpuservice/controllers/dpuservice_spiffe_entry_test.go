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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/spire"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	spirev1alpha1 "github.com/nvidia/doca-platform/third_party/forked/github.com/spiffe/spire-controller-manager/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	spiffeEnvtestEventuallyTimeout  = 10 * time.Second
	spiffeEnvtestEventuallyInterval = 200 * time.Millisecond
)

// The DPUService SPIFFE specs run against their own envtest environment rather than the shared
// suite. They need a zero-trust DPFOperatorConfig with spec.security.spiffe, and that field cannot
// be removed once set, so flipping the suite-wide singleton would leak into every other spec.
//
// The value over the fake-client unit tests is that the ClusterStaticEntry objects are validated
// against the real upstream CRD schema and go through a real status subresource, which is where
// a wrong field name or type would actually surface.
var _ = Describe("DPUService SPIFFE entries", Ordered, func() {
	var (
		testEnv    *envtest.Environment
		k8sClient  client.Client
		envCtx     context.Context
		cancel     context.CancelFunc
		reconciler *DPUServiceReconciler
	)

	BeforeAll(func() {
		if os.Getenv("KUBEBUILDER_ASSETS") == "" {
			Skip("KUBEBUILDER_ASSETS not set; skipping DPUService SPIFFE envtest")
		}

		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{
				filepath.Join("..", "..", "..", "config", "dpuservice", "crd", "bases"),
				filepath.Join("..", "..", "..", "config", "provisioning", "crd", "bases"),
				filepath.Join("..", "..", "..", "config", "operator-crds"),
				filepath.Join("..", "..", "..", "test", "objects", "crd", "spire"),
			},
			ErrorIfCRDPathMissing: true,
			BinaryAssetsDirectory: filepath.Join("..", "..", "..", "hack", "tools", "bin", "k8s",
				fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
		}
		restCfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())

		s := k8sruntime.NewScheme()
		Expect(corev1.AddToScheme(s)).To(Succeed())
		Expect(dpuservicev1.AddToScheme(s)).To(Succeed())
		Expect(provisioningv1.AddToScheme(s)).To(Succeed())
		Expect(operatorv1.AddToScheme(s)).To(Succeed())
		Expect(spirev1alpha1.AddToScheme(s)).To(Succeed())

		k8sClient, err = client.New(restCfg, client.Options{Scheme: s})
		Expect(err).NotTo(HaveOccurred())

		envCtx, cancel = context.WithCancel(context.Background())
		// Unlike the DPUDevice SPIFFE envtest this does not start a manager: the full DPUService
		// reconcile path needs ArgoCD and live DPUCluster clients, none of which this behavior
		// touches, so the SPIFFE sub-reconciler is driven directly instead.
		reconciler = &DPUServiceReconciler{Client: k8sClient, UncachedClient: k8sClient, Scheme: s}
	})

	AfterAll(func() {
		if cancel != nil {
			cancel()
			Expect(testEnv.Stop()).To(Succeed())
		}
	})

	It("registers, renders and deregisters entries for a SPIFFE-enabled DPUService", func() {
		suffix := utilrand.String(6)
		namespace := "spiffe-" + suffix
		serial := "MT" + utilrand.String(10)
		serviceID := "svc-" + suffix
		clusterName, clusterNamespace := "cluster-"+suffix, "tenant-"+suffix

		Expect(k8sClient.Create(envCtx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})).To(Succeed())

		cfg := spiffeEnvtestOperatorConfig(namespace)
		Expect(k8sClient.Create(envCtx, cfg)).To(Succeed())

		By("Creating a SPIFFE-mode DPU bound to the targeted DPUCluster")
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-" + suffix, Namespace: namespace},
			Spec: provisioningv1.DPUSpec{
				BFB:           ptr.To("bfb"),
				SerialNumber:  serial,
				DPUDeviceName: "device-" + suffix,
				DPUFlavor:     "flavor",
				DPUNodeName:   "node-" + suffix,
				NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				Cluster:       provisioningv1.K8sCluster{Name: clusterName, Namespace: clusterNamespace},
			},
		}
		Expect(k8sClient.Create(envCtx, dpu)).To(Succeed())
		dpu.Status.IdentityMode = ptr.To(provisioningv1.IdentityModeSpiffe)
		Expect(k8sClient.Status().Update(envCtx, dpu)).To(Succeed())

		dpuService := &dpuservicev1.DPUService{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-" + suffix, Namespace: namespace},
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceID: ptr.To(serviceID),
				HelmChart: dpuservicev1.HelmChart{
					Source: dpuservicev1.ApplicationSource{
						RepoURL: "oci://example.com/charts", Chart: "noop", Version: "0.0.1",
					},
				},
				Security: &dpuservicev1.DPUServiceSecurity{
					Privileged: ptr.To(false),
					SPIFFE:     &dpuservicev1.DPUServiceSPIFFE{},
				},
			},
		}
		Expect(k8sClient.Create(envCtx, dpuService)).To(Succeed())
		conditions.EnsureConditions(dpuService, dpuservicev1.Conditions)

		clusterConfigs := []*dpucluster.Config{{Cluster: &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: clusterNamespace},
		}}}

		By("Reconciling twice: the first pass takes the finalizer, the second creates the entry")
		Expect(reconciler.reconcileSPIFFEEntries(envCtx, dpuService, clusterConfigs, cfg)).To(Succeed())
		Expect(reconciler.reconcileSPIFFEEntries(envCtx, dpuService, clusterConfigs, cfg)).To(Succeed())

		entryName, err := dpuServiceClusterStaticEntryName(namespace, dpuService.Name, serial)
		Expect(err).NotTo(HaveOccurred())
		renderer, err := spire.NewDPUServiceIdentityRenderer(cfg.Spec.Security.SPIFFE)
		Expect(err).NotTo(HaveOccurred())
		wantIdentity, err := renderer.Render(dpuService, dpu, serviceID)
		Expect(err).NotTo(HaveOccurred())

		By("Verifying the entry the API server actually accepted against the upstream CRD schema")
		entry := &spirev1alpha1.ClusterStaticEntry{}
		Expect(k8sClient.Get(envCtx, client.ObjectKey{Name: entryName}, entry)).To(Succeed())
		Expect(entry.Spec.SPIFFEID).To(Equal(wantIdentity.SPIFFEID))
		Expect(entry.Spec.ParentID).To(Equal(wantIdentity.ParentID))
		Expect(entry.Spec.ClassName).To(Equal(testClassName))
		Expect(entry.Spec.Selectors).To(ConsistOf(
			"k8s:ns:"+namespace,
			"k8s:pod-label:"+dpuservicev1.DPFServiceIDLabelKey+":"+serviceID,
		))
		// metav1.Duration round-trips through the CRD as a string, so a wrong type here would
		// have been rejected on create rather than silently stored.
		Expect(entry.Spec.X509SVIDTTL.Duration).To(Equal(time.Hour))
		Expect(entry.Spec.JWTSVIDTTL.Duration).To(Equal(120 * time.Second))
		Expect(entry.Labels).To(HaveKeyWithValue(dpuservicev1.DPUServiceNameLabelKey, dpuService.Name))
		Expect(entry.Labels).To(HaveKeyWithValue(dpuservicev1.DPUServiceNamespaceLabelKey, namespace))

		By("Reporting Pending until spire-controller-manager renders the entry")
		cond := conditions.Get(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady)
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(string(conditions.ReasonPending)))

		By("Flipping to Ready once the entry is rendered")
		entry.Status.Rendered = true
		entry.Status.Set = true
		Expect(k8sClient.Status().Update(envCtx, entry)).To(Succeed())
		Expect(reconciler.reconcileSPIFFEEntries(envCtx, dpuService, clusterConfigs, cfg)).To(Succeed())
		Expect(conditions.Get(dpuService, dpuservicev1.ConditionSPIFFEEntriesReady).Status).To(Equal(metav1.ConditionTrue))

		By("Deregistering the entry when the DPUService is deleted")
		done, err := reconciler.reconcileDeleteSPIFFEEntries(envCtx, dpuService)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())
		Eventually(func() bool {
			return apierrors.IsNotFound(
				k8sClient.Get(envCtx, client.ObjectKey{Name: entryName}, &spirev1alpha1.ClusterStaticEntry{}))
		}).WithTimeout(spiffeEnvtestEventuallyTimeout).WithPolling(spiffeEnvtestEventuallyInterval).Should(BeTrue())
	})
})

// spiffeEnvtestOperatorConfig returns a zero-trust config with SPIFFE enabled. Zero-trust is not
// optional here: the API rejects spec.security.spiffe in any other deployment mode, and it in turn
// requires installViaRedfish.
func spiffeEnvtestOperatorConfig(namespace string) *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "dpfoperatorconfig", Namespace: namespace},
		Spec: operatorv1.DPFOperatorConfigSpec{
			DeploymentMode: operatorv1.DeploymentModeZeroTrust,
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("bfb-pvc"),
				InstallInterface: &operatorv1.ProvisioningInstallInterface{
					InstallViaRedfish: &operatorv1.InstallViaRedfish{},
				},
			},
			Security: &operatorv1.SecurityConfiguration{
				SPIFFE: &operatorv1.SPIFFEConfiguration{
					// Never dialed by the control-plane half; only the DPU cloud-init consumes it.
					SPIREServerAddress:              "spire-server.spire-system.svc:8081",
					SPIRETrustDomain:                testTrustDomain,
					KubeAPIAudience:                 "dpf-dpu-agent",
					SPIREOIDCURL:                    "https://oidc.example.test:30443",
					SPIREControllerManagerClassName: testClassName,
					TrustBundle: operatorv1.SPIFFETrustBundleConfigMapReference{
						Name:      "spire-bundle",
						Namespace: namespace,
					},
				},
			},
		},
	}
}
