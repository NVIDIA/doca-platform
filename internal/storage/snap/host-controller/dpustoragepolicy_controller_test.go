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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("DPUStoragePolicy controller", Ordered, func() {
	var (
		ctx           context.Context
		cancel        context.CancelFunc
		managerStopCh chan struct{}
	)
	BeforeAll(func() {
		ctx, cancel = context.WithCancel(testCtx)
		By("starting manager with only DPUStoragePolicy controller")
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Controller: config.Controller{
				SkipNameValidation: ptr.To(true),
			},
			Cache: cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					&storagev1.DPUStoragePolicy{}: {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
				},
			},
			Metrics: server.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(indexers.SetupIndexers(ctx, mgr)).To(Succeed())

		reconciler := &DPUStoragePolicyReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			Options: Options{
				Namespace:       testNsNameHost,
				TargetNamespace: testNsNameDPU,
			},
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

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
		It("should become valid and ready when all referenced vendors are ready", func() {
			By("Create ready DPUStorageVendors")
			v1 := getDPUStorageVendorWithName("vendor1")
			v2 := getDPUStorageVendorWithName("vendor2")
			createObjects(v1, v2)

			// Set vendors to Ready=True
			setDPUStorageVendorReady(v1, testClient)
			setDPUStorageVendorReady(v2, testClient)

			By("Create DPUStoragePolicy referencing vendors")
			dpuStoragePolicy := getDPUStoragePolicyWithVendors([]string{"vendor1", "vendor2"})
			createObjects(dpuStoragePolicy)

			By("Verify DPUStoragePolicy is reconciled, valid and ready")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStoragePolicy), dpuStoragePolicy)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyReconciled)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyValid)).To(BeTrue())
				g.Expect(conditions.IsTrue(dpuStoragePolicy, conditions.TypeReady)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should be invalid when a referenced vendor is missing", func() {
			By("Create DPUStoragePolicy referencing a non-existing vendor")
			dpuStoragePolicy := getDPUStoragePolicyWithVendors([]string{"missing-vendor"})
			createObjects(dpuStoragePolicy)

			By("Verify DPUStoragePolicy is reconciled but invalid")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStoragePolicy), dpuStoragePolicy)).NotTo(HaveOccurred())
				g.Expect(conditions.IsTrue(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyReconciled)).To(BeTrue())

				validCond := conditions.Get(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyValid)
				g.Expect(validCond).NotTo(BeNil())
				g.Expect(validCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(validCond.Message).To(ContainSubstring("not found"))

				readyCond := conditions.Get(dpuStoragePolicy, conditions.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			}, timeout, interval).Should(Succeed())
		})

		It("should be invalid when a referenced vendor is not ready", func() {
			By("Create a not-ready DPUStorageVendor")
			v := getDPUStorageVendorWithName("vendor-not-ready")
			createObjects(v)
			// Leave it without Ready=True condition

			By("Create DPUStoragePolicy referencing the not-ready vendor")
			dpuStoragePolicy := getDPUStoragePolicyWithVendors([]string{"vendor-not-ready"})
			createObjects(dpuStoragePolicy)

			By("Verify DPUStoragePolicy is reconciled but invalid due to vendor not ready")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStoragePolicy), dpuStoragePolicy)).NotTo(HaveOccurred())
				validCond := conditions.Get(dpuStoragePolicy, storagev1.ConditionDPUStoragePolicyValid)
				g.Expect(validCond).NotTo(BeNil())
				g.Expect(validCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(validCond.Message).To(ContainSubstring("vendor is invalid"))
			}, timeout, interval).Should(Succeed())
		})

		It("should correctly handle deletion of DPUStoragePolicy", func() {
			By("Create a ready vendor and a DPUStoragePolicy")
			v := getDPUStorageVendorWithName("vendor-del")
			createObjects(v)

			setDPUStorageVendorReady(v, testClient)

			dpuStoragePolicy := getDPUStoragePolicyWithVendors([]string{"vendor-del"})
			createObjects(dpuStoragePolicy)

			By("Verify finalizer is added")
			Eventually(func(g Gomega) {
				var obj storagev1.DPUStoragePolicy
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuStoragePolicy), &obj)).NotTo(HaveOccurred())
				g.Expect(controllerutil.ContainsFinalizer(&obj, storagev1.DPUStoragePolicyFinalizer)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Delete DPUStoragePolicy")
			Expect(testClient.Delete(ctx, dpuStoragePolicy)).NotTo(HaveOccurred())

			By("Verify DPUStoragePolicy is deleted")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(dpuStoragePolicy), dpuStoragePolicy)
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
			dpuStoragePolicy := getDPUStoragePolicyWithVendors([]string{"v-a"})
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

		It("transitions to Ready state after vendors become ready", func() {
			// Create vendors first, but set them not ready
			v := getDPUStorageVendorWithName("vendor-tr")
			createObjects(v)

			dpuStoragePolicy := getDPUStoragePolicyWithVendors([]string{"vendor-tr"})
			createObjects(dpuStoragePolicy)

			// Now set vendor ready and expect policy to transition to success
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(v), v)).NotTo(HaveOccurred())
				v.Status.Conditions = []metav1.Condition{{
					Type:               string(conditions.TypeReady),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: v.Generation,
				}}
				g.Expect(testClient.Status().Update(ctx, v)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				newObj := &storagev1.DPUStoragePolicy{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
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
	})
})

// getDPUStorageVendorWithName returns a vendor object with the provided name
func getDPUStorageVendorWithName(name string) *storagev1.DPUStorageVendor {
	return &storagev1.DPUStorageVendor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNsNameHost,
		},
		Spec: storagev1.DPUStorageVendorSpec{
			StorageClassName: "sc-" + name,
			PluginName:       "plugin-" + name,
		},
	}
}

// getDPUStoragePolicyWithVendors returns a DPUStoragePolicy referencing the given vendors
func getDPUStoragePolicyWithVendors(vendors []string) *storagev1.DPUStoragePolicy {
	return &storagev1.DPUStoragePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storage-policy",
			Namespace: testNsNameHost,
		},
		Spec: storagev1.DPUStoragePolicySpec{
			DPUStorageVendors: vendors,
		},
	}
}
