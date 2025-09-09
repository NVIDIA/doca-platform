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

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeClientPkg "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	callTimeout = time.Second * 10
)

var (
	errTest = errors.New("test error")
)

func TestController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

type fakeClusterHelper struct {
	Error  error
	Client client.Client
}

func (f *fakeClusterHelper) Run(ctx context.Context) (_ error) {
	return nil
}

func (f *fakeClusterHelper) Wait(ctx context.Context) (_ error) {
	return nil
}

func (f *fakeClusterHelper) GetClient(ctx context.Context) (client.Client, error) {
	return f.Client, f.Error
}

// getClusterClient returns fully configured client for the cluster
func getClusterClient(initObjs ...client.Object) client.Client {
	return getClusterClientBuilder(initObjs...).Build()
}

// getClusterClientBuilder returns a new client builder for the cluster that can be used to additionally configure the client
func getClusterClientBuilder(initObjs ...client.Object) *fakeClientPkg.ClientBuilder {
	ExpectWithOffset(1, provisioningv1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	ExpectWithOffset(1, storagev1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	return fakeClientPkg.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(initObjs...).
		WithInterceptorFuncs(interceptor.Funcs{Create: func(
			ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if obj.GetUID() == "" {
				obj.SetUID(types.UID(uuid.NewString()))
			}
			return client.Create(ctx, obj, opts...)
		}}).
		WithStatusSubresource(&provisioningv1.DPU{}, &storagev1.DPUVolume{}, &storagev1.DPUVolumeAttachment{}).
		WithIndex(&provisioningv1.DPU{}, "spec.dpuNodeName", func(o client.Object) []string {
			return []string{o.(*provisioningv1.DPU).Spec.DPUNodeName}
		})
}
