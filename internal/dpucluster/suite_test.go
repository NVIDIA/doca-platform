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

package dpucluster

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

type fakeEnv struct {
	scheme *apiruntime.Scheme
	obj    []client.Object
}

// Environment encapsulates a Kubernetes local test environment.
type Environment struct {
	ctrl.Manager
	client.Client
	Config *rest.Config

	env *envtest.Environment
}

// FakeKubeClientOption defines options to construct a fake kube client.
type FakeKubeClientOption func(testEnv *fakeEnv)

func withObjects(obj ...client.Object) FakeKubeClientOption {
	return func(testEnv *fakeEnv) {
		testEnv.obj = obj
	}
}

func (t *fakeEnv) fakeKubeClient(opts ...FakeKubeClientOption) client.Client {
	for _, o := range opts {
		o(t)
	}
	return fake.NewClientBuilder().
		WithScheme(t.scheme).
		WithObjects(t.obj...).
		WithStatusSubresource(t.obj...).
		Build()
}

var (
	cfg                        *rest.Config
	testClient                 client.Client
	testEnv                    Environment
	ctx, testManagerCancelFunc = context.WithCancel(ctrl.SetupSignalHandler())
	fakeTestEnv                *fakeEnv
)

func TestMain(m *testing.M) {
	setupLogger := ctrl.Log.WithName("dpf-cluster-cache-controller-test-setup")
	envTest := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "provisioning", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.29.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	if err := provisioningv1.AddToScheme(scheme.Scheme); err != nil {
		panic(fmt.Sprintf("Failed to add provisioning scheme: %v", err))
	}

	s := scheme.Scheme

	var err error
	cfg, err = envTest.Start()
	if err != nil {
		panic(fmt.Sprintf("Failed to set up test environment: %v", err))
	}

	testClient, err = client.New(cfg, client.Options{Scheme: s})
	if err != nil {
		panic(fmt.Sprintf("Failed to create client: %v", err))
	}

	testManager, err := ctrl.NewManager(cfg,
		ctrl.Options{
			Scheme: scheme.Scheme,
			// Set metrics server bind address to 0 to disable it.
			Metrics: server.Options{
				BindAddress: "0",
			}})
	if err != nil {
		panic(fmt.Sprintf("Failed to create manager: %v", err))
	}

	testEnv = Environment{
		Manager: testManager,
		Client:  testClient,
		Config:  cfg,
		env:     envTest,
	}

	fakeTestEnv = &fakeEnv{
		scheme: s,
	}

	go func() {
		if err := testEnv.Manager.Start(ctx); err != nil {
			panic(fmt.Sprintf("Failed to start manager: %v", err))
		}
	}()

	// run the test suite in this package.
	if code := m.Run(); code != 0 {
		setupLogger.Info("error running tests: ", code)
	}

	if testManagerCancelFunc != nil {
		testManagerCancelFunc()
	}

	if err := testEnv.env.Stop(); err != nil {
		panic(fmt.Sprintf("Failed to stop test environment: %v", err))
	}
}
