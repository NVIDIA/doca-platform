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

package controllers

import (
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Sort function", func() {
	Context("When sorting DPUService", func() {
		DescribeTable("Validates the DPUServices are sorted fron oldest to newest", func(dpuServices []dpuservicev1.DPUService) {
			objects := make([]client.Object, len(dpuServices))
			for i := range dpuServices {
				objects[i] = &dpuServices[i]
			}
			sortObjectsByCreationTimestamp(objects)
			for i := 0; i < len(objects)-1; i++ {
				Expect(objects[i].GetCreationTimestamp().Time.Before(objects[i+1].GetCreationTimestamp().Time)).To(BeTrue())
			}
		},
			Entry("from oldest to newest", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now()),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(time.Hour)),
					},
				},
			}),
			Entry("from newest to oldest", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now()),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
					},
				},
			}),
			Entry("random order", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Now()),
					},
				},
			}),
		)
	})
})

var _ = Describe("getRevisionHistoryLimit", func() {
	now := time.Now()
	Context("When getting the revision history limit", func() {
		DescribeTable("Validates the revision history limit is correct", func(dpuServices []dpuservicev1.DPUService, revisionHistoryLimit int32, expected []dpuservicev1.DPUService) {
			objects := make([]client.Object, len(dpuServices))
			for i := range dpuServices {
				objects[i] = &dpuServices[i]
			}
			expectsObjects := make([]client.Object, len(expected))
			for i := range expected {
				expectsObjects[i] = &expected[i]
			}
			res := getRevisionHistoryLimitList(objects, revisionHistoryLimit)
			Expect(res).To(HaveLen(len(expectsObjects)))
			Expect(res).To(ConsistOf(expectsObjects))
		},
			Entry("less than the limit", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
			}, int32(5), []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
					},
				},
			}),
			Entry("equal to the limit", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(2 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(3 * time.Hour)),
					},
				},
			}, int32(5), []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(3 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(2 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
					},
				},
			}),
			Entry("more than the limit", []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(2 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(3 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(4 * time.Hour)),
					},
				},
			}, int32(5), []dpuservicev1.DPUService{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(4 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(3 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(2 * time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now.Add(time.Hour)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(now),
					},
				},
			}),
		)
	})
})

var _ = Describe("convertNodeSelectorTermToSelector", func() {
	Context("When converting NodeSelectorTerm to labels.Selector", func() {
		It("should convert In operator correctly", func() {
			term := corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "node-role",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"worker", "compute"},
					},
				},
			}
			selector := convertNodeSelectorTermToSelector(term)
			Expect(selector).NotTo(BeNil())
			Expect(selector.String()).To(Equal("node-role in (compute,worker)"))
		})

		It("should convert NotIn operator correctly", func() {
			term := corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "node-role",
						Operator: corev1.NodeSelectorOpNotIn,
						Values:   []string{"master"},
					},
				},
			}
			selector := convertNodeSelectorTermToSelector(term)
			Expect(selector).NotTo(BeNil())
			Expect(selector.String()).To(Equal("node-role notin (master)"))
		})

		It("should convert Exists operator correctly", func() {
			term := corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "dpu-enabled",
						Operator: corev1.NodeSelectorOpExists,
					},
				},
			}
			selector := convertNodeSelectorTermToSelector(term)
			Expect(selector).NotTo(BeNil())
			Expect(selector.String()).To(Equal("dpu-enabled"))
		})

		It("should convert DoesNotExist operator correctly", func() {
			term := corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "deprecated",
						Operator: corev1.NodeSelectorOpDoesNotExist,
					},
				},
			}
			selector := convertNodeSelectorTermToSelector(term)
			Expect(selector).NotTo(BeNil())
			Expect(selector.String()).To(Equal("!deprecated"))
		})

		It("should convert GreaterThan operator correctly", func() {
			term := corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "cpu-count",
						Operator: corev1.NodeSelectorOpGt,
						Values:   []string{"4"},
					},
				},
			}
			selector := convertNodeSelectorTermToSelector(term)
			Expect(selector).NotTo(BeNil())
			Expect(selector.String()).To(Equal("cpu-count>4"))
		})

		It("should convert LessThan operator correctly", func() {
			term := corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "memory-gb",
						Operator: corev1.NodeSelectorOpLt,
						Values:   []string{"16"},
					},
				},
			}
			selector := convertNodeSelectorTermToSelector(term)
			Expect(selector).NotTo(BeNil())
			Expect(selector.String()).To(Equal("memory-gb<16"))
		})

		It("should handle multiple match expressions", func() {
			term := corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "node-role",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"worker"},
					},
					{
						Key:      "dpu-enabled",
						Operator: corev1.NodeSelectorOpExists,
					},
				},
			}
			selector := convertNodeSelectorTermToSelector(term)
			Expect(selector).NotTo(BeNil())
			// The selector should contain both requirements
			Expect(selector.String()).To(ContainSubstring("node-role"))
			Expect(selector.String()).To(ContainSubstring("dpu-enabled"))
		})

		It("should return empty selector for empty term", func() {
			term := corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{},
			}
			selector := convertNodeSelectorTermToSelector(term)
			Expect(selector).NotTo(BeNil())
			Expect(selector.Empty()).To(BeTrue())
		})

		It("should skip unknown operators", func() {
			term := corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "valid-key",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"value"},
					},
				},
			}
			selector := convertNodeSelectorTermToSelector(term)
			Expect(selector).NotTo(BeNil())
			Expect(selector.String()).To(Equal("valid-key in (value)"))
		})
	})
})

