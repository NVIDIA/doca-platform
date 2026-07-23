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

package controller

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DPUCluster Controller", func() {
	Context("When reconciling a resource", func() {
		var testNS *corev1.Namespace
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(k8sClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, testNS)
		})

		It("should successfully reconcile a resource with dummy handler", func() {
			controllerReconciler := &DPUClusterReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				rvCache:        make(map[types.NamespacedName]int64),
				ClusterHandler: &dummyHandler{},
			}

			By("Creating the resource")
			dpuCluster := getMinimalDPUCluster(testNS.Name)
			dpuCluster.Spec.Type = "kamaji"
			dpuCluster.Spec.MaxNodes = 1000
			dpuCluster.Spec.ClusterEndpoint = &provisioningv1.ClusterEndpointSpec{
				Keepalived: &provisioningv1.KeepalivedSpec{
					Interface:       "mock",
					VIP:             "10.10.10.10",
					VirtualRouterID: 1,
				},
			}
			Expect(k8sClient.Create(ctx, dpuCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, dpuCluster)

			By("Reconciling the created resource")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(dpuCluster),
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})

		It("persists status fields changed by the cluster handler without returned conditions", func() {
			clusterHandler := &dummyHandler{
				handlerType: "kamaji",
				reconcileCluster: func(_ context.Context, dc *provisioningv1.DPUCluster) (string, []metav1.Condition, error) {
					dc.Status.EtcdEncryptionAtRest = &provisioningv1.DPUClusterEtcdEncryptionAtRestStatus{
						Provider: "staticKey",
					}
					return "", nil, nil
				},
			}
			controllerReconciler := &DPUClusterReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				rvCache:        make(map[types.NamespacedName]int64),
				ClusterHandler: clusterHandler,
			}
			dpuCluster := getMinimalDPUCluster(testNS.Name)
			dpuCluster.Spec.Type = clusterHandler.Type()
			dpuCluster.Spec.MaxNodes = 1000
			Expect(k8sClient.Create(ctx, dpuCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupWithFinalizerRemovalAndWait, ctx, k8sClient, dpuCluster)
			dpuCluster.Status.Phase = provisioningv1.PhaseCreating
			Expect(k8sClient.Status().Update(ctx, dpuCluster)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(dpuCluster),
			})
			Expect(err).NotTo(HaveOccurred())

			current := &provisioningv1.DPUCluster{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuCluster), current)).To(Succeed())
			Expect(current.Status.EtcdEncryptionAtRest).NotTo(BeNil())
			Expect(current.Status.EtcdEncryptionAtRest.Provider).To(Equal("staticKey"))
		})

		It("persists status fields changed by the cluster handler when kubeconfig is also updated", func() {
			clusterHandler := &dummyHandler{
				handlerType: "kamaji",
				reconcileCluster: func(_ context.Context, dc *provisioningv1.DPUCluster) (string, []metav1.Condition, error) {
					dc.Status.EtcdEncryptionAtRest = &provisioningv1.DPUClusterEtcdEncryptionAtRestStatus{
						Provider: "vaultKMS",
					}
					return "admin-kubeconfig", nil, nil
				},
			}
			controllerReconciler := &DPUClusterReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				rvCache:        make(map[types.NamespacedName]int64),
				ClusterHandler: clusterHandler,
			}
			dpuCluster := getMinimalDPUCluster(testNS.Name)
			dpuCluster.Spec.Type = clusterHandler.Type()
			dpuCluster.Spec.MaxNodes = 1000
			Expect(k8sClient.Create(ctx, dpuCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupWithFinalizerRemovalAndWait, ctx, k8sClient, dpuCluster)
			dpuCluster.Status.Phase = provisioningv1.PhaseCreating
			Expect(k8sClient.Status().Update(ctx, dpuCluster)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(dpuCluster),
			})
			Expect(err).NotTo(HaveOccurred())

			current := &provisioningv1.DPUCluster{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuCluster), current)).To(Succeed())
			Expect(current.Spec.Kubeconfig).To(Equal("admin-kubeconfig"))
			Expect(current.Status.EtcdEncryptionAtRest).NotTo(BeNil())
			Expect(current.Status.EtcdEncryptionAtRest.Provider).To(Equal("vaultKMS"))
		})

		It("should fail to create a resource with name exceeding the maximum length", func() {
			By("Creating the resource")
			dpuCluster := getMinimalDPUCluster(testNS.Name)
			dpuCluster.Name = utilrand.String(64)
			Expect(k8sClient.Create(ctx, dpuCluster)).To(HaveOccurred())
		})
	})
})

type dummyHandler struct {
	handlerType      string
	reconcileCluster func(context.Context, *provisioningv1.DPUCluster) (string, []metav1.Condition, error)
}

func (h *dummyHandler) ReconcileCluster(ctx context.Context, dc *provisioningv1.DPUCluster) (string, []metav1.Condition, error) {
	if h.reconcileCluster != nil {
		return h.reconcileCluster(ctx, dc)
	}
	return "", nil, nil
}

func (h *dummyHandler) CleanUpCluster(_ context.Context, _ *provisioningv1.DPUCluster) (bool, error) {
	return true, nil
}

func (h *dummyHandler) DPFOperatorConfigToDPUClusters(_ context.Context, _ client.Object) []reconcile.Request {
	return nil
}

func (h *dummyHandler) Type() string {
	if h.handlerType != "" {
		return h.handlerType
	}
	return "dummy"
}

// getMinimalDPUCluster returns a DPUCluster that can be applied as is, without any required field needed.
func getMinimalDPUCluster(namespace string) *provisioningv1.DPUCluster {
	return &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dpucluster-",
			Namespace:    namespace,
		},
		Spec: provisioningv1.DPUClusterSpec{
			Type: "static",
		},
	}
}
