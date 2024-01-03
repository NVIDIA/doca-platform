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

package controllers //nolint:dupl

import (
	"context"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	dpucluster "github.com/nvidia/doca-platform/internal/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	dsiResourceName = "test-dpu-service-ifc"
)

//nolint:dupl
var _ = Describe("ServiceInterfaceSet Controller", func() {
	Context("When reconciling a resource", func() {
		var cleanupObjects []client.Object
		BeforeEach(func() {
			cleanupObjects = []client.Object{}
			By("Faking GetdpuClusters to use the envtest cluster instead of a separate one")
			dpuCluster := testutils.GetTestDPUCluster("default", "envtest")
			kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
			cleanupObjects = append(cleanupObjects, kamajiSecret)

			Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
			cleanupObjects = append(cleanupObjects, &dpuCluster)
		})
		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		})
		It("should successfully reconcile the DPUServiceInterface ", func() {
			By("Create DPUServiceInterface")
			cleanupObjects = append(cleanupObjects, createDPUServiceInterface(ctx, dsiResourceName, testNS, &metav1.LabelSelector{}))
			By("Verify ServiceInterfaceSet is created")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: dsiResourceName, Namespace: testNS}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, scs)
			}, timeout*30, interval).Should(Succeed())
			By("Verify ServiceInterfaceSet")
			scs := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: dsiResourceName, Namespace: testNS}}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			for k, v := range testutils.GetTestLabels() {
				Expect(scs.Labels[k]).To(Equal(v))
			}
			Expect(scs.Spec).To(BeEquivalentTo(*getTestServiceInterfaceSetSpec(&metav1.LabelSelector{})))
			By("Update DPUServiceInterface")
			labelSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}
			Eventually(func(g Gomega) {
				dsc := &dpuservicev1.DPUServiceInterface{ObjectMeta: metav1.ObjectMeta{Name: dsiResourceName, Namespace: testNS}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dsc), dsc)).NotTo(HaveOccurred())
				updatedSpec := getTestServiceInterfaceSetSpec(labelSelector)
				dsc.Spec.Template.Spec = *updatedSpec
				g.Expect(testClient.Update(ctx, dsc)).To(Succeed())
			}).Should(Succeed())
			By("Verify ServiceInterfaceSet is updated")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: dsiResourceName, Namespace: testNS}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
				g.Expect(scs.Spec).To(BeEquivalentTo(*getTestServiceInterfaceSetSpec(labelSelector)))
			}, timeout*30, interval).Should(Succeed())
		})
		It("should successfully delete the DPUServiceInterface and ServiceInterfaceSet", func() {
			By("Create DPUServiceInterface")
			cleanupObjects = append(cleanupObjects, createDPUServiceInterface(ctx, dsiResourceName, testNS, &metav1.LabelSelector{}))
			By("Verify ServiceInterfaceSet is created")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: dsiResourceName, Namespace: testNS}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			}, timeout*30, interval).Should(Succeed())
			By("Delete DPUServiceInterface")
			dsc := &dpuservicev1.DPUServiceInterface{ObjectMeta: metav1.ObjectMeta{Name: dsiResourceName, Namespace: testNS}}
			Expect(testClient.Delete(ctx, dsc)).NotTo(HaveOccurred())
			By("Verify ServiceInterfaceSet is deleted")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceInterfaceSet{ObjectMeta: metav1.ObjectMeta{Name: dsiResourceName, Namespace: testNS}}
				err := testClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout*30, interval).Should(Succeed())
			By("Verify DPUServiceInterface is deleted")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.DPUServiceInterface{ObjectMeta: metav1.ObjectMeta{Name: dsiResourceName, Namespace: testNS}}
				err := testClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout*30, interval).Should(Succeed())
		})
	})
	Context("When checking the status transitions", func() {
		var (
			testNS              *corev1.Namespace
			dpuServiceInterface *dpuservicev1.DPUServiceInterface
			dpuCluster          provisioningv1.DPUCluster
			kamajiSecret        *corev1.Secret
			dpuClusterClient    client.Client
			i                   *informer.TestInformer
		)

		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)

			By("Adding fake kamaji cluster")
			dpuCluster = testutils.GetTestDPUCluster(testNS.Name, "envtest")
			var err error
			kamajiSecret, err = testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, kamajiSecret)

			Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, &dpuCluster)
			dpuClusterClient, err = dpucluster.NewConfig(testClient, &dpuCluster).Client(ctx)
			Expect(err).ToNot(HaveOccurred())

			By("Creating the informer infrastructure for DPUServiceInterface")
			i = informer.NewInformer(cfg, dpuservicev1.DPUServiceInterfaceGroupVersionKind, testNS.Name, "dpuserviceinterfaces")
			DeferCleanup(i.Cleanup)
			go i.Run()

			By("Creating a DPUServiceInterface")
			dpuServiceInterface = createDPUServiceInterface(ctx, "interface", testNS.Name, &metav1.LabelSelector{})
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceInterface)
		})
		It("DPUServiceInterface has all the conditions with Pending Reason at start of the reconciliation loop", func() {
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceInterface{}
				newObj := &dpuservicev1.DPUServiceInterface{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(BeEmpty())
				g.Expect(newObj.Status.Conditions).ToNot(BeEmpty())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUServiceInterface has condition ServiceInterfaceSetReconciled with Success Reason at end of successful reconciliation loop but ServiceInterfaceSetReady with Pending reason on underlying object not ready", func() {
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceInterface{}
				newObj := &dpuservicev1.DPUServiceInterface{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReconciled)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		// TODO: Fix that test when we implement status for ServiceInterfaceSet
		It("DPUServiceInterface has all conditions with Success Reason at end of successful reconciliation loop and underlying object ready", Pending, func() {
			// TODO: Patch InterfaceSet with status

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceInterface{}
				newObj := &dpuservicev1.DPUServiceInterface{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("DPUServiceInterface has condition ServiceInterfaceSetReconciled with Error Reason at the end of a reconciliation loop that failed", func() {
			By("Setting the DPUCluster to an invalid state")
			Expect(testClient.Delete(ctx, kamajiSecret)).To(Succeed())

			DeferCleanup(func() {
				By("Reverting the DPUCluster to ready to ensure DPUServiceInterface deletion can be done")
				kamajiSecret.ResourceVersion = ""
				Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
			})

			By("Checking condition")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceInterface{}
				newObj := &dpuservicev1.DPUServiceInterface{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReconciled)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonError)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))

		})
		It("DPUServiceInterface has condition ServiceInterfaceSetReconciled with AwaitingDeletion Reason when there are still objects in the DPUCluster", func() {
			By("Ensuring that the DPUServiceInterface has been reconciled successfully")
			Eventually(func(g Gomega) []metav1.Condition {
				got := &dpuservicev1.DPUServiceInterface{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceInterface), got)).To(Succeed())
				return got.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ContainElement(
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReconciled)),
					HaveField("Status", metav1.ConditionTrue),
				),
			))

			By("Adding finalizer to the underlying object")
			gotInterfaceSet := &dpuservicev1.ServiceInterfaceSet{}
			Eventually(dpuClusterClient.Get).WithArguments(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "interface"}, gotInterfaceSet).Should(Succeed())
			gotInterfaceSet.SetFinalizers([]string{"test.dpu.nvidia.com/test"})
			gotInterfaceSet.SetGroupVersionKind(dpuservicev1.ServiceInterfaceSetGroupVersionKind)
			gotInterfaceSet.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, gotInterfaceSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Deleting the DPUServiceInterface")
			Expect(testClient.Delete(ctx, dpuServiceInterface)).To(Succeed())

			By("Checking the deleted condition is added")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceInterface{}
				newObj := &dpuservicev1.DPUServiceInterface{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReconciled)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("1")),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceInterfaceSetReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))

			By("Removing finalizer from the underlying object to ensure deletion")
			gotInterfaceSet = &dpuservicev1.ServiceInterfaceSet{}
			Eventually(dpuClusterClient.Get).WithArguments(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "interface"}, gotInterfaceSet).Should(Succeed())
			gotInterfaceSet.SetFinalizers([]string{})
			gotInterfaceSet.SetGroupVersionKind(dpuservicev1.ServiceInterfaceSetGroupVersionKind)
			gotInterfaceSet.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, gotInterfaceSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			// Trigger reconcile to avoid waiting the duration we have specified when objects are not yet deleted in the
			// underlying cluster.
			// TODO: consider if there's ways to speed up this reconcile.
			Eventually(func(g Gomega) {
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, dpuServiceInterface)).To(Succeed())
			}).Should(Succeed())
		})
	})
})

