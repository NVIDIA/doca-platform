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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/indexers"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corestoragev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("DPUVolumeAttachment Controller", Ordered, func() {
	var (
		ctx           context.Context
		cancel        context.CancelFunc
		managerStopCh chan struct{}
	)
	BeforeAll(func() {
		By("starting manager with DPUVolumeAttachment controller and DPUCluster watch-registrar")
		ctx, cancel = context.WithCancel(testCtx)
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Controller: config.Controller{
				SkipNameValidation: ptr.To(true),
			},
			Cache: cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					&storagev1.DPUVolume{}:           {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
					&storagev1.DPUVolumeAttachment{}: {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
					&provisioningv1.DPUNode{}:        {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
					&provisioningv1.DPU{}:            {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
				},
			},
			Metrics: server.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(indexers.SetupIndexers(ctx, mgr)).To(Succeed())

		volumeAttachmentReconciler := &DPUVolumeAttachmentReconciler{
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
					volumeAttachmentReconciler.WatchDPUClusterVolumeAttachment,
					volumeAttachmentReconciler.WatchDPUClusterSVVolumeAttachment,
				},
			})
		Expect(errRC).NotTo(HaveOccurred())

		volumeAttachmentReconciler.RemoteCache = rc
		Expect(volumeAttachmentReconciler.SetupWithManager(mgr)).To(Succeed())

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
		It("should successfully reconcile the DPUVolumeAttachment when all dependencies exist and are ready", func() {
			By("Create CSIDriver in DPU cluster")
			csiDriver := getCSIDriver()
			createObjectsDPU(csiDriver)

			By("Create DPUVolume")
			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			By("Create Volume CR in DPU cluster")
			volume := getVolume()
			createObjectsDPU(volume)

			By("Update DPUVolume status to bound")
			setDPUVolumeReadyWithVolumeInfo(dpuVolume)

			By("Create DPUNode and DPU")
			dpuNode := getDPUNode()
			dpu := getDPU()
			createObjects(dpuNode, dpu)
			setDPUReady(dpu)

			By("Create DPUVolumeAttachment")
			dpuVolumeAttachment := getDPUVolumeAttachment()
			createObjects(dpuVolumeAttachment)

			By("Verify SVVolumeAttachment is created in DPU cluster")
			svVolumeAttachment := &storagev1.SVVolumeAttachment{}
			svVolumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, svVolumeAttachmentKey, svVolumeAttachment)).NotTo(HaveOccurred())
				g.Expect(svVolumeAttachment.Spec.NodeName).To(Equal(dpu.Name))
				g.Expect(svVolumeAttachment.Spec.Source.PersistentVolumeName).NotTo(BeNil())
				g.Expect(*svVolumeAttachment.Spec.Source.PersistentVolumeName).To(Equal("test-pv"))
			}, timeout, interval).Should(Succeed())

			By("Mark SVVolumeAttachment as attached")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, svVolumeAttachmentKey, svVolumeAttachment)).NotTo(HaveOccurred())
				svVolumeAttachment.Status.Attached = true
				svVolumeAttachment.Status.AttachmentMetadata = map[string]string{"test-meta": "value"}
				g.Expect(testClientDPU.Status().Update(ctx, svVolumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUVolumeAttachment is reconciled")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)).To(BeTrue())
				g.Expect(dpuVolumeAttachment.Status.ControllerAttached).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.ControllerAttached).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Verify VolumeAttachment is created in DPU cluster")
			volumeAttachment := &storagev1.VolumeAttachment{}
			volumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeAttachmentKey, volumeAttachment)).NotTo(HaveOccurred())
				g.Expect(volumeAttachment.Spec.NodeName).To(Equal(dpu.Name))
				g.Expect(volumeAttachment.Spec.Source.VolumeRef.Name).To(Equal(dpuVolumeAttachment.Spec.DPUVolumeName))
				g.Expect(volumeAttachment.Spec.FunctionTypeConfig).To(Equal(dpuVolumeAttachment.Spec.FunctionTypeConfig))
				g.Expect(volumeAttachment.Status.StorageAttached).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Mark VolumeAttachment as DPU attached")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeAttachmentKey, volumeAttachment)).NotTo(HaveOccurred())
				volumeAttachment.Status.DPU.Attached = true
				volumeAttachment.Status.DPU.PCIDeviceAddress = "0000:03:00.0"
				volumeAttachment.Status.DPU.DeviceName = "/dev/nvme0n1"
				g.Expect(testClientDPU.Status().Update(ctx, volumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUVolumeAttachment status is updated with attachment information")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentControllerAttached)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentDPUAttached)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)).To(BeTrue())
				g.Expect(dpuVolumeAttachment.Status.DPUAttached).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.DPUAttached).To(BeTrue())
				g.Expect(dpuVolumeAttachment.Status.DPU.PCIAddress).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.DPU.PCIAddress).To(Equal("0000:03:00.0"))
				g.Expect(dpuVolumeAttachment.Status.DPU.FuncVUID).To(BeNil())
				g.Expect(dpuVolumeAttachment.Status.DPU.DeviceName).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.DPU.DeviceName).To(Equal("/dev/nvme0n1"))
				g.Expect(dpuVolumeAttachment.Status.AttachmentMetadata).To(Equal(map[string]string{"test-meta": "value"}))
			}, timeout, interval).Should(Succeed())

			By("Backfill function VUID on the existing VolumeAttachment")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeAttachmentKey, volumeAttachment)).NotTo(HaveOccurred())
				volumeAttachment.Status.DPU.FuncVUID = "test-function-vuid"
				g.Expect(testClientDPU.Status().Update(ctx, volumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify the function VUID is propagated to the ready DPUVolumeAttachment")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuVolumeAttachment, conditions.TypeReady)).To(BeTrue())
				g.Expect(dpuVolumeAttachment.Status.DPU.FuncVUID).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.DPU.FuncVUID).To(Equal("test-function-vuid"))
			}, timeout, interval).Should(Succeed())
		})

		It("should mark DPUVolumeAttachment as pending when DPUVolume is missing", func() {
			By("Create DPUVolumeAttachment without DPUVolume")
			dpuVolumeAttachment := getDPUVolumeAttachment()
			createObjects(dpuVolumeAttachment)

			By("Verify DPUVolumeAttachment is pending due to missing DPUVolume")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				reconciledCond := conditions.Get(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)
				g.Expect(reconciledCond).NotTo(BeNil())
				g.Expect(reconciledCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(reconciledCond.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(reconciledCond.Message).To(ContainSubstring("DPUVolume test-vol1 not found"))
			}, timeout, interval).Should(Succeed())
		})

		It("should mark DPUVolumeAttachment as pending when DPUVolume is not bound", func() {
			By("Create DPUVolume without making it ready")
			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			By("Create DPUVolumeAttachment")
			dpuVolumeAttachment := getDPUVolumeAttachment()
			createObjects(dpuVolumeAttachment)

			By("Verify DPUVolumeAttachment is pending due to DPUVolume not being bound")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				reconciledCond := conditions.Get(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)
				g.Expect(reconciledCond).NotTo(BeNil())
				g.Expect(reconciledCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(reconciledCond.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(reconciledCond.Message).To(ContainSubstring("is not ready"))
			}, timeout, interval).Should(Succeed())
		})

		It("should mark DPUVolumeAttachment as pending when DPUNode is missing", func() {
			By("Create and setup DPUVolume to be ready")
			csiDriver := getCSIDriver()
			createObjectsDPU(csiDriver)

			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			volume := getVolume()
			createObjectsDPU(volume)

			setDPUVolumeReadyWithVolumeInfo(dpuVolume)

			By("Create DPUVolumeAttachment without DPUNode")
			dpuVolumeAttachment := getDPUVolumeAttachment()
			createObjects(dpuVolumeAttachment)

			By("Verify DPUVolumeAttachment is pending due to missing DPUNode")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				reconciledCond := conditions.Get(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)
				g.Expect(reconciledCond).NotTo(BeNil())
				g.Expect(reconciledCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(reconciledCond.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(reconciledCond.Message).To(ContainSubstring("DPUNode"))
				g.Expect(reconciledCond.Message).To(ContainSubstring("not found"))
			}, timeout, interval).Should(Succeed())
		})

		It("should handle DPUVolumeAttachment deletion correctly", func() {
			By("Setup complete DPUVolumeAttachment scenario")
			csiDriver := getCSIDriver()
			createObjectsDPU(csiDriver)

			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			volume := getVolume()
			createObjectsDPU(volume)

			setDPUVolumeReadyWithVolumeInfo(dpuVolume)

			dpuNode := getDPUNode()
			dpu := getDPU()
			createObjects(dpuNode, dpu)
			setDPUReady(dpu)

			dpuVolumeAttachment := getDPUVolumeAttachment()
			createObjects(dpuVolumeAttachment)

			// Wait for DPUVolumeAttachment to be reconciled and attachments created
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				g.Expect(controllerutil.ContainsFinalizer(dpuVolumeAttachment, storagev1.DPUVolumeAttachmentFinalizer)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			svVolumeAttachment := &storagev1.SVVolumeAttachment{}
			svVolumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, svVolumeAttachmentKey, svVolumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Mark SVVolumeAttachment as attached")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, svVolumeAttachmentKey, svVolumeAttachment)).NotTo(HaveOccurred())
				svVolumeAttachment.Status.Attached = true
				g.Expect(testClientDPU.Status().Update(ctx, svVolumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			volumeAttachment := &storagev1.VolumeAttachment{}
			volumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeAttachmentKey, volumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Delete DPUVolumeAttachment")
			Expect(testClient.Delete(ctx, dpuVolumeAttachment)).NotTo(HaveOccurred())

			By("Verify SVVolumeAttachment is deleted in DPU cluster")
			Eventually(func(g Gomega) {
				err := testClientDPU.Get(ctx, svVolumeAttachmentKey, svVolumeAttachment)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Verify VolumeAttachment is deleted in DPU cluster")
			Eventually(func(g Gomega) {
				err := testClientDPU.Get(ctx, volumeAttachmentKey, volumeAttachment)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUVolumeAttachment is deleted")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should handle CSI driver that doesn't require attachment", func() {
			By("Setup scenario with CSI driver that doesn't require attachment")
			csiDriver := getCSIDriver()
			csiDriver.Spec.AttachRequired = ptr.To(false) // no attachment required
			createObjectsDPU(csiDriver)

			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			volume := getVolume()
			createObjectsDPU(volume)

			setDPUVolumeReadyWithVolumeInfo(dpuVolume)

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuVolume, storagev1.ConditionDPUVolumeBound)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			dpuNode := getDPUNode()
			dpu := getDPU()
			createObjects(dpuNode, dpu)
			setDPUReady(dpu)

			By("Create DPUVolumeAttachment")
			dpuVolumeAttachment := getDPUVolumeAttachment()
			createObjects(dpuVolumeAttachment)

			By("Verify DPUVolumeAttachment is reconciled without creating SVVolumeAttachment")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentControllerAttached)).To(BeTrue())
				g.Expect(dpuVolumeAttachment.Status.ControllerAttached).NotTo(BeNil())
				g.Expect(*dpuVolumeAttachment.Status.ControllerAttached).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Verify SVVolumeAttachment is not created")
			svVolumeAttachment := &storagev1.SVVolumeAttachment{}
			svVolumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: testNsNameDPU}
			Consistently(func() bool {
				err := testClientDPU.Get(ctx, svVolumeAttachmentKey, svVolumeAttachment)
				return apierrors.IsNotFound(err)
			}, time.Second, time.Millisecond*100).Should(BeTrue())

			By("Verify VolumeAttachment is still created in DPU cluster")
			volumeAttachment := &storagev1.VolumeAttachment{}
			volumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeAttachmentKey, volumeAttachment)).NotTo(HaveOccurred())
				g.Expect(volumeAttachment.Status.StorageAttached).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should recreate SVVolumeAttachment when it has incorrect specification", func() {
			By("Setup complete scenario")
			csiDriver := getCSIDriver()
			createObjectsDPU(csiDriver)

			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			volume := getVolume()
			createObjectsDPU(volume)

			setDPUVolumeReadyWithVolumeInfo(dpuVolume)

			dpuNode := getDPUNode()
			dpu := getDPU()
			createObjects(dpuNode, dpu)
			setDPUReady(dpu)

			By("Create SVVolumeAttachment with wrong specification before DPUVolumeAttachment")
			dpuVolumeAttachment := getDPUVolumeAttachment()
			wrongSVVolumeAttachment := &storagev1.SVVolumeAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpuVolumeAttachment.Name,
					Namespace: testNsNameDPU,
				},
				Spec: corestoragev1.VolumeAttachmentSpec{
					NodeName: "wrong-node",
					Source: corestoragev1.VolumeAttachmentSource{
						PersistentVolumeName: ptr.To("wrong-pv"),
					},
				},
			}
			Expect(testClientDPU.Create(ctx, wrongSVVolumeAttachment)).To(Succeed())
			originalUID := wrongSVVolumeAttachment.UID

			By("Create DPUVolumeAttachment")
			createObjects(dpuVolumeAttachment)

			svVolumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: testNsNameDPU}

			By("Wait for SVVolumeAttachment to be recreated with correct specification")
			Eventually(func(g Gomega) {
				svVolumeAttachment := &storagev1.SVVolumeAttachment{}
				g.Expect(testClientDPU.Get(ctx, svVolumeAttachmentKey, svVolumeAttachment)).NotTo(HaveOccurred())
				g.Expect(svVolumeAttachment.UID).NotTo(Equal(originalUID))
				g.Expect(svVolumeAttachment.Spec.NodeName).To(Equal(dpu.Name))
				g.Expect(svVolumeAttachment.Spec.Source.PersistentVolumeName).NotTo(BeNil())
				g.Expect(*svVolumeAttachment.Spec.Source.PersistentVolumeName).To(Equal("test-pv"))
			}, timeout, interval).Should(Succeed())
		})

		It("should update VolumeAttachment when it has incorrect specification", func() {
			By("Setup complete scenario")
			csiDriver := getCSIDriver()
			createObjectsDPU(csiDriver)

			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			volume := getVolume()
			createObjectsDPU(volume)

			setDPUVolumeReadyWithVolumeInfo(dpuVolume)

			dpuNode := getDPUNode()
			dpu := getDPU()
			createObjects(dpuNode, dpu)
			setDPUReady(dpu)

			dpuVolumeAttachment := getDPUVolumeAttachment()
			createObjects(dpuVolumeAttachment)

			By("Wait for SVVolumeAttachment to be created")
			svVolumeAttachment := &storagev1.SVVolumeAttachment{}
			svVolumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: testNsNameDPU}
			By("Mark SVVolumeAttachment as attached to trigger VolumeAttachment creation")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, svVolumeAttachmentKey, svVolumeAttachment)).NotTo(HaveOccurred())
				svVolumeAttachment.Status.Attached = true
				g.Expect(testClientDPU.Status().Update(ctx, svVolumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Wait for VolumeAttachment to be created with correct specification")
			volumeAttachment := &storagev1.VolumeAttachment{}
			volumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeAttachmentKey, volumeAttachment)).NotTo(HaveOccurred())
				g.Expect(volumeAttachment.Spec.NodeName).To(Equal(dpu.Name))
				g.Expect(volumeAttachment.Status.StorageAttached).To(BeTrue())
			}, timeout, interval).Should(Succeed())
			originalUID := volumeAttachment.UID

			By("Update VolumeAttachment with incorrect specification to simulate drift")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeAttachmentKey, volumeAttachment)).NotTo(HaveOccurred())
				volumeAttachment.Spec.NodeName = "wrong-node"
				g.Expect(testClientDPU.Update(ctx, volumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Wait for VolumeAttachment to be recreated with correct spec")
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, volumeAttachmentKey, volumeAttachment)).NotTo(HaveOccurred())
				g.Expect(volumeAttachment.UID).NotTo(Equal(originalUID))
				g.Expect(volumeAttachment.Spec.NodeName).To(Equal(dpu.Name))
				g.Expect(volumeAttachment.Spec.FunctionTypeConfig).To(Equal(dpuVolumeAttachment.Spec.FunctionTypeConfig))
				g.Expect(volumeAttachment.Status.StorageAttached).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should handle SVVolumeAttachment with AttachError", func() {
			By("Setup complete scenario")
			csiDriver := getCSIDriver()
			createObjectsDPU(csiDriver)

			dpuVolume := getDPUVolume()
			createObjects(dpuVolume)

			volume := getVolume()
			createObjectsDPU(volume)

			setDPUVolumeReadyWithVolumeInfo(dpuVolume)

			dpuNode := getDPUNode()
			dpu := getDPU()
			createObjects(dpuNode, dpu)
			setDPUReady(dpu)

			By("Create DPUVolumeAttachment")
			dpuVolumeAttachment := getDPUVolumeAttachment()
			createObjects(dpuVolumeAttachment)

			By("Verify SVVolumeAttachment is created in DPU cluster")
			svVolumeAttachment := &storagev1.SVVolumeAttachment{}
			svVolumeAttachmentKey := client.ObjectKey{Name: dpuVolumeAttachment.Name, Namespace: testNsNameDPU}
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, svVolumeAttachmentKey, svVolumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Set AttachError on SVVolumeAttachment")
			errorMessage := "Failed to attach volume: device not found"
			Eventually(func(g Gomega) {
				g.Expect(testClientDPU.Get(ctx, svVolumeAttachmentKey, svVolumeAttachment)).NotTo(HaveOccurred())
				svVolumeAttachment.Status.Attached = false
				svVolumeAttachment.Status.AttachError = &corestoragev1.VolumeError{
					Message: errorMessage,
				}
				g.Expect(testClientDPU.Status().Update(ctx, svVolumeAttachment)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify DPUVolumeAttachment has error condition set")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuVolumeAttachment), dpuVolumeAttachment)).NotTo(HaveOccurred())
				controllerAttachedCond := conditions.Get(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentControllerAttached)
				g.Expect(controllerAttachedCond).NotTo(BeNil())
				g.Expect(controllerAttachedCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(controllerAttachedCond.Reason).To(Equal(string(conditions.ReasonPending)))
				g.Expect(controllerAttachedCond.Message).To(ContainSubstring(errorMessage))
			}, timeout, interval).Should(Succeed())
		})
	})
})
