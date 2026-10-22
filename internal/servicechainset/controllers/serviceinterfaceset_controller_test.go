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

package controller //nolint:dupl

import (
	"context"
	"sort"
	"testing"
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
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	svcIfcSetName = "svc-if-set"
)

//nolint:dupl
var _ = Describe("ServiceInterfaceSet Controller", func() {
	Context("When reconciling a resource", func() {
		var (
			cleanupObjects []client.Object
			testNS         *corev1.Namespace
		)
		BeforeEach(func() {
			cleanupObjects = []client.Object{}
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(cleanupNSIPathTestResources(ctx, testClient, cleanupObjects)).To(Succeed())
			DeferCleanup(cleanServiceInterfaces, testNS.Name)
		})
		It("should successfully reconcile the ServiceInterfaceSet without Node Selector", func() {
			By("Create ServiceInterfaceSet, without Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{}))
			By("Verify no NSI entries yet, no nodes")
			Consistently(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(BeEmpty())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes")
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2", "node3"))
			}, timeout*3, interval).Should(Succeed())
		})
		It("should successfully reconcile the ServiceInterfaceSet with Node Selector", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes, 2 matches")
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2"))
			}, timeout*3, interval).Should(Succeed())
		})
		It("should successfully reconcile the ServiceInterfaceSet with Node Selector and mark deselected entry terminating", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", labels))

			By("Reconciling the created resource, 3 nodes, 3 matches")
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2", "node3"))
			}, timeout*3, interval).Should(Succeed())

			By("Patch Node-3 label to not be selected")
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node3"}}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(node), node)).To(Succeed())
			patch := node.DeepCopy()
			patch.Labels = make(map[string]string)
			Eventually(func() error {
				return testClient.Patch(ctx, patch, client.MergeFrom(node))
			}, timeout, interval).Should(Succeed())

			By("Expecting node3 entry Terminating and only 2 active entries")
			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2"))
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("node3", dpuservicev1.NSITypeSFC),
				}, nsi)).To(Succeed())
				g.Expect(nsi.Spec.Interfaces).To(ContainElement(And(
					HaveField("Name", entryName),
					HaveField("Terminating", BeTrue()),
				)))
			}, timeout*3, interval).Should(Succeed())
		})
		It("should successfully reconcile the ServiceInterfaceSet after update", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes, 2 matches")
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2"))
			}, timeout*3, interval).Should(Succeed())

			By("patch ServiceInterfaceSet Spec")
			sis := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: svcIfcSetName, Namespace: testNS.Name}}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sis), sis)).To(Succeed())
			patch := sis.DeepCopy()
			updatedSpec := &dpuservicev1.ServiceInterfaceSpec{
				InterfaceType: dpuservicev1.InterfaceTypeVLAN,
				Vlan: &dpuservicev1.VLAN{
					VlanID:             100,
					ParentInterfaceRef: "p7",
				},
			}
			patch.Spec.Template.Spec = *updatedSpec
			Eventually(func() error {
				return testClient.Patch(ctx, patch, client.MergeFrom(sis))
			}, timeout, interval).Should(Succeed())

			By("Reconciling the updated resource onto NSI entries")
			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)
			Eventually(func(g Gomega) {
				for _, node := range []string{"node1", "node2"} {
					nsi := &dpuservicev1.NodeServiceInterfaces{}
					g.Expect(testClient.Get(ctx, client.ObjectKey{
						Namespace: utils.NSIObjectsNamespace,
						Name:      nsiName(node, dpuservicev1.NSITypeSFC),
					}, nsi)).To(Succeed())
					var entry *dpuservicev1.InterfaceEntry
					for i := range nsi.Spec.Interfaces {
						if nsi.Spec.Interfaces[i].Name == entryName {
							entry = &nsi.Spec.Interfaces[i]
							break
						}
					}
					g.Expect(entry).NotTo(BeNil())
					g.Expect(entry.InterfaceType).To(Equal(dpuservicev1.InterfaceTypeVLAN))
					g.Expect(entry.Vlan).NotTo(BeNil())
					g.Expect(entry.Vlan.VlanID).To(Equal(100))
					g.Expect(entry.Vlan.ParentInterfaceRef).To(Equal("p7-" + node))
				}
			}, timeout*3, interval).Should(Succeed())
		})
		It("should successfully delete the ServiceInterfaceSet", func() {
			By("Creating ServiceInterfaceSet, with Node Selector")
			sis := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}})
			cleanupObjects = append(cleanupObjects, sis)

			By("Creating 2 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))

			By("Reconciling the created resource, 2 nodes, 2 matches")
			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2"))
			}, timeout*3, interval).Should(Succeed())

			By("Deleting ServiceInterfaceSet")
			Expect(testClient.Delete(ctx, sis)).NotTo(HaveOccurred())

			By("Waiting for NSI entries to become Terminating")
			Eventually(func(g Gomega) {
				for _, node := range []string{"node1", "node2"} {
					nsi := &dpuservicev1.NodeServiceInterfaces{}
					g.Expect(testClient.Get(ctx, client.ObjectKey{
						Namespace: utils.NSIObjectsNamespace,
						Name:      nsiName(node, dpuservicev1.NSITypeSFC),
					}, nsi)).To(Succeed())
					g.Expect(nsi.Spec.Interfaces).To(ContainElement(And(
						HaveField("Name", entryName),
						HaveField("Terminating", BeTrue()),
					)))
				}
			}, timeout*3, interval).Should(Succeed())

			By("Verifying the ServiceInterfaceSet is not deleted while entries await release — finalizer holds")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sis), &dpuservicev1.ServiceInterfaceSet{})).To(Succeed())
			}, 5*time.Second, interval).Should(Succeed())

			By("Releasing NSI entries")
			for _, node := range []string{"node1", "node2"} {
				nsiKey := client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName(node, dpuservicev1.NSITypeSFC),
				}
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				Expect(testClient.Get(ctx, nsiKey, nsi)).To(Succeed())
				setEntryResourceReleased(nsi, entryName)
				nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
				nsi.SetManagedFields(nil)
				Expect(testClient.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
			}

			By("Verifying ServiceInterfaceSet is deleted")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(sis), &dpuservicev1.ServiceInterfaceSet{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout*10, interval).Should(Succeed())
		})
		It("ServiceInterfaceSet has condition ServiceInterfacesReconciled with AwaitingDeletion Reason when NSI entries are not released", func() {
			By("Creating ServiceInterfaceSet, with Node Selector")
			set := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}})
			cleanupObjects = append(cleanupObjects, set)

			By("Creating 2 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))

			By("Ensuring that the ServiceInterfaceSet has been reconciled successfully")
			Eventually(func(g Gomega) []metav1.Condition {
				got := &dpuservicev1.ServiceInterfaceSet{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: set.Namespace, Name: set.Name}, got)).To(Succeed())
				return got.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ContainElement(
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionTrue),
				),
			))

			By("Deleting the ServiceInterfaceSet without releasing NSI entries")
			Expect(testClient.Delete(ctx, set)).To(Succeed())

			By("Checking the deleted condition is added")
			// Terminating NSI entries are skipped by updateSummaryNSI, so ServiceInterfacesReady
			// can be True while Reconciled stays AwaitingDeletion until ResourceReleased.
			Eventually(func(g Gomega) []metav1.Condition {
				got := &dpuservicev1.ServiceInterfaceSet{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: set.Namespace, Name: set.Name}, got)).To(Succeed())
				return got.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(And(
				ContainElement(And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				)),
				ContainElement(And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				)),
			))
		})
		It("should successfully reconcile the ServiceInterfaceSet with maximum name length", func() {
			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Create ServiceInterfaceSet, without Node Selector")
			sis := getServiceInterfaceSet(testNS.Name, &metav1.LabelSelector{})
			sis.Name = utilrand.String(63)
			Expect(testClient.Create(ctx, sis)).NotTo(HaveOccurred())
			cleanupObjects = append(cleanupObjects, sis)

			By("Checking that NSI entries are created for all nodes")
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, sis.Name)).To(ConsistOf("node1", "node2", "node3"))
			}, timeout*3, interval).Should(Succeed())
		})
		It("should fail to create a ServiceInterfaceSet with name exceeding the maximum length", func() {
			By("Create ServiceInterfaceSet, without Node Selector")
			sis := getServiceInterfaceSet(testNS.Name, &metav1.LabelSelector{})
			sis.Name = utilrand.String(64)
			Expect(testClient.Create(ctx, sis)).To(HaveOccurred())
		})
		It("should set parentInterfaceRef field for ServiceInterface type VF", func() {
			By("Create a node")
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", nil))

			By("Create ServiceInterfaceSet of type VF with parentInterfaceRef field set")
			sis := serviceInterfaceSpec(testNS.Name, nil)
			sis.Spec.Template.Spec = getTypedTestServiceInterfaceSpec(dpuservicev1.InterfaceTypeVF, nil)
			sis.Spec.Template.Spec.VF.ParentInterfaceRef = ptr.To("p100")
			Expect(testClient.Create(ctx, sis)).NotTo(HaveOccurred())
			cleanupObjects = append(cleanupObjects, sis)

			By("Reconciling the created resource onto NSI")
			entryName := interfaceEntryName(testNS.Name, sis.Name)
			Eventually(func(g Gomega) {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("node1", dpuservicev1.NSITypeSFC),
				}, nsi)).To(Succeed())
				var entry *dpuservicev1.InterfaceEntry
				for i := range nsi.Spec.Interfaces {
					if nsi.Spec.Interfaces[i].Name == entryName {
						entry = &nsi.Spec.Interfaces[i]
						break
					}
				}
				g.Expect(entry).NotTo(BeNil())
				g.Expect(entry.VF).NotTo(BeNil())
				g.Expect(entry.VF.ParentInterfaceRef).To(Equal(ptr.To("p100-node1")))
			}, timeout*3, interval).Should(Succeed())
		})
		It("should set virtualNetwork field for ServiceInterface type PF", func() {
			By("Create a node")
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", nil))

			By("Create VPC ServiceInterfaceSet with virtualNetwork (sticky legacy)")
			cleanupObjects = append(cleanupObjects, createLegacyTypedServiceInterfaceSet(ctx, testNS.Name, nil, dpuservicev1.InterfaceTypePF, ptr.To("myvnet")))

			By("Reconciling the created resource")
			Eventually(func(g Gomega) {
				serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				g.Expect(testClient.List(ctx, serviceInterfaceList)).NotTo(HaveOccurred())
				g.Expect(serviceInterfaceList.Items).To(HaveLen(1))
				si := serviceInterfaceList.Items[0]
				g.Expect(si.Spec.PF.VirtualNetwork).To(Equal(ptr.To("myvnet")))
			}, timeout*3, interval).Should(Succeed())
		})
		It("should set virtualNetwork field for ServiceInterface type VF", func() {
			By("Create a node")
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", nil))

			By("Create VPC ServiceInterfaceSet with virtualNetwork (sticky legacy)")
			cleanupObjects = append(cleanupObjects, createLegacyTypedServiceInterfaceSet(ctx, testNS.Name, nil, dpuservicev1.InterfaceTypeVF, ptr.To("myvnet")))

			By("Reconciling the created resource")
			Eventually(func(g Gomega) {
				serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				g.Expect(testClient.List(ctx, serviceInterfaceList)).NotTo(HaveOccurred())
				g.Expect(serviceInterfaceList.Items).To(HaveLen(1))
				si := serviceInterfaceList.Items[0]
				g.Expect(si.Spec.VF.VirtualNetwork).To(Equal(ptr.To("myvnet")))
			}, timeout*3, interval).Should(Succeed())
		})
		It("should set virtualNetwork field for ServiceInterface type Service", func() {
			By("Create a node")
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", nil))

			By("Create VPC ServiceInterfaceSet with virtualNetwork (sticky legacy)")
			cleanupObjects = append(cleanupObjects, createLegacyTypedServiceInterfaceSet(ctx, testNS.Name, nil, dpuservicev1.InterfaceTypeService, ptr.To("myvnet")))

			By("Reconciling the created resource")
			Eventually(func(g Gomega) {
				serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				g.Expect(testClient.List(ctx, serviceInterfaceList)).NotTo(HaveOccurred())
				g.Expect(serviceInterfaceList.Items).To(HaveLen(1))
				si := serviceInterfaceList.Items[0]
				g.Expect(si.Spec.Service.VirtualNetwork).To(Equal(ptr.To("myvnet")))
			}, timeout*3, interval).Should(Succeed())
		})
		It("should set patch fields for ServiceInterface type Patch", func() {
			By("Create a node")
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", nil))

			By("Create ServiceInterfaceSet with patch fields")
			sis := serviceInterfaceSpec(testNS.Name, nil)
			peerExternalIDs := map[string]string{
				"key1": "value1",
				"key2": "value2",
			}
			sis.Spec.Template.Spec = getTypedTestServiceInterfaceSpec(dpuservicev1.InterfaceTypePatch, nil)
			sis.Spec.Template.Spec.Patch.PeerPatchName = ptr.To("custom-patch-name")
			sis.Spec.Template.Spec.Patch.PeerExternalIDs = peerExternalIDs
			Expect(testClient.Create(ctx, sis)).NotTo(HaveOccurred())
			cleanupObjects = append(cleanupObjects, sis)

			By("Reconciling the created resource onto NSI")
			entryName := interfaceEntryName(testNS.Name, sis.Name)
			Eventually(func(g Gomega) {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("node1", dpuservicev1.NSITypeSFC),
				}, nsi)).To(Succeed())
				var entry *dpuservicev1.InterfaceEntry
				for i := range nsi.Spec.Interfaces {
					if nsi.Spec.Interfaces[i].Name == entryName {
						entry = &nsi.Spec.Interfaces[i]
						break
					}
				}
				g.Expect(entry).NotTo(BeNil())
				g.Expect(entry.Patch).NotTo(BeNil())
				g.Expect(entry.Patch.PeerBridge).To(Equal("br-ext"))
				g.Expect(entry.Patch.PeerPatchName).To(Equal(ptr.To("custom-patch-name")))
				g.Expect(entry.Patch.PeerExternalIDs).To(Equal(peerExternalIDs))
			}, timeout*3, interval).Should(Succeed())
		})
		//nolint:goconst
		It("should apply template labels and annotations onto NSI entries", func() {
			fooAnnotKey := "foo-annot"
			fooAnnotValue := "value"
			fooLabelKey := "foo-label"
			fooLabelValue := "value"

			By("Create a node")
			nodeLabels := map[string]string{"my-label": "node1"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", nodeLabels))

			By("Create a ServiceInterfaceSet with labels and annotations")
			sis := getServiceInterfaceSet(testNS.Name, &metav1.LabelSelector{MatchLabels: nodeLabels})
			sis.Spec.Template.Annotations = map[string]string{fooAnnotKey: fooAnnotValue}
			sis.Spec.Template.Labels = map[string]string{fooLabelKey: fooLabelValue}
			Expect(testClient.Create(ctx, sis)).ToNot(HaveOccurred())
			cleanupObjects = append(cleanupObjects, sis)

			By("Verify NSI entry has expected labels and annotations")
			entryName := interfaceEntryName(testNS.Name, sis.Name)
			Eventually(func(g Gomega) {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName("node1", dpuservicev1.NSITypeSFC),
				}, nsi)).To(Succeed())
				var entry *dpuservicev1.InterfaceEntry
				for i := range nsi.Spec.Interfaces {
					if nsi.Spec.Interfaces[i].Name == entryName {
						entry = &nsi.Spec.Interfaces[i]
						break
					}
				}
				g.Expect(entry).NotTo(BeNil())
				g.Expect(entry.Annotations).To(HaveKeyWithValue(fooAnnotKey, fooAnnotValue))
				g.Expect(entry.Labels).To(HaveKeyWithValue(fooLabelKey, fooLabelValue))
			}, timeout*3, interval).Should(Succeed())
		})
		It("should not patch NSI entries that are already up to date", func() {
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: labels}))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))

			By("Waiting for the NSI entries to be created")
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2"))
			}, timeout*3, interval).Should(Succeed())

			By("Reconciling again and expecting no NSI PATCH")
			var childPatches []string
			c, err := client.NewWithWatch(cfg, client.Options{Scheme: testClient.Scheme(), Cache: &client.CacheOptions{Reader: testClient}})
			Expect(err).NotTo(HaveOccurred())
			r := &ServiceInterfaceSetReconciler{
				Client: interceptor.NewClient(c, interceptor.Funcs{
					Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						if _, ok := obj.(*dpuservicev1.ServiceInterfaceSet); !ok {
							childPatches = append(childPatches, client.ObjectKeyFromObject(obj).String())
						}
						return c.Patch(ctx, obj, patch, opts...)
					},
				}),
				Scheme: testClient.Scheme(),
			}
			req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNS.Name, Name: svcIfcSetName}}
			Eventually(func(g Gomega) {
				childPatches = nil
				_, err := r.Reconcile(ctx, req)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(childPatches).To(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})
		It("should restore an NSI entry that drifted from the template", func() {
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: labels}))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))

			By("Waiting for the NSI entry to be created")
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1"))
			}, timeout*3, interval).Should(Succeed())

			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)
			nsiKey := client.ObjectKey{Namespace: utils.NSIObjectsNamespace, Name: nsiName("node1", dpuservicev1.NSITypeSFC)}
			nsi := &dpuservicev1.NodeServiceInterfaces{}
			Expect(testClient.Get(ctx, nsiKey, nsi)).To(Succeed())

			By("Changing the entry's VF ID with another field manager")
			drifted := nsi.DeepCopy()
			changed := 0
			for i := range drifted.Spec.Interfaces {
				if drifted.Spec.Interfaces[i].Name == entryName {
					drifted.Spec.Interfaces[i].VF.VFID = 7
					changed++
				}
			}
			Expect(changed).To(Equal(1))
			Expect(testClient.Patch(ctx, drifted, client.MergeFrom(nsi), client.FieldOwner("test-drift"))).To(Succeed())

			By("Expecting the controller to restore the entry")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.NodeServiceInterfaces{}
				g.Expect(testClient.Get(ctx, nsiKey, got)).To(Succeed())
				g.Expect(got.Spec.Interfaces).To(ContainElement(And(
					HaveField("Name", entryName),
					HaveField("VF.VFID", 0),
				)))
			}, timeout*3, interval).Should(Succeed())
		})
		It("should reactivate a terminating NSI entry when its node is selected again", func() {
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: labels}))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))

			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2"))
			}, timeout*3, interval).Should(Succeed())

			setNodeLabels := func(name string, nodeLabels map[string]string) {
				node := &corev1.Node{}
				Expect(testClient.Get(ctx, client.ObjectKey{Name: name}, node)).To(Succeed())
				patch := node.DeepCopy()
				patch.Labels = nodeLabels
				Eventually(func() error {
					return testClient.Patch(ctx, patch, client.MergeFrom(node))
				}, timeout, interval).Should(Succeed())
			}

			By("Deselecting node2")
			setNodeLabels("node2", map[string]string{})
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1"))
			}, timeout*3, interval).Should(Succeed())

			By("Selecting node2 again")
			setNodeLabels("node2", labels)
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2"))
			}, timeout*3, interval).Should(Succeed())
		})
	})

	Context("Validating ServiceInterfaceSet creation", func() {
		var (
			cleanupObjects []client.Object
			testNS         *corev1.Namespace
		)
		BeforeEach(func() {
			cleanupObjects = []client.Object{}
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(cleanupNSIPathTestResources(ctx, testClient, cleanupObjects)).To(Succeed())
			DeferCleanup(cleanServiceInterfaces, testNS.Name)
		})
		It("should successfully create the ServiceInterfaceSet with vlan interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeVLAN))
		})
		It("should successfully create the ServiceInterfaceSet with pf interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypePF))
		})
		It("should successfully create the ServiceInterfaceSet with vf interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeVF))
		})
		It("should successfully create the ServiceInterfaceSet with physical interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypePhysical))
		})
		It("should successfully create the ServiceInterfaceSet with patch interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypePatch))
		})
		It("should successfully create the ServiceInterfaceSet with service interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeService))
		})

		It("should fail to create the ServiceInterfaceSet with missing vlan interface", func() {
			createInvalidTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeVLAN)
		})
		It("should fail to create the ServiceInterfaceSet with missing pf interface", func() {
			createInvalidTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypePF)
		})
		It("should fail to create the ServiceInterfaceSet with missing vf interface", func() {
			createInvalidTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeVF)
		})
		It("should fail to create the ServiceInterfaceSet with missing physical interface", func() {
			createInvalidTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypePhysical)
		})
		It("should fail to create the ServiceInterfaceSet with missing service definition", func() {
			createInvalidTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeService)
		})
		It("should fail to create the ServiceInterfaceSet with missing patch interface", func() {
			createInvalidTypedServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypePatch)
		})
		It("should successfully create the ServiceInterfaceSet and have all conditions set", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			obj := createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}})
			cleanupObjects = append(cleanupObjects, obj)
			Eventually(func(g Gomega) {
				assertServiceInterfaceSetCondition(g, testClient, obj)
			}).WithTimeout(30 * time.Second).Should(BeNil())
		})
	})

	Context("When checking the status transitions", func() {
		var (
			cleanupObjects []client.Object
			testNS         *corev1.Namespace
		)
		BeforeEach(func() {
			cleanupObjects = []client.Object{}
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(cleanupNSIPathTestResources(ctx, testClient, cleanupObjects)).To(Succeed())
			DeferCleanup(cleanServiceInterfaces, testNS.Name)
		})
		It("ServiceInterfaceSet has condition with Pending Reason after object creation", func() {
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes, 2 matches")
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2"))
			}, timeout*3, interval).Should(Succeed())

			Eventually(func(g Gomega) []metav1.Condition {
				obj := &dpuservicev1.ServiceInterfaceSet{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: svcIfcSetName}, obj)).To(Succeed())
				g.Expect(obj.Status.Conditions).ToNot(BeEmpty())
				return obj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("ServiceInterfaceSet has condition with Success Reason at the end of a successful reconciliation loop and underlying object ready", func() {
			By("Creating the service interface set")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes, 2 matches")
			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2"))
			}, timeout*3, interval).Should(Succeed())

			By("Updating the status of the underlying NSI entries")
			for _, node := range []string{"node1", "node2"} {
				nsi := &dpuservicev1.NodeServiceInterfaces{}
				Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: utils.NSIObjectsNamespace,
					Name:      nsiName(node, dpuservicev1.NSITypeSFC),
				}, nsi)).To(Succeed())
				setEntryReady(nsi, entryName)
				nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
				nsi.SetManagedFields(nil)
				Expect(testClient.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
			}

			By("Checking the conditions")
			Eventually(func(g Gomega) []metav1.Condition {
				obj := &dpuservicev1.ServiceInterfaceSet{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: svcIfcSetName}, obj)).To(Succeed())
				g.Expect(obj.Status.Conditions).ToNot(BeEmpty())
				return obj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("ServiceInterfaceSet has condition with Pending Reason with partial success", func() {
			By("Creating the service interface set")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, testNS.Name, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes, 2 matches")
			entryName := interfaceEntryName(testNS.Name, svcIfcSetName)
			Eventually(func(g Gomega) {
				g.Expect(nodesWithActiveNSIEntry(g, testNS.Name, svcIfcSetName)).To(ConsistOf("node1", "node2"))
			}, timeout*3, interval).Should(Succeed())

			By("Marking only one NSI entry Ready")
			nsi := &dpuservicev1.NodeServiceInterfaces{}
			Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: utils.NSIObjectsNamespace,
				Name:      nsiName("node1", dpuservicev1.NSITypeSFC),
			}, nsi)).To(Succeed())
			setEntryReady(nsi, entryName)
			nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
			nsi.SetManagedFields(nil)
			Expect(testClient.Status().Patch(ctx, nsi, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Checking the conditions")
			Eventually(func(g Gomega) []metav1.Condition {
				obj := &dpuservicev1.ServiceInterfaceSet{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: svcIfcSetName}, obj)).To(Succeed())
				g.Expect(obj.Status.Conditions).ToNot(BeEmpty())
				return obj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
	})
})

