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
	"context"
	"fmt"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// assertConditionCurrent waits for the named condition on the given entry to be
// True and for its ObservedGeneration to match the NSI's current metadata.generation.
// This mirrors conditions.IsTrue / isEntryResourceReleased in the controller,
// confirming that the test's status patch will actually be acted upon.
// Eventually is used to tolerate the informer-cache propagation window between
// a status Patch and when the cached client reflects the update.
func assertConditionCurrent(nsiKey client.ObjectKey, entryName string, condType conditions.ConditionType) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		got := &dpuservicev1.NodeServiceInterfaces{}
		g.Expect(testClient.Get(ctx, nsiKey, got)).To(Succeed())
		entryStatus := got.GetEntryStatus(entryName)
		g.Expect(entryStatus).NotTo(BeNil(), "entry %q should have a status on NSI %s", entryName, nsiKey)
		g.Expect(conditions.IsTrue(entryStatus, condType)).To(BeTrue(),
			"condition %q on entry %q should be True with ObservedGeneration matching NSI generation %d",
			condType, entryName, got.GetGeneration())
	}, timeout, interval).Should(Succeed())
}

// setEntryReady simulates a downstream reconciler setting Ready=True on an NSI entry.
// ObservedGeneration is set to the NSI's current generation.
func setEntryReady(nsi *dpuservicev1.NodeServiceInterfaces, entryName string) {
	cond := metav1.Condition{
		Type:               string(conditions.TypeReady),
		Status:             metav1.ConditionTrue,
		Reason:             string(conditions.ReasonSuccess),
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: nsi.GetGeneration(),
	}
	for i := range nsi.Status.InterfaceStatuses {
		if nsi.Status.InterfaceStatuses[i].Name != entryName {
			continue
		}
		nsi.Status.InterfaceStatuses[i].Conditions = append(
			nsi.Status.InterfaceStatuses[i].Conditions, cond)
		return
	}
	nsi.Status.InterfaceStatuses = append(nsi.Status.InterfaceStatuses, dpuservicev1.InterfaceEntryStatus{
		Name:       entryName,
		Conditions: []metav1.Condition{cond},
	})
}

// setEntryResourceReleased simulates the SFC/VPC reconciler setting ResourceReleased=True
// on a terminating NSI entry. ObservedGeneration is set to the NSI's current generation
// to satisfy the isEntryResourceReleased check.
func setEntryResourceReleased(nsi *dpuservicev1.NodeServiceInterfaces, entryName string) {
	cond := metav1.Condition{
		Type:               string(dpuservicev1.ResourceReleased),
		Status:             metav1.ConditionTrue,
		Reason:             string(conditions.ReasonSuccess),
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: nsi.GetGeneration(),
	}
	for i := range nsi.Status.InterfaceStatuses {
		if nsi.Status.InterfaceStatuses[i].Name != entryName {
			continue
		}
		nsi.Status.InterfaceStatuses[i].Conditions = append(
			nsi.Status.InterfaceStatuses[i].Conditions, cond)
		return
	}
	nsi.Status.InterfaceStatuses = append(nsi.Status.InterfaceStatuses, dpuservicev1.InterfaceEntryStatus{
		Name:       entryName,
		Conditions: []metav1.Condition{cond},
	})
}

