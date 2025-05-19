/*
COPYRIGHT 2025 NVIDIA

Licensed under the Apache License, Version 2.0 (the License);
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an AS IS BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"path/filepath"
	"testing"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// These are shared across your test suite
var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

// The standard Go test entry point
func TestControllerSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Snap Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			// Make sure this directory actually contains your CRDs
			filepath.Join("..", "..", "..", "..", "config", "snap", "crd"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// Start the envtest control plane
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Add your CRDs (snapstoragev1) and any other APIs (corev1, etc.) to the default scheme
	scheme := scheme.Scheme
	err = snapstoragev1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	// If you need corev1 APIs to create Namespaces, you can also do:
	// corev1.AddToScheme(scheme.Scheme)

	// Create the client
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Create a context, used to stop the manager in AfterSuite
	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())

	// Optionally create a manager if you want to run your actual controllers
	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		// metrics server optional:
		Metrics: server.Options{
			BindAddress: "0", // or ":8080"
		},
		LeaderElection: false,
	})
	Expect(err).ToNot(HaveOccurred())

	// Example: set up your custom controller(s)
	// e.g. VolumeAttachmentReconciler
	volumeAttachmentReconciler := &VolumeAttachmentReconciler{
		Client:   k8sManager.GetClient(),
		Scheme:   k8sManager.GetScheme(),
		NodeName: "test-node", // This must match the testNodeName constant in the test
	}
	err = volumeAttachmentReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	// Start the manager in a goroutine, so tests can run
	go func() {
		defer GinkgoRecover()
		err := k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if cancel != nil {
		cancel()
	}
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
