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
	"fmt"
	"math/rand"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
)

// These tests are written in BDD-style using Ginkgo framework. Refer to
// http://onsi.github.io/ginkgo to learn more.
var _ = Describe("Allocator", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		testNS *corev1.Namespace
		alloc  allocator.Allocator
		rnd    *rand.Rand
	)

	var createDPU = func(name string, dpuClusterSelector *metav1.LabelSelector) *provisioningv1.DPU {
		return &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUSpec{
				SerialNumber:  "MT25066004C" + utilrand.String(5),
				DPUNodeName:   "test-node",
				DPUDeviceName: "test-dpudevice",
				BFB:           "test-bfb",
				PCIAddress:    ptr.To("0000-4b-00"),
				DPUFlavor:     "test-flavor",
				Cluster: provisioningv1.K8sCluster{
					ClusterSpec: provisioningv1.ClusterSpec{
						Selector: dpuClusterSelector,
					},
				},
			},
		}
	}

	var createDPUCluster = func(name string, maxNode int, ready bool, labels map[string]string) *provisioningv1.DPUCluster {
		dc := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
				UID:       types.UID(fmt.Sprintf("%d", rnd.Int())),
				Labels:    labels,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type:     string(provisioningv1.KamajiCluster),
				MaxNodes: maxNode,
			},
		}
		if !ready {
			return dc
		}
		dc.Spec.Kubeconfig = "test-admin-kubeconfig"
		dc.Status.Phase = provisioningv1.PhaseReady
		dc.Status.Conditions = append(dc.Status.Conditions, []metav1.Condition{
			{
				Type:   string(provisioningv1.ConditionCreated),
				Status: metav1.ConditionTrue,
			},
			{
				Type:   string(provisioningv1.ConditionReady),
				Status: metav1.ConditionTrue,
			},
		}...)
		return dc
	}

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.TODO(), 20*time.Second)
		rnd = rand.New(rand.NewSource(time.Now().Unix()))

		By("creating the namespace")
		// Notes:
		// 1. Namespace usage limitation:
		// EnvTest does not support namespace deletion. Deleting a namespace will seem to succeed,
		// but the namespace will just be put in a Terminating state, and never actually be reclaimed.
		// See: https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
		// 2. the value in GenerateName is not defined as a constant intentionally,
		// because it shouldn't be referenced directly.
		// 3. testNS is the only way to reference the namespace in the test.
		// 4. always create a new namespace for each test, never reuse an existing namespace
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "allocator-test"}}
		Eventually(func() error {
			return k8sClient.Create(ctx, testNS)
		}).WithTimeout(10 * time.Second).Should(Succeed())

		By("creating allocator")
		alloc = allocator.NewAllocator(k8sClient)
	})

	AfterEach(func() {
		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
		cancel()
	})

	Context("obj test context", func() {
		It("allocate cluster", func() {
			dpu := createDPU("dpu", nil)
			Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
			dc := createDPUCluster("dc", 1, true, nil)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc)))
		})
		It("allocate DPU", func() {
			dpu0 := createDPU("dpu0", nil)
			Expect(k8sClient.Create(context.TODO(), dpu0)).To(Succeed())
			dc1 := createDPUCluster("dc1", 20, true, nil)
			alloc.SaveCluster(dc1)
			// allocate the first DPU to dc1
			result, err := alloc.Allocate(ctx, dpu0)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc1.Name, Namespace: dc1.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu0), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc1)))
			Expect(alloc.GetDPUsCount(dc1)).To(Equal(1))

			// create another DPUCluster
			dc2 := createDPUCluster("dc2", 20, true, nil)
			alloc.SaveCluster(dc2)

			// create 9 DPUs and allocate them
			for i := 0; i < 9; i++ {
				dpu := createDPU(fmt.Sprintf("dpu-%d", i), nil)
				Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
				result, err = alloc.Allocate(ctx, dpu)
				Expect(err).To(Succeed())
				Expect(result).To(Equal(allocator.AllocateResult{Name: dc1.Name, Namespace: dc1.Namespace}))
			}
			// 10 DPUs should be allocated to dc1
			Expect(alloc.GetDPUsCount(dc1)).To(Equal(10))
		})
		It("allocate DPU without DPUClusterSelector, create two DPUClusters before allocating DPUs", func() {
			dc1 := createDPUCluster("dc1", 20, true, nil)
			alloc.SaveCluster(dc1)
			dc2 := createDPUCluster("dc2", 20, true, nil)
			alloc.SaveCluster(dc2)

			// create 20 DPUs and allocate them
			for i := 0; i < 20; i++ {
				dpu := createDPU(fmt.Sprintf("dpu-%d", i), nil)
				Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
				_, err := alloc.Allocate(ctx, dpu)
				Expect(err).To(Succeed())
			}
			count1 := alloc.GetDPUsCount(dc1)
			count2 := alloc.GetDPUsCount(dc2)

			// either dc1 has 20 DPUs and dc2 has 0 DPUs, or dc1 has 0 DPUs and dc2 has 20 DPUs
			Expect((count1 == 20 && count2 == 0) || (count1 == 0 && count2 == 20)).To(BeTrue())
		})
		It("allocate DPU without DPUClusterSelector, fulfill one cluster then allocate DPUs to another cluster", func() {
			dpu0 := createDPU("dpu0", nil)
			Expect(k8sClient.Create(context.TODO(), dpu0)).To(Succeed())
			dc1 := createDPUCluster("dc1", 10, true, nil)
			alloc.SaveCluster(dc1)
			// allocate the first DPU to dc1
			result, err := alloc.Allocate(ctx, dpu0)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc1.Name, Namespace: dc1.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu0), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc1)))
			Expect(alloc.GetDPUsCount(dc1)).To(Equal(1))

			// create another DPUCluster
			dc2 := createDPUCluster("dc2", 10, true, nil)
			alloc.SaveCluster(dc2)

			// create 14 DPUs and allocate them
			for i := 0; i < 14; i++ {
				dpu := createDPU(fmt.Sprintf("dpu-%d", i), nil)
				Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
				_, err = alloc.Allocate(ctx, dpu)
				Expect(err).To(Succeed())
			}
			// 10 DPUs should be allocated to dc1
			Expect(alloc.GetDPUsCount(dc1)).To(Equal(10))
			// 5 DPUs should be allocated to dc2
			Expect(alloc.GetDPUsCount(dc2)).To(Equal(5))
		})
		It("allocate DPU with DPUClusterSelector", func() {
			dc1 := createDPUCluster("dc", 10, true, map[string]string{"cluster-name": "test-cluster-1"})
			dc2 := createDPUCluster("dc", 10, true, map[string]string{"cluster-name": "test-cluster-2"})
			alloc.SaveCluster(dc1)
			alloc.SaveCluster(dc2)

			// allocate 3 DPUs to dc1
			for i := 0; i < 3; i++ {
				dpu := createDPU(fmt.Sprintf("dpu-%d", i), &metav1.LabelSelector{MatchLabels: map[string]string{"cluster-name": "test-cluster-1"}})
				Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
				result, err := alloc.Allocate(ctx, dpu)
				Expect(err).To(Succeed())
				Expect(result).To(Equal(allocator.AllocateResult{Name: dc1.Name, Namespace: dc1.Namespace}))
			}
			// allocate 7 DPUs to dc2
			for i := 3; i < 10; i++ {
				dpu := createDPU(fmt.Sprintf("dpu-%d", i), &metav1.LabelSelector{MatchLabels: map[string]string{"cluster-name": "test-cluster-2"}})
				Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
				result, err := alloc.Allocate(ctx, dpu)
				Expect(err).To(Succeed())
				Expect(result).To(Equal(allocator.AllocateResult{Name: dc2.Name, Namespace: dc2.Namespace}))
			}
		})
		It("allocate DPU with DPUClusterSelector, fulfill DPUClusters according to the label selector", func() {
			dc1 := createDPUCluster("dc", 10, true, map[string]string{"cluster-name": "test-cluster"})
			dc2 := createDPUCluster("dc", 10, true, map[string]string{"cluster-name": "test-cluster"})
			dc3 := createDPUCluster("dc", 10, true, map[string]string{"cluster-name": "test-cluster-3"})
			alloc.SaveCluster(dc1)
			alloc.SaveCluster(dc2)
			alloc.SaveCluster(dc3)

			// create 20 DPUs and allocate them to cluster1 and cluster2
			for i := 0; i < 20; i++ {
				dpu := createDPU(fmt.Sprintf("dpu-%d", i), &metav1.LabelSelector{MatchLabels: map[string]string{"cluster-name": "test-cluster"}})
				Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
				_, err := alloc.Allocate(ctx, dpu)
				Expect(err).To(Succeed())
			}

			// cluster1 and cluster2 are full, so the remaining 10 DPUs should be failed to allocate
			for i := 20; i < 30; i++ {
				dpu := createDPU(fmt.Sprintf("dpu-%d", i), &metav1.LabelSelector{MatchLabels: map[string]string{"cluster-name": "test-cluster"}})
				Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
				_, err := alloc.Allocate(ctx, dpu)
				Expect(err).NotTo(Succeed())
			}
			Expect(alloc.GetDPUsCount(dc1)).To(Equal(10))
			Expect(alloc.GetDPUsCount(dc2)).To(Equal(10))
			Expect(alloc.GetDPUsCount(dc3)).To(Equal(0))
		})
		It("allocate DPU with DPUClusterSelector, fulfill DPUClusters according to the match expressions", func() {
			dc1 := createDPUCluster("dc", 10, true, map[string]string{"cluster-name": "test-cluster-1"})
			dc2 := createDPUCluster("dc", 10, true, map[string]string{"cluster-name": "test-cluster-2"})
			dc3 := createDPUCluster("dc", 10, true, map[string]string{"cluster-name": "test-cluster-3"})
			alloc.SaveCluster(dc1)
			alloc.SaveCluster(dc2)
			alloc.SaveCluster(dc3)

			// create 30 DPUs and allocate them to cluster1 and cluster2
			for i := 0; i < 30; i++ {
				dpu := createDPU(fmt.Sprintf("dpu-%d", i), &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "cluster-name",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"test-cluster-1", "test-cluster-2", "test-cluster-3", "non-existing-cluster"},
					},
				}})
				Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
				_, err := alloc.Allocate(ctx, dpu)
				Expect(err).To(Succeed())
			}
			Expect(alloc.GetDPUsCount(dc1)).To(Equal(10))
			Expect(alloc.GetDPUsCount(dc2)).To(Equal(10))
			Expect(alloc.GetDPUsCount(dc3)).To(Equal(10))
		})
		It("cluster not ready", func() {
			dpu := createDPU("dpu", nil)
			Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
			dc := createDPUCluster("dc", 1, false, nil)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(types.NamespacedName{}))
		})
		It("no cluster", func() {
			dpu := createDPU("dpu", nil)
			Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
			result, err := alloc.Allocate(ctx, dpu)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(types.NamespacedName{}))
		})
		It("reach max node limit", func() {
			dpu1 := createDPU("dpu1", nil)
			Expect(k8sClient.Create(context.TODO(), dpu1)).To(Succeed())
			dc := createDPUCluster("dc", 1, true, nil)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu1)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu1), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc)))

			dpu2 := createDPU("dpu2", nil)
			Expect(k8sClient.Create(context.TODO(), dpu2)).To(Succeed())
			result, err = alloc.Allocate(ctx, dpu2)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu2), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(types.NamespacedName{}))
		})
		It("release DPU", func() {
			dpu1 := createDPU("dpu1", nil)
			Expect(k8sClient.Create(context.TODO(), dpu1)).To(Succeed())
			dc := createDPUCluster("dc", 1, true, nil)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu1)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu1), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc)))

			dpu2 := createDPU("dpu2", nil)
			Expect(k8sClient.Create(context.TODO(), dpu2)).To(Succeed())
			result, err = alloc.Allocate(ctx, dpu2)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu2), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(types.NamespacedName{}))

			alloc.ReleaseDPU(dpu1)
			result, err = alloc.Allocate(ctx, dpu2)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu2), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc)))
		})
		It("update cluster status", func() {
			dpu := createDPU("dpu", nil)
			Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
			dc := createDPUCluster("dc", 1, false, nil)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(types.NamespacedName{}))

			dc.Status = createDPUCluster("", 1, true, nil).Status
			alloc.SaveCluster(dc)
			result, err = alloc.Allocate(ctx, dpu)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc)))
		})
		It("allocator restart - reach max node limit", func() {
			dpu1 := createDPU("dpu1", nil)
			Expect(k8sClient.Create(context.TODO(), dpu1)).To(Succeed())
			dc := createDPUCluster("dc", 1, true, nil)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu1)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu1), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc)))

			// alloc2 is a simulation of restarted allocator, which has a clean cache
			alloc2 := allocator.NewAllocator(k8sClient)
			alloc2.SaveCluster(dc)
			dpu2 := createDPU("dpu2", nil)
			Expect(k8sClient.Create(context.TODO(), dpu2)).To(Succeed())
			// in this Allocate() call, the alloc2 should find dpu1 before allocate for dpu2, meaning that the allocation for dpu2 should fail
			result, err = alloc.Allocate(ctx, dpu2)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
		})
		It("allocator restart - should success", func() {
			dpu1 := createDPU("dpu1", nil)
			Expect(k8sClient.Create(context.TODO(), dpu1)).To(Succeed())
			dc := createDPUCluster("dc", 2, true, nil)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu1)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu1), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc)))

			// alloc2 is a simulation of restarted allocator, which has a clean cache
			alloc2 := allocator.NewAllocator(k8sClient)
			alloc2.SaveCluster(dc)
			dpu2 := createDPU("dpu2", nil)
			Expect(k8sClient.Create(context.TODO(), dpu2)).To(Succeed())
			result, err = alloc.Allocate(ctx, dpu2)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu2), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc)))
		})
		It("manually assigned DPU", func() {
			dc := createDPUCluster("dc", 1, true, nil)
			// dpu1 is manually assigned by user, it does not go through the allocation procedure
			dpu1 := createDPU("dpu1", nil)
			dpu1.Spec.Cluster.Name = dc.Name
			dpu1.Spec.Cluster.Namespace = dc.Namespace
			Expect(k8sClient.Create(context.TODO(), dpu1)).To(Succeed())
			alloc.SaveCluster(dc)
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu1), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(cutil.GetNamespacedName(dc)))

			dpu2 := createDPU("dpu2", nil)
			Expect(k8sClient.Create(context.TODO(), dpu2)).To(Succeed())
			result, err := alloc.Allocate(ctx, dpu2)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu2), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).Should(Equal(types.NamespacedName{}))
		})
	})
},
)
