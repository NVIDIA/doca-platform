/*
Copyright 2026 NVIDIA

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
	"slices"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Set by the per-node sfc-controller; nothing removes it once the node (and that controller) is gone.
	agentServiceChainFinalizer     = dpuservicev1.SvcDpuGroupName + "/ServiceChain-finalizer"
	agentServiceInterfaceFinalizer = dpuservicev1.SvcDpuGroupName + "/ServiceInterface-finalizer"
	orphanTestLabelKey             = "role"
	orphanTestLabelValue           = "orphan-cleanup"
)

// setFinalizers replaces the finalizers of the given object with a merge patch.
func setFinalizers(obj client.Object, finalizers ...string) {
	GinkgoHelper()
	base := obj.DeepCopyObject().(client.Object)
	obj.SetFinalizers(finalizers)
	Expect(client.IgnoreNotFound(testClient.Patch(ctx, obj, client.MergeFrom(base)))).To(Succeed())
}

// createEmptyNSI creates an NSI without entries for the node, and removes it with its finalizers when the spec ends.
func createEmptyNSI(nodeName string, finalizers ...string) *dpuservicev1.NodeServiceInterfaces {
	GinkgoHelper()
	nsi := &dpuservicev1.NodeServiceInterfaces{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  utils.NSIObjectsNamespace,
			Name:       nsiName(nodeName, dpuservicev1.NSITypeSFC),
			Finalizers: finalizers,
		},
		Spec: dpuservicev1.NodeServiceInterfacesSpec{
			Node: nodeName,
			Type: dpuservicev1.NSITypeSFC,
		},
	}
	Expect(testClient.Create(ctx, nsi)).To(Succeed())
	DeferCleanup(func() {
		setFinalizers(nsi)
		Expect(client.IgnoreNotFound(testClient.Delete(ctx, nsi))).To(Succeed())
	})
	return nsi
}

// seedLegacyServiceInterfaceWithFinalizer creates a legacy ServiceInterface child of the set with the sfc-controller
// finalizer, and removes it when the spec ends so that a failing spec leaves nothing behind.
func seedLegacyServiceInterfaceWithFinalizer(set *dpuservicev1.ServiceInterfaceSet, name, nodeName string) *dpuservicev1.ServiceInterface {
	GinkgoHelper()
	legacy := seedLegacyServiceInterface(ctx, set, name, nodeName)
	DeferCleanup(func() {
		setFinalizers(legacy)
		Expect(client.IgnoreNotFound(testClient.Delete(ctx, legacy))).To(Succeed())
	})
	setFinalizers(legacy, agentServiceInterfaceFinalizer)
	return legacy
}

// bumpResourceVersion changes the object on the server, so that a copy read before is stale.
func bumpResourceVersion(obj client.Object) {
	GinkgoHelper()
	base := obj.DeepCopyObject().(client.Object)
	obj.SetLabels(map[string]string{"bumped": "true"})
	Expect(testClient.Patch(ctx, obj, client.MergeFrom(base))).To(Succeed())
}

// expectEventuallyGone waits for the object with the given key to be deleted.
func expectEventuallyGone(key client.ObjectKey, obj client.Object) {
	GinkgoHelper()
	Eventually(func() bool {
		return apierrors.IsNotFound(testClient.Get(ctx, key, obj))
	}, timeout*3, interval).Should(BeTrue())
}

// deleteNodeAndWait deletes the Node and waits until it is gone.
func deleteNodeAndWait(name string) {
	GinkgoHelper()
	Expect(client.IgnoreNotFound(testClient.Delete(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}))).To(Succeed())
	Eventually(func() bool {
		return apierrors.IsNotFound(testClient.Get(ctx, types.NamespacedName{Name: name}, &corev1.Node{}))
	}, timeout, interval).Should(BeTrue())
}

// deselectNode removes the labels of the Node so that the test selector no longer matches it.
func deselectNode(name string) {
	GinkgoHelper()
	node := &corev1.Node{}
	Expect(testClient.Get(ctx, types.NamespacedName{Name: name}, node)).To(Succeed())
	base := node.DeepCopy()
	node.Labels = map[string]string{}
	Eventually(func() error {
		return testClient.Patch(ctx, node, client.MergeFrom(base))
	}, timeout, interval).Should(Succeed())
}

var _ = Describe("Cleanup of objects left behind by a node that no longer exists", func() {
	var (
		testNS         *corev1.Namespace
		cleanupObjects []client.Object
		nodeNames      []string
		selector       *metav1.LabelSelector
	)

	BeforeEach(func() {
		cleanupObjects = nil
		nodeNames = nil
		selector = &metav1.LabelSelector{MatchLabels: map[string]string{orphanTestLabelKey: orphanTestLabelValue}}
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-orphan-"}}
		Expect(testClient.Create(ctx, testNS)).To(Succeed())
		DeferCleanup(testClient.Delete, ctx, testNS)
	})

	// newNode creates a node selected by the test selector.
	newNode := func(name string) {
		nodeNames = append(nodeNames, name)
		cleanupObjects = append(cleanupObjects, createNode(ctx, name, map[string]string{orphanTestLabelKey: orphanTestLabelValue}))
	}

	AfterEach(func() {
		// Nodes first: with no node there is nothing left to wait for, so set deletion can complete.
		for _, n := range nodeNames {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: n}}))).To(Succeed())
		}
		scList := &dpuservicev1.ServiceChainList{}
		Expect(testClient.List(ctx, scList, client.InNamespace(testNS.Name))).To(Succeed())
		for i := range scList.Items {
			setFinalizers(&scList.Items[i])
		}
		nsiList := &dpuservicev1.NodeServiceInterfacesList{}
		Expect(testClient.List(ctx, nsiList, client.InNamespace(utils.NSIObjectsNamespace))).To(Succeed())
		for i := range nsiList.Items {
			if slices.Contains(nodeNames, nsiList.Items[i].Spec.Node) {
				setFinalizers(&nsiList.Items[i])
			}
		}
		for _, o := range cleanupObjects {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, o))).To(Succeed())
		}
	})

	Context("ServiceChains", func() {
		// Creates the set, waits for the node's ServiceChain and adds the sfc-controller finalizer to it.
		createServiceChainWithAgentFinalizer := func(nodeName string) (*dpuservicev1.ServiceChainSet, *dpuservicev1.ServiceChain) {
			GinkgoHelper()
			set := createServiceChainSet(ctx, testNS.Name, selector)
			cleanupObjects = append(cleanupObjects, set)

			sc := &dpuservicev1.ServiceChain{}
			Eventually(func(g Gomega) {
				list := &dpuservicev1.ServiceChainList{}
				g.Expect(testClient.List(ctx, list, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(list.Items).To(HaveLen(1))
				g.Expect(list.Items[0].Spec.Node).To(HaveValue(Equal(nodeName)))
				sc = list.Items[0].DeepCopy()
			}, timeout*3, interval).Should(Succeed())
			setFinalizers(sc, agentServiceChainFinalizer)
			return set, sc
		}

		It("releases the ServiceChain of a node that was removed", func() {
			newNode("orphan-sc-node1")
			_, sc := createServiceChainWithAgentFinalizer("orphan-sc-node1")

			By("Removing the node, as happens when its DPU is deleted")
			deleteNodeAndWait("orphan-sc-node1")

			By("Expecting the ServiceChain to be deleted although nobody removes the sfc-controller finalizer")
			expectEventuallyGone(client.ObjectKeyFromObject(sc), &dpuservicev1.ServiceChain{})
		})

		It("only releases the ServiceChain of the removed node and keeps the others", func() {
			newNode("orphan-sc-node1")
			newNode("orphan-sc-node2")
			set := createServiceChainSet(ctx, testNS.Name, selector)
			cleanupObjects = append(cleanupObjects, set)

			byNode := map[string]*dpuservicev1.ServiceChain{}
			Eventually(func(g Gomega) {
				list := &dpuservicev1.ServiceChainList{}
				g.Expect(testClient.List(ctx, list, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(list.Items).To(HaveLen(2))
				for i := range list.Items {
					byNode[*list.Items[i].Spec.Node] = list.Items[i].DeepCopy()
				}
			}, timeout*3, interval).Should(Succeed())
			for _, sc := range byNode {
				setFinalizers(sc, agentServiceChainFinalizer)
			}

			deleteNodeAndWait("orphan-sc-node1")

			expectEventuallyGone(client.ObjectKeyFromObject(byNode["orphan-sc-node1"]), &dpuservicev1.ServiceChain{})
			Consistently(func(g Gomega) {
				got := &dpuservicev1.ServiceChain{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(byNode["orphan-sc-node2"]), got)).To(Succeed())
				g.Expect(got.DeletionTimestamp.IsZero()).To(BeTrue())
				g.Expect(got.Finalizers).To(ConsistOf(agentServiceChainFinalizer))
			}, 3*time.Second, interval).Should(Succeed())
		})

		It("keeps the sfc-controller finalizer while the node still exists but is no longer selected", func() {
			newNode("orphan-sc-node1")
			_, sc := createServiceChainWithAgentFinalizer("orphan-sc-node1")

			By("Deselecting the node; its sfc-controller is still running so it must be able to clean up")
			deselectNode("orphan-sc-node1")

			Eventually(func(g Gomega) {
				got := &dpuservicev1.ServiceChain{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sc), got)).To(Succeed())
				g.Expect(got.DeletionTimestamp.IsZero()).To(BeFalse())
			}, timeout*3, interval).Should(Succeed())
			Consistently(func(g Gomega) {
				got := &dpuservicev1.ServiceChain{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sc), got)).To(Succeed())
				g.Expect(got.Finalizers).To(ConsistOf(agentServiceChainFinalizer))
			}, 3*time.Second, interval).Should(Succeed())
		})

		It("lets a deleted ServiceChainSet go away when the node of its ServiceChain was removed", func() {
			newNode("orphan-sc-node1")
			set, _ := createServiceChainWithAgentFinalizer("orphan-sc-node1")

			deleteNodeAndWait("orphan-sc-node1")
			Expect(testClient.Delete(ctx, set)).To(Succeed())

			expectEventuallyGone(client.ObjectKeyFromObject(set), &dpuservicev1.ServiceChainSet{})
		})

		It("lets a deleted ServiceChainSet go away when the node was removed after the deletion started", func() {
			newNode("orphan-sc-node1")
			set, _ := createServiceChainWithAgentFinalizer("orphan-sc-node1")

			By("Deleting the set; it must wait for the sfc-controller of the node that still exists")
			Expect(testClient.Delete(ctx, set)).To(Succeed())
			Consistently(func() error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(set), &dpuservicev1.ServiceChainSet{})
			}, 3*time.Second, interval).Should(Succeed())

			By("Removing the node")
			deleteNodeAndWait("orphan-sc-node1")
			expectEventuallyGone(client.ObjectKeyFromObject(set), &dpuservicev1.ServiceChainSet{})
		})
	})

	Context("NodeServiceInterfaces", func() {
		nsiKey := func(nodeName string) client.ObjectKey {
			return client.ObjectKey{
				Namespace: utils.NSIObjectsNamespace,
				Name:      nsiName(nodeName, dpuservicev1.NSITypeSFC),
			}
		}

		// Creates the set, waits for the node's NSI and adds the sfc-controller finalizer to it.
		createNSIWithAgentFinalizer := func(nodeName string) *dpuservicev1.ServiceInterfaceSet {
			GinkgoHelper()
			sis := createServiceInterfaceSet(ctx, testNS.Name, selector)
			cleanupObjects = append(cleanupObjects, sis)
			nsi := &dpuservicev1.NodeServiceInterfaces{}
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, nsiKey(nodeName), nsi)).To(Succeed())
			}, timeout*3, interval).Should(Succeed())
			setFinalizers(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)
			return sis
		}

		It("releases the NSI entry and the NSI of a node that was removed", func() {
			newNode("orphan-nsi-node1")
			createNSIWithAgentFinalizer("orphan-nsi-node1")

			By("Removing the node, as happens when its DPU is deleted")
			deleteNodeAndWait("orphan-nsi-node1")

			By("Expecting the NSI to be gone without anybody setting ResourceReleased or removing the finalizer")
			expectEventuallyGone(nsiKey("orphan-nsi-node1"), &dpuservicev1.NodeServiceInterfaces{})
		})

		It("keeps the NSI entry awaiting release while the node still exists but is no longer selected", func() {
			newNode("orphan-nsi-node1")
			createNSIWithAgentFinalizer("orphan-nsi-node1")

			deselectNode("orphan-nsi-node1")

			Eventually(func(g Gomega) {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, nsiKey("orphan-nsi-node1"), nsi)).To(Succeed())
				g.Expect(nsi.Spec.Interfaces).To(ContainElement(HaveField("Terminating", BeTrue())))
			}, timeout*3, interval).Should(Succeed())
			Consistently(func(g Gomega) {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, nsiKey("orphan-nsi-node1"), nsi)).To(Succeed())
				g.Expect(nsi.Spec.Interfaces).To(HaveLen(1))
				g.Expect(nsi.Finalizers).To(ConsistOf(dpuservicev1.NodeServiceInterfacesFinalizer))
			}, 3*time.Second, interval).Should(Succeed())
		})

		It("deletes an empty NSI left behind for a node that no longer exists", func() {
			leaked := createEmptyNSI("orphan-nsi-leaked", dpuservicev1.NodeServiceInterfacesFinalizer)

			By("Reconciling any ServiceInterfaceSet")
			newNode("orphan-nsi-node1")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, selector))

			expectEventuallyGone(client.ObjectKeyFromObject(leaked), &dpuservicev1.NodeServiceInterfaces{})
		})

		It("doesn't delete an NSI that got a new entry after it was read as empty", func() {
			nsi := createEmptyNSI("orphan-nsi-stale")
			stale := nsi.DeepCopy()

			By("Adding an entry after the empty NSI was read")
			updated := nsi.DeepCopy()
			updated.Spec.Interfaces = []dpuservicev1.InterfaceEntry{{
				Name:          "late-entry",
				InterfaceType: dpuservicev1.InterfaceTypePhysical,
				Physical:      &dpuservicev1.Physical{InterfaceName: "p0"},
			}}
			Expect(testClient.Patch(ctx, updated, client.MergeFrom(nsi))).To(Succeed())

			reconciler := &ServiceInterfaceSetReconciler{Client: testClient}
			Expect(apierrors.IsConflict(reconciler.deleteNSIIfEmpty(ctx, stale))).To(BeTrue())
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(nsi), &dpuservicev1.NodeServiceInterfaces{})).To(Succeed())
		})

		It("doesn't try to delete an empty NSI that is already being deleted", func() {
			newNode("orphan-nsi-live")
			nsi := createEmptyNSI("orphan-nsi-live", dpuservicev1.NodeServiceInterfacesFinalizer)
			Expect(testClient.Delete(ctx, nsi)).To(Succeed())

			By("Reading the NSI once it is seen as being deleted, then changing it so that the copy is stale")
			stale := &dpuservicev1.NodeServiceInterfaces{}
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(nsi), stale)).To(Succeed())
				g.Expect(stale.DeletionTimestamp.IsZero()).To(BeFalse())
			}, timeout, interval).Should(Succeed())
			bumpResourceVersion(nsi)

			reconciler := &ServiceInterfaceSetReconciler{Client: testClient}
			Expect(reconciler.deleteNSIIfEmpty(ctx, stale)).To(Succeed())
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(nsi), nsi)).To(Succeed())
			Expect(nsi.Finalizers).To(ConsistOf(dpuservicev1.NodeServiceInterfacesFinalizer))
		})

		It("doesn't release finalizers of an object that changed after it was read", func() {
			nsi := createEmptyNSI("orphan-nsi-lock", dpuservicev1.NodeServiceInterfacesFinalizer)
			stale := nsi.DeepCopy()
			bumpResourceVersion(nsi)

			err := releaseFinalizers(ctx, testClient, stale, dpuservicev1.NodeServiceInterfacesFinalizer)
			Expect(apierrors.IsConflict(err)).To(BeTrue())
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(nsi), nsi)).To(Succeed())
			Expect(nsi.Finalizers).To(ConsistOf(dpuservicev1.NodeServiceInterfacesFinalizer))
		})

		It("releases a leftover legacy ServiceInterface of a node that was removed", func() {
			sis := getServiceInterfaceSet(testNS.Name, selector)
			legacy := seedLegacyServiceInterfaceWithFinalizer(sis, "legacy-gone", "orphan-legacy-gone")

			Expect(testClient.Create(ctx, sis)).To(Succeed())
			cleanupObjects = append(cleanupObjects, sis)

			expectEventuallyGone(client.ObjectKeyFromObject(legacy), &dpuservicev1.ServiceInterface{})
		})

		It("releases leftover legacy ServiceInterfaces of a deleted set when their node was removed", func() {
			sis := getServiceInterfaceSet(testNS.Name, selector)
			legacy := seedLegacyServiceInterfaceWithFinalizer(sis, "legacy-deleted-set", "orphan-legacy-deleted-set")

			By("Reconciling the deletion until the cache has the child")
			reconciler := &ServiceInterfaceSetReconciler{Client: testClient}
			Eventually(func(g Gomega) {
				_, err := reconciler.reconcileDeleteNSI(ctx, sis)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(legacy), &dpuservicev1.ServiceInterface{}))).To(BeTrue())
			}, timeout*3, interval).Should(Succeed())
		})

		It("lets a deleted ServiceInterfaceSet go away when the node was removed", func() {
			newNode("orphan-nsi-node1")
			sis := createNSIWithAgentFinalizer("orphan-nsi-node1")

			deleteNodeAndWait("orphan-nsi-node1")
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, sis))).To(Succeed())

			expectEventuallyGone(client.ObjectKeyFromObject(sis), &dpuservicev1.ServiceInterfaceSet{})
		})
	})
})
