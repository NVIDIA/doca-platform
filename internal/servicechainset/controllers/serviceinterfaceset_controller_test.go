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
	"fmt"
	"sort"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	svcIfcSetName = "svc-if-set"
)

//nolint:dupl
var _ = Describe("ServiceInterfaceSet Controller", func() {
	Context("When reconciling a resource", func() {
		var cleanupObjects []client.Object
		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})
		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		})
		It("should successfully reconcile the ServiceInterfaceSet without Node Selector", func() {
			By("Create ServiceInterfaceSet, without Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, &metav1.LabelSelector{}))
			By("Verify ServiceInterface not created, no nodes")
			Consistently(func(g Gomega) {
				serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				err := testClient.List(ctx, serviceInterfaceList)
				Expect(err).NotTo(HaveOccurred())
				Expect(serviceInterfaceList.Items).To(BeEmpty())
			}).WithTimeout(20 * time.Second).Should(Succeed())

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes")
			Eventually(func(g Gomega) {
				assertServiceInterfaceList(ctx, g, 3, &cleanupObjects, getTestServiceInterfaceSpec())
			}, timeout*3, interval).Should(Succeed())
			By("Delete ServiceInterfaceSet Spec")
			Expect(testClient.Delete(ctx, &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: svcIfcSetName, Namespace: defaultNS}})).To(Succeed())
		})
		It("should successfully reconcile the ServiceInterfaceSet with Node Selector", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes, 2 matches")
			Eventually(func(g Gomega) {
				assertServiceInterfaceList(ctx, g, 2, &cleanupObjects, getTestServiceInterfaceSpec())
			}, timeout*30, interval).Should(Succeed())
			By("Delete ServiceInterfaceSet Spec")
			Expect(testClient.Delete(ctx, &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: svcIfcSetName, Namespace: defaultNS}})).To(Succeed())
		})
		It("should successfully reconcile the ServiceInterfaceSet with Node Selector and remove Service Interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", labels))

			By("Reconciling the created resource, 3 nodes, 3 matches")
			Eventually(func(g Gomega) {
				assertServiceInterfaceList(ctx, g, 3, &cleanupObjects, getTestServiceInterfaceSpec())
			}, timeout*30, interval).Should(Succeed())

			By("Pathc Node-3 label to not be selected")
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node3"}}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(node), node)).To(Succeed())
			patch := node.DeepCopy()
			patch.Labels = make(map[string]string)

			Eventually(func() error {
				return testClient.Patch(ctx, patch, client.MergeFrom(node))
			}, timeout, interval).Should(Succeed())

			By("Reconciling the created resource, 3 nodes, 2 matching")
			Eventually(func(g Gomega) {
				assertServiceInterfaceList(ctx, g, 2, &cleanupObjects, getTestServiceInterfaceSpec())
			}, timeout*30, interval).Should(Succeed())
			By("Delete ServiceInterfaceSet Spec")
			Expect(testClient.Delete(ctx, &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: svcIfcSetName, Namespace: defaultNS}})).To(Succeed())
		})
		It("should successfully reconcile the ServiceInterfaceSet after update", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Reconciling the created resource, 3 nodes, 2 matches")
			Eventually(func(g Gomega) {
				assertServiceInterfaceList(ctx, g, 2, &cleanupObjects, getTestServiceInterfaceSpec())
			}, timeout*30, interval).Should(Succeed())

			By("patch ServiceInterfaceSet Spec")
			sis := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: svcIfcSetName, Namespace: defaultNS}}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sis), sis)).To(Succeed())
			patch := sis.DeepCopy()
			updatedSpec := &dpuservicev1.ServiceInterfaceSpec{
				InterfaceType: dpuservicev1.InterfaceTypeVLAN,
				Vlan: &dpuservicev1.VLAN{
					VlanID:             100,
					ParentInterfaceRef: "p7",
				},
				VF: &dpuservicev1.VF{
					VFID:               3,
					PFID:               7,
					ParentInterfaceRef: ptr.To("p10"),
				},
				PF: &dpuservicev1.PF{
					ID: 8,
				},
			}
			patch.Spec.Template.Spec = *updatedSpec
			Eventually(func() error {
				return testClient.Patch(ctx, patch, client.MergeFrom(sis))
			}, timeout, interval).Should(Succeed())
			By("Reconciling the updated resource")
			Eventually(func(g Gomega) {
				assertServiceInterfaceList(ctx, g, 2, &cleanupObjects, updatedSpec)
			}, timeout, interval).Should(Succeed())
			By("Delete ServiceInterfaceSet Spec")
			Expect(testClient.Delete(ctx, &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: svcIfcSetName, Namespace: defaultNS}})).To(Succeed())
		})
		It("should successfully delete the ServiceInterfaceSet", func() {
			By("Creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Creating 2 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))

			By("Reconciling the created resource, 2 nodes, 2 matches")
			Eventually(func(g Gomega) {
				assertServiceInterfaceList(ctx, g, 2, &cleanupObjects, getTestServiceInterfaceSpec())
			}, timeout*30, interval).Should(Succeed())

			By("Deleting ServiceInterfaceSet")
			sis := cleanupObjects[0].(*dpuservicev1.ServiceInterfaceSet)
			Expect(testClient.Delete(ctx, sis)).NotTo(HaveOccurred())

			By("Verifying ServiceInterfaceSet is deleted")
			Eventually(func(g Gomega) {
				sis := cleanupObjects[0].(*dpuservicev1.ServiceInterfaceSet)
				err := testClient.Get(ctx, client.ObjectKeyFromObject(sis), sis)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout*30, interval).Should(Succeed())
		})
		It("should successfully delete the ServiceInterfaceSet and corresponding ServiceInterfaces in its namespace only but not cross-namespace", func() {
			By("Creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}))

			By("Creating 2 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))

			By("Verifying ServiceInterfaceSets have been reconciled")
			Eventually(func(g Gomega) {
				// The second ServiceInterfaceSet should still exist
				serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				g.ExpectWithOffset(1, testClient.List(ctx, serviceInterfaceList)).NotTo(HaveOccurred())
				g.Expect(serviceInterfaceList.Items).To(HaveLen(2))
			}, timeout*30, interval).Should(Succeed())

			By("Create another namespace with a ServiceInterface copy from the default namespace")
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "zzz-testing-foo"}}
			Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, namespace))).To(Succeed())
			siList := &dpuservicev1.ServiceInterfaceList{}
			ExpectWithOffset(1, testClient.List(ctx, siList)).NotTo(HaveOccurred())
			siCopy := siList.Items[0].DeepCopy()
			siCopy.SetResourceVersion("")
			siCopy.SetManagedFields(nil)
			siCopy.SetOwnerReferences(nil)
			siCopy.SetUID("")
			siCopy.Namespace = "zzz-testing-foo"
			Expect(testClient.Create(ctx, siCopy)).To(Succeed())
			cleanupObjects = append(cleanupObjects, siCopy)

			By("Deleting ServiceInterfaceSet")
			sis := cleanupObjects[0].(*dpuservicev1.ServiceInterfaceSet)
			Expect(testClient.Delete(ctx, sis)).NotTo(HaveOccurred())

			By("Verifying ServiceInterface is deleted only in its namespace")
			Eventually(func(g Gomega) {
				sis := cleanupObjects[0].(*dpuservicev1.ServiceInterfaceSet)
				err := testClient.Get(ctx, client.ObjectKeyFromObject(sis), sis)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())

				siList := &dpuservicev1.ServiceInterfaceList{}
				g.ExpectWithOffset(1, testClient.List(ctx, siList)).NotTo(HaveOccurred())
				g.Expect(siList.Items).To(HaveLen(1))
				g.Expect(siList.Items[0].GetNamespace()).NotTo(Equal(defaultNS))
			}, timeout*30, interval).Should(Succeed())
		})
		It("should successfully reconcile the ServiceInterface with maximum name length", func() {
			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node2", labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node3", make(map[string]string)))

			By("Create ServiceInterfaceSet, without Node Selector")
			sis := getServiceInterfaceSet(&metav1.LabelSelector{})
			sis.Name = utilrand.String(63)
			Expect(testClient.Create(ctx, sis)).NotTo(HaveOccurred())
			cleanupObjects = append(cleanupObjects, sis)

			By("Checking that ServiceInterfaces are created")
			Eventually(func(g Gomega) {
				gotServiceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				g.Expect(testClient.List(ctx, gotServiceInterfaceList)).To(Succeed())
				g.Expect(gotServiceInterfaceList.Items).To(HaveLen(3))
			}, timeout*30, interval).Should(Succeed())
		})
		It("should fail to create a ServiceInterfaceSet with name exceeding the maximum length", func() {
			By("Create ServiceInterfaceSet, without Node Selector")
			sis := getServiceInterfaceSet(&metav1.LabelSelector{})
			sis.Name = utilrand.String(64)
			Expect(testClient.Create(ctx, sis)).To(HaveOccurred())
		})
		It("verify ServiceInterface node labeling", func() {
			By("Create ServiceInterfaceSet, without Node Selector")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceSet(ctx, &metav1.LabelSelector{}))

			By("Create 3 nodes")
			labels := map[string]string{"role": "firewall"}
			nodeNames := []string{"node1", "node2", "node3"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, nodeNames[0], labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, nodeNames[1], labels))
			cleanupObjects = append(cleanupObjects, createNode(ctx, nodeNames[2], make(map[string]string)))

			By("Reconciling the created resource, 3 nodes")
			Eventually(func(g Gomega) {
				serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				g.Expect(testClient.List(ctx, serviceInterfaceList)).NotTo(HaveOccurred())
				siNodes := []string{}
				for _, si := range serviceInterfaceList.Items {
					serviceInterface := si
					cleanupObjects = append(cleanupObjects, &serviceInterface)
					siNodes = append(siNodes, si.Labels[ServiceInterfaceNodeNameLabel])
				}
				sort.Strings(siNodes)
				g.Expect(siNodes).To(HaveLen(3))
				for i := range siNodes {
					g.Expect(siNodes[i]).To(Equal(nodeNames[i]))
				}
			}, timeout*30, interval).Should(Succeed())
			By("Delete ServiceInterfaceSet Spec")
			Expect(testClient.Delete(ctx, &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: svcIfcSetName, Namespace: defaultNS}})).To(Succeed())
		})
		It("should set parentInterfaceRef field for ServiceInterface type VF", func() {
			By("Create a node")
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", nil))

			By("Create ServiceInterfaceSet of type VF with parentInterfaceRef field set")
			sis := serviceInterfaceSpec(nil)
			sis.Spec.Template.Spec = getTypedTestServiceInterfaceSpec(dpuservicev1.InterfaceTypeVF, nil)
			sis.Spec.Template.Spec.VF.ParentInterfaceRef = ptr.To("p100")
			Expect(testClient.Create(ctx, sis)).NotTo(HaveOccurred())
			cleanupObjects = append(cleanupObjects, sis)

			By("Reconciling the created resource")
			Eventually(func(g Gomega) {
				serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				g.Expect(testClient.List(ctx, serviceInterfaceList)).NotTo(HaveOccurred())
				g.Expect(serviceInterfaceList.Items).To(HaveLen(1))
				si := serviceInterfaceList.Items[0]
				assertServiceInterface(g, &si, &sis.Spec.Template.Spec)
			}, timeout*3, interval).Should(Succeed())
		})
		It("should set virtualNetwork field for ServiceInterface type PF", func() {
			By("Create a node")
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", nil))

			By("Create ServiceInterfaceSet with virtualNetwork")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, nil, dpuservicev1.InterfaceTypePF, ptr.To("myvnet")))

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

			By("Create ServiceInterfaceSet with virtualNetwork")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, nil, dpuservicev1.InterfaceTypeVF, ptr.To("myvnet")))

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

			By("Create ServiceInterfaceSet with virtualNetwork")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, nil, dpuservicev1.InterfaceTypeService, ptr.To("myvnet")))

			By("Reconciling the created resource")
			Eventually(func(g Gomega) {
				serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				g.Expect(testClient.List(ctx, serviceInterfaceList)).NotTo(HaveOccurred())
				g.Expect(serviceInterfaceList.Items).To(HaveLen(1))
				si := serviceInterfaceList.Items[0]
				g.Expect(si.Spec.Service.VirtualNetwork).To(Equal(ptr.To("myvnet")))
			}, timeout*3, interval).Should(Succeed())
		})
		//nolint:goconst
		It("should preserve labels and annotation from ServiceInterface that are not specified in ServiceInterfaceSet", func() {
			// define test label/annotation keys an values
			fooAnnotKey := "foo-annot"
			fooAnnotValue := "value"
			fooAnnotOverrideValue := "override-value"
			fooLabelKey := "foo-label"
			fooLabelValue := "value"
			fooLabelOverrideValue := "override-value"
			someOtherAnnotKey := "some-other-annot"
			someOtherAnnotValue := "some-other-value"
			someOtherLabelKey := "some-other-label"
			someOtherLabelValue := "some-other-value"

			By("Create a node")
			nodeLabels := map[string]string{"my-label": "node1"}
			cleanupObjects = append(cleanupObjects, createNode(ctx, "node1", nodeLabels))

			By("Create a ServiceIntefaceSet with labels and annotations")
			sis := getServiceInterfaceSet(nil)
			sis.Spec.Template.Annotations = map[string]string{fooAnnotKey: fooAnnotValue}
			sis.Spec.Template.Labels = map[string]string{fooLabelKey: fooLabelValue}
			Expect(testClient.Create(ctx, sis)).ToNot(HaveOccurred())
			cleanupObjects = append(cleanupObjects, sis)

			By("Update created ServiceInterface with labels and annotations")
			si := &dpuservicev1.ServiceInterface{}
			si.Name = fmt.Sprintf("%s-%s", sis.Name, "node1")
			si.Namespace = sis.Namespace
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(si), si)).To(Succeed())
				g.Expect(si.Annotations).To(HaveKeyWithValue(fooAnnotKey, fooAnnotValue))
				g.Expect(si.Labels).To(HaveKeyWithValue(fooLabelKey, fooLabelValue))
			}, timeout*3, interval).Should(Succeed())

			// we set a new label and annotation, and override label and annotation from ServiceInterfaceSet
			// expected result is that the labels and annotaions from ServiceInterfaceSet are restored.
			si.Annotations[fooAnnotKey] = fooAnnotOverrideValue
			si.Annotations[someOtherAnnotKey] = someOtherAnnotValue
			si.Labels[fooLabelKey] = fooLabelOverrideValue
			si.Labels[someOtherLabelKey] = someOtherLabelValue
			Expect(testClient.Update(ctx, si)).To(Succeed())

			By("update ServiceInterfaceSet node selector to trigger reconcile")
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(sis), sis)).To(Succeed())
			sis.Spec.NodeSelector = &metav1.LabelSelector{
				MatchLabels: nodeLabels,
			}
			Expect(testClient.Update(ctx, sis)).To(Succeed())

			By("Verify ServiceInteface has expected labels and annotations")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(si), si)).To(Succeed())
				g.Expect(si.Annotations).To(And(
					HaveKeyWithValue(fooAnnotKey, fooAnnotValue),
					HaveKeyWithValue(someOtherAnnotKey, someOtherAnnotValue),
				))
				g.Expect(si.Labels).To(And(
					HaveKeyWithValue(fooLabelKey, fooLabelValue),
					HaveKeyWithValue(someOtherLabelKey, someOtherLabelValue),
				))
			}, timeout*3, interval).Should(Succeed())
		})
	})

	Context("Validating ServiceInterfaceSet creation", func() {
		var cleanupObjects []client.Object
		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})
		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		})
		It("should successfully create the ServiceInterfaceSet with vlan interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeVLAN, nil))
		})
		It("should successfully create the ServiceInterfaceSet with pf interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypePF, nil))
		})
		It("should successfully create the ServiceInterfaceSet with vf interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeVF, nil))
		})
		It("should successfully create the ServiceInterfaceSet with physical interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypePhysical, nil))
		})
		It("should successfully create the ServiceInterfaceSet with ovn interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeOVN, nil))
		})
		It("should successfully create the ServiceInterfaceSet with service interface", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			cleanupObjects = append(cleanupObjects, createTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeService, nil))
		})

		It("should fail to create the ServiceInterfaceSet with missing vlan interface", func() {
			createInvalidTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeVLAN)
		})
		It("should fail to create the ServiceInterfaceSet with missing pf interface", func() {
			createInvalidTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypePF)
		})
		It("should fail to create the ServiceInterfaceSet with missing vf interface", func() {
			createInvalidTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeVF)
		})
		It("should fail to create the ServiceInterfaceSet with missing physical interface", func() {
			createInvalidTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypePhysical)
		})
		It("should fail to create the ServiceInterfaceSet with missing service definition", func() {
			createInvalidTypedServiceInterfaceSet(ctx, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}, dpuservicev1.InterfaceTypeService)
		})
		It("should successfully create the ServiceInterfaceSet and have all conditions set", func() {
			By("creating ServiceInterfaceSet, with Node Selector")
			obj := createServiceInterfaceSet(ctx, &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}})
			cleanupObjects = append(cleanupObjects, obj)
			Eventually(func(g Gomega) {
				assertServiceInterfaceSetCondition(g, testClient, obj)
			}).WithTimeout(30 * time.Second).Should(BeNil())
		})
	})
})

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

