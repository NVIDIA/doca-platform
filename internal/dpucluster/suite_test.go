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

package controlplane

import (
	"context"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type testEnv struct {
	scheme *runtime.Scheme
	obj    []client.Object
}

// FakeKubeClientOption defines options to construct a fake kube client.
type FakeKubeClientOption func(testEnv *testEnv)

func withObjects(obj ...client.Object) FakeKubeClientOption {
	return func(testEnv *testEnv) {
		testEnv.obj = obj
	}
}

func (t *testEnv) fakeKubeClient(opts ...FakeKubeClientOption) client.Client {
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
	ctx = context.Background()
	env *testEnv
)

func TestControlPlane(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Control Plane Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	scheme := runtime.NewScheme()
	_ = provisioningv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	env = &testEnv{
		scheme: scheme,
	}
})