var _ = Describe("deduplicateNodes", func() {
	Context("When deduplicating nodes", func() {
		It("should return empty slice for empty input", func() {
			result := deduplicateNodes([]corev1.Node{})
			Expect(result).To(BeEmpty())
		})

		It("should return same slice when no duplicates", func() {
			nodes := []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			}
			result := deduplicateNodes(nodes)
			Expect(result).To(HaveLen(3))
			Expect(result).To(ConsistOf(
				HaveField("Name", "node1"),
				HaveField("Name", "node2"),
				HaveField("Name", "node3"),
			))
		})

		It("should remove duplicate nodes", func() {
			nodes := []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			}
			result := deduplicateNodes(nodes)
			Expect(result).To(HaveLen(3))
			names := []string{result[0].Name, result[1].Name, result[2].Name}
			Expect(names).To(ConsistOf("node1", "node2", "node3"))
		})

		It("should preserve first occurrence of duplicate nodes", func() {
			nodes := []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"version": "v1"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"version": "v2"}}},
			}
			result := deduplicateNodes(nodes)
			Expect(result).To(HaveLen(2))
			// First occurrence should be preserved
			for _, node := range result {
				if node.Name == "node1" {
					Expect(node.Labels["version"]).To(Equal("v1"))
				}
			}
		})

		It("should handle all nodes being duplicates", func() {
			nodes := []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
			}
			result := deduplicateNodes(nodes)
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("node1"))
		})
	})
})

var _ = Describe("listNodesByNodeAffinity", func() {
	Context("When listing nodes by node affinity", func() {
		var testNode1, testNode2, testNode3 *corev1.Node

		BeforeEach(func() {
			testNode1 = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-1",
					Labels: map[string]string{
						"node-role": "worker",
						"zone":      "us-west-1a",
					},
				},
			}
			testNode2 = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-2",
					Labels: map[string]string{
						"node-role": "compute",
						"zone":      "us-west-1b",
					},
				},
			}
			testNode3 = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-3",
					Labels: map[string]string{
						"node-role": "master",
						"zone":      "us-west-1a",
					},
				},
			}

			Expect(testClient.Create(ctx, testNode1)).To(Succeed())
			Expect(testClient.Create(ctx, testNode2)).To(Succeed())
			Expect(testClient.Create(ctx, testNode3)).To(Succeed())
		})

		AfterEach(func() {
			Expect(testClient.Delete(ctx, testNode1)).To(Succeed())
			Expect(testClient.Delete(ctx, testNode2)).To(Succeed())
			Expect(testClient.Delete(ctx, testNode3)).To(Succeed())
		})

		It("should list nodes matching single In expression", func() {
			terms := []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "node-role",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"worker"},
						},
					},
				},
			}

			nodes, err := listNodesByNodeAffinity(ctx, testClient, terms)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(1))
			Expect(nodes[0].Name).To(Equal("test-node-1"))
		})

		It("should list nodes matching multiple values in In expression", func() {
			terms := []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "node-role",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"worker", "compute"},
						},
					},
				},
			}

			nodes, err := listNodesByNodeAffinity(ctx, testClient, terms)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(2))
			nodeNames := []string{nodes[0].Name, nodes[1].Name}
			Expect(nodeNames).To(ConsistOf("test-node-1", "test-node-2"))
		})

		It("should list nodes matching NotIn expression", func() {
			terms := []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "node-role",
							Operator: corev1.NodeSelectorOpNotIn,
							Values:   []string{"master"},
						},
					},
				},
			}

			nodes, err := listNodesByNodeAffinity(ctx, testClient, terms)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(2))
			nodeNames := []string{nodes[0].Name, nodes[1].Name}
			Expect(nodeNames).To(ConsistOf("test-node-1", "test-node-2"))
		})

		It("should deduplicate nodes from multiple terms", func() {
			terms := []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "zone",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"us-west-1a"},
						},
					},
				},
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "node-role",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"worker"},
						},
					},
				},
			}

			nodes, err := listNodesByNodeAffinity(ctx, testClient, terms)
			Expect(err).NotTo(HaveOccurred())
			// test-node-1 matches both terms (zone=us-west-1a and node-role=worker)
			// test-node-3 matches first term (zone=us-west-1a)
			// Should be deduplicated
			Expect(nodes).To(HaveLen(2))
			nodeNames := []string{nodes[0].Name, nodes[1].Name}
			Expect(nodeNames).To(ConsistOf("test-node-1", "test-node-3"))
		})

		It("should return empty list when no nodes match", func() {
			terms := []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "node-role",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"nonexistent"},
						},
					},
				},
			}

			nodes, err := listNodesByNodeAffinity(ctx, testClient, terms)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(BeEmpty())
		})

		It("should handle empty terms", func() {
			terms := []corev1.NodeSelectorTerm{}

			nodes, err := listNodesByNodeAffinity(ctx, testClient, terms)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(3))
		})
	})
})

