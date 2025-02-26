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

package dpfctl

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	argov1 "github.com/nvidia/doca-platform/internal/argocd/api/application/v1alpha1"

	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	testClient client.Client
	testEnv    *envtest.Environment
)

func TestMain(m *testing.M) {
	setupLogger := ctrl.Log.WithName("dpfctl-test-setup")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "deploy", "helm", "dpf-operator", "templates", "crds"),
			filepath.Join("..", "..", "test", "objects", "crd", "argocd"),
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

	var err error

	s := scheme.Scheme

	if err := operatorv1.AddToScheme(scheme.Scheme); err != nil {
		panic(fmt.Sprintf("Failed to add DPFOperatorv1 scheme: %v", err))
	}
	if err := dpuservicev1.AddToScheme(scheme.Scheme); err != nil {
		panic(fmt.Sprintf("Failed to add DPUservice v1 scheme: %v", err))
	}
	if err := provisioningv1.AddToScheme(scheme.Scheme); err != nil {
		panic(fmt.Sprintf("Failed to add Provisioning v1 scheme: %v", err))
	}
	if err := argov1.AddToScheme(scheme.Scheme); err != nil {
		panic(fmt.Sprintf("Failed to add Argo scheme: %v", err))
	}

	// cfg is defined in this file globally in this package. This allows the resource collector to use this
	// config when it runs.
	cfg, err := testEnv.Start()
	if err != nil {
		panic(fmt.Sprintf("Failed to set up test environment: %v", err))
	}

	// testClient is defined globally in this package so it can be used by the resource.
	testClient, err = client.New(cfg, client.Options{Scheme: s})
	if err != nil {
		panic(fmt.Sprintf("Failed to create client: %v", err))
	}

	// run the test suite in this package.
	if code := m.Run(); code != 0 {
		setupLogger.Info("error running tests: ", code)
	}

	if err := testEnv.Stop(); err != nil {
		panic(fmt.Sprintf("Failed to stop test environment: %v", err))
	}
}