// cleanupNSIPathTestResources tears down NSI-path test fixtures. ServiceInterfaceSet
// deletion requires NSI entries to be marked terminating and ResourceReleased before
// the finalizer is cleared; initiate that handshake before waiting on CleanupAndWait.
// nsiNamespace is the DPF-owned namespace where NSI objects live.
func cleanupNSIPathTestResources(ctx context.Context, c client.Client, nsiNamespace string, objs []client.Object) error {
	namespace := nsiNamespace
	sisObjects := make([]client.Object, 0, len(objs))
	otherObjects := make([]client.Object, 0, len(objs))
	for _, o := range objs {
		if _, ok := o.(*dpuservicev1.ServiceInterfaceSet); ok {
			sisObjects = append(sisObjects, o)
			continue
		}
		otherObjects = append(otherObjects, o)
	}

	for _, o := range sisObjects {
		if err := c.Delete(ctx, o); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	if len(sisObjects) > 0 {
		if err := wait.PollUntilContextTimeout(ctx, interval, timeout*10, true, func(ctx context.Context) (bool, error) {
			nsiList := &dpuservicev1.NodeServiceInterfacesList{}
			if err := c.List(ctx, nsiList, client.InNamespace(namespace)); err != nil {
				return false, err
			}
			for i := range nsiList.Items {
				nsi := &nsiList.Items[i]
				for _, entry := range nsi.Spec.Interfaces {
					if !entry.Terminating {
						return false, nil
					}
				}
			}
			return true, nil
		}); err != nil {
			return fmt.Errorf("waiting for NSI entries to become terminating: %w", err)
		}

		nsiList := &dpuservicev1.NodeServiceInterfacesList{}
		if err := c.List(ctx, nsiList, client.InNamespace(namespace)); err != nil {
			return err
		}
		for i := range nsiList.Items {
			nsi := nsiList.Items[i].DeepCopy()
			if len(nsi.Spec.Interfaces) == 0 {
				continue
			}
			for _, entry := range nsi.Spec.Interfaces {
				setEntryResourceReleased(nsi, entry.Name)
			}
			nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
			nsi.SetManagedFields(nil)
			if err := c.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test-cleanup")); err != nil {
				return err
			}
		}

		for _, o := range sisObjects {
			key := client.ObjectKeyFromObject(o)
			if err := wait.PollUntilContextTimeout(ctx, interval, timeout*10, true, func(ctx context.Context) (bool, error) {
				if err := c.Get(ctx, key, o); err != nil {
					if apierrors.IsNotFound(err) {
						return true, nil
					}
					return false, err
				}
				return false, nil
			}); err != nil {
				return fmt.Errorf("waiting for ServiceInterfaceSet %s deletion: %w", key, err)
			}
		}
	}

	nsiList := &dpuservicev1.NodeServiceInterfacesList{}
	if err := c.List(ctx, nsiList, client.InNamespace(namespace)); err != nil {
		return err
	}
	remaining := append([]client.Object{}, otherObjects...)
	for i := range nsiList.Items {
		remaining = append(remaining, &nsiList.Items[i])
	}
	return testutils.CleanupAndWait(ctx, c, remaining...)
}