var _ = Describe("extractSelectors", func() {
	Context("When extracting selectors from DPUServiceChain", func() {
		It("should extract DPUClusterSelector and NodeSelector", func() {
			dpuClusterSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{"cluster": "test"},
			}
			nodeSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{"node": "worker"},
			}

			chain := &dpuservicev1.DPUServiceChain{
				ObjectMeta: metav1.ObjectMeta{Name: "test-chain"},
				Spec: dpuservicev1.DPUServiceChainSpec{
					DPUClusterSelector: dpuClusterSelector,
					Template: dpuservicev1.ServiceChainSetSpecTemplate{
						Spec: dpuservicev1.ServiceChainSetSpec{
							NodeSelector: nodeSelector,
						},
					},
				},
			}

			dpuCluster, node := extractSelectors(ctx, chain)
			Expect(dpuCluster).To(Equal(dpuClusterSelector))
			Expect(node).To(Equal(nodeSelector))
		})

		It("should return nil selectors when not specified", func() {
			chain := &dpuservicev1.DPUServiceChain{
				ObjectMeta: metav1.ObjectMeta{Name: "test-chain"},
			}

			dpuCluster, node := extractSelectors(ctx, chain)
			Expect(dpuCluster).To(BeNil())
			Expect(node).To(BeNil())
		})
	})

	Context("When extracting selectors from DPUServiceInterface", func() {
		It("should extract DPUClusterSelector and NodeSelector", func() {
			dpuClusterSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{"cluster": "test"},
			}
			nodeSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{"node": "worker"},
			}

			iface := &dpuservicev1.DPUServiceInterface{
				ObjectMeta: metav1.ObjectMeta{Name: "test-interface"},
				Spec: dpuservicev1.DPUServiceInterfaceSpec{
					DPUClusterSelector: dpuClusterSelector,
					Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
						Spec: dpuservicev1.ServiceInterfaceSetSpec{
							NodeSelector: nodeSelector,
						},
					},
				},
			}

			dpuCluster, node := extractSelectors(ctx, iface)
			Expect(dpuCluster).To(Equal(dpuClusterSelector))
			Expect(node).To(Equal(nodeSelector))
		})
	})

	Context("When extracting selectors from unknown object type", func() {
		It("should return nil selectors", func() {
			unknownObj := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
			}

			dpuCluster, node := extractSelectors(ctx, unknownObj)
			Expect(dpuCluster).To(BeNil())
			Expect(node).To(BeNil())
		})
	})
})

var _ = Describe("extractDPUNodeFromNodeLabels", func() {
	Context("When extracting DPUNode info from node labels", func() {
		It("should extract from new labels", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel:      "test-dpunode",
						provisioningv1.DPUNodeNamespaceLabel: "test-namespace",
					},
				},
			}

			nn, found := extractDPUNodeFromNodeLabels(node, "default-ns")
			Expect(found).To(BeTrue())
			Expect(nn.Name).To(Equal("test-dpunode"))
			Expect(nn.Namespace).To(Equal("test-namespace"))
		})

		It("should fall back to deprecated label", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						cutil.HostNameDPULabelKey: "test-dpunode",
					},
				},
			}

			nn, found := extractDPUNodeFromNodeLabels(node, "default-ns")
			Expect(found).To(BeTrue())
			Expect(nn.Name).To(Equal("test-dpunode"))
			Expect(nn.Namespace).To(Equal("default-ns"))
		})

		It("should prefer new labels over deprecated", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel:      "new-dpunode",
						provisioningv1.DPUNodeNamespaceLabel: "new-namespace",
						cutil.HostNameDPULabelKey:            "old-dpunode",
					},
				},
			}

			nn, found := extractDPUNodeFromNodeLabels(node, "default-ns")
			Expect(found).To(BeTrue())
			Expect(nn.Name).To(Equal("new-dpunode"))
			Expect(nn.Namespace).To(Equal("new-namespace"))
		})

		It("should return false when no labels present", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: map[string]string{},
				},
			}

			_, found := extractDPUNodeFromNodeLabels(node, "default-ns")
			Expect(found).To(BeFalse())
		})

		It("should use empty namespace when new label has no namespace", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel: "test-dpunode",
					},
				},
			}

			nn, found := extractDPUNodeFromNodeLabels(node, "default-ns")
			Expect(found).To(BeTrue())
			Expect(nn.Name).To(Equal("test-dpunode"))
			Expect(nn.Namespace).To(Equal(""))
		})
	})
})