// cleanServiceInterfaces removes all the serviceInterfaces that a ServiceInterfaceSet creates in a particular namespace
func cleanServiceInterfaces(namespace string) {
	By("Ensuring all ServiceInterfaces are cleaned up")
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
	Expect(testClient.List(ctx, serviceInterfaceList, client.InNamespace(namespace))).To(Succeed())
	objs := make([]client.Object, 0, len(serviceInterfaceList.Items))
	for i := range serviceInterfaceList.Items {
		objs = append(objs, &serviceInterfaceList.Items[i])
	}
	Eventually(func(g Gomega) {
		g.Expect(testutils.CleanupAndWait(ctx, testClient, objs...)).To(Succeed())
	}).WithTimeout(180 * time.Second).Should(Succeed())
}

func assertServiceInterfaceSetCondition(g Gomega, testClient client.Client, serviceInterfaceSet *dpuservicev1.ServiceInterfaceSet) {
	gotServiceInterfaceSet := &dpuservicev1.ServiceInterfaceSet{}
	g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(serviceInterfaceSet), gotServiceInterfaceSet)).To(Succeed())
	g.Expect(gotServiceInterfaceSet.Status.Conditions).NotTo(BeNil())
	g.Expect(gotServiceInterfaceSet.Status.Conditions).To(ConsistOf(
		And(
			HaveField("Type", string(conditions.TypeReady)),
			HaveField("Status", metav1.ConditionTrue),
			HaveField("Reason", string(conditions.ReasonSuccess)),
		),
		And(
			HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReconciled)),
			HaveField("Status", metav1.ConditionTrue),
			HaveField("Reason", string(conditions.ReasonSuccess)),
		),
		And(
			HaveField("Type", string(dpuservicev1.ConditionServiceInterfacesReady)),
			HaveField("Status", metav1.ConditionTrue),
			HaveField("Reason", string(conditions.ReasonSuccess)),
		),
	))
}

