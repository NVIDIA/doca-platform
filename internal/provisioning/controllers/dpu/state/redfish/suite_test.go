/*
Copyright 2025 NVIDIA

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

package redfish

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	redfishmock "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	testutils "github.com/nvidia/doca-platform/test/utils"

	nvidiaNodeMaintenancev1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg         *rest.Config
	k8sClient   client.Client
	testEnv     *envtest.Environment
	ctx         context.Context
	cancel      context.CancelFunc
	testNS      *corev1.Namespace
	testObjects []client.Object
	// testCATrustBundlePEM is a valid CA certificate PEM used to populate the dpf-ca-trust-bundle
	// ConfigMap that NewTLSClient reads. Individual tests that dial a mock BMC should patch this
	// bundle with mockServer.GetServerCertPEM() so server chain verification succeeds.
	testCATrustBundlePEM []byte
)

func TestRedfish(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Redfish Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "..", "config", "provisioning", "crd", "bases"),
			filepath.Join("..", "..", "..", "..", "..", "..", "deploy", "charts", "dpf-operator", "templates", "crds"),
			filepath.Join("..", "..", "..", "..", "..", "..", "test", "objects", "crd", "cert-manager"),
			filepath.Join("..", "..", "..", "..", "..", "..", "test", "objects", "crd", "nodemaintenances"),
		},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	testScheme := scheme.Scheme
	err = provisioningv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = operatorv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = nvidiaNodeMaintenancev1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	ctx, cancel = context.WithCancel(context.TODO())
	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// The verified mTLS client validates the BMC server certificate chain against the CA trust
	// bundle, and also self-checks that its own client key pair chains to that same bundle.
	// httptest serves a fixed built-in certificate (carrying the 127.0.0.1 IP SAN) that is shared
	// by every mock server, so capturing it once here lets the bundle verify all mock BMCs.
	probeServer, probeErr := redfishmock.CreateMockRedfishServer("BF-24.10", "password")
	Expect(probeErr).NotTo(HaveOccurred())
	serverCertPEM := probeServer.GetServerCertPEM()
	probeServer.Stop()
	Expect(serverCertPEM).NotTo(BeEmpty())

	// The verified mTLS client reads its key pair from a mounted directory. Provide one for the
	// suite so NewTLSClient can build the client (the mock BMC does not require client auth). The
	// client leaf is signed by the CA returned from CreateMTLSCerts, so that CA must be in the
	// trust bundle for the client-cert chain self-check in NewTLSClient to pass.
	clientCACrt, clientCrt, clientKey, _, _ := testutils.CreateMTLSCerts("127.0.0.1")
	// The bundle must verify both the mock BMC server certificate and the Redfish client cert.
	testCATrustBundlePEM = append(append([]byte{}, serverCertPEM...), clientCACrt...)
	certDir, mkErr := os.MkdirTemp("", "redfish-client-cert")
	Expect(mkErr).NotTo(HaveOccurred())
	for _, d := range []string{certDir, certDir + "-bf4"} {
		Expect(os.MkdirAll(d, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(d, corev1.TLSCertKey), clientCrt, 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(d, corev1.TLSPrivateKeyKey), clientKey, 0o600)).To(Succeed())
	}
	rfclient.SetClientCertDir(certDir)
	DeferCleanup(func() {
		_ = os.RemoveAll(certDir)
		_ = os.RemoveAll(certDir + "-bf4")
	})
})

var _ = BeforeEach(func() {
	testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "redfish-test-"}}
	Eventually(func() error {
		return k8sClient.Create(ctx, testNS)
	}).WithTimeout(10 * time.Second).Should(Succeed())
	testObjects = []client.Object{}

	// NewTLSClient reads the CA bundle from the dpf-ca-trust-bundle ConfigMap in the DPU namespace.
	// Create it in every test namespace so mTLS Redfish clients can be constructed.
	caBundleCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rfclient.CATrustBundleConfigMap,
			Namespace: testNS.Name,
		},
		Data: map[string]string{
			rfclient.CATrustBundleKey: string(testCATrustBundlePEM),
		},
	}
	Expect(k8sClient.Create(ctx, caBundleCM)).To(Succeed())
})

var _ = AfterEach(func() {
	for _, obj := range testObjects {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
	}
	if testNS != nil {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, testNS))).To(Succeed())
	}
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := (func() (err error) {
		sleepTime := 1 * time.Millisecond
		for i := 0; i < 12; i++ {
			if err = testEnv.Stop(); err == nil {
				return
			}
			sleepTime *= 2
			time.Sleep(sleepTime)
		}
		return
	})()
	Expect(err).NotTo(HaveOccurred())
})

func createObject(obj client.Object) {
	Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	testObjects = append(testObjects, obj)
}

func dpuFlavorObj(name string) *provisioningv1.DPUFlavor {
	return &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: provisioningv1.DPUFlavorSpec{},
	}
}

func dpuObj(name string) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
			Labels:    make(map[string]string),
		},
		Spec: provisioningv1.DPUSpec{
			BFB:          ptr.To("test-bfb"),
			SerialNumber: "MT25066004C" + utilrand.String(5),
			DPUFlavor:    "dpu-flavor",
			NodeEffect:   provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
		},
	}
}

func dpuNodeObj(name string) *provisioningv1.DPUNode {
	return &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
			Labels: map[string]string{
				cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel: "true",
			},
		},
		Spec: provisioningv1.DPUNodeSpec{
			NodeRebootMethod: &provisioningv1.NodeRebootMethod{
				GNOI: &provisioningv1.GNOI{},
			},
			NodeDMSAddress: &provisioningv1.DMSAddress{IP: "127.0.0.1", Port: 57400},
		},
	}
}

func dpuDeviceObj(name string) *provisioningv1.DPUDevice {
	return &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: provisioningv1.DPUDeviceSpec{
			SerialNumber: "MT25066004C" + utilrand.String(5),
			NumberOfPFs:  ptr.To(2),
		},
	}
}

func dpuClusterObj(name string, dpuType string) *provisioningv1.DPUCluster {
	return &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: provisioningv1.DPUClusterSpec{
			Type:       dpuType,
			Kubeconfig: fmt.Sprintf("%v-admin-kubeconfig", name),
		},
	}
}
