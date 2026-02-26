/*
Copyright 2024 NVIDIA

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

package controller

import (
	"context"
	"crypto/tls"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/discovery"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpucluster"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpudevice"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode"
	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunodemaintenance"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpuset"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	provisioningwebhooks "github.com/nvidia/doca-platform/internal/provisioning/webhooks"

	nvidiaNodeMaintenancev1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	// This testBFB file is small but has the same structure as a BFB for the purposes of reading versions.
	// This file was written by the unit test at $PROJECT_ROOT/internal/bfb/version_test.go
	//go:embed testdata/test-bfb.bfb
	testBFB []byte

	cfg               *rest.Config
	k8sClient         client.Client
	testEnv           *envtest.Environment
	ctx               context.Context
	cancel            context.CancelFunc
	bfbServerURL      string
	dpunodeReconciler *dpunode.DPUNodeReconciler
	dpuReconciler     *dpu.DPUReconciler
	dpuMap            *dutil.DPUInProvisioningMap
)

const (
	maxDPUParallelInstallations = 1
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Provisioning Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "provisioning", "crd", "bases"),
			filepath.Join("..", "..", "..", "deploy", "charts", "dpf-operator", "templates", "crds"),
			filepath.Join("..", "..", "..", "test", "objects", "crd", "cert-manager"),
			filepath.Join("..", "..", "..", "test", "objects", "crd", "nodemaintenances"),
		},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "..", "config", "provisioning", "webhook")},
		},
	}

	dir, err := os.MkdirTemp("", "")
	Expect(err).NotTo(HaveOccurred())
	cutil.BFBBaseDir = dir

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme := scheme.Scheme
	err = provisioningv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = operatorv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = admissionv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = nvidiaNodeMaintenancev1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())

	webhookInstallOptions := &testEnv.WebhookInstallOptions
	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		WebhookServer: webhook.NewServer(
			webhook.Options{
				Host:    webhookInstallOptions.LocalServingHost,
				Port:    webhookInstallOptions.LocalServingPort,
				CertDir: webhookInstallOptions.LocalServingCertDir,
			}),
		LeaderElection: false,
		Metrics: server.Options{
			BindAddress: "0",
		}})
	Expect(err).ToNot(HaveOccurred())

	alloc := allocator.NewAllocator(k8sManager.GetClient())
	err = (&provisioningwebhooks.BFB{}).SetupWebhookWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())
	bfbReconciler := &bfb.BFBReconciler{
		Client:   k8sManager.GetClient(),
		Scheme:   k8sManager.GetScheme(),
		Recorder: k8sManager.GetEventRecorderFor(bfb.BFBControllerName),
	}
	err = bfbReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	bfsReconciler := &bluefieldsoftware.BlueFieldSoftwareReconciler{
		Client:   k8sManager.GetClient(),
		Scheme:   k8sManager.GetScheme(),
		Recorder: k8sManager.GetEventRecorderFor(bluefieldsoftware.BlueFieldSoftwareControllerName),
	}
	err = bfsReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	dpuMap = dutil.NewDPUInProvisioningMap(maxDPUParallelInstallations)

	dpuReconciler = dpu.NewDPUReconciler(k8sManager,
		alloc,
		&mockNodeJoinCommandGenerator{},
		&reboot.DMSPodExecUptimeChecker{},
		dutil.DPUOptions{DPUInstallInterface: string(provisioningv1.InstallViaMock), MaxDPUParallelInstallations: maxDPUParallelInstallations},
		dpuMap)
	err = dpuReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&provisioningwebhooks.DPUSet{}).SetupWebhookWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())
	dpusetReconciler := &dpuset.DPUSetReconciler{
		Client:   k8sManager.GetClient(),
		Scheme:   k8sManager.GetScheme(),
		Recorder: k8sManager.GetEventRecorderFor(dpuset.DPUSetControllerName),
	}
	err = dpusetReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&provisioningwebhooks.DPUFlavor{}).SetupWebhookWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())

	err = (&provisioningwebhooks.DPUDevice{Client: k8sManager.GetClient()}).SetupWebhookWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())

	err = (&provisioningwebhooks.DPUNode{Client: k8sManager.GetClient()}).SetupWebhookWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())

	err = (&provisioningwebhooks.DPUDiscoveryValidator{}).SetupWebhookWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())

	dpuclusterReconciler := &dpucluster.DPUClusterReconciler{
		Client:    k8sManager.GetClient(),
		Scheme:    k8sManager.GetScheme(),
		Recorder:  k8sManager.GetEventRecorderFor(dpucluster.DPUClusterControllerName),
		Allocator: alloc,
	}
	err = dpuclusterReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	dpunodeReconciler = &dpunode.DPUNodeReconciler{
		Client: k8sManager.GetClient(),
		Options: dnutil.HostAgentPodOptions{
			HostAgentImageWithTag: "example.com/doca-platform-foundation/dpf-provisioning-controller/hostdriver:v0.1.0",
		},
		Recorder:            k8sManager.GetEventRecorderFor(dpunode.DPUNodeControllerName),
		DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaGNOI)),
	}
	err = dpunodeReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	nodeReconciler := &dpunode.NodeReconciler{
		Client: k8sManager.GetClient(),
		Options: dnutil.HostAgentPodOptions{
			HostAgentImageWithTag: "example.com/doca-platform-foundation/dpf-provisioning-controller/hostdriver:v0.1.0",
			BFBRegistryAddress:    "bfb-registry:8082",
		},
	}
	err = nodeReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	// Add DPU Discovery controller
	dpuDiscoveryReconciler := &discovery.DPUDiscoveryReconciler{
		Client: k8sManager.GetClient(),
	}
	err = dpuDiscoveryReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	dpunodemaintenanceReconciler := &dpunodemaintenance.DPUNodeMaintenanceReconciler{
		Client:              k8sManager.GetClient(),
		DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaGNOI)),
		Recorder:            k8sManager.GetEventRecorderFor(dpunodemaintenance.DPUNodeMaintenanceControllerName),
		Options: dpunodemaintenance.DPUNodeMaintenanceOptions{
			MultiDPUOperationsSyncWaitTime: 30 * time.Second,
			MaxUnavailableDPUNodes:         50,
		},
	}
	err = dpunodemaintenanceReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	// Add DPUDevice controller
	dpuDeviceReconciler := &dpudevice.DPUDeviceReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}
	err = dpuDeviceReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

	defer bfbServer(testBFB)

	// wait for the webhook server to get ready
	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, webhookInstallOptions.LocalServingPort)
	Eventually(func() error {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			return err
		}
		conn.Close() //nolint: errcheck
		return nil
	}).Should(Succeed())

	By("creating dpf-operator-system namespace")
	Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace}}))).To(Succeed())
})

var _ = AfterSuite(func() {
	By("removing the BFB directory ")
	_ = os.RemoveAll(cutil.BFBBaseDir)

	By("deleting dpf-operator-system namespace")
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace}}))).To(Succeed())

	By("tearing down the test environment")
	if cancel != nil {
		cancel()
	}
	err := (func() (err error) {
		// Need to sleep if the first stop fails due to a bug:
		// https://github.com/kubernetes-sigs/controller-runtime/issues/1571
		sleepTime := 1 * time.Millisecond
		for i := 0; i < 12; i++ { // Exponentially sleep up to ~4s
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

const (
	BFB512KBPath = "/bf-bundle-dummy-512KB.bfb"
	BFB8KBPath   = "/bf-bundle-dummy-8KB.bfb"
)

func bfbServer(bfbToServe []byte) func() {
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Support both HEAD (for size verification) and GET (for download)
		Expect(r.Method).To(SatisfyAny(Equal("GET"), Equal("HEAD")))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bfbToServe)))
		w.WriteHeader(http.StatusOK)
		if r.Method == "GET" {
			_, _ = w.Write(bfbToServe)
		}
	}
	mux.HandleFunc(BFB512KBPath, handler)
	mux.HandleFunc(BFB8KBPath, handler)

	s := httptest.NewUnstartedServer(mux)
	// bfbServerURL is a global variable used by tests in this package to locate the BFB server.
	bfbServerURL = "http://" + s.Listener.Addr().String()

	go s.Start()
	return s.Close
}

type mockNodeJoinCommandGenerator struct{}

func (m *mockNodeJoinCommandGenerator) GenerateJoinCommand(ctx context.Context, dc *provisioningv1.DPUCluster) (string, error) {
	return "soup", nil
}
