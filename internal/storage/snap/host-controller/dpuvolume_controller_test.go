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
	"context"
	"time"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/indexers"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("DPUVolume Controller", Ordered, func() {
	var (
		ctx           context.Context
		cancel        context.CancelFunc
		managerStopCh chan struct{}
	)
	BeforeAll(func() {
		By("starting manager with DPUVolume controller and DPUCluster watch-registrar")
		ctx, cancel = context.WithCancel(testCtx)
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Controller: config.Controller{
				SkipNameValidation: ptr.To(true),
			},
			Cache: cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					&storagev1.DPUVolume{}:           {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
					&storagev1.DPUVolumeAttachment{}: {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
				},
			},
			Metrics: server.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(indexers.SetupIndexers(ctx, mgr)).To(Succeed())

		volumeReconciler := &DPUVolumeReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			Options: Options{
				Namespace:       testNsNameHost,
				TargetNamespace: testNsNameDPU,
			},
		}
		var errRC error
		rc, errRC := dpucluster.SetupRemoteCacheWithManager(ctx, mgr,
			dpucluster.OptionTimeout{Timeout: time.Second * 30},
			dpucluster.OptionHostClient{Client: mgr.GetClient()},
			dpucluster.OptionScheme{Scheme: mgr.GetScheme()},
			dpucluster.OptionUserAgent{UserAgent: "snap-host-controller"},
			dpucluster.OptionGetWatcherCallbacks{
				GetWatcherCallbacks: []dpucluster.GetWatcherCallback{
					volumeReconciler.WatchDPUClusterPV,
					volumeReconciler.WatchDPUClusterPVC,
					volumeReconciler.WatchDPUClusterVolume,
				},
			})
		Expect(errRC).NotTo(HaveOccurred())

		volumeReconciler.RemoteCache = rc
		Expect(volumeReconciler.SetupWithManager(mgr)).To(Succeed())

		managerStopCh = make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(managerStopCh)
			Expect(mgr.Start(ctx)).To(Succeed())
		}()
	})
	AfterAll(func() {
		cancel()
		Eventually(managerStopCh).WithTimeout(10 * time.Second).Should(BeClosed())
	})
	AfterEach(func() {
		cleanupTestObjects(ctx, testClient)
	})
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the DPUVolume when policy and vendor exist", func() {
			By("Create DPUStorageVendor and DPUStoragePolicy")
			dpuStorageVendor := getDPUStorageVendor()
			dpuStoragePolicy := getDPUStoragePolicy()
			createObjects(dpuStorageVendor, dpuStoragePolicy)

			By("Create StorageClass and CSIDriver in DPU cluster")
			storageClass := getStorageClass()
			csiDriver := getCSIDriver()
			createObjectsDPU(storageClass, csiDriver)

			By("Set DPUStorageVendor and DPUStoragePolicy as ready")
			setDPUStorageVendorReady(dpuStorageVendor, testClient)
			setDPUStoragePolicyReady(dpuStoragePolicy, testClient)

			By("Create DPUVolume")
			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			By("Verify DPUVolume is reconciled and scheduled")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuVolume, storagev1.ConditionDPUVolumeReconciled)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuVolume, storagev1.ConditionDPUVolumeScheduled)).To(BeTrue())
				g.Expect(dpuVolume.Status.State).NotTo(BeNil())
				g.Expect(dpuVolume.Status.State.DPUCluster).NotTo(BeNil())
				g.Expect(dpuVolume.Status.State.SelectedDPUStorageVendorName).NotTo(BeNil())
			}, timeout, interval).Should(Succeed())

			By("Verify PVC is created in DPU cluster")
			pvc := &corev1.PersistentVolumeClaim{}
			pvcKey := client.ObjectKey{Name: dpuVolume.Name + "-pvc", Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, pvcKey, pvc)).NotTo(HaveOccurred())
				g.Expect(pvc.Spec.StorageClassName).NotTo(BeNil())
				g.Expect(*pvc.Spec.StorageClassName).To(Equal("test-storage-class"))
				g.Expect(pvc.Spec.AccessModes).To(Equal(dpuVolume.Spec.AccessModes))
				g.Expect(pvc.Spec.Resources).To(Equal(dpuVolume.Spec.Resources))
			}, timeout, interval).Should(Succeed())

			By("Create and bind PV to the PVC")
			pv := createAndBindPV(pvc)

			By("Verify Volume CR is created in DPU cluster")
			volume := &storagev1.Volume{}
			volumeKey := client.ObjectKey{Name: dpuVolume.Name, Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeKey, volume)).NotTo(HaveOccurred())
				g.Expect(volume.Spec.Request.CapacityRange.Request).To(Equal(dpuVolume.Spec.Resources.Requests[corev1.ResourceStorage]))
				g.Expect(volume.Spec.Request.AccessModes).To(Equal(dpuVolume.Spec.AccessModes))
				g.Expect(volume.Spec.Request.VolumeMode).To(Equal(dpuVolume.Spec.VolumeMode))
				g.Expect(volume.Spec.StorageParameters).To(Equal(dpuVolume.Spec.Parameters))
				g.Expect(volume.Status.State).To(Equal(storagev1.VolumeStateAvailable))
			}, timeout, interval).Should(Succeed())

			By("Verify DPUVolume is bound and ready")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuVolume, storagev1.ConditionDPUVolumeBound)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuVolume, conditions.TypeReady)).To(BeTrue())
				g.Expect(dpuVolume.Status.Phase).NotTo(BeNil())
				g.Expect(*dpuVolume.Status.Phase).To(Equal(storagev1.DPUVolumePhaseBound))
				g.Expect(dpuVolume.Status.State.VolumeInfo).NotTo(BeNil())
				g.Expect(dpuVolume.Status.State.VolumeInfo.VolumeName).To(HaveValue(Equal(pv.Name)))
			}, timeout, interval).Should(Succeed())
		})

		It("should mark DPUVolume as pending when DPUStoragePolicy is missing", func() {
			By("Create DPUVolume without DPUStoragePolicy")
			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			By("Verify DPUVolume is reconciled but not scheduled")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				reconciledCond := conditions.Get(dpuVolume, storagev1.ConditionDPUVolumeReconciled)
				g.Expect(reconciledCond).NotTo(BeNil())
				g.Expect(reconciledCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(reconciledCond.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(reconciledCond.Message).To(And(ContainSubstring("DPUStoragePolicy"), ContainSubstring("not found")))
			}, timeout, interval).Should(Succeed())
		})

		It("should mark DPUVolume as pending when DPUStoragePolicy is not ready", func() {
			By("Create DPUStoragePolicy without making it ready")
			dpuStoragePolicy := getDPUStoragePolicy()
			createObjects(dpuStoragePolicy)

			By("Create DPUVolume")
			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			By("Verify DPUVolume is reconciled but pending due to policy not ready")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())

				reconciledCond := conditions.Get(dpuVolume, storagev1.ConditionDPUVolumeReconciled)
				g.Expect(reconciledCond).NotTo(BeNil())
				g.Expect(reconciledCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(reconciledCond.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(reconciledCond.Message).To(And(ContainSubstring("DPUStoragePolicy"), ContainSubstring("is not ready")))
			}, timeout, interval).Should(Succeed())
		})

		It("should fail when DPUStorageVendor is missing", func() {
			By("Create DPUStoragePolicy that references non-existent vendor")
			dpuStoragePolicy := getDPUStoragePolicy()
			// Change to reference non-existent vendor
			dpuStoragePolicy.Spec.DPUStorageVendors = []string{"non-existent-vendor"}
			createObjects(dpuStoragePolicy)

			By("Set DPUStoragePolicy as ready")
			setDPUStoragePolicyReady(dpuStoragePolicy, testClient)

			By("Create DPUVolume")
			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			By("Verify DPUVolume fails reconciliation due to missing vendor")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				reconciledCond := conditions.Get(dpuVolume, storagev1.ConditionDPUVolumeReconciled)
				g.Expect(reconciledCond).NotTo(BeNil())
				g.Expect(reconciledCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(reconciledCond.Reason).To(Equal(string(conditions.ReasonError)))
				g.Expect(reconciledCond.Message).To(And(ContainSubstring("DPUStorageVendor"), ContainSubstring("not found")))
			}, timeout, interval).Should(Succeed())
		})

		It("should handle DPUVolume deletion correctly", func() {
			By("Create DPUStorageVendor and DPUStoragePolicy")
			dpuStorageVendor := getDPUStorageVendor()
			dpuStoragePolicy := getDPUStoragePolicy()
			createObjects(dpuStorageVendor, dpuStoragePolicy)

			By("Create StorageClass and CSIDriver in DPU cluster")
			storageClass := getStorageClass()
			csiDriver := getCSIDriver()
			createObjectsDPU(storageClass, csiDriver)

			By("Set DPUStorageVendor and DPUStoragePolicy as ready")
			setDPUStorageVendorReady(dpuStorageVendor, testClient)
			setDPUStoragePolicyReady(dpuStoragePolicy, testClient)

			By("Create and schedule DPUVolume")
			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuVolume, storagev1.ConditionDPUVolumeReconciled)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Verify PVC is created")
			pvc := &corev1.PersistentVolumeClaim{}
			pvcKey := client.ObjectKey{Name: dpuVolume.Name + "-pvc", Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, pvcKey, pvc)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Create and bind PV to the PVC to simulate full lifecycle")
			pv := createAndBindPV(pvc)

			By("Verify Volume CR is created in DPU cluster")
			volume := &storagev1.Volume{}
			volumeKey := client.ObjectKey{Name: dpuVolume.Name, Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeKey, volume)).NotTo(HaveOccurred())
				g.Expect(volume.Status.State).To(Equal(storagev1.VolumeStateAvailable))
			}, timeout, interval).Should(Succeed())

			By("Verify DPUVolume has PV name in status")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				g.Expect(dpuVolume.Status.State).NotTo(BeNil())
				g.Expect(dpuVolume.Status.State.VolumeInfo).NotTo(BeNil())
				g.Expect(dpuVolume.Status.State.VolumeInfo.VolumeName).To(HaveValue(Equal(pv.Name)))
			}, timeout, interval).Should(Succeed())

			By("Delete DPUVolume")
			Expect(testClient.Delete(ctx, dpuVolume)).NotTo(HaveOccurred())

			By("Wait for PVC deletion timestamp to be set in DPU cluster")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, pvcKey, pvc)).NotTo(HaveOccurred())
				g.Expect(pvc.DeletionTimestamp).NotTo(BeNil())
			}, timeout, interval).Should(Succeed())

			By("Verify Volume CR is deleted in DPU cluster")
			Eventually(func(g Gomega) {
				err := testClientDPU.Get(ctx, volumeKey, volume)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Force remove PVC")
			Expect(testutils.CleanupWithFinalizerRemovalAndWait(ctx, testClientDPU, pvc)).To(Succeed())

			By("Verify DPUVolume is not deleted yet because PV still exists in DPU cluster")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				g.Expect(dpuVolume.DeletionTimestamp).NotTo(BeNil())
			}, time.Second*3, interval).Should(Succeed())

			By("Remove PV from DPU cluster")
			Expect(testutils.CleanupWithFinalizerRemovalAndWait(ctx, testClientDPU, pv)).To(Succeed())

			By("Verify DPUVolume is deleted")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should block DPUVolume deletion when volume is attached", func() {
			By("Create DPUVolumeAttachment")
			dpuVolumeAttachment := getDPUVolumeAttachment()
			createObjects(dpuVolumeAttachment)

			By("Create and schedule DPUVolume")
			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				g.Expect(controllerutil.ContainsFinalizer(dpuVolume, storagev1.DPUVolumeFinalizer)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Delete DPUVolume")
			Expect(testClient.Delete(ctx, dpuVolume)).NotTo(HaveOccurred())

			By("Verify DPUVolume is not deleted due to attachment")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				g.Expect(dpuVolume.DeletionTimestamp).NotTo(BeNil())
				reconciledCond := conditions.Get(dpuVolume, storagev1.ConditionDPUVolumeReconciled)
				g.Expect(reconciledCond).NotTo(BeNil())
				g.Expect(reconciledCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(reconciledCond.Reason).To(Equal(string(conditions.ReasonAwaitingDeletion)))
				g.Expect(reconciledCond.Message).To(ContainSubstring("volume is attached"))
			}, timeout, interval).Should(Succeed())

			By("Delete DPUVolumeAttachment")
			Expect(testClient.Delete(ctx, dpuVolumeAttachment)).NotTo(HaveOccurred())

			By("Verify DPUVolume is now deleted")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should recreate PVC when it has incorrect specification", func() {
			By("Create DPUStorageVendor and DPUStoragePolicy")
			dpuStorageVendor := getDPUStorageVendor()
			dpuStoragePolicy := getDPUStoragePolicy()
			createObjects(dpuStorageVendor, dpuStoragePolicy)

			By("Create StorageClass and CSIDriver in DPU cluster")
			storageClass := getStorageClass()
			csiDriver := getCSIDriver()
			createObjectsDPU(storageClass, csiDriver)

			By("Set DPUStorageVendor and DPUStoragePolicy as ready")
			setDPUStorageVendorReady(dpuStorageVendor, testClient)
			setDPUStoragePolicyReady(dpuStoragePolicy, testClient)

			By("Create PVC with wrong specification before DPUVolume")
			dpuVolume := getDPUVolume()
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpuVolume.Name + "-pvc",
					Namespace: testNsNameDPU,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes:      dpuVolume.Spec.AccessModes,
					Resources:        dpuVolume.Spec.Resources,
					StorageClassName: ptr.To("wrong-storage-class"),
				},
			}

			ownerRefHelper := utils.New(dpuVolumeOwnedByAnnotation)
			ownerRefHelper.SetOwnedBy(pvc, client.ObjectKeyFromObject(dpuVolume))

			// Create the incorrect PVC in the DPU cluster
			Expect(testClientDPU.Create(ctx, pvc)).To(Succeed())
			originalUID := pvc.UID

			By("Create DPUVolume")
			createObjects(dpuVolume)

			pvcKey := client.ObjectKey{Name: dpuVolume.Name + "-pvc", Namespace: testNsNameDPU}

			By("Wait for PVC to be marked for deletion due to incorrect spec")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, pvcKey, pvc)).NotTo(HaveOccurred())
				g.Expect(pvc.DeletionTimestamp).NotTo(BeNil())
			}, timeout, interval).Should(Succeed())

			By("Force remove PVC")
			Expect(testutils.CleanupWithFinalizerRemovalAndWait(ctx, testClientDPU, pvc)).To(Succeed())

			By("Wait for PVC to be recreated with correct specification")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, pvcKey, pvc)).NotTo(HaveOccurred())
				g.Expect(pvc.UID).NotTo(Equal(originalUID))
				g.Expect(pvc.Spec.StorageClassName).NotTo(BeNil())
				g.Expect(*pvc.Spec.StorageClassName).To(Equal("test-storage-class"))
			}, timeout, interval).Should(Succeed())
		})
		It("should update Volume CR when it has incorrect specification", func() {
			By("Create DPUStorageVendor and DPUStoragePolicy")
			dpuStorageVendor := getDPUStorageVendor()
			dpuStoragePolicy := getDPUStoragePolicy()
			createObjects(dpuStorageVendor, dpuStoragePolicy)

			By("Create StorageClass and CSIDriver in DPU cluster")
			storageClass := getStorageClass()
			csiDriver := getCSIDriver()
			createObjectsDPU(storageClass, csiDriver)

			By("Set DPUStorageVendor and DPUStoragePolicy as ready")
			setDPUStorageVendorReady(dpuStorageVendor, testClient)
			setDPUStoragePolicyReady(dpuStoragePolicy, testClient)

			By("Create DPUVolume")
			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			By("Wait for PVC to be created")
			pvc := &corev1.PersistentVolumeClaim{}
			pvcKey := client.ObjectKey{Name: dpuVolume.Name + "-pvc", Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, pvcKey, pvc)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Create and bind PV to the PVC")
			createAndBindPV(pvc)

			By("Wait for Volume CR to be created with correct specification")
			volume := &storagev1.Volume{}
			volumeKey := client.ObjectKey{Name: dpuVolume.Name, Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeKey, volume)).NotTo(HaveOccurred())
				g.Expect(volume.Spec.StorageParameters).To(Equal(dpuVolume.Spec.Parameters))
				g.Expect(volume.Spec.Request.CapacityRange.Request).To(Equal(dpuVolume.Spec.Resources.Requests[corev1.ResourceStorage]))
				g.Expect(volume.Spec.Request.AccessModes).To(Equal(dpuVolume.Spec.AccessModes))
				g.Expect(volume.Status.State).To(Equal(storagev1.VolumeStateAvailable))
			}, timeout, interval).Should(Succeed())
			originalUID := volume.UID

			By("Update Volume CR with incorrect specification to simulate drift")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeKey, volume)).NotTo(HaveOccurred())
				volume.Spec.StorageParameters = map[string]string{
					"wrong-param": "wrong-value",
				}
				volume.Spec.Request.CapacityRange.Request = *resource.NewQuantity(2*1073741824, resource.BinarySI) // Wrong size
				volume.Spec.Request.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}        // Wrong access mode
				g.Expect(testClientDPU.Update(ctx, volume)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Wait for Volume CR to be corrected back to the desired specification")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeKey, volume)).NotTo(HaveOccurred())
				// Verify the spec has been updated back to match the DPUVolume
				g.Expect(volume.Spec.StorageParameters).To(Equal(dpuVolume.Spec.Parameters))
				g.Expect(volume.Spec.Request.CapacityRange.Request).To(Equal(dpuVolume.Spec.Resources.Requests[corev1.ResourceStorage]))
				g.Expect(volume.Spec.Request.AccessModes).To(Equal(dpuVolume.Spec.AccessModes))
				// Verify it's the same Volume object (not recreated)
				g.Expect(volume.UID).To(Equal(originalUID))
				g.Expect(volume.Status.State).To(Equal(storagev1.VolumeStateAvailable))
			}, timeout, interval).Should(Succeed())
		})
	})
})
