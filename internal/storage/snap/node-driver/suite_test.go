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
	"path/filepath"
	"testing"
	"time"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// These are shared across your test suite
var (
	testTimeout             = time.Second * 15
	testConsistentlyTimeout = time.Second * 5
	testInterval            = time.Millisecond * 250
	cfg                     *rest.Config
	k8sClient               client.Client
	testEnv                 *envtest.Environment
	testNamespace           = "test-namespace"
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
			filepath.Join("..", "..", "..", "..", "config", "storage", "crd", "bases"),
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

	// Add corev1 APIs to create Namespaces
	err = corev1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	// Create the client
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Create test namespace
	Expect(k8sClient.Create(GinkgoT().Context(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	})).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
