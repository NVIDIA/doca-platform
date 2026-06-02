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

package controllers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpudevice"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode"
	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunodemaintenance"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpuset"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const dmsServerPodName = "dms-server-pod-name"

var (
	cfg                        *rest.Config
	testClient                 client.Client
	testEnv                    *envtest.Environment
	ctx, testManagerCancelFunc = context.WithCancel(ctrl.SetupSignalHandler())
	key                        *rsa.PrivateKey
	cert                       *x509.Certificate
)

func TestMain(m *testing.M) {
	ctrllog.SetLogger(zap.New(zap.UseDevMode(true)))
	setupLogger := ctrl.Log.WithName("dms-server-controller-test-setup")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "deploy", "charts", "dpf-operator", "templates", "crds"),
			filepath.Join("..", "..", "..", "..", "..", "test", "objects", "crd", "cert-manager"),
			filepath.Join("..", "..", "..", "..", "..", "test", "objects", "crd", "argocd"),
			filepath.Join("..", "..", "..", "..", "..", "test", "objects", "crd", "kamaji"),
		},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}
	var err error

	s := scheme.Scheme
	if err := provisioningv1.AddToScheme(s); err != nil {
		panic(fmt.Sprintf("Failed to add provisioningv1 scheme: %v", err))
	}

	if err = operatorv1.AddToScheme(s); err != nil {
		panic(fmt.Sprintf("Failed to add provisioningv1 scheme: %v", err))
	}
	// cfg is defined in this file globally in this package. This allows the resource collector to use this
	// config when it runs.
	cfg, err = testEnv.Start()
	if err != nil {
		panic(fmt.Sprintf("Failed to set up test environment: %v", err))
	}

	ctrl.SetLogger(klog.Background())

	// create the certs required for communication between the DPU controller and the mock DMS server
	key, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("Failed to set up test certificate private key: %v", err))
	}
	cert, err = newCert(key)
	if err != nil {
		panic(fmt.Sprintf("Failed to set up test certificate: %v", err))
	}
	// testClient is defined globally in this package so it can be used by the resource.
	testClient, err = client.New(cfg, client.Options{Scheme: s})
	if err != nil {
		panic(fmt.Sprintf("Failed to create client: %v", err))
	}

	testManager, err := ctrl.NewManager(cfg,
		ctrl.Options{
			Scheme: s,
			// Set metrics server bind address to 0 to disable it.
			Metrics: server.Options{
				BindAddress: "0",
			}})
	if err != nil {
		panic(fmt.Sprintf("Failed to create test manager: %v", err))
	}

	// Set base directory for BFB and kubeconfig on the DPU controller.
	// This overwrites hardcoded variables in order to enable testing.
	cutil.BFBBaseDir = os.TempDir()
	cutil.KubeconfigBaseDir = os.TempDir()

	err = os.MkdirAll(cutil.BFBBaseDir, 0755)
	if err != nil {
		panic(err)
	}

	dpuSetReconciler := dpuset.DPUSetReconciler{
		Client:   testClient,
		Scheme:   testManager.GetScheme(),
		Recorder: testManager.GetEventRecorderFor("dpu-set-controller"),
	}
	if err := dpuSetReconciler.SetupWithManager(testManager); err != nil {
		panic(fmt.Sprintf("Failed to setup DPU reconciler: %v", err))
	}

	dpuMap := dutil.NewDPUInProvisioningMap(50)

	dpuReconciler := dpu.NewDPUReconciler(
		testManager,
		allocator.NewAllocator(testClient),
		&mockKubeadmJoinCommandGenerator{},
		&mockDPUArtifactGenerator{},
		&mockHostUptimeReporter{},
		dutil.DPUOptions{DPUInstallInterface: string(provisioningv1.InstallViaGNOI), MaxDPUParallelInstallations: 50},
		dpuMap)
	if err := dpuReconciler.SetupWithManager(testManager); err != nil {
		panic(fmt.Sprintf("Failed to setup DPU reconciler: %v", err))
	}

	dpuNodeReconciler := dpunode.DPUNodeReconciler{
		Client:              testManager.GetClient(),
		DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaGNOI)),
		Options: dnutil.HostAgentPodOptions{
			HostAgentImageWithTag: "example.com/oof:v1.1.1",
			ImagePullSecrets:      []corev1.LocalObjectReference{{Name: "one"}},
			DMSTimeout:            100,
			DMSPodTimeout:         100,
		},
	}
	if err := dpuNodeReconciler.SetupWithManager(testManager); err != nil {
		panic(fmt.Sprintf("Failed to setup dpuNode reconciler: %v", err))
	}

	dpuNodeMaintenanceReconciler := dpunodemaintenance.DPUNodeMaintenanceReconciler{
		Client:              testManager.GetClient(),
		DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaGNOI)),
		Recorder:            testManager.GetEventRecorderFor(dpunodemaintenance.DPUNodeMaintenanceControllerName),
	}
	if err := dpuNodeMaintenanceReconciler.SetupWithManager(testManager); err != nil {
		panic(fmt.Sprintf("Failed to setup dpuNodeMaintenance reconciler: %v", err))
	}

	nodeReconciler := dpunode.NodeReconciler{
		Client: testManager.GetClient(),
		Options: dnutil.HostAgentPodOptions{
			HostAgentImageWithTag: "example.com/oof:v1.1.1",
			ImagePullSecrets:      []corev1.LocalObjectReference{{Name: "one"}},
			DMSTimeout:            100,
			DMSPodTimeout:         100,
		},
	}
	if err := nodeReconciler.SetupWithManager(testManager); err != nil {
		panic(fmt.Sprintf("Failed to setup Node reconciler: %v", err))
	}

	dmsServerReconciler := DMSServerReconciler{
		Client:  testClient,
		PodName: dmsServerPodName,
	}
	if err := dmsServerReconciler.SetupWithManager(testManager); err != nil {
		panic(fmt.Sprintf("Failed to setup DMSServer reconciler: %v", err))
	}
	// Add DPUDevice controller
	dpuDeviceReconciler := &dpudevice.DPUDeviceReconciler{
		Client: testManager.GetClient(),
		Scheme: testManager.GetScheme(),
	}
	if err := dpuDeviceReconciler.SetupWithManager(testManager); err != nil {
		panic(fmt.Sprintf("Failed to setup DPUDevice reconciler: %v", err))
	}

	hostAgentServerReconciler := HostAgentServerReconciler{
		Client: testClient,
	}
	if err := hostAgentServerReconciler.SetupWithManager(testManager); err != nil {
		panic(fmt.Sprintf("Failed to setup HostAgentServer reconciler: %v", err))
	}

	go func() {
		if err := testManager.Start(ctx); err != nil {
			panic(fmt.Sprintf("Failed to start test manager: %v", err))
		}
	}()

	// run the test suite in this package.
	if code := m.Run(); code != 0 {
		setupLogger.Info("error running tests:", "code", code)
	}

	if testManagerCancelFunc != nil {
		testManagerCancelFunc()
	}

	if err := testEnv.Stop(); err != nil {
		panic(fmt.Sprintf("Failed to stop test environment: %v", err))
	}
}