func assertServiceInterfaceList(ctx context.Context, g Gomega, nodeCount int, cleanupObjects *[]client.Object,
	testSpec *dpuservicev1.ServiceInterfaceSpec) {
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
	g.ExpectWithOffset(1, testClient.List(ctx, serviceInterfaceList)).NotTo(HaveOccurred())
	g.ExpectWithOffset(1, serviceInterfaceList.Items).To(HaveLen(nodeCount))

	nodeMap := make(map[string]bool)
	for _, si := range serviceInterfaceList.Items {
		serviceInterface := si
		*cleanupObjects = append(*cleanupObjects, &serviceInterface)
		assertServiceInterface(g, &si, testSpec)
		nodeMap[*si.Spec.Node] = true
	}
	g.ExpectWithOffset(1, nodeMap).To(HaveLen(nodeCount))
}

func assertServiceInterface(g Gomega, si *dpuservicev1.ServiceInterface, testSpec *dpuservicev1.ServiceInterfaceSpec) {
	specCopy := testSpec.DeepCopy()
	node := si.Spec.Node
	if specCopy.Vlan != nil {
		specCopy.Vlan.ParentInterfaceRef = specCopy.Vlan.ParentInterfaceRef + "-" + *node
	}
	if specCopy.VF.ParentInterfaceRef != nil {
		specCopy.VF.ParentInterfaceRef = ptr.To(*specCopy.VF.ParentInterfaceRef + "-" + *node)
	}

	specCopy.Node = node
	g.ExpectWithOffset(2, si.Spec).To(Equal(*specCopy))
	g.ExpectWithOffset(2, *node).NotTo(BeEmpty())
	g.ExpectWithOffset(2, si.Name).To(Equal(svcIfcSetName + "-" + *node))
	g.ExpectWithOffset(2, si.Labels[ServiceInterfaceSetNameLabel]).To(Equal(svcIfcSetName))
	g.ExpectWithOffset(2, si.Labels[ServiceInterfaceSetNamespaceLabel]).To(Equal(defaultNS))
	g.ExpectWithOffset(2, si.OwnerReferences).To(HaveLen(1))
	for k, v := range testutils.GetTestLabels() {
		g.ExpectWithOffset(2, si.Labels[k]).To(Equal(v))
	}
}

