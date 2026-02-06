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
	"path/filepath"
	"runtime"
	"testing"
	"time"

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg                    *rest.Config
	testClient             client.Client
	testEnv                *envtest.Environment
	testCtx, testCtxCancel = context.WithCancel(ctrl.SetupSignalHandler())
	testNamespace          = "test-namespace"
)

const (
	testTimeout  = time.Second * 30
	testInterval = time.Millisecond * 250
)

func TestNodeSRIOVDevicePluginConfigControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodeSRIOVDevicePluginConfig Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			// noderesources CRDs - generated via controller-gen.
			filepath.Join("..", "..", "..", "config", "nodesriovdeviceplugin", "crd", "bases"),
			// provisioning CRDs for DPU, DPUNode objects.
			filepath.Join("..", "..", "..", "config", "provisioning", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = noderesourcesv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = provisioningv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	testClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(testClient).NotTo(BeNil())

	// Create test namespace.
	ns := &corev1.Namespace{}
	ns.Name = testNamespace
	Expect(testClient.Create(testCtx, ns)).To(Succeed())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if testCtxCancel != nil {
		testCtxCancel()
	}
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