// nodesWithActiveNSIEntry returns sorted node names that have a non-terminating SFC NSI entry for the set.
func nodesWithActiveNSIEntry(g Gomega, setNS, setName string) []string {
	entryName := interfaceEntryName(setNS, setName)
	nsiList := &dpuservicev1.NodeServiceInterfacesList{}
	g.Expect(testClient.List(ctx, nsiList, client.InNamespace(utils.NSIObjectsNamespace))).To(Succeed())
	nodes := []string{}
	for i := range nsiList.Items {
		nsi := nsiList.Items[i]
		if nsi.Spec.Type != dpuservicev1.NSITypeSFC {
			continue
		}
		for _, e := range nsi.Spec.Interfaces {
			if e.Name == entryName && !e.Terminating {
				nodes = append(nodes, nsi.Spec.Node)
			}
		}
	}
	sort.Strings(nodes)
	return nodes
}

func getServiceInterfaceSet(ns string, labelSelector *metav1.LabelSelector) *dpuservicev1.ServiceInterfaceSet {
	sis := serviceInterfaceSpec(ns, labelSelector)
	sis.Spec.Template.Spec = *getTestServiceInterfaceSpec()
	return sis
}

func createServiceInterfaceSet(ctx context.Context, ns string, labelSelector *metav1.LabelSelector) *dpuservicev1.ServiceInterfaceSet {
	sis := getServiceInterfaceSet(ns, labelSelector)
	Expect(testClient.Create(ctx, sis)).NotTo(HaveOccurred())
	return sis
}