var _ = Describe("ServiceInterfaceSet Controller — NSI path", func() {
	Context("When reconciling a new ServiceInterfaceSet", func() {
		var (
			cleanupObjects []client.Object
			testNS         *corev1.Namespace
		)
		BeforeEach(func() {
			cleanupObjects = []client.Object{}
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-nsi-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		AfterEach(func() {
			Expect(cleanupNSIPathTestResources(ctx, testClient, utils.NSIObjectsNamespace, cleanupObjects)).To(Succeed())
		})

		It("should select the NSI path and commit the mode annotation on first reconcile", func() {
			By("Creating a brand-new ServiceInterfaceSet (no pre-existing ServiceInterfaces)")
			sis := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{})
			cleanupObjects = append(cleanupObjects, sis)

			By("Waiting for the interface-mode annotation to be stamped as 'nsi'")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.ServiceInterfaceSet{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sis), got)).To(Succeed())
				g.Expect(got.Annotations).To(HaveKeyWithValue(interfaceModeAnnotation, interfaceModeNSI))
			}, timeout*3, interval).Should(Succeed())

			By("Verifying no legacy ServiceInterface objects are created")
			Consistently(func(g Gomega) {
				siList := &dpuservicev1.ServiceInterfaceList{}
				g.Expect(testClient.List(ctx, siList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(siList.Items).To(BeEmpty())
			}, 5*time.Second, interval).Should(Succeed())
		})

		It("should create one NSI entry per matched node", func() {
			By("Creating 2 matching nodes and 1 unmatched node")
			matchLabels := map[string]string{"role": "nsi-fw"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-node1", matchLabels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-node2", matchLabels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-node3", map[string]string{}))

			By("Creating a ServiceInterfaceSet selecting only the 2 matching nodes")
			sis := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: matchLabels})
			cleanupObjects = append(cleanupObjects, sis)

			By("Expecting two NSI objects, each with one entry from this set")
			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)
			Eventually(func(g Gomega) {
				for _, node := range []string{"nsi-node1", "nsi-node2"} {
					nsi := &dpuservicev1.NodeServiceInterfaces{}
					g.Expect(testClient.Get(ctx, client.ObjectKey{
						Namespace: utils.NSIObjectsNamespace,
						Name:      nsiName(node, dpuservicev1.NSITypeSFC),
					}, nsi)).To(Succeed())
					g.Expect(nsi.Spec.Node).To(Equal(node))
					g.Expect(nsi.Spec.Type).To(Equal(dpuservicev1.NSITypeSFC))
					names := make([]string, 0, len(nsi.Spec.Interfaces))
					for _, e := range nsi.Spec.Interfaces {
						names = append(names, e.Name)
					}
					g.Expect(names).To(ContainElement(entryName))
				}
			}, timeout*3, interval).Should(Succeed())

			By("Verifying no NSI for the unmatched node")
			Consistently(func(g Gomega) {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				err := testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("nsi-node3", dpuservicev1.NSITypeSFC),
				}, nsi)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, 5*time.Second, interval).Should(Succeed())
		})

		It("should mark an entry terminating when a node is deselected", func() {
			By("Creating 2 matching nodes")
			matchLabels := map[string]string{"role": "nsi-fw"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-node1", matchLabels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-node2", matchLabels))

			By("Creating a ServiceInterfaceSet")
			sis := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: matchLabels})
			cleanupObjects = append(cleanupObjects, sis)

			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)

			By("Waiting for both NSI entries to appear")
			Eventually(func(g Gomega) {
				for _, node := range []string{"nsi-node1", "nsi-node2"} {
					nsi := &dpuservicev1.NodeServiceInterfaces{}
					g.Expect(testClient.Get(ctx, client.ObjectKey{
						Namespace: utils.NSIObjectsNamespace,
						Name:      nsiName(node, dpuservicev1.NSITypeSFC),
					}, nsi)).To(Succeed())
					g.Expect(nsi.Spec.Interfaces).To(ContainElement(HaveField("Name", entryName)))
				}
			}, timeout*3, interval).Should(Succeed())

			By("Removing the label from nsi-node2 to deselect it")
			node2 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "nsi-node2"}}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(node2), node2)).To(Succeed())
			patched := node2.DeepCopy()
			patched.Labels = map[string]string{}
			Eventually(func() error {
				return testClient.Patch(ctx, patched, client.MergeFrom(node2))
			}, timeout, interval).Should(Succeed())

			By("Expecting the entry on nsi-node2 to be marked Terminating=true")
			Eventually(func(g Gomega) {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("nsi-node2", dpuservicev1.NSITypeSFC),
				}, nsi)).To(Succeed())
				g.Expect(nsi.Spec.Interfaces).To(ContainElement(And(
					HaveField("Name", entryName),
					HaveField("Terminating", BeTrue()),
				)), "entry should be present and Terminating")
			}, timeout*3, interval).Should(Succeed())
		})

		It("should remove the entry after ResourceReleased=True is set", func() {
			By("Creating 1 matching node")
			matchLabels := map[string]string{"role": "nsi-term"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-node1", matchLabels))

			By("Creating a ServiceInterfaceSet")
			sis := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: matchLabels})
			cleanupObjects = append(cleanupObjects, sis)

			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)

			By("Waiting for NSI entry to appear")
			Eventually(func(g Gomega) {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("nsi-node1", dpuservicev1.NSITypeSFC),
				}, nsi)).To(Succeed())
				g.Expect(nsi.Spec.Interfaces).To(ContainElement(HaveField("Name", entryName)))
			}, timeout*3, interval).Should(Succeed())

			By("Deselecting the node to trigger Terminating=true")
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "nsi-node1"}}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(node), node)).To(Succeed())
			patched := node.DeepCopy()
			patched.Labels = map[string]string{}
			Eventually(func() error {
				return testClient.Patch(ctx, patched, client.MergeFrom(node))
			}, timeout, interval).Should(Succeed())

			By("Waiting for Terminating=true on the entry")
			nsi := &dpuservicev1.NodeServiceInterfaces{}
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("nsi-node1", dpuservicev1.NSITypeSFC),
				}, nsi)).To(Succeed())
				g.Expect(nsi.Spec.Interfaces).To(ContainElement(And(
					HaveField("Name", entryName),
					HaveField("Terminating", BeTrue()),
				)), "entry should be present and Terminating")
			}, timeout*3, interval).Should(Succeed())

			By("Verifying the entry stays Terminating and is not removed before ResourceReleased is set")
			Consistently(func(g Gomega) {
				got := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("nsi-node1", dpuservicev1.NSITypeSFC),
				}, got)).To(Succeed())
				g.Expect(got.Spec.Interfaces).To(ContainElement(And(
					HaveField("Name", entryName),
					HaveField("Terminating", BeTrue()),
				)), "entry should remain Terminating until ResourceReleased is set")
			}, 5*time.Second, interval).Should(Succeed())

			By("Simulating the SFC reconciler setting ResourceReleased=True")
			setEntryResourceReleased(nsi, entryName)
			nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
			nsi.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test-sfc"))).To(Succeed())

			By("Expecting the NSI to be deleted — the single entry was removed leaving it empty")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("nsi-node1", dpuservicev1.NSITypeSFC),
				}, &dpuservicev1.NodeServiceInterfaces{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout*3, interval).Should(Succeed())
		})

		It("should delete the ServiceInterfaceSet after all entries are released", func() {
			By("Creating 2 matching nodes")
			matchLabels := map[string]string{"role": "nsi-del"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-node1", matchLabels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-node2", matchLabels))

			By("Creating a ServiceInterfaceSet")
			sis := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: matchLabels})
			cleanupObjects = append(cleanupObjects, sis)

			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)

			By("Waiting for both NSI entries")
			nsiObjects := map[string]*dpuservicev1.NodeServiceInterfaces{}
			Eventually(func(g Gomega) {
				for _, node := range []string{"nsi-node1", "nsi-node2"} {
					nsi := &dpuservicev1.NodeServiceInterfaces{}
					g.Expect(testClient.Get(ctx, client.ObjectKey{
						Namespace: utils.NSIObjectsNamespace,
						Name:      nsiName(node, dpuservicev1.NSITypeSFC),
					}, nsi)).To(Succeed())
					g.Expect(nsi.Spec.Interfaces).To(ContainElement(HaveField("Name", entryName)))
					nsiObjects[node] = nsi.DeepCopy()
				}
			}, timeout*3, interval).Should(Succeed())

			By("Deleting the ServiceInterfaceSet")
			Expect(testClient.Delete(ctx, sis)).To(Succeed())

			By("Expecting entries to be marked Terminating=true")
			Eventually(func(g Gomega) {
				for _, node := range []string{"nsi-node1", "nsi-node2"} {
					nsi := &dpuservicev1.NodeServiceInterfaces{}
					g.Expect(testClient.Get(ctx, client.ObjectKey{
						Namespace: utils.NSIObjectsNamespace,
						Name:      nsiName(node, dpuservicev1.NSITypeSFC),
					}, nsi)).To(Succeed())
					g.Expect(nsi.Spec.Interfaces).To(ContainElement(And(
						HaveField("Name", entryName),
						HaveField("Terminating", BeTrue()),
					)), "entry on %s should be Terminating", node)
					nsiObjects[node] = nsi.DeepCopy()
				}
			}, timeout*3, interval).Should(Succeed())

			By("Verifying the ServiceInterfaceSet is not deleted while entries await release — finalizer holds")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sis), &dpuservicev1.ServiceInterfaceSet{})).To(Succeed())
			}, 5*time.Second, interval).Should(Succeed())

			By("Simulating ResourceReleased=True on all entries")
			for _, nsi := range nsiObjects {
				setEntryResourceReleased(nsi, entryName)
				nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
				nsi.SetManagedFields(nil)
				Expect(testClient.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test-sfc"))).To(Succeed())
			}

			By("Expecting the ServiceInterfaceSet to be fully deleted")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.ServiceInterfaceSet{}
				err := testClient.Get(ctx, client.ObjectKeyFromObject(sis), got)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout*10, interval).Should(Succeed())
		})

		It("should reflect NSI entry readiness in ServiceInterfaceSet conditions", func() {
			By("Creating 1 matching node")
			matchLabels := map[string]string{"role": "nsi-ready"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-node1", matchLabels))

			By("Creating a ServiceInterfaceSet")
			sis := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: matchLabels})
			cleanupObjects = append(cleanupObjects, sis)

			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)

			By("Waiting for the NSI entry to appear")
			nsi := &dpuservicev1.NodeServiceInterfaces{}
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("nsi-node1", dpuservicev1.NSITypeSFC),
				}, nsi)).To(Succeed())
				g.Expect(nsi.Spec.Interfaces).To(ContainElement(HaveField("Name", entryName)))
			}, timeout*3, interval).Should(Succeed())

			By("Checking that the ServiceInterfaceSet is Reconciled=True but Ready=False (entry not yet ready)")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.ServiceInterfaceSet{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sis), got)).To(Succeed())
				g.Expect(got.Status.Conditions).To(ContainElement(And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionTrue),
				)))
				g.Expect(got.Status.Conditions).To(ContainElement(And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReady)),
					HaveField("Status", metav1.ConditionFalse),
				)))
				g.Expect(got.Status.Conditions).To(ContainElement(And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
				)))
			}, timeout*3, interval).Should(Succeed())

			By("Verifying the ServiceInterfaceSet stays Ready=False before we act — no spurious ready flip")
			Consistently(func(g Gomega) {
				got := &dpuservicev1.ServiceInterfaceSet{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sis), got)).To(Succeed())
				g.Expect(got.Status.Conditions).To(ContainElement(And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReady)),
					HaveField("Status", metav1.ConditionFalse),
				)))
			}, 3*time.Second, interval).Should(Succeed())

			By("Simulating the SFC reconciler marking the NSI entry as Ready")
			setEntryReady(nsi, entryName)
			nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
			nsi.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test-sfc"))).To(Succeed())

			By("Checking that the ServiceInterfaceSet transitions to Ready=True")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.ServiceInterfaceSet{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sis), got)).To(Succeed())
				g.Expect(got.Status.Conditions).To(ContainElement(And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				)))
				g.Expect(got.Status.Conditions).To(ContainElement(And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				)))
				g.Expect(got.Status.Conditions).To(ContainElement(And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				)))
			}, timeout*3, interval).Should(Succeed())
		})

		It("should isolate field managers and only delete the NSI when the last entry is removed", func() {
			By("Creating 1 node selected by both Set-A and Set-B")
			matchLabels := map[string]string{"role": "nsi-multi-fm"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-multi-fm-node", matchLabels))

			By("Creating a second namespace for Set-B")
			testNSB := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-nsi-b-"}}
			Expect(testClient.Create(ctx, testNSB)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNSB)

			entryNameA := interfaceEntryName(testNS.Name, svcIfcSetName)
			entryNameB := interfaceEntryName(testNSB.Name, svcIfcSetName)
			nsiKey := client.ObjectKey{
				Namespace: utils.NSIObjectsNamespace,
				Name:      nsiName("nsi-multi-fm-node", dpuservicev1.NSITypeSFC),
			}
			nsi := &dpuservicev1.NodeServiceInterfaces{}

			By("Creating Set-A and waiting for the NSI to be created at generation 1")
			setA := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: matchLabels})
			cleanupObjects = append(cleanupObjects, setA)
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, nsiKey, nsi)).To(Succeed())
				g.Expect(nsi.Spec.Interfaces).To(ContainElement(HaveField("Name", entryNameA)))
			}, timeout*3, interval).Should(Succeed())
			Expect(nsi.Generation).To(Equal(int64(1)), "NSI should be created at generation 1")
			Expect(nsi.GetEntryStatus(entryNameA)).To(BeNil(), "Set-A entry should have no status conditions yet")

			By("Marking Set-A's entry as Ready and verifying the condition")
			setEntryReady(nsi, entryNameA)
			nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
			nsi.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test-sfc"))).To(Succeed())
			assertConditionCurrent(nsiKey, entryNameA, conditions.TypeReady)

			By("Creating Set-B and waiting for its entry — NSI generation must increment to 2")
			setB := createServiceInterfaceSet(ctx, testNSB.Name, &metav1.LabelSelector{MatchLabels: matchLabels})
			cleanupObjects = append(cleanupObjects, setB)
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, nsiKey, nsi)).To(Succeed())
				names := make([]string, 0, len(nsi.Spec.Interfaces))
				for _, e := range nsi.Spec.Interfaces {
					names = append(names, e.Name)
				}
				g.Expect(names).To(ContainElements(entryNameA, entryNameB))
			}, timeout*3, interval).Should(Succeed())
			Expect(nsi.Generation).To(Equal(int64(2)), "adding Set-B's entry must bump NSI to generation 2")
			Expect(nsi.GetEntryStatus(entryNameB)).To(BeNil(), "Set-B entry should have no status conditions yet")

			By("Marking Set-B's entry as Ready and verifying the condition")
			setEntryReady(nsi, entryNameB)
			nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
			nsi.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test-sfc"))).To(Succeed())
			assertConditionCurrent(nsiKey, entryNameB, conditions.TypeReady)

			By("Deleting Set-A and waiting for its entry to become Terminating — generation must increase beyond 2")
			Expect(testClient.Delete(ctx, setA)).To(Succeed())
			Eventually(func(g Gomega) {
				got := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, nsiKey, got)).To(Succeed())
				g.Expect(got.Spec.Interfaces).To(ContainElement(And(
					HaveField("Name", entryNameA),
					HaveField("Terminating", BeTrue()),
				)), "Set-A entry should be Terminating")
				nsi = got.DeepCopy()
			}, timeout*3, interval).Should(Succeed())
			Expect(nsi.Generation).To(Equal(int64(3)), "marking Set-A Terminating must bump generation to 3")

			By("Verifying the NSI stays at generation 3 — Set-A's entry must not be removed before ResourceReleased is set")
			Consistently(func(g Gomega) {
				got := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, nsiKey, got)).To(Succeed())
				g.Expect(got.Generation).To(Equal(int64(3)), "NSI generation must not advance before ResourceReleased")
				g.Expect(got.Spec.Interfaces).To(ContainElement(And(
					HaveField("Name", entryNameA),
					HaveField("Terminating", BeTrue()),
				)), "Set-A's entry should remain Terminating until ResourceReleased is set")
			}, 5*time.Second, interval).Should(Succeed())

			By("Releasing Set-A's entry")
			setEntryResourceReleased(nsi, entryNameA)
			nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
			nsi.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test-sfc"))).To(Succeed())

			By("Expecting Set-A's entry to be removed while Set-B's entry is preserved — generation must increase")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, nsiKey, got)).To(Succeed())
				names := make([]string, 0, len(got.Spec.Interfaces))
				for _, e := range got.Spec.Interfaces {
					names = append(names, e.Name)
				}
				g.Expect(names).NotTo(ContainElement(entryNameA), "Set-A's entry should be removed")
				g.Expect(names).To(ContainElement(entryNameB), "Set-B's entry should remain")
				nsi = got.DeepCopy()
			}, timeout*3, interval).Should(Succeed())
			Expect(nsi.Generation).To(Equal(int64(4)), "removing Set-A's entry must bump generation to 4")

			By("Verifying Set-A is fully deleted — its finalizer was released")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(setA), &dpuservicev1.ServiceInterfaceSet{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "Set-A should be fully deleted")
			}, timeout*5, interval).Should(Succeed())

			By("Deleting Set-B and waiting for its entry — the last one — to become Terminating — generation must increase")
			Expect(testClient.Delete(ctx, setB)).To(Succeed())
			Eventually(func(g Gomega) {
				got := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, nsiKey, got)).To(Succeed())
				g.Expect(got.Spec.Interfaces).To(ContainElement(And(
					HaveField("Name", entryNameB),
					HaveField("Terminating", BeTrue()),
				)), "Set-B entry should be Terminating")
				nsi = got.DeepCopy()
			}, timeout*3, interval).Should(Succeed())
			Expect(nsi.Generation).To(Equal(int64(5)), "marking Set-B Terminating must bump generation to 5")

			By("Verifying the NSI stays at generation 5 — Set-B's entry must not be removed before ResourceReleased is set")
			Consistently(func(g Gomega) {
				got := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, nsiKey, got)).To(Succeed())
				g.Expect(got.Generation).To(Equal(int64(5)), "NSI generation must not advance before ResourceReleased")
				g.Expect(got.Spec.Interfaces).To(ContainElement(And(
					HaveField("Name", entryNameB),
					HaveField("Terminating", BeTrue()),
				)), "Set-B's entry should remain Terminating until ResourceReleased is set")
			}, 5*time.Second, interval).Should(Succeed())

			By("Releasing Set-B's entry — the last one")
			setEntryResourceReleased(nsi, entryNameB)
			nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
			nsi.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test-sfc"))).To(Succeed())

			By("Expecting the NSI to be deleted now that it has no remaining entries")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, nsiKey, &dpuservicev1.NodeServiceInterfaces{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "NSI should be deleted after its last entry is removed")
			}, timeout*3, interval).Should(Succeed())
		})

		It("should not create duplicate entries when the same field manager reconciles multiple times", func() {
			By("Creating 1 matching node")
			matchLabels := map[string]string{"role": "nsi-idem"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "nsi-idem-node", matchLabels))

			By("Creating a ServiceInterfaceSet")
			sis := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: matchLabels})
			cleanupObjects = append(cleanupObjects, sis)

			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)
			nsiKey := client.ObjectKey{
				Namespace: utils.NSIObjectsNamespace,
				Name:      nsiName("nsi-idem-node", dpuservicev1.NSITypeSFC),
			}

			By("Waiting for exactly one NSI entry to be created")
			Eventually(func(g Gomega) {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, nsiKey, nsi)).To(Succeed())
				count := 0
				for _, e := range nsi.Spec.Interfaces {
					if e.Name == entryName {
						count++
					}
				}
				g.Expect(count).To(Equal(1), "there should be exactly one entry for this SIS")
			}, timeout*3, interval).Should(Succeed())

			By("Triggering additional reconcile cycles by annotating the ServiceInterfaceSet")
			for i := range 3 {
				got := &dpuservicev1.ServiceInterfaceSet{}
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sis), got)).To(Succeed())
				updated := got.DeepCopy()
				if updated.Annotations == nil {
					updated.Annotations = map[string]string{}
				}
				updated.Annotations["test-reconcile-trigger"] = fmt.Sprintf("%d", i)
				Expect(testClient.Patch(ctx, updated, client.MergeFrom(got))).To(Succeed())
				time.Sleep(2 * interval)
			}

			By("Verifying the NSI always has exactly one entry for this ServiceInterfaceSet (SSA is idempotent)")
			Consistently(func(g Gomega) {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, nsiKey, nsi)).To(Succeed())
				count := 0
				for _, e := range nsi.Spec.Interfaces {
					if e.Name == entryName {
						count++
					}
				}
				g.Expect(count).To(Equal(1), "entry should appear exactly once even after multiple reconciles")
			}, 5*time.Second, interval).Should(Succeed())
		})
	})
})
