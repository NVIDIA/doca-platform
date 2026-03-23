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

package resourcehelper

import (
	"context"
	"errors"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var (
	errTest = errors.New("test error")
)

const (
	testNamespace     = "test-namespace"
	testDPUCluster    = "test-dpu-cluster"
	testConfigMapName = "test-configmap"
	testFinalizer     = "test.finalizer"
)

func getTestConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigMapName,
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"key": "value",
		},
	}
}

func getDPUCluster(name string) *provisioningv1.DPUCluster {
	return &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
	}
}

func getFakeClientBuilder() *fake.ClientBuilder {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme)
}

var _ = Describe("DeleteResourceInDPUClusters", func() {
	var (
		ctx               context.Context
		dpuClient         client.Client
		dpuCluster        *provisioningv1.DPUCluster
		dpuClusterClients []dpuclusterhelper.ClientForDPUCluster
		configMap         *corev1.ConfigMap
		objectKey         client.ObjectKey
	)
	BeforeEach(func() {
		ctx = context.Background()
		dpuClient = getFakeClientBuilder().Build()
		dpuCluster = getDPUCluster(testDPUCluster)
		dpuClusterClients = []dpuclusterhelper.ClientForDPUCluster{{DPUCluster: dpuCluster, Client: dpuClient}}
		configMap = getTestConfigMap()
		objectKey = client.ObjectKey{Name: testConfigMapName, Namespace: testNamespace}
	})
	Context("when resource does not exist", func() {
		It("should succeed and return completed result", func() {
			result, err := DeleteResourceInDPUClusters(ctx, dpuClusterClients, "ConfigMap",
				objectKey, configMap, []string{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeTrue())
			Expect(result.Reason).To(BeEmpty())
		})
	})
	Context("when resource exists and is not being deleted", func() {
		BeforeEach(func() {
			dpuClient = getFakeClientBuilder().WithObjects(configMap).Build()
			dpuClusterClients = []dpuclusterhelper.ClientForDPUCluster{{DPUCluster: dpuCluster, Client: dpuClient}}
		})
		It("should mark resource for deletion", func() {
			result, err := DeleteResourceInDPUClusters(ctx, dpuClusterClients, "ConfigMap",
				objectKey, configMap, []string{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("is marked for removal"))
			Expect(apierrors.IsNotFound(dpuClient.Get(ctx, objectKey, &corev1.ConfigMap{}))).To(BeTrue())
		})

		It("should remove finalizers before deletion", func() {
			configMapWithFinalizer := getTestConfigMap()
			controllerutil.AddFinalizer(configMapWithFinalizer, testFinalizer)
			dpuClient = getFakeClientBuilder().WithObjects(configMapWithFinalizer).Build()
			dpuClusterClients = []dpuclusterhelper.ClientForDPUCluster{{DPUCluster: dpuCluster, Client: dpuClient}}
			result, err := DeleteResourceInDPUClusters(ctx, dpuClusterClients, "ConfigMap",
				objectKey, configMap, []string{testFinalizer})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("is marked for removal"))
			Expect(apierrors.IsNotFound(dpuClient.Get(ctx, objectKey, &corev1.ConfigMap{}))).To(BeTrue())
		})

		It("should return error when deletion fails", func() {
			failingClient := getFakeClientBuilder().
				WithObjects(getTestConfigMap()).
				WithInterceptorFuncs(interceptor.Funcs{
					Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						return errTest
					},
				}).
				Build()

			failingClusterClients := []dpuclusterhelper.ClientForDPUCluster{{DPUCluster: dpuCluster, Client: failingClient}}
			_, err := DeleteResourceInDPUClusters(ctx, failingClusterClients, "ConfigMap",
				objectKey, configMap, []string{})
			Expect(err).To(MatchError(errTest))
		})

		It("should handle NotFound error during deletion gracefully", func() {
			failingClient := getFakeClientBuilder().
				WithObjects(getTestConfigMap()).
				WithInterceptorFuncs(interceptor.Funcs{
					Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						return apierrors.NewNotFound(corev1.Resource("configmaps"), testConfigMapName)
					},
				}).
				Build()

			failingClusterClients := []dpuclusterhelper.ClientForDPUCluster{{DPUCluster: dpuCluster, Client: failingClient}}
			result, err := DeleteResourceInDPUClusters(ctx, failingClusterClients, "ConfigMap",
				objectKey, configMap, []string{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeTrue())
			Expect(result.Reason).To(BeEmpty())
		})
	})

	Context("when resource is already being deleted", func() {
		It("should not attempt deletion when resource is already being deleted", func() {
			deletionAttempted := false
			finalizerRemoved := false

			testCM := getTestConfigMap()
			testCM.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			controllerutil.AddFinalizer(testCM, testFinalizer)

			interceptorClient := getFakeClientBuilder().
				WithObjects(testCM).
				WithInterceptorFuncs(interceptor.Funcs{
					Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						finalizerRemoved = true
						return nil
					},
					Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						deletionAttempted = true
						return nil
					},
				}).
				Build()

			interceptorClusterClients := []dpuclusterhelper.ClientForDPUCluster{{DPUCluster: dpuCluster, Client: interceptorClient}}
			result, err := DeleteResourceInDPUClusters(ctx, interceptorClusterClients, "ConfigMap",
				objectKey, configMap, []string{testFinalizer})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("not removed yet"))
			Expect(deletionAttempted).To(BeFalse())
			Expect(finalizerRemoved).To(BeTrue())
		})

		It("should return error when finalizer removal fails", func() {
			testCM := getTestConfigMap()
			controllerutil.AddFinalizer(testCM, testFinalizer)
			testCM.DeletionTimestamp = &metav1.Time{}

			failingClient := getFakeClientBuilder().
				WithObjects(testCM).
				WithInterceptorFuncs(interceptor.Funcs{
					Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						return errTest
					},
				}).
				Build()

			failingClusterClients := []dpuclusterhelper.ClientForDPUCluster{{DPUCluster: dpuCluster, Client: failingClient}}
			_, err := DeleteResourceInDPUClusters(ctx, failingClusterClients, "ConfigMap",
				objectKey, configMap, []string{testFinalizer})
			Expect(err).To(MatchError(errTest))
		})
	})

	Context("when working with multiple DPU clusters", func() {
		var (
			secondDPUClient     client.Client
			secondDPUCluster    *provisioningv1.DPUCluster
			multiClusterClients []dpuclusterhelper.ClientForDPUCluster
		)

		BeforeEach(func() {
			secondDPUClient = getFakeClientBuilder().Build()
			secondDPUCluster = getDPUCluster("second-dpu-cluster")
			multiClusterClients = []dpuclusterhelper.ClientForDPUCluster{
				{DPUCluster: dpuCluster, Client: dpuClient},
				{DPUCluster: secondDPUCluster, Client: secondDPUClient},
			}
		})

		It("should delete resource from all clusters where it exists", func() {
			// Setup both clusters with the resource
			configMap1 := getTestConfigMap()
			configMap2 := getTestConfigMap()
			dpuClient = getFakeClientBuilder().WithObjects(configMap1).Build()
			secondDPUClient = getFakeClientBuilder().WithObjects(configMap2).Build()
			multiClusterClients = []dpuclusterhelper.ClientForDPUCluster{
				{DPUCluster: dpuCluster, Client: dpuClient},
				{DPUCluster: secondDPUCluster, Client: secondDPUClient},
			}
			result, err := DeleteResourceInDPUClusters(ctx, multiClusterClients, "ConfigMap",
				objectKey, configMap, []string{})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("ConfigMap"))
			Expect(result.Reason).To(ContainSubstring("is marked for removal"))

			// Verify resources were deleted in both clusters (fake client deletes immediately when no finalizers)
			var cm1, cm2 corev1.ConfigMap
			err = dpuClient.Get(ctx, objectKey, &cm1)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			err = secondDPUClient.Get(ctx, objectKey, &cm2)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("should handle mixed scenarios across clusters", func() {
			// Setup resource only in first cluster
			configMap1 := getTestConfigMap()
			dpuClient = getFakeClientBuilder().WithObjects(configMap1).Build()
			multiClusterClients = []dpuclusterhelper.ClientForDPUCluster{
				{DPUCluster: dpuCluster, Client: dpuClient},
				{DPUCluster: secondDPUCluster, Client: secondDPUClient},
			}

			result, err := DeleteResourceInDPUClusters(ctx, multiClusterClients, "ConfigMap",
				objectKey, configMap, []string{})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("ConfigMap"))
			Expect(result.Reason).To(ContainSubstring("is marked for removal"))
			// Should only contain one cluster reference since resource only exists in first cluster
			Expect(result.Reason).To(ContainSubstring(testDPUCluster))
		})

		It("should return error when deletion from one cluster fails", func() {
			// Setup resources in both clusters
			configMap1 := getTestConfigMap()
			dpuClient = getFakeClientBuilder().WithObjects(configMap1).Build()

			// Make first cluster fail
			failingClient := getFakeClientBuilder().
				WithObjects(configMap1).
				WithInterceptorFuncs(interceptor.Funcs{
					Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						return errTest
					},
				}).
				Build()
			mixedClusterClients := []dpuclusterhelper.ClientForDPUCluster{
				{DPUCluster: dpuCluster, Client: dpuClient},
				{DPUCluster: secondDPUCluster, Client: failingClient},
			}
			_, err := DeleteResourceInDPUClusters(ctx, mixedClusterClients, "ConfigMap",
				objectKey, configMap, []string{})
			Expect(err).To(MatchError(errTest))
		})
	})
	Context("when Get operation fails", func() {
		It("should return error when Get fails with non-NotFound error", func() {
			failingClient := getFakeClientBuilder().
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return errTest
					},
				}).
				Build()

			failingClusterClients := []dpuclusterhelper.ClientForDPUCluster{
				{DPUCluster: dpuCluster, Client: failingClient},
			}
			_, err := DeleteResourceInDPUClusters(ctx, failingClusterClients, "ConfigMap",
				objectKey, configMap, []string{})
			Expect(err).To(MatchError(errTest))
		})
	})
})

