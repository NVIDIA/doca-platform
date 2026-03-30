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

package zerotrust

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	testEnv   *envtest.Environment
	k8sClient client.Client
	testCfg   *restclient.Config
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestZerotrustClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Zerotrust Client Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "config", "provisioning", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,

		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	testCfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(testCfg).NotTo(BeNil())

	scheme := scheme.Scheme
	err = provisioningv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())
	k8sClient, err = client.New(testCfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	Expect(testEnv.Stop()).To(Succeed())
})