func setServiceInterfaceSetModeLegacy(sis *dpuservicev1.ServiceInterfaceSet) {
	if sis.Annotations == nil {
		sis.Annotations = map[string]string{}
	}
	sis.Annotations[interfaceModeAnnotation] = interfaceModeLegacy
}

func createLegacyTypedServiceInterfaceSet(ctx context.Context, ns string, labelSelector *metav1.LabelSelector, typ string, vn *string) *dpuservicev1.ServiceInterfaceSet {
	sis := serviceInterfaceSpec(ns, labelSelector)
	sis.Spec.Template.Spec = getTypedTestServiceInterfaceSpec(typ, vn)
	setServiceInterfaceSetModeLegacy(sis)

	Expect(testClient.Create(ctx, sis)).NotTo(HaveOccurred())
	return sis
}

func createTypedServiceInterfaceSet(ctx context.Context, ns string, labelSelector *metav1.LabelSelector, typ string) *dpuservicev1.ServiceInterfaceSet {
	sis := serviceInterfaceSpec(ns, labelSelector)
	sis.Spec.Template.Spec = getTypedTestServiceInterfaceSpec(typ, nil)

	Expect(testClient.Create(ctx, sis)).NotTo(HaveOccurred())
	return sis
}

func createInvalidTypedServiceInterfaceSet(ctx context.Context, ns string, labelSelector *metav1.LabelSelector, typ string) {
	sis := serviceInterfaceSpec(ns, labelSelector)
	sis.Spec.Template.Spec = getInvalidTestServiceInterfaceSpec(typ)

	Expect(testClient.Create(ctx, sis)).To(HaveOccurred())
}

