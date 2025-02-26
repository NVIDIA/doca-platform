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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DPUCluster Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		dpucluster := &provisioningv1.DPUCluster{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind DPUCluster")
			err := k8sClient.Get(ctx, typeNamespacedName, dpucluster)
			if err != nil && apierrors.IsNotFound(err) {
				resource := &provisioningv1.DPUCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: provisioningv1.DPUClusterSpec{
						Type:     "kamaji",
						MaxNodes: 1000,
						Version:  "v1.31.0",
						ClusterEndpoint: &provisioningv1.ClusterEndpointSpec{
							Keepalived: &provisioningv1.KeepalivedSpec{
								Interface:       "mock",
								VIP:             "10.10.10.10",
								VirtualRouterID: 1,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &provisioningv1.DPUCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance DPUCluster")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &DPUClusterReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				rvCache:        make(map[types.NamespacedName]int64),
				ClusterHandler: &dummyHandler{},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

type dummyHandler struct{}

func (h *dummyHandler) ReconcileCluster(_ context.Context, _ *provisioningv1.DPUCluster) (string, []metav1.Condition, error) {
	return "", nil, nil
}

func (h *dummyHandler) CleanUpCluster(_ context.Context, _ *provisioningv1.DPUCluster) (bool, error) {
	return true, nil
}

func (h *dummyHandler) Type() string {
	return "dummy"
}
