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
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/test/utils/informer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("DPUStorageVendor Controller", Ordered, func() {
	var (
		ctx           context.Context
		cancel        context.CancelFunc
		managerStopCh chan struct{}
	)
	BeforeAll(func() {
		By("starting manager with DPUStorageVendor controller and DPUCluster watch-registrar")
		ctx, cancel = context.WithCancel(testCtx)
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Controller: config.Controller{
				SkipNameValidation: ptr.To(true),
			},
			Cache: cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					&storagev1.DPUStorageVendor{}: {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
				},
			},
			Metrics: server.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(indexers.SetupIndexers(ctx, mgr)).To(Succeed())

		vendorReconciler := &DPUStorageVendorReconciler{
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
					vendorReconciler.WatchDPUClusterStorageClass,
					vendorReconciler.WatchDPUClusterCSIDriver,
				},
			})

		Expect(errRC).NotTo(HaveOccurred())

		vendorReconciler.RemoteCache = rc
		Expect(vendorReconciler.SetupWithManager(mgr)).To(Succeed())

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
		It("should successfully reconcile the DPUStorageVendor when StorageClass and CSIDriver exist", func() {
			By("Create StorageClass and CSIDriver in DPU cluster")
			storageClass := getStorageClass()
			csiDriver := getCSIDriver()
			createObjectsDPU(storageClass, csiDriver)

			By("Create DPUStorageVendor")
			dpuStorageVendor := getDPUStorageVendor()
			createObjects(dpuStorageVendor)

			By("Verify DPUStorageVendor is reconciled and valid")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStorageVendor), dpuStorageVendor)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorValid)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuStorageVendor, conditions.TypeReady)).To(BeTrue())
				g.Expect(dpuStorageVendor.Status.DPUClusters).ToNot(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		It("should mark DPUStorageVendor as invalid when StorageClass is missing", func() {
			By("Create DPUStorageVendor without StorageClass")
			dpuStorageVendor := getDPUStorageVendor()
			createObjects(dpuStorageVendor)

			By("Verify DPUStorageVendor is reconciled but marked as invalid")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStorageVendor), dpuStorageVendor)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled)).To(BeTrue())

				validCond := conditions.Get(dpuStorageVendor, storagev1.ConditionDPUStorageVendorValid)
				g.Expect(validCond).NotTo(BeNil())
				g.Expect(validCond.Status).To(Equal(metav1.ConditionFalse))

				readyCond := conditions.Get(dpuStorageVendor, conditions.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))

				// Check the error message contains information about missing StorageClass
				g.Expect(validCond.Message).To(ContainSubstring("StorageClass test-storage-class not found"))
			}, timeout, interval).Should(Succeed())
		})

		It("should mark DPUStorageVendor as invalid when CSIDriver is missing", func() {
			By("Create StorageClass without CSIDriver in DPU cluster")
			storageClass := getStorageClass()
			createObjectsDPU(storageClass)

			By("Create DPUStorageVendor")
			dpuStorageVendor := getDPUStorageVendor()
			createObjects(dpuStorageVendor)

			By("Verify DPUStorageVendor is reconciled but marked as invalid due to missing CSIDriver")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStorageVendor), dpuStorageVendor)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled)).To(BeTrue())

				validCond := conditions.Get(dpuStorageVendor, storagev1.ConditionDPUStorageVendorValid)
				g.Expect(validCond).NotTo(BeNil())
				g.Expect(validCond.Status).To(Equal(metav1.ConditionFalse))

				readyCond := conditions.Get(dpuStorageVendor, conditions.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))

				// Check the error message contains information about missing CSIDriver
				g.Expect(validCond.Message).To(ContainSubstring("CSIDriver test-csi-driver not found"))
			}, timeout, interval).Should(Succeed())
		})

		It("should correctly handle deletion of DPUStorageVendor", func() {
			By("Create StorageClass and CSIDriver in DPU cluster")
			storageClass := getStorageClass()
			csiDriver := getCSIDriver()
			createObjectsDPU(storageClass, csiDriver)

			By("Create DPUStorageVendor")
			dpuStorageVendor := getDPUStorageVendor()
			createObjects(dpuStorageVendor)

			By("Verify DPUStorageVendor is initially valid")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStorageVendor), dpuStorageVendor)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorValid)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Delete DPUStorageVendor")
			Expect(testClient.Delete(ctx, dpuStorageVendor)).NotTo(HaveOccurred())

			By("Verify DPUStorageVendor is deleted")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(dpuStorageVendor), dpuStorageVendor)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should become valid when missing StorageClass is created", func() {
			By("Create DPUStorageVendor without StorageClass")
			dpuStorageVendor := getDPUStorageVendor()
			createObjects(dpuStorageVendor)

			By("Verify DPUStorageVendor is initially invalid")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStorageVendor), dpuStorageVendor)).NotTo(HaveOccurred())
				validCond := conditions.Get(dpuStorageVendor, storagev1.ConditionDPUStorageVendorValid)
				g.Expect(validCond).NotTo(BeNil())
				g.Expect(validCond.Status).To(Equal(metav1.ConditionFalse))
			}, timeout, interval).Should(Succeed())

			By("Create StorageClass and CSIDriver in DPU cluster")
			storageClass := getStorageClass()
			csiDriver := getCSIDriver()
			createObjectsDPU(storageClass, csiDriver)

			By("Verify DPUStorageVendor becomes valid")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStorageVendor), dpuStorageVendor)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorReconciled)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuStorageVendor, storagev1.ConditionDPUStorageVendorValid)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuStorageVendor, conditions.TypeReady)).To(BeTrue())
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
					HaveField("Type", string(storagev1.ConditionDPUStorageVendorValid)),
					HaveField("Status", metav1.ConditionUnknown),
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
			By("Create StorageClass and CSIDriver first")
			storageClass := getStorageClass()
			csiDriver := getCSIDriver()
			createObjectsDPU(storageClass, csiDriver)

			dpuStorageVendor := getDPUStorageVendor()
			createObjects(dpuStorageVendor)

			// Verify conditions transition to Ready
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUStorageVendor{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				g.Expect(newObj.Status.Conditions).ToNot(BeEmpty())
				g.Expect(newObj.Status.DPUClusters).ToNot(BeEmpty())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(storagev1.ConditionDPUStorageVendorValid)),
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
	})
})
