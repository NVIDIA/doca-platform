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

package controllers

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	argov1 "github.com/nvidia/doca-platform/internal/argocd/api/application/v1alpha1"

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

var cfg *rest.Config
var testClient client.Client
var testEnv *envtest.Environment
var ctx, testManagerCancelFunc = context.WithCancel(ctrl.SetupSignalHandler())

func TestDPUService(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "dpuservice", "crd", "bases"),
			filepath.Join("..", "..", "..", "config", "servicechainset", "crd", "bases"),
			filepath.Join("..", "..", "..", "config", "provisioning", "crd", "bases"),
			filepath.Join("..", "..", "..", "deploy", "helm", "dpf-operator", "templates", "crds"),
			filepath.Join("..", "..", "..", "test", "objects", "crd", "argocd"),
			filepath.Join("..", "..", "..", "test", "objects", "crd", "kamaji"),
		},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.29.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = dpuservicev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = provisioningv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = argov1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = operatorv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	s := scheme.Scheme
	// +kubebuilder:scaffold:scheme

	testClient, err = client.New(cfg, client.Options{Scheme: s})
	Expect(err).NotTo(HaveOccurred())
	Expect(testClient).NotTo(BeNil())

	By("setting up and running the test reconciler")
	testManager, err := ctrl.NewManager(cfg,
		ctrl.Options{
			Scheme: scheme.Scheme,
			// Set metrics server bind address to 0 to disable it.
			Metrics: server.Options{
				BindAddress: "0",
			}})
	Expect(err).ToNot(HaveOccurred())

	err = (&DPUServiceReconciler{
		Client: testClient,
		Scheme: testManager.GetScheme(),
	}).SetupWithManager(ctx, testManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&DPUDeploymentReconciler{
		Client: testClient,
		Scheme: testManager.GetScheme(),
	}).SetupWithManager(testManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&DPUServiceCredentialRequestReconciler{
		Client: testClient,
		Scheme: testManager.GetScheme(),
	}).SetupWithManager(testManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = testManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if testManagerCancelFunc != nil {
		testManagerCancelFunc()
	}
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