var _ = Describe("WaitForResourceRemovalInDPUClusters", func() {
	var (
		ctx               context.Context
		dpuClient         client.Client
		dpuCluster        *provisioningv1.DPUCluster
		dpuClusterClients []dpuclusterhelper.ClientForDPUCluster
		configMap         *corev1.ConfigMap
		objectKey         client.ObjectKey
	)
	BeforeEach(func() {
		ctx = context.Background()
		dpuClient = getFakeClientBuilder().Build()
		dpuCluster = getDPUCluster(testDPUCluster)
		dpuClusterClients = []dpuclusterhelper.ClientForDPUCluster{{DPUCluster: dpuCluster, Client: dpuClient}}
		configMap = getTestConfigMap()
		objectKey = client.ObjectKey{Name: testConfigMapName, Namespace: testNamespace}
	})
	Context("when resource does not exist", func() {
		It("should succeed and return completed result", func() {
			result, err := WaitForResourceRemovalInDPUClusters(ctx, dpuClusterClients, "ConfigMap",
				objectKey, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeTrue())
			Expect(result.Reason).To(BeEmpty())
		})
	})
	Context("when resource still exists", func() {
		BeforeEach(func() {
			dpuClient = getFakeClientBuilder().WithObjects(configMap).Build()
			dpuClusterClients = []dpuclusterhelper.ClientForDPUCluster{{DPUCluster: dpuCluster, Client: dpuClient}}
		})
		It("should return not completed", func() {
			result, err := WaitForResourceRemovalInDPUClusters(ctx, dpuClusterClients, "ConfigMap",
				objectKey, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("not removed yet"))
		})
		It("should not attempt deletion", func() {
			deletionAttempted := false
			interceptorClient := getFakeClientBuilder().
				WithObjects(getTestConfigMap()).
				WithInterceptorFuncs(interceptor.Funcs{
					Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						deletionAttempted = true
						return nil
					},
				}).
				Build()
			interceptorClusterClients := []dpuclusterhelper.ClientForDPUCluster{{DPUCluster: dpuCluster, Client: interceptorClient}}
			_, err := WaitForResourceRemovalInDPUClusters(ctx, interceptorClusterClients, "ConfigMap",
				objectKey, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(deletionAttempted).To(BeFalse())
		})
	})
	Context("when working with multiple DPU clusters", func() {
		var (
			secondDPUClient     client.Client
			secondDPUCluster    *provisioningv1.DPUCluster
			multiClusterClients []dpuclusterhelper.ClientForDPUCluster
		)

		BeforeEach(func() {
			secondDPUClient = getFakeClientBuilder().Build()
			secondDPUCluster = getDPUCluster("second-dpu-cluster")
			multiClusterClients = []dpuclusterhelper.ClientForDPUCluster{
				{DPUCluster: dpuCluster, Client: dpuClient},
				{DPUCluster: secondDPUCluster, Client: secondDPUClient},
			}
		})

		It("should return completed when resource is gone from all clusters", func() {
			result, err := WaitForResourceRemovalInDPUClusters(ctx, multiClusterClients, "ConfigMap",
				objectKey, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeTrue())
			Expect(result.Reason).To(BeEmpty())
		})

		It("should return not completed when resource exists in some clusters", func() {
			configMap1 := getTestConfigMap()
			dpuClient = getFakeClientBuilder().WithObjects(configMap1).Build()
			multiClusterClients = []dpuclusterhelper.ClientForDPUCluster{
				{DPUCluster: dpuCluster, Client: dpuClient},
				{DPUCluster: secondDPUCluster, Client: secondDPUClient},
			}

			result, err := WaitForResourceRemovalInDPUClusters(ctx, multiClusterClients, "ConfigMap",
				objectKey, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Completed).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring(testDPUCluster))
			Expect(result.Reason).To(ContainSubstring("not removed yet"))
		})

		It("should return error when Get fails in one cluster", func() {
			failingClient := getFakeClientBuilder().
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return errTest
					},
				}).
				Build()
			mixedClusterClients := []dpuclusterhelper.ClientForDPUCluster{
				{DPUCluster: dpuCluster, Client: dpuClient},
				{DPUCluster: secondDPUCluster, Client: failingClient},
			}
			_, err := WaitForResourceRemovalInDPUClusters(ctx, mixedClusterClients, "ConfigMap",
				objectKey, configMap)
			Expect(err).To(MatchError(errTest))
		})
	})
	Context("when Get operation fails", func() {
		It("should return error when Get fails with non-NotFound error", func() {
			failingClient := getFakeClientBuilder().
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return errTest
					},
				}).
				Build()

			failingClusterClients := []dpuclusterhelper.ClientForDPUCluster{
				{DPUCluster: dpuCluster, Client: failingClient},
			}
			_, err := WaitForResourceRemovalInDPUClusters(ctx, failingClusterClients, "ConfigMap",
				objectKey, configMap)
			Expect(err).To(MatchError(errTest))
		})
	})
})