func serviceInterfaceSpec(ns string, labelSelector *metav1.LabelSelector) *dpuservicev1.ServiceInterfaceSet {
	sis := &dpuservicev1.ServiceInterfaceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcIfcSetName,
			Namespace: ns,
		},
		Spec: dpuservicev1.ServiceInterfaceSetSpec{
			NodeSelector: labelSelector,
			Template: dpuservicev1.ServiceInterfaceSpecTemplate{
				ObjectMeta: dpuservicev1.ObjectMeta{
					Labels: testutils.GetTestLabels(),
				},
			},
		},
	}
	return sis
}

func getTestServiceInterfaceSpec() *dpuservicev1.ServiceInterfaceSpec {
	return &dpuservicev1.ServiceInterfaceSpec{
		InterfaceType: dpuservicev1.InterfaceTypeVF,
		Vlan: &dpuservicev1.VLAN{
			VlanID:             102,
			ParentInterfaceRef: "p0",
		},
		VF: &dpuservicev1.VF{
			VFID: 0,
			PFID: 1,
		},
		PF: &dpuservicev1.PF{
			ID: 3,
		},
	}
}

func getTypedTestServiceInterfaceSpec(typ string, vn *string) dpuservicev1.ServiceInterfaceSpec {
	sfc := dpuservicev1.ServiceInterfaceSpec{}
	switch typ {
	case dpuservicev1.InterfaceTypeVLAN:
		sfc.InterfaceType = dpuservicev1.InterfaceTypeVLAN
		sfc.Vlan = &dpuservicev1.VLAN{
			VlanID:             102,
			ParentInterfaceRef: "p0",
		}
	case dpuservicev1.InterfaceTypePF:
		sfc.InterfaceType = dpuservicev1.InterfaceTypePF
		sfc.PF = &dpuservicev1.PF{
			ID:             3,
			VirtualNetwork: vn,
		}
	case dpuservicev1.InterfaceTypeVF:
		sfc.InterfaceType = dpuservicev1.InterfaceTypeVF
		sfc.VF = &dpuservicev1.VF{
			VFID:           0,
			PFID:           1,
			VirtualNetwork: vn,
		}
	case dpuservicev1.InterfaceTypePhysical:
		sfc.InterfaceType = dpuservicev1.InterfaceTypePhysical
		sfc.Physical = &dpuservicev1.Physical{
			InterfaceName: "enp33s0f0np0",
		}
	case dpuservicev1.InterfaceTypePatch:
		sfc.InterfaceType = dpuservicev1.InterfaceTypePatch
		sfc.Patch = &dpuservicev1.PatchDef{
			PeerBridge: "br-ext",
		}
	case dpuservicev1.InterfaceTypeService:
		sfc.InterfaceType = dpuservicev1.InterfaceTypeService
		sfc.Service = &dpuservicev1.ServiceDef{
			ServiceID:      "awsome-firewall",
			Network:        "mybrsfc",
			InterfaceName:  "net1",
			VirtualNetwork: vn,
		}
	}

	return sfc
}