func getServiceInterfaceSet(labelSelector *metav1.LabelSelector) *dpuservicev1.ServiceInterfaceSet {
	sis := serviceInterfaceSpec(labelSelector)
	sis.Spec.Template.Spec = *getTestServiceInterfaceSpec()
	return sis
}

func createServiceInterfaceSet(ctx context.Context, labelSelector *metav1.LabelSelector) *dpuservicev1.ServiceInterfaceSet {
	sis := getServiceInterfaceSet(labelSelector)
	Expect(testClient.Create(ctx, sis)).NotTo(HaveOccurred())
	return sis
}

func createTypedServiceInterfaceSet(ctx context.Context, labelSelector *metav1.LabelSelector, typ string, vn *string) *dpuservicev1.ServiceInterfaceSet {
	sis := serviceInterfaceSpec(labelSelector)
	sis.Spec.Template.Spec = getTypedTestServiceInterfaceSpec(typ, vn)

	Expect(testClient.Create(ctx, sis)).NotTo(HaveOccurred())
	return sis
}

func createInvalidTypedServiceInterfaceSet(ctx context.Context, labelSelector *metav1.LabelSelector, typ string) {
	sis := serviceInterfaceSpec(labelSelector)
	sis.Spec.Template.Spec = getInvalidTestServiceInterfaceSpec(typ)

	Expect(testClient.Create(ctx, sis)).To(HaveOccurred())
}

func serviceInterfaceSpec(labelSelector *metav1.LabelSelector) *dpuservicev1.ServiceInterfaceSet {
	sis := &dpuservicev1.ServiceInterfaceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcIfcSetName,
			Namespace: defaultNS,
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
	case dpuservicev1.InterfaceTypeOVN:
		sfc.InterfaceType = dpuservicev1.InterfaceTypeOVN
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
	}
	return sfc
}