func createDPUServiceInterface(ctx context.Context, name string, namespace string, labelSelector *metav1.LabelSelector) *dpuservicev1.DPUServiceInterface {
	dsc := &dpuservicev1.DPUServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUServiceInterfaceSpec{
			Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
				ObjectMeta: dpuservicev1.ObjectMeta{
					Labels: testutils.GetTestLabels(),
				},
				Spec: *getTestServiceInterfaceSetSpec(labelSelector),
			},
		},
	}
	Expect(testClient.Create(ctx, dsc)).NotTo(HaveOccurred())
	return dsc
}

func getTestServiceInterfaceSpec() *dpuservicev1.ServiceInterfaceSpec {
	return &dpuservicev1.ServiceInterfaceSpec{
		InterfaceType: dpuservicev1.InterfaceTypeVF,
		Vlan: &dpuservicev1.VLAN{
			VlanID:             102,
			ParentInterfaceRef: "p0",
		},
		VF: &dpuservicev1.VF{
			VFID:               0,
			PFID:               1,
			ParentInterfaceRef: "p0",
		},
		PF: &dpuservicev1.PF{
			ID: 3,
		},
	}
}

func getTestServiceInterfaceSetSpec(labelSelector *metav1.LabelSelector) *dpuservicev1.ServiceInterfaceSetSpec {
	return &dpuservicev1.ServiceInterfaceSetSpec{
		NodeSelector: labelSelector,
		Template: dpuservicev1.ServiceInterfaceSpecTemplate{
			Spec: *getTestServiceInterfaceSpec(),
			ObjectMeta: dpuservicev1.ObjectMeta{
				Labels: testutils.GetTestLabels(),
			},
		},
	}
}
