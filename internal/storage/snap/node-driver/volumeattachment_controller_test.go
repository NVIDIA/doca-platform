/*
COPYRIGHT 2025 NVIDIA

Licensed under the Apache License, Version 2.0 (the License);
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an AS IS BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"slices"
	"time"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("VolumeAttachmentReconciler in node-driver", Ordered, func() {
	var (
		ctx context.Context
		ns  *corev1.Namespace
	)

	BeforeAll(func() {
		ctx = context.Background()
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "node-driver-test-",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	AfterAll(func() {
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	Context("When VolumeAttachment is created but storageAttached=false", func() {
		var va *snapstoragev1.VolumeAttachment
		BeforeEach(func() {
			va = &snapstoragev1.VolumeAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "va-false",
					Namespace: ns.Name,
				},
				Spec: snapstoragev1.VolumeAttachmentSpec{
					NodeName: "test-node",
					Source: snapstoragev1.VolumeSource{
						VolumeRef: &snapstoragev1.ObjectRef{
							APIVersion: snapstoragev1.GroupVersion.String(),
							Kind:       snapstoragev1.VolumeKind,
							Name:       "some-volume",
							Namespace:  ns.Name,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, va)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, va)).To(Succeed())
			// Wait for the VolumeAttachment to be fully removed before proceeding
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(va), &snapstoragev1.VolumeAttachment{})
				return apierrors.IsNotFound(err)
			}).WithTimeout(5*time.Second).WithPolling(500*time.Millisecond).
				Should(BeTrue(), "Expected VolumeAttachment to be fully removed")
		})

		It("should not add finalizer or handle attachment", func() {
			fetched := &snapstoragev1.VolumeAttachment{}
			// Check if the finalizer or the DPU.Attached changed
			Consistently(func() bool {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(va), fetched); err != nil {
					return false
				}
				// If the finalizer is added or DPU.Attached set to true, that means the
				// controller continued despite storageAttached=false
				return containsFinalizer(fetched, dpuFinalizer) || fetched.Status.DPU.Attached
			}).WithTimeout(2*time.Second).WithPolling(200*time.Millisecond).
				Should(BeFalse(), "Expected no changes while storageAttached=false")
		})
	})

	Context("When deleting a VolumeAttachment that is attached", func() {
		var va *snapstoragev1.VolumeAttachment
		BeforeEach(func() {
			va = &snapstoragev1.VolumeAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "va-delete",
					Namespace: ns.Name,
				},
				Spec: snapstoragev1.VolumeAttachmentSpec{
					NodeName: "test-node",
					Source: snapstoragev1.VolumeSource{
						VolumeRef: &snapstoragev1.ObjectRef{
							APIVersion: snapstoragev1.GroupVersion.String(),
							Kind:       snapstoragev1.VolumeKind,
							Name:       "some-volume-to-detach",
							Namespace:  ns.Name,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, va)).To(Succeed())
			Eventually(func() error {
				fetched := &snapstoragev1.VolumeAttachment{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(va), fetched); err != nil {
					return err
				}
				fetched.Status.StorageAttached = true
				return k8sClient.Status().Update(ctx, fetched)
			}).Should(Succeed())

			// Wait for the controller to process the change and add the finalizer
			Eventually(func() bool {
				fetched := &snapstoragev1.VolumeAttachment{}
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(va), fetched)
				return containsFinalizer(fetched, dpuFinalizer)
			}).WithTimeout(10*time.Second).WithPolling(500*time.Millisecond).
				Should(BeTrue(), "Expected finalizer to appear once attached")

			// Update DPU.Attached to true
			Eventually(func() error {
				fetched := &snapstoragev1.VolumeAttachment{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(va), fetched); err != nil {
					return err
				}
				fetched.Status.DPU.Attached = true
				return k8sClient.Status().Update(ctx, fetched)
			}).Should(Succeed())
		})

		It("should remove the finalizer after handleDetachment", func() {
			Expect(k8sClient.Delete(ctx, va)).To(Succeed())
			Eventually(func() bool {
				fetched := &snapstoragev1.VolumeAttachment{}
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(va), fetched)
				// If it's fully gone, that means finalizer was removed => success
				if err != nil {
					return true
				}
				// If the finalizer is no longer present, it can be deleted soon
				return !containsFinalizer(fetched, dpuFinalizer)
			}).WithTimeout(10*time.Second).WithPolling(500*time.Millisecond).
				Should(BeTrue(), "Expected finalizer removal after detachment")
		})
	})
})

// Utility to check finalizer presence
func containsFinalizer(va *snapstoragev1.VolumeAttachment, finalizer string) bool {
	return slices.Contains(va.GetFinalizers(), finalizer)
}

// TODO: add more tests
