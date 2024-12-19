/*
COPYRIGHT 2024 NVIDIA

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

package controller //nolint:dupl

import (
	"context"
	"sort"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	resourceName = "test-resource"
	defaultNS    = "default"
)

//nolint:dupl
var _ = Describe("ServiceChainSet Controller", func() {
	Context("When reconciling a resource", func() {
		var cleanupObjects []client.Object
		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})
		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		})
		It("should successfully reconcile the ServiceChainSet without Node Selector", func() {
			By("Create ServiceChainSet, without Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceChainSet(ctx, &metav1.LabelSelector{}))
			By("Verify ServiceChain not created, no nodes")
			Consistently(func(g Gomega) {
				serviceChainList := &dpuservicev1.ServiceChainList{}
				err := testClient.List(ctx, serviceChainList)
				Expect(err).NotTo(HaveOccurred())
				Expect(serviceChainList.Items).To(BeEmpty())
			}).WithTimeout(20 * time.Second).Should(Succeed())

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes")
			Eventually(func(g Gomega) {
				assertServiceChainList(ctx, g, 3, &cleanupObjects, getTestServiceChainSpec())
			}, timeout*30, interval).Should(Succeed())

		})
		It("should successfully reconcile the ServiceChainSet with Node Selector", func() {
			By("creating ServiceChainSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceChainSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes, 2 matches")
			Eventually(func(g Gomega) {
				assertServiceChainList(ctx, g, 2, &cleanupObjects, getTestServiceChainSpec())
			}, timeout*30, interval).Should(Succeed())
		})
		It("should successfully reconcile the ServiceChainSet with Node Selector and remove Service Chain", func() {
			By("creating ServiceChainSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceChainSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", labels))

			By("Reconciling the created resource, 3 nodes, 3 matches")
			Eventually(func(g Gomega) {
				assertServiceChainList(ctx, g, 3, &cleanupObjects, getTestServiceChainSpec())
			}, timeout*30, interval).Should(Succeed())

			By("Patch Node-3 label to not be selected")
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node3"}}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(node), node)).To(Succeed())
			patch := node.DeepCopy()
			patch.Labels = make(map[string]string)
			Eventually(func() error {
				return testClient.Patch(ctx, patch, client.MergeFrom(node))
			}, timeout, interval).Should(Succeed())

			By("Reconciling the created resource, 3 nodes, 2 matching")
			Eventually(func(g Gomega) {
				assertServiceChainList(ctx, g, 2, &cleanupObjects, getTestServiceChainSpec())
			}, timeout*30, interval).Should(Succeed())
		})
		It("should successfully reconcile the ServiceChainSet after update", func() {
			By("creating ServiceChainSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceChainSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes, 2 matches")
			Eventually(func(g Gomega) {
				assertServiceChainList(ctx, g, 2, &cleanupObjects, getTestServiceChainSpec())
			}, timeout*30, interval).Should(Succeed())

			By("Update ServiceChainSet Spec")
			updatedSpec := &dpuservicev1.ServiceChainSpec{
				Switches: []dpuservicev1.Switch{
					{
						Ports: []dpuservicev1.Port{
							{
								ServiceInterface: dpuservicev1.ServiceIfc{
									MatchLabels: map[string]string{
										"svc.dpu.nvidia.com/interface": "p1",
									},
								},
							},
						},
					},
				},
			}

			scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: defaultNS}}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).To(Succeed())
			patch := scs.DeepCopy()
			patch.Spec.Template.Spec = *updatedSpec
			Eventually(func() error {
				return testClient.Patch(ctx, patch, client.MergeFrom(scs))
			}, timeout, interval).Should(Succeed())
			By("Reconciling the updated resource")
			Eventually(func(g Gomega) {
				assertServiceChainList(ctx, g, 2, &cleanupObjects, updatedSpec)
			}, timeout*30, interval).Should(Succeed())
		})
		It("should successfully delete the ServiceChainSet", func() {
			By("Creating ServiceChainSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceChainSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Creating 2 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))

			By("Reconciling the created resource, 2 nodes, 2 matches")
			Eventually(func(g Gomega) {
				assertServiceChainList(ctx, g, 2, &cleanupObjects, getTestServiceChainSpec())
			}, timeout*30, interval).Should(Succeed())

			By("Deleting ServiceChainSet")
			scs := cleanupObjects[0].(*dpuservicev1.ServiceChainSet)
			Expect(testClient.Delete(ctx, scs)).NotTo(HaveOccurred())

			By("Verifying ServiceChainSet is deleted")
			Eventually(func(g Gomega) {
				scs := cleanupObjects[0].(*dpuservicev1.ServiceChainSet)
				err := testClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout*30, interval).Should(Succeed())
		})
		It("should successfully delete the ServiceChainSet and corresponding ServiceInterfaces in its namespace only but not cross-namespace", func() {
			By("Creating ServiceChainSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceChainSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Creating 2 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))

			By("Verifying ServiceChainSets have been reconciled")
			Eventually(func(g Gomega) {
				// The second ServiceInterfaceSet should still exist
				serviceChainList := &dpuservicev1.ServiceChainList{}
				g.ExpectWithOffset(1, testClient.List(ctx, serviceChainList)).NotTo(HaveOccurred())
				g.Expect(serviceChainList.Items).To(HaveLen(2))
			}, timeout*30, interval).Should(Succeed())

			By("Create another namespace with a ServiceChain copy from the default namespace")
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "zzz-testing-foo"}}
			Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, namespace))).To(Succeed())
			scList := &dpuservicev1.ServiceChainList{}
			ExpectWithOffset(1, testClient.List(ctx, scList)).NotTo(HaveOccurred())
			scCopy := scList.Items[0].DeepCopy()
			scCopy.SetResourceVersion("")
			scCopy.SetManagedFields(nil)
			scCopy.SetOwnerReferences(nil)
			scCopy.SetUID("")
			scCopy.Namespace = "zzz-testing-foo"
			Expect(testClient.Create(ctx, scCopy)).To(Succeed())
			cleanupObjects = append(cleanupObjects, scCopy)

			By("Deleting ServiceChainSet")
			scs := cleanupObjects[0].(*dpuservicev1.ServiceChainSet)
			Expect(testClient.Delete(ctx, scs)).NotTo(HaveOccurred())

			By("Verifying ServiceChainSet is deleted")
			Eventually(func(g Gomega) {
				scs := cleanupObjects[0].(*dpuservicev1.ServiceChainSet)
				err := testClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())

				scList := &dpuservicev1.ServiceChainList{}
				g.ExpectWithOffset(1, testClient.List(ctx, scList)).NotTo(HaveOccurred())
				g.Expect(scList.Items).To(HaveLen(1))
				g.Expect(scList.Items[0].GetNamespace()).NotTo(Equal(defaultNS))
			}, timeout*30, interval).Should(Succeed())
		})
		It("verify ServiceChain node labeling", func() {
			By("Create ServiceChainSet, without Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceChainSet(ctx, &metav1.LabelSelector{}))
			By("Verify ServiceChain not created, no nodes")
			Consistently(func(g Gomega) {
				serviceChainList := &dpuservicev1.ServiceChainList{}
				err := testClient.List(ctx, serviceChainList)
				Expect(err).NotTo(HaveOccurred())
				Expect(serviceChainList.Items).To(BeEmpty())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			nodeNames := []string{"node1", "node2", "node3"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, nodeNames[0], labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, nodeNames[1], labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, nodeNames[2], make(map[string]string)))

			By("Reconciling the created resource, 3 nodes")
			Eventually(func(g Gomega) {
				serviceChainList := &dpuservicev1.ServiceChainList{}
				g.Expect(testClient.List(ctx, serviceChainList)).NotTo(HaveOccurred())
				scNodes := []string{}
				for _, si := range serviceChainList.Items {
					serviceInterface := si
					cleanupObjects = append(cleanupObjects, &serviceInterface)
					scNodes = append(scNodes, si.Labels[ServiceInterfaceNodeNameLabel])
				}
				sort.Strings(scNodes)
				g.Expect(scNodes).To(HaveLen(3))
				for i := range scNodes {
					g.Expect(scNodes[i]).To(Equal(nodeNames[i]))
				}
			}, timeout*30, interval).Should(Succeed())
		})
	})
	Context("Validating ServiceChainSet creation", func() {
		var cleanupObjects []client.Object
		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})
		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		})
		It("should successfully create the ServiceChainSet with port service interface", func() {
			By("creating ServiceChainSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceChainSet(ctx,
				&metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}))
		})
		It("should successfully create the ServiceChainSet with port service", func() {
			By("creating ServiceChainSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceChainSet(ctx,
				&metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}))
		})
		It("should successfully create the ServiceChainSet and have all conditions set", func() {
			By("creating ServiceChainSet, with Node Selector")
			obj := createServiceChainSet(ctx,
				&metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}})
			cleanupObjects = append(cleanupObjects, obj)
			Eventually(func(g Gomega) {
				assertServiceChainSetCondition(g, testClient, obj)
			}).WithTimeout(30 * time.Second).Should(BeNil())
		})
	})
})

func assertServiceChainSetCondition(g Gomega, testClient client.Client, serviceChainSet *dpuservicev1.ServiceChainSet) {
	gotServiceChainSet := &dpuservicev1.ServiceChainSet{}
	g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(serviceChainSet), gotServiceChainSet)).To(Succeed())
	g.Expect(gotServiceChainSet.Status.Conditions).NotTo(BeNil())
	g.Expect(gotServiceChainSet.Status.Conditions).To(ConsistOf(
		And(
			HaveField("Type", string(conditions.TypeReady)),
			HaveField("Status", metav1.ConditionTrue),
			HaveField("Reason", string(conditions.ReasonSuccess)),
		),
		And(
			HaveField("Type", string(dpuservicev1.ConditionServiceChainsReconciled)),
			HaveField("Status", metav1.ConditionTrue),
			HaveField("Reason", string(conditions.ReasonSuccess)),
		),
		And(
			HaveField("Type", string(dpuservicev1.ConditionServiceChainsReady)),
			HaveField("Status", metav1.ConditionTrue),
			HaveField("Reason", string(conditions.ReasonSuccess)),
		),
	))
}

func createServiceChainSet(ctx context.Context, labelSelector *metav1.LabelSelector) *dpuservicev1.ServiceChainSet {
	scs := serviceChainSet(labelSelector)
	scs.Spec.Template.Spec = *getTestServiceChainSpec()

	Expect(testClient.Create(ctx, scs)).NotTo(HaveOccurred())
	return scs
}

func serviceChainSet(labelSelector *metav1.LabelSelector) *dpuservicev1.ServiceChainSet {
	scs := &dpuservicev1.ServiceChainSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: defaultNS,
		},
		Spec: dpuservicev1.ServiceChainSetSpec{
			NodeSelector: labelSelector,
			Template: dpuservicev1.ServiceChainSpecTemplate{
				ObjectMeta: dpuservicev1.ObjectMeta{
					Labels: testutils.GetTestLabels(),
				},
			},
		},
	}
	return scs
}

func getTestServiceChainSpec() *dpuservicev1.ServiceChainSpec {
	return &dpuservicev1.ServiceChainSpec{
		Switches: []dpuservicev1.Switch{
			{
				Ports: []dpuservicev1.Port{
					{
						ServiceInterface: dpuservicev1.ServiceIfc{
							MatchLabels: map[string]string{
								dpuservicev1.DPFServiceIDLabelKey: "firewall",
								"svc.dpu.nvidia.com/interface":    "eth0",
							},
							IPAM: &dpuservicev1.IPAM{
								MatchLabels: map[string]string{
									"svc.dpu.nvidia.com/ippool": "mypool",
								},
							},
						},
					},
				},
			},
		},
	}
}

func createNode(ctx context.Context, name string, labels map[string]string) *corev1.Node {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	Expect(testClient.Create(ctx, node)).NotTo(HaveOccurred())
	return node
}

func assertServiceChainList(ctx context.Context, g Gomega, nodeCount int, cleanupObjects *[]client.Object,
	testSpec *dpuservicev1.ServiceChainSpec) {
	serviceChainList := &dpuservicev1.ServiceChainList{}
	g.ExpectWithOffset(1, testClient.List(ctx, serviceChainList)).NotTo(HaveOccurred())
	g.ExpectWithOffset(1, serviceChainList.Items).To(HaveLen(nodeCount))

	nodeMap := make(map[string]bool)
	for _, sc := range serviceChainList.Items {
		serviceChain := sc
		*cleanupObjects = append(*cleanupObjects, &serviceChain)
		assertServiceChain(g, &sc, testSpec)
		nodeMap[*sc.Spec.Node] = true
	}
	g.ExpectWithOffset(1, nodeMap).To(HaveLen(nodeCount))
}

func assertServiceChain(g Gomega, sc *dpuservicev1.ServiceChain, testSpec *dpuservicev1.ServiceChainSpec) {
	specCopy := testSpec.DeepCopy()
	node := sc.Spec.Node
	specCopy.Node = node
	g.ExpectWithOffset(2, sc.Spec).To(Equal(*specCopy))
	g.ExpectWithOffset(2, *node).NotTo(BeEmpty())
	g.ExpectWithOffset(2, sc.Name).To(Equal(resourceName + "-" + *node))
	g.ExpectWithOffset(2, sc.Labels[ServiceChainSetNameLabel]).To(Equal(resourceName))
	g.ExpectWithOffset(2, sc.Labels[ServiceChainSetNamespaceLabel]).To(Equal(defaultNS))
	g.ExpectWithOffset(2, sc.OwnerReferences).To(HaveLen(1))
	for k, v := range testutils.GetTestLabels() {
		g.ExpectWithOffset(2, sc.Labels[k]).To(Equal(v))
	}
}