func getInvalidTestServiceInterfaceSpec(typ string) dpuservicev1.ServiceInterfaceSpec {
	sfc := getTypedTestServiceInterfaceSpec(typ, nil)
	switch typ {
	case dpuservicev1.InterfaceTypeVLAN:
		sfc.Vlan = nil
	case dpuservicev1.InterfaceTypePF:
		sfc.PF = nil
	case dpuservicev1.InterfaceTypeVF:
		sfc.VF = nil
	case dpuservicev1.InterfaceTypePhysical:
		sfc.Physical = nil
	case dpuservicev1.InterfaceTypeService:
		sfc.Service = nil
	case dpuservicev1.InterfaceTypePatch:
		sfc.Patch = nil
	}
	return sfc
}

func testServiceInterface() *dpuservicev1.ServiceInterface {
	return &dpuservicev1.ServiceInterface{
		ObjectMeta: testChildMeta(),
		Spec: dpuservicev1.ServiceInterfaceSpec{
			Node:          ptr.To("node1"),
			InterfaceType: dpuservicev1.InterfaceTypeVF,
			VF:            &dpuservicev1.VF{VFID: 0, PFID: 1, VirtualNetwork: ptr.To("vnet")},
		},
	}
}

func TestServiceInterfaceUpToDate(t *testing.T) {
	tests := []struct {
		name   string
		cached func(*dpuservicev1.ServiceInterface)
		want   bool
	}{
		{name: "identical", want: true},
		{
			name:   "spec field differs",
			cached: func(si *dpuservicev1.ServiceInterface) { si.Spec.VF.VFID = 7 },
			want:   false,
		},
		{
			name: "extra spec field on cached",
			cached: func(si *dpuservicev1.ServiceInterface) {
				si.Spec.Vlan = &dpuservicev1.VLAN{VlanID: 100, ParentInterfaceRef: "p0-node1"}
			},
			want: false,
		},
		{
			name:   "metadata differs",
			cached: func(si *dpuservicev1.ServiceInterface) { si.Annotations["x"] = "2" },
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			cached, desired := testServiceInterface(), testServiceInterface()
			if tt.cached != nil {
				tt.cached(cached)
			}
			g.Expect(serviceInterfaceUpToDate(cached, desired)).To(Equal(tt.want))
		})
	}
}

