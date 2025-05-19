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

package util

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg        *rest.Config
	k8sClient  client.Client
	clientset  *kubernetes.Clientset
	testEnv    *envtest.Environment
	ctx        context.Context
	cancel     context.CancelFunc
	testClient client.Client
)

func TestMain(m *testing.M) {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.TODO())

	// Setup the test environment
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "..", "config", "provisioning", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	if err != nil {
		logf.Log.Error(err, "Failed to start test environment")
		os.Exit(1)
	}

	err = provisioningv1.AddToScheme(testEnv.Scheme)
	if err != nil {
		logf.Log.Error(err, "Failed to add provisioning scheme")
		os.Exit(1)
	}

	k8sClient, err = client.New(cfg, client.Options{Scheme: testEnv.Scheme})
	if err != nil {
		logf.Log.Error(err, "Failed to create k8s client")
		os.Exit(1)
	}

	clientset, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		logf.Log.Error(err, "Failed to create clientset")
		os.Exit(1)
	}

	testClient = k8sClient

	// Run the tests
	code := m.Run()

	// Teardown
	cancel()
	err = testEnv.Stop()
	if err != nil {
		logf.Log.Error(err, "Failed to stop test environment")
	}

	os.Exit(code)
}
