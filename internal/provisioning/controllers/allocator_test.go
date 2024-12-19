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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These tests are written in BDD-style using Ginkgo framework. Refer to
// http://onsi.github.io/ginkgo to learn more.
var _ = Describe("Allocator", func() {
	const (
		DefaultNS = "dpf-provisioning-allocator-test"
	)
	var (
		ctx    context.Context
		cancel context.CancelFunc
		testNS *corev1.Namespace
		alloc  allocator.Allocator
		rnd    *rand.Rand
	)

	var createDPU = func(name string) *provisioningv1.DPU {
		return &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUSpec{
				NodeName:   "test-node",
				BFB:        "test-bfb",
				PCIAddress: "0000-4b-00",
				DPUFlavor:  "test-flavor",
			},
		}
	}

	var createDPUCluster = func(name string, maxNode int, ready bool) *provisioningv1.DPUCluster {
		dc := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
				UID:       types.UID(fmt.Sprintf("%d", rnd.Int())),
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type:     "nvidia",
				MaxNodes: maxNode,
				Version:  "v1.30.2",
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
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: DefaultNS}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, testNS))).To(Succeed())

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
			dpu := createDPU("dpu")
			Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
			dc := createDPUCluster("dc", 1, true)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(cutil.GetNamespacedName(dc)))
		})
		It("allocate the first cluster in alphabetical order", func() {
			dpu := createDPU("dpu")
			Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
			dc1 := createDPUCluster("dc1", 1, true)
			alloc.SaveCluster(dc1)
			dc2 := createDPUCluster("dc2", 1, true)
			alloc.SaveCluster(dc2)
			result, err := alloc.Allocate(ctx, dpu)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc1.Name, Namespace: dc1.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(cutil.GetNamespacedName(dc1)))
		})
		It("cluster not ready", func() {
			dpu := createDPU("dpu")
			Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
			dc := createDPUCluster("dc", 1, false)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(types.NamespacedName{}))
		})
		It("no cluster", func() {
			dpu := createDPU("dpu")
			Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
			result, err := alloc.Allocate(ctx, dpu)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(types.NamespacedName{}))
		})
		It("reach max node limit", func() {
			dpu1 := createDPU("dpu1")
			Expect(k8sClient.Create(context.TODO(), dpu1)).To(Succeed())
			dc := createDPUCluster("dc", 1, true)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu1)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu1), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(cutil.GetNamespacedName(dc)))

			dpu2 := createDPU("dpu2")
			Expect(k8sClient.Create(context.TODO(), dpu2)).To(Succeed())
			result, err = alloc.Allocate(ctx, dpu2)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu2), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(types.NamespacedName{}))
		})
		It("release DPU", func() {
			dpu1 := createDPU("dpu1")
			Expect(k8sClient.Create(context.TODO(), dpu1)).To(Succeed())
			dc := createDPUCluster("dc", 1, true)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu1)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu1), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(cutil.GetNamespacedName(dc)))

			dpu2 := createDPU("dpu2")
			Expect(k8sClient.Create(context.TODO(), dpu2)).To(Succeed())
			result, err = alloc.Allocate(ctx, dpu2)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu2), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(types.NamespacedName{}))

			alloc.ReleaseDPU(dpu1)
			result, err = alloc.Allocate(ctx, dpu2)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu2), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(cutil.GetNamespacedName(dc)))
		})
		It("update cluster status", func() {
			dpu := createDPU("dpu")
			Expect(k8sClient.Create(context.TODO(), dpu)).To(Succeed())
			dc := createDPUCluster("dc", 1, false)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(types.NamespacedName{}))

			dc.Status = createDPUCluster("", 1, true).Status
			alloc.SaveCluster(dc)
			result, err = alloc.Allocate(ctx, dpu)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(cutil.GetNamespacedName(dc)))
		})
		It("allocator restart - reach max node limit", func() {
			dpu1 := createDPU("dpu1")
			Expect(k8sClient.Create(context.TODO(), dpu1)).To(Succeed())
			dc := createDPUCluster("dc", 1, true)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu1)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu1), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(cutil.GetNamespacedName(dc)))

			// alloc2 is a simulation of restarted allocator, which has a clean cache
			alloc2 := allocator.NewAllocator(k8sClient)
			alloc2.SaveCluster(dc)
			dpu2 := createDPU("dpu2")
			Expect(k8sClient.Create(context.TODO(), dpu2)).To(Succeed())
			// in this Allocate() call, the alloc2 should find dpu1 before allocate for dpu2, meaning that the allocation for dpu2 should fail
			result, err = alloc.Allocate(ctx, dpu2)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
		})
		It("allocator restart - should success", func() {
			dpu1 := createDPU("dpu1")
			Expect(k8sClient.Create(context.TODO(), dpu1)).To(Succeed())
			dc := createDPUCluster("dc", 2, true)
			alloc.SaveCluster(dc)
			result, err := alloc.Allocate(ctx, dpu1)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu1), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(cutil.GetNamespacedName(dc)))

			// alloc2 is a simulation of restarted allocator, which has a clean cache
			alloc2 := allocator.NewAllocator(k8sClient)
			alloc2.SaveCluster(dc)
			dpu2 := createDPU("dpu2")
			Expect(k8sClient.Create(context.TODO(), dpu2)).To(Succeed())
			result, err = alloc.Allocate(ctx, dpu2)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{Name: dc.Name, Namespace: dc.Namespace}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu2), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(cutil.GetNamespacedName(dc)))
		})
		It("manually assigned DPU", func() {
			dc := createDPUCluster("dc", 1, true)
			// dpu1 is manually assigned by user, it does not go through the allocation procedure
			dpu1 := createDPU("dpu1")
			dpu1.Spec.Cluster.Name = dc.Name
			dpu1.Spec.Cluster.Namespace = dc.Namespace
			Expect(k8sClient.Create(context.TODO(), dpu1)).To(Succeed())
			alloc.SaveCluster(dc)
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu1), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(cutil.GetNamespacedName(dc)))

			dpu2 := createDPU("dpu2")
			Expect(k8sClient.Create(context.TODO(), dpu2)).To(Succeed())
			result, err := alloc.Allocate(ctx, dpu2)
			Expect(err).NotTo(Succeed())
			Expect(result).To(Equal(allocator.AllocateResult{}))
			Eventually(func(g Gomega) types.NamespacedName {
				fetchedDPU := &provisioningv1.DPU{}
				g.Expect(k8sClient.Get(ctx, cutil.GetNamespacedName(dpu2), fetchedDPU)).To(Succeed())
				return types.NamespacedName{Name: fetchedDPU.Spec.Cluster.Name, Namespace: fetchedDPU.Spec.Cluster.Namespace}
			}).WithTimeout(10 * time.Second).WithPolling(1 * time.Millisecond).Should(Equal(types.NamespacedName{}))
		})
	})
},
)
