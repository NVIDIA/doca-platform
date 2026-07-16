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
	"os"
	"path/filepath"
	"runtime"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/spire"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var _ = Describe("DPUDevice SPIFFE reconcile (envtest)", Ordered, func() {
	var (
		testEnv   *envtest.Environment
		k8sClient client.Client
		envCtx    context.Context
		cancel    context.CancelFunc
		mgrDone   chan struct{}
	)

	BeforeAll(func() {
		if os.Getenv("KUBEBUILDER_ASSETS") == "" {
			Skip("KUBEBUILDER_ASSETS not set; skipping DPUDevice SPIFFE envtest")
		}

		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{
				filepath.Join("..", "..", "..", "..", "config", "provisioning", "crd", "bases"),
				filepath.Join("..", "..", "..", "..", "config", "operator-crds"),
				filepath.Join("..", "..", "..", "..", "test", "objects", "crd", "spire"),
				filepath.Join("..", "..", "..", "..", "test", "objects", "crd", "certmanager"),
			},
			ErrorIfCRDPathMissing: true,
			BinaryAssetsDirectory: filepath.Join("..", "..", "..", "..", "hack", "tools", "bin", "k8s",
				"1.32.0-"+runtime.GOOS+"-"+runtime.GOARCH),
		}
		restCfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())

		scheme := k8sruntime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(operatorv1.AddToScheme(scheme)).To(Succeed())
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		scheme.AddKnownTypeWithName(clusterStaticEntryGVK, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(clusterStaticEntryGVK.GroupVersion().WithKind("ClusterStaticEntryList"), &unstructured.UnstructuredList{})
		k8sClient, err = client.New(restCfg, client.Options{Scheme: scheme})
		Expect(err).NotTo(HaveOccurred())

		mgr, err := ctrl.NewManager(restCfg, ctrl.Options{Scheme: scheme, LeaderElection: false})
		Expect(err).NotTo(HaveOccurred())
		reconciler := &DPUDeviceReconciler{
			Client:   mgr.GetClient(),
			Recorder: mgr.GetEventRecorderFor(DPUDeviceControllerName),
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

		envCtx, cancel = context.WithCancel(context.Background())
		mgrDone = make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(mgrDone)
			Expect(mgr.Start(envCtx)).To(Succeed())
		}()
		cacheSynced := make(chan bool, 1)
		go func() {
			cacheSynced <- mgr.GetCache().WaitForCacheSync(envCtx)
		}()
		Eventually(cacheSynced).Should(Receive(BeTrue()))
	})

	AfterAll(func() {
		if cancel != nil {
			cancel()
			// Wait for the manager goroutine to fully return before stopping the API server, so it
			// cannot issue requests against an already-stopped server during shutdown.
			Eventually(mgrDone).Should(BeClosed())
			Expect(testEnv.Stop()).To(Succeed())
		}
	})

	It("reconciles and deregisters a class-scoped SPIFFE entry", func() {
		suffix := utilrand.String(6)
		namespace := "spiffe-" + suffix
		deviceName := "device-" + suffix
		dpuName := "dpu-" + suffix
		serial := "MT" + utilrand.String(10)

		Expect(k8sClient.Create(envCtx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})).To(Succeed())

		cfg := &operatorv1.DPFOperatorConfig{
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
						SPIREServerAddress:              "spire.example.test:30481",
						SPIRETrustDomain:                "example.test",
						KubeAPIAudience:                 "dpf-dpu-agent",
						SPIREOIDCURL:                    "https://oidc.example.test:30443",
						SPIREControllerManagerClassName: "spire-mgmt-spire",
						TrustBundle: operatorv1.SPIFFETrustBundleConfigMapReference{
							Name:      "spire-bundle",
							Namespace: namespace,
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(envCtx, cfg)).To(Succeed())

		node := &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{Name: "node-" + suffix, Namespace: namespace},
			Spec:       provisioningv1.DPUNodeSpec{DPUs: []provisioningv1.DPURef{{Name: deviceName}}},
		}
		Expect(k8sClient.Create(envCtx, node)).To(Succeed())

		device := &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deviceName,
				Namespace: namespace,
				Labels:    map[string]string{provisioningv1.DPUDeviceLabelSkipHWProvisioning: "true"},
			},
			Spec: provisioningv1.DPUDeviceSpec{SerialNumber: serial},
		}
		Expect(k8sClient.Create(envCtx, device)).To(Succeed())

		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: dpuName, Namespace: namespace},
			Spec: provisioningv1.DPUSpec{
				BFB:           ptr.To("test-bfb"),
				SerialNumber:  serial,
				DPUDeviceName: deviceName,
				DPUFlavor:     "dpu-flavor",
				DPUNodeName:   node.Name,
				NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
			},
		}
		Expect(k8sClient.Create(envCtx, dpu)).To(Succeed())
		dpu.Status.Phase = provisioningv1.DPUReady
		dpu.Status.Conditions = []metav1.Condition{}
		dpu.Status.IdentityMode = ptr.To(provisioningv1.IdentityModeSpiffe)
		Expect(k8sClient.Status().Update(envCtx, dpu)).To(Succeed())

		cseName, err := spire.DPUAgentClusterStaticEntryName(serial)
		Expect(err).NotTo(HaveOccurred())
		cse := newClusterStaticEntry()
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(envCtx, client.ObjectKey{Name: cseName}, cse)).To(Succeed())
			className, found, err := unstructured.NestedString(cse.Object, "spec", "className")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(className).To(Equal("spire-mgmt-spire"))
		}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

		Expect(unstructured.SetNestedField(cse.Object, true, "status", "rendered")).To(Succeed())
		Expect(unstructured.SetNestedField(cse.Object, false, "status", "set")).To(Succeed())
		Expect(k8sClient.Status().Update(envCtx, cse)).To(Succeed())
		statusCSE := newClusterStaticEntry()
		Expect(k8sClient.Get(envCtx, client.ObjectKey{Name: cseName}, statusCSE)).To(Succeed())
		rendered, found, err := unstructured.NestedBool(statusCSE.Object, "status", "rendered")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(rendered).To(BeTrue())

		Expect(k8sClient.Delete(envCtx, device)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(envCtx, client.ObjectKey{Name: cseName}, newClusterStaticEntry()))
		}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(BeTrue())
	})
})
