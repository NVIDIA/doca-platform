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

package controller

import (
	"context"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Variables are already declared in suite_test.go

// Test setup is handled by suite_test.go

// Helper functions for creating test objects
// Note: These functions are already defined in podipam_controller_test.go
// We'll use the existing ones from that file

// createTestPodWithName creates a test pod with a custom name for envtest
func createTestPodWithName(name string, annotations map[string]string, labels map[string]string) *corev1.Pod {
	if annotations == nil {
		annotations = make(map[string]string)
	}
	if labels == nil {
		labels = make(map[string]string)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: corev1.PodSpec{
			NodeName: "worker-1",
			Containers: []corev1.Container{
				{Name: "ctr1", Image: "image"},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	Expect(testClient.Create(ctx, pod)).To(Succeed())
	Eventually(func() error {
		return testClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, pod)
	}, 10*time.Second).Should(Succeed())
	return pod
}

var _ = Describe("PodRestartController Envtest Integration", func() {
	var (
		ctx            context.Context
		controller     *PodRestartController
		cleanupObjects []client.Object
	)

	BeforeEach(func() {
		ctx = context.Background()
		controller = &PodRestartController{
			Client: testClient,
			Scheme: scheme.Scheme,
		}
		cleanupObjects = []client.Object{}
	})

	AfterEach(func() {
		// Clean up created objects first (ServiceInterfaces, ServiceChains, etc.)
		By("Cleaning up the objects")
		Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())

		// Then clean up any remaining pods by setting them to succeeded state
		// This is done separately to avoid conflicts with controller reconciliation
		podList := &corev1.PodList{}
		if err := testClient.List(ctx, podList); err == nil {
			for i := range podList.Items {
				pod := &podList.Items[i]
				// Only update if pod is not being deleted and not already succeeded
				if pod.DeletionTimestamp == nil && pod.Status.Phase != corev1.PodSucceeded {
					pod.Status.Phase = corev1.PodSucceeded
					_ = testClient.Status().Patch(ctx, pod, client.Merge)
				}
			}
		}
	})

	Describe("handlePodRestart", func() {
		It("should delete the pod", func() {
			pod := createTestPodWithName(
				"test-pod-standalone",
				map[string]string{NetworkAttachmentAnnot: "test-network"},
				nil,
			)

			// Get fresh copy before updating status to avoid conflicts
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())

			// Set pod to Running state so it can be properly processed by the controller
			pod.Status.Phase = corev1.PodRunning
			Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

			// Test that the function completes without error
			err := controller.handlePodRestart(ctx, pod)
			Expect(err).ToNot(HaveOccurred())
		})

	})

	Describe("needsRestartDueToDigestChange", func() {
		It("returns false if pod has no network config digest annotation", func() {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
			Expect(controller.needsRestartDueToDigestChange(ctx, pod)).To(BeFalse())
		})

		It("returns true when digest has changed", func() {
			serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-digest-change", "firewall", "sfceth1")
			cleanupObjects = append(cleanupObjects, serviceInterface)

			mtu := 1500
			sc := createServiceChainWithServiceInterface(ctx, "sc-digest-change", nil, "firewall", "sfceth1", &mtu)
			cleanupObjects = append(cleanupObjects, sc)

			// Wait for resources to be available in cache and properly indexed
			Eventually(func(g Gomega) {
				retrievedSC := &dpuservicev1.ServiceChain{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "sc-digest-change"}, retrievedSC)).To(Succeed())
				retrievedSI := &dpuservicev1.ServiceInterface{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "firewall-sfceth1-digest-change"}, retrievedSI)).To(Succeed())

				// Also verify getServiceInterfaceWithLabels can find it via label selector
				_, err := getServiceInterfaceWithLabels(ctx, testClient, "worker-1", "default", map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
					"svc.dpu.nvidia.com/interface":    "sfceth1",
				})
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

			pod := createTestPodWithName(
				"test-pod-digest-change",
				map[string]string{
					NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					NetworkDigestAnnotation: "old-digest",
				},
				map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
				},
			)

			// Get fresh copy before updating status to avoid conflicts
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())

			// Set pod to Running state so it can be processed
			pod.Status.Phase = corev1.PodRunning
			Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

			// Now check if restart is needed - resources should be available
			Eventually(func(g Gomega) {
				needsRestart, err := controller.needsRestartDueToDigestChange(ctx, pod)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(needsRestart).To(BeTrue())
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("returns true when Pending pod has valid network and outdated digest", func() {
			serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-pending-valid", "firewall", "sfceth1")
			cleanupObjects = append(cleanupObjects, serviceInterface)

			mtu := 1500
			sc := createServiceChainWithServiceInterface(ctx, "sc-pending-valid", nil, "firewall", "sfceth1", &mtu)
			cleanupObjects = append(cleanupObjects, sc)

			// Wait for resources to be available in cache and properly indexed
			Eventually(func(g Gomega) {
				retrievedSC := &dpuservicev1.ServiceChain{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "sc-pending-valid"}, retrievedSC)).To(Succeed())
				retrievedSI := &dpuservicev1.ServiceInterface{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "firewall-sfceth1-pending-valid"}, retrievedSI)).To(Succeed())

				// Also verify getServiceInterfaceWithLabels can find it via label selector
				_, err := getServiceInterfaceWithLabels(ctx, testClient, "worker-1", "default", map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
					"svc.dpu.nvidia.com/interface":    "sfceth1",
				})
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

			pod := createTestPodWithName(
				"test-pod-pending-valid-network",
				map[string]string{
					NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					NetworkDigestAnnotation: "outdated-digest",
				},
				map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
				},
			)

			// Set pod to Pending state - this simulates a pod stuck in Pending with outdated digest
			pod.Status.Phase = corev1.PodPending
			Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

			// Verify the pod is in Pending state
			Eventually(func(g Gomega) {
				currentPod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)).To(Succeed())
				g.Expect(currentPod.Status.Phase).To(Equal(corev1.PodPending))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			// Now check if restart is needed - should return true for Pending pod with valid network and outdated digest
			Eventually(func(g Gomega) {
				currentPod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)).To(Succeed())
				needsRestart, err := controller.needsRestartDueToDigestChange(ctx, currentPod)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(needsRestart).To(BeTrue(), "Pending pod with valid network and outdated digest should need restart")
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("returns false when Pending pod has valid network and current digest", func() {
			serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-pending-current", "firewall", "sfceth1")
			cleanupObjects = append(cleanupObjects, serviceInterface)

			mtu := 1500
			sc := createServiceChainWithServiceInterface(ctx, "sc-pending-current", nil, "firewall", "sfceth1", &mtu)
			cleanupObjects = append(cleanupObjects, sc)

			// Wait for resources to be available in cache and properly indexed
			Eventually(func(g Gomega) {
				retrievedSC := &dpuservicev1.ServiceChain{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "sc-pending-current"}, retrievedSC)).To(Succeed())
				retrievedSI := &dpuservicev1.ServiceInterface{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "firewall-sfceth1-pending-current"}, retrievedSI)).To(Succeed())

				// Also verify getServiceInterfaceWithLabels can find it via label selector
				_, err := getServiceInterfaceWithLabels(ctx, testClient, "worker-1", "default", map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
					"svc.dpu.nvidia.com/interface":    "sfceth1",
				})
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

			// Create a temporary pod with the same configuration to calculate the expected digest
			testPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-for-pending-digest",
					Namespace: "default",
					Annotations: map[string]string{
						NetworkAttachmentAnnot: `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					},
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "worker-1",
					Containers: []corev1.Container{
						{Name: "ctr1", Image: "image"},
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodPending},
			}

			// Calculate the expected digest - wrap in Eventually to handle cache delays
			var expectedDigest string
			Eventually(func(g Gomega) {
				networks, err := GetPodNetworks(testPod)
				g.Expect(err).NotTo(HaveOccurred())
				digest, err := CalculatePodNetworkDigest(ctx, testClient, testPod, networks)
				g.Expect(err).ToNot(HaveOccurred())
				expectedDigest = digest
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

			pod := createTestPodWithName(
				"test-pod-pending-current-digest",
				map[string]string{
					NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					NetworkDigestAnnotation: expectedDigest,
				},
				map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
				},
			)

			// Set pod to Pending state - this simulates a pod in Pending with current digest
			pod.Status.Phase = corev1.PodPending
			Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

			// Verify the pod is in Pending state
			Eventually(func(g Gomega) {
				currentPod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)).To(Succeed())
				g.Expect(currentPod.Status.Phase).To(Equal(corev1.PodPending))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			// Now check if restart is needed - should return false for Pending pod with valid network and current digest
			Eventually(func(g Gomega) {
				currentPod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)).To(Succeed())
				needsRestart, err := controller.needsRestartDueToDigestChange(ctx, currentPod)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(needsRestart).To(BeFalse(), "Pending pod with valid network and current digest should not need restart")
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("returns false when digest has not changed", func() {
			serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-digest-match-unique", "firewall", "sfceth1")
			cleanupObjects = append(cleanupObjects, serviceInterface)

			mtu := 1500
			sc := createServiceChainWithServiceInterface(ctx, "sc-digest-match-unique", nil, "firewall", "sfceth1", &mtu)
			cleanupObjects = append(cleanupObjects, sc)

			// Wait for resources to be available in cache and properly indexed
			Eventually(func(g Gomega) {
				retrievedSC := &dpuservicev1.ServiceChain{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "sc-digest-match-unique"}, retrievedSC)).To(Succeed())
				retrievedSI := &dpuservicev1.ServiceInterface{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "firewall-sfceth1-digest-match-unique"}, retrievedSI)).To(Succeed())

				// Also verify getServiceInterfaceWithLabels can find it via label selector
				_, err := getServiceInterfaceWithLabels(ctx, testClient, "worker-1", "default", map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
					"svc.dpu.nvidia.com/interface":    "sfceth1",
				})
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

			// Create a pod with the same configuration that will be used for digest calculation
			testPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-for-digest",
					Namespace: "default",
					Annotations: map[string]string{
						NetworkAttachmentAnnot: `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					},
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "worker-1",
					Containers: []corev1.Container{
						{Name: "ctr1", Image: "image"},
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			}

			// Calculate the expected digest - wrap in Eventually to handle cache delays
			var expectedDigest string
			Eventually(func(g Gomega) {
				networks, err := GetPodNetworks(testPod)
				g.Expect(err).NotTo(HaveOccurred())
				digest, err := CalculatePodNetworkDigest(ctx, testClient, testPod, networks)
				g.Expect(err).ToNot(HaveOccurred())
				expectedDigest = digest
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

			pod := createTestPodWithName(
				"test-pod-digest-match-unique",
				map[string]string{
					NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					NetworkDigestAnnotation: expectedDigest,
				},
				map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
				},
			)

			// Get fresh copy before updating status to avoid conflicts
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())

			// Set pod to Running state so it can be processed
			pod.Status.Phase = corev1.PodRunning
			Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

			// Use Eventually to wait for ServiceChain/ServiceInterface to be available in cache
			// This allows transient errors (missing resources) while they propagate
			Eventually(func(g Gomega) {
				needsRestart, err := controller.needsRestartDueToDigestChange(ctx, pod)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(needsRestart).To(BeFalse())
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("returns false when pod is in Pending phase with invalid network", func() {
			pod := createTestPodWithName(
				"test-pod-pending",
				map[string]string{
					NetworkAttachmentAnnot:  `[{"name":"invalid-network"}]`,
					NetworkDigestAnnotation: "stored-digest",
				},
				map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
				},
			)

			// Update pod status to pending
			pod.Status.Phase = corev1.PodPending
			Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

			createdPod := &corev1.Pod{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), createdPod)).To(Succeed())
			Expect(createdPod.Status.Phase).To(Equal(corev1.PodPending))

			needsRestart, err := controller.needsRestartDueToDigestChange(ctx, createdPod)
			Expect(err).NotTo(HaveOccurred())
			Expect(needsRestart).To(BeFalse())
		})

		It("returns false when pod has DeletionTimestamp", func() {
			pod := createTestPodWithName(
				"test-pod-deleting",
				map[string]string{
					NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					NetworkDigestAnnotation: "stored-digest",
				},
				map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
				},
			)

			now := metav1.Now()
			pod.DeletionTimestamp = &now

			needsRestart, err := controller.needsRestartDueToDigestChange(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(needsRestart).To(BeFalse())
		})
	})

	Describe("shouldProcessPod", func() {
		It("returns false if pod has no digest annotation", func() {
			pod := &corev1.Pod{
				Status:     corev1.PodStatus{Phase: corev1.PodPending},
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
			}
			Expect(shouldProcessPod(pod, nil)).To(BeFalse())
		})
		It("returns false if pod is in Pending phase with invalid network", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodPending},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{NetworkDigestAnnotation: "some-digest"},
				},
			}
			networks := []*multustypes.NetworkSelectionElement{
				{Name: "invalid-network"},
			}
			Expect(shouldProcessPod(pod, networks)).To(BeFalse())
		})
		It("returns false if pod is being deleted", func() {
			now := metav1.Now()
			pod := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}, ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now}}
			Expect(shouldProcessPod(pod, nil)).To(BeFalse())
		})
		It("returns false if pod is running and has network annotation with no network digest annotation", func() {
			pod := &corev1.Pod{
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{NetworkAttachmentAnnot: "foo"}, DeletionTimestamp: nil},
			}
			Expect(shouldProcessPod(pod, nil)).To(BeFalse())
		})
		It("returns true if pod is running and has network annotation and digest annotation", func() {
			pod := &corev1.Pod{
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{NetworkAttachmentAnnot: "foo", NetworkDigestAnnotation: "some-digest"}, DeletionTimestamp: nil},
			}
			Expect(shouldProcessPod(pod, nil)).To(BeTrue())
		})
		It("returns false if pod has invalid network annotation", func() {
			pod := &corev1.Pod{
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{NetworkAttachmentAnnot: "foo", NetworkDigestAnnotation: "some-digest"}, DeletionTimestamp: nil},
			}
			// Create networks with invalid network
			networks := []*multustypes.NetworkSelectionElement{
				{
					Name:             "valid-network",
					Namespace:        "default",
					InterfaceRequest: "eth0",
				},
				{
					Name:             "invalid-network",
					Namespace:        "invalid-namespace",
					InterfaceRequest: "invalid-interface",
				},
			}
			Expect(shouldProcessPod(pod, networks)).To(BeFalse())
		})
		It("returns true if pod has only valid networks", func() {
			pod := &corev1.Pod{
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{NetworkAttachmentAnnot: "foo", NetworkDigestAnnotation: "some-digest"}, DeletionTimestamp: nil},
			}
			// Create networks with only valid networks
			networks := []*multustypes.NetworkSelectionElement{
				{
					Name:             "valid-network-1",
					Namespace:        "default",
					InterfaceRequest: "eth0",
				},
				{
					Name:             "valid-network-2",
					Namespace:        "default",
					InterfaceRequest: "eth1",
				},
			}
			Expect(shouldProcessPod(pod, networks)).To(BeTrue())
		})
		It("returns true if Pending pod has valid network and digest annotation", func() {
			pod := &corev1.Pod{
				Status:     corev1.PodStatus{Phase: corev1.PodPending},
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{NetworkAttachmentAnnot: "foo", NetworkDigestAnnotation: "some-digest"}, DeletionTimestamp: nil},
			}
			// Create networks with valid networks (not invalid-network)
			networks := []*multustypes.NetworkSelectionElement{
				{
					Name:             "valid-network",
					Namespace:        "default",
					InterfaceRequest: "eth0",
				},
			}
			Expect(shouldProcessPod(pod, networks)).To(BeTrue())
		})
	})

	Describe("Reconcile", func() {
		It("should handle ServiceChain not found gracefully", func() {
			// Create a ServiceChain and then delete it to trigger reconciliation
			serviceChain := createServiceChainWithServiceInterface(ctx, "test-notfound", nil, "firewall", "sfceth1", ptr.To(1500))

			// Delete the ServiceChain to trigger reconciliation
			Expect(testClient.Delete(ctx, serviceChain)).To(Succeed())

			// The controller should handle the deletion gracefully
			Eventually(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(serviceChain), &dpuservicev1.ServiceChain{}))).To(BeTrue())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("should restart pod only when digest changes", func() {
			serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-integration", "firewall", "sfceth1")
			cleanupObjects = append(cleanupObjects, serviceInterface)

			mtu1 := 1500
			sc := createServiceChainWithServiceInterface(ctx, "sc-integration-test", nil, "firewall", "sfceth1", &mtu1)
			cleanupObjects = append(cleanupObjects, sc)

			pod := createTestPodWithName(
				"test-pod-integration",
				map[string]string{
					NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					NetworkDigestAnnotation: "initial-digest",
				},
				map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
				},
			)

			// Get fresh copy before updating status to avoid conflicts
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())

			// Set pod to Running state so it can be processed by the controller
			pod.Status.Phase = corev1.PodRunning
			Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

			// Verify the pod is in Running state before reconciliation
			Eventually(func(g Gomega) {
				currentPod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)).To(Succeed())
				g.Expect(currentPod.Status.Phase).To(Equal(corev1.PodRunning))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			// Trigger reconciliation by updating the ServiceChain to cause digest change
			sc.Spec.Switches[0].ServiceMTU = ptr.To(1600) // Change MTU to trigger reconciliation
			Expect(testClient.Patch(ctx, sc, client.Merge)).To(Succeed())

			// Wait for the reconciliation to complete - check for pod being marked for deletion
			Eventually(func(g Gomega) {
				currentPod := &corev1.Pod{}
				err := testClient.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)
				// Pod should either be deleted (not found) or marked for deletion
				if err != nil {
					g.Expect(err.Error()).To(ContainSubstring("not found"))
				} else {
					g.Expect(currentPod.DeletionTimestamp).NotTo(BeNil())
				}
			}).WithTimeout(15 * time.Second).Should(Succeed())
		})

		It("should skip reconciliation when ServiceChain is marked for deletion", func() {
			serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-deletion-skip", "firewall", "sfceth1")
			cleanupObjects = append(cleanupObjects, serviceInterface)

			serviceChain := createServiceChainWithServiceInterface(ctx, "sc-for-deletion", nil, "firewall", "sfceth1", ptr.To(1500))
			// Don't add serviceChain to cleanupObjects since we're deleting it manually

			pod := createTestPodWithName(
				"test-pod-deletion-skip",
				map[string]string{
					NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					NetworkDigestAnnotation: "old-digest",
				},
				map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
				},
			)

			// Delete the ServiceChain to mark it for deletion
			Expect(testClient.Delete(ctx, serviceChain)).To(Succeed())

			// The controller should skip processing when ServiceChain is marked for deletion
			// Pod should still exist (not processed due to deletion)
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("should not process pods on different nodes", func() {
			serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-diff-node", "firewall", "sfceth1")
			cleanupObjects = append(cleanupObjects, serviceInterface)

			serviceChain := createServiceChainWithServiceInterface(ctx, "sc-diff-node", nil, "firewall", "sfceth1", ptr.To(1500))
			cleanupObjects = append(cleanupObjects, serviceChain)

			// Create pod on a different node from the start
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-diff-node",
					Namespace: "default",
					Annotations: map[string]string{
						NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
						NetworkDigestAnnotation: "old-digest",
					},
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "worker-2", // Different node
					Containers: []corev1.Container{
						{Name: "ctr1", Image: "image"},
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			}
			Expect(testClient.Create(ctx, pod)).To(Succeed())

			// Trigger reconciliation by updating the ServiceChain
			serviceChain.Spec.Switches[0].ServiceMTU = ptr.To(1600)
			Expect(testClient.Patch(ctx, serviceChain, client.Merge)).To(Succeed())

			// Pod should still exist (not processed because it's on different node)
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).ToNot(HaveOccurred())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("should handle multiple pods on the same node correctly", func() {
			serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-multi-pod", "firewall", "sfceth1")
			cleanupObjects = append(cleanupObjects, serviceInterface)

			serviceChain := createServiceChainWithServiceInterface(ctx, "sc-multi-pod", nil, "firewall", "sfceth1", ptr.To(1500))
			cleanupObjects = append(cleanupObjects, serviceChain)

			pod1 := createTestPodWithName(
				"test-pod-multi-handle-1",
				map[string]string{
					NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					NetworkDigestAnnotation: "old-digest-1",
				},
				map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
				},
			)
			// Don't add pod1 to cleanupObjects - controller will handle deletion

			// Get fresh copy before updating status to avoid conflicts
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod1), pod1)).To(Succeed())

			// Set pod1 to Running state
			pod1.Status.Phase = corev1.PodRunning
			Expect(testClient.Status().Patch(ctx, pod1, client.Merge)).To(Succeed())

			pod2 := createTestPodWithName(
				"test-pod-multi-handle-2",
				map[string]string{
					NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
					NetworkDigestAnnotation: "matching-digest",
				},
				map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "firewall",
				},
			)
			// Don't add pod2 to cleanupObjects - controller will handle deletion

			// Get fresh copy before updating status to avoid conflicts
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod2), pod2)).To(Succeed())

			// Set pod2 to Running state
			pod2.Status.Phase = corev1.PodRunning
			Expect(testClient.Status().Patch(ctx, pod2, client.Merge)).To(Succeed())

			// Verify both pods are in Running state before reconciliation
			Eventually(func(g Gomega) {
				currentPod1 := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod1), currentPod1)).To(Succeed())
				g.Expect(currentPod1.Status.Phase).To(Equal(corev1.PodRunning))

				currentPod2 := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod2), currentPod2)).To(Succeed())
				g.Expect(currentPod2.Status.Phase).To(Equal(corev1.PodRunning))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			// Trigger reconciliation by updating the ServiceChain
			serviceChain.Spec.Switches[0].ServiceMTU = ptr.To(1600)
			Expect(testClient.Patch(ctx, serviceChain, client.Merge)).To(Succeed())

			// Wait for the reconciliation to complete - check for pod1 being marked for deletion
			Eventually(func(g Gomega) {
				currentPod := &corev1.Pod{}
				err := testClient.Get(ctx, client.ObjectKeyFromObject(pod1), currentPod)
				// Pod should either be deleted (not found) or marked for deletion
				if err != nil {
					g.Expect(err.Error()).To(ContainSubstring("not found"))
				} else {
					g.Expect(currentPod.DeletionTimestamp).NotTo(BeNil())
				}
			}).WithTimeout(15 * time.Second).Should(Succeed())

			// Pod2 should still exist (no restart needed)
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(pod2), &corev1.Pod{})
				g.Expect(err).ToNot(HaveOccurred())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

	})

	var _ = Describe("hasInvalidNetwork", func() {
		It("should return false for empty network slice", func() {
			networks := []*multustypes.NetworkSelectionElement{}
			Expect(HasInvalidNetwork(networks)).To(BeFalse())
		})

		It("should return false for nil network slice", func() {
			Expect(HasInvalidNetwork(nil)).To(BeFalse())
		})

		It("should return false when all networks are valid", func() {
			networks := []*multustypes.NetworkSelectionElement{
				{Name: "valid-network-1"},
				{Name: "valid-network-2"},
				{Name: "another-valid-network"},
			}
			Expect(HasInvalidNetwork(networks)).To(BeFalse())
		})

		It("should return true when one network is invalid", func() {
			networks := []*multustypes.NetworkSelectionElement{
				{Name: "valid-network-1"},
				{Name: "invalid-network"}, // This is the invalid network
				{Name: "valid-network-2"},
			}
			Expect(HasInvalidNetwork(networks)).To(BeTrue())
		})

		It("should return true when all networks are invalid", func() {
			networks := []*multustypes.NetworkSelectionElement{
				{Name: "invalid-network"},
				{Name: "invalid-network"},
				{Name: "invalid-network"},
			}
			Expect(HasInvalidNetwork(networks)).To(BeTrue())
		})

		It("should return true when only network is invalid", func() {
			networks := []*multustypes.NetworkSelectionElement{
				{Name: "invalid-network"},
			}
			Expect(HasInvalidNetwork(networks)).To(BeTrue())
		})

		It("should return false when network name is similar but not exactly invalid", func() {
			networks := []*multustypes.NetworkSelectionElement{
				{Name: "invalid-network-extra"},
				{Name: "my-invalid-network"},
				{Name: "invalid-network-suffix"},
			}
			Expect(HasInvalidNetwork(networks)).To(BeFalse())
		})

		It("should return false when network name is case sensitive", func() {
			networks := []*multustypes.NetworkSelectionElement{
				{Name: "Invalid-Network"},
				{Name: "INVALID-NETWORK"},
				{Name: "invalid-Network"},
			}
			Expect(HasInvalidNetwork(networks)).To(BeFalse())
		})

		It("should return false when network name has extra spaces", func() {
			networks := []*multustypes.NetworkSelectionElement{
				{Name: " invalid-network"},
				{Name: "invalid-network "},
				{Name: " invalid-network "},
			}
			Expect(HasInvalidNetwork(networks)).To(BeFalse())
		})

		It("should return false when network has empty name", func() {
			networks := []*multustypes.NetworkSelectionElement{
				{Name: ""},
				{Name: "valid-network"},
			}
			Expect(HasInvalidNetwork(networks)).To(BeFalse()) // Empty name is not "invalid-network"
		})

		It("should handle networks with additional fields", func() {
			networks := []*multustypes.NetworkSelectionElement{
				{
					Name:             "invalid-network",
					InterfaceRequest: "eth0",
					MacRequest:       "00:11:22:33:44:55",
				},
				{
					Name:             "valid-network",
					InterfaceRequest: "eth1",
				},
			}
			Expect(HasInvalidNetwork(networks)).To(BeTrue())
		})
	})

	Describe("Integration Tests - Full Controller Flow", func() {
		Context("ServiceChain and ServiceInterface scenarios", func() {
			It("should delete pod when ServiceChain is on correct node and pod has digest mismatch", func() {
				By("Create ServiceInterface on correct node")
				serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-correct-node", "firewall", "sfceth1")
				cleanupObjects = append(cleanupObjects, serviceInterface)

				By("Create ServiceChain on correct node")
				serviceChain := createServiceChainWithServiceInterface(ctx, "sc-correct-node", nil, "firewall", "sfceth1", ptr.To(1500))
				cleanupObjects = append(cleanupObjects, serviceChain)

				By("Create Pod with digest mismatch")
				pod := createTestPodWithName(
					"test-pod-digest-mismatch",
					map[string]string{
						NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
						NetworkDigestAnnotation: "old-digest",
					},
					map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				)

				By("Get fresh copy before updating status to avoid conflicts")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())

				By("Set pod to Running state")
				pod.Status.Phase = corev1.PodRunning
				Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

				By("Verify ServiceChain has correct node")
				Eventually(func(g Gomega) {
					sc := &dpuservicev1.ServiceChain{}
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(serviceChain), sc)).To(Succeed())
					g.Expect(sc.Spec.Node).ToNot(BeNil())
					g.Expect(*sc.Spec.Node).To(Equal("worker-1"))
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("Verify pod is on correct node")
				Eventually(func(g Gomega) {
					currentPod := &corev1.Pod{}
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)).To(Succeed())
					g.Expect(currentPod.Spec.NodeName).To(Equal("worker-1"))
					g.Expect(currentPod.Status.Phase).To(Equal(corev1.PodRunning))
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("Trigger ServiceChain reconciliation to check for pod restart")
				// Update the ServiceChain to trigger reconciliation
				serviceChain.Spec.Switches[0].ServiceMTU = ptr.To(1600) // Change MTU to trigger reconciliation
				Expect(testClient.Patch(ctx, serviceChain, client.Merge)).To(Succeed())

				By("Verify pod is eventually deleted due to digest mismatch")
				Eventually(func(g Gomega) {
					currentPod := &corev1.Pod{}
					err := testClient.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)
					// Pod should either be deleted (not found) or marked for deletion
					if err != nil {
						g.Expect(err.Error()).To(ContainSubstring("not found"))
					} else {
						g.Expect(currentPod.DeletionTimestamp).NotTo(BeNil())
					}
				}).WithTimeout(15 * time.Second).Should(Succeed())
			})

			It("should not delete pod when ServiceChain is on wrong node", func() {
				By("Create ServiceInterface on correct node")
				serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-wrong-node", "firewall", "sfceth1")
				cleanupObjects = append(cleanupObjects, serviceInterface)

				By("Create ServiceChain on wrong node")
				serviceChain := &dpuservicev1.ServiceChain{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "sc-wrong-node",
						Namespace: "default",
					},
					Spec: dpuservicev1.ServiceChainSpec{
						Node: ptr.To("worker-2"), // Wrong node
						Switches: []dpuservicev1.Switch{
							{
								ServiceMTU: ptr.To(1500),
								Ports: []dpuservicev1.Port{
									{
										ServiceInterface: dpuservicev1.ServiceIfc{
											MatchLabels: map[string]string{
												dpuservicev1.DPFServiceIDLabelKey: "firewall",
												"svc.dpu.nvidia.com/interface":    "sfceth1",
											},
										},
									},
								},
							},
						},
					},
				}
				Expect(testClient.Create(ctx, serviceChain)).To(Succeed())
				cleanupObjects = append(cleanupObjects, serviceChain)

				By("Create Pod with digest mismatch")
				pod := createTestPodWithName(
					"test-pod-wrong-node",
					map[string]string{
						NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
						NetworkDigestAnnotation: "old-digest",
					},
					map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				)

				By("Get fresh copy before updating status to avoid conflicts")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())

				By("Set pod to Running state")
				pod.Status.Phase = corev1.PodRunning
				Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

				By("Trigger ServiceChain reconciliation")
				serviceChain.Spec.Switches[0].ServiceMTU = ptr.To(1600)
				Expect(testClient.Patch(ctx, serviceChain, client.Merge)).To(Succeed())

				By("Verify pod is consistently not deleted due to wrong node")
				Consistently(func(g Gomega) {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Second).Should(Succeed())
			})
		})

		Context("Pod annotation scenarios", func() {
			It("should not delete pod when no network annotation exists", func() {
				By("Create ServiceInterface")
				serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-no-annot", "firewall", "sfceth1")
				cleanupObjects = append(cleanupObjects, serviceInterface)

				By("Create ServiceChain")
				serviceChain := createServiceChainWithServiceInterface(ctx, "sc-no-annot", nil, "firewall", "sfceth1", ptr.To(1500))
				cleanupObjects = append(cleanupObjects, serviceChain)

				By("Create Pod with no network annotation")
				pod := createTestPodWithName(
					"test-pod-no-annot",
					map[string]string{}, // No network annotation
					map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				)

				By("Get fresh copy before updating status to avoid conflicts")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())

				By("Set pod to Running state")
				pod.Status.Phase = corev1.PodRunning
				Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

				By("Trigger ServiceChain reconciliation")
				serviceChain.Spec.Switches[0].ServiceMTU = ptr.To(1600)
				Expect(testClient.Patch(ctx, serviceChain, client.Merge)).To(Succeed())

				By("Verify pod is consistently not deleted")
				Consistently(func(g Gomega) {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Second).Should(Succeed())
			})

			It("should not delete pod when no digest annotation exists", func() {
				By("Create ServiceInterface")
				serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-no-digest", "firewall", "sfceth1")
				cleanupObjects = append(cleanupObjects, serviceInterface)

				By("Create ServiceChain")
				serviceChain := createServiceChainWithServiceInterface(ctx, "sc-no-digest", nil, "firewall", "sfceth1", ptr.To(1500))
				cleanupObjects = append(cleanupObjects, serviceChain)

				By("Create Pod with network annotation but no digest")
				pod := createTestPodWithName(
					"test-pod-no-digest",
					map[string]string{
						NetworkAttachmentAnnot: `[{"name":"mybrsfc","interface":"sfceth1"}]`,
						// No NetworkDigestAnnotation
					},
					map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				)

				By("Get fresh copy before updating status to avoid conflicts")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())

				By("Set pod to Running state")
				pod.Status.Phase = corev1.PodRunning
				Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

				By("Trigger ServiceChain reconciliation")
				serviceChain.Spec.Switches[0].ServiceMTU = ptr.To(1600)
				Expect(testClient.Patch(ctx, serviceChain, client.Merge)).To(Succeed())

				By("Verify pod is consistently not deleted")
				Consistently(func(g Gomega) {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Second).Should(Succeed())
			})

			It("should not delete pod when digest annotation matches", func() {
				By("Create ServiceInterface")
				serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-match-digest", "firewall", "sfceth1")
				cleanupObjects = append(cleanupObjects, serviceInterface)

				By("Create ServiceChain")
				serviceChain := createServiceChainWithServiceInterface(ctx, "sc-match-digest", nil, "firewall", "sfceth1", ptr.To(1500))
				cleanupObjects = append(cleanupObjects, serviceChain)

				By("Create Pod with matching digest")
				pod := createTestPodWithName(
					"test-pod-match-digest",
					map[string]string{
						NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
						NetworkDigestAnnotation: "matching-digest",
					},
					map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				)

				By("Get fresh copy before updating status to avoid conflicts")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())

				By("Set pod to Running state")
				pod.Status.Phase = corev1.PodRunning
				Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

				By("Trigger ServiceChain reconciliation")
				serviceChain.Spec.Switches[0].ServiceMTU = ptr.To(1600)
				Expect(testClient.Patch(ctx, serviceChain, client.Merge)).To(Succeed())

				By("Verify pod is consistently not deleted")
				Consistently(func(g Gomega) {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Second).Should(Succeed())
			})
		})

		Context("Pod state scenarios", func() {
			It("should not delete pod when pod is in Pending state", func() {
				By("Create ServiceInterface")
				serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-pending", "firewall", "sfceth1")
				cleanupObjects = append(cleanupObjects, serviceInterface)

				By("Create ServiceChain")
				serviceChain := createServiceChainWithServiceInterface(ctx, "sc-pending", nil, "firewall", "sfceth1", ptr.To(1500))
				cleanupObjects = append(cleanupObjects, serviceChain)

				By("Create Pod in Pending state with digest mismatch")
				pod := createTestPodWithName(
					"test-pod-pending-state",
					map[string]string{
						NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
						NetworkDigestAnnotation: "old-digest",
					},
					map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				)

				By("Set pod to Pending state")
				pod.Status.Phase = corev1.PodPending
				Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

				By("Verify pod is consistently not deleted when in Pending state")
				Consistently(func(g Gomega) {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Second).Should(Succeed())
			})

			It("should not delete pod when pod is being deleted", func() {
				By("Create ServiceInterface")
				serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-deleting", "firewall", "sfceth1")
				cleanupObjects = append(cleanupObjects, serviceInterface)

				By("Create ServiceChain")
				serviceChain := createServiceChainWithServiceInterface(ctx, "sc-deleting", nil, "firewall", "sfceth1", ptr.To(1500))
				cleanupObjects = append(cleanupObjects, serviceChain)

				By("Create Pod with digest mismatch")
				pod := createTestPodWithName(
					"test-pod-deleting-state",
					map[string]string{
						NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
						NetworkDigestAnnotation: "old-digest",
					},
					map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				)

				By("Get fresh copy before updating status to avoid conflicts")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())

				By("Set pod to Running state")
				pod.Status.Phase = corev1.PodRunning
				Expect(testClient.Status().Patch(ctx, pod, client.Merge)).To(Succeed())

				By("Delete the pod to mark it for deletion")
				Expect(testClient.Delete(ctx, pod)).To(Succeed())

				By("Verify pod is marked for deletion")
				Eventually(func(g Gomega) {
					currentPod := &corev1.Pod{}
					err := testClient.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(currentPod.DeletionTimestamp).NotTo(BeNil())
				}).WithTimeout(5 * time.Second).Should(Succeed())

				By("Trigger ServiceChain reconciliation")
				serviceChain.Spec.Switches[0].ServiceMTU = ptr.To(1600)
				Expect(testClient.Patch(ctx, serviceChain, client.Merge)).To(Succeed())

				By("Verify pod is consistently not deleted again when already marked for deletion")
				Consistently(func(g Gomega) {
					currentPod := &corev1.Pod{}
					err := testClient.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)
					g.Expect(err).ToNot(HaveOccurred())
					// The pod should still exist and be marked for deletion, but not be deleted again
					g.Expect(currentPod.DeletionTimestamp).NotTo(BeNil())
				}).WithTimeout(10 * time.Second).Should(Succeed())
			})
		})

		Context("Multiple pods scenarios", func() {
			It("should delete only pods with digest mismatch when multiple pods exist", func() {
				By("Create ServiceInterface")
				serviceInterface := createServiceInterfaceForService(ctx, "firewall-sfceth1-multi", "firewall", "sfceth1")
				cleanupObjects = append(cleanupObjects, serviceInterface)

				By("Create ServiceChain")
				serviceChain := createServiceChainWithServiceInterface(ctx, "sc-multi", nil, "firewall", "sfceth1", ptr.To(1500))
				cleanupObjects = append(cleanupObjects, serviceChain)

				By("Create Pod1 with digest mismatch")
				pod1 := createTestPodWithName(
					"test-pod-multi-mismatch-1",
					map[string]string{
						NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
						NetworkDigestAnnotation: "old-digest-1",
					},
					map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				)

				By("Create Pod2 with matching digest")
				pod2 := createTestPodWithName(
					"test-pod-multi-match-2",
					map[string]string{
						NetworkAttachmentAnnot:  `[{"name":"mybrsfc","interface":"sfceth1"}]`,
						NetworkDigestAnnotation: "matching-digest",
					},
					map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "firewall",
					},
				)

				By("Get fresh copies before updating status to avoid conflicts")
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod1), pod1)).To(Succeed())
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(pod2), pod2)).To(Succeed())

				By("Set both pods to Running state")
				pod1.Status.Phase = corev1.PodRunning
				Expect(testClient.Status().Patch(ctx, pod1, client.Merge)).To(Succeed())
				pod2.Status.Phase = corev1.PodRunning
				Expect(testClient.Status().Patch(ctx, pod2, client.Merge)).To(Succeed())

				By("Trigger ServiceChain reconciliation")
				serviceChain.Spec.Switches[0].ServiceMTU = ptr.To(1600)
				Expect(testClient.Patch(ctx, serviceChain, client.Merge)).To(Succeed())

				By("Verify Pod1 is eventually deleted")
				Eventually(func(g Gomega) {
					currentPod := &corev1.Pod{}
					err := testClient.Get(ctx, client.ObjectKeyFromObject(pod1), currentPod)
					// Pod should either be deleted (not found) or marked for deletion
					if err != nil {
						g.Expect(err.Error()).To(ContainSubstring("not found"))
					} else {
						g.Expect(currentPod.DeletionTimestamp).NotTo(BeNil())
					}
				}).WithTimeout(15 * time.Second).Should(Succeed())

				By("Verify Pod2 is consistently not deleted")
				Consistently(func(g Gomega) {
					err := testClient.Get(ctx, client.ObjectKeyFromObject(pod2), &corev1.Pod{})
					g.Expect(err).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Second).Should(Succeed())
			})
		})
	})
})
