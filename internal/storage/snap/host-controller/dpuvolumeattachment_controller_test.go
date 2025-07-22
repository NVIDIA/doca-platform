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

package hostcontroller //nolint:dupl

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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:dupl
var _ = Describe("DPUVolumeAttachmentAttachment Controller", func() {
	var (
		cleanupObjects []client.Object
	)
	AfterEach(func() {
		By("Cleaning up the objects")
		Expect(testutils.CleanupWithFinalizerRemovalAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		cleanupObjects = nil
	})
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the DPUVolumeAttachment", func() {
			By("Create DPUVolume and DPUVolumeAttachment")
			dpuVolume := getDPUVolume()
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolumeAttachment, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolume, dpuVolumeAttachment, dpuNode, dpu)

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)

			By("Verify VolumeAttachment is created")
			volAttach := &storagev1.VolumeAttachment{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1-attach1", Namespace: testNsNameDPU}}
			volAttachKey := client.ObjectKeyFromObject(volAttach)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())
			cleanupObjects = append(cleanupObjects, volAttach)

			Expect(volAttach.Spec.NodeName).To(BeEquivalentTo(dpu.Name))
			Expect(volAttach.Spec.Source.VolumeRef).NotTo(BeNil())
			Expect(volAttach.Spec.Source.VolumeRef.Name).To(Equal(dpuVolume.Name))
			Expect(volAttach.Spec.Source.VolumeRef.Namespace).To(Equal(testNsNameDPU))

			By("Check status is reported back to DPUVolumeAttachment")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
				volAttach.Spec.Parameters = map[string]string{"param1": "value1", "param2": "value2"}
				g.Expect(testClientDPU.Update(ctx, volAttach)).NotTo(HaveOccurred())
				volAttach.Status.StorageAttached = true
				volAttach.Status.DPU.Attached = true
				volAttach.Status.DPU.PCIDeviceAddress = "0000:00:00.0"
				volAttach.Status.DPU.BdevAttrs.NVMeNsID = 1
				volAttach.Status.DPU.BdevAttrs.NVMeUUID = "550e8400-e29b-41d4-a716-446655440000"

				g.Expect(testClientDPU.Status().Update(ctx, volAttach)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				g.Expect(dpuVolumeAttachment.Status.AttachmentMetadata).To(Equal(map[string]string{"param1": "value1", "param2": "value2"}))
				g.Expect(dpuVolumeAttachment.Status.ControllerAttached).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.ControllerAttached).To(BeTrue())
				g.Expect(dpuVolumeAttachment.Status.DPUAttached).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.DPUAttached).To(BeTrue())
				g.Expect(dpuVolumeAttachment.Status.DPU).NotTo(BeNil())
				g.Expect(dpuVolumeAttachment.Status.DPU.PCIAddress).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.DPU.PCIAddress).To(Equal("0000:00:00.0"))
				g.Expect(dpuVolumeAttachment.Status.DPU.NVMEAttrs).NotTo(BeNil())
				g.Expect(dpuVolumeAttachment.Status.DPU.NVMEAttrs.NamespaceID).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.DPU.NVMEAttrs.NamespaceID).To(Equal(int64(1)))
				g.Expect(dpuVolumeAttachment.Status.DPU.NVMEAttrs.NamespaceUUID).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.DPU.NVMEAttrs.NamespaceUUID).To(Equal("550e8400-e29b-41d4-a716-446655440000"))
			}, timeout, interval).Should(Succeed())
		})
		It("should recreate removed VolumeAttachment", func() {
			By("Create DPUVolume and DPUVolumeAttachment")
			dpuVolume := getDPUVolume()
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolumeAttachment, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolume, dpuVolumeAttachment, dpuNode, dpu)

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)

			By("Verify VolumeAttachment is created")
			volAttach := &storagev1.VolumeAttachment{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1-attach1", Namespace: testNsNameDPU}}
			volAttachKey := client.ObjectKeyFromObject(volAttach)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())
			cleanupObjects = append(cleanupObjects, volAttach)
			origVolAttachID := volAttach.GetUID()

			By("Delete VolumeAttachment")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
				Expect(testClientDPU.Delete(ctx, volAttach)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Wait for a new VolumeAttachment")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
				g.Expect(volAttach.GetUID()).NotTo(Equal(origVolAttachID))
			}, timeout, interval).Should(Succeed())
		})
		It("should recreate VolumeAttachment when configuration mismatch", func() {
			By("Create DPUVolume and DPUVolumeAttachment")
			dpuVolume := getDPUVolume()
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolumeAttachment, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolume, dpuVolumeAttachment, dpuNode, dpu)

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)

			By("Verify VolumeAttachment is created and update NodeName")
			volAttach := &storagev1.VolumeAttachment{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1-attach1", Namespace: testNsNameDPU}}
			volAttachKey := client.ObjectKeyFromObject(volAttach)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
				// Update NodeName to create mismatch
				volAttach.Spec.NodeName = "not-expected"
				g.Expect(testClientDPU.Update(ctx, volAttach)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())
			cleanupObjects = append(cleanupObjects, volAttach)
			origVolAttachID := volAttach.GetUID()

			By("Wait for a new VolumeAttachment")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
				g.Expect(volAttach.GetUID()).NotTo(Equal(origVolAttachID))
			}, timeout, interval).Should(Succeed())
		})
		It("should reconcile DPUVolumes", func() {
			By("Create DPUVolume and DPUVolumeAttachment")
			dpuVolume := getDPUVolume()
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolumeAttachment, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolumeAttachment, dpuNode, dpu)

			By("Verify VolumeAttachment is not created")
			volAttach := &storagev1.VolumeAttachment{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1-attach1", Namespace: testNsNameDPU}}
			volAttachKey := client.ObjectKeyFromObject(volAttach)
			Consistently(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClientDPU.Get(ctx, volAttachKey, volAttach))).To(BeTrue())
			}, time.Second*5, interval).Should(Succeed())

			By("Create DPUVolume")
			createObjects(dpuVolume)

			By("Verify DPUVolumeAttachment is pending because DPUVolume is not ready")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				reconciledCondition := conditions.Get(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)
				g.Expect(reconciledCondition).NotTo(BeNil())
				g.Expect(reconciledCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(reconciledCondition.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(reconciledCondition.Message).To(And(ContainSubstring("DPUVolume"), ContainSubstring("is not ready yet")))
			}, timeout, interval).Should(Succeed())

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)

			By("Verify VolumeAttachment is created")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())
			cleanupObjects = append(cleanupObjects, volAttach)
		})
		It("should reconcile DPUNode", func() {
			By("Create DPUVolume and DPUVolumeAttachment")
			dpuVolume := getDPUVolume()
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolumeAttachment, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolume, dpuVolumeAttachment, dpu)

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)

			By("Verify VolumeAttachment is not created")
			volAttach := &storagev1.VolumeAttachment{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1-attach1", Namespace: testNsNameDPU}}
			volAttachKey := client.ObjectKeyFromObject(volAttach)
			Consistently(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClientDPU.Get(ctx, volAttachKey, volAttach))).To(BeTrue())
			}, time.Second*5, interval).Should(Succeed())

			By("Create DPUNode")
			createObjects(dpuNode)

			By("Verify VolumeAttachment is created")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())
			cleanupObjects = append(cleanupObjects, volAttach)
		})
		It("should reconcile DPU", func() {
			By("Create DPUVolume and DPUVolumeAttachment")
			dpuVolume := getDPUVolume()
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolumeAttachment, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolume, dpuVolumeAttachment, dpuNode)

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)

			By("Verify VolumeAttachment is not created")
			volAttach := &storagev1.VolumeAttachment{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1-attach1", Namespace: testNsNameDPU}}
			volAttachKey := client.ObjectKeyFromObject(volAttach)
			Consistently(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClientDPU.Get(ctx, volAttachKey, volAttach))).To(BeTrue())
			}, time.Second*5, interval).Should(Succeed())

			By("Create DPU")
			createObjects(dpu)

			By("Verify VolumeAttachment is created")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())
			cleanupObjects = append(cleanupObjects, volAttach)
		})

		It("should cleanup orphaned VolumeAttachment when DPUVolumeAttachment does not exist", func() {
			By("Create orphaned VolumeAttachment directly in DPU cluster without matching DPUVolumeAttachment")
			orphanedVolumeAttachment := getVolumeAttachment()
			// Add finalizer to prevent immediate deletion
			orphanedVolumeAttachment.Finalizers = []string{"test.storage.nvidia.com/cleanup-test"}
			cleanupObjects = append(cleanupObjects, orphanedVolumeAttachment)
			createObjectsDPU(orphanedVolumeAttachment)

			volumeAttachmentKey := client.ObjectKeyFromObject(orphanedVolumeAttachment)
			By("Wait for orphaned VolumeAttachment to have deletion timestamp")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeAttachmentKey, orphanedVolumeAttachment)).NotTo(HaveOccurred())
				g.Expect(orphanedVolumeAttachment.DeletionTimestamp).NotTo(BeNil())
			}, timeout, interval).Should(Succeed())

			By("Remove finalizer")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeAttachmentKey, orphanedVolumeAttachment)).NotTo(HaveOccurred())
				orphanedVolumeAttachment.Finalizers = []string{}
				g.Expect(testClientDPU.Update(ctx, orphanedVolumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify orphaned VolumeAttachment is deleted from DPU cluster")
			Eventually(func(g Gomega) {
				err := testClientDPU.Get(ctx, volumeAttachmentKey, orphanedVolumeAttachment)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
		It("Conflicting DPUVolumeAttachments", func() {
			By("Create DPUVolume and DPUNode")
			dpuVolume := getDPUVolume()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolume, dpuNode, dpu)

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)

			By("Create the first DPUVolumeAttachment")
			firstAttachment := getDPUVolumeAttachment()
			firstAttachment.Name = "first-attachment"
			cleanupObjects = append(cleanupObjects, firstAttachment)
			createObjects(firstAttachment)

			By("Wait for first attachment to be reconciled")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(firstAttachment), firstAttachment)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(firstAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Create a second DPUVolumeAttachment with same volume and node (conflicting)")
			secondAttachment := getDPUVolumeAttachment()
			secondAttachment.Name = "second-attachment"
			cleanupObjects = append(cleanupObjects, secondAttachment)
			createObjects(secondAttachment)

			By("Check that the first attachment remains reconciled")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(firstAttachment), firstAttachment)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(firstAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)).To(BeTrue())
			}, time.Second*5, interval).Should(Succeed())

			By("Check that the second attachment is pending due to conflicts")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(secondAttachment), secondAttachment)).NotTo(HaveOccurred())
				reconciledCondition := conditions.Get(secondAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)
				g.Expect(reconciledCondition).NotTo(BeNil())
				g.Expect(reconciledCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(reconciledCondition.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(reconciledCondition.Message).To(ContainSubstring("has multiple attachments"))
			}, timeout, interval).Should(Succeed())

			By("Delete the first attachment")
			Expect(testClient.Delete(ctx, firstAttachment)).NotTo(HaveOccurred())

			By("Check that the second attachment is reconciled")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(secondAttachment), secondAttachment)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(secondAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	})
	Context("When checking the status transitions", func() {
		var (
			i *informer.TestInformer
		)
		BeforeEach(func() {
			By("Creating the informer infrastructure for DPUVolumeAttachment")
			i = informer.NewInformer(cfg, storagev1.DPUVolumeAttachmentGroupVersionKind, testNsNameHost, "dpuvolumeattachments")
			DeferCleanup(i.Cleanup)
			go i.Run()
			Eventually(i.HasSynced).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())
		})

		It("DPUVolumeAttachment has all the conditions with Pending Reason at start of the reconciliation loop", func() {
			dpuVolume := getDPUVolume()
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolumeAttachment, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolume, dpuVolumeAttachment, dpuNode, dpu)

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &storagev1.DPUVolumeAttachment{}
				newObj := &storagev1.DPUVolumeAttachment{}
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
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentControllerAttached)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentDPUAttached)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUVolumeAttachment has condition DPUVolumeAttachmentReconciled but not ready when underlying object is not ready", func() {
			dpuVolume := getDPUVolume()
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolumeAttachment, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolume, dpuVolumeAttachment, dpuNode, dpu)

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUVolumeAttachment{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentControllerAttached)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentDPUAttached)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUVolumeAttachment is ready when storage is attached", func() {
			dpuVolume := getDPUVolume()
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolumeAttachment, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolume, dpuVolumeAttachment, dpuNode, dpu)

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)
			Eventually(func(g Gomega) {
				volAttach := &storagev1.VolumeAttachment{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1-attach1", Namespace: testNsNameDPU}}
				volAttachKey := client.ObjectKeyFromObject(volAttach)
				g.Expect(testClientDPU.Get(ctx, volAttachKey, volAttach)).NotTo(HaveOccurred())
				volAttach.Status.StorageAttached = true
				volAttach.Status.DPU.Attached = true
				g.Expect(testClientDPU.Status().Update(ctx, volAttach)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUVolumeAttachment{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentControllerAttached)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentDPUAttached)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("DPUVolumeAttachment should have ReasonAwaitingDeletion when deleting", func() {
			dpuVolume := getDPUVolume()
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuNode := getDPUNode()
			dpu := getDPU()
			cleanupObjects = append(cleanupObjects, dpuVolumeAttachment, dpuVolume, dpuNode, dpu)
			createObjects(dpuVolume, dpuVolumeAttachment, dpuNode, dpu)

			By("Update Volume status to Available in DPU cluster")
			updateVolumeStatusToAvailable(dpuVolume.Name)

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
			Expect(testClient.Delete(ctx, dpuVolumeAttachment)).NotTo(HaveOccurred())

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUVolumeAttachment{}
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
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentControllerAttached)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentDPUAttached)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		DescribeTable("DPUVolumeAttachment reconciled conditions",
			func(msgSubstring string, objs ...client.Object) {
				cleanupObjects = append(cleanupObjects, objs...)
				createObjects(objs...)

				// Update Volume status to Available if DPUVolume is created
				for _, obj := range objs {
					if dpuVolume, ok := obj.(*storagev1.DPUVolume); ok {
						By("Update Volume status to Available in DPU cluster")
						updateVolumeStatusToAvailable(dpuVolume.Name)
					}
				}
				Eventually(func(g Gomega) []metav1.Condition {
					ev := &informer.Event{}
					g.Eventually(i.UpdateEvents).Should(Receive(ev))
					newObj := &storagev1.DPUVolumeAttachment{}
					g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
					g.Expect(newObj.Status.Conditions).ToNot(BeEmpty())
					return newObj.Status.Conditions
				}).WithTimeout(10 * time.Second).Should(ConsistOf(
					And(
						HaveField("Type", string(conditions.TypeReady)),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
					And(
						HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentReconciled)),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", string(conditions.ReasonPending)),
						HaveField("Message", ContainSubstring(msgSubstring)),
					),
					And(
						HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentControllerAttached)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
					And(
						HaveField("Type", string(storagev1.ConditionDPUVolumeAttachmentDPUAttached)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
				))
			},
			Entry("has condition DPUVolumeAttachmentReconciled false when there is no DPUVolume", "DPUVolume ", getDPUVolumeAttachment(), getDPUNode(), getDPU()),
			Entry("has condition DPUVolumeAttachmentReconciled false when there is no DPU", "DPU ", getDPUVolumeAttachment(), getDPUVolume(), getDPUNode()),
			Entry("has condition DPUVolumeAttachmentReconciled false when there is no DPUNode", "DPUNode ", getDPUVolumeAttachment(), getDPUVolume(), getDPU()),
		)
	})
})