func testInterfaceEntry() dpuservicev1.InterfaceEntry {
	return dpuservicev1.InterfaceEntry{
		Name:          "ns_set",
		Labels:        map[string]string{"a": "1"},
		InterfaceType: dpuservicev1.InterfaceTypeVF,
		VF:            &dpuservicev1.VF{VFID: 0, PFID: 1},
	}
}

func testOwnedNSIEntry() ownedNSIEntry {
	return ownedNSIEntry{
		NSI: &dpuservicev1.NodeServiceInterfaces{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsiName("node1", dpuservicev1.NSITypeSFC),
				Labels: map[string]string{
					nsiNodeLabel: "node1",
					nsiTypeLabel: dpuservicev1.NSITypeSFC,
				},
			},
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Node:       "node1",
				Type:       dpuservicev1.NSITypeSFC,
				Interfaces: []dpuservicev1.InterfaceEntry{testInterfaceEntry()},
			},
		},
		Entry: testInterfaceEntry(),
	}
}

func TestNSIEntryUpToDate(t *testing.T) {
	tests := []struct {
		name    string
		owned   func(*ownedNSIEntry)
		desired func(*dpuservicev1.InterfaceEntry)
		nsiType string
		want    bool
	}{
		{name: "identical", want: true},
		{
			name:    "nil and empty annotations are equal",
			desired: func(e *dpuservicev1.InterfaceEntry) { e.Annotations = map[string]string{} },
			want:    true,
		},
		{
			name:  "entry terminating",
			owned: func(o *ownedNSIEntry) { o.Entry.Terminating = true },
			want:  false,
		},
		{
			name:  "entry field differs",
			owned: func(o *ownedNSIEntry) { o.Entry.VF.VFID = 7 },
			want:  false,
		},
		{
			name:    "NSI type differs",
			nsiType: dpuservicev1.NSITypeVPC,
			want:    false,
		},
		{
			name:  "type label missing",
			owned: func(o *ownedNSIEntry) { delete(o.NSI.Labels, nsiTypeLabel) },
			want:  false,
		},
		{
			name:  "node label differs",
			owned: func(o *ownedNSIEntry) { o.NSI.Labels[nsiNodeLabel] = "node2" },
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			owned, desired := testOwnedNSIEntry(), testInterfaceEntry()
			if tt.owned != nil {
				tt.owned(&owned)
			}
			if tt.desired != nil {
				tt.desired(&desired)
			}
			nsiType := dpuservicev1.NSITypeSFC
			if tt.nsiType != "" {
				nsiType = tt.nsiType
			}
			g.Expect(nsiEntryUpToDate(owned, desired, "node1", nsiType)).To(Equal(tt.want))
		})
	}
}
