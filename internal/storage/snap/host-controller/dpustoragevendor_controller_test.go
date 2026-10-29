/*
COPYRIGHT 2025 NVIDIA

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

package hostcontroller

import (
	"time"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPUStorageVendor Controller", func() {
	var (
		cleanupObjects []client.Object
	)
	AfterEach(func() {
		By("Cleaning up the objects")
		Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		cleanupObjects = nil
	})
	Context("When reconciling a resource", func() {

		It("should successfully reconcile the DPUStorageVendor", func() {
			By("Create DPUStorageVendor")
			dpuStorageVendor := getDPUStorageVendor()
			cleanupObjects = append(cleanupObjects, dpuStorageVendor)
			createObjects(dpuStorageVendor)
			By("Verify StorageVendor is created")

			storageVendor := &storagev1.StorageVendor{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStorageVendor.Name, Namespace: testNsNameDPU},
			}
			storageVendorKey := client.ObjectKeyFromObject(storageVendor)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storageVendor)
			}, timeout, interval).Should(Succeed())

			By("Verify StorageVendor has correct spec")
			Expect(storageVendor.Spec.StorageClassName).To(Equal(dpuStorageVendor.Spec.StorageClassName))
			Expect(storageVendor.Spec.PluginName).To(Equal(dpuStorageVendor.Spec.PluginName))

			By("Verify DPUStorageVendor is reconciled and ready")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStorageVendor), dpuStorageVendor)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuStorageVendor, conditions.TypeReady)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should update StorageVendor when spec is not matching with DPUStorageVendor", func() {
			By("Create DPUStorageVendor")
			dpuStorageVendor := getDPUStorageVendor()
			cleanupObjects = append(cleanupObjects, dpuStorageVendor)
			createObjects(dpuStorageVendor)

			By("Verify StorageVendor is created")
			storageVendor := &storagev1.StorageVendor{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStorageVendor.Name, Namespace: testNsNameDPU},
			}
			storageVendorKey := client.ObjectKeyFromObject(storageVendor)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storageVendor)
			}, timeout, interval).Should(Succeed())

			By("Update StorageVendor spec")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				storageVendor.Spec.StorageClassName = "updated-storage-class"
				storageVendor.Spec.PluginName = "updated-plugin"
				g.Expect(testClientDPU.Update(ctx, storageVendor)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify StorageVendor is updated back to match DPUStorageVendor")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				g.Expect(storageVendor.Spec.StorageClassName).To(Equal(dpuStorageVendor.Spec.StorageClassName))
				g.Expect(storageVendor.Spec.PluginName).To(Equal(dpuStorageVendor.Spec.PluginName))
			}, timeout, interval).Should(Succeed())
		})

		It("should recreate StorageVendor when deleted", func() {
			By("Create DPUStorageVendor")
			dpuStorageVendor := getDPUStorageVendor()
			cleanupObjects = append(cleanupObjects, dpuStorageVendor)
			createObjects(dpuStorageVendor)

			By("Verify StorageVendor is created")
			storageVendor := &storagev1.StorageVendor{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStorageVendor.Name, Namespace: testNsNameDPU},
			}
			storageVendorKey := client.ObjectKeyFromObject(storageVendor)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storageVendor)
			}, timeout, interval).Should(Succeed())
			origVendorUID := storageVendor.GetUID()

			By("Delete StorageVendor")
			Expect(testClientDPU.Delete(ctx, storageVendor)).NotTo(HaveOccurred())

			By("Verify StorageVendor is recreated")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				g.Expect(storageVendor.GetUID()).NotTo(Equal(origVendorUID))
			}, timeout, interval).Should(Succeed())
		})

		It("should correctly handle deletion of DPUStorageVendor", func() {
			By("Create DPUStorageVendor")
			dpuStorageVendor := getDPUStorageVendor()
			cleanupObjects = append(cleanupObjects, dpuStorageVendor)
			createObjects(dpuStorageVendor)

			By("Verify StorageVendor is created")
			storageVendor := &storagev1.StorageVendor{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStorageVendor.Name, Namespace: testNsNameDPU},
			}
			storageVendorKey := client.ObjectKeyFromObject(storageVendor)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storageVendor)
			}, timeout, interval).Should(Succeed())

			By("Delete DPUStorageVendor")
			Expect(testClient.Delete(ctx, dpuStorageVendor)).NotTo(HaveOccurred())

			By("Verify StorageVendor is deleted")
			Eventually(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClientDPU.Get(ctx, storageVendorKey, storageVendor))).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should mark DPUStorageVendor as awaiting deletion when StorageVendor is being deleted", func() {
			By("Create DPUStorageVendor")
			dpuStorageVendor := getDPUStorageVendor()
			dpuStorageVendorKey := client.ObjectKeyFromObject(dpuStorageVendor)
			cleanupObjects = append(cleanupObjects, dpuStorageVendor)
			createObjects(dpuStorageVendor)

			By("Verify StorageVendor is created")
			storageVendor := &storagev1.StorageVendor{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStorageVendor.Name, Namespace: testNsNameDPU},
			}
			storageVendorKey := client.ObjectKeyFromObject(storageVendor)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storageVendor)
			}, timeout, interval).Should(Succeed())

			By("Add finalizer to StorageVendor")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				storageVendor.Finalizers = []string{"test-finalizer"}
				g.Expect(testClientDPU.Update(ctx, storageVendor)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Delete StorageVendor")
			Expect(testClientDPU.Delete(ctx, storageVendor)).NotTo(HaveOccurred())

			By("Verify DPUStorageVendor is marked as awaiting deletion")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, dpuStorageVendorKey, dpuStorageVendor)).NotTo(HaveOccurred())
				cond := conditions.Get(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(conditions.ReasonAwaitingDeletion)))
				g.Expect(cond.Message).To(Equal("StorageVendor is deleting"))
			}, timeout, interval).Should(Succeed())

			By("Remove finalizer from StorageVendor")
			var originalUID types.UID
			Eventually(func(g Gomega) {
				err := testClientDPU.Get(ctx, storageVendorKey, storageVendor)
				g.Expect(err).NotTo(HaveOccurred())
				originalUID = storageVendor.UID
				storageVendor.Finalizers = []string{}
				g.Expect(testClientDPU.Update(ctx, storageVendor)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify StorageVendor is recreated with a different UID")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				g.Expect(storageVendor.UID).NotTo(Equal(originalUID))
			}, timeout, interval).Should(Succeed())
		})

		It("should update StorageVendor when DPUStorageVendor spec changes", func() {
			By("Create DPUStorageVendor")
			dpuStorageVendor := getDPUStorageVendor()
			dpuStorageVendorKey := client.ObjectKeyFromObject(dpuStorageVendor)
			cleanupObjects = append(cleanupObjects, dpuStorageVendor)
			createObjects(dpuStorageVendor)

			By("Verify StorageVendor is created with initial spec")
			storageVendor := &storagev1.StorageVendor{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStorageVendor.Name, Namespace: testNsNameDPU},
			}
			storageVendorKey := client.ObjectKeyFromObject(storageVendor)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storageVendor)
				g.Expect(storageVendor.Spec.StorageClassName).To(Equal(dpuStorageVendor.Spec.StorageClassName))
				g.Expect(storageVendor.Spec.PluginName).To(Equal(dpuStorageVendor.Spec.PluginName))
			}, timeout, interval).Should(Succeed())

			By("Update DPUStorageVendor spec")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, dpuStorageVendorKey, dpuStorageVendor)).NotTo(HaveOccurred())
				dpuStorageVendor.Spec.StorageClassName = "updated-storage-class"
				dpuStorageVendor.Spec.PluginName = "updated-plugin"
				g.Expect(testClient.Update(ctx, dpuStorageVendor)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify StorageVendor spec is updated to match new DPUStorageVendor spec")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, storageVendor)).NotTo(HaveOccurred())
				g.Expect(storageVendor.Spec.StorageClassName).To(Equal("updated-storage-class"))
				g.Expect(storageVendor.Spec.PluginName).To(Equal("updated-plugin"))
			}, timeout, interval).Should(Succeed())

			By("Verify DPUStorageVendor remains reconciled and ready after update")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, dpuStorageVendorKey, dpuStorageVendor)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuStorageVendor, conditions.TypeReady)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should cleanup orphaned StorageVendor when DPUStorageVendor does not exist", func() {
			By("Create orphaned StorageVendor directly in DPU cluster without matching DPUStorageVendor")
			orphanedStorageVendor := &storagev1.StorageVendor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "orphaned-storage-vendor",
					Namespace: testNsNameDPU,
				},
				Spec: storagev1.StorageVendorSpec{
					StorageClassName: "orphaned-storage-class",
					PluginName:       "orphaned-plugin",
				},
			}
			// Add finalizer to prevent immediate deletion
			orphanedStorageVendor.Finalizers = []string{"test.storage.nvidia.com/cleanup-test"}
			cleanupObjects = append(cleanupObjects, orphanedStorageVendor)
			createObjectsDPU(orphanedStorageVendor)

			storageVendorKey := client.ObjectKeyFromObject(orphanedStorageVendor)
			By("Wait for orphaned StorageVendor to have deletion timestamp")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, orphanedStorageVendor)).NotTo(HaveOccurred())
				g.Expect(orphanedStorageVendor.DeletionTimestamp).NotTo(BeNil())
			}, timeout, interval).Should(Succeed())

			By("Remove finalizer")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, storageVendorKey, orphanedStorageVendor)).NotTo(HaveOccurred())
				orphanedStorageVendor.Finalizers = []string{}
				g.Expect(testClientDPU.Update(ctx, orphanedStorageVendor)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify orphaned StorageVendor is deleted from DPU cluster")
			Eventually(func(g Gomega) {
				err := testClientDPU.Get(ctx, storageVendorKey, orphanedStorageVendor)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("When checking the status transitions", func() {
		var (
			i *informer.TestInformer
		)
		BeforeEach(func() {
			By("Creating the informer infrastructure for DPUStorageVendor")
			i = informer.NewInformer(cfg, storagev1.DPUStorageVendorGroupVersionKind, testNsNameHost, "dpustoragevendors")
			DeferCleanup(i.Cleanup)
			go i.Run()
			Eventually(i.HasSynced).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())
		})

		It("DPUStorageVendor has all the conditions with Pending Reason at start of the reconciliation loop", func() {
			dpuStorageVendor := getDPUStorageVendor()
			cleanupObjects = append(cleanupObjects, dpuStorageVendor)
			createObjects(dpuStorageVendor)

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &storagev1.DPUStorageVendor{}
				newObj := &storagev1.DPUStorageVendor{}
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
					HaveField("Type", string(storagev1.ConditionDPUStorageVendorReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})

		It("DPUStorageVendor transitions to Ready state after successful reconciliation", func() {
			dpuStorageVendor := getDPUStorageVendor()
			cleanupObjects = append(cleanupObjects, dpuStorageVendor)
			createObjects(dpuStorageVendor)

			// Wait for StorageVendor creation and reconciliation
			storageVendor := &storagev1.StorageVendor{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStorageVendor.Name, Namespace: testNsNameDPU},
			}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, client.ObjectKeyFromObject(storageVendor), storageVendor)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storageVendor)
			}, timeout, interval).Should(Succeed())

			// Verify conditions transition to Ready
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUStorageVendor{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				if conditions.IsTrue(newObj, conditions.TypeReady) &&
					conditions.IsTrue(newObj, storagev1.ConditionDPUStorageVendorReconciled) {
					return newObj.Status.Conditions
				}
				return nil
			}).WithTimeout(30 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUStorageVendorReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})

		It("DPUStorageVendor should have ReasonAwaitingDeletion when deleting", func() {
			dpuStorageVendor := getDPUStorageVendor()
			cleanupObjects = append(cleanupObjects, dpuStorageVendor)
			createObjects(dpuStorageVendor)

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStorageVendor), dpuStorageVendor)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			Expect(testClient.Delete(ctx, dpuStorageVendor)).NotTo(HaveOccurred())

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUStorageVendor{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				g.Expect(newObj.Status.Conditions).ToNot(BeEmpty())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUStorageVendorReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
			))
		})
	})
})

// getDPUStorageVendor returns a test DPUStorageVendor
func getDPUStorageVendor() *storagev1.DPUStorageVendor {
	return &storagev1.DPUStorageVendor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storage-vendor",
			Namespace: testNsNameHost,
		},
		Spec: storagev1.DPUStorageVendorSpec{
			StorageClassName: "test-storage-class",
			PluginName:       "test-plugin",
		},
	}
}
