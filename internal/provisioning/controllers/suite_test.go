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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/bfb"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpucluster"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpuset"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	provisioningwebhooks "github.com/nvidia/doca-platform/internal/provisioning/webhooks"

	nvidiaNodeMaintenancev1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
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
			filepath.Join("..", "..", "..", "test", "objects", "crd", "cert-manager"),
			filepath.Join("..", "..", "..", "test", "objects", "crd", "nodemaintenances"),
		},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "..", "config", "provisioning", "webhook")},
		},
	}

	// Set internal provisioning controller variables
	cutil.BFBBaseDir = filepath.Join(os.TempDir(), "dpf-bfb")

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme := scheme.Scheme
	err = provisioningv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = admissionv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = nvidiaNodeMaintenancev1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = certmanagerv1.AddToScheme(scheme)
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

	err = (&provisioningwebhooks.DPU{}).SetupWebhookWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())
	dpuReconciler := &dpu.DPUReconciler{
		Client:    k8sManager.GetClient(),
		Scheme:    k8sManager.GetScheme(),
		Recorder:  k8sManager.GetEventRecorderFor(dpu.DPUControllerName),
		Allocator: alloc,
	}
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

	dpuclusterReconciler := &dpucluster.DPUClusterReconciler{
		Client:    k8sManager.GetClient(),
		Scheme:    k8sManager.GetScheme(),
		Recorder:  k8sManager.GetEventRecorderFor(dpucluster.DPUClusterControllerName),
		Allocator: alloc,
	}
	err = dpuclusterReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

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
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if cancel != nil {
		cancel()
	}
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
