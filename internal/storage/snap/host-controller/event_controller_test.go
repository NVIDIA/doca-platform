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
	"fmt"
	"time"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/indexers"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	eventv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("Event Controller", Ordered, func() {
	var (
		ctx               context.Context
		cancel            context.CancelFunc
		managerStopCh     chan struct{}
		fakeEventRecorder *record.FakeRecorder
		testDPUVolume     *storagev1.DPUVolume
		testPVC           *corev1.PersistentVolumeClaim
	)

	BeforeAll(func() {
		By("starting manager with Event controller")
		ctx, cancel = context.WithCancel(testCtx)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Controller: config.Controller{
				SkipNameValidation: ptr.To(true),
			},
			Cache: cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					&storagev1.DPUVolume{}: {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
				},
			},
			Metrics: server.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(indexers.SetupIndexers(ctx, mgr)).To(Succeed())

		fakeEventRecorder = record.NewFakeRecorder(100)
		eventReconciler := &EventReconciler{
			Client:   mgr.GetClient(),
			Scheme:   mgr.GetScheme(),
			Recorder: fakeEventRecorder,
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
			dpucluster.OptionDefaultNamespaces{DefaultNamespaces: map[string]cache.Config{testNsNameDPU: {}}},
			dpucluster.OptionByObject{
				ByObject: map[client.Object]cache.ByObject{
					&eventv1.Event{}: {
						Field: fields.AndSelectors(
							fields.OneTermEqualSelector("regarding.apiVersion", "v1"),
							fields.OneTermEqualSelector("regarding.kind", "PersistentVolumeClaim")),
					},
				},
			},
			dpucluster.OptionGetWatcherCallbacks{
				GetWatcherCallbacks: []dpucluster.GetWatcherCallback{
					eventReconciler.WatchDPUClusterEvent,
				},
			})
		Expect(errRC).NotTo(HaveOccurred())

		eventReconciler.RemoteCache = rc
		Expect(eventReconciler.SetupWithManager(mgr)).To(Succeed())

		managerStopCh = make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(managerStopCh)
			Expect(mgr.Start(ctx)).To(Succeed())
		}()
	})

	AfterAll(func() {
		By("stopping manager")
		cancel()
		Eventually(managerStopCh).WithTimeout(10 * time.Second).Should(BeClosed())
	})

	BeforeEach(func() {
		// Clear any existing events from the fake recorder
		for len(fakeEventRecorder.Events) > 0 {
			<-fakeEventRecorder.Events
		}

		// Generate unique names for this test run to avoid conflicts
		testSuffix := time.Now().UnixNano()

		// Create test DPUVolume
		testDPUVolume = &storagev1.DPUVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-event-volume-%d", testSuffix),
				Namespace: testNsNameHost,
			},
			Spec: storagev1.DPUVolumeSpec{
				DPUStoragePolicyName: "test-policy",
				AccessModes:          []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: *resource.NewQuantity(1073741824, resource.BinarySI),
					},
				},
			},
		}
		createObjects(testDPUVolume)

		// Create test PVC with owner reference to DPUVolume
		testPVC = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-event-pvc-%d", testSuffix),
				Namespace: testNsNameDPU,
				Annotations: map[string]string{
					dpuVolumeOwnedByAnnotation: client.ObjectKeyFromObject(testDPUVolume).String(),
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: *resource.NewQuantity(1073741824, resource.BinarySI),
					},
				},
			},
		}
		createObjectsDPU(testPVC)
	})

	Context("when processing PVC events", func() {
		It("should redistribute PVC events to DPUVolume", func() {
			By("creating a PVC event in the DPU cluster")
			pvcEvent := &eventv1.Event{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-pvc-event-%d", time.Now().UnixNano()),
					Namespace: testNsNameDPU,
				},
				Regarding: corev1.ObjectReference{
					APIVersion: "v1",
					Kind:       "PersistentVolumeClaim",
					Name:       testPVC.Name,
					Namespace:  testPVC.Namespace,
				},
				Reason:              "ProvisioningSucceeded",
				Note:                "Successfully provisioned volume",
				Type:                corev1.EventTypeNormal,
				EventTime:           metav1.MicroTime{Time: time.Now()},
				Action:              "Provision",
				ReportingController: "test-controller",
				ReportingInstance:   "test-instance",
			}
			createObjectsDPU(pvcEvent)

			By("waiting for the controller to process the event and redistribute it")
			Eventually(func() bool {
				return len(fakeEventRecorder.Events) >= 1
			}, timeout, interval).Should(BeTrue())

			By("verifying the redistributed event contains expected information")
			var foundExpectedEvent bool
			for len(fakeEventRecorder.Events) > 0 {
				recordedEvent := <-fakeEventRecorder.Events
				if recordedEvent != "" {
					Expect(recordedEvent).To(ContainSubstring("Event from PVC"))
					Expect(recordedEvent).To(ContainSubstring(testPVC.Name))
					Expect(recordedEvent).To(ContainSubstring(testDPUClusterName))
					Expect(recordedEvent).To(ContainSubstring("Successfully provisioned volume"))
					foundExpectedEvent = true
					break
				}
			}
			Expect(foundExpectedEvent).To(BeTrue())
		})

		It("should ignore non-PVC events", func() {
			By("creating a Pod event in the DPU cluster")
			podEvent := &eventv1.Event{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-pod-event-%d", time.Now().UnixNano()),
					Namespace: testNsNameDPU,
				},
				Regarding: corev1.ObjectReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "test-pod",
					Namespace:  testNsNameDPU,
				},
				Reason:              "Started",
				Note:                "Pod started successfully",
				Type:                corev1.EventTypeNormal,
				EventTime:           metav1.MicroTime{Time: time.Now()},
				Action:              "Start",
				ReportingController: "test-controller",
				ReportingInstance:   "test-instance",
			}
			createObjectsDPU(podEvent)

			By("waiting and verifying no events are redistributed")
			Consistently(func() int {
				return len(fakeEventRecorder.Events)
			}, 2*time.Second, interval).Should(Equal(0))
		})
	})
})
