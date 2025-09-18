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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("DPUStoragePolicy controller", func() {
	var (
		cleanupObjects []client.Object
	)
	AfterEach(func() {
		By("Cleaning up the objects")
		Expect(testutils.CleanupAndWait(testCtx, testClient, cleanupObjects...)).To(Succeed())
		cleanupObjects = nil
	})

	Context("Reconcile DPUStoragePolicy", func() {
		It("should create StoragePolicy in the DPU cluster", func() {
			By("Create DPUStoragePolicy")
			dpuStoragePolicy := getDPUStoragePolicy()
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			By("Verify StoragePolicy is created")
			storagePolicy := &storagev1.StoragePolicy{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStoragePolicy.Name, Namespace: testNsNameDPU},
			}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, client.ObjectKeyFromObject(storagePolicy), storagePolicy)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storagePolicy)
				g.Expect(storagePolicy.Spec.StorageVendors).To(Equal(dpuStoragePolicy.Spec.DPUStorageVendors))
				g.Expect(storagePolicy.Spec.StorageParameters).To(Equal(dpuStoragePolicy.Spec.Parameters))
				g.Expect(storagePolicy.Spec.StorageSelectionAlg).To(Equal(storagev1.LocalNVolumes))
			}, timeout, interval).Should(Succeed())
		})

		It("should update StoragePolicy when DPUStoragePolicy changes", func() {
			By("Create DPUStoragePolicy")
			dpuStoragePolicy := getDPUStoragePolicy()
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			By("Verify StoragePolicy is created")
			storagePolicy := &storagev1.StoragePolicy{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStoragePolicy.Name, Namespace: testNsNameDPU},
			}
			storagePolicyKey := client.ObjectKeyFromObject(storagePolicy)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storagePolicy)
			}, timeout, interval).Should(Succeed())

			By("Update StoragePolicy spec")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())

				storagePolicy.Spec.StorageVendors = []string{"vendor1", "vendor2", "vendor3"}
				storagePolicy.Spec.StorageParameters = map[string]string{"key1": "value1", "key2": "value2"}
				storagePolicy.Spec.StorageSelectionAlg = storagev1.Random

				g.Expect(testClientDPU.Update(testCtx, storagePolicy)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify StoragePolicy is updated back to match DPUStoragePolicy")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				g.Expect(storagePolicy.Spec.StorageVendors).To(Equal(dpuStoragePolicy.Spec.DPUStorageVendors))
				g.Expect(storagePolicy.Spec.StorageParameters).To(Equal(dpuStoragePolicy.Spec.Parameters))
				g.Expect(storagePolicy.Spec.StorageSelectionAlg).To(Equal(ConvertDPUSelectionAlgorithmToStorageSelectionAlg(dpuStoragePolicy.Spec.SelectionAlgorithm)))
			}, timeout, interval).Should(Succeed())
		})

		It("should correctly handle deletion of DPUStoragePolicy", func() {
			By("Create DPUStoragePolicy")
			dpuStoragePolicy := getDPUStoragePolicy()
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			By("Verify StoragePolicy is created")
			storagePolicy := &storagev1.StoragePolicy{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStoragePolicy.Name, Namespace: testNsNameDPU},
			}
			storagePolicyKey := client.ObjectKeyFromObject(storagePolicy)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storagePolicy)
			}, timeout, interval).Should(Succeed())

			By("Delete DPUStoragePolicy")
			Expect(testClient.Delete(testCtx, dpuStoragePolicy)).NotTo(HaveOccurred())

			By("Verify StoragePolicy is deleted")
			Eventually(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy))).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should add finalizer to DPUStoragePolicy", func() {
			By("Create DPUStoragePolicy")
			dpuStoragePolicy := getDPUStoragePolicy()
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			By("Verify finalizer is added")
			Eventually(func(g Gomega) {
				var obj storagev1.DPUStoragePolicy
				g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(dpuStoragePolicy), &obj)).NotTo(HaveOccurred())
				g.Expect(controllerutil.ContainsFinalizer(&obj, storagev1.DPUStoragePolicyFinalizer)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should update status to match the StoragePolicy status in DPU cluster", func() {
			By("Create DPUStoragePolicy")
			dpuStoragePolicy := getDPUStoragePolicy()
			dpuStoragePolicyKey := client.ObjectKeyFromObject(dpuStoragePolicy)
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			By("Verify StoragePolicy is created")
			storagePolicy := &storagev1.StoragePolicy{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStoragePolicy.Name, Namespace: testNsNameDPU},
			}
			storagePolicyKey := client.ObjectKeyFromObject(storagePolicy)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storagePolicy)
			}, timeout, interval).Should(Succeed())

			By("Update StoragePolicy status")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				setStoragePolicyValidStatus(storagePolicy)
				g.Expect(testClientDPU.Status().Update(testCtx, storagePolicy)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUStoragePolicy status is updated")
			Eventually(func(g Gomega) {
				var obj storagev1.DPUStoragePolicy
				g.Expect(testClient.Get(testCtx, dpuStoragePolicyKey, &obj)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(&obj, storagev1.ConditionDPUStoragePolicyValid)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should recreate StoragePolicy when it is deleted", func() {
			By("Create DPUStoragePolicy")
			dpuStoragePolicy := getDPUStoragePolicy()
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			By("Verify StoragePolicy is created")
			storagePolicy := &storagev1.StoragePolicy{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStoragePolicy.Name, Namespace: testNsNameDPU},
			}
			storagePolicyKey := client.ObjectKeyFromObject(storagePolicy)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storagePolicy)
			}, timeout, interval).Should(Succeed())
			origUID := storagePolicy.GetUID()

			By("Delete StoragePolicy")
			Expect(testClientDPU.Delete(testCtx, storagePolicy)).NotTo(HaveOccurred())

			By("Verify StoragePolicy is recreated")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				g.Expect(storagePolicy.GetUID()).NotTo(Equal(origUID))
			}, timeout, interval).Should(Succeed())

			By("Verify the recreated StoragePolicy has the correct spec")
			Expect(storagePolicy.Spec.StorageVendors).To(Equal(dpuStoragePolicy.Spec.DPUStorageVendors))
			Expect(storagePolicy.Spec.StorageParameters).To(Equal(dpuStoragePolicy.Spec.Parameters))
			Expect(storagePolicy.Spec.StorageSelectionAlg).To(Equal(ConvertDPUSelectionAlgorithmToStorageSelectionAlg(dpuStoragePolicy.Spec.SelectionAlgorithm)))
		})

		It("should mark DPUStoragePolicy as awaiting deletion when StoragePolicy is being deleted", func() {
			By("Create DPUStoragePolicy")
			dpuStoragePolicy := getDPUStoragePolicy()
			dpuStoragePolicyKey := client.ObjectKeyFromObject(dpuStoragePolicy)
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			By("Verify StoragePolicy is created")
			storagePolicy := &storagev1.StoragePolicy{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStoragePolicy.Name, Namespace: testNsNameDPU},
			}
			storagePolicyKey := client.ObjectKeyFromObject(storagePolicy)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storagePolicy)
			}, timeout, interval).Should(Succeed())

			By("Delete StoragePolicy but add finalizer to prevent actual deletion")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				storagePolicy.Finalizers = []string{"test-finalizer"}
				g.Expect(testClientDPU.Update(testCtx, storagePolicy)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			Expect(testClientDPU.Delete(testCtx, storagePolicy)).NotTo(HaveOccurred())

			By("Verify StoragePolicy has deletion timestamp")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				g.Expect(storagePolicy.DeletionTimestamp).NotTo(BeNil())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUStoragePolicy is marked as awaiting deletion")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, dpuStoragePolicyKey, dpuStoragePolicy)).NotTo(HaveOccurred())
				cond := conditions.Get(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyReconciled)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(conditions.ReasonAwaitingDeletion)))
				g.Expect(cond.Message).To(Equal("StoragePolicy is deleting"))
				// Check Ready condition is also updated
				readyCond := conditions.Get(dpuStoragePolicy, conditions.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(readyCond.Reason).To(Equal(string(conditions.ReasonAwaitingDeletion)))
			}, timeout, interval).Should(Succeed())

			By("Remove finalizer from StoragePolicy")
			var originalUID types.UID
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				originalUID = storagePolicy.UID
				storagePolicy.Finalizers = []string{}
				g.Expect(testClientDPU.Update(testCtx, storagePolicy)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify StoragePolicy is recreated with a different UID")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				g.Expect(storagePolicy.UID).NotTo(Equal(originalUID))
			}, timeout, interval).Should(Succeed())
		})

		It("should update StoragePolicy when DPUStoragePolicy spec changes", func() {
			By("Create DPUStoragePolicy")
			dpuStoragePolicy := getDPUStoragePolicy()
			dpuStoragePolicyKey := client.ObjectKeyFromObject(dpuStoragePolicy)
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			By("Verify StoragePolicy is created with initial spec")
			storagePolicy := &storagev1.StoragePolicy{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStoragePolicy.Name, Namespace: testNsNameDPU},
			}
			storagePolicyKey := client.ObjectKeyFromObject(storagePolicy)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storagePolicy)
				g.Expect(storagePolicy.Spec.StorageVendors).To(Equal(dpuStoragePolicy.Spec.DPUStorageVendors))
				g.Expect(storagePolicy.Spec.StorageParameters).To(Equal(dpuStoragePolicy.Spec.Parameters))
				g.Expect(storagePolicy.Spec.StorageSelectionAlg).To(Equal(ConvertDPUSelectionAlgorithmToStorageSelectionAlg(dpuStoragePolicy.Spec.SelectionAlgorithm)))
			}, timeout, interval).Should(Succeed())

			By("Update DPUStoragePolicy spec")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, dpuStoragePolicyKey, dpuStoragePolicy)).NotTo(HaveOccurred())
				dpuStoragePolicy.Spec.DPUStorageVendors = []string{"updated-vendor1", "updated-vendor2"}
				dpuStoragePolicy.Spec.Parameters = map[string]string{"updated-key1": "updated-value1"}
				dpuStoragePolicy.Spec.SelectionAlgorithm = ptr.To(storagev1.SelectionAlgorithmRandom)
				g.Expect(testClient.Update(testCtx, dpuStoragePolicy)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify StoragePolicy spec is updated to match new DPUStoragePolicy spec")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				g.Expect(storagePolicy.Spec.StorageVendors).To(Equal([]string{"updated-vendor1", "updated-vendor2"}))
				g.Expect(storagePolicy.Spec.StorageParameters).To(Equal(map[string]string{"updated-key1": "updated-value1"}))
				g.Expect(storagePolicy.Spec.StorageSelectionAlg).To(Equal(storagev1.Random))
			}, timeout, interval).Should(Succeed())

			By("Set StoragePolicy status to valid")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				setStoragePolicyValidStatus(storagePolicy)
				g.Expect(testClientDPU.Status().Update(testCtx, storagePolicy)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUStoragePolicy remains reconciled and ready after update")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, dpuStoragePolicyKey, dpuStoragePolicy)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyReconciled)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuStoragePolicy, conditions.TypeReady)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should cleanup orphaned StoragePolicy when DPUStoragePolicy does not exist", func() {
			By("Create orphaned StoragePolicy directly in DPU cluster without matching DPUStoragePolicy")
			orphanedStoragePolicy := &storagev1.StoragePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "orphaned-storage-policy",
					Namespace: testNsNameDPU,
				},
				Spec: storagev1.StoragePolicySpec{
					StorageVendors:      []string{"orphaned-vendor"},
					StorageParameters:   map[string]string{"orphaned": "true"},
					StorageSelectionAlg: storagev1.LocalNVolumes,
				},
			}
			// Add finalizer to prevent immediate deletion
			orphanedStoragePolicy.Finalizers = []string{"test.storage.nvidia.com/cleanup-test"}
			cleanupObjects = append(cleanupObjects, orphanedStoragePolicy)
			createObjectsDPU(orphanedStoragePolicy)

			storagePolicyKey := client.ObjectKeyFromObject(orphanedStoragePolicy)
			By("Wait for orphaned StoragePolicy to have deletion timestamp")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, orphanedStoragePolicy)).NotTo(HaveOccurred())
				g.Expect(orphanedStoragePolicy.DeletionTimestamp).NotTo(BeNil())
			}, timeout, interval).Should(Succeed())

			By("Remove finalizer")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, orphanedStoragePolicy)).NotTo(HaveOccurred())
				orphanedStoragePolicy.Finalizers = []string{}
				g.Expect(testClientDPU.Update(testCtx, orphanedStoragePolicy)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify orphaned StoragePolicy is deleted from DPU cluster")
			Eventually(func(g Gomega) {
				err := testClientDPU.Get(testCtx, storagePolicyKey, orphanedStoragePolicy)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("When checking the status transitions", func() {
		var (
			i *informer.TestInformer
		)
		BeforeEach(func() {
			By("Creating the informer infrastructure for DPUStoragePolicy")
			i = informer.NewInformer(cfg, storagev1.DPUStoragePolicyGroupVersionKind, testNsNameHost, "dpustoragepolicies")
			DeferCleanup(i.Cleanup)
			go i.Run()
			Eventually(i.HasSynced).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())
		})

		It("DPUStoragePolicy has all the conditions with Pending Reason at start of the reconciliation loop", func() {
			dpuStoragePolicy := getDPUStoragePolicy()
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &storagev1.DPUStoragePolicy{}
				newObj := &storagev1.DPUStoragePolicy{}
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
					HaveField("Type", string(storagev1.ConditionDPUStoragePolicyReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUStoragePolicyValid)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})

		It("All conditions should eventually be ready when StoragePolicy status is valid", func() {
			By("Create DPUStoragePolicy")
			dpuStoragePolicy := getDPUStoragePolicy()
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			By("Verify StoragePolicy is created in DPU cluster")
			storagePolicy := &storagev1.StoragePolicy{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStoragePolicy.Name, Namespace: testNsNameDPU},
			}
			storagePolicyKey := client.ObjectKeyFromObject(storagePolicy)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storagePolicy)
			}, timeout, interval).Should(Succeed())

			By("First set StoragePolicy status to valid")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				setStoragePolicyValidStatus(storagePolicy)
				g.Expect(testClientDPU.Status().Update(testCtx, storagePolicy)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUStoragePolicy conditions transition to ready")
			dpuStoragePolicyKey := client.ObjectKeyFromObject(dpuStoragePolicy)
			Eventually(func(g Gomega) []metav1.Condition {
				var newObj storagev1.DPUStoragePolicy
				g.Expect(testClient.Get(testCtx, dpuStoragePolicyKey, &newObj)).NotTo(HaveOccurred())
				return newObj.Status.Conditions
			}, timeout, interval).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUStoragePolicyReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUStoragePolicyValid)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})

		It("Conditions should transition from Ready to Unready when StoragePolicy status becomes invalid", func() {
			By("Create DPUStoragePolicy")
			dpuStoragePolicy := getDPUStoragePolicy()
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			By("Verify StoragePolicy is created in DPU cluster")
			storagePolicy := &storagev1.StoragePolicy{
				ObjectMeta: metav1.ObjectMeta{Name: dpuStoragePolicy.Name, Namespace: testNsNameDPU},
			}
			storagePolicyKey := client.ObjectKeyFromObject(storagePolicy)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, storagePolicy)
			}, timeout, interval).Should(Succeed())

			By("First set StoragePolicy status to valid")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())
				setStoragePolicyValidStatus(storagePolicy)
				g.Expect(testClientDPU.Status().Update(testCtx, storagePolicy)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Wait for DPUStoragePolicy to become Ready")
			dpuStoragePolicyKey := client.ObjectKeyFromObject(dpuStoragePolicy)
			Eventually(func(g Gomega) {
				var obj storagev1.DPUStoragePolicy
				g.Expect(testClient.Get(testCtx, dpuStoragePolicyKey, &obj)).NotTo(HaveOccurred())
				cond := conditions.Get(&obj, conditions.TypeReady)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, timeout, interval).Should(Succeed())

			By("Update StoragePolicy status to indicate vendors are invalid")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(testCtx, storagePolicyKey, storagePolicy)).NotTo(HaveOccurred())

				storagePolicy.Status.State = storagev1.StorageVendorStateInvalid
				storagePolicy.Status.Message = "Storage vendors validation failed"

				g.Expect(testClientDPU.Status().Update(testCtx, storagePolicy)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUStoragePolicy conditions transition to unready")
			Eventually(func(g Gomega) []metav1.Condition {
				var newObj storagev1.DPUStoragePolicy
				g.Expect(testClient.Get(testCtx, dpuStoragePolicyKey, &newObj)).NotTo(HaveOccurred())
				return newObj.Status.Conditions
			}, timeout, interval).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUStoragePolicyReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUStoragePolicyValid)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})

		It("DPUStoragePolicy should have ReasonAwaitingDeletion when deleting", func() {
			dpuStoragePolicy := getDPUStoragePolicy()
			cleanupObjects = append(cleanupObjects, dpuStoragePolicy)
			createObjects(dpuStoragePolicy)

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(dpuStoragePolicy), dpuStoragePolicy)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyReconciled)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			Expect(testClient.Delete(testCtx, dpuStoragePolicy)).NotTo(HaveOccurred())

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUStoragePolicy{}
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
					HaveField("Type", string(storagev1.ConditionDPUStoragePolicyReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUStoragePolicyValid)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
	})
})

// setStoragePolicyValidStatus sets the status of the StoragePolicy to valid
func setStoragePolicyValidStatus(storagePolicy *storagev1.StoragePolicy) {
	storagePolicy.Status.State = storagev1.StorageVendorStateValid
	storagePolicy.Status.Message = "test message"
}

// getDPUStoragePolicy returns a test DPUStoragePolicy
func getDPUStoragePolicy() *storagev1.DPUStoragePolicy {
	return &storagev1.DPUStoragePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storage-policy",
			Namespace: testNsNameHost,
		},
		Spec: storagev1.DPUStoragePolicySpec{
			DPUStorageVendors:  []string{"vendor1", "vendor2"},
			Parameters:         map[string]string{"key": "value"},
			SelectionAlgorithm: ptr.To(storagev1.SelectionAlgorithmNumberVolumes),
		},
	}
}
