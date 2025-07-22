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
	"maps"
	"time"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:dupl
var _ = Describe("DPUVolume Controller", func() {
	var (
		cleanupObjects []client.Object
	)
	AfterEach(func() {
		By("Cleaning up the objects")
		Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		cleanupObjects = nil
	})
	Context("When reconciling a resource", func() {

		It("should successfully reconcile the DPUVolume", func() {
			By("Create DPUVolume")
			dpuVolume := getDPUVolume()
			cleanupObjects = append(cleanupObjects, dpuVolume)
			createObjects(dpuVolume)
			By("Verify Volume is created")

			vol := &storagev1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1", Namespace: testNsNameDPU}}
			volKey := client.ObjectKeyFromObject(vol)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volKey, vol)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, vol)
			}, timeout, interval).Should(Succeed())

			By("Verify Volume is created")
			parameters := maps.Clone(dpuVolume.Spec.Parameters)
			if parameters == nil {
				parameters = map[string]string{}
			}
			parameters[storageParametersPolicyKey] = dpuVolume.Spec.DPUStoragePolicyName
			Expect(vol.Spec.StorageParameters).To(BeEquivalentTo(parameters))
			Expect(equality.Semantic.DeepEqual(vol.Spec.Request.CapacityRange.Request,
				dpuVolume.Spec.Resources.Requests[corev1.ResourceStorage])).To(BeTrue())
			Expect(vol.Spec.Request.VolumeMode).To(Equal(dpuVolume.Spec.VolumeMode))
			Expect(vol.Spec.Request.AccessModes).To(Equal(dpuVolume.Spec.AccessModes))

			By("Check status is reported back to DPUVolume")
			updateVolumeStatusToAvailable(vol.Name)

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				// Verify that DPUVolume status is updated with volume information
				g.Expect(dpuVolume.Status.State).NotTo(BeNil())
				g.Expect(dpuVolume.Status.State.VolumeInfo).NotTo(BeNil())
				g.Expect(dpuVolume.Status.State.VolumeInfo.VolumeAttributes).To(HaveKeyWithValue("test-attr1", "value1"))
				g.Expect(dpuVolume.Status.State.VolumeInfo.VolumeAttributes).To(HaveKeyWithValue("test-attr2", "value2"))
				g.Expect(dpuVolume.Status.State.SelectedDPUStorageVendorName).NotTo(BeNil())
				g.Expect(*dpuVolume.Status.State.SelectedDPUStorageVendorName).To(Equal("test-vendor"))
				g.Expect(dpuVolume.Status.State.StorageVendorPluginName).NotTo(BeNil())
				g.Expect(*dpuVolume.Status.State.StorageVendorPluginName).To(Equal("test-plugin"))
				g.Expect(dpuVolume.Status.State.CSIDriverName).NotTo(BeNil())
				g.Expect(*dpuVolume.Status.State.CSIDriverName).To(Equal("test-csi-driver"))
				g.Expect(dpuVolume.Status.State.StorageClassName).NotTo(BeNil())
				g.Expect(*dpuVolume.Status.State.StorageClassName).To(Equal("test-storage-class"))
				g.Expect(dpuVolume.Status.State.PersistentVolumeClaimRef).NotTo(BeNil())
				g.Expect(dpuVolume.Status.State.PersistentVolumeClaimRef.Name).To(Equal("test-pvc"))
				g.Expect(dpuVolume.Status.State.PersistentVolumeClaimRef.Namespace).To(Equal(testNsNameDPU))
				g.Expect(dpuVolume.Status.Phase).NotTo(BeNil())
				g.Expect(*dpuVolume.Status.Phase).To(Equal(storagev1.DPUVolumePhaseBound))
				g.Expect(conditions.IsTrue(dpuVolume, storagev1.ConditionDPUVolumeReconciled)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuVolume, storagev1.ConditionDPUVolumeScheduled)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuVolume, storagev1.ConditionDPUVolumeBound)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
		It("should recreate removed Volume", func() {
			By("Create DPUVolume")
			dpuVolume := getDPUVolume()
			cleanupObjects = append(cleanupObjects, dpuVolume)
			createObjects(dpuVolume)

			By("Verify Volume is created")
			vol := &storagev1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1", Namespace: testNsNameDPU}}
			volKey := client.ObjectKeyFromObject(vol)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volKey, vol)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, vol)
			}, timeout, interval).Should(Succeed())
			origVolID := vol.GetUID()

			By("Delete Volume")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volKey, vol)).NotTo(HaveOccurred())
				Expect(testClientDPU.Delete(ctx, vol)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Wait for a new Volume")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volKey, vol)).NotTo(HaveOccurred())
				g.Expect(vol.GetUID()).NotTo(Equal(origVolID))
			}, timeout, interval).Should(Succeed())
		})
		It("should recreate Volume when configuration mismatch", func() {
			By("Create DPUVolume")
			dpuVolume := getDPUVolume()
			cleanupObjects = append(cleanupObjects, dpuVolume)
			createObjects(dpuVolume)

			By("Verify Volume is created and update parameters")
			vol := &storagev1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1", Namespace: testNsNameDPU}}
			volKey := client.ObjectKeyFromObject(vol)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volKey, vol)).NotTo(HaveOccurred())
				// Update parameters to create mismatch
				vol.Spec.StorageParameters["param1"] = "new-value"
				g.Expect(testClientDPU.Update(ctx, vol)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())
			origVolID := vol.GetUID()

			By("Wait for a new Volume")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volKey, vol)).NotTo(HaveOccurred())
				g.Expect(vol.GetUID()).NotTo(Equal(origVolID))
			}, timeout, interval).Should(Succeed())
		})
		It("should not remove attached Volume", func() {
			By("Create DPUVolume")
			dpuVolume := getDPUVolume()
			dpuVolumeKey := client.ObjectKeyFromObject(dpuVolume)
			dpuVolumeAttachment := getDPUVolumeAttachment()
			dpuVolumeAttachmentKey := client.ObjectKeyFromObject(dpuVolumeAttachment)
			cleanupObjects = append(cleanupObjects, dpuVolume, dpuVolumeAttachment)
			createObjects(dpuVolume, dpuVolumeAttachment)

			By("Verify Volume is created")
			vol := &storagev1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "test-vol1", Namespace: testNsNameDPU}}
			volKey := client.ObjectKeyFromObject(vol)
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volKey, vol)).NotTo(HaveOccurred())
				cleanupObjects = append(cleanupObjects, vol)
			}, timeout, interval).Should(Succeed())

			By("Delete DPUVolume")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, dpuVolumeKey, dpuVolume)).NotTo(HaveOccurred())
				Expect(testClient.Delete(ctx, dpuVolume)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUVolume is not deleted")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(ctx, dpuVolumeKey, dpuVolume)).NotTo(HaveOccurred())
			}, time.Second*5, interval).Should(Succeed())

			By("Delete DPUVolumeAttachment")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, dpuVolumeAttachmentKey, dpuVolumeAttachment)).NotTo(HaveOccurred())
				Expect(testClient.Delete(ctx, dpuVolumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUVolume and Volume removed")
			Eventually(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClient.Get(ctx, dpuVolumeKey, dpuVolume))).To(BeTrue())
				g.Expect(apierrors.IsNotFound(testClientDPU.Get(ctx, volKey, vol))).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should cleanup orphaned Volume when DPUVolume does not exist", func() {
			By("Create orphaned Volume directly in DPU cluster without matching DPUVolume")
			orphanedVolume := getVolume()
			// Add finalizer to prevent immediate deletion
			orphanedVolume.Finalizers = []string{"test.storage.nvidia.com/cleanup-test"}
			cleanupObjects = append(cleanupObjects, orphanedVolume)
			createObjectsDPU(orphanedVolume)

			volumeKey := client.ObjectKeyFromObject(orphanedVolume)
			By("Wait for orphaned Volume to have deletion timestamp")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeKey, orphanedVolume)).NotTo(HaveOccurred())
				g.Expect(orphanedVolume.DeletionTimestamp).NotTo(BeNil())
			}, timeout, interval).Should(Succeed())

			By("Remove finalizer")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeKey, orphanedVolume)).NotTo(HaveOccurred())
				orphanedVolume.Finalizers = []string{}
				g.Expect(testClientDPU.Update(ctx, orphanedVolume)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify orphaned Volume is deleted from DPU cluster")
			Eventually(func(g Gomega) {
				err := testClientDPU.Get(ctx, volumeKey, orphanedVolume)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	})
	Context("When checking the status transitions", func() {
		var (
			i *informer.TestInformer
		)
		BeforeEach(func() {
			By("Creating the informer infrastructure for DPUVolume")
			i = informer.NewInformer(cfg, storagev1.DPUVolumeGroupVersionKind, testNsNameHost, "dpuvolumes")
			DeferCleanup(i.Cleanup)
			go i.Run()
			Eventually(i.HasSynced).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())
		})
		It("DPUVolume has all the conditions with Pending Reason at start of the reconciliation loop", func() {
			dpuVolume := getDPUVolume()
			cleanupObjects = append(cleanupObjects, dpuVolume)
			createObjects(dpuVolume)
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &storagev1.DPUVolume{}
				newObj := &storagev1.DPUVolume{}
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
					HaveField("Type", string(storagev1.ConditionDPUVolumeReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeScheduled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeBound)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUVolume has condition DPUVolumeReconciled but not ready when underlying object is not ready", func() {
			dpuVolume := getDPUVolume()
			cleanupObjects = append(cleanupObjects, dpuVolume)
			createObjects(dpuVolume)
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUVolume{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeScheduled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeBound)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUVolume is scheduled but not ready", func() {
			dpuVolume := getDPUVolume()
			cleanupObjects = append(cleanupObjects, dpuVolume)
			createObjects(dpuVolume)
			Eventually(func(g Gomega) {
				vol := &storagev1.Volume{ObjectMeta: metav1.ObjectMeta{Name: dpuVolume.Name, Namespace: testNsNameDPU}}
				g.Expect(testClientDPU.Get(ctx, client.ObjectKeyFromObject(vol), vol)).NotTo(HaveOccurred())
				vol.Spec.VolumeSpecDPU.StorageVendorPluginName = "test-plugin"
				vol.Spec.VolumeSpecDPU.StorageVendorName = "test-vendor"
				g.Expect(testClientDPU.Update(ctx, vol)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUVolume{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeScheduled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeBound)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUVolume is ready when underlying object is ready", func() {
			dpuVolume := getDPUVolume()
			cleanupObjects = append(cleanupObjects, dpuVolume)
			createObjects(dpuVolume)
			Eventually(func(g Gomega) {
				vol := &storagev1.Volume{ObjectMeta: metav1.ObjectMeta{Name: dpuVolume.Name, Namespace: testNsNameDPU}}
				g.Expect(testClientDPU.Get(ctx, client.ObjectKeyFromObject(vol), vol)).NotTo(HaveOccurred())
				vol.Spec.VolumeSpecDPU.StorageVendorPluginName = "test-plugin"
				vol.Spec.VolumeSpecDPU.StorageVendorName = "test-vendor"
				g.Expect(testClientDPU.Update(ctx, vol)).NotTo(HaveOccurred())
				vol.Status.State = storagev1.VolumeStateAvailable
				g.Expect(testClientDPU.Status().Update(ctx, vol)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUVolume{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeScheduled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeBound)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("DPUVolume should be not ready with ReasonAwaitingDeletion when deleting", func() {
			dpuVolume := getDPUVolume()
			cleanupObjects = append(cleanupObjects, dpuVolume)
			createObjects(dpuVolume)
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuVolume, storagev1.ConditionDPUVolumeReconciled)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
			Expect(testClient.Delete(ctx, dpuVolume)).NotTo(HaveOccurred())
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUVolume{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeScheduled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUVolumeBound)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
	})
})
